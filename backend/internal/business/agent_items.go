package business

// Items an agent lists in a store (A-04 step 4), one at a time, within the
// edit window (D-39). The catalogue owns store_items and products; this side
// checks the business, the store and the window, and records the add for
// A-15 in the same transaction.

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	adminfieldapi "github.com/todayz/backend/gen/api/admin-field"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/businessdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// toFieldItem is an item as the field routes show it; editable is whether
// the caller may change it now.
func toFieldItem(it CatalogueItem, editable bool) adminfieldapi.FieldItem {
	out := adminfieldapi.FieldItem{
		Brand: it.Brand, CreatedAt: it.CreatedAt.UTC(), Editable: editable, Id: it.ID.String(), IsListed: it.IsListed,
		Name: it.Name, OnHand: int(it.OnHand), PackSize: float32(it.PackSize), PricePaise: int(it.PricePaise),
		ProductId: it.ProductID.String(), ProductStatus: adminfieldapi.FieldItemProductStatus(it.ProductStatus),
		StoreId: it.StoreID.String(), Unit: adminfieldapi.FieldUnit(it.Unit), UpdatedAt: it.UpdatedAt.UTC(),
		Variant: it.Variant,
	}
	out.MrpPaise = intPtr(it.MrpPaise)
	out.ProductMrpPaise = intPtr(it.ProductMrpPaise)
	return out
}

func intPtr(v *int64) *int {
	if v == nil {
		return nil
	}
	n := int(*v)
	return &n
}

func int64Ptr(v *int) *int64 {
	if v == nil {
		return nil
	}
	n := int64(*v)
	return &n
}

// storeCall is the business and store of an item route, checked.
func (f *Field) storeCall(ctx context.Context, c fieldCall, bidStr, sidStr string) (businessdb.Business, ids.ID, error) {
	bid, err := pathID(bidStr)
	if err != nil {
		return businessdb.Business{}, ids.Zero, err
	}
	sid, err := pathID(sidStr)
	if err != nil {
		return businessdb.Business{}, ids.Zero, err
	}
	b, err := f.business(ctx, c, bid)
	if err != nil {
		return businessdb.Business{}, ids.Zero, err
	}
	if err := f.storeOf(ctx, bid, sid); err != nil {
		return businessdb.Business{}, ids.Zero, err
	}
	return b, sid, nil
}

// listItems is readable after the window too; itemsEditable says whether
// the caller may still add or change.
func (f *Field) listItems(ctx context.Context, bidStr, sidStr string, params adminfieldapi.ListFieldItemsParams) (adminfieldapi.FieldItemList, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldItemList{}, err
	}
	lim, before, err := fieldPage(params.Limit, params.Cursor)
	if err != nil {
		return adminfieldapi.FieldItemList{}, err
	}
	b, sid, err := f.storeCall(ctx, c, bidStr, sidStr)
	if err != nil {
		return adminfieldapi.FieldItemList{}, err
	}
	w, err := f.window(ctx)
	if err != nil {
		return adminfieldapi.FieldItemList{}, err
	}
	now := f.s.clock.Now()
	items, more, err := f.d.Catalogue.ListItems(ctx, sid, before, lim)
	if err != nil {
		return adminfieldapi.FieldItemList{}, fmt.Errorf("business: field items: %w", err)
	}
	ok, until := editable(b, c.agent(), w, now)
	out := adminfieldapi.FieldItemList{ItemsEditable: ok, ServerNow: now.UTC(), Items: make([]adminfieldapi.FieldItem, 0, len(items))}
	if b.Status == businessVerified {
		out.EditableUntil = until
	}
	// D-232: an agent may change only the items they added.
	var mine map[ids.ID]bool
	if ok && c.agent() && len(items) > 0 {
		keys := make([]pgtype.UUID, len(items))
		for i, it := range items {
			keys[i] = ids.UUID(it.ID)
		}
		added, err := f.s.q(ctx).FieldItemsAddedByAgent(ctx, businessdb.FieldItemsAddedByAgentParams{StoreItemIds: keys, AgentID: ids.UUID(c.p.UserID)})
		if err != nil {
			return adminfieldapi.FieldItemList{}, fmt.Errorf("business: field items added: %w", err)
		}
		mine = make(map[ids.ID]bool, len(added))
		for _, u := range added {
			id, _ := ids.FromUUID(u)
			mine[id] = true
		}
	}
	for _, it := range items {
		out.Items = append(out.Items, toFieldItem(it, ok && (!c.agent() || mine[it.ID])))
	}
	if more && len(items) > 0 {
		out.NextCursor = fieldCursor(items[len(items)-1].ID)
	}
	return out, nil
}

