package imageimport

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
	"github.com/todayz/backend/internal/platform/storage/storagetest"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

var t0 = time.Date(2026, 10, 26, 6, 30, 0, 0, time.UTC)

const (
	catStaples   = "0190c47e-0000-7000-8000-000000000101"
	catDairy     = "0190c47e-0000-7000-8000-000000000102"
	catBeverages = "0190c47e-0000-7000-8000-000000000105"
	catHousehold = "0190c47e-0000-7000-8000-000000000108"
	catMedicines = "0190c47e-0000-7000-8000-000000000201"
)

// fakeFacts serves the three databases and their image host from testdata
// (SC-PL-04: no network in tests).
type fakeFacts struct {
	mu      sync.Mutex
	images  *httptest.Server
	dbs     map[string]*httptest.Server
	agents  map[string]bool
	calls   []string
	tooMany int // the status a throttled database answers, 0 when it is not
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func newFakeFacts(t *testing.T) *fakeFacts {
	t.Helper()
	f := &fakeFacts{dbs: map[string]*httptest.Server{}, agents: map[string]bool{}}
	f.images = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.record(r, "img")
		http.ServeFile(w, r, filepath.Join("testdata", "images", filepath.Base(r.URL.Path)))
	}))
	t.Cleanup(f.images.Close)
	for _, s := range Sources {
		code := s.Code
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			f.record(r, code)
			if f.tooMany != 0 {
				w.WriteHeader(f.tooMany)
				return
			}
			var file string
			switch {
			case strings.HasPrefix(r.URL.Path, "/api/v2/product/"):
				require.Equal(t, fields, r.URL.Query().Get("fields"))
				file = filepath.Join("testdata", code, "product", filepath.Base(r.URL.Path)+".json")
			case r.URL.Path == "/cgi/search.pl":
				q := r.URL.Query()
				require.Equal(t, "1", q.Get("json"))
				require.Equal(t, "5", q.Get("page_size"))
				slug := strings.Trim(slugRe.ReplaceAllString(strings.ToLower(q.Get("search_terms")), "-"), "-")
				file = filepath.Join("testdata", code, "search", slug+".json")
			}
			b, err := os.ReadFile(file) //nolint:gosec // test fixtures
			if file == "" || err != nil {
				if code != "off" || !strings.HasPrefix(r.URL.Path, "/cgi/") {
					http.NotFound(w, r)
					return
				}
				b = []byte(`{"count":0,"products":[]}`)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.ReplaceAll(string(b), "{{images}}", f.images.URL)))
		}))
		t.Cleanup(srv.Close)
		f.dbs[code] = srv
	}
	return f
}

func (f *fakeFacts) record(r *http.Request, who string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents[r.Header.Get("User-Agent")] = true
	f.calls = append(f.calls, who+" "+r.URL.Path)
}

func (f *fakeFacts) options(limit int) Options {
	o := Options{UserAgent: UserAgent("test", "https://todayz.in"), Limit: limit, BaseURLs: map[string]string{}}
	for code, srv := range f.dbs {
		o.BaseURLs[code] = srv.URL
	}
	u, _ := url.Parse(f.images.URL)
	o.ImageHosts = []string{u.Host}
	return o
}

type world struct {
	t      *testing.T
	tdb    *testkit.DB
	fake   *storagetest.Fake
	facts  *fakeFacts
	clk    *clock.Fake
	slept  []time.Duration
	store  ids.ID
	brands map[string]ids.ID
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	w := &world{t: t, tdb: testkit.NewDB(t), fake: storagetest.New(t), facts: newFakeFacts(t), clk: clock.NewFake(t0),
		store: ids.New(t0), brands: map[string]ids.ID{}}
	biz := ids.New(t0)
	w.exec(`INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'B', 'ABCDEF')`, ids.UUID(biz))
	w.exec(`INSERT INTO stores (id, business_id, slug, short_code, name) VALUES ($1, $2, 's1', 'ABCDE2', 'S1')`, ids.UUID(w.store), ids.UUID(biz))
	return w
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	_, err := w.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(w.t, err, sql)
}

func (w *world) scalar(sql string, args ...any) any {
	w.t.Helper()
	var v any
	require.NoError(w.t, w.tdb.OwnerPool.QueryRow(context.Background(), sql, args...).Scan(&v), sql)
	return v
}

func (w *world) product(name, brand, category string, barcodes ...string) ids.ID {
	w.t.Helper()
	var brandID any
	if brand != "" {
		b, ok := w.brands[brand]
		if !ok {
			b = ids.New(t0)
			w.brands[brand] = b
			w.exec(`INSERT INTO brands (id, name, slug) VALUES ($1, $2, $3)`, ids.UUID(b), brand,
				strings.Trim(slugRe.ReplaceAllString(strings.ToLower(brand), "-"), "-"))
		}
		brandID = ids.UUID(b)
	}
	if barcodes == nil {
		barcodes = []string{}
	}
	id := ids.New(t0)
	w.exec(`INSERT INTO products (id, name, brand_id, category_id, pack_size, unit, status, barcodes)
	  VALUES ($1, $2, $3, $4, 1, 'pc', 'active', $5)`, ids.UUID(id), name, brandID, category, barcodes)
	return id
}

