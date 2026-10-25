package support

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	adminapi "github.com/todayz/backend/gen/api/admin-support"
	bizapi "github.com/todayz/backend/gen/api/biz-support"
	meapi "github.com/todayz/backend/gen/api/support"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The customer's tickets (C-19) and the priority change (D-226).

func (e *env) openMe(customer ids.ID, body meapi.MyTicketCreate) meapi.MyTicketThread {
	e.t.Helper()
	th, err := e.svc.openMe(e.ctx, customer, body)
	require.NoError(e.t, err)
	return th
}

func meTID(t *testing.T, th meapi.MyTicketThread) ids.ID {
	t.Helper()
	id, err := ids.Parse(th.Ticket.Id)
	require.NoError(t, err)
	return id
}

// SC-SP-01 (customer): a customer opens a ticket at normal priority, due
// support_sla_normal_min from now; an order on it must be their own and
// brings its store.
func TestOpenCustomerTicket(t *testing.T) {
	e := newEnv(t)
	asha := e.user("Asha")
	now := e.clk.Now()
	th := e.openMe(asha, meapi.MyTicketCreate{Category: "app", Subject: "Cannot change language", Body: "The app stays in English."})
	require.Equal(t, meapi.MyTicketStatus("open"), th.Ticket.Status)
	require.Nil(t, th.Ticket.StoreName)
	require.Len(t, th.Messages, 1)
	require.Equal(t, meapi.Customer, th.Messages[0].From)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE opener_type = 'customer' AND business_id IS NULL
		AND priority = 'normal' AND sla_started_at = $1 AND sla_due_at = $2`, now, now.Add(24*time.Hour)))

	ravi := e.user("Ravi")
	theirs := e.tdb.InsertOrder(t, e.id(), ravi, e.store)
	_, err := e.svc.openMe(e.ctx, asha, meapi.MyTicketCreate{Category: "order", Subject: "Not mine", Body: "x", OrderId: new(theirs.String())})
	require.Equal(t, []string{"body.orderId"}, field(t, err))

	own := e.tdb.InsertOrder(t, e.id(), asha, e.store)
	th = e.openMe(asha, meapi.MyTicketCreate{Category: "order", Subject: "Atta missing", Body: "Not in the bag.", OrderId: new(own.String())})
	require.Equal(t, "Lakshmi Traders", deref(th.Ticket.StoreName), "the order's store is filled in")
	require.Equal(t, own.String(), deref(th.Ticket.OrderId))

	// The inbox sees it as a customer ticket on that store; the business does not.
	d, err := e.svc.getAdmin(e.ctx, meTID(t, th))
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketOpenerType("customer"), d.Ticket.OpenerType)
	require.Equal(t, e.store.String(), deref(d.Ticket.StoreId))
	l, err := e.svc.listBiz(e.ctx, e.biz, nil, nil)
	require.NoError(t, err)
	require.Empty(t, l.Items)
}

// SC-SP-03…05 (customer): support's reply shows as "support" with no name and
// stops the timer; the customer's reply reopens it; notes never show; a
// closed ticket takes no reply.
func TestCustomerThread(t *testing.T) {
	e := newEnv(t)
	asha := e.user("Asha")
	tid := meTID(t, e.openMe(asha, meapi.MyTicketCreate{Category: "credits", Subject: "Credits missing", Body: "My cashback did not come."}))

	_, err := e.svc.adminMessage(e.ctx, e.agent, tid, "note", adminapi.TicketMessageRequest{Body: "Ledger shows a reversal."})
	require.NoError(t, err)
	_, err = e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "Which order was it?"})
	require.NoError(t, err)

	th, err := e.svc.getMeThread(e.ctx, asha, tid)
	require.NoError(t, err)
	require.Equal(t, meapi.MyTicketStatus("waiting"), th.Ticket.Status)
	require.Len(t, th.Messages, 2, "the note is not in the customer's thread")
	require.Equal(t, meapi.Support, th.Messages[1].From)
	for _, m := range th.Messages {
		require.NotContains(t, m.Body, "reversal")
	}

	e.clk.Advance(time.Hour)
	th, err = e.svc.replyMe(e.ctx, asha, tid, meapi.MyTicketMessageCreate{Body: "TZ-4821."})
	require.NoError(t, err)
	require.Equal(t, meapi.MyTicketStatus("open"), th.Ticket.Status)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND sla_started_at = $2 AND sla_due_at = $3`,
		ids.UUID(tid), e.clk.Now(), e.clk.Now().Add(24*time.Hour)))

	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeClosed, "Credited.")
	require.NoError(t, err)
	_, err = e.svc.replyMe(e.ctx, asha, tid, meapi.MyTicketMessageCreate{Body: "Thanks"})
	require.Equal(t, genproblems.InvalidTransition, code(t, err))
}

