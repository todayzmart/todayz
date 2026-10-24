package credits

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/creditsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Deps is what the service needs. Only Clock is required: without the
// database or settings every operation answers ErrUnavailable (503 on the
// routes).
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	// Notices writes "credits earned" and "credits expiring" to the
	// customer's inbox in the posting's transaction; nil sends nothing
	// (the reminder job then refuses to run).
	Notices notice.Sender
}

// Service is the database ledger: Holder, Poster, the account-deletion hook,
// the expiry, reminder and reconcile jobs and the wallet reads.
//
// Lock order (every operation, so two never wait on each other in a cycle):
// the customer and store account_balances rows in account id order (one
// LockBalances call per operation), then the hold row (LockHold), then the
// customer's lots. The platform accounts are not locked up front: their
// entries are inserted last in each posting, so the balance trigger takes
// their row lock after every other lock is held.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	notices  notice.Sender
	ids      *ids.Generator
}

var (
	_ Holder = (*Service)(nil)
	_ Poster = (*Service)(nil)
)

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("credits: a clock is required")
	}
	return &Service{db: d.DB, clock: d.Clock, settings: d.Settings, notices: d.Notices, ids: ids.NewGenerator()}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "credits: not configured (needs the database and settings)")
		return ErrUnavailable
	}
	return nil
}

// now is the clock's time at the database's precision.
func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *creditsdb.Queries { return creditsdb.New(s.db.Q(ctx)) }

func (s *Service) newID(now time.Time) pgtype.UUID { return ids.UUID(s.ids.New(now)) }

// Rules reads the credit settings in force.
func (s *Service) Rules(ctx context.Context) (Rules, error) {
	if err := s.ready(ctx); err != nil {
		return Rules{}, err
	}
	n := func(k settings.Key) (int, error) {
		v, err := s.settings.Int(ctx, k)
		if err != nil {
			return 0, fmt.Errorf("credits: %s: %w", k, err)
		}
		return int(min(max(v, 0), math.MaxInt32)), nil
	}
	var r Rules
	var err error
	if r.MaxPerOrder, err = n(settings.CreditMaxPerOrder); err != nil {
		return Rules{}, err
	}
	if r.ExpiryDays, err = n(settings.CreditExpiryDays); err != nil {
		return Rules{}, err
	}
	if r.OrdersPerMonth, err = n(settings.CreditOrdersPerMonth); err != nil {
		return Rules{}, err
	}
	if r.StoreRedemptionsPerDay, err = n(settings.StoreCreditRedemptionsPerDay); err != nil {
		return Rules{}, err
	}
	if r.ReminderDays, err = n(settings.CreditExpiryReminderDays); err != nil {
		return Rules{}, err
	}
	p, err := s.settings.Paise(ctx, settings.CreditMinOrderValuePaise)
	if err != nil {
		return Rules{}, fmt.Errorf("credits: %s: %w", settings.CreditMinOrderValuePaise, err)
	}
	r.MinOrderPaise = int64(p)
	return r, nil
}

// --- accounts ------------------------------------------------------------------

// platformIDs are the four platform accounts migration 20101 created.
var platformIDs = map[AccountKind]pgtype.UUID{
	PlatformMarketing:   mustUUID("01930000-0000-7000-8000-000000000001"),
	PlatformExpired:     mustUUID("01930000-0000-7000-8000-000000000002"),
	PlatformAdjustments: mustUUID("01930000-0000-7000-8000-000000000003"),
	PlatformPayoutsCash: mustUUID("01930000-0000-7000-8000-000000000004"),
}

func mustUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		panic(err)
	}
	return u
}

func ownerType(k AccountKind) string {
	if k == StoreCredit {
		return "store"
	}
	return "customer"
}

