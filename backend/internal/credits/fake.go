package credits

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// Fake is an in-memory ledger that implements Holder and Poster with the
// same rules as the database service. Packages that build on credits test
// against it; the property tests drive it with random sequences (CR-13)
// and check the invariants with Check.
type Fake struct {
	Clock clock.Clock
	Rules Rules
	// CashbackBP is each store's cashback rate (stores.cashback_bp).
	CashbackBP map[ids.ID]int

	mu       sync.Mutex
	gen      *ids.Generator
	balances map[Account]int64
	txs      map[string]*fakeTx
	order    []*fakeTx
	holds    map[Ref]*fakeHold
	lots     []*fakeLot
	attempts int // posts attempted, repeats included
	flags    []string
}

type fakeTx struct {
	ID   ids.ID
	Kind TxKind
	Key  string
	Legs []Leg
}

type fakeHold struct {
	Ref                        Ref
	Customer, Store            ids.ID
	Amount, Captured, Released int
	Status                     string
	CreatedAt                  time.Time
	// portions is the net held per lot (Σ held_delta of the hold's rows).
	portions map[ids.ID]int
	cashback int
	settled  bool
}

type fakeLot struct {
	ID, Customer         ids.ID
	Source               TxKind
	Original             int
	Remaining, Held      int
	EarnedAt, ExpiresAt  time.Time
	Expired              bool
	remDelta, heldDelta int // Σ of the lot's consumptions (invariant 8)
}

// NewFake returns an empty ledger with the rules' defaults as given.
func NewFake(c clock.Clock, r Rules) *Fake {
	return &Fake{Clock: c, Rules: r, CashbackBP: map[ids.ID]int{}}
}

func (f *Fake) init() {
	if f.gen == nil {
		f.gen = ids.NewGenerator()
		f.balances = map[Account]int64{}
		f.txs = map[string]*fakeTx{}
		f.holds = map[Ref]*fakeHold{}
		if f.CashbackBP == nil {
			f.CashbackBP = map[ids.ID]int{}
		}
	}
}

func (f *Fake) now() time.Time { return f.Clock.Now().UTC() }

// Balance is an account's balance.
func (f *Fake) Balance(a Account) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	return f.balances[a]
}

// Flags are the risk-flag rules raised so far.
func (f *Fake) Flags() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.flags...)
}

// post records one transaction; a repeated key posts nothing.
func (f *Fake) post(kind TxKind, key string, legs []Leg) (tx *fakeTx, dup bool, err error) {
	f.attempts++
	if t, ok := f.txs[key]; ok {
		return t, true, nil
	}
	var sum int64
	for _, l := range legs {
		if l.Amount == 0 {
			return nil, false, fmt.Errorf("%w: zero leg", ErrInvalid)
		}
		sum += l.Amount
	}
	if sum != 0 {
		return nil, false, fmt.Errorf("%w: legs sum to %d", ErrInvalid, sum)
	}
	for _, l := range legs {
		if (l.Account.Kind == CustomerWallet || l.Account.Kind == CustomerHeld) && f.balances[l.Account]+l.Amount < 0 {
			return nil, false, ErrInsufficient
		}
	}
	for _, l := range legs {
		f.balances[l.Account] += l.Amount
	}
	t := &fakeTx{ID: f.gen.New(f.now()), Kind: kind, Key: key, Legs: legs}
	f.txs[key] = t
	f.order = append(f.order, t)
	return t, false, nil
}

func wallet(c ids.ID) Account { return Account{Kind: CustomerWallet, Owner: c} }
func held(c ids.ID) Account   { return Account{Kind: CustomerHeld, Owner: c} }
func store(s ids.ID) Account  { return Account{Kind: StoreCredit, Owner: s} }

var expiredAcct = Account{Kind: PlatformExpired}

func (f *Fake) customerLots(c ids.ID) []lotState {
	var out []lotState
	for _, l := range f.lots {
		if l.Customer == c {
			out = append(out, lotState{ID: l.ID, Remaining: l.Remaining, ExpiresAt: l.ExpiresAt})
		}
	}
	return out
}

func (f *Fake) lot(id ids.ID) *fakeLot {
	for _, l := range f.lots {
		if l.ID == id {
			return l
		}
	}
	panic("credits fake: no lot " + id.String())
}