// SC-SP-08 (customer): a customer sees only tickets they opened as a
// customer: not another customer's, and not a business ticket they opened as
// an owner.
func TestCustomerIsolation(t *testing.T) {
	e := newEnv(t)
	asha, ravi := e.user("Asha"), e.user("Ravi")
	tid := meTID(t, e.openMe(asha, meapi.MyTicketCreate{Category: "account", Subject: "Change my number", Body: "New SIM."}))

	_, err := e.svc.getMeThread(e.ctx, ravi, tid)
	require.Equal(t, genproblems.NotFound, code(t, err))
	_, err = e.svc.replyMe(e.ctx, ravi, tid, meapi.MyTicketMessageCreate{Body: "Mine now"})
	require.Equal(t, genproblems.NotFound, code(t, err))
	l, err := e.svc.listMe(e.ctx, ravi, nil, nil)
	require.NoError(t, err)
	require.Empty(t, l.Items)

	// The owner's business ticket is not in the owner's customer list.
	biz := e.tid(e.open("Payout missing"))
	l, err = e.svc.listMe(e.ctx, e.owner, nil, nil)
	require.NoError(t, err)
	require.Empty(t, l.Items)
	_, err = e.svc.getMeThread(e.ctx, e.owner, biz)
	require.Equal(t, genproblems.NotFound, code(t, err))
	_, err = e.svc.replyMe(e.ctx, e.owner, biz, meapi.MyTicketMessageCreate{Body: "x"})
	require.Equal(t, genproblems.NotFound, code(t, err))

	l, err = e.svc.listMe(e.ctx, asha, nil, nil)
	require.NoError(t, err)
	require.Len(t, l.Items, 1)
}

// SC-SP-07 (customer): a customer's upload goes under support/customers/ and
// only on that customer's own message; a business upload by the same person
// does not go on their customer ticket.
func TestCustomerAttachments(t *testing.T) {
	e := newEnv(t)
	u, err := e.svc.meUpload(e.ctx, e.owner, meapi.MyTicketAttachmentCreate{FileName: "bill.jpg", ContentType: "image/jpeg", SizeBytes: 900})
	require.NoError(t, err)
	require.Contains(t, u.Upload.Url, "/todayz-private/support/customers/")
	require.Equal(t, "900", u.Upload.Headers.ContentLength)

	b, err := e.svc.bizUpload(e.ctx, e.owner, e.biz, bizapi.BizAttachmentCreate{FileName: "stmt.pdf", ContentType: "application/pdf", SizeBytes: 100})
	require.NoError(t, err)
	_, err = e.svc.openMe(e.ctx, e.owner, meapi.MyTicketCreate{Category: "other", Subject: "Mixed", Body: "x", AttachmentIds: &[]string{b.Attachment.Id}})
	require.Equal(t, []string{"body.attachmentIds"}, field(t, err), "a business upload is not a customer's")
	_, err = e.svc.openBiz(e.ctx, e.owner, e.biz, bizapi.BizTicketCreate{Subject: "Mixed", Body: "x", Category: "other", AttachmentIds: &[]string{u.Attachment.Id}})
	require.Equal(t, []string{"body.attachmentIds"}, field(t, err), "a customer upload is not the business's")

	th := e.openMe(e.owner, meapi.MyTicketCreate{Category: "other", Subject: "Bill", Body: "Attached.", AttachmentIds: &[]string{u.Attachment.Id}})
	require.Len(t, th.Messages[0].Attachments, 1)
	require.NotNil(t, th.Messages[0].Attachments[0].Url)
}

// D-226: support changes the priority (audited); an open ticket's due time
// becomes its timer's start plus the new priority's minutes; a waiting
// ticket keeps no timer; a closed one refuses.
func TestSetPriority(t *testing.T) {
	e := newEnv(t)
	start := e.clk.Now()
	tid := e.tid(e.open("Payout missing"))
	e.clk.Advance(30 * time.Minute)

	d, err := e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("urgent"))
	require.NoError(t, err)
	require.Equal(t, adminapi.TicketPriority("urgent"), d.Ticket.Priority)
	require.Equal(t, start.Add(time.Hour), *d.Ticket.SlaDueAt, "from when the timer started")
	audits := func() int {
		return e.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_priority' AND subject_id = $1 AND actor_id = $2`, tid.String(), ids.UUID(e.agent))
	}
	require.Equal(t, 1, audits())
	require.Equal(t, 1, e.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_priority' AND before->>'priority' = 'normal' AND after->>'priority' = 'urgent'`))

	_, err = e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("urgent"))
	require.NoError(t, err)
	require.Equal(t, 1, audits(), "the same priority changes nothing")

	// Past due at once when the new SLA is shorter than the time already gone.
	e.setting(settings.SupportSlaHighMin, "15")
	d, err = e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("high"))
	require.NoError(t, err)
	require.Equal(t, start.Add(15*time.Minute), *d.Ticket.SlaDueAt)
	require.True(t, d.Ticket.Overdue)

	_, err = e.svc.adminMessage(e.ctx, e.agent, tid, "reply", adminapi.TicketMessageRequest{Body: "Checking."})
	require.NoError(t, err)
	d, err = e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("normal"))
	require.NoError(t, err)
	require.Nil(t, d.Ticket.SlaDueAt, "a waiting ticket has no timer")

	// The opener's next reply starts the timer at the ticket's priority.
	e.clk.Advance(time.Hour)
	_, err = e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("urgent"))
	require.NoError(t, err)
	_, err = e.svc.replyBiz(e.ctx, e.owner, e.biz, tid, bizapi.BizTicketMessageCreate{Body: "The week of 12 October."})
	require.NoError(t, err)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND sla_due_at = $2`, ids.UUID(tid), e.clk.Now().Add(time.Hour)))

	_, err = e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("rush"))
	require.Equal(t, []string{"body.priority"}, field(t, err))
	_, err = e.svc.closeTicket(e.ctx, e.agent, tid, adminapi.TicketCloseRequestOutcomeClosed, "Done.")
	require.NoError(t, err)
	_, err = e.svc.setPriority(e.ctx, e.agent, tid, adminapi.TicketPriority("high"))
	require.Equal(t, genproblems.InvalidTransition, code(t, err))
}
