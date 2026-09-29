package aliyun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	alb "github.com/alibabacloud-go/alb-20200616/v2/client"
	cms "github.com/alibabacloud-go/cms-20190101/v10/client"
	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	rds "github.com/alibabacloud-go/rds-20140815/v16/client"
	slb "github.com/alibabacloud-go/slb-20140515/v4/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type fakeECS struct{ pages int }

func (f *fakeECS) DescribeInstancesWithContext(ctx context.Context, req *ecs.DescribeInstancesRequest, _ *dara.RuntimeOptions) (*ecs.DescribeInstancesResponse, error) {
	f.pages++
	if req.NextToken == nil {
		return &ecs.DescribeInstancesResponse{Body: &ecs.DescribeInstancesResponseBody{
			NextToken: dara.String("page2"),
			Instances: &ecs.DescribeInstancesResponseBodyInstances{Instance: []*ecs.DescribeInstancesResponseBodyInstancesInstance{{
				InstanceId: dara.String("i-bp1"), InstanceName: dara.String("prod-api-07"), Status: dara.String("Running"),
				InstanceType: dara.String("ecs.c7.2xlarge"), Cpu: dara.Int32(8), Memory: dara.Int32(16384),
				ZoneId: dara.String("cn-hangzhou-h"), InstanceChargeType: dara.String("PrePaid"),
				ExpiredTime: dara.String("2027-03-13T16:00Z"), CreationTime: dara.String("2025-03-14T02:22Z"),
				VpcAttributes: &ecs.DescribeInstancesResponseBodyInstancesInstanceVpcAttributes{
					VpcId: dara.String("vpc-1"), VSwitchId: dara.String("vsw-1"),
					PrivateIpAddress: &ecs.DescribeInstancesResponseBodyInstancesInstanceVpcAttributesPrivateIpAddress{IpAddress: []*string{dara.String("10.12.4.21")}},
				},
				EipAddress: &ecs.DescribeInstancesResponseBodyInstancesInstanceEipAddress{IpAddress: dara.String("47.98.113.20")},
				Tags: &ecs.DescribeInstancesResponseBodyInstancesInstanceTags{Tag: []*ecs.DescribeInstancesResponseBodyInstancesInstanceTagsTag{
					{TagKey: dara.String("env"), TagValue: dara.String("prod")},
				}},
				InternetMaxBandwidthOut: dara.Int32(200),
			}}},
		}}, nil
	}
	return &ecs.DescribeInstancesResponse{Body: &ecs.DescribeInstancesResponseBody{
		Instances: &ecs.DescribeInstancesResponseBodyInstances{Instance: []*ecs.DescribeInstancesResponseBodyInstancesInstance{{
			InstanceId: dara.String("i-bp2"), Status: dara.String("Stopped"), InstanceChargeType: dara.String("PostPaid"),
			ExpiredTime: dara.String("2099-12-31T15:59Z"),
		}}},
	}}, nil
}

func TestCollectECS(t *testing.T) {
	api := &fakeECS{}
	res, err := collectECS(context.Background(), api, "cn-hangzhou")
	if err != nil {
		t.Fatal(err)
	}
	if api.pages != 2 || len(res) != 2 {
		t.Fatalf("pagination: pages=%d resources=%d", api.pages, len(res))
	}
	r := res[0]
	if r.Name != "prod-api-07" || r.Status != model.StatusRunning || r.ChargeType != model.ChargePrepaid {
		t.Fatalf("resource: %+v", r)
	}
	if r.ExpireAt == nil || r.ExpireAt.Format(time.RFC3339) != "2027-03-13T16:00:00Z" {
		t.Fatalf("expire: %v", r.ExpireAt)
	}
	if len(r.PrivateIPs) != 1 || r.PublicIPs[0] != "47.98.113.20" || r.Tags["env"] != "prod" || r.Extra["bandwidth_out_mbps"] != int32(200) {
		t.Fatalf("details: %+v", r)
	}
	stopped := res[1]
	if stopped.Name != "i-bp2" || stopped.Status != model.StatusStopped || stopped.ExpireAt != nil {
		t.Fatalf("pay-as-you-go instance must not expire: %+v", stopped)
	}
}

