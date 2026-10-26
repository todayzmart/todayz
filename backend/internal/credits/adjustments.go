package credits

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgtype"
	openapi_types "github.com/oapi-codegen/runtime/types"

	api "github.com/todayz/backend/gen/api/admin-credits"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/creditsdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-09 Credits (W3-01; 01 §7; D-05, D-73): the ledger browser, the expiry
// preview and admin adjustments of a customer's wallet.
//
// An adjustment posts at once (admin_adjustment, against
// platform_adjustments) while the same admin's adjustments to the same
// customer in the current IST day, |amount| summed with this one's, stay at
// or under admin_adjust_dual_approval_above_paise (D-73, D-237: so splitting
// one large adjustment into small ones does not skip the second person).
// Above it, it waits in
// admin_adjustment_requests for a second finance or super_admin; the
// requester can only cancel it (D-05; the table's CHECK keeps it too). Every
// step is one audit row in the posting's transaction, and every posting goes
// through Post, so it balances to zero and a repeat of its key posts nothing.

const (
	adminDefaultLimit = 20  //settings:ok the contract's Limit default
	adminMaxLimit     = 100 //settings:ok the contract's Limit maximum
	maxAdjustReason   = 500 //settings:ok contract bound (CreditAdjustmentCreate.reason ≤ 500)
	maxAdjustLink     = 200 //settings:ok contract bound (CreditAdjustmentCreate.link ≤ 200)
	refAdjustment     = "adjustment"
)

// AdminDeps are the admin credits service's collaborators.
type AdminDeps struct {
	Ledger *Service
	Audit  *audit.Writer
	// Hash finds a customer by phone in the account search; without it a
	// phone search answers 503.
	Hash *pii.Hasher
}

// Admin implements the admin-credits routes.
type Admin struct {
	s     *Service
	audit *audit.Writer
	hash  *pii.Hasher
	ids   *ids.Generator
}

// NewAdmin builds the admin credits service.
func NewAdmin(d AdminDeps) (*Admin, error) {
	if d.Ledger == nil {
		return nil, errors.New("credits: the admin service needs the ledger")
	}
	return &Admin{s: d.Ledger, audit: d.Audit, hash: d.Hash, ids: ids.NewGenerator()}, nil
}

func (a *Admin) readReady(ctx context.Context) error {
	if err := a.s.dbReady(ctx); err != nil {
		return asProblem(err)
	}
	return nil
}

// writeReady is the database, settings and the audit writer every change needs.
func (a *Admin) writeReady(ctx context.Context) error {
	if err := a.s.ready(ctx); err != nil {
		return asProblem(err)
	}
	if a.audit == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "credits: the audit writer is not configured")
		return problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
	}
	return nil
}

func adminPrincipal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

// --- ledger browser ----------------------------------------------------------------

