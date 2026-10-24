package pos

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	posapi "github.com/todayz/backend/gen/api/pos"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/realtime"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// claimTTL is how long a bill QR can be confirmed (01 §6: 120 s).
const claimTTL = 120 * time.Second //settings:ok 01 §6 fixes the bill QR at 120 s; no platform setting exists

// A bill QR token is base64url(claim id ‖ nonce ‖ tag): 16 + 16 + 16 bytes.
// The tag is the first 16 bytes of HMAC-SHA256 under BILL_QR_SIGNING_KEY
// over a version label, the claim id, the nonce, the store, the amount and
// the expiry, so a token is bound to the row it was made for and a changed
// row (or key) no longer verifies.
const (
	tokenIDLen    = 16
	tokenNonceLen = 16
	tokenTagLen   = 16
	tokenLen      = tokenIDLen + tokenNonceLen + tokenTagLen
)

var errBadToken = errors.New("pos: bill claim token does not verify")

func (s *Service) claimTag(id ids.ID, nonce []byte, storeID ids.ID, amount int64, expires time.Time) []byte {
	m := hmac.New(sha256.New, s.billKey)
	m.Write([]byte("todayz.bill_claim.v1"))
	m.Write(id[:])
	m.Write(nonce)
	m.Write(storeID[:])
	var n [16]byte
	binary.BigEndian.PutUint64(n[:8], uint64(amount))              // #nosec G115 -- a claim amount is at least 100 paise
	binary.BigEndian.PutUint64(n[8:], uint64(expires.UnixMicro())) // #nosec G115 -- after 1970
	m.Write(n[:])
	return m.Sum(nil)[:tokenTagLen]
}

func (s *Service) claimToken(c posdb.BillClaim) string {
	id := mustID(c.ID)
	b := make([]byte, 0, tokenLen)
	b = append(b, id[:]...)
	b = append(b, c.TokenNonce...)
	b = append(b, s.claimTag(id, c.TokenNonce, mustID(c.StoreID), c.AmountPaise, c.ExpiresAt)...)
	return base64.RawURLEncoding.EncodeToString(b)
}

// tokenClaim is the claim id a token names, before its tag is checked
// against the row.
func tokenClaim(token string) (ids.ID, []byte, []byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != tokenLen {
		return ids.Zero, nil, nil, errBadToken
	}
	var id ids.ID
	copy(id[:], raw[:tokenIDLen])
	return id, raw[tokenIDLen : tokenIDLen+tokenNonceLen], raw[tokenIDLen+tokenNonceLen:], nil
}

// verify checks a token's nonce and tag against its claim row.
func (s *Service) verify(c posdb.BillClaim, nonce, tag []byte) bool {
	if !hmac.Equal(nonce, c.TokenNonce) {
		return false
	}
	want := s.claimTag(mustID(c.ID), c.TokenNonce, mustID(c.StoreID), c.AmountPaise, c.ExpiresAt)
	return hmac.Equal(tag, want)
}

// claimsReady also needs the signing key and the feature flag on.
func (s *Service) claimsReady(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.billKey == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	on, err := s.flag(ctx, settings.FeatureBillQrEnabled)
	if err != nil {
		return asProblem(err)
	}
	if !on {
		return problem.New(genproblems.Conflict, "")
	}
	return nil
}

// claimStatus is the API status: used once a sale took it, expired once it
// is past its 120 s, confirmed or not (D-131), or was cancelled.
func (s *Service) claimStatus(c posdb.BillClaim) posapi.BillClaimStatus {
	live := s.now().Before(c.ExpiresAt)
	switch {
	case c.PosSaleID.Valid:
		return posapi.Used
	case c.Status == "claimed" && live:
		return posapi.Claimed
	case c.Status == "open" && live:
		return posapi.Open
	}
	return posapi.Expired
}

func (s *Service) publishClaim(ctx context.Context, c posdb.BillClaim) error {
	t, err := tx(ctx)
	if err != nil {
		return err
	}
	return realtime.Publish(ctx, t, realtime.StoreChannel(mustID(c.StoreID)),
		realtime.Event{Type: BillClaimUpdated, ID: mustID(c.ID), Version: int64(c.Version)})
}

