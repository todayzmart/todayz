package support

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adminapi "github.com/todayz/backend/gen/api/admin-support"
	bizapi "github.com/todayz/backend/gen/api/biz-support"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// The SC-SP scenarios (08 §3.7) at the service: the authz half (who may call
// which route) is the policy golden's, testdata/support.golden.

type env struct {
	t     *testing.T
	ctx   context.Context
	tdb   *testkit.DB
	clk   *clock.Fake
	st    *settings.Cache
	svc   *Service
	gen   *ids.Generator
	n     int
	owner ids.ID // Lakshmi's owner
	agent ids.ID // a support agent
	biz   ids.ID
	store ids.ID
	other ids.ID // another business
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	clk := clock.NewFake(time.Date(2026, 10, 22, 4, 30, 0, 0, time.UTC))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{TTL: time.Hour, Log: log})
	d := db.New(tdb.Pool)
	svc, err := New(Deps{DB: d, Clock: clk, Settings: st, Audit: audit.New(d, clk, nil), Storage: config.Storage{
		Endpoint: "http://127.0.0.1:9100", AccessKeyID: config.NewSecret([]byte("minio")),
		SecretAccessKey: config.NewSecret([]byte("minio-secret")), BucketPrivate: "todayz-private",
	}})
	require.NoError(t, err)
	e := &env{t: t, ctx: context.Background(), tdb: tdb, clk: clk, st: st, svc: svc, gen: ids.NewGenerator()}
	e.owner = e.user("Lakshmi")
	e.agent = e.user("Priya")
	e.exec(`INSERT INTO admin_users (user_id, roles) VALUES ($1, '{support}')`, ids.UUID(e.agent))
	e.biz, e.store = e.business("Lakshmi Traders")
	e.other, _ = e.business("Kaveri Fresh")
	return e
}

func (e *env) id() ids.ID { return e.gen.New(e.clk.Now()) }

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(e.ctx, sql, args...)
	require.NoError(e.t, err)
}

func (e *env) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(e.ctx, sql, args...).Scan(&n))
	return n
}

func (e *env) user(name string) ids.ID {
	e.t.Helper()
	id := e.tdb.InsertUser(e.t, e.id())
	e.exec(`UPDATE users SET name = $2 WHERE id = $1`, ids.UUID(id), name)
	return id
}

func (e *env) business(name string) (biz, store ids.ID) {
	e.t.Helper()
	e.n++
	code := string("ABCDEFGHJK"[e.n]) + "SUP2" + string("ABCDEFGHJK"[e.n])
	biz, store = e.id(), e.id()
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code, status) VALUES ($1, $2, $3, 'verified')`, ids.UUID(biz), name, code)
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name, cashback_bp) VALUES ($1, $2, $3, $4, $5, 500)`,
		ids.UUID(store), ids.UUID(biz), strings.ToLower(strings.ReplaceAll(name, " ", "-")), code[1:]+"Z", name)
	return biz, store
}

func (e *env) setting(key settings.Key, v string) {
	e.t.Helper()
	e.exec(`UPDATE platform_settings SET value = $2::jsonb WHERE key = $1`, string(key), v)
	e.st.Invalidate()
}

// open opens a business ticket as the owner.
func (e *env) open(subject string) bizapi.BizTicketThread {
	e.t.Helper()
	th, err := e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{
		Subject: subject, Body: "Our payout has not arrived.", Category: "payouts", StoreId: new(e.store.String()),
	})
	require.NoError(e.t, err)
	return th
}

func (e *env) tid(th bizapi.BizTicketThread) ids.ID {
	e.t.Helper()
	id, err := ids.Parse(th.Ticket.Id)
	require.NoError(e.t, err)
	return id
}

func code(t *testing.T, err error) string {
	t.Helper()
	var pe *problem.Error
	require.True(t, errors.As(err, &pe), "want a problem, got %v", err)
	return pe.Code
}

func field(t *testing.T, err error) []string {
	t.Helper()
	var pe *problem.Error
	require.True(t, errors.As(err, &pe), "want a problem, got %v", err)
	var out []string
	for k := range pe.Fields {
		out = append(out, k)
	}
	return out
}

