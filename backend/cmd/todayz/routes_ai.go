package main

import (
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/platform/httpx"
)

// mountAI mounts the ai domain's routes. Only the work package that builds
// internal/ai edits this file (D-50). W3-10 serves no route of its own: each
// feature mounts its draft endpoints under its own prefix (04 §2.8) and builds
// them from aiKitFor(d).
func mountAI(r chi.Router, d *deps) {}

// aiProviderTimeout bounds one provider call; a feature with a tighter budget
// (search assist, 3 s) sets a context deadline.
const aiProviderTimeout = 30 * time.Second //settings:ok an HTTP client guard, not a product number

// aiKit is the AI platform a feature package is built from (W3-10).
type aiKit struct {
	LLM    client.LLM // metered Anthropic client (the mock in local and test)
	Guard  *guard.Guard
	Drafts *drafts.Service
	stt    client.STT
	d      *deps
}

// STT is the metered speech client for feature (voice_stock or voice_search).
func (k *aiKit) STT(feature string) client.STT {
	return client.NewMeteredSTT(k.stt, k.d.Clock, feature)
}

var (
	aiKitsMu sync.Mutex
	aiKits   = map[*deps]*aiKit{}
)

// aiKitFor returns the kit for d, built once, so every feature shares one
// guard (one cost gauge) and one HTTP client.
func aiKitFor(d *deps) *aiKit {
	aiKitsMu.Lock()
	defer aiKitsMu.Unlock()
	if k, ok := aiKits[d]; ok {
		return k
	}
	p := d.Config.Providers
	hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: d.Config.NoEgress, Timeout: aiProviderTimeout})
	k := &aiKit{
		LLM:    client.NewMetered(client.NewAnthropic(p.LLMAPIURL, p.AnthropicAPIKey, hc), d.Clock),
		Guard:  guard.New(d.DB, d.Settings, d.Clock, guard.ProvidersFrom(d.Config)),
		Drafts: drafts.New(d.DB, d.Clock),
		stt:    client.NewSarvam(p.STTAPIURL, p.SarvamAPIKey, hc),
		d:      d,
	}
	aiKits[d] = k
	return k
}
