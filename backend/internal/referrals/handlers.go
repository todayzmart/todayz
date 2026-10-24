package referrals

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	referralsapi "github.com/todayz/backend/gen/api/referrals"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// Ops maps the referrals operations to their authz actions, keyed by the
// generated strict server's method names.
var Ops = map[string]string{
	"OpenReferralLink":      authz.Public,
	"GetMyReferrals":        policy.MeReferralsView,
	"ClaimReferral":         policy.MeReferralsClaim,
	"GetBusinessReferrals":  policy.ReferralsView,
	"ClaimBusinessReferral": policy.ReferralsClaim,
}

// Register mounts the referrals routes behind slot 15. The request is stashed
// in the context last so handlers can read the cookie and user agent.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	referralsapi.Register(r, handlers{s},
		referralsapi.StrictMiddlewareFunc(authz.Strict[referralsapi.StrictHandlerFunc](az, Ops)),
		stashRequest[referralsapi.StrictHandlerFunc])
}

type requestKey struct{}

func stashRequest[H ~func(context.Context, http.ResponseWriter, *http.Request, any) (any, error)](next H, _ string) H {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		return next(context.WithValue(ctx, requestKey{}, r), w, r, req)
	}
}

func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}

func cookieFrom(ctx context.Context) string {
	r := requestFrom(ctx)
	if r == nil {
		return ""
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
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
		return ids.Zero, notFound()
	}
	return id, nil
}

func claimInput(ctx context.Context, b *referralsapi.ReferralClaim) ClaimInput {
	in := ClaimInput{Cookie: cookieFrom(ctx)}
	if b != nil {
		if b.Code != nil {
			in.Code = *b.Code
		}
		if b.Referrer != nil {
			in.As = string(*b.Referrer)
		}
	}
	return in
}

func claimBody(r ClaimResult) referralsapi.ReferralClaimResult {
	return referralsapi.ReferralClaimResult{
		ReferralId: r.ReferralID.String(), Kind: referralsapi.ReferralKind(r.Kind),
		Status: referralsapi.ReferralClaimStatus(r.Status), RewardCredits: r.RewardCredits,
	}
}

type handlers struct{ s *Service }

var _ referralsapi.StrictServerInterface = handlers{}

// OpenReferralLink implements GET /v1/public/ref/{code}.
func (h handlers) OpenReferralLink(ctx context.Context, req referralsapi.OpenReferralLinkRequestObject) (referralsapi.OpenReferralLinkResponseObject, error) {
	c := Click{Code: req.Code, Cookie: cookieFrom(ctx), IP: reqctx.ClientIP(ctx)}
	if req.Params.S != nil {
		c.Standee = *req.Params.S
	}
	if req.Params.K != nil {
		c.Kind = string(*req.Params.K)
	}
	if r := requestFrom(ctx); r != nil {
		c.UserAgent = r.UserAgent()
	}
	l, err := h.s.Open(ctx, c)
	if err != nil {
		return nil, asProblem(err)
	}
	noStore := "no-store"
	out := referralsapi.OpenReferralLink302Response{Headers: referralsapi.OpenReferralLink302ResponseHeaders{
		CacheControl: &noStore, Location: &l.Location,
	}}
	if l.SetCookie != "" {
		out.Headers.SetCookie = &l.SetCookie
	}
	return out, nil
}

// GetMyReferrals implements GET /v1/me/referrals.
func (h handlers) GetMyReferrals(ctx context.Context, _ referralsapi.GetMyReferralsRequestObject) (referralsapi.GetMyReferralsResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	m, err := h.s.Me(ctx, p.UserID)
	if err != nil {
		return nil, err
	}
	return referralsapi.GetMyReferrals200JSONResponse(m), nil
}

// ClaimReferral implements POST /v1/me/referrals/claim.
func (h handlers) ClaimReferral(ctx context.Context, req referralsapi.ClaimReferralRequestObject) (referralsapi.ClaimReferralResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.s.ClaimCustomer(ctx, p.UserID, p.SessionID, claimInput(ctx, req.Body))
	if err != nil {
		return nil, err
	}
	if !r.Created {
		return referralsapi.ClaimReferral200JSONResponse(claimBody(r)), nil
	}
	return referralsapi.ClaimReferral201JSONResponse(claimBody(r)), nil
}

// GetBusinessReferrals implements GET /v1/biz/businesses/{bid}/referrals.
func (h handlers) GetBusinessReferrals(ctx context.Context, req referralsapi.GetBusinessReferralsRequestObject) (referralsapi.GetBusinessReferralsResponseObject, error) {
	bid, err := pathID(req.Bid)
	if err != nil {
		return nil, err
	}
	b, err := h.s.Business(ctx, bid)
	if err != nil {
		return nil, err
	}
	return referralsapi.GetBusinessReferrals200JSONResponse(b), nil
}

// ClaimBusinessReferral implements POST /v1/biz/businesses/{bid}/referrals/claim.
func (h handlers) ClaimBusinessReferral(ctx context.Context, req referralsapi.ClaimBusinessReferralRequestObject) (referralsapi.ClaimBusinessReferralResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	bid, err := pathID(req.Bid)
	if err != nil {
		return nil, err
	}
	r, err := h.s.ClaimBusiness(ctx, bid, p.UserID, p.SessionID, claimInput(ctx, req.Body))
	if err != nil {
		return nil, err
	}
	if !r.Created {
		return referralsapi.ClaimBusinessReferral200JSONResponse(claimBody(r)), nil
	}
	return referralsapi.ClaimBusinessReferral201JSONResponse(claimBody(r)), nil
}
