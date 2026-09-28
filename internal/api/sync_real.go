package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"gorm.io/gorm"
)

type syncTask struct {
	region       string
	collector    string
	resourceType string
}

type syncResult struct {
	task      syncTask
	resources []model.Resource
	metrics   []cloud.Series
	err       error
}

func tasksFor(a model.CloudAccount, regions []string) []syncTask {
	tasks := make([]syncTask, 0, len(regions)*4+1)
	collectors := []syncTask{
		{collector: "vm", resourceType: "vm"},
		{collector: "rds", resourceType: "rds"},
	}
	if a.Provider == "aliyun" {
		collectors = append(collectors,
			syncTask{collector: "clb", resourceType: "lb"},
			syncTask{collector: "alb", resourceType: "lb"},
		)
	} else {
		collectors = append(collectors, syncTask{collector: "lb", resourceType: "lb"})
	}
	for _, region := range regions {
		for _, collector := range collectors {
			collector.region = region
			tasks = append(tasks, collector)
		}
	}
	tasks = append(tasks, syncTask{region: regions[0], collector: "oss", resourceType: "oss"})
	return tasks
}

func (s *Server) runRealSync(ctx context.Context, j model.SyncJob, a model.CloudAccount) {
	plain, err := secret.Decrypt(s.Config.SecretKey, a.SecretEncrypted)
	if err != nil {
		s.finishSync(j, "failed", 0, 1, err.Error())
		return
	}
	p := s.provider(a.Provider)
	if p == nil {
		s.finishSync(j, "failed", 0, 1, "不支持的云厂商: "+a.Provider)
		return
	}
	regions := splitRegions(a.Regions)
	if a.AutoRegions {
		validateCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
		identity, validateErr := p.Validate(validateCtx, a, plain)
		cancel()
		if validateErr != nil {
			s.finishSync(j, "failed", 0, 1, "刷新地域失败: "+validateErr.Error())
			return
		}
		if len(identity.Regions) > 0 {
			regions = identity.Regions
			s.DB.Model(&a).Updates(map[string]any{"regions": strings.Join(regions, ","), "uid": identity.UID})
		}
	}
	if len(regions) == 0 {
		s.finishSync(j, "failed", 0, 1, "未选择同步地域")
		return
	}

	tasks := tasksFor(a, regions)
	s.DB.Model(&j).Update("tasks_total", len(tasks))
	results := make(chan syncResult, len(tasks))
	taskLimit := make(chan struct{}, 4)
	metricLimit := make(chan struct{}, 8)
	var workers sync.WaitGroup
	for _, task := range tasks {
		workers.Add(1)
		go func(task syncTask) {
			defer workers.Done()
			select {
			case taskLimit <- struct{}{}:
				defer func() { <-taskLimit }()
			case <-ctx.Done():
				results <- syncResult{task: task, err: ctx.Err()}
				return
			}
			taskCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			defer cancel()
			resources, listErr := p.List(taskCtx, a, plain, task.region, task.collector)
			result := syncResult{task: task, resources: resources, err: listErr}
			if listErr == nil && task.resourceType == "vm" {
				result.metrics = collectCPUMetrics(taskCtx, p, a, plain, resources, metricLimit)
				applyCPUSnapshots(resources, result.metrics)
			}
			results <- result
		}(task)
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	done, successCount := 0, 0
	issues := make([]string, 0)
	cpuHours := map[time.Time]struct {
		sum   float64
		count int
	}{}
	for result := range results {
		done++
		s.DB.Model(&j).Update("tasks_done", done)
		if result.err != nil {
			if errors.Is(result.err, context.Canceled) && ctx.Err() != nil {
				continue
			}
			if !unsupportedRegion(result.err) {
				issues = append(issues, fmt.Sprintf("%s %s: %v", result.task.region, result.task.collector, result.err))
			}
			continue
		}
		if err := s.saveTask(a, result.task, result.resources); err != nil {
			issues = append(issues, fmt.Sprintf("%s %s 保存失败: %v", result.task.region, result.task.collector, err))
			continue
		}
		successCount++
		metricFailures := 0
		for _, series := range result.metrics {
			if series.Error != "" {
				metricFailures++
				continue
			}
			for _, point := range series.Points {
				hour := point.Time.UTC().Truncate(time.Hour)
				value := cpuHours[hour]
				value.sum += point.Value
				value.count++
				cpuHours[hour] = value
			}
		}
		if metricFailures > 0 {
			issues = append(issues, fmt.Sprintf("%s vm: %d 台主机 CPU 指标获取失败", result.task.region, metricFailures))
		}
	}
	if ctx.Err() != nil {
		s.finishSync(j, "cancelled", done, len(issues), "用户取消")
		return
	}
	for hour, value := range cpuHours {
		if value.count == 0 {
			continue
		}
		row := model.HostCPUHourly{AccountID: a.ID, Provider: a.Provider, Hour: hour}
		s.DB.Where("account_id = ? AND hour = ?", a.ID, hour).FirstOrCreate(&row)
		s.DB.Model(&row).Updates(map[string]any{"sum": value.sum, "count": value.count})
	}
	status := "success"
	if len(issues) > 0 {
		status = "partial"
		if successCount == 0 {
			status = "failed"
		}
	}
	s.finishSync(j, status, done, len(issues), strings.Join(issues, "\n"))
}

func splitRegions(value string) []string {
	seen := map[string]bool{}
	regions := make([]string, 0)
	for _, region := range strings.Split(value, ",") {
		region = strings.TrimSpace(region)
		if region != "" && !seen[region] {
			seen[region] = true
			regions = append(regions, region)
		}
	}
	return regions
}

func collectCPUMetrics(ctx context.Context, p cloud.Provider, a model.CloudAccount, plain string, resources []model.Resource, limit chan struct{}) []cloud.Series {
	series := make([]cloud.Series, len(resources))
	jobs := make(chan int)
	var workers sync.WaitGroup
	workerCount := min(8, len(resources))
	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				select {
				case limit <- struct{}{}:
				case <-ctx.Done():
					series[index] = cloud.Series{ResourceID: resources[index].ID, Error: ctx.Err().Error()}
					continue
				}
				value, err := p.Metrics(ctx, a, plain, resources[index], "cpu", 24*time.Hour)
				<-limit
				if err != nil {
					value.Error = err.Error()
				}
				series[index] = value
			}
		}()
	}
	for index := range resources {
		select {
		case jobs <- index:
		case <-ctx.Done():
			series[index] = cloud.Series{ResourceID: resources[index].ID, Error: ctx.Err().Error()}
		}
	}
	close(jobs)
	workers.Wait()
	return series
}

