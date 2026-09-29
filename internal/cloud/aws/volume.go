package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// collectVolumes lists EBS volumes in the "available" state (not attached).
func collectVolumes(ctx context.Context, api ec2.DescribeVolumesAPIClient, region string) ([]cloud.Resource, error) {
	pager := ec2.NewDescribeVolumesPaginator(api, &ec2.DescribeVolumesInput{
		Filters:    []ec2types.Filter{{Name: aws.String("status"), Values: []string{"available"}}},
		MaxResults: aws.Int32(500),
	})
	var out []cloud.Resource
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, v := range page.Volumes {
			out = append(out, volumeResource(v, region))
		}
	}
	return out, nil
}

func volumeResource(v ec2types.Volume, region string) cloud.Resource {
	id := aws.ToString(v.VolumeId)
	tags := ec2Tags(v.Tags)
	name := tags["Name"]
	if name == "" {
		name = id
	}
	size := aws.ToInt32(v.Size)
	extra := map[string]any{
		"size_gib":  size,
		"category":  string(v.VolumeType),
		"encrypted": aws.ToBool(v.Encrypted),
	}
	if v.Iops != nil {
		extra["iops"] = *v.Iops
	}
	return cloud.Resource{
		Type:       model.TypeDisk,
		Region:     region,
		Zone:       aws.ToString(v.AvailabilityZone),
		ResourceID: id,
		Name:       name,
		Status:     model.StatusAvailable,
		RawStatus:  string(v.State),
		Spec:       fmt.Sprintf("%s · %d GiB", v.VolumeType, size),
		ChargeType: model.ChargePostpaid,
		CreatedAt:  v.CreateTime,
		Tags:       tags,
		Extra:      extra,
	}
}
