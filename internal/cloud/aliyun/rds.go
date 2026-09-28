package aliyun

import (
	"context"
	"log/slog"
	"strconv"
	"strings"

	rds "github.com/alibabacloud-go/rds-20140815/v16/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type rdsAPI interface {
	DescribeDBInstancesWithContext(ctx context.Context, req *rds.DescribeDBInstancesRequest, rt *dara.RuntimeOptions) (*rds.DescribeDBInstancesResponse, error)
	DescribeDBInstanceAttributeWithContext(ctx context.Context, req *rds.DescribeDBInstanceAttributeRequest, rt *dara.RuntimeOptions) (*rds.DescribeDBInstanceAttributeResponse, error)
}

func collectRDS(ctx context.Context, api rdsAPI, region string) ([]cloud.Resource, error) {
	var out []cloud.Resource
	for page := int32(1); page < 1000; page++ {
		resp, err := api.DescribeDBInstancesWithContext(ctx, &rds.DescribeDBInstancesRequest{
			RegionId:   dara.String(region),
			PageSize:   dara.Int32(100),
			PageNumber: dara.Int32(page),
		}, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil || resp.Body.Items == nil || len(resp.Body.Items.DBInstance) == 0 {
			break
		}
		for _, db := range resp.Body.Items.DBInstance {
			out = append(out, rdsResource(db, region))
		}
		if total := int(dara.Int32Value(resp.Body.TotalRecordCount)); total > 0 && len(out) >= total {
			break
		}
		if len(resp.Body.Items.DBInstance) < 100 {
			break
		}
	}
	enrichRDS(ctx, api, out)
	return out, nil
}

func rdsResource(db *rds.DescribeDBInstancesResponseBodyItemsDBInstance, region string) cloud.Resource {
	id := dara.StringValue(db.DBInstanceId)
	name := dara.StringValue(db.DBInstanceDescription)
	if name == "" {
		name = id
	}
	extra := map[string]any{
		"engine":         dara.StringValue(db.Engine),
		"engine_version": dara.StringValue(db.EngineVersion),
		"storage_type":   dara.StringValue(db.DBInstanceStorageType),
		"net_type":       dara.StringValue(db.DBInstanceNetType),
		"vswitch_id":     dara.StringValue(db.VSwitchId),
	}
	if cs := dara.StringValue(db.ConnectionString); cs != "" {
		extra["endpoint"] = cs
	}
	if cpu, err := strconv.Atoi(dara.StringValue(db.DBInstanceCPU)); err == nil {
		extra["cpu"] = cpu
	}
	if mem := dara.Int32Value(db.DBInstanceMemory); mem > 0 {
		extra["memory_mib"] = mem
	}
	charge := chargeType(dara.StringValue(db.PayType))
	expire := parseExpire(dara.StringValue(db.ExpireTime))
	if charge != model.ChargePrepaid {
		expire = nil
	}
	status := dara.StringValue(db.DBInstanceStatus)
	return cloud.Resource{
		Type:       model.TypeRDS,
		Region:     region,
		Zone:       dara.StringValue(db.ZoneId),
		ResourceID: id,
		Name:       name,
		Status:     normalizeRDSStatus(status),
		RawStatus:  status,
		Spec:       dara.StringValue(db.DBInstanceClass),
		VpcID:      dara.StringValue(db.VpcId),
		ChargeType: charge,
		ExpireAt:   expire,
		CreatedAt:  parseTime(dara.StringValue(db.CreateTime)),
		Tags:       map[string]string{},
		Extra:      extra,
	}
}

// enrichRDS adds storage size and connection limits, which the list API does
// not return. Failures are ignored: the fields are informational.
func enrichRDS(ctx context.Context, api rdsAPI, res []cloud.Resource) {
	index := map[string]*cloud.Resource{}
	for i := range res {
		index[res[i].ResourceID] = &res[i]
	}
	for start := 0; start < len(res); start += 30 {
		var ids []string
		for _, r := range res[start:min(start+30, len(res))] {
			ids = append(ids, r.ResourceID)
		}
		resp, err := api.DescribeDBInstanceAttributeWithContext(ctx, &rds.DescribeDBInstanceAttributeRequest{DBInstanceId: dara.String(strings.Join(ids, ","))}, runtime())
		if err != nil {
			slog.Debug("查询 RDS 实例详情失败", "err", err)
			return
		}
		if resp == nil || resp.Body == nil || resp.Body.Items == nil {
			continue
		}
		for _, a := range resp.Body.Items.DBInstanceAttribute {
			r, ok := index[dara.StringValue(a.DBInstanceId)]
			if !ok {
				continue
			}
			if s := dara.Int32Value(a.DBInstanceStorage); s > 0 {
				r.Extra["storage_gb"] = s
			}
			if c := dara.Int32Value(a.MaxConnections); c > 0 {
				r.Extra["max_connections"] = c
			}
			if port := dara.StringValue(a.Port); port != "" {
				if ep, ok := r.Extra["endpoint"].(string); ok && ep != "" && !strings.Contains(ep, ":") {
					r.Extra["endpoint"] = ep + ":" + port
				}
			}
		}
	}
}

func normalizeRDSStatus(s string) string {
	switch s {
	case "Running":
		return model.StatusRunning
	case "Stopped", "Released":
		return model.StatusStopped
	case "Creating":
		return model.StatusPending
	case "Deleting":
		return model.StatusTerminating
	case "":
		return model.StatusUnknown
	}
	// Rebooting, DBInstanceClassChanging, TRANSING, EngineVersionUpgrading,
	// Restoring, NET_MODIFYING, ... are transitional.
	return model.StatusChanging
}
