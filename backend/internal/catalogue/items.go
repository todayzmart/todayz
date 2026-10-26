package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	catalogueapi "github.com/todayz/backend/gen/api/catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// --- categories ------------------------------------------------------------------

// languages the category names carry; en is final, the rest are drafts.
var languages = []string{"en", "hi", "kn", "te", "ta"}

// language is the first supported language in an Accept-Language header
// (q weights are honoured), en when none is.
func language(h *string) string {
	if h == nil {
		return "en"
	}
	best, bestQ := "en", -1.0
	for _, part := range strings.Split(*h, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if _, err := fmt.Sscanf(v, "%g", &q); err != nil {
				continue
			}
		}
		base, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		for _, l := range languages {
			if base == l && q > bestQ && q > 0 {
				best, bestQ = l, q
			}
		}
	}
	return best
}

type names struct {
	En string  `json:"en"`
	Hi *string `json:"hi,omitempty"`
	Kn *string `json:"kn,omitempty"`
	Te *string `json:"te,omitempty"`
	Ta *string `json:"ta,omitempty"`
}

func (n names) in(lang string) string {
	var v *string
	switch lang {
	case "hi":
		v = n.Hi
	case "kn":
		v = n.Kn
	case "te":
		v = n.Te
	case "ta":
		v = n.Ta
	}
	if v != nil && *v != "" {
		return *v
	}
	return n.En
}

func (s *Service) listCategories(ctx context.Context, acceptLanguage *string) (catalogueapi.CategoryList, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.CategoryList{}, err
	}
	rows, err := s.q(ctx).ListCategories(ctx)
	if err != nil {
		return catalogueapi.CategoryList{}, fmt.Errorf("catalogue: categories: %w", err)
	}
	lang := language(acceptLanguage)
	out := catalogueapi.CategoryList{Items: []catalogueapi.Category{}}
	at := map[pgtype.UUID]int{}
	for _, r := range rows {
		var n names
		if err := json.Unmarshal(r.Names, &n); err != nil {
			return catalogueapi.CategoryList{}, fmt.Errorf("catalogue: category names: %w", err)
		}
		api := catalogueapi.CategoryNames{En: n.En, Hi: n.Hi, Kn: n.Kn, Te: n.Te, Ta: n.Ta}
		id := ulidOf(r.ID)
		if !r.ParentID.Valid {
			at[r.ID] = len(out.Items)
			out.Items = append(out.Items, catalogueapi.Category{
				Id: id, Slug: r.Slug, Name: n.in(lang), Names: api, Tint: catalogueapi.CategoryTint(r.Tint),
				Icon: r.Icon, Children: []catalogueapi.CategoryChild{},
			})
			continue
		}
		// Parents sort first (the query orders by parent_id IS NOT NULL).
		i, ok := at[r.ParentID]
		if !ok {
			continue
		}
		out.Items[i].Children = append(out.Items[i].Children, catalogueapi.CategoryChild{
			Id: id, Slug: r.Slug, Name: n.in(lang), Names: api, Tint: catalogueapi.CategoryTint(r.Tint), Icon: r.Icon,
		})
	}
	return out, nil
}

// --- product search -------------------------------------------------------------

func (s *Service) searchProducts(ctx context.Context, bid string, p catalogueapi.SearchProductsParams) (catalogueapi.ProductMatchList, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.ProductMatchList{}, err
	}
	sc, err := scope(bid, "")
	if err != nil {
		return catalogueapi.ProductMatchList{}, err
	}
	var v invalid
	q := trimmed(p.Q)
	v.text("query.q", q)
	if q == nil && p.Barcode == nil {
		v.add("query.q", "send q or barcode")
	}
	if err := v.err(); err != nil {
		return catalogueapi.ProductMatchList{}, err
	}
	arg := cataloguedb.SearchProductsParams{Barcode: p.Barcode, Lim: limitOf(p.Limit)}
	if q != nil {
		arg.Q, arg.Pattern = q, ptr(likePattern(*q))
	}
	if p.StoreId != nil {
		// The store must be one this caller may see in this business (a
		// cashier limited to other stores, or another business's store, is 403).
		sid, err := ids.Parse(*p.StoreId)
		if err != nil {
			return catalogueapi.ProductMatchList{}, fieldErr("query.storeId", "not a store id")
		}
		if err := s.also(ctx, policy.ItemView, authz.Scope{BusinessID: sc.BusinessID, StoreID: sid}); err != nil {
			return catalogueapi.ProductMatchList{}, err
		}
		arg.StoreID = ids.UUID(sid)
	}
	rows, err := s.q(ctx).SearchProducts(ctx, arg)
	if err != nil {
		return catalogueapi.ProductMatchList{}, fmt.Errorf("catalogue: search products: %w", err)
	}
	out := catalogueapi.ProductMatchList{Items: make([]catalogueapi.ProductMatch, 0, len(rows))}
	for _, r := range rows {
		out.Items = append(out.Items, catalogueapi.ProductMatch{
			Id: ulidOf(r.ID), Name: r.Name, Brand: r.BrandName, Variant: r.Variant, PackSize: float32(r.PackSize),
			Unit: catalogueapi.Unit(r.Unit), MrpPaise: intp(r.MrpPaise), CategoryId: ulidOf(r.CategoryID),
			Barcodes: nonNil(r.Barcodes), InStore: r.InStore,
		})
	}
	return out, nil
}

