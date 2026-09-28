package config

import (
	"go.yaml.in/yaml/v3"
	"os"
	"strconv"
)

type Config struct {
	Address, DBPath, SecretKey, AdminPassword, AppName string
	Demo                                               bool
	IdleCPU                                            float64
	ExpiringDays                                       int
	AuditDays                                          int
}

func Load() (Config, error) {
	var file struct {
		App struct {
			Name string `yaml:"name"`
		} `yaml:"app"`
		Server struct {
			Address string `yaml:"address"`
		} `yaml:"server"`
		Store struct {
			Path string `yaml:"path"`
		} `yaml:"store"`
		Idle struct {
			CPUThreshold float64 `yaml:"cpu_threshold"`
		} `yaml:"idle"`
		Expiring struct {
			Days int `yaml:"days"`
		} `yaml:"expiring"`
		Audit struct {
			RetentionDays int `yaml:"retention_days"`
		} `yaml:"audit"`
	}
	path := os.Getenv("OPS_CONFIG")
	if path == "" {
		path = "configs/config.yaml"
	}
	if b, err := os.ReadFile(path); err == nil {
		if err := yaml.Unmarshal(b, &file); err != nil {
			return Config{}, err
		}
	} else if os.Getenv("OPS_CONFIG") != "" {
		return Config{}, err
	}
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	def := func(v, fallback string) string {
		if v != "" {
			return v
		}
		return fallback
	}
	idle, _ := strconv.ParseFloat(get("OPS_IDLE_CPU_THRESHOLD", def(strconv.FormatFloat(file.Idle.CPUThreshold, 'f', -1, 64), "5")), 64)
	if idle == 0 {
		idle = 5
	}
	exp, _ := strconv.Atoi(get("OPS_EXPIRING_DAYS", strconv.Itoa(file.Expiring.Days)))
	if exp == 0 {
		exp = 30
	}
	audit, _ := strconv.Atoi(get("OPS_AUDIT_RETENTION_DAYS", strconv.Itoa(file.Audit.RetentionDays)))
	if audit == 0 {
		audit = 180
	}
	return Config{Address: get("OPS_ADDR", def(file.Server.Address, ":8080")), DBPath: get("OPS_DB_PATH", def(file.Store.Path, "data/ops.db")), SecretKey: get("OPS_SECRET_KEY", ""), AdminPassword: get("OPS_ADMIN_PASSWORD", ""), AppName: get("OPS_APP_NAME", def(file.App.Name, "云枢")), Demo: get("OPS_DEMO", "") == "true", IdleCPU: idle, ExpiringDays: exp, AuditDays: audit}, nil
}
