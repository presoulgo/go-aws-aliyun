package aliyun

import (
	"context"
	"strconv"

	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type eipAPI interface {
	DescribeEipAddressesWithContext(ctx context.Context, req *ecs.DescribeEipAddressesRequest, rt *dara.RuntimeOptions) (*ecs.DescribeEipAddressesResponse, error)
}

// collectEIPs lists elastic IPs that are not bound to anything (Available).
func collectEIPs(ctx context.Context, api eipAPI, region string) ([]cloud.Resource, error) {
	var out []cloud.Resource
	for page := int32(1); page < 1000; page++ {
		resp, err := api.DescribeEipAddressesWithContext(ctx, &ecs.DescribeEipAddressesRequest{
			RegionId:   dara.String(region),
			Status:     dara.String("Available"),
			PageSize:   dara.Int32(100),
			PageNumber: dara.Int32(page),
		}, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil || resp.Body.EipAddresses == nil || len(resp.Body.EipAddresses.EipAddress) == 0 {
			break
		}
		for _, e := range resp.Body.EipAddresses.EipAddress {
			out = append(out, eipResource(e, region))
		}
		if total := int(dara.Int32Value(resp.Body.TotalCount)); total > 0 && len(out) >= total {
			break
		}
		if len(resp.Body.EipAddresses.EipAddress) < 100 {
			break
		}
	}
	return out, nil
}

func eipResource(e *ecs.DescribeEipAddressesResponseBodyEipAddressesEipAddress, region string) cloud.Resource {
	id := dara.StringValue(e.AllocationId)
	ip := dara.StringValue(e.IpAddress)
	name := ip
	if name == "" {
		name = id
	}
	extra := map[string]any{"internet_charge_type": dara.StringValue(e.InternetChargeType)}
	spec := ""
	if bw, err := strconv.Atoi(dara.StringValue(e.Bandwidth)); err == nil && bw > 0 {
		extra["bandwidth_mbps"] = bw
		spec = strconv.Itoa(bw) + " Mbps"
	}
	var public []string
	if ip != "" {
		public = []string{ip}
	}
	charge := chargeType(dara.StringValue(e.ChargeType))
	expire := parseExpire(dara.StringValue(e.ExpiredTime))
	if charge != model.ChargePrepaid {
		expire = nil
	}
	return cloud.Resource{
		Type:       model.TypeEIP,
		Region:     region,
		ResourceID: id,
		Name:       name,
		Status:     model.StatusAvailable,
		RawStatus:  dara.StringValue(e.Status),
		Spec:       spec,
		PublicIPs:  public,
		ChargeType: charge,
		ExpireAt:   expire,
		CreatedAt:  parseTime(dara.StringValue(e.AllocationTime)),
		Tags:       map[string]string{},
		Extra:      extra,
	}
}
