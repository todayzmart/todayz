package admin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	api "github.com/todayz/backend/gen/api/admin-stores"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-05 Stores (06 §3, D-40, D-41). The Review tab approves a store waiting at
// a verified business or sends it back to draft with a note; the detail edits
// the pin and hours, force-closes until a time, reopens, and resets strikes.
// Search shows a store only while it and its business are live/verified, so a
// suspended business hides its stores without touching them.

func (s *Service) listStores(ctx context.Context, p api.ListAdminStoresParams) (api.AdminStoreList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminStoreList{}, err
	}
	lim, at, id, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminStoreList{}, err
	}
	var st, city *string
	if p.Status != nil {
		v := string(*p.Status)
		st = &v
	}
	if p.City != nil {
		if v := strings.TrimSpace(*p.City); v != "" {
			city = &v
		}
	}
	rows, err := s.q(ctx).ListStores(ctx, admindb.ListStoresParams{
		Status: st, City: city, BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminStoreList{}, err
	}
	out := api.AdminStoreList{Items: []api.AdminStoreSummary{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		m := api.AdminStoreSummary{
			Id: idString(r.ID), Name: r.Name, BusinessId: idString(r.BusinessID), BusinessName: r.BusinessName,
			BusinessStatus: api.AdminStoreSummaryBusinessStatus(r.BusinessStatus), Status: api.AdminStoreStatus(r.Status),
			Locality: r.Locality, City: r.City, ClosedUntil: utcPtr(r.ClosedUntil),
			ActiveStrikes: int(r.ActiveStrikes), CreatedAt: r.CreatedAt.UTC(),
		}
		if r.HasReliability {
			m.ReliabilityScore = &r.ReliabilityScore
		}
		out.Items = append(out.Items, m)
	}
	return out, nil
}

func (s *Service) store(ctx context.Context, id ids.ID) (api.AdminStore, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminStore{}, err
	}
	q := s.q(ctx)
	r, err := q.GetStore(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminStore{}, notFound()
	}
	if err != nil {
		return api.AdminStore{}, err
	}
	out := api.AdminStore{
		Id: idString(r.ID), Name: r.Name, BusinessId: idString(r.BusinessID), BusinessName: r.BusinessName,
		BusinessStatus: api.AdminStoreBusinessStatus(r.BusinessStatus), Status: api.AdminStoreStatus(r.Status),
		AddressLine1: r.AddressLine1, AddressLine2: r.AddressLine2, Landmark: r.Landmark, Locality: r.Locality,
		City: r.City, Pincode: r.Pincode, ClosedUntil: utcPtr(r.ClosedUntil), ReviewNote: r.ReviewNote,
		LiveAt: utcPtr(r.LiveAt), ReviewedAt: utcPtr(r.ReviewedAt), RatingCount: int(r.RatingCount),
		ReliabilityComputedAt: utcPtr(r.ReliabilityComputedAt), ActiveStrikes: int(r.ActiveStrikes),
		CreatedAt: r.CreatedAt.UTC(), ServerNow: s.now(), Strikes: []api.AdminStoreStrike{},
	}
	if r.HasLocation {
		out.Location = &struct {
			Lat float64 `json:"lat"`
			Lng float64 `json:"lng"`
		}{Lat: r.Lat, Lng: r.Lng}
	}
	if r.HasRating {
		out.RatingAvg = &r.RatingAvg
	}
	if r.HasAcceptRate {
		out.AcceptRate30d = &r.AcceptRate30d
	}
	if r.HasExpiryRate {
		out.UnacceptedExpiryRate30d = &r.UnacceptedExpiryRate30d
	}
	if r.HasReliability {
		out.ReliabilityScore = &r.ReliabilityScore
	}
	out.Hours = storedHours(ctx, r.Hours)
	strikes, err := q.StoreStrikes(ctx, ids.UUID(id))
	if err != nil {
		return api.AdminStore{}, err
	}
	for _, k := range strikes {
		out.Strikes = append(out.Strikes, api.AdminStoreStrike{
			Id: idString(k.ID), Reason: k.Reason, OrderId: optID(k.OrderID), CreatedAt: k.CreatedAt.UTC(),
			ResetAt: utcPtr(k.ResetAt), ResetReason: k.ResetReason,
		})
	}
	return out, nil
}

