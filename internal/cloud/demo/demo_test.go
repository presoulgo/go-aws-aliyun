package demo

import (
	"context"
	"errors"
	"math"
	"slices"
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

// Bucket size follows the real clouds' reporting periods (Alibaba Cloud hourly,
// AWS daily) and ends at the size shown in the resource list.
func TestBucketMetrics(t *testing.T) {
	end := time.Now()
	cases := []struct {
		p    *Provider
		ak   string
		span time.Duration
		step time.Duration
		keys []string
	}{
		{NewAliyun().WithoutDelay(), akAliMain, 6 * time.Hour, time.Hour, []string{cloud.MetricStorage, cloud.MetricRequests, cloud.MetricNetIn, cloud.MetricNetOut}},
		{NewAWS().WithoutDelay(), akAWSProd, 7 * 24 * time.Hour, 24 * time.Hour, []string{cloud.MetricStorage, cloud.MetricObjects}},
	}
	for _, c := range cases {
		cred := cloud.Credential{AccessKeyID: c.ak, AccessKeySecret: demoSecret}
		buckets, err := c.p.Collect(context.Background(), cred, model.TypeBucket, "")
		if err != nil || len(buckets) == 0 {
			t.Fatalf("%s buckets: %v %d", c.p.Name(), err, len(buckets))
		}
		b := buckets[0]
		ref := cloud.ResourceRef{Type: model.TypeBucket, Region: b.Region, ResourceID: b.ResourceID, Extra: b.Extra}
		if got := c.p.SupportedMetrics(ref); !slices.Equal(got, c.keys) {
			t.Fatalf("%s bucket metrics = %v", c.p.Name(), got)
		}
		series, err := c.p.QueryMetrics(context.Background(), cred, ref, cloud.MetricQuery{Keys: c.keys, Start: end.Add(-c.span), End: end, Period: 5 * time.Minute})
		if err != nil {
			t.Fatal(err)
		}
		pts := series[0].Points
		if len(pts) < 2 || time.Duration(pts[1][0]-pts[0][0])*time.Millisecond != c.step {
			t.Fatalf("%s storage points = %v", c.p.Name(), pts)
		}
		size, _ := cloud.ExtraFloat(b.Extra, "size_bytes")
		if last := pts[len(pts)-1][1]; math.Abs(last-size)/size > 0.02 {
			t.Fatalf("%s storage ends at %v, bucket size is %v", c.p.Name(), last, size)
		}
		if n := len(series[1].Points); c.step == time.Hour && n < 70 {
			t.Fatalf("requests at 5 minutes over 6h should give ~72 points, got %d", n)
		}
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
