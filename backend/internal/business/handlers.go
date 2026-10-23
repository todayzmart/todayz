package business

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	businessapi "github.com/todayz/backend/gen/api/business"
	publicapi "github.com/todayz/backend/gen/api/public"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// noStore is the Cache-Control of an answer that must not be kept (an
// upload URL, the lead form's receipt).
const noStore = "no-store"

// maxBody bounds the copy bodyKeys reads; the validator (slot 13) already
// refused anything larger than the 1 MB body limit.
const maxBody = 1 << 20

// businessOps maps every /v1/biz operation to its action (slot 15). The
// staff and settings routes that need more than one action run the second
// check in the handler (Service.also): a manager inviting or removing a
// manager needs staff.manage, a settings change with asksMuted asks.manage.
var businessOps = map[string]string{
	"ListBusinesses":         policy.BusinessList,
	"CreateBusiness":         policy.BusinessCreate,
	"GetBusiness":            policy.BusinessView,
	"UpdateBusiness":         policy.BusinessEdit,
	"SubmitBusiness":         policy.BusinessSubmit,
	"GetKyc":                 policy.KycView,
	"PutKyc":                 policy.KycEdit,
	"ListBusinessDocuments":  policy.KycEdit,
	"CreateBusinessDocument": policy.KycEdit,
	"ListStaff":              policy.StaffView,
	"InviteStaff":            policy.StaffManageCashiers,
	"RemoveStaff":            policy.StaffManageCashiers,
	"UpdateStaff":            policy.StaffManage,
	"AcceptStaffInvite":      policy.StaffAcceptInvite,
	"ListStores":             policy.StoreManage,
	"CreateStore":            policy.StoreManage,
	"GetStore":               policy.StoreManage,
	"UpdateStore":            policy.StoreManage,
	"SubmitStore":            policy.StoreManage,
	"GetStoreSettings":       policy.StoreSettingsEdit,
	"UpdateStoreSettings":    policy.StoreSettingsEdit,
}

// publicOps: the site's lead form is open to anyone (Turnstile guards it).
var publicOps = map[string]string{
	"CreateLead": authz.Public,
}

// Register mounts /v1/biz/businesses/* and /v1/public/leads on r. az is the
// permission engine (slot 15); it may be nil only in a route-only test, where
// every call is then refused. svc may be built without a database (503).
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	businessapi.Register(r.With(bodyKeys), bizHandlers{svc},
		businessapi.StrictMiddlewareFunc(authz.Strict[businessapi.StrictHandlerFunc](az, businessOps)),
	)
	publicapi.Register(r, publicHandlers{svc},
		publicapi.StrictMiddlewareFunc(authz.Strict[publicapi.StrictHandlerFunc](az, publicOps)),
	)
}

type bizHandlers struct{ s *Service }

type publicHandlers struct{ s *Service }

var (
	_ businessapi.StrictServerInterface = bizHandlers{}
	_ publicapi.StrictServerInterface   = publicHandlers{}
)

// --- body keys ----------------------------------------------------------------

type keysKey struct{}

// bodyKeys records the top-level keys of a PATCH or PUT body, so that a key
// sent as null (clear it) differs from a key left out (keep it); the
// generated request types read both as nil. The body is restored for the
// strict server, which still decodes and the validator still checks it.
func bodyKeys(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method != http.MethodPatch && r.Method != http.MethodPut) || r.Body == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		var raw map[string]json.RawMessage
		keys := map[string]bool{}
		if json.Unmarshal(body, &raw) == nil {
			for k := range raw {
				keys[k] = true
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keysKey{}, keys)))
	})
}

// sent reports whether the request body had the top-level key (even as null).
func sent(ctx context.Context, key string) bool {
	keys, _ := ctx.Value(keysKey{}).(map[string]bool)
	return keys[key]
}

// --- /v1/biz -------------------------------------------------------------------

func (h bizHandlers) ListBusinesses(ctx context.Context, _ businessapi.ListBusinessesRequestObject) (businessapi.ListBusinessesResponseObject, error) {
	l, err := h.s.listBusinesses(ctx)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.ListBusinesses200JSONResponse(l), nil
}

