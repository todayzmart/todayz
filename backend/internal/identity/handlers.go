package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	authapi "github.com/todayz/backend/gen/api/auth"
	meapi "github.com/todayz/backend/gen/api/me"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/ifmatch"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// noStore is the Cache-Control of every answer that carries a token.
const noStore = "no-store"

// maxPatchBody bounds the copy patchKeys reads; the validator (slot 13)
// already refused anything larger than the 1 MB body limit.
const maxPatchBody = 1 << 20

// authOps and meOps map every operation of the two strict servers to its
// action (slot 15). OTP send and verify are public; refresh, logout and
// logout-all authenticate by their cookie inside the handler. The TOTP routes
// are not in the policy tables either: they are how an admin gets the
// otp+totp session that every admin action needs, so the handler checks the
// caller (an admin-app bearer, any amr) itself.
var (
	authOps = map[string]string{
		"SendOtp":        authz.Public,
		"VerifyOtp":      authz.Public,
		"RefreshSession": authz.Public,
		"Logout":         authz.Public,
		"LogoutAll":      authz.Public,
		"EnrolTotp":      authz.Public,
		"VerifyTotp":     authz.Public,
		"RecoverTotp":    authz.Public,
	}
	meOps = map[string]string{
		"GetMe":    policy.MeView,
		"UpdateMe": policy.MeEdit,
		"DeleteMe": policy.MeDelete,

		"ListMyAddresses": policy.MeAddressesView,
		"CreateAddress":   policy.MeAddressesManage,
		"UpdateAddress":   policy.MeAddressesManage,
		"DeleteAddress":   policy.MeAddressesManage,
	}
)

// Register mounts /v1/auth/* and /v1/me on r. az is the permission engine
// (slot 15).
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	authapi.Register(r, authHandlers{svc},
		authapi.StrictMiddlewareFunc(authz.Strict[authapi.StrictHandlerFunc](az, authOps)),
		stashRequest[authapi.StrictHandlerFunc],
	)
	meapi.Register(r.With(patchKeys), meHandlers{svc},
		meapi.StrictMiddlewareFunc(authz.Strict[meapi.StrictHandlerFunc](az, meOps)),
	)
}

// --- request access -------------------------------------------------------

type requestKey struct{}

// stashRequest puts the *http.Request in the context, so a strict handler
// can read the cookie, Origin and User-Agent the generated request object
// does not carry. The generated server applies middlewares in order, the
// last one outermost; this one is listed last so authz sees the stashed
// context too.
func stashRequest[H ~func(context.Context, http.ResponseWriter, *http.Request, any) (any, error)](next H, _ string) H {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, req any) (any, error) {
		return next(context.WithValue(ctx, requestKey{}, r), w, r, req)
	}
}

func requestFrom(ctx context.Context) *http.Request {
	r, _ := ctx.Value(requestKey{}).(*http.Request)
	return r
}

type patchKeysKey struct{}

// patchKeys records which top-level keys a PATCH body has, so that
// `"name": null` (clear it) differs from no name at all; the generated
// MePatch and AddressPatch read both as nil. The body is restored for the
// strict server.
func patchKeys(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxPatchBody+1))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var keys map[string]json.RawMessage
		if json.Unmarshal(body, &keys) != nil {
			keys = nil
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), patchKeysKey{}, keys)))
	})
}

// bodyHas reports whether the PATCH body had the key, null included.
func bodyHas(ctx context.Context, key string) bool {
	keys, _ := ctx.Value(patchKeysKey{}).(map[string]json.RawMessage)
	return keys[key] != nil
}

// --- cookies ----------------------------------------------------------------

// refreshCookie is the app's refresh cookie (D-14): host-only (no Domain),
// Path=/v1/auth, HttpOnly, Secure, SameSite=Strict.
func refreshCookie(app authn.App, token string, expires, now time.Time) string {
	maxAge := int(expires.Sub(now) / time.Second)
	if maxAge < 1 {
		maxAge = 1
	}
	c := http.Cookie{
		Name: app.CookieName(), Value: token, Path: authn.RefreshCookiePath,
		MaxAge: maxAge, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}
	return c.String()
}

// clearCookie expires the app's refresh cookie (Max-Age=0).
func clearCookie(app authn.App) string {
	c := http.Cookie{
		Name: app.CookieName(), Value: "", Path: authn.RefreshCookiePath,
		MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}
	return c.String()
}

