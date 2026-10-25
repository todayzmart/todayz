package pos

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	syncapi "github.com/todayz/backend/gen/api/sync"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// The sync's fixed limits (sync.yaml, 02 §4.3, D-36).
const (
	maxEntries = 500
	maxChanges = 500
	// changesKept is how long catalogue changes are kept, and how long a
	// device may go without syncing before it must take a new snapshot.
	changesKept = 30 * 24 * time.Hour //settings:ok D-36 keeps catalogue changes 30 days; no platform setting exists
)

// ist is India Standard Time, for the per-day risk counts (01 §17 counts
// days in IST).
var ist = time.FixedZone("IST", 5*3600+1800)

// rejected is an entry that is not applied; its savepoint is rolled back.
type rejected struct{ reason syncapi.SyncRejection }

func (r rejected) Error() string { return "pos: sync entry rejected: " + string(r.reason) }

func reject(r syncapi.SyncRejection) error { return rejected{reason: r} }

// syncRun is one sync's context.
type syncRun struct {
	sc      authz.Scope
	p       authn.Principal
	device  ids.ID
	real    bool
	st      posdb.StoreInfoRow
	now     time.Time
	offline int // counter-code links made offline by this sync
	// offers are this sync's sales with a device offer discount the offers
	// engine would not give at sold_at; noRole those of them rung by a user
	// without pos.discount (D-131).
	offers, noRole int
}

