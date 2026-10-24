// Package baskets is the customer's saved baskets (W2-14; D-110, 01 §11.4,
// 03 baskets and basket_lines, 04 /v1/me/baskets). Basket search itself is
// read-only and lives in internal/search (POST /v1/discover/basket-search);
// this package only keeps the lists.
//
// Rules this package keeps:
//
//   - A basket is private: every read and write filters on the caller's
//     user id, and another customer's basket answers 404.
//   - At most basket_max_saved baskets per customer (422
//     basket_limit_reached) and basket_max_items lines per basket (422
//     validation_failed on body.lines). Creates of one customer serialise on
//     an advisory lock so two concurrent creates cannot both pass the count.
//   - A line is a master product or free text, never both. Free text is
//     stored normalised as search normalises a query (search.Normalize,
//     injected; 08 BSK-03); text that normalises to nothing is 422.
//   - PATCH needs If-Match (428 when missing, 412 when stale). Replacing the
//     lines clears last_split unless the same request sends one; a sent
//     lastSplit may only name this basket's lines.
//   - DELETE is a soft delete. Account deletion deletes the rows
//     (OnAccountDelete, idempotent).
package baskets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	basketsapi "github.com/todayz/backend/gen/api/baskets"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/basketsdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/ifmatch"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Deps is what the service needs. Only Clock is required: without the
// database or settings every operation answers 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	// Normalize is search's normalisation (search.Normalize); nil keeps
	// lower-cased, trimmed text.
	Normalize func(string) string
}

// Service implements the baskets routes and the account-deletion hook.
type Service struct {
	db        *db.DB
	clock     clock.Clock
	settings  *settings.Cache
	normalize func(string) string
	ids       *ids.Generator
}

var errUnavailable = errors.New("baskets: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("baskets: a clock is required")
	}
	s := &Service{db: d.DB, clock: d.Clock, settings: d.Settings, normalize: d.Normalize, ids: ids.NewGenerator()}
	if s.normalize == nil {
		s.normalize = func(t string) string { return strings.ToLower(strings.TrimSpace(t)) }
	}
	return s, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "baskets: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *basketsdb.Queries { return basketsdb.New(s.db.Q(ctx)) }

func (s *Service) count(ctx context.Context, k settings.Key) (int, error) {
	n, err := s.settings.Int(ctx, k)
	if err != nil {
		return 0, fmt.Errorf("baskets: %s: %w", k, err)
	}
	return int(min(max(n, 0), 1<<20)), nil
}

// OnAccountDelete deletes the customer's baskets (01 §16; lines go by
// cascade). It runs in identity's deletion transaction and is idempotent.
func (s *Service) OnAccountDelete(ctx context.Context, userID ids.ID) error {
	if s.db == nil {
		return errUnavailable
	}
	return s.q(ctx).DeleteCustomerBaskets(ctx, ids.UUID(userID))
}

// --- reads ----------------------------------------------------------------------

func (s *Service) list(ctx context.Context, userID ids.ID) (basketsapi.BasketList, error) {
	if err := s.ready(ctx); err != nil {
		return basketsapi.BasketList{}, err
	}
	rows, err := s.q(ctx).ListBaskets(ctx, ids.UUID(userID))
	if err != nil {
		return basketsapi.BasketList{}, err
	}
	out := basketsapi.BasketList{Baskets: make([]basketsapi.BasketSummary, 0, len(rows))}
	for _, r := range rows {
		out.Baskets = append(out.Baskets, basketsapi.BasketSummary{
			BasketId: idString(r.ID), Name: r.Name, LineCount: int(r.LineCount),
			UpdatedAt: r.UpdatedAt.UTC(), Version: int(r.Version),
		})
	}
	return out, nil
}

// header is the basket row every read and write returns.
type header struct {
	ID        pgtype.UUID
	Name      string
	Version   int32
	LastSplit []byte
	UpdatedAt time.Time
}

func (s *Service) get(ctx context.Context, userID, basketID ids.ID) (basketsapi.Basket, error) {
	if err := s.ready(ctx); err != nil {
		return basketsapi.Basket{}, err
	}
	r, err := s.q(ctx).GetBasket(ctx, basketsdb.GetBasketParams{ID: ids.UUID(basketID), CustomerID: ids.UUID(userID)})
	if errors.Is(err, pgx.ErrNoRows) {
		return basketsapi.Basket{}, notFound()
	}
	if err != nil {
		return basketsapi.Basket{}, err
	}
	return s.render(ctx, header(r))
}

