// Package audit appends rows to audit_log (03). There is no update or delete:
// the table refuses both with a trigger, and this package offers only Write.
// Write joins the transaction in ctx, so the audit row commits or rolls back
// with the change it describes.
package audit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/reqctx"
)

// Actor kinds (ck_audit_log_actor_kind).
const (
	ActorUser   = "user"
	ActorStaff  = "staff"
	ActorAdmin  = "admin"
	ActorSystem = "system"
	ActorDevice = "device"
)

var (
	reAction  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	reSubject = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Entry is one audited change. Before and After are JSON-encoded as given;
// never put a phone, OTP, order code, token or address in them.
type Entry struct {
	ActorKind   string
	ActorID     ids.ID // zero for system
	Action      string // domain.verb, e.g. "settings.update"
	SubjectType string // e.g. "platform_setting", "store"
	SubjectID   string
	Before      any
	After       any
	// IP overrides the client IP in ctx (reqctx.ClientIP). Only its keyed hash is stored.
	IP string
}

// Writer appends entries.
type Writer struct {
	db    *db.DB
	clock clock.Clock
	ids   *ids.Generator
	// ipKey is PII_HASH_KEY; nil stores no ip_hash.
	ipKey []byte
}

// New returns a writer. ipHashKey is the PII_HASH_KEY bytes (may be nil locally).
func New(d *db.DB, c clock.Clock, ipHashKey []byte) *Writer {
	return &Writer{db: d, clock: c, ids: ids.NewGenerator(), ipKey: ipHashKey}
}

// Write appends e and returns its id.
func (w *Writer) Write(ctx context.Context, e Entry) (ids.ID, error) {
	switch e.ActorKind {
	case ActorUser, ActorStaff, ActorAdmin, ActorSystem, ActorDevice:
	default:
		return ids.Zero, fmt.Errorf("audit: bad actor kind %q", e.ActorKind)
	}
	if !reAction.MatchString(e.Action) {
		return ids.Zero, fmt.Errorf("audit: action %q must be domain.verb", e.Action)
	}
	if !reSubject.MatchString(e.SubjectType) {
		return ids.Zero, fmt.Errorf("audit: bad subject type %q", e.SubjectType)
	}
	if e.ActorKind != ActorSystem && e.ActorID == ids.Zero {
		return ids.Zero, errors.New("audit: actor id required unless the actor is the system")
	}
	before, err := encode(e.Before)
	if err != nil {
		return ids.Zero, fmt.Errorf("audit: before: %w", err)
	}
	after, err := encode(e.After)
	if err != nil {
		return ids.Zero, fmt.Errorf("audit: after: %w", err)
	}
	ip := e.IP
	if ip == "" {
		ip = reqctx.ClientIP(ctx)
	}
	var ipHash []byte
	if ip != "" && len(w.ipKey) > 0 {
		m := hmac.New(sha256.New, w.ipKey)
		m.Write([]byte(ip))
		ipHash = m.Sum(nil)
	}
	now := w.clock.Now()
	id := w.ids.New(now)
	var actor any
	if e.ActorID != ids.Zero {
		actor = ids.UUID(e.ActorID)
	}
	var reqID any
	if r := reqctx.RequestID(ctx); r != "" {
		reqID = r
	}
	var subjectID any
	if e.SubjectID != "" {
		subjectID = e.SubjectID
	}
	_, err = w.db.Q(ctx).Exec(ctx, `
		INSERT INTO audit_log (id, actor_kind, actor_id, action, subject_type, subject_id, before, after, ip_hash, request_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		ids.UUID(id), e.ActorKind, actor, e.Action, e.SubjectType, subjectID, before, after, ipHash, reqID, now)
	if err != nil {
		return ids.Zero, fmt.Errorf("audit: insert: %w", err)
	}
	return id, nil
}

func encode(v any) ([]byte, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		if !json.Valid(x) {
			return nil, errors.New("invalid JSON")
		}
		return x, nil
	default:
		return json.Marshal(v)
	}
}
