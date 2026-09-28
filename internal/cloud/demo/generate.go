package demo

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

var (
	vmRoles     = []string{"web", "api", "worker", "job", "cache", "gateway", "search", "etl", "batch", "monitor", "mq", "nginx", "order", "user", "payment", "report", "cron", "admin", "push", "media"}
	rdsNames    = []string{"order-db", "user-db", "payment-db", "inventory-db", "report-db", "log-db", "config-db", "cms-db", "coupon-db", "risk-db"}
	lbNames     = []string{"web-lb", "api-lb", "internal-lb", "admin-lb", "gateway-lb", "open-api-lb"}
	bucketUses  = []string{"logs", "backup", "static", "uploads", "artifacts", "data", "reports", "images", "archive", "exports"}
	teams       = map[string]string{"web": "frontend", "api": "platform", "order": "trade", "payment": "trade", "user": "account", "search": "search", "etl": "data", "report": "data", "media": "media"}
	awsVMSpecs  = []string{"t3.medium", "m6i.large", "m6i.xlarge", "c6i.large", "c6i.2xlarge", "r6i.xlarge", "m7g.large", "c7g.xlarge"}
	aliVMSpecs  = []string{"ecs.g7.large", "ecs.g7.xlarge", "ecs.c7.large", "ecs.c7.2xlarge", "ecs.r7.xlarge", "ecs.c6.large", "ecs.g6.2xlarge", "ecs.u1-c1m2.xlarge"}
	awsRDS      = []struct{ engine, version, class string }{{"mysql", "8.0.39", "db.m6i.large"}, {"postgres", "16.4", "db.r6g.large"}, {"mysql", "8.0.39", "db.t4g.medium"}, {"postgres", "15.8", "db.m6g.xlarge"}}
	aliRDS      = []struct{ engine, version, class string }{{"MySQL", "8.0", "mysql.n2.medium.1"}, {"MySQL", "5.7", "rds.mysql.s3.large"}, {"PostgreSQL", "15.0", "pg.n2.medium.2c"}, {"MySQL", "8.0", "mysql.x4.large.2c"}}
	aliOS       = []string{"Alibaba Cloud Linux 3.2104 LTS 64位", "CentOS 7.9 64位", "Ubuntu 22.04 64位", "Windows Server 2022 数据中心版 64位中文版"}
	aliPrefixes = map[string]string{"cn-hangzhou": "bp1", "cn-shanghai": "uf6", "cn-beijing": "2ze", "cn-shenzhen": "wz9", "cn-hongkong": "j6c", "ap-southeast-1": "t4n", "cn-zhangjiakou": "8vb"}
)

// specSize derives vCPU and memory (MiB) from common instance type names.
func specSize(spec string) (int, int) {
	parts := strings.Split(spec, ".")
	size := parts[len(parts)-1]
	vcpu := map[string]int{"small": 2, "medium": 2, "large": 2, "xlarge": 4, "2xlarge": 8, "4xlarge": 16, "8xlarge": 32}[size]
	if vcpu == 0 {
		vcpu = 4
	}
	ratio := 4
	switch {
	case strings.Contains(spec, "c6") || strings.Contains(spec, "c7") || strings.HasPrefix(spec, "t3"):
		ratio = 2
	case strings.Contains(spec, "r6") || strings.Contains(spec, "r7"):
		ratio = 8
	}
	if spec == "t3.small" {
		return 2, 2048
	}
	return vcpu, vcpu * ratio * 1024
}

type gen struct {
	rng   *rand.Rand
	prof  profile
	ak    string
	now   time.Time
	names map[string]int
	used  map[string]bool
}

func generate(ak string, prof profile, now time.Time) *accountData {
	g := &gen{
		rng:   rand.New(rand.NewPCG(hash64(ak), hash64(ak, "demo"))),
		prof:  prof,
		ak:    ak,
		now:   now,
		names: map[string]int{},
		used:  map[string]bool{},
	}
	d := &accountData{ak: ak, prof: prof, cpuBase: map[string]float64{}, status: map[string]string{}, born: now}
	heroIDs := map[string]bool{}

	var mine []hero
	for _, h := range heroes {
		if h.ak == ak {
			mine = append(mine, h)
			g.used[h.name] = true
		}
	}
	for _, typ := range []string{model.TypeVM, model.TypeRDS, model.TypeLB, model.TypeBucket} {
		total := map[string]int{model.TypeVM: prof.counts.vm, model.TypeRDS: prof.counts.rds, model.TypeLB: prof.counts.lb, model.TypeBucket: prof.counts.bucket}[typ]
		n := 0
		for _, h := range mine {
			if h.typ == typ {
				r, cpu := g.heroResource(h)
				heroIDs[r.ResourceID] = true
				d.add(r, cpu)
				n++
			}
		}
		for ; n < total; n++ {
			r, cpu := g.resource(typ)
			d.add(r, cpu)
		}
	}
	g.markExpiring(d, heroIDs)
	return d
}

