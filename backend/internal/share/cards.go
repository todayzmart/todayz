package share

// Share cards (01 §11.1): one per store item, created on the first "Share",
// read by the customer app's /x/{code} Pages Function for the link preview.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	shareapi "github.com/todayz/backend/gen/api/share"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/ratelimit"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	codeLen      = 10 //settings:ok the contract's ShareCode length
	codeAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	// createWindow is the window share_card_create_per_hour counts over;
	// rows older than keepCreates are deleted as new ones arrive.
	createWindow = time.Hour
	keepCreates  = 2 * time.Hour
	// codeTries bounds retries on a code collision (62^10 codes: a retry is
	// already vanishingly rare).
	codeTries = 5 //settings:ok retry bound, not a product rule

	// renderCooldown is how long a queued render holds off the next one for
	// the same card, so reads of a card whose storage keeps failing do not
	// queue a job each.
	renderCooldown = 10 * time.Minute
	// keepOpenDays: RetainOpens deletes share_card_opens rows from before
	// yesterday (IST).
	keepOpenDays = 2 //settings:ok dedupe window of share_opened, not a product rule
	readGroup    = "share_read"
)

// readRule bounds card reads and re-creates per IP HMAC. Like
// ratelimit.DefaultRules it stops a script, never a person, and is not a
// product number.
var readRule = ratelimit.Rule{PerMinute: 60, Burst: 60} //settings:ok coarse abuse guard, like ratelimit.DefaultRules

// KindRender is the event job that redraws a card whose image shows another
// price (internal/jobs/asks.go declares it).
const KindRender = "share_card_render"

// RenderArgs are share_card_render's arguments.
type RenderArgs struct {
	CardID string `json:"card_id"`
}

var errCodeTaken = errors.New("share: code collision")

func unavailable() error { return problem.New(genproblems.ShareCardUnavailable, "") }

// create returns the item's card, creating it (and its image) on first use.
func (s *Service) create(ctx context.Context, body *shareapi.ShareCardCreate) (shareapi.ShareCard, error) {
	if err := s.ready(ctx); err != nil {
		return shareapi.ShareCard{}, err
	}
	ipKey, err := s.limitReads(ctx)
	if err != nil {
		return shareapi.ShareCard{}, err
	}
	if s.hash == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "share: PII_HASH_KEY missing; new cards cannot be counted")
		return shareapi.ShareCard{}, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	itemID, err := ids.Parse(body.StoreItemId)
	if err != nil {
		return shareapi.ShareCard{}, unavailable()
	}
	item, err := s.item(ctx, itemID)
	if err != nil {
		return shareapi.ShareCard{}, err
	}
	card, err := s.q(ctx).CardByItem(ctx, ids.UUID(itemID))
	if err == nil {
		s.requestRender(ctx, idOf(card.ID), card.PricePaiseRendered, item)
		return s.cardView(card.Code, item), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return shareapi.ShareCard{}, fmt.Errorf("share: card by item: %w", err)
	}

	var keys [][]byte
	if ipKey != nil {
		keys = append(keys, ipKey)
	}
	if body.AnonId != nil {
		keys = append(keys, s.hash.Sum("share:anon:"+body.AnonId.String()))
	}
	// A create with neither an IP nor a device cannot be counted, so it is
	// refused like one over the limit.
	if len(keys) == 0 {
		return shareapi.ShareCard{}, problem.New(genproblems.RateLimited, "").WithHeader("Retry-After", strconv.Itoa(int(createWindow/time.Second)))
	}
	limit, err := s.settings.Int(ctx, settings.ShareCardCreatePerHour)
	if err != nil {
		return shareapi.ShareCard{}, err
	}

	var code string
	var cardID ids.ID
	for range codeTries {
		code, cardID, err = s.insertCard(ctx, itemID, keys, limit)
		if !errors.Is(err, errCodeTaken) {
			break
		}
	}
	if err != nil {
		return shareapi.ShareCard{}, err
	}
	// The first image is drawn here, so the share sheet has it; creates are
	// counted. A failure is logged and a later read queues the render.
	if err := s.render(ctx, cardID, code, item); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "share: render card", slog.String("error", err.Error()))
	}
	return s.cardView(code, item), nil
}

// limitReads takes a token from the caller's read bucket, keyed by the IP
// HMAC, and returns the key (nil without an IP or a hash key: those callers
// share one bucket).
func (s *Service) limitReads(ctx context.Context) ([]byte, error) {
	var key []byte
	if s.hash != nil {
		key = s.hash.IPLimit(visitorIP(ctx))
	}
	bucket := "-"
	if key != nil {
		bucket = hex.EncodeToString(key)
	}
	if r := s.limit.Allow(readGroup, bucket); !r.Allowed {
		return nil, problem.New(genproblems.RateLimited, "").
			WithHeader("Retry-After", strconv.Itoa(max(1, int(math.Ceil(r.RetryAfter.Seconds())))))
	}
	return key, nil
}

