package aws

import (
	"context"
	"crypto/sha256"
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
	"sync"
	"time"
)

type Provider struct{}

var roleProviders sync.Map

func cfg(ctx context.Context, a model.CloudAccount, secret, region string) (base.Config, error) {
	if region == "" {
		region = "us-east-1"
	}
	c, err := config.LoadDefaultConfig(ctx, config.WithRegion(region), config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(a.AccessKeyID, secret, "")))
	if err != nil {
		return c, err
	}
	if a.RoleARN != "" {
		key := fmt.Sprintf("%d:%s:%s:%x", a.ID, a.AccessKeyID, a.RoleARN, sha256.Sum256([]byte(secret)))
		candidate := base.NewCredentialsCache(stscreds.NewAssumeRoleProvider(sts.NewFromConfig(c), a.RoleARN, func(options *stscreds.AssumeRoleOptions) {
			options.RoleSessionName = "yunshu-readonly"
		}))
		provider, _ := roleProviders.LoadOrStore(key, candidate)
		c.Credentials = provider.(base.CredentialsProvider)
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
	case "pending", "starting", "modifying", "creating", "provisioning", "rebooting", "backing-up", "storage-optimization", "upgrading", "renaming", "resetting-master-credentials":
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
			cpu     int
			memory  float64
			network string
		}{}
		for start := 0; start < len(types); start += 100 {
			end := min(start+100, len(types))
			resp, err := client.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{InstanceTypes: types[start:end]})
			if err != nil {
				return nil, err
			}
			for _, v := range resp.InstanceTypes {
				size := struct {
					cpu     int
					memory  float64
					network string
				}{}
				if v.VCpuInfo != nil {
					size.cpu = int(base.ToInt32(v.VCpuInfo.DefaultVCpus))
				}
				if v.MemoryInfo != nil {
					size.memory = float64(base.ToInt64(v.MemoryInfo.SizeInMiB)) / 1024
				}
				if v.NetworkInfo != nil {
					size.network = base.ToString(v.NetworkInfo.NetworkPerformance)
				}
				info[string(v.InstanceType)] = size
			}
		}
		for i := range out {
			if size, ok := info[out[i].Spec]; ok {
				out[i].VCPU = size.cpu
				out[i].MemoryGB = size.memory
				metadata := map[string]any{}
				_ = json.Unmarshal([]byte(out[i].Extra), &metadata)
				metadata["network_performance"] = size.network
				out[i].Extra = extra(metadata)
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
				tagValues := map[string]string{}
				metadata := map[string]any{
					"engine": base.ToString(v.Engine), "engine_version": base.ToString(v.EngineVersion),
					"storage_gb": base.ToInt32(v.AllocatedStorage), "storage_type": base.ToString(v.StorageType),
					"availability_zone": base.ToString(v.AvailabilityZone), "multi_az": base.ToBool(v.MultiAZ),
					"publicly_accessible": base.ToBool(v.PubliclyAccessible), "created_at": v.InstanceCreateTime,
				}
				endpoint := ""
				if v.Endpoint != nil {
					endpoint = base.ToString(v.Endpoint.Address)
					metadata["endpoint"] = endpoint
					metadata["port"] = base.ToInt32(v.Endpoint.Port)
				}
				if v.DBSubnetGroup != nil {
					metadata["vpc"] = base.ToString(v.DBSubnetGroup.VpcId)
					metadata["subnet_group"] = base.ToString(v.DBSubnetGroup.DBSubnetGroupName)
				}
				securityGroups := make([]string, 0, len(v.VpcSecurityGroups))
				for _, group := range v.VpcSecurityGroups {
					securityGroups = append(securityGroups, base.ToString(group.VpcSecurityGroupId))
				}
				metadata["security_groups"] = securityGroups
				if arn := base.ToString(v.DBInstanceArn); arn != "" {
					metadata["arn"] = arn
					tagOutput, tagErr := client.ListTagsForResource(ctx, &rds.ListTagsForResourceInput{ResourceName: base.String(arn)})
					if tagErr != nil {
						metadata["tag_error"] = tagErr.Error()
					} else {
						for _, tag := range tagOutput.TagList {
							tagValues[base.ToString(tag.Key)] = base.ToString(tag.Value)
						}
					}
				}
				name := tagValues["Name"]
				if name == "" {
					name = id
				}
				out = append(out, model.Resource{Provider: "aws", Type: "rds", Region: region, CloudID: id, Name: name, Status: status(base.ToString(v.DBInstanceStatus)), IP: endpoint, Spec: base.ToString(v.DBInstanceClass), Tags: tags(tagValues), Extra: extra(metadata)})
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
				spec := strings.ToUpper(string(v.Type))
				switch spec {
				case "APPLICATION":
					spec = "ALB"
				case "NETWORK":
					spec = "NLB"
				case "GATEWAY":
					spec = "GWLB"
				}
				subnets := make([]string, 0, len(v.AvailabilityZones))
				zones := make([]string, 0, len(v.AvailabilityZones))
				for _, zone := range v.AvailabilityZones {
					subnets = append(subnets, base.ToString(zone.SubnetId))
					zones = append(zones, base.ToString(zone.ZoneName))
				}
				metadata := map[string]any{
					"arn": base.ToString(v.LoadBalancerArn), "dns": base.ToString(v.DNSName), "scheme": v.Scheme,
					"vpc": base.ToString(v.VpcId), "subnets": subnets, "availability_zones": zones,
					"security_groups": v.SecurityGroups, "ip_address_type": v.IpAddressType, "created_at": v.CreatedTime,
				}
				out = append(out, model.Resource{Provider: "aws", Type: "lb", Region: region, CloudID: base.ToString(v.LoadBalancerArn), Name: base.ToString(v.LoadBalancerName), Status: status(state), Spec: spec, Tags: "{}", Extra: extra(metadata)})
			}
		}
		for start := 0; start < len(out); start += 20 {
			end := min(start+20, len(out))
			arns := make([]string, 0, end-start)
			indexByARN := make(map[string]int, end-start)
			for index := start; index < end; index++ {
				arns = append(arns, out[index].CloudID)
				indexByARN[out[index].CloudID] = index
			}
			tagOutput, tagErr := client.DescribeTags(ctx, &elasticloadbalancingv2.DescribeTagsInput{ResourceArns: arns})
			if tagErr != nil {
				for index := start; index < end; index++ {
					metadata := map[string]any{}
					_ = json.Unmarshal([]byte(out[index].Extra), &metadata)
					metadata["tag_error"] = tagErr.Error()
					out[index].Extra = extra(metadata)
				}
				continue
			}
			for _, description := range tagOutput.TagDescriptions {
				index, ok := indexByARN[base.ToString(description.ResourceArn)]
				if !ok {
					continue
				}
				tagValues := map[string]string{}
				for _, tag := range description.Tags {
					tagValues[base.ToString(tag.Key)] = base.ToString(tag.Value)
				}
				out[index].Tags = tags(tagValues)
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
			bucketRegion := "us-east-1"
			location, locationErr := client.GetBucketLocation(ctx, &s3.GetBucketLocationInput{Bucket: base.String(name)})
			if locationErr == nil {
				bucketRegion = string(location.LocationConstraint)
				if bucketRegion == "" {
					bucketRegion = "us-east-1"
				} else if bucketRegion == "EU" {
					bucketRegion = "eu-west-1"
				}
			}
			metadata := map[string]any{"created_at": v.CreationDate, "location": bucketRegion}
			if locationErr != nil {
				metadata["location_error"] = locationErr.Error()
				bucketRegion = region
			}
			tagValues := map[string]string{}
			bucketConfig, bucketConfigErr := cfg(ctx, a, secret, bucketRegion)
			if bucketConfigErr != nil {
				metadata["enrichment_error"] = bucketConfigErr.Error()
			} else {
				bucketClient := s3.NewFromConfig(bucketConfig)
				tagOutput, tagErr := bucketClient.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{Bucket: base.String(name)})
				if tagErr == nil {
					for _, tag := range tagOutput.TagSet {
						tagValues[base.ToString(tag.Key)] = base.ToString(tag.Value)
					}
				} else if !strings.Contains(strings.ToLower(tagErr.Error()), "nosuchtagset") {
					metadata["tag_error"] = tagErr.Error()
				}
				monitorClient := cloudwatch.NewFromConfig(bucketConfig)
				storageSeries, storageErr := s3StorageSeries(ctx, monitorClient, cloud.Series{Supported: true}, name, 72*time.Hour)
				if storageErr == nil && len(storageSeries.Points) > 0 {
					metadata["capacity_gb"] = storageSeries.Current
				} else if storageErr != nil {
					metadata["capacity_error"] = storageErr.Error()
				} else {
					metadata["capacity_error"] = "CloudWatch 未返回存储容量"
				}
				objectCount, objectErr := s3ObjectCount(ctx, monitorClient, name)
				if objectErr == nil {
					metadata["object_count"] = objectCount
				} else {
					metadata["object_count_error"] = objectErr.Error()
				}
			}
			out = append(out, model.Resource{Provider: "aws", Type: "oss", Region: bucketRegion, CloudID: name, Name: name, Status: "running", Spec: "S3", Tags: tags(tagValues), Extra: extra(metadata)})
		}
	}
	return out, nil
}

