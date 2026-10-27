package pos

import (
	"bytes"
	"context"
	"errors"

	"github.com/go-chi/chi/v5"

	posapi "github.com/todayz/backend/gen/api/pos"
	receiptsapi "github.com/todayz/backend/gen/api/receipts"
	syncapi "github.com/todayz/backend/gen/api/sync"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// Ops maps the pos, sync and receipts operations to their authz actions,
// keyed as the generated strict servers name them (the Go method name).
var Ops = map[string]string{
	"CreatePosSale":      policy.PosSell,
	"ReversePosSale":     policy.PosReverse,
	"CreateBillClaim":    policy.PosLinkCustomer,
	"GetBillClaim":       policy.PosLinkCustomer,
	"VerifyCounterCode":  policy.PosLinkCustomer,
	"SyncStore":          policy.PosSync,
	"ListPosSales":       policy.PosSales,
	"GetPosSale":         policy.PosSales,
	"GetPosSalesSummary": policy.PosSalesSummary,
	"ExportPosSales":     policy.PosSalesExport,

	"PreviewBillClaim":   policy.MeBillClaim,
	"ConfirmBillClaim":   policy.MeBillClaim,
	"GetCounterCodeSeed": policy.MeCounterCode,
	"ListMyPurchases":    policy.MePurchases,

	"GetPublicReceipt": authz.Public,
}

// Register mounts the pos, sync and receipts operations behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	h := handlers{s}
	posapi.Register(r, h, posapi.StrictMiddlewareFunc(authz.Strict[posapi.StrictHandlerFunc](az, Ops)))
	syncapi.Register(r, h, syncapi.StrictMiddlewareFunc(authz.Strict[syncapi.StrictHandlerFunc](az, Ops)))
	receiptsapi.Register(r, h, receiptsapi.StrictMiddlewareFunc(authz.Strict[receiptsapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var (
	_ posapi.StrictServerInterface      = handlers{}
	_ syncapi.StrictServerInterface     = handlers{}
	_ receiptsapi.StrictServerInterface = handlers{}
)

// staffScope is the business and store the route's check passed.
func staffScope(ctx context.Context) (authz.Scope, error) {
	d, ok := authz.DecisionFrom(ctx)
	if !ok || d.Scope.StoreID.IsZero() {
		return authz.Scope{}, problem.Wrap(genproblems.Internal, errors.New("pos: no store in the authz decision"))
	}
	return d.Scope, nil
}

// noBody is the validator's job; this guards a router test without it.
func noBody() error { return fieldErr("body", "required") }

func (h handlers) CreatePosSale(ctx context.Context, req posapi.CreatePosSaleRequestObject) (posapi.CreatePosSaleResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, noBody()
	}
	out, err := h.s.CreateSale(ctx, sc, *req.Body)
	if err != nil {
		return nil, err
	}
	return posapi.CreatePosSale200JSONResponse(out), nil
}

func (h handlers) ListPosSales(ctx context.Context, req posapi.ListPosSalesRequestObject) (posapi.ListPosSalesResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	out, err := h.s.Sales(ctx, sc, p.Limit, p.Cursor, SalesFilter{
		From: p.From, To: p.To, Payment: p.Payment, Status: p.Status, Link: p.Link, Offline: p.Offline, Cashier: p.Cashier,
	})
	if err != nil {
		return nil, err
	}
	return posapi.ListPosSales200JSONResponse(out), nil
}

func (h handlers) GetPosSalesSummary(ctx context.Context, req posapi.GetPosSalesSummaryRequestObject) (posapi.GetPosSalesSummaryResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	out, err := h.s.SalesSummary(ctx, sc, SalesFilter{
		From: p.From, To: p.To, Payment: p.Payment, Status: p.Status, Link: p.Link, Offline: p.Offline, Cashier: p.Cashier,
	})
	if err != nil {
		return nil, err
	}
	return posapi.GetPosSalesSummary200JSONResponse(out), nil
}

func (h handlers) ExportPosSales(ctx context.Context, req posapi.ExportPosSalesRequestObject) (posapi.ExportPosSalesResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	p := req.Params
	b, err := h.s.ExportSales(ctx, sc, SalesFilter{
		From: p.From, To: p.To, Payment: p.Payment, Status: p.Status, Link: p.Link, Offline: p.Offline, Cashier: p.Cashier,
	})
	if err != nil {
		return nil, err
	}
	return posapi.ExportPosSales200TextcsvResponse{Body: bytes.NewReader(b), ContentLength: int64(len(b))}, nil
}

func (h handlers) GetPosSale(ctx context.Context, req posapi.GetPosSaleRequestObject) (posapi.GetPosSaleResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.SaleId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.SaleDetail(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	return posapi.GetPosSale200JSONResponse(out), nil
}

func (h handlers) ReversePosSale(ctx context.Context, req posapi.ReversePosSaleRequestObject) (posapi.ReversePosSaleResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, noBody()
	}
	id, err := pathID(req.SaleId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.ReverseSale(ctx, sc, id, req.Body.Reason)
	if err != nil {
		return nil, err
	}
	return posapi.ReversePosSale200JSONResponse(out), nil
}

func (h handlers) CreateBillClaim(ctx context.Context, req posapi.CreateBillClaimRequestObject) (posapi.CreateBillClaimResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, noBody()
	}
	out, err := h.s.CreateClaim(ctx, sc, *req.Body)
	if err != nil {
		return nil, err
	}
	return posapi.CreateBillClaim201JSONResponse(out), nil
}

func (h handlers) GetBillClaim(ctx context.Context, req posapi.GetBillClaimRequestObject) (posapi.GetBillClaimResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	id, err := pathID(req.ClaimId)
	if err != nil {
		return nil, err
	}
	out, err := h.s.GetClaim(ctx, sc, id)
	if err != nil {
		return nil, err
	}
	return posapi.GetBillClaim200JSONResponse(out), nil
}

func (h handlers) VerifyCounterCode(ctx context.Context, req posapi.VerifyCounterCodeRequestObject) (posapi.VerifyCounterCodeResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, noBody()
	}
	out, err := h.s.VerifyCode(ctx, sc, *req.Body)
	if err != nil {
		return nil, err
	}
	return posapi.VerifyCounterCode200JSONResponse(out), nil
}

func (h handlers) PreviewBillClaim(ctx context.Context, req posapi.PreviewBillClaimRequestObject) (posapi.PreviewBillClaimResponseObject, error) {
	out, err := h.s.PreviewClaim(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	return posapi.PreviewBillClaim200JSONResponse(out), nil
}

func (h handlers) ConfirmBillClaim(ctx context.Context, req posapi.ConfirmBillClaimRequestObject) (posapi.ConfirmBillClaimResponseObject, error) {
	if req.Body == nil {
		return nil, noBody()
	}
	out, err := h.s.ConfirmClaim(ctx, req.Token, req.Body.CreditsToUse)
	if err != nil {
		return nil, err
	}
	return posapi.ConfirmBillClaim200JSONResponse(out), nil
}

func (h handlers) GetCounterCodeSeed(ctx context.Context, _ posapi.GetCounterCodeSeedRequestObject) (posapi.GetCounterCodeSeedResponseObject, error) {
	out, err := h.s.Seed(ctx)
	if err != nil {
		return nil, err
	}
	return posapi.GetCounterCodeSeed200JSONResponse(out), nil
}

func (h handlers) SyncStore(ctx context.Context, req syncapi.SyncStoreRequestObject) (syncapi.SyncStoreResponseObject, error) {
	sc, err := staffScope(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, noBody()
	}
	out, err := h.s.Sync(ctx, sc, *req.Body)
	if err != nil {
		return nil, err
	}
	return syncapi.SyncStore200JSONResponse(out), nil
}

func (h handlers) GetPublicReceipt(ctx context.Context, req receiptsapi.GetPublicReceiptRequestObject) (receiptsapi.GetPublicReceiptResponseObject, error) {
	out, err := h.s.Receipt(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	return receiptsapi.GetPublicReceipt200JSONResponse{Body: out, Headers: receiptsapi.GetPublicReceipt200ResponseHeaders{CacheControl: ptr("no-store")}}, nil
}

func (h handlers) ListMyPurchases(ctx context.Context, req receiptsapi.ListMyPurchasesRequestObject) (receiptsapi.ListMyPurchasesResponseObject, error) {
	out, err := h.s.Purchases(ctx, req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	return receiptsapi.ListMyPurchases200JSONResponse(out), nil
}
