package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	alb "github.com/alibabacloud-go/alb-20200616/v2/client"
	cms "github.com/alibabacloud-go/cms-20190101/v10/client"
	"github.com/alibabacloud-go/darabonba-openapi/v2/client"
	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	rds "github.com/alibabacloud-go/rds-20140815/v16/client"
	slb "github.com/alibabacloud-go/slb-20140515/v4/client"
	sts "github.com/alibabacloud-go/sts-20150401/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"
	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"strconv"
	"strings"
	"time"
)

type Provider struct{}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
func num(p *int32) int {
	if p == nil {
		return 0
	}
	return int(*p)
}
func raw(v any) string { b, _ := json.Marshal(v); return string(b) }
func configFor(a model.CloudAccount, secret, region string) *client.Config {
	if region == "" {
		region = "cn-hangzhou"
	}
	return new(client.Config).SetAccessKeyId(a.AccessKeyID).SetAccessKeySecret(secret).SetRegionId(region)
}
func state(v string) string {
	switch strings.ToLower(v) {
	case "running", "active", "normal", "available":
		return "running"
	case "stopped", "inactive":
		return "stopped"
	case "starting", "creating", "modifying":
		return "starting"
	default:
		return "abnormal"
	}
}
func expiry(v string) *time.Time {
	if v == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05Z", "2006-01-02T15:04:05+08:00", "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}
