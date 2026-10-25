package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/orders"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/pii"
)

// The W2-02 concurrency tests from the review: parallel wrong codes, a
// pickup-code collision on both redraw paths, a double accept, and an end
// state's release racing another order's accept. Codes stay in variables and
// are never printed; a failure names statuses and problem codes only.

// codeScript is a pickup-code randomness source: each draw takes the next
// scripted code (crypto/rand.Int reads 2 bytes for 4 digits and keeps the
// value when it is below 10^4), and crypto/rand once the script runs out.
type codeScript struct {
	mu    sync.Mutex
	codes []uint16
}

func (s *codeScript) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.codes) == 0 || len(p) < 2 {
		return rand.Read(p)
	}
	clear(p)
	binary.BigEndian.PutUint16(p[len(p)-2:], s.codes[0])
	s.codes = s.codes[1:]
	return len(p), nil
}

func (s *codeScript) push(codes ...uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.codes = append(s.codes, codes...)
}

// withCodeScript makes the next newOrderShop draw codes from a script. No
// test in this package runs in parallel, so the package variable is safe.
func withCodeScript(t *testing.T) *codeScript {
	t.Helper()
	s := &codeScript{}
	orderCodeRand = s
	t.Cleanup(func() { orderCodeRand = nil })
	return s
}

// codeHMAC is what the service stores for code at S (code.go lookup).
func (o *orderShop) codeHMAC(code string) []byte {
	o.t.Helper()
	h, err := pii.NewHasher(o.cfg.Keys.OrderCodeHMACKey.Bytes())
	require.NoError(o.t, err)
	return h.Sum("order_code:" + o.sid + ":" + code)
}

func (o *orderShop) storedHMAC(id string) []byte {
	o.t.Helper()
	var b []byte
	require.NoError(o.t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT code_hmac FROM orders WHERE id = $1`, uuidOf(o.t, id)).Scan(&b))
	return b
}

// parallel runs fs at once and waits for all of them.
func parallel(fs ...func()) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, f := range fs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			f()
		}()
	}
	close(start)
	wg.Wait()
}

// Review 1a (BK-12 under load): 20 wrong codes at once on one held order
// serialise on the order row: exactly 4 code_invalid (attempts left 4, 3, 2,
// 1, once each) and 16 code_locked, code_attempts stops at 5, the code is
// locked, and the right code is then refused with nothing sold.
func TestVerifyCodeParallelWrongCodes(t *testing.T) {
	o := newOrderShop(t)
	id := o.accepted()
	code := o.code(id)
	bad := wrongCode(code)

	const n = 20
	out := make([]resp, n)
	fs := make([]func(), n)
	for i := range fs {
		fs[i] = func() { out[i] = o.verify(o.cashier, id, bad) }
	}
	parallel(fs...)

	tally := map[string]int{}
	left := map[float64]int{}
	for _, r := range out {
		switch {
		case r.code == http.StatusUnprocessableEntity && problemCode(r) == "code_invalid":
			tally["code_invalid"]++
			f, _ := ext(r, "attemptsLeft").(float64)
			left[f]++
		case r.code == http.StatusConflict && problemCode(r) == "code_locked":
			tally["code_locked"]++
		default:
			t.Fatalf("a wrong code answered %d %q", r.code, problemCode(r))
		}
	}
	require.Equal(t, map[string]int{"code_invalid": 4, "code_locked": 16}, tally)
	require.Equal(t, map[float64]int{4: 1, 3: 1, 2: 1, 1: 1}, left, "each count was seen once")

	var attempts int
	var lockedAt *time.Time
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT code_attempts, code_locked_at FROM orders WHERE id = $1`, uuidOf(t, id)).Scan(&attempts, &lockedAt))
	require.Equal(t, 5, attempts, "attempts never pass the lock")
	require.NotNil(t, lockedAt, "the code is locked")

	r := o.verify(o.cashier, id, code)
	require.Equal(t, http.StatusConflict, r.code)
	require.Equal(t, "code_locked", problemCode(r), "a locked code refuses the right one")
	require.Equal(t, "accepted", o.status(id))
	require.Equal(t, 1, o.held(id))
	require.Zero(t, o.a.count(`SELECT count(*) FROM stock_movements WHERE source = 'sale_online' AND ref_id = $1`, uuidOf(t, id)))
}

