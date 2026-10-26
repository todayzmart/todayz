package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/testclock"
	"github.com/todayz/backend/internal/testkit"
)

// newCatalogueAPI is newBizAPI with the settings cache the API also has (the
// catalogue reads low_stock_default and the stale-stock hours from it).
func newCatalogueAPI(t *testing.T) *bizAPI {
	t.Helper()
	tdb := testkit.NewDB(t)
	cfg, err := config.Load(env(localEnv(tdb)), nil)
	require.NoError(t, err)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)) // Friday 11:30 IST
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	d := deps{
		Config: cfg, Log: log, Clock: clk,
		DB: db.New(tdb.Pool), Ready: &health.Readiness{},
		Settings:  settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{Log: log}),
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}
	h, err := buildRouter(d, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	return &bizAPI{t: t, h: h, clk: clk, tdb: tdb}
}

// catalogueShop is a business with two stores, its owner, a manager, a
// cashier of store 1 and a cashier of store 2 only, plus a second business
// with its own owner and store, and two master products.
type catalogueShop struct {
	a                                  *bizAPI
	owner, manager, cashier, cashierS2 string
	ownerY                             string
	B, S, S2, Y, SY                    string
	atta, butter                       string
}

func newCatalogueShop(t *testing.T) *catalogueShop {
	t.Helper()
	a := newCatalogueAPI(t)
	c := &catalogueShop{a: a}
	c.owner = a.signIn(ownerPhone, "business")
	c.ownerY = a.signIn(otherOwnerPhone, "business")
	bid, sid := a.newBusiness(c.owner, "Lakshmi Traders")
	c.B, c.S = "/v1/biz/businesses/"+bid, "/v1/biz/businesses/"+bid+"/stores/"+sid
	r := a.call(c.owner, http.MethodPost, c.B+"/stores", map[string]any{"name": "Lakshmi Traders 2", "locality": "BTM Layout"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	sid2 := r.body["id"].(string)
	c.S2 = c.B + "/stores/" + sid2
	by, sy := a.newBusiness(c.ownerY, "Ganesh Stores")
	c.Y, c.SY = "/v1/biz/businesses/"+by, "/v1/biz/businesses/"+by+"/stores/"+sy

	invite := func(phone, role string, stores []string) string {
		body := map[string]any{"phone": phone, "role": role}
		if stores != nil {
			body["storeIds"] = stores
		}
		r := a.call(c.owner, http.MethodPost, c.B+"/staff", body, nil)
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		staff := r.body["id"].(string)
		tok := a.signIn(phone, "business")
		r = a.call(tok, http.MethodPost, c.B+"/staff/"+staff+"/accept", nil, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		return tok
	}
	c.manager = invite(managerPhone, "manager", nil)
	c.cashier = invite(cashierPhone, "cashier", []string{sid})
	c.cashierS2 = invite(cashier2Phone, "cashier", []string{sid2})

	c.atta, c.butter = ids.New(a.clk.Now()).String(), ids.New(a.clk.Now()).String()
	a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, mrp_paise, barcodes) VALUES
	  ($1, 'Aashirvaad Atta', '0190c47e-0000-7000-8000-000000000101', 5, 'kg', 30000, '{8901725133856}'),
	  ($2, 'Amul Butter', '0190c47e-0000-7000-8000-000000000101', 100, 'g', 6000, '{8901262010016}')`,
		uuidOf(t, c.atta), uuidOf(t, c.butter))
	return c
}

// storeID is the last segment of a store path.
func (c *catalogueShop) storeID(path string) string { return path[len(path)-26:] }

func idem(a *bizAPI) map[string]string {
	return map[string]string{"Idempotency-Key": ids.New(a.clk.Now()).String()}
}

func (c *catalogueShop) move(tok, item string, body map[string]any) resp {
	c.a.t.Helper()
	return c.a.call(tok, http.MethodPost, c.S+"/items/"+item+"/movements", body, idem(c.a))
}

func batchesOf(r resp) []map[string]any {
	var out []map[string]any
	for _, it := range r.body["items"].([]any) {
		out = append(out, it.(map[string]any))
	}
	return out
}

// TestCatalogueHTTP walks items, movements, history and batches through the
// real pipeline by role (02 §3.3 catalogue rows; SC-CT in this slice).
func TestCatalogueHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	c := newCatalogueShop(t)
	a := c.a
	today := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC) // IST date of the fake clock
	day := func(n int) string { return today.AddDate(0, 0, n).Format(time.DateOnly) }

	// --- categories are public; product search is for members ---------------
	r := a.call("", http.MethodGet, "/v1/discover/categories", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotEmpty(t, r.body["items"])
	r = a.call(c.cashier, http.MethodGet, c.B+"/products/search?barcode=8901262010016", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	require.Equal(t, c.butter, r.body["items"].([]any)[0].(map[string]any)["id"])

	// --- create a store item from the master (SC-CT); above the MRP is allowed (D-245)
	item := map[string]any{"productId": c.butter, "pricePaise": 6100, "openingStock": 10, "expiresOn": day(3), "costPaise": 4800}
	r = a.call(c.cashier, http.MethodPost, c.S+"/items", item, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a cashier adds no items: %s", r.raw)
	r = a.call(c.manager, http.MethodPost, c.S+"/items", item, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	butter := r.body["id"].(string)
	require.EqualValues(t, 10, r.body["onHand"])
	require.Equal(t, `"v1"`, r.hdr.Get("ETag"))
	r = a.call(c.manager, http.MethodPost, c.S+"/items", item, nil)
	require.Equal(t, http.StatusConflict, r.code, "one item per product per store: %s", r.raw)
	r = a.call(c.owner, http.MethodPost, c.S+"/items", map[string]any{"productId": c.atta, "pricePaise": 29000}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	atta := r.body["id"].(string)
	require.EqualValues(t, 0, r.body["onHand"])

	r = a.call(c.cashier, http.MethodGet, c.S+"/items?q=butter", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, r.body["items"], 1)
	r = a.call(c.cashier, http.MethodGet, c.B+"/products/search?q=amul&storeId="+c.storeID(c.S), nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, true, r.body["items"].([]any)[0].(map[string]any)["inStore"], r.raw)

	// --- PATCH an item: If-Match required (428), stale (412), price by role --
	cur := a.call(c.cashier, http.MethodGet, c.S+"/items/"+butter, nil, nil)
	require.Equal(t, http.StatusOK, cur.code, cur.raw)
	r = a.call(c.manager, http.MethodPatch, c.S+"/items/"+butter, map[string]any{"pricePaise": 5700}, nil)
	require.Equal(t, http.StatusPreconditionRequired, r.code, r.raw)
	r = a.call(c.manager, http.MethodPatch, c.S+"/items/"+butter, map[string]any{"pricePaise": 5700}, map[string]string{"If-Match": `"v9"`})
	require.Equal(t, http.StatusPreconditionFailed, r.code, r.raw)
	r = a.call(c.cashier, http.MethodPatch, c.S+"/items/"+butter, map[string]any{"isListed": false}, ifMatch(cur))
	require.Equal(t, http.StatusForbidden, r.code, "a cashier changes no listing: %s", r.raw)
	r = a.call(c.manager, http.MethodPatch, c.S+"/items/"+butter, map[string]any{"pricePaise": 5700, "lowStockAt": 3}, ifMatch(cur))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 5700, r.body["pricePaise"])
	require.Equal(t, `"v2"`, r.hdr.Get("ETag"))
	r = a.call(c.manager, http.MethodPatch, c.S+"/items/"+butter, map[string]any{"pricePaise": 5600}, ifMatch(cur))
	require.Equal(t, http.StatusPreconditionFailed, r.code, "the old ETag is stale: %s", r.raw)

	// --- movements by role (D-06), source outside the enum refused (D-01) ----
	r = a.call(c.manager, http.MethodPost, c.S+"/items/"+butter+"/movements", map[string]any{"kind": "restock", "quantity": 1}, nil)
	require.Equal(t, http.StatusBadRequest, r.code, "an Idempotency-Key is required: %s", r.raw)
	r = c.move(c.manager, butter, map[string]any{"kind": "sale_pos", "quantity": 1})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "a sale is not a stock movement kind: %s", r.raw)
	r = c.move(c.cashier, butter, map[string]any{"kind": "restock", "quantity": 5})
	require.Equal(t, http.StatusForbidden, r.code, "a cashier restocks nothing: %s", r.raw)
	r = c.move(c.cashier, butter, map[string]any{"kind": "damage", "quantity": 1})
	require.Equal(t, http.StatusForbidden, r.code, "a cashier writes off nothing: %s", r.raw)
	r = c.move(c.manager, butter, map[string]any{"kind": "damage", "quantity": 11})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "damage beyond available: %s", r.raw)
	require.Contains(t, fieldsOf(r), "body.quantity", r.raw)

	key := idem(a)
	restock := map[string]any{"kind": "restock", "quantity": 8, "expiresOn": day(1), "costPaise": 4700}
	r = a.call(c.manager, http.MethodPost, c.S+"/items/"+butter+"/movements", restock, key)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.EqualValues(t, 18, r.body["item"].(map[string]any)["onHand"])
	r = a.call(c.manager, http.MethodPost, c.S+"/items/"+butter+"/movements", restock, key)
	require.Equal(t, http.StatusCreated, r.code, "a replay answers the stored response: %s", r.raw)
	require.EqualValues(t, 18, r.body["item"].(map[string]any)["onHand"], "and writes nothing")

	// Damage takes the earliest expiry first (D-99): the +1 batch, not +3.
	r = c.move(c.owner, butter, map[string]any{"kind": "damage", "quantity": 2, "note": "torn packs"})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	mv := r.body["movements"].([]any)[0].(map[string]any)
	require.EqualValues(t, -2, mv["delta"])
	require.Len(t, mv["batches"], 1)

	// A cashier counts; a count equal to on hand writes nothing; a count up
	// adds a "found in count" batch with no expiry.
	r = c.move(c.cashier, butter, map[string]any{"kind": "count_correction", "quantity": 16})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Empty(t, r.body["movements"], "a count equal to on hand moves nothing")
	r = c.move(c.cashier, butter, map[string]any{"kind": "count_correction", "quantity": 17})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.EqualValues(t, 17, r.body["item"].(map[string]any)["onHand"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM stock_batches WHERE store_item_id = $1 AND found_in_count AND expires_on IS NULL AND quantity = 1`, uuidOf(t, butter)))

	// --- history: on hand equals the sum of movements (SC-CT) -----------------
	r = a.call(c.cashier, http.MethodGet, c.S+"/items/"+butter+"/history", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	var sum float64
	for _, m := range r.body["items"].([]any) {
		sum += m.(map[string]any)["delta"].(float64)
	}
	require.EqualValues(t, 17, sum)
	require.Len(t, r.body["items"], 4, "opening, restock, damage, count")

	// --- batches: manager and above; going to waste; PATCH cost/expiry -------
	r = a.call(c.cashier, http.MethodGet, c.S+"/batches", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "batches are manager and above: %s", r.raw)
	r = a.call(c.manager, http.MethodGet, c.S+"/batches?expiresBefore="+day(6), nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	waste := batchesOf(r)
	require.Len(t, waste, 2, "both dated batches expire within 5 days: %s", r.raw)
	require.Equal(t, day(1), waste[0]["expiresOn"], "earliest expiry first")
	require.EqualValues(t, 6, waste[0]["quantity"])
	require.EqualValues(t, 10, waste[1]["quantity"])
	first, second := waste[0], waste[1]

	bpath := c.S + "/batches/" + first["id"].(string)
	r = a.call(c.manager, http.MethodPatch, bpath, map[string]any{"costPaise": 4500}, nil)
	require.Equal(t, http.StatusPreconditionRequired, r.code, r.raw)
	r = a.call(c.cashier, http.MethodPatch, bpath, map[string]any{"costPaise": 4500}, map[string]string{"If-Match": first["etag"].(string)})
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = a.call(c.manager, http.MethodPatch, bpath, map[string]any{"costPaise": 4500, "expiresOn": day(-1)}, map[string]string{"If-Match": first["etag"].(string)})
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 4500, r.body["costPaise"])
	require.Equal(t, true, r.body["expired"])
	require.Equal(t, r.hdr.Get("ETag"), r.body["etag"])
	r = a.call(c.manager, http.MethodPatch, bpath, map[string]any{"costPaise": 4400}, map[string]string{"If-Match": first["etag"].(string)})
	require.Equal(t, http.StatusPreconditionFailed, r.code, "stale batch ETag: %s", r.raw)
	r = a.call(c.manager, http.MethodPatch, bpath, map[string]any{"quantity": 99}, map[string]string{"If-Match": `"v2"`})
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "cost and expiry only: %s", r.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.batch_update' AND subject_id = $1`, first["id"]))
	r = a.call(c.manager, http.MethodPost, c.S+"/batches", map[string]any{"itemId": butter}, idem(a))
	require.Equal(t, http.StatusMethodNotAllowed, r.code, "no endpoint creates a batch: %s", r.raw)

	// --- Mark expired: one movement per ticked batch, at min(batch, available)
	r = c.move(c.manager, butter, map[string]any{"kind": "expired", "batchIds": []string{first["id"].(string)}})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Len(t, r.body["movements"], 1)
	require.EqualValues(t, -6, r.body["movements"].([]any)[0].(map[string]any)["delta"])
	require.EqualValues(t, 11, r.body["item"].(map[string]any)["onHand"])
	r = c.move(c.manager, butter, map[string]any{"kind": "expired", "batchIds": []string{first["id"].(string)}})
	require.Equal(t, http.StatusConflict, r.code, "nothing left in that batch: %s", r.raw)
	r = a.call(c.manager, http.MethodGet, c.S+"/batches?expiresBefore="+day(6), nil, nil)
	require.Len(t, batchesOf(r), 1, "a batch used up leaves going to waste: %s", r.raw)
	require.Equal(t, second["id"], batchesOf(r)[0]["id"])
	r = a.call(c.manager, http.MethodGet, c.S+"/batches?empty=true&itemId="+butter, nil, nil)
	require.Len(t, batchesOf(r), 3, "empty=true includes used-up batches: %s", r.raw)

	// --- stale stock (> 72 h) is flagged (SC-CT; ranking is W1-06's search) --
	r = a.call(c.cashier, http.MethodGet, c.S+"/items/"+atta, nil, nil)
	require.Equal(t, true, r.body["stale"], "stock never confirmed is stale: %s", r.raw)
	r = a.call(c.cashier, http.MethodGet, c.S+"/items/"+butter, nil, nil)
	require.Equal(t, false, r.body["stale"], r.raw)
	require.True(t, strings.HasSuffix(r.body["createdAt"].(string), "Z"), "timestamps are UTC: %s", r.raw)
	require.True(t, strings.HasSuffix(r.body["updatedAt"].(string), "Z"), "timestamps are UTC: %s", r.raw)
	// Last confirmed 73 h ago (moving the clock instead would expire the tokens).
	a.exec(`UPDATE store_items SET stock_updated_at = $2 WHERE id = $1`, uuidOf(t, butter), a.clk.Now().Add(-73*time.Hour))
	r = a.call(c.cashier, http.MethodGet, c.S+"/items/"+butter, nil, nil)
	require.Equal(t, true, r.body["stale"], r.raw)
	r = c.move(c.cashier, butter, map[string]any{"kind": "count_correction", "quantity": 11})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, false, r.body["item"].(map[string]any)["stale"], "a confirming count makes the stock fresh")
}

