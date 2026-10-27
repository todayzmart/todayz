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
	"unicode"
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
// one in A-08 (or turns them all down). A kind with a waiting or picked photo
// is not searched again; a kind whose candidates were all turned down is, and
// its turned-down files are skipped. --again first drops the waiting
// candidates so those kinds are searched afresh. The run also gives an
// approved kind's photo to products added since it was approved.

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
	Again     bool   // first drop waiting candidates, so their kinds are searched again
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
	Cleared   int // waiting candidates dropped by --again
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
		if t.o.Again {
			if rep.Cleared, err = t.clear(ctx); err != nil {
				return rep, err
			}
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
		if _, ok := kindWords(k.TypeKey); !ok {
			line.Reason = "only a pack size or code, not a kind of item"
			rep.Missed = append(rep.Missed, line)
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
			n, err := t.fillKind(ctx, imageimportdb.New(t.d.DB.Q(ctx)), a.ID, a.CategoryID, a.TypeKey, *a.PublicKey)
			total += n
			return err
		})
		if err != nil {
			return total, fmt.Errorf("imageimport: fill %s: %w", a.TypeKey, err)
		}
	}
	return total, nil
}

// fillKind gives a kind's approved photo to every product of the kind that
// has no image and never had this photo taken down. It returns how many.
func (t *TypeImporter) fillKind(ctx context.Context, q *imageimportdb.Queries, photoID, categoryID pgtype.UUID, typeKey, publicKey string) (int, error) {
	prods, err := q.TypeKindProducts(ctx, imageimportdb.TypeKindProductsParams{CategoryID: categoryID, TypeKey: typeKey, TypePhotoID: photoID})
	if err != nil || len(prods) == 0 {
		return 0, err
	}
	now := t.d.Clock.Now()
	imgIDs := make([]pgtype.UUID, len(prods))
	for i := range prods {
		imgIDs[i] = ids.UUID(t.ids.New(now))
	}
	if err := q.InsertTypeImages(ctx, imageimportdb.InsertTypeImagesParams{Now: now, Ids: imgIDs, ProductIds: prods, TypePhotoID: photoID}); err != nil {
		return 0, err
	}
	if err := q.SetTypeProductImages(ctx, imageimportdb.SetTypeProductImagesParams{ImageKey: publicKey, Now: now, Ids: imgIDs, ProductIds: prods}); err != nil {
		return 0, err
	}
	return len(prods), nil
}

