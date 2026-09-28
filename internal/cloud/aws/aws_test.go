package aws

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/smithy-go"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type fakeEC2 struct{}

func (fakeEC2) DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	launch := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{Instances: []ec2types.Instance{
		{
			InstanceId: aws.String("i-running"), InstanceType: "c6i.2xlarge",
			State:            &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
			Placement:        &ec2types.Placement{AvailabilityZone: aws.String("us-east-1a")},
			PrivateIpAddress: aws.String("172.31.8.144"), PublicIpAddress: aws.String("54.210.33.17"),
			VpcId: aws.String("vpc-1"), SubnetId: aws.String("subnet-1"), LaunchTime: &launch,
			PlatformDetails: aws.String("Linux/UNIX"),
			Tags:            []ec2types.Tag{{Key: aws.String("Name"), Value: aws.String("order-worker-03")}, {Key: aws.String("env"), Value: aws.String("prod")}},
			SecurityGroups:  []ec2types.GroupIdentifier{{GroupId: aws.String("sg-1")}},
			NetworkInterfaces: []ec2types.InstanceNetworkInterface{{PrivateIpAddresses: []ec2types.InstancePrivateIpAddress{
				{PrivateIpAddress: aws.String("172.31.8.144"), Association: &ec2types.InstanceNetworkInterfaceAssociation{PublicIp: aws.String("54.210.33.17")}},
				{PrivateIpAddress: aws.String("172.31.8.145")},
			}}},
		},
		{InstanceId: aws.String("i-stopped"), InstanceType: "t3.small", State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameStopped}},
		{InstanceId: aws.String("i-gone"), InstanceType: "t3.small", State: &ec2types.InstanceState{Name: ec2types.InstanceStateNameTerminated}},
	}}}}, nil
}

func (fakeEC2) DescribeInstanceTypes(ctx context.Context, in *ec2.DescribeInstanceTypesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstanceTypesOutput, error) {
	return &ec2.DescribeInstanceTypesOutput{InstanceTypes: []ec2types.InstanceTypeInfo{
		{InstanceType: "c6i.2xlarge", VCpuInfo: &ec2types.VCpuInfo{DefaultVCpus: aws.Int32(8)}, MemoryInfo: &ec2types.MemoryInfo{SizeInMiB: aws.Int64(16384)}},
	}}, nil
}

func TestCollectEC2(t *testing.T) {
	res, err := collectEC2(context.Background(), fakeEC2{}, "us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("terminated instance must be skipped, got %d", len(res))
	}
	r := res[0]
	if r.Name != "order-worker-03" || r.Status != model.StatusRunning || r.Zone != "us-east-1a" || r.Spec != "c6i.2xlarge" {
		t.Fatalf("unexpected resource: %+v", r)
	}
	if len(r.PrivateIPs) != 2 || len(r.PublicIPs) != 1 {
		t.Fatalf("ips: %v %v", r.PrivateIPs, r.PublicIPs)
	}
	if r.Extra["cpu"] != int32(8) || r.Extra["memory_mib"] != int64(16384) || r.Tags["env"] != "prod" {
		t.Fatalf("extra/tags: %+v %+v", r.Extra, r.Tags)
	}
	if res[1].Name != "i-stopped" || res[1].Status != model.StatusStopped {
		t.Fatalf("stopped instance: %+v", res[1])
	}
}

type fakeELB struct{}

