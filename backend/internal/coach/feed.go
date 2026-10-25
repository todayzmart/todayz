package coach

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	coachapi "github.com/todayz/backend/gen/api/coach"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/coachdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

const (
	defaultLimit = 20  //settings:ok the contract's Limit default (04 §1)
	maxLimit     = 100 //settings:ok the contract's Limit maximum (04 §1)
)

var statusesOf = map[coachapi.CoachCardFilter][]string{
	coachapi.CoachCardFilterLive:      {"new", "seen"},
	coachapi.CoachCardFilterDone:      {"done"},
	coachapi.CoachCardFilterDismissed: {"dismissed"},
	coachapi.CoachCardFilterAll:       {"new", "seen", "done", "dismissed", "expired"},
}

func fieldErr(path, reason string) error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func page(limit *int, cursor *string) (lim, off int, err error) {
	lim = defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, 0, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(*cursor)
	n, ok := strings.CutPrefix(string(b), "o:")
	if err != nil || !ok {
		return 0, 0, fieldErr("query.cursor", "not a cursor from this list")
	}
	off, err = strconv.Atoi(n)
	if err != nil || off < 0 || off > 1<<20 {
		return 0, 0, fieldErr("query.cursor", "not a cursor from this list")
	}
	return lim, off, nil
}

// List is the store's cards, newest first. Reading the live feed marks its
// new cards seen.
func (s *Service) List(ctx context.Context, storeID ids.ID, f coachapi.CoachCardFilter, limit *int, cursor *string) (coachapi.CoachCardList, error) {
	st, ok := statusesOf[f]
	if !ok {
		return coachapi.CoachCardList{}, fieldErr("query.status", "not a coach card filter")
	}
	lim, off, err := page(limit, cursor)
	if err != nil {
		return coachapi.CoachCardList{}, err
	}
	now := s.d.Clock.Now()
	sid := ids.UUID(storeID)
	q := coachdb.New(s.d.DB.Q(ctx))
	rows, err := q.ListCards(ctx, coachdb.ListCardsParams{StoreID: sid, Statuses: st, Off: int32(off), Lim: int32(lim + 1)}) //nolint:gosec // bounded above
	if err != nil {
		return coachapi.CoachCardList{}, err
	}
	out := coachapi.CoachCardList{Items: []coachapi.CoachCard{}, ServerNow: now.UTC()}
	if len(rows) > lim {
		rows = rows[:lim]
		c := base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(off+lim)))
		out.NextCursor = &c
	}
	var fresh []pgtype.UUID
	for _, r := range rows {
		if r.Status == "new" {
			fresh = append(fresh, r.ID)
		}
		out.Items = append(out.Items, cardOf(r))
	}
	if len(fresh) > 0 {
		if err := q.MarkSeen(ctx, coachdb.MarkSeenParams{Now: &now, StoreID: sid, Ids: fresh}); err != nil {
			return coachapi.CoachCardList{}, err
		}
	}
	return out, nil
}

// Done marks a card done (the owner used it).
func (s *Service) Done(ctx context.Context, by, storeID, cardID ids.ID) (coachapi.CoachCard, error) {
	return s.act(ctx, by, storeID, cardID, "done", nil, weightDone)
}

// Dismiss is Not useful: the card goes and its subject stays quiet for
// coach_dismiss_quiet_days.
func (s *Service) Dismiss(ctx context.Context, by, storeID, cardID ids.ID) (coachapi.CoachCard, error) {
	days, err := s.d.Settings.Int(ctx, settings.CoachDismissQuietDays)
	if err != nil {
		return coachapi.CoachCard{}, err
	}
	until := s.d.Clock.Now().Add(time.Duration(days) * 24 * time.Hour)
	return s.act(ctx, by, storeID, cardID, "dismissed", &until, weightDismiss)
}

func (s *Service) act(ctx context.Context, by, storeID, cardID ids.ID, status string, quiet *time.Time, delta int32) (coachapi.CoachCard, error) {
	now := s.d.Clock.Now()
	var card coachdb.CoachSuggestion
	err := s.d.DB.InTx(ctx, func(ctx context.Context) error {
		q := coachdb.New(s.d.DB.Q(ctx))
		c, err := q.ActCard(ctx, coachdb.ActCardParams{
			Status: status, Now: &now, By: ids.UUID(by), QuietUntil: quiet, ID: ids.UUID(cardID), StoreID: ids.UUID(storeID),
		})
		if db.IsNoRows(err) {
			c, err = q.GetCard(ctx, coachdb.GetCardParams{ID: ids.UUID(cardID), StoreID: ids.UUID(storeID)})
			switch {
			case db.IsNoRows(err):
				return problem.New(genproblems.NotFound, "")
			case err != nil:
				return err
			case c.Status == status:
				card = c // a repeat of the same act is a no-op
				return nil
			default:
				return problem.New(genproblems.InvalidTransition, "")
			}
		}
		if err != nil {
			return err
		}
		card = c
		up := coachdb.BumpWeightParams{StoreID: ids.UUID(storeID), Detector: c.Detector, DeltaBp: delta, Now: now}
		if status == "done" {
			up.Done = 1
		} else {
			up.Dismissed = 1
		}
		return q.BumpWeight(ctx, up)
	})
	if err != nil {
		var p *problem.Error
		if errors.As(err, &p) {
			return coachapi.CoachCard{}, p
		}
		return coachapi.CoachCard{}, err
	}
	return cardOf(card), nil
}

func cardOf(r coachdb.CoachSuggestion) coachapi.CoachCard {
	id, _ := ids.FromUUID(r.ID)
	c := coachapi.CoachCard{
		Id: id.String(), Detector: r.Detector, SubjectRef: r.SubjectRef,
		Kind: coachapi.CoachCardKind(r.Kind), Topic: coachapi.CoachTopic(r.Topic),
		Title: r.Title, Body: r.Body, Status: coachapi.CoachCardStatus(r.Status),
		GeneratedBy: coachapi.CoachCardGeneratedBy(r.GeneratedBy), Lang: coachapi.CoachCardLang(r.Lang),
		Action:    coachapi.CoachAction{Type: coachapi.CoachActionType(r.ActionType), Payload: map[string]interface{}{}},
		Numbers:   map[string]interface{}{},
		CreatedAt: r.CreatedAt.UTC(), ValidUntil: r.ValidUntil.UTC(),
	}
	_ = json.Unmarshal(r.Numbers, &c.Numbers)
	_ = json.Unmarshal(r.ActionPayload, &c.Action.Payload)
	if r.ActedAt != nil {
		t := r.ActedAt.UTC()
		c.ActedAt = &t
	}
	if r.QuietUntil != nil {
		t := r.QuietUntil.UTC()
		c.QuietUntil = &t
	}
	return c
}