// LedgerAccounts finds accounts by a customer's phone, a store's short code,
// an account, customer or store id, or "platform" for the platform accounts.
func (a *Admin) LedgerAccounts(ctx context.Context, q string) (api.AdminLedgerAccountList, error) {
	if err := a.readReady(ctx); err != nil {
		return api.AdminLedgerAccountList{}, err
	}
	q = strings.TrimSpace(q)
	cq := a.s.q(ctx)
	var rows []creditsdb.AccountsByOwnerRow
	var err error
	switch {
	case strings.EqualFold(q, "platform"):
		var rs []creditsdb.PlatformAccountsRow
		if rs, err = cq.PlatformAccounts(ctx); err == nil {
			for _, r := range rs {
				rows = append(rows, creditsdb.AccountsByOwnerRow(r))
			}
		}
	case isULID(q):
		id, _ := ids.Parse(q)
		if rows, err = cq.AccountsByOwner(ctx, ids.UUID(id)); err == nil {
			var rs []creditsdb.CustomerWithoutWalletByIDRow
			if rs, err = cq.CustomerWithoutWalletByID(ctx, ids.UUID(id)); err == nil {
				for _, r := range rs {
					rows = append(rows, unopenedWallet(r.ID, r.OwnerName))
				}
			}
		}
	case looksLikePhone(q):
		p, ok := phoneE164(q)
		if !ok {
			return api.AdminLedgerAccountList{}, adminFieldErr("query.q", "not a phone number")
		}
		if a.hash == nil {
			telemetry.Logger(ctx).ErrorContext(ctx, "credits: phone search needs the PII hash key")
			return api.AdminLedgerAccountList{}, problem.Wrap(genproblems.ServiceUnavailable, ErrUnavailable)
		}
		var rs []creditsdb.AccountsByPhoneHashRow
		h := a.hash.Phone(p)
		if rs, err = cq.AccountsByPhoneHash(ctx, h); err == nil {
			for _, r := range rs {
				rows = append(rows, creditsdb.AccountsByOwnerRow(r))
			}
			var us []creditsdb.CustomerWithoutWalletByPhoneHashRow
			if us, err = cq.CustomerWithoutWalletByPhoneHash(ctx, h); err == nil {
				for _, u := range us {
					rows = append(rows, unopenedWallet(u.ID, u.OwnerName))
				}
			}
		}
	default:
		var rs []creditsdb.AccountsByStoreCodeRow
		if rs, err = cq.AccountsByStoreCode(ctx, q); err == nil {
			for _, r := range rs {
				rows = append(rows, creditsdb.AccountsByOwnerRow(r))
			}
		}
	}
	if err != nil {
		return api.AdminLedgerAccountList{}, problem.Wrap(genproblems.Internal, err)
	}
	items := make([]api.AdminLedgerAccount, 0, min(len(rows), adminMaxLimit))
	for i, r := range rows {
		if i == adminMaxLimit {
			break
		}
		items = append(items, ledgerAccount(r))
	}
	return api.AdminLedgerAccountList{Items: items, ServerNow: a.s.now()}, nil
}

// unopenedWallet is the search row of a customer who has never had a ledger
// posting (D-240): a customer_wallet with no account id and a zero balance.
// Accounts open at the first posting (Service.accountID), so an adjustment
// names the customer instead and opens the wallet in its own transaction.
func unopenedWallet(customer pgtype.UUID, name string) creditsdb.AccountsByOwnerRow {
	return creditsdb.AccountsByOwnerRow{
		Kind: string(CustomerWallet), OwnerType: ownerType(CustomerWallet), OwnerID: customer, OwnerName: name,
	}
}

func ledgerAccount(r creditsdb.AccountsByOwnerRow) api.AdminLedgerAccount {
	var name *string
	if r.OwnerName != "" {
		name = &r.OwnerName
	}
	return api.AdminLedgerAccount{
		Id: optUUID(r.ID), Kind: api.AdminLedgerAccountKind(r.Kind),
		OwnerType: api.AdminLedgerAccountOwnerType(r.OwnerType), OwnerId: optUUID(r.OwnerID), OwnerName: name,
		BalancePaise: int(r.Balance * paisePerCredit),
	}
}

