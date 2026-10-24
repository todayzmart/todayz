package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/gen/sqlc/notifydb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

// --- fakes ----------------------------------------------------------------------

// recJobs records what Send enqueues.
type recJobs struct {
	mu   sync.Mutex
	jobs []recJob
}

type recJob struct {
	Kind, UniqueKey string
	Args            json.RawMessage
}

func (r *recJobs) EnqueueTx(_ context.Context, tx pgx.Tx, kind string, args any, o platformjobs.EnqueueOpts) error {
	if tx == nil {
		return errors.New("no tx")
	}
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, recJob{Kind: kind, UniqueKey: o.UniqueKey, Args: b})
	return nil
}

func (r *recJobs) all() []recJob {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.jobs)
}

// fakeProvider records messages and answers with res or err.
type fakeProvider struct {
	mu   sync.Mutex
	sent []Message
	res  Result
	err  error
}

func (*fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) Send(_ context.Context, m Message) (Result, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, m)
	return p.res, p.err
}

func (p *fakeProvider) messages() []Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.sent)
}

// fakeUnseen is W2-02's UnseenOrders over a fixed list.
type fakeUnseen struct {
	mu      sync.Mutex
	orders  []notice.UnseenOrder // sorted by (CreatedAt, OrderID)
	queries []notice.UnseenQuery
	err     error
}

func (f *fakeUnseen) add(o ...notice.UnseenOrder) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders = append(f.orders, o...)
	slices.SortFunc(f.orders, func(a, b notice.UnseenOrder) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return bytes.Compare(a.OrderID[:], b.OrderID[:])
	})
}

func (f *fakeUnseen) remove(id ids.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders = slices.DeleteFunc(f.orders, func(o notice.UnseenOrder) bool { return o.OrderID == id })
}

func (f *fakeUnseen) Unseen(_ context.Context, q notice.UnseenQuery) ([]notice.UnseenOrder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, q)
	if f.err != nil {
		return nil, f.err
	}
	var out []notice.UnseenOrder
	for _, o := range f.orders {
		if o.CreatedAt.After(q.CreatedBefore) {
			continue
		}
		if a := q.After; a != nil {
			c := o.CreatedAt.Compare(a.CreatedAt)
			if c < 0 || (c == 0 && bytes.Compare(o.OrderID[:], a.OrderID[:]) <= 0) {
				continue
			}
		}
		out = append(out, o)
		if len(out) == q.Limit {
			break
		}
	}
	return out, nil
}

// fakeContacts is W1-04's StoreContacts.
type fakeContacts struct {
	m   map[ids.ID]notice.Contact
	err error
}

func (f fakeContacts) Contact(_ context.Context, id ids.ID) (notice.Contact, bool, error) {
	if f.err != nil {
		return notice.Contact{}, false, f.err
	}
	c, ok := f.m[id]
	return c, ok, nil
}

// --- environment -----------------------------------------------------------------

type tenv struct {
	t        *testing.T
	tdb      *testkit.DB
	clk      *clock.Fake
	st       *settings.Cache
	jobs     *recJobs
	sms, wa  *fakeProvider
	svc      *Service
	log      *bytes.Buffer
	user     ids.ID // a customer
	owner    ids.ID // the business owner
	biz      ids.ID
	store    ids.ID
	store2   ids.ID
	otherBiz ids.ID
}

var testPIIKey = bytes.Repeat([]byte{7}, 32)