// accountID returns the account's id, creating a customer or store account
// on first use.
func (s *Service) accountID(ctx context.Context, q *creditsdb.Queries, a Account, now time.Time) (pgtype.UUID, error) {
	if a.Kind.platform() {
		return platformIDs[a.Kind], nil
	}
	p := creditsdb.AccountIDParams{Kind: string(a.Kind), OwnerID: ids.UUID(a.Owner)}
	id, err := q.AccountID(ctx, p)
	if err == nil {
		return id, nil
	}
	if !db.IsNoRows(err) {
		return pgtype.UUID{}, fmt.Errorf("credits: account: %w", err)
	}
	if err := q.InsertAccount(ctx, creditsdb.InsertAccountParams{
		Now: now, ID: s.newID(now), Kind: string(a.Kind), OwnerType: ownerType(a.Kind), OwnerID: p.OwnerID,
	}); err != nil {
		return pgtype.UUID{}, fmt.Errorf("credits: new account: %w", err)
	}
	if id, err = q.AccountID(ctx, p); err != nil {
		return pgtype.UUID{}, fmt.Errorf("credits: account: %w", err)
	}
	return id, nil
}

// locked is an account whose balance row this transaction holds.
type locked struct {
	ID      pgtype.UUID
	Balance int64
}

// lock creates (on first use) and locks the customer and store accounts in
// one statement, in account id order. Platform accounts are skipped.
func (s *Service) lock(ctx context.Context, q *creditsdb.Queries, now time.Time, accts ...Account) (map[Account]locked, error) {
	out := make(map[Account]locked, len(accts))
	byID := map[pgtype.UUID]Account{}
	var list []pgtype.UUID
	for _, a := range accts {
		if a.Kind.platform() {
			continue
		}
		if _, ok := out[a]; ok {
			continue
		}
		id, err := s.accountID(ctx, q, a, now)
		if err != nil {
			return nil, err
		}
		out[a] = locked{ID: id}
		byID[id] = a
		list = append(list, id)
	}
	rows, err := q.LockBalances(ctx, list)
	if err != nil {
		return nil, fmt.Errorf("credits: lock balances: %w", err)
	}
	if len(rows) != len(list) {
		return nil, errors.New("credits: an account has no balance row")
	}
	for _, r := range rows {
		a := byID[r.AccountID]
		out[a] = locked{ID: r.AccountID, Balance: r.Balance}
	}
	return out, nil
}

// --- postings ------------------------------------------------------------------

// entry is one leg resolved to an account id.
type entry struct {
	Account  pgtype.UUID
	Platform bool
	Amount   int64
}

// txSpec is one posting.
type txSpec struct {
	Kind      TxKind
	Ref       Ref    // for the package's own postings
	RefType   string // for Poster postings (Ref wins when set)
	RefID     ids.ID
	Key       string
	CreatedBy ids.ID
	Memo      string
	Entries   []entry
}

// post writes one transaction and its entries. A key that posted before
// writes nothing and returns that transaction with dup true.
func (s *Service) post(ctx context.Context, q *creditsdb.Queries, now time.Time, t txSpec) (pgtype.UUID, bool, error) {
	p := creditsdb.InsertTransactionParams{
		ID: s.newID(now), Kind: string(t.Kind), IdempotencyKey: t.Key, Now: now,
	}
	switch {
	case t.Ref.valid():
		rt := string(t.Ref.Type)
		p.RefType, p.RefID = &rt, ids.UUID(t.Ref.ID)
	case t.RefType != "":
		rt := t.RefType
		p.RefType, p.RefID = &rt, ids.UUID(t.RefID)
	}
	if t.CreatedBy != ids.Zero {
		p.CreatedBy = ids.UUID(t.CreatedBy)
	}
	if t.Memo != "" {
		m := t.Memo
		p.Memo = &m
	}
	txID, err := q.InsertTransaction(ctx, p)
	if db.IsNoRows(err) {
		id, err := q.TransactionByKey(ctx, t.Key)
		if err != nil {
			return pgtype.UUID{}, false, fmt.Errorf("credits: posted key: %w", err)
		}
		return id, true, nil
	}
	if err != nil {
		return pgtype.UUID{}, false, fmt.Errorf("credits: insert transaction: %w", err)
	}
	es := append([]entry(nil), t.Entries...)
	// Platform entries last: their balance rows are the hot ones.
	sort.SliceStable(es, func(i, j int) bool { return !es[i].Platform && es[j].Platform })
	for _, e := range es {
		if err := q.InsertEntry(ctx, creditsdb.InsertEntryParams{
			ID: s.newID(now), TransactionID: txID, AccountID: e.Account, Amount: e.Amount, Now: now,
		}); err != nil {
			return pgtype.UUID{}, false, fmt.Errorf("credits: insert entry: %w", err)
		}
	}
	return txID, false, nil
}