// SC-SP-01: a business opens a ticket with its first message; the due time is
// the opening plus support_sla_normal_min; a store or order must be its own.
func TestOpenBusinessTicket(t *testing.T) {
	e := newEnv(t)
	now := e.clk.Now()
	th := e.open("Payout missing")
	require.Equal(t, bizapi.BizTicketStatus("open"), th.Ticket.Status)
	require.Regexp(t, `^SUP-\d{6}$`, th.Ticket.Number)
	require.Equal(t, "Lakshmi Traders", deref(th.Ticket.StoreName))
	require.Len(t, th.Messages, 1)
	require.Equal(t, bizapi.Business, th.Messages[0].From)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE sla_due_at = $1 AND priority = 'normal'`, now.Add(24*time.Hour)))

	_, otherStore := e.business("Third Shop")
	_, err := e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Wrong store", Body: "x", Category: "account", StoreId: new(otherStore.String())})
	require.Equal(t, []string{"body.storeId"}, field(t, err))

	customer := e.user("Asha")
	order := e.tdb.InsertOrder(t, e.id(), customer, otherStore)
	_, err = e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Wrong order", Body: "x", Category: "order", OrderId: new(order.String())})
	require.Equal(t, []string{"body.orderId"}, field(t, err))

	own := e.tdb.InsertOrder(t, e.id(), customer, e.store)
	th, err = e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "About an order", Body: "x", Category: "order", OrderId: new(own.String())})
	require.NoError(t, err)
	require.Equal(t, e.store.String(), deref(th.Ticket.StoreId), "the order's store is filled in")

	_, err = e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "  ", Body: "x", Category: "order"})
	require.Equal(t, []string{"body.subject"}, field(t, err))
}

// SC-SP-02: due times come from platform_settings per priority; the open tab
// lists soonest due first and counts what is overdue.
func TestSLAFromSettings(t *testing.T) {
	e := newEnv(t)
	e.setting(settings.SupportSlaNormalMin, "60")
	e.open("First")
	e.clk.Advance(10 * time.Minute)
	e.setting(settings.SupportSlaNormalMin, "15")
	b := e.open("Second")

	l, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{})
	require.NoError(t, err)
	require.Len(t, l.Items, 2)
	require.Equal(t, b.Ticket.Id, l.Items[0].Id, "soonest due first")
	require.Equal(t, e.clk.Now().Add(15*time.Minute), *l.Items[0].SlaDueAt)
	require.Equal(t, e.clk.Now().Add(50*time.Minute), *l.Items[1].SlaDueAt)
	require.Zero(t, l.Counts.Overdue)

	e.clk.Advance(16 * time.Minute)
	l, err = e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{})
	require.NoError(t, err)
	require.True(t, l.Items[0].Overdue)
	require.False(t, l.Items[1].Overdue)
	require.Equal(t, 1, l.Counts.Overdue)
	require.Equal(t, 2, l.Counts.Open)
	require.Equal(t, e.clk.Now(), l.ServerNow)
}

// SC-SP-03: an agent's reply hands the turn to the opener: waiting, no due
// time, the first response recorded, an unassigned ticket taken by the agent;
// the business sees "TodayZ support", never the agent's name.
func TestAgentReply(t *testing.T) {
	e := newEnv(t)
	tid := e.tid(e.open("Payout missing"))
	e.clk.Advance(time.Hour)
	d, err := e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "We are checking with the bank."})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketStatusWaiting, d.Ticket.Status)
	require.Nil(t, d.Ticket.SlaDueAt)
	require.Equal(t, e.clk.Now(), *d.FirstResponseAt)
	require.Equal(t, e.agent.String(), d.Ticket.Assignee.Id)

	th, err := e.svc.getThread(e.ctx, e.biz, tid)
	require.NoError(t, err)
	require.Len(t, th.Messages, 2)
	require.Equal(t, bizapi.Support, th.Messages[1].From)
	require.Nil(t, th.Messages[1].AuthorName)
	require.Equal(t, bizapi.BizTicketStatus("waiting"), th.Ticket.Status)

	e.clk.Advance(time.Hour)
	d, err = e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "Still checking."})
	require.NoError(t, err)
	require.Equal(t, e.clk.Now().Add(-time.Hour), *d.FirstResponseAt, "the first response stays")
}

// SC-SP-04: the opener's reply reopens a waiting or resolved ticket with a
// fresh due time; a closed ticket takes no reply from anyone.
func TestOpenerReplyAndClose(t *testing.T) {
	e := newEnv(t)
	tid := e.tid(e.open("Payout missing"))
	_, err := e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "Which week?"})
	require.NoError(t, err)

	e.clk.Advance(2 * time.Hour)
	th, err := e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "The week of 12 October."})
	require.NoError(t, err)
	require.Equal(t, bizapi.BizTicketStatus("open"), th.Ticket.Status)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND sla_due_at = $2`, ids.UUID(tid), e.clk.Now().Add(24*time.Hour)))

	// A second reply while open keeps the due time.
	e.clk.Advance(time.Hour)
	_, err = e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "Any news?"})
	require.NoError(t, err)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND sla_due_at = $2`, ids.UUID(tid), e.clk.Now().Add(23*time.Hour)))

	d, err := e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeResolved, "Paid on 14 October.")
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketStatusResolved, d.Ticket.Status)
	require.Nil(t, d.Ticket.SlaDueAt)
	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeResolved, "again")
	require.Equal(t, genproblems.InvalidTransition, code(t, err))

	th, err = e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "It is not in the account."})
	require.NoError(t, err)
	require.Equal(t, bizapi.BizTicketStatus("open"), th.Ticket.Status, "a resolved ticket reopens")

	d, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeClosed, "Duplicate of SUP-001002.")
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketStatusClosed, d.Ticket.Status)
	require.Equal(t, "Duplicate of SUP-001002.", deref(d.CloseReason))

	_, err = e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "Hello?"})
	require.Equal(t, genproblems.InvalidTransition, code(t, err))
	_, err = e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "Hello"})
	require.Equal(t, genproblems.InvalidTransition, code(t, err))
	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeClosed, "again")
	require.Equal(t, genproblems.InvalidTransition, code(t, err))
	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeResolved, " ")
	require.Equal(t, []string{"body.reason"}, field(t, err))
}

// SC-SP-05: internal notes stay inside TodayZ: they do not change the status
// or the due time, and the business's thread never carries them.
func TestNotesHidden(t *testing.T) {
	e := newEnv(t)
	tid := e.tid(e.open("Payout missing"))
	d, err := e.svc.adminMessage(e.ctx, e.agent, tid, "note", adminapi.TicketMessageRequest{Body: "Bank says UTR pending; do not promise a date."})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketStatusOpen, d.Ticket.Status)
	require.NotNil(t, d.Ticket.SlaDueAt)
	require.Nil(t, d.FirstResponseAt, "a note is not a response")
	require.Len(t, d.Messages, 2)
	require.Equal(t, "note", string(d.Messages[1].Kind))

	th, err := e.svc.getThread(e.ctx, e.biz, tid)
	require.NoError(t, err)
	require.Len(t, th.Messages, 1)
	for _, m := range th.Messages {
		require.NotContains(t, m.Body, "UTR")
	}
	l, err := e.svc.listBiz(e.ctx, e.biz, nil, nil)
	require.NoError(t, err)
	require.Len(t, l.Items, 1)
}

// SC-SP-06: assign, resolve and close each write one audit row with the
// actor; the assignee must be an active support, admin or super_admin user.
func TestAssignAndAudit(t *testing.T) {
	e := newEnv(t)
	tid := e.tid(e.open("Payout missing"))
	audits := func(action string) int {
		return e.count(`SELECT count(*) FROM audit_log WHERE action = $1 AND subject_type = 'support_ticket' AND subject_id = $2 AND actor_id = $3 AND actor_kind = 'admin'`,
			action, tid.String(), ids.UUID(e.agent))
	}
	d, err := e.svc.assign(e.ctx, e.agent, tid, new(e.agent.String()))
	require.NoError(t, err)
	require.Equal(t, e.agent.String(), d.Ticket.Assignee.Id)
	require.Equal(t, "Priya", deref(d.Ticket.Assignee.Name))
	require.Len(t, d.Agents, 1)
	require.Equal(t, 1, audits("support.ticket_assign"))
	_, err = e.svc.assign(e.ctx, e.agent, tid, new(e.agent.String()))
	require.NoError(t, err)
	require.Equal(t, 1, audits("support.ticket_assign"), "the same assignee again changes nothing")

	finance := e.user("Ravi")
	e.exec(`INSERT INTO admin_users (user_id, roles) VALUES ($1, '{finance}')`, ids.UUID(finance))
	_, err = e.svc.assign(e.ctx, e.agent, tid, new(finance.String()))
	require.Equal(t, []string{"body.assigneeId"}, field(t, err))
	_, err = e.svc.assign(e.ctx, e.agent, tid, new(e.owner.String()))
	require.Equal(t, []string{"body.assigneeId"}, field(t, err), "a business user is not an agent")

	d, err = e.svc.assign(e.ctx, e.agent, tid, nil)
	require.NoError(t, err)
	require.Nil(t, d.Ticket.Assignee)
	require.Equal(t, 2, audits("support.ticket_assign"))

	l, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Assignee: new(adminapi.Unassigned)})
	require.NoError(t, err)
	require.Len(t, l.Items, 1)

	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeResolved, "Paid.")
	require.NoError(t, err)
	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeClosed, "No reply in a week.")
	require.NoError(t, err)
	require.Equal(t, 1, audits("support.ticket_resolve"))
	require.Equal(t, 1, audits("support.ticket_close"))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_close' AND after->>'reason' = 'No reply in a week.' AND before->>'status' = 'resolved'`))

	_, err = e.svc.assign(e.ctx, e.agent, tid, new(e.agent.String()))
	require.Equal(t, genproblems.InvalidTransition, code(t, err), "a closed ticket is not reassigned")
}

