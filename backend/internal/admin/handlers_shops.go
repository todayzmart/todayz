package admin

import (
	"context"

	bizapi "github.com/todayz/backend/gen/api/admin-businesses"
	leadsapi "github.com/todayz/backend/gen/api/admin-leads"
	storesapi "github.com/todayz/backend/gen/api/admin-stores"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// BusinessOps maps the admin-businesses operations to their authz actions.
// Support sees the queue read-only; every move is admin and super_admin.
var BusinessOps = map[string]string{
	"ListAdminBusinesses":         policy.AdminBusinessView,
	"GetAdminBusiness":            policy.AdminBusinessView,
	"VerifyAdminBusiness":         policy.AdminBusinessVerify,
	"RequestAdminBusinessInfo":    policy.AdminBusinessVerify,
	"RejectAdminBusiness":         policy.AdminBusinessVerify,
	"SuspendAdminBusiness":        policy.AdminBusinessVerify,
	"ReinstateAdminBusiness":      policy.AdminBusinessVerify,
	"ReviewAdminBusinessKyc":      policy.AdminBusinessVerify,
	"ReviewAdminBusinessDocument": policy.AdminBusinessVerify,
	"RevealAdminBusinessKyc":      policy.AdminKycReveal,
}

// StoreOps maps the admin-stores operations to their authz actions.
var StoreOps = map[string]string{
	"ListAdminStores":        policy.AdminStoreView,
	"GetAdminStore":          policy.AdminStoreView,
	"ReviewAdminStore":       policy.AdminStoreReview,
	"UpdateAdminStore":       policy.AdminStoreEdit,
	"ForceCloseAdminStore":   policy.AdminStoreEdit,
	"ReopenAdminStore":       policy.AdminStoreEdit,
	"ResetAdminStoreStrikes": policy.AdminStrikesReset,
}

// LeadOps maps the admin-leads operations to their authz actions.
var LeadOps = map[string]string{
	"ListAdminLeads":     policy.AdminLeadView,
	"SetAdminLeadStatus": policy.AdminLeadManage,
}

type businessHandlers struct{ s *Service }

var _ bizapi.StrictServerInterface = businessHandlers{}

// ListAdminBusinesses implements GET /v1/admin/businesses.
func (h businessHandlers) ListAdminBusinesses(ctx context.Context, req bizapi.ListAdminBusinessesRequestObject) (bizapi.ListAdminBusinessesResponseObject, error) {
	out, err := h.s.listBusinesses(ctx, req.Params.Status, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.ListAdminBusinesses200JSONResponse(out), nil
}

// GetAdminBusiness implements GET /v1/admin/businesses/{businessId}.
func (h businessHandlers) GetAdminBusiness(ctx context.Context, req bizapi.GetAdminBusinessRequestObject) (bizapi.GetAdminBusinessResponseObject, error) {
	id, err := pathID(req.BusinessId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.business(ctx, id)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.GetAdminBusiness200JSONResponse(out), nil
}

// VerifyAdminBusiness implements POST /v1/admin/businesses/{businessId}/verify.
func (h businessHandlers) VerifyAdminBusiness(ctx context.Context, req bizapi.VerifyAdminBusinessRequestObject) (bizapi.VerifyAdminBusinessResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.verifyBusiness(ctx, actor, id, req.Body.Note)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.VerifyAdminBusiness200JSONResponse(out), nil
}

// RequestAdminBusinessInfo implements POST /v1/admin/businesses/{businessId}/request-info.
func (h businessHandlers) RequestAdminBusinessInfo(ctx context.Context, req bizapi.RequestAdminBusinessInfoRequestObject) (bizapi.RequestAdminBusinessInfoResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.requestBusinessInfo(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.RequestAdminBusinessInfo200JSONResponse(out), nil
}

// RejectAdminBusiness implements POST /v1/admin/businesses/{businessId}/reject.
func (h businessHandlers) RejectAdminBusiness(ctx context.Context, req bizapi.RejectAdminBusinessRequestObject) (bizapi.RejectAdminBusinessResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.setBusinessStatus(ctx, actor, id, []string{bizSubmitted, bizNeedsInfo}, bizRejected, "business.reject", req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.RejectAdminBusiness200JSONResponse(out), nil
}

// SuspendAdminBusiness implements POST /v1/admin/businesses/{businessId}/suspend.
func (h businessHandlers) SuspendAdminBusiness(ctx context.Context, req bizapi.SuspendAdminBusinessRequestObject) (bizapi.SuspendAdminBusinessResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.setBusinessStatus(ctx, actor, id, []string{bizVerified}, bizSuspended, "business.suspend", req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.SuspendAdminBusiness200JSONResponse(out), nil
}

// ReinstateAdminBusiness implements POST /v1/admin/businesses/{businessId}/reinstate.
func (h businessHandlers) ReinstateAdminBusiness(ctx context.Context, req bizapi.ReinstateAdminBusinessRequestObject) (bizapi.ReinstateAdminBusinessResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.setBusinessStatus(ctx, actor, id, []string{bizSuspended}, bizVerified, "business.reinstate", req.Body.Reason)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.ReinstateAdminBusiness200JSONResponse(out), nil
}

// ReviewAdminBusinessKyc implements POST /v1/admin/businesses/{businessId}/kyc/review.
func (h businessHandlers) ReviewAdminBusinessKyc(ctx context.Context, req bizapi.ReviewAdminBusinessKycRequestObject) (bizapi.ReviewAdminBusinessKycResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.reviewKyc(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.ReviewAdminBusinessKyc200JSONResponse(out), nil
}

// RevealAdminBusinessKyc implements POST /v1/admin/businesses/{businessId}/kyc/reveal.
// The answer is never stored, by the idempotency store or by a cache.
func (h businessHandlers) RevealAdminBusinessKyc(ctx context.Context, req bizapi.RevealAdminBusinessKycRequestObject) (bizapi.RevealAdminBusinessKycResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.revealKyc(ctx, actor, id, req.Body.Field)
	if err != nil {
		return nil, asProblem(err)
	}
	noStore := "no-store"
	return bizapi.RevealAdminBusinessKyc200JSONResponse{Body: out, Headers: bizapi.RevealAdminBusinessKyc200ResponseHeaders{CacheControl: &noStore}}, nil
}

// ReviewAdminBusinessDocument implements POST /v1/admin/businesses/{businessId}/documents/{docId}/review.
func (h businessHandlers) ReviewAdminBusinessDocument(ctx context.Context, req bizapi.ReviewAdminBusinessDocumentRequestObject) (bizapi.ReviewAdminBusinessDocumentResponseObject, error) {
	actor, id, err := adminCall(ctx, req.BusinessId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	doc, err := pathID(req.DocId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.reviewDocument(ctx, actor, id, doc, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return bizapi.ReviewAdminBusinessDocument200JSONResponse(out), nil
}

type storeHandlers struct{ s *Service }

var _ storesapi.StrictServerInterface = storeHandlers{}

// ListAdminStores implements GET /v1/admin/stores.
func (h storeHandlers) ListAdminStores(ctx context.Context, req storesapi.ListAdminStoresRequestObject) (storesapi.ListAdminStoresResponseObject, error) {
	out, err := h.s.listStores(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.ListAdminStores200JSONResponse(out), nil
}

// GetAdminStore implements GET /v1/admin/stores/{storeId}.
func (h storeHandlers) GetAdminStore(ctx context.Context, req storesapi.GetAdminStoreRequestObject) (storesapi.GetAdminStoreResponseObject, error) {
	id, err := pathID(req.StoreId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.store(ctx, id)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.GetAdminStore200JSONResponse(out), nil
}

// UpdateAdminStore implements PATCH /v1/admin/stores/{storeId}.
func (h storeHandlers) UpdateAdminStore(ctx context.Context, req storesapi.UpdateAdminStoreRequestObject) (storesapi.UpdateAdminStoreResponseObject, error) {
	actor, id, err := adminCall(ctx, req.StoreId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.updateStore(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.UpdateAdminStore200JSONResponse(out), nil
}

// ForceCloseAdminStore implements POST /v1/admin/stores/{storeId}/force-close.
func (h storeHandlers) ForceCloseAdminStore(ctx context.Context, req storesapi.ForceCloseAdminStoreRequestObject) (storesapi.ForceCloseAdminStoreResponseObject, error) {
	actor, id, err := adminCall(ctx, req.StoreId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.forceClose(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.ForceCloseAdminStore200JSONResponse(out), nil
}

// ReopenAdminStore implements POST /v1/admin/stores/{storeId}/reopen.
func (h storeHandlers) ReopenAdminStore(ctx context.Context, req storesapi.ReopenAdminStoreRequestObject) (storesapi.ReopenAdminStoreResponseObject, error) {
	actor, id, err := adminCall(ctx, req.StoreId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.reopen(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.ReopenAdminStore200JSONResponse(out), nil
}

// ReviewAdminStore implements POST /v1/admin/stores/{storeId}/review.
func (h storeHandlers) ReviewAdminStore(ctx context.Context, req storesapi.ReviewAdminStoreRequestObject) (storesapi.ReviewAdminStoreResponseObject, error) {
	actor, id, err := adminCall(ctx, req.StoreId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.reviewStore(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.ReviewAdminStore200JSONResponse(out), nil
}

// ResetAdminStoreStrikes implements POST /v1/admin/stores/{storeId}/strikes/reset.
func (h storeHandlers) ResetAdminStoreStrikes(ctx context.Context, req storesapi.ResetAdminStoreStrikesRequestObject) (storesapi.ResetAdminStoreStrikesResponseObject, error) {
	actor, id, err := adminCall(ctx, req.StoreId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.resetStoreStrikes(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return storesapi.ResetAdminStoreStrikes200JSONResponse(out), nil
}

type leadHandlers struct{ s *Service }

var _ leadsapi.StrictServerInterface = leadHandlers{}

// ListAdminLeads implements GET /v1/admin/leads.
func (h leadHandlers) ListAdminLeads(ctx context.Context, req leadsapi.ListAdminLeadsRequestObject) (leadsapi.ListAdminLeadsResponseObject, error) {
	out, err := h.s.listLeads(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return leadsapi.ListAdminLeads200JSONResponse(out), nil
}

// SetAdminLeadStatus implements POST /v1/admin/leads/{leadId}/status.
func (h leadHandlers) SetAdminLeadStatus(ctx context.Context, req leadsapi.SetAdminLeadStatusRequestObject) (leadsapi.SetAdminLeadStatusResponseObject, error) {
	actor, id, err := adminCall(ctx, req.LeadId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.setLeadStatus(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return leadsapi.SetAdminLeadStatus200JSONResponse(out), nil
}