func (f *Fake) consume(l *fakeLot, rem, hld int) {
	l.Remaining += rem
	l.Held += hld
	l.remDelta += rem
	l.heldDelta += hld
}

// expireCustomer expires the customer's due lots (the inline part of the
// expiry job), one posting per expires_at.
func (f *Fake) expireCustomer(c ids.ID, now time.Time) error {
	groups := map[time.Time][]*fakeLot{}
	for _, l := range f.lots {
		if l.Customer == c && !l.Expired && due(l.ExpiresAt, now) {
			groups[l.ExpiresAt] = append(groups[l.ExpiresAt], l)
		}
	}
	for exp, ls := range groups {
		n := 0
		for _, l := range ls {
			n += l.Remaining
		}
		if n > 0 {
			key := fmt.Sprintf("expiry:%s:%d", c, exp.Unix())
			if _, dup, err := f.post(TxExpiry, key, []Leg{{wallet(c), -int64(n)}, {expiredAcct, int64(n)}}); err != nil {
				return err
			} else if !dup {
				for _, l := range ls {
					if l.Remaining > 0 {
						f.consume(l, -l.Remaining, 0)
					}
				}
			}
		}
		for _, l := range ls {
			l.Expired = true
		}
	}
	return nil
}

// Expire runs the expiry job over every customer.
func (f *Fake) Expire(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	seen := map[ids.ID]bool{}
	for _, l := range f.lots {
		if !seen[l.Customer] {
			seen[l.Customer] = true
			if err := f.expireCustomer(l.Customer, f.now()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *Fake) usage(c, s ids.ID, now time.Time) usage {
	var u usage
	day, month := clock.StartOfDayIST(now), clock.StartOfMonthIST(now)
	for _, h := range f.holds {
		if h.Status == holdReleased || h.CreatedAt.After(now) {
			continue
		}
		if h.Customer == c && !h.CreatedAt.Before(month) {
			u.CustomerMonth++
		}
		if h.Store == s && !h.CreatedAt.Before(day) {
			u.StoreDay++
		}
	}
	return u
}

func (f *Fake) available(c ids.ID, now time.Time) int {
	n := 0
	for _, l := range f.lots {
		if l.Customer == c && !due(l.ExpiresAt, now) {
			n += l.Remaining
		}
	}
	return n
}

// Allow implements Holder.
func (f *Fake) Allow(_ context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (Allowance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	now := f.now()
	return allow(f.Rules, requested, eligiblePaise, f.available(customerID, now), f.usage(customerID, storeID, now)), nil
}

// Hold implements Holder.
func (f *Fake) Hold(_ context.Context, req HoldRequest) (Allowance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	if !req.Ref.valid() || req.Credits <= 0 {
		return Allowance{}, ErrInvalid
	}
	if h, ok := f.holds[req.Ref]; ok {
		return Allowance{Allowed: h.Amount - h.Released, Max: h.Amount}, nil
	}
	now := f.now()
	if err := f.expireCustomer(req.CustomerID, now); err != nil {
		return Allowance{}, err
	}
	a := allow(f.Rules, req.Credits, req.EligiblePaise, f.available(req.CustomerID, now), f.usage(req.CustomerID, req.StoreID, now))
	if a.Allowed < req.Credits {
		if rule := capRule(a.Limit); rule != "" {
			f.flags = append(f.flags, rule)
		}
		a.Allowed = 0
		return a, nil
	}
	ps, ok := takeFIFO(f.customerLots(req.CustomerID), req.Credits, now)
	if !ok {
		return Allowance{}, errors.New("credits fake: lots short of the balance")
	}
	if _, _, err := f.post(TxSpendHold, req.Ref.key("hold"), []Leg{
		{wallet(req.CustomerID), -int64(req.Credits)}, {held(req.CustomerID), int64(req.Credits)},
	}); err != nil {
		return Allowance{}, err
	}
	h := &fakeHold{Ref: req.Ref, Customer: req.CustomerID, Store: req.StoreID, Amount: req.Credits,
		Status: holdHeld, CreatedAt: now, portions: map[ids.ID]int{}}
	for _, p := range ps {
		f.consume(f.lot(p.Lot), -p.N, p.N)
		h.portions[p.Lot] += p.N
	}
	f.holds[req.Ref] = h
	return a, nil
}

func (f *Fake) heldPortions(h *fakeHold) []portion {
	var ps []portion
	for id, n := range h.portions {
		if n > 0 {
			ps = append(ps, portion{Lot: id, N: n, ExpiresAt: f.lot(id).ExpiresAt})
		}
	}
	return ps
}

// release returns ps of hold h: to their lots, or to platform_expired for
// due lots.
func (f *Fake) release(h *fakeHold, ps []portion, key string) error {
	back, expired, backN, expiredN := splitDue(ps, f.now())
	legs := []Leg{{held(h.Customer), -int64(backN + expiredN)}}
	if backN > 0 {
		legs = append(legs, Leg{wallet(h.Customer), int64(backN)})
	}
	if expiredN > 0 {
		legs = append(legs, Leg{expiredAcct, int64(expiredN)})
	}
	if _, dup, err := f.post(TxSpendRelease, key, legs); err != nil || dup {
		return err
	}
	for _, p := range back {
		f.consume(f.lot(p.Lot), p.N, -p.N)
		h.portions[p.Lot] -= p.N
	}
	for _, p := range expired {
		f.consume(f.lot(p.Lot), 0, -p.N)
		h.portions[p.Lot] -= p.N
	}
	h.Released += backN + expiredN
	return nil
}

// Reduce implements Holder.
func (f *Fake) Reduce(_ context.Context, ref Ref, to int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	h, ok := f.holds[ref]
	if !ok || h.Status != holdHeld || to < 0 {
		return 0, nil
	}
	open := h.Amount - h.Released
	if to >= open {
		return 0, nil
	}
	ps, ok := takeLatest(f.heldPortions(h), open-to)
	if !ok {
		return 0, errors.New("credits fake: hold portions short")
	}
	if err := f.release(h, ps, fmt.Sprintf("%s:%d", ref.key("reduce"), to)); err != nil {
		return 0, err
	}
	if to == 0 {
		h.Status = holdReleased
	}
	return open - to, nil
}

// Release implements Holder.
func (f *Fake) Release(_ context.Context, ref Ref) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	h, ok := f.holds[ref]
	if !ok || h.Status != holdHeld {
		return 0, nil
	}
	open := h.Amount - h.Released
	if err := f.release(h, f.heldPortions(h), ref.key("release")); err != nil {
		return 0, err
	}
	h.Status = holdReleased
	return open, nil
}

// Complete implements Holder.
func (f *Fake) Complete(_ context.Context, c Completion) (Completed, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	if !c.Ref.valid() {
		return Completed{}, ErrInvalid
	}
	var out Completed
	h, ok := f.holds[c.Ref]
	if ok && h.settled {
		return Completed{Captured: h.Captured, Cashback: h.cashback}, nil
	}
	if ok && h.Status == holdHeld {
		n := h.Amount - h.Released
		if _, _, err := f.post(TxSpendCapture, c.Ref.key("capture"), []Leg{
			{held(h.Customer), -int64(n)}, {store(h.Store), int64(n)},
		}); err != nil {
			return Completed{}, err
		}
		for _, p := range f.heldPortions(h) {
			f.consume(f.lot(p.Lot), 0, -p.N)
			h.portions[p.Lot] = 0
		}
		h.Captured, h.Status = n, holdCaptured
	}
	if ok {
		out.Captured = h.Captured
	}
	cb := cashback(c.EligiblePaise-int64(out.Captured)*paisePerCredit, f.CashbackBP[c.StoreID])
	if cb > 0 {
		_, dup, err := f.post(TxCashback, c.Ref.key("cashback"), []Leg{
			{store(c.StoreID), -int64(cb)}, {wallet(c.CustomerID), int64(cb)},
		})
		if err != nil {
			return Completed{}, err
		}
		if !dup {
			f.addLot(c.CustomerID, TxCashback, cb)
		}
	}
	out.Cashback = cb
	if !ok {
		h = &fakeHold{Ref: c.Ref, Customer: c.CustomerID, Store: c.StoreID, Status: holdNone}
		f.holds[c.Ref] = h
	}
	h.settled, h.cashback = true, cb
	return out, nil
}

func (f *Fake) addLot(c ids.ID, kind TxKind, n int) {
	now := f.now()
	f.lots = append(f.lots, &fakeLot{ID: f.gen.New(now), Customer: c, Source: kind, Original: n,
		Remaining: n, EarnedAt: now, ExpiresAt: expiresAt(now, f.Rules.ExpiryDays)})
}

// Post implements Poster.
func (f *Fake) Post(_ context.Context, p Posting) (Posted, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	if err := validatePosting(p); err != nil {
		return Posted{}, err
	}
	if t, ok := f.txs[p.IdempotencyKey]; ok {
		f.attempts++
		return Posted{TransactionID: t.ID, Duplicate: true}, nil
	}
	now := f.now()
	type debit struct {
		c  ids.ID
		ps []portion
	}
	var debits []debit
	for _, l := range p.Legs {
		if l.Account.Kind == CustomerWallet && l.Amount < 0 {
			if err := f.expireCustomer(l.Account.Owner, now); err != nil {
				return Posted{}, err
			}
			ps, ok := takeFIFO(f.customerLots(l.Account.Owner), int(-l.Amount), now)
			if !ok {
				return Posted{}, ErrInsufficient
			}
			debits = append(debits, debit{l.Account.Owner, ps})
		}
	}
	tx, _, err := f.post(p.Kind, p.IdempotencyKey, p.Legs)
	if err != nil {
		return Posted{}, err
	}
	for _, d := range debits {
		for _, pt := range d.ps {
			f.consume(f.lot(pt.Lot), -pt.N, 0)
		}
	}
	for _, l := range p.Legs {
		if l.Account.Kind == CustomerWallet && l.Amount > 0 {
			f.addLot(l.Account.Owner, p.Kind, int(l.Amount))
		}
	}
	return Posted{TransactionID: tx.ID}, nil
}

// OnAccountDelete forfeits the customer's available credits (D-31).
func (f *Fake) OnAccountDelete(_ context.Context, userID ids.ID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	n := f.balances[wallet(userID)]
	if n > 0 {
		if _, dup, err := f.post(TxForfeitDeletion, "forfeit:"+userID.String(), []Leg{
			{wallet(userID), -n}, {expiredAcct, n},
		}); err != nil || dup {
			return err
		}
	}
	for _, l := range f.lots {
		if l.Customer == userID {
			if l.Remaining > 0 {
				f.consume(l, -l.Remaining, 0)
			}
			l.Expired = true
		}
	}
	return nil
}

// Check verifies the ledger invariants (03 §6): 1 balanced transactions,
// 2 no negative wallet or held, 3 lots match wallet + held per customer,
// 4 every settled hold is captured or released consistently, 6 one posting
// per key, 7 balances equal Σ entries, 8 lots equal Σ their consumptions.
func (f *Fake) Check() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.init()
	sums := map[Account]int64{}
	keys := map[string]bool{}
	for _, t := range f.order {
		var s int64
		for _, l := range t.Legs {
			s += l.Amount
			sums[l.Account] += l.Amount
		}
		if s != 0 {
			return fmt.Errorf("invariant 1: %s sums to %d", t.Key, s)
		}
		if keys[t.Key] {
			return fmt.Errorf("invariant 6: %s posted twice", t.Key)
		}
		keys[t.Key] = true
	}
	for a, b := range f.balances {
		if sums[a] != b {
			return fmt.Errorf("invariant 7: %v cache %d, entries %d", a, b, sums[a])
		}
		if (a.Kind == CustomerWallet || a.Kind == CustomerHeld) && b < 0 {
			return fmt.Errorf("invariant 2: %v is %d", a, b)
		}
	}
	perCustomer := map[ids.ID]int64{}
	for _, l := range f.lots {
		if l.Remaining < 0 || l.Held < 0 || l.Remaining+l.Held > l.Original {
			return fmt.Errorf("lot %s out of range: %d+%d of %d", l.ID, l.Remaining, l.Held, l.Original)
		}
		if l.Remaining != l.Original+l.remDelta || l.Held != l.heldDelta {
			return fmt.Errorf("invariant 8: lot %s", l.ID)
		}
		perCustomer[l.Customer] += int64(l.Remaining + l.Held)
	}
	for a := range f.balances {
		if a.Kind == CustomerWallet {
			perCustomer[a.Owner] += 0
		}
	}
	for c, n := range perCustomer {
		if got := f.balances[wallet(c)] + f.balances[held(c)]; got != n {
			return fmt.Errorf("invariant 3: customer %s lots %d, wallet+held %d", c, n, got)
		}
	}
	for _, h := range f.holds {
		switch h.Status {
		case holdNone:
		case holdHeld:
			if h.Captured != 0 || h.Released >= h.Amount {
				return fmt.Errorf("invariant 4: held hold %v", h.Ref)
			}
		case holdCaptured:
			if h.Captured <= 0 || h.Captured+h.Released != h.Amount {
				return fmt.Errorf("invariant 4: captured hold %v", h.Ref)
			}
		case holdReleased:
			if h.Captured != 0 || h.Released != h.Amount {
				return fmt.Errorf("invariant 4: released hold %v", h.Ref)
			}
		default:
			return fmt.Errorf("invariant 4: hold %v status %q", h.Ref, h.Status)
		}
	}
	return nil
}

// Hold statuses (credit_holds.status); holdNone marks a completion without
// a hold in the Fake only.
const (
	holdHeld     = "held"
	holdCaptured = "captured"
	holdReleased = "released"
	holdNone     = ""
)

// capRule is the risk-flag rule a cap refusal raises.
func capRule(l Limit) string {
	switch l {
	case LimitStoreCap:
		return "credits.store_daily_cap"
	case LimitMonthlyCap:
		return "credits.customer_monthly_cap"
	}
	return ""
}

// validatePosting checks a Poster posting's shape.
func validatePosting(p Posting) error {
	switch p.Kind {
	case TxReferralReward, TxAdminAdjustment, TxPayout:
	default:
		return fmt.Errorf("%w: kind %q is not posted through Post", ErrInvalid, p.Kind)
	}
	if p.IdempotencyKey == "" || len(p.IdempotencyKey) > 200 {
		return fmt.Errorf("%w: idempotency key", ErrInvalid)
	}
	if (p.RefType == "") != (p.RefID == ids.Zero) {
		return fmt.Errorf("%w: ref type and id go together", ErrInvalid)
	}
	if len(p.Legs) < 2 {
		return fmt.Errorf("%w: a posting has two legs or more", ErrInvalid)
	}
	seen := map[Account]bool{}
	var sum int64
	for _, l := range p.Legs {
		a := l.Account
		if l.Amount == 0 || seen[a] {
			return fmt.Errorf("%w: zero or repeated leg", ErrInvalid)
		}
		seen[a] = true
		sum += l.Amount
		switch {
		case a.Kind == CustomerHeld:
			return fmt.Errorf("%w: customer_held moves only with holds", ErrInvalid)
		case a.Kind.platform():
			if a.Owner != ids.Zero {
				return fmt.Errorf("%w: a platform account has no owner", ErrInvalid)
			}
		case a.Kind == CustomerWallet || a.Kind == StoreCredit:
			if a.Owner == ids.Zero {
				return fmt.Errorf("%w: %s needs an owner", ErrInvalid, a.Kind)
			}
		default:
			return fmt.Errorf("%w: account kind %q", ErrInvalid, a.Kind)
		}
		if !legAllowed(p.Kind, a.Kind, l.Amount) {
			return fmt.Errorf("%w: %s cannot move %s by %d", ErrInvalid, p.Kind, a.Kind, l.Amount)
		}
	}
	if sum != 0 {
		return fmt.Errorf("%w: legs sum to %d", ErrInvalid, sum)
	}
	return nil
}

// legAllowed is which accounts each Poster kind may move (03 §6 postings).
func legAllowed(k TxKind, a AccountKind, amount int64) bool {
	switch k {
	case TxReferralReward:
		// platform_marketing pays; wallets and store accounts receive.
		return (a == PlatformMarketing && amount < 0) || ((a == CustomerWallet || a == StoreCredit) && amount > 0)
	case TxAdminAdjustment:
		return a == PlatformAdjustments || a == CustomerWallet || a == StoreCredit
	case TxPayout:
		return (a == StoreCredit && amount < 0) || (a == PlatformPayoutsCash && amount > 0)
	}
	return false
}
