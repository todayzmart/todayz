package support

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	adminapi "github.com/todayz/backend/gen/api/admin-support"
	bizapi "github.com/todayz/backend/gen/api/biz-support"
	meapi "github.com/todayz/backend/gen/api/support"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The lead's review of b45d936 (D-226): abuse limits, the claim-time checks,
// admin uploads bound to their ticket, the reopen audit, account deletion and
// the unclaimed-upload sweep.

func (e *env) bizUp(user ids.ID, name, ct string, size int) bizapi.BizAttachmentUpload {
	e.t.Helper()
	u, err := e.svc.bizUpload(e.ctx, user, e.biz, bizapi.BizAttachmentCreate{FileName: name, ContentType: bizapi.BizAttachmentContentType(ct), SizeBytes: int64(size)})
	require.NoError(e.t, err)
	return u
}

func (e *env) meUp(user ids.ID, name, ct string, size int) meapi.MyTicketAttachmentUpload {
	e.t.Helper()
	u, err := e.svc.meUpload(e.ctx, user, meapi.MyTicketAttachmentCreate{FileName: name, ContentType: meapi.MyTicketAttachmentContentType(ct), SizeBytes: int64(size)})
	require.NoError(e.t, err)
	return u
}

// support_open_tickets_max caps what one person has open or waiting (422);
// support_tickets_per_hour caps openings in a rolling hour (429). Both are
// read at the moment of the request.
func TestOpenLimits(t *testing.T) {
	e := newEnv(t)
	e.setting(settings.SupportOpenTicketsMax, "2")
	first := e.tid(e.open("One"))
	e.open("Two")
	_, err := e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Three", Body: "x", Category: "other"})
	require.Equal(t, genproblems.SupportTicketLimitReached, code(t, err))
	// Customer tickets by the same person count too: the cap is per opener.
	_, err = e.svc.openMe(e.ctx, e.owner, meapi.MyTicketCreate{Category: "other", Subject: "Three", Body: "x"})
	require.Equal(t, genproblems.SupportTicketLimitReached, code(t, err))
	// Someone else is not held back.
	e.openMe(e.user("Asha"), meapi.MyTicketCreate{Category: "other", Subject: "Mine", Body: "x"})

	// A resolved ticket no longer counts.
	_, err = e.svc.closeTicket(e.ctx, e.agent, first, adminapi.TicketCloseRequestOutcomeResolved, "Paid.")
	require.NoError(t, err)
	e.open("Three")

	e.setting(settings.SupportOpenTicketsMax, "50")
	e.setting(settings.SupportTicketsPerHour, "4")
	e.open("Four")
	_, err = e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Five", Body: "x", Category: "other"})
	require.Equal(t, genproblems.RateLimited, code(t, err), "four opened within the hour")
	e.clk.Advance(61 * time.Minute)
	e.open("Five")
	require.Equal(t, 6, e.count(`SELECT count(*) FROM support_tickets`))
}

// support_uploads_per_hour caps upload requests per person in a rolling hour.
func TestUploadLimit(t *testing.T) {
	e := newEnv(t)
	e.setting(settings.SupportUploadsPerHour, "5")
	for range 5 {
		e.bizUp(e.owner, "a.png", "image/png", 10)
	}
	_, err := e.svc.bizUpload(e.ctx, e.owner, e.biz, bizapi.BizAttachmentCreate{FileName: "b.png", ContentType: "image/png", SizeBytes: 10})
	require.Equal(t, genproblems.RateLimited, code(t, err))
	_, err = e.svc.meUpload(e.ctx, e.owner, meapi.MyTicketAttachmentCreate{FileName: "b.png", ContentType: "image/png", SizeBytes: 10})
	require.Equal(t, genproblems.RateLimited, code(t, err), "the same person on the customer side")
	e.bizUp(e.user("Suresh"), "c.png", "image/png", 10)
	e.clk.Advance(61 * time.Minute)
	e.bizUp(e.owner, "d.png", "image/png", 10)
}