// storedHours reads the stored hours, which share the contract's shape. Bad
// stored data shows as no hours rather than failing the page.
func storedHours(ctx context.Context, raw []byte) api.AdminStoreHours {
	h := api.AdminStoreHours{Holidays: []api.AdminStoreHoliday{}}
	if len(raw) == 0 {
		return h
	}
	if err := json.Unmarshal(raw, &h); err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "admin: stored store hours did not parse")
		return api.AdminStoreHours{Holidays: []api.AdminStoreHoliday{}}
	}
	if h.Holidays == nil {
		h.Holidays = []api.AdminStoreHoliday{}
	}
	return h
}

// storeMove locks the store (and its business), runs change and writes the
// audit row in one transaction, then reads the store back.
func (s *Service) storeMove(ctx context.Context, actor, id ids.ID, action string,
	change func(ctx context.Context, q *admindb.Queries, cur admindb.LockStoreRow) (before, after map[string]any, err error),
) (api.AdminStore, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminStore{}, err
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockStore(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		before, after, err := change(ctx, q, cur)
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "store", SubjectID: id.String(), Before: before, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminStore{}, err
	}
	return s.store(ctx, id)
}

// updateStore moves the pin and/or replaces the hours, with a reason.
func (s *Service) updateStore(ctx context.Context, actor, id ids.ID, body api.AdminStoreUpdate) (api.AdminStore, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminStore{}, err
	}
	if body.Location == nil && body.Hours == nil {
		return api.AdminStore{}, fieldErr("body", "change the location or the hours")
	}
	var hours []byte
	if body.Hours != nil {
		if s.hours == nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "admin: the store hours check is not configured")
			return api.AdminStore{}, unavailable()
		}
		raw, err := json.Marshal(body.Hours)
		if err != nil {
			return api.AdminStore{}, err
		}
		stored, errs, err := s.hours(raw, "body.hours")
		if err != nil {
			return api.AdminStore{}, err
		}
		if len(errs) > 0 {
			p := fieldErr(errs[0].Path, errs[0].Reason)
			for _, e := range errs[1:] {
				p = p.WithField(e.Path, e.Reason)
			}
			return api.AdminStore{}, p
		}
		hours = stored
	}
	return s.storeMove(ctx, actor, id, "store.update", func(ctx context.Context, q *admindb.Queries, cur admindb.LockStoreRow) (map[string]any, map[string]any, error) {
		before := map[string]any{}
		after := map[string]any{"reason": reason}
		if l := body.Location; l != nil {
			if err := q.SetStorePin(ctx, admindb.SetStorePinParams{Lat: l.Lat, Lng: l.Lng, ID: ids.UUID(id)}); err != nil {
				return nil, nil, err
			}
			if cur.HasLocation {
				before["location"] = map[string]any{"lat": cur.Lat, "lng": cur.Lng}
			} else {
				before["location"] = nil
			}
			after["location"] = map[string]any{"lat": l.Lat, "lng": l.Lng}
		}
		if hours != nil {
			if err := q.SetStoreHours(ctx, admindb.SetStoreHoursParams{Hours: hours, ID: ids.UUID(id)}); err != nil {
				return nil, nil, err
			}
			before["hours"] = json.RawMessage(nonEmptyJSON(cur.Hours))
			after["hours"] = json.RawMessage(hours)
		}
		return before, after, nil
	})
}

func nonEmptyJSON(b []byte) []byte {
	if len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// forceClose shuts a live or paused store until a future time; the store
// keeps its status and opens again by itself at until.
func (s *Service) forceClose(ctx context.Context, actor, id ids.ID, body api.AdminStoreForceClose) (api.AdminStore, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminStore{}, err
	}
	until := body.Until.UTC()
	if !until.After(s.now()) {
		return api.AdminStore{}, fieldErr("body.until", "must be in the future")
	}
	return s.storeMove(ctx, actor, id, "store.force_close", func(ctx context.Context, q *admindb.Queries, cur admindb.LockStoreRow) (map[string]any, map[string]any, error) {
		if cur.Status != "live" && cur.Status != "paused" {
			return nil, nil, invalidTransition("Only a live or paused store can be closed.")
		}
		if err := q.SetStoreClosedUntil(ctx, admindb.SetStoreClosedUntilParams{ClosedUntil: &until, ID: ids.UUID(id)}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"closedUntil": utcPtr(cur.ClosedUntil)}, map[string]any{"closedUntil": until, "reason": reason}, nil
	})
}

