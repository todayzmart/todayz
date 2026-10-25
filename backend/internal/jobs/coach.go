package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/coach"
	"github.com/todayz/backend/internal/coach/detectors"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
)

const coachRunbook = "docs/runbooks/coach.md"

// coachProviderTimeout bounds one model call of a coach card.
const coachProviderTimeout = 30 * time.Second //settings:ok an HTTP client guard, not a product number

// registercoach declares the shop coach's three kinds (11 §2.1, W3-11): the
// daily and weekly detector runs (batched over the live stores, D-98) and
// the 15-minute realtime rules. Class ai: they write cards a person acts on,
// never stock, money or orders.
func registercoach(r *Registry, d Deps) {
	var (
		mu  sync.Mutex
		svc *coach.Service
	)
	service := func() (*coach.Service, error) {
		mu.Lock()
		defer mu.Unlock()
		if svc != nil {
			return svc, nil
		}
		if d.Pool == nil {
			return nil, errors.New("coach: no database")
		}
		s, err := CoachService(d)
		if err != nil {
			return nil, err
		}
		svc = s
		return svc, nil
	}
	batched := func(j detectors.Job) platformjobs.Handler {
		return func(ctx context.Context, run platformjobs.Run) (platformjobs.Result, error) {
			s, err := service()
			if err != nil {
				return platformjobs.Result{}, err
			}
			sum, err := s.RunBatched(ctx, j, run.PeriodKey)
			return platformjobs.Result{Summary: sum.Map()}, err
		}
	}
	platformjobs.Define(r, platformjobs.Kind{
		Name: string(detectors.Daily), Class: platformjobs.ClassAI,
		Description: "Runs the coach's daily detectors for every live store and writes the day's top cards (coach_max_cards_per_day), worded by the model or the rule template (11 §2.1)",
		Schedule:    "0 6 * * *", UniqueBy: platformjobs.PerDay,
		Owner: "W3-11", Runbook: coachRunbook, Timeout: 30 * time.Minute, Batched: true, MaxAttempts: 3,
		Handler: batched(detectors.Daily),
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: string(detectors.Weekly), Class: platformjobs.ClassAI,
		Description: "Runs the coach's weekly detectors (summary, bought together, regulars, rating, accept time, setup) every Monday for every live store (11 §2.1)",
		Schedule:    "0 7 * * 1", UniqueBy: platformjobs.PerDay,
		Owner: "W3-11", Runbook: coachRunbook, Timeout: 30 * time.Minute, Batched: true, MaxAttempts: 3,
		Handler: batched(detectors.Weekly),
	})
	platformjobs.Define(r, platformjobs.Kind{
		Name: string(detectors.Realtime), Class: platformjobs.ClassAI,
		Description: "Every 15 minutes, the coach's realtime rules: the expiry-penalty nudge for stores whose 30-day unaccepted-expiry rate is above shop_expiry_penalty_threshold_bp (01 §5, D-46). Rules only",
		Schedule:    "*/15 * * * *", UniqueBy: platformjobs.PerTick,
		Owner: "W3-11", Runbook: coachRunbook, Timeout: time.Minute, MaxAttempts: 2,
		Handler: func(ctx context.Context, _ platformjobs.Run) (platformjobs.Result, error) {
			s, err := service()
			if err != nil {
				return platformjobs.Result{}, err
			}
			sum, err := s.RunRealtime(ctx)
			return platformjobs.Result{Summary: sum.Map()}, err
		},
	})
}

// CoachService builds the coach for the worker. Without provider config the
// model is left out and every card is the rule template.
func CoachService(d Deps) (*coach.Service, error) {
	cd := coach.Deps{Clock: d.Clock, Settings: d.Settings}
	if d.Pool != nil {
		cd.DB = db.New(d.Pool)
	}
	if d.Config != nil && cd.DB != nil {
		p := d.Config.Providers
		hc := httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: d.Config.NoEgress, Timeout: coachProviderTimeout})
		cd.LLM = client.NewMetered(client.NewAnthropic(p.LLMAPIURL, p.AnthropicAPIKey, hc), d.Clock)
		cd.Guard = guard.New(cd.DB, d.Settings, d.Clock, guard.ProvidersFrom(d.Config))
	}
	return coach.New(cd)
}