// At claim the object must be in the bucket with the declared size and type
// (HEAD), and younger than support_upload_ttl_hours; storage trouble is 503.
func TestClaimChecksTheObject(t *testing.T) {
	e := newEnv(t)
	claim := func(u bizapi.BizAttachmentUpload) error {
		_, err := e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "File", Body: "x", Category: "other", AttachmentIds: &[]string{u.Attachment.Id}})
		return err
	}
	missing := e.bizUp(e.owner, "a.pdf", "application/pdf", 100)
	require.Equal(t, []string{"body.attachmentIds"}, field(t, claim(missing)), "not uploaded")

	bigger := e.bizUp(e.owner, "b.pdf", "application/pdf", 100)
	e.s3.put(t, bigger.Upload.Url, "application/pdf", 101)
	require.Equal(t, []string{"body.attachmentIds"}, field(t, claim(bigger)), "another size")

	other := e.bizUp(e.owner, "c.pdf", "application/pdf", 100)
	e.s3.put(t, other.Upload.Url, "image/png", 100)
	require.Equal(t, []string{"body.attachmentIds"}, field(t, claim(other)), "another type")

	down := e.bizUp(e.owner, "d.pdf", "application/pdf", 100)
	e.s3.put(t, down.Upload.Url, "application/pdf", 100)
	e.s3.failing(t, down.Upload.Url, true)
	require.Equal(t, genproblems.ServiceUnavailable, code(t, claim(down)))
	e.s3.failing(t, down.Upload.Url, false)
	require.NoError(t, claim(down))

	e.setting(settings.SupportUploadTTLHours, "1")
	old := e.bizUp(e.owner, "e.pdf", "application/pdf", 100)
	e.s3.put(t, old.Upload.Url, "application/pdf", 100)
	e.clk.Advance(61 * time.Minute)
	require.Equal(t, []string{"body.attachmentIds"}, field(t, claim(old)), "past the TTL")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_attachments WHERE message_id IS NOT NULL`))
}

// An agent's upload is asked for on one ticket and only a message there can
// claim it; uploads never cross between the agent, business and customer sides.
func TestUploadSidesAndTickets(t *testing.T) {
	e := newEnv(t)
	a := e.tid(e.open("First"))
	b := e.tid(e.open("Second"))
	up, err := e.svc.adminUpload(e.ctx, e.agent, a, adminapi.TicketAttachmentCreate{FileName: "utr.jpg", ContentType: "image/jpeg", SizeBytes: 500})
	require.NoError(t, err)
	e.s3.put(t, up.Upload.Url, "image/jpeg", 500)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_attachments WHERE ticket_id = $1 AND message_id IS NULL`, ids.UUID(a)))

	_, err = e.svc.adminMessage(e.ctx, e.agent, b, "reply", adminapi.TicketMessageRequest{Body: "x", AttachmentIds: &[]string{up.Attachment.Id}})
	require.Equal(t, []string{"body.attachmentIds"}, field(t, err), "asked for on another ticket")
	_, err = e.svc.replyBiz(e.ctx, e.agent, e.biz, a, bizapi.BizTicketMessageCreate{Body: "x", AttachmentIds: &[]string{up.Attachment.Id}})
	require.Error(t, err, "an agent upload is not a business upload")

	biz := e.bizUp(e.owner, "s.pdf", "application/pdf", 100)
	e.s3.put(t, biz.Upload.Url, "application/pdf", 100)
	_, err = e.svc.adminMessage(e.ctx, e.agent, a, "reply", adminapi.TicketMessageRequest{Body: "x", AttachmentIds: &[]string{biz.Attachment.Id}})
	require.Equal(t, []string{"body.attachmentIds"}, field(t, err), "a business upload is not the agent's")

	_, err = e.svc.adminMessage(e.ctx, e.agent, a, "note", adminapi.TicketMessageRequest{Body: "UTR attached.", AttachmentIds: &[]string{up.Attachment.Id}})
	require.NoError(t, err)
}