// insertCard counts the caller's creates, inserts the card and records the
// create, in one transaction. A card another request created meanwhile is
// returned as it is, uncounted.
func (s *Service) insertCard(ctx context.Context, itemID ids.ID, keys [][]byte, limit int64) (string, ids.ID, error) {
	var code string
	var cardID ids.ID
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		now := s.now()
		for _, k := range keys {
			n, err := q.CountCreates(ctx, asksdb.CountCreatesParams{KeyHash: k, Since: now.Add(-createWindow)})
			if err != nil {
				return fmt.Errorf("share: count creates: %w", err)
			}
			if int64(n) >= limit {
				return problem.New(genproblems.RateLimited, "").WithHeader("Retry-After", strconv.Itoa(int(createWindow/time.Second)))
			}
		}
		c, err := newCode()
		if err != nil {
			return err
		}
		id := s.ids.New(now)
		rows, err := q.InsertCard(ctx, asksdb.InsertCardParams{ID: ids.UUID(id), StoreItemID: ids.UUID(itemID), Code: c, Now: now})
		if db.IsUniqueViolation(err, "ux_share_cards_code") {
			return errCodeTaken
		}
		if err != nil {
			return fmt.Errorf("share: insert card: %w", err)
		}
		if rows == 0 {
			existing, err := q.CardByItem(ctx, ids.UUID(itemID))
			if err != nil {
				return fmt.Errorf("share: card by item: %w", err)
			}
			code, cardID = existing.Code, idOf(existing.ID)
			return nil
		}
		for _, k := range keys {
			if err := q.InsertCreate(ctx, asksdb.InsertCreateParams{KeyHash: k, Now: now}); err != nil {
				return fmt.Errorf("share: insert create: %w", err)
			}
		}
		if err := q.DeleteOldCreates(ctx, now.Add(-keepCreates)); err != nil {
			return fmt.Errorf("share: delete old creates: %w", err)
		}
		code, cardID = c, id
		return nil
	})
	return code, cardID, err
}

// get is the link preview of a card.
func (s *Service) get(ctx context.Context, code string) (shareapi.SharePreview, error) {
	if err := s.ready(ctx); err != nil {
		return shareapi.SharePreview{}, err
	}
	ipKey, err := s.limitReads(ctx)
	if err != nil {
		return shareapi.SharePreview{}, err
	}
	card, err := s.q(ctx).CardByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return shareapi.SharePreview{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return shareapi.SharePreview{}, fmt.Errorf("share: card by code: %w", err)
	}
	item, err := s.item(ctx, idOf(card.StoreItemID))
	if err != nil {
		return shareapi.SharePreview{}, err
	}
	s.requestRender(ctx, idOf(card.ID), card.PricePaiseRendered, item)
	s.opened(ctx, idOf(card.ID), ipKey)
	mrp := item.MrpPaise
	if mrp == nil {
		mrp = item.ProductMrpPaise
	}
	return shareapi.SharePreview{
		StoreItemId: idOf(item.ID).String(), ProductId: ulidPtr(item.ProductID),
		ItemName: item.ItemName, StoreName: item.StoreName, StoreSlug: item.StoreSlug, Locality: item.Locality,
		PricePaise: int(item.PricePaise), MrpPaise: intPtr(mrp), ImageUrl: s.imageURLFor(card.Code),
	}, nil
}

// item reads a store item for its card: 410 when it is gone or its store is
// not live or its business not verified. The two are the same answer.
func (s *Service) item(ctx context.Context, id ids.ID) (asksdb.ItemForCardRow, error) {
	item, err := s.q(ctx).ItemForCard(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return item, unavailable()
	}
	if err != nil {
		return item, fmt.Errorf("share: item for card: %w", err)
	}
	if !item.Available {
		return item, unavailable()
	}
	return item, nil
}

// opened records share_opened the first time this IP reads the card's
// preview on an IST day. Without an IP key it records nothing, so a read
// never writes an unbounded row. A failure is logged.
func (s *Service) opened(ctx context.Context, cardID ids.ID, key []byte) {
	if key == nil || s.events == nil {
		return
	}
	n, err := s.q(ctx).RecordOpen(ctx, asksdb.RecordOpenParams{
		CardID: ids.UUID(cardID), KeyHash: key, Day: pgDate(s.now().In(clock.IST)),
	})
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "share: record open", slog.String("error", err.Error()))
		return
	}
	if n == 1 {
		s.event(ctx, "share_opened", map[string]any{})
	}
}

