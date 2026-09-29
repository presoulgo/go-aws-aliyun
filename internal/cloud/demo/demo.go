package demo

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	aliprov "github.com/presoulgo/go-aws-aliyun/internal/cloud/aliyun"
	awsprov "github.com/presoulgo/go-aws-aliyun/internal/cloud/aws"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Provider serves generated data under the "aws" or "aliyun" name.
type Provider struct {
	name  string
	now   func() time.Time
	delay bool

	mu       sync.Mutex
	accounts map[string]*accountData
}

type accountData struct {
	ak        string
	prof      profile
	resources []cloud.Resource
	cpuBase   map[string]float64
	status    map[string]string
	born      time.Time
}

// NewAWS returns a demo provider registered as "aws".
func NewAWS() *Provider { return newProvider(model.ProviderAWS) }

// NewAliyun returns a demo provider registered as "aliyun".
func NewAliyun() *Provider { return newProvider(model.ProviderAliyun) }

func newProvider(name string) *Provider {
	return &Provider{name: name, now: time.Now, delay: true, accounts: map[string]*accountData{}}
}

// WithoutDelay disables the simulated API latency (tests).
func (p *Provider) WithoutDelay() *Provider {
	p.delay = false
	return p
}

func (p *Provider) Name() string { return p.name }

// RegionName implements cloud.RegionNamer with the real providers' names.
func (p *Provider) RegionName(id string) string {
	if p.name == model.ProviderAWS {
		return awsprov.RegionName(id)
	}
	return aliprov.RegionName(id)
}

func (p *Provider) ResourceTypes() []cloud.TypeSpec {
	return []cloud.TypeSpec{
		{Type: model.TypeVM},
		{Type: model.TypeRDS},
		{Type: model.TypeLB},
		{Type: model.TypeBucket, Global: true},
		{Type: model.TypeDisk},
		{Type: model.TypeEIP},
	}
}

// sleep simulates API latency so sync progress is visible in the UI.
func (p *Provider) sleep(ctx context.Context, min, max time.Duration) error {
	if !p.delay {
		return ctx.Err()
	}
	d := min + time.Duration(rand.Int64N(int64(max-min)+1))
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (p *Provider) Validate(ctx context.Context, cred cloud.Credential) (*cloud.AccountInfo, error) {
	if err := p.sleep(ctx, 300*time.Millisecond, 700*time.Millisecond); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cred.AccessKeyID) == "" || cred.AccessKeySecret == "" {
		return nil, cloud.Wrap(cloud.ErrAuth, errors.New("AccessKey 不能为空"))
	}
	if strings.EqualFold(cred.AccessKeySecret, "wrong") {
		return nil, cloud.Wrap(cloud.ErrAuth, errors.New("InvalidAccessKeyId.NotFound: Specified access key is not found（演示：Secret 填 wrong 会模拟校验失败）"))
	}
	prof := p.profileFor(cred)
	arn := "arn:aws:iam::" + prof.uid + ":user/yunshu-readonly"
	if p.name == model.ProviderAliyun {
		arn = "acs:ram::" + prof.uid + ":user/yunshu-readonly"
	}
	if cred.RoleARN != "" {
		arn = cred.RoleARN
	}
	return &cloud.AccountInfo{AccountUID: prof.uid, Arn: arn}, nil
}

func (p *Provider) ListRegions(ctx context.Context, cred cloud.Credential) ([]cloud.Region, error) {
	if err := p.sleep(ctx, 100*time.Millisecond, 300*time.Millisecond); err != nil {
		return nil, err
	}
	prof := p.profileFor(cred)
	out := make([]cloud.Region, 0, len(prof.enabled))
	for _, id := range prof.enabled {
		name := aliprov.RegionName(id)
		if p.name == model.ProviderAWS {
			name = awsprov.RegionName(id)
		}
		out = append(out, cloud.Region{ID: id, Name: name})
	}
	return out, nil
}