func (s *Service) consume(ctx context.Context, q *creditsdb.Queries, now time.Time, lot, txID, holdID pgtype.UUID, kind string, rem, hld int) error {
	if err := q.InsertConsumption(ctx, creditsdb.InsertConsumptionParams{
		ID: s.newID(now), LotID: lot, TransactionID: txID, HoldID: holdID, Kind: kind,
		RemainingDelta: i32(rem), HeldDelta: i32(hld), Now: now,
	}); err != nil {
		return fmt.Errorf("credits: lot %s: %w", kind, err)
	}
	return nil
}

func (s *Service) newLot(ctx context.Context, q *creditsdb.Queries, now time.Time, customer ids.ID, wallet, txID pgtype.UUID, source TxKind, n, days int) error {
	if err := q.InsertLot(ctx, creditsdb.InsertLotParams{
		ID: s.newID(now), CustomerID: ids.UUID(customer), CustomerAccountID: wallet, SourceTransactionID: txID,
		SourceKind: string(source), Original: i32(n), Now: now, ExpiresAt: expiresAt(now, days),
	}); err != nil {
		return fmt.Errorf("credits: new lot: %w", err)
	}
	return nil
}

// expireDue expires the customer's due lots, one expiry posting per
// expires_at (the inline part of the expiry job). The caller holds the
// customer's wallet row.
func (s *Service) expireDue(ctx context.Context, q *creditsdb.Queries, now time.Time, customer ids.ID, wallet pgtype.UUID) (int, error) {
	lots, err := q.DueLots(ctx, creditsdb.DueLotsParams{CustomerID: ids.UUID(customer), Now: now})
	if err != nil || len(lots) == 0 {
		return 0, wrap("due lots", err)
	}
	total := 0
	all := make([]pgtype.UUID, 0, len(lots))
	for i := 0; i < len(lots); {
		j, n := i, 0
		for ; j < len(lots) && lots[j].ExpiresAt.Equal(lots[i].ExpiresAt); j++ {
			n += int(lots[j].Remaining)
			all = append(all, lots[j].ID)
		}
		if n > 0 {
			key := fmt.Sprintf("expiry:%s:%d", customer, lots[i].ExpiresAt.Unix())
			txID, dup, err := s.post(ctx, q, now, txSpec{Kind: TxExpiry, Key: key, Entries: []entry{
				{Account: wallet, Amount: -int64(n)}, {Account: platformIDs[PlatformExpired], Platform: true, Amount: int64(n)},
			}})
			if err != nil {
				return 0, err
			}
			if !dup {
				for _, l := range lots[i:j] {
					if l.Remaining > 0 {
						if err := s.consume(ctx, q, now, l.ID, txID, pgtype.UUID{}, "expiry", -int(l.Remaining), 0); err != nil {
							return 0, err
						}
					}
				}
				total += n
			}
		}
		i = j
	}
	if err := q.MarkLotsExpired(ctx, creditsdb.MarkLotsExpiredParams{Now: &now, Ids: all}); err != nil {
		return 0, wrap("mark expired", err)
	}
	return total, nil
}

func wrap(what string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("credits: %s: %w", what, err)
}

// --- Holder ----------------------------------------------------------------------

// Allow implements Holder. It reads the balance as of now (a due lot no
// longer counts, whether or not the expiry job has run) and writes nothing.
func (s *Service) Allow(ctx context.Context, customerID, storeID ids.ID, requested int, eligiblePaise int64) (Allowance, error) {
	r, err := s.Rules(ctx)
	if err != nil {
		return Allowance{}, err
	}
	q, now := s.q(ctx), s.now()
	avail, err := q.SpendableCredits(ctx, creditsdb.SpendableCreditsParams{CustomerID: ids.UUID(customerID), Now: now})
	if err != nil {
		return Allowance{}, wrap("available", err)
	}
	u, err := s.usage(ctx, q, customerID, storeID, now)
	if err != nil {
		return Allowance{}, err
	}
	return allow(r, requested, eligiblePaise, int(avail), u), nil
}