// --- missing products -------------------------------------------------------------

// createProduct creates a product the master catalogue does not have
// (D-37): pending_review, with the body's store as its creator, so that
// store can list it at once and the A-08 queue sees it. The engine checked
// product.create in the business; the store needs it too.
func (s *Service) createProduct(ctx context.Context, bid string, b *catalogueapi.ProductCreate) (catalogueapi.CreatedProduct, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.CreatedProduct{}, err
	}
	sc, err := scope(bid, "")
	if err != nil {
		return catalogueapi.CreatedProduct{}, err
	}
	if b == nil {
		return catalogueapi.CreatedProduct{}, problem.New(genproblems.BadRequest, "")
	}
	if sc.StoreID, err = ids.Parse(b.StoreId); err != nil {
		return catalogueapi.CreatedProduct{}, fieldErr("body.storeId", "not a store id")
	}
	if err := s.also(ctx, policy.ProductCreate, sc); err != nil {
		return catalogueapi.CreatedProduct{}, err
	}
	categoryID, err := ids.Parse(b.CategoryId)
	if err != nil {
		return catalogueapi.CreatedProduct{}, fieldErr("body.categoryId", "not a category id")
	}
	cq := s.q(ctx)
	var v invalid
	name := strings.TrimSpace(b.Name)
	v.text("body.name", &name)
	variant, brand := trimmed(b.Variant), trimmed(b.Brand)
	v.text("body.variant", variant)
	v.text("body.brand", brand)
	barcodes := []string{}
	if b.Barcodes != nil {
		for i, bc := range *b.Barcodes {
			if slices.Contains(barcodes, bc) {
				continue
			}
			taken, err := cq.BarcodeTaken(ctx, bc)
			if err != nil {
				return catalogueapi.CreatedProduct{}, fmt.Errorf("catalogue: barcode: %w", err)
			}
			if taken {
				v.add(fmt.Sprintf("body.barcodes[%d]", i), "a product with this barcode exists; search for it")
			}
			barcodes = append(barcodes, bc)
		}
	}
	if err := v.err(); err != nil {
		return catalogueapi.CreatedProduct{}, err
	}
	now := s.clock.Now()
	productID := s.ids.New(now)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := cataloguedb.New(s.db.Q(ctx))
		var brandID pgtype.UUID
		// A brand whose name has no ASCII letter or digit has no slug; the
		// product is saved without it and review can add it.
		if brand != nil {
			if slug := brandSlug(*brand); slug != "" {
				if brandID, err = q.InsertBrand(ctx, cataloguedb.InsertBrandParams{
					ID: ids.UUID(s.ids.New(now)), Name: *brand, Slug: slug, Now: now,
				}); err != nil {
					return fmt.Errorf("catalogue: brand: %w", err)
				}
			}
		}
		if err := q.InsertProduct(ctx, cataloguedb.InsertProductParams{
			ID: ids.UUID(productID), Name: name, BrandID: brandID, CategoryID: ids.UUID(categoryID), Variant: variant,
			PackSize: float64(b.PackSize), Unit: string(b.Unit), MrpPaise: int64p(b.MrpPaise), Barcodes: barcodes,
			Status: "pending_review", CreatedByStoreID: ids.UUID(sc.StoreID), Now: now,
		}); err != nil {
			if db.IsForeignKeyViolation(err, "fk_products_category_id") {
				return fieldErr("body.categoryId", "unknown category")
			}
			return fmt.Errorf("catalogue: new product: %w", err)
		}
		return nil
	})
	if err != nil {
		return catalogueapi.CreatedProduct{}, err
	}
	r, err := s.q(ctx).GetProduct(ctx, ids.UUID(productID))
	if err != nil {
		return catalogueapi.CreatedProduct{}, fmt.Errorf("catalogue: product: %w", err)
	}
	return catalogueapi.CreatedProduct{
		Id: ulidOf(r.ID), Name: r.Name, Brand: r.BrandName, Variant: r.Variant, PackSize: float32(r.PackSize),
		Unit: catalogueapi.Unit(r.Unit), MrpPaise: intp(r.MrpPaise), CategoryId: ulidOf(r.CategoryID),
		Barcodes: nonNil(r.Barcodes), Status: catalogueapi.PendingReview,
	}, nil
}

