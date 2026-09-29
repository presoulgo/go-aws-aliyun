package aws

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Storage types summed into the bucket size.
var s3StorageTypes = []string{
	"StandardStorage", "StandardIAStorage", "OneZoneIAStorage", "IntelligentTieringFAStorage",
	"IntelligentTieringIAStorage", "GlacierInstantRetrievalStorage", "GlacierStorage", "DeepArchiveStorage",
}

func collectS3(ctx context.Context, api s3.ListBucketsAPIClient, cwFor func(region string) cwAPI, defRegion string, now time.Time) ([]cloud.Resource, error) {
	var out []cloud.Resource
	pager := s3.NewListBucketsPaginator(api, &s3.ListBucketsInput{MaxBuckets: aws.Int32(1000)})
	for pager.HasMorePages() {
		page, err := pager.NextPage(ctx)
		if err != nil {
			return nil, classify(err)
		}
		for _, b := range page.Buckets {
			region := aws.ToString(b.BucketRegion)
			if region == "" {
				region = defRegion
			}
			name := aws.ToString(b.Name)
			out = append(out, cloud.Resource{
				Type:       model.TypeBucket,
				Region:     region,
				ResourceID: name,
				Name:       name,
				Status:     model.StatusRunning,
				RawStatus:  "available",
				Spec:       "S3",
				ChargeType: model.ChargePostpaid,
				CreatedAt:  b.CreationDate,
				Tags:       map[string]string{},
				Extra:      map[string]any{},
			})
		}
	}

	byRegion := map[string][]int{}
	for i, r := range out {
		byRegion[r.Region] = append(byRegion[r.Region], i)
	}
	for region, idx := range byRegion {
		names := make([]string, len(idx))
		for j, i := range idx {
			names[j] = out[i].Name
		}
		stats, err := bucketStats(ctx, cwFor(region), names, now)
		if err != nil {
			slog.Debug("查询 S3 容量失败", "region", region, "err", err)
			continue
		}
		for _, i := range idx {
			if st, ok := stats[out[i].Name]; ok {
				out[i].Extra["size_bytes"] = st[0]
				out[i].Extra["object_count"] = st[1]
			}
		}
	}
	return out, nil
}

// bucketStats returns [size bytes, object count] per bucket from the daily
// S3 storage metrics.
func bucketStats(ctx context.Context, api cwAPI, names []string, now time.Time) (map[string][2]float64, error) {
	var queries []cwtypes.MetricDataQuery
	type ref struct {
		bucket string
		count  bool
	}
	refs := map[string]ref{}
	add := func(bucket, metric, storageType string, count bool) {
		id := fmt.Sprintf("b%d", len(queries))
		refs[id] = ref{bucket: bucket, count: count}
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(id),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{
					Namespace:  aws.String("AWS/S3"),
					MetricName: aws.String(metric),
					Dimensions: []cwtypes.Dimension{dim("BucketName", bucket), dim("StorageType", storageType)},
				},
				Period: aws.Int32(86400),
				Stat:   aws.String("Average"),
			},
			ReturnData: aws.Bool(true),
		})
	}
	for _, n := range names {
		for _, st := range s3StorageTypes {
			add(n, "BucketSizeBytes", st, false)
		}
		add(n, "NumberOfObjects", "AllStorageTypes", true)
	}
	results, err := getMetricData(ctx, api, queries, now.Add(-72*time.Hour), now)
	if err != nil {
		return nil, err
	}
	out := map[string][2]float64{}
	for id, r := range results {
		if len(r.Values) == 0 {
			continue
		}
		latest := 0
		for j := range r.Timestamps {
			if r.Timestamps[j].After(r.Timestamps[latest]) {
				latest = j
			}
		}
		if latest >= len(r.Values) {
			continue
		}
		rf := refs[id]
		v := out[rf.bucket]
		if rf.count {
			v[1] = r.Values[latest]
		} else {
			v[0] += r.Values[latest]
		}
		out[rf.bucket] = v
	}
	return out, nil
}