func (fakeELB) DescribeLoadBalancers(ctx context.Context, in *elbv2.DescribeLoadBalancersInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error) {
	return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: []elbtypes.LoadBalancer{
		{
			LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/app/api-alb-prod/7c1e9d2a4b6f8031"),
			LoadBalancerName: aws.String("api-alb-prod"), Type: elbtypes.LoadBalancerTypeEnumApplication,
			Scheme: elbtypes.LoadBalancerSchemeEnumInternetFacing, DNSName: aws.String("api.elb.amazonaws.com"),
			State: &elbtypes.LoadBalancerState{Code: elbtypes.LoadBalancerStateEnumActive},
		},
		{
			LoadBalancerArn:  aws.String("arn:aws:elasticloadbalancing:us-east-1:123:loadbalancer/net/ingest/2f4a"),
			LoadBalancerName: aws.String("ingest"), Type: elbtypes.LoadBalancerTypeEnumNetwork,
			Scheme: elbtypes.LoadBalancerSchemeEnumInternal,
			State:  &elbtypes.LoadBalancerState{Code: elbtypes.LoadBalancerStateEnumProvisioning},
		},
	}}, nil
}

func (fakeELB) DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, _ ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error) {
	return &elbv2.DescribeTagsOutput{TagDescriptions: []elbtypes.TagDescription{
		{ResourceArn: aws.String(in.ResourceArns[0]), Tags: []elbtypes.Tag{{Key: aws.String("team"), Value: aws.String("trade")}}},
	}}, nil
}

func TestCollectELB(t *testing.T) {
	res, err := collectELB(context.Background(), fakeELB{}, "us-east-1")
	if err != nil {
		t.Fatal(err)
	}
	alb, nlb := res[0], res[1]
	if alb.ResourceID != "app/api-alb-prod/7c1e9d2a4b6f8031" || alb.Extra["lb_kind"] != cloud.LBKindALB || alb.Extra["network"] != "internet" || alb.Tags["team"] != "trade" {
		t.Fatalf("alb: %+v", alb)
	}
	if nlb.Status != model.StatusPending || nlb.Spec != "NLB" || nlb.Extra["network"] != "internal" {
		t.Fatalf("nlb: %+v", nlb)
	}
	if got := SupportedMetrics(cloud.ResourceRef{Type: model.TypeLB, Extra: nlb.Extra}); contains(got, cloud.MetricQPS) {
		t.Fatalf("NLB must not report QPS: %v", got)
	}
	if got := SupportedMetrics(cloud.ResourceRef{Type: model.TypeLB, Extra: alb.Extra}); !contains(got, cloud.MetricQPS) {
		t.Fatalf("ALB must report QPS: %v", got)
	}
}

func TestNormalizeRDSStatus(t *testing.T) {
	cases := map[string]string{
		"available": model.StatusRunning, "stopped": model.StatusStopped, "modifying": model.StatusChanging,
		"configuring-log-exports": model.StatusChanging, "storage-full": model.StatusFailed,
		"incompatible-network": model.StatusFailed, "weird": model.StatusUnknown,
	}
	for in, want := range cases {
		if got := normalizeRDSStatus(in); got != want {
			t.Errorf("%s => %s, want %s", in, got, want)
		}
	}
}

// fakeCW returns a constant value per metric name at two timestamps.
type fakeCW struct {
	values map[string]float64
	calls  int
}

func (f *fakeCW) GetMetricData(ctx context.Context, in *cloudwatch.GetMetricDataInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.GetMetricDataOutput, error) {
	f.calls++
	t0 := in.StartTime.Truncate(time.Hour)
	var results []cwtypes.MetricDataResult
	for _, q := range in.MetricDataQueries {
		name := aws.ToString(q.MetricStat.Metric.MetricName)
		v, ok := f.values[name]
		if !ok {
			results = append(results, cwtypes.MetricDataResult{Id: q.Id})
			continue
		}
		results = append(results, cwtypes.MetricDataResult{
			Id:         q.Id,
			Timestamps: []time.Time{t0, t0.Add(time.Hour)},
			Values:     []float64{v, v},
		})
	}
	return &cloudwatch.GetMetricDataOutput{MetricDataResults: results}, nil
}

