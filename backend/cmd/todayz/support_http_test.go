package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/health"
	"github.com/todayz/backend/internal/platform/idempotency"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/queue"
	"github.com/todayz/backend/internal/platform/testclock"
)

// Support over HTTP (W3-08, the lead's review of b45d936): who may call
// which route, idempotent create and reply, races, a settings change before a
// priority change, uploads that cross sides, and account deletion.

// supportBucket is the private bucket for these tests: it keeps the size and
// type of what was "uploaded" (put) and answers the API's HEAD and DELETE.
type supportBucket struct {
	srv     *httptest.Server
	mu      sync.Mutex
	objects map[string][2]string // path → content type, size
}

func newSupportBucket(t *testing.T) *supportBucket {
	t.Helper()
	b := &supportBucket{objects: map[string][2]string{}}
	b.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.mu.Lock()
		defer b.mu.Unlock()
		o, ok := b.objects[r.URL.Path]
		switch {
		case r.Method == http.MethodDelete:
			delete(b.objects, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		case !ok:
			w.WriteHeader(http.StatusNotFound)
		default:
			w.Header().Set("Content-Type", o[0])
			w.Header().Set("Content-Length", o[1])
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(b.srv.Close)
	return b
}

func (b *supportBucket) put(t *testing.T, upload resp) {
	t.Helper()
	target := upload.body["upload"].(map[string]any)
	u, err := url.Parse(target["url"].(string))
	require.NoError(t, err)
	h := target["headers"].(map[string]any)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objects[u.Path] = [2]string{h["Content-Type"].(string), h["Content-Length"].(string)}
}

func (b *supportBucket) has(t *testing.T, upload resp) bool {
	t.Helper()
	u, err := url.Parse(upload.body["upload"].(map[string]any)["url"].(string))
	require.NoError(t, err)
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.objects[u.Path]
	return ok
}

// supportShop is the orders shop with the private bucket configured: the
// router is rebuilt over the same database with Storage pointing at the fake.
type supportShop struct {
	*orderShop
	bucket *supportBucket
	desk   *adminDesk
	BY     string // the other owner's business
}

func newSupportShop(t *testing.T) *supportShop {
	t.Helper()
	o := newOrderShop(t)
	b := newSupportBucket(t)
	cfg := *o.cfg
	cfg.Storage.Endpoint = b.srv.URL
	cfg.Storage.AccessKeyID = config.NewSecret([]byte("test-access"))
	cfg.Storage.SecretAccessKey = config.NewSecret([]byte("test-secret-value"))
	cfg.Storage.BucketPrivate = "todayz-private"
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	q, err := queue.NewInsertClient(o.a.tdb.Pool, queue.Options{Log: log})
	require.NoError(t, err)
	h, err := buildRouter(deps{
		Config: &cfg, Log: log, Clock: o.a.clk,
		DB: db.New(o.a.tdb.Pool), Ready: &health.Readiness{}, Settings: o.settings, Queue: q,
		TestClock: testclock.New(clock.NewOffset(nil), nopStore{}, log),
	}, idempotency.NewMemoryStore(), contractValidator(t))
	require.NoError(t, err)
	o.a.h = h
	return &supportShop{orderShop: o, bucket: b, desk: newAdminDesk(o, "support"), BY: strings.Split(o.SY, "/stores/")[0]}
}

func key(a *bizAPI) map[string]string {
	return map[string]string{"Idempotency-Key": ids.New(a.clk.Now()).String()}
}

// bizTicket opens a business ticket as tok and returns its id.
func (s *supportShop) bizTicket(tok, subject string, files ...string) string {
	s.t.Helper()
	body := map[string]any{"subject": subject, "body": "Our payout has not arrived.", "category": "payouts"}
	if len(files) > 0 {
		body["attachmentIds"] = files
	}
	r := s.a.call(tok, http.MethodPost, s.B+"/support/tickets", body, key(s.a))
	require.Equal(s.t, http.StatusCreated, r.code, r.raw)
	return r.body["ticket"].(map[string]any)["id"].(string)
}

func (s *supportShop) upload(tok, path string) resp {
	s.t.Helper()
	r := s.a.call(tok, http.MethodPost, path, map[string]any{"fileName": "statement.pdf", "contentType": "application/pdf", "sizeBytes": 2048}, nil)
	require.Equal(s.t, http.StatusCreated, r.code, r.raw)
	return r
}

func attID(r resp) string { return r.body["attachment"].(map[string]any)["id"].(string) }

func TestSupportHTTPAuthz(t *testing.T) {
	s := newSupportShop(t)
	a := s.a
	for _, c := range []struct {
		name, tok, method, path string
		want                    int
	}{
		{"anonymous, business", "", http.MethodGet, s.B + "/support/tickets", http.StatusUnauthorized},
		{"anonymous, customer", "", http.MethodGet, "/v1/me/support/tickets", http.StatusUnauthorized},
		{"anonymous, inbox", "", http.MethodGet, "/v1/admin/tickets", http.StatusUnauthorized},
		{"cashier lists", s.cashier, http.MethodGet, s.B + "/support/tickets", http.StatusForbidden},
		{"cashier uploads", s.cashier, http.MethodPost, s.B + "/support/attachments", http.StatusForbidden},
		{"owner of A on B", s.owner, http.MethodGet, s.BY + "/support/tickets", http.StatusForbidden},
		{"owner of A uploads on B", s.owner, http.MethodPost, s.BY + "/support/attachments", http.StatusForbidden},
		{"manager lists", s.manager, http.MethodGet, s.B + "/support/tickets", http.StatusOK},
		{"customer on the inbox", s.cust, http.MethodGet, "/v1/admin/tickets", http.StatusForbidden},
	} {
		var body any
		if c.method == http.MethodPost {
			body = map[string]any{"fileName": "a.png", "contentType": "image/png", "sizeBytes": 10}
		}
		r := a.call(c.tok, c.method, c.path, body, nil)
		require.Equal(t, c.want, r.code, "%s: %s", c.name, r.raw)
	}
	r := a.call(s.cashier, http.MethodPost, s.B+"/support/tickets", map[string]any{"subject": "Payout missing", "body": "x", "category": "payouts"}, key(a))
	require.Equal(t, http.StatusForbidden, r.code, r.raw)

	tid := s.bizTicket(s.owner, "Payout missing")
	r = a.call(s.owner2, http.MethodGet, s.B+"/support/tickets/"+tid+"/messages", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, "owner of B on A's ticket: %s", r.raw)
	r = a.call(s.owner2, http.MethodGet, s.BY+"/support/tickets/"+tid+"/messages", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "A's ticket under B: %s", r.raw)
	r = a.call(s.cust, http.MethodGet, "/v1/me/support/tickets/"+tid+"/messages", nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, "a business ticket is not the customer's: %s", r.raw)

	require.Equal(t, 1, a.count(`SELECT count(*) FROM admin_users`))
	for _, role := range []string{"finance", "analyst"} {
		a.exec(`UPDATE admin_users SET roles = $1`, []string{role})
		tok := s.desk.token()
		r = a.call(tok, http.MethodGet, "/v1/admin/tickets", nil, nil)
		require.Equal(t, http.StatusForbidden, r.code, "%s: %s", role, r.raw)
		r = a.call(tok, http.MethodPost, "/v1/admin/tickets/"+tid+"/reply", map[string]any{"body": "x"}, key(a))
		require.Equal(t, http.StatusForbidden, r.code, "%s replies: %s", role, r.raw)
	}
	a.exec(`UPDATE admin_users SET roles = '{support}'`)
	r = a.call(s.desk.token(), http.MethodGet, "/v1/admin/tickets", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// Create and reply need an Idempotency-Key on every side; the same key and
// bytes replay the first answer and open nothing new.
func TestSupportHTTPIdempotency(t *testing.T) {
	s := newSupportShop(t)
	a := s.a
	agent := s.desk.token()
	create := map[string]any{"subject": "Payout missing", "body": "Our payout has not arrived.", "category": "payouts"}
	reply := map[string]any{"body": "Any news?"}

	r := a.call(s.owner, http.MethodPost, s.B+"/support/tickets", create, nil)
	require.Equal(t, http.StatusBadRequest, r.code, r.raw)
	require.Equal(t, "idempotency_key_required", problemCode(r))
	r = a.call(s.cust, http.MethodPost, "/v1/me/support/tickets", map[string]any{"subject": "Bill", "body": "x", "category": "other"}, nil)
	require.Equal(t, "idempotency_key_required", problemCode(r), r.raw)

	k := key(a)
	first := a.call(s.owner, http.MethodPost, s.B+"/support/tickets", create, k)
	require.Equal(t, http.StatusCreated, first.code, first.raw)
	again := a.call(s.owner, http.MethodPost, s.B+"/support/tickets", create, k)
	require.Equal(t, http.StatusCreated, again.code, again.raw)
	require.Equal(t, first.raw, again.raw, "the stored answer")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_tickets`))
	changed := map[string]any{"subject": "Payout late", "body": "Our payout has not arrived.", "category": "payouts"}
	r = a.call(s.owner, http.MethodPost, s.B+"/support/tickets", changed, k)
	require.Equal(t, http.StatusUnprocessableEntity, r.code, r.raw)
	require.Equal(t, "idempotency_mismatch", problemCode(r))
	tid := first.body["ticket"].(map[string]any)["id"].(string)

	mk := key(a)
	m1 := a.call(s.cust, http.MethodPost, "/v1/me/support/tickets", map[string]any{"subject": "Bill", "body": "x", "category": "other"}, mk)
	require.Equal(t, http.StatusCreated, m1.code, m1.raw)
	m2 := a.call(s.cust, http.MethodPost, "/v1/me/support/tickets", map[string]any{"subject": "Bill", "body": "x", "category": "other"}, mk)
	require.Equal(t, m1.raw, m2.raw)
	mid := m1.body["ticket"].(map[string]any)["id"].(string)
	require.Equal(t, 2, a.count(`SELECT count(*) FROM support_tickets`))

	for _, c := range []struct {
		tok, path string
		want      int
	}{
		{s.owner, s.B + "/support/tickets/" + tid + "/messages", http.StatusCreated},
		{s.cust, "/v1/me/support/tickets/" + mid + "/messages", http.StatusCreated},
		{agent, "/v1/admin/tickets/" + tid + "/reply", http.StatusOK},
		{agent, "/v1/admin/tickets/" + tid + "/notes", http.StatusOK},
	} {
		r = a.call(c.tok, http.MethodPost, c.path, reply, nil)
		require.Equal(t, "idempotency_key_required", problemCode(r), "%s: %s", c.path, r.raw)
		k := key(a)
		r1 := a.call(c.tok, http.MethodPost, c.path, reply, k)
		require.Equal(t, c.want, r1.code, "%s: %s", c.path, r1.raw)
		r2 := a.call(c.tok, http.MethodPost, c.path, reply, k)
		require.Equal(t, r1.raw, r2.raw, c.path)
	}
	require.Equal(t, 4, a.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1`, uuidOf(t, tid)), "first, owner, reply, note")
	require.Equal(t, 2, a.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1`, uuidOf(t, mid)))
}

func race(fs ...func() resp) []resp {
	out := make([]resp, len(fs))
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i, f := range fs {
		wg.Go(func() {
			<-start
			out[i] = f()
		})
	}
	close(start)
	wg.Wait()
	return out
}

func TestSupportHTTPConcurrency(t *testing.T) {
	s := newSupportShop(t)
	a := s.a
	agent := s.desk.token()
	post := func(tok, path string, body any) func() resp {
		hdr := key(a)
		return func() resp { return a.call(tok, http.MethodPost, path, body, hdr) }
	}
	state := func(tid string) (status string, due *time.Time, started *time.Time) {
		t.Helper()
		require.NoError(t, a.tdb.OwnerPool.QueryRow(t.Context(), `SELECT status, sla_due_at, sla_started_at FROM support_tickets WHERE id = $1`, uuidOf(t, tid)).
			Scan(&status, &due, &started))
		return status, due, started
	}

	// An opener reply racing a resolve: both land, in some order, and the
	// timer agrees with the status. Seven tickets in an hour is past the
	// default rate limit, so this test raises it.
	a.exec(`UPDATE platform_settings SET value = '50'::jsonb WHERE key = 'support_tickets_per_hour'`)
	s.settings.Invalidate()
	for range 5 {
		tid := s.bizTicket(s.owner, "Race")
		rs := race(
			post(s.owner, s.B+"/support/tickets/"+tid+"/messages", map[string]any{"body": "Any news?"}),
			post(agent, "/v1/admin/tickets/"+tid+"/close", map[string]any{"outcome": "resolved", "reason": "Paid."}),
		)
		require.Equal(t, http.StatusCreated, rs[0].code, rs[0].raw)
		require.Equal(t, http.StatusOK, rs[1].code, rs[1].raw)
		status, due, _ := state(tid)
		switch status {
		case "open":
			require.NotNil(t, due, "reopened with a timer")
		case "resolved":
			require.Nil(t, due)
		default:
			t.Fatalf("status %s", status)
		}
		r := a.call(agent, http.MethodPost, "/v1/admin/tickets/"+tid+"/close", map[string]any{"outcome": "closed", "reason": "Done."}, nil)
		require.Equal(t, http.StatusOK, r.code, r.raw)
	}

	// Two claims of one upload: one message gets it, the other is refused
	// and leaves nothing behind.
	tid := s.bizTicket(s.owner, "Statement")
	up := s.upload(s.owner, s.B+"/support/attachments")
	s.bucket.put(t, up)
	path := s.B + "/support/tickets/" + tid + "/messages"
	rs := race(
		post(s.owner, path, map[string]any{"body": "One", "attachmentIds": []string{attID(up)}}),
		post(s.owner, path, map[string]any{"body": "Two", "attachmentIds": []string{attID(up)}}),
	)
	codes := []int{rs[0].code, rs[1].code}
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusUnprocessableEntity}, codes, "%s | %s", rs[0].raw, rs[1].raw)
	require.Equal(t, 2, a.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1`, uuidOf(t, tid)), "the first and one reply")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_attachments WHERE ticket_id = $1 AND message_id IS NOT NULL`, uuidOf(t, tid)))

	// Two opener replies on a waiting ticket: both land, one timer from the
	// first of them.
	r := a.call(agent, http.MethodPost, "/v1/admin/tickets/"+tid+"/reply", map[string]any{"body": "Which week?"}, key(a))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	s.advance(time.Hour)
	at := a.clk.Now()
	rs = race(
		post(s.owner, path, map[string]any{"body": "The week of 12 October."}),
		post(s.owner, path, map[string]any{"body": "Or the week after."}),
	)
	require.Equal(t, http.StatusCreated, rs[0].code, rs[0].raw)
	require.Equal(t, http.StatusCreated, rs[1].code, rs[1].raw)
	status, due, started := state(tid)
	require.Equal(t, "open", status)
	require.WithinDuration(t, at, *started, time.Second)
	require.Equal(t, started.Add(24*time.Hour), *due, "one timer at normal priority")
	require.Equal(t, 5, a.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1`, uuidOf(t, tid)))
}