func newEnv(t *testing.T, mut ...func(*Deps)) *tenv {
	t.Helper()
	tdb := testkit.NewDB(t)
	clk := clock.NewFake(time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC))
	logBuf := &bytes.Buffer{}
	log := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	e := &tenv{
		t: t, tdb: tdb, clk: clk, log: logBuf,
		st:   settings.NewCache(settings.PGLoader(tdb.Pool), settings.Options{TTL: time.Hour, Log: log}),
		jobs: &recJobs{}, sms: &fakeProvider{res: Result{ProviderID: "p-1", Units: 2}}, wa: &fakeProvider{res: Result{ProviderID: "w-1", Units: 1}},
	}
	e.user, e.owner = e.newID(), e.newID()
	e.biz, e.store, e.store2, e.otherBiz = e.newID(), e.newID(), e.newID(), e.newID()
	e.exec(`INSERT INTO users (id) VALUES ($1), ($2)`, ids.UUID(e.user), ids.UUID(e.owner))
	e.exec(`INSERT INTO businesses (id, brand_name, referral_code) VALUES ($1, 'Lakshmi Traders', 'ABCDEF'), ($2, 'Ganesh Stores', 'ABCDEG')`,
		ids.UUID(e.biz), ids.UUID(e.otherBiz))
	e.exec(`INSERT INTO stores (id, business_id, slug, short_code, name) VALUES
		($1, $2, 'lakshmi-traders-jayanagar', 'ABCDEH', 'Lakshmi Traders'),
		($3, $2, 'lakshmi-traders-btm', 'ABCDEJ', 'Lakshmi Traders BTM')`,
		ids.UUID(e.store), ids.UUID(e.biz), ids.UUID(e.store2))
	d := Deps{
		DB: db.New(tdb.Pool), Clock: clk, Settings: e.st, Log: log, Jobs: e.jobs,
		Config:           &config.Config{Env: config.EnvTest, Keys: config.AppKeys{PIIEncKey: config.NewSecret(testPIIKey)}},
		Providers:        map[notice.Channel]Provider{notice.SMS: e.sms, notice.WhatsApp: e.wa},
		CostPerUnitPaise: map[notice.Channel]int64{notice.SMS: 25},
	}
	for _, m := range mut {
		m(&d)
	}
	svc, err := New(d)
	require.NoError(t, err)
	e.svc = svc
	return e
}

func (e *tenv) newID() ids.ID {
	e.clk.Advance(time.Millisecond)
	return ids.New(e.clk.Now())
}

func (e *tenv) exec(sql string, args ...any) {
	e.t.Helper()
	_, err := e.tdb.OwnerPool.Exec(context.Background(), sql, args...)
	require.NoError(e.t, err)
}