func (h bizHandlers) CreateBusiness(ctx context.Context, req businessapi.CreateBusinessRequestObject) (businessapi.CreateBusinessResponseObject, error) {
	b, v, err := h.s.createBusiness(ctx, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.CreateBusiness201JSONResponse{Body: b, Headers: businessapi.CreateBusiness201ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) GetBusiness(ctx context.Context, req businessapi.GetBusinessRequestObject) (businessapi.GetBusinessResponseObject, error) {
	b, v, err := h.s.getBusiness(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.GetBusiness200JSONResponse{Body: b, Headers: businessapi.GetBusiness200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) UpdateBusiness(ctx context.Context, req businessapi.UpdateBusinessRequestObject) (businessapi.UpdateBusinessResponseObject, error) {
	b, v, err := h.s.updateBusiness(ctx, req.Bid, req.Params.IfMatch, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.UpdateBusiness200JSONResponse{Body: b, Headers: businessapi.UpdateBusiness200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) SubmitBusiness(ctx context.Context, req businessapi.SubmitBusinessRequestObject) (businessapi.SubmitBusinessResponseObject, error) {
	b, v, err := h.s.submitBusiness(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.SubmitBusiness200JSONResponse{Body: b, Headers: businessapi.SubmitBusiness200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) GetKyc(ctx context.Context, req businessapi.GetKycRequestObject) (businessapi.GetKycResponseObject, error) {
	k, v, err := h.s.getKyc(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.GetKyc200JSONResponse{Body: k, Headers: businessapi.GetKyc200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) PutKyc(ctx context.Context, req businessapi.PutKycRequestObject) (businessapi.PutKycResponseObject, error) {
	k, v, err := h.s.putKyc(ctx, req.Bid, req.Params.IfMatch, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.PutKyc200JSONResponse{Body: k, Headers: businessapi.PutKyc200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) ListBusinessDocuments(ctx context.Context, req businessapi.ListBusinessDocumentsRequestObject) (businessapi.ListBusinessDocumentsResponseObject, error) {
	l, err := h.s.listDocuments(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.ListBusinessDocuments200JSONResponse(l), nil
}

func (h bizHandlers) CreateBusinessDocument(ctx context.Context, req businessapi.CreateBusinessDocumentRequestObject) (businessapi.CreateBusinessDocumentResponseObject, error) {
	d, err := h.s.createDocument(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.CreateBusinessDocument201JSONResponse{Body: d, Headers: businessapi.CreateBusinessDocument201ResponseHeaders{CacheControl: ptr(noStore)}}, nil
}

func (h bizHandlers) ListStaff(ctx context.Context, req businessapi.ListStaffRequestObject) (businessapi.ListStaffResponseObject, error) {
	l, err := h.s.listStaff(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.ListStaff200JSONResponse(l), nil
}

func (h bizHandlers) InviteStaff(ctx context.Context, req businessapi.InviteStaffRequestObject) (businessapi.InviteStaffResponseObject, error) {
	m, err := h.s.inviteStaff(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.InviteStaff201JSONResponse(m), nil
}

func (h bizHandlers) RemoveStaff(ctx context.Context, req businessapi.RemoveStaffRequestObject) (businessapi.RemoveStaffResponseObject, error) {
	if err := h.s.removeStaff(ctx, req.Bid, req.StaffId); err != nil {
		return nil, asProblem(err)
	}
	return businessapi.RemoveStaff204Response{}, nil
}

func (h bizHandlers) UpdateStaff(ctx context.Context, req businessapi.UpdateStaffRequestObject) (businessapi.UpdateStaffResponseObject, error) {
	m, err := h.s.updateStaff(ctx, req.Bid, req.StaffId, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.UpdateStaff200JSONResponse(m), nil
}

func (h bizHandlers) AcceptStaffInvite(ctx context.Context, req businessapi.AcceptStaffInviteRequestObject) (businessapi.AcceptStaffInviteResponseObject, error) {
	m, err := h.s.acceptInvite(ctx, req.Bid, req.StaffId)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.AcceptStaffInvite200JSONResponse(m), nil
}

func (h bizHandlers) ListStores(ctx context.Context, req businessapi.ListStoresRequestObject) (businessapi.ListStoresResponseObject, error) {
	l, err := h.s.listStores(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.ListStores200JSONResponse(l), nil
}

func (h bizHandlers) CreateStore(ctx context.Context, req businessapi.CreateStoreRequestObject) (businessapi.CreateStoreResponseObject, error) {
	st, v, err := h.s.createStore(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.CreateStore201JSONResponse{Body: st, Headers: businessapi.CreateStore201ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) GetStore(ctx context.Context, req businessapi.GetStoreRequestObject) (businessapi.GetStoreResponseObject, error) {
	st, v, err := h.s.getStore(ctx, req.Bid, req.Sid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.GetStore200JSONResponse{Body: st, Headers: businessapi.GetStore200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) UpdateStore(ctx context.Context, req businessapi.UpdateStoreRequestObject) (businessapi.UpdateStoreResponseObject, error) {
	st, v, err := h.s.updateStore(ctx, req.Bid, req.Sid, req.Params.IfMatch, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.UpdateStore200JSONResponse{Body: st, Headers: businessapi.UpdateStore200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) SubmitStore(ctx context.Context, req businessapi.SubmitStoreRequestObject) (businessapi.SubmitStoreResponseObject, error) {
	st, v, err := h.s.submitStore(ctx, req.Bid, req.Sid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.SubmitStore200JSONResponse{Body: st, Headers: businessapi.SubmitStore200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) GetStoreSettings(ctx context.Context, req businessapi.GetStoreSettingsRequestObject) (businessapi.GetStoreSettingsResponseObject, error) {
	st, v, err := h.s.getSettings(ctx, req.Bid, req.Sid)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.GetStoreSettings200JSONResponse{Body: st, Headers: businessapi.GetStoreSettings200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h bizHandlers) UpdateStoreSettings(ctx context.Context, req businessapi.UpdateStoreSettingsRequestObject) (businessapi.UpdateStoreSettingsResponseObject, error) {
	st, v, err := h.s.updateSettings(ctx, req.Bid, req.Sid, req.Params.IfMatch, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return businessapi.UpdateStoreSettings200JSONResponse{Body: st, Headers: businessapi.UpdateStoreSettings200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

// --- /v1/public ----------------------------------------------------------------

func (h publicHandlers) CreateLead(ctx context.Context, req publicapi.CreateLeadRequestObject) (publicapi.CreateLeadResponseObject, error) {
	if err := h.s.createLead(ctx, req.Body); err != nil {
		return nil, asProblem(err)
	}
	return publicapi.CreateLead202JSONResponse{
		Body:    publicapi.LeadAccepted{Received: publicapi.True},
		Headers: publicapi.CreateLead202ResponseHeaders{CacheControl: ptr(noStore)},
	}, nil
}
