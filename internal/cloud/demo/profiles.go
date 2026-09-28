// Package demo implements cloud.Provider with generated, deterministic data
// so the platform can be tried without real cloud credentials. The seeded
// accounts and resources mirror the product prototype.
package demo

import "github.com/presoulgo/go-aws-aliyun/internal/model"

// SeedAccount is a demo account created on first start in demo mode.
type SeedAccount struct {
	Name        string
	Provider    string
	Partition   string
	AccessKeyID string
	Secret      string
	RoleARN     string
	Remark      string
	Regions     []string
}

type counts struct{ vm, rds, lb, bucket int }

type profile struct {
	provider  string
	uid       string
	env       string
	enabled   []string // what ListRegions returns
	resources []string // regions that hold resources, heaviest first
	counts    counts
	// expiring is how many generated prepaid resources expire within 30 days
	// (on top of the fixed ones).
	expiring int
	// failRegion simulates an ALB timeout in this region.
	failRegion string
}

var awsGlobalRegions = []string{
	"us-east-1", "us-east-2", "us-west-1", "us-west-2", "ap-northeast-1", "ap-northeast-2", "ap-northeast-3",
	"ap-southeast-1", "ap-southeast-2", "ap-south-1", "eu-central-1", "eu-west-1", "eu-west-2", "eu-west-3",
	"eu-north-1", "ca-central-1", "sa-east-1",
}

var awsChinaRegions = []string{"cn-north-1", "cn-northwest-1"}

var aliyunRegions = []string{
	"cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-zhangjiakou", "cn-shenzhen", "cn-guangzhou", "cn-chengdu",
	"cn-hongkong", "ap-southeast-1", "ap-northeast-1", "ap-southeast-5", "us-west-1", "us-east-1", "eu-central-1",
}

const (
	akAWSProd   = "AKIADEMOPROD00000001"
	akAWSChina  = "AKIADEMOCHINA0000002"
	akAWSData   = "AKIADEMODATA00000003"
	akAliMain   = "LTAI5tDEMOMAIN000001"
	akAliShop   = "LTAI5tDEMOSHOP000002"
	akAliTest   = "LTAI5tDEMOTEST000003"
	demoSecret  = "demo-secret-not-real"
	roleAWSData = "arn:aws:iam::771044329015:role/OpsReadOnly"
	roleAliShop = "acs:ram::1507442290112290:role/ops-readonly"
)

var profiles = map[string]profile{
	akAWSProd: {
		provider: model.ProviderAWS, uid: "482910345561", env: "prod", enabled: awsGlobalRegions,
		resources: []string{"us-east-1", "us-west-2", "ap-northeast-1", "eu-central-1", "us-east-2", "ap-southeast-1", "eu-west-1", "ap-south-1", "sa-east-1"},
		counts:    counts{vm: 120, rds: 14, lb: 18, bucket: 57},
	},
	akAWSChina: {
		provider: model.ProviderAWS, uid: "092566712208", env: "prod", enabled: awsChinaRegions,
		resources: awsChinaRegions,
		counts:    counts{vm: 38, rds: 4, lb: 5, bucket: 17},
	},
	akAWSData: {
		provider: model.ProviderAWS, uid: "771044329015", env: "data", enabled: awsGlobalRegions,
		resources: []string{"ap-northeast-1", "eu-central-1", "us-west-2"},
		counts:    counts{vm: 56, rds: 6, lb: 8, bucket: 21},
	},
	akAliMain: {
		provider: model.ProviderAliyun, uid: "1829330177664401", env: "prod", enabled: aliyunRegions,
		resources: []string{"cn-hangzhou", "cn-shanghai", "cn-beijing", "cn-shenzhen", "cn-hongkong", "ap-southeast-1"},
		counts:    counts{vm: 96, rds: 12, lb: 15, bucket: 24},
		expiring:  3, failRegion: "cn-hongkong",
	},
	akAliShop: {
		provider: model.ProviderAliyun, uid: "1507442290112290", env: "prod", enabled: aliyunRegions,
		resources: []string{"cn-hangzhou", "cn-shanghai", "cn-shenzhen", "cn-beijing"},
		counts:    counts{vm: 112, rds: 14, lb: 17, bucket: 33},
		expiring:  3,
	},
	akAliTest: {
		provider: model.ProviderAliyun, uid: "1391204488990876", env: "test", enabled: aliyunRegions,
		resources: []string{"cn-beijing", "cn-hangzhou", "cn-zhangjiakou"},
		counts:    counts{vm: 64, rds: 8, lb: 10, bucket: 15},
		expiring:  2,
	},
}

