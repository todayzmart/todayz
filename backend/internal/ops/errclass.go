package ops

import (
	"context"
	"errors"
	"regexp"

	"github.com/aws/smithy-go"

	"github.com/todayz/backend/internal/platform/egress"
)

var reErrorCode = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]{0,63}$`)

// ErrorClass names the kind of a panel's failure from fixed words and a
// checked AWS error code only: never the error text, which can carry an ARN,
// an account id or a host.
func ErrorClass(err error) string {
	var ce *credentialsError
	var ae smithy.APIError
	switch {
	case err == nil:
		return ""
	case errors.Is(err, egress.ErrBlocked):
		return "EgressBlocked"
	case errors.As(err, &ce):
		return "Credentials"
	case errors.As(err, &ae) && reErrorCode.MatchString(ae.ErrorCode()):
		return ae.ErrorCode()
	case errors.Is(err, context.DeadlineExceeded):
		return "Timeout"
	case errors.Is(err, context.Canceled):
		return "Cancelled"
	default:
		return "Error"
	}
}
