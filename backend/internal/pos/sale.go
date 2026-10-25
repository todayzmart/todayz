package pos

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	posapi "github.com/todayz/backend/gen/api/pos"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/money"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// codeTries is how many wrong counter codes lock a bill (01 §6).
const codeTries = 5 //settings:ok 01 §6 fixes the lock at 5 tries; no platform setting exists

// Counter-code brute force across bills (D-131): wrong codes are counted per
// (store, phone), per phone across stores (pos_code_phone_failures_per_hour
// in phoneAnyWindow) and per cashier on a rolling window, online and offline
// alike, so a new saleClientId does not reset them. codeCheckTTL is how long
// a verified check may wait for its sale.
const (
	phoneFailMax      = 5                //settings:ok D-131 fixes 5 wrong codes per phone at a store; no platform setting exists
	phoneFailWindow   = 15 * time.Minute //settings:ok D-131 fixed window
	phoneLock         = 15 * time.Minute //settings:ok D-131 fixed lock
	cashierFailMax    = 20               //settings:ok D-131 fixes 20 wrong codes per cashier; no platform setting exists
	cashierFailWindow = time.Hour
	cashierLock       = time.Hour
	phoneAnyWindow    = time.Hour
	codeCheckTTL      = 2 * time.Minute //settings:ok D-131 fixed age of a verified check
)

// soldAtMaxAge is the 01 §17 "sold_at clamp for offline sales" (72 h, a
// fixed value). TODO(lead): 01 names the number but not the direction; read
// as the oldest sold_at a sync records (D-131).
const soldAtMaxAge = 72 * time.Hour //settings:ok 01 §17 lists this 72 h as a fixed value, not a setting

// Link methods and statuses (pos_sales.link_method, link_status).
const (
	methodBillQR      = "bill_qr"
	methodCounterCode = "counter_code"
	methodPhone       = "phone_unverified"

	linkLinked  = "linked"
	linkPending = "pending_verification"
	linkFailed  = "failed"
)

// Why an offline counter code failed (pos_sales.link_fail_reason, W2-16).
const (
	failCodeUsed   = "code_used"
	failCodeWrong  = "code_wrong"
	failClockOff   = "clock_off"
	failTooLate    = "too_late"
	failLocked     = "locked"
	failUnverified = "unverified"
)

// receiptTTL is how long a receipt link works (D-45: 30 days; there is no
// setting for it).
const receiptTTL = 30 * 24 * time.Hour //settings:ok D-45 fixes the receipt link at 30 days

// BillClaimUpdated is the store event a bill QR's change sends: claimed by
// a customer, or used by a sale (the POS refetches the claim).
const BillClaimUpdated realtime.Type = "bill_claim.updated"

func init() { realtime.Register(BillClaimUpdated, realtime.KindStore) }

var payments = []string{"cash", "upi", "card", "other"}

// link is a sale's customer link as it will be written.
type link struct {
	method, status string // "" when the sale has no link
	failReason     string // a failed offline counter code: why (W2-16)
	customerID     ids.ID
	phoneHash      []byte
	phoneEnc       []byte
	offlineAt      *time.Time
	skewMs         *int32
	claimID        ids.ID // bill_qr: the claim to bind
	codeDevice     ids.ID // counter_code: the (device, step) use to bind
	codeStep       int64
}

// ledger reports whether the link touches the customer's ledger: credits,
// cashback and referrals (D-07).
func (l link) ledger() bool {
	return l.status == linkLinked && (l.method == methodBillQR || l.method == methodCounterCode)
}

// saleWrite is a priced sale ready to write.
type saleWrite struct {
	StoreID, ClientID ids.ID
	Cashier           ids.ID
	Device            ids.ID // pos_sales.device_id: the device or the session family
	StockDevice       ids.ID // a devices row for stock_movements, zero when none
	DeviceSeq         *int64
	Bill              bill
	Payment           string
	SoldAt            time.Time
	Link              link
	Offers            []AppliedOffer // online only: what the bill's offers took, redeemed with the sale
	OfferUnmatched    bool           // offline only: a device offer discount the engine would not give (D-131)
	Path              string         // the request field of the bill, for errors
}

// clampSoldAt is the sold_at a sale records. A device's clock may run ahead:
// a sale is never sold after it was received, nor more than soldAtMaxAge
// before (01 §17, D-131).
func clampSoldAt(at, now time.Time) time.Time {
	at = at.UTC().Truncate(time.Microsecond)
	switch {
	case at.After(now):
		return now
	case at.Before(now.Add(-soldAtMaxAge)):
		return now.Add(-soldAtMaxAge)
	}
	return at
}

