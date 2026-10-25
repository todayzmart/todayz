package catalogue

import (
	"context"
	"fmt"
	"strings"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/cataloguedb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Field agents (W3-06, A-04) add a shop's products one by one while they
// onboard it. The business domain owns those routes and decides who may
// write (the agent who onboarded the business, inside the D-39 window); these
// methods only do the catalogue part, in the caller's transaction where they
// write. They run no permission check of their own.

// FieldItem is a store item as the field routes show it.
type FieldItem struct {
	ID, StoreID, ProductID ids.ID
	Name                   string // the product's name (a local name is the shop's, not the agent's)
	Brand, Variant         *string
	PackSize               float64
	Unit                   string
	PricePaise             int64
	MrpPaise               *int64 // the store's MRP
	ProductMrpPaise        *int64
	ProductStatus          string // active, pending_review, merged, retired
	OnHand                 int32
	IsListed               bool
	CreatedAt, UpdatedAt   time.Time
}

// FieldNewProduct is a product the master catalogue does not have yet; it is
// created pending_review and goes to the A-08 queue.
type FieldNewProduct struct {
	Name       string
	CategoryID ids.ID
	Variant    *string
	PackSize   float64
	Unit       string
	MrpPaise   *int64
	Barcode    *string
}

// FieldItemCreate adds one product to a store: ProductID (an active master
// product) or NewProduct, never both.
type FieldItemCreate struct {
	StoreID      ids.ID
	ProductID    *ids.ID
	NewProduct   *FieldNewProduct
	PricePaise   int64
	MrpPaise     *int64
	OpeningStock int32
	IsListed     bool
	AgentID      ids.ID
}

// FieldItemUpdate changes an item's price, MRP or listing; a Set flag false
// leaves the field alone.
type FieldItemUpdate struct {
	StoreID, ItemID ids.ID
	SetPrice        bool
	PricePaise      int64
	SetMrp          bool
	MrpPaise        *int64
	SetListed       bool
	IsListed        bool
}

// FieldProductMatch is a master product an agent may list.
type FieldProductMatch struct {
	ID, CategoryID ids.ID
	Name           string
	Brand, Variant *string
	PackSize       float64
	Unit           string
	MrpPaise       *int64
	InStore        bool
}

// fieldSearchLimit is how many matches the agent's search returns (the
// contract says up to 20).
const fieldSearchLimit = 20 //settings:ok a page size

// FieldSearchProducts finds active master products by name words or barcode.
func (s *Service) FieldSearchProducts(ctx context.Context, storeID ids.ID, q, barcode *string) ([]FieldProductMatch, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	var v invalid
	q = trimmed(q)
	barcode = trimmed(barcode)
	v.text("query.q", q)
	if q == nil && barcode == nil {
		v.add("query.q", "send q or barcode")
	}
	if err := v.err(); err != nil {
		return nil, err
	}
	arg := cataloguedb.SearchProductsParams{StoreID: ids.UUID(storeID), Barcode: barcode, Lim: fieldSearchLimit}
	if q != nil {
		arg.Q, arg.Pattern = q, ptr(likePattern(*q))
	}
	rows, err := s.q(ctx).SearchProducts(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("catalogue: field search: %w", err)
	}
	out := make([]FieldProductMatch, 0, len(rows))
	for _, r := range rows {
		id, _ := ids.FromUUID(r.ID)
		cat, _ := ids.FromUUID(r.CategoryID)
		out = append(out, FieldProductMatch{
			ID: id, CategoryID: cat, Name: r.Name, Brand: r.BrandName, Variant: r.Variant,
			PackSize: r.PackSize, Unit: r.Unit, MrpPaise: r.MrpPaise, InStore: r.InStore,
		})
	}
	return out, nil
}

// FieldListItems is a page of a store's items, newest first. before is the
// last id of the previous page (zero for the first); more is true when there
// is another page.
func (s *Service) FieldListItems(ctx context.Context, storeID, before ids.ID, limit int32) (items []FieldItem, more bool, err error) {
	if err := s.ready(ctx); err != nil {
		return nil, false, err
	}
	arg := cataloguedb.ListFieldItemsParams{StoreID: ids.UUID(storeID), Lim: limit + 1}
	if before != ids.Zero {
		arg.BeforeID = ids.UUID(before)
	}
	rows, err := s.q(ctx).ListFieldItems(ctx, arg)
	if err != nil {
		return nil, false, fmt.Errorf("catalogue: field items: %w", err)
	}
	if len(rows) > int(limit) {
		rows, more = rows[:limit], true
	}
	items = make([]FieldItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, fieldItem(cataloguedb.GetFieldItemRow{
			ID: r.ID, StoreID: r.StoreID, ProductID: r.ProductID, LocalName: r.LocalName, PricePaise: r.PricePaise,
			MrpPaise: r.MrpPaise, OnHand: r.OnHand, IsListed: r.IsListed, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			ProductName: r.ProductName, Variant: r.Variant, PackSize: r.PackSize, Unit: r.Unit,
			ProductStatus: r.ProductStatus, BrandName: r.BrandName, ProductMrpPaise: r.ProductMrpPaise,
		}))
	}
	return items, more, nil
}