// A settings change lands before a priority change: the new due time uses
// the minutes in force at the change, from when the timer started.
func TestSupportHTTPSettingThenPriority(t *testing.T) {
	s := newSupportShop(t)
	a := s.a
	agent := s.desk.token()
	start := a.clk.Now()
	tid := s.bizTicket(s.owner, "Payout missing")
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND sla_due_at = $2`, uuidOf(t, tid), start.Add(24*time.Hour)))

	a.exec(`UPDATE platform_settings SET value = '90'::jsonb WHERE key = 'support_sla_high_min'`)
	s.settings.Invalidate()
	r := a.call(agent, http.MethodPost, "/v1/admin/tickets/"+tid+"/priority", map[string]any{"priority": "high"}, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	due, err := time.Parse(time.RFC3339Nano, r.body["ticket"].(map[string]any)["slaDueAt"].(string))
	require.NoError(t, err)
	require.True(t, start.Add(90*time.Minute).Equal(due), "due %s", due)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM audit_log WHERE action = 'support.ticket_priority' AND subject_id = $1`, tid))
}

// Uploads never cross sides: a customer's upload is not the business's, a
// business's is not the customer's, an agent's is neither, even for the same
// person.
func TestSupportHTTPAttachmentSides(t *testing.T) {
	s := newSupportShop(t)
	a := s.a
	agent := s.desk.token()
	tid := s.bizTicket(s.owner, "Payout missing")
	var ownerAsCustomer string
	s.login(&ownerAsCustomer, ownerPhone, "customer")
	mine := a.call(ownerAsCustomer, http.MethodPost, "/v1/me/support/tickets", map[string]any{"subject": "Bill", "body": "x", "category": "other"}, key(a))
	require.Equal(t, http.StatusCreated, mine.code, mine.raw)
	mid := mine.body["ticket"].(map[string]any)["id"].(string)

	custUp := s.upload(ownerAsCustomer, "/v1/me/support/attachments")
	bizUp := s.upload(s.owner, s.B+"/support/attachments")
	r := a.call(agent, http.MethodPost, "/v1/admin/tickets/"+tid+"/attachments", map[string]any{"fileName": "utr.pdf", "contentType": "application/pdf", "sizeBytes": 2048}, nil)
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	agentUp := r
	for _, u := range []resp{custUp, bizUp, agentUp} {
		s.bucket.put(t, u)
	}

	for _, c := range []struct {
		name, tok, path, file string
	}{
		{"customer upload on the business ticket", s.owner, s.B + "/support/tickets/" + tid + "/messages", attID(custUp)},
		{"agent upload by the business", s.owner, s.B + "/support/tickets/" + tid + "/messages", attID(agentUp)},
		{"business upload on the customer ticket", ownerAsCustomer, "/v1/me/support/tickets/" + mid + "/messages", attID(bizUp)},
		{"agent upload by the customer", ownerAsCustomer, "/v1/me/support/tickets/" + mid + "/messages", attID(agentUp)},
		{"business upload by the agent", agent, "/v1/admin/tickets/" + tid + "/reply", attID(bizUp)},
		{"agent upload on another ticket", agent, "/v1/admin/tickets/" + mid + "/reply", attID(agentUp)},
	} {
		r := a.call(c.tok, http.MethodPost, c.path, map[string]any{"body": "x", "attachmentIds": []string{c.file}}, key(a))
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "%s: %s", c.name, r.raw)
	}
	require.Zero(t, a.count(`SELECT count(*) FROM support_attachments WHERE message_id IS NOT NULL`))

	r = a.call(agent, http.MethodPost, "/v1/admin/tickets/"+tid+"/reply", map[string]any{"body": "UTR attached.", "attachmentIds": []string{attID(agentUp)}}, key(a))
	require.Equal(t, http.StatusOK, r.code, r.raw)
}

