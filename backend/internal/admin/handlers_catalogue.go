package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	catalogueapi "github.com/todayz/backend/gen/api/admin-catalogue"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// CatalogueOps maps the admin-catalogue operations to their authz actions.
// The catalogue role reads and works the review queue; admin and
// super_admin also edit products, categories and synonyms.
var CatalogueOps = map[string]string{
	"ListAdminProducts":          policy.AdminCatalogueView,
	"GetAdminProduct":            policy.AdminCatalogueView,
	"CreateAdminProduct":         policy.AdminCatalogueManage,
	"UpdateAdminProduct":         policy.AdminCatalogueManage,
	"ListAdminPendingProducts":   policy.AdminCatalogueView,
	"ApproveAdminPendingProduct": policy.AdminCatalogueReview,
	"MergeAdminPendingProduct":   policy.AdminCatalogueReview,
	"RejectAdminPendingProduct":  policy.AdminCatalogueReview,
	"ListAdminCategories":        policy.AdminCatalogueView,
	"CreateAdminCategory":        policy.AdminCatalogueManage,
	"UpdateAdminCategory":        policy.AdminCatalogueManage,
	"ListAdminSynonyms":          policy.AdminCatalogueView,
	"CreateAdminSynonym":         policy.AdminCatalogueManage,
	"DeleteAdminSynonym":         policy.AdminCatalogueManage,
}

// maxBody is the request body limit (the platform's 1 MB).
const maxBody = 1 << 20

type keysKey struct{}