// Sync applies a device's outbox in deviceSeq order and returns the
// catalogue changes after its cursor (POST {S}/sync). One transaction; each
// entry in its own savepoint so a rejected entry leaves nothing behind.
func (s *Service) Sync(ctx context.Context, sc authz.Scope, body syncapi.SyncRequest) (syncapi.SyncResponse, error) {
	if err := s.readyToSell(ctx); err != nil {
		return syncapi.SyncResponse{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return syncapi.SyncResponse{}, err
	}
	if len(body.Entries) > maxEntries {
		return syncapi.SyncResponse{}, fieldErr("body.entries", "count")
	}
	if body.Cursor < 0 {
		return syncapi.SyncResponse{}, fieldErr("body.cursor", "range")
	}
	for i, e := range body.Entries {
		if _, err := ids.Parse(e.ClientId); err != nil {
			return syncapi.SyncResponse{}, fieldErr(fmt.Sprintf("body.entries[%d].clientId", i), "ulid")
		}
		if e.DeviceSeq < 1 || (i > 0 && e.DeviceSeq <= body.Entries[i-1].DeviceSeq) {
			return syncapi.SyncResponse{}, fieldErr(fmt.Sprintf("body.entries[%d].deviceSeq", i), "order")
		}
	}
	device, real, err := s.device(ctx, p)
	if err != nil {
		return syncapi.SyncResponse{}, asProblem(err)
	}
	cursor := int64(body.Cursor)
	var out syncapi.SyncResponse
	err = s.syncTx(ctx, sc.StoreID, func(ctx context.Context) error {
		out = syncapi.SyncResponse{}
		run := &syncRun{sc: sc, p: p, device: device, real: real, now: s.now()}
		var err error
		if run.st, err = s.store(ctx, sc.StoreID); err != nil {
			return err
		}
		if err := s.checkCursor(ctx, run, cursor); err != nil {
			return err
		}
		if err := s.lockSyncCodes(ctx, run, body.Entries); err != nil {
			return err
		}
		out.Results = make([]syncapi.SyncEntryResult, len(body.Entries))
		for i, e := range body.Entries {
			if out.Results[i], err = s.syncEntry(ctx, run, i, e); err != nil {
				return err
			}
		}
		if run.offline > 0 || run.offers > 0 {
			if err := s.offlineRisk(ctx, run); err != nil {
				return err
			}
		}
		if run.noRole > 0 {
			if err := s.offerRoleRisk(ctx, run); err != nil {
				return err
			}
		}
		// The entries' own movements are catalogue changes too.
		if run.st, err = s.store(ctx, sc.StoreID); err != nil {
			return err
		}
		if out.Changes, out.Cursor, out.HasMore, err = s.changes(ctx, sc.StoreID, cursor, run.st.CatalogueSeq); err != nil {
			return err
		}
		err = s.q(ctx).UpsertSyncCursor(ctx, posdb.UpsertSyncCursorParams{
			DeviceID: ids.UUID(device), StoreID: ids.UUID(sc.StoreID), Seq: int64(out.Cursor), Now: run.now,
		})
		if err != nil {
			return fmt.Errorf("pos: sync cursor: %w", err)
		}
		return nil
	})
	if err != nil {
		return syncapi.SyncResponse{}, asProblem(err)
	}
	out.ServerNow = s.now()
	return out, nil
}

// syncAttempts bounds how often one sync runs its transaction: Postgres
// rolls back a deadlock's victim (40P01) or a serialization failure (40001)
// whole, so the batch runs again from the start. The sweep takes one claim
// per transaction (D-132) and never waits on a second customer's accounts,
// but a batch locks several customers' ledger accounts and claims in batch
// order, and another sync, an online sale or an order completion may lock
// some of them in another; one of the two is then the victim.
const syncAttempts = 3

// syncTx runs fn in a transaction, again on a retryable failure, up to
// syncAttempts times; each retry is logged and counted in
// tz_pos_sync_retried_total.
func (s *Service) syncTx(ctx context.Context, storeID ids.ID, fn func(ctx context.Context) error) error {
	return retryTx(ctx, s.db.InTx, fn, func(attempt int, err error) {
		syncRetried(ctx)
		telemetry.Logger(ctx).WarnContext(ctx, "pos: sync transaction retried", "store_id", storeID.String(), "attempt", attempt, "err", err)
	})
}

// retryTx runs fn through inTx up to syncAttempts times while it fails with
// a retryable error (db.IsRetryable), calling retried before each new
// attempt and waiting retryBackoff, so the two sides of a deadlock do not
// meet again at once. It returns the last error, and stops early when ctx
// ends.
func retryTx(ctx context.Context, inTx func(context.Context, func(context.Context) error) error, fn func(context.Context) error, retried func(attempt int, err error)) error {
	for attempt := 1; ; attempt++ {
		err := inTx(ctx, fn)
		if err == nil || attempt == syncAttempts || !db.IsRetryable(err) || ctx.Err() != nil {
			return err
		}
		retried(attempt, err)
		t := time.NewTimer(retryBackoff(attempt))
		select {
		case <-ctx.Done():
			t.Stop()
			return err
		case <-t.C:
		}
	}
}

// retryBackoff is the wait before attempt+1: 20 ms a past attempt plus up to
// 20 ms of jitter (20–40 ms, then 40–60 ms).
func retryBackoff(attempt int) time.Duration {
	const step = 20 * time.Millisecond
	return time.Duration(attempt)*step + rand.N(step) // #nosec G404 -- jitter, not a secret
}

// lockSyncCodes takes, before any entry, the code locks of the cashier and
// of every phone the batch's counter codes name, in sorted order (D-131).
// Taken entry by entry, two cashiers syncing the same phones in opposite
// order would deadlock and one sync would fail; held to commit, the locks
// taken again by each entry are no-ops.
func (s *Service) lockSyncCodes(ctx context.Context, run *syncRun, entries []syncapi.SyncEntry) error {
	return s.lockKeys(ctx, s.syncCodeKeys(run.sc.StoreID, run.p.UserID, entries))
}

// syncCodeKeys is the code keys a batch locks up front: none without a
// counter code, else the cashier's, even when no phone parses (taken later,
// after stock rows, it would invert VerifyCode's order), then the phones'.
func (s *Service) syncCodeKeys(store, cashier ids.ID, entries []syncapi.SyncEntry) []string {
	if s.hasher == nil {
		return nil
	}
	var phones [][]byte
	codes := false
	for _, e := range entries {
		if e.Sale == nil || e.Sale.Link == nil || e.Sale.Link.Method != syncapi.CounterCode || e.Sale.Link.Code == nil {
			continue
		}
		codes = true
		phones = append(phones, s.guard(store, cashier, e.Sale.Link.Phone).phone)
	}
	if !codes {
		return nil
	}
	return codeKeys(cashier, phones...)
}

// checkCursor refuses (410 resync_required) a cursor the changes cannot
// bring up to date: ahead of the store, older than the kept changes, or
// from a device that has not synced in changesKept.
func (s *Service) checkCursor(ctx context.Context, run *syncRun, cursor int64) error {
	resync := problem.New(genproblems.ResyncRequired, "")
	seq := run.st.CatalogueSeq
	if cursor > seq {
		return resync
	}
	q := s.q(ctx)
	c, err := q.GetSyncCursor(ctx, posdb.GetSyncCursorParams{DeviceID: ids.UUID(run.device), StoreID: ids.UUID(run.sc.StoreID)})
	switch {
	case err == nil && run.now.Sub(c.LastSyncedAt) > changesKept:
		return resync
	case err != nil && !isNoRows(err):
		return fmt.Errorf("pos: get sync cursor: %w", err)
	}
	if cursor == seq {
		return nil
	}
	w, err := q.ChangeWindow(ctx, posdb.ChangeWindowParams{StoreID: ids.UUID(run.sc.StoreID), Cursor: cursor})
	if err != nil {
		return fmt.Errorf("pos: change window: %w", err)
	}
	// Changes are pruned oldest first, so the changes after a kept cursor
	// are all there; cursor 0 needs the store's first change kept.
	kept := w.CursorKept || (cursor == 0 && w.OldestSeq == 1)
	if !kept || (w.CursorKept && run.now.Sub(w.CursorAt) > changesKept) {
		return resync
	}
	return nil
}

// syncEntry applies one entry, or reports it duplicate or rejected.
func (s *Service) syncEntry(ctx context.Context, run *syncRun, i int, e syncapi.SyncEntry) (syncapi.SyncEntryResult, error) {
	clientID, _ := ids.Parse(e.ClientId)
	res := syncapi.SyncEntryResult{ClientId: e.ClientId, DeviceSeq: e.DeviceSeq}
	q := s.q(ctx)
	prev, err := q.GetSyncEntry(ctx, posdb.GetSyncEntryParams{StoreID: ids.UUID(run.sc.StoreID), ClientID: ids.UUID(clientID)})
	switch {
	case err == nil:
		return s.duplicate(ctx, ids.UUID(run.sc.StoreID), res, prev.PosSaleID, prev.MovementID)
	case !isNoRows(err):
		return res, fmt.Errorf("pos: get sync entry: %w", err)
	}
	if e.Kind == syncapi.Sale {
		// The same bill rung online first (the device retried offline).
		if sale, err := q.GetSaleByClient(ctx, posdb.GetSaleByClientParams{StoreID: ids.UUID(run.sc.StoreID), ClientID: ids.UUID(clientID)}); err == nil {
			return s.duplicate(ctx, ids.UUID(run.sc.StoreID), res, sale.ID, nullID(ids.Zero))
		} else if !isNoRows(err) {
			return res, fmt.Errorf("pos: sale by client: %w", err)
		}
	}
	path := fmt.Sprintf("body.entries[%d]", i)
	var applied syncapi.SyncEntryResult
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		var err error
		if e.Kind == syncapi.Sale {
			applied, err = s.syncSale(ctx, run, clientID, e, path)
		} else {
			applied, err = s.syncStock(ctx, run, clientID, e)
		}
		return err
	})
	var rj rejected
	switch {
	case errors.As(err, &rj):
		res.Status, res.Reason = syncapi.Rejected, ptr(rj.reason)
		return res, nil
	case errors.Is(err, errDuplicate):
		res.Status = syncapi.Duplicate
		return res, nil
	case err != nil:
		var pe *problem.Error
		if errors.As(err, &pe) && pe.Code == genproblems.ValidationFailed {
			res.Status, res.Reason = syncapi.Rejected, ptr(syncapi.Invalid)
			return res, nil
		}
		return res, err
	}
	applied.ClientId, applied.DeviceSeq, applied.Status = e.ClientId, e.DeviceSeq, syncapi.Applied
	return applied, nil
}

