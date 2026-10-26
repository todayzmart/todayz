package imageimport

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/imageimportdb"
	"github.com/todayz/backend/internal/platform/ids"
)

// Item-type photos (D-253, `todayz images types`). A product with no pack
// photo shows a free-licence photo of its kind ("toor dal", "toothpaste") from
// Wikimedia Commons. A kind is (category, item_type_key(name, brand)): the
// name without the brand and the trailing pack size. For each kind with a
// product still on the placeholder, the importer stores up to TypeCandidates
// Commons files as pending candidates in the private bucket; an admin picks
// one in A-08 (or turns them all down). A kind with a stored candidate is not
// searched again, so a turned-down file does not come back. The run also
// gives an approved kind's photo to products added since it was approved.

// CommonsAPI is the Wikimedia Commons API.
const CommonsAPI = "https://commons.wikimedia.org/w/api.php"

// CommonsHosts serve its files: originals on upload, resized copies on thumb.
var CommonsHosts = []string{"upload.wikimedia.org", "thumb.wikimedia.org"}

// CommonsPace spaces calls to Commons (its API asks bots to call one at a time).
const CommonsPace = 1 * time.Second

// TypeCandidates is how many files a kind keeps for review.
const TypeCandidates = 3

// thumbWidth is the width of the copy taken (Commons scales it for us).
const thumbWidth = 800

// minWidth is the narrowest original worth showing.
const minWidth = 400

// CommonsLicences are the licences a type photo may carry: free to copy and
// change with a credit. Anything else (fair use, GFDL only, non-commercial)
// is skipped. The customer app shows the credit (search.licenceURLs).
var CommonsLicences = map[string]bool{
	"CC BY-SA 2.0": true, "CC BY-SA 2.5": true, "CC BY-SA 3.0": true, "CC BY-SA 4.0": true,
	"CC BY 2.0": true, "CC BY 2.5": true, "CC BY 3.0": true, "CC BY 4.0": true,
	"CC0": true, "Public domain": true,
}

// TypeOptions shape one `images types` run.
type TypeOptions struct {
	UserAgent string // required
	Limit     int    // kinds to search; at least 1
	DryRun    bool   // search only; copy and store nothing
	APIURL    string // default CommonsAPI (tests)
	FileHosts []string
}

// TypeLine is one kind's outcome.
type TypeLine struct {
	Category string
	TypeKey  string
	Products int
	Files    []string // Commons file names stored (or that would be)
	Reason   string
}

// TypeReport is what a run did.
type TypeReport struct {
	Filled    int // products given an approved kind's photo
	Searched  int
	Found     []TypeLine
	Missed    []TypeLine
	Medicines []TypeLine
	Failed    []TypeLine
	DryRun    bool
}

// TypeImporter runs `images types`.
type TypeImporter struct {
	d     Deps
	o     TypeOptions
	hosts map[string]bool
	ids   *ids.Generator
	im    *Importer // for pacing and downloads
}

// NewTypes checks the options.
func NewTypes(d Deps, o TypeOptions) (*TypeImporter, error) {
	if d.DB == nil || d.Client == nil || d.Clock == nil || d.Log == nil {
		return nil, errors.New("imageimport: missing dependency")
	}
	if d.Store == nil && !o.DryRun {
		return nil, errors.New("imageimport: storage is not configured")
	}
	if o.UserAgent == "" {
		return nil, errors.New("imageimport: a User-Agent is required")
	}
	if o.Limit < 1 {
		return nil, errors.New("imageimport: limit must be at least 1")
	}
	if d.Sleep == nil {
		d.Sleep = sleep
	}
	if o.APIURL == "" {
		o.APIURL = CommonsAPI
	}
	hosts := o.FileHosts
	if hosts == nil {
		hosts = CommonsHosts
	}
	t := &TypeImporter{d: d, o: o, hosts: map[string]bool{}, ids: ids.NewGenerator()}
	for _, h := range hosts {
		t.hosts[h] = true
	}
	t.im = &Importer{d: d, o: Options{UserAgent: o.UserAgent}, hosts: t.hosts, ids: t.ids, last: map[string]time.Time{}}
	return t, nil
}

// Run first gives approved kinds' photos to products that lack one, then
// searches up to Limit kinds that have none yet.
func (t *TypeImporter) Run(ctx context.Context) (TypeReport, error) {
	rep := TypeReport{DryRun: t.o.DryRun}
	if !t.o.DryRun {
		n, err := t.fill(ctx)
		rep.Filled = n
		if err != nil {
			return rep, err
		}
	}
	kinds, err := imageimportdb.New(t.d.DB.Q(ctx)).TypeKinds(ctx, int32(t.o.Limit)) // #nosec G115 -- the CLI bounds the limit
	if err != nil {
		return rep, fmt.Errorf("imageimport: kinds: %w", err)
	}
	for _, k := range kinds {
		rep.Searched++
		line := TypeLine{Category: k.CategorySlug, TypeKey: k.TypeKey, Products: int(k.Products)}
		if skipped(k.CategorySlug, k.ParentSlug) {
			rep.Medicines = append(rep.Medicines, line)
			continue
		}
		files, err := t.kind(ctx, k)
		line.Files = files
		switch {
		case err != nil && stopping(ctx, err):
			return rep, err
		case err != nil:
			line.Reason = err.Error()
			rep.Failed = append(rep.Failed, line)
			t.d.Log.WarnContext(ctx, "type photo search failed", slog.String("type_key", k.TypeKey), slog.String("error", err.Error()))
		case len(files) == 0:
			line.Reason = "no free-licence photo found"
			rep.Missed = append(rep.Missed, line)
		default:
			rep.Found = append(rep.Found, line)
		}
	}
	return rep, nil
}

