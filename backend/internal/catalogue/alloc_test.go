package catalogue

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/ids"
)

// The D-99 allocation as pure functions (stock rule 8). The unit tests pin
// each order; the property test drives random sequences of every write
// through a model of the ledger, as Writer applies them, and checks the
// W1-05 row's invariants after each step.
//
// The plan names rapid for property tests (02 §1, 08 §1); the module is not
// in go.mod and adding it was not possible offline in this slice, so the
// sequences come from math/rand/v2 with fixed seeds (every failure prints
// its seed and step; reproducible, no shrinking). Switching to rapid is a
// mechanical change: the model below does not depend on the generator.

var (
	day0 = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) // an IST date as midnight UTC
	t0   = time.Date(2026, 10, 20, 5, 0, 0, 0, time.UTC)
)

func bid(n byte) ids.ID {
	var id ids.ID
	id[0], id[15] = 0x01, n
	return id
}

func exp(days int) *time.Time {
	d := day0.AddDate(0, 0, days)
	return &d
}

func takesOf(ts []take) map[ids.ID]int32 {
	m := map[ids.ID]int32{}
	for _, t := range ts {
		m[t.ID] += t.Qty
	}
	return m
}

func TestSaleTakeOrder(t *testing.T) {
	bs := []batch{
		{ID: bid(1), Qty: 3, Received: t0, Expires: exp(-1)},                 // expired
		{ID: bid(2), Qty: 4, Received: t0.Add(time.Hour)},                    // undated, newer
		{ID: bid(3), Qty: 2, Received: t0, Expires: exp(5)},                  // fresh, later
		{ID: bid(4), Qty: 1, Received: t0, Expires: exp(0)},                  // expires today: still fresh
		{ID: bid(5), Qty: 5, Received: t0},                                   // undated, older
		{ID: bid(6), Qty: 0, Received: t0, Expires: exp(1)},                  // sold out
		{ID: bid(7), Qty: 2, Received: t0.Add(-time.Hour), Expires: exp(-3)}, // expired earlier
	}
	// on hand 20, batches hold 17: 3 loose units come before expired stock.
	got := saleTake(bs, 20, 20, day0)
	require.Equal(t, []take{
		{bid(4), 1}, {bid(3), 2}, {bid(5), 5}, {bid(2), 4}, // fresh by expiry, undated by received
		{bid(7), 2}, {bid(1), 3}, // 3 loose taken without rows, then expired by expiry
	}, got)
	require.Equal(t, []take{{bid(4), 1}, {bid(3), 1}}, saleTake(bs, 2, 20, day0))
	// Loose stock covers the remainder: no expired batch is touched.
	require.Equal(t, []take{{bid(4), 1}, {bid(3), 2}, {bid(5), 5}, {bid(2), 4}}, saleTake(bs, 15, 20, day0))
	// With on_hand below the batches (cannot happen with the invariant, but
	// a sale is never refused) no loose stock is assumed.
	require.Equal(t, []take{{bid(4), 1}, {bid(3), 2}, {bid(5), 5}, {bid(2), 4}, {bid(7), 1}}, saleTake(bs, 13, 0, day0))
	// A sale beyond everything takes everything and records nothing more.
	require.Equal(t, int32(17), sumTake(saleTake(bs, 100, 17, day0)))
}

func sumTake(ts []take) int32 {
	var n int32
	for _, t := range ts {
		n += t.Qty
	}
	return n
}

func TestPlainTakeOrder(t *testing.T) {
	bs := []batch{
		{ID: bid(1), Qty: 3, Received: t0, Expires: exp(-1)},
		{ID: bid(2), Qty: 4, Received: t0.Add(time.Hour)},
		{ID: bid(3), Qty: 2, Received: t0, Expires: exp(5)},
		{ID: bid(5), Qty: 5, Received: t0},
	}
	require.Equal(t, []take{{bid(1), 3}, {bid(3), 2}, {bid(5), 1}}, plainTake(bs, 6))
	require.Equal(t, []take{{bid(1), 3}, {bid(3), 2}, {bid(5), 5}, {bid(2), 4}}, plainTake(bs, 50))
	require.Empty(t, plainTake(nil, 5))
}

func TestStockInRoom(t *testing.T) {
	require.Equal(t, int32(10), stockInRoom(10, 10, 0))
	require.Equal(t, int32(4), stockInRoom(10, 4, 0), "on hand was -6: only 4 may go into the batch")
	require.Equal(t, int32(0), stockInRoom(10, -2, 0))
	require.Equal(t, int32(3), stockInRoom(10, 8, 5))
	require.Equal(t, int32(0), stockInRoom(10, 8, 9))
}

