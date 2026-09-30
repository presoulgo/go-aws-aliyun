package service

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud/demo"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"github.com/presoulgo/go-aws-aliyun/internal/secret"
)

// fakeProvider serves canned results keyed by "type|region".
type fakeProvider struct {
	mu      sync.Mutex
	data    map[string][]cloud.Resource
	errs    map[string]error
	cpu     map[string]cloud.CPUStat
	block   chan struct{}
	regions []string
}

func (f *fakeProvider) Name() string { return model.ProviderAWS }
func (f *fakeProvider) Validate(ctx context.Context, c cloud.Credential) (*cloud.AccountInfo, error) {
	return &cloud.AccountInfo{AccountUID: "123456789012"}, nil
}
func (f *fakeProvider) ListRegions(ctx context.Context, c cloud.Credential) ([]cloud.Region, error) {
	var out []cloud.Region
	for _, r := range f.regions {
		out = append(out, cloud.Region{ID: r})
	}
	return out, nil
}
func (f *fakeProvider) ResourceTypes() []cloud.TypeSpec {
	return []cloud.TypeSpec{{Type: model.TypeVM}, {Type: model.TypeBucket, Global: true}}
}
func (f *fakeProvider) Collect(ctx context.Context, c cloud.Credential, typ, region string) ([]cloud.Resource, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := typ + "|" + region
	return f.data[key], f.errs[key]
}
func (f *fakeProvider) CPUSnapshot(ctx context.Context, c cloud.Credential, region string, ids []string) (map[string]cloud.CPUStat, error) {
	out := map[string]cloud.CPUStat{}
	for _, id := range ids {
		if st, ok := f.cpu[id]; ok {
			out[id] = st
		}
	}
	return out, nil
}
func (f *fakeProvider) SupportedMetrics(ref cloud.ResourceRef) []string {
	return []string{cloud.MetricCPU}
}
func (f *fakeProvider) QueryMetrics(ctx context.Context, c cloud.Credential, ref cloud.ResourceRef, q cloud.MetricQuery) ([]cloud.Series, error) {
	return []cloud.Series{{Key: cloud.MetricCPU, Unit: cloud.UnitPercent, Points: []cloud.Point{{1, 2}}}}, nil
}

func vm(id, region string) cloud.Resource {
	return cloud.Resource{Type: model.TypeVM, Region: region, ResourceID: id, Name: id, Status: model.StatusRunning, Tags: map[string]string{"env": "prod"}}
}

type syncEnv struct {
	accounts *AccountService
	syncer   *SyncService
	res      *ResourceService
	admin    Actor
}

func newSyncEnv(t *testing.T, providers ...cloud.Provider) *syncEnv {
	t.Helper()
	db := newTestDB(t)
	audit := NewAuditService(db)
	box, _ := secret.NewBox(bytes.Repeat([]byte{1}, 32))
	reg := cloud.NewRegistry(providers...)
	accounts := NewAccountService(db, box, reg, audit)
	return &syncEnv{
		accounts: accounts,
		syncer:   NewSyncService(db, accounts, audit, SyncOptions{Concurrency: 4, TaskTimeout: 5 * time.Second}),
		res:      NewResourceService(db, reg, 5, 30),
		admin:    Actor{UserID: 1, Username: "admin", Role: model.RoleAdmin},
	}
}

