package offers

import (
	"context"
	"sync"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// ErrUnavailable is Redeem's answer when an offer ran out (or was paused)
// between the quote and the redemption.
var ErrUnavailable = errUnavailableOffer{}

type errUnavailableOffer struct{}

func (errUnavailableOffer) Error() string { return "offers: an offer is no longer available" }

// Fake is an in-memory Pricer and Redeemer for tests of the callers (orders,
// POS, basket search): it runs the real engine over the offers it is given, so
// a caller's test sees the same prices the database-backed service gives.
type Fake struct {
	mu     sync.Mutex
	clock  clock.Clock
	offers []Offer
	cats   map[ids.ID][]ids.ID
	used   map[ids.ID]map[ids.ID]int // customer → offer → redemptions
	held   map[Ref][]Applied
}

// NewFake returns a Fake that reads time from c.
func NewFake(c clock.Clock) *Fake {
	return &Fake{clock: c, cats: map[ids.ID][]ids.ID{}, used: map[ids.ID]map[ids.ID]int{}, held: map[Ref][]Applied{}}
}

// Add puts an offer in force.
func (f *Fake) Add(o ...Offer) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.offers = append(f.offers, o...)
	return f
}

// Categorise says which category (and ancestors) a store item belongs to.
func (f *Fake) Categorise(item ids.ID, cats ...ids.ID) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cats[item] = cats
	return f
}

// Price implements Pricer.
func (f *Fake) Price(_ context.Context, cart Cart) (Quote, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	at := cart.At
	if at.IsZero() {
		at = f.clock.Now()
	}
	cats := make([][]ids.ID, len(cart.Lines))
	for i, l := range cart.Lines {
		cats[i] = f.cats[l.StoreItemID]
	}
	return compute(input{cart: cart, now: at, offers: f.offers, cats: cats, used: f.used[cart.CustomerID]}), nil
}

// Redeem implements Redeemer.
func (f *Fake) Redeem(_ context.Context, ref Ref, customerID ids.ID, q Quote) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, done := f.held[ref]; done {
		return nil
	}
	for _, a := range q.Applied {
		i := f.index(a.OfferID)
		if i < 0 || (f.offers[i].MaxRedemptions > 0 && f.offers[i].Redemptions >= f.offers[i].MaxRedemptions) {
			return ErrUnavailable
		}
	}
	for _, a := range q.Applied {
		f.offers[f.index(a.OfferID)].Redemptions++
		if f.used[customerID] == nil {
			f.used[customerID] = map[ids.ID]int{}
		}
		f.used[customerID][a.OfferID]++
	}
	f.held[ref] = append([]Applied(nil), q.Applied...)
	return nil
}

// Release implements Redeemer. The Fake does not remember which customer a
// ref belonged to for the per-customer count; it gives the offer's counter
// back, which is what the limit tests need.
func (f *Fake) Release(_ context.Context, ref Ref) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.held[ref] {
		if i := f.index(a.OfferID); i >= 0 && f.offers[i].Redemptions > 0 {
			f.offers[i].Redemptions--
		}
		for _, m := range f.used {
			if m[a.OfferID] > 0 {
				m[a.OfferID]--
				break
			}
		}
	}
	delete(f.held, ref)
	return nil
}

func (f *Fake) index(id ids.ID) int {
	for i := range f.offers {
		if f.offers[i].ID == id {
			return i
		}
	}
	return -1
}

var (
	_ Pricer   = (*Fake)(nil)
	_ Redeemer = (*Fake)(nil)
)
