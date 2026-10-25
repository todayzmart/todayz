// Package admin is the admin console's own domain (W3-05, 04 §2.7): customers
// (A-06: exact phone search, block, unblock, strike reset, session revoke) and
// the audit log (A-19 and every screen's audit side panel). Admin order reads
// and the admin cancel live in internal/orders, review moderation in
// internal/reviews and the jobs console in platform/jobs/adminapi.
//
// Every change writes an audit_log row in the same transaction; a phone is
// never logged and is shown only masked.
package admin

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	defaultLimit = 20  //settings:ok the contract's Limit default
	maxLimit     = 100 //settings:ok the contract's Limit maximum
	maxReason    = 500 //settings:ok contract bound (AdminCustomerChange.reason ≤ 500)
)

var errUnavailable = errors.New("admin: not configured")

// StrikeRecounter recomputes a customer's strike count and booking limit
// after a reset (internal/orders implements it).
type StrikeRecounter interface {
	RecountCustomerStrikes(ctx context.Context, userID ids.ID) error
}

// Deps are the admin service's collaborators.
type Deps struct {
	DB    *db.DB
	Clock clock.Clock
	Audit *audit.Writer
	// Authz answers the audit side panel's per-subject check.
	Authz authz.Authorizer
	// Seal opens a stored phone for masking; Hash finds a phone by its keyed
	// hash. Without them phone search answers 503 and phones show as null.
	Seal *pii.Sealer
	Hash *pii.Hasher
	// Strikes is internal/orders' recount.
	Strikes StrikeRecounter
}

// Service implements the admin-customers and admin-audit routes.
type Service struct {
	db      *db.DB
	clock   clock.Clock
	audit   *audit.Writer
	az      authz.Authorizer
	seal    *pii.Sealer
	hash    *pii.Hasher
	strikes StrikeRecounter
}

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("admin: a clock is required")
	}
	return &Service{db: d.DB, clock: d.Clock, audit: d.Audit, az: d.Authz, seal: d.Seal, hash: d.Hash, strikes: d.Strikes}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: not configured (needs the database)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

// writeReady is ready plus the audit writer every change needs.
func (s *Service) writeReady(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.audit == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: the audit writer is not configured")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *admindb.Queries { return admindb.New(s.db.Q(ctx)) }

// maskedPhone opens a sealed phone and masks it; nil when there is none or
// it cannot be opened (the error is logged without the value).
func (s *Service) maskedPhone(ctx context.Context, enc []byte) *string {
	if len(enc) == 0 || s.seal == nil {
		return nil
	}
	plain, err := s.seal.Open("phone", enc)
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "admin: a stored phone did not open")
		return nil
	}
	m := pii.MaskPhone(string(plain))
	return &m
}

// e164 accepts the forms an operator types: +91 98765 43210, 919876543210,
// 9876543210.
func e164(s string) (string, bool) {
	d := strings.NewReplacer(" ", "", "+", "").Replace(s)
	switch {
	case len(d) == 10:
		d = "91" + d
	case len(d) == 12 && strings.HasPrefix(d, "91"):
	default:
		return "", false
	}
	return pii.NormalizePhone("+" + d)
}

func cleanReason(r string) (string, error) {
	r = strings.TrimSpace(r)
	if r == "" || len([]rune(r)) > maxReason {
		return "", fieldErr("body.reason", "1 to 500 characters")
	}
	return r, nil
}

func pageArgs(limit *int, cursor *string) (int, *time.Time, pgtype.UUID, error) {
	lim := defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, nil, pgtype.UUID{}, nil
	}
	at, id, err := decodeCursor(*cursor)
	if err != nil {
		return 0, nil, pgtype.UUID{}, fieldErr("query.cursor", "not a cursor from this list")
	}
	return lim, &at, ids.UUID(id), nil
}

func encodeCursor(at time.Time, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id.String()))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.Zero, errors.New("admin: cursor has no id")
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	return time.Unix(0, n).UTC(), id, nil
}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

// pathID parses a ULID path parameter; one that is not a ULID names nothing (404).
func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, notFound()
	}
	return id, nil
}

func idString(u pgtype.UUID) string {
	id, _ := ids.FromUUID(u)
	return id.String()
}

func optID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := idString(u)
	return &s
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
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

func invalidTransition(detail string) *problem.Error {
	return problem.New(genproblems.InvalidTransition, detail)
}
