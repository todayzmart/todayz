package notify

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	notificationsapi "github.com/todayz/backend/gen/api/notifications"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/notifydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
)

// Page sizes of the inbox lists (04 §1: at most 100).
const (
	defaultLimit = 20
	maxLimit     = 100
)

// Ops maps the notifications domain's operations to their authz actions,
// keyed as the generated strict server names them (the Go method name).
var Ops = map[string]string{
	"ListMyNotifications":       policy.MeNotifications,
	"ReadMyNotifications":       policy.MeNotifications,
	"GetMyNotificationPrefs":    policy.MeNotificationPrefs,
	"UpdateMyNotificationPrefs": policy.MeNotificationPrefs,
	"ListBusinessInbox":         policy.InboxView,
	"ReadBusinessInbox":         policy.InboxView,
}

// Register mounts the inbox and preference operations behind slot 15.
func Register(r chi.Router, s *Service, az authz.Authorizer) {
	notificationsapi.Register(r, handlers{s},
		notificationsapi.StrictMiddlewareFunc(authz.Strict[notificationsapi.StrictHandlerFunc](az, Ops)))
}

type handlers struct{ s *Service }

var _ notificationsapi.StrictServerInterface = handlers{}

func (h handlers) ready() error {
	if h.s == nil || h.s.d.DB == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errors.New("notify: no database"))
	}
	return nil
}

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

// --- customer inbox ------------------------------------------------------------