func (f *Field) searchProducts(ctx context.Context, bidStr, sidStr string, params adminfieldapi.SearchFieldProductsParams) (adminfieldapi.FieldProductMatchList, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldProductMatchList{}, err
	}
	_, sid, err := f.storeCall(ctx, c, bidStr, sidStr)
	if err != nil {
		return adminfieldapi.FieldProductMatchList{}, err
	}
	q, barcode := trimmedPtr(params.Q), trimmedPtr(params.Barcode)
	if q == nil && barcode == nil {
		return adminfieldapi.FieldProductMatchList{}, problem.New(genproblems.ValidationFailed, "").WithField("query.q", "give a name or a barcode")
	}
	rows, err := f.d.Catalogue.SearchProducts(ctx, sid, q, barcode)
	if err != nil {
		return adminfieldapi.FieldProductMatchList{}, fmt.Errorf("business: field search: %w", err)
	}
	out := adminfieldapi.FieldProductMatchList{Items: make([]adminfieldapi.FieldProductMatch, 0, len(rows))}
	for _, p := range rows {
		out.Items = append(out.Items, adminfieldapi.FieldProductMatch{
			Brand: p.Brand, CategoryId: p.CategoryID.String(), Id: p.ID.String(), InStore: p.InStore,
			MrpPaise: intPtr(p.MrpPaise), Name: p.Name, PackSize: float32(p.PackSize),
			Unit: adminfieldapi.FieldUnit(p.Unit), Variant: p.Variant,
		})
	}
	return out, nil
}

// createItem lists one product in the store: a master product by id, or a
// new one (pending review, D-64). Everyone needs the business editable; a
// field agent only within the window.
func (f *Field) createItem(ctx context.Context, bidStr, sidStr string, body *adminfieldapi.FieldItemCreate) (adminfieldapi.FieldItem, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldItem{}, err
	}
	if body == nil {
		return adminfieldapi.FieldItem{}, problem.New(genproblems.BadRequest, "")
	}
	in := CatalogueItemCreate{PricePaise: int64(body.PricePaise), MrpPaise: int64Ptr(body.MrpPaise), IsListed: true, AgentID: c.p.UserID}
	if body.IsListed != nil {
		in.IsListed = *body.IsListed
	}
	if body.OpeningStock != nil {
		in.OpeningStock = int32(*body.OpeningStock) //nolint:gosec // the validator bounds it to 0..1,000,000
	}
	var v invalid
	switch {
	case body.ProductId != nil && body.NewProduct != nil:
		v.add("body.productId", "give a product or a new product, not both")
	case body.ProductId != nil:
		id, err := ids.Parse(*body.ProductId)
		if err != nil {
			v.add("body.productId", "must be an id")
		}
		in.ProductID = &id
	case body.NewProduct != nil:
		np := body.NewProduct
		v.text("body.newProduct.name", &np.Name)
		cat, err := ids.Parse(np.CategoryId)
		if err != nil {
			v.add("body.newProduct.categoryId", "must be an id")
		}
		in.NewProduct = &CatalogueNewProduct{
			Name: np.Name, CategoryID: cat, Variant: trimmedPtr(np.Variant), PackSize: float64(np.PackSize),
			Unit: string(np.Unit), MrpPaise: int64Ptr(np.MrpPaise), Barcode: trimmedPtr(np.Barcode),
		}
	default:
		v.add("body.productId", "give a product or a new product")
	}
	if err := v.err(); err != nil {
		return adminfieldapi.FieldItem{}, err
	}
	var item CatalogueItem
	err = f.s.db.InTx(ctx, func(ctx context.Context) error {
		b, sid, err := f.storeCall(ctx, c, bidStr, sidStr)
		if err != nil {
			return err
		}
		if err := f.mayEdit(ctx, c, b); err != nil {
			return err
		}
		in.StoreID = sid
		id, err := f.d.Catalogue.CreateItem(ctx, in)
		if err != nil {
			return err
		}
		now := f.s.clock.Now()
		if err := f.s.q(ctx).InsertFieldItemAdd(ctx, businessdb.InsertFieldItemAddParams{
			StoreItemID: ids.UUID(id), BusinessID: b.ID, StoreID: ids.UUID(sid), AgentID: ids.UUID(c.p.UserID), Now: now,
		}); err != nil {
			return fmt.Errorf("business: field item add: %w", err)
		}
		if item, err = f.d.Catalogue.Item(ctx, sid, id); err != nil {
			return err
		}
		return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "store_item.create", "store_item", id.String(), nil,
			map[string]any{"storeId": sid.String(), "productId": item.ProductID.String(), "pricePaise": item.PricePaise,
				"isListed": item.IsListed, "productStatus": item.ProductStatus})
	})
	if err != nil {
		return adminfieldapi.FieldItem{}, asProblem(err)
	}
	return toFieldItem(item, true), nil
}

