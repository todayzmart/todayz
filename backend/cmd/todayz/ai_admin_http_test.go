package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// W3-13 (AI-A1…A3, D-233) through the router and the real policies: each admin assistant is the
// common 404 while its flag is off, takes its screen's check, shows a draft to its creator only and
// applies only what the admin confirms, through the screen's own writer.

const (
	catalogueDrafts = "/v1/admin/ai/catalogue-cleanup/drafts"
	triageDrafts    = "/v1/admin/ai/ticket-triage/drafts"
	riskDrafts      = "/v1/admin/ai/risk-summary/drafts"
)

// adminDraft creates a draft as tok and returns its id and the ready draft.
func (o *retailShop) adminDraft(tok, path string, body map[string]any) (string, resp) {
	o.t.Helper()
	r := o.a.call(tok, http.MethodPost, path, body, idem(o.a))
	require.Equal(o.t, http.StatusAccepted, r.code, r.raw)
	id := r.body["draftId"].(string)
	g := o.a.call(tok, http.MethodGet, path+"/"+id, nil, nil)
	require.Equal(o.t, http.StatusOK, g.code, g.raw)
	require.Equal(o.t, "ready", g.body["status"], g.raw)
	return id, g
}

// someoneElses hands a draft to another user, so its creator is no longer the caller.
func (o *retailShop) someoneElses(draft string) {
	o.t.Helper()
	o.a.exec(`UPDATE ai_drafts SET actor_user_id = (SELECT id FROM users WHERE id <> actor_user_id ORDER BY id LIMIT 1) WHERE id = $1`,
		uuidOf(o.t, draft))
}

