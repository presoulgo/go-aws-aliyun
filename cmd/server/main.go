// Command server runs the 云枢 multi-cloud operations platform.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/presoulgo/go-aws-aliyun/internal/api"
	"github.com/presoulgo/go-aws-aliyun/internal/auth"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/aliyun"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/aws"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/demo"
	"github.com/presoulgo/go-aws-aliyun/internal/config"
	"github.com/presoulgo/go-aws-aliyun/internal/scheduler"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"github.com/presoulgo/go-aws-aliyun/internal/service"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
	"github.com/presoulgo/go-aws-aliyun/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "配置文件路径（默认尝试 configs/config.yaml）")
	demoFlag := flag.Bool("demo", false, "演示模式：使用生成的数据，不连接真实云厂商")
	showVersion := flag.Bool("version", false, "打印版本号")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return nil
	}

	path, optional := *configPath, false
	if path == "" {
		path, optional = filepath.Join("configs", "config.yaml"), true
	}
	cfg, err := config.Load(path, optional)
	if err != nil {
		return err
	}
	if *demoFlag {
		cfg.Demo = true
	}
	setupLogger(cfg.Log)
	gin.SetMode(gin.ReleaseMode)

	db, err := store.Open(cfg.Data.DBPath)
	if err != nil {
		return err
	}
	defer store.Close(db)

	masterKey, created, err := secret.LoadOrCreateKey(cfg.Security.MasterKey, filepath.Join(cfg.Data.Dir, "master.key"))
	if err != nil {
		return fmt.Errorf("加载主密钥失败: %w", err)
	}
	if created {
		slog.Warn("已生成主密钥，请妥善备份；丢失后需要重新录入所有云账号的 AccessKey", "path", filepath.Join(cfg.Data.Dir, "master.key"))
	}
	box, err := secret.NewBox(masterKey)
	if err != nil {
		return err
	}
	jwtKey, _, err := secret.LoadOrCreateKey(cfg.Security.JWTSecret, filepath.Join(cfg.Data.Dir, "jwt.key"))
	if err != nil {
		return fmt.Errorf("加载 JWT 密钥失败: %w", err)
	}

	audit := service.NewAuditService(db)
	users := service.NewUserService(db, audit,
		auth.NewTokens(jwtKey, cfg.Security.TokenTTL.D()),
		auth.NewLimiter(cfg.Security.LoginMaxFailures, cfg.Security.LoginLockDuration.D()),
		cfg.Security.LoginLockDuration.D())

	createdAdmin, generated, err := users.EnsureAdmin(cfg.Security.AdminPassword)
	if err != nil {
		return err
	}
	if createdAdmin {
		if generated != "" {
			slog.Warn("已创建初始管理员，请登录后尽快修改密码", "username", "admin", "password", generated)
		} else {
			slog.Info("已使用配置的密码创建初始管理员", "username", "admin")
		}
	}

	registry := cloud.NewRegistry(aws.New(), aliyun.New())
	if cfg.Demo {
		registry = cloud.NewRegistry(demo.NewAWS(), demo.NewAliyun())
		slog.Warn("演示模式已开启：云厂商数据均为生成的示例数据，不会连接真实云账号")
	}
	accounts := service.NewAccountService(db, box, registry, audit)
	syncer := service.NewSyncService(db, accounts, audit, service.SyncOptions{
		Concurrency: cfg.Sync.Concurrency,
		TaskTimeout: cfg.Sync.TaskTimeout.D(),
		KeepJobs:    cfg.Sync.KeepJobs,
	})
	if err := syncer.MarkInterrupted(); err != nil {
		return err
	}
	resources := service.NewResourceService(db, registry, cfg.Insight.IdleCPUThreshold, cfg.Insight.ExpiringDays)
	metrics := service.NewMetricsService(db, accounts, registry, cfg.Metrics.CacheTTL.D())
	dashboard := service.NewDashboardService(db, cfg.Insight.IdleCPUThreshold, cfg.Insight.ExpiringDays)
	changes := service.NewChangeService(db)
	alerts := service.NewAlertService(db, box, audit, cfg.App.ExternalURL)
	resources.SetSyncInterval(cfg.Sync.Interval.D())
	dashboard.SetSyncInterval(cfg.Sync.Interval.D())
	alerts.SetSyncInterval(cfg.Sync.Interval.D())
	if err := alerts.EnsureRules(); err != nil {
		return err
	}
	syncer.OnFinish = alerts.OnSyncFinished

	handler, err := api.New(api.Deps{
		Meta: api.Meta{
			Name: cfg.App.Name, Demo: cfg.Demo, Version: version,
			SyncIntervalMinutes: int(cfg.Sync.Interval.D().Minutes()),
			AuditRetentionDays:  int(cfg.Audit.Retention.D().Hours() / 24),
			ChangeRetentionDays: int(cfg.Changes.Retention.D().Hours() / 24),
		},
		TrustedProxies: cfg.Server.TrustedProxies,
		Users:          users,
		Audit:          audit,
		Accounts:       accounts,
		Sync:           syncer,
		Resources:      resources,
		Metrics:        metrics,
		Dashboard:      dashboard,
		Changes:        changes,
		Alerts:         alerts,
		Web:            web.Dist(),
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	syncer.SetBaseContext(ctx)

	startDelay := cfg.Sync.StartDelay.D()
	if cfg.Demo {
		seeded, err := seedDemoAccounts(ctx, accounts)
		if err != nil {
			return err
		}
		if seeded {
			startDelay = time.Second
		}
	}
	sched := &scheduler.Scheduler{
		Sync:         syncer,
		Audit:        audit,
		Changes:      changes,
		Alerts:       alerts,
		Interval:     cfg.Sync.Interval.D(),
		StartDelay:   startDelay,
		AuditRetain:  cfg.Audit.Retention.D(),
		ChangeRetain: cfg.Changes.Retention.D(),
	}
	go sched.Run(ctx)

	srv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("云枢已启动", "addr", cfg.Server.Addr, "version", version, "demo", cfg.Demo)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
	}
	slog.Info("正在停止服务…")
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	syncer.Wait()
	return err
}

// seedDemoAccounts creates the prototype's accounts on the first demo start.
func seedDemoAccounts(ctx context.Context, accounts *service.AccountService) (bool, error) {
	existing, err := accounts.List()
	if err != nil {
		return false, err
	}
	if len(existing) > 0 {
		return false, nil
	}
	for _, a := range demo.SeedAccounts() {
		_, err := accounts.Create(ctx, service.SystemActor, service.AccountInput{
			Name: a.Name, Provider: a.Provider, Partition: a.Partition, AccessKeyID: a.AccessKeyID,
			AccessKeySecret: a.Secret, RoleARN: a.RoleARN, Regions: a.Regions, Remark: a.Remark,
		})
		if err != nil {
			return false, fmt.Errorf("创建演示账号 %s 失败: %w", a.Name, err)
		}
	}
	slog.Info("已创建演示账号", "count", len(demo.SeedAccounts()))
	return true, nil
}

func setupLogger(c config.LogConfig) {
	var level slog.Level
	switch strings.ToLower(c.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler
	if strings.ToLower(c.Format) == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
}
