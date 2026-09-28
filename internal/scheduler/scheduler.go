// Package scheduler runs periodic synchronization and housekeeping.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/service"
)

// Scheduler triggers syncs on an interval and cleans up old data.
type Scheduler struct {
	Sync         *service.SyncService
	Audit        *service.AuditService
	Interval     time.Duration
	StartDelay   time.Duration
	AuditRetain  time.Duration
	CPURetention time.Duration
}

// Run blocks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	if s.CPURetention <= 0 {
		s.CPURetention = 8 * 24 * time.Hour
	}
	first := time.NewTimer(s.StartDelay)
	defer first.Stop()
	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	housekeeping := time.NewTicker(6 * time.Hour)
	defer housekeeping.Stop()

	s.cleanup()
	for {
		select {
		case <-ctx.Done():
			return
		case <-first.C:
			s.syncAll()
		case <-ticker.C:
			s.syncAll()
		case <-housekeeping.C:
			s.cleanup()
		}
	}
}

func (s *Scheduler) syncAll() {
	jobs, err := s.Sync.SyncAll(service.SystemActor, false)
	if err != nil {
		slog.Error("定时同步失败", "err", err)
		return
	}
	slog.Info("已触发定时同步", "accounts", len(jobs))
}

func (s *Scheduler) cleanup() {
	now := time.Now()
	if s.AuditRetain > 0 {
		if n, err := s.Audit.Cleanup(now.Add(-s.AuditRetain)); err != nil {
			slog.Error("清理审计日志失败", "err", err)
		} else if n > 0 {
			slog.Info("已清理过期审计日志", "count", n)
		}
	}
	if err := s.Sync.CleanupCPUHistory(now.Add(-s.CPURetention)); err != nil {
		slog.Error("清理 CPU 历史失败", "err", err)
	}
}
