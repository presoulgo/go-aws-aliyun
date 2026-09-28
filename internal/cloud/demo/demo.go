package demo

import (
	"context"
	"fmt"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"gorm.io/gorm"
	"hash/fnv"
	"math"
	"math/rand"
	"strings"
	"time"
)

type Provider struct{}

func (Provider) Validate(_ context.Context, a model.CloudAccount, _ string) (cloud.Identity, error) {
	return cloud.Identity{UID: "demo-" + a.Provider + "-001", Regions: []string{"cn-hangzhou", "cn-shanghai", "cn-beijing", "us-east-1", "ap-northeast-1"}}, nil
}
func (Provider) List(_ context.Context, _ model.CloudAccount, _, _, _ string) ([]model.Resource, error) {
	return nil, nil
}
func (Provider) Metrics(_ context.Context, _ model.CloudAccount, _ string, r model.Resource, metric string, span time.Duration) (cloud.Series, error) {
	return Metric(r, metric, span), nil
}

func Metric(r model.Resource, metric string, span time.Duration) cloud.Series {
	s := cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: r.Provider, Supported: cloud.Supports(r, metric), Unit: "%", Points: []cloud.Point{}}
	if !s.Supported {
		return s
	}
	if metric == "network_in" || metric == "network_out" {
		s.Unit = "Byte/s"
	}
	if metric == "connections" {
		s.Unit = "个"
	}
	if metric == "qps" {
		s.Unit = "次/s"
	}
	if metric == "storage" {
		s.Unit = "GB"
	}
	h := fnv.New64a()
	fmt.Fprintf(h, "%d:%s:%s", r.ID, r.CloudID, metric)
	seed := int64(h.Sum64() & 0x7fffffffffffffff)
	rng := rand.New(rand.NewSource(seed))
	n := 48
	step := span / time.Duration(n)
	if step < time.Minute {
		step = time.Minute
	}
	end := time.Now().UTC().Truncate(step)
	base := 25.0 + float64(seed%35)
	if metric == "cpu" && r.CPU1H > 0 {
		base = r.CPU1H
	}
	if metric == "network_in" || metric == "network_out" {
		base *= 18000
	}
	if metric == "connections" || metric == "qps" {
		base *= 9
	}
	if metric == "storage" {
		base *= 2
	}
	for i := 0; i < n; i++ {
		v := base*(0.75+0.25*math.Sin(float64(i)/5)) + rng.Float64()*base*0.16
		if s.Unit == "%" {
			v = math.Min(100, v)
		}
		p := cloud.Point{Time: end.Add(-time.Duration(n-1-i) * step), Value: math.Round(v*10) / 10}
		s.Points = append(s.Points, p)
		s.Average += p.Value
		if p.Value > s.Max {
			s.Max = p.Value
		}
	}
	s.Average = math.Round(s.Average/float64(n)*10) / 10
	s.Current = s.Points[n-1].Value
	return s
}

var accountSpecs = []struct {
	name, provider, partition string
	counts                    [4]int
	regions                   []string
}{
	{"生产账号", "aws", "global", [4]int{132, 15, 19, 43}, []string{"us-east-1", "ap-northeast-1", "eu-central-1"}},
	{"预发账号", "aws", "global", [4]int{48, 6, 8, 29}, []string{"ap-northeast-1", "us-west-2", "ap-southeast-1", "eu-west-1"}},
	{"中国区", "aws", "china", [4]int{34, 3, 4, 23}, []string{"cn-north-1", "cn-northwest-1"}},
	{"主账号", "aliyun", "china", [4]int{151, 20, 25, 30}, []string{"cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-hongkong", "cn-shenzhen", "cn-chengdu", "cn-qingdao", "cn-zhangjiakou"}},
	{"测试账号", "aliyun", "china", [4]int{62, 7, 10, 18}, []string{"cn-shanghai", "cn-hangzhou", "cn-shenzhen", "cn-beijing", "cn-guangzhou"}},
	{"海外账号", "aliyun", "global", [4]int{59, 7, 7, 24}, []string{"cn-beijing", "ap-southeast-1", "eu-central-1", "us-west-1", "ap-southeast-2"}},
}