func (d *accountData) add(r cloud.Resource, cpu float64) {
	d.resources = append(d.resources, r)
	d.status[r.ResourceID] = r.Status
	if r.Type == model.TypeVM && r.Status == model.StatusRunning {
		d.cpuBase[r.ResourceID] = cpu
	}
}

// markExpiring gives a few generated prepaid resources an expiry within 30
// days so the dashboard shows the prototype's reminder list.
func (g *gen) markExpiring(d *accountData, heroIDs map[string]bool) {
	var eligible []int
	for i, r := range d.resources {
		if r.ChargeType == model.ChargePrepaid && r.Type != model.TypeBucket && !heroIDs[r.ResourceID] {
			eligible = append(eligible, i)
		}
	}
	if len(eligible) == 0 || g.prof.expiring == 0 {
		return
	}
	step := max(1, len(eligible)/(g.prof.expiring+1))
	for k := 0; k < g.prof.expiring && k*step < len(eligible); k++ {
		r := &d.resources[eligible[len(eligible)-1-k*step]]
		t := g.expireIn(9 + (k*7)%20)
		r.ExpireAt = &t
	}
}

func (g *gen) expireIn(days int) time.Time {
	// Alibaba Cloud prepaid resources expire at 00:00 Beijing time.
	bj := g.now.UTC().Add(8 * time.Hour)
	day := time.Date(bj.Year(), bj.Month(), bj.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	return day.Add(-8 * time.Hour)
}

func (g *gen) pickRegion() string {
	regions := g.prof.resources
	if len(regions) == 1 {
		return regions[0]
	}
	// The first region is the busiest, the rest share the remainder.
	if g.rng.Float64() < 0.34 {
		return regions[0]
	}
	if len(regions) > 2 && g.rng.Float64() < 0.3 {
		return regions[1]
	}
	return regions[1+g.rng.IntN(len(regions)-1)]
}

func (g *gen) envPrefix() string {
	switch g.prof.env {
	case "test":
		return "test"
	case "data":
		return "data"
	case "stg":
		return "stg"
	}
	return "prod"
}

func (g *gen) uniqueName(base string) string {
	for {
		g.names[base]++
		name := fmt.Sprintf("%s-%02d", base, g.names[base])
		if !g.used[name] {
			g.used[name] = true
			return name
		}
	}
}

const alnum = "abcdefghijklmnopqrstuvwxyz0123456789"
const hexdigits = "0123456789abcdef"

func (g *gen) randString(chars string, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[g.rng.IntN(len(chars))]
	}
	return string(b)
}

func (g *gen) aliPrefix(region string) string {
	if p, ok := aliPrefixes[region]; ok {
		return p
	}
	return "gw8"
}

func (g *gen) privateIP(region string) string {
	if g.prof.provider == model.ProviderAWS && g.prof.env == "prod" {
		return fmt.Sprintf("172.31.%d.%d", g.rng.IntN(64), 2+g.rng.IntN(250))
	}
	return fmt.Sprintf("10.%d.%d.%d", 10+int(hash64(region)%60), g.rng.IntN(32), 2+g.rng.IntN(250))
}

func (g *gen) publicIP() string {
	if g.prof.provider == model.ProviderAWS {
		return fmt.Sprintf("%d.%d.%d.%d", []int{3, 18, 34, 44, 52, 54}[g.rng.IntN(6)], g.rng.IntN(256), g.rng.IntN(256), 1+g.rng.IntN(254))
	}
	return fmt.Sprintf("%d.%d.%d.%d", []int{39, 47, 101, 106, 116, 120, 139}[g.rng.IntN(7)], g.rng.IntN(256), g.rng.IntN(256), 1+g.rng.IntN(254))
}

func (g *gen) cpuBase() float64 {
	switch x := g.rng.Float64(); {
	case x < 0.076:
		return 0.8 + g.rng.Float64()*3.7
	case x < 0.2:
		// Busy, but below the prototype's top hosts even at the daily peak.
		return 42 + g.rng.Float64()*12
	default:
		return 8 + g.rng.Float64()*32
	}
}

func (g *gen) created() *time.Time {
	t := g.now.Add(-time.Duration(30+g.rng.IntN(1100)) * 24 * time.Hour).Truncate(time.Minute).UTC()
	return &t
}

