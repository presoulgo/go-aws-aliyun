package cloud

import (
	"context"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
	"time"
)

type Point struct {
	Time  time.Time `json:"time"`
	Value float64   `json:"value"`
}
type Series struct {
	ResourceID uint    `json:"resource_id"`
	Metric     string  `json:"metric"`
	Name       string  `json:"name"`
	Provider   string  `json:"provider"`
	Unit       string  `json:"unit"`
	Supported  bool    `json:"supported"`
	Points     []Point `json:"points"`
	Current    float64 `json:"current"`
	Average    float64 `json:"average"`
	Max        float64 `json:"max"`
	Error      string  `json:"error,omitempty"`
}
type Identity struct {
	UID     string   `json:"uid"`
	Regions []string `json:"regions"`
}
type Provider interface {
	Validate(context.Context, model.CloudAccount, string) (Identity, error)
	List(context.Context, model.CloudAccount, string, string, string) ([]model.Resource, error)
	Metrics(context.Context, model.CloudAccount, string, model.Resource, string, time.Duration) (Series, error)
}

var Catalog = []map[string]any{
	{"key": "cpu", "name": "CPU 使用率", "unit": "%", "types": []string{"vm", "rds"}, "providers": []string{"aws", "aliyun"}},
	{"key": "memory", "name": "内存使用率", "unit": "%", "types": []string{"vm", "rds"}, "providers": []string{"aliyun"}},
	{"key": "network_in", "name": "网络流入", "unit": "Byte/s", "types": []string{"vm", "lb"}, "providers": []string{"aws", "aliyun"}},
	{"key": "network_out", "name": "网络流出", "unit": "Byte/s", "types": []string{"vm", "lb"}, "providers": []string{"aws", "aliyun"}},
	{"key": "connections", "name": "连接数", "unit": "个", "types": []string{"rds", "lb"}, "providers": []string{"aws", "aliyun"}},
	{"key": "qps", "name": "请求数", "unit": "次/s", "types": []string{"lb"}, "providers": []string{"aws", "aliyun"}},
	{"key": "storage", "name": "存储空间", "unit": "GB", "types": []string{"rds", "oss"}, "providers": []string{"aws", "aliyun"}},
}

func Supports(r model.Resource, metric string) bool {
	if metric == "qps" && (r.Spec == "NLB" || r.Spec == "CLB") {
		return false
	}
	if r.Provider == "aws" && r.Type == "lb" && (metric == "network_in" || metric == "network_out") {
		// ELB publishes total processed bytes, not separate inbound and outbound byte series.
		return false
	}
	if r.Spec == "GWLB" && (metric == "qps" || metric == "connections") {
		return false
	}
	for _, item := range Catalog {
		if item["key"] != metric {
			continue
		}
		tp, ok := item["types"].([]string)
		if !ok {
			return false
		}
		for _, t := range tp {
			if t == r.Type {
				for _, p := range item["providers"].([]string) {
					if p == r.Provider {
						return true
					}
				}
			}
		}
	}
	return false
}
