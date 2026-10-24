package referrals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/todayz/backend/gen/sqlc/referralsdb"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	platformjobs "github.com/todayz/backend/internal/platform/jobs"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// Rewards post after the purchase commits (D-169). The purchase's
// transaction only marks the referral qualified and enqueues KindReward; the
// job posts each side in a transaction of its own, so one posting takes one
// ledger lock call and never runs while a purchase holds its accounts. The
// sweep is the backstop for a job that never ran and retries a side left
// unposted, backing off per referral (attempts, next_attempt_at).
//
// Lock order: ledger accounts first, the referral row last. A purchase and a
// reversal take them in that order too (their credit postings, then the
// referrals hook), so a posting cannot deadlock with either.

// KindReward is the event job that posts a qualified referral's rewards
// (internal/jobs/referrals.go registers it). Its args are RewardArgs; it is
// unique per referral while pending or running.
const KindReward = "referral_reward"

// RewardArgs are KindReward's args.
type RewardArgs struct {
	ReferralID ids.ID `json:"referral_id"`
}

// The retry backoff for a side left unposted: rewardRetryBase after the
// first failed pass, doubling per pass, at most rewardRetryMax. Operational
// pacing, not a product rule.
const (
	rewardRetryBase = 10 * time.Minute
	rewardRetryMax  = 6 * time.Hour
)

func rewardBackoff(attempts int32) time.Duration {
	d := rewardRetryBase
	for i := int32(0); i < attempts && d < rewardRetryMax; i++ {
		d *= 2
	}
	return min(d, rewardRetryMax)
}

// ruleRewardRefused is the risk flag on a referral whose reward the ledger
// refused for good (D-169).
const ruleRewardRefused = "referral.reward_refused"

// Reasons in tz_referral_reward_failed_total: one per side a pass left
// unposted.
const (
	failLedger  = "ledger_error" // the ledger errored or did not post; retried with the backoff
	failRefused = "refused"      // the ledger refused for good; parked until a person clears the flag
	failError   = "error"        // any other error in the side's transaction; retried with the backoff
	failWaiting = "waiting"      // the beneficiary is not payable yet (no live store); retried with the backoff
	failBlocked = "blocked"      // a blocked customer referrer (D-168); retried once unblocked
)

func init() {
	telemetry.RegisterLabelValues("reason", failLedger, failRefused, failError, failWaiting, failBlocked)
}

var (
	metricsOnce sync.Once
	mFailed     metric.Int64Counter
)

func countFailed(ctx context.Context, reason string) {
	metricsOnce.Do(func() {
		mFailed, _ = otel.Meter("todayz/referrals").Int64Counter("tz_referral_reward_failed_total",
			metric.WithDescription("Referral reward sides left unposted by a posting pass, by reason"))
	})
	if mFailed != nil {
		mFailed.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", telemetry.LabelValue("reason", reason))))
	}
}

// enqueueReward enqueues KindReward for a referral the caller's transaction
// just qualified. It runs in a savepoint and only logs a failure: the
// referral is due at once, so the sweep posts it.
func (s *Service) enqueueReward(ctx context.Context, id pgtype.UUID) {
	if s.jobs == nil {
		return
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		tx, ok := db.TxFrom(ctx)
		if !ok {
			return errors.New("referrals: enqueue outside a transaction")
		}
		return s.jobs.EnqueueTx(ctx, tx, KindReward, RewardArgs{ReferralID: idOf(id)},
			platformjobs.EnqueueOpts{UniqueKey: "referral:" + idOf(id).String()})
	})
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "referrals: reward job not enqueued; the sweep posts it", "referral", idOf(id).String(), "err", err)
	}
}

// beneficiary states.
const (
	payPost    = iota // post to the account
	payWait           // not payable yet (no live store): try again with the backoff
	payBlocked        // a blocked customer referrer: the sweep skips it until unblocked (D-168)
	payVoid           // nobody to pay (a deleted customer): mark done unposted
)

