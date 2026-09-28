package aliyun

import (
	"testing"

	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func TestMetricDefinitionsSeparateCLBAndALB(t *testing.T) {
	tests := []struct {
		resource  model.Resource
		metric    string
		namespace string
		name      string
		unit      string
	}{
		{model.Resource{Type: "vm"}, "network_in", "acs_ecs_dashboard", "InternetInRate", "Byte/s"},
		{model.Resource{Type: "rds"}, "cpu", "acs_rds_dashboard", "CpuUsage", "%"},
		{model.Resource{Type: "lb", Spec: "CLB"}, "connections", "acs_slb_dashboard", "InstanceActiveConnection", "个"},
		{model.Resource{Type: "lb", Spec: "ALB"}, "qps", "acs_alb", "LoadBalancerQPS", "次/s"},
		{model.Resource{Type: "oss"}, "storage", "acs_oss_dashboard", "MeteringStorageUtilization", "GB"},
	}
	for _, test := range tests {
		namespace, name, _, unit := aliyunMetric(test.resource, test.metric)
		if namespace != test.namespace || name != test.name || unit != test.unit {
			t.Errorf("%s/%s = %s %s %s", test.resource.Type, test.metric, namespace, name, unit)
		}
	}
}