func (e *syncEnv) runSync(t *testing.T, accountID uint) *model.SyncJob {
	t.Helper()
	job, err := e.syncer.Trigger(accountID, e.admin, true)
	if err != nil {
		t.Fatal(err)
	}
	e.syncer.Wait()
	final, err := e.syncer.Job(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return final
}

func TestSyncUpsertSweepAndPartialFailure(t *testing.T) {
	hour := time.Now().Truncate(time.Hour).Unix()
	fp := &fakeProvider{
		regions: []string{"r1", "r2"},
		data: map[string][]cloud.Resource{
			"vm|r1":   {vm("a", "r1"), vm("b", "r1")},
			"vm|r2":   {vm("c", "r2")},
			"bucket|": {{Type: model.TypeBucket, Region: "r1", ResourceID: "x", Name: "x", Status: model.StatusRunning}},
		},
		cpu: map[string]cloud.CPUStat{"a": {Hourly: map[int64]float64{hour - 3600: 2, hour: 4}}},
	}
	env := newSyncEnv(t, fp)
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "fake", Provider: model.ProviderAWS, AccessKeyID: "AKIA1234", AccessKeySecret: "s"})
	if err != nil {
		t.Fatal(err)
	}

	job := env.runSync(t, acc.ID)
	if job.Status != model.JobSuccess || job.TasksTotal != 3 || job.TasksDone != 3 {
		t.Fatalf("first job: %+v", job)
	}
	items, total, _ := env.res.List(ResourceFilter{})
	if total != 4 {
		t.Fatalf("expected 4 resources, got %d", total)
	}
	for _, r := range items {
		if r.ResourceID == "a" && (r.CPU24h == nil || *r.CPU24h != 3 || !r.Idle) {
			t.Fatalf("cpu snapshot not stored on a: %+v", r)
		}
	}

	// Second run: b disappears, r2 fails (c must survive), bucket list empty.
	fp.mu.Lock()
	fp.data["vm|r1"] = []cloud.Resource{vm("a", "r1")}
	fp.data["bucket|"] = nil
	fp.errs = map[string]error{"vm|r2": errors.New("boom")}
	fp.mu.Unlock()
	job = env.runSync(t, acc.ID)
	if job.Status != model.JobPartial || job.ErrorCount != 1 || job.Errors[0].Region != "r2" {
		t.Fatalf("second job: %+v", job)
	}
	items, total, _ = env.res.List(ResourceFilter{Sort: "name:asc"})
	if total != 2 || items[0].ResourceID != "a" || items[1].ResourceID != "c" {
		t.Fatalf("after sweep: %+v", items)
	}
	view, _ := env.accounts.Get(acc.ID)
	if view.LastSyncStatus != model.JobPartial || view.ResourceCount != 2 || view.LastJob == nil {
		t.Fatalf("account view: %+v", view)
	}

	// An unsupported region is skipped, not an error, and cleans up.
	fp.mu.Lock()
	fp.errs = map[string]error{"vm|r2": cloud.Wrap(cloud.ErrRegionUnsupported, errors.New("nope"))}
	fp.mu.Unlock()
	job = env.runSync(t, acc.ID)
	if job.Status != model.JobSuccess || job.ErrorCount != 0 {
		t.Fatalf("unsupported region must be skipped: %+v", job)
	}
	if _, total, _ = env.res.List(ResourceFilter{}); total != 1 {
		t.Fatalf("resources in an unsupported region are removed, got %d", total)
	}
}

func TestSyncCancelAndConcurrentTrigger(t *testing.T) {
	fp := &fakeProvider{regions: []string{"r1"}, data: map[string][]cloud.Resource{}, block: make(chan struct{})}
	env := newSyncEnv(t, fp)
	acc, err := env.accounts.Create(context.Background(), env.admin, AccountInput{Name: "fake", Provider: model.ProviderAWS, AccessKeyID: "AKIA1234", AccessKeySecret: "s"})
	if err != nil {
		t.Fatal(err)
	}
	job1, err := env.syncer.Trigger(acc.ID, env.admin, true)
	if err != nil {
		t.Fatal(err)
	}
	job2, err := env.syncer.Trigger(acc.ID, env.admin, true)
	if err != nil || job2.ID != job1.ID {
		t.Fatalf("second trigger must return the running job: %v %v", job2, err)
	}
	if err := env.syncer.Cancel(job1.ID, env.admin); err != nil {
		t.Fatal(err)
	}
	final, _ := env.syncer.Job(job1.ID)
	if final.Status != model.JobCancelled || final.FinishedAt == nil {
		t.Fatalf("cancelled job: %+v", final)
	}
	if err := env.syncer.Cancel(job1.ID, env.admin); err == nil {
		t.Fatal("cancelling a finished job must fail")
	}
}