// Review 1b (D-04, ux_orders_store_code_active): a drawn code an open order of
// the store holds is drawn again, on both paths. The check sees it (in_use);
// or another transaction takes it after the check, the insert hits the unique
// index, and only that attempt's savepoint rolls back (unique_violation). The
// order then has a different code and all its rows.
func TestOrderCodeCollisionRedraws(t *testing.T) {
	script := withCodeScript(t)
	o := newOrderShop(t)
	ctx := context.Background()

	// in_use: B draws A's code first.
	script.push(1111, 1111, 2222)
	a := o.order()
	require.Equal(t, o.codeHMAC("1111"), o.storedHMAC(a), "the scripted draw is the code")
	r := o.place(o.cust2, line{o.salt, 1})
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	b := r.body["id"].(string)
	require.Equal(t, o.codeHMAC("2222"), o.storedHMAC(b), "the taken code was drawn again")
	require.Equal(t, 1, strings.Count(o.logs.String(), `"reason":"in_use"`))

	// unique_violation: an open transaction gives A the code C draws first,
	// so C's check (committed rows) passes and its insert waits on the index.
	script.push(3333, 4444)
	tx, err := o.a.tdb.OwnerPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `UPDATE orders SET code_hmac = $1 WHERE id = $2`, o.codeHMAC("3333"), uuidOf(t, a))
	require.NoError(t, err)
	var txPID int
	require.NoError(t, tx.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&txPID))
	done := make(chan resp, 1)
	go func() { done <- o.place(o.cust2, line{o.atta, 1}) }()
	// Blocked by this transaction, not by any lock in the cluster: an
	// unrelated waiter let the commit run before C's insert, so C's check
	// saw the code (in_use) and the test flaked.
	require.Eventually(t, func() bool {
		return o.a.count(`SELECT count(*) FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid))`, txPID) > 0
	}, 10*time.Second, 10*time.Millisecond, "the insert waits on the open transaction")
	require.NoError(t, tx.Commit(ctx))
	r = <-done
	require.Equal(t, http.StatusCreated, r.code, r.raw)
	c := r.body["id"].(string)
	require.Equal(t, o.codeHMAC("4444"), o.storedHMAC(c), "the code taken mid-insert was drawn again")
	require.Equal(t, 1, strings.Count(o.logs.String(), `"reason":"unique_violation"`))
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_items WHERE order_id = $1`, uuidOf(t, c)), "only the attempt rolled back")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1`, uuidOf(t, c)))
	require.Equal(t, 3, o.a.count(`SELECT count(DISTINCT code_hmac) FROM orders WHERE store_id = $1 AND status = 'placed'`, uuidOf(t, o.sid)))
}