func applyCPUSnapshots(resources []model.Resource, series []cloud.Series) {
	now := time.Now()
	for index := range resources {
		if index >= len(series) || series[index].Error != "" || len(series[index].Points) == 0 {
			continue
		}
		resources[index].CPU24H = series[index].Average
		var sum float64
		var count int
		for _, point := range series[index].Points {
			if point.Time.After(now.Add(-time.Hour)) {
				sum += point.Value
				count++
			}
		}
		if count > 0 {
			resources[index].CPU1H = sum / float64(count)
		} else {
			resources[index].CPU1H = series[index].Current
		}
		resources[index].MetricsAt = &now
	}
}

func unsupportedRegion(err error) bool {
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"unsupportedoperation", "operationunsupported", "invalidregionid", "regionnotsupport",
		"not supported in this region", "not support this region", "service is not available in this region", "optinrequired",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	return false
}

func (s *Server) saveTask(a model.CloudAccount, task syncTask, resources []model.Resource) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		query := syncScope(tx, a, task)
		var old []model.Resource
		if err := query.Find(&old).Error; err != nil {
			return err
		}
		oldByID := map[string]model.Resource{}
		for _, resource := range old {
			oldByID[resource.CloudID] = resource
		}
		seen := make([]string, 0, len(resources))
		for _, resource := range resources {
			resource.AccountID = a.ID
			resource.Provider = a.Provider
			resource.Type = task.resourceType
			if prior, ok := oldByID[resource.CloudID]; ok {
				resource.ID = prior.ID
				resource.CreatedAt = prior.CreatedAt
				if resource.MetricsAt == nil {
					resource.CPU1H = prior.CPU1H
					resource.CPU24H = prior.CPU24H
					resource.MetricsAt = prior.MetricsAt
				}
			}
			if err := tx.Save(&resource).Error; err != nil {
				return err
			}
			seen = append(seen, resource.CloudID)
		}
		deleteQuery := syncScope(tx, a, task)
		if len(seen) > 0 {
			deleteQuery = deleteQuery.Where("cloud_id NOT IN ?", seen)
		}
		return deleteQuery.Delete(&model.Resource{}).Error
	})
}

func syncScope(db *gorm.DB, a model.CloudAccount, task syncTask) *gorm.DB {
	query := db.Where("account_id = ? AND type = ?", a.ID, task.resourceType)
	if task.resourceType != "oss" {
		query = query.Where("region = ?", task.region)
	}
	if a.Provider == "aliyun" && task.resourceType == "lb" {
		query = query.Where("spec = ?", strings.ToUpper(task.collector))
	}
	return query
}

func (s *Server) finishSync(j model.SyncJob, status string, done, issueCount int, detail string) {
	var count int64
	s.DB.Model(&model.Resource{}).Where("account_id = ?", j.AccountID).Count(&count)
	now := time.Now()
	s.DB.Model(&j).Updates(map[string]any{"status": status, "tasks_done": done, "error_count": issueCount, "errors": detail, "resource_count": count, "finished_at": now})
	s.DB.Create(&model.AuditLog{Category: "sync", Action: "finish", Result: status, Actor: j.TriggeredBy, Target: j.AccountName, Detail: detail})
}
