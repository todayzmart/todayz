package catalogue

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
)

// maxBody bounds the copy bodyKeys reads; the validator (slot 13) already
// refused anything larger than the 1 MB body limit.
const maxBody = 1 << 20

// ops maps every catalogue operation to the action the engine checks
// (slot 15) in the path's business and store. A movement kind or a price
// change that needs more runs the second check in the service (also).
var ops = map[string]string{
	"ListCategories":      authz.Public,
	"SearchProducts":      policy.ItemView,
	"CreateProduct":       policy.ProductCreate,
	"ListStoreItems":      policy.ItemView,
	"GetStoreItem":        policy.ItemView,
	"GetStoreItemHistory": policy.ItemView,
	"CreateStoreItem":     policy.ItemManage,
	"UpdateStoreItem":     policy.ItemManage,
	"CreateStockMovement": policy.StockCount,
	"ListStockBatches":    policy.BatchesManage,
	"UpdateStockBatch":    policy.BatchesManage,
}

// Register mounts the catalogue routes on r. az is the permission engine
// (slot 15); it may be nil only in a route-only test, where every non-public
// call is then refused. svc may be built without a database (503).
func Register(r chi.Router, svc *Service, az authz.Authorizer) {
	catalogueapi.Register(r.With(bodyKeys), handlers{svc},
		catalogueapi.StrictMiddlewareFunc(authz.Strict[catalogueapi.StrictHandlerFunc](az, ops)),
	)
}

type handlers struct{ s *Service }

var _ catalogueapi.StrictServerInterface = handlers{}

// --- body keys ----------------------------------------------------------------

type keysKey struct{}

// bodyKeys records the top-level keys of a PATCH body, so that a key sent as
// null (clear it) differs from a key left out (keep it); the generated
// request types read both as nil. The body is restored for the strict
// server.
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

// --- handlers -------------------------------------------------------------------

func (h handlers) ListCategories(ctx context.Context, req catalogueapi.ListCategoriesRequestObject) (catalogueapi.ListCategoriesResponseObject, error) {
	l, err := h.s.listCategories(ctx, req.Params.AcceptLanguage)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListCategories200JSONResponse(l), nil
}

func (h handlers) SearchProducts(ctx context.Context, req catalogueapi.SearchProductsRequestObject) (catalogueapi.SearchProductsResponseObject, error) {
	l, err := h.s.searchProducts(ctx, req.Bid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.SearchProducts200JSONResponse(l), nil
}

func (h handlers) CreateProduct(ctx context.Context, req catalogueapi.CreateProductRequestObject) (catalogueapi.CreateProductResponseObject, error) {
	p, err := h.s.createProduct(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.CreateProduct201JSONResponse(p), nil
}

func (h handlers) ListStoreItems(ctx context.Context, req catalogueapi.ListStoreItemsRequestObject) (catalogueapi.ListStoreItemsResponseObject, error) {
	l, err := h.s.listItems(ctx, req.Bid, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListStoreItems200JSONResponse(l), nil
}

func (h handlers) CreateStoreItem(ctx context.Context, req catalogueapi.CreateStoreItemRequestObject) (catalogueapi.CreateStoreItemResponseObject, error) {
	it, v, err := h.s.createItem(ctx, req.Bid, req.Sid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.CreateStoreItem201JSONResponse{Body: it, Headers: catalogueapi.CreateStoreItem201ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) GetStoreItem(ctx context.Context, req catalogueapi.GetStoreItemRequestObject) (catalogueapi.GetStoreItemResponseObject, error) {
	it, v, err := h.s.getItem(ctx, req.Bid, req.Sid, req.ItemId)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.GetStoreItem200JSONResponse{Body: it, Headers: catalogueapi.GetStoreItem200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) UpdateStoreItem(ctx context.Context, req catalogueapi.UpdateStoreItemRequestObject) (catalogueapi.UpdateStoreItemResponseObject, error) {
	it, v, err := h.s.updateItem(ctx, req.Bid, req.Sid, req.ItemId, req.Params.IfMatch, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.UpdateStoreItem200JSONResponse{Body: it, Headers: catalogueapi.UpdateStoreItem200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}

func (h handlers) GetStoreItemHistory(ctx context.Context, req catalogueapi.GetStoreItemHistoryRequestObject) (catalogueapi.GetStoreItemHistoryResponseObject, error) {
	l, err := h.s.history(ctx, req.Bid, req.Sid, req.ItemId, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.GetStoreItemHistory200JSONResponse(l), nil
}

func (h handlers) CreateStockMovement(ctx context.Context, req catalogueapi.CreateStockMovementRequestObject) (catalogueapi.CreateStockMovementResponseObject, error) {
	res, err := h.s.createMovement(ctx, req.Bid, req.Sid, req.ItemId, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.CreateStockMovement201JSONResponse(res), nil
}

func (h handlers) ListStockBatches(ctx context.Context, req catalogueapi.ListStockBatchesRequestObject) (catalogueapi.ListStockBatchesResponseObject, error) {
	l, err := h.s.listBatches(ctx, req.Bid, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.ListStockBatches200JSONResponse(l), nil
}

func (h handlers) UpdateStockBatch(ctx context.Context, req catalogueapi.UpdateStockBatchRequestObject) (catalogueapi.UpdateStockBatchResponseObject, error) {
	b, v, err := h.s.updateBatch(ctx, req.Bid, req.Sid, req.BatchId, req.Params.IfMatch, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return catalogueapi.UpdateStockBatch200JSONResponse{Body: b, Headers: catalogueapi.UpdateStockBatch200ResponseHeaders{ETag: ptr(etag(v))}}, nil
}
