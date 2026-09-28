package aws

import (
	"context"
	"encoding/json"
	"fmt"
	base "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	ct "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"sort"
	"strings"
	"time"
)

type Provider struct{}

func cfg(ctx context.Context, a model.CloudAccount, secret, region string) (base.Config, error) {
	if region == "" {
		region = "us-east-1"
	}
	c, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(a.AccessKeyID, secret, "")))
	if err != nil {
		return c, err
	}
	if a.RoleARN != "" {
		c.Credentials = base.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(c), a.RoleARN))
	}
	return c, nil
}
func (Provider) Validate(ctx context.Context, a model.CloudAccount, secret string) (cloud.Identity, error) {
	region := "us-east-1"
	if a.Partition == "china" {
		region = "cn-north-1"
	}
	c, err := cfg(ctx, a, secret, region)
	if err != nil {
		return cloud.Identity{}, err
	}
	who, err := sts.NewFromConfig(c).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return cloud.Identity{}, err
	}
	out, err := ec2.NewFromConfig(c).DescribeRegions(ctx, &ec2.DescribeRegionsInput{})
	if err != nil {
		return cloud.Identity{}, err
	}
	regions := []string{}
	for _, r := range out.Regions {
		regions = append(regions, base.ToString(r.RegionName))
	}
	return cloud.Identity{UID: base.ToString(who.Account), Regions: regions}, nil
}
func status(v string) string {
	switch strings.ToLower(v) {
	case "running", "available", "active":
		return "running"
	case "stopped", "stopping":
		return "stopped"
	case "pending", "starting", "modifying", "creating":
		return "starting"
	default:
		return "abnormal"
	}
}
func tags(v map[string]string) string { b, _ := json.Marshal(v); return string(b) }
func extra(v map[string]any) string   { b, _ := json.Marshal(v); return string(b) }
func (Provider) List(ctx context.Context, a model.CloudAccount, secret, region, typ string) ([]model.Resource, error) {
	c, err := cfg(ctx, a, secret, region)
	if err != nil {
		return nil, err
	}
	out := []model.Resource{}
	switch typ {
	case "vm":
		client := ec2.NewFromConfig(c)
		p := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, reservation := range page.Reservations {
				for _, v := range reservation.Instances {
					name := ""
					tv := map[string]string{}
					for _, tag := range v.Tags {
						tv[base.ToString(tag.Key)] = base.ToString(tag.Value)
					}
					name = tv["Name"]
					if name == "" {
						name = base.ToString(v.InstanceId)
					}
					groups := []string{}
					for _, g := range v.SecurityGroups {
						groups = append(groups, base.ToString(g.GroupId))
					}
					cpu := 0
					if v.CpuOptions != nil {
						cpu = int(base.ToInt32(v.CpuOptions.CoreCount)) * int(base.ToInt32(v.CpuOptions.ThreadsPerCore))
					}
					state := ""
					if v.State != nil {
						state = string(v.State.Name)
					}
					r := model.Resource{Provider: "aws", Type: "vm", Region: region, CloudID: base.ToString(v.InstanceId), Name: name, Status: status(state), IP: base.ToString(v.PrivateIpAddress), Spec: string(v.InstanceType), VCPU: cpu, Tags: tags(tv), Extra: extra(map[string]any{"os": base.ToString(v.PlatformDetails), "subnet": base.ToString(v.SubnetId), "security_groups": groups})}
					out = append(out, r)
				}
			}
		}
		types := []ec2types.InstanceType{}
		seen := map[ec2types.InstanceType]bool{}
		for _, r := range out {
			typ := ec2types.InstanceType(r.Spec)
			if !seen[typ] {
				seen[typ] = true
				types = append(types, typ)
			}
		}
		info := map[string]struct {
			cpu    int
			memory float64
		}{}
		for start := 0; start < len(types); start += 100 {
			end := min(start+100, len(types))
			resp, err := client.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{InstanceTypes: types[start:end]})
			if err != nil {
				return nil, err
			}
			for _, v := range resp.InstanceTypes {
				size := struct {
					cpu    int
					memory float64
				}{}
				if v.VCpuInfo != nil {
					size.cpu = int(base.ToInt32(v.VCpuInfo.DefaultVCpus))
				}
				if v.MemoryInfo != nil {
					size.memory = float64(base.ToInt64(v.MemoryInfo.SizeInMiB)) / 1024
				}
				info[string(v.InstanceType)] = size
			}
		}
		for i := range out {
			if size, ok := info[out[i].Spec]; ok {
				out[i].VCPU = size.cpu
				out[i].MemoryGB = size.memory
			}
		}
	case "rds":
		client := rds.NewFromConfig(c)
		p := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, v := range page.DBInstances {
				id := base.ToString(v.DBInstanceIdentifier)
				out = append(out, model.Resource{Provider: "aws", Type: "rds", Region: region, CloudID: id, Name: id, Status: status(base.ToString(v.DBInstanceStatus)), Spec: base.ToString(v.DBInstanceClass), Tags: "{}", Extra: extra(map[string]any{"engine": base.ToString(v.Engine), "engine_version": base.ToString(v.EngineVersion), "storage_gb": base.ToInt32(v.AllocatedStorage), "endpoint": v.Endpoint})})
			}
		}
	case "lb":
		client := elasticloadbalancingv2.NewFromConfig(c)
		p := elasticloadbalancingv2.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
		for p.HasMorePages() {
			page, err := p.NextPage(ctx)
			if err != nil {
				return nil, err
			}
			for _, v := range page.LoadBalancers {
				state := ""
				if v.State != nil {
					state = string(v.State.Code)
				}
				out = append(out, model.Resource{Provider: "aws", Type: "lb", Region: region, CloudID: base.ToString(v.LoadBalancerArn), Name: base.ToString(v.LoadBalancerName), Status: status(state), Spec: strings.ToUpper(string(v.Type)), Tags: "{}", Extra: extra(map[string]any{"dns": base.ToString(v.DNSName), "scheme": v.Scheme, "vpc": base.ToString(v.VpcId)})})
			}
		}
	case "oss":
		client := s3.NewFromConfig(c)
		page, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
		if err != nil {
			return nil, err
		}
		for _, v := range page.Buckets {
			name := base.ToString(v.Name)
			out = append(out, model.Resource{Provider: "aws", Type: "oss", Region: "global", CloudID: name, Name: name, Status: "running", Spec: "S3", Tags: "{}", Extra: extra(map[string]any{"created_at": v.CreationDate})})
		}
	}
	return out, nil
}
func (Provider) Metrics(ctx context.Context, a model.CloudAccount, secret string, r model.Resource, metric string, span time.Duration) (cloud.Series, error) {
	s := cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: r.Provider, Supported: cloud.Supports(r, metric), Points: []cloud.Point{}}
	if !s.Supported {
		return s, nil
	}
	namespace, name, dimension := "AWS/EC2", "CPUUtilization", "InstanceId"
	s.Unit = "%"
	switch r.Type {
	case "rds":
		namespace = "AWS/RDS"
		dimension = "DBInstanceIdentifier"
	case "lb":
		namespace = "AWS/ApplicationELB"
		if r.Spec == "NLB" {
			namespace = "AWS/NetworkELB"
		}
		dimension = "LoadBalancer"
	case "oss":
		namespace = "AWS/S3"
		dimension = "BucketName"
	}
	switch metric {
	case "network_in":
		name = "NetworkIn"
		s.Unit = "Byte/s"
	case "network_out":
		name = "NetworkOut"
		s.Unit = "Byte/s"
	case "connections":
		name = "DatabaseConnections"
		if r.Type == "lb" {
			name = "ActiveConnectionCount"
			if r.Spec == "NLB" {
				name = "ActiveFlowCount"
			}
		}
		s.Unit = "个"
	case "qps":
		name = "RequestCount"
		s.Unit = "次/s"
	case "storage":
		name = "FreeStorageSpace"
		if r.Type == "oss" {
			name = "BucketSizeBytes"
		}
		s.Unit = "GB"
	case "memory":
		s.Supported = false
		return s, nil
	}
	c, err := cfg(ctx, a, secret, r.Region)
	if err != nil {
		return s, err
	}
	period := int32(300)
	if span > 24*time.Hour {
		period = 3600
	}
	end := time.Now()
	start := end.Add(-span)
	v := r.CloudID
	if r.Type == "lb" {
		if i := strings.Index(v, "loadbalancer/"); i >= 0 {
			v = v[i+len("loadbalancer/"):]
		}
	}
	dimensions := []ct.Dimension{{Name: base.String(dimension), Value: base.String(v)}}
	if r.Type == "oss" {
		dimensions = append(dimensions, ct.Dimension{Name: base.String("StorageType"), Value: base.String("StandardStorage")})
		period = 86400
	}
	resp, err := cloudwatch.NewFromConfig(c).GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{Namespace: base.String(namespace), MetricName: base.String(name), Dimensions: dimensions, StartTime: &start, EndTime: &end, Period: &period, Statistics: []ct.Statistic{ct.StatisticAverage}})
	if err != nil {
		return s, err
	}
	for _, p := range resp.Datapoints {
		value := base.ToFloat64(p.Average)
		if s.Unit == "Byte/s" || s.Unit == "次/s" {
			value /= float64(period)
		}
		if s.Unit == "GB" {
			value /= 1024 * 1024 * 1024
		}
		s.Points = append(s.Points, cloud.Point{Time: *p.Timestamp, Value: value})
		s.Average += value
		if value > s.Max {
			s.Max = value
		}
	}
	sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].Time.Before(s.Points[j].Time) })
	if len(s.Points) > 0 {
		s.Average /= float64(len(s.Points))
		s.Current = s.Points[len(s.Points)-1].Value
	}
	return s, nil
}

var _ = fmt.Sprint
