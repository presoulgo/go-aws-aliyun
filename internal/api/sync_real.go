package api

import (
	"context"
	"fmt"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"gorm.io/gorm"
	"strings"
	"time"
)

type syncTask struct{ region, typ string }

func (s *Server) runRealSync(ctx context.Context, j model.SyncJob, a model.CloudAccount) {
	plain, err := secret.Decrypt(s.Config.SecretKey, a.SecretEncrypted)
	if err != nil {
		s.finishSync(j, "failed", 0, 1, err.Error())
		return
	}
	p := s.provider(a.Provider)
	regions := []string{}
	for _, r := range strings.Split(a.Regions, ",") {
		if strings.TrimSpace(r) != "" {
			regions = append(regions, strings.TrimSpace(r))
		}
	}
	if a.AutoRegions {
		identity, e := p.Validate(ctx, a, plain)
		if e == nil && len(identity.Regions) > 0 {
			regions = identity.Regions
			s.DB.Model(&a).Updates(map[string]any{"regions": strings.Join(regions, ","), "uid": identity.UID})
		}
	}
	if len(regions) == 0 {
		s.finishSync(j, "failed", 0, 1, "未选择同步地域")
		return
	}
	tasks := []syncTask{}
	for _, region := range regions {
		for _, typ := range []string{"vm", "rds", "lb"} {
			tasks = append(tasks, syncTask{region, typ})
		}
	}
	tasks = append(tasks, syncTask{regions[0], "oss"})
	s.DB.Model(&j).Update("tasks_total", len(tasks))
	done, successCount := 0, 0
	errors := []string{}
	cpuHours := map[time.Time]struct {
		sum   float64
		count int
	}{}
	for _, task := range tasks {
		if ctx.Err() != nil {
			s.finishSync(j, "cancelled", done, len(errors), "用户取消")
			return
		}
		resources, err := p.List(ctx, a, plain, task.region, task.typ)
		if err == nil {
			if task.typ == "vm" {
				for i := range resources {
					series, e := p.Metrics(ctx, a, plain, resources[i], "cpu", 24*time.Hour)
					if e == nil && len(series.Points) > 0 {
						resources[i].CPU24H = series.Average
						resources[i].CPU1H = series.Current
						now := time.Now()
						resources[i].MetricsAt = &now
						for _, pt := range series.Points {
							hour := pt.Time.UTC().Truncate(time.Hour)
							v := cpuHours[hour]
							v.sum += pt.Value
							v.count++
							cpuHours[hour] = v
						}
					}
				}
			}
			err = s.saveTask(a, task, resources)
		}
		if err != nil {
			if !strings.Contains(strings.ToLower(err.Error()), "not supported") {
				errors = append(errors, fmt.Sprintf("%s %s: %v", task.region, task.typ, err))
			}
		} else {
			successCount++
		}
		done++
		s.DB.Model(&j).Update("tasks_done", done)
	}
	for hour, v := range cpuHours {
		if v.count == 0 {
			continue
		}
		row := model.HostCPUHourly{AccountID: a.ID, Provider: a.Provider, Hour: hour}
		s.DB.Where("account_id = ? AND hour = ?", a.ID, hour).FirstOrCreate(&row)
		s.DB.Model(&row).Updates(map[string]any{"sum": v.sum, "count": v.count})
	}
	status := "success"
	if len(errors) > 0 {
		status = "partial"
		if successCount == 0 {
			status = "failed"
		}
	}
	s.finishSync(j, status, done, len(errors), strings.Join(errors, "\n"))
}
func (s *Server) saveTask(a model.CloudAccount, task syncTask, resources []model.Resource) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		q := tx.Where("account_id = ? AND type = ?", a.ID, task.typ)
		if task.typ != "oss" {
			q = q.Where("region = ?", task.region)
		}
		var old []model.Resource
		if err := q.Find(&old).Error; err != nil {
			return err
		}
		oldByID := map[string]model.Resource{}
		for _, r := range old {
			oldByID[r.CloudID] = r
		}
		seen := []string{}
		for _, r := range resources {
			r.AccountID = a.ID
			r.Provider = a.Provider
			if prior, ok := oldByID[r.CloudID]; ok {
				r.ID = prior.ID
				r.CreatedAt = prior.CreatedAt
				if r.MetricsAt == nil {
					r.CPU1H = prior.CPU1H
					r.CPU24H = prior.CPU24H
					r.MetricsAt = prior.MetricsAt
				}
			}
			if err := tx.Save(&r).Error; err != nil {
				return err
			}
			seen = append(seen, r.CloudID)
		}
		deleteQuery := tx.Where("account_id = ? AND type = ?", a.ID, task.typ)
		if task.typ != "oss" {
			deleteQuery = deleteQuery.Where("region = ?", task.region)
		}
		if len(seen) > 0 {
			deleteQuery = deleteQuery.Where("cloud_id NOT IN ?", seen)
		}
		return deleteQuery.Delete(&model.Resource{}).Error
	})
}
func (s *Server) finishSync(j model.SyncJob, status string, done, errors int, detail string) {
	var count int64
	s.DB.Model(&model.Resource{}).Where("account_id = ?", j.AccountID).Count(&count)
	now := time.Now()
	s.DB.Model(&j).Updates(map[string]any{"status": status, "tasks_done": done, "error_count": errors, "errors": detail, "resource_count": count, "finished_at": now})
}

var _ cloud.Provider