// ListMyNotifications implements GET /v1/me/notifications.
func (h handlers) ListMyNotifications(ctx context.Context, req notificationsapi.ListMyNotificationsRequestObject) (notificationsapi.ListMyNotificationsResponseObject, error) {
	if err := h.ready(); err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	lim, at, bid, err := pageArgs(req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	q := notifydb.New(h.s.d.DB.Pool)
	rows, err := q.ListUserNotifications(ctx, notifydb.ListUserNotificationsParams{
		UserID: ids.UUID(p.UserID), App: AppCustomer, BeforeAt: at, BeforeID: bid, Lim: int32(lim + 1),
	})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	unread, err := q.CountUserUnread(ctx, notifydb.CountUserUnreadParams{UserID: ids.UUID(p.UserID), App: AppCustomer})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	items := make([]item, len(rows))
	for i, r := range rows {
		items[i] = item{r.ID, r.StoreID, r.Kind, r.Params, r.RefType, r.RefID, r.ReadAt, r.CreatedAt}
	}
	return notificationsapi.ListMyNotifications200JSONResponse(
		page(items, lim, unread, langOf(req.Params.AcceptLanguage), false)), nil
}

// ReadMyNotifications implements POST /v1/me/notifications/read.
func (h handlers) ReadMyNotifications(ctx context.Context, req notificationsapi.ReadMyNotificationsRequestObject) (notificationsapi.ReadMyNotificationsResponseObject, error) {
	if err := h.ready(); err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	all, list, err := readArgs(req.Body)
	if err != nil {
		return nil, err
	}
	q := notifydb.New(h.s.d.DB.Pool)
	if err := q.MarkUserRead(ctx, notifydb.MarkUserReadParams{
		Now: h.s.d.Clock.Now(), UserID: ids.UUID(p.UserID), App: AppCustomer, MarkAll: all, Ids: list,
	}); err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	unread, err := q.CountUserUnread(ctx, notifydb.CountUserUnreadParams{UserID: ids.UUID(p.UserID), App: AppCustomer})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	return notificationsapi.ReadMyNotifications200JSONResponse{UnreadCount: int(unread)}, nil
}

// --- business inbox ------------------------------------------------------------

// storeLimit is the member's store list as a query argument: nil for every
// store of the business, else the stores (an empty list sees business-wide
// rows only).
func (h handlers) storeLimit(ctx context.Context, p authn.Principal, bid ids.ID) ([]pgtype.UUID, error) {
	if h.s.d.Staff == nil {
		return nil, problem.Wrap(genproblems.ServiceUnavailable, errors.New("notify: no staff resolver"))
	}
	m, ok, err := h.s.d.Staff.Membership(ctx, p.UserID, bid)
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	if !ok {
		// authz allowed a moment ago; the membership ended in between.
		return nil, problem.New(genproblems.NotFound, "")
	}
	if m.StoreIDs == nil {
		return nil, nil
	}
	out := make([]pgtype.UUID, len(m.StoreIDs))
	for i, s := range m.StoreIDs {
		out[i] = ids.UUID(s)
	}
	return out, nil
}

func scopeBusiness(ctx context.Context) (ids.ID, error) {
	d, ok := authz.DecisionFrom(ctx)
	if !ok || d.Scope.BusinessID == (ids.ID{}) {
		return ids.ID{}, problem.Wrap(genproblems.Internal, errors.New("notify: no authz decision"))
	}
	return d.Scope.BusinessID, nil
}

// ListBusinessInbox implements GET /v1/biz/businesses/{bid}/inbox.
func (h handlers) ListBusinessInbox(ctx context.Context, req notificationsapi.ListBusinessInboxRequestObject) (notificationsapi.ListBusinessInboxResponseObject, error) {
	if err := h.ready(); err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	bid, err := scopeBusiness(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := h.storeLimit(ctx, p, bid)
	if err != nil {
		return nil, err
	}
	lim, at, before, err := pageArgs(req.Params.Limit, req.Params.Cursor)
	if err != nil {
		return nil, err
	}
	q := notifydb.New(h.s.d.DB.Pool)
	rows, err := q.ListBusinessNotifications(ctx, notifydb.ListBusinessNotificationsParams{
		BusinessID: ids.UUID(bid), StoreIds: stores, BeforeAt: at, BeforeID: before, Lim: int32(lim + 1),
	})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	unread, err := q.CountBusinessUnread(ctx, notifydb.CountBusinessUnreadParams{BusinessID: ids.UUID(bid), StoreIds: stores})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	items := make([]item, len(rows))
	for i, r := range rows {
		items[i] = item{r.ID, r.StoreID, r.Kind, r.Params, r.RefType, r.RefID, r.ReadAt, r.CreatedAt}
	}
	return notificationsapi.ListBusinessInbox200JSONResponse(
		page(items, lim, unread, langOf(req.Params.AcceptLanguage), true)), nil
}

// ReadBusinessInbox implements POST /v1/biz/businesses/{bid}/inbox/read.
func (h handlers) ReadBusinessInbox(ctx context.Context, req notificationsapi.ReadBusinessInboxRequestObject) (notificationsapi.ReadBusinessInboxResponseObject, error) {
	if err := h.ready(); err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	bid, err := scopeBusiness(ctx)
	if err != nil {
		return nil, err
	}
	stores, err := h.storeLimit(ctx, p, bid)
	if err != nil {
		return nil, err
	}
	all, list, err := readArgs(req.Body)
	if err != nil {
		return nil, err
	}
	q := notifydb.New(h.s.d.DB.Pool)
	if err := q.MarkBusinessRead(ctx, notifydb.MarkBusinessReadParams{
		Now: h.s.d.Clock.Now(), BusinessID: ids.UUID(bid), StoreIds: stores, MarkAll: all, Ids: list,
	}); err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	unread, err := q.CountBusinessUnread(ctx, notifydb.CountBusinessUnreadParams{BusinessID: ids.UUID(bid), StoreIds: stores})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	return notificationsapi.ReadBusinessInbox200JSONResponse{UnreadCount: int(unread)}, nil
}

// --- preferences ---------------------------------------------------------------

// GetMyNotificationPrefs implements GET /v1/me/notification-prefs.
func (h handlers) GetMyNotificationPrefs(ctx context.Context, _ notificationsapi.GetMyNotificationPrefsRequestObject) (notificationsapi.GetMyNotificationPrefsResponseObject, error) {
	if err := h.ready(); err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out, err := h.prefs(ctx, p)
	if err != nil {
		return nil, err
	}
	return notificationsapi.GetMyNotificationPrefs200JSONResponse(out), nil
}

// UpdateMyNotificationPrefs implements PATCH /v1/me/notification-prefs.
func (h handlers) UpdateMyNotificationPrefs(ctx context.Context, req notificationsapi.UpdateMyNotificationPrefsRequestObject) (notificationsapi.UpdateMyNotificationPrefsResponseObject, error) {
	if err := h.ready(); err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, problem.New(genproblems.ValidationFailed, "").WithField("body", "required")
	}
	app := string(p.App)
	var bad *problem.Error
	for i, it := range req.Body.Items {
		def, ok := catalogue[it.Kind]
		if !ok || def.App != app {
			bad = fieldErr(bad, fmt.Sprintf("body.items[%d].kind", i), "not a kind this app can switch")
			continue
		}
		if !channelAllowed(def, noticeChannel(it.Channel)) {
			bad = fieldErr(bad, fmt.Sprintf("body.items[%d].channel", i), "not a channel of this kind")
		}
	}
	if bad != nil {
		return nil, bad
	}
	now := h.s.d.Clock.Now()
	err = h.s.d.DB.InTx(ctx, func(ctx context.Context) error {
		q := h.s.d.DB.Q(ctx)
		for _, it := range req.Body.Items {
			if err := notifydb.New(q).UpsertPref(ctx, notifydb.UpsertPrefParams{
				UserID: ids.UUID(p.UserID), App: app, Kind: it.Kind, Channel: string(it.Channel), Enabled: it.Enabled, Now: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, problem.Wrap(genproblems.Internal, err)
	}
	out, err := h.prefs(ctx, p)
	if err != nil {
		return nil, err
	}
	return notificationsapi.UpdateMyNotificationPrefs200JSONResponse(out), nil
}

func noticeChannel(c notificationsapi.NotificationChannel) notice.Channel { return notice.Channel(c) }

func fieldErr(p *problem.Error, path, reason string) *problem.Error {
	if p == nil {
		p = problem.New(genproblems.ValidationFailed, "")
	}
	return p.WithField(path, reason)
}

// prefs lists every switch of the principal's app: each kind of the app with
// each of its channels, on unless a row says off.
func (h handlers) prefs(ctx context.Context, p authn.Principal) (notificationsapi.NotificationPrefs, error) {
	app := string(p.App)
	rows, err := notifydb.New(h.s.d.DB.Pool).ListPrefs(ctx, notifydb.ListPrefsParams{UserID: ids.UUID(p.UserID), App: app})
	if err != nil {
		return notificationsapi.NotificationPrefs{}, problem.Wrap(genproblems.Internal, err)
	}
	set := map[string]bool{}
	for _, r := range rows {
		set[r.Kind+"/"+r.Channel] = r.Enabled
	}
	out := notificationsapi.NotificationPrefs{Items: []notificationsapi.NotificationPref{}}
	for _, k := range Kinds() {
		def := catalogue[k]
		if def.App != app {
			continue
		}
		for _, ch := range def.Channels {
			on, ok := set[k+"/"+string(ch)]
			if !ok {
				on = true
			}
			out.Items = append(out.Items, notificationsapi.NotificationPref{
				Kind: k, Channel: notificationsapi.NotificationChannel(ch), Enabled: on,
			})
		}
	}
	return out, nil
}

// --- shared --------------------------------------------------------------------

type item struct {
	ID        pgtype.UUID
	StoreID   pgtype.UUID
	Kind      string
	Params    json.RawMessage
	RefType   *string
	RefID     pgtype.UUID
	ReadAt    *time.Time
	CreatedAt time.Time
}

func page(rows []item, lim int, unread int64, lang string, business bool) notificationsapi.NotificationPage {
	out := notificationsapi.NotificationPage{Items: []notificationsapi.Notification{}, UnreadCount: int(unread)}
	more := len(rows) > lim
	if more {
		rows = rows[:lim]
	}
	for _, r := range rows {
		id, _ := ids.FromUUID(r.ID)
		var params map[string]string
		_ = json.Unmarshal(r.Params, &params)
		t, _ := textFor(r.Kind, lang, params)
		n := notificationsapi.Notification{
			Id: id.String(), Kind: r.Kind, Title: clipRunes(t.Title, 200), Body: clipRunes(t.Body, 1000),
			CreatedAt: r.CreatedAt.UTC(),
		}
		if r.ReadAt != nil {
			v := r.ReadAt.UTC()
			n.ReadAt = &v
		}
		if r.RefType != nil {
			if rid, ok := ids.FromUUID(r.RefID); ok {
				n.Ref = &notificationsapi.NotificationRef{Type: *r.RefType, Id: rid.String()}
			}
		}
		if business {
			if sid, ok := ids.FromUUID(r.StoreID); ok {
				v := sid.String()
				n.StoreId = &v
			}
		}
		out.Items = append(out.Items, n)
	}
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		id, _ := ids.FromUUID(last.ID)
		c := encodeCursor(last.CreatedAt, id)
		out.NextCursor = &c
	}
	return out
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func langOf(h *string) string {
	if h == nil {
		return "en"
	}
	return langFromHeader(*h)
}

// pageArgs reads limit and cursor. A cursor that does not decode is 422 on
// query.cursor.
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
		return 0, nil, pgtype.UUID{}, problem.New(genproblems.ValidationFailed, "").WithField("query.cursor", "not a cursor from this list")
	}
	return lim, &at, ids.UUID(id), nil
}

// readArgs reads {ids} or {all:true}: exactly one.
func readArgs(b *notificationsapi.NotificationReadRequest) (bool, []pgtype.UUID, error) {
	if b == nil {
		return false, nil, problem.New(genproblems.ValidationFailed, "").WithField("body", "required")
	}
	all := b.All != nil && bool(*b.All)
	has := b.Ids != nil && len(*b.Ids) > 0
	if all == has {
		return false, nil, problem.New(genproblems.ValidationFailed, "").WithField("body", "send ids or all, not both")
	}
	if all {
		return true, []pgtype.UUID{}, nil
	}
	list := make([]pgtype.UUID, 0, len(*b.Ids))
	for i, s := range *b.Ids {
		id, err := ids.Parse(s)
		if err != nil {
			return false, nil, problem.New(genproblems.ValidationFailed, "").WithField(fmt.Sprintf("body.ids[%d]", i), "not a ULID")
		}
		list = append(list, ids.UUID(id))
	}
	return false, list, nil
}

// encodeCursor makes the opaque cursor after (createdAt, id).
func encodeCursor(at time.Time, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id.String()))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.ID{}, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.ID{}, errors.New("notify: cursor has no id")
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return time.Time{}, ids.ID{}, err
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return time.Time{}, ids.ID{}, err
	}
	return time.Unix(0, n).UTC(), id, nil
}
