// Package support is the support ticket: the business's help and tickets
// (B-15) and the admin inbox (A-14) (W3-08; 01 §13, 03 support_tickets,
// support_messages and support_attachments, 04 §2.7, D-226), and the
// customer's own tickets (C-19, /v1/me/support/*), which W2-09 deferred.
//
// Rules this package keeps:
//
//   - A business owner or manager opens a ticket for their business (cashiers
//     cannot: support.tickets is owner and manager). A store or order on it must
//     be the business's own (422 otherwise). A customer opens one for
//     themselves, optionally on one of their own orders (422 otherwise). Both
//     start at normal; support may change the priority (audited), which moves
//     the due time to the timer's start plus the new priority's minutes.
//   - The status says whose turn it is. open: support owes a reply and the SLA
//     runs, due support_sla_<priority>_min after the opening or the opener's
//     latest reply, read from the setting at that moment. An agent's reply makes
//     it waiting and stops the timer; resolve and close stop it too. The
//     opener's reply on a waiting or resolved ticket reopens it. A closed ticket
//     takes no more replies (409 invalid_transition); an agent may still add a
//     note to it.
//   - Past the due time the inbox shows the ticket as overdue; nothing changes
//     on its own.
//   - A note is support's only: the business and customer reads never return one.
//   - Assign, priority, resolve and close are audit_log rows written in the same
//     transaction; resolve and close take a reason. An agent's reply on an
//     unassigned ticket assigns it to that agent.
//   - Attachments are objects in the private bucket (R2, MinIO locally),
//     uploaded first through a presigned PUT, then claimed by one message of
//     the uploader (and of the same business, or of none for a customer or an
//     agent). Reads hand out
//     presigned GET links valid for 15 minutes.
package support

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/supportdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Contract bounds (01 §13, D-226): they are the API's shape, so they change
// through code review, not in the console.
const (
	maxBody        = 4000     //settings:ok contract bound (message body ≤ 4000)
	maxReason      = 500      //settings:ok contract bound (close reason ≤ 500)
	maxAttachments = 5        //settings:ok contract bound (attachmentIds maxItems)
	maxFileBytes   = 10 << 20 //settings:ok contract bound (10 MB per attachment)
	minSubject     = 3        //settings:ok contract bound (subject 3..120)
	maxSubject     = 120      //settings:ok contract bound (subject 3..120)

	defaultLimit = 20  //settings:ok page size default, 04 §1
	maxLimit     = 100 //settings:ok page size cap, 04 §1
)

// Deps is what the service needs. Only Clock is required: without the
// database and settings every operation answers 503, without the audit
// writer the audited admin operations do, and without storage an upload does
// (attachment links are then null).
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	Audit    *audit.Writer
	Storage  config.Storage
}

// Service implements the business and admin support routes.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	audit    *audit.Writer
	presign  *presigner
	ids      *ids.Generator
}

var (
	errUnavailable = errors.New("support: not configured")
	errNoStorage   = errors.New("support: storage is not configured")
)

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("support: a clock is required")
	}
	return &Service{
		db: d.DB, clock: d.Clock, settings: d.Settings, audit: d.Audit,
		presign: newPresigner(d.Storage), ids: ids.NewGenerator(),
	}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "support: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) auditReady(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.audit == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errors.New("support: the audit writer is not configured"))
	}
	return nil
}

func (s *Service) storageReady() error {
	if s.presign == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *supportdb.Queries { return supportdb.New(s.db.Q(ctx)) }

// slaKeys maps a priority to its SLA setting (01 §17).
var slaKeys = map[string]settings.Key{
	"urgent": settings.SupportSlaUrgentMin,
	"high":   settings.SupportSlaHighMin,
	"normal": settings.SupportSlaNormalMin,
}

// slaDue is when support's reply is due on a ticket of this priority that
// becomes open at now.
func (s *Service) slaDue(ctx context.Context, priority string, now time.Time) (time.Time, error) {
	key, ok := slaKeys[priority]
	if !ok {
		return time.Time{}, errors.New("support: unknown priority " + priority)
	}
	d, err := s.settings.Duration(ctx, key)
	if err != nil {
		return time.Time{}, err
	}
	return now.Add(d), nil
}

// openerTimer is the SLA timer after the opener's reply: kept while the
// ticket is already open, otherwise started at now from the priority's setting.
func (s *Service) openerTimer(ctx context.Context, t supportdb.LockTicketRow, now time.Time) (start, due *time.Time, err error) {
	if t.Status == "open" && t.SlaStartedAt != nil && t.SlaDueAt != nil {
		return t.SlaStartedAt, t.SlaDueAt, nil
	}
	d, err := s.slaDue(ctx, t.Priority, now)
	if err != nil {
		return nil, nil, err
	}
	return &now, &d, nil
}

// --- attachments ---------------------------------------------------------------------

// upload is a new attachment row and the presigned PUT for it.
type upload struct {
	id          ids.ID
	key         string
	contentType string
	size        int64
	fileName    string
	url         string
	expiresAt   time.Time
}

// Object key prefixes, one per kind of uploader.
const (
	prefixAdmin     = "support/admin/"
	prefixCustomers = "support/customers/"
)

// newUpload validates the file, records the upload as the uploader's (and
// the business's, when business is set) and signs the PUT. prefix is
// prefixAdmin or prefixCustomers; a business upload goes under the business.
func (s *Service) newUpload(ctx context.Context, uploader ids.ID, business pgtype.UUID, prefix, fileName, contentType string, size int64) (upload, error) {
	fileName = strings.TrimSpace(fileName)
	if fileName == "" || utf8.RuneCountInString(fileName) > 120 { //settings:ok contract bound (fileName ≤ 120)
		return upload{}, fieldErr("body.fileName", "1 to 120 characters")
	}
	if !allowedType[contentType] {
		return upload{}, fieldErr("body.contentType", "JPEG, PNG, WebP or PDF")
	}
	if size < 1 || size > maxFileBytes {
		return upload{}, fieldErr("body.sizeBytes", "must be between 1 byte and 10 MB")
	}
	now := s.now()
	id := s.ids.New(now)
	if business.Valid {
		prefix = "support/businesses/" + idString(business) + "/"
	}
	key := prefix + id.String()
	if err := s.q(ctx).InsertAttachment(ctx, supportdb.InsertAttachmentParams{
		ID: ids.UUID(id), UploadedBy: ids.UUID(uploader), BusinessID: business, ObjectKey: key,
		ContentType: contentType, SizeBytes: int32(size), FileName: fileName, Now: now, //nolint:gosec // size ≤ 10 MB, checked above
	}); err != nil {
		return upload{}, err
	}
	return upload{
		id: id, key: key, contentType: contentType, size: size, fileName: fileName,
		url: s.presign.putURL(key, contentType, size, now), expiresAt: now.Add(uploadTTL),
	}, nil
}

var allowedType = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true, "application/pdf": true}

