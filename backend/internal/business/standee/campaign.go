// Package standee is W3-09's QR standee campaigns (01 §8, 06 B-10; D-26,
// D-162, D-223): the standee rows of a store, each with its own QR link, the
// scan → sign-up → qualified counts of each, and the request for a reprint.
//
// A standee has no code of its own (D-26). Its QR is the store's short-code
// link with `s=<standee id>`, and a scan is the referral click that link
// records (referrals/click.go checks the standee is the store's, SC-ST-03).
// This package only reads those clicks and referrals; it never writes them.
// W2-10's PDF route is not here.
package standee

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	standeeapi "github.com/todayz/backend/gen/api/standee"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/standeedb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// MaxPerStore is how many standees a store can have.
// TODO(lead): 01 has no number for this; 20 is a conservative guard against
// runaway rows (D-223). Move it to platform_settings if shops need more.
const MaxPerStore = 20

// maxCampaign is the longest campaign label, in characters (the contract's
// StandeeCampaign).
const maxCampaign = 40 //settings:ok ck_qr_standees_campaign bound, not a setting

// Deps is what the service needs. DB may be missing in a route-only test:
// every operation then answers 503.
type Deps struct {
	Config *config.Config
	DB     *db.DB
	Clock  clock.Clock
	// PublicURL is the API's public origin, the start of every QR link
	// (API_PUBLIC_URL; Config's when empty).
	PublicURL string
}

// Service implements the standee routes.
type Service struct {
	db        *db.DB
	clock     clock.Clock
	audit     *audit.Writer
	ids       *ids.Generator
	publicURL string
}

var errUnavailable = errors.New("standee: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("standee: a clock is required")
	}
	s := &Service{db: d.DB, clock: d.Clock, ids: ids.NewGenerator(), publicURL: strings.TrimRight(d.PublicURL, "/")}
	if s.publicURL == "" && d.Config != nil {
		s.publicURL = strings.TrimRight(d.Config.APIPublicURL, "/")
	}
	if d.DB != nil {
		var ipKey []byte
		if d.Config != nil && d.Config.Keys.PIIHashKey.IsSet() {
			ipKey = d.Config.Keys.PIIHashKey.Bytes()
		}
		s.audit = audit.New(d.DB, d.Clock, ipKey)
	}
	return s, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "standee: not configured (needs the database)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) q(ctx context.Context) *standeedb.Queries { return standeedb.New(s.db.Q(ctx)) }

// List is the store's standees, newest first, with their counts and the
// totals of all of them.
func (s *Service) List(ctx context.Context, store ids.ID) (standeeapi.StandeeList, error) {
	if err := s.ready(ctx); err != nil {
		return standeeapi.StandeeList{}, err
	}
	q := s.q(ctx)
	code, err := q.StoreShortCode(ctx, ids.UUID(store))
	if db.IsNoRows(err) {
		return standeeapi.StandeeList{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return standeeapi.StandeeList{}, fmt.Errorf("standee: store: %w", err)
	}
	rows, err := q.ListStandees(ctx, ids.UUID(store))
	if err != nil {
		return standeeapi.StandeeList{}, fmt.Errorf("standee: list: %w", err)
	}
	out := standeeapi.StandeeList{Items: make([]standeeapi.Standee, 0, len(rows)), Limit: MaxPerStore}
	for _, r := range rows {
		st := s.toAPI(code, row(r))
		out.Totals = add(out.Totals, st.Counts)
		out.Items = append(out.Items, st)
	}
	return out, nil
}

// Create adds a standee for a campaign. The store row is locked so two
// creates count in turn and the limit holds.
func (s *Service) Create(ctx context.Context, p authn.Principal, store ids.ID, campaign string) (standeeapi.Standee, error) {
	if err := s.ready(ctx); err != nil {
		return standeeapi.Standee{}, err
	}
	campaign, err := cleanCampaign(campaign)
	if err != nil {
		return standeeapi.Standee{}, err
	}
	now := s.clock.Now()
	var out standeeapi.Standee
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		code, err := q.LockStore(ctx, ids.UUID(store))
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("standee: lock store: %w", err)
		}
		n, err := q.CountStandees(ctx, ids.UUID(store))
		if err != nil {
			return fmt.Errorf("standee: count: %w", err)
		}
		if n >= MaxPerStore {
			return problem.New(genproblems.StandeeLimitReached, "")
		}
		id := s.ids.New(now)
		created, err := q.InsertStandee(ctx, standeedb.InsertStandeeParams{
			ID: ids.UUID(id), StoreID: ids.UUID(store), Campaign: campaign, CreatedBy: ids.UUID(p.UserID), Now: now,
		})
		if err != nil {
			return fmt.Errorf("standee: insert: %w", err)
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorStaff, ActorID: p.UserID, Action: "standee.create", SubjectType: "qr_standee",
			SubjectID: id.String(), After: map[string]any{"store_id": store.String(), "campaign": campaign},
		}); err != nil {
			return err
		}
		out = s.toAPI(code, standeeRow{
			ID: created.ID, StoreID: created.StoreID, Campaign: created.Campaign, PrintedAt: created.PrintedAt,
			ReorderRequestedAt: created.ReorderRequestedAt, CreatedAt: created.CreatedAt, Version: created.Version,
		})
		return nil
	})
	if err != nil {
		return standeeapi.Standee{}, err
	}
	return out, nil
}

