package service

import (
	"time"

	"gorm.io/gorm"
)

// Allow two sync intervals and at least 90 minutes.
func freshnessWindow(interval time.Duration) time.Duration {
	if interval*2 > 90*time.Minute {
		return interval * 2
	}
	return 90 * time.Minute
}

func withFreshCPU(q *gorm.DB, now time.Time, window time.Duration) *gorm.DB {
	return q.Where("resources.synced_at >= ? AND resources.metrics_at >= ? AND resources.metrics_at <= ?", now.Add(-window), now.Add(-window-time.Hour), now.Add(5*time.Minute)).
		Where("NOT EXISTS (SELECT 1 FROM cloud_accounts ca WHERE ca.id = resources.account_id AND ca.last_sync_status = 'failed')").
		Where("NOT EXISTS (SELECT 1 FROM sync_scopes ss WHERE ss.account_id = resources.account_id AND ss.region = resources.region AND ss.type IN ('vm', 'metrics') AND ss.status = 'failed')")
}

func freshAt(at *time.Time, now time.Time, window time.Duration) bool {
	return at != nil && !at.IsZero() && !at.Before(now.Add(-window)) && !at.After(now.Add(5*time.Minute))
}

// Cloud CPU timestamps identify the start of an hourly average. Its age is
// measured from the end of that hour, while keeping the original timestamp.
func freshCPUAt(at *time.Time, now time.Time, window time.Duration) bool {
	return freshAt(at, now, window+time.Hour)
}