func (p *Provider) Collect(ctx context.Context, cred cloud.Credential, typ, region string) ([]cloud.Resource, error) {
	if err := p.sleep(ctx, 120*time.Millisecond, 420*time.Millisecond); err != nil {
		return nil, err
	}
	data := p.data(cred)
	var out []cloud.Resource
	for _, r := range data.resources {
		if r.Type == typ && (typ == model.TypeBucket || r.Region == region) {
			out = append(out, copyResource(r))
		}
	}
	if typ == model.TypeLB && region != "" && region == data.prof.failRegion {
		var clbOnly []cloud.Resource
		for _, r := range out {
			if cloud.ExtraString(r.Extra, "lb_kind") != cloud.LBKindALB {
				clbOnly = append(clbOnly, r)
			}
		}
		return clbOnly, fmt.Errorf("ALB: %w", cloud.Wrap(cloud.ErrNetwork, errors.New("ListLoadBalancers 请求超时（context deadline exceeded）")))
	}
	return out, nil
}

func (p *Provider) CPUSnapshot(ctx context.Context, cred cloud.Credential, region string, ids []string) (map[string]cloud.CPUStat, error) {
	if err := p.sleep(ctx, 50*time.Millisecond, 150*time.Millisecond); err != nil {
		return nil, err
	}
	data := p.data(cred)
	now := p.now()
	out := map[string]cloud.CPUStat{}
	last := now.Truncate(time.Hour)
	for _, id := range ids {
		base, ok := data.cpuBase[id]
		if !ok {
			continue
		}
		seed := hash64(id, "cpu")
		hv, isHero := heroCPU(id, data)
		hourly := map[int64]float64{}
		for h := last.Add(-23 * time.Hour); !h.After(last); h = h.Add(time.Hour) {
			if isHero {
				hourly[h.Unix()] = round1(heroCPUAt(hv, seed, h, now, region))
			} else {
				hourly[h.Unix()] = round1(cpuAt(base, seed, h, region))
			}
		}
		if isHero {
			hourly[last.Unix()] = hv
		}
		out[id] = cloud.CPUStat{Hourly: hourly}
	}
	return out, nil
}

func (p *Provider) SupportedMetrics(ref cloud.ResourceRef) []string {
	if p.name == model.ProviderAWS {
		return awsprov.SupportedMetrics(ref)
	}
	return aliprov.SupportedMetrics(ref)
}

func (p *Provider) QueryMetrics(ctx context.Context, cred cloud.Credential, ref cloud.ResourceRef, q cloud.MetricQuery) ([]cloud.Series, error) {
	if err := p.sleep(ctx, 60*time.Millisecond, 200*time.Millisecond); err != nil {
		return nil, err
	}
	data := p.data(cred)
	supported := map[string]bool{}
	for _, k := range p.SupportedMetrics(ref) {
		supported[k] = true
	}
	period := q.Period
	if period < time.Minute {
		period = time.Minute
	}
	status := data.status[ref.ResourceID]
	now := p.now()
	var out []cloud.Series
	for _, key := range q.Keys {
		if !supported[key] {
			continue
		}
		def, _ := cloud.LookupMetric(ref.Type, key)
		s := cloud.Series{Key: key, Unit: def.Unit, Points: []cloud.Point{}}
		step := max(period, sparsePeriod(p.name, ref.Type, key))
		if status == model.StatusRunning || status == "" {
			for t := q.Start.Truncate(step); !t.After(q.End); t = t.Add(step) {
				if t.Before(q.Start) {
					continue
				}
				v := metricAt(ref, key, data, t, now)
				s.Points = append(s.Points, cloud.Point{float64(t.UnixMilli()), round3(v)})
			}
		}
		out = append(out, s)
	}
	return out, nil
}

// profileFor returns the prototype profile for known demo keys, or a small
// generated one for any other key typed into the account form.
func (p *Provider) profileFor(cred cloud.Credential) profile {
	if prof, ok := profiles[cred.AccessKeyID]; ok && prof.provider == p.name {
		return prof
	}
	h := hash64(cred.AccessKeyID)
	if p.name == model.ProviderAWS {
		enabled := awsGlobalRegions
		res := []string{"us-east-1", "ap-northeast-1", "eu-west-1"}
		if cred.Partition == model.PartitionAWSCN {
			enabled, res = awsChinaRegions, awsChinaRegions
		}
		return profile{provider: p.name, uid: fmt.Sprintf("%012d", h%1_000_000_000_000), env: "stg", enabled: enabled, resources: res,
			counts: counts{vm: 12, rds: 2, lb: 2, bucket: 4, disk: 1, eip: 1}}
	}
	return profile{provider: p.name, uid: fmt.Sprintf("%016d", h%10_000_000_000_000_000), env: "stg", enabled: aliyunRegions,
		resources: []string{"cn-hangzhou", "cn-shenzhen"}, counts: counts{vm: 12, rds: 2, lb: 2, bucket: 4, disk: 1, eip: 1}, expiring: 1}
}