func (f *fakeCW) ListMetrics(ctx context.Context, in *cloudwatch.ListMetricsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.ListMetricsOutput, error) {
	return &cloudwatch.ListMetricsOutput{Metrics: []cwtypes.Metric{{
		Namespace: in.Namespace, MetricName: in.MetricName,
		Dimensions: []cwtypes.Dimension{dim("InstanceId", "i-1"), dim("ImageId", "ami-1")},
	}}}, nil
}

func TestQueryMetricsConversions(t *testing.T) {
	cw := &fakeCW{values: map[string]float64{
		"CPUUtilization": 42, "NetworkIn": 300 * 1000, "EBSReadBytes": 600, "DiskReadBytes": 300,
		"mem_used_percent": 55, "FreeStorageSpace": 25 * 1024 * 1024 * 1024,
	}}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	vm := cloud.ResourceRef{Type: model.TypeVM, ResourceID: "i-1"}
	series, err := queryMetrics(context.Background(), cw, vm, cloud.MetricQuery{
		Keys:  []string{cloud.MetricCPU, cloud.MetricNetIn, cloud.MetricDiskRead, cloud.MetricMem},
		Start: now.Add(-time.Hour), End: now, Period: time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, s := range series {
		if len(s.Points) != 2 {
			t.Fatalf("%s: expected 2 points, got %d", s.Key, len(s.Points))
		}
		got[s.Key] = s.Points[0][1]
	}
	// EC2 uses a 300s minimum period: NetworkIn Sum 300000 bytes -> 8000 bit/s.
	if got[cloud.MetricNetIn] != 8000 {
		t.Errorf("net_in = %v", got[cloud.MetricNetIn])
	}
	// EBS + instance store bytes are summed then divided by the period.
	if got[cloud.MetricDiskRead] != 3 {
		t.Errorf("disk_read = %v", got[cloud.MetricDiskRead])
	}
	if got[cloud.MetricCPU] != 42 || got[cloud.MetricMem] != 55 {
		t.Errorf("cpu/mem = %v %v", got[cloud.MetricCPU], got[cloud.MetricMem])
	}

	rds := cloud.ResourceRef{Type: model.TypeRDS, ResourceID: "db-1", Extra: map[string]any{"allocated_storage_gib": float64(100)}}
	series, err = queryMetrics(context.Background(), cw, rds, cloud.MetricQuery{Keys: []string{cloud.MetricDiskUtil}, Start: now.Add(-time.Hour), End: now, Period: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if v := series[0].Points[0][1]; v != 75 {
		t.Errorf("disk_util = %v, want 75", v)
	}
	if contains(SupportedMetrics(cloud.ResourceRef{Type: model.TypeRDS}), cloud.MetricDiskUtil) {
		t.Error("disk_util needs allocated storage")
	}
}

func TestCPUSnapshot(t *testing.T) {
	cw := &fakeCW{values: map[string]float64{"CPUUtilization": 12.5}}
	now := time.Date(2026, 9, 28, 12, 30, 0, 0, time.UTC)
	stats, err := cpuSnapshot(context.Background(), cw, []string{"i-1", "i-2"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("stats: %+v", stats)
	}
	if v, ok := stats["i-1"].Average(); !ok || v != 12.5 {
		t.Fatalf("average = %v %v", v, ok)
	}
}

type apiErr struct{ code string }

func (e apiErr) Error() string                 { return e.code }
func (e apiErr) ErrorCode() string             { return e.code }
func (e apiErr) ErrorMessage() string          { return e.code }
func (e apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }

func TestClassify(t *testing.T) {
	if err := classify(apiErr{"InvalidClientTokenId"}); !errors.Is(err, cloud.ErrAuth) {
		t.Errorf("auth: %v", err)
	}
	if err := classify(apiErr{"UnauthorizedOperation"}); !errors.Is(err, cloud.ErrPermission) {
		t.Errorf("permission: %v", err)
	}
	if err := classify(apiErr{"OptInRequired"}); !errors.Is(err, cloud.ErrRegionUnsupported) {
		t.Errorf("region: %v", err)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
