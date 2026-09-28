package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaultsWhenFileMissing(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":8080" || cfg.Sync.Interval.D() != 30*time.Minute {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.Data.DBPath != filepath.Join("data", "ops.db") {
		t.Fatalf("db path = %q", cfg.Data.DBPath)
	}
}

func TestLoadFileAndEnvOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := `
app:
  name: 测试平台
server:
  addr: ":9000"
data:
  dir: /var/lib/ops
sync:
  interval: 45m
audit:
  retention: 90d
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPS_ADDR", ":7000")
	t.Setenv("OPS_DEMO", "true")
	cfg, err := Load(path, false)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != ":7000" {
		t.Errorf("env should override file, got %s", cfg.Server.Addr)
	}
	if cfg.App.Name != "测试平台" || !cfg.Demo {
		t.Errorf("unexpected app/demo: %+v", cfg.App)
	}
	if cfg.Sync.Interval.D() != 45*time.Minute {
		t.Errorf("interval = %v", cfg.Sync.Interval)
	}
	if cfg.Audit.Retention.D() != 90*24*time.Hour {
		t.Errorf("retention = %v", cfg.Audit.Retention)
	}
	if cfg.Data.DBPath != filepath.Join("/var/lib/ops", "ops.db") {
		t.Errorf("db path = %s", cfg.Data.DBPath)
	}
}

func TestLoadRejectsUnknownKeysAndBadValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("servr:\n  addr: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, false); err == nil {
		t.Fatal("expected unknown key error")
	}
	if err := os.WriteFile(path, []byte("sync:\n  concurrency: 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, false); err == nil {
		t.Fatal("expected validation error")
	}
}
