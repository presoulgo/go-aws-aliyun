package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"gorm.io/gorm"
	"strings"
	"sync"
	"time"
)

func (s *Server) resourceQuery(c *gin.Context) *gorm.DB {
	q := s.DB.Model(&model.Resource{})
	for _, f := range []string{"provider", "type", "region", "status"} {
		if v := c.Query(f); v != "" {
			q = q.Where(f+" = ?", v)
		}
	}
	if v := c.Query("account_id"); v != "" {
		q = q.Where("account_id = ?", v)
	}
	if v := c.Query("q"); v != "" {
		for _, part := range strings.Fields(v) {
			if strings.HasPrefix(part, "env:") {
				q = q.Where("tags LIKE ?", "%\"env\":\""+strings.TrimPrefix(part, "env:")+"\"%")
			} else {
				like := "%" + part + "%"
				q = q.Where("name LIKE ? OR cloud_id LIKE ? OR ip LIKE ? OR tags LIKE ?", like, like, like, like)
			}
		}
	}
	if c.Query("idle") == "1" {
		q = q.Where("type = 'vm' AND status = 'running' AND metrics_at IS NOT NULL AND cpu24_h < ?", s.Config.IdleCPU)
	}
	if c.Query("expiring") == "1" {
		q = q.Where("expires_at BETWEEN ? AND ?", time.Now(), time.Now().AddDate(0, 0, s.Config.ExpiringDays))
	}
	return q
}

func (s *Server) resources(c *gin.Context) {
	q := s.resourceQuery(c)
	var total int64
	q.Count(&total)
	p, size := page(c)
	order := "id desc"
	if c.Query("sort") == "cpu_1h" {
		order = "cpu1_h desc"
	}
	if c.Query("sort") == "cpu_1h_asc" {
		order = "cpu1_h asc"
	}
	var items []model.Resource
	s.resourceQuery(c).Order(order).Offset((p - 1) * size).Limit(size).Find(&items)
	c.JSON(200, gin.H{"items": items, "total": total, "page": p, "page_size": size})
}
func (s *Server) filters(c *gin.Context) {
	q := s.resourceQuery(c)
	var counts []struct {
		Type  string `json:"type"`
		Count int64  `json:"count"`
	}
	q.Select("type, count(*) as count").Group("type").Scan(&counts)
	var regions, statuses []string
	s.resourceQuery(c).Distinct("region").Pluck("region", &regions)
	s.resourceQuery(c).Distinct("status").Pluck("status", &statuses)
	c.JSON(200, gin.H{"types": counts, "regions": regions, "statuses": statuses, "idle_cpu_threshold": s.Config.IdleCPU, "expiring_days": s.Config.ExpiringDays})
}
func (s *Server) resource(c *gin.Context) {
	var r model.Resource
	if s.DB.First(&r, id(c)).Error != nil {
		fail(c, 404, "资源不存在")
		return
	}
	c.JSON(200, r)
}
func catalog() any { return cloud.Catalog }

type cachedMetric struct {
	value cloud.Series
	until time.Time
}