func TestReverseRestore(t *testing.T) {
	order := []ids.ID{bid(1), bid(2), bid(3)}
	taken := map[ids.ID]int32{bid(1): 2, bid(2): 3, bid(3): 1}
	require.Equal(t, []take{{bid(1), 2}, {bid(2), 2}}, reverseRestore(order, taken, nil, 4, 100))
	require.Equal(t, []take{{bid(2), 3}, {bid(3), 1}}, reverseRestore(order, taken, map[ids.ID]int32{bid(1): 2}, 4, 100))
	require.Equal(t, []take{{bid(1), 2}}, reverseRestore(order, taken, nil, 4, 2), "room caps what goes back into batches")
	require.Empty(t, reverseRestore(order, taken, nil, 4, -3))
}

// --- the model ---------------------------------------------------------------

type mBatch struct {
	batch
	received int32
}

type mSale struct {
	taken    map[ids.ID]int32
	qty      int32
	reversed int32
	restored map[ids.ID]int32
}

type model struct {
	t       *testing.T
	r       *rand.Rand
	seed    uint64
	step    int
	today   time.Time
	now     time.Time
	onHand  int32
	batches []*mBatch
	next    byte
	nextHi  byte
	sales   []*mSale
}

func (m *model) fail(format string, args ...any) {
	m.t.Helper()
	m.t.Fatalf("seed %d step %d: %s", m.seed, m.step, fmt.Sprintf(format, args...))
}

func (m *model) view() []batch {
	out := make([]batch, 0, len(m.batches))
	for _, b := range m.batches {
		if b.Qty > 0 {
			out = append(out, b.batch)
		}
	}
	return out
}

func (m *model) find(id ids.ID) *mBatch {
	for _, b := range m.batches {
		if b.ID == id {
			return b
		}
	}
	m.fail("no batch %s", id)
	return nil
}

func (m *model) newID() ids.ID {
	m.next++
	if m.next == 0 {
		m.nextHi++
	}
	var id ids.ID
	id[0], id[14], id[15] = 0x01, m.nextHi, m.next
	return id
}

func (m *model) sumBatches() int64 {
	var n int64
	for _, b := range m.batches {
		n += int64(b.Qty)
	}
	return n
}

// apply moves the batches by signed takes (negative out, positive back).
func (m *model) apply(ts []take, sign int32) {
	for _, k := range ts {
		b := m.find(k.ID)
		b.Qty += sign * k.Qty
		if b.Qty < 0 {
			m.fail("batch %s below 0", k.ID)
		}
		if b.Qty > b.received {
			m.fail("batch %s above what it received", k.ID)
		}
	}
}

func (m *model) checkInvariants() {
	m.t.Helper()
	for _, b := range m.batches {
		if b.Qty < 0 {
			m.fail("batch below 0")
		}
	}
	if m.sumBatches() > max(int64(m.onHand), 0) {
		m.fail("Σ batch %d > max(on_hand %d, 0)", m.sumBatches(), m.onHand)
	}
}

// emptiedBefore checks that every batch ranked before a batch the movement
// took from was emptied by it: earlier stock is used up first.
func (m *model) emptiedBefore(before []batch, took map[ids.ID]int32, earlier func(a, b batch) bool, what string) {
	m.t.Helper()
	for _, x := range before {
		if took[x.ID] == 0 {
			continue
		}
		for _, y := range before {
			if y.ID != x.ID && earlier(y, x) && took[y.ID] < y.Qty {
				m.fail("%s took from %s while %s (earlier) kept %d", what, x.ID, y.ID, y.Qty-took[y.ID])
			}
		}
	}
}

func expiresBefore(a, b batch) bool {
	switch {
	case a.Expires == nil:
		return false
	case b.Expires == nil:
		return true
	default:
		return a.Expires.Before(*b.Expires)
	}
}

func (m *model) stockIn() {
	q := int32(1 + m.r.IntN(20))
	m.onHand += q
	var e *time.Time
	switch m.r.IntN(5) {
	case 0, 1, 2:
		e = exp(int(m.today.Sub(day0).Hours()/24) + m.r.IntN(14) - 3)
	case 3: // cost only
	default:
		return // no batch
	}
	room := stockInRoom(q, int64(m.onHand), m.sumBatches())
	m.batches = append(m.batches, &mBatch{batch: batch{ID: m.newID(), Qty: room, Received: m.now, Expires: e}, received: q})
}

func (m *model) sale() {
	q := int32(1 + m.r.IntN(25))
	before := m.view()
	takes := saleTake(before, q, m.onHand, m.today)
	took := takesOf(takes)
	if sumTake(takes) > q {
		m.fail("sale of %d took %d", q, sumTake(takes))
	}
	// Invariant 4: an expired batch only when no unexpired stock remains.
	var fresh, expired int64
	for _, b := range before {
		if b.expired(m.today) {
			expired += int64(took[b.ID])
		} else {
			fresh += int64(b.Qty)
		}
	}
	loose := max(int64(m.onHand), 0) - sumQty(before)
	if expired > 0 && fresh+max(loose, 0) >= int64(q) {
		m.fail("sale of %d used %d expired with %d unexpired and %d loose left", q, expired, fresh, loose)
	}
	if expired > 0 && expired > int64(q)-fresh-max(loose, 0) {
		m.fail("sale used more expired stock than it lacked")
	}
	m.emptiedBefore(before, took, func(a, b batch) bool {
		ca, cb := saleClass(a, m.today), saleClass(b, m.today)
		if ca != cb {
			return ca < cb
		}
		if ca == 1 {
			return a.Received.Before(b.Received)
		}
		return a.Expires.Before(*b.Expires)
	}, "sale")
	m.apply(takes, -1)
	m.onHand -= q
	m.sales = append(m.sales, &mSale{taken: took, qty: q, restored: map[ids.ID]int32{}})
}