type fakeRDS struct{}

func (fakeRDS) DescribeDBInstancesWithContext(ctx context.Context, req *rds.DescribeDBInstancesRequest, _ *dara.RuntimeOptions) (*rds.DescribeDBInstancesResponse, error) {
	return &rds.DescribeDBInstancesResponse{Body: &rds.DescribeDBInstancesResponseBody{
		TotalRecordCount: dara.Int32(2),
		Items: &rds.DescribeDBInstancesResponseBodyItems{DBInstance: []*rds.DescribeDBInstancesResponseBodyItemsDBInstance{
			{DBInstanceId: dara.String("rm-1"), DBInstanceDescription: dara.String("prod-mysql-main"), DBInstanceStatus: dara.String("Running"),
				Engine: dara.String("MySQL"), EngineVersion: dara.String("8.0"), PayType: dara.String("Prepaid"),
				ExpireTime: dara.String("2026-10-06T16:00:00Z"), ConnectionString: dara.String("rm-1.mysql.rds.aliyuncs.com")},
			{DBInstanceId: dara.String("rm-2"), DBInstanceStatus: dara.String("DBInstanceClassChanging"), PayType: dara.String("Postpaid")},
		}},
	}}, nil
}

func (fakeRDS) DescribeDBInstanceAttributeWithContext(ctx context.Context, req *rds.DescribeDBInstanceAttributeRequest, _ *dara.RuntimeOptions) (*rds.DescribeDBInstanceAttributeResponse, error) {
	if dara.StringValue(req.DBInstanceId) != "rm-1,rm-2" {
		return nil, fmt.Errorf("unexpected ids %s", dara.StringValue(req.DBInstanceId))
	}
	return &rds.DescribeDBInstanceAttributeResponse{Body: &rds.DescribeDBInstanceAttributeResponseBody{
		Items: &rds.DescribeDBInstanceAttributeResponseBodyItems{DBInstanceAttribute: []*rds.DescribeDBInstanceAttributeResponseBodyItemsDBInstanceAttribute{
			{DBInstanceId: dara.String("rm-1"), DBInstanceStorage: dara.Int32(500), MaxConnections: dara.Int32(4000), Port: dara.String("3306")},
		}},
	}}, nil
}

func TestCollectRDS(t *testing.T) {
	res, err := collectRDS(context.Background(), fakeRDS{}, "cn-hangzhou")
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Name != "prod-mysql-main" || res[0].Extra["storage_gb"] != int32(500) || res[0].Extra["endpoint"] != "rm-1.mysql.rds.aliyuncs.com:3306" {
		t.Fatalf("rds: %+v", res[0])
	}
	if res[0].ExpireAt == nil || res[1].Status != model.StatusChanging || res[1].Name != "rm-2" {
		t.Fatalf("rds: %+v / %+v", res[0], res[1])
	}
}

type fakeCLB struct{}

func (fakeCLB) DescribeLoadBalancersWithContext(ctx context.Context, req *slb.DescribeLoadBalancersRequest, _ *dara.RuntimeOptions) (*slb.DescribeLoadBalancersResponse, error) {
	return &slb.DescribeLoadBalancersResponse{Body: &slb.DescribeLoadBalancersResponseBody{
		TotalCount: dara.Int32(1),
		LoadBalancers: &slb.DescribeLoadBalancersResponseBodyLoadBalancers{LoadBalancer: []*slb.DescribeLoadBalancersResponseBodyLoadBalancersLoadBalancer{
			{LoadBalancerId: dara.String("lb-1"), LoadBalancerName: dara.String("gw-slb-public"), LoadBalancerStatus: dara.String("active"),
				Address: dara.String("39.106.22.8"), AddressType: dara.String("internet"), PayType: dara.String("PrePay"),
				LoadBalancerSpec: dara.String("slb.s3.medium")},
		}},
	}}, nil
}