func (w *world) importer(o Options) *Importer {
	w.t.Helper()
	im, err := New(Deps{
		DB: db.New(w.tdb.Pool), Store: storage.New(w.fake.Config(), w.fake.Client()), Client: &http.Client{Timeout: 5 * time.Second},
		Clock: w.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sleep: func(_ context.Context, d time.Duration) error {
			w.slept = append(w.slept, d)
			w.clk.Advance(d)
			return nil
		},
	}, o)
	require.NoError(w.t, err)
	return im
}

func names(lines []Line) []string {
	out := []string{}
	for _, l := range lines {
		out = append(out, l.Name)
	}
	return out
}

func TestImportFillsMissingImages(t *testing.T) {
	w := newWorld(t)
	amul := w.product("Amul Butter 100g", "Amul", catDairy, "8901262010016")
	odonil := w.product("Odonil Room Freshener", "Odonil", catHousehold, "8901023012345")
	coke := w.product("Coca-Cola Original Taste 750 ml", "Coca-Cola", catBeverages)
	w.product("Tata Tea Gold", "Tata Tea", catBeverages)
	w.product("Dolo 650", "Micro Labs", catMedicines, "8901234567894")
	w.product("Onion", "", catStaples)
	w.product("Hotlinked", "Elsewhere", catStaples, "8900000000017")
	w.product("No picture", "Plain", catStaples, "8900000000024")
	w.product("A gif", "Odd", catStaples, "8900000000031")
	// Already has a photo waiting for review, or an import was turned down:
	// not tried.
	waiting := w.product("Waiting", "Amul", catDairy, "8901262010016")
	w.exec(`INSERT INTO product_images (id, product_id, source, status, object_key, content_type, store_id)
	  VALUES ($1, $2, 'retailer', 'pending', 'k1', 'image/jpeg', $3)`, ids.UUID(ids.New(t0)), ids.UUID(waiting), ids.UUID(w.store))
	refused := w.product("Refused", "Amul", catDairy, "8901262010016")
	w.exec(`INSERT INTO product_images (id, product_id, source, status, object_key, content_type, licence, attribution, source_url, page_url, match)
	  VALUES ($1, $2, 'off', 'rejected', 'k2', 'image/jpeg', 'CC BY-SA 3.0', 'Open Food Facts contributors', 'https://x', 'https://y', 'barcode')`, ids.UUID(ids.New(t0)), ids.UUID(refused))

	rep, err := w.importer(w.facts.options(50)).Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 9, rep.Tried)
	require.ElementsMatch(t, []string{"Amul Butter 100g", "Odonil Room Freshener", "Coca-Cola Original Taste 750 ml"}, names(rep.Imported))
	require.ElementsMatch(t, []string{"Tata Tea Gold", "No picture"}, names(rep.Missed))
	require.Equal(t, []string{"Dolo 650"}, names(rep.Medicines))
	require.Equal(t, []string{"Onion"}, names(rep.NoKey))
	require.ElementsMatch(t, []string{"Hotlinked", "A gif"}, names(rep.Failed))

	// Each import is a pending draft with its source, page, licence and author,
	// copied into the private bucket; the product has no image until approval.
	type row struct{ source, key, ct, ref, page, licence, author, match, status string }
	get := func(p ids.ID) row {
		var r row
		require.NoError(t, w.tdb.OwnerPool.QueryRow(context.Background(), `SELECT source, object_key, content_type, source_ref, page_url, licence, attribution, match, status
		  FROM product_images WHERE product_id = $1`, ids.UUID(p)).Scan(&r.source, &r.key, &r.ct, &r.ref, &r.page, &r.licence, &r.author, &r.match, &r.status))
		return r
	}
	a := get(amul)
	require.Equal(t, row{"off", a.key, "image/jpeg", "8901262010016", w.facts.dbs["off"].URL + "/product/8901262010016",
		"CC BY-SA 3.0", "Open Food Facts contributors", "barcode", "pending"}, a)
	require.True(t, strings.HasPrefix(a.key, "product-images/"+amul.String()+"/"))
	body, ok := w.fake.Private(a.key)
	require.True(t, ok)
	want, _ := os.ReadFile("testdata/images/amul.jpg")
	require.Equal(t, want, body)
	require.Nil(t, w.scalar(`SELECT image_id FROM products WHERE id = $1`, ids.UUID(amul)))

	// Household looks in Open Products Facts first, then falls back.
	o := get(odonil)
	require.Equal(t, "off", o.source)
	require.Equal(t, "barcode", o.match)
	// No barcode: matched by brand and name, skipping the wrong variant.
	c := get(coke)
	require.Equal(t, "name", c.match)
	require.Equal(t, "5449000000996", c.ref)
	require.Equal(t, "image/png", c.ct)

	// Every call named the app; the databases were paced.
	require.Equal(t, map[string]bool{"TodayZ-ImageImport/test (https://todayz.in)": true}, w.facts.agents)
	require.NotEmpty(t, w.slept)
	for _, d := range w.slept {
		require.LessOrEqual(t, d, SearchPace)
	}
	require.Contains(t, w.facts.calls, "opf /api/v2/product/8901023012345")
	for _, c := range w.facts.calls {
		require.NotContains(t, c, "8901234567894", "medicines are never looked up")
	}

	// A second run finds nothing new to import.
	rep, err = w.importer(w.facts.options(50)).Run(context.Background())
	require.NoError(t, err)
	require.Empty(t, rep.Imported)
}