func (s *Service) usage(ctx context.Context, q *creditsdb.Queries, customerID, storeID ids.ID, now time.Time) (usage, error) {
	row, err := q.CapUsage(ctx, creditsdb.CapUsageParams{
		CustomerID: ids.UUID(customerID), MonthStart: clock.StartOfMonthIST(now), Now: now,
		StoreID: ids.UUID(storeID), DayStart: clock.StartOfDayIST(now),
	})
	if err != nil {
		return usage{}, wrap("cap usage", err)
	}
	return usage{CustomerMonth: int(row.CustomerMonth), StoreDay: int(row.StoreDay)}, nil
}

func (s *Service) holdByRef(ctx context.Context, q *creditsdb.Queries, ref Ref) (creditsdb.CreditHold, bool, error) {
	var h creditsdb.CreditHold
	var err error
	if ref.Type == RefOrder {
		h, err = q.HoldByOrder(ctx, ids.UUID(ref.ID))
	} else {
		h, err = q.HoldByBillClaim(ctx, ids.UUID(ref.ID))
	}
	if db.IsNoRows(err) {
		return h, false, nil
	}
	if err != nil {
		return h, false, wrap("hold", err)
	}
	return h, true, nil
}

// Hold implements Holder.
func (s *Service) Hold(ctx context.Context, req HoldRequest) (Allowance, error) {
	if !req.Ref.valid() || req.Credits <= 0 || req.CustomerID == ids.Zero || req.StoreID == ids.Zero {
		return Allowance{}, ErrInvalid
	}
	r, err := s.Rules(ctx)
	if err != nil {
		return Allowance{}, err
	}
	c := req.CustomerID
	var out Allowance
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		acc, err := s.lock(ctx, q, now, wallet(c), held(c))
		if err != nil {
			return err
		}
		if h, ok, err := s.holdByRef(ctx, q, req.Ref); err != nil {
			return err
		} else if ok {
			out = Allowance{Allowed: int(h.Amount - h.Released), Max: int(h.Amount)}
			return nil
		}
		if _, err := s.expireDue(ctx, q, now, c, acc[wallet(c)].ID); err != nil {
			return err
		}
		avail, err := q.SpendableCredits(ctx, creditsdb.SpendableCreditsParams{CustomerID: ids.UUID(c), Now: now})
		if err != nil {
			return wrap("available", err)
		}
		u, err := s.usage(ctx, q, c, req.StoreID, now)
		if err != nil {
			return err
		}
		a := allow(r, req.Credits, req.EligiblePaise, int(avail), u)
		if a.Allowed < req.Credits {
			a.Allowed = 0
			out = a
			return nil
		}
		rows, err := q.SpendableLots(ctx, ids.UUID(c))
		if err != nil {
			return wrap("lots", err)
		}
		ps, ok := takeFIFO(lotStates(rows), req.Credits, now)
		if !ok {
			return errors.New("credits: lots short of the spendable balance")
		}
		txID, dup, err := s.post(ctx, q, now, txSpec{Kind: TxSpendHold, Ref: req.Ref, Key: req.Ref.key("hold"), Entries: []entry{
			{Account: acc[wallet(c)].ID, Amount: -int64(req.Credits)}, {Account: acc[held(c)].ID, Amount: int64(req.Credits)},
		}})
		if err != nil {
			return err
		}
		if dup {
			return errors.New("credits: hold posted without a hold row")
		}
		holdID := s.newID(now)
		hp := creditsdb.InsertHoldParams{ID: holdID, CustomerID: ids.UUID(c), StoreID: ids.UUID(req.StoreID), Amount: i32(req.Credits), Now: now}
		if req.Ref.Type == RefOrder {
			hp.OrderID = ids.UUID(req.Ref.ID)
		} else {
			hp.BillClaimID = ids.UUID(req.Ref.ID)
		}
		if err := q.InsertHold(ctx, hp); err != nil {
			return wrap("insert hold", err)
		}
		for _, p := range ps {
			if err := s.consume(ctx, q, now, ids.UUID(p.Lot), txID, holdID, "hold", -p.N, p.N); err != nil {
				return err
			}
		}
		out = a
		return nil
	})
	if err != nil {
		return Allowance{}, err
	}
	if rule := capRule(out.Limit); rule != "" {
		countCapRefused(ctx, out.Limit)
		s.flag(ctx, rule, req)
	}
	return out, nil
}