func (fakeCLB) DescribeLoadBalancerAttributeWithContext(ctx context.Context, req *slb.DescribeLoadBalancerAttributeRequest, _ *dara.RuntimeOptions) (*slb.DescribeLoadBalancerAttributeResponse, error) {
	return &slb.DescribeLoadBalancerAttributeResponse{Body: &slb.DescribeLoadBalancerAttributeResponseBody{
		EndTimeStamp: dara.Int64(time.Date(2026, 10, 19, 16, 0, 0, 0, time.UTC).UnixMilli()),
	}}, nil
}

type failingALB struct{ err error }

func (f failingALB) ListLoadBalancersWithContext(ctx context.Context, req *alb.ListLoadBalancersRequest, _ *dara.RuntimeOptions) (*alb.ListLoadBalancersResponse, error) {
	return nil, f.err
}

func TestCollectLBPartialFailure(t *testing.T) {
	res, err := collectLB(context.Background(), fakeCLB{}, failingALB{err: errors.New("request timeout")}, "cn-hongkong")
	if err == nil || len(res) != 1 {
		t.Fatalf("expected CLB results plus an ALB error, got %d %v", len(res), err)
	}
	if res[0].ExpireAt == nil || res[0].Extra["lb_kind"] != cloud.LBKindCLB || res[0].Extra["network"] != "internet" {
		t.Fatalf("clb: %+v", res[0])
	}
	unsupported := &openapi.ClientError{Code: dara.String("InvalidRegionId.NotSupport")}
	res, err = collectLB(context.Background(), fakeCLB{}, failingALB{err: unsupported}, "cn-hongkong")
	if err != nil || len(res) != 1 {
		t.Fatalf("an unsupported ALB region must be ignored: %d %v", len(res), err)
	}
}

type eofRDS struct{}

func (eofRDS) DescribeDBInstancesWithContext(ctx context.Context, req *rds.DescribeDBInstancesRequest, _ *dara.RuntimeOptions) (*rds.DescribeDBInstancesResponse, error) {
	return nil, io.EOF
}

func (eofRDS) DescribeDBInstanceAttributeWithContext(ctx context.Context, req *rds.DescribeDBInstanceAttributeRequest, _ *dara.RuntimeOptions) (*rds.DescribeDBInstanceAttributeResponse, error) {
	return nil, io.EOF
}

// The endpoints of these product regions only close the connection (EOF); they
// are skipped without a call, while EOF anywhere else is still reported.
func TestSkipRegionsWithoutProduct(t *testing.T) {
	if _, err := collectRDS(context.Background(), eofRDS{}, "eu-west-3"); !errors.Is(err, cloud.ErrRegionUnsupported) {
		t.Fatalf("RDS in eu-west-3 must be skipped: %v", err)
	}
	if _, err := collectRDS(context.Background(), eofRDS{}, "eu-central-1"); err == nil || errors.Is(err, cloud.ErrRegionUnsupported) {
		t.Fatalf("EOF in other regions must stay an error: %v", err)
	}
	for _, region := range []string{"cn-huhehaote", "eu-west-3"} {
		res, err := collectLB(context.Background(), fakeCLB{}, failingALB{err: io.EOF}, region)
		if err != nil || len(res) != 1 {
			t.Fatalf("ALB in %s must be skipped and CLB kept: %d %v", region, len(res), err)
		}
	}
	if _, err := collectLB(context.Background(), fakeCLB{}, failingALB{err: io.EOF}, "cn-hangzhou"); err == nil {
		t.Fatal("ALB EOF in a region that offers ALB must be reported")
	}
}

type fakeCMS struct{ calls []string }