// cookieValue reads the app's refresh cookie; empty when absent. Only the
// cookie of app is read (D-14): another app's cookie on the same request is
// ignored.
func cookieValue(r *http.Request, app authn.App) string {
	if r == nil {
		return ""
	}
	c, err := r.Cookie(app.CookieName())
	if err != nil {
		return ""
	}
	return c.Value
}

func ptr[T any](v T) *T { return &v }

func parseApp(s string) (authn.App, error) {
	app, ok := authn.ParseApp(s)
	if !ok {
		return "", problem.New(genproblems.ValidationFailed, "").WithField("body.app", "must be one of customer, business, admin")
	}
	return app, nil
}

// --- /v1/auth ---------------------------------------------------------------

type authHandlers struct{ s *Service }

var _ authapi.StrictServerInterface = authHandlers{}

func (h authHandlers) SendOtp(ctx context.Context, req authapi.SendOtpRequestObject) (authapi.SendOtpResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	app, err := parseApp(string(req.Body.App))
	if err != nil {
		return nil, err
	}
	res, err := h.s.SendOTP(ctx, app, req.Body.Phone, string(req.Body.Purpose), req.Body.TurnstileToken)
	if err != nil {
		return nil, err
	}
	return authapi.SendOtp200JSONResponse{
		Body: authapi.OtpSendResponse{
			RequestId:   res.RequestID.String(),
			ResendAfter: int((res.ResendAfter + time.Second - 1) / time.Second),
		},
		Headers: authapi.SendOtp200ResponseHeaders{CacheControl: ptr(noStore)},
	}, nil
}

func (h authHandlers) VerifyOtp(ctx context.Context, req authapi.VerifyOtpRequestObject) (authapi.VerifyOtpResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	app, err := parseApp(string(req.Body.App))
	if err != nil {
		return nil, err
	}
	var ua string
	if r := requestFrom(ctx); r != nil {
		ua = r.UserAgent()
	}
	in, err := h.s.VerifyOTP(ctx, VerifyInput{
		App: app, RequestID: req.Body.RequestId, Code: req.Body.Code, DeviceID: req.Body.DeviceId, UserAgent: ua,
	})
	if err != nil {
		return nil, err
	}
	body := authapi.OtpVerifyResponse{
		AccessToken: in.AccessToken, ExpiresAt: in.ExpiresAt.UTC(), IsNewUser: in.IsNewUser,
		TokenType: authapi.OtpVerifyResponseTokenTypeBearer, UserId: in.UserID.String(),
	}
	if in.NextStep != "" {
		body.NextStep = ptr(authapi.OtpVerifyResponseNextStep(in.NextStep))
	}
	return authapi.VerifyOtp200JSONResponse{
		Body: body,
		Headers: authapi.VerifyOtp200ResponseHeaders{
			CacheControl: ptr(noStore),
			SetCookie:    ptr(refreshCookie(app, in.RefreshToken, in.RefreshExpiresAt, h.s.clock.Now())),
		},
	}, nil
}

// checkOrigin is the CSRF rule of refresh and logout (AU-16). A refusal does
// not clear the cookie: a forged cross-site request must not sign anyone out.
func (h authHandlers) checkOrigin(r *http.Request, app authn.App, client *string) error {
	if r == nil {
		return problem.New(genproblems.Forbidden, "")
	}
	var c string
	if client != nil {
		c = *client
	}
	if err := authn.CheckRefreshOrigin(app, r.Header.Get("Origin"), c, h.s.cfg.CORSOrigins()); err != nil {
		return problem.New(genproblems.Forbidden, "")
	}
	return nil
}

func (h authHandlers) RefreshSession(ctx context.Context, req authapi.RefreshSessionRequestObject) (authapi.RefreshSessionResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	app, err := parseApp(string(req.Body.App))
	if err != nil {
		return nil, err
	}
	r := requestFrom(ctx)
	if err := h.checkOrigin(r, app, req.Params.XTZClient); err != nil {
		return nil, err
	}
	in, err := h.s.Refresh(ctx, RefreshInput{App: app, Token: cookieValue(r, app), UserAgent: r.UserAgent()})
	if err != nil {
		// A retry inside RotationGrace keeps the cookie (see graceRetry).
		var g graceRetry
		if errors.As(err, &g) {
			return nil, g.p
		}
		// A refused refresh ends the client's sign-in: clear its cookie so it
		// stops retrying a dead token (401 and 403 alike; 5xx keeps it).
		var p *problem.Error
		if errors.As(err, &p) && (problem.Status(err) == http.StatusUnauthorized || problem.Status(err) == http.StatusForbidden) {
			return nil, p.WithHeader("Set-Cookie", clearCookie(app))
		}
		return nil, err
	}
	return authapi.RefreshSession200JSONResponse{
		Body: authapi.AccessTokenResponse{
			AccessToken: in.AccessToken, ExpiresAt: in.ExpiresAt.UTC(), TokenType: authapi.AccessTokenResponseTokenTypeBearer,
		},
		Headers: authapi.RefreshSession200ResponseHeaders{
			CacheControl: ptr(noStore),
			SetCookie:    ptr(refreshCookie(app, in.RefreshToken, in.RefreshExpiresAt, h.s.clock.Now())),
		},
	}, nil
}