// flag raises a risk flag for a cap refusal (01 §7, D-30) on the pool, so it
// survives the caller rolling the checkout back. A failure is logged, not
// returned: the refusal stands either way.
func (s *Service) flag(ctx context.Context, rule string, req HoldRequest) {
	subjectType, subject := "user", req.CustomerID
	if rule == capRule(LimitStoreCap) {
		subjectType, subject = "store", req.StoreID
	}
	details, _ := json.Marshal(map[string]string{"ref_type": string(req.Ref.Type), "ref_id": req.Ref.ID.String()})
	now := s.now()
	// TODO(lead): one flag per refused hold; the plan does not say whether
	// repeats in a day should fold into one row. Score 0: no number given.
	if err := creditsdb.New(s.db.Pool).InsertRiskFlag(context.WithoutCancel(ctx), creditsdb.InsertRiskFlagParams{
		ID: s.newID(now), SubjectType: subjectType, SubjectID: ids.UUID(subject), Rule: rule, Details: details, Now: now,
	}); err != nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "credits: risk flag not written", "rule", rule, "err", err)
	}
}

func lotStates(rows []creditsdb.SpendableLotsRow) []lotState {
	out := make([]lotState, 0, len(rows))
	for _, r := range rows {
		id, _ := ids.FromUUID(r.ID)
		out = append(out, lotState{ID: id, Remaining: int(r.Remaining), ExpiresAt: r.ExpiresAt})
	}
	return out
}

// openHold locks the customer's accounts and the hold, in that order, and
// returns the hold as it is under the lock. ok is false for no hold or one
// that is no longer held.
func (s *Service) openHold(ctx context.Context, q *creditsdb.Queries, now time.Time, ref Ref) (creditsdb.CreditHold, map[Account]locked, bool, error) {
	h, ok, err := s.holdByRef(ctx, q, ref)
	if err != nil || !ok || h.Status != holdHeld {
		return h, nil, false, err
	}
	c, _ := ids.FromUUID(h.CustomerID)
	acc, err := s.lock(ctx, q, now, wallet(c), held(c))
	if err != nil {
		return h, nil, false, err
	}
	if h, err = q.LockHold(ctx, h.ID); err != nil {
		return h, nil, false, wrap("lock hold", err)
	}
	return h, acc, h.Status == holdHeld, nil
}

func (s *Service) portions(ctx context.Context, q *creditsdb.Queries, h creditsdb.CreditHold) ([]portion, error) {
	rows, err := q.HoldPortions(ctx, h.ID)
	if err != nil {
		return nil, wrap("hold portions", err)
	}
	ps := make([]portion, 0, len(rows))
	for _, r := range rows {
		id, _ := ids.FromUUID(r.LotID)
		ps = append(ps, portion{Lot: id, N: int(r.N), ExpiresAt: r.ExpiresAt})
	}
	if sumPortions(ps) != int(h.Amount-h.Released) {
		return nil, errors.New("credits: hold portions do not match the hold")
	}
	return ps, nil
}

// release returns ps of hold h to their lots, or to platform_expired for
// lots that are due (D-27), and records n released on the hold.
func (s *Service) release(ctx context.Context, q *creditsdb.Queries, now time.Time, h creditsdb.CreditHold, acc map[Account]locked, ref Ref, ps []portion, key string, final bool) error {
	c, _ := ids.FromUUID(h.CustomerID)
	back, expired, backN, expiredN := splitDue(ps, now)
	es := []entry{{Account: acc[held(c)].ID, Amount: -int64(backN + expiredN)}}
	if backN > 0 {
		es = append(es, entry{Account: acc[wallet(c)].ID, Amount: int64(backN)})
	}
	if expiredN > 0 {
		es = append(es, entry{Account: platformIDs[PlatformExpired], Platform: true, Amount: int64(expiredN)})
	}
	txID, dup, err := s.post(ctx, q, now, txSpec{Kind: TxSpendRelease, Ref: ref, Key: key, Entries: es})
	if err != nil {
		return err
	}
	if dup {
		return errors.New("credits: release posted before for an open hold")
	}
	for _, p := range back {
		if err := s.consume(ctx, q, now, ids.UUID(p.Lot), txID, h.ID, "release", p.N, -p.N); err != nil {
			return err
		}
	}
	for _, p := range expired {
		if err := s.consume(ctx, q, now, ids.UUID(p.Lot), txID, h.ID, "release", 0, -p.N); err != nil {
			return err
		}
	}
	return wrap("hold released", q.AddHoldReleased(ctx, creditsdb.AddHoldReleasedParams{
		N: i32(backN + expiredN), Final: final, Now: now, ID: h.ID,
	}))
}