func Seed(db *gorm.DB) error {
	var count int64
	db.Model(&model.CloudAccount{}).Count(&count)
	if count > 0 {
		return nil
	}
	types := []string{"vm", "rds", "lb", "oss"}
	now := time.Now().UTC()
	idleAssigned := 0
	for ai, spec := range accountSpecs {
		a := model.CloudAccount{Name: spec.name, Provider: spec.provider, Partition: spec.partition, CredentialType: "demo", UID: fmt.Sprintf("%d%09d", 1000+ai, ai+1), AccessKeyID: "DEMO••••" + fmt.Sprintf("%04d", ai+1), Regions: strings.Join(spec.regions, ","), AutoRegions: true, Enabled: true}
		if err := db.Create(&a).Error; err != nil {
			return err
		}
		for ti, t := range types {
			for i := 0; i < spec.counts[ti]; i++ {
				region := spec.regions[i%len(spec.regions)]
				name := fmt.Sprintf("%s-%s-%03d", map[bool]string{true: "ali", false: "aws"}[spec.provider == "aliyun"], t, i+1)
				if ti == 0 && i == 0 {
					name = []string{"order-worker-03", "ingest-node-11", "prod-web-12", "prod-api-07", "es-data-02", "redis-proxy-01"}[ai]
				}
				if ti == 1 && ai == 3 && i == 0 {
					name = "prod-mysql-main"
				}
				if ti == 2 && ai == 3 && i == 0 {
					name = "gw-slb-public"
				}
				status := "running"
				if ti == 0 && ((i > 0 && i%11 == 0) || (i == 1 && ai < 4)) {
					status = "stopped"
				}
				if ti == 2 && i%29 == 0 {
					status = "abnormal"
				}
				cpu := float64((i*7+ai*13)%70) + 3
				if ti == 0 && i == 0 {
					cpu = []float64{88.1, 79.2, 22.4, 92.4, 83.6, 74.5}[ai]
				}
				cpu24 := math.Max(5.1, cpu*0.69)
				if ti == 0 && status == "running" && i > 1 && idleAssigned < 37 {
					cpu24 = 2.1
					idleAssigned++
				}
				if ti == 0 && i == 0 && ai == 4 {
					region = "cn-shanghai"
				}
				if ti == 0 && i == 0 && ai == 5 {
					region = "cn-beijing"
				}
				r := model.Resource{AccountID: a.ID, Provider: spec.provider, Type: t, Region: region, CloudID: fmt.Sprintf("%s-%d-%d-%04d", spec.provider, ai, ti, i+1), Name: name, Status: status, IP: fmt.Sprintf("10.%d.%d.%d", ai+1, ti+1, i%254+1), Spec: []string{"ecs.c7.2xlarge", "mysql.rds.large", "ALB", "Standard"}[ti], VCPU: 4, MemoryGB: 8, Tags: fmt.Sprintf(`{"env":"%s","team":"platform"}`, map[bool]string{true: "prod", false: "test"}[i%3 != 0]), Extra: `{"os":"Linux","subnet":"vpc-prod","security_groups":["sg-readonly"]}`, CPU1H: cpu, CPU24H: cpu24, MetricsAt: &now}
				switch t {
				case "rds":
					r.IP = fmt.Sprintf("db-%d-%d.internal", ai+1, i+1)
					r.Extra = fmt.Sprintf(`{"engine":"MySQL","engine_version":"8.0","storage_gb":%d,"storage_type":"cloud_essd","endpoint":"%s","port":"3306","vpc":"vpc-prod"}`, 100+(i%8)*50, r.IP)
				case "lb":
					r.IP = fmt.Sprintf("lb-%d-%d.example.internal", ai+1, i+1)
					r.Extra = fmt.Sprintf(`{"dns":"%s","scheme":"internet-facing","vpc":"vpc-prod","bandwidth":100}`, r.IP)
				case "oss":
					r.IP = ""
					r.Extra = fmt.Sprintf(`{"location":"%s","storage_class":"Standard","capacity_gb":%.1f,"object_count":%d}`, region, float64(25+i*7), 1200+i*113)
				}
				if spec.provider == "aws" && ti == 0 {
					r.Spec = "m6i.xlarge"
				}
				if ti == 2 && i%3 == 0 {
					r.Spec = "NLB"
				}
				if ti == 2 && spec.provider == "aliyun" && i%3 == 1 {
					r.Spec = "CLB"
				}
				if (ti == 0 || ti == 1 || ti == 2) && ai == 3 && i < map[int]int{0: 8, 1: 2, 2: 2}[ti] {
					exp := now.AddDate(0, 0, 8+i*2)
					r.ExpiresAt = &exp
				}
				if err := db.Create(&r).Error; err != nil {
					return err
				}
			}
		}
		status := "success"
		errors := ""
		errorCount := 0
		if ai == 3 {
			status = "partial"
			errors = "cn-hongkong ALB 采集超时"
			errorCount = 1
		}
		finished := now.Add(-time.Duration(ai+2) * time.Minute)
		j := model.SyncJob{AccountID: a.ID, Provider: a.Provider, AccountName: a.Name, Status: status, TriggeredBy: "schedule", TasksTotal: 9, TasksDone: 9, ErrorCount: errorCount, ResourceCount: spec.counts[0] + spec.counts[1] + spec.counts[2] + spec.counts[3], Errors: errors, StartedAt: finished.Add(-48 * time.Second), FinishedAt: &finished}
		db.Create(&j)
		for h := 0; h < 24; h++ {
			hour := now.Truncate(time.Hour).Add(-time.Duration(h) * time.Hour)
			v := float64(34+ai*3) + 6*math.Sin(float64(h)/4)
			db.Create(&model.HostCPUHourly{AccountID: a.ID, Provider: a.Provider, Hour: hour, Sum: v * float64(spec.counts[0]), Count: spec.counts[0]})
		}
	}
	return nil
}