func TestAdminAICatalogueHTTP(t *testing.T) {
	o := newRetailShop(t, false)
	desk := newAdminDesk(o.orderShop, "catalogue")
	tok := desk.token()
	a := o.a
	staples := ulidOf(t, "0190c47e-0000-7000-8000-000000000101") // the seeded staples category (10501)
	pending := ids.New(a.clk.Now()).String()
	a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, status, created_by_store_id)
	  VALUES ($1, 'amul butter salted', $2, 1, 'kg', 'pending_review', $3)`, uuidOf(t, pending), uuidOf(t, staples), uuidOf(t, o.sid))
	// The category lacks Tamil, so its names are drafted too.
	a.exec(`UPDATE categories SET names = names - 'ta' WHERE id = $1`, uuidOf(t, staples))
	o.llm.reply("catalogue_cleanup", map[string]any{
		"name": "Amul Butter Salted", "brandName": "Amul", "variant": nil, "packSize": 1, "unit": "kg",
		"categoryRef": "c1", "duplicateRef": "p1", "duplicateReason": "Same brand and same butter.",
		"categoryNames": map[string]any{"en": "Staples", "hi": "अनाज", "kn": "ಧಾನ್ಯ", "te": "ధాన్యాలు", "ta": "தானியம்"},
	})
	create := map[string]any{"pendingId": pending}

	r := a.call(tok, http.MethodPost, catalogueDrafts, create, idem(a))
	require.Equal(t, http.StatusNotFound, r.code, "flag off: %s", r.raw)
	require.Zero(t, o.llm.count())
	o.setting("ai_catalogue_enabled", "true")
	r = a.call(tok, http.MethodPost, catalogueDrafts, map[string]any{"pendingId": ids.New(a.clk.Now()).String()}, idem(a))
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Contains(t, fieldsOf(r), "body.pendingId")

	id, g := o.adminDraft(tok, catalogueDrafts, create)
	norm := g.body["normalise"].(map[string]any)
	assert.Equal(t, "Amul Butter Salted", norm["name"])
	assert.Equal(t, false, norm["canApply"], "catalogue reviews; editing needs admin.catalogue.manage")
	dup := g.body["duplicate"].(map[string]any)
	assert.Equal(t, true, dup["canApply"])
	assert.Equal(t, "Same brand and same butter.", dup["reason"])
	names := g.body["categoryNames"].(map[string]any)["names"].(map[string]any)
	assert.Equal(t, "தானியம்", names["ta"])
	assert.NotEqual(t, "Staples", names["en"], "a language the category has keeps its name")

	line := func(id, outcome string) map[string]any { return map[string]any{"lineId": id, "outcome": outcome} }

	// Normalise and merge together is refused before anything is written.
	c := o.confirm(tok, catalogueDrafts, id, line("l1", "accepted"), line("l2", "accepted"), line("l3", "discarded"))
	require.Equal(t, http.StatusUnprocessableEntity, c.code, c.raw)
	require.Contains(t, fieldsOf(c), "body.lines")
	// An edited merge must pick one of the draft's own look-alikes.
	c = o.confirm(tok, catalogueDrafts, id, line("l1", "discarded"),
		map[string]any{"lineId": "l2", "outcome": "edited", "value": map[string]any{"productId": pending}}, line("l3", "discarded"))
	require.Equal(t, http.StatusUnprocessableEntity, c.code, c.raw)
	require.Contains(t, fieldsOf(c), "body.lines[1].value.productId")

	// Another admin's draft is the common 404.
	other, _ := o.adminDraft(tok, catalogueDrafts, create)
	o.someoneElses(other)
	r = a.call(tok, http.MethodGet, catalogueDrafts+"/"+other, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	c = o.confirm(tok, catalogueDrafts, other, line("l2", "accepted"))
	require.Equal(t, http.StatusNotFound, c.code, c.raw)

	// The reviewer merges through A-08's writer, audited; the names need admin.catalogue.manage,
	// so that line is not_permitted and the draft stays open.
	c = o.confirm(tok, catalogueDrafts, id, line("l1", "discarded"), line("l2", "accepted"), line("l3", "accepted"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, map[string]string{"l1": "discarded", "l2": "accepted", "l3": "not_permitted"}, outcomesOf(c))
	assert.Equal(t, "ready", c.body["status"])
	require.Zero(t, a.count(`SELECT count(*) FROM products WHERE id = $1 AND status = 'pending_review'`, uuidOf(t, pending)))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE subject_id = $1 AND action LIKE 'catalogue.%'`, pending))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM categories WHERE id = $1 AND NOT names ? 'ta'`, uuidOf(t, staples)))

	// An admin may name the category, but the product has left the queue: 409; discarding closes it.
	a.exec(`UPDATE admin_users SET roles = ARRAY['admin']`)
	tok = desk.token()
	c = o.confirm(tok, catalogueDrafts, id, line("l3", "accepted"))
	require.Equal(t, http.StatusConflict, c.code, c.raw)
	require.Equal(t, "invalid_transition", problemCode(c))
	c = o.confirm(tok, catalogueDrafts, id, line("l3", "discarded"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, "confirmed", c.body["status"])

	// An admin edits a fresh pending product and names its category, both through A-08's writers.
	fresh := ids.New(a.clk.Now()).String()
	a.exec(`INSERT INTO products (id, name, category_id, pack_size, unit, status, created_by_store_id)
	  VALUES ($1, 'amul butter salted', $2, 1, 'kg', 'pending_review', $3)`, uuidOf(t, fresh), uuidOf(t, staples), uuidOf(t, o.sid))
	id, g = o.adminDraft(tok, catalogueDrafts, map[string]any{"pendingId": fresh})
	assert.Equal(t, true, g.body["normalise"].(map[string]any)["canApply"])
	c = o.confirm(tok, catalogueDrafts, id,
		map[string]any{"lineId": "l1", "outcome": "edited", "value": map[string]any{"name": "Amul Butter Salted", "categoryId": staples}},
		line("l2", "discarded"), line("l3", "accepted"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, map[string]string{"l1": "edited", "l2": "discarded", "l3": "accepted"}, outcomesOf(c))
	assert.Equal(t, "confirmed", c.body["status"])
	require.Equal(t, 1, a.count(`SELECT count(*) FROM products WHERE id = $1 AND name = 'Amul Butter Salted' AND status = 'pending_review'`, uuidOf(t, fresh)))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM categories WHERE id = $1 AND names ->> 'ta' = 'தானியம்'`, uuidOf(t, staples)))

	// support has no A-08 queue: the route itself is 403.
	a.exec(`UPDATE admin_users SET roles = ARRAY['support']`)
	tok = desk.token()
	r = a.call(tok, http.MethodPost, catalogueDrafts, create, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}

