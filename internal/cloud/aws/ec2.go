package aws

import (
	"context"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// ec2API is the subset of the EC2 client used by the collector.
type ec2API interface {
	ec2.DescribeInstancesAPIClient
	ec2.DescribeInstanceTypesAPIClient
}

type typeInfo struct {
	vcpu   int32
	memMiB int64
}

func collectEC2(ctx context.Context, api ec2API, region string) ([]cloud.Resource, error) {
	var instances []ec2types.Instance
	pager := ec2.NewDescribeInstancesPaginator(api, &ec2.DescribeInstancesInput{MaxResults: aws.Int32(1000)})
	for pager.HasMorePages() {
		out, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, r := range out.Reservations {
			instances = append(instances, r.Instances...)
		}
	}

	specs := describeInstanceTypes(ctx, api, instances)
	out := make([]cloud.Resource, 0, len(instances))
	for _, in := range instances {
		state := ec2types.InstanceStateNameTerminated
		if in.State != nil {
			state = in.State.Name
		}
		if state == ec2types.InstanceStateNameTerminated {
			continue
		}
		id := aws.ToString(in.InstanceId)
		tags := ec2Tags(in.Tags)
		name := tags["Name"]
		if name == "" {
			name = id
		}
		var private, public []string
		if ip := aws.ToString(in.PrivateIpAddress); ip != "" {
			private = append(private, ip)
		}
		if ip := aws.ToString(in.PublicIpAddress); ip != "" {
			public = append(public, ip)
		}
		for _, ni := range in.NetworkInterfaces {
			for _, pip := range ni.PrivateIpAddresses {
				private = append(private, aws.ToString(pip.PrivateIpAddress))
				if pip.Association != nil {
					public = append(public, aws.ToString(pip.Association.PublicIp))
				}
			}
		}
		extra := map[string]any{
			"subnet_id":    aws.ToString(in.SubnetId),
			"image_id":     aws.ToString(in.ImageId),
			"os":           aws.ToString(in.PlatformDetails),
			"architecture": string(in.Architecture),
			"key_name":     aws.ToString(in.KeyName),
		}
		var sgs []string
		for _, g := range in.SecurityGroups {
			sgs = append(sgs, aws.ToString(g.GroupId))
		}
		if len(sgs) > 0 {
			extra["security_groups"] = sgs
		}
		if in.InstanceLifecycle != "" {
			extra["lifecycle"] = string(in.InstanceLifecycle)
		}
		if spec, ok := specs[string(in.InstanceType)]; ok {
			extra["cpu"] = spec.vcpu
			extra["memory_mib"] = spec.memMiB
		} else if in.CpuOptions != nil && in.CpuOptions.CoreCount != nil {
			extra["cpu"] = aws.ToInt32(in.CpuOptions.CoreCount) * max(aws.ToInt32(in.CpuOptions.ThreadsPerCore), 1)
		}
		zone := ""
		if in.Placement != nil {
			zone = aws.ToString(in.Placement.AvailabilityZone)
		}
		out = append(out, cloud.Resource{
			Type:       model.TypeVM,
			Region:     region,
			Zone:       zone,
			ResourceID: id,
			Name:       name,
			Status:     normalizeEC2State(state),
			RawStatus:  string(state),
			Spec:       string(in.InstanceType),
			PrivateIPs: dedupe(private),
			PublicIPs:  dedupe(public),
			VpcID:      aws.ToString(in.VpcId),
			ChargeType: model.ChargePostpaid,
			CreatedAt:  in.LaunchTime,
			Tags:       tags,
			Extra:      extra,
		})
	}
	return out, nil
}

// describeInstanceTypes looks up vCPU and memory for the instance types in
// use. Failures are logged and ignored: the fields are informational.
func describeInstanceTypes(ctx context.Context, api ec2API, instances []ec2types.Instance) map[string]typeInfo {
	seen := map[ec2types.InstanceType]bool{}
	var want []ec2types.InstanceType
	for _, in := range instances {
		if in.InstanceType != "" && !seen[in.InstanceType] {
			seen[in.InstanceType] = true
			want = append(want, in.InstanceType)
		}
	}
	out := map[string]typeInfo{}
	for start := 0; start < len(want); start += 100 {
		batch := want[start:min(start+100, len(want))]
		resp, err := api.DescribeInstanceTypes(ctx, &ec2.DescribeInstanceTypesInput{InstanceTypes: batch})
		if err != nil {
			slog.Debug("查询 EC2 实例规格失败", "err", err)
			return out
		}
		for _, it := range resp.InstanceTypes {
			var info typeInfo
			if it.VCpuInfo != nil {
				info.vcpu = aws.ToInt32(it.VCpuInfo.DefaultVCpus)
			}
			if it.MemoryInfo != nil {
				info.memMiB = aws.ToInt64(it.MemoryInfo.SizeInMiB)
			}
			out[string(it.InstanceType)] = info
		}
	}
	return out
}

func normalizeEC2State(s ec2types.InstanceStateName) string {
	switch s {
	case ec2types.InstanceStateNamePending:
		return model.StatusPending
	case ec2types.InstanceStateNameRunning:
		return model.StatusRunning
	case ec2types.InstanceStateNameStopping:
		return model.StatusStopping
	case ec2types.InstanceStateNameStopped:
		return model.StatusStopped
	case ec2types.InstanceStateNameShuttingDown:
		return model.StatusTerminating
	}
	return model.StatusUnknown
}

func ec2Tags(tags []ec2types.Tag) map[string]string {
	out := make(map[string]string, len(tags))
	for _, t := range tags {
		out[aws.ToString(t.Key)] = aws.ToString(t.Value)
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