func (e *tenv) count(sql string, args ...any) int {
	e.t.Helper()
	var n int
	require.NoError(e.t, e.tdb.OwnerPool.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

func (e *tenv) setting(key settings.Key, value string) {
	e.t.Helper()
	e.exec(`UPDATE platform_settings SET value = $2 WHERE key = $1`, string(key), value)
	e.st.Invalidate()
}

// send runs Send in its own transaction, committed on success.
func (e *tenv) send(n notice.Notification) (ids.ID, error) {
	ctx := context.Background()
	tx, err := e.tdb.Pool.Begin(ctx)
	require.NoError(e.t, err)
	id, err := e.svc.Send(ctx, tx, n)
	if err != nil {
		require.NoError(e.t, tx.Rollback(ctx))
		return id, err
	}
	require.NoError(e.t, tx.Commit(ctx))
	return id, nil
}

func (e *tenv) mustSend(n notice.Notification) ids.ID {
	e.t.Helper()
	id, err := e.send(n)
	require.NoError(e.t, err)
	return id
}

func (e *tenv) unseenNote(phone, lang string) notice.Notification {
	return notice.Notification{
		Kind: notice.KindOrderUnseen, BusinessID: e.biz, StoreID: e.store,
		Params:   map[string]string{"store": "Lakshmi Traders", "minutes": "3"},
		Ref:      &notice.Ref{Type: "order", ID: e.newID()},
		Outside:  []notice.Outside{{Channel: notice.SMS, Phone: phone, Lang: lang}},
		PrefUser: e.owner,
	}
}

// deliveryOf returns the deliveries of notification nid.
func (e *tenv) deliveriesOf(nid ids.ID) []notifydb.GetDeliveryRow {
	e.t.Helper()
	rows, err := e.tdb.OwnerPool.Query(context.Background(), `SELECT id FROM notification_deliveries WHERE notification_id = $1 ORDER BY id`, ids.UUID(nid))
	require.NoError(e.t, err)
	var out []notifydb.GetDeliveryRow
	var list []ids.ID
	for rows.Next() {
		var u [16]byte
		require.NoError(e.t, rows.Scan(&u))
		list = append(list, ids.ID(u))
	}
	require.NoError(e.t, rows.Err())
	for _, id := range list {
		r, err := notifydb.New(e.tdb.Pool).GetDelivery(context.Background(), ids.UUID(id))
		require.NoError(e.t, err)
		out = append(out, r)
	}
	return out
}

// --- Send ---------------------------------------------------------------------------

func TestSendRefusesProgrammingErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	ok := notice.Notification{Kind: notice.KindOrderUpdate, UserID: e.user, Params: map[string]string{"store": "Ganesh", "variant": "ready"}}
	for _, c := range []struct {
		name string
		mut  func(*notice.Notification)
		want error
	}{
		{"unknown kind", func(n *notice.Notification) { n.Kind = "nope" }, ErrUnknownKind},
		{"no recipient", func(n *notice.Notification) { n.UserID = ids.ID{} }, ErrRecipient},
		{"both recipients", func(n *notice.Notification) { n.BusinessID = e.biz }, ErrRecipient},
		{"a customer kind to a business", func(n *notice.Notification) { n.UserID, n.BusinessID = ids.ID{}, e.biz }, ErrRecipient},
		{"a business kind to a user", func(n *notice.Notification) { n.Kind = notice.KindNewOrder; n.Params = nil }, ErrRecipient},
		{"a store without its business", func(n *notice.Notification) { n.StoreID = e.store }, ErrRecipient},
		{"an undeclared param", func(n *notice.Notification) { n.Params["phone"] = "x" }, ErrParams},
		{"a bad ref type", func(n *notice.Notification) { n.Ref = &notice.Ref{Type: "Order!", ID: e.user} }, ErrRef},
		{"a ref without id", func(n *notice.Notification) { n.Ref = &notice.Ref{Type: "order"} }, ErrRef},
	} {
		n := ok
		n.Params = map[string]string{"store": "Ganesh", "variant": "ready"}
		c.mut(&n)
		_, err := e.send(n)
		require.ErrorIs(t, err, c.want, c.name)
	}
	// A store of another business.
	_, err := e.send(notice.Notification{Kind: notice.KindNewOrder, BusinessID: e.otherBiz, StoreID: e.store})
	require.ErrorIs(t, err, ErrStore)
	// No transaction.
	_, err = e.svc.Send(context.Background(), nil, ok)
	require.ErrorIs(t, err, realtime.ErrNoTx)
	require.Zero(t, e.count(`SELECT count(*) FROM notifications`))
	require.Zero(t, e.count(`SELECT count(*) FROM outbox_events`))
}

func TestSendWritesTheRowAndItsEventInTheCallersTransaction(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	order := e.newID()
	id := e.mustSend(notice.Notification{
		Kind: notice.KindOrderUpdate, UserID: e.user,
		Params: map[string]string{"store": "Ganesh", "variant": "accepted"}, Ref: &notice.Ref{Type: "order", ID: order},
	})
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notifications WHERE id = $1 AND user_id = $2 AND app = 'customer' AND ref_type = 'order' AND ref_id = $3`,
		ids.UUID(id), ids.UUID(e.user), ids.UUID(order)))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM outbox_events WHERE channel = $1 AND payload = $2::jsonb`,
		string(realtime.UserChannel(e.user)), `{"type":"inbox.updated","id":"`+id.String()+`","version":1}`))

	// A store row is announced on that store; a business-wide row on each store.
	sid := e.mustSend(notice.Notification{Kind: notice.KindNewOrder, BusinessID: e.biz, StoreID: e.store2, Params: map[string]string{"store": "BTM"}})
	require.Equal(t, 1, e.count(`SELECT count(*) FROM outbox_events WHERE payload->>'id' = $1`, sid.String()))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM outbox_events WHERE payload->>'id' = $1 AND channel = $2`, sid.String(), string(realtime.StoreChannel(e.store2))))
	wide := e.mustSend(notice.Notification{Kind: notice.KindNewAsk, BusinessID: e.biz})
	require.Equal(t, 2, e.count(`SELECT count(*) FROM outbox_events WHERE payload->>'id' = $1`, wide.String()))

	// Rolled back with the caller: no row, no event, no delivery.
	ctx := context.Background()
	tx, err := e.tdb.Pool.Begin(ctx)
	require.NoError(t, err)
	rid, err := e.svc.Send(ctx, tx, e.unseenNote(testPhone, "en"))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback(ctx))
	require.Zero(t, e.count(`SELECT count(*) FROM notifications WHERE id = $1`, ids.UUID(rid)))
	require.Zero(t, e.count(`SELECT count(*) FROM outbox_events WHERE payload->>'id' = $1`, rid.String()))
	require.Zero(t, e.count(`SELECT count(*) FROM notification_deliveries WHERE notification_id = $1`, ids.UUID(rid)))
}

func TestSendQueuesAnOutsideMessageOnlyWhenEverySwitchAllows(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)

	// Queued: the kind allows SMS, the flag is on (default), a provider and
	// the sealer exist, the owner has not switched it off.
	id := e.mustSend(e.unseenNote(testPhone, "hi"))
	ds := e.deliveriesOf(id)
	require.Len(t, ds, 1)
	require.Equal(t, StatusQueued, ds[0].Status)
	require.Equal(t, "sms", ds[0].Channel)
	require.Equal(t, "fake", ds[0].Provider)
	require.Equal(t, "hi", ds[0].Lang)
	require.True(t, ds[0].HasPhone)
	// The phone is sealed: not in the row in clear, not in the job.
	require.Zero(t, e.count(`SELECT count(*) FROM notification_deliveries WHERE position($1::bytea IN to_enc) > 0`, []byte("9000000011")))
	jobs := e.jobs.all()
	require.Len(t, jobs, 1)
	require.Equal(t, KindDeliver, jobs[0].Kind)
	did, _ := ids.FromUUID(ds[0].ID)
	require.JSONEq(t, `{"deliveryId":"`+did.String()+`"}`, string(jobs[0].Args))
	require.Equal(t, did.String(), jobs[0].UniqueKey)

	skipped := func(name string, n notice.Notification) {
		t.Helper()
		before := len(e.jobs.all())
		id := e.mustSend(n)
		require.Empty(t, e.deliveriesOf(id), name)
		require.Len(t, e.jobs.all(), before, name)
		require.Equal(t, 1, e.count(`SELECT count(*) FROM notifications WHERE id = $1`, ids.UUID(id)), "%s: the inbox row stands on its own", name)
	}
	// The kind does not use SMS.
	n := notice.Notification{Kind: notice.KindNewOrder, BusinessID: e.biz, StoreID: e.store, Params: map[string]string{"store": "x"},
		Outside: []notice.Outside{{Channel: notice.SMS, Phone: testPhone}}}
	skipped("kind", n)
	// No usable phone (Send takes E.164 only; the caller normalises).
	skipped("phone", e.unseenNote("12345", "en"))
	skipped("phone with spaces", e.unseenNote("+91 90000 00011", "en"))
	// The owner switched the kind off.
	e.exec(`INSERT INTO notification_prefs (user_id, app, kind, channel, enabled, updated_at) VALUES ($1, 'business', 'order_unseen', 'sms', false, now())`, ids.UUID(e.owner))
	skipped("pref", e.unseenNote(testPhone, "en"))
	e.exec(`DELETE FROM notification_prefs`)
	// The platform flag is off.
	e.setting(settings.NotifySmsEnabled, "false")
	skipped("flag", e.unseenNote(testPhone, "en"))
	e.setting(settings.NotifySmsEnabled, "true")
	require.Len(t, e.deliveriesOf(e.mustSend(e.unseenNote(testPhone, "en"))), 1, "on again")

	// No provider, no sealer, no queue.
	for name, mut := range map[string]func(*Deps){
		"provider": func(d *Deps) { d.Providers = map[notice.Channel]Provider{} },
		"sealer":   func(d *Deps) { d.Config = nil },
		"jobs":     func(d *Deps) { d.Jobs = nil },
		"settings": func(d *Deps) { d.Settings = nil },
	} {
		e2 := newEnv(t, mut)
		id := e2.mustSend(e2.unseenNote(testPhone, "en"))
		require.Empty(t, e2.deliveriesOf(id), name)
		require.Empty(t, e2.jobs.all(), name)
	}
	require.NotContains(t, e.log.String(), "9000000011", "no phone in any log line")
}

// --- Deliver --------------------------------------------------------------------------

func TestDeliverSendsAtMostOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	id := e.mustSend(e.unseenNote(testPhone, "hi"))
	did, _ := ids.FromUUID(e.deliveriesOf(id)[0].ID)

	res, err := e.svc.Deliver(context.Background(), did)
	require.NoError(t, err)
	require.Equal(t, DeliverResult{Claimed: true, Status: StatusSent}, res)
	msgs := e.sms.messages()
	require.Len(t, msgs, 1)
	require.Equal(t, Message{
		To: testPhone, Template: "tz_order_unseen_hi", Lang: "hi",
		Vars: []Var{{"store", "Lakshmi Traders"}, {"minutes", "3"}},
		Text: msgs[0].Text,
	}, msgs[0])
	require.Contains(t, msgs[0].Text, "3 मिनट")

	d := e.deliveriesOf(id)[0]
	require.Equal(t, StatusSent, d.Status)
	require.Equal(t, "p-1", *d.ProviderID)
	require.False(t, d.HasPhone, "the phone is cleared once the delivery ends")
	require.EqualValues(t, 2*25, d.CostPaise, "units x the per-unit estimate")

	// A retried job finds the row claimed and sends nothing.
	res, err = e.svc.Deliver(context.Background(), did)
	require.NoError(t, err)
	require.Equal(t, DeliverResult{}, res)
	require.Len(t, e.sms.messages(), 1)
	// An unknown delivery is a no-op too.
	res, err = e.svc.Deliver(context.Background(), e.newID())
	require.NoError(t, err)
	require.False(t, res.Claimed)
}

// D-212: DLT caps each template variable at 30 characters, so the SMS cuts
// a long store name to 30 runes, in the variables and the body alike; the
// in-app row keeps the full name.
func TestDeliverCutsSMSParamsForDLT(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	n := e.unseenNote(testPhone, "en")
	n.Params["store"] = "Sri Lakshmi Venkateswara General Stores and Provisions"
	id := e.mustSend(n)
	did, _ := ids.FromUUID(e.deliveriesOf(id)[0].ID)
	_, err := e.svc.Deliver(context.Background(), did)
	require.NoError(t, err)
	m := e.sms.messages()[0]
	require.Equal(t, []Var{{"store", "Sri Lakshmi Venkateswara Gener"}, {"minutes", "3"}}, m.Vars)
	require.Equal(t, "A new order at Sri Lakshmi Venkateswara Gener has not been opened for 3 minutes. Open the TodayZ business app to accept it.", m.Text)
	var params string
	require.NoError(t, e.tdb.OwnerPool.QueryRow(context.Background(), `SELECT params::text FROM notifications WHERE id = $1`, ids.UUID(id)).Scan(&params))
	require.Contains(t, params, "General Stores and Provisions", "the inbox keeps the full name")
}

func TestDeliverFailureIsFinalAndCarriesNoPhone(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	e.sms.err = errors.New("notify: provider call failed: msg91 sms: status 400 for 919000000011\nagain")
	id := e.mustSend(e.unseenNote(testPhone, "kn"))
	did, _ := ids.FromUUID(e.deliveriesOf(id)[0].ID)

	res, err := e.svc.Deliver(context.Background(), did)
	require.NoError(t, err, "a provider failure is recorded, not retried")
	require.Equal(t, DeliverResult{Claimed: true, Status: StatusFailed}, res)
	d := e.deliveriesOf(id)[0]
	require.Equal(t, StatusFailed, d.Status)
	require.NotContains(t, *d.Error, "9000000011")
	require.NotContains(t, *d.Error, "\n")
	require.False(t, d.HasPhone)
	require.Equal(t, "tz_order_unseen", e.sms.messages()[0].Template, "kn falls back to the English template")
	require.Equal(t, "en", e.sms.messages()[0].Lang)

	res, err = e.svc.Deliver(context.Background(), did)
	require.NoError(t, err)
	require.False(t, res.Claimed)
	require.Len(t, e.sms.messages(), 1, "never sent twice")

	// A delivery whose provider went away fails without a call.
	e2 := newEnv(t)
	id = e2.mustSend(e2.unseenNote(testPhone, "en"))
	did, _ = ids.FromUUID(e2.deliveriesOf(id)[0].ID)
	gone, err := New(Deps{DB: db.New(e2.tdb.Pool), Clock: e2.clk, Providers: map[notice.Channel]Provider{},
		Config: &config.Config{Keys: config.AppKeys{PIIEncKey: config.NewSecret(testPIIKey)}}})
	require.NoError(t, err)
	res, err = gone.Deliver(context.Background(), did)
	require.NoError(t, err)
	require.Equal(t, StatusFailed, res.Status)
	require.Equal(t, "provider not configured", *e2.deliveriesOf(id)[0].Error)
	require.NotContains(t, e.log.String()+e2.log.String(), "9000000011")
}

// --- escalation -----------------------------------------------------------------------

func TestEscalateOncePerOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	now := e.clk.Now()
	src := &fakeUnseen{}
	contacts := fakeContacts{m: map[ids.ID]notice.Contact{
		e.store: {BusinessID: e.biz, Phone: testPhone, Lang: "hi", OwnerUserID: e.owner, StoreName: "Lakshmi Traders"},
	}}
	o1 := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store, CreatedAt: now.Add(-4 * time.Minute)}
	fresh := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store, CreatedAt: now.Add(-2 * time.Minute)}
	src.add(o1, fresh)

	res, err := e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.Equal(t, EscalateResult{Unseen: 1, Escalated: 1}, res)
	// order_unseen_sms_after_min (3, from settings) decides the cut-off.
	require.Equal(t, now.Add(-3*time.Minute), src.queries[0].CreatedBefore)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notify_order_escalations WHERE order_id = $1`, ids.UUID(o1.OrderID)))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notifications WHERE kind = 'order_unseen' AND business_id = $1 AND store_id = $2 AND ref_id = $3 AND params->>'minutes' = '3'`,
		ids.UUID(e.biz), ids.UUID(e.store), ids.UUID(o1.OrderID)))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notification_deliveries WHERE channel = 'sms' AND lang = 'hi'`), "SMS in the business language")

	// The next minute: o1 is already done; fresh is now due.
	e.clk.Advance(time.Minute)
	res, err = e.svc.Escalate(context.Background(), e.clk.Now(), src, contacts)
	require.NoError(t, err)
	require.Equal(t, EscalateResult{Unseen: 2, Escalated: 1, Already: 1}, res)
	res, err = e.svc.Escalate(context.Background(), e.clk.Now(), src, contacts)
	require.NoError(t, err)
	require.Equal(t, EscalateResult{Unseen: 2, Already: 2}, res)
	require.Equal(t, 2, e.count(`SELECT count(*) FROM notification_deliveries`), "one message per order, ever")
	require.Len(t, e.jobs.all(), 2)

	// The setting is read, not hard-coded.
	e.setting(settings.OrderUnseenSmsAfterMin, "10")
	_, err = e.svc.Escalate(context.Background(), e.clk.Now(), src, contacts)
	require.NoError(t, err)
	require.Equal(t, e.clk.Now().Add(-10*time.Minute), src.queries[len(src.queries)-1].CreatedBefore)
	require.NotContains(t, e.log.String(), "9000000011")
}

func TestEscalateContactsAndChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	now := e.clk.Now()
	src := &fakeUnseen{}
	unknownStore := e.newID()
	contacts := fakeContacts{m: map[ids.ID]notice.Contact{
		// No phone at all: the inbox row is still written.
		e.store: {BusinessID: e.biz, StoreName: "Lakshmi Traders", OwnerUserID: e.owner},
		// A language that is not a notification language: English.
		e.store2: {BusinessID: e.biz, Phone: testPhone, Lang: "fr", StoreName: strings.Repeat("ल", 200)},
	}}
	a := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store, CreatedAt: now.Add(-time.Hour)}
	b := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store2, CreatedAt: now.Add(-time.Hour)}
	c := notice.UnseenOrder{OrderID: e.newID(), StoreID: unknownStore, CreatedAt: now.Add(-time.Hour)}
	src.add(a, b, c)

	res, err := e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.Equal(t, EscalateResult{Unseen: 3, Escalated: 2, NoContact: 1, NoOutside: 1}, res)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notification_deliveries WHERE lang = 'en'`))
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notifications WHERE store_id = $1 AND char_length(params->>'store') = $2`, ids.UUID(e.store2), maxParamRunes),
		"a long store name is cut to the param limit, not refused")

	// WhatsApp when it is on and configured; SMS otherwise.
	e.setting(settings.NotifyWhatsappEnabled, "true")
	d := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store2, CreatedAt: now.Add(-time.Hour)}
	src.add(d)
	res, err = e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.EqualValues(t, 1, res.Escalated)
	require.Equal(t, 1, e.count(`SELECT count(*) FROM notification_deliveries WHERE channel = 'whatsapp'`))

	// Both channels off: the inbox row only.
	e.setting(settings.NotifyWhatsappEnabled, "false")
	e.setting(settings.NotifySmsEnabled, "false")
	f := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store2, CreatedAt: now.Add(-time.Hour)}
	src.add(f)
	res, err = e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.EqualValues(t, 1, res.Escalated)
	require.EqualValues(t, 1, res.NoOutside)

	// A failing source is an error; a failing contact lookup fails that order only.
	src.err = errors.New("orders down")
	_, err = e.svc.Escalate(context.Background(), now, src, contacts)
	require.Error(t, err)
	src.err = nil
	g := notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store, CreatedAt: now.Add(-time.Hour)}
	src.add(g)
	res, err = e.svc.Escalate(context.Background(), now, src, fakeContacts{err: errors.New("contacts down")})
	require.Error(t, err)
	require.EqualValues(t, 2, res.Failed, "g, and c (never claimed: its store had no contact, so it is retried too)")
	require.Zero(t, e.count(`SELECT count(*) FROM notify_order_escalations WHERE order_id = $1`, ids.UUID(g.OrderID)), "retried next tick")

	// Missing inputs.
	_, err = e.svc.Escalate(context.Background(), now, nil, contacts)
	require.Error(t, err)
	_, err = e.svc.Escalate(context.Background(), now, src, nil)
	require.Error(t, err)
}

