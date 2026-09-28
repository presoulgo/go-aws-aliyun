// Package config loads the server configuration from an optional YAML file and
// OPS_* environment variables (environment wins).
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type Config struct {
	App      AppConfig      `yaml:"app"`
	Server   ServerConfig   `yaml:"server"`
	Data     DataConfig     `yaml:"data"`
	Security SecurityConfig `yaml:"security"`
	Sync     SyncConfig     `yaml:"sync"`
	Metrics  MetricsConfig  `yaml:"metrics"`
	Insight  InsightConfig  `yaml:"insight"`
	Audit    AuditConfig    `yaml:"audit"`
	Log      LogConfig      `yaml:"log"`
	// Demo replaces the real cloud providers with generated data.
	Demo bool `yaml:"demo"`
}

type AppConfig struct {
	Name string `yaml:"name"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	// TrustedProxies lists proxy addresses whose X-Forwarded-For is honoured
	// when recording client IPs. Empty means the socket address is used.
	TrustedProxies []string `yaml:"trusted_proxies"`
}

type DataConfig struct {
	Dir    string `yaml:"dir"`
	DBPath string `yaml:"db_path"`
}

type SecurityConfig struct {
	// MasterKey encrypts cloud secrets (base64, 32 bytes). Generated into
	// <data dir>/master.key on first start when empty.
	MasterKey string `yaml:"master_key"`
	// JWTSecret signs login tokens. Generated into <data dir>/jwt.key when empty.
	JWTSecret         string   `yaml:"jwt_secret"`
	TokenTTL          Duration `yaml:"token_ttl"`
	AdminPassword     string   `yaml:"admin_password"`
	LoginMaxFailures  int      `yaml:"login_max_failures"`
	LoginLockDuration Duration `yaml:"login_lock_duration"`
}

type SyncConfig struct {
	Interval    Duration `yaml:"interval"`
	StartDelay  Duration `yaml:"start_delay"`
	Concurrency int      `yaml:"concurrency"`
	TaskTimeout Duration `yaml:"task_timeout"`
	KeepJobs    int      `yaml:"keep_jobs"`
}

type MetricsConfig struct {
	CacheTTL Duration `yaml:"cache_ttl"`
}

type InsightConfig struct {
	IdleCPUThreshold float64 `yaml:"idle_cpu_threshold"`
	ExpiringDays     int     `yaml:"expiring_days"`
}

type AuditConfig struct {
	Retention Duration `yaml:"retention"`
}

type LogConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// Default returns the configuration used when nothing is configured.
func Default() *Config {
	return &Config{
		App:    AppConfig{Name: "云枢"},
		Server: ServerConfig{Addr: ":8080"},
		Data:   DataConfig{Dir: "data"},
		Security: SecurityConfig{
			TokenTTL:          Duration(12 * time.Hour),
			LoginMaxFailures:  5,
			LoginLockDuration: Duration(5 * time.Minute),
		},
		Sync: SyncConfig{
			Interval:    Duration(30 * time.Minute),
			StartDelay:  Duration(10 * time.Second),
			Concurrency: 8,
			TaskTimeout: Duration(2 * time.Minute),
			KeepJobs:    50,
		},
		Metrics: MetricsConfig{CacheTTL: Duration(60 * time.Second)},
		Insight: InsightConfig{IdleCPUThreshold: 5, ExpiringDays: 30},
		Audit:   AuditConfig{Retention: Duration(180 * 24 * time.Hour)},
		Log:     LogConfig{Level: "info", Format: "text"},
	}
}

// Load reads the YAML file at path (skipped when path is empty or the file is
// missing and optional is true), then applies OPS_* environment overrides.
func Load(path string, optional bool) (*Config, error) {
	cfg := Default()
	if path != "" {
		raw, err := os.ReadFile(path)
		switch {
		case err == nil:
			dec := yaml.NewDecoder(bytes.NewReader(raw))
			dec.KnownFields(true)
			if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
				return nil, fmt.Errorf("解析配置文件 %s 失败: %w", path, err)
			}
		case errors.Is(err, os.ErrNotExist) && optional:
		default:
			return nil, fmt.Errorf("读取配置文件失败: %w", err)
		}
	}
	if err := applyEnv(cfg); err != nil {
		return nil, err
	}
	cfg.normalize()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) normalize() {
	if c.Data.Dir == "" {
		c.Data.Dir = "data"
	}
	if c.Data.DBPath == "" {
		c.Data.DBPath = filepath.Join(c.Data.Dir, "ops.db")
	}
	if strings.TrimSpace(c.App.Name) == "" {
		c.App.Name = "云枢"
	}
}

// Validate checks value ranges.
func (c *Config) Validate() error {
	switch {
	case c.Server.Addr == "":
		return errors.New("server.addr 不能为空")
	case c.Security.TokenTTL.D() < time.Minute:
		return errors.New("security.token_ttl 至少 1 分钟")
	case c.Security.LoginMaxFailures < 1:
		return errors.New("security.login_max_failures 至少为 1")
	case c.Sync.Interval.D() < time.Minute:
		return errors.New("sync.interval 至少 1 分钟")
	case c.Sync.Concurrency < 1 || c.Sync.Concurrency > 64:
		return errors.New("sync.concurrency 需在 1~64 之间")
	case c.Sync.TaskTimeout.D() < 5*time.Second:
		return errors.New("sync.task_timeout 至少 5 秒")
	case c.Insight.IdleCPUThreshold < 0 || c.Insight.IdleCPUThreshold > 100:
		return errors.New("insight.idle_cpu_threshold 需在 0~100 之间")
	case c.Insight.ExpiringDays < 1:
		return errors.New("insight.expiring_days 至少为 1")
	}
	if c.Sync.KeepJobs < 1 {
		c.Sync.KeepJobs = 50
	}
	return nil
}

func applyEnv(c *Config) error {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok {
			*dst = v
		}
	}
	str("OPS_APP_NAME", &c.App.Name)
	str("OPS_ADDR", &c.Server.Addr)
	str("OPS_DATA_DIR", &c.Data.Dir)
	str("OPS_DB_PATH", &c.Data.DBPath)
	str("OPS_MASTER_KEY", &c.Security.MasterKey)
	str("OPS_JWT_SECRET", &c.Security.JWTSecret)
	str("OPS_ADMIN_PASSWORD", &c.Security.AdminPassword)
	str("OPS_LOG_LEVEL", &c.Log.Level)
	str("OPS_LOG_FORMAT", &c.Log.Format)

	durations := map[string]*Duration{
		"OPS_TOKEN_TTL":        &c.Security.TokenTTL,
		"OPS_SYNC_INTERVAL":    &c.Sync.Interval,
		"OPS_SYNC_START_DELAY": &c.Sync.StartDelay,
		"OPS_AUDIT_RETENTION":  &c.Audit.Retention,
	}
	for key, dst := range durations {
		if v, ok := os.LookupEnv(key); ok {
			d, err := ParseDuration(v)
			if err != nil {
				return fmt.Errorf("环境变量 %s: %w", key, err)
			}
			*dst = Duration(d)
		}
	}
	if v, ok := os.LookupEnv("OPS_SYNC_CONCURRENCY"); ok {
		n, err := strconv.Atoi(v)
		if err != nil {
			return fmt.Errorf("环境变量 OPS_SYNC_CONCURRENCY: %w", err)
		}
		c.Sync.Concurrency = n
	}
	if v, ok := os.LookupEnv("OPS_DEMO"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("环境变量 OPS_DEMO: %w", err)
		}
		c.Demo = b
	}
	return nil
}