// bodyKeys records the top-level keys of a PATCH body, so that a key sent as
// null (clear it) differs from a key left out (keep it); the generated
// request types read both as nil. The body is restored for the strict server.
func bodyKeys(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.Body == nil {
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

// sent reports whether the PATCH body had the top-level key (even as null).
func sent(ctx context.Context, key string) bool {
	keys, _ := ctx.Value(keysKey{}).(map[string]bool)
	return keys[key]
}

type catalogueHandlers struct{ s *Service }

var _ catalogueapi.StrictServerInterface = catalogueHandlers{}

// ListAdminProducts implements GET /v1/admin/products.
func (h catalogueHandlers) ListAdminProducts(ctx context.Context, req catalogueapi.ListAdminProductsRequestObject) (catalogueapi.ListAdminProductsResponseObject, error) {
	out, err := h.s.listProducts(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListAdminProducts200JSONResponse(out), nil
}

// CreateAdminProduct implements POST /v1/admin/products.
func (h catalogueHandlers) CreateAdminProduct(ctx context.Context, req catalogueapi.CreateAdminProductRequestObject) (catalogueapi.CreateAdminProductResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	out, err := h.s.createProduct(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.CreateAdminProduct201JSONResponse(out), nil
}

// GetAdminProduct implements GET /v1/admin/products/{productId}.
func (h catalogueHandlers) GetAdminProduct(ctx context.Context, req catalogueapi.GetAdminProductRequestObject) (catalogueapi.GetAdminProductResponseObject, error) {
	id, err := pathID(req.ProductId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.product(ctx, id)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.GetAdminProduct200JSONResponse(out), nil
}

// UpdateAdminProduct implements PATCH /v1/admin/products/{productId}.
func (h catalogueHandlers) UpdateAdminProduct(ctx context.Context, req catalogueapi.UpdateAdminProductRequestObject) (catalogueapi.UpdateAdminProductResponseObject, error) {
	actor, id, err := adminCall(ctx, req.ProductId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.updateProduct(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.UpdateAdminProduct200JSONResponse(out), nil
}

// ListAdminPendingProducts implements GET /v1/admin/pending-products.
func (h catalogueHandlers) ListAdminPendingProducts(ctx context.Context, req catalogueapi.ListAdminPendingProductsRequestObject) (catalogueapi.ListAdminPendingProductsResponseObject, error) {
	out, err := h.s.listPending(ctx, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListAdminPendingProducts200JSONResponse(out), nil
}

// ApproveAdminPendingProduct implements POST /v1/admin/pending-products/{pendingId}/approve.
func (h catalogueHandlers) ApproveAdminPendingProduct(ctx context.Context, req catalogueapi.ApproveAdminPendingProductRequestObject) (catalogueapi.ApproveAdminPendingProductResponseObject, error) {
	actor, id, err := adminCall(ctx, req.PendingId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.approvePending(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ApproveAdminPendingProduct200JSONResponse(out), nil
}

// MergeAdminPendingProduct implements POST /v1/admin/pending-products/{pendingId}/merge.
func (h catalogueHandlers) MergeAdminPendingProduct(ctx context.Context, req catalogueapi.MergeAdminPendingProductRequestObject) (catalogueapi.MergeAdminPendingProductResponseObject, error) {
	actor, id, err := adminCall(ctx, req.PendingId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.mergePending(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.MergeAdminPendingProduct200JSONResponse(out), nil
}

// RejectAdminPendingProduct implements POST /v1/admin/pending-products/{pendingId}/reject.
func (h catalogueHandlers) RejectAdminPendingProduct(ctx context.Context, req catalogueapi.RejectAdminPendingProductRequestObject) (catalogueapi.RejectAdminPendingProductResponseObject, error) {
	actor, id, err := adminCall(ctx, req.PendingId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.rejectPending(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.RejectAdminPendingProduct200JSONResponse(out), nil
}

// ListAdminCategories implements GET /v1/admin/categories.
func (h catalogueHandlers) ListAdminCategories(ctx context.Context, _ catalogueapi.ListAdminCategoriesRequestObject) (catalogueapi.ListAdminCategoriesResponseObject, error) {
	out, err := h.s.listCategories(ctx)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListAdminCategories200JSONResponse(out), nil
}

// CreateAdminCategory implements POST /v1/admin/categories.
func (h catalogueHandlers) CreateAdminCategory(ctx context.Context, req catalogueapi.CreateAdminCategoryRequestObject) (catalogueapi.CreateAdminCategoryResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	out, err := h.s.createCategory(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.CreateAdminCategory201JSONResponse(out), nil
}

// UpdateAdminCategory implements PATCH /v1/admin/categories/{categoryId}.
func (h catalogueHandlers) UpdateAdminCategory(ctx context.Context, req catalogueapi.UpdateAdminCategoryRequestObject) (catalogueapi.UpdateAdminCategoryResponseObject, error) {
	actor, id, err := adminCall(ctx, req.CategoryId, req.Body != nil)
	if err != nil {
		return nil, err
	}
	out, err := h.s.updateCategory(ctx, actor, id, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.UpdateAdminCategory200JSONResponse(out), nil
}

// ListAdminSynonyms implements GET /v1/admin/synonyms.
func (h catalogueHandlers) ListAdminSynonyms(ctx context.Context, req catalogueapi.ListAdminSynonymsRequestObject) (catalogueapi.ListAdminSynonymsResponseObject, error) {
	out, err := h.s.listSynonyms(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListAdminSynonyms200JSONResponse(out), nil
}

// CreateAdminSynonym implements POST /v1/admin/synonyms.
func (h catalogueHandlers) CreateAdminSynonym(ctx context.Context, req catalogueapi.CreateAdminSynonymRequestObject) (catalogueapi.CreateAdminSynonymResponseObject, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, fieldErr("body", "required")
	}
	out, err := h.s.createSynonym(ctx, p.UserID, *req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.CreateAdminSynonym201JSONResponse(out), nil
}

// DeleteAdminSynonym implements DELETE /v1/admin/synonyms/{synonymId}.
func (h catalogueHandlers) DeleteAdminSynonym(ctx context.Context, req catalogueapi.DeleteAdminSynonymRequestObject) (catalogueapi.DeleteAdminSynonymResponseObject, error) {
	actor, id, err := adminCall(ctx, req.SynonymId, true)
	if err != nil {
		return nil, err
	}
	if err := h.s.deleteSynonym(ctx, actor, id); err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.DeleteAdminSynonym204Response{}, nil
}
