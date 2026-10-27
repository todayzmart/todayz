package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/democatalogue"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/dbenv"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/ids"
)

func runDemo(ctx context.Context, args []string, getenv func(string) string, out io.Writer) error {
	if len(args) == 0 || args[0] != "catalogue" {
		return usageError{msg: "demo takes catalogue [--limit N] [--dry-run] [--from DIR]"}
	}
	fs := flag.NewFlagSet("demo catalogue", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	limit := fs.Int("limit", 1200, "maximum products to import")
	dry := fs.Bool("dry-run", false, "inspect only")
	source := fs.String("source", "off", "product database: off, obf or opf")
	categoryTag := fs.String("category-tag", "", "optional Open Facts category tag (for focused India searches)")
	searchTerms := fs.String("search-terms", "", "optional Open Facts search words (for focused India searches)")
	from := fs.String("from", "", "directory of raw Open Facts search JSON (defaults under .cache)")
	if err := fs.Parse(args[1:]); err != nil || fs.NArg() > 0 {
		return usageError{msg: "demo catalogue takes [--source off|obf|opf] [--category-tag TAG | --search-terms WORDS] [--limit N] [--dry-run] [--from DIR]"}
	}
	if *categoryTag != "" && *searchTerms != "" {
		return usageError{msg: "use only one of --category-tag or --search-terms"}
	}
	if *limit < 1 || *limit > 1500 {
		return usageError{msg: "--limit must be between 1 and 1500"}
	}
	cfg, _, err := cliBoot("demo", getenv, out)
	if err != nil {
		return err
	}
	if cfg.Env != config.EnvLocal && cfg.Env != config.EnvTest {
		return errors.New("demo catalogue is available only in APP_ENV=local or test")
	}
	if cfg.NoEgress {
		return errors.New("demo catalogue calls an Open Facts database; it cannot run with TODAYZ_NO_EGRESS")
	}
	baseURL, label := factsSource(*source)
	if baseURL == "" {
		return usageError{msg: "--source must be off, obf or opf"}
	}
	dir := *from
	if dir == "" {
		dir = filepath.Join(".cache", *source+"-demo")
	}
	if !cfg.DatabaseURL.IsSet() {
		return errors.New("DATABASE_URL is not set")
	}
	products, err := loadFacts(ctx, baseURL, label, dir, *categoryTag, *searchTerms, *limit)
	if err != nil {
		return err
	}
	if *dry {
		_, err = fmt.Fprintf(out, "%s products with front photos: %d\n", label, len(products))
		return err
	}
	pool, err := db.Open(ctx, cfg.DatabaseURL.Reveal(), db.Options{AppName: "todayz-demo-catalogue", MaxConns: 4})
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := dbenv.RequireDev(ctx, pool, cfg.Env, "demo catalogue"); err != nil {
		return err
	}
	d := db.New(pool)
	stores, err := demoStores(ctx, d)
	if err != nil {
		return err
	}
	if len(stores) == 0 {
		return errors.New("no live demo stores found; seed the local demo database first")
	}
	added := 0
	skipped := 0
	gen := ids.NewGenerator()
	clk := clock.Real{}
	writer := catalogue.NewWriter(clk)
	catalogueService, err := catalogue.New(catalogue.Deps{DB: d, Clock: clk})
	if err != nil {
		return err
	}
	for _, p := range products {
		cat, ok := democatalogue.CategoryForSource(*source, p.CategoriesTags, p.Categories, strings.Join(p.LabelsTags, " "))
		if !ok {
			skipped++
			continue
		}
		err = d.InTx(ctx, func(txctx context.Context) error {
			now := clk.Now()
			name := p.Name
			if len(name) > 120 {
				name = name[:120]
			}
			brand := p.Brands
			if i := strings.Index(brand, ","); i >= 0 {
				brand = brand[:i]
			}
			brand = strings.TrimSpace(brand)
			pack, unit := parsePack(p.Quantity)
			price := demoPrice(cat, pack, unit)
			id, created, e := catalogueService.ImportDemoProduct(txctx, catalogue.DemoProduct{Name: name, Brand: brand, CategorySlug: cat, Barcode: p.Code, PackSize: pack, Unit: unit, MRPPaise: price})
			if e != nil {
				return e
			}
			if !created {
				return nil
			}
			q := d.Q(txctx)
			for i, sid := range stores {
				itemID := gen.New(now)
				var got pgtype.UUID
				e = q.QueryRow(txctx, `INSERT INTO store_items(id,store_id,product_id,price_paise,mrp_paise,low_stock_at,is_listed,search,created_at,updated_at) SELECT $1,$2,$3,$4,$5,3,true,p.search || setweight(to_tsvector('simple',coalesce(b.name,'')),'B'),$6,$6 FROM products p LEFT JOIN brands b ON b.id=p.brand_id WHERE p.id=$3 ON CONFLICT(store_id,product_id) DO NOTHING RETURNING id`, ids.UUID(itemID), ids.UUID(sid), ids.UUID(id), price, price, now).Scan(&got)
				if e != nil {
					return e
				}
				iid, _ := ids.FromUUID(got)
				_, e = writer.AddStock(txctx, d.Q(txctx), catalogue.StockIn{StoreID: sid, ItemID: iid, Qty: int32(8 + i), Source: catalogue.SourceRestock, Note: ptrString(label + " local demo catalogue")})
				if e != nil {
					return e
				}
			}
			added++
			return nil
		})
		if err != nil {
			return fmt.Errorf("import %s barcode %s: %w", label, p.Code, err)
		}
	}
	_, err = fmt.Fprintf(out, "Added %d products; skipped %d unmapped products from %s. Run `todayz images import --source %s --newest --limit %d` to queue front images for review.\n", added, skipped, label, *source, *limit)
	return err
}

func factsSource(source string) (string, string) {
	switch source {
	case "off":
		return "https://world.openfoodfacts.org", "Open Food Facts"
	case "obf":
		return "https://world.openbeautyfacts.org", "Open Beauty Facts"
	case "opf":
		return "https://world.openproductsfacts.org", "Open Products Facts"
	default:
		return "", ""
	}
}

func loadFacts(ctx context.Context, baseURL, label, dir, categoryTag, searchTerms string, limit int) ([]democatalogue.Product, error) {
	cachePrefix := "page"
	if categoryTag != "" {
		cachePrefix += "-" + slugify(categoryTag)
	} else if searchTerms != "" {
		cachePrefix += "-term-" + slugify(searchTerms)
	}
	pattern := cachePrefix + "-*.json"
	if categoryTag == "" && searchTerms == "" {
		pattern = cachePrefix + "-[0-9]*.json"
	}
	entries, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	all := make([]democatalogue.Product, 0)
	seen := map[string]bool{}
	for _, name := range entries {
		f, e := os.Open(name) // #nosec G304 -- a cache file this command wrote, found by glob
		if e != nil {
			return nil, e
		}
		ps, e := democatalogue.Decode(f)
		_ = f.Close()
		if e != nil {
			return nil, e
		}
		for _, p := range ps {
			if !seen[p.Code] {
				all = append(all, p)
				seen[p.Code] = true
			}
		}
	}
	if len(all) < limit {
		all, err = fetchFacts(ctx, baseURL, label, dir, cachePrefix, categoryTag, searchTerms, limit, all, len(entries)+1)
		if err != nil {
			return nil, err
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func fetchFacts(ctx context.Context, baseURL, label, dir, cachePrefix, categoryTag, searchTerms string, limit int, all []democatalogue.Product, startPage int) ([]democatalogue.Product, error) {
	client := httpx.NewOutboundClient(httpx.OutboundOptions{Timeout: 30 * time.Second})
	ua := "TodayZ-demo/0.1 (contact: https://todayz.in)"
	for page := startPage; len(all) < limit; page++ {
		if page > startPage {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Second):
			}
		}
		v := url.Values{"countries_tags": {"en:india"}, "page_size": {"100"}, "page": {""}, "fields": {"code,product_name,brands,quantity,categories,categories_tags,labels_tags,image_front_url,url,creator,contributors"}, "json": {"1"}}
		v.Set("page", fmt.Sprint(page))
		if categoryTag != "" {
			v.Set("categories_tags", categoryTag)
		} else if searchTerms != "" {
			v.Set("search_terms", searchTerms)
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/v2/search?"+v.Encode(), nil)
		if e != nil {
			return nil, e
		}
		req.Header.Set("User-Agent", ua)
		resp, e := client.Do(req)
		if e != nil {
			return nil, e
		}
		if resp.StatusCode != http.StatusOK {
			status := resp.StatusCode
			_ = resp.Body.Close()
			if status == http.StatusServiceUnavailable && len(all) > 0 {
				return all, nil
			}
			return nil, fmt.Errorf("%s answered %s", label, resp.Status)
		}
		var raw json.RawMessage
		raw, e = io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		_ = resp.Body.Close()
		if e != nil {
			return nil, e
		}
		if e = os.MkdirAll(dir, 0o750); e != nil {
			return nil, e
		}
		if e = os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%04d.json", cachePrefix, page)), raw, 0o600); e != nil {
			return nil, e
		}
		ps, e := democatalogue.Decode(strings.NewReader(string(raw)))
		if e != nil {
			return nil, e
		}
		if len(ps) == 0 {
			break
		}
		for _, p := range ps {
			if !containsCode(all, p.Code) {
				all = append(all, p)
			}
		}
	}
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}

func containsCode(products []democatalogue.Product, code string) bool {
	for _, p := range products {
		if p.Code == code {
			return true
		}
	}
	return false
}

func demoStores(ctx context.Context, d *db.DB) ([]ids.ID, error) {
	rows, err := d.Q(ctx).Query(ctx, `SELECT id FROM stores WHERE status='live' ORDER BY created_at,id LIMIT 3`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ids.ID
	for rows.Next() {
		var id pgtype.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		v, _ := ids.FromUUID(id)
		out = append(out, v)
	}
	return out, rows.Err()
}
func parsePack(s string) (float64, string) {
	s = strings.ToLower(strings.TrimSpace(s))
	var n float64
	var u string
	if _, err := fmt.Sscanf(s, "%f %s", &n, &u); err != nil || n <= 0 {
		return 1, "pack"
	}
	switch strings.TrimSuffix(u, "s") {
	case "kg":
		return n, "kg"
	case "g":
		return n, "g"
	case "l":
		return n, "l"
	case "ml":
		return n, "ml"
	default:
		return n, "pack"
	}
}
func demoPrice(cat string, n float64, unit string) int64 {
	base := int64(19900)
	switch cat {
	case "staples":
		base = 8000
	case "dairy-eggs":
		base = 6000
	case "snacks":
		base = 3000
	case "beverages":
		base = 4000
	case "spices-oils":
		base = 9000
	case "household":
		base = 12000
	case "personal-care":
		base = 9000
	case "baby-care":
		base = 18000
	}
	if unit == "g" || unit == "ml" {
		base = base * int64(max(1, n)) / 500
	}
	return max(1000, base)
}
func slugify(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
func ptrString(s string) *string { return &s }
