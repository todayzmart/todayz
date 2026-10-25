package main

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// ulidOf turns a fixed uuid from a migration into the API's id form.
func ulidOf(t *testing.T, u string) string {
	t.Helper()
	var p pgtype.UUID
	require.NoError(t, p.Scan(u))
	id, ok := ids.FromUUID(p)
	require.True(t, ok)
	return id.String()
}

// W3-05 A-08: the master catalogue. catalogue reads and works the review queue;
// admin also creates and edits products, categories and synonyms; each audited.
func TestAdminCatalogueHTTP(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "catalogue")
	tok := desk.token()
	a := o.a
	get := func(path string) resp { t.Helper(); return a.call(tok, http.MethodGet, path, nil, nil) }
	post := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPost, path, body, nil) }
	patch := func(path string, body any) resp { t.Helper(); return a.call(tok, http.MethodPatch, path, body, nil) }
	newID := func() string { return ids.New(a.clk.Now()).String() }
	// The seeded "staples" child category and its "grocery" parent (10501).
	catStaples := ulidOf(t, "0190c47e-0000-7000-8000-000000000101")
	catGrocery := ulidOf(t, "0190c47e-0000-7000-8000-000000000100")

	// Reads: the three products the shop lists, newest first, by name or barcode.
	r := get("/v1/admin/products?limit=2")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 2)
	require.NotEmpty(t, r.body["nextCursor"])
	r = get("/v1/admin/products?limit=2&cursor=" + url.QueryEscape(r.body["nextCursor"].(string)))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.GreaterOrEqual(t, len(r.body["items"].([]any)), 1)
	r = get("/v1/admin/products?q=" + url.QueryEscape("amul"))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	butter := r.body["items"].([]any)[0].(map[string]any)
	require.Equal(t, "Amul Butter", butter["name"])
	require.Equal(t, float64(1), butter["storeCount"])
	require.Equal(t, "Atta, rice and dal", butter["categoryName"])
	r = get("/v1/admin/products?q=8901262010016")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.Equal(t, butter["id"], r.body["items"].([]any)[0].(map[string]any)["id"])
	r = get("/v1/admin/products/" + butter["id"].(string))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, butter["id"], r.body["id"])
	r = get("/v1/admin/products/" + newID())
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	// catalogue may not create or edit; admin may.
	create := map[string]any{"name": "Toor Dal", "categoryId": catStaples, "variant": "Unpolished", "packSize": 1, "unit": "kg", "mrpPaise": 18900, "barcodes": []string{"8901725181234"}}
	r = post("/v1/admin/products", create)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	r = post("/v1/admin/products", create)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	toor := r.body["id"].(string)
	require.Equal(t, "active", r.body["status"])
	require.Equal(t, "Unpolished", r.body["variant"])
	require.Equal(t, float64(0), r.body["storeCount"])
	r = post("/v1/admin/products", map[string]any{"name": "Toor Dal 2", "categoryId": catStaples, "packSize": 1, "unit": "kg", "barcodes": []string{"8901725181234"}})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "conflict", problemCode(r))
	r = post("/v1/admin/products", map[string]any{"name": "Toor Dal 2", "categoryId": newID(), "packSize": 1, "unit": "kg"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.categoryId")

	// PATCH: a null variant clears it, an unsent mrp stays; a reason is required.
	r = patch("/v1/admin/products/"+toor, map[string]any{"variant": nil})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = patch("/v1/admin/products/"+toor, map[string]any{"reason": "the pack has no variant", "variant": nil})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Nil(t, r.body["variant"])
	require.Equal(t, float64(18900), r.body["mrpPaise"])
	r = patch("/v1/admin/products/"+toor, map[string]any{"reason": "same code as the butter", "barcodes": []string{"8901262010016"}})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	// A rename refreshes the search text of every store item of the product.
	r = patch("/v1/admin/products/"+butter["id"].(string), map[string]any{"reason": "the label says salted", "name": "Amul Salted Butter"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM store_items WHERE product_id = $1 AND search @@ to_tsquery('simple', 'salted')`, uuidOf(t, butter["id"].(string))))
	r = patch("/v1/admin/products/"+toor, map[string]any{"reason": "no longer made", "status": "retired"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "retired", r.body["status"])
	r = patch("/v1/admin/products/"+toor, map[string]any{"reason": "back on the shelves", "status": "active"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, 3, a.count(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.product.update' AND subject_type = 'product' AND subject_id = $1`, toor))

	// The review queue: oldest first, with duplicate suggestions; catalogue decides.
	a.exec(`UPDATE admin_users SET roles = ARRAY['catalogue']`)
	pending := func(name string) string {
		id := newID()
		a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, status, created_by_store_id) VALUES ($1, $2, $3, 1, 'kg', 'pending_review', $4)`,
			uuidOf(t, id), name, uuidOf(t, catStaples), uuidOf(t, o.sid))
		return id
	}
	p1, p2, p3 := pending("Toor Dal Unpolished"), pending("Tata Salt Iodised"), pending("Mystery Service")
	r = get("/v1/admin/pending-products")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	items := r.body["items"].([]any)
	require.Len(t, items, 3)
	require.Equal(t, false, r.body["assistEnabled"], "ai_catalogue_enabled is off by default")
	first := items[0].(map[string]any)
	require.Equal(t, p1, first["product"].(map[string]any)["id"])
	require.NotEmpty(t, first["createdByStoreName"])
	sugg := first["suggestions"].([]any)
	require.NotEmpty(t, sugg)
	require.Equal(t, toor, sugg[0].(map[string]any)["product"].(map[string]any)["id"])

	r = post("/v1/admin/pending-products/"+p1+"/approve", map[string]any{})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "active", r.body["status"])
	r = post("/v1/admin/pending-products/"+p1+"/approve", map[string]any{})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))

	r = post("/v1/admin/pending-products/"+p3+"/reject", map[string]any{})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post("/v1/admin/pending-products/"+p3+"/reject", map[string]any{"reason": "a service, not a product"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "retired", r.body["status"])

	// Merge: not into itself, not into a non-active product; synonyms move with it.
	r = post("/v1/admin/pending-products/"+p2+"/merge", map[string]any{"intoProductId": p2})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.intoProductId")
	r = post("/v1/admin/pending-products/"+p2+"/merge", map[string]any{"intoProductId": p3})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = get("/v1/admin/products?q=" + url.QueryEscape("tata salt") + "&status=active")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	saltID := r.body["items"].([]any)[0].(map[string]any)["id"].(string)
	a.exec(`INSERT INTO product_synonyms (id, term, synonym, lang, product_id) VALUES ($1, 'salt', 'namak', 'hi', $2), ($3, 'salt', 'uppu', 'te', $2), ($4, 'salt', 'namak', 'hi', $5)`,
		uuidOf(t, newID()), uuidOf(t, p2), uuidOf(t, newID()), uuidOf(t, newID()), uuidOf(t, saltID))
	r = post("/v1/admin/pending-products/"+p2+"/merge", map[string]any{"intoProductId": saltID})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "merged", r.body["status"])
	require.Equal(t, saltID, r.body["mergedInto"])
	require.Equal(t, 0, a.count(`SELECT count(*) FROM product_synonyms WHERE product_id = $1`, uuidOf(t, p2)))
	require.Equal(t, 2, a.count(`SELECT count(*) FROM product_synonyms WHERE product_id = $1`, uuidOf(t, saltID)))
	// A pending product a store already lists cannot be merged away.
	a.exec(`UPDATE products SET status = 'pending_review' WHERE id = $1`, uuidOf(t, butter["id"].(string)))
	r = post("/v1/admin/pending-products/"+butter["id"].(string)+"/merge", map[string]any{"intoProductId": toor})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	require.Equal(t, "invalid_transition", problemCode(r))
	a.exec(`UPDATE products SET status = 'active' WHERE id = $1`, uuidOf(t, butter["id"].(string)))
	r = get("/v1/admin/pending-products")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Empty(t, r.body["items"])
	a.exec(`UPDATE platform_settings SET value = 'true' WHERE key = 'ai_catalogue_enabled'`)
	o.settings.Invalidate()
	r = get("/v1/admin/pending-products")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["assistEnabled"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.pending.merge' AND subject_id = $1`, p2))

	// Categories: catalogue reads; admin creates (slug unique, two levels) and edits.
	r = get("/v1/admin/categories")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotEmpty(t, r.body["items"])
	r = post("/v1/admin/categories", map[string]any{"slug": "festive", "names": map[string]any{"en": "Festive"}, "tint": "peach", "icon": "sparkles"})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	r = post("/v1/admin/categories", map[string]any{"slug": "festive", "parentId": catGrocery, "names": map[string]any{"en": " Festive ", "hi": "त्योहार"}, "tint": "peach", "icon": "sparkles", "sort": 90})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	festive := r.body["id"].(string)
	require.Equal(t, "Festive", r.body["names"].(map[string]any)["en"])
	r = post("/v1/admin/categories", map[string]any{"slug": "festive", "names": map[string]any{"en": "Festive"}, "tint": "peach", "icon": "sparkles"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = post("/v1/admin/categories", map[string]any{"slug": "diyas", "parentId": festive, "names": map[string]any{"en": "Diyas"}, "tint": "peach", "icon": "flame"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.parentId")
	r = patch("/v1/admin/categories/"+festive, map[string]any{"reason": "reordered for Diwali", "sort": 5, "tint": "amber"})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, float64(5), r.body["sort"])
	require.Equal(t, "amber", r.body["tint"])
	r = patch("/v1/admin/categories/"+newID(), map[string]any{"reason": "x", "sort": 5})
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	// Synonyms: lower-cased and trimmed, a pair once, delete audited.
	r = post("/v1/admin/synonyms", map[string]any{"term": " Akash Kandil ", "synonym": "DIWALI LANTERN", "lang": "en"})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	syn := r.body["id"].(string)
	require.Equal(t, "akash kandil", r.body["term"])
	require.Equal(t, "diwali lantern", r.body["synonym"])
	r = post("/v1/admin/synonyms", map[string]any{"term": "akash kandil", "synonym": "diwali lantern", "lang": "en"})
	require.Equal(t, http.StatusConflict, r.code, r.raw)
	r = post("/v1/admin/synonyms", map[string]any{"term": "curd", "synonym": "Curd", "lang": "en"})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = post("/v1/admin/synonyms", map[string]any{"term": "salt", "synonym": "lavan", "lang": "hi", "productId": p2})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.productId")
	r = get("/v1/admin/synonyms?q=akash")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	r = a.call(tok, http.MethodDelete, "/v1/admin/synonyms/"+syn, nil, nil)
	require.Equal(t, http.StatusNoContent, r.code, r.raw)
	r = a.call(tok, http.MethodDelete, "/v1/admin/synonyms/"+syn, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.synonym.delete' AND subject_type = 'synonym' AND subject_id = $1`, syn))

	// The product's audit side panel (catalogue sees it).
	a.exec(`UPDATE admin_users SET roles = ARRAY['catalogue']`)
	r = get("/v1/admin/audit-log/subject?subjectType=product&subjectId=" + toor)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 4, "create and three updates")
	// support has no catalogue access.
	a.exec(`UPDATE admin_users SET roles = ARRAY['support']`)
	r = get("/v1/admin/products")
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}
