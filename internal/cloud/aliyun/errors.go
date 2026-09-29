package aliyun

import (
	"errors"
	"net"
	"strings"

	"github.com/alibabacloud-go/tea/dara"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
)

type codedError interface {
	GetCode() *string
}

func errorCode(err error) string {
	var ce codedError
	if errors.As(err, &ce) {
		return dara.StringValue(ce.GetCode())
	}
	return ""
}

// classify maps Alibaba Cloud errors to cloud error kinds.
func classify(err error) error {
	if err == nil {
		return nil
	}
	code := errorCode(err)
	lower := strings.ToLower(code)
	switch {
	case code == "":
	case strings.HasPrefix(code, "InvalidAccessKeyId"), code == "SignatureDoesNotMatch", code == "IncompleteSignature",
		strings.HasPrefix(code, "InvalidSecurityToken"), code == "InvalidAccessKeySecret":
		return cloud.Wrap(cloud.ErrAuth, err)
	case strings.HasPrefix(code, "Forbidden"), code == "NoPermission", code == "AccessDenied",
		strings.Contains(lower, "nopermission"), strings.Contains(lower, "notauthorized"):
		return cloud.Wrap(cloud.ErrPermission, err)
	case strings.Contains(lower, "region") && (strings.Contains(lower, "invalid") || strings.Contains(lower, "notsupport") ||
		strings.Contains(lower, "unsupport") || strings.Contains(lower, "notfound") || strings.Contains(lower, "notopen")):
		return cloud.Wrap(cloud.ErrRegionUnsupported, err)
	case strings.HasPrefix(code, "Throttling"), strings.Contains(lower, "throttling"), code == "ServiceUnavailable.Throttling":
		return cloud.Wrap(cloud.ErrThrottled, err)
	}
	// A product endpoint that does not resolve means the product is not
	// deployed in that region.
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return cloud.Wrap(cloud.ErrRegionUnsupported, err)
	}
	if strings.Contains(err.Error(), "no such host") {
		return cloud.Wrap(cloud.ErrRegionUnsupported, err)
	}
	if cloud.IsNetwork(err) {
		return cloud.Wrap(cloud.ErrNetwork, err)
	}
	return err
}
