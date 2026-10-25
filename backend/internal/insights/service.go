// Package insights serves the business Overview (B-03), Demand insights (the
// B-03 card and the B-12 Insights tab, D-99), the B-12 Sales tab and the B-09
// repeat customers. Every number is computed at read time from the store's
// own sales, stock and searches; the insight kinds themselves are in
// internal/insights/facts, which W3-11's coach imports (D-99). Nothing here
// changes stock, prices or orders: an insight's action is a prefill the
// client opens.
package insights

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/insightsdb"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Fixed numbers of the plan (not settings).
const (
	// defaultLimit and maxLimit are the common Limit parameter's (04 §1).
	defaultLimit = 20  //settings:ok common Limit default in api/common/parameters.yaml
	maxLimit     = 100 //settings:ok common Limit maximum in api/common/parameters.yaml
	// cardRows is the B-03 card: the top 4 firing rows, one per kind (D-99).
	cardRows = 4 //settings:ok 06 B-03 fixes the card at 4 rows
)

// Deps is what the service needs. DB or Settings may be missing in a
// route-only test: every operation then answers 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	// Seal opens the sealed phones shown masked on B-09; nil leaves them out.
	Seal *pii.Sealer
	// Asks adds the unanswered asks to the nearby searches (D-101); nil is
	// zero asks until W3-16's asks.Demand lands.
	Asks facts.AskDemand
}

// Service implements the insights routes.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	seal     *pii.Sealer
	asks     facts.AskDemand
	ids      *ids.Generator
}

var errUnavailable = errors.New("insights: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("insights: a clock is required")
	}
	asks := d.Asks
	if asks == nil {
		asks = facts.NoAsks{}
	}
	return &Service{db: d.DB, clock: d.Clock, settings: d.Settings, seal: d.Seal, asks: asks, ids: ids.NewGenerator()}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "insights: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *insightsdb.Queries { return insightsdb.New(s.db.Q(ctx)) }

func (s *Service) source(ctx context.Context) facts.Source {
	return facts.Source{Q: s.q(ctx), Settings: s.settings, Asks: s.asks}
}

func fieldErr(path, reason string) error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

// page reads the limit and the opaque offset cursor of a list.
func page(limit *int, cursor *string) (lim, off int, err error) {
	lim = defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return 0, 0, fieldErr("query.cursor", "not a cursor from this list")
	}
	n, ok := strings.CutPrefix(string(b), "o:")
	if !ok {
		return 0, 0, fieldErr("query.cursor", "not a cursor from this list")
	}
	off, err = strconv.Atoi(n)
	if err != nil || off < 0 {
		return 0, 0, fieldErr("query.cursor", "not a cursor from this list")
	}
	return lim, off, nil
}

// window cuts [off, off+lim) out of n items and returns the next cursor.
func window(n, lim, off int) (from, to int, next *string) {
	from = min(off, n)
	to = min(from+lim, n)
	if to < n {
		c := base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(to)))
		next = &c
	}
	return from, to, next
}

// shortName is a customer's first name and last initial ("Ananya R."); the
// business never sees a full name it did not type itself.
func shortName(name string) string {
	parts := strings.Fields(name)
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return parts[0] + " " + initial(parts[len(parts)-1]) + "."
}

// initial is the first letter of a word with the marks that belong to it,
// so a Tamil or Hindi initial keeps its vowel sign ("குமார்" → "கு").
func initial(word string) string {
	r, n := utf8.DecodeRuneInString(word)
	out := string(r)
	for _, m := range word[n:] {
		if !unicode.In(m, unicode.Mn, unicode.Mc) {
			break
		}
		out += string(m)
	}
	return out
}

// firstName is the greeting name.
func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return ""
}
