package aws

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cwtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type cwAPI interface {
	cloudwatch.GetMetricDataAPIClient
	ListMetrics(ctx context.Context, in *cloudwatch.ListMetricsInput, optFns ...func(*cloudwatch.Options)) (*cloudwatch.ListMetricsOutput, error)
}

// cwSource is one CloudWatch metric feeding a catalog metric.
type cwSource struct {
	namespace string
	metric    string
	dims      []cwtypes.Dimension
	stat      string
	// scale converts a datapoint into the catalog unit; period is in seconds.
	scale func(v, period float64) float64
}

type cwPlan struct {
	sources []cwSource
	// post converts the combined value (derived metrics); false drops it.
	post func(v float64) (float64, bool)
	// agentMetric means dimensions must be discovered with ListMetrics
	// because the CloudWatch agent appends its own dimensions.
	agentMetric bool
	minPeriod   time.Duration
}

func identity(v, _ float64) float64  { return v }
func perSecond(v, p float64) float64 { return v / p }
func bitsPerSecond(v, p float64) float64 {
	return v * 8 / p
}
func perMinuteAvg(v, p float64) float64 { return v / (p / 60) }

func dim(name, value string) cwtypes.Dimension {
	return cwtypes.Dimension{Name: aws.String(name), Value: aws.String(value)}
}

func planFor(ref cloud.ResourceRef, key string) (cwPlan, bool) {
	src := func(ns, metric, stat string, scale func(v, p float64) float64, dims ...cwtypes.Dimension) cwSource {
		return cwSource{namespace: ns, metric: metric, stat: stat, scale: scale, dims: dims}
	}
	switch ref.Type {
	case model.TypeVM:
		d := dim("InstanceId", ref.ResourceID)
		ec2 := func(sources ...cwSource) (cwPlan, bool) {
			return cwPlan{sources: sources, minPeriod: 5 * time.Minute}, true
		}
		switch key {
		case cloud.MetricCPU:
			return ec2(src("AWS/EC2", "CPUUtilization", "Average", identity, d))
		case cloud.MetricMem:
			return cwPlan{sources: []cwSource{src("CWAgent", "mem_used_percent", "Average", identity, d)}, agentMetric: true, minPeriod: time.Minute}, true
		case cloud.MetricNetIn:
			return ec2(src("AWS/EC2", "NetworkIn", "Sum", bitsPerSecond, d))
		case cloud.MetricNetOut:
			return ec2(src("AWS/EC2", "NetworkOut", "Sum", bitsPerSecond, d))
		case cloud.MetricDiskRead:
			return ec2(src("AWS/EC2", "EBSReadBytes", "Sum", perSecond, d), src("AWS/EC2", "DiskReadBytes", "Sum", perSecond, d))
		case cloud.MetricDiskWrite:
			return ec2(src("AWS/EC2", "EBSWriteBytes", "Sum", perSecond, d), src("AWS/EC2", "DiskWriteBytes", "Sum", perSecond, d))
		}
	case model.TypeRDS:
		d := dim("DBInstanceIdentifier", ref.ResourceID)
		switch key {
		case cloud.MetricCPU:
			return cwPlan{sources: []cwSource{src("AWS/RDS", "CPUUtilization", "Average", identity, d)}}, true
		case cloud.MetricFreeMem:
			return cwPlan{sources: []cwSource{src("AWS/RDS", "FreeableMemory", "Average", identity, d)}}, true
		case cloud.MetricConnections:
			return cwPlan{sources: []cwSource{src("AWS/RDS", "DatabaseConnections", "Average", identity, d)}}, true
		case cloud.MetricDiskUtil:
			gib, ok := cloud.ExtraFloat(ref.Extra, "allocated_storage_gib")
			if !ok || gib <= 0 {
				return cwPlan{}, false
			}
			total := gib * 1024 * 1024 * 1024
			return cwPlan{
				sources: []cwSource{src("AWS/RDS", "FreeStorageSpace", "Average", identity, d)},
				post: func(free float64) (float64, bool) {
					used := (total - free) / total * 100
					return clamp(used, 0, 100), true
				},
			}, true
		}
	case model.TypeLB:
		d := dim("LoadBalancer", ref.ResourceID)
		kind := cloud.ExtraString(ref.Extra, "lb_kind")
		ns := map[string]string{cloud.LBKindALB: "AWS/ApplicationELB", cloud.LBKindNLB: "AWS/NetworkELB", cloud.LBKindGWL: "AWS/GatewayELB"}[kind]
		if ns == "" {
			return cwPlan{}, false
		}
		switch key {
		case cloud.MetricQPS:
			if kind != cloud.LBKindALB {
				return cwPlan{}, false
			}
			return cwPlan{sources: []cwSource{src(ns, "RequestCount", "Sum", perSecond, d)}}, true
		case cloud.MetricActiveConn:
			if kind == cloud.LBKindALB {
				return cwPlan{sources: []cwSource{src(ns, "ActiveConnectionCount", "Sum", perMinuteAvg, d)}}, true
			}
			return cwPlan{sources: []cwSource{src(ns, "ActiveFlowCount", "Average", identity, d)}}, true
		case cloud.MetricNewConn:
			if kind == cloud.LBKindALB {
				return cwPlan{sources: []cwSource{src(ns, "NewConnectionCount", "Sum", perSecond, d)}}, true
			}
			return cwPlan{sources: []cwSource{src(ns, "NewFlowCount", "Sum", perSecond, d)}}, true
		case cloud.MetricTraffic:
			return cwPlan{sources: []cwSource{src(ns, "ProcessedBytes", "Sum", bitsPerSecond, d)}}, true
		}
	}
	return cwPlan{}, false
}

