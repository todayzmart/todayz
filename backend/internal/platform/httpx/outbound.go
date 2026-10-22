package httpx

import (
	"net/http"
	"time"

	"github.com/todayz/backend/internal/platform/egress"
)

// ErrEgressBlocked is returned for a non-loopback call while egress is blocked.
var ErrEgressBlocked = egress.ErrBlocked

// OutboundOptions configure NewOutboundClient.
type OutboundOptions struct {
	// NoEgress is config.Config.NoEgress: TODAYZ_NO_EGRESS=1 or APP_ENV=test.
	NoEgress bool
	// Timeout is the whole-request timeout (default 10 s).
	Timeout time.Duration
}

// NewOutboundClient is the only way backend code makes outbound HTTP calls
// (SMS, WhatsApp, push, Turnstile, STT, LLM, geocoder, OTLP). With NoEgress it
// reaches loopback only (the local mocks) and refuses everything else before
// DNS and again at dial time (SC-PL-04, D-17).
func NewOutboundClient(o OutboundOptions) *http.Client {
	return egress.NewClient(egress.Options{Block: o.NoEgress, Timeout: o.Timeout})
}
