package aws

import (
	"context"
	"log/slog"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type elbAPI interface {
	elbv2.DescribeLoadBalancersAPIClient
	DescribeTags(ctx context.Context, in *elbv2.DescribeTagsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTagsOutput, error)
}

func collectELB(ctx context.Context, api elbAPI, region string) ([]cloud.Resource, error) {
	var lbs []elbtypes.LoadBalancer
	pager := elbv2.NewDescribeLoadBalancersPaginator(api, &elbv2.DescribeLoadBalancersInput{PageSize: aws.Int32(400)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		lbs = append(lbs, page.LoadBalancers...)
	}
	tags := elbTags(ctx, api, lbs)
	out := make([]cloud.Resource, 0, len(lbs))
	for _, lb := range lbs {
		arn := aws.ToString(lb.LoadBalancerArn)
		kind := lbKind(lb.Type)
		state := ""
		if lb.State != nil {
			state = string(lb.State.Code)
		}
		var zones []string
		for _, z := range lb.AvailabilityZones {
			zones = append(zones, aws.ToString(z.ZoneName))
		}
		network := "internal"
		if lb.Scheme == elbtypes.LoadBalancerSchemeEnumInternetFacing {
			network = "internet"
		}
		out = append(out, cloud.Resource{
			Type:       model.TypeLB,
			Region:     region,
			Zone:       strings.Join(zones, ","),
			ResourceID: lbDimension(arn),
			Name:       aws.ToString(lb.LoadBalancerName),
			Status:     normalizeELBState(state),
			RawStatus:  state,
			Spec:       strings.ToUpper(kind),
			VpcID:      aws.ToString(lb.VpcId),
			ChargeType: model.ChargePostpaid,
			CreatedAt:  lb.CreatedTime,
			Tags:       tags[arn],
			Extra: map[string]any{
				"lb_kind": kind,
				"arn":     arn,
				"address": aws.ToString(lb.DNSName),
				"network": network,
			},
		})
	}
	return out, nil
}

// lbDimension returns the part of the ARN after "loadbalancer/", which is the
// CloudWatch "LoadBalancer" dimension value (e.g. app/name/1234abcd).
func lbDimension(arn string) string {
	if i := strings.Index(arn, ":loadbalancer/"); i >= 0 {
		return arn[i+len(":loadbalancer/"):]
	}
	return arn
}

func lbKind(t elbtypes.LoadBalancerTypeEnum) string {
	switch t {
	case elbtypes.LoadBalancerTypeEnumNetwork:
		return cloud.LBKindNLB
	case elbtypes.LoadBalancerTypeEnumGateway:
		return cloud.LBKindGWL
	default:
		return cloud.LBKindALB
	}
}

func normalizeELBState(s string) string {
	switch s {
	case "active", "active_impaired":
		return model.StatusRunning
	case "provisioning":
		return model.StatusPending
	case "failed":
		return model.StatusFailed
	}
	return model.StatusUnknown
}

func elbTags(ctx context.Context, api elbAPI, lbs []elbtypes.LoadBalancer) map[string]map[string]string {
	out := map[string]map[string]string{}
	for start := 0; start < len(lbs); start += 20 {
		var arns []string
		for _, lb := range lbs[start:min(start+20, len(lbs))] {
			arns = append(arns, aws.ToString(lb.LoadBalancerArn))
		}
		resp, err := api.DescribeTags(ctx, &elbv2.DescribeTagsInput{ResourceArns: arns})
		if err != nil {
			slog.Debug("查询负载均衡标签失败", "err", err)
			return out
		}
		for _, d := range resp.TagDescriptions {
			m := map[string]string{}
			for _, t := range d.Tags {
				m[aws.ToString(t.Key)] = aws.ToString(t.Value)
			}
			out[aws.ToString(d.ResourceArn)] = m
		}
	}
	return out
}