// brandSlug is a brand's slug: ASCII lower-case letters and digits joined by
// single hyphens; empty when the name has none.
func brandSlug(name string) string {
	var b strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if hyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			hyphen = false
			b.WriteRune(r)
		default:
			hyphen = true
		}
	}
	return b.String()
}

// --- store items ----------------------------------------------------------------

func (s *Service) toItem(r cataloguedb.GetStoreItemRow, v view) catalogueapi.StoreItem {
	name := r.ProductName
	if r.LocalName != nil {
		name = *r.LocalName
	}
	avail := r.OnHand - r.Reserved
	it := catalogueapi.StoreItem{
		Id: ulidOf(r.ID), StoreId: ulidOf(r.StoreID), ProductId: ulidOf(r.ProductID),
		Name: name, ProductName: r.ProductName, LocalName: r.LocalName, Brand: r.BrandName, Variant: r.Variant,
		PackSize: float32(r.PackSize), Unit: catalogueapi.Unit(r.Unit), CategoryId: ulidOf(r.CategoryID),
		Barcodes: nonNil(r.Barcodes), PricePaise: int(r.PricePaise), MrpPaise: intp(r.MrpPaise),
		ProductMrpPaise: intp(r.ProductMrpPaise), OnHand: int(r.OnHand), Reserved: int(r.Reserved),
		Available: int(avail), LowStock: LowStock(avail, r.LowStockAt, v.lowDefault), IsListed: r.IsListed,
		StockUpdatedAt: r.StockUpdatedAt, Stale: Stale(r.StockUpdatedAt, v.now, v.staleAfter),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
		ImageKey: r.ImageKey, Photo: s.itemPhoto(r, v.now), CanAddPhoto: ptr(r.CanAddPhoto),
	}
	if r.LowStockAt != nil {
		it.LowStockAt = ptr(int(*r.LowStockAt))
	}
	if r.StockUpdatedAt != nil {
		t := r.StockUpdatedAt.UTC()
		it.StockUpdatedAt = &t
	}
	return it
}

// item reads one item of the store (another store's item is 404).
func (s *Service) item(ctx context.Context, sid, itemID ids.ID) (catalogueapi.StoreItem, int32, error) {
	r, err := s.q(ctx).GetStoreItem(ctx, cataloguedb.GetStoreItemParams{ID: ids.UUID(itemID), StoreID: ids.UUID(sid)})
	if db.IsNoRows(err) {
		return catalogueapi.StoreItem{}, 0, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return catalogueapi.StoreItem{}, 0, fmt.Errorf("catalogue: item: %w", err)
	}
	v, err := s.itemView(ctx)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	return s.toItem(r, v), r.Version, nil
}

func (s *Service) getItem(ctx context.Context, bid, sid, itemID string) (catalogueapi.StoreItem, int32, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	id, err := pathID(itemID)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	return s.item(ctx, sc.StoreID, id)
}

func (s *Service) listItems(ctx context.Context, bid, sid string, p catalogueapi.ListStoreItemsParams) (catalogueapi.StoreItemList, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.StoreItemList{}, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.StoreItemList{}, err
	}
	v, err := s.itemView(ctx)
	if err != nil {
		return catalogueapi.StoreItemList{}, err
	}
	var inv invalid
	q := trimmed(p.Q)
	inv.text("query.q", q)
	if err := inv.err(); err != nil {
		return catalogueapi.StoreItemList{}, err
	}
	lim := limitOf(p.Limit)
	arg := cataloguedb.ListStoreItemsParams{
		StoreID: ids.UUID(sc.StoreID), LowOnly: p.LowStock != nil && *p.LowStock,
		ListedOnly: p.Listed != nil && *p.Listed, LowDefault: v.lowDefault, Lim: lim + 1,
	}
	if q != nil {
		arg.Q, arg.Pattern = q, ptr(likePattern(*q))
	}
	if p.CategoryId != nil {
		c, err := ids.Parse(*p.CategoryId)
		if err != nil {
			return catalogueapi.StoreItemList{}, fieldErr("query.categoryId", "not a category id")
		}
		arg.CategoryID = ids.UUID(c)
	}
	if p.Cursor != nil {
		name, id, err := decodeCursor(*p.Cursor)
		if err != nil {
			return catalogueapi.StoreItemList{}, badCursor()
		}
		arg.AfterName, arg.AfterID = &name, ids.UUID(id)
	}
	rows, err := s.q(ctx).ListStoreItems(ctx, arg)
	if err != nil {
		return catalogueapi.StoreItemList{}, fmt.Errorf("catalogue: list items: %w", err)
	}
	out := catalogueapi.StoreItemList{Items: make([]catalogueapi.StoreItem, 0, min(len(rows), int(lim)))}
	for i, r := range rows {
		if i == int(lim) {
			last := rows[i-1]
			id, _ := ids.FromUUID(last.ID)
			out.NextCursor = ptr(encodeCursor(last.ProductName, id))
			break
		}
		out.Items = append(out.Items, s.toItem(cataloguedb.GetStoreItemRow(r), v))
	}
	return out, nil
}