func (h authHandlers) Logout(ctx context.Context, req authapi.LogoutRequestObject) (authapi.LogoutResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	app, err := parseApp(string(req.Body.App))
	if err != nil {
		return nil, err
	}
	r := requestFrom(ctx)
	if err := h.checkOrigin(r, app, req.Params.XTZClient); err != nil {
		return nil, err
	}
	if err := h.s.Logout(ctx, RefreshInput{App: app, Token: cookieValue(r, app), UserAgent: r.UserAgent()}); err != nil {
		var p *problem.Error
		if errors.As(err, &p) && problem.Status(err) == http.StatusUnauthorized {
			return nil, p.WithHeader("Set-Cookie", clearCookie(app))
		}
		return nil, err
	}
	return authapi.Logout204Response{Headers: authapi.Logout204ResponseHeaders{SetCookie: ptr(clearCookie(app))}}, nil
}

func (h authHandlers) LogoutAll(ctx context.Context, req authapi.LogoutAllRequestObject) (authapi.LogoutAllResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	app, err := parseApp(string(req.Body.App))
	if err != nil {
		return nil, err
	}
	r := requestFrom(ctx)
	if err := h.checkOrigin(r, app, req.Params.XTZClient); err != nil {
		return nil, err
	}
	if _, err := h.s.LogoutAll(ctx, RefreshInput{App: app, Token: cookieValue(r, app), UserAgent: r.UserAgent()}); err != nil {
		var g graceRetry
		if errors.As(err, &g) {
			return nil, g.p
		}
		var p *problem.Error
		if errors.As(err, &p) && problem.Status(err) == http.StatusUnauthorized {
			return nil, p.WithHeader("Set-Cookie", clearCookie(app))
		}
		return nil, err
	}
	return authapi.LogoutAll204Response{Headers: authapi.LogoutAll204ResponseHeaders{SetCookie: ptr(clearCookie(app))}}, nil
}

// adminCaller is the principal of a TOTP route: an admin-app bearer token
// with any amr (slot 11 already refused a bad one).
func adminCaller(ctx context.Context) (authn.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if err := adminPrincipal(p); err != nil {
		return authn.Principal{}, err
	}
	return p, nil
}

func (h authHandlers) EnrolTotp(ctx context.Context, _ authapi.EnrolTotpRequestObject) (authapi.EnrolTotpResponseObject, error) {
	p, err := adminCaller(ctx)
	if err != nil {
		return nil, err
	}
	e, err := h.s.EnrolTOTP(ctx, p)
	if err != nil {
		return nil, err
	}
	return authapi.EnrolTotp200JSONResponse{
		Body:    authapi.TotpEnrolResponse{Secret: e.Secret, OtpauthUri: e.URI, RecoveryCodes: e.RecoveryCodes},
		Headers: authapi.EnrolTotp200ResponseHeaders{CacheControl: ptr(noStore)},
	}, nil
}