func (g *gen) tags(role string) map[string]string {
	env := map[string]string{"prod": "prod", "data": "prod", "test": "test", "stg": "staging"}[g.prof.env]
	t := map[string]string{"env": env, "app": role}
	if team, ok := teams[role]; ok {
		t["team"] = team
	} else {
		t["team"] = "ops"
	}
	if g.rng.Float64() < 0.6 {
		t["managed-by"] = "terraform"
	}
	return t
}

func (g *gen) resource(typ string) (cloud.Resource, float64) {
	region := g.pickRegion()
	aws := g.prof.provider == model.ProviderAWS
	switch typ {
	case model.TypeVM:
		role := vmRoles[g.rng.IntN(len(vmRoles))]
		name := g.uniqueName(g.envPrefix() + "-" + role)
		var id, spec, raw, status string
		x := g.rng.Float64()
		if aws {
			id = "i-0" + g.randString(hexdigits, 16)
			spec = awsVMSpecs[g.rng.IntN(len(awsVMSpecs))]
			raw, status = "running", model.StatusRunning
			if x > 0.93 {
				raw, status = "stopped", model.StatusStopped
			} else if x > 0.92 {
				raw, status = "pending", model.StatusPending
			}
		} else {
			id = "i-" + g.aliPrefix(region) + g.randString(alnum, 17)
			spec = aliVMSpecs[g.rng.IntN(len(aliVMSpecs))]
			raw, status = "Running", model.StatusRunning
			if x > 0.93 {
				raw, status = "Stopped", model.StatusStopped
			} else if x > 0.92 {
				raw, status = "Starting", model.StatusStarting
			}
		}
		vcpu, mem := specSize(spec)
		extra := map[string]any{"cpu": vcpu, "memory_mib": mem}
		if aws {
			extra["os"] = []string{"Linux/UNIX", "Linux/UNIX", "Linux/UNIX", "Windows"}[g.rng.IntN(4)]
			extra["subnet_id"] = "subnet-0" + g.randString(hexdigits, 16)
			extra["security_groups"] = []string{"sg-0" + g.randString(hexdigits, 16)}
			extra["image_id"] = "ami-0" + g.randString(hexdigits, 16)
		} else {
			extra["os"] = aliOS[g.rng.IntN(len(aliOS))]
			extra["vswitch_id"] = "vsw-" + g.aliPrefix(region) + g.randString(alnum, 17)
			extra["security_groups"] = []string{"sg-" + g.aliPrefix(region) + g.randString(alnum, 17)}
			extra["image_id"] = "aliyun_3_x64_20G_alibase_20250629.vhd"
		}
		var public []string
		if g.rng.Float64() < 0.3 {
			public = []string{g.publicIP()}
			if !aws {
				extra["bandwidth_out_mbps"] = []int{5, 10, 20, 50, 100}[g.rng.IntN(5)]
			}
		}
		r := cloud.Resource{
			Type: model.TypeVM, Region: region, Zone: g.zone(region), ResourceID: id, Name: name,
			Status: status, RawStatus: raw, Spec: spec,
			PrivateIPs: []string{g.privateIP(region)}, PublicIPs: public,
			VpcID: g.vpc(region), CreatedAt: g.created(), Tags: g.tags(role), Extra: extra,
		}
		g.charge(&r)
		return r, g.cpuBase()
	case model.TypeRDS:
		name := g.uniqueName(g.envPrefix() + "-" + rdsNames[g.rng.IntN(len(rdsNames))])
		extra := map[string]any{}
		var id, spec, raw, status string
		if aws {
			e := awsRDS[g.rng.IntN(len(awsRDS))]
			id, spec = name, e.class
			extra["engine"], extra["engine_version"] = e.engine, e.version
			extra["allocated_storage_gib"] = 100 * (1 + g.rng.IntN(10))
			extra["endpoint"] = fmt.Sprintf("%s.c%s.%s.rds.amazonaws.com:%d", name, g.randString(alnum, 12), region, map[string]int{"mysql": 3306, "postgres": 5432}[e.engine])
			raw, status = "available", model.StatusRunning
			if g.rng.Float64() > 0.95 {
				raw, status = "backing-up", model.StatusChanging
			}
		} else {
			e := aliRDS[g.rng.IntN(len(aliRDS))]
			id = "rm-" + g.aliPrefix(region) + g.randString(alnum, 16)
			spec = e.class
			extra["engine"], extra["engine_version"] = e.engine, e.version
			extra["storage_gb"] = 100 * (1 + g.rng.IntN(10))
			extra["endpoint"] = id + ".mysql.rds.aliyuncs.com:3306"
			raw, status = "Running", model.StatusRunning
		}
		r := cloud.Resource{
			Type: model.TypeRDS, Region: region, Zone: g.zone(region), ResourceID: id, Name: name,
			Status: status, RawStatus: raw, Spec: spec, VpcID: g.vpc(region), CreatedAt: g.created(),
			Tags: g.tags(strings.TrimSuffix(strings.TrimPrefix(name, g.envPrefix()+"-"), "-db")), Extra: extra,
		}
		g.charge(&r)
		return r, 0
	case model.TypeLB:
		name := g.uniqueName(g.envPrefix() + "-" + lbNames[g.rng.IntN(len(lbNames))])
		internet := g.rng.Float64() < 0.6
		network := "internal"
		if internet {
			network = "internet"
		}
		extra := map[string]any{"network": network}
		var id, spec, raw string
		if aws {
			kind := cloud.LBKindALB
			prefix := "app"
			if g.rng.Float64() < 0.3 {
				kind, prefix = cloud.LBKindNLB, "net"
			}
			id = fmt.Sprintf("%s/%s/%s", prefix, name, g.randString(hexdigits, 16))
			spec = strings.ToUpper(kind)
			suffix := "amazonaws.com"
			if strings.HasPrefix(region, "cn-") {
				suffix = "amazonaws.com.cn"
			}
			extra["lb_kind"] = kind
			extra["address"] = fmt.Sprintf("%s-%d.%s.elb.%s", name, 100000+g.rng.IntN(899999), region, suffix)
			raw = "active"
		} else if g.rng.Float64() < 0.55 {
			id = "lb-" + g.aliPrefix(region) + g.randString(alnum, 17)
			spec = "CLB"
			extra["lb_kind"] = cloud.LBKindCLB
			extra["lb_spec"] = []string{"slb.s1.small", "slb.s2.medium", "slb.s3.medium"}[g.rng.IntN(3)]
			if internet {
				extra["address"] = g.publicIP()
			} else {
				extra["address"] = g.privateIP(region)
			}
			raw = "active"
		} else {
			id = "alb-" + g.randString(alnum, 16)
			spec = "ALB"
			extra["lb_kind"] = cloud.LBKindALB
			extra["lb_spec"] = []string{"标准版", "基础版"}[g.rng.IntN(2)]
			extra["address"] = fmt.Sprintf("%s.%s.alb.aliyuncs.com", id, region)
			raw = "Active"
		}
		r := cloud.Resource{
			Type: model.TypeLB, Region: region, ResourceID: id, Name: name,
			Status: model.StatusRunning, RawStatus: raw, Spec: spec, VpcID: g.vpc(region),
			CreatedAt: g.created(), Tags: g.tags("gateway"), Extra: extra,
		}
		g.charge(&r)
		return r, 0
	default:
		name := g.uniqueName(fmt.Sprintf("%s-%s-%s", g.envPrefix(), bucketUses[g.rng.IntN(len(bucketUses))], g.randString(alnum, 4)))
		size := math.Pow(10, 9+g.rng.Float64()*4.5)
		extra := map[string]any{"size_bytes": math.Round(size), "object_count": math.Round(size / (150e3 + g.rng.Float64()*900e3))}
		spec := "S3"
		if !aws {
			spec = "OSS"
			class := []string{"Standard", "Standard", "Standard", "IA", "Archive"}[g.rng.IntN(5)]
			extra["storage_class"] = class
			extra["storage_class_label"] = map[string]string{"Standard": "标准存储", "IA": "低频访问", "Archive": "归档存储"}[class]
		}
		return cloud.Resource{
			Type: model.TypeBucket, Region: region, ResourceID: name, Name: name,
			Status: model.StatusRunning, RawStatus: "available", Spec: spec, ChargeType: model.ChargePostpaid,
			CreatedAt: g.created(), Tags: map[string]string{}, Extra: extra,
		}, 0
	}
}