func TestImportDryRunAndLimit(t *testing.T) {
	w := newWorld(t)
	w.product("Amul Butter", "Amul", catDairy, "8901262010016")
	w.product("Onion", "", catStaples)
	o := w.facts.options(1)
	o.DryRun = true
	im, err := New(Deps{DB: db.New(w.tdb.Pool), Client: &http.Client{}, Clock: w.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, o)
	require.NoError(t, err, "a dry run needs no storage")
	rep, err := im.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, rep.Tried)
	require.Len(t, rep.Imported, 1)
	require.EqualValues(t, 0, w.scalar(`SELECT count(*) FROM product_images`))
	require.Empty(t, w.fake.Images())
	for _, c := range w.facts.calls {
		require.False(t, strings.HasPrefix(c, "img "), "a dry run downloads nothing")
	}
}

func TestImportStopsWhenRateLimited(t *testing.T) {
	// Open Food Facts sheds load with a 503 page ("unusually high demand"), so it stops a run like a 429.
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			w := newWorld(t)
			w.product("Amul Butter", "Amul", catDairy, "8901262010016")
			w.product("Amul Cheese", "Amul", catDairy, "8901262010023")
			w.facts.tooMany = status
			rep, err := w.importer(w.facts.options(10)).Run(context.Background())
			require.ErrorIs(t, err, ErrRateLimited)
			require.Equal(t, 1, rep.Tried)
			require.Len(t, w.facts.calls, 1, "no call after the first refusal")
		})
	}
}

func TestNewRefuses(t *testing.T) {
	d := Deps{DB: &db.DB{}, Client: &http.Client{}, Clock: clock.NewFake(t0), Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	_, err := New(d, Options{UserAgent: "x", Limit: 1})
	require.ErrorContains(t, err, "storage")
	_, err = New(d, Options{Limit: 1, DryRun: true})
	require.ErrorContains(t, err, "User-Agent")
	_, err = New(d, Options{UserAgent: "x", DryRun: true})
	require.ErrorContains(t, err, "limit")
	_, err = New(d, Options{UserAgent: "x", Limit: 1, DryRun: true, Only: "amazon"})
	require.ErrorContains(t, err, "unknown source")
}

func TestMatching(t *testing.T) {
	require.Equal(t, "obf", primary("personal-care", "grocery"))
	require.Equal(t, "obf", primary("skin-care", "beauty"))
	require.Equal(t, "opf", primary("household", "grocery"))
	require.Equal(t, "opf", primary("batteries-lighting", "electronics"))
	require.Equal(t, "off", primary("snacks", "grocery"))
	require.True(t, skipped("medicines", "pharmacy"))
	require.False(t, skipped("baby-care", "pharmacy"))

	require.True(t, barcode("8901262010016"))
	require.True(t, barcode("12345678"))
	require.False(t, barcode("1234567"))
	require.False(t, barcode("89012620100AB"))
	require.False(t, barcode("2900000010192"), "a shop's in-store code is another product elsewhere")
	require.False(t, barcode("212345678901"), "UPC-A random weight")
	require.False(t, barcode("0412345678903"), "company internal")
	require.False(t, barcode("9812345678901"), "coupon")
	require.False(t, barcode("21234567"), "EAN-8 in-store")
	require.True(t, barcode("0690225106542"))
	require.True(t, barcode("012345678905"), "UPC-A")
	require.True(t, barcode("9780140449136"), "a book")

	require.Equal(t, []string{"amul", "butter"}, tokens("Amul Butter 500g (Pack of 2)"))
	require.True(t, nameMatch("Parle-G Gold Biscuits 1kg", "Parle", "Parle-G Gold Biscuits", "Parle"))
	require.False(t, nameMatch("Parle-G Gold Biscuits", "Parle", "Parle-G Gold Biscuits", "Britannia"), "another brand")
	require.False(t, nameMatch("Boost Health Drink 500g", "Boost", "Boost Energy Bar", "Boost"))
	require.False(t, nameMatch("Amul", "Amul", "Amul Butter", "Amul"), "nothing beyond the brand to match")
	require.False(t, nameMatch("Butter", "", "Butter", "Amul"), "no brand")
	require.Equal(t, []string{"india", "gate", "basmati", "rice"}, tokens("India Gate Basmati rice 1 kg"))
	require.False(t, nameMatch("India Gate Basmati rice 1 kg", "India Gate", "Brown Rice India gate 1 kg", "India Gate"), "a unit is not a matching word")
}
