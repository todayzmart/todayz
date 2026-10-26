// Package imageimport fills missing product images from Open Food Facts, Open
// Beauty Facts and Open Products Facts (D-252, `todayz images import`). It
// looks a product up by barcode first, then by name and brand; copies the
// image into our private bucket (never a hot link); and stores it as a pending
// product image with its source, page, licence and author. An admin approves it
// in the image queue before anyone sees it. Medicines are never imported.
//
// The databases ask API users for a User-Agent naming the app and a contact,
// and at most 100 product reads and 10 searches a minute; the importer stays
// well under that.
package imageimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/imageimportdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// Pacing between calls to one database (15 product reads and under 10
// searches a minute).
const (
	ProductPace = 4200 * time.Millisecond
	SearchPace  = 6500 * time.Millisecond
)

// maxImageBytes is the largest image copied (the product photo limit).
const maxImageBytes = 5 << 20

// pageSize is how many candidates one query reads.
const pageSize = 100

// ErrRateLimited stops a run when a database answers 429, or 503, which is
// how Open Food Facts sheds load ("unusually high demand").
var ErrRateLimited = errors.New("imageimport: rate limited, run again later")

// Store keeps the copied image in the private bucket (*storage.Client).
type Store interface {
	Put(ctx context.Context, key, contentType string, body []byte, now time.Time) error
}