func saleClass(b batch, today time.Time) int {
	switch {
	case b.Expires == nil:
		return 1
	case b.expired(today):
		return 2
	default:
		return 0
	}
}

func (m *model) reverse() {
	var open []*mSale
	for _, s := range m.sales {
		if s.reversed < s.qty {
			open = append(open, s)
		}
	}
	if len(open) == 0 {
		return
	}
	s := open[m.r.IntN(len(open))]
	q := int32(1 + m.r.IntN(int(s.qty-s.reversed)))
	var src []batch
	for id := range s.taken {
		src = append(src, m.find(id).batch)
	}
	slices.SortFunc(src, func(a, b batch) int { return a.ID.Compare(b.ID) })
	room := max(int64(m.onHand)+int64(q), 0) - m.sumBatches()
	puts := reverseRestore(restoreOrder(src, m.today), s.taken, s.restored, q, room)
	// Invariant 6: a reversal restores the same batches, never more than
	// each lost.
	for _, p := range puts {
		if s.taken[p.ID] == 0 {
			m.fail("reversal put %d into %s, which the sale did not take from", p.Qty, p.ID)
		}
		s.restored[p.ID] += p.Qty
		if s.restored[p.ID] > s.taken[p.ID] {
			m.fail("reversal restored %d to %s, the sale took %d", s.restored[p.ID], p.ID, s.taken[p.ID])
		}
	}
	// With room, a full reversal puts every unit back where it came from.
	var lost int32
	for id, n := range s.taken {
		lost += n - (s.restored[id] - takesOf(puts)[id])
	}
	if want := min(int64(q), int64(lost), max(room, 0)); int64(sumTake(puts)) != want {
		m.fail("reversal of %d restored %d, want %d", q, sumTake(puts), want)
	}
	m.apply(puts, +1)
	m.onHand += q
	s.reversed += q
}

func (m *model) plain(q int32, what string) {
	before := m.view()
	takes := plainTake(before, q)
	took := takesOf(takes)
	if int64(sumTake(takes)) != min(int64(q), sumQty(before)) {
		m.fail("%s of %d took %d from batches holding %d", what, q, sumTake(takes), sumQty(before))
	}
	// Invariant 5: earliest expiry first.
	m.emptiedBefore(before, took, expiresBefore, what)
	m.apply(takes, -1)
	m.onHand -= q
}

func (m *model) count() {
	counted := int32(m.r.IntN(40))
	d := counted - m.onHand
	switch {
	case d < 0:
		m.plain(-d, "count down")
	case d > 0:
		room := stockInRoom(d, int64(counted), m.sumBatches())
		m.onHand = counted
		if room > 0 {
			m.batches = append(m.batches, &mBatch{batch: batch{ID: m.newID(), Qty: room, Received: m.now}, received: room})
		}
	}
}

func (m *model) writeOff() {
	var pick []batch
	for _, b := range m.view() {
		if m.r.IntN(2) == 0 {
			pick = append(pick, b)
		}
	}
	left := int64(m.onHand)
	for _, b := range plainOrder(pick) {
		n := min(int64(b.Qty), left)
		if n <= 0 {
			continue
		}
		m.apply([]take{{b.ID, int32(n)}}, -1)
		m.onHand -= int32(n)
		left -= n
	}
	if left < 0 && len(pick) > 0 && m.onHand < 0 {
		m.fail("write-off drove on hand below 0")
	}
}

func TestAllocationProperties(t *testing.T) {
	runs := 10_000
	if testing.Short() {
		runs = 1_000
	}
	for seed := uint64(1); seed <= uint64(runs); seed++ { //nolint:gosec // small
		m := &model{t: t, r: rand.New(rand.NewPCG(seed, 0x5eed)), seed: seed, today: day0, now: t0}
		steps := 10 + m.r.IntN(50)
		for m.step = 0; m.step < steps; m.step++ {
			switch k := m.r.IntN(100); {
			case k < 25:
				m.stockIn()
			case k < 55:
				m.sale()
			case k < 65:
				m.reverse()
			case k < 75:
				m.plain(int32(1+m.r.IntN(15)), "damage")
			case k < 83:
				m.count()
			case k < 90:
				m.writeOff()
			default:
				days := m.r.IntN(3)
				m.today = m.today.AddDate(0, 0, days)
				m.now = m.now.Add(time.Duration(days)*24*time.Hour + time.Minute)
			}
			m.checkInvariants()
		}
	}
}