// Review 1c (BK-19's sibling): two accepts of one order at once with
// different Idempotency-Keys: one 200, one 409, and the stock holds the
// order's quantities once.
func TestDoubleAcceptParallel(t *testing.T) {
	o := newOrderShop(t)
	for range 4 {
		r := o.place(o.cust, line{o.butter, 2}, line{o.salt, 3})
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		id := r.body["id"].(string)
		_, butter0 := o.stock(o.butter)
		_, salt0 := o.stock(o.salt)

		var x, y resp
		parallel(
			func() { x = o.staff(o.cashier, id, "accept", map[string]any{}) },
			func() { y = o.staff(o.manager, id, "accept", map[string]any{}) },
		)
		codes := map[int]int{x.code: 1}
		codes[y.code]++
		require.Equal(t, map[int]int{http.StatusOK: 1, http.StatusConflict: 1}, codes, "%s | %s", x.raw, y.raw)
		loser := x
		if x.code == http.StatusOK {
			loser = y
		}
		require.Equal(t, "invalid_transition", problemCode(loser))

		_, butter1 := o.stock(o.butter)
		_, salt1 := o.stock(o.salt)
		require.Equal(t, 2, butter1-butter0, "butter reserved once")
		require.Equal(t, 3, salt1-salt0, "salt reserved once")
		require.Equal(t, 2, o.held(id))
		require.Equal(t, 1, o.a.count(`SELECT count(*) FROM order_events WHERE order_id = $1 AND to_status = 'accepted'`, uuidOf(t, id)))
		require.Equal(t, 1, o.a.count(`SELECT count(*) FROM river_job WHERE kind = $1 AND args->'a'->>'orderId' = $2`, orders.KindNoShow, id))
	}
	_, reserved := o.stock(o.butter)
	require.Equal(t, 8, reserved)
	_, reserved = o.stock(o.salt)
	require.Equal(t, 12, reserved)
}

// Review 3 (lock order): an end state's release and another order's accept
// on the same two items. Accept writes reservations in item id order, but
// the release is one UPDATE whose trigger moves store_items in the order it
// meets the rows; a row rewritten later (here a no-op update of the lower
// item's reservation, as a quantity change would) is met after the higher
// one. The release locks its items in id order first, as accept does, so the
// two never deadlock (a deadlock answers 500, SQLSTATE 40P01). Without that
// lock this test deadlocks.
func TestReleaseVersusAcceptNoDeadlock(t *testing.T) {
	o := newOrderShop(t)
	hi, lo := o.butter, o.salt
	if hi < lo { // canonical UUID text sorts as the uuid does
		hi, lo = lo, hi
	}
	const rounds = 3
	for range rounds {
		r := o.place(o.cust, line{hi, 1}, line{lo, 1})
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		p := r.body["id"].(string)
		r = o.staff(o.cashier, p, "accept", map[string]any{})
		require.Equal(t, http.StatusOK, r.code, r.raw)
		o.a.exec(`UPDATE stock_reservations SET status = status WHERE order_id = $1 AND store_item_id = $2`,
			uuidOf(t, p), uuidOf(t, lo))
		r = o.place(o.cust2, line{hi, 1}, line{lo, 1})
		require.Equal(t, http.StatusCreated, r.code, r.raw)
		q := r.body["id"].(string)

		// Force the bad interleaving: an outside transaction holds lo; the
		// accept queues on it first, then the cancel. When lo frees, the
		// accept holds it and wants hi; a release that took hi before lo
		// would now wait on lo, a cycle.
		ctx := context.Background()
		tx, err := o.a.tdb.OwnerPool.Begin(ctx)
		require.NoError(t, err)
		_, err = tx.Exec(ctx, `SELECT 1 FROM store_items WHERE id = $1 FOR UPDATE`, uuidOf(t, lo))
		require.NoError(t, err)
		waiting := func(n int) {
			require.Eventually(t, func() bool {
				return o.a.count(`SELECT count(*) FROM pg_locks WHERE NOT granted`) >= n
			}, 10*time.Second, 5*time.Millisecond)
		}
		var can, acc resp
		accDone, canDone := make(chan struct{}), make(chan struct{})
		go func() { defer close(accDone); acc = o.staff(o.cashier, q, "accept", map[string]any{}) }()
		waiting(1)
		go func() {
			defer close(canDone)
			can = o.me(o.cust, p, "cancel", map[string]any{"reason": "changed_mind"})
		}()
		waiting(2)
		require.NoError(t, tx.Commit(ctx))
		<-accDone
		<-canDone
		require.Equal(t, http.StatusOK, can.code, "cancel: %s", can.raw)
		require.Equal(t, http.StatusOK, acc.code, "accept: %s", acc.raw)
		require.Zero(t, o.held(p))
		require.Equal(t, 2, o.held(q))
	}
	for _, item := range []string{hi, lo} {
		onHand, reserved := o.stock(item)
		require.Equal(t, 500, onHand)
		require.Equal(t, rounds, reserved, "only the accepted orders hold stock")
	}
}

