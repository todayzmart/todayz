package search

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/geo/h3"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The search log (01 §11, D-99): one search_log row per answered search,
// zero results too, and the search_performed / search_zero_results events.
// Only the first page of a search is recorded (a next page is the same
// search). Neither the row nor the events carry the customer's location:
// the row keeps only the H3 res-8 cell (D-59); the events carry no query
// text. A failure here is logged (without the
// query) and never fails the search.

// Barcode lengths D-99 treats as "an exact barcode" (EAN-8 to GTIN-14).
const (
	barcodeMinDigits = 8
	barcodeMaxDigits = 14 //settings:ok GTIN-14 length, not a product rule
)

// candidateLimit caps the products the D-99 exact-name rule compares.
const candidateLimit = 50

// chipsNone is the chips prop while the refine-chip parser is not built.
const chipsNone = "none"

// logEntry is what one search records.
type logEntry struct {
	req     searchReq
	norm    string
	results int // matching in-stock items
	stores  int
	start   time.Time
	// product is the line's master product (a basket product line); when
	// unset the product is matched from norm.
	product pgtype.UUID
}

// record writes the search_log row and the events.
func (s *Service) record(ctx context.Context, q *searchdb.Queries, e logEntry) {
	log := telemetry.Logger(ctx)
	now := s.now()
	product := e.product
	if !product.Valid {
		var err error
		if product, err = productFor(ctx, q, e.norm); err != nil {
			log.WarnContext(ctx, "search: product match failed", slog.String("error", err.Error()))
		}
	}
	user := pgtype.UUID{}
	if uid, err := ids.Parse(s.userID(ctx)); err == nil {
		user = ids.UUID(uid)
	}
	var category *string
	if e.req.Category != "" {
		c := e.req.Category
		category = &c
	}
	queryNorm := e.norm
	if utf8.RuneCountInString(queryNorm) > maxQueryRunes {
		queryNorm = string([]rune(queryNorm)[:maxQueryRunes])
	}
	var cell *int64
	if c, err := h3.LatLngToCell(e.req.Lat, e.req.Lng, h3.AnalyticsRes); err == nil {
		v := c.Int64()
		cell = &v
	}
	err := q.InsertSearchLog(ctx, searchdb.InsertSearchLogParams{
		ID: ids.UUID(ids.New(now)), CreatedAt: now, QueryNorm: queryNorm,
		ResultsCount: i32(e.results), StoreCount: i32(e.stores),
		RadiusM: i32(e.req.RadiusM), GeoCell: cell, Category: category, ProductID: product, UserID: user,
		Source: e.req.Source,
	})
	if err != nil {
		log.WarnContext(ctx, "search: search_log insert failed", slog.String("error", err.Error()))
	}
	if s.events == nil {
		return
	}
	latency := now.Sub(e.start).Milliseconds()
	if latency < 0 {
		latency = 0
	}
	props := map[string]any{
		"results": e.results, "stores": e.stores, "radius": e.req.RadiusM, "sort": e.req.Sort,
		"has_offer": e.req.HasOffer, "latency_ms": int(latency), "chips": chipsNone, "source": e.req.Source,
	}
	zero := map[string]any{"radius": e.req.RadiusM, "source": e.req.Source}
	if category != nil {
		props["category"], zero["category"] = *category, *category
	}
	if pid, ok := ids.FromUUID(product); ok {
		props["product_id"], zero["product_id"] = pid.String(), pid.String()
	}
	s.event(ctx, q, user, "search_performed", props)
	if e.results == 0 {
		s.event(ctx, q, user, "search_zero_results", zero)
	}
}

// event validates props against the catalogue and writes the event.
func (s *Service) event(ctx context.Context, q *searchdb.Queries, user pgtype.UUID, name string, props map[string]any) {
	log := telemetry.Logger(ctx)
	ev, ok := s.events.Event(name)
	if !ok {
		log.ErrorContext(ctx, "search: event not in the catalogue", slog.String("event", name))
		return
	}
	if err := ev.Validate(props); err != nil {
		log.ErrorContext(ctx, "search: event props invalid", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	raw, err := json.Marshal(props)
	if err != nil {
		log.ErrorContext(ctx, "search: event props", slog.String("event", name), slog.String("error", err.Error()))
		return
	}
	now := s.now()
	if err := q.InsertServerEvent(ctx, searchdb.InsertServerEventParams{
		ID: ids.UUID(ids.New(now)), Name: name, OccurredAt: now, UserID: user, Props: raw,
	}); err != nil {
		log.WarnContext(ctx, "search: event insert failed", slog.String("event", name), slog.String("error", err.Error()))
	}
}

// productFor is the D-99 product_id of a search: the one active master
// product whose barcode is the query (8 to 14 digits), or whose normalised
// name (with its variant and/or pack size) or one of whose synonyms equals
// the normalised query. No fuzzy match and no threshold: when the rule finds
// no product, or more than one (TODO(lead): D-99 does not say; an ambiguous
// query records none), the search carries none.
func productFor(ctx context.Context, q *searchdb.Queries, norm string) (pgtype.UUID, error) {
	none := pgtype.UUID{}
	if norm == "" {
		return none, nil
	}
	if isBarcode(norm) {
		found, err := q.ProductsByBarcode(ctx, norm)
		if err != nil || len(found) != 1 {
			return none, err
		}
		return found[0], nil
	}
	matches := map[[16]byte]pgtype.UUID{}
	syn, err := q.SynonymsFor(ctx, []string{norm})
	if err != nil {
		return none, err
	}
	for _, r := range syn {
		if r.ProductID.Valid && (r.Term == norm || r.Synonym == norm) {
			matches[r.ProductID.Bytes] = r.ProductID
		}
	}
	words := parse(norm).words
	if len(words) > 0 {
		cands, err := q.ProductsByWords(ctx, searchdb.ProductsByWordsParams{Tsq: tsExact(words), Lim: candidateLimit})
		if err != nil {
			return none, err
		}
		for _, c := range cands {
			if productNameIs(norm, c) {
				matches[c.ID.Bytes] = c.ID
			}
		}
	}
	if len(matches) != 1 {
		return none, nil
	}
	for _, id := range matches {
		return id, nil
	}
	return none, nil
}

// productNameIs reports whether norm is the product's name, alone or with its
// variant and/or its pack size, normalised.
func productNameIs(norm string, c searchdb.ProductsByWordsRow) bool {
	pk := packText(c.PackSize, c.Unit)
	forms := []string{c.Name, c.Name + " " + pk}
	if c.Variant != nil && *c.Variant != "" {
		forms = append(forms, c.Name+" "+*c.Variant, c.Name+" "+*c.Variant+" "+pk)
	}
	for _, f := range forms {
		if Normalize(f) == norm {
			return true
		}
	}
	return false
}

// isBarcode: the normalised query is 8 to 14 digits.
func isBarcode(norm string) bool {
	return isDigits(norm) && len(norm) >= barcodeMinDigits && len(norm) <= barcodeMaxDigits
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
