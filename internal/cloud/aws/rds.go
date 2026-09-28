package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func collectRDS(ctx context.Context, api rds.DescribeDBInstancesAPIClient, region string) ([]cloud.Resource, error) {
	var out []cloud.Resource
	pager := rds.NewDescribeDBInstancesPaginator(api, &rds.DescribeDBInstancesInput{MaxRecords: aws.Int32(100)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, db := range page.DBInstances {
			out = append(out, rdsResource(db, region))
		}
	}
	return out, nil
}

func rdsResource(db rdstypes.DBInstance, region string) cloud.Resource {
	id := aws.ToString(db.DBInstanceIdentifier)
	status := aws.ToString(db.DBInstanceStatus)
	extra := map[string]any{
		"engine":              aws.ToString(db.Engine),
		"engine_version":      aws.ToString(db.EngineVersion),
		"storage_type":        aws.ToString(db.StorageType),
		"multi_az":            aws.ToBool(db.MultiAZ),
		"arn":                 aws.ToString(db.DBInstanceArn),
		"publicly_accessible": aws.ToBool(db.PubliclyAccessible),
	}
	if db.AllocatedStorage != nil {
		extra["allocated_storage_gib"] = aws.ToInt32(db.AllocatedStorage)
	}
	if db.Endpoint != nil && db.Endpoint.Address != nil {
		extra["endpoint"] = fmt.Sprintf("%s:%d", aws.ToString(db.Endpoint.Address), aws.ToInt32(db.Endpoint.Port))
	}
	vpc := ""
	if db.DBSubnetGroup != nil {
		vpc = aws.ToString(db.DBSubnetGroup.VpcId)
	}
	tags := make(map[string]string, len(db.TagList))
	for _, t := range db.TagList {
		tags[aws.ToString(t.Key)] = aws.ToString(t.Value)
	}
	return cloud.Resource{
		Type:       model.TypeRDS,
		Region:     region,
		Zone:       aws.ToString(db.AvailabilityZone),
		ResourceID: id,
		Name:       id,
		Status:     normalizeRDSStatus(status),
		RawStatus:  status,
		Spec:       aws.ToString(db.DBInstanceClass),
		VpcID:      vpc,
		ChargeType: model.ChargePostpaid,
		CreatedAt:  db.InstanceCreateTime,
		Tags:       tags,
		Extra:      extra,
	}
}

func normalizeRDSStatus(s string) string {
	switch s {
	case "available":
		return model.StatusRunning
	case "stopped":
		return model.StatusStopped
	case "starting":
		return model.StatusStarting
	case "stopping":
		return model.StatusStopping
	case "deleting":
		return model.StatusTerminating
	case "failed", "storage-full", "inaccessible-encryption-credentials", "restore-error":
		return model.StatusFailed
	}
	if strings.HasPrefix(s, "incompatible-") {
		return model.StatusFailed
	}
	switch s {
	case "creating", "backing-up", "modifying", "rebooting", "renaming", "resetting-master-credentials",
		"upgrading", "maintenance", "moving-to-vpc", "storage-optimization", "converting-to-vpc":
		return model.StatusChanging
	}
	if strings.HasPrefix(s, "configuring-") {
		return model.StatusChanging
	}
	return model.StatusUnknown
}
