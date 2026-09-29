package cloud

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
)

// Error kinds returned by providers. Wrap them with fmt.Errorf("%w: ...").
var (
	// ErrAuth: the access key is invalid, disabled or the signature is wrong.
	ErrAuth = errors.New("凭证无效")
	// ErrPermission: the credential lacks permission for the call.
	ErrPermission = errors.New("权限不足")
	// ErrRegionUnsupported: the product is not available in the region or the
	// region is not enabled for the account. Sync treats it as "skipped".
	ErrRegionUnsupported = errors.New("该地域不支持此产品")
	// ErrNetwork: the endpoint could not be reached.
	ErrNetwork = errors.New("网络不可达")
	// ErrThrottled: the provider rate limited the request.
	ErrThrottled = errors.New("请求过于频繁")
)

// Wrap attaches a kind to a provider error, keeping the original message.
func Wrap(kind, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %s", kind, strings.TrimSpace(err.Error()))
}

// IsNetwork reports DNS/connection failures that are not API responses.
func IsNetwork(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// Describe turns a provider error into a short, user-facing message.
func Describe(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrAuth):
		return "AccessKey 无效、已禁用或签名错误：" + detail(err)
	case errors.Is(err, ErrPermission):
		return "权限不足，请为该 AccessKey 授予只读权限：" + detail(err)
	case errors.Is(err, ErrRegionUnsupported):
		return "该地域不支持此产品"
	case errors.Is(err, ErrThrottled):
		return "请求过于频繁，已被云厂商限流，请稍后重试"
	case errors.Is(err, ErrNetwork), IsNetwork(err):
		return "网络请求失败：" + detail(err)
	default:
		return detail(err)
	}
}

var kinds = []error{ErrAuth, ErrPermission, ErrRegionUnsupported, ErrNetwork, ErrThrottled}

// detail strips the kind prefixes added by Wrap and shortens the message.
func detail(err error) string {
	msg := err.Error()
	for _, k := range kinds {
		msg = strings.ReplaceAll(msg, k.Error()+": ", "")
	}
	return shorten(msg, 240)
}

func shorten(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