func fieldItem(r cataloguedb.GetFieldItemRow) FieldItem {
	id, _ := ids.FromUUID(r.ID)
	sid, _ := ids.FromUUID(r.StoreID)
	pid, _ := ids.FromUUID(r.ProductID)
	return FieldItem{
		ID: id, StoreID: sid, ProductID: pid, Name: r.ProductName, Brand: r.BrandName, Variant: r.Variant,
		PackSize: r.PackSize, Unit: r.Unit, PricePaise: r.PricePaise, MrpPaise: r.MrpPaise,
		ProductMrpPaise: r.ProductMrpPaise, ProductStatus: r.ProductStatus, OnHand: r.OnHand,
		IsListed: r.IsListed, CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

// FieldItem reads one item of a store.
func (s *Service) FieldItem(ctx context.Context, storeID, itemID ids.ID) (FieldItem, error) {
	if err := s.ready(ctx); err != nil {
		return FieldItem{}, err
	}
	r, err := s.q(ctx).GetFieldItem(ctx, cataloguedb.GetFieldItemParams{ID: ids.UUID(itemID), StoreID: ids.UUID(storeID)})
	if db.IsNoRows(err) {
		return FieldItem{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return FieldItem{}, fmt.Errorf("catalogue: field item: %w", err)
	}
	return fieldItem(r), nil
}

// FieldCreateItem adds one product to a store and returns the new item's id.
// It must run inside the caller's transaction (the business domain records
// the agent's add in the same one). A new product is created pending_review
// with the store as its creator, so the A-08 queue sees it; the item lists
// it at once for the shop, and search shows it to customers only once the
// product is active (search reads active products).
//
// TODO(lead): 01 §5 does not say whether a store item of a pending product
// is visible to customers before A-08 approves it; this follows the search
// rule above.
func (s *Service) FieldCreateItem(ctx context.Context, in FieldItemCreate) (ids.ID, error) {
	if err := s.ready(ctx); err != nil {
		return ids.Zero, err
	}
	now := s.clock.Now()
	q := s.db.Q(ctx)
	cq := cataloguedb.New(q)
	var productID ids.ID
	var productMRP *int64
	switch {
	case in.ProductID != nil && in.NewProduct != nil:
		return ids.Zero, fieldErr("body.newProduct", "send productId or newProduct, not both")
	case in.ProductID != nil:
		prod, err := cq.GetProduct(ctx, ids.UUID(*in.ProductID))
		if db.IsNoRows(err) || (err == nil && prod.Status != "active") {
			return ids.Zero, fieldErr("body.productId", "no such product")
		}
		if err != nil {
			return ids.Zero, fmt.Errorf("catalogue: product: %w", err)
		}
		productID, productMRP = *in.ProductID, prod.MrpPaise
	case in.NewProduct != nil:
		np := in.NewProduct
		var v invalid
		name := strings.TrimSpace(np.Name)
		v.text("body.newProduct.name", &name)
		if name == "" {
			v.add("body.newProduct.name", "required")
		}
		variant := trimmed(np.Variant)
		v.text("body.newProduct.variant", variant)
		barcodes := []string{}
		if b := trimmed(np.Barcode); b != nil {
			taken, err := cq.BarcodeTaken(ctx, *b)
			if err != nil {
				return ids.Zero, fmt.Errorf("catalogue: barcode: %w", err)
			}
			if taken {
				v.add("body.newProduct.barcode", "a product with this barcode exists; search for it")
			}
			barcodes = append(barcodes, *b)
		}
		if err := v.err(); err != nil {
			return ids.Zero, err
		}
		productID = s.ids.New(now)
		if err := cq.InsertProduct(ctx, cataloguedb.InsertProductParams{
			ID: ids.UUID(productID), Name: name, CategoryID: ids.UUID(np.CategoryID), Variant: variant,
			PackSize: np.PackSize, Unit: np.Unit, MrpPaise: np.MrpPaise, Barcodes: barcodes,
			Status: "pending_review", CreatedByStoreID: ids.UUID(in.StoreID), Now: now,
		}); err != nil {
			if db.IsForeignKeyViolation(err, "fk_products_category_id") {
				return ids.Zero, fieldErr("body.newProduct.categoryId", "unknown category")
			}
			return ids.Zero, fmt.Errorf("catalogue: new product: %w", err)
		}
		productMRP = np.MrpPaise
	default:
		return ids.Zero, fieldErr("body.productId", "send productId or newProduct")
	}
	if m := effectiveMRP(in.MrpPaise, productMRP); m != nil && in.PricePaise > *m {
		return ids.Zero, fieldErr("body.pricePaise", "must not be above the MRP")
	}
	itemID := s.ids.New(now)
	if _, err := cq.InsertStoreItem(ctx, cataloguedb.InsertStoreItemParams{
		ID: ids.UUID(itemID), StoreID: ids.UUID(in.StoreID), ProductID: ids.UUID(productID),
		PricePaise: in.PricePaise, MrpPaise: in.MrpPaise, IsListed: in.IsListed, Now: now,
	}); err != nil {
		if db.IsUniqueViolation(err, "ux_store_items_store_product") {
			return ids.Zero, problem.New(genproblems.Conflict, "the store already lists this product")
		}
		return ids.Zero, fmt.Errorf("catalogue: insert item: %w", err)
	}
	if in.OpeningStock > 0 {
		_, err := s.writer.AddStock(ctx, q, StockIn{
			StoreID: in.StoreID, ItemID: itemID, Qty: in.OpeningStock, Source: SourceOpening,
			Who: Who{UserID: in.AgentID},
		})
		if err := writerProblem(err); err != nil {
			return ids.Zero, err
		}
		return itemID, nil
	}
	return itemID, s.recordChange(ctx, q, in.StoreID, itemID)
}

// FieldUpdateItem changes an item's price, MRP or listing, inside the
// caller's transaction. There is no If-Match on the field routes: the agent
// is the only writer while onboarding, so the change applies to the current
// version.
func (s *Service) FieldUpdateItem(ctx context.Context, in FieldItemUpdate) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	q := s.db.Q(ctx)
	// The store's catalogue sequence first (lock order, catalogue.go).
	if err := s.recordChange(ctx, q, in.StoreID, in.ItemID); err != nil {
		return err
	}
	cq := cataloguedb.New(q)
	cur, err := cq.GetStoreItem(ctx, cataloguedb.GetStoreItemParams{ID: ids.UUID(in.ItemID), StoreID: ids.UUID(in.StoreID)})
	if db.IsNoRows(err) {
		return problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return fmt.Errorf("catalogue: item: %w", err)
	}
	price, mrp := cur.PricePaise, cur.MrpPaise
	if in.SetPrice {
		price = in.PricePaise
	}
	if in.SetMrp {
		mrp = in.MrpPaise
	}
	if m := effectiveMRP(mrp, cur.ProductMrpPaise); m != nil && price > *m {
		field := "body.pricePaise"
		if !in.SetPrice {
			field = "body.mrpPaise"
		}
		return fieldErr(field, "the price must not be above the MRP")
	}
	if _, err := cq.UpdateStoreItem(ctx, cataloguedb.UpdateStoreItemParams{
		SetPrice: in.SetPrice, PricePaise: in.PricePaise, SetMrp: in.SetMrp, MrpPaise: in.MrpPaise,
		SetListed: in.SetListed, IsListed: in.IsListed,
		ID: ids.UUID(in.ItemID), StoreID: ids.UUID(in.StoreID), Version: cur.Version,
	}); err != nil {
		if db.IsNoRows(err) {
			return problem.New(genproblems.Conflict, "the item changed; try again")
		}
		return fmt.Errorf("catalogue: update item: %w", err)
	}
	return nil
}