// beneficiary is the account one side's reward goes to: a customer's wallet,
// or the primary store of a business (a store referrer's business too,
// 01 §8, D-72). A deleted customer is void; a blocked customer referrer
// waits until unblocked (D-168).
func (s *Service) beneficiary(ctx context.Context, side, partyType string, partyID pgtype.UUID) (Beneficiary, int, error) {
	q := s.q(ctx)
	switch partyType {
	case partyCustomer:
		status, err := q.UserStatus(ctx, partyID)
		switch {
		case db.IsNoRows(err):
			return Beneficiary{}, payVoid, nil
		case err != nil:
			return Beneficiary{}, 0, err
		}
		switch {
		case status == "deleted":
			return Beneficiary{}, payVoid, nil
		case status == "blocked" && side == SideReferrer:
			return Beneficiary{}, payBlocked, nil
		}
		return Beneficiary{Kind: BeneficiaryCustomer, ID: idOf(partyID)}, payPost, nil
	case partyStore, partyBusiness:
		biz := partyID
		if partyType == partyStore {
			b, err := q.StoreBusiness(ctx, partyID)
			if err != nil {
				return Beneficiary{}, 0, err
			}
			biz = b
		}
		st, err := q.PrimaryStore(ctx, biz)
		if err != nil {
			if db.IsNoRows(err) {
				return Beneficiary{}, payWait, nil
			}
			return Beneficiary{}, 0, err
		}
		return Beneficiary{Kind: BeneficiaryStore, ID: idOf(st)}, payPost, nil
	}
	return Beneficiary{}, 0, fmt.Errorf("referrals: unknown party %q", partyType)
}

// side is one side of a referral row.
type side struct {
	name    string
	done    bool
	typ     string
	id      pgtype.UUID
	credits int32
}

func sideOf(row referralsdb.Referral, name string) side {
	if name == SideReferrer {
		return side{name, row.ReferrerRewardedAt != nil, row.ReferrerType, row.ReferrerID, row.ReferrerCredits}
	}
	return side{name, row.RefereeRewardedAt != nil, row.RefereeType, row.RefereeID, row.RefereeCredits}
}

// RewardKey is the ledger idempotency key of one side's reward.
func RewardKey(referralID ids.ID, side string) string {
	return "referral:" + referralID.String() + ":" + side
}

// Outcomes of one side's posting pass.
const (
	sideNothing = iota // nothing to do (already done, no longer qualified, changed under the pass)
	sidePosted         // posted, or marked done unposted (void, zero credits)
	sideWait           // not payable yet: retried with the backoff
	sideBlocked        // a blocked referrer: retried once unblocked
)

// errStale rolls a side's transaction back when the referral changed between
// the unlocked read and the lock (reversed, or posted by another pass).
var errStale = errors.New("referrals: referral changed while posting")

// errBeneficiaryChanged rolls a side's transaction back when the beneficiary
// changed between the first read and the ledger lock (a customer deleted or
// blocked meanwhile); the pass tries the side once more and sees the change.
var errBeneficiaryChanged = errors.New("referrals: beneficiary changed while posting")

// ledgerError marks a ledger failure (an error, or nothing posted).
type ledgerError struct{ err error }

func (e ledgerError) Error() string { return "referrals: ledger: " + e.err.Error() }
func (e ledgerError) Unwrap() error { return e.err }

var errNotPosted = errors.New("the ledger did not post")

// postSide posts one side of a qualified referral in a transaction of its
// own. The referral is read unlocked, the ledger posts (one lock call on its
// accounts), the beneficiary is read again under that lock, then the
// referral is locked and checked again: if either changed, the whole
// transaction, posting included, rolls back.
//
// The beneficiary recheck closes a race with account deletion: deletion
// forfeits the wallet under the same wallet lock, in the transaction that
// marks the user deleted. If it committed first, the recheck sees deleted;
// if it comes after, it forfeits the lot this posting made.
func (s *Service) postSide(ctx context.Context, id pgtype.UUID, name string) (int, error) {
	out := sideNothing
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		row, err := q.GetReferral(ctx, id)
		if err != nil {
			if db.IsNoRows(err) {
				return nil
			}
			return err
		}
		sd := sideOf(row, name)
		if row.Status != statusQualified || sd.done {
			return nil
		}
		to, pay, err := s.beneficiary(ctx, name, sd.typ, sd.id)
		if err != nil {
			return err
		}
		switch pay {
		case payWait:
			out = sideWait
			return nil
		case payBlocked:
			out = sideBlocked
			return nil
		}
		var txID ids.ID
		if pay == payPost && sd.credits > 0 {
			posted := false
			txID, posted, err = s.ledger.PostReward(ctx, Reward{
				ReferralID: idOf(row.ID), Side: name, To: to, Credits: int(sd.credits),
				IdempotencyKey: RewardKey(idOf(row.ID), name),
			})
			switch {
			case errors.Is(err, ErrRewardRefused):
				return err
			case err != nil:
				return ledgerError{err}
			case !posted:
				return ledgerError{errNotPosted}
			}
			again, pay2, err := s.beneficiary(ctx, name, sd.typ, sd.id)
			if err != nil {
				return err
			}
			if pay2 != payPost || again != to {
				return errBeneficiaryChanged
			}
		}
		row, err = q.LockReferral(ctx, id)
		if err != nil {
			return err
		}
		if row.Status != statusQualified || sideOf(row, name).done {
			return errStale
		}
		row, err = q.SetSideRewarded(ctx, referralsdb.SetSideRewardedParams{Side: name, TransactionID: optUUID(txID), Now: s.now(), ID: row.ID})
		if err != nil {
			return err
		}
		if txID != ids.Zero {
			s.notify(ctx, row, to, int(sd.credits))
		}
		out = sidePosted
		return nil
	})
	if errors.Is(err, errStale) {
		return sideNothing, nil
	}
	if err != nil {
		return sideNothing, err
	}
	return out, nil
}

