package api

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"time"
)

func (s *Server) startSync(a model.CloudAccount, actor string) model.SyncJob {
	s.mu.Lock()
	if jobID := s.accountRunning[a.ID]; jobID != 0 {
		var existing model.SyncJob
		s.DB.First(&existing, jobID)
		s.mu.Unlock()
		return existing
	}
	tasksTotal := 9
	if !s.Config.Demo {
		regions := splitRegions(a.Regions)
		if len(regions) > 0 {
			tasksTotal = len(tasksFor(a, regions))
		} else {
			tasksTotal = 0
		}
	}
	j := model.SyncJob{AccountID: a.ID, AccountName: a.Name, Provider: a.Provider, Status: "running", TriggeredBy: actor, TasksTotal: tasksTotal, StartedAt: time.Now()}
	s.DB.Create(&j)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancels[j.ID] = cancel
	s.accountRunning[a.ID] = j.ID
	s.mu.Unlock()
	go s.runSync(ctx, j, a)
	return j
}
func (s *Server) runSync(ctx context.Context, j model.SyncJob, a model.CloudAccount) {
	defer func() { s.mu.Lock(); delete(s.cancels, j.ID); delete(s.accountRunning, a.ID); s.mu.Unlock() }()
	if !s.Config.Demo {
		s.runRealSync(ctx, j, a)
		return
	}
	for i := 1; i <= 9; i++ {
		select {
		case <-ctx.Done():
			s.finishSync(j, "cancelled", i-1, 0, "用户取消")
			return
		case <-time.After(350 * time.Millisecond):
		}
		s.DB.Model(&j).Update("tasks_done", i)
	}
	status := "success"
	errorCount := 0
	errors := ""
	if s.Config.Demo && a.Provider == "aliyun" && a.Name == "主账号" {
		status = "partial"
		errorCount = 1
		errors = "cn-hongkong ALB 采集超时"
	}
	s.finishSync(j, status, 9, errorCount, errors)
}
func (s *Server) syncAccount(c *gin.Context) {
	a, ok := s.account(c)
	if !ok {
		return
	}
	if !a.Enabled {
		fail(c, 400, "账号已停用")
		return
	}
	j := s.startSync(a, user(c).Username)
	s.log(c, "sync", "start", "success", a.Name, fmt.Sprint(j.ID))
	c.JSON(202, j)
}
func (s *Server) syncAll(c *gin.Context) {
	var accounts []model.CloudAccount
	s.DB.Where("enabled = ?", true).Find(&accounts)
	jobs := []model.SyncJob{}
	for _, a := range accounts {
		jobs = append(jobs, s.startSync(a, user(c).Username))
	}
	s.log(c, "sync", "all", "success", "all", fmt.Sprint(len(jobs)))
	c.JSON(202, gin.H{"items": jobs})
}
func (s *Server) cancelJob(c *gin.Context) {
	jobID := id(c)
	s.mu.Lock()
	cancel := s.cancels[jobID]
	s.mu.Unlock()
	if cancel == nil {
		fail(c, 404, "任务不在运行")
		return
	}
	cancel()
	s.log(c, "sync", "cancel", "success", fmt.Sprint(jobID), "")
	c.JSON(200, gin.H{"ok": true})
}
func (s *Server) syncJobs(c *gin.Context) {
	q := s.DB.Model(&model.SyncJob{})
	if v := c.Query("account_id"); v != "" {
		q = q.Where("account_id = ?", v)
	}
	p, size := page(c)
	var total int64
	q.Count(&total)
	var items []model.SyncJob
	q.Order("id desc").Offset((p - 1) * size).Limit(size).Find(&items)
	c.JSON(200, gin.H{"items": items, "total": total})
}
func (s *Server) syncStatus(c *gin.Context) {
	var latest model.SyncJob
	s.DB.Where("status <> ?", "running").Order("finished_at desc").Limit(1).Find(&latest)
	var running int64
	s.DB.Model(&model.SyncJob{}).Where("status = ?", "running").Count(&running)
	status := "normal"
	if latest.Status == "partial" {
		status = "partial"
	}
	if latest.Status == "failed" {
		status = "failed"
	}
	c.JSON(200, gin.H{"status": status, "last_completed": latest.FinishedAt, "running": running})
}
func (s *Server) Schedule(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	cleanup := time.NewTicker(24 * time.Hour)
	defer cleanup.Stop()
	cleanupExpired := func() {
		s.DB.Where("created_at < ?", time.Now().AddDate(0, 0, -s.Config.AuditDays)).Delete(&model.AuditLog{})
		s.DB.Where("hour < ?", time.Now().AddDate(0, 0, -8)).Delete(&model.HostCPUHourly{})
	}
	cleanupExpired()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			var accounts []model.CloudAccount
			s.DB.Where("enabled = ?", true).Find(&accounts)
			for _, a := range accounts {
				s.startSync(a, "schedule")
			}
		case <-cleanup.C:
			cleanupExpired()
		}
	}
}