// --- the POS side ------------------------------------------------------------

// CreateClaim makes a bill QR (POST {S}/bill-claims).
func (s *Service) CreateClaim(ctx context.Context, sc authz.Scope, body posapi.BillClaimCreateRequest) (posapi.BillClaim, error) {
	if err := s.claimsReady(ctx); err != nil {
		return posapi.BillClaim{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.BillClaim{}, err
	}
	clientID, err := ids.Parse(body.SaleClientId)
	if err != nil {
		return posapi.BillClaim{}, fieldErr("body.saleClientId", "ulid")
	}
	// The ₹1 floor (01 §7): a QR is for a bill a customer still pays for.
	if body.AmountPaise < 100 || body.AmountPaise > 10_000_000_000 {
		return posapi.BillClaim{}, fieldErr("body.amountPaise", "range")
	}
	nonce, err := s.random(tokenNonceLen)
	if err != nil {
		return posapi.BillClaim{}, asProblem(err)
	}
	var c posdb.BillClaim
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		now := s.now()
		id := s.ids.New(now)
		// A new QR for the bill supersedes the earlier ones (D-131): one
		// live claim per bill, and no hold left behind.
		if err := s.cancelClaims(ctx, sc.StoreID, clientID, id, now); err != nil {
			return err
		}
		var err error
		c, err = s.q(ctx).InsertBillClaim(ctx, posdb.InsertBillClaimParams{
			ID: ids.UUID(id), StoreID: ids.UUID(sc.StoreID), SaleClientID: ids.UUID(clientID),
			AmountPaise: int64(body.AmountPaise), TokenNonce: nonce, ExpiresAt: now.Add(claimTTL),
			CreatedBy: ids.UUID(p.UserID), Now: now,
		})
		if err != nil {
			return fmt.Errorf("pos: insert bill claim: %w", err)
		}
		return nil
	})
	if err != nil {
		return posapi.BillClaim{}, asProblem(err)
	}
	v := s.claimView(c, nil)
	v.Token = ptr(s.claimToken(c))
	return v, nil
}

// GetClaim is a bill QR's state for the POS (GET {S}/bill-claims/{id}).
func (s *Service) GetClaim(ctx context.Context, sc authz.Scope, id ids.ID) (posapi.BillClaim, error) {
	if err := s.ready(ctx); err != nil {
		return posapi.BillClaim{}, err
	}
	r, err := s.q(ctx).GetBillClaim(ctx, ids.UUID(id))
	if isNoRows(err) || (err == nil && mustID(r.StoreID) != sc.StoreID) {
		return posapi.BillClaim{}, notFound()
	}
	if err != nil {
		return posapi.BillClaim{}, asProblem(fmt.Errorf("pos: bill claim: %w", err))
	}
	c := posdb.BillClaim{
		ID: r.ID, StoreID: r.StoreID, SaleClientID: r.SaleClientID, PosSaleID: r.PosSaleID,
		AmountPaise: r.AmountPaise, TokenNonce: r.TokenNonce, ExpiresAt: r.ExpiresAt, CreatedBy: r.CreatedBy,
		ClaimedBy: r.ClaimedBy, ClaimedAt: r.ClaimedAt, CreditsRequested: r.CreditsRequested,
		CreditsUsed: r.CreditsUsed, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version,
	}
	return s.claimView(c, r.CustomerName), nil
}

func (s *Service) claimView(c posdb.BillClaim, customer *string) posapi.BillClaim {
	v := posapi.BillClaim{
		Id: mustID(c.ID).String(), SaleClientId: mustID(c.SaleClientID).String(), AmountPaise: int(c.AmountPaise),
		CreditsUsed: int(c.CreditsUsed), ExpiresAt: c.ExpiresAt, PosSaleId: idString(c.PosSaleID),
		Status: s.claimStatus(c), Version: int(c.Version), ServerNow: s.now(),
	}
	if c.ClaimedBy.Valid && customer != nil && *customer != "" {
		v.CustomerName = customer
	}
	return v
}

