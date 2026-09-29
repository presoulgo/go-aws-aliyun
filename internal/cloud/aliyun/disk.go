package aliyun

import (
	"context"
	"fmt"

	ecs "github.com/alibabacloud-go/ecs-20140526/v7/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type diskAPI interface {
	DescribeDisksWithContext(ctx context.Context, req *ecs.DescribeDisksRequest, rt *dara.RuntimeOptions) (*ecs.DescribeDisksResponse, error)
}

// collectDisks lists unattached (Available) cloud disks: they are billed
// without serving any instance.
func collectDisks(ctx context.Context, api diskAPI, region string) ([]cloud.Resource, error) {
	req := &ecs.DescribeDisksRequest{RegionId: dara.String(region), Status: dara.String("Available"), MaxResults: dara.Int32(100)}
	var out []cloud.Resource
	for page := 0; page < 1000; page++ {
		resp, err := api.DescribeDisksWithContext(ctx, req, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil {
			break
		}
		if resp.Body.Disks != nil {
			for _, d := range resp.Body.Disks.Disk {
				out = append(out, diskResource(d, region))
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

var diskCategories = map[string]string{
	"cloud":            "普通云盘",
	"cloud_efficiency": "高效云盘",
	"cloud_ssd":        "SSD 云盘",
	"cloud_essd":       "ESSD 云盘",
	"cloud_essd_entry": "ESSD Entry 云盘",
	"cloud_auto":       "ESSD AutoPL 云盘",
}

func diskResource(d *ecs.DescribeDisksResponseBodyDisksDisk, region string) cloud.Resource {
	id := dara.StringValue(d.DiskId)
	name := dara.StringValue(d.DiskName)
	if name == "" {
		name = id
	}
	category := dara.StringValue(d.Category)
	label := diskCategories[category]
	if label == "" {
		label = category
	}
	size := dara.Int32Value(d.Size)
	tags := map[string]string{}
	if d.Tags != nil {
		for _, t := range d.Tags.Tag {
			tags[dara.StringValue(t.TagKey)] = dara.StringValue(t.TagValue)
		}
	}
	extra := map[string]any{
		"size_gib":  size,
		"category":  category,
		"disk_type": dara.StringValue(d.Type),
		"encrypted": dara.BoolValue(d.Encrypted),
	}
	if pl := dara.StringValue(d.PerformanceLevel); pl != "" {
		extra["performance_level"] = pl
	}
	if t := parseTime(dara.StringValue(d.DetachedTime)); t != nil {
		extra["detached_at"] = t.Format("2006-01-02T15:04:05Z")
	}
	charge := chargeType(dara.StringValue(d.DiskChargeType))
	expire := parseExpire(dara.StringValue(d.ExpiredTime))
	if charge != model.ChargePrepaid {
		expire = nil
	}
	status := dara.StringValue(d.Status)
	return cloud.Resource{
		Type:       model.TypeDisk,
		Region:     region,
		Zone:       dara.StringValue(d.ZoneId),
		ResourceID: id,
		Name:       name,
		Status:     model.StatusAvailable,
		RawStatus:  status,
		Spec:       fmt.Sprintf("%s · %d GiB", label, size),
		ChargeType: charge,
		ExpireAt:   expire,
		CreatedAt:  parseTime(dara.StringValue(d.CreationTime)),
		Tags:       tags,
		Extra:      extra,
	}
}
