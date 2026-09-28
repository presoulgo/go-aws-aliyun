package aws

import (
	"errors"

	"github.com/aws/smithy-go"

	"github.com/presoulgo/go-aws-aliyun/internal/cloud"
)

// classify maps AWS API errors to cloud error kinds.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var ae smithy.APIError
	if errors.As(err, &ae) {
		switch ae.ErrorCode() {
		case "InvalidClientTokenId", "SignatureDoesNotMatch", "AuthFailure", "UnrecognizedClientException",
			"InvalidAccessKeyId", "ExpiredToken", "ExpiredTokenException", "InvalidToken", "IncompleteSignature":
			return cloud.Wrap(cloud.ErrAuth, err)
		case "AccessDenied", "AccessDeniedException", "UnauthorizedOperation", "UnauthorizedAccess",
			"Forbidden", "AllAccessDisabled", "AuthorizationError":
			return cloud.Wrap(cloud.ErrPermission, err)
		case "OptInRequired", "InvalidRegion", "InvalidEndpoint":
			return cloud.Wrap(cloud.ErrRegionUnsupported, err)
		case "Throttling", "ThrottlingException", "RequestLimitExceeded", "TooManyRequestsException", "SlowDown":
			return cloud.Wrap(cloud.ErrThrottled, err)
		}
		return err
	}
	if cloud.IsNetwork(err) {
		return cloud.Wrap(cloud.ErrNetwork, err)
	}
	return err
}