// errSaleExists is a sale whose (store, clientId) was already written.
var errSaleExists = errors.New("pos: sale already written")

// written is a sale as written: its row, lines and (first time only) the
// receipt token.
type written struct {
	Sale    posdb.PosSale
	Lines   []posdb.PosSaleLine
	Receipt *string
}

// writeSale writes a sale, its lines and their sale_pos movements, binds its
// link, makes the receipt token and posts cashback and the referral. It
// runs in the caller's transaction and answers errSaleExists when the
// client id was already written.
func (s *Service) writeSale(ctx context.Context, st posdb.StoreInfoRow, in saleWrite) (written, error) {
	t, err := tx(ctx)
	if err != nil {
		return written{}, err
	}
	q := s.q(ctx)
	now := s.now()
	b := in.Bill
	saleID := s.ids.New(now)
	l := in.Link
	soldAt := clampSoldAt(in.SoldAt, now)
	row, err := q.InsertSale(ctx, posdb.InsertSaleParams{
		ID: ids.UUID(saleID), StoreID: ids.UUID(in.StoreID), ClientID: ids.UUID(in.ClientID),
		DeviceID: ids.UUID(in.Device), DeviceSeq: in.DeviceSeq, CashierUserID: ids.UUID(in.Cashier),
		CustomerID: nullID(l.customerID), LinkMethod: optString(l.method), LinkStatus: optString(l.status),
		LinkFailReason: optString(l.failReason), OfflineLinkAt: l.offlineAt, DeviceClockSkewMs: l.skewMs,
		LinkPhoneHash: l.phoneHash, LinkPhoneEnc: l.phoneEnc, OfflineOfferUnmatched: in.OfferUnmatched,
		ItemSubtotalPaise: b.ItemSubtotal, BillOfferDiscountPaise: b.BillOffer, SubtotalPaise: b.Subtotal,
		DiscountPaise: b.Discount, RoundOffPaise: b.RoundOff, CreditsUsed: i32(int64(b.Credits)),
		TotalPaise: b.Total, PaymentMethod: in.Payment, SoldAt: soldAt, Now: now,
	})
	if isNoRows(err) {
		return written{}, errSaleExists
	}
	if err != nil {
		return written{}, fmt.Errorf("pos: insert sale: %w", err)
	}
	for i, ln := range b.Lines {
		var mv ids.ID
		if ln.ItemID != ids.Zero {
			mv, err = s.stock.Sell(ctx, t, StockSale{
				StoreID: in.StoreID, ItemID: ln.ItemID, Qty: ln.Qty, SaleID: saleID,
				ClientID: ln.ClientID, UserID: in.Cashier, DeviceID: in.StockDevice,
			})
			switch {
			case errors.Is(err, ErrItemNotFound):
				return written{}, fieldErr(fmt.Sprintf("%s.lines[%d].storeItemId", in.Path, i), "not_found")
			case errors.Is(err, ErrClientIDReused):
				return written{}, fieldErr(fmt.Sprintf("%s.lines[%d].clientId", in.Path, i), "reused")
			case err != nil:
				return written{}, fmt.Errorf("pos: sell line: %w", err)
			}
		}
		err = q.InsertSaleLine(ctx, posdb.InsertSaleLineParams{
			ID: ids.UUID(s.ids.New(now)), SaleID: row.ID, ClientID: ids.UUID(ln.ClientID),
			StoreItemID: nullID(ln.ItemID), NameSnapshot: ln.Name, UnitPricePaise: ln.Unit, Quantity: ln.Qty,
			OfferDiscountPaise: ln.Offer, ManualDiscountPaise: ln.Manual, LineTotalPaise: ln.Total,
			MovementID: nullID(mv), Now: now,
		})
		if err != nil {
			return written{}, fmt.Errorf("pos: insert sale line: %w", err)
		}
	}
	if len(in.Offers) > 0 {
		// Priced as a walk-in (the link is not known then), so no offer with
		// a per-customer limit applied; a linked customer is recorded.
		var customer ids.ID
		if l.ledger() {
			customer = l.customerID
		}
		err := s.redeemer.Redeem(ctx, saleID, customer, in.Offers)
		if errors.Is(err, ErrOfferGone) {
			return written{}, problem.New(genproblems.PriceChanged, "")
		}
		if err != nil {
			return written{}, fmt.Errorf("pos: redeem offers: %w", err)
		}
	}
	if l.claimID != ids.Zero {
		c, err := q.BindBillClaim(ctx, posdb.BindBillClaimParams{PosSaleID: row.ID, Now: now, ID: ids.UUID(l.claimID)})
		if err != nil {
			return written{}, fmt.Errorf("pos: bind bill claim: %w", err)
		}
		err = s.credits.Capture(ctx, l.claimID, l.customerID, in.StoreID)
		if errors.Is(err, ErrHoldReleased) {
			return written{}, problem.New(genproblems.ClaimExpired, "")
		}
		if err != nil {
			return written{}, fmt.Errorf("pos: capture credits: %w", err)
		}
		if err := s.publishClaim(ctx, c); err != nil {
			return written{}, err
		}
	}
	// The bill is rung: its other QRs are dead and their holds go back.
	if err := s.cancelClaims(ctx, in.StoreID, in.ClientID, l.claimID, now); err != nil {
		return written{}, err
	}
	if l.codeDevice != ids.Zero {
		err := q.BindCodeUse(ctx, posdb.BindCodeUseParams{PosSaleID: row.ID, CustomerDeviceID: ids.UUID(l.codeDevice), TimeStep: l.codeStep})
		if err != nil {
			return written{}, fmt.Errorf("pos: bind code use: %w", err)
		}
	}
	token, err := s.receipt(ctx, saleID, now)
	if err != nil {
		return written{}, err
	}
	if l.ledger() {
		if row, err = s.rewards(ctx, st, row, l.customerID); err != nil {
			return written{}, err
		}
	}
	lines, err := q.SaleLines(ctx, row.ID)
	if err != nil {
		return written{}, fmt.Errorf("pos: sale lines: %w", err)
	}
	return written{Sale: row, Lines: lines, Receipt: &token}, nil
}