// clear drops the waiting candidates of kinds with no picked photo, and their
// copies in the private bucket.
func (t *TypeImporter) clear(ctx context.Context) (int, error) {
	keys, err := imageimportdb.New(t.d.DB.Q(ctx)).ClearPendingTypePhotos(ctx)
	if err != nil {
		return 0, fmt.Errorf("imageimport: clear candidates: %w", err)
	}
	for _, k := range keys {
		if err := t.d.Store.Remove(ctx, k, t.d.Clock.Now()); err != nil {
			t.d.Log.WarnContext(ctx, "type photo copy not removed", slog.String("key", k), slog.String("error", err.Error()))
		}
	}
	return len(keys), nil
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

// typeQuery is one Commons search and the words a file's title must carry.
type typeQuery struct {
	search string
	words  []string
}

// packWords are sizes and counts left in a kind ("garbage bags medium",
// "diaper pants l", "matchbox pack of"); they are not searched for.
var packWords = map[string]bool{
	"xs": true, "s": true, "m": true, "l": true, "xl": true, "xxl": true, "small": true, "medium": true, "large": true,
	"pack": true, "of": true, "pcs": true, "pc": true, "g": true, "kg": true, "ml": true, "ltr": true, "litre": true,
}

// kindWords are a kind's words without pack sizes; false when what is left is
// no kind of item ("500 g", "qa pack ₹120").
func kindWords(typeKey string) ([]string, bool) {
	var out []string
	letters := 0
	for _, w := range strings.Fields(strings.ToLower(strings.ReplaceAll(typeKey, `"`, ""))) {
		n := 0
		for _, r := range w {
			if unicode.IsLetter(r) {
				n++
			}
		}
		if n == 0 || packWords[w] {
			continue
		}
		out = append(out, w)
		letters += n
	}
	return out, letters >= 3
}

// typeAliases name a kind the way Commons titles its photos: Indian shop
// names to the English for the thing itself ("besan" → "gram flour"), and
// retail names to what the photo shows. Searched before the kind's own words.
var typeAliases = map[string]string{
	"sugar": "granulated sugar", "salt": "table salt", "iodised salt": "table salt",
	"besan": "gram flour", "sooji": "semolina", "rava": "semolina", "idli rava": "semolina",
	"maida": "maida flour", "atta": "wheat flour", "wheat atta": "wheat flour", "whole wheat atta": "wheat flour",
	"jowar flour": "sorghum flour", "ragi flour": "finger millet flour", "rajma": "kidney beans",
	"kabuli chana": "chickpeas", "chana dal": "split chickpeas", "toor dal": "pigeon peas", "moong dal": "mung beans",
	"masoor dal": "red lentils", "urad dal": "black gram", "poha": "flattened rice", "sabudana": "tapioca pearls",
	"idli rice": "parboiled rice", "ghee": "clarified butter", "curd": "yogurt", "probiotic curd": "yogurt",
	"kasuri methi": "fenugreek leaves", "dust tea": "tea leaves", "leaf tea": "tea leaves",
	"filter coffee powder": "ground coffee", "classic salted chips": "potato chips", "2-minute noodles": "instant noodles",
	"apple shimla": "apples", "beans": "green beans", "kajal": "kohl", "face wash": "facial cleanser",
	"hand wash": "liquid soap", "garbage bags": "garbage bag", "diaper pants": "diaper", "dry iron": "clothes iron",
	"spike guard": "surge protector", "extension board": "power strip", "mcb": "circuit breaker",
	"bib tap": "water tap", "solvent cement": "pvc cement", "ball pens blue": "ballpoint pen",
	"long notebook": "notebook", "wired earphones": "earphones", "interior emulsion": "emulsion paint",
	"enamel paint white": "enamel paint", "primer": "paint primer", "pav": "pav bread",
}

// categoryHints are words one of which a file must also carry (in its title,
// description or categories) for a kind in that category, so "conditioner"
// in hair care finds hair conditioner, not air conditioners. Keyed by the
// category's slug, else its parent's.
var categoryHints = map[string]string{
	"staples":            "grain OR grains OR flour OR lentil OR lentils OR pulses OR rice OR raw OR india",
	"spices-oils":        "spice OR spices OR oil OR powder OR seeds OR india",
	"snacks":             "snack OR snacks OR food",
	"bakery":             "bread OR bakery OR baked",
	"beverages":          "drink OR beverage OR tea OR coffee OR juice OR bottle",
	"dairy-eggs":         "milk OR dairy OR food",
	"fruits-vegetables":  "vegetable OR vegetables OR fruit OR fruits OR market",
	"household":          "cleaning OR cleaner OR household OR laundry OR kitchen",
	"personal-care":      "hygiene OR cosmetic OR cosmetics OR toiletries OR bathroom",
	"hair-care":          "hair",
	"skin-care":          "skin OR cosmetic OR cosmetics OR cream OR lotion",
	"makeup":             "makeup OR cosmetic OR cosmetics",
	"baby-care":          "baby",
	"health-wellness":    "health OR ayurveda OR ayurvedic OR herbal",
	"electrical":         "electrical OR electric OR wiring OR circuit",
	"plumbing":           "plumbing OR pipe OR water OR tap",
	"paints":             "paint OR wall",
	"tools":              "tool OR tools",
	"school-supplies":    "stationery OR school OR pen OR pencil",
	"office-supplies":    "stationery OR office OR paper",
	"mobile-accessories": "phone OR mobile OR smartphone OR usb",
	"small-appliances":   "appliance OR kitchen OR electric",
	"batteries-lighting": "battery OR batteries OR light OR lamp",
	"grocery":            "food",
	"beauty":             "cosmetic OR cosmetics",
}

// queries are the Commons searches for a kind, best first. For the kind's
// alias, then its own words: every word in the file's title with the
// category's hint; the last two words with the hint for a longer kind; then
// for two words or more the title words alone. A single word is never
// searched without the hint ("ginger" alone finds Ginger Rogers).
func queries(typeKey, category, parent string) []typeQuery {
	words, ok := kindWords(typeKey)
	if !ok {
		return nil
	}
	hint := categoryHints[category]
	if hint == "" {
		hint = categoryHints[parent]
	}
	intitle := func(ws []string) string {
		q := make([]string, len(ws))
		for i, w := range ws {
			q[i] = "intitle:" + w
		}
		return strings.Join(q, " ")
	}
	var out []typeQuery
	add := func(ws []string) {
		if hint != "" {
			out = append(out, typeQuery{search: intitle(ws) + " " + hint, words: ws})
			if len(ws) > 2 {
				out = append(out, typeQuery{search: intitle(ws[len(ws)-2:]) + " " + hint, words: ws[len(ws)-2:]})
			}
		}
		if len(ws) > 1 {
			out = append(out, typeQuery{search: intitle(ws), words: ws})
		}
	}
	if alias, ok := typeAliases[strings.Join(words, " ")]; ok {
		add(strings.Fields(alias))
	}
	add(words)
	return out
}

// titleWords are a title's lower-case words.
func titleWords(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// sameWord matches a word and its plural or a longer form ("biscuits" and
// "biscuit", "salt" and "salted"); words under four letters match exactly.
func sameWord(a, b string) bool {
	if a == b {
		return true
	}
	if len(a) < 4 || len(b) < 4 {
		return false
	}
	n := min(5, len(a), len(b))
	return a[:n] == b[:n]
}

// offTopic are title words that mark a photo of something else: places,
// paintings, signs, plants in a field, and cooked dishes (a kind is the
// packet, not what is made with it). A word the kind itself uses is fine
// ("biryani masala", "fruit cake").
var offTopic = map[string]bool{
	"geograph": true, "falls": true, "factory": true, "map": true, "painting": true, "portrait": true,
	"poster": true, "logo": true, "sign": true, "street": true, "road": true, "station": true, "church": true,
	"temple": true, "museum": true, "met": true, "county": true, "river": true, "lake": true, "beach": true,
	"village": true, "advertisement": true, "eb1911": true, "britannica": true, "plant": true, "farm": true,
	"farming": true, "field": true, "harvest": true, "manufacturing": true, "industries": true,
	"conference": true, "stamp": true, "stampsheet": true, "symbol": true, "diagram": true, "infographic": true,
	"micrograph": true, "bhl": true, "page": true, "mural": true, "statue": true, "cottage": true,
	"curry": true, "rasam": true, "tadka": true, "pakoda": true, "pakodas": true, "pakora": true,
	"paratha": true, "parantha": true, "kachori": true, "halwa": true, "burfi": true, "vada": true,
	"laddu": true, "ladoo": true, "laddo": true, "thali": true, "kebab": true, "korma": true, "chawal": true,
	"roti": true, "bhakri": true, "bhaji": true, "cheela": true, "chilla": true, "recipe": true, "dish": true,
	"pizza": true, "omelette": true, "soup": true, "sandwich": true, "toast": true, "salad": true,
	"biryani": true, "dosa": true, "idli": true, "sambar": true, "sambhar": true, "chaat": true, "fried": true,
	"cooked": true, "served": true, "meal": true, "breakfast": true, "lunch": true, "dinner": true,
	"platter": true, "pasta": true, "wings": true, "tikki": true, "porridge": true, "dumplings": true,
	"doughnut": true, "pudding": true, "stuffed": true, "stuffing": true, "kosambari": true,
	"cake": true, "fish": true, "chicken": true, "egg": true, "steak": true, "kimchi": true,
}

// onTopic is a title that carries every word searched for and nothing that
// marks it as a photo of something else.
func onTopic(title string, q typeQuery, own []string) bool {
	ts := titleWords(title)
	has := func(w string) bool {
		for _, t := range ts {
			if sameWord(t, w) {
				return true
			}
		}
		return false
	}
	for _, w := range q.words {
		for _, part := range titleWords(w) {
			if !has(part) {
				return false
			}
		}
	}
	for _, t := range ts {
		if !offTopic[t] {
			continue
		}
		mine := false
		for _, w := range own {
			if sameWord(t, w) {
				mine = true
			}
		}
		if !mine {
			return false
		}
	}
	return true
}

// sameShot reduces a file name to its first words, so a series ("Toothbrush
// (2021) 02.jpg", "… 03.jpg", "Brown Bread from a Colonial Oven-Baughan (1912)
// - 1", "… - 2") offers one candidate, not three.
func sameShot(title string) string {
	ws := titleWords(digits.ReplaceAllString(title, " "))
	if len(ws) > 0 && (ws[len(ws)-1] == "jpg" || ws[len(ws)-1] == "jpeg" || ws[len(ws)-1] == "png" || ws[len(ws)-1] == "webp") {
		ws = ws[:len(ws)-1]
	}
	return strings.Join(ws[:min(4, len(ws))], " ")
}

var digits = regexp.MustCompile(`[0-9]+`)

// kind searches Commons for a kind and stores up to TypeCandidates files it
// has not had before.
func (t *TypeImporter) kind(ctx context.Context, k imageimportdb.TypeKindsRow) ([]string, error) {
	known, err := imageimportdb.New(t.d.DB.Q(ctx)).TypeKindSources(ctx, imageimportdb.TypeKindSourcesParams{CategoryID: k.CategoryID, TypeKey: k.TypeKey})
	if err != nil {
		return nil, fmt.Errorf("imageimport: kind sources: %w", err)
	}
	seen := map[string]bool{}
	for _, title := range known {
		seen[sameShot(title)] = true
	}
	own, _ := kindWords(k.TypeKey)
	qs := queries(k.TypeKey, k.CategorySlug, k.ParentSlug)
	for _, q := range qs {
		own = append(own, q.words...)
	}
	var found []commonsFile
	for _, q := range qs {
		files, err := t.search(ctx, q.search)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			if key := sameShot(f.Title); !seen[key] && onTopic(f.Title, q, own) {
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
		_, ok, err := t.store(ctx, k.CategoryID, k.TypeKey, f, len(stored)+1)
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
	v.Set("generator", "search")
	v.Set("gsrnamespace", "6")
	v.Set("gsrlimit", "20")
	v.Set("gsrsearch", query+" filetype:bitmap")
	files, err := t.imageInfo(ctx, v)
	if err != nil {
		return nil, err
	}
	var out []commonsFile
	for _, f := range files {
		if usable(f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// imageInfo asks Commons for the files a query names (a search, or titles),
// with their size, type, licence and credit, in the query's order. A title
// Commons does not have is left out.
func (t *TypeImporter) imageInfo(ctx context.Context, v url.Values) ([]commonsFile, error) {
	v.Set("action", "query")
	v.Set("format", "json")
	v.Set("formatversion", "2")
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
	if _, err := t.im.getJSON(ctx, "commons:api", CommonsPace, t.o.APIURL+"?"+v.Encode(), &body); err != nil {
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
		out = append(out, commonsFile{
			Title: strings.TrimPrefix(p.Title, "File:"), ThumbURL: ii.ThumbURL, PageURL: ii.DescriptionURL,
			Licence: meta("LicenseShortName"), Author: plainText(meta("Artist")), Width: ii.Width, Mime: ii.Mime,
			Restrict: meta("Restrictions"),
		})
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
// kind and returns the row's id; false when the kind already had it.
func (t *TypeImporter) store(ctx context.Context, categoryID pgtype.UUID, typeKey string, f commonsFile, rank int) (ids.ID, bool, error) {
	if err := t.im.wait(ctx, "commons:file", CommonsPace); err != nil {
		return ids.Zero, false, err
	}
	body, ct, err := t.im.download(ctx, f.ThumbURL)
	if err != nil {
		return ids.Zero, false, err
	}
	now := t.d.Clock.Now()
	id := t.ids.New(now)
	key := "type-photos/" + id.String() + ext(ct)
	if err := t.d.Store.Put(ctx, key, ct, body, now); err != nil {
		return ids.Zero, false, fmt.Errorf("imageimport: put: %w", err)
	}
	author := f.Author
	if author == "" {
		author = "Wikimedia Commons"
	}
	n, err := imageimportdb.New(t.d.DB.Q(ctx)).InsertTypePhoto(ctx, imageimportdb.InsertTypePhotoParams{
		ID: ids.UUID(id), CategoryID: categoryID, TypeKey: typeKey, Rank: int16(rank), // #nosec G115 -- at most TypeCandidates
		ObjectKey: key, ContentType: ct, SizeBytes: int32(len(body)), // #nosec G115 -- download caps it at maxImageBytes
		SourceRef: f.Title, SourceUrl: f.ThumbURL, PageUrl: f.PageURL, Licence: f.Licence, Attribution: author, Now: now,
	})
	if err != nil {
		return ids.Zero, false, fmt.Errorf("imageimport: insert type photo: %w", err)
	}
	return id, n == 1, nil
}
