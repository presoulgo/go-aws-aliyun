package cloud

import (
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

// Metric units; the web UI formats values based on them.
const (
	UnitPercent   = "percent"
	UnitBitsPerS  = "bit/s"
	UnitBytesPerS = "byte/s"
	UnitCount     = "count"
	UnitCountPerS = "count/s"
	UnitBytes     = "byte"
)

// Metric keys.
const (
	MetricCPU         = "cpu_util"
	MetricMem         = "mem_util"
	MetricNetIn       = "net_in"
	MetricNetOut      = "net_out"
	MetricDiskRead    = "disk_read"
	MetricDiskWrite   = "disk_write"
	MetricFreeMem     = "free_mem"
	MetricDiskUtil    = "disk_util"
	MetricConnections = "connections"
	MetricQPS         = "qps"
	MetricActiveConn  = "active_conn"
	MetricNewConn     = "new_conn"
	MetricTraffic     = "traffic"
)

// MetricDef is a standard metric shared by all clouds.
type MetricDef struct {
	Type      string   `json:"type"`
	Key       string   `json:"key"`
	Name      string   `json:"name"`
	Unit      string   `json:"unit"`
	Providers []string `json:"providers"`
	Note      string   `json:"note,omitempty"`
}

var both = []string{model.ProviderAWS, model.ProviderAliyun}

// Catalog lists every standard metric.
var Catalog = []MetricDef{
	{Type: model.TypeVM, Key: MetricCPU, Name: "CPU 使用率", Unit: UnitPercent, Providers: both},
	{Type: model.TypeVM, Key: MetricMem, Name: "内存使用率", Unit: UnitPercent, Providers: both, Note: "AWS 需安装 CloudWatch Agent，阿里云需安装云监控插件"},
	{Type: model.TypeVM, Key: MetricNetIn, Name: "网络入带宽", Unit: UnitBitsPerS, Providers: both},
	{Type: model.TypeVM, Key: MetricNetOut, Name: "网络出带宽", Unit: UnitBitsPerS, Providers: both},
	{Type: model.TypeVM, Key: MetricDiskRead, Name: "磁盘读吞吐", Unit: UnitBytesPerS, Providers: both},
	{Type: model.TypeVM, Key: MetricDiskWrite, Name: "磁盘写吞吐", Unit: UnitBytesPerS, Providers: both},

	{Type: model.TypeRDS, Key: MetricCPU, Name: "CPU 使用率", Unit: UnitPercent, Providers: both},
	{Type: model.TypeRDS, Key: MetricDiskUtil, Name: "磁盘使用率", Unit: UnitPercent, Providers: both},
	{Type: model.TypeRDS, Key: MetricMem, Name: "内存使用率", Unit: UnitPercent, Providers: []string{model.ProviderAliyun}},
	{Type: model.TypeRDS, Key: MetricFreeMem, Name: "可用内存", Unit: UnitBytes, Providers: []string{model.ProviderAWS}},
	{Type: model.TypeRDS, Key: MetricConnections, Name: "连接数", Unit: UnitCount, Providers: []string{model.ProviderAWS}},

	{Type: model.TypeLB, Key: MetricQPS, Name: "QPS", Unit: UnitCountPerS, Providers: both, Note: "仅七层负载均衡提供"},
	{Type: model.TypeLB, Key: MetricActiveConn, Name: "活跃连接数", Unit: UnitCount, Providers: both},
	{Type: model.TypeLB, Key: MetricNewConn, Name: "新建连接数", Unit: UnitCountPerS, Providers: both},
	{Type: model.TypeLB, Key: MetricTraffic, Name: "流量", Unit: UnitBitsPerS, Providers: both},
}

// MetricsFor returns the catalog entries of a resource type.
func MetricsFor(typ string) []MetricDef {
	var out []MetricDef
	for _, m := range Catalog {
		if m.Type == typ {
			out = append(out, m)
		}
	}
	return out
}

// LookupMetric finds a catalog entry.
func LookupMetric(typ, key string) (MetricDef, bool) {
	for _, m := range Catalog {
		if m.Type == typ && m.Key == key {
			return m, true
		}
	}
	return MetricDef{}, false
}

// RangeSpec maps a UI time range to a query window and granularity.
type RangeSpec struct {
	Key      string        `json:"key"`
	Duration time.Duration `json:"-"`
	Period   time.Duration `json:"-"`
}

var ranges = map[string]RangeSpec{
	"1h":  {Key: "1h", Duration: time.Hour, Period: time.Minute},
	"6h":  {Key: "6h", Duration: 6 * time.Hour, Period: 5 * time.Minute},
	"24h": {Key: "24h", Duration: 24 * time.Hour, Period: 15 * time.Minute},
	"7d":  {Key: "7d", Duration: 7 * 24 * time.Hour, Period: time.Hour},
}

// LookupRange returns the spec for "1h", "6h", "24h" or "7d".
func LookupRange(key string) (RangeSpec, bool) {
	r, ok := ranges[key]
	return r, ok
}

// LBKind is stored in Resource.Extra["lb_kind"] for load balancers.
const (
	LBKindALB = "alb" // AWS application LB / Alibaba Cloud ALB (layer 7)
	LBKindNLB = "nlb" // AWS network LB (layer 4)
	LBKindGWL = "gwlb"
	LBKindCLB = "clb" // Alibaba Cloud classic LB (layer 4 and/or 7)
)

// ExtraString reads a string from Resource.Extra.
func ExtraString(extra map[string]any, key string) string {
	if extra == nil {
		return ""
	}
	if v, ok := extra[key].(string); ok {
		return v
	}
	return ""
}

// ExtraFloat reads a number from Resource.Extra (JSON numbers decode as float64).
func ExtraFloat(extra map[string]any, key string) (float64, bool) {
	if extra == nil {
		return 0, false
	}
	switch v := extra[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	}
	return 0, false
}

// ExtraBool reads a bool from Resource.Extra.
func ExtraBool(extra map[string]any, key string) bool {
	if extra == nil {
		return false
	}
	b, _ := extra[key].(bool)
	return b
}