func TestEscalatePagesPastAStuckBacklog(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t)
	now := e.clk.Now()
	src := &fakeUnseen{}
	contacts := fakeContacts{m: map[ids.ID]notice.Contact{e.store: {BusinessID: e.biz, StoreName: "Lakshmi Traders"}}}
	// A page of old orders already escalated and still unseen (the store
	// never opened them), then newer ones behind them.
	var all []notice.UnseenOrder
	for i := 0; i < escalatePage+30; i++ {
		all = append(all, notice.UnseenOrder{OrderID: e.newID(), StoreID: e.store, CreatedAt: now.Add(-time.Hour + time.Duration(i)*time.Second)})
	}
	src.add(all[:escalatePage]...)
	res, err := e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.EqualValues(t, escalatePage, res.Escalated)

	src.add(all[escalatePage:]...)
	res, err = e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.EqualValues(t, 30, res.Escalated, "the newer orders are reached on the second page")
	require.EqualValues(t, escalatePage, res.Already)
	// A full page asks for the next one: run 1 is 100 + an empty page,
	// run 2 is 100 + the 30 after the cursor.
	require.Len(t, src.queries, 2+2)
	require.Nil(t, src.queries[2].After)
	require.Equal(t, all[escalatePage-1].OrderID, src.queries[3].After.OrderID)
	require.Equal(t, escalatePage+30, e.count(`SELECT count(*) FROM notify_order_escalations`))

	// An order that leaves the source (seen, accepted) is simply not listed.
	src.remove(all[0].OrderID)
	res, err = e.svc.Escalate(context.Background(), now, src, contacts)
	require.NoError(t, err)
	require.EqualValues(t, escalatePage+29, res.Already)
	require.Zero(t, res.Escalated)
}