func (h authHandlers) VerifyTotp(ctx context.Context, req authapi.VerifyTotpRequestObject) (authapi.VerifyTotpResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	p, err := adminCaller(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.s.VerifyTOTP(ctx, p, req.Body.Code, string(req.Body.Purpose))
	if err != nil {
		return nil, err
	}
	hdr := authapi.VerifyTotp200ResponseHeaders{CacheControl: ptr(noStore)}
	if res.RefreshToken != "" { // purpose login rotated the sign-in
		hdr.SetCookie = ptr(refreshCookie(authn.AppAdmin, res.RefreshToken, res.RefreshExpiresAt, h.s.clock.Now()))
	}
	return authapi.VerifyTotp200JSONResponse{
		Body: authapi.AccessTokenResponse{
			AccessToken: res.AccessToken, ExpiresAt: res.ExpiresAt.UTC(), TokenType: authapi.AccessTokenResponseTokenTypeBearer,
		},
		Headers: hdr,
	}, nil
}

func (h authHandlers) RecoverTotp(ctx context.Context, req authapi.RecoverTotpRequestObject) (authapi.RecoverTotpResponseObject, error) {
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	p, err := adminCaller(ctx)
	if err != nil {
		return nil, err
	}
	res, err := h.s.RecoverTOTP(ctx, p, req.Body.RecoveryCode)
	if err != nil {
		return nil, err
	}
	return authapi.RecoverTotp200JSONResponse{
		Body: authapi.TotpRecoverResponse{
			AccessToken: res.AccessToken, ExpiresAt: res.ExpiresAt.UTC(),
			TokenType: authapi.TotpRecoverResponseTokenTypeBearer, RecoveryCodesLeft: res.RecoveryCodesLeft,
		},
		Headers: authapi.RecoverTotp200ResponseHeaders{
			CacheControl: ptr(noStore),
			SetCookie:    ptr(refreshCookie(authn.AppAdmin, res.RefreshToken, res.RefreshExpiresAt, h.s.clock.Now())),
		},
	}, nil
}

// --- /v1/me -----------------------------------------------------------------

type meHandlers struct{ s *Service }

var _ meapi.StrictServerInterface = meHandlers{}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func etag(version int32) string { return `"v` + strconv.FormatInt(int64(version), 10) + `"` }

// parseETag reads `"v{version}"` (the validator checked the pattern); a
// version that does not fit is stale by definition.
func parseETag(s string) (int32, bool) { return ifmatch.Parse(s) }

func toAPIMe(m Me) meapi.Me {
	out := meapi.Me{
		Id: m.ID.String(), Name: m.Name, PreferredLang: meapi.Language(m.PreferredLang),
		PhoneMasked: m.PhoneMasked, App: meapi.MeApp(m.App), CreatedAt: m.CreatedAt,
	}
	if m.App == authn.AppAdmin {
		roles := slices.Clone(m.Roles)
		if roles == nil {
			roles = []string{}
		}
		out.Roles = &roles
	}
	if m.Customer != nil {
		cp := &meapi.CustomerProfile{ReferralCode: m.Customer.ReferralCode}
		if m.Customer.LimitedUntil != nil {
			cp.ActiveOrderLimit = &meapi.ActiveOrderLimit{
				MaxActiveOrders: MaxActiveOrdersWhenLimited, Reason: meapi.NoShowStrikes, Until: *m.Customer.LimitedUntil,
			}
		}
		out.Customer = cp
	}
	return out
}

func (h meHandlers) GetMe(ctx context.Context, _ meapi.GetMeRequestObject) (meapi.GetMeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	m, err := h.s.GetMe(ctx, p)
	if err != nil {
		return nil, err
	}
	return meapi.GetMe200JSONResponse{Body: toAPIMe(m), Headers: meapi.GetMe200ResponseHeaders{ETag: ptr(etag(m.Version))}}, nil
}

func (h meHandlers) UpdateMe(ctx context.Context, req meapi.UpdateMeRequestObject) (meapi.UpdateMeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	// If-Match is optional in the contract so that a missing one reaches
	// here and answers 428 (a required header would be the validator's 422).
	if req.Params.IfMatch == nil || *req.Params.IfMatch == "" {
		return nil, problem.New(genproblems.PreconditionRequired, "")
	}
	version, ok := parseETag(*req.Params.IfMatch)
	if !ok {
		return nil, problem.New(genproblems.PreconditionFailed, "")
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	patch := MePatch{SetName: bodyHas(ctx, "name") || req.Body.Name != nil}
	if req.Body.Name != nil {
		name := strings.TrimSpace(*req.Body.Name)
		if name == "" {
			return nil, problem.New(genproblems.ValidationFailed, "").WithField("body.name", "must not be blank")
		}
		patch.Name = &name
	}
	if req.Body.PreferredLang != nil {
		patch.PreferredLang = ptr(string(*req.Body.PreferredLang))
	}
	if !patch.SetName && patch.PreferredLang == nil {
		return nil, problem.New(genproblems.ValidationFailed, "").WithField("body", "nothing to change")
	}
	m, err := h.s.UpdateMe(ctx, p, version, patch)
	if err != nil {
		return nil, err
	}
	return meapi.UpdateMe200JSONResponse{Body: toAPIMe(m), Headers: meapi.UpdateMe200ResponseHeaders{ETag: ptr(etag(m.Version))}}, nil
}

func (h meHandlers) DeleteMe(ctx context.Context, req meapi.DeleteMeRequestObject) (meapi.DeleteMeResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	if err := h.s.DeleteAccount(ctx, p, DeleteInput{RequestID: req.Body.OtpRequestId, Code: req.Body.Code}); err != nil {
		return nil, err
	}
	// Every session is revoked; the cookie goes too.
	return meapi.DeleteMe204Response{Headers: meapi.DeleteMe204ResponseHeaders{SetCookie: ptr(clearCookie(authn.AppCustomer))}}, nil
}

// --- /v1/me/addresses (W2-15) ------------------------------------------------

func toAPIAddress(a Address) meapi.Address {
	return meapi.Address{
		AddressId: a.ID.String(), Label: a.Label, Line1: a.Line1, Line2: a.Line2, Landmark: a.Landmark,
		Pincode: a.Pincode, Lat: a.Lat, Lng: a.Lng, IsDefault: a.IsDefault, UpdatedAt: a.UpdatedAt, Version: a.Version,
	}
}

// addressID parses the path id; a malformed one is 404 like a foreign one.
func addressID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, notFound()
	}
	return id, nil
}