// cancelClaims cancels the bill's live claims other than keep (zero keeps
// none) and releases the holds of those a customer confirmed (D-131).
func (s *Service) cancelClaims(ctx context.Context, storeID, clientID, keep ids.ID, now time.Time) error {
	rows, err := s.q(ctx).CancelBillClaims(ctx, posdb.CancelBillClaimsParams{
		Now: now, StoreID: ids.UUID(storeID), SaleClientID: ids.UUID(clientID), KeepID: nullID(keep),
	})
	if err != nil {
		return fmt.Errorf("pos: cancel bill claims: %w", err)
	}
	for _, r := range rows {
		if !r.ClaimedBy.Valid {
			continue
		}
		if err := s.credits.Release(ctx, mustID(r.ID)); err != nil {
			return fmt.Errorf("pos: release credits: %w", err)
		}
	}
	return nil
}

// claimSweep is how many claims one page of ExpireClaims takes.
const claimSweep = 500

// Sweep is one ExpireClaims pass: the claims it expired, the holds it
// released and the claims it left live because their release failed.
type Sweep struct{ Expired, Released, Failed int }

// ExpireClaims expires the live claims past their 120 s and releases the
// holds of those a customer confirmed (the pos_bill_claim_expiry job,
// D-131). It pages through every claim due at the pass's now by the keyset
// (expires_at, id), one transaction a page, until the due set is done or
// ctx (the job's timeout) ends. Each claim is its own savepoint: a claim
// whose release fails is logged by id, counted in
// tz_pos_claim_release_failed_total and left live for the next pass, and
// the page goes on past it, so failing claims never hold back younger ones.
func (s *Service) ExpireClaims(ctx context.Context) (Sweep, error) {
	var out Sweep
	if err := s.ready(ctx); err != nil {
		return out, err
	}
	now := s.now()
	// The keyset starts before every claim (a NULL id would match none).
	after := posdb.DueBillClaimsParams{Now: now, AfterID: pgtype.UUID{Valid: true}, Lim: claimSweep}
	for {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		var page Sweep
		n := 0
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			page = Sweep{}
			rows, err := s.q(ctx).DueBillClaims(ctx, after)
			if err != nil {
				return fmt.Errorf("pos: due bill claims: %w", err)
			}
			n = len(rows)
			for _, r := range rows {
				if err := s.expireClaim(ctx, r, now, &page); err != nil {
					return err
				}
				after.AfterAt, after.AfterID = r.ExpiresAt, r.ID
			}
			return nil
		})
		if err != nil {
			return out, err
		}
		out.Expired, out.Released, out.Failed = out.Expired+page.Expired, out.Released+page.Released, out.Failed+page.Failed
		if n < claimSweep {
			return out, nil
		}
	}
}

// expireClaim expires one due claim in its own savepoint and releases its
// hold; a failed release rolls the claim back, is counted and leaves it live.
func (s *Service) expireClaim(ctx context.Context, r posdb.DueBillClaimsRow, now time.Time, out *Sweep) error {
	id := mustID(r.ID)
	released := false
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		n, err := s.q(ctx).ExpireBillClaim(ctx, posdb.ExpireBillClaimParams{ID: r.ID, Now: now})
		if err != nil || n == 0 {
			return err
		}
		if r.ClaimedBy.Valid {
			if err := s.credits.Release(ctx, id); err != nil {
				return fmt.Errorf("%w: %w", errRelease, err)
			}
			released = true
		}
		return nil
	})
	switch {
	case errors.Is(err, errRelease):
		out.Failed++
		claimReleaseFailed(ctx)
		telemetry.Logger(ctx).ErrorContext(ctx, "pos: claim hold not released; retried next pass", "claim_id", id.String(), "err", err)
	case err != nil:
		return fmt.Errorf("pos: expire bill claim: %w", err)
	default:
		out.Expired++
		if released {
			out.Released++
		}
	}
	return nil
}

// errRelease marks a claim whose hold the ledger did not release.
var errRelease = errors.New("pos: release credits")

var (
	metricsOnce         sync.Once
	mClaimReleaseFailed metric.Int64Counter
)