// LedgerEntries is one account's entries, newest first.
func (a *Admin) LedgerEntries(ctx context.Context, accountID ids.ID, limit *int, cursor *string) (api.AdminLedgerEntryPage, error) {
	if err := a.readReady(ctx); err != nil {
		return api.AdminLedgerEntryPage{}, err
	}
	lim, at, cid, err := adminPage(limit, cursor)
	if err != nil {
		return api.AdminLedgerEntryPage{}, err
	}
	cq := a.s.q(ctx)
	acct, err := cq.AccountByID(ctx, ids.UUID(accountID))
	if db.IsNoRows(err) {
		return api.AdminLedgerEntryPage{}, problem.New(genproblems.NotFound, "")
	}
	if err != nil {
		return api.AdminLedgerEntryPage{}, problem.Wrap(genproblems.Internal, err)
	}
	rows, err := cq.AccountEntries(ctx, creditsdb.AccountEntriesParams{
		AccountID: ids.UUID(accountID), CursorAt: at, CursorID: cid, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, adminMaxLimit]
	})
	if err != nil {
		return api.AdminLedgerEntryPage{}, problem.Wrap(genproblems.Internal, err)
	}
	var next *string
	if len(rows) > lim {
		last := rows[lim-1]
		c := adminCursor(last.CreatedAt, mustID(last.ID))
		next, rows = &c, rows[:lim]
	}
	items := make([]api.AdminLedgerEntry, 0, len(rows))
	for _, r := range rows {
		items = append(items, api.AdminLedgerEntry{
			Id: uuidString(r.ID), TransactionId: uuidString(r.TransactionID), Kind: api.AdminLedgerTransactionKind(r.Kind),
			RefType: r.RefType, RefId: optUUID(r.RefID), Memo: r.Memo, AmountPaise: int(r.Amount * paisePerCredit),
			CreatedBy: optUUID(r.CreatedBy), CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return api.AdminLedgerEntryPage{
		Account: ledgerAccount(creditsdb.AccountsByOwnerRow(acct)), Items: items, NextCursor: next, ServerNow: a.s.now(),
	}, nil
}

// ExpiryPreview sums what will expire from now to the end of the IST day
// days−1 days ahead, per IST day.
func (a *Admin) ExpiryPreview(ctx context.Context, days int) (api.AdminExpiryPreview, error) {
	if err := a.readReady(ctx); err != nil {
		return api.AdminExpiryPreview{}, err
	}
	now := a.s.now()
	to := clock.StartOfDayIST(now).AddDate(0, 0, days)
	rows, err := a.s.q(ctx).ExpiryPreview(ctx, creditsdb.ExpiryPreviewParams{FromAt: now, ToAt: to})
	if err != nil {
		return api.AdminExpiryPreview{}, problem.Wrap(genproblems.Internal, err)
	}
	out := api.AdminExpiryPreview{From: now, To: to, Days: make([]api.AdminExpiryDay, 0, len(rows)), ServerNow: now}
	for _, r := range rows {
		out.Days = append(out.Days, api.AdminExpiryDay{
			Day: openDate(r.Day), ExpiringPaise: int(r.Credits * paisePerCredit), Lots: int(r.Lots), Customers: int(r.Customers),
		})
		out.TotalPaise += int(r.Credits * paisePerCredit)
	}
	return out, nil
}

// --- adjustments -------------------------------------------------------------------

// AdjustmentRequests lists requests, newest first; status filters.
func (a *Admin) AdjustmentRequests(ctx context.Context, caller ids.ID, status *string, limit *int, cursor *string) (api.CreditAdjustmentList, error) {
	if err := a.readReady(ctx); err != nil {
		return api.CreditAdjustmentList{}, err
	}
	lim, at, cid, err := adminPage(limit, cursor)
	if err != nil {
		return api.CreditAdjustmentList{}, err
	}
	rows, err := a.s.q(ctx).AdjustmentRequests(ctx, creditsdb.AdjustmentRequestsParams{
		Status: status, CursorAt: at, CursorID: cid, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, adminMaxLimit]
	})
	if err != nil {
		return api.CreditAdjustmentList{}, problem.Wrap(genproblems.Internal, err)
	}
	var next *string
	if len(rows) > lim {
		last := rows[lim-1]
		c := adminCursor(last.RequestedAt, mustID(last.ID))
		next, rows = &c, rows[:lim]
	}
	items := make([]api.CreditAdjustmentRequest, 0, len(rows))
	for _, r := range rows {
		items = append(items, adjustmentView(creditsdb.AdjustmentRequestRow(r), caller))
	}
	return api.CreditAdjustmentList{Items: items, NextCursor: next, ServerNow: a.s.now()}, nil
}

func adjustmentView(r creditsdb.AdjustmentRequestRow, caller ids.ID) api.CreditAdjustmentRequest {
	nz := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	return api.CreditAdjustmentRequest{
		Id: uuidString(r.ID), CustomerId: uuidString(r.CustomerID), CustomerName: nz(r.CustomerName),
		AmountPaise: int(r.Amount) * paisePerCredit, Reason: r.Reason, Link: r.Link,
		Status: api.CreditAdjustmentStatus(r.Status), RequestedBy: uuidString(r.RequestedBy),
		RequestedByName: nz(r.RequestedByName), RequestedAt: r.RequestedAt.UTC(), Mine: mustID(r.RequestedBy) == caller,
		ApprovedBy: optUUID(r.ApprovedBy), ApprovedByName: r.ApprovedByName, DecidedAt: utc(r.DecidedAt),
		DecisionNote: r.DecisionNote, TransactionId: optUUID(r.TransactionID),
	}
}

// AdjustmentInput is a create request, as the route received it.
type AdjustmentInput struct {
	// AccountID is the customer's wallet; CustomerID the customer, for one
	// whose wallet is not open yet (D-240). Exactly one is set.
	AccountID   string
	CustomerID  string
	AmountPaise int
	Reason      string
	Link        *string
	// IdempotencyKey is the request's Idempotency-Key; it keys an
	// at-once posting, so even a retry past the HTTP layer posts once.
	IdempotencyKey string
}

// CreateAdjustment posts at once at or under the threshold, else records a
// pending request.
func (a *Admin) CreateAdjustment(ctx context.Context, p authn.Principal, in AdjustmentInput) (api.CreditAdjustmentResult, error) {
	if err := a.writeReady(ctx); err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	reason, err := cleanText("body.reason", in.Reason, maxAdjustReason)
	if err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	var link *string
	if in.Link != nil {
		l, err := cleanText("body.link", *in.Link, maxAdjustLink)
		if err != nil {
			return api.CreditAdjustmentResult{}, err
		}
		link = &l
	}
	if in.AmountPaise == 0 || in.AmountPaise%paisePerCredit != 0 {
		return api.CreditAdjustmentResult{}, adminFieldErr("body.amountPaise", "whole credits (a multiple of 100), never 0")
	}
	credits := int64(in.AmountPaise / paisePerCredit)
	var accountID, customerID ids.ID
	field := "body.accountId"
	switch {
	case in.AccountID != "" && in.CustomerID != "":
		return api.CreditAdjustmentResult{}, adminFieldErr("body.customerId", "name the account or the customer, not both")
	case in.CustomerID != "":
		field = "body.customerId"
		if customerID, err = ids.Parse(in.CustomerID); err != nil {
			return api.CreditAdjustmentResult{}, adminFieldErr(field, "not a customer")
		}
	default:
		if accountID, err = ids.Parse(in.AccountID); err != nil {
			return api.CreditAdjustmentResult{}, adminFieldErr(field, "not an account")
		}
	}
	threshold, err := a.s.settings.Paise(ctx, settings.AdminAdjustDualApprovalAbovePaise)
	if err != nil {
		return api.CreditAdjustmentResult{}, problem.Wrap(genproblems.Internal, err)
	}
	abs := int64(in.AmountPaise)
	if abs < 0 {
		abs = -abs
	}

	now := a.s.now()
	id := a.ids.New(now)
	var out api.CreditAdjustmentResult
	err = a.s.db.InTx(ctx, func(ctx context.Context) error {
		cq := a.s.q(ctx)
		acct, err := a.adjustWallet(ctx, p.UserID, accountID, customerID)
		if err != nil {
			return err
		}
		// 03 puts admin_adjustment_requests on the customer's wallet only; a
		// store_credit adjustment (a shop's statement correction) is refused
		// until the plan says who may make one and how it shows on B-10
		// (conservative reading, accepted by the lead 25 Oct).
		if acct.Kind != string(CustomerWallet) {
			return adminFieldErr(field, "must be a customer's wallet")
		}
		customer := mustID(acct.OwnerID)
		live, err := cq.CustomerExists(ctx, acct.OwnerID)
		if err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		if !live {
			return adminFieldErr(field, "the customer's account is deleted")
		}
		dayPaise, pending, err := a.needsSecond(ctx, p.UserID, acct, abs, int64(threshold), in.IdempotencyKey, now)
		if err != nil {
			return err
		}
		after := map[string]any{
			"customerId": customer.String(), "amountPaise": in.AmountPaise, "reason": reason, "link": link,
			"thresholdPaise": int64(threshold), "dayTotalPaise": dayPaise,
		}
		if pending {
			// At once, Post checks the balance itself (after the key, so a replay
			// of a debit still answers its first result).
			if credits < 0 && acct.Balance < -credits {
				return problem.New(genproblems.AdjustmentInsufficientBalance, "")
			}
			var key *string
			if in.IdempotencyKey != "" {
				key = &in.IdempotencyKey
			}
			n, err := cq.InsertAdjustmentRequest(ctx, creditsdb.InsertAdjustmentRequestParams{
				ID: ids.UUID(id), CustomerID: acct.OwnerID, Amount: int32(credits), // #nosec G115 -- |credits| ≤ 1,000,000 (contract bound)
				Reason: reason, Link: link, RequestedBy: ids.UUID(p.UserID), Now: now, IdempotencyKey: key,
			})
			if err != nil {
				return problem.Wrap(genproblems.Internal, err)
			}
			if n == 0 {
				// A replay of this requester's key: the first request, never a second.
				first, err := cq.AdjustmentRequestByKey(ctx, creditsdb.AdjustmentRequestByKeyParams{RequestedBy: ids.UUID(p.UserID), IdempotencyKey: key})
				if err != nil {
					return problem.Wrap(genproblems.Internal, err)
				}
				if first.CustomerID != acct.OwnerID || int64(first.Amount) != credits || first.Reason != reason || deref(first.Link) != deref(link) {
					return problem.New(genproblems.Conflict, "this idempotency key made another adjustment request")
				}
				id = mustID(first.ID)
			} else if _, err := a.audit.Write(ctx, audit.Entry{
				ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: "credits.adjustment_request",
				SubjectType: "credit_adjustment", SubjectID: id.String(), After: after,
			}); err != nil {
				return err
			}
			r, err := cq.AdjustmentRequest(ctx, ids.UUID(id))
			if err != nil {
				return problem.Wrap(genproblems.Internal, err)
			}
			v := adjustmentView(r, p.UserID)
			out = api.CreditAdjustmentResult{Outcome: "pending", Request: &v}
			return nil
		}
		key := id.String()
		if in.IdempotencyKey != "" {
			key = p.UserID.String() + ":" + in.IdempotencyKey
			// An identical replay posts with the first ref, so Post returns the
			// first transaction; a different body under the key is still 409.
			ref, err := cq.AdjustmentRefByKey(ctx, KeyPrefix[TxAdminAdjustment]+key)
			switch {
			case err == nil:
				id = mustID(ref)
			case !db.IsNoRows(err):
				return problem.Wrap(genproblems.Internal, err)
			}
		}
		posted, err := a.post(ctx, p.UserID, customer, credits, id, key, reason)
		if err != nil {
			return err
		}
		if !posted.Duplicate {
			after["transactionId"] = posted.TransactionID.String()
			if _, err := a.audit.Write(ctx, audit.Entry{
				ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: "credits.adjust",
				SubjectType: "credit_adjustment", SubjectID: id.String(), After: after,
			}); err != nil {
				return err
			}
		}
		t := posted.TransactionID.String()
		out = api.CreditAdjustmentResult{Outcome: "posted", TransactionId: &t}
		return nil
	})
	if err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	out.ServerNow = a.s.now()
	return out, nil
}

// adjustWallet is the wallet an adjustment names: by account id, or by
// customer id (D-240). A customer (a user with a customer profile) whose
// wallet is not open yet (accounts open at the first posting) gets a row with
// no id and a zero balance: the
// at-once posting opens the wallet in this transaction through the same path
// as every first posting (Service.accountID), and a pending request opens
// nothing until it is approved. On the customer path the D-237 lock on admin
// and customer is taken before the wallet is looked up, so a concurrent
// adjustment by the same admin that opened the wallet is seen (needsSecond
// takes the same lock again; a transaction lock is re-entrant), and its
// posting counts in the day's sum.
func (a *Admin) adjustWallet(ctx context.Context, admin, accountID, customerID ids.ID) (creditsdb.AccountByIDRow, error) {
	cq := a.s.q(ctx)
	field := "body.accountId"
	if !customerID.IsZero() {
		field = "body.customerId"
		if err := cq.LockAdminCustomerAdjust(ctx, admin.String()+":"+customerID.String()); err != nil {
			return creditsdb.AccountByIDRow{}, problem.Wrap(genproblems.Internal, err)
		}
		id, err := cq.AccountID(ctx, creditsdb.AccountIDParams{Kind: string(CustomerWallet), OwnerID: ids.UUID(customerID)})
		switch {
		case err == nil:
			accountID = mustID(id)
		case db.IsNoRows(err):
			live, err := cq.IsCustomer(ctx, ids.UUID(customerID))
			if err != nil {
				return creditsdb.AccountByIDRow{}, problem.Wrap(genproblems.Internal, err)
			}
			if !live {
				return creditsdb.AccountByIDRow{}, adminFieldErr(field, "not a customer")
			}
			return creditsdb.AccountByIDRow{
				Kind: string(CustomerWallet), OwnerType: ownerType(CustomerWallet), OwnerID: ids.UUID(customerID),
			}, nil
		default:
			return creditsdb.AccountByIDRow{}, problem.Wrap(genproblems.Internal, err)
		}
	}
	acct, err := cq.AccountByID(ctx, ids.UUID(accountID))
	if db.IsNoRows(err) {
		return creditsdb.AccountByIDRow{}, adminFieldErr(field, "not an account")
	}
	if err != nil {
		return creditsdb.AccountByIDRow{}, problem.Wrap(genproblems.Internal, err)
	}
	return acct, nil
}

// needsSecond decides whether an adjustment waits for a second approver
// (D-237): it does when this admin's adjustments to this customer in the
// current IST day, absolute values, pending and posted, plus this one, come
// to more than the threshold. It takes a transaction lock on the admin and
// customer first, so two concurrent requests cannot both pass under it. A
// replay of an Idempotency-Key keeps the path its first request took (its
// first request is already in the sum). dayPaise is the sum, this one included.
// A wallet not open yet (acct.ID null, D-240) has no entries, so only the
// admin's requests for the customer count.
func (a *Admin) needsSecond(ctx context.Context, admin ids.ID, acct creditsdb.AccountByIDRow, abs, threshold int64, idemKey string, now time.Time) (dayPaise int64, pending bool, err error) {
	cq := a.s.q(ctx)
	if err := cq.LockAdminCustomerAdjust(ctx, admin.String()+":"+mustID(acct.OwnerID).String()); err != nil {
		return 0, false, problem.Wrap(genproblems.Internal, err)
	}
	if idemKey != "" {
		_, err := cq.AdjustmentRefByKey(ctx, KeyPrefix[TxAdminAdjustment]+admin.String()+":"+idemKey)
		switch {
		case err == nil:
			return 0, false, nil
		case !db.IsNoRows(err):
			return 0, false, problem.Wrap(genproblems.Internal, err)
		}
		_, err = cq.AdjustmentRequestByKey(ctx, creditsdb.AdjustmentRequestByKeyParams{RequestedBy: ids.UUID(admin), IdempotencyKey: &idemKey})
		switch {
		case err == nil:
			return 0, true, nil
		case !db.IsNoRows(err):
			return 0, false, problem.Wrap(genproblems.Internal, err)
		}
	}
	done, err := cq.AdminAdjustedSince(ctx, creditsdb.AdminAdjustedSinceParams{
		WalletID: acct.ID, Since: clock.StartOfDayIST(now), AdminID: ids.UUID(admin), CustomerID: acct.OwnerID,
	})
	if err != nil {
		return 0, false, problem.Wrap(genproblems.Internal, err)
	}
	dayPaise = done*paisePerCredit + abs
	return dayPaise, dayPaise > threshold, nil
}

// post is the admin_adjustment posting: the wallet by credits, the platform's
// adjustments account the other way. Its key is "adjust:" + key: the request
// id for an approval, the caller and their Idempotency-Key for an at-once one.
func (a *Admin) post(ctx context.Context, by, customer ids.ID, credits int64, ref ids.ID, key, memo string) (Posted, error) {
	res, err := a.s.Post(ctx, Posting{
		Kind: TxAdminAdjustment, RefType: refAdjustment, RefID: ref,
		IdempotencyKey: KeyPrefix[TxAdminAdjustment] + key, CreatedBy: by, Memo: memo,
		Legs: []Leg{
			{Account: Account{Kind: CustomerWallet, Owner: customer}, Amount: credits},
			{Account: Account{Kind: PlatformAdjustments}, Amount: -credits},
		},
	})
	switch {
	case errors.Is(err, ErrInsufficient):
		return Posted{}, problem.New(genproblems.AdjustmentInsufficientBalance, "")
	case errors.Is(err, ErrKeyReused):
		return Posted{}, problem.New(genproblems.Conflict, "this idempotency key posted another adjustment")
	case err != nil:
		return Posted{}, asProblem(err)
	}
	return res, nil
}

// ApproveAdjustment posts a pending request; the approver is never the
// requester (D-05).
func (a *Admin) ApproveAdjustment(ctx context.Context, p authn.Principal, requestID ids.ID, note *string) (api.CreditAdjustmentResult, error) {
	if err := a.writeReady(ctx); err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	var cleaned *string
	if note != nil {
		n, err := cleanText("body.note", *note, maxAdjustReason)
		if err != nil {
			return api.CreditAdjustmentResult{}, err
		}
		cleaned = &n
	}
	var out api.CreditAdjustmentResult
	err := a.s.db.InTx(ctx, func(ctx context.Context) error {
		cq := a.s.q(ctx)
		r, err := cq.LockAdjustmentRequest(ctx, ids.UUID(requestID))
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		if mustID(r.RequestedBy) == p.UserID {
			return problem.New(genproblems.ApproverIsRequester, "")
		}
		if r.Status != "pending" {
			return problem.New(genproblems.InvalidTransition, "the request is "+r.Status)
		}
		// The customer may have deleted their account since the request.
		live, err := cq.CustomerExists(ctx, r.CustomerID)
		if err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		if !live {
			return problem.New(genproblems.InvalidTransition, "the customer's account is deleted")
		}
		res, err := a.post(ctx, p.UserID, mustID(r.CustomerID), int64(r.Amount), requestID, requestID.String(), r.Reason)
		if err != nil {
			return err
		}
		posted := res.TransactionID
		now := a.s.now()
		if err := cq.ApproveAdjustmentRequest(ctx, creditsdb.ApproveAdjustmentRequestParams{
			ID: r.ID, ApprovedBy: ids.UUID(p.UserID), Now: &now, DecisionNote: cleaned, TransactionID: ids.UUID(posted),
		}); err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		if _, err := a.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: "credits.adjustment_approve",
			SubjectType: "credit_adjustment", SubjectID: requestID.String(),
			Before: map[string]any{"status": r.Status, "version": r.Version},
			After: map[string]any{
				"status": "approved", "version": r.Version + 1, "note": cleaned, "transactionId": posted.String(),
				"customerId": mustID(r.CustomerID).String(), "amountPaise": int(r.Amount) * paisePerCredit,
			},
		}); err != nil {
			return err
		}
		v, err := cq.AdjustmentRequest(ctx, r.ID)
		if err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		view, t := adjustmentView(v, p.UserID), posted.String()
		out = api.CreditAdjustmentResult{Outcome: "approved", Request: &view, TransactionId: &t}
		return nil
	})
	if err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	out.ServerNow = a.s.now()
	return out, nil
}