func s3ObjectCount(ctx context.Context, client *cloudwatch.Client, bucket string) (float64, error) {
	end := time.Now()
	start := end.Add(-72 * time.Hour)
	period := int32(86400)
	response, err := client.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{
		Namespace: base.String("AWS/S3"), MetricName: base.String("NumberOfObjects"), StartTime: &start, EndTime: &end,
		Period: &period, Statistics: []ct.Statistic{ct.StatisticAverage}, Dimensions: []ct.Dimension{
			{Name: base.String("BucketName"), Value: base.String(bucket)},
			{Name: base.String("StorageType"), Value: base.String("AllStorageTypes")},
		},
	})
	if err != nil {
		return 0, err
	}
	var latest time.Time
	var count float64
	for _, point := range response.Datapoints {
		if point.Timestamp != nil && point.Timestamp.After(latest) {
			latest = *point.Timestamp
			count = base.ToFloat64(point.Average)
		}
	}
	if latest.IsZero() {
		return 0, fmt.Errorf("CloudWatch 未返回对象数")
	}
	return count, nil
}
func (Provider) Metrics(ctx context.Context, a model.CloudAccount, secret string, r model.Resource, metric string, span time.Duration) (cloud.Series, error) {
	series := cloud.Series{ResourceID: r.ID, Metric: metric, Name: r.Name, Provider: r.Provider, Supported: cloud.Supports(r, metric), Points: []cloud.Point{}}
	if !series.Supported {
		return series, nil
	}
	c, err := cfg(ctx, a, secret, r.Region)
	if err != nil {
		return series, err
	}
	client := cloudwatch.NewFromConfig(c)
	if r.Type == "oss" && metric == "storage" {
		return s3StorageSeries(ctx, client, series, r.CloudID, span)
	}
	namespace, name, dimension, unit, statistic := awsMetric(r, metric)
	if name == "" {
		series.Supported = false
		return series, nil
	}
	series.Unit = unit
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
	resp, err := client.GetMetricStatistics(ctx, &cloudwatch.GetMetricStatisticsInput{Namespace: base.String(namespace), MetricName: base.String(name), Dimensions: dimensions, StartTime: &start, EndTime: &end, Period: &period, Statistics: []ct.Statistic{statistic}})
	if err != nil {
		return series, err
	}
	for _, p := range resp.Datapoints {
		value := base.ToFloat64(p.Average)
		if statistic == ct.StatisticSum {
			value = base.ToFloat64(p.Sum)
		}
		if statistic == ct.StatisticSum && (series.Unit == "Byte/s" || series.Unit == "次/s") {
			value /= float64(period)
		}
		if series.Unit == "GB" {
			value /= 1024 * 1024 * 1024
		}
		if p.Timestamp == nil {
			continue
		}
		series.Points = append(series.Points, cloud.Point{Time: *p.Timestamp, Value: value})
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

func awsMetric(resource model.Resource, metric string) (namespace, name, dimension, unit string, statistic ct.Statistic) {
	unit, statistic = "%", ct.StatisticAverage
	switch resource.Type {
	case "vm":
		namespace, dimension = "AWS/EC2", "InstanceId"
		switch metric {
		case "cpu":
			name = "CPUUtilization"
		case "network_in":
			name, unit, statistic = "NetworkIn", "Byte/s", ct.StatisticSum
		case "network_out":
			name, unit, statistic = "NetworkOut", "Byte/s", ct.StatisticSum
		}
	case "rds":
		namespace, dimension = "AWS/RDS", "DBInstanceIdentifier"
		switch metric {
		case "cpu":
			name = "CPUUtilization"
		case "connections":
			name, unit = "DatabaseConnections", "个"
		case "storage":
			name, unit = "FreeStorageSpace", "GB"
		}
	case "lb":
		dimension, unit = "LoadBalancer", "个"
		if resource.Spec == "NLB" {
			namespace = "AWS/NetworkELB"
			if metric == "connections" {
				name = "ActiveFlowCount"
			}
		} else if resource.Spec == "ALB" {
			namespace = "AWS/ApplicationELB"
			switch metric {
			case "connections":
				name = "ActiveConnectionCount"
			case "qps":
				name, unit, statistic = "RequestCount", "次/s", ct.StatisticSum
			}
		}
	}
	return namespace, name, dimension, unit, statistic
}

func s3StorageSeries(ctx context.Context, client *cloudwatch.Client, series cloud.Series, bucket string, span time.Duration) (cloud.Series, error) {
	storageTypes := []string{
		"StandardStorage", "StandardIAStorage", "StandardIASizeOverhead", "StandardIAObjectOverhead",
		"OneZoneIAStorage", "OneZoneIASizeOverhead", "IntelligentTieringFAStorage", "IntelligentTieringIAStorage",
		"IntelligentTieringAAStorage", "IntelligentTieringAIAStorage", "IntelligentTieringDAAStorage",
		"GlacierStorage", "GlacierStagingStorage", "GlacierObjectOverhead", "GlacierS3ObjectOverhead",
		"DeepArchiveStorage", "DeepArchiveStagingStorage", "DeepArchiveObjectOverhead", "DeepArchiveS3ObjectOverhead",
		"GlacierInstantRetrievalStorage", "GlacierIRSizeOverhead", "ReducedRedundancyStorage",
	}
	queries := make([]ct.MetricDataQuery, 0, len(storageTypes))
	for index, storageType := range storageTypes {
		queries = append(queries, ct.MetricDataQuery{
			Id: base.String(fmt.Sprintf("storage%d", index)),
			MetricStat: &ct.MetricStat{
				Metric: &ct.Metric{Namespace: base.String("AWS/S3"), MetricName: base.String("BucketSizeBytes"), Dimensions: []ct.Dimension{
					{Name: base.String("BucketName"), Value: base.String(bucket)},
					{Name: base.String("StorageType"), Value: base.String(storageType)},
				}},
				Period: base.Int32(86400), Stat: base.String("Average"),
			},
			ReturnData: base.Bool(true),
		})
	}
	end := time.Now()
	if span < 72*time.Hour {
		span = 72 * time.Hour
	}
	start := end.Add(-span)
	response, err := client.GetMetricData(ctx, &cloudwatch.GetMetricDataInput{MetricDataQueries: queries, StartTime: &start, EndTime: &end, ScanBy: ct.ScanByTimestampAscending})
	if err != nil {
		return series, err
	}
	values := map[time.Time]float64{}
	for _, result := range response.MetricDataResults {
		for index, timestamp := range result.Timestamps {
			if index < len(result.Values) {
				values[timestamp] += result.Values[index] / (1024 * 1024 * 1024)
			}
		}
	}
	series.Unit = "GB"
	for timestamp, value := range values {
		series.Points = append(series.Points, cloud.Point{Time: timestamp, Value: value})
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
