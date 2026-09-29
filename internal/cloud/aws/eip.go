package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type addressAPI interface {
	DescribeAddresses(ctx context.Context, in *ec2.DescribeAddressesInput, opts ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
}

// collectEIPs lists Elastic IPs without an association: AWS bills idle ones.
func collectEIPs(ctx context.Context, api addressAPI, region string) ([]cloud.Resource, error) {
	resp, err := api.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, classify(err)
	}
	var out []cloud.Resource
	for _, a := range resp.Addresses {
		if aws.ToString(a.AssociationId) != "" || aws.ToString(a.InstanceId) != "" || aws.ToString(a.NetworkInterfaceId) != "" {
			continue
		}
		ip := aws.ToString(a.PublicIp)
		id := aws.ToString(a.AllocationId)
		if id == "" {
			id = ip
		}
		tags := ec2Tags(a.Tags)
		name := tags["Name"]
		if name == "" {
			name = ip
		}
		var public []string
		if ip != "" {
			public = []string{ip}
		}
		out = append(out, cloud.Resource{
			Type:       model.TypeEIP,
			Region:     region,
			ResourceID: id,
			Name:       name,
			Status:     model.StatusAvailable,
			RawStatus:  "unassociated",
			PublicIPs:  public,
			ChargeType: model.ChargePostpaid,
			Tags:       tags,
			Extra:      map[string]any{"domain": string(a.Domain), "network_border_group": aws.ToString(a.NetworkBorderGroup)},
		})
	}
	return out, nil
}
