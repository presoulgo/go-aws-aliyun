package aliyun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	cms "github.com/alibabacloud-go/cms-20190101/v10/client"
	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

type cmsAPI interface {
	DescribeMetricListWithContext(ctx context.Context, req *cms.DescribeMetricListRequest, rt *dara.RuntimeOptions) (*cms.DescribeMetricListResponse, error)
}

type cmsSource struct {
	namespace string
	metric    string
	dimKey    string
	// minPeriod is the reporting period of metrics published less often than
	// every minute; they are never queried at a finer period.
	minPeriod time.Duration
	// perMinute sources report an amount per minute (requests, bytes). They are
	// read per minute and summed into a per-second rate over each point,
	// multiplied by scale (8 turns bytes into bits).
	perMinute bool
	scale     float64
}

type cmsPlan struct {
	sources []cmsSource
}

func planFor(ref cloud.ResourceRef, key string) (cmsPlan, bool) {
	s := func(ns, dimKey string, metrics ...string) (cmsPlan, bool) {
		plan := cmsPlan{}
		for _, m := range metrics {
			plan.sources = append(plan.sources, cmsSource{namespace: ns, metric: m, dimKey: dimKey})
		}
		return plan, true
	}
	switch ref.Type {
	case model.TypeVM:
		const ns, dk = "acs_ecs_dashboard", "instanceId"
		switch key {
		case cloud.MetricCPU:
			return s(ns, dk, "CPUUtilization")
		case cloud.MetricMem:
			return s(ns, dk, "memory_usedutilization")
		case cloud.MetricNetIn:
			return s(ns, dk, "IntranetInRate", "InternetInRate", "VPC_PublicIP_InternetInRate")
		case cloud.MetricNetOut:
			return s(ns, dk, "IntranetOutRate", "InternetOutRate", "VPC_PublicIP_InternetOutRate")
		case cloud.MetricDiskRead:
			return s(ns, dk, "DiskReadBPS")
		case cloud.MetricDiskWrite:
			return s(ns, dk, "DiskWriteBPS")
		}
	case model.TypeRDS:
		const ns, dk = "acs_rds_dashboard", "instanceId"
		switch key {
		case cloud.MetricCPU:
			return s(ns, dk, "CpuUsage")
		case cloud.MetricMem:
			return s(ns, dk, "MemoryUsage")
		case cloud.MetricDiskUtil:
			return s(ns, dk, "DiskUsage")
		}
	case model.TypeLB:
		switch cloud.ExtraString(ref.Extra, "lb_kind") {
		case cloud.LBKindCLB:
			const ns, dk = "acs_slb_dashboard", "instanceId"
			switch key {
			case cloud.MetricQPS:
				return s(ns, dk, "InstanceQps")
			case cloud.MetricActiveConn:
				return s(ns, dk, "InstanceActiveConnection")
			case cloud.MetricNewConn:
				return s(ns, dk, "InstanceNewConnection")
			case cloud.MetricTraffic:
				return s(ns, dk, "InstanceTrafficRX", "InstanceTrafficTX")
			}
		case cloud.LBKindALB:
			const ns, dk = "acs_alb", "loadBalancerId"
			switch key {
			case cloud.MetricQPS:
				return s(ns, dk, "LoadBalancerQPS")
			case cloud.MetricActiveConn:
				return s(ns, dk, "LoadBalancerActiveConnection")
			case cloud.MetricNewConn:
				return s(ns, dk, "LoadBalancerNewConnection")
			case cloud.MetricTraffic:
				return s(ns, dk, "LoadBalancerInBits", "LoadBalancerOutBits")
			}
		}
	case model.TypeBucket:
		const ns, dk = "acs_oss_dashboard", "BucketName"
		rate := func(metric string, scale float64) (cmsPlan, bool) {
			return cmsPlan{sources: []cmsSource{{namespace: ns, metric: metric, dimKey: dk, perMinute: true, scale: scale}}}, true
		}
		switch key {
		case cloud.MetricStorage:
			return cmsPlan{sources: []cmsSource{{namespace: ns, metric: "MeteringStorageUtilization", dimKey: dk, minPeriod: time.Hour}}}, true
		case cloud.MetricRequests:
			return rate("TotalRequestCount", 1)
		case cloud.MetricNetIn:
			return rate("InternetRecv", 8)
		case cloud.MetricNetOut:
			return rate("InternetSend", 8)
		}
	}
	return cmsPlan{}, false
}

// SupportedMetrics lists the catalog keys CloudMonitor can provide for ref.
func SupportedMetrics(ref cloud.ResourceRef) []string {
	var out []string
	for _, m := range cloud.MetricsFor(ref.Type) {
		if _, ok := planFor(ref, m.Key); ok {
			out = append(out, m.Key)
		}
	}
	return out
}

type datapoint struct {
	Timestamp  int64    `json:"timestamp"`
	InstanceID string   `json:"instanceId"`
	LBID       string   `json:"loadBalancerId"`
	Average    *float64 `json:"Average"`
	Value      *float64 `json:"Value"`
	Sum        *float64 `json:"Sum"`
	Maximum    *float64 `json:"Maximum"`
}

