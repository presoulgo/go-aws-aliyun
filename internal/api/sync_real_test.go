package api

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/config"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
	"github.com/presoulgo/go-aws-aliyun/internal/store"
)

type syncProvider struct{}

func (syncProvider) Validate(context.Context, model.CloudAccount, string) (cloud.Identity, error) {
	return cloud.Identity{UID: "123456789", Regions: []string{"us-east-1"}}, nil
}

func (syncProvider) List(_ context.Context, _ model.CloudAccount, _ string, region, collector string) ([]model.Resource, error) {
	switch collector {
	case "vm":
		return []model.Resource{{Type: "vm", Region: region, CloudID: "i-new", Name: "new", Status: "running", Tags: "{}", Extra: "{}"}}, nil
	case "rds":
		return nil, errors.New("AccessDenied: rds read permission missing")
	default:
		return []model.Resource{}, nil
	}
}

func (syncProvider) Metrics(_ context.Context, _ model.CloudAccount, _ string, resource model.Resource, metric string, _ time.Duration) (cloud.Series, error) {
	now := time.Now()
	return cloud.Series{ResourceID: resource.ID, Metric: metric, Supported: true, Points: []cloud.Point{
		{Time: now.Add(-90 * time.Minute), Value: 10},
		{Time: now.Add(-30 * time.Minute), Value: 20},
	}, Current: 20, Average: 15, Max: 20}, nil
}

func TestRealSyncKeepsFailedScopesAndUpdatesSuccessfulScopes(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "sync.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	const key = "real-sync-test-key"
	encrypted, err := secret.Encrypt(key, "secret")
	if err != nil {
		t.Fatal(err)
	}
	account := model.CloudAccount{Name: "production", Provider: "aws", AccessKeyID: "AKIDEXAMPLE1234", SecretEncrypted: encrypted, Regions: "us-east-1", Enabled: true}
	if err := db.Create(&account).Error; err != nil {
		t.Fatal(err)
	}
	db.Create(&model.Resource{AccountID: account.ID, Provider: "aws", Type: "vm", Region: "us-east-1", CloudID: "i-old", Name: "old vm", Tags: "{}", Extra: "{}"})
	db.Create(&model.Resource{AccountID: account.ID, Provider: "aws", Type: "rds", Region: "us-east-1", CloudID: "db-old", Name: "old db", Tags: "{}", Extra: "{}"})
	job := model.SyncJob{AccountID: account.ID, AccountName: account.Name, Provider: account.Provider, Status: "running", StartedAt: time.Now()}
	db.Create(&job)
	server := New(db, config.Config{SecretKey: key})
	server.providers["aws"] = syncProvider{}
	server.runRealSync(context.Background(), job, account)

	if err := db.First(&job, job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if job.Status != "partial" || job.TasksDone != 4 || job.ErrorCount != 1 {
		t.Fatalf("job status=%s progress=%d/%d errors=%d", job.Status, job.TasksDone, job.TasksTotal, job.ErrorCount)
	}
	var resources []model.Resource
	db.Order("type, cloud_id").Find(&resources)
	if len(resources) != 2 || resources[0].CloudID != "db-old" || resources[1].CloudID != "i-new" {
		t.Fatalf("unexpected resources after partial sync: %+v", resources)
	}
	if resources[1].CPU24H != 15 || resources[1].CPU1H != 20 || resources[1].MetricsAt == nil {
		t.Fatalf("CPU snapshot was not applied: %+v", resources[1])
	}
	var hourly int64
	db.Model(&model.HostCPUHourly{}).Where("account_id = ?", account.ID).Count(&hourly)
	if hourly != 2 {
		t.Fatalf("hourly CPU rows=%d, want 2", hourly)
	}
	public := publicAccount(account)
	if public["access_key_id"] == account.AccessKeyID {
		t.Fatal("account response contains unmasked access key")
	}
}

func TestAlibabaLoadBalancerScopesAreIndependent(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	defer sqlDB.Close()
	account := model.CloudAccount{Name: "aliyun", Provider: "aliyun"}
	db.Create(&account)
	db.Create(&model.Resource{AccountID: account.ID, Provider: "aliyun", Type: "lb", Region: "cn-hangzhou", CloudID: "clb-old", Spec: "CLB", Tags: "{}", Extra: "{}"})
	db.Create(&model.Resource{AccountID: account.ID, Provider: "aliyun", Type: "lb", Region: "cn-hangzhou", CloudID: "alb-keep", Spec: "ALB", Tags: "{}", Extra: "{}"})
	server := New(db, config.Config{SecretKey: "key"})
	if err := server.saveTask(account, syncTask{region: "cn-hangzhou", collector: "clb", resourceType: "lb"}, nil); err != nil {
		t.Fatal(err)
	}
	var resources []model.Resource
	db.Find(&resources)
	if len(resources) != 1 || resources[0].CloudID != "alb-keep" {
		t.Fatalf("ALB scope was changed by CLB sync: %+v", resources)
	}
}