// RejectAdjustment closes a pending request without posting: a second
// person rejects it; its requester's own reject cancels it.
func (a *Admin) RejectAdjustment(ctx context.Context, p authn.Principal, requestID ids.ID, note string) (api.CreditAdjustmentResult, error) {
	if err := a.writeReady(ctx); err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	n, err := cleanText("body.note", note, maxAdjustReason)
	if err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	var out api.CreditAdjustmentResult
	err = a.s.db.InTx(ctx, func(ctx context.Context) error {
		cq := a.s.q(ctx)
		r, err := cq.LockAdjustmentRequest(ctx, ids.UUID(requestID))
		if db.IsNoRows(err) {
			return problem.New(genproblems.NotFound, "")
		}
		if err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		if r.Status != "pending" {
			return problem.New(genproblems.InvalidTransition, "the request is "+r.Status)
		}
		status, by := "rejected", ids.UUID(p.UserID)
		if mustID(r.RequestedBy) == p.UserID {
			status, by = "cancelled", pgtype.UUID{}
		}
		now := a.s.now()
		if err := cq.DecideAdjustmentRequest(ctx, creditsdb.DecideAdjustmentRequestParams{
			ID: r.ID, Status: status, ApprovedBy: by, Now: &now, DecisionNote: &n,
		}); err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		if _, err := a.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: map[string]string{"rejected": "credits.adjustment_reject", "cancelled": "credits.adjustment_cancel"}[status],
			SubjectType: "credit_adjustment", SubjectID: requestID.String(),
			Before: map[string]any{"status": r.Status, "version": r.Version},
			After:  map[string]any{"status": status, "version": r.Version + 1, "note": n},
		}); err != nil {
			return err
		}
		v, err := cq.AdjustmentRequest(ctx, r.ID)
		if err != nil {
			return problem.Wrap(genproblems.Internal, err)
		}
		view := adjustmentView(v, p.UserID)
		out = api.CreditAdjustmentResult{Outcome: api.CreditAdjustmentResultOutcome(status), Request: &view}
		return nil
	})
	if err != nil {
		return api.CreditAdjustmentResult{}, err
	}
	out.ServerNow = a.s.now()
	return out, nil
}