// SeedAccounts returns the demo accounts shown in the prototype.
func SeedAccounts() []SeedAccount {
	return []SeedAccount{
		{Name: "AWS 生产账号", Provider: model.ProviderAWS, Partition: model.PartitionAWS, AccessKeyID: akAWSProd, Secret: demoSecret,
			Regions: profiles[akAWSProd].resources, Remark: "海外电商主站"},
		{Name: "AWS 中国区", Provider: model.ProviderAWS, Partition: model.PartitionAWSCN, AccessKeyID: akAWSChina, Secret: demoSecret,
			Remark: "北京、宁夏区域"},
		{Name: "AWS 数据平台", Provider: model.ProviderAWS, Partition: model.PartitionAWS, AccessKeyID: akAWSData, Secret: demoSecret,
			RoleARN: roleAWSData, Regions: profiles[akAWSData].resources, Remark: "通过 AssumeRole 访问成员账号"},
		{Name: "阿里云 主账号", Provider: model.ProviderAliyun, AccessKeyID: akAliMain, Secret: demoSecret, Remark: "核心业务"},
		{Name: "阿里云 电商业务", Provider: model.ProviderAliyun, AccessKeyID: akAliShop, Secret: demoSecret,
			RoleARN: roleAliShop, Regions: profiles[akAliShop].resources, Remark: "通过 RAM 角色访问"},
		{Name: "阿里云 测试账号", Provider: model.ProviderAliyun, AccessKeyID: akAliTest, Secret: demoSecret, Remark: "测试与 CI"},
	}
}

// hero is a hand-written resource from the prototype.
type hero struct {
	ak       string
	typ      string
	region   string
	zone     string // optional; random when empty
	name     string
	id       string
	spec     string
	status   string // raw provider status
	cpu      float64
	priv     string
	pub      string
	expireIn int // days, 0 = none (pay as you go)
	extra    map[string]any
	tags     map[string]string
}

