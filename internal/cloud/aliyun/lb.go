package aliyun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	alb "github.com/alibabacloud-go/alb-20200616/v2/client"
	slb "github.com/alibabacloud-go/slb-20140515/v4/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type clbAPI interface {
	DescribeLoadBalancersWithContext(ctx context.Context, req *slb.DescribeLoadBalancersRequest, rt *dara.RuntimeOptions) (*slb.DescribeLoadBalancersResponse, error)
	DescribeLoadBalancerAttributeWithContext(ctx context.Context, req *slb.DescribeLoadBalancerAttributeRequest, rt *dara.RuntimeOptions) (*slb.DescribeLoadBalancerAttributeResponse, error)
}

type albAPI interface {
	ListLoadBalancersWithContext(ctx context.Context, req *alb.ListLoadBalancersRequest, rt *dara.RuntimeOptions) (*alb.ListLoadBalancersResponse, error)
}

// collectLB lists classic (CLB) and application (ALB) load balancers. When one
// product fails, the other's results are still returned together with the
// error so the caller can keep partial data.
func collectLB(ctx context.Context, clbc clbAPI, albc albAPI, region string) ([]cloud.Resource, error) {
	clbs, clbErr := collectCLB(ctx, clbc, region)
	albs, albErr := collectALB(ctx, albc, region)
	out := append(clbs, albs...)
	var errs []error
	if clbErr != nil && !errors.Is(clbErr, cloud.ErrRegionUnsupported) {
		errs = append(errs, fmt.Errorf("CLB: %w", clbErr))
	}
	if albErr != nil && !errors.Is(albErr, cloud.ErrRegionUnsupported) {
		errs = append(errs, fmt.Errorf("ALB: %w", albErr))
	}
	if clbErr != nil && albErr != nil && errors.Is(clbErr, cloud.ErrRegionUnsupported) && errors.Is(albErr, cloud.ErrRegionUnsupported) {
		return nil, clbErr
	}
	return out, errors.Join(errs...)
}