func (d datapoint) value() (float64, bool) {
	switch {
	case d.Average != nil:
		return *d.Average, true
	case d.Value != nil:
		return *d.Value, true
	case d.Sum != nil:
		return *d.Sum, true
	case d.Maximum != nil:
		return *d.Maximum, true
	}
	return 0, false
}

// describeMetricList fetches all pages of one metric.
func describeMetricList(ctx context.Context, api cmsAPI, region, ns, metric, dims string, period time.Duration, start, end time.Time) ([]datapoint, error) {
	req := &cms.DescribeMetricListRequest{
		RegionId:   dara.String(region),
		Namespace:  dara.String(ns),
		MetricName: dara.String(metric),
		Dimensions: dara.String(dims),
		Period:     dara.String(strconv.Itoa(int(period / time.Second))),
		StartTime:  dara.String(strconv.FormatInt(start.UnixMilli(), 10)),
		EndTime:    dara.String(strconv.FormatInt(end.UnixMilli(), 10)),
		Length:     dara.String("1440"),
	}
	var out []datapoint
	for page := 0; page < 100; page++ {
		resp, err := api.DescribeMetricListWithContext(ctx, req, runtime())
		if err != nil {
			return nil, classify(err)
		}
		if resp == nil || resp.Body == nil {
			break
		}
		if resp.Body.Success != nil && !*resp.Body.Success {
			return nil, classify(fmt.Errorf("云监控返回错误 %s: %s", dara.StringValue(resp.Body.Code), dara.StringValue(resp.Body.Message)))
		}
		if raw := dara.StringValue(resp.Body.Datapoints); raw != "" && raw != "[]" {
			var dps []datapoint
			if err := json.Unmarshal([]byte(raw), &dps); err != nil {
				return nil, fmt.Errorf("解析云监控数据失败: %w", err)
			}
			out = append(out, dps...)
		}
		next := dara.StringValue(resp.Body.NextToken)
		if next == "" {
			break
		}
		req.NextToken = dara.String(next)
	}
	return out, nil
}

func dimensions(key string, ids ...string) string {
	list := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		list = append(list, map[string]string{key: id})
	}
	if len(list) == 1 {
		b, _ := json.Marshal(list[0])
		return "[" + string(b) + "]"
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func queryMetrics(ctx context.Context, api cmsAPI, ref cloud.ResourceRef, q cloud.MetricQuery) ([]cloud.Series, error) {
	period := q.Period
	if period < time.Minute {
		period = time.Minute
	}
	var out []cloud.Series
	for _, key := range q.Keys {
		plan, ok := planFor(ref, key)
		if !ok {
			continue
		}
		sums := map[int64]float64{}
		for _, src := range plan.sources {
			p, start := period, q.Start
			switch {
			case src.perMinute:
				p, start = time.Minute, q.Start.Truncate(period)
			case src.minPeriod > p:
				p = src.minPeriod
			}
			dps, err := describeMetricList(ctx, api, ref.Region, src.namespace, src.metric, dimensions(src.dimKey, ref.ResourceID), p, start, q.End)
			if err != nil {
				// A metric the resource does not publish is not fatal.
				if errors.Is(err, cloud.ErrRegionUnsupported) {
					continue
				}
				return nil, err
			}
			step, end := period.Milliseconds(), q.End.UnixMilli()
			for _, dp := range dps {
				v, ok := dp.value()
				if !ok {
					continue
				}
				ts := dp.Timestamp
				if src.perMinute {
					// Sum the minutes of each point into a per-second rate; the
					// current, unfinished point only counts the time elapsed.
					ts -= ts % step
					span := min(ts+step, end) - ts
					if span <= 0 {
						continue
					}
					v = v * src.scale / (float64(span) / 1000)
				}
				sums[ts] += v
			}
		}
		def, _ := cloud.LookupMetric(ref.Type, key)
		points := make([]cloud.Point, 0, len(sums))
		for ts, v := range sums {
			points = append(points, cloud.Point{float64(ts), math.Round(v*1000) / 1000})
		}
		sort.Slice(points, func(i, j int) bool { return points[i][0] < points[j][0] })
		out = append(out, cloud.Series{Key: key, Unit: def.Unit, Points: points})
	}
	return out, nil
}

// cpuSnapshot reads hourly CPUUtilization for up to 50 instances per call.
func cpuSnapshot(ctx context.Context, api cmsAPI, region string, ids []string, now time.Time) (map[string]cloud.CPUStat, error) {
	out := make(map[string]cloud.CPUStat, len(ids))
	start := now.Truncate(time.Hour).Add(-23 * time.Hour)
	for from := 0; from < len(ids); from += 50 {
		batch := ids[from:min(from+50, len(ids))]
		dps, err := describeMetricList(ctx, api, region, "acs_ecs_dashboard", "CPUUtilization", dimensions("instanceId", batch...), time.Hour, start, now)
		if err != nil {
			return nil, err
		}
		for _, dp := range dps {
			v, ok := dp.value()
			if !ok || dp.InstanceID == "" {
				continue
			}
			st, exists := out[dp.InstanceID]
			if !exists {
				st = cloud.CPUStat{Hourly: map[int64]float64{}}
				out[dp.InstanceID] = st
			}
			st.Hourly[time.UnixMilli(dp.Timestamp).Truncate(time.Hour).Unix()] = math.Round(v*1000) / 1000
		}
	}
	return out, nil
}