// Review 2: the store inbox's query (ListStoreOrders, a 3-status tab) on a
// busy store walks ix_orders_store_placed in order and stops at the page; no
// sort. The plan is logged for the review.
func TestStoreInboxUsesIndex(t *testing.T) {
	o := newOrderShop(t)
	a := o.order()
	ctx := context.Background()
	// 20k more placed orders of S, a second apart, copied from a.
	o.a.exec(`INSERT INTO orders (id, customer_id, store_id, fulfilment, subtotal_paise, items_after_offers_paise,
	    item_subtotal_paise, total_paise, code_enc, code_hmac, accept_by, placed_at)
	  SELECT gen_random_uuid(), customer_id, store_id, fulfilment, subtotal_paise, items_after_offers_paise,
	    item_subtotal_paise, total_paise, code_enc, sha256(int8send(g)), accept_by, placed_at - make_interval(secs => g)
	  FROM orders, generate_series(1, 20000) g WHERE id = $1`, uuidOf(t, a))
	o.a.exec(`ANALYZE orders`)

	rows, err := o.a.tdb.OwnerPool.Query(ctx, `EXPLAIN (COSTS OFF)
	SELECT o.id, o.display_number, o.status, o.status_reason, o.fulfilment, o.total_paise, o.placed_at,
	       o.accept_by, o.hold_until, o.version, o.seen_at, u.name AS customer_name,
	       (SELECT count(*) FROM order_items oi WHERE oi.order_id = o.id)::int AS item_count
	FROM orders o JOIN users u ON u.id = o.customer_id
	WHERE o.store_id = $1
	  AND o.status = ANY($2::text[])
	  AND ($3::bool IS NULL OR o.status <> 'delivery_failed'
	       OR (o.return_confirmed_at IS NOT NULL) = $3::bool)
	  AND ($4::timestamptz IS NULL
	       OR (o.placed_at, o.id) < ($4::timestamptz, $5::uuid))
	ORDER BY o.placed_at DESC, o.id DESC
	LIMIT $6`, uuidOf(t, o.sid), []string{"placed", "accepted", "ready"}, nil, nil, nil, 51)
	require.NoError(t, err)
	var plan []string
	for rows.Next() {
		var l string
		require.NoError(t, rows.Scan(&l))
		plan = append(plan, l)
	}
	require.NoError(t, rows.Err())
	text := strings.Join(plan, "\n")
	t.Logf("ListStoreOrders, 3 statuses, 20001 orders of S:\n%s", text)
	require.Contains(t, text, "ix_orders_store_placed")
	require.NotContains(t, text, "Sort", "the index gives the order")
}

// acceptedOf places lines and accepts them in full. Two orders with no item
// in common do not wait on each other's store_items locks, so only the
// lock under test can serialise them.
func (o *orderShop) acceptedOf(lines ...line) string {
	o.t.Helper()
	id := o.orderOf(lines...)
	r := o.staff(o.cashier, id, "accept", map[string]any{})
	require.Equal(o.t, http.StatusOK, r.code, r.raw)
	return id
}