// RewardResult is what one posting pass did for a referral.
type RewardResult struct {
	Posted  int  // sides posted (or marked done) in this pass
	Done    bool // the referral is rewarded
	Waiting bool // a side is still unposted; the next try is at next_attempt_at
}

// PostRewards posts each unposted side of a qualified referral, each side in
// its own transaction (the referral_reward job and the sweep call it; never
// inside a caller's transaction). A side left unposted records an attempt:
// a ledger error or a beneficiary not payable yet backs off, a refusal
// raises referral.reward_refused (the sweep skips the referral until a
// person clears it). The error is a database error recording the outcome.
func (s *Service) PostRewards(ctx context.Context, referralID ids.ID) (RewardResult, error) {
	var res RewardResult
	if s.db == nil {
		return res, errUnavailable
	}
	if _, ok := db.TxFrom(ctx); ok {
		return res, errors.New("referrals: rewards post in their own transactions, not the caller's")
	}
	id := ids.UUID(referralID)
	log := telemetry.Logger(ctx)
	backoff, unposted := false, false
	for _, name := range []string{SideReferrer, SideReferee} {
		out, err := s.postSide(ctx, id, name)
		if errors.Is(err, errBeneficiaryChanged) {
			out, err = s.postSide(ctx, id, name)
		}
		switch {
		case errors.Is(err, ErrRewardRefused):
			countFailed(ctx, failRefused)
			log.ErrorContext(ctx, "referrals: reward refused by the ledger", "referral", referralID.String(), "side", name, "err", err)
			if err := s.flagRefused(ctx, id, name); err != nil {
				return res, err
			}
			unposted = true
		case err != nil:
			reason := failError
			var le ledgerError
			if errors.As(err, &le) {
				reason = failLedger
			}
			countFailed(ctx, reason)
			log.WarnContext(ctx, "referrals: reward not posted", "referral", referralID.String(), "side", name, "reason", reason, "err", err)
			unposted, backoff = true, true
		case out == sidePosted:
			res.Posted++
		case out == sideWait:
			countFailed(ctx, failWaiting)
			unposted, backoff = true, true
		case out == sideBlocked:
			countFailed(ctx, failBlocked)
			unposted = true
		}
	}
	row, err := s.q(ctx).GetReferral(ctx, id)
	if err != nil {
		if db.IsNoRows(err) {
			return res, nil
		}
		return res, err
	}
	res.Done = row.Status == statusRewarded
	if !unposted || row.Status != statusQualified {
		return res, nil
	}
	res.Waiting = true
	return res, s.recordAttempt(ctx, row, backoff)
}

// recordAttempt records a pass that left a qualified referral with a side
// unposted: due again after the backoff, or at once (a blocked referrer and
// a refused reward are kept out of the batch by their own condition).
func (s *Service) recordAttempt(ctx context.Context, row referralsdb.Referral, backoff bool) error {
	if row.Status != statusQualified {
		return nil
	}
	now := s.now()
	next := now
	if backoff {
		next = now.Add(rewardBackoff(row.Attempts))
	}
	return s.q(ctx).RecordRewardAttempt(ctx, referralsdb.RecordRewardAttemptParams{Next: next, Now: now, ID: row.ID})
}

// refusedDetails is a referral.reward_refused flag's details.
type refusedDetails struct {
	Kind  string   `json:"kind"`
	Sides []string `json:"sides"`
}