func TestDemoEndToEnd(t *testing.T) {
	env := newSyncEnv(t, demo.NewAWS().WithoutDelay(), demo.NewAliyun().WithoutDelay())
	for _, a := range demo.SeedAccounts() {
		_, err := env.accounts.Create(context.Background(), env.admin, AccountInput{
			Name: a.Name, Provider: a.Provider, Partition: a.Partition, AccessKeyID: a.AccessKeyID,
			AccessKeySecret: a.Secret, RoleARN: a.RoleARN, Regions: a.Regions,
		})
		if err != nil {
			t.Fatalf("create %s: %v", a.Name, err)
		}
	}
	if _, err := env.syncer.SyncAll(env.admin, true); err != nil {
		t.Fatal(err)
	}
	env.syncer.Wait()

	accounts, _ := env.accounts.List()
	partial := 0
	for _, a := range accounts {
		if a.LastSyncStatus == model.JobPartial {
			partial++
			if a.Name != "阿里云 主账号" {
				t.Fatalf("only the main Alibaba Cloud account simulates a failure, got %s: %s", a.Name, a.LastSyncError)
			}
		} else if a.LastSyncStatus != model.JobSuccess {
			t.Fatalf("%s: %s %s", a.Name, a.LastSyncStatus, a.LastSyncError)
		}
	}
	if partial != 1 {
		t.Fatalf("expected one partial account, got %d", partial)
	}

	// 770–784 inventory items plus 24 unattached disks and 10 unbound EIPs.
	_, total, _ := env.res.List(ResourceFilter{})
	if total < 804 || total > 818 {
		t.Fatalf("total resources = %d", total)
	}
	top, _, _ := env.res.List(ResourceFilter{Type: model.TypeVM, Sort: "cpu_1h:desc", Page: Page{PageSize: 1}})
	if top[0].Name != "prod-api-07" {
		t.Fatalf("top CPU host = %s", top[0].Name)
	}
	tagged, _, _ := env.res.List(ResourceFilter{Query: "cost-center:cc-102"})
	if len(tagged) != 1 || tagged[0].Name != "prod-api-07" {
		t.Fatalf("tag search: %+v", tagged)
	}
	if _, n, _ := env.res.List(ResourceFilter{Expiring: true}); n != 12 {
		t.Fatalf("expiring = %d", n)
	}
	f, err := env.res.Filters(ResourceFilter{Type: model.TypeVM, Provider: model.ProviderAWS})
	if err != nil || f.Counts[model.TypeVM] != 214 || len(f.Regions) == 0 || len(f.Accounts) != 3 {
		t.Fatalf("filters: %+v %v", f, err)
	}
	all, err := env.res.Filters(ResourceFilter{Type: model.TypeVM})
	if err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{}
	for _, r := range all.Regions {
		labels[r.Value] = r.Label
	}
	if labels["cn-hangzhou"] != "华东1（杭州）" || labels["us-east-1"] == "" {
		t.Fatalf("region labels: %v", labels)
	}

	dash := NewDashboardService(env.syncer.db, 5, 30)
	sum, err := dash.Summary("")
	if err != nil {
		t.Fatal(err)
	}
	if sum.Accounts != 6 || sum.VMTotal != 486 || sum.Expiring != 12 || sum.TopCPU[0].Name != "prod-api-07" || len(sum.RecentSync) != 4 {
		t.Fatalf("summary: accounts=%d vm=%d expiring=%d top=%v recent=%d", sum.Accounts, sum.VMTotal, sum.Expiring, sum.TopCPU, len(sum.RecentSync))
	}
	for _, s := range sum.RecentSync {
		if (s.Status == model.JobPartial) != (s.FirstError != nil) {
			t.Fatalf("recent sync %s: status %s, first error %+v", s.AccountName, s.Status, s.FirstError)
		}
		if s.FirstError != nil && (s.FirstError.Region != "cn-hongkong" || s.FirstError.Type != model.TypeLB) {
			t.Fatalf("first error: %+v", s.FirstError)
		}
	}
	withData := 0
	for _, p := range sum.CPUTrend {
		if p.AWS != nil && p.Aliyun != nil {
			withData++
		}
	}
	if len(sum.CPUTrend) != 24 || withData < 23 {
		t.Fatalf("cpu trend points with data: %d/%d", withData, len(sum.CPUTrend))
	}
	awsSum, _ := dash.Summary(model.ProviderAWS)
	if awsSum.Accounts != 3 || awsSum.Expiring != 0 || awsSum.VMTotal != 214 {
		t.Fatalf("aws summary: %+v", awsSum)
	}

	metrics := NewMetricsService(env.syncer.db, env.accounts, env.accounts.registry, time.Minute)
	res, err := metrics.ForResource(context.Background(), top[0].ID, nil, "6h")
	if err != nil || len(res.Series) != 6 || len(res.Series[0].Points) == 0 {
		t.Fatalf("resource metrics: %v %+v", err, res)
	}
	lbs, _, _ := env.res.List(ResourceFilter{Type: model.TypeLB, Query: "ingest-nlb"})
	albs, _, _ := env.res.List(ResourceFilter{Type: model.TypeLB, Query: "api-alb-prod"})
	cmp, err := metrics.Compare(context.Background(), CompareInput{ResourceIDs: []uint{albs[0].ID, lbs[0].ID}, Key: cloud.MetricQPS, Range: "1h"})
	if err != nil {
		t.Fatal(err)
	}
	if !cmp.Items[0].Supported || cmp.Items[1].Supported || len(cmp.Items[0].Points) == 0 {
		t.Fatalf("compare: ALB supports QPS, NLB does not: %+v", cmp.Items)
	}
}