// MarkSheetPrinted records the first Print of the store's own sheet (D-281),
// which ticks the setup step; a later Print changes nothing.
func (s *Service) MarkSheetPrinted(ctx context.Context, store ids.ID) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	now := s.clock.Now()
	if err := s.q(ctx).MarkStoreSheetPrinted(ctx, standeedb.MarkStoreSheetPrintedParams{StoreID: ids.UUID(store), Now: &now}); err != nil {
		return fmt.Errorf("standee: sheet printed: %w", err)
	}
	return nil
}

// RequestReorder marks the standee as waiting for a reprint. A request
// already waiting is answered unchanged and writes nothing.
// TODO(lead): admin fulfilment (who prints and clears it) is not built (D-223).
func (s *Service) RequestReorder(ctx context.Context, p authn.Principal, store, standee ids.ID) (standeeapi.Standee, error) {
	if err := s.ready(ctx); err != nil {
		return standeeapi.Standee{}, err
	}
	now := s.clock.Now()
	var out standeeapi.Standee
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		code, err := q.StoreShortCode(ctx, ids.UUID(store))
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("standee: store: %w", err)
		}
		locked, err := q.LockStandee(ctx, standeedb.LockStandeeParams{ID: ids.UUID(standee), StoreID: ids.UUID(store)})
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return fmt.Errorf("standee: lock: %w", err)
		}
		if locked.ReorderRequestedAt == nil {
			if err := q.RequestReorder(ctx, standeedb.RequestReorderParams{
				ID: ids.UUID(standee), StoreID: ids.UUID(store), RequestedBy: ids.UUID(p.UserID), Now: now,
			}); err != nil {
				return fmt.Errorf("standee: reorder: %w", err)
			}
			if _, err := s.audit.Write(ctx, audit.Entry{
				ActorKind: audit.ActorStaff, ActorID: p.UserID, Action: "standee.reorder_requested", SubjectType: "qr_standee",
				SubjectID: standee.String(), Before: map[string]any{"reorder_requested_at": nil},
				After: map[string]any{"reorder_requested_at": now},
			}); err != nil {
				return err
			}
		}
		r, err := q.GetStandee(ctx, standeedb.GetStandeeParams{ID: ids.UUID(standee), StoreID: ids.UUID(store)})
		if err != nil {
			return fmt.Errorf("standee: get: %w", err)
		}
		out = s.toAPI(code, row(standeedb.ListStandeesRow(r)))
		return nil
	})
	if err != nil {
		return standeeapi.Standee{}, err
	}
	return out, nil
}

// cleanCampaign trims the label and checks it is 1–40 characters (the
// validator has checked the contract's bounds already; this is the database's
// rule too, ck_qr_standees_campaign).
func cleanCampaign(c string) (string, error) {
	c = strings.TrimSpace(c)
	if c == "" || utf8.RuneCountInString(c) > maxCampaign || !utf8.ValidString(c) || strings.ContainsFunc(c, isControl) {
		return "", problem.New(genproblems.ValidationFailed, "").WithField("body.campaign", "invalid")
	}
	return c, nil
}

func isControl(r rune) bool { return r < 0x20 || r == 0x7f }

func uuidString(u pgtype.UUID) string {
	id, _ := ids.FromUUID(u)
	return id.String()
}