// istDate turns an API date into the IST calendar date as midnight UTC.
func istDate(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
	return &t
}

func (s *Service) createItem(ctx context.Context, bid, sid string, b *catalogueapi.StoreItemCreate) (catalogueapi.StoreItem, int32, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	p, err := principal(ctx)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	if b == nil {
		return catalogueapi.StoreItem{}, 0, problem.New(genproblems.BadRequest, "")
	}
	// The price is always set on create, so the price action is needed too.
	if err := s.also(ctx, policy.ItemPriceEdit, sc); err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	productID, err := ids.Parse(b.ProductId)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, fieldErr("body.productId", "not a product id")
	}
	now := s.clock.Now()
	var v invalid
	v.text("body.localName", b.LocalName)
	opening := int32(0)
	if b.OpeningStock != nil {
		opening = i32i(*b.OpeningStock) // contract: 0..1,000,000
	}
	if opening == 0 && b.ExpiresOn != nil {
		v.add("body.expiresOn", "needs openingStock above 0")
	}
	if opening == 0 && b.CostPaise != nil {
		v.add("body.costPaise", "needs openingStock above 0")
	}
	exp := istDate(b.ExpiresOn)
	if exp != nil && exp.Before(Today(now)) {
		v.add("body.expiresOn", "must not be in the past")
	}
	if err := v.err(); err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	// A product waiting for review is usable at once in the store that
	// created it (01 §5, D-37).
	prod, err := s.q(ctx).GetProduct(ctx, ids.UUID(productID))
	if db.IsNoRows(err) || (err == nil && prod.Status != "active" &&
		(prod.Status != "pending_review" || prod.CreatedByStoreID != ids.UUID(sc.StoreID))) {
		return catalogueapi.StoreItem{}, 0, fieldErr("body.productId", "no such product")
	}
	if err != nil {
		return catalogueapi.StoreItem{}, 0, fmt.Errorf("catalogue: product: %w", err)
	}
	price := int64(b.PricePaise)
	mrp := int64p(b.MrpPaise)
	listed := true
	if b.IsListed != nil {
		listed = *b.IsListed
	}
	var low *int32
	if b.LowStockAt != nil {
		low = ptr(i32i(*b.LowStockAt)) // contract: 0..1,000,000
	}
	itemID := s.ids.New(now)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		if _, err := cataloguedb.New(q).InsertStoreItem(ctx, cataloguedb.InsertStoreItemParams{
			ID: ids.UUID(itemID), StoreID: ids.UUID(sc.StoreID), ProductID: ids.UUID(productID), LocalName: b.LocalName,
			PricePaise: price, MrpPaise: mrp, LowStockAt: low, IsListed: listed, Now: now,
		}); err != nil {
			if db.IsUniqueViolation(err, "ux_store_items_store_product") {
				return problem.New(genproblems.Conflict, "the store already lists this product")
			}
			return fmt.Errorf("catalogue: insert item: %w", err)
		}
		if opening > 0 {
			_, err := s.writer.AddStock(ctx, q, StockIn{
				StoreID: sc.StoreID, ItemID: itemID, Qty: opening, Source: SourceOpening,
				ExpiresOn: exp, CostPaise: int64p(b.CostPaise), Who: Who{UserID: p.UserID},
			})
			return writerProblem(err)
		}
		return s.recordChange(ctx, q, sc.StoreID, itemID)
	})
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	return s.item(ctx, sc.StoreID, itemID)
}