var heroes = []hero{
	// AWS 生产账号
	{ak: akAWSProd, typ: model.TypeVM, region: "us-east-1", zone: "us-east-1a", name: "order-worker-03", id: "i-0a7f3c29e1b84d5f6", spec: "c6i.2xlarge", status: "running", cpu: 88.1, priv: "172.31.8.144", pub: "54.210.33.17",
		tags: map[string]string{"env": "prod", "app": "order", "team": "trade"}},
	{ak: akAWSProd, typ: model.TypeVM, region: "us-east-1", zone: "us-east-1b", name: "bastion-01", id: "i-0f19d3a6c8b27e540", spec: "t3.small", status: "running", cpu: 2.1, priv: "172.31.0.10", pub: "3.91.24.188",
		tags: map[string]string{"env": "prod", "app": "bastion", "team": "ops"}},
	{ak: akAWSProd, typ: model.TypeVM, region: "us-east-1", name: "api-gw-02", spec: "c6i.xlarge", status: "running", cpu: 71.3},
	{ak: akAWSProd, typ: model.TypeVM, region: "us-west-2", name: "orders-sync-05", spec: "m6i.xlarge", status: "running", cpu: 61.0},
	{ak: akAWSProd, typ: model.TypeRDS, region: "us-east-1", name: "orders-pg", spec: "db.r6g.xlarge", status: "available",
		extra: map[string]any{"engine": "postgres", "engine_version": "16.4", "allocated_storage_gib": 400}},
	{ak: akAWSProd, typ: model.TypeLB, region: "us-east-1", name: "api-alb-prod", id: "app/api-alb-prod/7c1e9d2a4b6f8031", status: "active",
		extra: map[string]any{"lb_kind": "alb", "network": "internet", "address": "api-alb-prod-1843729.us-east-1.elb.amazonaws.com"}},
	{ak: akAWSProd, typ: model.TypeBucket, region: "us-east-1", name: "prod-logs-archive-us",
		extra: map[string]any{"size_bytes": 41.6e12, "object_count": 902441887}},
	// AWS 数据平台
	{ak: akAWSData, typ: model.TypeVM, region: "ap-northeast-1", zone: "ap-northeast-1c", name: "ingest-node-11", id: "i-05c2e9b7d41a3f8e2", spec: "m6i.xlarge", status: "running", cpu: 79.2, priv: "10.40.3.77"},
	{ak: akAWSData, typ: model.TypeVM, region: "ap-northeast-1", name: "kafka-broker-1", spec: "r6i.xlarge", status: "running", cpu: 66.8},
	{ak: akAWSData, typ: model.TypeVM, region: "eu-central-1", zone: "eu-central-1a", name: "analytics-etl-02", id: "i-0b6e1d4f7a92c3805", spec: "r6i.2xlarge", status: "pending", priv: "10.50.1.23"},
	{ak: akAWSData, typ: model.TypeRDS, region: "ap-northeast-1", name: "analytics-mysql", spec: "db.m6i.large", status: "modifying",
		extra: map[string]any{"engine": "mysql", "engine_version": "8.0.39", "allocated_storage_gib": 200}},
	{ak: akAWSData, typ: model.TypeLB, region: "ap-northeast-1", name: "ingest-nlb", id: "net/ingest-nlb/2f4a6c8e0b1d3f57", status: "active",
		extra: map[string]any{"lb_kind": "nlb", "network": "internal", "address": "ingest-nlb-9a8b7c.elb.ap-northeast-1.amazonaws.com"}},
	{ak: akAWSData, typ: model.TypeBucket, region: "ap-northeast-1", name: "data-lake-raw",
		extra: map[string]any{"size_bytes": 118.3e12, "object_count": 1284002519}},
	// AWS 中国区
	{ak: akAWSChina, typ: model.TypeRDS, region: "cn-north-1", name: "cn-app-db", spec: "db.m5.large", status: "available",
		extra: map[string]any{"engine": "mysql", "engine_version": "8.0.39", "allocated_storage_gib": 100}},
	{ak: akAWSChina, typ: model.TypeLB, region: "cn-northwest-1", name: "cn-web-alb", id: "app/cn-web-alb/0d2f4b6a8c1e3a57", status: "active",
		extra: map[string]any{"lb_kind": "alb", "network": "internet", "address": "cn-web-alb-11223.cn-northwest-1.elb.amazonaws.com.cn"}},
	{ak: akAWSChina, typ: model.TypeBucket, region: "cn-north-1", name: "cn-app-uploads",
		extra: map[string]any{"size_bytes": 736e9, "object_count": 2093775}},
	// 阿里云 主账号
	{ak: akAliMain, typ: model.TypeVM, region: "cn-shanghai", zone: "cn-shanghai-l", name: "es-data-02", id: "i-uf6d0n3v8r2k5t1w9q", spec: "ecs.r7.4xlarge", status: "Running", cpu: 83.6, priv: "10.20.1.9", expireIn: 65},
	{ak: akAliMain, typ: model.TypeVM, region: "cn-beijing", name: "redis-proxy-01", spec: "ecs.g7.large", status: "Running", cpu: 74.5},
	{ak: akAliMain, typ: model.TypeVM, region: "cn-hangzhou", zone: "cn-hangzhou-i", name: "gw-nat-02", id: "i-bp1c7h3j9k2l5m8n0p", spec: "ecs.g7.large", status: "Running", cpu: 23.5, priv: "10.12.0.5", pub: "116.62.40.9", expireIn: 114},
	{ak: akAliMain, typ: model.TypeRDS, region: "cn-hangzhou", zone: "cn-hangzhou-h", name: "prod-mysql-main", id: "rm-bp1x7k2m9n3q4w5e6", spec: "rds.mysql.x4.large.2c", status: "Running", expireIn: 8,
		extra: map[string]any{"engine": "MySQL", "engine_version": "8.0", "storage_gb": 500}},
	{ak: akAliMain, typ: model.TypeRDS, region: "cn-beijing", name: "report-sqlserver", id: "rm-2ze4h6j8k0l2m4n6", spec: "mssql.x4.medium.e2", status: "Running", expireIn: 215,
		extra: map[string]any{"engine": "SQLServer", "engine_version": "2019_std_ha", "storage_gb": 250}},
	{ak: akAliMain, typ: model.TypeLB, region: "cn-beijing", name: "gw-slb-public", id: "lb-2zem3k8x1q9v7r5t0", status: "active", expireIn: 21,
		extra: map[string]any{"lb_kind": "clb", "network": "internet", "address": "39.106.22.8", "lb_spec": "slb.s3.medium"}},
	{ak: akAliMain, typ: model.TypeLB, region: "cn-shanghai", name: "internal-slb-01", id: "lb-uf6q8w2e4r6t8y0u", status: "inactive",
		extra: map[string]any{"lb_kind": "clb", "network": "internal", "address": "10.20.0.100", "lb_spec": "slb.s1.small"}},
	{ak: akAliMain, typ: model.TypeBucket, region: "cn-shanghai", name: "backup-rds-daily",
		extra: map[string]any{"size_bytes": 16.2e12, "object_count": 4108, "storage_class": "IA", "storage_class_label": "低频访问"}},
	// 阿里云 电商业务
	{ak: akAliShop, typ: model.TypeVM, region: "cn-hangzhou", zone: "cn-hangzhou-h", name: "prod-api-07", id: "i-bp1f3k9x2m7qa8d0c1e", spec: "ecs.c7.2xlarge", status: "Running", cpu: 92.4,
		priv: "10.12.4.21", pub: "47.98.113.20", expireIn: 167,
		extra: map[string]any{"bandwidth_out_mbps": 200},
		tags:  map[string]string{"env": "prod", "app": "api-gateway", "team": "trade", "owner": "ops", "cost-center": "cc-102", "managed-by": "terraform"}},
	{ak: akAliShop, typ: model.TypeVM, region: "cn-shanghai", zone: "cn-shanghai-g", name: "prod-web-12", id: "i-uf618w2l5q0z7n4m6x3", spec: "ecs.g7.xlarge", status: "Running", cpu: 41.7, priv: "10.20.6.12", pub: "139.196.8.45", expireIn: 13},
	{ak: akAliShop, typ: model.TypeVM, region: "cn-hangzhou", name: "search-api-03", spec: "ecs.c7.xlarge", status: "Running", cpu: 69.9},
	{ak: akAliShop, typ: model.TypeVM, region: "cn-shanghai", name: "video-transcode-2", spec: "ecs.c7.4xlarge", status: "Running", cpu: 64.2},
	{ak: akAliShop, typ: model.TypeVM, region: "cn-hangzhou", name: "bi-report-02", spec: "ecs.g7.xlarge", status: "Running", cpu: 18.4, expireIn: 26},
	{ak: akAliShop, typ: model.TypeRDS, region: "cn-shanghai", zone: "cn-shanghai-l", name: "user-center-db", id: "rm-uf6a2b8c4d0e1f3g5", spec: "mysql.n4.xlarge.2c", status: "Running", expireIn: 143,
		extra: map[string]any{"engine": "MySQL", "engine_version": "8.0", "storage_gb": 300}},
	{ak: akAliShop, typ: model.TypeLB, region: "cn-shanghai", name: "shop-alb", id: "alb-8k2m4n6p0q2r4s6t", status: "Active",
		extra: map[string]any{"lb_kind": "alb", "network": "internet", "address": "alb-8k2m4n6p0q2r4s6t.cn-shanghai.alb.aliyuncs.com", "lb_spec": "标准版"}},
	{ak: akAliShop, typ: model.TypeBucket, region: "cn-hangzhou", name: "shop-static-assets",
		extra: map[string]any{"size_bytes": 2.84e12, "object_count": 18420113, "storage_class": "Standard", "storage_class_label": "标准存储"}},
	// 阿里云 测试账号
	{ak: akAliTest, typ: model.TypeVM, region: "cn-beijing", zone: "cn-beijing-k", name: "ci-runner-04", id: "i-2ze9k1f7m3p0r6t2y8", spec: "ecs.c6.large", status: "Stopped", priv: "10.30.2.40"},
	{ak: akAliTest, typ: model.TypeBucket, region: "cn-beijing", name: "test-artifacts",
		extra: map[string]any{"size_bytes": 512e9, "object_count": 88301, "storage_class": "Standard", "storage_class_label": "标准存储"}},
}
