package aliyun

import (
	"context"
	"crypto/sha256"
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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Provider struct{}

type resolvedCredentials struct {
	accessKeyID     string
	accessKeySecret string
	securityToken   string
	expiresAt       time.Time
}

var (
	roleCredentials   sync.Map
	roleCredentialsMu sync.Mutex
)

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
func baseConfig(accessKeyID, secret, token, region string) *client.Config {
	if region == "" {
		region = "cn-hangzhou"
	}
	cfg := new(client.Config).SetAccessKeyId(accessKeyID).SetAccessKeySecret(secret).SetRegionId(region)
	if token != "" {
		cfg.SetSecurityToken(token)
	}
	return cfg
}

func credentialsFor(a model.CloudAccount, secret, region string) (resolvedCredentials, error) {
	credentials := resolvedCredentials{accessKeyID: a.AccessKeyID, accessKeySecret: secret}
	if a.RoleARN == "" {
		return credentials, nil
	}
	cacheKey := fmt.Sprintf("%d:%s:%s:%x", a.ID, a.AccessKeyID, a.RoleARN, sha256.Sum256([]byte(secret)))
	if cached, ok := roleCredentials.Load(cacheKey); ok {
		value := cached.(resolvedCredentials)
		if time.Until(value.expiresAt) > 5*time.Minute {
			return value, nil
		}
	}
	roleCredentialsMu.Lock()
	defer roleCredentialsMu.Unlock()
	if cached, ok := roleCredentials.Load(cacheKey); ok {
		value := cached.(resolvedCredentials)
		if time.Until(value.expiresAt) > 5*time.Minute {
			return value, nil
		}
	}
	stsClient, err := sts.NewClient(baseConfig(a.AccessKeyID, secret, "", region))
	if err != nil {
		return credentials, err
	}
	response, err := stsClient.AssumeRoleWithOptions(
		new(sts.AssumeRoleRequest).SetRoleArn(a.RoleARN).SetRoleSessionName("yunshu-readonly").SetDurationSeconds(3600),
		&dara.RuntimeOptions{},
	)
	if err != nil {
		return credentials, err
	}
	if response.Body == nil || response.Body.Credentials == nil {
		return credentials, fmt.Errorf("AssumeRole 未返回临时凭证")
	}
	value := response.Body.Credentials
	credentials.accessKeyID = str(value.AccessKeyId)
	credentials.accessKeySecret = str(value.AccessKeySecret)
	credentials.securityToken = str(value.SecurityToken)
	credentials.expiresAt, _ = time.Parse(time.RFC3339, str(value.Expiration))
	if credentials.accessKeyID == "" || credentials.accessKeySecret == "" || credentials.securityToken == "" {
		return credentials, fmt.Errorf("AssumeRole 返回的临时凭证不完整")
	}
	roleCredentials.Store(cacheKey, credentials)
	return credentials, nil
}

func configFor(a model.CloudAccount, secret, region string) (*client.Config, error) {
	credentials, err := credentialsFor(a, secret, region)
	if err != nil {
		return nil, err
	}
	return baseConfig(credentials.accessKeyID, credentials.accessKeySecret, credentials.securityToken, region), nil
}
func state(v string) string {
	switch strings.ToLower(v) {
	case "running", "active", "normal", "available":
		return "running"
	case "stopped", "inactive":
		return "stopped"
	case "starting", "creating", "modifying", "activating", "restarting", "upgrading", "maintaining":
		return "starting"
	default:
		return "abnormal"
	}
}
func expiry(v string) *time.Time {
	if v == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}
func (Provider) Validate(ctx context.Context, a model.CloudAccount, secret string) (cloud.Identity, error) {
	cfg, err := configFor(a, secret, "cn-hangzhou")
	if err != nil {
		return cloud.Identity{}, err
	}
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
	cfg, err := configFor(a, secret, region)
	if err != nil {
		return nil, err
	}
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
					out = append(out, model.Resource{Provider: "aliyun", Type: "vm", Region: region, CloudID: str(v.InstanceId), Name: name, Status: state(str(v.Status)), IP: ip, Spec: str(v.InstanceType), VCPU: num(v.Cpu), MemoryGB: float64(num(v.Memory)) / 1024, Tags: raw(tv), Extra: raw(map[string]any{"os": str(v.OSName), "subnet": subnet, "security_groups": v.SecurityGroupIds, "internet_bandwidth_in_mbps": num(v.InternetMaxBandwidthIn), "internet_bandwidth_out_mbps": num(v.InternetMaxBandwidthOut)}), ExpiresAt: expiry(str(v.ExpiredTime))})
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
				vcpu, _ := strconv.Atoi(str(v.DBInstanceCPU))
				ex := map[string]any{"engine": str(v.Engine), "engine_version": str(v.EngineVersion), "vpc": str(v.VpcId), "vswitch": str(v.VSwitchId), "network_type": str(v.DBInstanceNetType), "pay_type": str(v.PayType)}
				tagValues := map[string]string{}
				expiresAt := expiry(str(v.ExpireTime))
				endpoint := ""
				attr, attrErr := cl.DescribeDBInstanceAttributeWithContext(ctx, new(rds.DescribeDBInstanceAttributeRequest).SetDBInstanceId(rid), runtime)
				if attrErr == nil && attr.Body != nil && attr.Body.Items != nil && len(attr.Body.Items.DBInstanceAttribute) > 0 {
					av := attr.Body.Items.DBInstanceAttribute[0]
					ex["storage_gb"] = num(av.DBInstanceStorage)
					ex["storage_type"] = str(av.DBInstanceStorageType)
					ex["zone"] = str(av.ZoneId)
					ex["max_connections"] = num(av.MaxConnections)
					endpoint = str(av.ConnectionString)
					ex["endpoint"] = endpoint
					ex["port"] = str(av.Port)
					if expiresAt == nil {
						expiresAt = expiry(str(av.ExpireTime))
					}
				} else if attrErr != nil {
					ex["attribute_error"] = attrErr.Error()
				}
				tagResponse, tagErr := cl.DescribeTagsWithContext(ctx, new(rds.DescribeTagsRequest).SetRegionId(region).SetResourceType("INSTANCE").SetDBInstanceId(rid), runtime)
				if tagErr == nil && tagResponse.Body != nil && tagResponse.Body.Items != nil {
					for _, tag := range tagResponse.Body.Items.TagInfos {
						tagValues[str(tag.TagKey)] = str(tag.TagValue)
					}
				} else if tagErr != nil {
					ex["tag_error"] = tagErr.Error()
				}
				out = append(out, model.Resource{Provider: "aliyun", Type: "rds", Region: region, CloudID: rid, Name: name, Status: state(str(v.DBInstanceStatus)), IP: endpoint, Spec: str(v.DBInstanceClass), VCPU: vcpu, MemoryGB: float64(num(v.DBInstanceMemory)) / 1024, Tags: raw(tagValues), Extra: raw(ex), ExpiresAt: expiresAt})
			}
			if int(page)*100 >= num(resp.Body.TotalRecordCount) {
				break
			}
		}
	case "clb":
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
				id := str(v.LoadBalancerId)
				name := str(v.LoadBalancerName)
				if name == "" {
					name = id
				}
				tags := map[string]string{}
				if v.Tags != nil {
					for _, tag := range v.Tags.Tag {
						tags[str(tag.TagKey)] = str(tag.TagValue)
					}
				}
				out = append(out, model.Resource{Provider: "aliyun", Type: "lb", Region: region, CloudID: id, Name: name, Status: state(str(v.LoadBalancerStatus)), IP: str(v.Address), Spec: "CLB", Tags: raw(tags), Extra: raw(map[string]any{"spec": str(v.LoadBalancerSpec), "bandwidth": num(v.Bandwidth), "vpc": str(v.VpcId), "vswitch": str(v.VSwitchId)})})
			}
			if int(page)*100 >= num(resp.Body.TotalCount) {
				break
			}
		}
	case "alb":
		ac, err := alb.NewClient(cfg)
		if err != nil {
			return nil, err
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
				id := str(v.LoadBalancerId)
				name := str(v.LoadBalancerName)
				if name == "" {
					name = id
				}
				tags := map[string]string{}
				for _, tag := range v.Tags {
					tags[str(tag.Key)] = str(tag.Value)
				}
				out = append(out, model.Resource{Provider: "aliyun", Type: "lb", Region: region, CloudID: id, Name: name, Status: state(str(v.LoadBalancerStatus)), Spec: "ALB", Tags: raw(tags), Extra: raw(map[string]any{"dns": str(v.DNSName), "edition": str(v.LoadBalancerEdition), "vpc": str(v.VpcId)})})
			}
			token = str(resp.Body.NextToken)
			if token == "" {
				break
			}
		}
	case "oss":
		resolved, err := credentialsFor(a, secret, region)
		if err != nil {
			return nil, err
		}
		oc := oss.NewClient(oss.LoadDefaultConfig().WithRegion(region).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(resolved.accessKeyID, resolved.accessKeySecret, resolved.securityToken)))
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
				name := str(v.Name)
				bucketRegion := strings.TrimPrefix(str(v.Region), "oss-")
				if bucketRegion == "" {
					bucketRegion = strings.TrimPrefix(str(v.Location), "oss-")
				}
				if bucketRegion == "" {
					bucketRegion = region
				}
				bucketClient := oss.NewClient(oss.LoadDefaultConfig().WithRegion(bucketRegion).WithCredentialsProvider(credentials.NewStaticCredentialsProvider(resolved.accessKeyID, resolved.accessKeySecret, resolved.securityToken)))
				metadata := map[string]any{"location": str(v.Location), "storage_class": str(v.StorageClass), "created_at": v.CreationDate}
				stat, statErr := bucketClient.GetBucketStat(ctx, &oss.GetBucketStatRequest{Bucket: &name})
				if statErr == nil {
					metadata["capacity_gb"] = float64(stat.Storage) / (1024 * 1024 * 1024)
					metadata["object_count"] = stat.ObjectCount
					metadata["multipart_upload_count"] = stat.MultipartUploadCount
					metadata["stats_updated_at"] = time.Unix(stat.LastModifiedTime, 0)
				} else {
					metadata["stats_error"] = statErr.Error()
				}
				tagValues := map[string]string{}
				tagResponse, tagErr := bucketClient.GetBucketTags(ctx, &oss.GetBucketTagsRequest{Bucket: &name})
				if tagErr == nil && tagResponse.Tagging != nil && tagResponse.Tagging.TagSet != nil {
					for _, tag := range tagResponse.Tagging.TagSet.Tags {
						tagValues[str(tag.Key)] = str(tag.Value)
					}
				} else if tagErr != nil {
					metadata["tag_error"] = tagErr.Error()
				}
				out = append(out, model.Resource{Provider: "aliyun", Type: "oss", Region: bucketRegion, CloudID: name, Name: name, Status: "running", Spec: str(v.StorageClass), Tags: raw(tagValues), Extra: raw(metadata)})
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
	series := cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: "aliyun", Supported: cloud.Supports(r, metric), Points: []cloud.Point{}}
	if !series.Supported {
		return series, nil
	}
	namespace, name, dimension, unit := aliyunMetric(r, metric)
	if name == "" {
		series.Supported = false
		return series, nil
	}
	series.Unit = unit
	cfg, err := configFor(a, secret, r.Region)
	if err != nil {
		return series, err
	}
	client, err := cms.NewClient(cfg)
	if err != nil {
		return series, err
	}
	now := time.Now()
	dimensionValue := map[string]string{dimension: r.CloudID}
	if a.UID != "" {
		dimensionValue["userId"] = a.UID
	}
	dimensionsJSON, _ := json.Marshal([]map[string]string{dimensionValue})
	period := "300"
	if span > 24*time.Hour || r.Type == "oss" {
		period = "3600"
	}
	request := new(cms.DescribeMetricListRequest).
		SetNamespace(namespace).
		SetMetricName(name).
		SetDimensions(string(dimensionsJSON)).
		SetStartTime(strconv.FormatInt(now.Add(-span).UnixMilli(), 10)).
		SetEndTime(strconv.FormatInt(now.UnixMilli(), 10)).
		SetPeriod(period)
	response, err := client.DescribeMetricListWithContext(ctx, request, &dara.RuntimeOptions{})
	if err != nil {
		return series, err
	}
	if response.Body == nil || str(response.Body.Datapoints) == "" {
		return series, nil
	}
	var rawPoints []map[string]any
	if err := json.Unmarshal([]byte(str(response.Body.Datapoints)), &rawPoints); err != nil {
		return series, fmt.Errorf("解析云监控数据: %w", err)
	}
	for _, point := range rawPoints {
		timestamp, ok := metricNumber(point, "timestamp", "Timestamp")
		if !ok {
			continue
		}
		value, ok := metricNumber(point, "Average", "Value", "Maximum", "Minimum")
		if !ok {
			continue
		}
		if (r.Type == "vm" || r.Type == "lb") && (metric == "network_in" || metric == "network_out") {
			value /= 8 // CloudMonitor reports network bandwidth in bit/s.
		}
		if r.Type == "oss" && metric == "storage" {
			value /= 1024 * 1024 * 1024
		}
		series.Points = append(series.Points, cloud.Point{Time: time.UnixMilli(int64(timestamp)), Value: value})
		series.Average += value
		if value > series.Max {
			series.Max = value
		}
	}
	sort.Slice(series.Points, func(i, j int) bool { return series.Points[i].Time.Before(series.Points[j].Time) })
	if len(series.Points) > 0 {
		series.Average /= float64(len(series.Points))
		series.Current = series.Points[len(series.Points)-1].Value
	}
	return series, nil
}