// attachmentIDs parses the ids a message claims: at most five, no repeats.
func attachmentIDs(in *[]string) ([]pgtype.UUID, error) {
	if in == nil || len(*in) == 0 {
		return nil, nil
	}
	if len(*in) > maxAttachments {
		return nil, fieldErr("body.attachmentIds", "at most 5")
	}
	out := make([]pgtype.UUID, 0, len(*in))
	seen := map[ids.ID]bool{}
	for i, v := range *in {
		id, err := ids.Parse(v)
		if err != nil {
			return nil, fieldErr("body.attachmentIds["+strconv.Itoa(i)+"]", "invalid")
		}
		if seen[id] {
			return nil, fieldErr("body.attachmentIds["+strconv.Itoa(i)+"]", "repeated")
		}
		seen[id] = true
		out = append(out, ids.UUID(id))
	}
	return out, nil
}

// claim links the uploads to the message. Every id must be an upload of the
// caller, of the same business (none when business is not set), that no
// message holds yet.
func (s *Service) claim(ctx context.Context, ticket, message pgtype.UUID, uploads []pgtype.UUID, uploader ids.ID, business pgtype.UUID) error {
	if len(uploads) == 0 {
		return nil
	}
	n, err := s.q(ctx).ClaimAttachments(ctx, supportdb.ClaimAttachmentsParams{
		TicketID: ticket, MessageID: message, Ids: uploads, UploadedBy: ids.UUID(uploader), BusinessID: business,
	})
	if err != nil {
		return err
	}
	if n != int64(len(uploads)) {
		return fieldErr("body.attachmentIds", "must be your own uploads that no message holds yet")
	}
	return nil
}

// file is an attachment as both APIs show it.
type file struct {
	id          ids.ID
	fileName    string
	contentType string
	size        int
	url         *string
}

// filesByMessage reads a ticket's attachments, grouped by message, each with
// a presigned GET link (nil while storage is not configured).
func (s *Service) filesByMessage(ctx context.Context, ticket pgtype.UUID) (map[pgtype.UUID][]file, error) {
	rows, err := s.q(ctx).ListTicketAttachments(ctx, ticket)
	if err != nil {
		return nil, err
	}
	now := s.now()
	out := make(map[pgtype.UUID][]file, len(rows))
	for _, r := range rows {
		var u *string
		if s.presign != nil {
			u = ptr(s.presign.getURL(r.ObjectKey, now))
		}
		out[r.MessageID] = append(out[r.MessageID], file{
			id: mustID(r.ID), fileName: r.FileName, contentType: r.ContentType, size: int(r.SizeBytes), url: u,
		})
	}
	return out, nil
}

// --- text ----------------------------------------------------------------------------

func cleanBody(b string) (string, error) {
	b = strings.TrimSpace(b)
	if b == "" || utf8.RuneCountInString(b) > maxBody {
		return "", fieldErr("body.body", "1 to 4000 characters")
	}
	return b, nil
}

func cleanReason(r string) (string, error) {
	r = strings.TrimSpace(r)
	if r == "" || utf8.RuneCountInString(r) > maxReason {
		return "", fieldErr("body.reason", "1 to 500 characters")
	}
	return r, nil
}

func cleanSubject(sub string) (string, error) {
	sub = strings.TrimSpace(sub)
	if n := utf8.RuneCountInString(sub); n < minSubject || n > maxSubject {
		return "", fieldErr("body.subject", "3 to 120 characters")
	}
	return sub, nil
}

// --- helpers --------------------------------------------------------------------------

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func optID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	return ptr(idString(u))
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func ptr[T any](v T) *T { return &v }

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// pageArgs reads limit and cursor. A cursor that does not decode is 422 on query.cursor.
func pageArgs(limit *int, cursor *string) (int32, *time.Time, pgtype.UUID, error) {
	lim := defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return int32(lim), nil, pgtype.UUID{}, nil //nolint:gosec // lim ≤ maxLimit
	}
	at, id, err := decodeCursor(*cursor)
	if err != nil {
		return 0, nil, pgtype.UUID{}, fieldErr("query.cursor", "not a cursor from this list")
	}
	return int32(lim), &at, ids.UUID(id), nil //nolint:gosec // lim ≤ maxLimit
}

// encodeCursor makes the opaque cursor after (at, id).
func encodeCursor(at time.Time, id pgtype.UUID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + idString(id)))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.Zero, errors.New("support: cursor has no id")
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

func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
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

func invalidTransition() *problem.Error { return problem.New(genproblems.InvalidTransition, "") }