// SupportedMetrics lists the catalog keys CloudWatch can provide for ref.
func SupportedMetrics(ref cloud.ResourceRef) []string {
	var out []string
	for _, m := range cloud.MetricsFor(ref.Type) {
		if _, ok := planFor(ref, m.Key); ok {
			out = append(out, m.Key)
		}
	}
	return out
}

type queryRef struct {
	key    int
	source cwSource
}

func queryMetrics(ctx context.Context, api cwAPI, ref cloud.ResourceRef, q cloud.MetricQuery) ([]cloud.Series, error) {
	type keyPlan struct {
		key  string
		plan cwPlan
	}
	var plans []keyPlan
	period := q.Period
	for _, k := range q.Keys {
		plan, ok := planFor(ref, k)
		if !ok {
			continue
		}
		if plan.minPeriod > period {
			period = plan.minPeriod
		}
		plans = append(plans, keyPlan{key: k, plan: plan})
	}
	if period < time.Minute {
		period = time.Minute
	}
	periodSec := int32(period / time.Second)

	var queries []cwtypes.MetricDataQuery
	refs := map[string]queryRef{}
	for i, kp := range plans {
		for _, s := range kp.plan.sources {
			dims := s.dims
			if kp.plan.agentMetric {
				found, err := discoverDimensions(ctx, api, s.namespace, s.metric, s.dims)
				if err != nil {
					return nil, classify(err)
				}
				if found == nil {
					continue
				}
				dims = found
			}
			id := fmt.Sprintf("q%d", len(queries))
			refs[id] = queryRef{key: i, source: s}
			queries = append(queries, cwtypes.MetricDataQuery{
				Id: aws.String(id),
				MetricStat: &cwtypes.MetricStat{
					Metric: &cwtypes.Metric{Namespace: aws.String(s.namespace), MetricName: aws.String(s.metric), Dimensions: dims},
					Period: aws.Int32(periodSec),
					Stat:   aws.String(s.stat),
				},
				ReturnData: aws.Bool(true),
			})
		}
	}

	sums := make([]map[int64]float64, len(plans))
	for i := range sums {
		sums[i] = map[int64]float64{}
	}
	if len(queries) > 0 {
		results, err := getMetricData(ctx, api, queries, q.Start, q.End)
		if err != nil {
			return nil, err
		}
		for id, r := range results {
			qr := refs[id]
			for j, ts := range r.Timestamps {
				if j >= len(r.Values) {
					break
				}
				sums[qr.key][ts.UnixMilli()] += qr.source.scale(r.Values[j], float64(periodSec))
			}
		}
	}

	out := make([]cloud.Series, 0, len(plans))
	for i, kp := range plans {
		def, _ := cloud.LookupMetric(ref.Type, kp.key)
		points := make([]cloud.Point, 0, len(sums[i]))
		for ts, v := range sums[i] {
			if kp.plan.post != nil {
				var ok bool
				if v, ok = kp.plan.post(v); !ok {
					continue
				}
			}
			points = append(points, cloud.Point{float64(ts), round(v)})
		}
		sort.Slice(points, func(a, b int) bool { return points[a][0] < points[b][0] })
		out = append(out, cloud.Series{Key: kp.key, Unit: def.Unit, Points: points})
	}
	return out, nil
}