var errDuplicate = errors.New("pos: sync entry already applied")

// duplicate is an entry an earlier sync (or the online route) applied.
func (s *Service) duplicate(ctx context.Context, storeID pgtype.UUID, res syncapi.SyncEntryResult, saleID, movementID pgtype.UUID) (syncapi.SyncEntryResult, error) {
	res.Status = syncapi.Duplicate
	res.SaleId, res.MovementId = idString(saleID), idString(movementID)
	if saleID.Valid {
		sale, err := s.q(ctx).GetSale(ctx, posdb.GetSaleParams{ID: saleID, StoreID: storeID})
		if err == nil {
			res.LinkStatus = syncLinkStatus(sale.LinkMethod, sale.LinkStatus)
		} else if !isNoRows(err) {
			return res, fmt.Errorf("pos: get sale: %w", err)
		}
	}
	return res, nil
}

// entryAllowed checks an entry's own action for the signed-in role (D-06).
func (s *Service) entryAllowed(ctx context.Context, run *syncRun, action string) error {
	err := s.also(ctx, action, run.sc)
	var pe *problem.Error
	if errors.As(err, &pe) && (pe.Code == genproblems.Forbidden || pe.Code == genproblems.Unauthorized) {
		return reject(syncapi.Forbidden)
	}
	return err
}