// claimReleaseFailed counts a claim the sweep left live because its hold's
// release failed (tz_pos_claim_release_failed_total; alert
// tz-pos-claim-release-failed in infra/grafana/alerts/pos.yaml).
func claimReleaseFailed(ctx context.Context) {
	metricsOnce.Do(func() {
		m := otel.Meter("todayz/pos")
		mClaimReleaseFailed, _ = m.Int64Counter(telemetry.MetricPrefix+"pos_claim_release_failed_total",
			metric.WithDescription("Expired bill claims whose credit hold the sweep could not release (retried next pass)"))
	})
	if mClaimReleaseFailed != nil {
		mClaimReleaseFailed.Add(ctx, 1)
	}
}

// --- the customer side -------------------------------------------------------

// claimByToken reads (or locks) the claim a token names and checks the
// token. A token that does not verify is 404, never a hint at why.
func (s *Service) claimByToken(ctx context.Context, token string, lock bool) (posdb.BillClaim, error) {
	id, nonce, tag, err := tokenClaim(token)
	if err != nil {
		return posdb.BillClaim{}, notFound()
	}
	q := s.q(ctx)
	var c posdb.BillClaim
	if lock {
		c, err = q.LockBillClaim(ctx, ids.UUID(id))
	} else {
		var r posdb.GetBillClaimRow
		r, err = q.GetBillClaim(ctx, ids.UUID(id))
		c = posdb.BillClaim{
			ID: r.ID, StoreID: r.StoreID, SaleClientID: r.SaleClientID, PosSaleID: r.PosSaleID,
			AmountPaise: r.AmountPaise, TokenNonce: r.TokenNonce, ExpiresAt: r.ExpiresAt, CreatedBy: r.CreatedBy,
			ClaimedBy: r.ClaimedBy, ClaimedAt: r.ClaimedAt, CreditsRequested: r.CreditsRequested,
			CreditsUsed: r.CreditsUsed, Status: r.Status, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, Version: r.Version,
		}
	}
	if isNoRows(err) {
		return c, notFound()
	}
	if err != nil {
		return c, fmt.Errorf("pos: bill claim: %w", err)
	}
	if !s.verify(c, nonce, tag) {
		return c, notFound()
	}
	return c, nil
}

// claimOpen refuses a claim that is no longer open to the customer: used or
// confirmed by anyone, the same customer included (claim_already_used), past
// 120 s or cancelled (claim_expired).
func (s *Service) claimOpen(c posdb.BillClaim) error {
	switch {
	case c.PosSaleID.Valid || c.Status == "claimed":
		return problem.New(genproblems.ClaimAlreadyUsed, "")
	case c.Status != "open" || !s.now().Before(c.ExpiresAt):
		return problem.New(genproblems.ClaimExpired, "")
	}
	return nil
}

// allowance is the most credits a bill of amount paise allows the customer:
// none under credit_min_order_value_paise (D-30), never past ₹1 still to
// pay, and within the wallet's caps (the Credits seam).
func (s *Service) allowance(ctx context.Context, customerID, storeID ids.ID, amount int64, requested int) (CreditsAllowance, error) {
	minOrder, err := s.paise(ctx, settings.CreditMinOrderValuePaise)
	if err != nil {
		return CreditsAllowance{}, err
	}
	if amount < minOrder {
		return CreditsAllowance{Limit: string(posapi.BelowCreditMinimum)}, nil
	}
	maxPer, err := s.count(ctx, settings.CreditMaxPerOrder)
	if err != nil {
		return CreditsAllowance{}, err
	}
	floor := int((amount - 100) / 100) // leave at least ₹1 to pay (01 §7)
	want := min(requested, maxPer, floor)
	a, err := s.credits.Allow(ctx, customerID, storeID, want, amount)
	if err != nil {
		return CreditsAllowance{}, fmt.Errorf("pos: credits allow: %w", err)
	}
	a.Allowed = min(max(a.Allowed, 0), want)
	if a.Limit == "" && a.Allowed < requested && requested > 0 {
		a.Limit = string(posapi.CreditMaxExceeded)
	}
	return a, nil
}