func TestAdminAITriageHTTP(t *testing.T) {
	o := newRetailShop(t, false)
	desk := newAdminDesk(o.orderShop, "support")
	tok := desk.token()
	a := o.a
	r := a.call(o.owner, http.MethodPost, o.B+"/support/tickets",
		map[string]any{"subject": "Payout missing", "body": "Our payout has not arrived this week.", "category": "other"}, key(a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	ticket := r.body["ticket"].(map[string]any)["id"].(string)
	reply := "Sorry for the wait. We are checking your payout and will write back today."
	o.llm.reply("ticket_triage", map[string]any{
		"category": "payouts", "priority": "high", "orderRef": "none", "reason": "The shop asks about a payout.",
		"summary": nil, "reply": reply,
	})
	create := map[string]any{"ticketId": ticket}

	r = a.call(tok, http.MethodPost, triageDrafts, create, idem(a))
	require.Equal(t, http.StatusNotFound, r.code, "flag off: %s", r.raw)
	o.setting("ai_triage_enabled", "true")

	id, g := o.adminDraft(tok, triageDrafts, create)
	cl := g.body["classify"].(map[string]any)
	assert.Equal(t, "payouts", cl["category"])
	assert.Nil(t, cl["orderId"])
	assert.Nil(t, g.body["summary"], "a short thread has no summary")
	assert.Equal(t, reply, g.body["reply"].(map[string]any)["text"])

	// An order that is not the opener's is refused, and nothing is written.
	c := o.confirm(tok, triageDrafts, id, map[string]any{"lineId": "l1", "outcome": "edited",
		"value": map[string]any{"orderId": ids.New(a.clk.Now()).String()}})
	require.Equal(t, http.StatusUnprocessableEntity, c.code, c.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND category = 'other'`, uuidOf(t, ticket)))

	// The agent edits the reply; classify is applied as drafted, both audited as A-14 writes.
	mine := "Sorry for the wait. Your payout is being checked now."
	c = o.confirm(tok, triageDrafts, id,
		map[string]any{"lineId": "l1", "outcome": "accepted"},
		map[string]any{"lineId": "l3", "outcome": "edited", "value": map[string]any{"text": mine}})
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, map[string]string{"l1": "accepted", "l3": "edited"}, outcomesOf(c))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND category = 'payouts' AND priority = 'high'`, uuidOf(t, ticket)))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1 AND author_type = 'admin' AND kind = 'reply' AND body = $2`,
		uuidOf(t, ticket), mine))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_triage' AND subject_id = $1`, ticket))

	// Another agent's draft is the common 404; catalogue has no A-14 inbox (403).
	other, _ := o.adminDraft(tok, triageDrafts, create)
	o.someoneElses(other)
	r = a.call(tok, http.MethodGet, triageDrafts+"/"+other, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['catalogue']`)
	tok = desk.token()
	r = a.call(tok, http.MethodPost, triageDrafts, create, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}

func TestAdminAIRiskHTTP(t *testing.T) {
	o := newRetailShop(t, false)
	desk := newAdminDesk(o.orderShop, "support")
	tok := desk.token()
	a := o.a
	at := a.clk.Now()
	store := o.S[strings.LastIndex(o.S, "/")+1:]
	flag := ids.New(at).String()
	a.exec(`INSERT INTO risk_flags (id, subject_type, subject_id, rule, score, created_at, updated_at)
	  VALUES ($1, 'store', $2, 'pos.reversal_ratio', 60, $3, $3)`, uuidOf(t, flag), uuidOf(t, store), at)
	create := map[string]any{"flagId": flag}
	o.llm.reply("risk_summary", map[string]any{"text": "The store scored 60 on the reversal ratio rule."})

	r := a.call(tok, http.MethodPost, riskDrafts, create, idem(a))
	require.Equal(t, http.StatusNotFound, r.code, "flag off: %s", r.raw)
	o.setting("ai_riskexplain_enabled", "true")

	id, g := o.adminDraft(tok, riskDrafts, create)
	lines := g.body["lines"].([]any)
	require.Len(t, lines, 1)
	assert.Equal(t, "The store scored 60 on the reversal ratio rule.", lines[0].(map[string]any)["text"])

	// An edit with a number the flag does not carry is refused (D-230).
	c := o.confirm(tok, riskDrafts, id, map[string]any{"lineId": "l1", "outcome": "edited",
		"value": map[string]any{"text": "The store scored 95 on the reversal ratio rule."}})
	require.Equal(t, http.StatusUnprocessableEntity, c.code, c.raw)
	require.Contains(t, fieldsOf(c), "body.lines[0].value.text")
	c = o.confirm(tok, riskDrafts, id, map[string]any{"lineId": "l1", "outcome": "accepted"})
	require.Equal(t, http.StatusOK, c.code, c.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'risk.flag_summary' AND subject_id = $1`, flag))
	require.Equal(t, 1, a.count(`SELECT count(*) FROM risk_flags WHERE id = $1 AND status = 'open'`, uuidOf(t, flag)),
		"a summary never clears or confirms a flag")

	other, _ := o.adminDraft(tok, riskDrafts, create)
	o.someoneElses(other)
	r = a.call(tok, http.MethodGet, riskDrafts+"/"+other, nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	a.exec(`UPDATE admin_users SET roles = ARRAY['catalogue']`)
	tok = desk.token()
	r = a.call(tok, http.MethodPost, riskDrafts, create, idem(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}