func (p *Provider) data(cred cloud.Credential) *accountData {
	key := cred.AccessKeyID + "|" + cred.Partition
	p.mu.Lock()
	defer p.mu.Unlock()
	if d, ok := p.accounts[key]; ok {
		return d
	}
	prof := p.profileFor(cred)
	d := generate(cred.AccessKeyID, prof, p.now())
	p.accounts[key] = d
	return d
}

func copyResource(r cloud.Resource) cloud.Resource {
	c := r
	c.Tags = make(map[string]string, len(r.Tags))
	for k, v := range r.Tags {
		c.Tags[k] = v
	}
	c.Extra = make(map[string]any, len(r.Extra))
	for k, v := range r.Extra {
		c.Extra[k] = v
	}
	return c
}

func heroCPU(id string, d *accountData) (float64, bool) {
	for _, h := range heroes {
		if h.ak == d.ak && h.cpu > 0 && heroID(h, d.prof) == id {
			return h.cpu, true
		}
	}
	return 0, false
}

// --- deterministic noise -------------------------------------------------

func hash64(parts ...string) uint64 {
	h := fnv.New64a()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return h.Sum64()
}

func mix(seed uint64, slot int64) float64 {
	x := seed ^ uint64(slot)*0x9E3779B97F4A7C15
	x ^= x >> 30
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 27
	x *= 0x94D049BB133111EB
	x ^= x >> 31
	return float64(x%20001)/10000 - 1
}

// smooth is value noise on a 10 minute grid plus a little per-minute jitter.
func smooth(seed uint64, t time.Time) float64 {
	const grid = 600
	u := t.Unix()
	k := u / grid
	frac := float64(u%grid) / grid
	a, b := mix(seed, k), mix(seed, k+1)
	return a + (b-a)*frac + 0.25*mix(seed^0xABCDEF, u/60)
}

// utcOffset is the approximate local time offset of a region, in hours, so the
// daily load curve follows the users of that region.
func utcOffset(region string) float64 {
	switch {
	case strings.HasPrefix(region, "us-west"):
		return -8
	case strings.HasPrefix(region, "us-"), strings.HasPrefix(region, "ca-"):
		return -5
	case strings.HasPrefix(region, "sa-"):
		return -3
	case strings.HasPrefix(region, "eu-"), strings.HasPrefix(region, "me-"):
		return 1
	case region == "ap-south-1":
		return 5.5
	case strings.HasPrefix(region, "ap-northeast"):
		return 9
	case strings.HasPrefix(region, "ap-southeast-2"):
		return 10
	default:
		return 8
	}
}

// daily peaks around 14:00 and bottoms out around 02:00 local time.
func daily(t time.Time, region string) float64 {
	local := t.UTC().Add(time.Duration(utcOffset(region) * float64(time.Hour)))
	h := float64(local.Hour()) + float64(local.Minute())/60
	return math.Sin((h - 8) / 24 * 2 * math.Pi)
}

func cpuAt(base float64, seed uint64, t time.Time, region string) float64 {
	// The daily swing is ±28% of the base, but busy hosts keep headroom below
	// 100% so their curve does not flatten at the ceiling.
	swing := math.Min(0.28*base, 0.9*(99-base))
	v := base + swing*daily(t, region) + 3.5*smooth(seed, t)*math.Min(1, base/20+0.2)
	return clamp(v, 0.3, 99.5)
}

// heroCPUAt keeps the prototype's busy hosts close to their headline value
// (for example prod-api-07 at 92.4%), so the dashboard Top 5 and the metric
// charts tell the same story.
// The curve passes through v at now and follows the daily cycle around it;
// the amplitude shrinks when that would push it past 98% or below 3%.
func heroCPUAt(v float64, seed uint64, t, now time.Time, region string) float64 {
	dn := daily(now, region)
	amp := 0.18 * v
	amp = math.Min(amp, (98-v)/math.Max(0.05, 1-dn))
	amp = math.Min(amp, (v-3)/math.Max(0.05, 1+dn))
	return clamp(v+amp*(daily(t, region)-dn)+1.8*smooth(seed, t), 0.3, 99.5)
}