// --- a sale ------------------------------------------------------------------

func (s *Service) syncSale(ctx context.Context, run *syncRun, clientID ids.ID, e syncapi.SyncEntry, path string) (syncapi.SyncEntryResult, error) {
	var res syncapi.SyncEntryResult
	if e.Sale == nil || e.Stock != nil {
		return res, reject(syncapi.Invalid)
	}
	in := e.Sale
	if err := s.entryAllowed(ctx, run, policy.PosSell); err != nil {
		return res, err
	}
	billManual := int64(deref(in.BillDiscountPaise))
	manual := billManual != 0
	for _, l := range in.Lines {
		manual = manual || deref(l.ManualDiscountPaise) != 0
	}
	if manual {
		if err := s.entryAllowed(ctx, run, policy.PosDiscount); err != nil {
			return res, err
		}
	}
	if !slices.Contains(payments, string(in.PaymentMethod)) || len(in.Lines) == 0 || len(in.Lines) > maxLines {
		return res, reject(syncapi.Invalid)
	}
	var want []pgtype.UUID
	lineIDs := make([]ids.ID, len(in.Lines))
	for i, l := range in.Lines {
		if l.StoreItemId == nil {
			continue
		}
		id, err := ids.Parse(*l.StoreItemId)
		if err != nil {
			return res, reject(syncapi.Invalid)
		}
		lineIDs[i] = id
		want = append(want, ids.UUID(id))
	}
	items, err := s.items(ctx, run.sc.StoreID, want)
	if err != nil {
		return res, err
	}
	lines := make([]lineInput, len(in.Lines))
	for i, l := range in.Lines {
		cid, err := ids.Parse(l.ClientId)
		if err != nil {
			return res, reject(syncapi.Invalid)
		}
		// Recorded as the device sold it (02 §4.3): its price, its offer and
		// its manual discount. An item that is gone is a custom line.
		li := lineInput{
			ClientID: cid, Name: l.Name, Unit: int64(l.UnitPricePaise), Qty: l.Quantity,
			Offer: int64(deref(l.OfferDiscountPaise)), Manual: int64(deref(l.ManualDiscountPaise)),
			path: fmt.Sprintf("%s.sale.lines[%d]", path, i),
		}
		if it, ok := items[lineIDs[i]]; ok {
			li.ItemID, li.ProductID = lineIDs[i], optID(it.ProductID)
		}
		lines[i] = li
	}
	b, perr := price(lines, 0, billManual, 0, path+".sale")
	if perr != nil {
		return res, reject(syncapi.Invalid)
	}
	l, err := s.offlineLink(ctx, run, in.Link, e.At)
	if err != nil {
		return res, err
	}
	unmatched, err := s.offerUnmatched(ctx, run, lines, e.At)
	if err != nil {
		return res, err
	}
	w, err := s.writeSale(ctx, run.st, saleWrite{
		StoreID: run.sc.StoreID, ClientID: clientID, Cashier: run.p.UserID, Device: run.device,
		StockDevice: realDevice(run.device, run.real), DeviceSeq: ptr(int64(e.DeviceSeq)), Bill: b,
		Payment: string(in.PaymentMethod), SoldAt: e.At, Link: l, Path: path + ".sale",
		OfferUnmatched: unmatched,
	})
	switch {
	case errors.Is(err, errSaleExists):
		return res, errDuplicate
	case err != nil:
		return res, err
	}
	if l.method == methodCounterCode && l.status == linkLinked {
		run.offline++
	}
	if unmatched {
		// The device's offer discount is kept as sold (no offer id reaches
		// the server). One the engine would not give counts as offline risk
		// (once per sale), and as a flag for the manager when the cashier
		// could not discount by hand (D-131).
		run.offers++
		err := s.also(ctx, policy.PosDiscount, run.sc)
		var pe *problem.Error
		switch {
		case errors.As(err, &pe) && (pe.Code == genproblems.Forbidden || pe.Code == genproblems.Unauthorized):
			run.noRole++
		case err != nil:
			return res, err
		}
	}
	err = s.q(ctx).InsertSyncEntry(ctx, posdb.InsertSyncEntryParams{
		StoreID: ids.UUID(run.sc.StoreID), ClientID: ids.UUID(clientID), DeviceID: ids.UUID(run.device),
		DeviceSeq: int64(e.DeviceSeq), Kind: string(e.Kind), PosSaleID: w.Sale.ID, Now: run.now,
	})
	if err != nil {
		return res, fmt.Errorf("pos: insert sync entry: %w", err)
	}
	res.SaleId = ptr(mustID(w.Sale.ID).String())
	res.ReceiptToken = w.Receipt
	res.LinkStatus = syncLinkStatus(w.Sale.LinkMethod, w.Sale.LinkStatus)
	return res, nil
}