// rewards posts a linked sale's cashback on what the customer paid before
// round-off and hands the sale to referrals, which may qualify a referral
// (its rewards post after the sale commits, D-169).
func (s *Service) rewards(ctx context.Context, st posdb.StoreInfoRow, row posdb.PosSale, customerID ids.ID) (posdb.PosSale, error) {
	paid := row.TotalPaise - int64(row.RoundOffPaise)
	saleID, storeID := mustID(row.ID), mustID(row.StoreID)
	if c := cashbackCredits(paid, st.CashbackBp); c > 0 {
		posted, err := s.credits.Cashback(ctx, Cashback{
			CustomerID: customerID, StoreID: storeID, SaleID: saleID, PaidPaise: paid, CashbackBP: int(st.CashbackBp),
		})
		if err != nil {
			return row, fmt.Errorf("pos: cashback: %w", err)
		}
		if posted > 0 {
			row, err = s.q(ctx).SetSaleCashback(ctx, posdb.SetSaleCashbackParams{CashbackCredits: i32(int64(posted)), Now: s.now(), ID: row.ID})
			if err != nil {
				return row, fmt.Errorf("pos: set cashback: %w", err)
			}
		}
	}
	err := s.referrals.Qualify(ctx, Purchase{
		CustomerID: customerID, StoreID: storeID, SaleID: saleID, LinkMethod: deref(row.LinkMethod),
		EligiblePaise: paid + int64(row.CreditsUsed)*money.PaisePerCredit, CreditsUsed: int(row.CreditsUsed),
	})
	if err != nil {
		return row, fmt.Errorf("pos: referral: %w", err)
	}
	return row, nil
}

// A receipt token: 16 random bytes, 22 base64url characters (D-45).
const (
	receiptBytes = 16
	receiptChars = 22
)