func metricAt(ref cloud.ResourceRef, key string, d *accountData, t, now time.Time) float64 {
	seed := hash64(ref.ResourceID, key)
	n := smooth(seed, t)
	day := daily(t, ref.Region)
	pick := func(lo, span uint64) float64 { return float64(lo + seed%span) }
	switch ref.Type {
	case model.TypeVM:
		switch key {
		case cloud.MetricCPU:
			if hv, ok := heroCPU(ref.ResourceID, d); ok {
				return heroCPUAt(hv, hash64(ref.ResourceID, "cpu"), t, now, ref.Region)
			}
			base := d.cpuBase[ref.ResourceID]
			if base == 0 {
				base = 20
			}
			return cpuAt(base, hash64(ref.ResourceID, "cpu"), t, ref.Region)
		case cloud.MetricMem:
			return clamp(pick(38, 36)+2*day+1.5*n, 1, 99)
		case cloud.MetricNetIn:
			return math.Max(0, pick(4, 60)*1e6*(1+0.45*day)*(1+0.18*n))
		case cloud.MetricNetOut:
			return math.Max(0, pick(6, 90)*1e6*(1+0.45*day)*(1+0.18*n))
		case cloud.MetricDiskRead:
			return math.Max(0, pick(5, 80)*1e5*(1+0.3*day)*(1+0.3*n))
		case cloud.MetricDiskWrite:
			return math.Max(0, pick(10, 140)*1e5*(1+0.3*day)*(1+0.3*n))
		}
	case model.TypeRDS:
		switch key {
		case cloud.MetricCPU:
			return clamp(pick(12, 50)*(1+0.3*day)+3*n, 0.5, 99)
		case cloud.MetricMem:
			return clamp(pick(55, 30)+1.5*day+n, 1, 99)
		case cloud.MetricFreeMem:
			return math.Max(1e8, pick(2, 14)*1e9*(1-0.1*day)*(1+0.03*n))
		case cloud.MetricDiskUtil:
			days := t.Sub(d.born).Hours() / 24
			return clamp(pick(35, 45)+0.05*days+0.2*n, 1, 99)
		case cloud.MetricConnections:
			return math.Max(1, pick(80, 1400)*(1+0.35*day)*(1+0.1*n))
		}
	case model.TypeLB:
		switch key {
		case cloud.MetricQPS:
			return math.Max(0, pick(200, 3800)*(1+0.5*day)*(1+0.15*n))
		case cloud.MetricActiveConn:
			return math.Max(0, pick(1000, 14000)*(1+0.4*day)*(1+0.1*n))
		case cloud.MetricNewConn:
			return math.Max(0, pick(40, 760)*(1+0.5*day)*(1+0.15*n))
		case cloud.MetricTraffic:
			return math.Max(0, pick(20, 580)*1e6*(1+0.45*day)*(1+0.15*n))
		}
	case model.TypeBucket:
		// Buckets grow 0.2%–1% a day and reach today's size and count now.
		ago := now.Sub(t).Hours() / 24
		growth := 0.002 + float64(seed%9)*0.001
		switch key {
		case cloud.MetricStorage:
			size, _ := cloud.ExtraFloat(ref.Extra, "size_bytes")
			return math.Max(0, math.Round(size*(1-growth*ago)*(1+0.001*n)))
		case cloud.MetricObjects:
			count, _ := cloud.ExtraFloat(ref.Extra, "object_count")
			return math.Max(0, math.Round(count*(1-growth*ago)))
		case cloud.MetricRequests:
			return math.Max(0, pick(2, 260)*(1+0.6*day)*(1+0.2*n))
		case cloud.MetricNetIn:
			return math.Max(0, pick(1, 30)*1e6*(1+0.5*day)*(1+0.2*n))
		case cloud.MetricNetOut:
			return math.Max(0, pick(4, 120)*1e6*(1+0.5*day)*(1+0.2*n))
		}
	}
	return 0
}

// sparsePeriod is how often the real clouds report bucket size and object
// count: Alibaba Cloud hourly, AWS daily. Other metrics follow the query.
func sparsePeriod(provider, typ, key string) time.Duration {
	if typ != model.TypeBucket || (key != cloud.MetricStorage && key != cloud.MetricObjects) {
		return 0
	}
	if provider == model.ProviderAWS {
		return 24 * time.Hour
	}
	return time.Hour
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
func round1(v float64) float64        { return math.Round(v*10) / 10 }
func round3(v float64) float64        { return math.Round(v*1000) / 1000 }