// Release implements Holder.
func (s *Service) Release(ctx context.Context, ref Ref) (int, error) {
	if !ref.valid() {
		return 0, ErrInvalid
	}
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	n := 0
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		h, acc, ok, err := s.openHold(ctx, q, now, ref)
		if err != nil || !ok {
			return err
		}
		ps, err := s.portions(ctx, q, h)
		if err != nil {
			return err
		}
		if err := s.release(ctx, q, now, h, acc, ref, ps, ref.key("release"), true); err != nil {
			return err
		}
		n = int(h.Amount - h.Released)
		return nil
	})
	return n, err
}

// Recap implements Holder.
func (s *Service) Recap(ctx context.Context, ref Ref, eligiblePaise int64) (int, error) {
	if !ref.valid() {
		return 0, ErrInvalid
	}
	r, err := s.Rules(ctx)
	if err != nil {
		return 0, err
	}
	kept := 0
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		h, acc, ok, err := s.openHold(ctx, q, now, ref)
		if err != nil || !ok {
			return err
		}
		open := int(h.Amount - h.Released)
		to := keep(r, open, eligiblePaise)
		if to == open {
			kept = open
			return nil
		}
		ps, err := s.portions(ctx, q, h)
		if err != nil {
			return err
		}
		sel, ok := takeLatest(ps, open-to)
		if !ok {
			return errors.New("credits: hold portions short")
		}
		if err := s.release(ctx, q, now, h, acc, ref, sel, fmt.Sprintf("%s:%d", ref.key("recap"), to), to == 0); err != nil {
			return err
		}
		kept = to
		return nil
	})
	return kept, err
}

// Complete implements Holder.
func (s *Service) Complete(ctx context.Context, c Completion) (Completed, error) {
	if !c.Ref.valid() || c.CustomerID == ids.Zero || c.StoreID == ids.Zero || c.EligiblePaise < 0 {
		return Completed{}, ErrInvalid
	}
	r, err := s.Rules(ctx)
	if err != nil {
		return Completed{}, err
	}
	var out Completed
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		bp, err := q.StoreCashbackBP(ctx, ids.UUID(c.StoreID))
		if db.IsNoRows(err) {
			return ErrInvalid
		} else if err != nil {
			return wrap("cashback rate", err)
		}
		h, hasHold, err := s.holdByRef(ctx, q, c.Ref)
		if err != nil {
			return err
		}
		if hasHold && (h.CustomerID != ids.UUID(c.CustomerID) || h.StoreID != ids.UUID(c.StoreID)) {
			return ErrInvalid
		}
		if !hasHold && bp <= 0 {
			return nil
		}
		acc, err := s.lock(ctx, q, now, held(c.CustomerID), wallet(c.CustomerID), store(c.StoreID))
		if err != nil {
			return err
		}
		if hasHold {
			if h, err = q.LockHold(ctx, h.ID); err != nil {
				return wrap("lock hold", err)
			}
			out.Captured = int(h.Captured)
			if h.Status == holdHeld {
				if out.Captured, err = s.capture(ctx, q, now, h, acc, c); err != nil {
					return err
				}
			}
		}
		cbKey := c.Ref.key("cashback")
		if _, err := q.TransactionByKey(ctx, cbKey); err == nil {
			n, err := q.KeyAmount(ctx, creditsdb.KeyAmountParams{IdempotencyKey: cbKey, Kind: string(CustomerWallet)})
			out.Cashback = int(n)
			return wrap("cashback paid", err)
		} else if !db.IsNoRows(err) {
			return wrap("cashback key", err)
		}
		cb := cashback(c.EligiblePaise-int64(out.Captured)*paisePerCredit, int(bp))
		if cb <= 0 {
			return nil
		}
		w := acc[wallet(c.CustomerID)].ID
		txID, _, err := s.post(ctx, q, now, txSpec{Kind: TxCashback, Ref: c.Ref, Key: cbKey, Memo: "bp:" + strconv.Itoa(int(bp)), Entries: []entry{
			{Account: acc[store(c.StoreID)].ID, Amount: -int64(cb)}, {Account: w, Amount: int64(cb)},
		}})
		if err != nil {
			return err
		}
		if err := s.newLot(ctx, q, now, c.CustomerID, w, txID, TxCashback, cb, r.ExpiryDays); err != nil {
			return err
		}
		out.Cashback = cb
		return s.notifyEarned(ctx, c.CustomerID, cb, c.Ref)
	})
	if err != nil {
		return Completed{}, err
	}
	return out, nil
}