// offerUnmatched reports whether an offline sale carries a line offer
// discount above what the offers engine gives that line at sold_at (clamped
// as the sale is): a custom line gets nothing from the engine. A sale the
// engine cannot price counts as unmatched; the engine's own failure is
// rolled back to a savepoint so the entry can still be kept (D-131), unless
// it is a deadlock or serialization failure, which goes up so the batch is
// retried (D-132).
func (s *Service) offerUnmatched(ctx context.Context, run *syncRun, lines []lineInput, soldAt time.Time) (bool, error) {
	if !slices.ContainsFunc(lines, func(l lineInput) bool { return l.Offer > 0 }) {
		return false, nil
	}
	var pl []PriceLine
	var at []int
	for i, l := range lines {
		if l.ItemID != ids.Zero {
			pl = append(pl, PriceLine{StoreItemID: l.ItemID, ProductID: l.ProductID, UnitPricePaise: l.Unit, Quantity: l.Qty})
			at = append(at, i)
		}
	}
	engine := make([]int64, len(lines))
	if len(pl) > 0 {
		var pr Pricing
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			var err error
			pr, err = s.pricer.Price(ctx, ids.Zero, run.sc.StoreID, clampSoldAt(soldAt, run.now), pl)
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if db.IsRetryable(err) {
				return false, err // the batch is retried (syncTx)
			}
			telemetry.Logger(ctx).WarnContext(ctx, "pos: offline offer not priced", "store_id", run.sc.StoreID.String(), "err", err)
			return true, nil
		}
		for j, i := range at {
			if j < len(pr.LineDiscounts) {
				engine[i] = max(pr.LineDiscounts[j], 0)
			}
		}
	}
	for i, l := range lines {
		if l.Offer > engine[i] {
			return true, nil
		}
	}
	return false, nil
}