// flagRefused raises referral.reward_refused for a side the ledger refused
// for good (D-169), in a transaction of its own (the side's rolled back).
// A referral has one such flag while it is open or confirmed: a second
// refused side is added to its sides, and a retried pass adds nothing. The
// referral's row lock orders concurrent passes. The sweep skips a referral
// with that flag open or confirmed; a person clears it once the cause is
// fixed.
func (s *Service) flagRefused(ctx context.Context, id pgtype.UUID, side string) error {
	return s.db.InTx(ctx, func(ctx context.Context) error {
		q, now := s.q(ctx), s.now()
		row, err := q.LockReferral(ctx, id)
		if err != nil {
			return err
		}
		flag, err := q.LockOpenRiskFlag(ctx, referralsdb.LockOpenRiskFlagParams{SubjectID: id, Rule: ruleRewardRefused})
		switch {
		case db.IsNoRows(err):
			details, _ := json.Marshal(refusedDetails{Kind: row.Kind, Sides: []string{side}})
			return q.InsertRiskFlag(ctx, referralsdb.InsertRiskFlagParams{
				ID: ids.UUID(s.ids.New(now)), SubjectType: "referral", SubjectID: id,
				Rule: ruleRewardRefused, Score: 50, Details: details, Now: now, //settings:ok a risk score
			})
		case err != nil:
			return err
		}
		var d refusedDetails
		if err := json.Unmarshal(flag.Details, &d); err != nil {
			return err
		}
		if slices.Contains(d.Sides, side) {
			return nil
		}
		d.Sides = append(d.Sides, side)
		if d.Kind == "" {
			d.Kind = row.Kind
		}
		details, _ := json.Marshal(d)
		return q.SetRiskFlagDetails(ctx, referralsdb.SetRiskFlagDetailsParams{Details: details, Now: now, ID: flag.ID})
	})
}

// notify sends "referral unlocked" to whoever was paid (01 §15: in-app +
// push). It runs in a savepoint and only logs a failure.
func (s *Service) notify(ctx context.Context, row referralsdb.Referral, to Beneficiary, credits int) {
	if s.notices == nil {
		return
	}
	n := notice.Notification{
		Params: map[string]string{"credits": strconv.Itoa(credits)},
		Ref:    &notice.Ref{Type: "referral", ID: idOf(row.ID)},
	}
	switch to.Kind {
	case BeneficiaryCustomer:
		n.Kind, n.UserID = notice.KindReferralUnlocked, to.ID
	case BeneficiaryStore:
		biz, err := s.q(ctx).StoreBusiness(ctx, ids.UUID(to.ID))
		if err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "referrals: notify: store business", "err", err)
			return
		}
		n.Kind, n.BusinessID, n.StoreID = notice.KindShopReferralUnlocked, idOf(biz), to.ID
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		tx, _ := db.TxFrom(ctx)
		_, err := s.notices.Send(ctx, tx, n)
		return err
	})
	if err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "referrals: notify", "referral", idOf(row.ID).String(), "err", err)
	}
}

// Sweep posts rewards that are due (a job that never ran, a side a pass left
// unposted whose backoff has passed) and qualifies shops whose verification
// came after their last counted sale. Each referral's sides post in their
// own transactions. waiting counts referrals a pass left with a side
// unposted. One referral's error does not stop the others: it is logged,
// the referral backs off, and the first error is returned at the end.
func (s *Service) Sweep(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{"rewarded": 0, "waiting": 0, "shops_qualified": 0}
	if s.db == nil || s.settings == nil {
		return out, errUnavailable
	}
	due, err := s.q(ctx).QualifiedReferrals(ctx, referralsdb.QualifiedReferralsParams{Now: s.now(), Lim: sweepBatch})
	if err != nil {
		return out, err
	}
	log := telemetry.Logger(ctx)
	var firstErr error
	fail := func(msg string, id pgtype.UUID, err error) {
		log.ErrorContext(ctx, msg, "referral", idOf(id).String(), "err", err)
		if firstErr == nil {
			firstErr = err
		}
	}
	for _, id := range due {
		res, err := s.PostRewards(ctx, idOf(id))
		if err != nil {
			countFailed(ctx, failError)
			fail("referrals: sweep: reward pass failed", id, err)
			out["waiting"]++
			// Back it off, so a referral that keeps failing cannot head
			// every batch.
			if row, err := s.q(ctx).GetReferral(ctx, id); err == nil {
				err = s.recordAttempt(ctx, row, true)
				if err != nil {
					log.WarnContext(ctx, "referrals: sweep: attempt not recorded", "referral", idOf(id).String(), "err", err)
				}
			}
			continue
		}
		switch {
		case res.Done:
			out["rewarded"]++
		case res.Waiting:
			out["waiting"]++
		}
	}
	pending, err := s.q(ctx).PendingShopReferrals(ctx, sweepBatch)
	if err != nil {
		return out, errors.Join(firstErr, err)
	}
	for _, id := range pending {
		var qualifiedNow bool
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			row, err := s.q(ctx).LockReferral(ctx, id)
			if err != nil {
				return err
			}
			if err := s.tryQualifyShop(ctx, row); err != nil {
				return err
			}
			after, err := s.q(ctx).GetReferral(ctx, id)
			qualifiedNow = err == nil && after.Status != statusPending
			return err
		})
		if err != nil {
			fail("referrals: sweep: shop qualification failed", id, err)
			continue
		}
		if qualifiedNow {
			out["shops_qualified"]++
		}
	}
	return out, firstErr
}