// capture moves the open hold to the store.
func (s *Service) capture(ctx context.Context, q *creditsdb.Queries, now time.Time, h creditsdb.CreditHold, acc map[Account]locked, c Completion) (int, error) {
	n := int(h.Amount - h.Released)
	ps, err := s.portions(ctx, q, h)
	if err != nil {
		return 0, err
	}
	txID, dup, err := s.post(ctx, q, now, txSpec{Kind: TxSpendCapture, Ref: c.Ref, Key: c.Ref.key("capture"), Entries: []entry{
		{Account: acc[held(c.CustomerID)].ID, Amount: -int64(n)}, {Account: acc[store(c.StoreID)].ID, Amount: int64(n)},
	}})
	if err != nil {
		return 0, err
	}
	if dup {
		return 0, errors.New("credits: capture posted before for an open hold")
	}
	for _, p := range ps {
		if err := s.consume(ctx, q, now, ids.UUID(p.Lot), txID, h.ID, "capture", 0, -p.N); err != nil {
			return 0, err
		}
	}
	if err := q.CaptureHold(ctx, creditsdb.CaptureHoldParams{Captured: i32(n), Now: &now, ID: h.ID}); err != nil {
		return 0, wrap("capture hold", err)
	}
	return n, nil
}

func (s *Service) notifyEarned(ctx context.Context, customer ids.ID, n int, ref Ref) error {
	if s.notices == nil {
		return nil
	}
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return errors.New("credits: no transaction in context")
	}
	_, err := s.notices.Send(ctx, tx, notice.Notification{
		Kind: notice.KindCreditsEarned, UserID: customer,
		Params: map[string]string{"credits": strconv.Itoa(n)},
		Ref:    &notice.Ref{Type: string(ref.Type), ID: ref.ID},
	})
	return wrap("credits earned notice", err)
}

// --- Poster ----------------------------------------------------------------------