// offlineLink checks a sale's offline link. A counter code links only when
// it verifies at the moment it was typed (the device's time less its clock
// offset), the offset was within maxClockOffset, the sale syncs within
// pos_offline_link_max_hours and the (device, step) was not used before;
// otherwise the sale is kept with a failed link ("Customer link failed").
func (s *Service) offlineLink(ctx context.Context, run *syncRun, in *syncapi.SyncSaleLink, at time.Time) (link, error) {
	if in == nil {
		return link{}, nil
	}
	switch in.Method {
	case syncapi.PhoneUnverified:
		l, err := s.phoneLink(in.Phone, "phone")
		if err != nil {
			var pe *problem.Error
			if errors.As(err, &pe) {
				return link{}, reject(syncapi.Invalid)
			}
		}
		return l, err
	case syncapi.CounterCode:
	default:
		return link{}, reject(syncapi.Invalid)
	}
	failed := link{method: methodCounterCode, status: linkFailed, failReason: failUnverified}
	fail := func(reason string) (link, error) {
		failed.failReason = reason
		return failed, nil
	}
	if in.Code == nil || in.ClockOffsetMs == nil || s.devices == nil || s.hasher == nil {
		return failed, nil
	}
	offset := time.Duration(*in.ClockOffsetMs) * time.Millisecond
	failed.skewMs = ptr(i32(int64(*in.ClockOffsetMs)))
	typed := at
	if in.CodeEnteredAt != nil {
		typed = *in.CodeEnteredAt
	}
	typed = typed.Add(-offset).UTC().Truncate(time.Microsecond)
	maxAge, err := s.duration(ctx, settings.PosOfflineLinkMaxHours)
	if err != nil {
		return link{}, err
	}
	if offset > maxClockOffset || offset < -maxClockOffset || typed.After(run.now.Add(totpPeriod)) {
		return fail(failClockOff)
	}
	if run.now.Sub(typed) > maxAge {
		return fail(failTooLate)
	}
	// Wrong codes count into the online locks at the sync's time, and a
	// locked phone or cashier fails the sync's remaining links (D-131).
	g := s.guard(run.sc.StoreID, run.p.UserID, in.Phone)
	locks, err := s.codeLocks(ctx, g, run.now)
	if err != nil {
		return link{}, err
	}
	if locks.locked() {
		return fail(failLocked)
	}
	m, ok, err := s.matchCode(ctx, in.Phone, *in.Code, typed)
	if err != nil {
		return link{}, err
	}
	reason := failCodeWrong
	if ok {
		_, err = s.q(ctx).InsertCodeUse(ctx, posdb.InsertCodeUseParams{
			CustomerDeviceID: ids.UUID(m.deviceID), TimeStep: m.step, StoreID: ids.UUID(run.sc.StoreID), Now: run.now,
		})
		switch {
		case isNoRows(err):
			ok, reason = false, failCodeUsed // the code was used before: as wrong as a guess
		case err != nil:
			return link{}, fmt.Errorf("pos: code use: %w", err)
		}
	}
	if !ok {
		if _, err := s.codeFailed(ctx, g, locks, "sync", run.now); err != nil {
			return link{}, err
		}
		return fail(reason)
	}
	return link{
		method: methodCounterCode, status: linkLinked, customerID: m.customerID,
		offlineAt: &typed, skewMs: failed.skewMs, codeDevice: m.deviceID, codeStep: m.step,
	}, nil
}

