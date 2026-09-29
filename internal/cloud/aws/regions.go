package aws

var regionNames = map[string]string{
	"us-east-1":      "美国东部（弗吉尼亚北部）",
	"us-east-2":      "美国东部（俄亥俄）",
	"us-west-1":      "美国西部（加利福尼亚北部）",
	"us-west-2":      "美国西部（俄勒冈）",
	"af-south-1":     "非洲（开普敦）",
	"ap-east-1":      "亚太地区（香港）",
	"ap-east-2":      "亚太地区（台北）",
	"ap-south-1":     "亚太地区（孟买）",
	"ap-south-2":     "亚太地区（海得拉巴）",
	"ap-southeast-1": "亚太地区（新加坡）",
	"ap-southeast-2": "亚太地区（悉尼）",
	"ap-southeast-3": "亚太地区（雅加达）",
	"ap-southeast-4": "亚太地区（墨尔本）",
	"ap-southeast-5": "亚太地区（马来西亚）",
	"ap-southeast-7": "亚太地区（泰国）",
	"ap-northeast-1": "亚太地区（东京）",
	"ap-northeast-2": "亚太地区（首尔）",
	"ap-northeast-3": "亚太地区（大阪）",
	"ca-central-1":   "加拿大（中部）",
	"ca-west-1":      "加拿大西部（卡尔加里）",
	"eu-central-1":   "欧洲（法兰克福）",
	"eu-central-2":   "欧洲（苏黎世）",
	"eu-west-1":      "欧洲（爱尔兰）",
	"eu-west-2":      "欧洲（伦敦）",
	"eu-west-3":      "欧洲（巴黎）",
	"eu-south-1":     "欧洲（米兰）",
	"eu-south-2":     "欧洲（西班牙）",
	"eu-north-1":     "欧洲（斯德哥尔摩）",
	"il-central-1":   "以色列（特拉维夫）",
	"me-south-1":     "中东（巴林）",
	"me-central-1":   "中东（阿联酋）",
	"mx-central-1":   "墨西哥（中部）",
	"sa-east-1":      "南美洲（圣保罗）",
	"cn-north-1":     "中国（北京）",
	"cn-northwest-1": "中国（宁夏）",
}

// RegionName returns the Chinese display name of an AWS region.
func RegionName(id string) string {
	if n, ok := regionNames[id]; ok {
		return n
	}
	return id
}

var regionPrefixOrder = map[string]int{"us": 0, "ca": 1, "sa": 2, "mx": 3, "eu": 4, "ap": 5, "cn": 6, "me": 7, "il": 8, "af": 9}

// regionOrder sorts regions by geography, then name.
func regionOrder(id string) string {
	o := 99
	if len(id) >= 2 {
		if v, ok := regionPrefixOrder[id[:2]]; ok {
			o = v
		}
	}
	return string(rune('A'+o)) + id
}