// charge sets the billing mode; Alibaba Cloud resources are often prepaid.
func (g *gen) charge(r *cloud.Resource) {
	r.ChargeType = model.ChargePostpaid
	if g.prof.provider != model.ProviderAliyun || g.rng.Float64() > 0.45 {
		return
	}
	r.ChargeType = model.ChargePrepaid
	t := g.expireIn(45 + g.rng.IntN(360))
	r.ExpireAt = &t
}

func (g *gen) zone(region string) string {
	if g.prof.provider == model.ProviderAWS {
		return region + string(rune('a'+g.rng.IntN(3)))
	}
	return region + "-" + string(rune('g'+g.rng.IntN(6)))
}

func (g *gen) vpc(region string) string {
	if g.prof.provider == model.ProviderAWS {
		return fmt.Sprintf("vpc-0%015x", hash64(g.ak, region)%(1<<60))
	}
	return "vpc-" + g.aliPrefix(region) + fmt.Sprintf("%017x", hash64(g.ak, region)%(1<<60))[:17]
}

func heroID(h hero, prof profile) string {
	if h.id != "" {
		return h.id
	}
	switch h.typ {
	case model.TypeVM:
		if prof.provider == model.ProviderAWS {
			return fmt.Sprintf("i-0%016x", hash64(h.name))
		}
		return "i-" + map[string]string{"cn-hangzhou": "bp1", "cn-shanghai": "uf6", "cn-beijing": "2ze"}[h.region] + fmt.Sprintf("%017x", hash64(h.name))[:17]
	case model.TypeRDS:
		if prof.provider == model.ProviderAWS {
			return h.name
		}
		return "rm-" + fmt.Sprintf("%016x", hash64(h.name))
	case model.TypeBucket:
		return h.name
	}
	return h.name
}

