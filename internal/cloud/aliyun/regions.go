package aliyun

import (
	"fmt"
	"strings"
	"time"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
)

// notOffered lists regions where Alibaba Cloud does not provide a product.
// ECS lists these regions and the SDK still builds an endpoint for them, but
// the endpoint only closes the connection (EOF), so collectors skip them
// without calling the API.
var notOffered = map[string]map[string]bool{
	"rds": {"eu-west-3": true},
	"alb": {"cn-huhehaote": true, "eu-west-3": true},
}

// skipRegion returns ErrRegionUnsupported when product is not offered in region.
func skipRegion(product, region string) error {
	if notOffered[product][region] {
		return cloud.Wrap(cloud.ErrRegionUnsupported, fmt.Errorf("阿里云未在 %s 提供 %s", region, strings.ToUpper(product)))
	}
	return nil
}

var regionNames = map[string]string{
	"cn-qingdao":     "华北1（青岛）",
	"cn-beijing":     "华北2（北京）",
	"cn-zhangjiakou": "华北3（张家口）",
	"cn-huhehaote":   "华北5（呼和浩特）",
	"cn-wulanchabu":  "华北6（乌兰察布）",
	"cn-hangzhou":    "华东1（杭州）",
	"cn-shanghai":    "华东2（上海）",
	"cn-nanjing":     "华东5（南京）",
	"cn-fuzhou":      "华东6（福州）",
	"cn-wuhan-lr":    "华中1（武汉）",
	"cn-shenzhen":    "华南1（深圳）",
	"cn-heyuan":      "华南2（河源）",
	"cn-guangzhou":   "华南3（广州）",
	"cn-chengdu":     "西南1（成都）",
	"cn-hongkong":    "中国香港",
	"ap-northeast-1": "日本（东京）",
	"ap-northeast-2": "韩国（首尔）",
	"ap-southeast-1": "新加坡",
	"ap-southeast-3": "马来西亚（吉隆坡）",
	"ap-southeast-5": "印度尼西亚（雅加达）",
	"ap-southeast-6": "菲律宾（马尼拉）",
	"ap-southeast-7": "泰国（曼谷）",
	"us-east-1":      "美国（弗吉尼亚）",
	"us-west-1":      "美国（硅谷）",
	"eu-west-1":      "英国（伦敦）",
	"eu-central-1":   "德国（法兰克福）",
	"me-east-1":      "阿联酋（迪拜）",
	"me-central-1":   "沙特（利雅得）",
}

// RegionName returns the Chinese display name of an Alibaba Cloud region.
func RegionName(id string) string {
	if n, ok := regionNames[id]; ok {
		return n
	}
	return id
}

func regionOrder(id string) string {
	o := "9"
	switch {
	case len(id) >= 3 && id[:3] == "cn-":
		o = "0"
	case len(id) >= 3 && id[:3] == "ap-":
		o = "1"
	case len(id) >= 3 && id[:3] == "us-":
		o = "2"
	case len(id) >= 3 && id[:3] == "eu-":
		o = "3"
	}
	return o + id
}

// parseTime handles the formats used by Alibaba Cloud APIs.
func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{"2006-01-02T15:04Z", "2006-01-02T15:04:05Z", time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// parseExpire ignores the far-future placeholder used by pay-as-you-go items.
func parseExpire(s string) *time.Time {
	t := parseTime(s)
	if t == nil || t.Year() >= 2099 {
		return nil
	}
	return t
}
