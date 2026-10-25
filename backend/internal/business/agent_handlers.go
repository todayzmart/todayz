package business

import (
	"context"

	adminfieldapi "github.com/todayz/backend/gen/api/admin-field"
)

type fieldHandlers struct{ f *Field }

var _ adminfieldapi.StrictServerInterface = fieldHandlers{}

func (h fieldHandlers) ListFieldAgents(ctx context.Context, _ adminfieldapi.ListFieldAgentsRequestObject) (adminfieldapi.ListFieldAgentsResponseObject, error) {
	out, err := h.f.agents(ctx)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.ListFieldAgents200JSONResponse(out), nil
}

func (h fieldHandlers) ListFieldBusinesses(ctx context.Context, req adminfieldapi.ListFieldBusinessesRequestObject) (adminfieldapi.ListFieldBusinessesResponseObject, error) {
	out, err := h.f.list(ctx, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.ListFieldBusinesses200JSONResponse(out), nil
}

func (h fieldHandlers) CreateFieldBusiness(ctx context.Context, req adminfieldapi.CreateFieldBusinessRequestObject) (adminfieldapi.CreateFieldBusinessResponseObject, error) {
	out, err := h.f.create(ctx, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.CreateFieldBusiness201JSONResponse(out), nil
}

func (h fieldHandlers) GetFieldBusiness(ctx context.Context, req adminfieldapi.GetFieldBusinessRequestObject) (adminfieldapi.GetFieldBusinessResponseObject, error) {
	out, err := h.f.get(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.GetFieldBusiness200JSONResponse(out), nil
}

func (h fieldHandlers) UpdateFieldBusiness(ctx context.Context, req adminfieldapi.UpdateFieldBusinessRequestObject) (adminfieldapi.UpdateFieldBusinessResponseObject, error) {
	out, err := h.f.update(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.UpdateFieldBusiness200JSONResponse(out), nil
}

func (h fieldHandlers) CreateFieldDocument(ctx context.Context, req adminfieldapi.CreateFieldDocumentRequestObject) (adminfieldapi.CreateFieldDocumentResponseObject, error) {
	out, err := h.f.createPhoto(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	cc := noStore
	return adminfieldapi.CreateFieldDocument201JSONResponse{
		Body: out, Headers: adminfieldapi.CreateFieldDocument201ResponseHeaders{CacheControl: &cc},
	}, nil
}

func (h fieldHandlers) SendFieldOwnerCode(ctx context.Context, req adminfieldapi.SendFieldOwnerCodeRequestObject) (adminfieldapi.SendFieldOwnerCodeResponseObject, error) {
	out, err := h.f.sendCode(ctx, req.Bid)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.SendFieldOwnerCode200JSONResponse(out), nil
}

func (h fieldHandlers) VerifyFieldOwnerCode(ctx context.Context, req adminfieldapi.VerifyFieldOwnerCodeRequestObject) (adminfieldapi.VerifyFieldOwnerCodeResponseObject, error) {
	out, err := h.f.verifyCode(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.VerifyFieldOwnerCode200JSONResponse(out), nil
}

func (h fieldHandlers) CreateFieldStore(ctx context.Context, req adminfieldapi.CreateFieldStoreRequestObject) (adminfieldapi.CreateFieldStoreResponseObject, error) {
	out, err := h.f.createStore(ctx, req.Bid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.CreateFieldStore201JSONResponse(out), nil
}

func (h fieldHandlers) ListFieldItems(ctx context.Context, req adminfieldapi.ListFieldItemsRequestObject) (adminfieldapi.ListFieldItemsResponseObject, error) {
	out, err := h.f.listItems(ctx, req.Bid, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.ListFieldItems200JSONResponse(out), nil
}

func (h fieldHandlers) CreateFieldItem(ctx context.Context, req adminfieldapi.CreateFieldItemRequestObject) (adminfieldapi.CreateFieldItemResponseObject, error) {
	out, err := h.f.createItem(ctx, req.Bid, req.Sid, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.CreateFieldItem201JSONResponse(out), nil
}

func (h fieldHandlers) UpdateFieldItem(ctx context.Context, req adminfieldapi.UpdateFieldItemRequestObject) (adminfieldapi.UpdateFieldItemResponseObject, error) {
	out, err := h.f.updateItem(ctx, req.Bid, req.Sid, req.ItemId, req.Body)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.UpdateFieldItem200JSONResponse(out), nil
}

func (h fieldHandlers) SearchFieldProducts(ctx context.Context, req adminfieldapi.SearchFieldProductsRequestObject) (adminfieldapi.SearchFieldProductsResponseObject, error) {
	out, err := h.f.searchProducts(ctx, req.Bid, req.Sid, req.Params)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.SearchFieldProducts200JSONResponse(out), nil
}

func (h fieldHandlers) GetFieldMyStats(ctx context.Context, _ adminfieldapi.GetFieldMyStatsRequestObject) (adminfieldapi.GetFieldMyStatsResponseObject, error) {
	out, err := h.f.myStats(ctx)
	if err != nil {
		return nil, asProblem(err)
	}
	return adminfieldapi.GetFieldMyStats200JSONResponse(out), nil
}
