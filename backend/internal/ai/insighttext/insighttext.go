// Package insighttext is AI-R4 (11 §1, W3-11): one plain sentence per Demand
// insights row in the shop's language. The rows and their numbers come from
// internal/insights/facts; the model only words them. A sentence is kept in
// insight_texts under a hash of the row's facts, so a changed number never
// shows an old sentence, and is served only while the hash matches. A row
// without a kept sentence shows the client's template; the missing sentences
// are written in the background (one batched model call per store), never on
// the request. A sentence with a number that is not in the row's facts is
// dropped (rule 6).
//
// TODO(lead): the sentence is in the store's language (stores.lang), not the
// viewer's; a staff member reading another language sees the store's. The
// suggested offer % for slow items is not written: the backend limits for it
// are not in 01 yet, so the template's offer link stays without a %.
package insighttext

import (
	"sync"
	"time"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/settings"
)

// Feature is the AI feature name.
const Feature = "insight_text"

const (
	// MaxText is one sentence (the contract allows 400).
	MaxText = 200 //settings:ok the prompt's sentence bound, under Insight.text maxLength 400
	// MaxRows is the most rows one call words (the B-12 tab's page).
	MaxRows = 20 //settings:ok one model call's batch, the common Limit default
	// retryAfter keeps a store from asking again while a run is recent.
	retryAfter = 10 * time.Minute
	runTimeout = 90 * time.Second
)

// Deps is what the service needs.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	LLM      client.LLM
	Guard    *guard.Guard
	// Go runs a background run (tests run it inline); nil is a goroutine.
	Go func(func())
}

// Service implements insights.Texter.
type Service struct {
	d     Deps
	mu    sync.Mutex
	tried map[string]time.Time // store id → last run start
}

// New builds the service.
func New(d Deps) *Service {
	if d.Go == nil {
		d.Go = func(f func()) { go f() }
	}
	return &Service{d: d, tried: map[string]time.Time{}}
}