// receipt makes the sale's receipt token: 128 random bits, stored as their
// SHA-256 (D-45). The token itself is returned once, never stored or logged.
func (s *Service) receipt(ctx context.Context, saleID ids.ID, now time.Time) (string, error) {
	raw, err := s.random(receiptBytes)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(raw)
	err = s.q(ctx).InsertReceiptToken(ctx, posdb.InsertReceiptTokenParams{
		ID: ids.UUID(s.ids.New(now)), TokenHash: h[:], PosSaleID: ids.UUID(saleID), ExpiresAt: now.Add(receiptTTL), Now: now,
	})
	if err != nil {
		return "", fmt.Errorf("pos: insert receipt token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// device is the signed-in device: its devices row when the session has one
// (real), else the session family.
func (s *Service) device(ctx context.Context, p authn.Principal) (id ids.ID, real bool, err error) {
	d, err := s.q(ctx).SessionDevice(ctx, posdb.SessionDeviceParams{FamilyID: ids.UUID(p.SessionID), UserID: ids.UUID(p.UserID)})
	switch {
	case isNoRows(err) || (err == nil && !d.Valid):
		return p.SessionID, false, nil
	case err != nil:
		return ids.Zero, false, fmt.Errorf("pos: session device: %w", err)
	}
	return mustID(d), true, nil
}

func (s *Service) store(ctx context.Context, storeID ids.ID) (posdb.StoreInfoRow, error) {
	st, err := s.q(ctx).StoreInfo(ctx, ids.UUID(storeID))
	if isNoRows(err) {
		return st, notFound()
	}
	if err != nil {
		return st, fmt.Errorf("pos: store: %w", err)
	}
	return st, nil
}

// --- the online sale -------------------------------------------------------

// CreateSale rings an online sale (POST {S}/pos/sales).
func (s *Service) CreateSale(ctx context.Context, sc authz.Scope, body posapi.PosSaleCreateRequest) (posapi.PosSale, error) {
	if err := s.readyToSell(ctx); err != nil {
		return posapi.PosSale{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.PosSale{}, err
	}
	clientID, err := ids.Parse(body.ClientId)
	if err != nil {
		return posapi.PosSale{}, fieldErr("body.clientId", "ulid")
	}
	if !slices.Contains(payments, string(body.PaymentMethod)) {
		return posapi.PosSale{}, fieldErr("body.paymentMethod", "enum")
	}
	if done, ok, err := s.existing(ctx, sc.StoreID, clientID); err != nil || ok {
		return done, err
	}
	st, err := s.store(ctx, sc.StoreID)
	if err != nil {
		return posapi.PosSale{}, asProblem(err)
	}
	lines, err := s.onlineLines(ctx, sc.StoreID, body.Lines)
	if err != nil {
		return posapi.PosSale{}, asProblem(err)
	}
	billManual := int64(deref(body.BillDiscountPaise))
	if billManual != 0 || slices.ContainsFunc(lines, func(l lineInput) bool { return l.Manual != 0 }) {
		if err := s.also(ctx, policy.PosDiscount, sc); err != nil {
			return posapi.PosSale{}, err
		}
	}
	billOffer, applied, err := s.offers(ctx, sc.StoreID, lines)
	if err != nil {
		return posapi.PosSale{}, asProblem(err)
	}
	b0, perr := price(lines, billOffer, billManual, 0, "body")
	if perr != nil {
		return posapi.PosSale{}, perr
	}
	device, real, err := s.device(ctx, p)
	if err != nil {
		return posapi.PosSale{}, asProblem(err)
	}
	var out written
	var replay bool
	var linkFail *problem.Error
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		l, credits, perr, err := s.onlineLink(ctx, sc.StoreID, clientID, body.Link, b0.Eligible)
		if errors.Is(err, errSaleExists) {
			replay = true
			return nil
		}
		if err != nil {
			return err
		}
		if perr != nil {
			linkFail = perr
			return nil //nolint:nilerr // the refusal is returned after commit: a wrong code's attempt must count
		}
		b := b0
		if credits > 0 {
			var perr *problem.Error
			if b, perr = price(lines, billOffer, billManual, credits, "body"); perr != nil {
				return perr
			}
		}
		if body.ExpectedTotalPaise != nil && int64(*body.ExpectedTotalPaise) != b.Total {
			return problem.New(genproblems.PriceChanged, "").WithExtension("totalPaise", b.Total)
		}
		out, err = s.writeSale(ctx, st, saleWrite{
			StoreID: sc.StoreID, ClientID: clientID, Cashier: p.UserID, Device: device,
			StockDevice: realDevice(device, real), Bill: b, Payment: string(body.PaymentMethod),
			SoldAt: s.now(), Link: l, Offers: applied, Path: "body",
		})
		if errors.Is(err, errSaleExists) {
			replay = true
			return nil
		}
		return err
	})
	switch {
	case err != nil:
		return posapi.PosSale{}, asProblem(err)
	case linkFail != nil:
		return posapi.PosSale{}, linkFail
	case replay:
		done, _, err := s.existing(ctx, sc.StoreID, clientID)
		return done, err
	}
	return s.saleView(ctx, out.Sale, out.Lines, out.Receipt)
}

// existing is the sale already rung with this client id, without its
// receipt token (only its hash is kept).
func (s *Service) existing(ctx context.Context, storeID, clientID ids.ID) (posapi.PosSale, bool, error) {
	q := s.q(ctx)
	row, err := q.GetSaleByClient(ctx, posdb.GetSaleByClientParams{StoreID: ids.UUID(storeID), ClientID: ids.UUID(clientID)})
	if isNoRows(err) {
		return posapi.PosSale{}, false, nil
	}
	if err != nil {
		return posapi.PosSale{}, false, asProblem(fmt.Errorf("pos: sale by client: %w", err))
	}
	lines, err := q.SaleLines(ctx, row.ID)
	if err != nil {
		return posapi.PosSale{}, false, asProblem(fmt.Errorf("pos: sale lines: %w", err))
	}
	v, err := s.saleView(ctx, row, lines, nil)
	return v, true, err
}

// onlineLines reads the request's lines: a catalogued line takes the item's
// name and must carry its price now (409 price_changed), a custom line
// needs a name.
func (s *Service) onlineLines(ctx context.Context, storeID ids.ID, in []posapi.PosSaleLineInput) ([]lineInput, error) {
	if len(in) == 0 || len(in) > maxLines {
		return nil, fieldErr("body.lines", "count")
	}
	want := make([]ids.ID, len(in))
	var uuids []pgtype.UUID
	for i, l := range in {
		if l.StoreItemId == nil {
			continue
		}
		id, err := ids.Parse(*l.StoreItemId)
		if err != nil {
			return nil, fieldErr(fmt.Sprintf("body.lines[%d].storeItemId", i), "ulid")
		}
		want[i] = id
		uuids = append(uuids, ids.UUID(id))
	}
	items, err := s.items(ctx, storeID, uuids)
	if err != nil {
		return nil, err
	}
	out := make([]lineInput, len(in))
	for i, l := range in {
		path := fmt.Sprintf("body.lines[%d]", i)
		cid, err := ids.Parse(l.ClientId)
		if err != nil {
			return nil, fieldErr(path+".clientId", "ulid")
		}
		li := lineInput{ClientID: cid, Unit: int64(l.UnitPricePaise), Qty: l.Quantity, Manual: int64(deref(l.ManualDiscountPaise)), path: path}
		if want[i] == ids.Zero {
			if l.Name == nil {
				return nil, fieldErr(path+".name", "required")
			}
			li.Name = *l.Name
		} else {
			it, ok := items[want[i]]
			if !ok {
				return nil, fieldErr(path+".storeItemId", "not_found")
			}
			if it.PricePaise != li.Unit {
				return nil, problem.New(genproblems.PriceChanged, "").
					WithExtension("storeItemId", want[i].String()).WithExtension("pricePaise", it.PricePaise)
			}
			li.ItemID, li.ProductID, li.Name = want[i], optID(it.ProductID), it.Name
		}
		out[i] = li
	}
	return out, nil
}

// items are the named store items of the store, by id.
func (s *Service) items(ctx context.Context, storeID ids.ID, want []pgtype.UUID) (map[ids.ID]posdb.StoreItemsByIDsRow, error) {
	m := make(map[ids.ID]posdb.StoreItemsByIDsRow, len(want))
	if len(want) == 0 {
		return m, nil
	}
	rows, err := s.q(ctx).StoreItemsByIDs(ctx, posdb.StoreItemsByIDsParams{StoreID: ids.UUID(storeID), Ids: want})
	if err != nil {
		return nil, fmt.Errorf("pos: store items: %w", err)
	}
	for _, r := range rows {
		m[mustID(r.ID)] = r
	}
	return m, nil
}

// offers runs the offers engine over the catalogued lines and sets their
// offer discounts; it returns the bill's and the offers to redeem.
func (s *Service) offers(ctx context.Context, storeID ids.ID, lines []lineInput) (int64, []AppliedOffer, error) {
	var pl []PriceLine
	var at []int
	for i, l := range lines {
		if l.ItemID != ids.Zero {
			pl = append(pl, PriceLine{StoreItemID: l.ItemID, ProductID: l.ProductID, UnitPricePaise: l.Unit, Quantity: l.Qty})
			at = append(at, i)
		}
	}
	if len(pl) == 0 {
		return 0, nil, nil
	}
	pr, err := s.pricer.Price(ctx, ids.Zero, storeID, time.Time{}, pl)
	if err != nil {
		return 0, nil, fmt.Errorf("pos: offers: %w", err)
	}
	for j, i := range at {
		if j < len(pr.LineDiscounts) {
			gross := lines[i].Unit * int64(lines[i].Qty)
			// A manual discount past what the offer left is refused by
			// price (manualDiscountPaise: range), never cut down.
			lines[i].Offer = min(max(pr.LineDiscounts[j], 0), gross)
		}
	}
	return max(pr.BillDiscount, 0), pr.Offers, nil
}

// onlineLink locks and checks the request's link. A refusal that must
// still commit (a counter code's state) comes back as perr with err nil.
func (s *Service) onlineLink(ctx context.Context, storeID, clientID ids.ID, in *posapi.PosSaleLinkInput, eligible int64) (l link, credits int, perr *problem.Error, err error) {
	if in == nil {
		return link{}, 0, nil, nil
	}
	q := s.q(ctx)
	switch string(in.Method) {
	case methodBillQR:
		if in.ClaimId == nil {
			return l, 0, nil, fieldErr("body.link.claimId", "required")
		}
		id, err := ids.Parse(*in.ClaimId)
		if err != nil {
			return l, 0, nil, fieldErr("body.link.claimId", "ulid")
		}
		c, err := q.LockBillClaim(ctx, ids.UUID(id))
		if isNoRows(err) || (err == nil && mustID(c.StoreID) != storeID) {
			return l, 0, nil, fieldErr("body.link.claimId", "not_found")
		}
		if err != nil {
			return l, 0, nil, fmt.Errorf("pos: lock bill claim: %w", err)
		}
		switch {
		case c.PosSaleID.Valid && mustID(c.SaleClientID) == clientID:
			// This bill's own sale took it: a concurrent retry waited on
			// the claim's lock and replays that sale.
			return l, 0, nil, errSaleExists
		case c.PosSaleID.Valid:
			return l, 0, nil, problem.New(genproblems.ClaimAlreadyUsed, "")
		case mustID(c.SaleClientID) != clientID || c.AmountPaise != eligible:
			return l, 0, nil, problem.New(genproblems.ClaimMismatch, "")
		case !s.now().Before(c.ExpiresAt), c.Status == "expired", c.Status == "cancelled":
			// A confirmed claim expires too (D-131): the sweep gives its
			// hold back.
			return l, 0, nil, problem.New(genproblems.ClaimExpired, "")
		case c.Status != "claimed":
			// Shown but not yet confirmed by the customer.
			return l, 0, nil, problem.New(genproblems.Conflict, "")
		}
		return link{method: methodBillQR, status: linkLinked, customerID: mustID(c.ClaimedBy), claimID: id}, int(c.CreditsUsed), nil, nil
	case methodCounterCode:
		c, err := q.LockCounterCheck(ctx, posdb.LockCounterCheckParams{StoreID: ids.UUID(storeID), SaleClientID: ids.UUID(clientID)})
		if isNoRows(err) {
			return l, 0, problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", codeTries), nil
		}
		if err != nil {
			return l, 0, nil, fmt.Errorf("pos: lock counter check: %w", err)
		}
		switch {
		case c.LockedAt != nil:
			return l, 0, problem.New(genproblems.CodeLocked, ""), nil
		case c.VerifiedAt == nil, s.now().Sub(*c.VerifiedAt) > codeCheckTTL:
			// Not verified, or verified too long ago for this sale (D-131).
			return l, 0, problem.New(genproblems.CodeInvalid, "").WithExtension("attemptsLeft", max(codeTries-int(c.Attempts), 0)), nil
		}
		return link{method: methodCounterCode, status: linkLinked, customerID: mustID(c.CustomerID),
			codeDevice: mustID(c.CustomerDeviceID), codeStep: deref(c.TimeStep)}, 0, nil, nil
	case methodPhone:
		if in.Phone == nil {
			return l, 0, nil, fieldErr("body.link.phone", "required")
		}
		l, err := s.phoneLink(*in.Phone, "body.link.phone")
		return l, 0, nil, err
	}
	return l, 0, nil, fieldErr("body.link.method", "enum")
}

// phoneLink is a typed phone: hashed and sealed, linked to no account and
// touching no ledger (D-07) until the customer verifies it.
func (s *Service) phoneLink(phone, path string) (link, error) {
	e164, ok := pii.NormalizePhone(phone)
	if !ok {
		return link{}, fieldErr(path, "phone")
	}
	enc, err := s.phones.Seal("phone", []byte(e164))
	if err != nil {
		return link{}, fmt.Errorf("pos: seal phone: %w", err)
	}
	return link{method: methodPhone, status: linkPending, phoneHash: s.hasher.Phone(e164), phoneEnc: enc}, nil
}

// --- reversal --------------------------------------------------------------

// ReverseSale reverses a sale (POST {S}/pos/sales/{saleId}/reverse).
func (s *Service) ReverseSale(ctx context.Context, sc authz.Scope, saleID ids.ID, reason string) (posapi.PosSale, error) {
	if err := s.readyToSell(ctx); err != nil {
		return posapi.PosSale{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.PosSale{}, err
	}
	if n := len([]rune(reason)); n < 1 || n > 200 { //settings:ok contract length of a reason
		return posapi.PosSale{}, fieldErr("body.reason", "length")
	}
	window, err := s.duration(ctx, settings.PosReversalWindowHours)
	if err != nil {
		return posapi.PosSale{}, asProblem(err)
	}
	var row posdb.PosSale
	var lines []posdb.PosSaleLine
	var shortfall int // cashback credits the customer had spent (D-74)
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		shortfall = 0
		t, err := tx(ctx)
		if err != nil {
			return err
		}
		q := s.q(ctx)
		now := s.now()
		sale, err := q.LockSale(ctx, posdb.LockSaleParams{ID: ids.UUID(saleID), StoreID: ids.UUID(sc.StoreID)})
		if isNoRows(err) {
			return notFound()
		}
		if err != nil {
			return fmt.Errorf("pos: lock sale: %w", err)
		}
		if sale.Status != "completed" {
			return problem.New(genproblems.InvalidTransition, "")
		}
		// An offline sale's window runs from when it reached the server:
		// the device's own time is not trusted for it (D-131).
		from := sale.SoldAt
		if sale.DeviceSeq != nil {
			from = sale.ReceivedAt
		}
		if now.Sub(from) > window {
			return problem.New(genproblems.ReversalWindowPassed, "")
		}
		if lines, err = q.SaleLines(ctx, sale.ID); err != nil {
			return fmt.Errorf("pos: sale lines: %w", err)
		}
		note := ptr(reason)
		for _, ln := range lines {
			if !ln.MovementID.Valid {
				continue
			}
			_, err := s.stock.Reverse(ctx, t, StockReversal{
				StoreID: sc.StoreID, MovementID: mustID(ln.MovementID), Qty: ln.Quantity, UserID: p.UserID, Note: note,
			})
			switch {
			case errors.Is(err, ErrItemNotFound), errors.Is(err, ErrStockInvalid):
				// The item or its movement is gone or can no longer take
				// the units back.
				return problem.New(genproblems.InvalidTransition, "")
			case err != nil:
				return fmt.Errorf("pos: reverse line: %w", err)
			}
		}
		if row, err = q.ReverseSale(ctx, posdb.ReverseSaleParams{Now: &now, ReversedBy: ids.UUID(p.UserID), Reason: note, ID: sale.ID}); err != nil {
			return fmt.Errorf("pos: reverse sale: %w", err)
		}
		if err := s.redeemer.Release(ctx, saleID); err != nil {
			return fmt.Errorf("pos: release offers: %w", err)
		}
		if sale.CustomerID.Valid && (sale.CreditsUsed > 0 || sale.CashbackCredits > 0) {
			r := SaleReversal{SaleID: saleID, CustomerID: mustID(sale.CustomerID), StoreID: sc.StoreID}
			claim, err := q.SaleBillClaim(ctx, sale.ID)
			switch {
			case err == nil:
				r.ClaimID = mustID(claim)
			case !isNoRows(err):
				return fmt.Errorf("pos: sale bill claim: %w", err)
			}
			if shortfall, err = s.credits.ReverseSale(ctx, r); err != nil {
				return fmt.Errorf("pos: reverse credits: %w", err)
			}
		}
		if sale.CustomerID.Valid && (link{method: deref(sale.LinkMethod), status: deref(sale.LinkStatus)}).ledger() {
			if err := s.referrals.Revoke(ctx, mustID(sale.CustomerID), saleID); err != nil {
				return fmt.Errorf("pos: revoke referral: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return posapi.PosSale{}, asProblem(err)
	}
	if shortfall > 0 {
		reverseShortfall(ctx)
		telemetry.Logger(ctx).WarnContext(ctx, "pos: reversed sale's cashback already spent; the store absorbs it",
			"sale_id", saleID.String(), "store_id", sc.StoreID.String(), "customer_id", mustID(row.CustomerID).String(),
			"shortfall_paise", int64(shortfall)*money.PaisePerCredit)
	}
	return s.saleView(ctx, row, lines, nil)
}

// --- views -----------------------------------------------------------------

// saleView is a sale as the POS sees it. The customer is named only for a
// bill_qr or counter_code link; a phone is never returned.
func (s *Service) saleView(ctx context.Context, row posdb.PosSale, lines []posdb.PosSaleLine, receipt *string) (posapi.PosSale, error) {
	v := posapi.PosSale{
		Id: mustID(row.ID).String(), ClientId: mustID(row.ClientID).String(),
		Lines:             make([]posapi.PosSaleLine, len(lines)),
		ItemSubtotalPaise: int(row.ItemSubtotalPaise), BillOfferDiscountPaise: int(row.BillOfferDiscountPaise),
		SubtotalPaise: int(row.SubtotalPaise), DiscountPaise: int(row.DiscountPaise), RoundOffPaise: int(row.RoundOffPaise),
		CreditsUsed: int(row.CreditsUsed), CashbackCredits: int(row.CashbackCredits), TotalPaise: int(row.TotalPaise),
		PaymentMethod: posapi.PosPaymentMethod(row.PaymentMethod), Status: posapi.PosSaleStatus(row.Status),
		ReversedAt: row.ReversedAt, ReversalReason: row.ReversalReason, SoldAt: row.SoldAt, ReceivedAt: row.ReceivedAt,
		ReceiptToken: receipt, Version: int(row.Version), ServerNow: s.now(),
	}
	for i, l := range lines {
		v.Lines[i] = posapi.PosSaleLine{
			Id: mustID(l.ID).String(), ClientId: mustID(l.ClientID).String(), StoreItemId: idString(l.StoreItemID),
			Name: l.NameSnapshot, UnitPricePaise: int(l.UnitPricePaise), Quantity: int(l.Quantity),
			OfferDiscountPaise: int(l.OfferDiscountPaise), ManualDiscountPaise: int(l.ManualDiscountPaise),
			LineTotalPaise: int(l.LineTotalPaise),
		}
	}
	if row.LinkMethod != nil {
		lk := &posapi.PosSaleLink{Method: posapi.PosLinkMethod(*row.LinkMethod), Status: posapi.PosLinkStatus(deref(row.LinkStatus)), Failure: linkFailure(row.LinkFailReason)}
		if row.CustomerID.Valid && *row.LinkMethod != methodPhone {
			name, err := s.q(ctx).UserName(ctx, row.CustomerID)
			if err != nil && !isNoRows(err) {
				return v, asProblem(fmt.Errorf("pos: customer name: %w", err))
			}
			if name != "" {
				lk.CustomerName = &name
			}
		}
		v.Link = lk
	}
	return v, nil
}

// linkFailure is a failed link's reason as the contract names it; nil for
// none (a sale that failed before W2-16 kept the reason).
func linkFailure(r *string) *posapi.PosLinkFailure {
	if r == nil {
		return nil
	}
	f := posapi.PosLinkFailure(*r)
	return &f
}

// --- small helpers ---------------------------------------------------------

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref[T any](p *T) T {
	var z T
	if p == nil {
		return z
	}
	return *p
}

// realDevice is the device for stock_movements.device_id (a devices row),
// zero when the session has none.
func realDevice(id ids.ID, real bool) ids.ID {
	if !real {
		return ids.Zero
	}
	return id
}

// --- reads for other domains -------------------------------------------------

// SaleRef is the part of a sale another domain may read (reviews, W2-06).
type SaleRef struct {
	StoreID ids.ID
	// CustomerID is the linked customer; zero when none.
	CustomerID ids.ID
	// LinkMethod is bill_qr or counter_code once the link is verified;
	// empty for an unlinked, phone-only or failed link.
	LinkMethod string
	// Completed is false for a reversed sale.
	Completed bool
}

// SaleRef reads a sale by id; ok is false when there is none.
func (s *Service) SaleRef(ctx context.Context, saleID ids.ID) (SaleRef, bool, error) {
	if err := s.ready(ctx); err != nil {
		return SaleRef{}, false, err
	}
	row, err := s.q(ctx).SaleRef(ctx, ids.UUID(saleID))
	if isNoRows(err) {
		return SaleRef{}, false, nil
	}
	if err != nil {
		return SaleRef{}, false, fmt.Errorf("pos: sale ref: %w", err)
	}
	out := SaleRef{StoreID: mustID(row.StoreID), CustomerID: optID(row.CustomerID), Completed: row.Status == "completed"}
	if l := (link{method: deref(row.LinkMethod), status: deref(row.LinkStatus)}); l.ledger() {
		out.LinkMethod = l.method
	}
	return out, true, nil
}