// istDay is the start of run.now's day in IST, in UTC.
func istDay(now time.Time) time.Time {
	local := now.In(ist)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, ist).UTC()
}

// offlineRisk flags the store once a day when its offline sales with a
// counter-code link or a device offer discount the engine would not give
// (D-131), each sale counted once, pass pos_offline_links_risk_per_day
// (01 §6).
func (s *Service) offlineRisk(ctx context.Context, run *syncRun) error {
	limit, err := s.count(ctx, settings.PosOfflineLinksRiskPerDay)
	if err != nil {
		return err
	}
	since := istDay(run.now)
	q := s.q(ctx)
	r, err := q.OfflineRiskSalesSince(ctx, posdb.OfflineRiskSalesSinceParams{StoreID: ids.UUID(run.sc.StoreID), Since: since})
	if err != nil {
		return fmt.Errorf("pos: offline risk sales: %w", err)
	}
	if int(r.N) <= limit {
		return nil
	}
	details, _ := json.Marshal(map[string]int{"offlineSales": int(r.N), "offlineLinks": int(r.Links), "offlineOfferSales": int(r.Offers), "limit": limit})
	err = q.InsertRiskFlagOnce(ctx, posdb.InsertRiskFlagOnceParams{
		ID: ids.UUID(s.ids.New(run.now)), StoreID: ids.UUID(run.sc.StoreID), Rule: "pos.offline_links",
		// TODO(lead): the plan gives no score for this flag; 50 is a middle value.
		Score: 50, Details: details, Now: run.now, Since: since,
	})
	if err != nil {
		return fmt.Errorf("pos: risk flag: %w", err)
	}
	return nil
}

// offerRoleRisk flags the store once a day when a cashier without
// pos.discount synced a sale with an offer discount the device applied
// (D-131): the sale is kept as sold, the manager looks at it.
func (s *Service) offerRoleRisk(ctx context.Context, run *syncRun) error {
	since := istDay(run.now)
	details, _ := json.Marshal(map[string]any{"cashierUserId": run.p.UserID.String(), "sales": run.noRole})
	err := s.q(ctx).InsertRiskFlagOnce(ctx, posdb.InsertRiskFlagOnceParams{
		ID: ids.UUID(s.ids.New(run.now)), StoreID: ids.UUID(run.sc.StoreID), Rule: "pos.offline_offer_without_discount_role",
		// TODO(lead): no score in the plan; 50 as for pos.offline_links.
		Score: 50, Details: details, Now: run.now, Since: since,
	})
	if err != nil {
		return fmt.Errorf("pos: risk flag: %w", err)
	}
	return nil
}

// --- a stock change ----------------------------------------------------------

var stockKinds = map[syncapi.SyncEntryKind]struct {
	change string
	action string
}{
	syncapi.Count:   {ChangeCount, policy.StockCount},
	syncapi.Restock: {ChangeRestock, policy.StockRestock},
	syncapi.Damage:  {ChangeDamage, policy.StockDamage},
	syncapi.Expired: {ChangeExpired, policy.StockDamage},
}