// An agent's reply on a resolved ticket makes it waiting again: the close
// reason goes and the change is audited.
func TestReplyReopensResolved(t *testing.T) {
	e := newEnv(t)
	tid := e.tid(e.open("Payout missing"))
	_, err := e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeResolved, "Paid on 14 October.")
	require.NoError(t, err)
	d, err := e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "One more thing."})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketStatusWaiting, d.Ticket.Status)
	require.Nil(t, d.CloseReason)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_reopen' AND subject_id = $1 AND actor_id = $2
		AND before->>'status' = 'resolved' AND after->>'status' = 'waiting'`, tid.String(), ids.UUID(e.agent)))

	_, err = e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "And another."})
	require.NoError(t, err)
	_, err = e.svc.adminMessage(e.ctx, e.agent, tid, "note", adminapi.TicketMessageRequest{Body: "A note."})
	require.NoError(t, err)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_reopen'`), "only the resolved one")
}

func (e *env) deleteAccount(user ids.ID) {
	e.t.Helper()
	require.NoError(e.t, e.svc.db.InTx(e.ctx, func(ctx context.Context) error { return e.svc.OnAccountDelete(ctx, user) }))
}

// Account deletion (01 §13): the customer's open, waiting and resolved
// tickets close with reason account_deleted (audited as the system); their
// subjects and the customer's messages become the placeholder; the customer's
// uploads go, rows and objects. Support's replies, other people's tickets and
// the person's business tickets and uploads stay. A second run changes nothing.
func TestAccountDeletion(t *testing.T) {
	e := newEnv(t)
	asha := e.user("Asha")
	file := e.meUp(asha, "bill.jpg", "image/jpeg", 900)
	e.s3.put(t, file.Upload.Url, "image/jpeg", 900)
	open := meTID(t, e.openMe(asha, meapi.MyTicketCreate{Category: "order", Subject: "Atta missing", Body: "My flat is 4B.", AttachmentIds: &[]string{file.Attachment.Id}}))
	waiting := meTID(t, e.openMe(asha, meapi.MyTicketCreate{Category: "app", Subject: "Language", Body: "Call me after six."}))
	_, err := e.svc.adminMessage(e.ctx, e.agent, waiting, "reply", adminapi.TicketMessageRequest{Body: "Which phone do you use?"})
	require.NoError(t, err)
	agentFile, err := e.svc.adminUpload(e.ctx, e.agent, waiting, adminapi.TicketAttachmentCreate{FileName: "steps.png", ContentType: "image/png", SizeBytes: 50})
	require.NoError(t, err)
	e.s3.put(t, agentFile.Upload.Url, "image/png", 50)
	_, err = e.svc.adminMessage(e.ctx, e.agent, waiting, "note", adminapi.TicketMessageRequest{Body: "Steps attached.", AttachmentIds: &[]string{agentFile.Attachment.Id}})
	require.NoError(t, err)
	closed := meTID(t, e.openMe(asha, meapi.MyTicketCreate{Category: "other", Subject: "Old one", Body: "Thanks."}))
	_, err = e.svc.closeTicket(e.ctx, e.agent, closed, adminapi.TicketCloseRequestOutcomeClosed, "Answered.")
	require.NoError(t, err)
	stray := e.meUp(asha, "draft.jpg", "image/jpeg", 10)
	e.s3.put(t, stray.Upload.Url, "image/jpeg", 10)

	// The same person as a business user (the routes check membership): that ticket stays with the business.
	bizFile := e.bizUp(asha, "stmt.pdf", "application/pdf", 100)
	e.s3.put(t, bizFile.Upload.Url, "application/pdf", 100)
	bizTicket, err := e.svc.openBiz(e.ctx, asha, e.biz, bizapi.BizTicketCreate{Subject: "Payout", Body: "Statement attached.", Category: "payouts", AttachmentIds: &[]string{bizFile.Attachment.Id}})
	require.NoError(t, err)
	other := meTID(t, e.openMe(e.user("Ravi"), meapi.MyTicketCreate{Category: "other", Subject: "Mine", Body: "Not Asha's."}))

	e.deleteAccount(asha)

	for _, tid := range []ids.ID{open, waiting} {
		require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND status = 'closed' AND close_reason = 'account_deleted'
			AND subject = $2 AND sla_due_at IS NULL`, ids.UUID(tid), deletedText))
		require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_close' AND actor_kind = 'system' AND subject_id = $1
			AND after->>'reason' = 'account_deleted'`, tid.String()))
	}
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND close_reason = 'Answered.' AND subject = $2`, ids.UUID(closed), deletedText))
	require.Zero(t, e.count(`SELECT count(*) FROM support_messages m JOIN support_tickets t ON t.id = m.ticket_id
		WHERE t.opener_type = 'customer' AND t.opener_user_id = $1 AND m.author_type = 'customer' AND m.body <> $2`, ids.UUID(asha), deletedText))
	require.Equal(t, 2, e.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1 AND author_type = 'admin' AND body <> $2`, ids.UUID(waiting), deletedText),
		"support's reply and note stay")

	require.False(t, e.s3.has(t, file.Upload.Url))
	require.False(t, e.s3.has(t, stray.Upload.Url))
	require.Zero(t, e.count(`SELECT count(*) FROM support_attachments WHERE uploaded_by = $1 AND business_id IS NULL`, ids.UUID(asha)))
	require.True(t, e.s3.has(t, agentFile.Upload.Url), "support's file stays")
	require.True(t, e.s3.has(t, bizFile.Upload.Url), "the business's file stays")
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND status = 'open' AND subject = 'Payout'`, ids.UUID(e.tid(bizTicket))))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND status = 'open' AND subject = 'Mine'`, ids.UUID(other)))

	audits := e.count(`SELECT count(*) FROM audit_log`)
	deletes := e.s3.deletes
	e.deleteAccount(asha)
	require.Equal(t, audits, e.count(`SELECT count(*) FROM audit_log`), "idempotent")
	require.Equal(t, deletes, e.s3.deletes)
}