// reopen ends a closure that is still running.
func (s *Service) reopen(ctx context.Context, actor, id ids.ID, body api.AdminStoreChange) (api.AdminStore, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminStore{}, err
	}
	return s.storeMove(ctx, actor, id, "store.reopen", func(ctx context.Context, q *admindb.Queries, cur admindb.LockStoreRow) (map[string]any, map[string]any, error) {
		if cur.ClosedUntil == nil || !cur.ClosedUntil.After(s.now()) {
			return nil, nil, invalidTransition("This store is not closed.")
		}
		if err := q.SetStoreClosedUntil(ctx, admindb.SetStoreClosedUntilParams{ID: ids.UUID(id)}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"closedUntil": cur.ClosedUntil.UTC()}, map[string]any{"closedUntil": nil, "reason": reason}, nil
	})
}

// reviewStore approves a store waiting for review (its business must be
// verified) or sends it back to draft with a note the owner sees.
func (s *Service) reviewStore(ctx context.Context, actor, id ids.ID, body api.AdminStoreReviewRequest) (api.AdminStore, error) {
	note, err := optNote(body.Note, "body.note")
	if err != nil {
		return api.AdminStore{}, err
	}
	sendBack := body.Decision == api.SendBack
	if sendBack && note == nil {
		return api.AdminStore{}, fieldErr("body.note", "required to send back")
	}
	action := "store.approve"
	if sendBack {
		action = "store.send_back"
	}
	return s.storeMove(ctx, actor, id, action, func(ctx context.Context, q *admindb.Queries, cur admindb.LockStoreRow) (map[string]any, map[string]any, error) {
		if cur.Status != "pending_review" {
			return nil, nil, invalidTransition("")
		}
		now := s.now()
		by := ids.UUID(actor)
		after := map[string]any{}
		if sendBack {
			if err := q.SendBackStore(ctx, admindb.SendBackStoreParams{By: by, Now: &now, Note: note, ID: ids.UUID(id)}); err != nil {
				return nil, nil, err
			}
			after["status"] = "draft"
			after["note"] = *note
		} else {
			if cur.BusinessStatus != bizVerified {
				return nil, nil, invalidTransition("The business is not verified yet.")
			}
			if err := q.ApproveStore(ctx, admindb.ApproveStoreParams{Now: &now, By: by, ID: ids.UUID(id)}); err != nil {
				return nil, nil, err
			}
			after["status"] = "live"
			if note != nil {
				after["note"] = *note
			}
		}
		return map[string]any{"status": cur.Status}, after, nil
	})
}

// resetStoreStrikes clears a store's active strikes with a reason (D-41). The
// reliability job picks the change up on its next run.
func (s *Service) resetStoreStrikes(ctx context.Context, actor, id ids.ID, body api.AdminStoreChange) (api.AdminStore, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminStore{}, err
	}
	return s.storeMove(ctx, actor, id, "store.strikes_reset", func(ctx context.Context, q *admindb.Queries, _ admindb.LockStoreRow) (map[string]any, map[string]any, error) {
		now := s.now()
		reset, err := q.ResetStrikes(ctx, admindb.ResetStrikesParams{
			Now: &now, By: ids.UUID(actor), Reason: &reason, SubjectType: "store", SubjectID: ids.UUID(id),
		})
		if err != nil {
			return nil, nil, err
		}
		if len(reset) == 0 {
			return nil, nil, invalidTransition("This store has no active strikes.")
		}
		return map[string]any{"activeStrikes": len(reset)},
			map[string]any{"activeStrikes": 0, "reason": reason, "strikeIds": uuidStrings(reset)}, nil
	})
}