func (s *Service) syncStock(ctx context.Context, run *syncRun, clientID ids.ID, e syncapi.SyncEntry) (syncapi.SyncEntryResult, error) {
	var res syncapi.SyncEntryResult
	k, ok := stockKinds[e.Kind]
	if !ok || e.Stock == nil || e.Sale != nil {
		return res, reject(syncapi.Invalid)
	}
	if err := s.entryAllowed(ctx, run, k.action); err != nil {
		return res, err
	}
	in := e.Stock
	itemID, err := ids.Parse(in.StoreItemId)
	if err != nil {
		return res, reject(syncapi.Invalid)
	}
	var qty int
	if k.change == ChangeCount {
		if in.Counted == nil || in.Quantity != nil {
			return res, reject(syncapi.Invalid)
		}
		qty = *in.Counted
	} else {
		if in.Quantity == nil || in.Counted != nil {
			return res, reject(syncapi.Invalid)
		}
		qty = *in.Quantity
	}
	if qty < 0 || qty > 1_000_000 {
		return res, reject(syncapi.Invalid)
	}
	t, err := tx(ctx)
	if err != nil {
		return res, err
	}
	r, err := s.stock.Change(ctx, t, StockChange{
		Kind: k.change, StoreID: run.sc.StoreID, ItemID: itemID, Qty: int32(qty), //nolint:gosec // checked above
		ClientID: clientID, UserID: run.p.UserID, DeviceID: realDevice(run.device, run.real), Note: in.Note,
	})
	switch {
	case errors.Is(err, ErrItemNotFound):
		return res, reject(syncapi.ItemNotFound)
	case errors.Is(err, ErrStockInvalid), errors.Is(err, ErrClientIDReused):
		return res, reject(syncapi.Invalid)
	case err != nil:
		return res, fmt.Errorf("pos: stock change: %w", err)
	case r.Replayed:
		return res, errDuplicate
	}
	mv := nullID(ids.Zero)
	if r.Wrote {
		mv = ids.UUID(r.MovementID)
	}
	err = s.q(ctx).InsertSyncEntry(ctx, posdb.InsertSyncEntryParams{
		StoreID: ids.UUID(run.sc.StoreID), ClientID: ids.UUID(clientID), DeviceID: ids.UUID(run.device),
		DeviceSeq: int64(e.DeviceSeq), Kind: string(e.Kind), MovementID: mv, Now: run.now,
	})
	if err != nil {
		return res, fmt.Errorf("pos: insert sync entry: %w", err)
	}
	res.MovementId = idString(mv)
	return res, nil
}

// --- catalogue changes -------------------------------------------------------

// changes are the newest change per entity in (after, upto], at most
// maxChanges, and the cursor to send next.
func (s *Service) changes(ctx context.Context, storeID ids.ID, after, upto int64) ([]syncapi.SyncChange, int, bool, error) {
	rows, err := s.q(ctx).ChangesAfter(ctx, posdb.ChangesAfterParams{
		StoreID: ids.UUID(storeID), After: after, Upto: upto, Lim: maxChanges + 1,
	})
	if err != nil {
		return nil, 0, false, fmt.Errorf("pos: changes: %w", err)
	}
	more := len(rows) > maxChanges
	if more {
		rows = rows[:maxChanges]
	}
	out := make([]syncapi.SyncChange, len(rows))
	for i, r := range rows {
		c := syncapi.SyncChange{
			Seq: int(r.Seq), Entity: syncapi.SyncChangeEntity(r.Entity), EntityId: mustID(r.EntityID).String(),
			Op: syncapi.SyncChangeOp(r.Op),
		}
		if r.ItemID.Valid {
			onHand, reserved := int(deref(r.OnHand)), int(deref(r.Reserved))
			c.Item = &syncapi.SyncItemState{
				Id: mustID(r.ItemID).String(), ProductId: idString(r.ProductID), Name: r.ItemName,
				PricePaise: int(deref(r.PricePaise)), OnHand: onHand, Reserved: reserved, Available: onHand - reserved,
				IsListed: deref(r.IsListed), CountNeeded: onHand < 0, Version: int(deref(r.ItemVersion)),
			}
			if r.MrpPaise != nil {
				c.Item.MrpPaise = ptr(int(*r.MrpPaise))
			}
		}
		out[i] = c
	}
	next := upto
	if more {
		next = rows[len(rows)-1].Seq
	}
	return out, int(next), more, nil
}

func syncLinkStatus(method, status *string) *syncapi.SyncEntryResultLinkStatus {
	if method == nil || *method == methodPhone || status == nil {
		return nil
	}
	switch *status {
	case linkLinked:
		return ptr(syncapi.Linked)
	case linkFailed:
		return ptr(syncapi.Failed)
	}
	return nil
}