// TestCatalogueTenancyHTTP: no one reaches another business's catalogue, by
// its paths or by putting its store under their own business, and a cashier
// of one store reaches nothing of the other store.
func TestCatalogueTenancyHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	c := newCatalogueShop(t)
	a := c.a
	r := a.call(c.ownerY, http.MethodPost, c.SY+"/items", map[string]any{"productId": c.butter, "pricePaise": 5800, "openingStock": 4, "expiresOn": "2026-10-30"}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	yItem := r.body["id"].(string)
	yCur := r
	r = a.call(c.ownerY, http.MethodGet, c.SY+"/batches", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	yBatch := batchesOf(r)[0]
	r = a.call(c.owner, http.MethodPost, c.S+"/items", map[string]any{"productId": c.atta, "pricePaise": 29000, "openingStock": 3}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	xItem := r.body["id"].(string)

	sy := c.SY[len(c.Y):] // "/stores/<sid>"
	bm := map[string]string{"If-Match": yBatch["etag"].(string)}
	type call struct {
		method, path string
		body         any
		hdr          map[string]string
	}
	calls := func(base string) []call {
		return []call{
			{http.MethodGet, base + "/items", nil, nil},
			{http.MethodGet, base + "/items/" + yItem, nil, nil},
			{http.MethodPost, base + "/items", map[string]any{"productId": c.atta, "pricePaise": 100}, nil},
			{http.MethodPatch, base + "/items/" + yItem, map[string]any{"pricePaise": 100}, ifMatch(yCur)},
			{http.MethodPost, base + "/items/" + yItem + "/movements", map[string]any{"kind": "count_correction", "quantity": 0}, idem(a)},
			{http.MethodGet, base + "/items/" + yItem + "/history", nil, nil},
			{http.MethodGet, base + "/batches", nil, nil},
			{http.MethodPatch, base + "/batches/" + yBatch["id"].(string), map[string]any{"costPaise": 1}, bm},
		}
	}
	// Y's own paths, and Y's store under X's business.
	for _, cl := range append(calls(c.SY), calls(c.B+sy)...) {
		for _, tok := range []string{c.owner, c.manager, c.cashier} {
			r := a.call(tok, cl.method, cl.path, cl.body, cl.hdr)
			require.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, r.code, "%s %s: %s", cl.method, cl.path, r.raw)
		}
	}
	r = a.call(c.owner, http.MethodGet, c.B+"/products/search?q=amul&storeId="+c.storeID(c.S), nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["items"].([]any)[0].(map[string]any)["inStore"], "Y's listing is not X's")
	r = a.call(c.ownerY, http.MethodGet, c.Y+"/products/search?q="+url.QueryEscape("aashirvaad")+"&storeId="+c.storeID(c.SY), nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, false, r.body["items"].([]any)[0].(map[string]any)["inStore"], "X's listing is not Y's")
	// Asking whether Y's store lists a product, from X's business, is refused.
	r = a.call(c.owner, http.MethodGet, c.B+"/products/search?q=amul&storeId="+c.storeID(c.SY), nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = a.call(c.cashierS2, http.MethodGet, c.B+"/products/search?q=amul&storeId="+c.storeID(c.S), nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "a store-2 cashier asks nothing of store 1: %s", r.raw)

	// X's item named under X's other store, and Y's item under X's store.
	r = a.call(c.owner, http.MethodGet, c.S2+"/items/"+xItem, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = a.call(c.owner, http.MethodGet, c.S+"/items/"+yItem, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = a.call(c.owner, http.MethodPost, c.S+"/items/"+yItem+"/movements", map[string]any{"kind": "restock", "quantity": 1}, idem(a))
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = a.call(c.owner, http.MethodPatch, c.S+"/batches/"+yBatch["id"].(string), map[string]any{"costPaise": 1}, bm)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)

	// A cashier of store 2 reaches nothing of store 1.
	r = a.call(c.cashierS2, http.MethodGet, c.S+"/items", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = a.call(c.cashierS2, http.MethodPost, c.S+"/items/"+xItem+"/movements", map[string]any{"kind": "count_correction", "quantity": 0}, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = a.call(c.cashierS2, http.MethodGet, c.S2+"/items", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)

	// Nothing of Y changed.
	r = a.call(c.ownerY, http.MethodGet, c.SY+"/items/"+yItem, nil, nil)
	require.EqualValues(t, 4, r.body["onHand"])
	require.EqualValues(t, 5800, r.body["pricePaise"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM stock_movements WHERE store_item_id = $1`, uuidOf(t, yItem)))
	require.Equal(t, 0, a.count(`SELECT count(*) FROM audit_log WHERE action = 'catalogue.batch_update'`))
}

// TestCatalogueCreateProductHTTP creates a missing product (D-37): manager
// and above, pending review, listable at once in the store that created it
// and nowhere else.
func TestCatalogueCreateProductHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	c := newCatalogueShop(t)
	a := c.a
	r := a.call("", http.MethodGet, "/v1/discover/categories", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	category := r.body["items"].([]any)[0].(map[string]any)["id"].(string)

	body := map[string]any{
		"storeId": c.storeID(c.S), "name": "Jaggery", "brand": "Local Mill", "packSize": 500, "unit": "g",
		"mrpPaise": 6000, "categoryId": category, "barcodes": []string{"8909876543210"},
	}
	r = a.call(c.cashier, http.MethodPost, c.B+"/products", body, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, "a cashier creates no products: %s", r.raw)
	r = a.call(c.owner, http.MethodPost, c.B+"/products", map[string]any{
		"storeId": c.storeID(c.SY), "name": "Jaggery", "packSize": 500, "unit": "g", "categoryId": category,
	}, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, "another business's store: %s", r.raw)
	r = a.call(c.manager, http.MethodPost, c.B+"/products", body, idem(a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "pending_review", r.body["status"])
	require.Equal(t, "Local Mill", r.body["brand"])
	require.EqualValues(t, 6000, r.body["mrpPaise"])
	product := r.body["id"].(string)

	r = a.call(c.manager, http.MethodPost, c.B+"/products", body, idem(a))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "the barcode is taken now: %s", r.raw)
	r = a.call(c.manager, http.MethodPost, c.B+"/products", map[string]any{
		"storeId": c.storeID(c.S), "name": "Jaggery", "packSize": 500, "unit": "g", "categoryId": ids.New(a.clk.Now()).String(),
	}, idem(a))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, "unknown category: %s", r.raw)

	// The creating store lists it at once, at any price (D-245); other stores
	// wait for review.
	r = a.call(c.manager, http.MethodPost, c.S+"/items", map[string]any{"productId": product, "pricePaise": 6500, "openingStock": 5}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	require.Equal(t, "Jaggery", r.body["name"])
	r = a.call(c.owner, http.MethodPost, c.S2+"/items", map[string]any{"productId": product, "pricePaise": 6500}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	r = a.call(c.ownerY, http.MethodPost, c.SY+"/items", map[string]any{"productId": product, "pricePaise": 6500}, nil)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
}
