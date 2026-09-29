package aliyun

import (
	"context"

	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type ecsAPI interface {
	DescribeInstancesWithContext(ctx context.Context, req *ecs.DescribeInstancesRequest, rt *dara.RuntimeOptions) (*ecs.DescribeInstancesResponse, error)
}

func collectECS(ctx context.Context, api ecsAPI, region string) ([]cloud.Resource, error) {
	req := &ecs.DescribeInstancesRequest{RegionId: dara.String(region), MaxResults: dara.Int32(100)}
	var out []cloud.Resource
	for page := 0; page < 1000; page++ {
		resp, err := api.DescribeInstancesWithContext(ctx, req, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil {
			break
		}
		if resp.Body.Instances != nil {
			for _, in := range resp.Body.Instances.Instance {
				out = append(out, ecsResource(in, region))
			}
		}
		next := dara.StringValue(resp.Body.NextToken)
		if next == "" {
			break
		}
		req.NextToken = dara.String(next)
	}
	return out, nil
}

func ecsResource(in *ecs.DescribeInstancesResponseBodyInstancesInstance, region string) cloud.Resource {
	id := dara.StringValue(in.InstanceId)
	name := dara.StringValue(in.InstanceName)
	if name == "" {
		name = id
	}
	var private, public []string
	vpc, vsw := "", ""
	if in.VpcAttributes != nil {
		vpc = dara.StringValue(in.VpcAttributes.VpcId)
		vsw = dara.StringValue(in.VpcAttributes.VSwitchId)
		if in.VpcAttributes.PrivateIpAddress != nil {
			private = append(private, strs(in.VpcAttributes.PrivateIpAddress.IpAddress)...)
		}
	}
	if in.InnerIpAddress != nil {
		private = append(private, strs(in.InnerIpAddress.IpAddress)...)
	}
	if in.PublicIpAddress != nil {
		public = append(public, strs(in.PublicIpAddress.IpAddress)...)
	}
	if in.EipAddress != nil {
		public = append(public, dara.StringValue(in.EipAddress.IpAddress))
	}
	tags := map[string]string{}
	if in.Tags != nil {
		for _, t := range in.Tags.Tag {
			tags[dara.StringValue(t.TagKey)] = dara.StringValue(t.TagValue)
		}
	}
	extra := map[string]any{
		"cpu":          dara.Int32Value(in.Cpu),
		"memory_mib":   dara.Int32Value(in.Memory),
		"os":           dara.StringValue(in.OSName),
		"vswitch_id":   vsw,
		"image_id":     dara.StringValue(in.ImageId),
		"network_type": dara.StringValue(in.InstanceNetworkType),
	}
	if in.InternetMaxBandwidthOut != nil && *in.InternetMaxBandwidthOut > 0 {
		extra["bandwidth_out_mbps"] = *in.InternetMaxBandwidthOut
	}
	if in.SecurityGroupIds != nil {
		if sgs := strs(in.SecurityGroupIds.SecurityGroupId); len(sgs) > 0 {
			extra["security_groups"] = sgs
		}
	}
	charge := chargeType(dara.StringValue(in.InstanceChargeType))
	var expire = parseExpire(dara.StringValue(in.ExpiredTime))
	if charge != model.ChargePrepaid {
		expire = nil
	}
	status := dara.StringValue(in.Status)
	return cloud.Resource{
		Type:       model.TypeVM,
		Region:     region,
		Zone:       dara.StringValue(in.ZoneId),
		ResourceID: id,
		Name:       name,
		Status:     normalizeECSStatus(status),
		RawStatus:  status,
		Spec:       dara.StringValue(in.InstanceType),
		PrivateIPs: dedupe(private),
		PublicIPs:  dedupe(public),
		VpcID:      vpc,
		ChargeType: charge,
		ExpireAt:   expire,
		CreatedAt:  parseTime(dara.StringValue(in.CreationTime)),
		Tags:       tags,
		Extra:      extra,
	}
}

func normalizeECSStatus(s string) string {
	switch s {
	case "Pending":
		return model.StatusPending
	case "Running":
		return model.StatusRunning
	case "Starting":
		return model.StatusStarting
	case "Stopping":
		return model.StatusStopping
	case "Stopped":
		return model.StatusStopped
	}
	return model.StatusUnknown
}

func chargeType(s string) string {
	switch s {
	case "PrePaid", "Prepaid", "PrePay":
		return model.ChargePrepaid
	default:
		return model.ChargePostpaid
	}
}

func strs(in []*string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if v := dara.StringValue(s); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