func (Provider) Validate(ctx context.Context, a model.CloudAccount, secret string) (cloud.Identity, error) {
	cfg := configFor(a, secret, "cn-hangzhou")
	sc, err := sts.NewClient(cfg)
	if err != nil {
		return cloud.Identity{}, err
	}
	identity, err := sc.GetCallerIdentity()
	if err != nil {
		return cloud.Identity{}, err
	}
	ec, err := ecs.NewClient(cfg)
	if err != nil {
		return cloud.Identity{}, err
	}
	regions, err := ec.DescribeRegionsWithContext(ctx, &ecs.DescribeRegionsRequest{}, &dara.RuntimeOptions{})
	if err != nil {
		return cloud.Identity{}, err
	}
	out := []string{}
	if regions.Body != nil && regions.Body.Regions != nil {
		for _, r := range regions.Body.Regions.Region {
			out = append(out, str(r.RegionId))
		}
	}
	return cloud.Identity{UID: str(identity.Body.AccountId), Regions: out}, nil
}
func (Provider) List(ctx context.Context, a model.CloudAccount, secret, region, typ string) ([]model.Resource, error) {
	cfg := configFor(a, secret, region)
	out := []model.Resource{}
	runtime := &dara.RuntimeOptions{}
	switch typ {
	case "vm":
		cl, err := ecs.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		token := ""
		for {
			req := new(ecs.DescribeInstancesRequest).SetRegionId(region).SetMaxResults(100)
			if token != "" {
				req.SetNextToken(token)
			}
			resp, err := cl.DescribeInstancesWithContext(ctx, req, runtime)
			if err != nil {
				return nil, err
			}
			if resp.Body != nil && resp.Body.Instances != nil {
				for _, v := range resp.Body.Instances.Instance {
					tv := map[string]string{}
					if v.Tags != nil {
						for _, t := range v.Tags.Tag {
							tv[str(t.TagKey)] = str(t.TagValue)
						}
					}
					ip := ""
					subnet := ""
					if v.VpcAttributes != nil {
						subnet = str(v.VpcAttributes.VSwitchId)
						if v.VpcAttributes.PrivateIpAddress != nil && len(v.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
							ip = str(v.VpcAttributes.PrivateIpAddress.IpAddress[0])
						}
					}
					name := str(v.InstanceName)
					if name == "" {
						name = str(v.InstanceId)
					}
					out = append(out, model.Resource{Provider: "aliyun", Type: "vm", Region: region, CloudID: str(v.InstanceId), Name: name, Status: state(str(v.Status)), IP: ip, Spec: str(v.InstanceType), VCPU: num(v.Cpu), MemoryGB: float64(num(v.Memory)) / 1024, Tags: raw(tv), Extra: raw(map[string]any{"os": str(v.OSName), "subnet": subnet, "security_groups": v.SecurityGroupIds}), ExpiresAt: expiry(str(v.ExpiredTime))})
				}
			}
			token = str(resp.Body.NextToken)
			if token == "" {
				break
			}
		}
	case "rds":
		cl, err := rds.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		for page := int32(1); ; page++ {
			resp, err := cl.DescribeDBInstancesWithContext(ctx, new(rds.DescribeDBInstancesRequest).SetRegionId(region).SetPageNumber(page).SetPageSize(100), runtime)
			if err != nil {
				return nil, err
			}
			if resp.Body == nil || resp.Body.Items == nil {
				break
			}
			for _, v := range resp.Body.Items.DBInstance {
				rid := str(v.DBInstanceId)
				name := str(v.DBInstanceDescription)
				if name == "" {
					name = rid
				}
				ex := map[string]any{"engine": str(v.Engine), "engine_version": str(v.EngineVersion)}
				attr, attrErr := cl.DescribeDBInstanceAttributeWithContext(ctx, new(rds.DescribeDBInstanceAttributeRequest).SetDBInstanceId(rid), runtime)
				if attrErr == nil && attr.Body != nil && attr.Body.Items != nil && len(attr.Body.Items.DBInstanceAttribute) > 0 {
					av := attr.Body.Items.DBInstanceAttribute[0]
					ex["storage_gb"] = num(av.DBInstanceStorage)
				}
				out = append(out, model.Resource{Provider: "aliyun", Type: "rds", Region: region, CloudID: rid, Name: name, Status: state(str(v.DBInstanceStatus)), Spec: str(v.DBInstanceClass), MemoryGB: float64(num(v.DBInstanceMemory)) / 1024, Tags: "{}", Extra: raw(ex), ExpiresAt: expiry(str(v.ExpireTime))})
			}
			if int(page)*100 >= num(resp.Body.TotalRecordCount) {
				break
			}
		}
	case "lb":
		cl, err := slb.NewClient(cfg)
		if err != nil {
			return nil, err
		}
		for page := int32(1); ; page++ {
			resp, err := cl.DescribeLoadBalancersWithContext(ctx, new(slb.DescribeLoadBalancersRequest).SetRegionId(region).SetPageNumber(page).SetPageSize(100), runtime)
			if err != nil {
				return nil, err
			}
			if resp.Body == nil || resp.Body.LoadBalancers == nil {
				break
			}
			for _, v := range resp.Body.LoadBalancers.LoadBalancer {
				out = append(out, model.Resource{Provider: "aliyun", Type: "lb", Region: region, CloudID: str(v.LoadBalancerId), Name: str(v.LoadBalancerName), Status: state(str(v.LoadBalancerStatus)), IP: str(v.Address), Spec: "CLB", Tags: "{}", Extra: raw(map[string]any{"spec": str(v.LoadBalancerSpec), "bandwidth": num(v.Bandwidth), "vpc": str(v.VpcId), "vswitch": str(v.VSwitchId)})})
			}
			if int(page)*100 >= num(resp.Body.TotalCount) {
				break
			}
		}
		ac, err := alb.NewClient(cfg)
		if err != nil {
			return out, err
		}
		token := ""
		for {
			req := new(alb.ListLoadBalancersRequest).SetMaxResults(100)
			if token != "" {
				req.SetNextToken(token)
			}
			resp, err := ac.ListLoadBalancersWithContext(ctx, req, runtime)
			if err != nil {
				return out, err
			}
			if resp.Body == nil {
				break
			}
			for _, v := range resp.Body.LoadBalancers {
				out = append(out, model.Resource{Provider: "aliyun", Type: "lb", Region: region, CloudID: str(v.LoadBalancerId), Name: str(v.LoadBalancerName), Status: state(str(v.LoadBalancerStatus)), Spec: "ALB", Tags: "{}", Extra: raw(map[string]any{"dns": str(v.DNSName), "edition": str(v.LoadBalancerEdition), "vpc": str(v.VpcId)})})
			}
			token = str(resp.Body.NextToken)
			if token == "" {
				break
			}
		}
	case "oss":
		oc := oss.NewClient(oss.LoadDefaultConfig().WithRegion(region).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(a.AccessKeyID, secret)))
		marker := ""
		for {
			req := &oss.ListBucketsRequest{MaxKeys: 100}
			if marker != "" {
				req.Marker = &marker
			}
			resp, err := oc.ListBuckets(ctx, req)
			if err != nil {
				return nil, err
			}
			for _, v := range resp.Buckets {
				out = append(out, model.Resource{Provider: "aliyun", Type: "oss", Region: str(v.Region), CloudID: str(v.Name), Name: str(v.Name), Status: "running", Spec: str(v.StorageClass), Tags: "{}", Extra: raw(map[string]any{"location": str(v.Location), "storage_class": str(v.StorageClass)})})
			}
			if !resp.IsTruncated || resp.NextMarker == nil {
				break
			}
			marker = *resp.NextMarker
		}
	}
	return out, nil
}
func (Provider) Metrics(ctx context.Context, a model.CloudAccount, secret string, r model.Resource, metric string, span time.Duration) (cloud.Series, error) {
	s := cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: "aliyun", Supported: cloud.Supports(r, metric), Unit: "%", Points: []cloud.Point{}}
	if !s.Supported {
		return s, nil
	}
	ns, name, dim := "acs_ecs_dashboard", "CPUUtilization", "instanceId"
	switch r.Type {
	case "rds":
		ns = "acs_rds_dashboard"
		dim = "instanceId"
	case "lb":
		ns = "acs_alb"
		dim = "loadBalancerId"
	case "oss":
		ns = "acs_oss"
		dim = "bucketName"
	}
	switch metric {
	case "memory":
		name = "memory_usedutilization"
	case "network_in":
		name = "InternetInRate"
		s.Unit = "Byte/s"
	case "network_out":
		name = "InternetOutRate"
		s.Unit = "Byte/s"
	case "connections":
		name = "ConnectionUtilization"
		s.Unit = "个"
	case "qps":
		name = "QPS"
		s.Unit = "次/s"
	case "storage":
		name = "diskusage_utilization"
		s.Unit = "GB"
	}
	cl, err := cms.NewClient(configFor(a, secret, r.Region))
	if err != nil {
		return s, err
	}
	now := time.Now()
	dimensions := fmt.Sprintf(`[{"%s":"%s"}]`, dim, r.CloudID)
	req := new(cms.DescribeMetricListRequest).SetNamespace(ns).SetMetricName(name).SetDimensions(dimensions).SetStartTime(strconv.FormatInt(now.Add(-span).UnixMilli(), 10)).SetEndTime(strconv.FormatInt(now.UnixMilli(), 10)).SetPeriod("300")
	resp, err := cl.DescribeMetricListWithContext(ctx, req, &dara.RuntimeOptions{})
	if err != nil {
		return s, err
	}
	if resp.Body == nil {
		return s, nil
	}
	var points []map[string]any
	json.Unmarshal([]byte(str(resp.Body.Datapoints)), &points)
	for _, p := range points {
		timestamp, _ := p["timestamp"].(float64)
		value, _ := p["Average"].(float64)
		if value == 0 {
			value, _ = p["Value"].(float64)
		}
		pt := cloud.Point{Time: time.UnixMilli(int64(timestamp)), Value: value}
		s.Points = append(s.Points, pt)
		s.Average += value
		if value > s.Max {
			s.Max = value
		}
	}
	if len(s.Points) > 0 {
		s.Average /= float64(len(s.Points))
		s.Current = s.Points[len(s.Points)-1].Value
	}
	return s, nil
}