// Deps are what an import needs. Store may be nil on a dry run.
type Deps struct {
	DB     *db.DB
	Store  Store
	Client *http.Client // httpx.NewOutboundClient
	Clock  clock.Clock
	Log    *slog.Logger
	// Sleep waits between calls; nil waits on a timer.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Options shape one run.
type Options struct {
	UserAgent  string            // required: "TodayZ-ImageImport/<version> (<contact>)"
	Limit      int               // products to try; at least 1
	DryRun     bool              // look up, copy and store nothing
	Only       string            // one source code, or "" for all
	BaseURLs   map[string]string // source code → base URL (tests); default Sources
	ImageHosts []string          // default ImageHosts
}

// UserAgent is the User-Agent the databases ask for.
func UserAgent(version, contact string) string {
	if version == "" {
		version = "dev"
	}
	return "TodayZ-ImageImport/" + version + " (" + contact + ")"
}

// Line is one product's outcome.
type Line struct {
	ProductID string
	Name      string
	Source    string // off, obf, opf
	Match     string // barcode or name
	Ref       string // the source's product code
	PageURL   string
	Reason    string // why it was missed or failed
}

// Report is what a run did.
type Report struct {
	Tried     int
	Imported  []Line // stored as pending (or would be, on a dry run)
	Missed    []Line // not in any database, or no image
	Medicines []Line // skipped: medicines stay on the placeholder
	NoKey     []Line // no barcode and no brand: nothing to look up
	Failed    []Line
	DryRun    bool
}

// Importer runs imports.
type Importer struct {
	d       Deps
	o       Options
	sources []Source
	hosts   map[string]bool
	ids     *ids.Generator
	last    map[string]time.Time // per source and kind, the last call
}

// New checks the options.
func New(d Deps, o Options) (*Importer, error) {
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
	im := &Importer{d: d, o: o, hosts: map[string]bool{}, ids: ids.NewGenerator(), last: map[string]time.Time{}}
	for _, s := range Sources {
		if o.Only != "" && s.Code != o.Only {
			continue
		}
		if u, ok := o.BaseURLs[s.Code]; ok {
			s.BaseURL = strings.TrimRight(u, "/")
		}
		im.sources = append(im.sources, s)
	}
	if len(im.sources) == 0 {
		return nil, fmt.Errorf("imageimport: unknown source %q", o.Only)
	}
	hosts := o.ImageHosts
	if hosts == nil {
		hosts = ImageHosts
	}
	for _, h := range hosts {
		im.hosts[h] = true
	}
	return im, nil
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run tries up to Limit products without an image.
func (im *Importer) Run(ctx context.Context) (Report, error) {
	rep := Report{DryRun: im.o.DryRun}
	q := imageimportdb.New(im.d.DB.Q(ctx))
	var after pgtype.UUID
	for rep.Tried < im.o.Limit {
		rows, err := q.ImportCandidates(ctx, imageimportdb.ImportCandidatesParams{After: after, Lim: pageSize})
		if err != nil {
			return rep, fmt.Errorf("imageimport: candidates: %w", err)
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			after = r.ID
			if rep.Tried >= im.o.Limit {
				break
			}
			rep.Tried++
			if err := im.one(ctx, r, &rep); err != nil {
				return rep, err
			}
		}
	}
	return rep, nil
}

// one handles a product; only a stopping error (cancelled, rate limited) is returned.
func (im *Importer) one(ctx context.Context, r imageimportdb.ImportCandidatesRow, rep *Report) error {
	pid, _ := ids.FromUUID(r.ID)
	line := Line{ProductID: pid.String(), Name: r.Name}
	if skipped(r.CategorySlug, r.ParentSlug) {
		rep.Medicines = append(rep.Medicines, line)
		return nil
	}
	var codes []string
	for _, b := range r.Barcodes {
		if barcode(b) {
			codes = append(codes, b)
		}
	}
	brand := ""
	if r.BrandName != nil {
		brand = *r.BrandName
	}
	if len(codes) == 0 && brand == "" {
		rep.NoKey = append(rep.NoKey, line)
		return nil
	}
	hit, err := im.find(ctx, r.CategorySlug, r.ParentSlug, codes, r.Name, brand)
	if err != nil {
		if stopping(ctx, err) {
			return err
		}
		line.Reason = err.Error()
		rep.Failed = append(rep.Failed, line)
		im.d.Log.WarnContext(ctx, "image import lookup failed", slog.String("product_id", line.ProductID), slog.String("error", err.Error()))
		return nil
	}
	if hit == nil {
		line.Reason = "not found"
		rep.Missed = append(rep.Missed, line)
		return nil
	}
	line.Source, line.Match, line.Ref, line.PageURL = hit.src.Code, hit.match, hit.code, hit.page
	if im.o.DryRun {
		rep.Imported = append(rep.Imported, line)
		return nil
	}
	stored, err := im.store(ctx, pid, hit)
	switch {
	case err != nil && stopping(ctx, err):
		return err
	case err != nil:
		line.Reason = err.Error()
		rep.Failed = append(rep.Failed, line)
		im.d.Log.WarnContext(ctx, "image import copy failed", slog.String("product_id", line.ProductID), slog.String("error", err.Error()))
	case !stored:
		line.Reason = "the product got an image meanwhile"
		rep.Missed = append(rep.Missed, line)
	default:
		rep.Imported = append(rep.Imported, line)
	}
	return nil
}

func stopping(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, ErrRateLimited)
}

// hit is a product found in a database, with its image.
type hit struct {
	src      Source
	code     string
	imageURL string
	page     string
	match    string
}

// offProduct is the part of a product the importer reads.
type offProduct struct {
	Code          string `json:"code"`
	ProductName   string `json:"product_name"`
	Brands        string `json:"brands"`
	ImageFrontURL string `json:"image_front_url"`
	ImageURL      string `json:"image_url"`
}

func (p offProduct) image() string {
	if p.ImageFrontURL != "" {
		return p.ImageFrontURL
	}
	return p.ImageURL
}

const fields = "code,product_name,brands,image_front_url,image_url"

// find looks the product up: each barcode in the category's database, then
// in the others; then, with a brand, a name search in the category's database.
func (im *Importer) find(ctx context.Context, category, parent string, codes []string, name, brand string) (*hit, error) {
	first := primary(category, parent)
	order := make([]Source, 0, len(im.sources))
	for _, s := range im.sources {
		if s.Code == first {
			order = append([]Source{s}, order...)
		} else {
			order = append(order, s)
		}
	}
	for _, code := range codes {
		for _, s := range order {
			p, err := im.product(ctx, s, code)
			if err != nil {
				return nil, err
			}
			if p != nil && p.image() != "" {
				return &hit{src: s, code: code, imageURL: p.image(), page: s.BaseURL + "/product/" + code, match: "barcode"}, nil
			}
		}
	}
	if brand == "" || order[0].Code != first {
		return nil, nil
	}
	hits, err := im.search(ctx, order[0], brand+" "+name)
	if err != nil {
		return nil, err
	}
	for _, p := range hits {
		if p.Code == "" || p.image() == "" || !nameMatch(name, brand, p.ProductName, p.Brands) {
			continue
		}
		return &hit{src: order[0], code: p.Code, imageURL: p.image(), page: order[0].BaseURL + "/product/" + url.PathEscape(p.Code), match: "name"}, nil
	}
	return nil, nil
}

func (im *Importer) product(ctx context.Context, s Source, code string) (*offProduct, error) {
	var body struct {
		Status  int        `json:"status"`
		Product offProduct `json:"product"`
	}
	u := s.BaseURL + "/api/v2/product/" + url.PathEscape(code) + "?fields=" + fields
	found, err := im.getJSON(ctx, s.Code+":product", ProductPace, u, &body)
	if err != nil || !found || body.Status != 1 {
		return nil, err
	}
	if body.Product.Code == "" {
		body.Product.Code = code
	}
	return &body.Product, nil
}

func (im *Importer) search(ctx context.Context, s Source, terms string) ([]offProduct, error) {
	v := url.Values{}
	v.Set("search_terms", terms)
	v.Set("search_simple", "1")
	v.Set("action", "process")
	v.Set("json", "1")
	v.Set("page_size", "5")
	v.Set("fields", fields)
	var body struct {
		Products []offProduct `json:"products"`
	}
	if _, err := im.getJSON(ctx, s.Code+":search", SearchPace, s.BaseURL+"/cgi/search.pl?"+v.Encode(), &body); err != nil {
		return nil, err
	}
	return body.Products, nil
}

// wait paces calls of one kind to one database.
func (im *Importer) wait(ctx context.Context, kind string, pace time.Duration) error {
	if last, ok := im.last[kind]; ok {
		if d := pace - im.d.Clock.Now().Sub(last); d > 0 {
			if err := im.d.Sleep(ctx, d); err != nil {
				return err
			}
		}
	}
	im.last[kind] = im.d.Clock.Now()
	return nil
}

// getJSON reads a JSON answer; a 404 is found=false.
func (im *Importer) getJSON(ctx context.Context, kind string, pace time.Duration, u string, out any) (bool, error) {
	if err := im.wait(ctx, kind, pace); err != nil {
		return false, err
	}
	res, err := im.get(ctx, u, "application/json")
	if err != nil {
		return false, err
	}
	defer func() { _ = res.Body.Close() }()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return false, nil
	case res.StatusCode != http.StatusOK:
		return false, statusErr(res)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(out); err != nil {
		return false, fmt.Errorf("imageimport: %s answer: %w", kind, err)
	}
	return true, nil
}

func (im *Importer) get(ctx context.Context, u, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", im.o.UserAgent)
	req.Header.Set("Accept", accept)
	return im.d.Client.Do(req)
}

func statusErr(res *http.Response) error {
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		return ErrRateLimited
	}
	return fmt.Errorf("imageimport: %s answered %s", res.Request.URL.Host, res.Status)
}