// slowStrikes makes every strike of subject (customer or store) wait 300 ms
// after its insert, inside its transaction, so two strikes at once overlap
// between the insert and the count or recount that follows it. The write
// after the count (a customer's recount, a store's flag) waits 300 ms too,
// so the first is not committed when the second one counts.
func (o *orderShop) slowStrikes(subject string) {
	o.t.Helper()
	o.a.exec(`CREATE FUNCTION test_slow_strike() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN PERFORM pg_sleep(0.3); RETURN NULL; END $$`)
	o.a.exec(`CREATE TRIGGER test_slow_strike AFTER INSERT ON strikes
		FOR EACH ROW WHEN (NEW.subject_type = '` + subject + `') EXECUTE FUNCTION test_slow_strike()`)
	if subject == "customer" {
		o.a.exec(`CREATE TRIGGER test_slow_recount AFTER UPDATE OF strikes_count_30d ON customer_profiles
			FOR EACH ROW EXECUTE FUNCTION test_slow_strike()`)
	} else {
		o.a.exec(`CREATE TRIGGER test_slow_flag AFTER INSERT ON risk_flags
			FOR EACH ROW WHEN (NEW.rule = 'store_strikes') EXECUTE FUNCTION test_slow_strike()`)
	}
}

// Lead review 1 (BK-46 under load): with 4 store strikes, two admin cancels
// at once, each with the store at fault, both count 5 unless they serialise
// on the store; they do, so the store gets 6 strikes and one open flag.
func TestStoreStrikesAtOnceRaiseOneFlag(t *testing.T) {
	o := newOrderShop(t)
	desk := newAdminDesk(o, "support")
	tok := desk.token()
	cancel := func(id string) resp {
		return desk.cancel(tok, id, map[string]any{"reason": "store_request", "strike": "store"})
	}
	for range 4 {
		r := cancel(o.accepted())
		require.Equal(t, http.StatusOK, r.code, r.raw)
	}
	require.Zero(t, o.storeFlags())
	a, b := o.acceptedOf(line{o.butter, 1}), o.acceptedOf(line{o.salt, 1})
	o.slowStrikes("store")

	var x, y resp
	parallel(func() { x = cancel(a) }, func() { y = cancel(b) })
	require.Equal(t, http.StatusOK, x.code, x.raw)
	require.Equal(t, http.StatusOK, y.code, y.raw)
	require.Equal(t, 6, o.storeStrikes())
	require.Equal(t, 1, o.storeFlags(), "one flag, not one per cancel")
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM risk_flags WHERE rule = 'store_strikes' AND status = 'open'`))
}

// Lead review 2 (BK-16 under load): a customer with one no-show has two
// pickups whose holds end at the same time. Both timers at once each
// insert a strike and recount; unless they serialise on the customer, the
// second recount, blind to the first strike, writes a count of 2 and no
// limit. They do: the count is 3 and the customer is limited.
func TestNoShowsAtOnceCountEachOther(t *testing.T) {
	o := newOrderShop(t)
	o.noShow()
	require.Equal(t, 1, o.unlimited(1))
	a, b := o.acceptedOf(line{o.butter, 1}), o.acceptedOf(line{o.salt, 1})
	_, holdA := o.times(a)
	_, holdB := o.times(b)
	hold := *holdA
	if holdB.After(hold) {
		hold = *holdB
	}
	o.set(hold)
	o.slowStrikes("customer")

	run := func(id string, h *time.Time) func() (platformjobs.FakeRun, error) {
		oid, err := ids.Parse(id)
		require.NoError(t, err)
		return func() (platformjobs.FakeRun, error) {
			return o.runner.RunEvent(context.Background(), orders.KindNoShow, orders.TimerArgs{OrderID: oid, HoldUntil: h})
		}
	}
	ra, rb := run(a, holdA), run(b, holdB)
	var x, y platformjobs.FakeRun
	var xe, ye error
	parallel(func() { x, xe = ra() }, func() { y, ye = rb() })
	require.NoError(t, xe)
	require.NoError(t, ye)
	expiredRun(t, x)
	expiredRun(t, y)
	require.Equal(t, 3, o.strikes())
	require.Equal(t, 1, o.a.count(`SELECT count(*) FROM customer_profiles cp
	  WHERE cp.strikes_count_30d = 3 AND cp.limited_until IS NOT NULL
	    AND EXISTS (SELECT 1 FROM strikes s WHERE s.subject_id = cp.user_id)`), "count 3 and limited")
}
