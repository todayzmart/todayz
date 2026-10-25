package share

// Share cards (01 §11.1): one per store item, created on the first "Share",
// read by the customer app's /x/{code} Pages Function for the link preview.

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	shareapi "github.com/todayz/backend/gen/api/share"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/asksdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
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
)

var errCodeTaken = errors.New("share: code collision")

func unavailable() error { return problem.New(genproblems.ShareCardUnavailable, "") }

// create returns the item's card, creating it (and its image) on first use.
func (s *Service) create(ctx context.Context, body *shareapi.ShareCardCreate) (shareapi.ShareCard, error) {
	if err := s.ready(ctx); err != nil {
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
		s.renderIfStale(ctx, idOf(card.ID), card.Code, card.PricePaiseRendered, item)
		return s.cardView(card.Code, item), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return shareapi.ShareCard{}, fmt.Errorf("share: card by item: %w", err)
	}

	var keys [][]byte
	if k := s.hash.IPLimit(reqctx.ClientIP(ctx)); k != nil {
		keys = append(keys, k)
	}
	if body.AnonId != nil {
		keys = append(keys, s.hash.Sum("share:anon:"+body.AnonId.String()))
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
	s.render(ctx, cardID, code, item)
	return s.cardView(code, item), nil
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
	s.renderIfStale(ctx, idOf(card.ID), card.Code, card.PricePaiseRendered, item)
	s.event(ctx, "share_opened", map[string]any{})
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

func (s *Service) renderIfStale(ctx context.Context, cardID ids.ID, code string, rendered *int64, item asksdb.ItemForCardRow) {
	if rendered != nil && *rendered == item.PricePaise {
		return
	}
	s.render(ctx, cardID, code, item)
}

// render draws the preview, stores it and records the price it shows. A
// failure is logged: the card still works, and the next read tries again.
func (s *Service) render(ctx context.Context, cardID ids.ID, code string, item asksdb.ItemForCardRow) {
	if s.images == nil {
		return
	}
	log := telemetry.Logger(ctx)
	png, err := RenderCard(CardImage{Item: item.ItemName, Store: item.StoreName, Locality: deref(item.Locality), PricePaise: item.PricePaise})
	if err != nil {
		log.ErrorContext(ctx, "share: render card", slog.String("error", err.Error()))
		return
	}
	key := imageKey(code)
	if err := s.images.Put(ctx, key, "image/png", png); err != nil {
		log.ErrorContext(ctx, "share: store card image", slog.String("error", err.Error()))
		return
	}
	now := s.now()
	price := item.PricePaise
	if err := s.q(ctx).SetCardRendered(ctx, asksdb.SetCardRenderedParams{
		ImageKey: &key, PricePaise: &price, Now: &now, ID: ids.UUID(cardID),
	}); err != nil {
		log.ErrorContext(ctx, "share: set card rendered", slog.String("error", err.Error()))
	}
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
