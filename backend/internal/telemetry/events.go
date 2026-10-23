package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/telemetrydb"
	"github.com/todayz/backend/internal/platform/eventcat"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// An event's time must be recent: a device that was offline for a while still reports, but a clock
// that is months off (or in the future) is a bug or abuse, not data.
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

// validate checks every event against the catalogue and returns one field entry per problem.
// Reasons never repeat a client value.
func (s *Service) validate(b *eventBatch) *problem.Error {
	now := s.d.Clock.Now()
	var perr *problem.Error
	fail := func(path, reason string) {
		if perr == nil {
			perr = problem.New("validation_failed", "")
		}
		_ = perr.WithField(path, reason)
	}
	seen := map[string]struct{}{}
	for i, e := range b.Events {
		p := fmt.Sprintf("body.events[%d]", i)
		if _, err := ids.Parse(e.ID); err != nil {
			fail(p+".id", "must be a ULID")
		} else if _, dup := seen[e.ID]; dup {
			fail(p+".id", "repeated in this batch")
		}
		seen[e.ID] = struct{}{}
		def, ok := s.cat.Event(e.Name)
		switch {
		case !ok:
			fail(p+".name", "not a known event")
		case def.Emitter != eventcat.EmitterClient:
			fail(p+".name", "this event is emitted by the server")
		default:
			props := e.Props
			if props == nil {
				props = map[string]any{}
			}
			if err := def.Validate(props); err != nil {
				fail(p+".props", err.Error())
			}
		}
		if e.T.Before(now.Add(-maxEventAge)) || e.T.After(now.Add(maxEventFuture)) {
			fail(p+".t", "outside the accepted time window")
		}
	}
	return perr
}

func (s *Service) postEvents(w http.ResponseWriter, r *http.Request) {
	var b eventBatch
	if !decode(w, r, &b) {
		return
	}
	if perr := s.validate(&b); perr != nil {
		problem.Write(w, r, perr)
		return
	}
	ctx := r.Context()
	var userID pgtype.UUID
	if s.d.UserID != nil {
		userID = optUUID(s.d.UserID(ctx))
	}
	anon, session := optUUID(b.AnonID), optUUID(b.SessionID)

	var accepted int64
	err := s.d.DB.InTx(ctx, func(ctx context.Context) error {
		q := telemetrydb.New(s.d.DB.Q(ctx))
		for _, e := range b.Events {
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
	_ = json.NewEncoder(w).Encode(map[string]int64{"accepted": accepted})
}
