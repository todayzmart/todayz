package catalogue

import (
	"bytes"
	"math"
	"slices"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// The D-99 allocation (03 stock rule 8), as pure functions over the item's
// batches so the property tests can drive them without a database. Writer
// applies the result inside the transaction.
//
// Every function keeps the invariant the trigger checks cannot see on their
// own: Σ batch quantity ≤ max(on_hand, 0) after the movement, given it held
// before.

// batch is one stock batch with remaining stock, as the allocator sees it.
type batch struct {
	ID       ids.ID
	Qty      int32
	Received time.Time
	// Expires is the IST calendar date as midnight UTC; nil is undated.
	Expires *time.Time
}

// take is one allocation row: the quantity a movement takes from (or puts
// back into) a batch, always positive here; Writer signs it.
type take struct {
	ID  ids.ID
	Qty int32
}

func sumQty(bs []batch) int64 {
	var n int64
	for _, b := range bs {
		n += int64(b.Qty)
	}
	return n
}

// expired is the "expired on shelf" test: expires_on before today (IST).
func (b batch) expired(today time.Time) bool { return b.Expires != nil && b.Expires.Before(today) }

func byReceived(a, b batch) int {
	if c := a.Received.Compare(b.Received); c != 0 {
		return c
	}
	return bytes.Compare(a.ID[:], b.ID[:])
}

func byExpiry(a, b batch) int {
	if c := a.Expires.Compare(*b.Expires); c != 0 {
		return c
	}
	return byReceived(a, b)
}

// plainOrder is the order damage, an expired write-off without batches and a
// count down use: dated batches by expiry, then undated, ties by oldest
// received and then id.
func plainOrder(bs []batch) []batch {
	var dated, undated []batch
	for _, b := range bs {
		if b.Qty <= 0 {
			continue
		}
		if b.Expires != nil {
			dated = append(dated, b)
		} else {
			undated = append(undated, b)
		}
	}
	slices.SortFunc(dated, byExpiry)
	slices.SortFunc(undated, byReceived)
	return append(dated, undated...)
}

// takeFrom takes up to qty from bs in order; it returns the rows and what is
// left of qty.
func takeFrom(bs []batch, qty int64) ([]take, int64) {
	var out []take
	for _, b := range bs {
		if qty <= 0 {
			break
		}
		n := min(int64(b.Qty), qty)
		if n <= 0 {
			continue
		}
		out = append(out, take{ID: b.ID, Qty: i32(n)}) // n ≤ b.Qty
		qty -= n
	}
	return out, qty
}

// saleTake is what a sale of qty takes from the batches (rule 8): unexpired
// dated batches by expiry, then undated by received, then stock in no batch
// (max(onHand, 0) − Σ batch, which records nothing), then expired batches by
// expiry. onHand is before the sale. A sale is never refused: what no batch
// and no loose stock covers drives on_hand below 0 and records nothing.
func saleTake(bs []batch, qty int32, onHand int32, today time.Time) []take {
	var fresh, undated, old []batch
	for _, b := range bs {
		switch {
		case b.Qty <= 0:
		case b.Expires == nil:
			undated = append(undated, b)
		case b.expired(today):
			old = append(old, b)
		default:
			fresh = append(fresh, b)
		}
	}
	slices.SortFunc(fresh, byExpiry)
	slices.SortFunc(undated, byReceived)
	slices.SortFunc(old, byExpiry)

	out, left := takeFrom(append(fresh, undated...), int64(qty))
	if left <= 0 {
		return out
	}
	loose := max(int64(onHand), 0) - sumQty(bs)
	if loose > 0 {
		left -= min(loose, left)
	}
	more, _ := takeFrom(old, left)
	return append(out, more...)
}

// plainTake is what damage, an expired write-off without batches or a count
// down of qty takes (rule 8's plain order); the rest is stock in no batch.
func plainTake(bs []batch, qty int32) []take {
	out, _ := takeFrom(plainOrder(bs), int64(qty))
	return out
}

// stockInRoom is how much of a stock in may go into a new batch: the stock
// the batches may hold after the movement, max(newOnHand, 0) − Σ batch,
// never below 0 and never above qty.
func stockInRoom(qty int32, newOnHand int64, inBatches int64) int32 {
	room := max(newOnHand, 0) - inBatches
	return i32(max(min(room, int64(qty)), 0)) // within 0..qty
}

// reverseRestore is what a reversal of qty puts back into the batches the
// source sale took from (rule 5). taken is the source's allocation (positive
// quantities), restored what earlier reversals already put back, room the
// stock the batches may still take (max(newOnHand, 0) − Σ batch). It walks
// the source batches in order (the sale order recomputed now, so the batch
// the sale would take first gets its stock back first) and puts back at most
// what each lost and still lacks.
func reverseRestore(order []ids.ID, taken, restored map[ids.ID]int32, qty int32, room int64) []take {
	var out []take
	left := min(int64(qty), max(room, 0))
	for _, id := range order {
		if left <= 0 {
			break
		}
		n := min(int64(taken[id]-restored[id]), left)
		if n <= 0 {
			continue
		}
		out = append(out, take{ID: id, Qty: i32(n)}) // n ≤ taken
		left -= n
	}
	return out
}

// restoreOrder orders the source's batches for a reversal: the sale order of
// today (unexpired dated by expiry, undated, expired), so a returned unit
// lands where the next sale would take it from first.
func restoreOrder(bs []batch, today time.Time) []ids.ID {
	var fresh, undated, old []batch
	for _, b := range bs {
		switch {
		case b.Expires == nil:
			undated = append(undated, b)
		case b.expired(today):
			old = append(old, b)
		default:
			fresh = append(fresh, b)
		}
	}
	slices.SortFunc(fresh, byExpiry)
	slices.SortFunc(undated, byReceived)
	slices.SortFunc(old, byExpiry)
	out := make([]ids.ID, 0, len(bs))
	for _, b := range slices.Concat(fresh, undated, old) {
		out = append(out, b.ID)
	}
	return out
}

// i32 converts a quantity the caller has already bounded (a min with an
// int32, or a contract maximum of 1,000,000). It saturates rather than wraps
// if that bound were ever broken, so a stock count can never flip sign.
func i32(n int64) int32 {
	switch {
	case n > math.MaxInt32:
		return math.MaxInt32
	case n < math.MinInt32:
		return math.MinInt32
	}
	return int32(n)
}

// i32i is i32 for a contract integer.
func i32i(n int) int32 { return i32(int64(n)) }