// Post implements Poster.
func (s *Service) Post(ctx context.Context, p Posting) (Posted, error) {
	if err := validatePosting(p); err != nil {
		return Posted{}, err
	}
	for _, l := range p.Legs {
		if l.Amount > math.MaxInt32 || l.Amount < -math.MaxInt32 {
			return Posted{}, fmt.Errorf("%w: amount too large", ErrInvalid)
		}
	}
	r, err := s.Rules(ctx)
	if err != nil {
		return Posted{}, err
	}
	var out Posted
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		accts := make([]Account, 0, len(p.Legs))
		for _, l := range p.Legs {
			accts = append(accts, l.Account)
		}
		acc, err := s.lock(ctx, q, now, accts...)
		if err != nil {
			return err
		}
		if id, err := q.TransactionByKey(ctx, p.IdempotencyKey); err == nil {
			out = Posted{TransactionID: mustID(id), Duplicate: true}
			return nil
		} else if !db.IsNoRows(err) {
			return wrap("posted key", err)
		}
		type debit struct {
			ps []portion
		}
		var debits []debit
		es := make([]entry, 0, len(p.Legs))
		for _, l := range p.Legs {
			id := acc[l.Account].ID
			if l.Account.Kind.platform() {
				id = platformIDs[l.Account.Kind]
			}
			es = append(es, entry{Account: id, Platform: l.Account.Kind.platform(), Amount: l.Amount})
			if l.Account.Kind != CustomerWallet || l.Amount > 0 {
				continue
			}
			c := l.Account.Owner
			if _, err := s.expireDue(ctx, q, now, c, id); err != nil {
				return err
			}
			rows, err := q.SpendableLots(ctx, ids.UUID(c))
			if err != nil {
				return wrap("lots", err)
			}
			ps, ok := takeFIFO(lotStates(rows), int(-l.Amount), now)
			if !ok {
				return ErrInsufficient
			}
			debits = append(debits, debit{ps})
		}
		txID, dup, err := s.post(ctx, q, now, txSpec{
			Kind: p.Kind, RefType: p.RefType, RefID: p.RefID, Key: p.IdempotencyKey,
			CreatedBy: p.CreatedBy, Memo: p.Memo, Entries: es,
		})
		if err != nil {
			return err
		}
		out = Posted{TransactionID: mustID(txID), Duplicate: dup}
		if dup {
			return nil
		}
		for _, d := range debits {
			for _, pt := range d.ps {
				if err := s.consume(ctx, q, now, ids.UUID(pt.Lot), txID, pgtype.UUID{}, "adjustment", -pt.N, 0); err != nil {
					return err
				}
			}
		}
		for _, l := range p.Legs {
			if l.Account.Kind == CustomerWallet && l.Amount > 0 {
				if err := s.newLot(ctx, q, now, l.Account.Owner, acc[l.Account].ID, txID, p.Kind, int(l.Amount), r.ExpiryDays); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return Posted{}, err
	}
	return out, nil
}

// --- account deletion ------------------------------------------------------------

// ErrHeld: the account still has credits held for an unfinished order or
// bill claim, so it cannot be deleted yet.
var ErrHeld = errors.New("credits: credits are still held")

// OnAccountDelete forfeits the customer's available credits to
// platform_expired (forfeit_deletion, D-31) and closes every lot. It runs in
// the deletion's transaction (identity's deletion hook). A repeat posts
// nothing.
func (s *Service) OnAccountDelete(ctx context.Context, userID ids.ID) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		if _, err := q.AccountID(ctx, creditsdb.AccountIDParams{Kind: string(CustomerWallet), OwnerID: ids.UUID(userID)}); db.IsNoRows(err) {
			return nil
		} else if err != nil {
			return wrap("account", err)
		}
		acc, err := s.lock(ctx, q, now, wallet(userID), held(userID))
		if err != nil {
			return err
		}
		if acc[held(userID)].Balance != 0 {
			// TODO(lead): orders refuses deletion with an unfinished order
			// (D-31), so nothing should be held here; refusing is the
			// conservative reading for a hold W2-04's bill claims may add.
			return ErrHeld
		}
		lots, err := q.SpendableLots(ctx, ids.UUID(userID))
		if err != nil {
			return wrap("lots", err)
		}
		if n := acc[wallet(userID)].Balance; n > 0 {
			txID, dup, err := s.post(ctx, q, now, txSpec{Kind: TxForfeitDeletion, Key: "forfeit:" + userID.String(), Entries: []entry{
				{Account: acc[wallet(userID)].ID, Amount: -n}, {Account: platformIDs[PlatformExpired], Platform: true, Amount: n},
			}})
			if err != nil {
				return err
			}
			if !dup {
				for _, l := range lots {
					if l.Remaining > 0 {
						if err := s.consume(ctx, q, now, l.ID, txID, pgtype.UUID{}, "forfeit", -int(l.Remaining), 0); err != nil {
							return err
						}
					}
				}
			}
		}
		open, err := q.OpenLotIDs(ctx, ids.UUID(userID))
		if err != nil {
			return wrap("open lots", err)
		}
		return wrap("close lots", q.MarkLotsExpired(ctx, creditsdb.MarkLotsExpiredParams{Now: &now, Ids: open}))
	})
}

// --- helpers ---------------------------------------------------------------------

func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

func i32(n int) int32 { return int32(min(max(n, math.MinInt32), math.MaxInt32)) } //nolint:gosec // clamped

// asProblem maps the package's errors to problems for the routes.
func asProblem(err error) error {
	var p *problem.Error
	switch {
	case errors.As(err, &p):
		return p
	case errors.Is(err, ErrUnavailable):
		return problem.Wrap(genproblems.ServiceUnavailable, err)
	}
	return problem.Wrap(genproblems.Internal, err)
}