// --- integration status -----------------------------------------------------------------

func TestIntegrationStatusNeverShowsAValue(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	e := newEnv(t, func(d *Deps) {
		d.Config.Providers = config.Providers{
			SMSAPIURL: "https://mocks.todayz.localhost/sms-voice", SMSSenderID: "TODAYZ",
			SMSDLTEntityID:       "1201169999000000001",
			SMSNoticeTemplateIDs: map[string]string{"tz_order_unseen": "1207169999000000201"},
			WhatsAppAPIKey:       config.NewSecret([]byte("wa-secret-value")),
		}
		d.Providers = map[notice.Channel]Provider{notice.SMS: &fakeProvider{}}
	})
	st, err := e.svc.IntegrationStatus(context.Background())
	require.NoError(t, err)
	b, err := json.Marshal(st)
	require.NoError(t, err)
	for _, v := range []string{"wa-secret-value", "mocks.todayz.localhost", "TODAYZ", "1201169999000000001", "1207169999000000201"} {
		require.NotContains(t, string(b), v)
	}
	byCh := map[notice.Channel]ChannelStatus{}
	for _, c := range st {
		byCh[c.Channel] = c
	}
	sms := byCh[notice.SMS]
	require.True(t, sms.Enabled)
	require.True(t, sms.Configured)
	require.True(t, sms.Active)
	require.Equal(t, []KeyStatus{{"SMS_API_URL", true}, {"SMS_SENDER_ID", true}, {"SMS_DLT_ENTITY_ID", true}, {"SMS_NOTICE_TEMPLATE_IDS", true}}, sms.Keys)
	require.Equal(t, "no DLT template id for: tz_order_unseen_hi", sms.Note, "names the catalogue template, never an id")
	wa := byCh[notice.WhatsApp]
	require.False(t, wa.Enabled, "notify_whatsapp_enabled defaults to off")
	require.False(t, wa.Active)
	require.Equal(t, []KeyStatus{{"WHATSAPP_API_URL", false}, {"WHATSAPP_API_KEY", true}}, wa.Keys)
	require.False(t, byCh[notice.Push].Configured)

	// Deployed (D-212): the instance role signs in AWS_REGION; no endpoint.
	e.svc.d.Config.Env = config.EnvProd
	st, err = e.svc.IntegrationStatus(context.Background())
	require.NoError(t, err)
	require.Equal(t, []KeyStatus{{"AWS_REGION", false}, {"SMS_SENDER_ID", true}, {"SMS_DLT_ENTITY_ID", true}, {"SMS_NOTICE_TEMPLATE_IDS", true}}, st[0].Keys)
}