func collectCLB(ctx context.Context, api clbAPI, region string) ([]cloud.Resource, error) {
	var out []cloud.Resource
	for page := int32(1); page < 1000; page++ {
		resp, err := api.DescribeLoadBalancersWithContext(ctx, &slb.DescribeLoadBalancersRequest{
			RegionId:   dara.String(region),
			PageSize:   dara.Int32(100),
			PageNumber: dara.Int32(page),
		}, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil || resp.Body.LoadBalancers == nil || len(resp.Body.LoadBalancers.LoadBalancer) == 0 {
			break
		}
		for _, lb := range resp.Body.LoadBalancers.LoadBalancer {
			out = append(out, clbResource(lb, region))
		}
		if total := int(dara.Int32Value(resp.Body.TotalCount)); total > 0 && len(out) >= total {
			break
		}
		if len(resp.Body.LoadBalancers.LoadBalancer) < 100 {
			break
		}
	}
	// Expiry is only returned by the attribute API; query it for prepaid ones.
	for i := range out {
		if out[i].ChargeType != model.ChargePrepaid {
			continue
		}
		attr, err := api.DescribeLoadBalancerAttributeWithContext(ctx, &slb.DescribeLoadBalancerAttributeRequest{
			RegionId:       dara.String(region),
			LoadBalancerId: dara.String(out[i].ResourceID),
		}, runtime())
		if err != nil {
			slog.Debug("查询 CLB 到期时间失败", "id", out[i].ResourceID, "err", err)
			continue
		}
		if attr != nil && attr.Body != nil {
			if ms := dara.Int64Value(attr.Body.EndTimeStamp); ms > 0 {
				t := time.UnixMilli(ms).UTC()
				if t.Year() < 2099 {
					out[i].ExpireAt = &t
				}
			} else {
				out[i].ExpireAt = parseExpire(dara.StringValue(attr.Body.EndTime))
			}
		}
	}
	return out, nil
}

func clbResource(lb *slb.DescribeLoadBalancersResponseBodyLoadBalancersLoadBalancer, region string) cloud.Resource {
	id := dara.StringValue(lb.LoadBalancerId)
	name := dara.StringValue(lb.LoadBalancerName)
	if name == "" {
		name = id
	}
	spec := dara.StringValue(lb.LoadBalancerSpec)
	network := "internal"
	if dara.StringValue(lb.AddressType) == "internet" {
		network = "internet"
	}
	tags := map[string]string{}
	if lb.Tags != nil {
		for _, t := range lb.Tags.Tag {
			tags[dara.StringValue(t.TagKey)] = dara.StringValue(t.TagValue)
		}
	}
	var created *time.Time
	if ms := dara.Int64Value(lb.CreateTimeStamp); ms > 0 {
		t := time.UnixMilli(ms).UTC()
		created = &t
	} else {
		created = parseTime(dara.StringValue(lb.CreateTime))
	}
	extra := map[string]any{
		"lb_kind": cloud.LBKindCLB,
		"address": dara.StringValue(lb.Address),
		"network": network,
	}
	if spec != "" {
		extra["lb_spec"] = spec
	}
	if bw := dara.Int32Value(lb.Bandwidth); bw > 0 {
		extra["bandwidth_mbps"] = bw
	}
	status := dara.StringValue(lb.LoadBalancerStatus)
	zone := dara.StringValue(lb.MasterZoneId)
	if s := dara.StringValue(lb.SlaveZoneId); s != "" {
		zone += "," + s
	}
	return cloud.Resource{
		Type:       model.TypeLB,
		Region:     region,
		Zone:       zone,
		ResourceID: id,
		Name:       name,
		Status:     normalizeCLBStatus(status),
		RawStatus:  status,
		Spec:       "CLB",
		VpcID:      dara.StringValue(lb.VpcId),
		ChargeType: chargeType(dara.StringValue(lb.PayType)),
		CreatedAt:  created,
		Tags:       tags,
		Extra:      extra,
	}
}

func collectALB(ctx context.Context, api albAPI, region string) ([]cloud.Resource, error) {
	if err := skipRegion("alb", region); err != nil {
		return nil, err
	}
	req := &alb.ListLoadBalancersRequest{MaxResults: dara.Int32(100)}
	var out []cloud.Resource
	for page := 0; page < 1000; page++ {
		resp, err := api.ListLoadBalancersWithContext(ctx, req, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil {
			break
		}
		for _, lb := range resp.Body.LoadBalancers {
			out = append(out, albResource(lb, region))
		}
		next := dara.StringValue(resp.Body.NextToken)
		if next == "" {
			break
		}
		req.NextToken = dara.String(next)
	}
	return out, nil
}

var albEditions = map[string]string{"Basic": "基础版", "Standard": "标准版", "StandardWithWaf": "WAF 增强版"}

func albResource(lb *alb.ListLoadBalancersResponseBodyLoadBalancers, region string) cloud.Resource {
	id := dara.StringValue(lb.LoadBalancerId)
	name := dara.StringValue(lb.LoadBalancerName)
	if name == "" {
		name = id
	}
	network := "internal"
	if dara.StringValue(lb.AddressType) == "Internet" {
		network = "internet"
	}
	tags := map[string]string{}
	for _, t := range lb.Tags {
		tags[dara.StringValue(t.Key)] = dara.StringValue(t.Value)
	}
	extra := map[string]any{
		"lb_kind": cloud.LBKindALB,
		"address": dara.StringValue(lb.DNSName),
		"network": network,
	}
	if ed, ok := albEditions[dara.StringValue(lb.LoadBalancerEdition)]; ok {
		extra["lb_spec"] = ed
	}
	charge := model.ChargePostpaid
	if lb.LoadBalancerBillingConfig != nil {
		charge = chargeType(dara.StringValue(lb.LoadBalancerBillingConfig.PayType))
	}
	status := dara.StringValue(lb.LoadBalancerStatus)
	return cloud.Resource{
		Type:       model.TypeLB,
		Region:     region,
		ResourceID: id,
		Name:       name,
		Status:     normalizeALBStatus(status),
		RawStatus:  status,
		Spec:       "ALB",
		VpcID:      dara.StringValue(lb.VpcId),
		ChargeType: charge,
		CreatedAt:  parseTime(dara.StringValue(lb.CreateTime)),
		Tags:       tags,
		Extra:      extra,
	}
}

func normalizeCLBStatus(s string) string {
	switch s {
	case "active":
		return model.StatusRunning
	case "inactive":
		return model.StatusStopped
	case "locked":
		return model.StatusFailed
	}
	return model.StatusUnknown
}

func normalizeALBStatus(s string) string {
	switch s {
	case "Active":
		return model.StatusRunning
	case "Inactive":
		return model.StatusStopped
	case "Provisioning":
		return model.StatusPending
	case "Configuring":
		return model.StatusChanging
	case "CreateFailed":
		return model.StatusFailed
	}
	return model.StatusUnknown
}