func (s *Service) render(ctx context.Context, h header) (basketsapi.Basket, error) {
	rows, err := s.q(ctx).ListLines(ctx, h.ID)
	if err != nil {
		return basketsapi.Basket{}, err
	}
	b := basketsapi.Basket{
		BasketId: idString(h.ID), Name: h.Name, Version: int(h.Version), UpdatedAt: h.UpdatedAt.UTC(),
		Lines: make([]basketsapi.BasketLine, 0, len(rows)),
	}
	for _, r := range rows {
		l := basketsapi.BasketLine{
			LineId: idString(r.ID), Text: r.Text, Quantity: int(r.Quantity), Position: int(r.Position),
		}
		if r.ProductID.Valid {
			pid := idString(r.ProductID)
			l.ProductId, l.ProductName, l.Variant, l.Unit = &pid, r.ProductName, r.Variant, r.Unit
			ps := r.PackSize
			l.PackSize = &ps
		}
		b.Lines = append(b.Lines, l)
	}
	if len(h.LastSplit) > 0 {
		var sp basketsapi.BasketSplit
		if err := json.Unmarshal(h.LastSplit, &sp); err != nil {
			return basketsapi.Basket{}, fmt.Errorf("baskets: last_split: %w", err)
		}
		b.LastSplit = &sp
	}
	return b, nil
}

// --- writes ---------------------------------------------------------------------

func (s *Service) create(ctx context.Context, userID ids.ID, in basketsapi.BasketCreate) (basketsapi.Basket, error) {
	if err := s.ready(ctx); err != nil {
		return basketsapi.Basket{}, err
	}
	name, err := cleanName(in.Name, "body.name")
	if err != nil {
		return basketsapi.Basket{}, err
	}
	var out basketsapi.Basket
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		lines, err := s.checkLines(ctx, in.Lines)
		if err != nil {
			return err
		}
		q := s.q(ctx)
		if err := q.LockCustomerBaskets(ctx, userID.String()); err != nil {
			return err
		}
		maxSaved, err := s.count(ctx, settings.BasketMaxSaved)
		if err != nil {
			return err
		}
		n, err := q.CountBaskets(ctx, ids.UUID(userID))
		if err != nil {
			return err
		}
		if int(n) >= maxSaved {
			return problem.New(genproblems.BasketLimitReached, "")
		}
		now := s.now()
		id := s.ids.New(now)
		r, err := q.InsertBasket(ctx, basketsdb.InsertBasketParams{ID: ids.UUID(id), CustomerID: ids.UUID(userID), Name: name, Now: now})
		if err != nil {
			return err
		}
		if err := s.insertLines(ctx, id, lines, now); err != nil {
			return err
		}
		out, err = s.render(ctx, header(r))
		return err
	})
	return out, err
}

func (s *Service) update(ctx context.Context, userID, basketID ids.ID, ifMatchHeader *string, in basketsapi.BasketPatch) (basketsapi.Basket, error) {
	if err := s.ready(ctx); err != nil {
		return basketsapi.Basket{}, err
	}
	want, err := ifMatch(ifMatchHeader)
	if err != nil {
		return basketsapi.Basket{}, err
	}
	if in.Lines != nil && in.LastSplit != nil {
		return basketsapi.Basket{}, fieldErr("body.lastSplit", "not with lines")
	}
	var name string
	if in.Name != nil {
		if name, err = cleanName(*in.Name, "body.name"); err != nil {
			return basketsapi.Basket{}, err
		}
	}
	var out basketsapi.Basket
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockBasket(ctx, basketsdb.LockBasketParams{ID: ids.UUID(basketID), CustomerID: ids.UUID(userID)})
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.Version != want {
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if in.Name == nil {
			name = cur.Name
		}
		split := cur.LastSplit
		now := s.now()
		switch {
		case in.Lines != nil:
			lines, err := s.checkLines(ctx, *in.Lines)
			if err != nil {
				return err
			}
			if err := q.DeleteLines(ctx, cur.ID); err != nil {
				return err
			}
			if err := s.insertLines(ctx, basketID, lines, now); err != nil {
				return err
			}
			split = nil
		case in.LastSplit != nil:
			if split, err = s.checkSplit(ctx, cur.ID, *in.LastSplit); err != nil {
				return err
			}
		}
		r, err := q.UpdateBasket(ctx, basketsdb.UpdateBasketParams{
			Name: name, LastSplit: split, Now: now, ID: cur.ID, CustomerID: ids.UUID(userID),
		})
		if err != nil {
			return err
		}
		out, err = s.render(ctx, header(r))
		return err
	})
	return out, err
}

func (s *Service) remove(ctx context.Context, userID, basketID ids.ID) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	now := s.now()
	n, err := s.q(ctx).SoftDeleteBasket(ctx, basketsdb.SoftDeleteBasketParams{Now: &now, ID: ids.UUID(basketID), CustomerID: ids.UUID(userID)})
	if err != nil {
		return err
	}
	if n == 0 {
		return notFound()
	}
	return nil
}

// line is one checked line, ready to insert.
type line struct {
	productID ids.ID // zero for free text
	text      *string
	quantity  int32
}