type metricResult struct {
	Timestamps []time.Time
	Values     []float64
}

// getMetricData runs queries in batches of 500 and merges paginated results.
func getMetricData(ctx context.Context, api cwAPI, queries []cwtypes.MetricDataQuery, start, end time.Time) (map[string]*metricResult, error) {
	out := map[string]*metricResult{}
	for from := 0; from < len(queries); from += 500 {
		batch := queries[from:min(from+500, len(queries))]
		pager := cloudwatch.NewGetMetricDataPaginator(api, &cloudwatch.GetMetricDataInput{
			MetricDataQueries: batch,
			StartTime:         aws.Time(start),
			EndTime:           aws.Time(end),
			ScanBy:            cwtypes.ScanByTimestampAscending,
		})
		for pager.HasMorePages() {
			page, err := pager.NextPage(ctx)
			if err != nil {
				return nil, classify(err)
			}
			for _, r := range page.MetricDataResults {
				id := aws.ToString(r.Id)
				mr, ok := out[id]
				if !ok {
					mr = &metricResult{}
					out[id] = mr
				}
				mr.Timestamps = append(mr.Timestamps, r.Timestamps...)
				mr.Values = append(mr.Values, r.Values...)
			}
		}
	}
	return out, nil
}

// discoverDimensions finds the full dimension set the CloudWatch agent
// publishes for a metric that includes the given dimensions.
func discoverDimensions(ctx context.Context, api cwAPI, namespace, metric string, dims []cwtypes.Dimension) ([]cwtypes.Dimension, error) {
	filters := make([]cwtypes.DimensionFilter, 0, len(dims))
	for _, d := range dims {
		filters = append(filters, cwtypes.DimensionFilter{Name: d.Name, Value: d.Value})
	}
	out, err := api.ListMetrics(ctx, &cloudwatch.ListMetricsInput{
		Namespace:  aws.String(namespace),
		MetricName: aws.String(metric),
		Dimensions: filters,
	})
	if err != nil {
		return nil, err
	}
	if len(out.Metrics) == 0 {
		return nil, nil
	}
	return out.Metrics[0].Dimensions, nil
}

// cpuSnapshot reads hourly CPUUtilization of instances over the last 24h.
func cpuSnapshot(ctx context.Context, api cwAPI, ids []string, now time.Time) (map[string]cloud.CPUStat, error) {
	out := make(map[string]cloud.CPUStat, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	queries := make([]cwtypes.MetricDataQuery, 0, len(ids))
	byQuery := map[string]string{}
	for i, id := range ids {
		qid := fmt.Sprintf("c%d", i)
		byQuery[qid] = id
		queries = append(queries, cwtypes.MetricDataQuery{
			Id: aws.String(qid),
			MetricStat: &cwtypes.MetricStat{
				Metric: &cwtypes.Metric{Namespace: aws.String("AWS/EC2"), MetricName: aws.String("CPUUtilization"), Dimensions: []cwtypes.Dimension{dim("InstanceId", id)}},
				Period: aws.Int32(3600),
				Stat:   aws.String("Average"),
			},
			ReturnData: aws.Bool(true),
		})
	}
	results, err := getMetricData(ctx, api, queries, start, now)
	if err != nil {
		return nil, err
	}
	for qid, r := range results {
		hourly := map[int64]float64{}
		for j, ts := range r.Timestamps {
			if j < len(r.Values) {
				hourly[ts.Truncate(time.Hour).Unix()] = round(r.Values[j])
			}
		}
		if len(hourly) > 0 {
			out[byQuery[qid]] = cloud.CPUStat{Hourly: hourly}
		}
	}
	return out, nil
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func round(v float64) float64 { return math.Round(v*1000) / 1000 }
