package demo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func collectAll(t *testing.T, p *Provider, seed SeedAccount) []cloud.Resource {
	t.Helper()
	cred := cloud.Credential{AccessKeyID: seed.AccessKeyID, AccessKeySecret: seed.Secret, Partition: seed.Partition, RoleARN: seed.RoleARN}
	regions := seed.Regions
	if len(regions) == 0 {
		rs, err := p.ListRegions(context.Background(), cred)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			regions = append(regions, r.ID)
		}
	}
	var out []cloud.Resource
	for _, typ := range []string{model.TypeVM, model.TypeRDS, model.TypeLB} {
		for _, r := range regions {
			res, _ := p.Collect(context.Background(), cred, typ, r)
			out = append(out, res...)
		}
	}
	res, err := p.Collect(context.Background(), cred, model.TypeBucket, "")
	if err != nil {
		t.Fatal(err)
	}
	return append(out, res...)
}

func TestSeedAccountsMatchPrototypeTotals(t *testing.T) {
	providers := map[string]*Provider{model.ProviderAWS: NewAWS().WithoutDelay(), model.ProviderAliyun: NewAliyun().WithoutDelay()}
	byType := map[string]map[string]int{}
	total := 0
	expiring := 0
	now := time.Now()
	for _, seed := range SeedAccounts() {
		res := collectAll(t, providers[seed.Provider], seed)
		for _, r := range res {
			if byType[seed.Provider] == nil {
				byType[seed.Provider] = map[string]int{}
			}
			byType[seed.Provider][r.Type]++
			total++
			if r.ExpireAt != nil && r.ExpireAt.Sub(now) < 30*24*time.Hour {
				expiring++
			}
		}
	}
	// The failing cn-hongkong ALB of the main account hides its ALBs, so the
	// load balancer count may be a little lower than the prototype's 42.
	if byType["aws"]["vm"] != 214 || byType["aliyun"]["vm"] != 272 || byType["aws"]["bucket"] != 95 || byType["aliyun"]["bucket"] != 72 {
		t.Fatalf("unexpected counts: %+v", byType)
	}
	if total < 770 || total > 784 {
		t.Fatalf("total = %d", total)
	}
	if expiring != 12 {
		t.Fatalf("expiring within 30 days = %d, want 12", expiring)
	}
}

func TestFailureInjectionAndMetrics(t *testing.T) {
	p := NewAliyun().WithoutDelay()
	cred := cloud.Credential{AccessKeyID: akAliMain, AccessKeySecret: demoSecret}
	res, err := p.Collect(context.Background(), cred, model.TypeLB, "cn-hongkong")
	if !errors.Is(err, cloud.ErrNetwork) {
		t.Fatalf("expected simulated timeout, got %v", err)
	}
	for _, r := range res {
		if cloud.ExtraString(r.Extra, "lb_kind") == cloud.LBKindALB {
			t.Fatal("ALBs must be missing from the partial result")
		}
	}

	shop := cloud.Credential{AccessKeyID: akAliShop, AccessKeySecret: demoSecret}
	vms, err := p.Collect(context.Background(), shop, model.TypeVM, "cn-hangzhou")
	if err != nil {
		t.Fatal(err)
	}
	var hero cloud.Resource
	for _, r := range vms {
		if r.Name == "prod-api-07" {
			hero = r
		}
	}
	if hero.ResourceID != "i-bp1f3k9x2m7qa8d0c1e" || hero.Tags["cost-center"] != "cc-102" {
		t.Fatalf("hero missing: %+v", hero)
	}
	stats, err := p.CPUSnapshot(context.Background(), shop, "cn-hangzhou", []string{hero.ResourceID})
	if err != nil {
		t.Fatal(err)
	}
	if last, _ := stats[hero.ResourceID].Last(); last != 92.4 {
		t.Fatalf("hero last-hour CPU = %v", last)
	}

	ref := cloud.ResourceRef{Type: model.TypeVM, Region: hero.Region, ResourceID: hero.ResourceID, Extra: hero.Extra}
	end := time.Now()
	series, err := p.QueryMetrics(context.Background(), shop, ref, cloud.MetricQuery{
		Keys: []string{cloud.MetricCPU, cloud.MetricNetIn}, Start: end.Add(-6 * time.Hour), End: end, Period: 5 * time.Minute,
	})
	if err != nil || len(series) != 2 {
		t.Fatalf("series: %v %v", err, series)
	}
	if n := len(series[0].Points); n < 70 || n > 74 {
		t.Fatalf("6h at 5m should give ~72 points, got %d", n)
	}
	again, _ := p.QueryMetrics(context.Background(), shop, ref, cloud.MetricQuery{Keys: []string{cloud.MetricCPU}, Start: end.Add(-6 * time.Hour), End: end, Period: 5 * time.Minute})
	if again[0].Points[10] != series[0].Points[10] {
		t.Fatal("metrics must be reproducible")
	}
}

func TestValidate(t *testing.T) {
	p := NewAWS().WithoutDelay()
	info, err := p.Validate(context.Background(), cloud.Credential{AccessKeyID: akAWSProd, AccessKeySecret: demoSecret})
	if err != nil || info.AccountUID != "482910345561" {
		t.Fatalf("validate: %v %+v", err, info)
	}
	if _, err := p.Validate(context.Background(), cloud.Credential{AccessKeyID: "AKIAXXXX", AccessKeySecret: "wrong"}); !errors.Is(err, cloud.ErrAuth) {
		t.Fatalf("wrong secret must fail: %v", err)
	}
	info, err = p.Validate(context.Background(), cloud.Credential{AccessKeyID: "AKIANEWKEY0000000000", AccessKeySecret: "x"})
	if err != nil || len(info.AccountUID) != 12 {
		t.Fatalf("unknown keys get a generated account: %v %+v", err, info)
	}
}