// SC-SP-07: attachments: a presigned PUT into the private bucket with the
// type and size signed, limits on both, and only the uploader's own unused
// upload can go on a message, once.
func TestAttachments(t *testing.T) {
	e := newEnv(t)
	u, err := e.svc.bizUpload(e.ctx, e.owner, e.biz, bizapi.BizAttachmentCreate{FileName: "statement.pdf", ContentType: "application/pdf", SizeBytes: 2048})
	require.NoError(t, err)
	require.Equal(t, bizapi.PUT, u.Upload.Method)
	require.Equal(t, "2048", u.Upload.Headers.ContentLength)
	put, err := url.Parse(u.Upload.Url)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9100", put.Host, "MinIO locally")
	require.True(t, strings.HasPrefix(put.Path, "/todayz-private/support/businesses/"+e.biz.String()+"/"), put.Path)
	require.Contains(t, put.Query().Get("X-Amz-SignedHeaders"), "content-length")

	_, err = e.svc.bizUpload(e.ctx, e.owner, e.biz, bizapi.BizAttachmentCreate{FileName: "big.pdf", ContentType: "application/pdf", SizeBytes: 10<<20 + 1})
	require.Equal(t, []string{"body.sizeBytes"}, field(t, err))
	_, err = e.svc.bizUpload(e.ctx, e.owner, e.biz, bizapi.BizAttachmentCreate{FileName: "x.exe", ContentType: "application/x-msdownload", SizeBytes: 10})
	require.Equal(t, []string{"body.contentType"}, field(t, err))

	th, err := e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Statement", Body: "Attached.", Category: "payouts",
		AttachmentIds: &[]string{u.Attachment.Id}})
	require.NoError(t, err)
	require.Len(t, th.Messages[0].Attachments, 1)
	get := th.Messages[0].Attachments[0].Url
	require.NotNil(t, get)
	require.Contains(t, *get, "X-Amz-Signature=")
	tid := e.tid(th)

	// Used once only.
	_, err = e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "Again", AttachmentIds: &[]string{u.Attachment.Id}})
	require.Equal(t, []string{"body.attachmentIds"}, field(t, err))
	// Not someone else's.
	manager := e.user("Suresh")
	m, err := e.svc.bizUpload(e.ctx, manager, e.biz, bizapi.BizAttachmentCreate{FileName: "a.png", ContentType: "image/png", SizeBytes: 10})
	require.NoError(t, err)
	_, err = e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "Theirs", AttachmentIds: &[]string{m.Attachment.Id}})
	require.Equal(t, []string{"body.attachmentIds"}, field(t, err))
	// An agent's upload goes on a reply or a note.
	a, err := e.svc.adminUpload(e.ctx, e.agent, tid, adminapi.TicketAttachmentCreate{FileName: "utr.jpg", ContentType: "image/jpeg", SizeBytes: 500})
	require.NoError(t, err)
	require.Contains(t, a.Upload.Url, "/todayz-private/support/admin/")
	d, err := e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "Here is the UTR.", AttachmentIds: &[]string{a.Attachment.Id}})
	require.NoError(t, err)
	require.Len(t, d.Messages[1].Attachments, 1)
	_, err = e.svc.adminMessage(e.ctx, e.agent, tid, "note", adminapi.TicketMessageRequest{Body: "x",
		AttachmentIds: &[]string{e.id().String(), e.id().String(), e.id().String(), e.id().String(), e.id().String(), e.id().String()}})
	require.Equal(t, genproblems.ValidationFailed, code(t, err), "at most five")
	require.Equal(t, 2, e.count(`SELECT count(*) FROM support_attachments WHERE message_id IS NOT NULL`))
}