// checkLines applies basket_max_items and the one-of rule, and checks that
// every named product exists. The request validator already bounded the
// quantity, text length and the array.
func (s *Service) checkLines(ctx context.Context, in []basketsapi.BasketLineInput) ([]line, error) {
	maxItems, err := s.count(ctx, settings.BasketMaxItems)
	if err != nil {
		return nil, err
	}
	if len(in) > maxItems {
		return nil, fieldErr("body.lines", "too many")
	}
	out := make([]line, 0, len(in))
	var want []pgtype.UUID
	at := map[ids.ID]int{}
	for i, l := range in {
		path := "body.lines[" + strconv.Itoa(i) + "]"
		if (l.ProductId == nil) == (l.Text == nil) {
			return nil, fieldErr(path, "one of productId or text")
		}
		if l.Quantity < 1 || l.Quantity > 99 { //settings:ok contract bound (1–99), not a setting
			return nil, fieldErr(path+".quantity", "out of range")
		}
		ln := line{quantity: int32(l.Quantity)} //nolint:gosec // bounded above
		if l.ProductId != nil {
			id, err := ids.Parse(*l.ProductId)
			if err != nil {
				return nil, fieldErr(path+".productId", "unknown")
			}
			ln.productID = id
			if _, seen := at[id]; !seen {
				at[id] = i
				want = append(want, ids.UUID(id))
			}
		} else {
			t := strings.TrimSpace(s.normalize(*l.Text))
			if t == "" {
				return nil, fieldErr(path+".text", "empty")
			}
			if len([]rune(t)) > 120 { //settings:ok ck_basket_lines_text bound, not a setting
				t = string([]rune(t)[:120]) //settings:ok same bound
			}
			ln.text = &t
		}
		out = append(out, ln)
	}
	if len(want) > 0 {
		got, err := s.q(ctx).ExistingProducts(ctx, want)
		if err != nil {
			return nil, err
		}
		found := make(map[ids.ID]bool, len(got))
		for _, u := range got {
			if id, ok := ids.FromUUID(u); ok {
				found[id] = true
			}
		}
		for i, l := range out {
			if l.productID != ids.Zero && !found[l.productID] {
				return nil, fieldErr("body.lines["+strconv.Itoa(i)+"].productId", "unknown")
			}
		}
	}
	return out, nil
}

func (s *Service) insertLines(ctx context.Context, basketID ids.ID, lines []line, now time.Time) error {
	q := s.q(ctx)
	for i, l := range lines {
		p := basketsdb.InsertLineParams{
			ID: ids.UUID(s.ids.New(now)), BasketID: ids.UUID(basketID), Text: l.text,
			Quantity: l.quantity, Position: int32(i), Now: now, //nolint:gosec // at most basket_max_items
		}
		if l.productID != ids.Zero {
			p.ProductID = ids.UUID(l.productID)
		}
		if err := q.InsertLine(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// checkSplit accepts a split that names only this basket's lines, each at
// most once, and stores it as sent.
func (s *Service) checkSplit(ctx context.Context, basketID pgtype.UUID, sp basketsapi.BasketSplit) ([]byte, error) {
	rows, err := s.q(ctx).ListLines(ctx, basketID)
	if err != nil {
		return nil, err
	}
	mine := make(map[string]bool, len(rows))
	for _, r := range rows {
		mine[idString(r.ID)] = true
	}
	seen := map[string]bool{}
	use := func(path, id string) error {
		if !mine[id] || seen[id] {
			return fieldErr(path, "unknown line")
		}
		seen[id] = true
		return nil
	}
	for i, st := range sp.Stores {
		if _, err := ids.Parse(st.StoreId); err != nil {
			return nil, fieldErr("body.lastSplit.stores["+strconv.Itoa(i)+"].storeId", "invalid")
		}
		for j, id := range st.LineIds {
			if err := use("body.lastSplit.stores["+strconv.Itoa(i)+"].lineIds["+strconv.Itoa(j)+"]", id); err != nil {
				return nil, err
			}
		}
	}
	for j, id := range sp.UncoveredLineIds {
		if err := use("body.lastSplit.uncoveredLineIds["+strconv.Itoa(j)+"]", id); err != nil {
			return nil, err
		}
	}
	sp.SearchedAt = sp.SearchedAt.UTC()
	b, err := json.Marshal(sp)
	if err != nil {
		return nil, err
	}
	if len(b) > 8192 { //settings:ok ck_baskets_last_split size bound, not a setting
		return nil, fieldErr("body.lastSplit", "too large")
	}
	return b, nil
}

// --- helpers ----------------------------------------------------------------------

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, notFound()
	}
	return id, nil
}

func cleanName(n, path string) (string, error) {
	n = strings.Join(strings.Fields(n), " ")
	if n == "" || len([]rune(n)) > 40 { //settings:ok contract bound (name ≤ 40), not a setting
		return "", fieldErr(path, "length")
	}
	return n, nil
}

func etag(version int) string { return `"v` + strconv.Itoa(version) + `"` }

// ifMatch reads a required If-Match: missing is 428, unreadable is 412.
func ifMatch(h *string) (int32, error) {
	if h == nil || strings.TrimSpace(*h) == "" {
		return 0, problem.New(genproblems.PreconditionRequired, "")
	}
	v, ok := ifmatch.Parse(*h)
	if !ok {
		return 0, problem.New(genproblems.PreconditionFailed, "")
	}
	return v, nil
}

func idString(u pgtype.UUID) string {
	id, _ := ids.FromUUID(u)
	return id.String()
}

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }
