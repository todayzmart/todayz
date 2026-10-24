package credits

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	businesscreditsapi "github.com/todayz/backend/gen/api/business-credits"
	walletapi "github.com/todayz/backend/gen/api/wallet"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/creditsdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// Page sizes for the history lists (04 §1).
const (
	defaultLimit = 20
	maxLimit     = 100
)

// Wallet is GET /v1/me/wallet: what the customer can spend now, what is
// held, what expires soon and how the caps stand (C-16).
func (s *Service) Wallet(ctx context.Context, customerID ids.ID) (walletapi.Wallet, error) {
	r, err := s.Rules(ctx)
	if err != nil {
		return walletapi.Wallet{}, err
	}
	q, now, c := s.q(ctx), s.now(), ids.UUID(customerID)
	bal, err := q.CustomerBalances(ctx, c)
	if err != nil {
		return walletapi.Wallet{}, wrap("balances", err)
	}
	avail, err := q.SpendableCredits(ctx, creditsdb.SpendableCreditsParams{CustomerID: c, Now: now})
	if err != nil {
		return walletapi.Wallet{}, wrap("available", err)
	}
	lots, err := q.ExpiringLots(ctx, creditsdb.ExpiringLotsParams{
		CustomerID: c, Now: now, Until: now.Add(time.Duration(r.ReminderDays) * 24 * time.Hour),
	})
	if err != nil {
		return walletapi.Wallet{}, wrap("expiring", err)
	}
	month, err := q.CustomerMonthHolds(ctx, creditsdb.CustomerMonthHoldsParams{
		CustomerID: c, MonthStart: clock.StartOfMonthIST(now), Now: now,
	})
	if err != nil {
		return walletapi.Wallet{}, wrap("month holds", err)
	}
	out := walletapi.Wallet{
		AvailableCredits: int(avail), HeldCredits: int(bal.Held), ExpiringSoon: []walletapi.ExpiringCredits{},
		MaxCreditsPerOrder: r.MaxPerOrder, MinOrderPaise: int(r.MinOrderPaise),
		CreditOrdersPerMonth: r.OrdersPerMonth, CreditOrdersThisMonth: int(month), ServerNow: now,
	}
	for _, l := range lots {
		if n := len(out.ExpiringSoon); n > 0 && out.ExpiringSoon[n-1].ExpiresAt.Equal(l.ExpiresAt) {
			out.ExpiringSoon[n-1].Credits += int(l.Remaining)
			continue
		}
		out.ExpiringSoon = append(out.ExpiringSoon, walletapi.ExpiringCredits{Credits: int(l.Remaining), ExpiresAt: l.ExpiresAt.UTC()})
	}
	return out, nil
}

// WalletLines is GET /v1/me/wallet/transactions: one line per posting that
// moved the customer's credits, newest first, in plain words (01 §7).
func (s *Service) WalletLines(ctx context.Context, customerID ids.ID, limit *int, cursor *string) (walletapi.WalletLineList, error) {
	if err := s.ready(ctx); err != nil {
		return walletapi.WalletLineList{}, err
	}
	lim, at, after, err := pageArgs(limit, cursor)
	if err != nil {
		return walletapi.WalletLineList{}, err
	}
	rows, err := s.q(ctx).WalletLines(ctx, creditsdb.WalletLinesParams{
		CustomerID: ids.UUID(customerID), CursorAt: at, CursorID: after, Lim: i32(lim + 1),
	})
	if err != nil {
		return walletapi.WalletLineList{}, wrap("wallet lines", err)
	}
	out := walletapi.WalletLineList{Items: []walletapi.WalletLine{}}
	more := len(rows) > lim
	if more {
		rows = rows[:lim]
	}
	for _, r := range rows {
		out.Items = append(out.Items, walletLine(r))
	}
	if more {
		last := rows[len(rows)-1]
		c := encodeCursor(last.CreatedAt, mustID(last.ID))
		out.NextCursor = &c
	}
	return out, nil
}

func walletLine(r creditsdb.WalletLinesRow) walletapi.WalletLine {
	p := walletapi.WalletLineParams{}
	shop, num := r.ShopName, ""
	if r.OrderNumber != nil {
		num = *r.OrderNumber
		p.OrderNumber = &num
	}
	if shop != "" {
		p.ShopName = &shop
	}
	var text string
	switch TxKind(r.Kind) {
	case TxSpendHold:
		text = "Held for " + orderAt(num, shop)
	case TxSpendCapture:
		text = "Spent" + atShop(shop) + orderSuffix(num)
	case TxSpendRelease:
		reason := releaseReason(r)
		p.Reason = &reason
		text = releaseText(reason, num, shop)
	case TxCashback:
		pct := percent(r.Memo)
		if pct != "" {
			p.Percent = &pct
			text = pct + "% back" + atShop(shop)
		} else {
			text = "Cashback" + atShop(shop)
		}
	case TxReferralReward:
		text = "Referral bonus"
	case TxExpiry:
		text = "Expired"
	case TxAdminAdjustment:
		text = "Adjustment by TodayZ"
	case TxForfeitDeletion:
		text = "Forfeited when the account was deleted"
	case TxReversal:
		text = "Reversed" + atShop(shop) + orderSuffix(num)
	default:
		text = "Credits updated"
	}
	return walletapi.WalletLine{
		Id: mustID(r.ID).String(), Kind: walletapi.WalletLineKind(r.Kind), CreatedAt: r.CreatedAt.UTC(),
		Credits: int(r.WalletDelta + r.HeldDelta), AvailableCredits: int(r.WalletDelta),
		Text: text, Params: p,
	}
}

func releaseReason(r creditsdb.WalletLinesRow) walletapi.WalletLineParamsReason {
	switch {
	case r.Memo != nil && *r.Memo == memoRecap:
		return walletapi.OrderChanged
	case r.RefType != nil && *r.RefType == string(RefBillClaim):
		return walletapi.BillClaimExpired
	case r.OrderStatus == nil:
		return walletapi.Released
	}
	switch *r.OrderStatus {
	case "cancelled":
		return walletapi.OrderCancelled
	case "expired":
		return walletapi.OrderExpired
	case "rejected":
		return walletapi.OrderRejected
	}
	return walletapi.Released
}

func releaseText(reason walletapi.WalletLineParamsReason, num, shop string) string {
	switch reason {
	case walletapi.OrderCancelled:
		return "Returned: " + orderWord(num) + " was cancelled"
	case walletapi.OrderExpired:
		return "Returned: " + orderWord(num) + " expired"
	case walletapi.OrderRejected:
		if shop != "" {
			return "Returned: " + shop + " could not take " + orderWord(num)
		}
		return "Returned: " + orderWord(num) + " was not accepted"
	case walletapi.OrderChanged:
		return "Returned: " + orderWord(num) + " changed"
	case walletapi.BillClaimExpired:
		return "Returned: the bill was not confirmed in time"
	}
	return "Returned to your wallet"
}

func orderWord(num string) string {
	if num == "" {
		return "your order"
	}
	return "order " + num
}

func orderAt(num, shop string) string { return orderWord(num) + atShop(shop) }

func atShop(shop string) string {
	if shop == "" {
		return ""
	}
	return " at " + shop
}

func orderSuffix(num string) string {
	if num == "" {
		return ""
	}
	return " · order " + num
}

// percent turns the cashback posting's memo ("bp:250") into "2.5".
func percent(memo *string) string {
	if memo == nil {
		return ""
	}
	v, ok := strings.CutPrefix(*memo, "bp:")
	if !ok {
		return ""
	}
	bp, err := strconv.Atoi(v)
	if err != nil || bp <= 0 {
		return ""
	}
	whole, frac := bp/100, bp%100
	if frac == 0 {
		return strconv.Itoa(whole)
	}
	return strings.TrimRight(strconv.Itoa(whole)+"."+strconv.Itoa(100+frac)[1:], "0")
}

// CreditAccount is GET {B}/credit-account: the business's store credit
// balances and the postings on them, newest first.
func (s *Service) CreditAccount(ctx context.Context, businessID ids.ID, limit *int, cursor *string) (businesscreditsapi.CreditAccount, error) {
	if err := s.ready(ctx); err != nil {
		return businesscreditsapi.CreditAccount{}, err
	}
	lim, at, after, err := pageArgs(limit, cursor)
	if err != nil {
		return businesscreditsapi.CreditAccount{}, err
	}
	q, b := s.q(ctx), ids.UUID(businessID)
	stores, err := q.BusinessStoreBalances(ctx, b)
	if err != nil {
		return businesscreditsapi.CreditAccount{}, wrap("store balances", err)
	}
	rows, err := q.StoreLines(ctx, creditsdb.StoreLinesParams{BusinessID: b, CursorAt: at, CursorID: after, Lim: i32(lim + 1)})
	if err != nil {
		return businesscreditsapi.CreditAccount{}, wrap("store lines", err)
	}
	out := businesscreditsapi.CreditAccount{
		Stores: []businesscreditsapi.StoreCreditBalance{}, Lines: []businesscreditsapi.CreditAccountLine{}, ServerNow: s.now(),
	}
	for _, st := range stores {
		out.BalanceCredits += int(st.Balance)
		out.Stores = append(out.Stores, businesscreditsapi.StoreCreditBalance{
			StoreId: mustID(st.ID).String(), Name: st.Name, BalanceCredits: int(st.Balance),
		})
	}
	more := len(rows) > lim
	if more {
		rows = rows[:lim]
	}
	for _, r := range rows {
		num := ""
		if r.OrderNumber != nil {
			num = *r.OrderNumber
		}
		out.Lines = append(out.Lines, businesscreditsapi.CreditAccountLine{
			Id: mustID(r.ID).String(), StoreId: mustID(r.StoreID).String(), Kind: businesscreditsapi.CreditAccountLineKind(r.Kind),
			Credits: int(r.Amount), OrderNumber: r.OrderNumber, CreatedAt: r.CreatedAt.UTC(), Text: storeText(TxKind(r.Kind), num),
		})
	}
	if more {
		last := rows[len(rows)-1]
		c := encodeCursor(last.CreatedAt, mustID(last.ID))
		out.NextCursor = &c
	}
	return out, nil
}

func storeText(k TxKind, num string) string {
	var t string
	switch k {
	case TxCashback:
		t = "Cashback paid"
	case TxSpendCapture:
		t = "Credits redeemed"
	case TxPayout:
		t = "Payout"
	case TxReferralReward:
		t = "Referral reward"
	case TxAdminAdjustment:
		t = "Adjustment by TodayZ"
	case TxReversal:
		t = "Reversed"
	default:
		t = "Credits updated"
	}
	return t + orderSuffix(num)
}

// pageArgs reads limit and cursor. A cursor that does not decode is 422 on
// query.cursor.
func pageArgs(limit *int, cursor *string) (int, *time.Time, pgtype.UUID, error) {
	lim := defaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), maxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, nil, pgtype.UUID{}, nil
	}
	at, id, err := decodeCursor(*cursor)
	if err != nil {
		return 0, nil, pgtype.UUID{}, problem.New(genproblems.ValidationFailed, "").WithField("query.cursor", "not a cursor from this list")
	}
	return lim, &at, ids.UUID(id), nil
}

// encodeCursor makes the opaque cursor after (createdAt, id).
func encodeCursor(at time.Time, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id.String()))
}

func decodeCursor(c string) (time.Time, ids.ID, error) {
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return time.Time{}, ids.Zero, errors.New("credits: cursor has no id")
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return time.Time{}, ids.Zero, err
	}
	return time.Unix(0, n).UTC(), id, nil
}