// --- helpers -----------------------------------------------------------------------

func cleanText(path, s string, maxRunes int) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > maxRunes {
		return "", adminFieldErr(path, "1 to "+strconv.Itoa(maxRunes)+" characters")
	}
	if strings.IndexFunc(s, func(r rune) bool { return unicode.IsControl(r) && r != '\n' }) >= 0 {
		return "", adminFieldErr(path, "must not hold control characters")
	}
	return s, nil
}

func adminFieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func isULID(s string) bool {
	_, err := ids.Parse(s)
	return len(s) == 26 && err == nil
}

// looksLikePhone: digits, spaces and a leading + only, 10 digits or more.
func looksLikePhone(s string) bool {
	n := 0
	for i, r := range s {
		switch {
		case r >= '0' && r <= '9':
			n++
		case r == ' ' || (r == '+' && i == 0):
		default:
			return false
		}
	}
	return n >= 10
}

// phoneE164 accepts +91 98765 43210, 919876543210 and 9876543210.
func phoneE164(s string) (string, bool) {
	d := strings.NewReplacer(" ", "", "+", "").Replace(s)
	switch {
	case len(d) == 10:
		d = "91" + d
	case len(d) == 12 && strings.HasPrefix(d, "91"):
	default:
		return "", false
	}
	return pii.NormalizePhone("+" + d)
}

