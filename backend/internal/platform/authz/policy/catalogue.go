package policy

import "github.com/todayz/backend/internal/platform/authz"

// Catalogue actions (W1-05, 02 §3.3 "catalogue" rows). items.bulk_import and
// catalogue.sync arrive with the import and snapshot/delta work that W1-05's
// weekend slice deferred.
const (
	ItemView      = "item.view"
	ItemManage    = "item.manage"
	ItemPriceEdit = "item.price.edit"
	StockCount    = "stock.count"
	StockRestock  = "stock.restock"
	StockDamage   = "stock.damage"
	BatchesManage = "batches.manage"
	ProductCreate = "product.create"
)

func init() {
	all := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow, authz.Cashier: authz.Allow}
	ownerManager := map[authz.Role]authz.Grant{authz.Owner: authz.Allow, authz.Manager: authz.Allow}
	Register(authz.Table{Domain: "catalogue", Actions: []authz.Action{
		{Name: ItemView, Kind: authz.KindBusiness, Grants: all, Doc: "GET {S}/items, {S}/items/{itemId}, item history, {B}/products/search"},
		{Name: ItemManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {S}/items (add item), PATCH {S}/items/{itemId} isListed and lowStockAt"},
		{Name: ItemPriceEdit, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "pricePaise and mrpPaise on POST {S}/items and PATCH {S}/items/{itemId}"},
		{Name: StockCount, Kind: authz.KindBusiness, Grants: all, Doc: "movement count_correction"},
		{Name: StockRestock, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "movements restock, supplier_bill"},
		{Name: StockDamage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "movements damage, expired (D-06)"},
		{Name: ProductCreate, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "POST {B}/products (a missing product, pending review, D-37); POST {B}/stores/{sid}/products/{productId}/photos and .../{photoId}/confirm (its photo, reviewed by an admin, D-252)"},
		{Name: BatchesManage, Kind: authz.KindBusiness, Grants: ownerManager, Doc: "GET {S}/batches, PATCH {S}/batches/{batchId} (cost and expiry, D-99)"},
	}})
}