// SC-SP-08: a business sees only its own tickets; another business's ticket
// is not found, for reading, replying or listing.
func TestBusinessIsolation(t *testing.T) {
	e := newEnv(t)
	tid := e.tid(e.open("Payout missing"))
	_, err := e.svc.getThread(e.ctx, e.other, tid)
	require.Equal(t, genproblems.NotFound, code(t, err))
	_, err = e.svc.replyBiz(e.ctx, e.owner, e.other, tid, bizapi.BizTicketMessageCreate{Body: "Mine now"})
	require.Equal(t, genproblems.NotFound, code(t, err))
	l, err := e.svc.listBiz(e.ctx, e.other, nil, nil)
	require.NoError(t, err)
	require.Empty(t, l.Items)
	_, err = e.svc.getThread(e.ctx, e.biz, e.id())
	require.Equal(t, genproblems.NotFound, code(t, err))
	_, err = e.svc.getAdmin(e.ctx, e.id())
	require.Equal(t, genproblems.NotFound, code(t, err))

	// A customer's ticket is in the inbox but never in a business's list.
	customer := e.user("Asha")
	e.exec(`INSERT INTO support_tickets (id, opener_type, opener_user_id, category, subject, status, priority, sla_started_at, sla_due_at, last_message_at)
		VALUES ($1, 'customer', $2, 'order', 'Pickup not ready', 'open', 'urgent', now(), now(), now())`, ids.UUID(e.id()), ids.UUID(customer))
	l, err = e.svc.listBiz(e.ctx, e.biz, nil, nil)
	require.NoError(t, err)
	require.Len(t, l.Items, 1)
	all, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{})
	require.NoError(t, err)
	require.Len(t, all.Items, 2)
	cust, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Opener: new(adminapi.TicketOpenerType("customer"))})
	require.NoError(t, err)
	require.Len(t, cust.Items, 1)
}

func TestListPaging(t *testing.T) {
	e := newEnv(t)
	for range 3 {
		e.open("Ticket")
		e.clk.Advance(time.Minute)
	}
	lim := 2
	l, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Limit: &lim})
	require.NoError(t, err)
	require.Len(t, l.Items, 2)
	require.NotNil(t, l.NextCursor)
	next, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Limit: &lim, Cursor: l.NextCursor})
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	require.Nil(t, next.NextCursor)
	require.NotEqual(t, l.Items[0].Id, next.Items[0].Id)

	b, err := e.svc.listBiz(e.ctx, e.biz, &lim, nil)
	require.NoError(t, err)
	require.Len(t, b.Items, 2)
	b2, err := e.svc.listBiz(e.ctx, e.biz, &lim, b.NextCursor)
	require.NoError(t, err)
	require.Len(t, b2.Items, 1)
}
