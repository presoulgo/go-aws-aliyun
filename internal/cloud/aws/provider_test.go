package aws

import (
	"testing"

	ct "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/presoulgo/go-aws-aliyun/internal/model"
)

func TestMetricDefinitionsUseCloudWatchStatisticsAndUnits(t *testing.T) {
	tests := []struct {
		resource model.Resource
		metric   string
		name     string
		unit     string
		stat     ct.Statistic
	}{
		{model.Resource{Type: "vm"}, "cpu", "CPUUtilization", "%", ct.StatisticAverage},
		{model.Resource{Type: "vm"}, "network_in", "NetworkIn", "Byte/s", ct.StatisticSum},
		{model.Resource{Type: "rds"}, "storage", "FreeStorageSpace", "GB", ct.StatisticAverage},
		{model.Resource{Type: "lb", Spec: "ALB"}, "qps", "RequestCount", "次/s", ct.StatisticSum},
		{model.Resource{Type: "lb", Spec: "NLB"}, "connections", "ActiveFlowCount", "个", ct.StatisticAverage},
	}
	for _, test := range tests {
		_, name, _, unit, statistic := awsMetric(test.resource, test.metric)
		if name != test.name || unit != test.unit || statistic != test.stat {
			t.Errorf("%s/%s = %s %s %s", test.resource.Type, test.metric, name, unit, statistic)
		}
	}
}