func adminPage(limit *int, cursor *string) (int, *time.Time, pgtype.UUID, error) {
	lim := adminDefaultLimit
	if limit != nil {
		lim = min(max(*limit, 1), adminMaxLimit)
	}
	if cursor == nil || *cursor == "" {
		return lim, nil, pgtype.UUID{}, nil
	}
	bad := adminFieldErr("query.cursor", "not a cursor from this list")
	b, err := base64.RawURLEncoding.DecodeString(*cursor)
	if err != nil {
		return 0, nil, pgtype.UUID{}, bad
	}
	ns, sid, ok := strings.Cut(string(b), ".")
	if !ok {
		return 0, nil, pgtype.UUID{}, bad
	}
	n, err := strconv.ParseInt(ns, 10, 64)
	if err != nil {
		return 0, nil, pgtype.UUID{}, bad
	}
	id, err := ids.Parse(sid)
	if err != nil {
		return 0, nil, pgtype.UUID{}, bad
	}
	at := time.Unix(0, n).UTC()
	return lim, &at, ids.UUID(id), nil
}

func adminCursor(at time.Time, id ids.ID) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at.UnixNano(), 10) + "." + id.String()))
}

func openDate(d pgtype.Date) openapi_types.Date { return openapi_types.Date{Time: d.Time} }

func uuidString(u pgtype.UUID) string { return mustID(u).String() }

func optUUID(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	s := uuidString(u)
	return &s
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}
