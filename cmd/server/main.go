package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/presoulgo/go-aws-aliyun/internal/api"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/demo"
	"github.com/presoulgo/go-aws-aliyun/internal/config"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
	"github.com/presoulgo/go-aws-aliyun/web"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}
	if !cfg.Demo && cfg.SecretKey == "" {
		slog.Error("OPS_SECRET_KEY must be set outside demo mode")
		os.Exit(1)
	}
	if cfg.SecretKey == "" {
		b := make([]byte, 32)
		rand.Read(b)
		cfg.SecretKey = hex.EncodeToString(b)
	}
	if cfg.Demo {
		slog.Warn("演示模式已启用：仅供体验，不要用于生产环境")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0700); err != nil {
		panic(err)
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		panic(err)
	}
	s := api.New(db, cfg)
	password, err := s.EnsureAdmin()
	if err != nil {
		panic(err)
	}
	if password != "" {
		slog.Warn("首启管理员密码", "username", "admin", "password", password)
	}
	if cfg.Demo {
		if err := demo.Seed(db); err != nil {
			panic(err)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go s.Schedule(ctx)
	dist, _ := fs.Sub(web.Dist, "dist")
	static := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		f, err := dist.Open(p)
		if err != nil {
			index, readErr := fs.ReadFile(dist, "index.html")
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(index)
			return
		}
		f.Close()
		http.FileServer(http.FS(dist)).ServeHTTP(w, r)
	})
	server := &http.Server{Addr: cfg.Address, Handler: s.Router(static)}
	go func() { <-ctx.Done(); server.Shutdown(context.Background()) }()
	slog.Info("云枢已启动", "address", cfg.Address)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}