// Deleting a customer account closes their tickets, blanks what they wrote
// and deletes their files (01 §13).
func TestSupportHTTPAccountDeletion(t *testing.T) {
	s := newSupportShop(t)
	a := s.a
	up := s.upload(s.cust, "/v1/me/support/attachments")
	s.bucket.put(t, up)
	r := a.call(s.cust, http.MethodPost, "/v1/me/support/tickets", map[string]any{
		"subject": "Atta missing", "body": "My flat is 4B.", "category": "other", "attachmentIds": []string{attID(up)},
	}, key(a))
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	tid := r.body["ticket"].(map[string]any)["id"].(string)

	del := a.deleteMe(s.cust, custPhone, "customer")
	require.Less(t, del.code, 300, del.raw)
	require.Equal(t, 1, a.count(`SELECT count(*) FROM support_tickets WHERE id = $1 AND status = 'closed' AND close_reason = 'account_deleted'
		AND subject <> 'Atta missing'`, uuidOf(t, tid)))
	require.Zero(t, a.count(`SELECT count(*) FROM support_messages WHERE ticket_id = $1 AND body = 'My flat is 4B.'`, uuidOf(t, tid)))
	require.Zero(t, a.count(`SELECT count(*) FROM support_attachments`))
	require.False(t, s.bucket.has(t, up))
}