func (g *gen) heroResource(h hero) (cloud.Resource, float64) {
	id := heroID(h, g.prof)
	aws := g.prof.provider == model.ProviderAWS
	status := map[string]string{
		"running": model.StatusRunning, "Running": model.StatusRunning, "available": model.StatusRunning,
		"active": model.StatusRunning, "Active": model.StatusRunning,
		"stopped": model.StatusStopped, "Stopped": model.StatusStopped, "inactive": model.StatusStopped,
		"pending": model.StatusPending, "Starting": model.StatusStarting, "modifying": model.StatusChanging,
	}[h.status]
	if h.typ == model.TypeBucket {
		status = model.StatusRunning
		h.status = "available"
	}
	extra := map[string]any{}
	for k, v := range h.extra {
		extra[k] = v
	}
	var private, public []string
	if h.priv != "" {
		private = []string{h.priv}
	}
	if h.pub != "" {
		public = []string{h.pub}
	}
	spec := h.spec
	switch h.typ {
	case model.TypeVM:
		if h.priv == "" {
			private = []string{g.privateIP(h.region)}
		}
		vcpu, mem := specSize(spec)
		extra["cpu"], extra["memory_mib"] = vcpu, mem
		if aws {
			extra["os"] = "Linux/UNIX"
			extra["subnet_id"] = "subnet-0" + g.randString(hexdigits, 16)
			extra["security_groups"] = []string{"sg-0" + g.randString(hexdigits, 16)}
		} else {
			extra["os"] = "Alibaba Cloud Linux 3.2104 LTS 64位"
			extra["vswitch_id"] = "vsw-" + g.aliPrefix(h.region) + g.randString(alnum, 17)
			extra["security_groups"] = []string{"sg-" + g.aliPrefix(h.region) + g.randString(alnum, 17)}
		}
	case model.TypeRDS:
		if _, ok := extra["endpoint"]; !ok {
			if aws {
				extra["endpoint"] = fmt.Sprintf("%s.c%s.%s.rds.amazonaws.com:5432", h.name, g.randString(alnum, 12), h.region)
			} else {
				extra["endpoint"] = id + ".mysql.rds.aliyuncs.com:3306"
			}
		}
	case model.TypeLB:
		spec = strings.ToUpper(cloud.ExtraString(extra, "lb_kind"))
	case model.TypeBucket:
		spec = "S3"
		if !aws {
			spec = "OSS"
		}
	}
	tags := h.tags
	if tags == nil {
		tags = g.tags(strings.Split(h.name, "-")[0])
	}
	zone := ""
	if h.typ != model.TypeBucket && h.typ != model.TypeLB {
		zone = g.zone(h.region)
	}
	r := cloud.Resource{
		Type: h.typ, Region: h.region, Zone: zone, ResourceID: id, Name: h.name,
		Status: status, RawStatus: h.status, Spec: spec, PrivateIPs: private, PublicIPs: public,
		VpcID: g.vpc(h.region), ChargeType: model.ChargePostpaid, CreatedAt: g.created(), Tags: tags, Extra: extra,
	}
	if h.typ == model.TypeBucket {
		r.VpcID = ""
	}
	if h.expireIn > 0 {
		t := g.expireIn(h.expireIn)
		r.ChargeType = model.ChargePrepaid
		r.ExpireAt = &t
	}
	return r, h.cpu
}