// recordChange records an item change for the delta feed (D-36) when no
// stock write did.
func (s *Service) recordChange(ctx context.Context, q db.Querier, storeID, itemID ids.ID) error {
	_, err := cataloguedb.New(q).RecordCatalogueChange(ctx, cataloguedb.RecordCatalogueChangeParams{
		StoreID: ids.UUID(storeID), Entity: "store_item", EntityID: ids.UUID(itemID), Op: "upsert", Now: s.clock.Now(),
	})
	if err != nil {
		return fmt.Errorf("catalogue: catalogue change: %w", err)
	}
	return nil
}

func (s *Service) updateItem(ctx context.Context, bid, sid, itemID string, h *string, b *catalogueapi.StoreItemUpdate) (catalogueapi.StoreItem, int32, error) {
	if err := s.ready(ctx); err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	sc, err := scope(bid, sid)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	id, err := pathID(itemID)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	if b == nil {
		return catalogueapi.StoreItem{}, 0, problem.New(genproblems.BadRequest, "")
	}
	setPrice, setMrp := sent(ctx, "pricePaise"), sent(ctx, "mrpPaise")
	setListed, setLow := sent(ctx, "isListed"), sent(ctx, "lowStockAt")
	if setPrice || setMrp {
		if err := s.also(ctx, policy.ItemPriceEdit, sc); err != nil {
			return catalogueapi.StoreItem{}, 0, err
		}
	}
	version, err := ifMatch(h)
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	if setPrice && b.PricePaise == nil {
		return catalogueapi.StoreItem{}, 0, fieldErr("body.pricePaise", "must not be null")
	}
	if setListed && b.IsListed == nil {
		return catalogueapi.StoreItem{}, 0, fieldErr("body.isListed", "must not be null")
	}
	arg := cataloguedb.UpdateStoreItemParams{
		SetPrice: setPrice, SetMrp: setMrp, SetListed: setListed, SetLowStock: setLow,
		MrpPaise: int64p(b.MrpPaise), ID: ids.UUID(id), StoreID: ids.UUID(sc.StoreID), Version: version,
	}
	if b.PricePaise != nil {
		arg.PricePaise = int64(*b.PricePaise)
	}
	if b.IsListed != nil {
		arg.IsListed = *b.IsListed
	}
	if b.LowStockAt != nil {
		arg.LowStockAt = ptr(i32i(*b.LowStockAt)) // contract: 0..1,000,000
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		// The store's catalogue sequence first: every writer takes the
		// stores row before the item (lock order, catalogue.go).
		if err := s.recordChange(ctx, s.db.Q(ctx), sc.StoreID, id); err != nil {
			return err
		}
		q := cataloguedb.New(s.db.Q(ctx))
		cur, err := q.GetStoreItem(ctx, cataloguedb.GetStoreItemParams{ID: ids.UUID(id), StoreID: ids.UUID(sc.StoreID)})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("catalogue: item: %w", err)
		}
		if cur.Version != version {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if _, err := q.UpdateStoreItem(ctx, arg); err != nil {
			if db.IsNoRows(err) {
				return problem.New(genproblems.PreconditionFailed, "")
			}
			return fmt.Errorf("catalogue: update item: %w", err)
		}
		return nil
	})
	if err != nil {
		return catalogueapi.StoreItem{}, 0, err
	}
	return s.item(ctx, sc.StoreID, id)
}

// --- conversions --------------------------------------------------------------

func ulidOf(u pgtype.UUID) string {
	id, _ := ids.FromUUID(u)
	return id.String()
}

func intp(v *int64) *int {
	if v == nil {
		return nil
	}
	return ptr(int(*v))
}

func int64p(v *int) *int64 {
	if v == nil {
		return nil
	}
	return ptr(int64(*v))
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// trimmed is s without surrounding space; nil when that leaves nothing.
func trimmed(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

// writerProblem maps the writer's errors to the API's problems.
func writerProblem(err error) error {
	var over *OverError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &over):
		return fieldErr("body.quantity", fmt.Sprintf("must be at most %d", over.Max))
	case errors.Is(err, ErrItemNotFound), errors.Is(err, ErrMovementNotFound):
		return problem.New(genproblems.NotFound, "")
	case errors.Is(err, ErrBatchNotFound):
		return fieldErr("body.batchIds", "not batches of this item")
	case errors.Is(err, ErrNothingToWriteOff):
		return problem.New(genproblems.Conflict, "nothing left to write off in these batches")
	case errors.Is(err, ErrQuantity):
		return fieldErr("body.quantity", "out of range")
	case errors.Is(err, ErrClientIDReused):
		return problem.New(genproblems.Conflict, "")
	}
	return err
}
