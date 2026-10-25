package insights

import (
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	insightsapi "github.com/todayz/backend/gen/api/insights"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/insights/facts"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Ops maps every insights operation to the action the engine checks (slot
// 15) in the path's business and store: owner and manager only.
var Ops = map[string]string{
	"GetStoreOverview":    policy.InsightsView,
	"ListStoreInsights":   policy.InsightsView,
	"DismissStoreInsight": policy.InsightsView,
	"GetStoreAnalytics":   policy.InsightsView,
	"ListStoreCustomers":  policy.CustomersView,
}

// Register mounts the insights routes on r. az is the permission engine; it
// may be nil only in a route-only test, where every call is then refused.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	insightsapi.Register(r, handlers{s},
		insightsapi.StrictMiddlewareFunc(authz.Strict[insightsapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var _ insightsapi.StrictServerInterface = handlers{}

func (h handlers) GetStoreOverview(ctx context.Context, req insightsapi.GetStoreOverviewRequestObject) (insightsapi.GetStoreOverviewResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	r := insightsapi.OverviewRangeToday
	if req.Params.Range != nil {
		r = *req.Params.Range
	}
	o, err := h.s.Overview(ctx, p.UserID, store, r)
	if err != nil {
		return nil, asProblem(err)
	}
	return insightsapi.GetStoreOverview200JSONResponse(o), nil
}

func (h handlers) ListStoreInsights(ctx context.Context, req insightsapi.ListStoreInsightsRequestObject) (insightsapi.ListStoreInsightsResponseObject, error) {
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	o := ListOpts{Limit: req.Params.Limit, Cursor: req.Params.Cursor}
	o.All = req.Params.All != nil && *req.Params.All == insightsapi.N1
	if req.Params.Kind != nil {
		k, ok := facts.KindOf(string(*req.Params.Kind))
		if !ok {
			return nil, fieldErr("query.kind", "not an insight kind")
		}
		o.Kind = &k
	}
	l, err := h.s.List(ctx, store, o)
	if err != nil {
		return nil, asProblem(err)
	}
	return insightsapi.ListStoreInsights200JSONResponse(l), nil
}

func (h handlers) DismissStoreInsight(ctx context.Context, req insightsapi.DismissStoreInsightRequestObject) (insightsapi.DismissStoreInsightResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	until, err := h.s.Dismiss(ctx, p.UserID, store, string(req.Body.Kind), req.Body.SubjectRef)
	if err != nil {
		return nil, asProblem(err)
	}
	return insightsapi.DismissStoreInsight200JSONResponse(insightsapi.InsightDismissed{QuietUntil: until}), nil
}

func (h handlers) GetStoreAnalytics(ctx context.Context, req insightsapi.GetStoreAnalyticsRequestObject) (insightsapi.GetStoreAnalyticsResponseObject, error) {
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	r := insightsapi.AnalyticsRangeN30d
	if req.Params.Range != nil {
		r = *req.Params.Range
	}
	a, err := h.s.Analytics(ctx, store, r, req.Params.Metric)
	if err != nil {
		return nil, asProblem(err)
	}
	return insightsapi.GetStoreAnalytics200JSONResponse(a), nil
}

func (h handlers) ListStoreCustomers(ctx context.Context, req insightsapi.ListStoreCustomersRequestObject) (insightsapi.ListStoreCustomersResponseObject, error) {
	store, err := pathID(req.Sid)
	if err != nil {
		return nil, err
	}
	seg := insightsapi.All
	if req.Params.Segment != nil {
		seg = *req.Params.Segment
	}
	l, err := h.s.Customers(ctx, store, seg, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return insightsapi.ListStoreCustomers200JSONResponse(l), nil
}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, problem.New(genproblems.NotFound, "")
	}
	return id, nil
}

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}