// updateItem changes an item's price, MRP or listing within the window.
func (f *Field) updateItem(ctx context.Context, bidStr, sidStr, itemStr string, body *adminfieldapi.FieldItemUpdate) (adminfieldapi.FieldItem, error) {
	c, err := f.call(ctx)
	if err != nil {
		return adminfieldapi.FieldItem{}, err
	}
	itemID, err := pathID(itemStr)
	if err != nil {
		return adminfieldapi.FieldItem{}, err
	}
	if body == nil {
		return adminfieldapi.FieldItem{}, problem.New(genproblems.BadRequest, "")
	}
	in := CatalogueItemUpdate{ItemID: itemID}
	if sent(ctx, "pricePaise") && body.PricePaise != nil {
		in.SetPrice, in.PricePaise = true, int64(*body.PricePaise)
	}
	if sent(ctx, "mrpPaise") {
		in.SetMrp, in.MrpPaise = true, int64Ptr(body.MrpPaise)
	}
	if sent(ctx, "isListed") && body.IsListed != nil {
		in.SetListed, in.IsListed = true, *body.IsListed
	}
	var item CatalogueItem
	err = f.s.db.InTx(ctx, func(ctx context.Context) error {
		b, sid, err := f.storeCall(ctx, c, bidStr, sidStr)
		if err != nil {
			return err
		}
		if err := f.mayEdit(ctx, c, b); err != nil {
			return err
		}
		in.StoreID = sid
		cur, err := f.d.Catalogue.Item(ctx, sid, itemID)
		if err != nil {
			return err
		}
		// D-232: an agent changes only the items they added; the shop's own
		// items are the shop's.
		if c.agent() {
			by, err := f.s.q(ctx).FieldItemAddedBy(ctx, ids.UUID(itemID))
			if db.IsNoRows(err) || (err == nil && by != ids.UUID(c.p.UserID)) {
				return problem.New(genproblems.Forbidden, "an agent changes only the items they added")
			}
			if err != nil {
				return fmt.Errorf("business: field item added by: %w", err)
			}
		}
		if in.SetPrice || in.SetMrp || in.SetListed {
			if err := f.d.Catalogue.UpdateItem(ctx, in); err != nil {
				return err
			}
		}
		if item, err = f.d.Catalogue.Item(ctx, sid, itemID); err != nil {
			return err
		}
		return f.s.writeAuditAs(ctx, audit.ActorAdmin, c.p.UserID, "store_item.update", "store_item", itemID.String(),
			map[string]any{"pricePaise": cur.PricePaise, "mrpPaise": cur.MrpPaise, "isListed": cur.IsListed},
			map[string]any{"pricePaise": item.PricePaise, "mrpPaise": item.MrpPaise, "isListed": item.IsListed})
	})
	if err != nil {
		return adminfieldapi.FieldItem{}, asProblem(err)
	}
	return toFieldItem(item, true), nil
}
