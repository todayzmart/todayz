package telemetry

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/telemetrydb"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// An event's time must be recent: a device that was offline for a while still reports, but a clock
// that is months off (or in the future) is a bug or abuse, not data. These two are code constants,
// not product rules: 01 §17 does not list them, so they are not in platform_settings.
const (
	maxEventAge    = 30 * 24 * time.Hour
	maxEventFuture = time.Hour
)

type productEvent struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	T       time.Time      `json:"t"`
	Props   map[string]any `json:"props"`
	GeoCell string         `json:"geoCell"`
}

type eventBatch struct {
	App        string         `json:"app"`
	AppVersion string         `json:"appVersion"`
	Platform   string         `json:"platform"`
	AnonID     string         `json:"anonId"`
	SessionID  string         `json:"sessionId"`
	Events     []productEvent `json:"events"`
}

func optUUID(s string) pgtype.UUID {
	if id, err := ids.Parse(s); err == nil {
		return ids.UUID(id)
	}
	return pgtype.UUID{}
}

// dropReason returns why an event is not stored ("" when it is), one of
// telemetry.EventDropReasons. A bad event never fails the batch: a stale client with an old event
// name must not lose its good events. Only a malformed envelope is an error (the request validator
// answers 400/422 before this runs).
func (s *Service) dropReason(e *productEvent, now time.Time, seen map[string]struct{}) string {
	if _, err := ids.Parse(e.ID); err != nil {
		return "invalid_id"
	}
	if _, dup := seen[e.ID]; dup {
		return "duplicate_id"
	}
	seen[e.ID] = struct{}{}
	def, ok := s.cat.Event(e.Name)
	switch {
	case !ok:
		return "unknown_event"
	case def.Emitter != eventcat.EmitterClient:
		return "server_event"
	}
	props := e.Props
	if props == nil {
		props = map[string]any{}
	}
	if err := def.Validate(props); err != nil {
		return "invalid_props"
	}
	if e.T.Before(now.Add(-maxEventAge)) || e.T.After(now.Add(maxEventFuture)) {
		return "out_of_window"
	}
	return ""
}

func (s *Service) postEvents(w http.ResponseWriter, r *http.Request) {
	var b eventBatch
	if !decode(w, r, &b) {
		return
	}
	ctx := r.Context()
	now := s.d.Clock.Now()
	seen := make(map[string]struct{}, len(b.Events))
	keep := make([]productEvent, 0, len(b.Events))
	var dropped int64
	for i := range b.Events {
		if reason := s.dropReason(&b.Events[i], now, seen); reason != "" {
			dropped++
			s.d.Metrics.EventDropped(ctx, reason)
			continue
		}
		keep = append(keep, b.Events[i])
	}

	var userID pgtype.UUID
	if s.d.UserID != nil {
		userID = optUUID(s.d.UserID(ctx))
	}
	anon, session := optUUID(b.AnonID), optUUID(b.SessionID)

	var accepted int64
	err := s.d.DB.InTx(ctx, func(ctx context.Context) error {
		q := telemetrydb.New(s.d.DB.Q(ctx))
		for _, e := range keep {
			id, _ := ids.Parse(e.ID) // validated above
			props := []byte("{}")
			if len(e.Props) > 0 {
				var err error
				if props, err = json.Marshal(e.Props); err != nil {
					return err
				}
			}
			n, err := q.InsertEvent(ctx, telemetrydb.InsertEventParams{
				ID: ids.UUID(id), Name: e.Name, OccurredAt: e.T.UTC(), App: b.App,
				UserID: userID, AnonID: anon, SessionID: session, Props: props,
				GeoCell: e.GeoCell, AppVersion: b.AppVersion, Platform: b.Platform,
			})
			if err != nil {
				return err
			}
			accepted += n
		}
		return nil
	})
	if err != nil {
		problem.Write(w, r, problem.Wrap("internal", err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]int64{"accepted": accepted, "dropped": dropped})
}