// fill gives each approved kind's photo to its products without an image.
func (t *TypeImporter) fill(ctx context.Context) (int, error) {
	approved, err := imageimportdb.New(t.d.DB.Q(ctx)).ApprovedTypePhotos(ctx)
	if err != nil {
		return 0, fmt.Errorf("imageimport: approved kinds: %w", err)
	}
	total := 0
	for _, a := range approved {
		if a.PublicKey == nil {
			continue
		}
		err := t.d.DB.InTx(ctx, func(ctx context.Context) error {
			q := imageimportdb.New(t.d.DB.Q(ctx))
			prods, err := q.TypeKindProducts(ctx, imageimportdb.TypeKindProductsParams{CategoryID: a.CategoryID, TypeKey: a.TypeKey, TypePhotoID: a.ID})
			if err != nil || len(prods) == 0 {
				return err
			}
			now := t.d.Clock.Now()
			imgIDs := make([]pgtype.UUID, len(prods))
			for i := range prods {
				imgIDs[i] = ids.UUID(t.ids.New(now))
			}
			if err := q.InsertTypeImages(ctx, imageimportdb.InsertTypeImagesParams{Now: now, Ids: imgIDs, ProductIds: prods, TypePhotoID: a.ID}); err != nil {
				return err
			}
			if err := q.SetTypeProductImages(ctx, imageimportdb.SetTypeProductImagesParams{ImageKey: *a.PublicKey, Now: now, Ids: imgIDs, ProductIds: prods}); err != nil {
				return err
			}
			total += len(prods)
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("imageimport: fill %s: %w", a.TypeKey, err)
		}
	}
	return total, nil
}

// commonsFile is one search result.
type commonsFile struct {
	Title    string
	ThumbURL string
	PageURL  string
	Licence  string
	Author   string
	Width    int
	Mime     string
	Restrict string
}

// queries are the Commons searches for a kind, best first: every word in the
// file's title ("toor dal" finds "Toor dal.jpg", not pigeon-pea fields), then
// the last two words for a longer kind ("classic salted chips" → "salted
// chips"), then a plain search.
func queries(typeKey string) []string {
	words := strings.Fields(typeKey)
	intitle := func(ws []string) string {
		q := make([]string, len(ws))
		for i, w := range ws {
			q[i] = "intitle:" + strings.ReplaceAll(w, `"`, "")
		}
		return strings.Join(q, " ")
	}
	out := []string{intitle(words)}
	if len(words) > 2 {
		out = append(out, intitle(words[len(words)-2:]))
	}
	return append(out, typeKey)
}

var digits = regexp.MustCompile(`[0-9()_\-.,]+`)

// sameShot reduces a file name to its words, so a series ("Toothbrush (2021)
// 02.jpg", "… 03.jpg") offers one candidate, not three.
func sameShot(title string) string {
	return strings.Join(strings.Fields(digits.ReplaceAllString(strings.ToLower(title), " ")), " ")
}

// kind searches Commons for a kind and stores up to TypeCandidates files.
func (t *TypeImporter) kind(ctx context.Context, k imageimportdb.TypeKindsRow) ([]string, error) {
	var found []commonsFile
	seen := map[string]bool{}
	for _, q := range queries(k.TypeKey) {
		files, err := t.search(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if key := sameShot(f.Title); !seen[key] {
				seen[key] = true
				found = append(found, f)
			}
		}
		if len(found) >= TypeCandidates {
			break
		}
	}
	var stored []string
	for _, f := range found {
		if len(stored) == TypeCandidates {
			break
		}
		if t.o.DryRun {
			stored = append(stored, f.Title)
			continue
		}
		ok, err := t.store(ctx, k, f, len(stored)+1)
		if err != nil {
			if stopping(ctx, err) {
				return stored, err
			}
			t.d.Log.WarnContext(ctx, "type photo copy failed", slog.String("file", f.Title), slog.String("error", err.Error()))
			continue
		}
		if ok {
			stored = append(stored, f.Title)
		}
	}
	return stored, nil
}

// search asks Commons for bitmap files matching a query, in its order, and
// keeps those with a free licence, no restriction and a usable size.
func (t *TypeImporter) search(ctx context.Context, query string) ([]commonsFile, error) {
	v := url.Values{}
	v.Set("action", "query")
	v.Set("format", "json")
	v.Set("formatversion", "2")
	v.Set("generator", "search")
	v.Set("gsrnamespace", "6")
	v.Set("gsrlimit", "12")
	v.Set("gsrsearch", query+" filetype:bitmap")
	v.Set("prop", "imageinfo")
	v.Set("iiprop", "url|mime|size|extmetadata")
	v.Set("iiurlwidth", fmt.Sprint(thumbWidth))
	v.Set("iiextmetadatafilter", "LicenseShortName|Artist|Restrictions|AttributionRequired")
	var body struct {
		Query struct {
			Pages []struct {
				Title     string `json:"title"`
				Index     int    `json:"index"`
				ImageInfo []struct {
					ThumbURL       string `json:"thumburl"`
					DescriptionURL string `json:"descriptionurl"`
					Mime           string `json:"mime"`
					Width          int    `json:"width"`
					ExtMetadata    map[string]struct {
						Value any `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if _, err := t.im.getJSON(ctx, "commons:search", CommonsPace, t.o.APIURL+"?"+v.Encode(), &body); err != nil {
		return nil, err
	}
	pages := body.Query.Pages
	// The search's order is in `index`; the pages come back by page id.
	for i := 1; i < len(pages); i++ {
		for j := i; j > 0 && pages[j].Index < pages[j-1].Index; j-- {
			pages[j], pages[j-1] = pages[j-1], pages[j]
		}
	}
	var out []commonsFile
	for _, p := range pages {
		if len(p.ImageInfo) == 0 {
			continue
		}
		ii := p.ImageInfo[0]
		meta := func(k string) string {
			if m, ok := ii.ExtMetadata[k]; ok {
				if s, ok := m.Value.(string); ok {
					return s
				}
			}
			return ""
		}
		f := commonsFile{
			Title: strings.TrimPrefix(p.Title, "File:"), ThumbURL: ii.ThumbURL, PageURL: ii.DescriptionURL,
			Licence: meta("LicenseShortName"), Author: plainText(meta("Artist")), Width: ii.Width, Mime: ii.Mime,
			Restrict: meta("Restrictions"),
		}
		if usable(f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// usable is a file the catalogue may show: a free licence, no trademark or
// personality restriction, a photo type, wide enough, with a page and a credit.
func usable(f commonsFile) bool {
	switch {
	case !CommonsLicences[f.Licence], f.Restrict != "", f.ThumbURL == "", f.PageURL == "":
		return false
	case f.Mime != "image/jpeg" && f.Mime != "image/png" && f.Mime != "image/webp":
		return false
	case f.Width < minWidth, len(f.Title) > 240, len(f.PageURL) > 500, len(f.ThumbURL) > 500: //settings:ok Commons field length caps
		return false
	}
	return f.Author != "" || f.Licence == "CC0" || f.Licence == "Public domain"
}

var tags = regexp.MustCompile(`<[^>]*>`)

// plainText turns Commons' HTML author into a short plain credit.
func plainText(s string) string {
	s = strings.Join(strings.Fields(html.UnescapeString(tags.ReplaceAllString(s, " "))), " ")
	if utf8.RuneCountInString(s) > 200 {
		r := []rune(s)
		s = strings.TrimSpace(string(r[:199])) + "…"
	}
	return s
}

// store copies a file into the private bucket as a pending candidate of its
// kind; false when the kind already had it.
func (t *TypeImporter) store(ctx context.Context, k imageimportdb.TypeKindsRow, f commonsFile, rank int) (bool, error) {
	if err := t.im.wait(ctx, "commons:file", CommonsPace); err != nil {
		return false, err
	}
	body, ct, err := t.im.download(ctx, f.ThumbURL)
	if err != nil {
		return false, err
	}
	now := t.d.Clock.Now()
	id := t.ids.New(now)
	key := "type-photos/" + id.String() + ext(ct)
	if err := t.d.Store.Put(ctx, key, ct, body, now); err != nil {
		return false, fmt.Errorf("imageimport: put: %w", err)
	}
	author := f.Author
	if author == "" {
		author = "Wikimedia Commons"
	}
	n, err := imageimportdb.New(t.d.DB.Q(ctx)).InsertTypePhoto(ctx, imageimportdb.InsertTypePhotoParams{
		ID: ids.UUID(id), CategoryID: k.CategoryID, TypeKey: k.TypeKey, Rank: int16(rank), // #nosec G115 -- at most TypeCandidates
		ObjectKey: key, ContentType: ct, SizeBytes: int32(len(body)), // #nosec G115 -- download caps it at maxImageBytes
		SourceRef: f.Title, SourceUrl: f.ThumbURL, PageUrl: f.PageURL, Licence: f.Licence, Attribution: author, Now: now,
	})
	if err != nil {
		return false, fmt.Errorf("imageimport: insert type photo: %w", err)
	}
	return n == 1, nil
}