// A storage failure rolls the deletion back, so the retry finishes it.
func TestAccountDeletionStorageDown(t *testing.T) {
	e := newEnv(t)
	asha := e.user("Asha")
	file := e.meUp(asha, "bill.jpg", "image/jpeg", 900)
	e.s3.put(t, file.Upload.Url, "image/jpeg", 900)
	tid := meTID(t, e.openMe(asha, meapi.MyTicketCreate{Category: "other", Subject: "Bill", Body: "x", AttachmentIds: &[]string{file.Attachment.Id}}))
	e.s3.failing(t, file.Upload.Url, true)
	err := e.svc.db.InTx(e.ctx, func(ctx context.Context) error { return e.svc.OnAccountDelete(ctx, asha) })
	require.Equal(t, genproblems.ServiceUnavailable, code(t, err))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND status = 'open' AND subject = 'Bill'`, ids.UUID(tid)))
	e.s3.failing(t, file.Upload.Url, false)
	e.deleteAccount(asha)
	require.False(t, e.s3.has(t, file.Upload.Url))
}

// The sweep deletes uploads no message claimed within support_upload_ttl_hours,
// object first; one whose object it cannot delete waits for the next run.
func TestSweepUploads(t *testing.T) {
	e := newEnv(t)
	e.setting(settings.SupportUploadTTLHours, "1")
	stale := e.bizUp(e.owner, "a.pdf", "application/pdf", 100)
	e.s3.put(t, stale.Upload.Url, "application/pdf", 100)
	stuck := e.bizUp(e.owner, "b.pdf", "application/pdf", 100)
	e.s3.put(t, stuck.Upload.Url, "application/pdf", 100)
	e.s3.failing(t, stuck.Upload.Url, true)
	e.meUp(e.owner, "c.jpg", "image/jpeg", 10) // asked for, never uploaded
	used := e.bizUp(e.owner, "d.pdf", "application/pdf", 100)
	e.s3.put(t, used.Upload.Url, "application/pdf", 100)
	_, err := e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Used", Body: "x", Category: "other", AttachmentIds: &[]string{used.Attachment.Id}})
	require.NoError(t, err)
	e.clk.Advance(61 * time.Minute)
	fresh := e.bizUp(e.owner, "e.pdf", "application/pdf", 100)
	e.s3.put(t, fresh.Upload.Url, "application/pdf", 100)

	res, err := e.svc.SweepUploads(e.ctx)
	require.NoError(t, err)
	require.Equal(t, SweepResult{Deleted: 2, Failed: 1}, res)
	require.False(t, e.s3.has(t, stale.Upload.Url))
	require.True(t, e.s3.has(t, stuck.Upload.Url))
	require.True(t, e.s3.has(t, used.Upload.Url))
	require.True(t, e.s3.has(t, fresh.Upload.Url))
	require.Equal(t, 3, e.count(`SELECT count(*) FROM support_attachments`))

	e.s3.failing(t, stuck.Upload.Url, false)
	res, err = e.svc.SweepUploads(e.ctx)
	require.NoError(t, err)
	require.Equal(t, SweepResult{Deleted: 1}, res)
	require.Equal(t, 2, e.count(`SELECT count(*) FROM support_attachments`))
}

// The inbox counts each status, filtered like the list, and each count is
// an index range of ix_support_tickets_counts.
func TestCountTickets(t *testing.T) {
	e := newEnv(t)
	resolved := e.tid(e.open("Resolved"))
	waiting := e.tid(e.open("Waiting"))
	closed := e.tid(e.open("Closed"))
	e.open("Open")
	e.openMe(e.user("Asha"), meapi.MyTicketCreate{Category: "other", Subject: "Customer", Body: "x"})
	_, err := e.svc.closeTicket(e.ctx, e.agent, resolved, adminapi.TicketCloseRequestOutcomeResolved, "Done.")
	require.NoError(t, err)
	_, err = e.svc.adminMessage(e.ctx, e.agent, waiting, "reply", adminapi.TicketMessageRequest{Body: "Which week?"})
	require.NoError(t, err)
	_, err = e.svc.closeTicket(e.ctx, e.agent, closed, adminapi.TicketCloseRequestOutcomeClosed, "Duplicate.")
	require.NoError(t, err)
	e.clk.Advance(25 * time.Hour)

	l, err := e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketCounts{Open: 2, Waiting: 1, Resolved: 1, Closed: 1, Overdue: 2}, l.Counts)
	l, err = e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Opener: new(adminapi.TicketOpenerType("business"))})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketCounts{Open: 1, Waiting: 1, Resolved: 1, Closed: 1, Overdue: 1}, l.Counts)
	l, err = e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Assignee: new(adminapi.Me)})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketCounts{Waiting: 1}, l.Counts, "the reply assigned the agent")
	l, err = e.svc.listAdmin(e.ctx, e.agent, adminapi.ListAdminTicketsParams{Assignee: new(adminapi.Unassigned)})
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketCounts{Open: 2, Resolved: 1, Closed: 1, Overdue: 2}, l.Counts)

	// With sequential scans priced out, a status count reads the index only.
	tx, err := e.tdb.OwnerPool.BeginTx(e.ctx, pgx.TxOptions{})
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(e.ctx) }()
	_, err = tx.Exec(e.ctx, `SET LOCAL enable_seqscan = off`)
	require.NoError(t, err)
	rows, err := tx.Query(e.ctx, `EXPLAIN (FORMAT TEXT) SELECT count(*) FROM support_tickets c
		WHERE c.status = 'closed' AND ('' = '' OR c.assignee_id IS NULL) AND ('' = '' OR c.opener_type = 'business')`)
	require.NoError(t, err)
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	require.Contains(t, strings.Join(lines, "\n"), "ix_support_tickets_counts")
}