func metricSpan(v string) time.Duration {
	switch v {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}
func (s *Server) metric(ctx context.Context, r model.Resource, metric string, span time.Duration) cloud.Series {
	key := fmt.Sprintf("%d:%s:%s", r.ID, metric, span)
	s.mu.Lock()
	cached, ok := s.metricCache[key]
	s.mu.Unlock()
	if ok && time.Now().Before(cached.until) {
		return cached.value
	}
	var a model.CloudAccount
	if err := s.DB.First(&a, r.AccountID).Error; err != nil {
		return cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: r.Provider, Error: "云账号不存在"}
	}
	plain := ""
	if !s.Config.Demo {
		var err error
		plain, err = secret.Decrypt(s.Config.SecretKey, a.SecretEncrypted)
		if err != nil {
			return cloud.Series{ResourceID: r.ID, Name: r.Name, Provider: r.Provider, Error: err.Error()}
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	provider := s.provider(r.Provider)
	if provider == nil {
		return cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: r.Provider, Error: "不支持的云厂商"}
	}
	result, err := provider.Metrics(ctx, a, plain, r, metric, span)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	s.mu.Lock()
	s.metricCache[key] = cachedMetric{value: result, until: time.Now().Add(2 * time.Minute)}
	s.mu.Unlock()
	return result
}
func (s *Server) resourceMetrics(c *gin.Context) {
	var r model.Resource
	if s.DB.First(&r, id(c)).Error != nil {
		fail(c, 404, "资源不存在")
		return
	}
	metrics := []string{"cpu", "memory", "network_in", "network_out"}
	if r.Type == "rds" {
		metrics = []string{"cpu", "connections", "storage", "memory"}
	}
	if r.Type == "lb" {
		metrics = []string{"connections", "qps", "network_in", "network_out"}
	}
	if r.Type == "oss" {
		metrics = []string{"storage"}
	}
	out := make([]cloud.Series, 0, len(metrics))
	out = make([]cloud.Series, len(metrics))
	var workers sync.WaitGroup
	for index, metric := range metrics {
		workers.Add(1)
		go func(index int, metric string) {
			defer workers.Done()
			out[index] = s.metric(c.Request.Context(), r, metric, metricSpan(c.DefaultQuery("range", "24h")))
		}(index, metric)
	}
	workers.Wait()
	c.JSON(200, gin.H{"items": out})
}
func (s *Server) metricsQuery(c *gin.Context) {
	var in struct {
		IDs    []uint `json:"ids"`
		Metric string `json:"metric"`
		Range  string `json:"range"`
	}
	if c.ShouldBindJSON(&in) != nil || len(in.IDs) > 4 || len(in.IDs) == 0 {
		fail(c, 400, "请选择 1 到 4 个资源")
		return
	}
	var resources []model.Resource
	s.DB.Where("id IN ?", in.IDs).Find(&resources)
	byID := make(map[uint]model.Resource, len(resources))
	for _, resource := range resources {
		byID[resource.ID] = resource
	}
	out := make([]cloud.Series, len(in.IDs))
	var workers sync.WaitGroup
	for index, resourceID := range in.IDs {
		resource, exists := byID[resourceID]
		if !exists {
			out[index] = cloud.Series{ResourceID: resourceID, Metric: in.Metric, Error: "资源不存在"}
			continue
		}
		workers.Add(1)
		go func(index int, resource model.Resource) {
			defer workers.Done()
			out[index] = s.metric(c.Request.Context(), resource, in.Metric, metricSpan(in.Range))
		}(index, resource)
	}
	workers.Wait()
	c.JSON(200, gin.H{"items": out})
}

func (s *Server) summary(c *gin.Context) {
	provider := c.Query("provider")
	aq := func() *gorm.DB {
		q := s.DB.Model(&model.CloudAccount{})
		if provider != "" {
			q = q.Where("provider = ?", provider)
		}
		return q
	}
	rq := func() *gorm.DB {
		q := s.DB.Model(&model.Resource{})
		if provider != "" {
			q = q.Where("provider = ?", provider)
		}
		return q
	}
	var accounts, resources, running, totalVM, idle, expiring int64
	aq().Count(&accounts)
	rq().Count(&resources)
	rq().Where("type = 'vm'").Count(&totalVM)
	rq().Where("type = 'vm' AND status = 'running'").Count(&running)
	rq().Where("type = 'vm' AND status = 'running' AND metrics_at IS NOT NULL AND cpu24_h < ?", s.Config.IdleCPU).Count(&idle)
	rq().Where("expires_at BETWEEN ? AND ?", time.Now(), time.Now().AddDate(0, 0, s.Config.ExpiringDays)).Count(&expiring)
	var distribution []struct {
		Provider string `json:"provider"`
		Type     string `json:"type"`
		Count    int64  `json:"count"`
	}
	rq().Select("provider,type,count(*) as count").Group("provider,type").Scan(&distribution)
	var top []model.Resource
	rq().Where("type = 'vm'").Order("cpu1_h desc").Limit(5).Find(&top)
	var exp []model.Resource
	rq().Where("expires_at BETWEEN ? AND ?", time.Now(), time.Now().AddDate(0, 0, s.Config.ExpiringDays)).Order("expires_at asc").Limit(5).Find(&exp)
	var jobs []model.SyncJob
	jq := s.DB.Order("id desc").Limit(5)
	if provider != "" {
		jq = jq.Where("provider = ?", provider)
	}
	jq.Find(&jobs)
	var cpu []struct {
		Provider string    `json:"provider"`
		Hour     time.Time `json:"hour"`
		Sum      float64   `json:"sum"`
		Count    int64     `json:"count"`
	}
	cq := s.DB.Model(&model.HostCPUHourly{}).Select("provider,hour,sum(sum) as sum,sum(count) as count").Where("hour >= ?", time.Now().Add(-24*time.Hour))
	if provider != "" {
		cq = cq.Where("provider = ?", provider)
	}
	cq.Group("provider,hour").Order("hour").Scan(&cpu)
	var nextDays int
	if len(exp) > 0 && exp[0].ExpiresAt != nil {
		nextDays = int(time.Until(*exp[0].ExpiresAt).Hours()/24) + 1
	}
	var regionLists []string
	aq().Pluck("regions", &regionLists)
	regions := 0
	for _, list := range regionLists {
		if list != "" {
			regions += len(strings.Split(list, ","))
		}
	}
	c.JSON(200, gin.H{"accounts": accounts, "resources": resources, "running_vms": running, "total_vms": totalVM, "idle_vms": idle, "idle_cpu_threshold": s.Config.IdleCPU, "expiring": expiring, "expiring_days": s.Config.ExpiringDays, "next_expiring_days": nextDays, "regions": regions, "distribution": distribution, "cpu_curve": cpu, "top_cpu": top, "expiring_items": exp, "recent_sync": jobs, "demo": s.Config.Demo})
}

func tags(r model.Resource) map[string]string {
	out := map[string]string{}
	json.Unmarshal([]byte(r.Tags), &out)
	return out
}