// PreviewClaim is what a scanned QR is for (POST /v1/me/bill-claims/{token}/preview).
func (s *Service) PreviewClaim(ctx context.Context, token string) (posapi.BillClaimPreview, error) {
	if err := s.claimsReady(ctx); err != nil {
		return posapi.BillClaimPreview{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.BillClaimPreview{}, err
	}
	c, err := s.claimByToken(ctx, token, false)
	if err != nil {
		return posapi.BillClaimPreview{}, asProblem(err)
	}
	if err := s.claimOpen(c); err != nil {
		return posapi.BillClaimPreview{}, err
	}
	st, err := s.store(ctx, mustID(c.StoreID))
	if err != nil {
		return posapi.BillClaimPreview{}, asProblem(err)
	}
	a, err := s.allowance(ctx, p.UserID, mustID(c.StoreID), c.AmountPaise, 1<<30)
	if err != nil {
		return posapi.BillClaimPreview{}, asProblem(err)
	}
	v := posapi.BillClaimPreview{
		ClaimId: mustID(c.ID).String(), AmountPaise: int(c.AmountPaise), ExpiresAt: c.ExpiresAt,
		MaxCredits: a.Allowed, Store: posapi.BillClaimStore{Id: mustID(st.ID).String(), Name: st.Name},
		ServerNow: s.now(),
	}
	if a.Limit != "" && a.Limit != string(posapi.CreditMaxExceeded) {
		v.CreditsLimit = ptr(posapi.BillClaimPreviewCreditsLimit(a.Limit))
	}
	return v, nil
}

// ConfirmClaim claims a bill with the customer's account and holds their
// credits (POST /v1/me/bill-claims/{token}/confirm). The claim row is held
// FOR UPDATE, so of two phones the first wins and the second gets
// claim_already_used.
func (s *Service) ConfirmClaim(ctx context.Context, token string, creditsToUse int) (posapi.BillClaimConfirmation, error) {
	if err := s.claimsReady(ctx); err != nil {
		return posapi.BillClaimConfirmation{}, err
	}
	p, err := principal(ctx)
	if err != nil {
		return posapi.BillClaimConfirmation{}, err
	}
	if creditsToUse < 0 || creditsToUse > 100_000 {
		return posapi.BillClaimConfirmation{}, fieldErr("body.creditsToUse", "range")
	}
	var c posdb.BillClaim
	var st posdb.StoreInfoRow
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		var err error
		if c, err = s.claimByToken(ctx, token, true); err != nil {
			return err
		}
		if err := s.claimOpen(c); err != nil {
			return err
		}
		storeID := mustID(c.StoreID)
		if st, err = s.store(ctx, storeID); err != nil {
			return err
		}
		a, err := s.allowance(ctx, p.UserID, storeID, c.AmountPaise, creditsToUse)
		if err != nil {
			return err
		}
		held := 0
		if a.Allowed > 0 {
			if held, err = s.credits.Hold(ctx, p.UserID, storeID, mustID(c.ID), a.Allowed); err != nil {
				return fmt.Errorf("pos: hold credits: %w", err)
			}
			held = min(max(held, 0), a.Allowed)
		}
		now := s.now()
		c, err = s.q(ctx).ClaimBillClaim(ctx, posdb.ClaimBillClaimParams{
			CustomerID: ids.UUID(p.UserID), Now: &now, CreditsRequested: i32(int64(creditsToUse)),
			CreditsUsed: i32(int64(held)), ID: c.ID,
		})
		if isNoRows(err) {
			return problem.New(genproblems.ClaimAlreadyUsed, "")
		}
		if err != nil {
			return fmt.Errorf("pos: claim bill: %w", err)
		}
		return s.publishClaim(ctx, c)
	})
	if err != nil {
		return posapi.BillClaimConfirmation{}, asProblem(err)
	}
	payable := c.AmountPaise - int64(c.CreditsUsed)*100
	return posapi.BillClaimConfirmation{
		ClaimId: mustID(c.ID).String(), AmountPaise: int(c.AmountPaise), CreditsUsed: int(c.CreditsUsed),
		PayablePaise: int(payable + int64(roundOff(payable))), Status: s.claimStatus(c),
		Store: posapi.BillClaimStore{Id: mustID(st.ID).String(), Name: st.Name}, ServerNow: s.now(),
	}, nil
}