func aliyunMetric(resource model.Resource, metric string) (namespace, name, dimension, unit string) {
	unit = "%"
	switch resource.Type {
	case "vm":
		namespace, dimension = "acs_ecs_dashboard", "instanceId"
		switch metric {
		case "cpu":
			name = "CPUUtilization"
		case "memory":
			name = "memory_usedutilization"
		case "network_in":
			name, unit = "InternetInRate", "Byte/s"
		case "network_out":
			name, unit = "InternetOutRate", "Byte/s"
		}
	case "rds":
		namespace, dimension = "acs_rds_dashboard", "instanceId"
		switch metric {
		case "cpu":
			name = "CpuUsage"
		case "memory":
			name = "MemoryUsage"
		case "connections":
			name, unit = "ConnectionUsage", "%"
		case "storage":
			name, unit = "DiskUsage", "%"
		}
	case "lb":
		dimension = "loadBalancerId"
		if resource.Spec == "CLB" {
			namespace = "acs_slb_dashboard"
			switch metric {
			case "connections":
				name, unit = "InstanceActiveConnection", "个"
			case "network_in":
				name, unit = "InstanceTrafficRX", "Byte/s"
			case "network_out":
				name, unit = "InstanceTrafficTX", "Byte/s"
			}
		} else {
			namespace = "acs_alb"
			switch metric {
			case "connections":
				name, unit = "LoadBalancerActiveConnection", "个"
			case "qps":
				name, unit = "LoadBalancerQPS", "次/s"
			case "network_in":
				name, unit = "LoadBalancerInBits", "Byte/s"
			case "network_out":
				name, unit = "LoadBalancerOutBits", "Byte/s"
			}
		}
	case "oss":
		namespace, dimension = "acs_oss_dashboard", "BucketName"
		if metric == "storage" {
			name, unit = "MeteringStorageUtilization", "GB"
		}
	}
	return namespace, name, dimension, unit
}

func metricNumber(point map[string]any, keys ...string) (float64, bool) {
	for _, key := range keys {
		value, exists := point[key]
		if !exists {
			continue
		}
		switch number := value.(type) {
		case float64:
			return number, true
		case string:
			parsed, err := strconv.ParseFloat(number, 64)
			if err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}