func (f *fakeCMS) DescribeMetricListWithContext(ctx context.Context, req *cms.DescribeMetricListRequest, _ *dara.RuntimeOptions) (*cms.DescribeMetricListResponse, error) {
	name := dara.StringValue(req.MetricName)
	f.calls = append(f.calls, name)
	var dps string
	switch name {
	case "IntranetInRate":
		dps = `[{"timestamp":1000,"instanceId":"i-1","Average":100},{"timestamp":2000,"instanceId":"i-1","Average":200}]`
	case "VPC_PublicIP_InternetInRate":
		dps = `[{"timestamp":1000,"instanceId":"i-1","Average":50}]`
	case "CPUUtilization":
		dps = `[{"timestamp":1759050000000,"instanceId":"i-1","Average":20},{"timestamp":1759053600000,"instanceId":"i-1","Average":40},{"timestamp":1759050000000,"instanceId":"i-2","Average":3}]`
	default:
		dps = `[]`
	}
	return &cms.DescribeMetricListResponse{Body: &cms.DescribeMetricListResponseBody{Success: dara.Bool(true), Datapoints: dara.String(dps)}}, nil
}

func TestQueryMetricsSumsSources(t *testing.T) {
	api := &fakeCMS{}
	ref := cloud.ResourceRef{Type: model.TypeVM, Region: "cn-hangzhou", ResourceID: "i-1"}
	series, err := queryMetrics(context.Background(), api, ref, cloud.MetricQuery{Keys: []string{cloud.MetricNetIn}, Start: time.Now().Add(-time.Hour), End: time.Now(), Period: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	pts := series[0].Points
	if len(pts) != 2 || pts[0][1] != 150 || pts[1][1] != 200 || series[0].Unit != cloud.UnitBitsPerS {
		t.Fatalf("net_in points = %v", pts)
	}
	if len(api.calls) != 3 {
		t.Fatalf("expected intranet + internet + VPC public queries, got %v", api.calls)
	}
}

var ossBase = time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)

// ossCMS serves OSS metrics: requests and traffic once a minute from ossBase
// for 7 minutes, storage once an hour.
type ossCMS struct {
	reqs []*cms.DescribeMetricListRequest
}

func (f *ossCMS) DescribeMetricListWithContext(ctx context.Context, req *cms.DescribeMetricListRequest, _ *dara.RuntimeOptions) (*cms.DescribeMetricListResponse, error) {
	f.reqs = append(f.reqs, req)
	var pts []string
	add := func(ts time.Time, v float64) {
		pts = append(pts, fmt.Sprintf(`{"timestamp":%d,"BucketName":"logs","userId":"1","Value":%g}`, ts.UnixMilli(), v))
	}
	switch name := dara.StringValue(req.MetricName); name {
	case "TotalRequestCount", "InternetSend":
		v := 120.0
		if name == "InternetSend" {
			v = 60e6
		}
		for m := range 7 {
			add(ossBase.Add(time.Duration(m)*time.Minute), v)
		}
	case "MeteringStorageUtilization":
		add(ossBase, 1<<30)
	}
	body := &cms.DescribeMetricListResponseBody{Success: dara.Bool(true), Datapoints: dara.String("[" + strings.Join(pts, ",") + "]")}
	return &cms.DescribeMetricListResponse{Body: body}, nil
}

// OSS reports requests and traffic per minute: each point sums its minutes
// into a per-second rate, and the unfinished last point only counts the
// minutes elapsed. Storage is reported hourly and read at that period.
func TestQueryMetricsBucket(t *testing.T) {
	api := &ossCMS{}
	ref := cloud.ResourceRef{Type: model.TypeBucket, Region: "cn-hangzhou", ResourceID: "logs"}
	series, err := queryMetrics(context.Background(), api, ref, cloud.MetricQuery{
		Keys:  []string{cloud.MetricRequests, cloud.MetricNetOut, cloud.MetricStorage},
		Start: ossBase.Add(2 * time.Minute), End: ossBase.Add(7 * time.Minute), Period: 5 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]cloud.Point{}
	for _, s := range series {
		got[s.Key] = s.Points
	}
	// 5 × 120 requests over 300s, then 2 × 120 over the 120s elapsed.
	if p := got[cloud.MetricRequests]; len(p) != 2 || p[0][1] != 2 || p[1][1] != 2 {
		t.Fatalf("requests = %v", p)
	}
	// 60 MB a minute is 8 Mbit/s.
	if p := got[cloud.MetricNetOut]; len(p) != 2 || p[0][1] != 8e6 || p[1][1] != 8e6 {
		t.Fatalf("net_out = %v", p)
	}
	if p := got[cloud.MetricStorage]; len(p) != 1 || p[0][1] != 1<<30 {
		t.Fatalf("storage = %v", p)
	}
	for _, req := range api.reqs {
		name, period := dara.StringValue(req.MetricName), dara.StringValue(req.Period)
		want := "60"
		if name == "MeteringStorageUtilization" {
			want = "3600"
		}
		if period != want || dara.StringValue(req.Namespace) != "acs_oss_dashboard" || dara.StringValue(req.Dimensions) != `[{"BucketName":"logs"}]` {
			t.Fatalf("%s: period %s, namespace %s, dimensions %s", name, period, dara.StringValue(req.Namespace), dara.StringValue(req.Dimensions))
		}
		if want == "60" && dara.StringValue(req.StartTime) != strconv.FormatInt(ossBase.UnixMilli(), 10) {
			t.Fatalf("%s must start at the aligned point, got %s", name, dara.StringValue(req.StartTime))
		}
	}
	if keys := SupportedMetrics(ref); len(keys) != 4 || slices.Contains(keys, cloud.MetricObjects) {
		t.Fatalf("oss metrics = %v", keys)
	}
}

func TestCPUSnapshot(t *testing.T) {
	stats, err := cpuSnapshot(context.Background(), &fakeCMS{}, "cn-hangzhou", []string{"i-1", "i-2"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if avg, _ := stats["i-1"].Average(); avg != 30 {
		t.Fatalf("i-1 average = %v", avg)
	}
	if last, _ := stats["i-1"].Last(); last != 40 {
		t.Fatalf("i-1 last = %v", last)
	}
	if _, ok := stats["i-2"]; !ok {
		t.Fatal("i-2 missing")
	}
}

func TestSupportedMetrics(t *testing.T) {
	clb := cloud.ResourceRef{Type: model.TypeLB, Extra: map[string]any{"lb_kind": cloud.LBKindCLB}}
	if len(SupportedMetrics(clb)) != 4 {
		t.Fatalf("clb metrics = %v", SupportedMetrics(clb))
	}
	rdsRef := cloud.ResourceRef{Type: model.TypeRDS}
	got := SupportedMetrics(rdsRef)
	for _, k := range got {
		if k == cloud.MetricConnections || k == cloud.MetricFreeMem {
			t.Fatalf("aliyun rds must not claim %s", k)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		err  error
		kind error
	}{
		{&openapi.ClientError{Code: dara.String("InvalidAccessKeyId.NotFound")}, cloud.ErrAuth},
		{&openapi.ClientError{Code: dara.String("SignatureDoesNotMatch")}, cloud.ErrAuth},
		{&openapi.ClientError{Code: dara.String("Forbidden.RAM")}, cloud.ErrPermission},
		{&openapi.ClientError{Code: dara.String("InvalidRegionId.NotSupport")}, cloud.ErrRegionUnsupported},
		{&openapi.ThrottlingError{Code: dara.String("Throttling.User")}, cloud.ErrThrottled},
		{dara.NewSDKError(map[string]interface{}{"code": "Forbidden", "message": "no"}), cloud.ErrPermission},
	}
	for _, c := range cases {
		if got := classify(c.err); !errors.Is(got, c.kind) {
			t.Errorf("classify(%v) = %v, want %v", c.err, got, c.kind)
		}
	}
}