// store copies the image into the private bucket and records it as pending;
// false when the product gained an image or a waiting photo meanwhile.
func (im *Importer) store(ctx context.Context, pid ids.ID, h *hit) (bool, error) {
	body, ct, err := im.download(ctx, h.imageURL)
	if err != nil {
		return false, err
	}
	now := im.d.Clock.Now()
	id := im.ids.New(now)
	key := "product-images/" + pid.String() + "/" + id.String() + ext(ct)
	if err := im.d.Store.Put(ctx, key, ct, body, now); err != nil {
		return false, fmt.Errorf("imageimport: put: %w", err)
	}
	size := int32(len(body)) // #nosec G115 -- download caps it at maxImageBytes
	n, err := imageimportdb.New(im.d.DB.Q(ctx)).InsertImportedImage(ctx, imageimportdb.InsertImportedImageParams{
		ID: ids.UUID(id), Source: h.src.Code, ObjectKey: key, ContentType: ct, SizeBytes: &size,
		SourceRef: &h.code, SourceUrl: &h.imageURL, PageUrl: &h.page, Licence: ptr(Licence),
		Attribution: &h.src.Author, Match: &h.match, Now: now, ProductID: ids.UUID(pid),
	})
	if err != nil {
		return false, fmt.Errorf("imageimport: insert: %w", err)
	}
	return n == 1, nil
}

// download fetches an image from an allowed host, at most maxImageBytes, and
// checks from its bytes that it is a JPEG, PNG or WebP.
func (im *Importer) download(ctx context.Context, raw string) ([]byte, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || !im.hosts[u.Host] {
		return nil, "", fmt.Errorf("imageimport: image host not allowed: %s", hostOf(u))
	}
	res, err := im.get(ctx, u.String(), "image/*")
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = res.Body.Close() }()
	if !im.hosts[res.Request.URL.Host] {
		return nil, "", fmt.Errorf("imageimport: image redirected to %s", res.Request.URL.Host)
	}
	if res.StatusCode != http.StatusOK {
		return nil, "", statusErr(res)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxImageBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("imageimport: image: %w", err)
	}
	if len(body) > maxImageBytes {
		return nil, "", errors.New("imageimport: image larger than 5 MB")
	}
	ct := http.DetectContentType(body)
	switch ct {
	case "image/jpeg", "image/png", "image/webp":
		return body, ct, nil
	}
	return nil, "", fmt.Errorf("imageimport: not an image (%s)", ct)
}

func hostOf(u *url.URL) string {
	if u == nil {
		return "?"
	}
	return u.Host
}

func ext(ct string) string {
	switch ct {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	}
	return ".jpg"
}

func ptr[T any](v T) *T { return &v }