func (h meHandlers) ListMyAddresses(ctx context.Context, _ meapi.ListMyAddressesRequestObject) (meapi.ListMyAddressesResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.s.ListAddresses(ctx, p)
	if err != nil {
		return nil, err
	}
	out := meapi.AddressList{Addresses: make([]meapi.Address, 0, len(list))}
	for _, a := range list {
		out.Addresses = append(out.Addresses, toAPIAddress(a))
	}
	return meapi.ListMyAddresses200JSONResponse(out), nil
}

func (h meHandlers) CreateAddress(ctx context.Context, req meapi.CreateAddressRequestObject) (meapi.CreateAddressResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	b := req.Body
	a, err := h.s.CreateAddress(ctx, p, AddressInput{
		Label: b.Label, Line1: b.Line1, Line2: b.Line2, Landmark: b.Landmark, Pincode: b.Pincode,
		Lat: b.Lat, Lng: b.Lng, IsDefault: b.IsDefault != nil && *b.IsDefault,
	})
	if err != nil {
		return nil, err
	}
	return meapi.CreateAddress201JSONResponse{Body: toAPIAddress(a), Headers: meapi.CreateAddress201ResponseHeaders{ETag: ptr(etag(a.Version))}}, nil
}

func (h meHandlers) UpdateAddress(ctx context.Context, req meapi.UpdateAddressRequestObject) (meapi.UpdateAddressResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := addressID(req.AddressId)
	if err != nil {
		return nil, err
	}
	// If-Match is optional in the contract so that a missing one answers 428.
	if req.Params.IfMatch == nil || strings.TrimSpace(*req.Params.IfMatch) == "" {
		return nil, problem.New(genproblems.PreconditionRequired, "")
	}
	version, ok := parseETag(*req.Params.IfMatch)
	if !ok {
		return nil, preconditionFailed()
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.BadRequest, "")
	}
	b := req.Body
	a, err := h.s.UpdateAddress(ctx, p, id, version, AddressPatch{
		Label: b.Label, Line1: b.Line1, Pincode: b.Pincode, IsDefault: b.IsDefault,
		SetLine2: bodyHas(ctx, "line2") || b.Line2 != nil, Line2: b.Line2,
		SetLandmark: bodyHas(ctx, "landmark") || b.Landmark != nil, Landmark: b.Landmark,
		Lat: b.Lat, Lng: b.Lng,
	})
	if err != nil {
		return nil, err
	}
	return meapi.UpdateAddress200JSONResponse{Body: toAPIAddress(a), Headers: meapi.UpdateAddress200ResponseHeaders{ETag: ptr(etag(a.Version))}}, nil
}

func (h meHandlers) DeleteAddress(ctx context.Context, req meapi.DeleteAddressRequestObject) (meapi.DeleteAddressResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	id, err := addressID(req.AddressId)
	if err != nil {
		return nil, err
	}
	if err := h.s.DeleteAddress(ctx, p, id); err != nil {
		return nil, err
	}
	return meapi.DeleteAddress204Response{}, nil
}