// RetainOpens deletes share_card_opens rows from before yesterday (IST).
func (s *Service) RetainOpens(ctx context.Context) (int64, error) {
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	before := s.now().In(clock.IST).AddDate(0, 0, 1-keepOpenDays)
	n, err := s.q(ctx).DeleteOldOpens(ctx, pgDate(before))
	if err != nil {
		return 0, fmt.Errorf("share: delete old opens: %w", err)
	}
	return n, nil
}

// requestRender queues share_card_render when the card's image shows another
// price, at most once per card in renderCooldown; the read serves the old
// image meanwhile. A failure is logged: the preview still works.
func (s *Service) requestRender(ctx context.Context, cardID ids.ID, rendered *int64, item asksdb.ItemForCardRow) {
	if s.jobs == nil || s.images == nil || (rendered != nil && *rendered == item.PricePaise) {
		return
	}
	now := s.now()
	before := now.Add(-renderCooldown)
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		n, err := s.q(ctx).ClaimRender(ctx, asksdb.ClaimRenderParams{ID: ids.UUID(cardID), Now: &now, CooldownBefore: &before})
		if err != nil || n == 0 {
			return err
		}
		tx, _ := db.TxFrom(ctx)
		return s.jobs.EnqueueTx(ctx, tx, KindRender, RenderArgs{CardID: cardID.String()},
			platformjobs.EnqueueOpts{UniqueKey: cardID.String()})
	})
	if err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "share: queue card render", slog.String("error", err.Error()))
	}
}

// Render is share_card_render: it redraws the card's image when it shows
// another price than the item's. A card that is gone, or whose item or store
// is, needs nothing. A storage failure, or a worker without the image store,
// is returned, so the job retries with River's backoff (and shows as failed),
// and the card row is left as it was.
func (s *Service) Render(ctx context.Context, cardID ids.ID) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.images == nil {
		return fmt.Errorf("share: render card: %w (no image store)", errUnavailable)
	}
	card, err := s.q(ctx).CardByID(ctx, ids.UUID(cardID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("share: card by id: %w", err)
	}
	item, err := s.item(ctx, idOf(card.StoreItemID))
	var p *problem.Error
	if errors.As(err, &p) && p.Code == string(genproblems.ShareCardUnavailable) {
		return nil
	}
	if err != nil {
		return err
	}
	if card.PricePaiseRendered != nil && *card.PricePaiseRendered == item.PricePaise {
		return nil
	}
	return s.render(ctx, cardID, card.Code, item)
}

// render draws the preview, stores it and records the price it shows. On any
// failure the card row is left as it was.
func (s *Service) render(ctx context.Context, cardID ids.ID, code string, item asksdb.ItemForCardRow) error {
	if s.images == nil {
		return nil
	}
	png, err := RenderCard(CardImage{Item: item.ItemName, Product: item.ProductName, Store: item.StoreName, Locality: deref(item.Locality), PricePaise: item.PricePaise})
	if err != nil {
		return fmt.Errorf("share: render card: %w", err)
	}
	key := imageKey(code)
	if err := s.images.Put(ctx, key, "image/png", png); err != nil {
		return fmt.Errorf("share: store card image: %w", err)
	}
	now := s.now()
	price := item.PricePaise
	if err := s.q(ctx).SetCardRendered(ctx, asksdb.SetCardRenderedParams{
		ImageKey: &key, PricePaise: &price, Now: &now, ID: ids.UUID(cardID),
	}); err != nil {
		return fmt.Errorf("share: set card rendered: %w", err)
	}
	return nil
}

func (s *Service) cardView(code string, item asksdb.ItemForCardRow) shareapi.ShareCard {
	return shareapi.ShareCard{
		Code: code, Url: s.appURL + "/x/" + code,
		Text:     item.ItemName + " at " + item.StoreName + ", " + Rupees(item.PricePaise) + " · TodayZ",
		ImageUrl: s.imageURLFor(code),
	}
}

func (s *Service) imageURLFor(code string) string { return s.imageURL + "/" + imageKey(code) }

// imageKey is the card's object key; the table's check holds it to this.
func imageKey(code string) string { return "share/" + code + ".png" }

// Rupees formats paise the way the share text shows a price: "₹38", or
// "₹38.50" when there are paise.
func Rupees(p int64) string {
	if p%100 == 0 {
		return "₹" + strconv.FormatInt(p/100, 10)
	}
	return fmt.Sprintf("₹%d.%02d", p/100, p%100)
}

func newCode() (string, error) {
	b := make([]byte, codeLen)
	n := big.NewInt(int64(len(codeAlphabet)))
	for i := range b {
		v, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("share: code: %w", err)
		}
		b[i] = codeAlphabet[v.Int64()]
	}
	return string(b), nil
}

func intPtr(p *int64) *int {
	if p == nil {
		return nil
	}
	v := int(*p)
	return &v
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
