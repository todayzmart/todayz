// Package risk is W3-02's risk and fraud work: the rules the risk_scan job
// runs, the A-13 flags queue (evidence, clear, confirm) and the A-10 referrals
// list with its reject (01 §2, §6–8, §13; 03 risk_flags; D-67, D-69, D-78,
// D-225).
//
// Rules this package keeps:
//
//   - A flag is a signal for a person. Nothing here blocks a customer or
//     suspends a business: confirming records the finding, and A-13 then
//     offers the customer's block or the business's suspend, their own
//     audited actions (W3-05). The one money effect is in payouts: a flag on
//     a business or its store holds that business's payout while it is open
//     or confirmed (01 §256), until someone clears it.
//   - Other packages raise their own flags on risk_flags (referral device
//     reuse, reward refused, offline counter-code links, the credits caps,
//     store strikes, unconfirmed returns); risk_scan raises the six rules in
//     rules.go. Every number comes from platform_settings.
//   - A rule raises at most one flag per subject and window (or IST day for
//     lockouts and bill claims; the OTP rules use their rolling window, so a
//     cleared flag does not come back at midnight), whatever the earlier
//     flag's status, so a cleared false positive is not raised again from
//     the same facts.
//   - Clear, confirm and a referral reject need a reason and write an
//     audit_log row in the same transaction. Confirm takes an open flag;
//     clear takes an open or confirmed one; reject takes a referral whose
//     reward has not posted.
//   - No phone number, OTP or code leaves this package: an ip or phone flag
//     is known by the first 16 bytes of its keyed hash, which A-13 never
//     shows.
package risk

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/riskdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

const (
	defaultLimit = 20  //settings:ok page size default, 04 §1
	maxLimit     = 100 //settings:ok page size cap, 04 §1

	// selfLikeLookback is how far back risk_scan looks for qualified
	// referrals; one already flagged is skipped, so the window only has to
	// outlast a worker outage.
	selfLikeLookback = 7 * 24 * time.Hour //settings:ok scan lookback, not a rule number
	// claimLookback is how far back risk_scan reads claimed bills; the rule's
	// own window is risk_bill_claim_window_minutes.
	claimLookback = 24 * time.Hour //settings:ok scan lookback, not a rule number
	// otpEvidenceWindow is how long otp_requests are kept (W1-01 purges them).
	otpEvidenceWindow = 24 * time.Hour //settings:ok retention of otp_requests, 03
)

// Deps is what the service needs. Only Clock is required: without the
// database, settings or (for clear, confirm and reject) the audit writer the
// operations that need them answer 503.
type Deps struct {
	DB       *db.DB
	Clock    clock.Clock
	Settings *settings.Cache
	Audit    *audit.Writer
	// OTPMaxAttempts is identity's attempts per OTP (W1-01): an OTP that used
	// them all without a right code counts for identity.otp_failed_phone.
	// Zero turns that rule off.
	OTPMaxAttempts int
}

// Service implements the A-13 and A-10 routes and the risk_scan job.
type Service struct {
	db       *db.DB
	clock    clock.Clock
	settings *settings.Cache
	audit    *audit.Writer
	otpMax   int
	ids      *ids.Generator
}

var errUnavailable = errors.New("risk: not configured")

// New builds the service.
func New(d Deps) (*Service, error) {
	if d.Clock == nil {
		return nil, errors.New("risk: a clock is required")
	}
	return &Service{db: d.DB, clock: d.Clock, settings: d.Settings, audit: d.Audit, otpMax: d.OTPMaxAttempts, ids: ids.NewGenerator()}, nil
}

func (s *Service) ready(ctx context.Context) error {
	if s.db == nil || s.settings == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "risk: not configured (needs the database and settings)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	return nil
}

func (s *Service) auditReady(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.audit == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errors.New("risk: the audit writer is not configured"))
	}
	return nil
}

func (s *Service) now() time.Time { return s.clock.Now().UTC().Truncate(time.Microsecond) }

func (s *Service) q(ctx context.Context) *riskdb.Queries { return riskdb.New(s.db.Q(ctx)) }

func (s *Service) count(ctx context.Context, k settings.Key) (int, error) {
	n, err := s.settings.Int(ctx, k)
	return int(n), err
}

// --- risk_scan ---------------------------------------------------------------------

// Scan runs the six rules once and raises what they find (the risk_scan job,
// every 5 minutes). It returns how many flags each rule raised.
//
// The run holds a transaction-scoped advisory lock: a run that overlaps one
// still going returns {"skipped": 1} and raises nothing, so two runs never
// race on the same dedupe check. Each rule runs in its own savepoint; a rule
// that fails is logged by name only (its error may quote row values), the
// others still run and commit, and Scan returns an error naming the failed
// rules so the job is retried.
func (s *Service) Scan(ctx context.Context) (map[string]int64, error) {
	if s.db == nil || s.settings == nil {
		return nil, errUnavailable
	}
	now := s.now()
	type rule struct {
		name string
		find func(context.Context, time.Time) ([]Finding, error)
	}
	rules := []rule{
		{RuleCodeLockouts, s.findLockouts},
		{RuleBillClaimSpread, s.findClaimSpread},
		{RuleReversalRatio, s.findReversals},
		{RuleSelfLike, s.findSelfLike},
		{RuleOTPIPFanout, s.findOTPFanout},
		{RuleOTPFailedPhone, s.findOTPFailed},
	}
	sum := make(map[string]int64, len(rules))
	var failed []string
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		got, err := s.q(ctx).TryScanLock(ctx)
		if err != nil {
			return err
		}
		if !got {
			telemetry.Logger(ctx).InfoContext(ctx, "risk_scan: another run holds the lock; skipped")
			sum["skipped"] = 1
			return nil
		}
		for _, r := range rules {
			var n int64
			err := s.db.InTx(ctx, func(ctx context.Context) error {
				fs, err := r.find(ctx, now)
				if err != nil {
					return err
				}
				n, err = s.raise(ctx, fs, now)
				return err
			})
			if err != nil {
				telemetry.Logger(ctx).ErrorContext(ctx, "risk_scan: rule failed", "rule", r.name, "cause", failCause(err))
				failed = append(failed, r.name)
				continue
			}
			sum[strings.ReplaceAll(r.name, ".", "_")] = n
		}
		return nil
	})
	if err != nil {
		return sum, err
	}
	if len(failed) > 0 {
		return sum, errors.New("risk_scan: rules failed: " + strings.Join(failed, ", "))
	}
	return sum, nil
}

// failCause names a failed rule's error for the log without its text or
// parameters: the SQLSTATE for a database error, else the error's Go type.
func failCause(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "sqlstate " + pgErr.Code
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline_exceeded"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return fmt.Sprintf("%T", err)
}

// raise inserts each finding unless its subject already has one (InsertFlagOnce).
func (s *Service) raise(ctx context.Context, fs []Finding, now time.Time) (int64, error) {
	var n int64
	q := s.q(ctx)
	for _, f := range fs {
		var day *string
		if f.Day != "" {
			day = &f.Day
		}
		c, err := q.InsertFlagOnce(ctx, riskdb.InsertFlagOnceParams{
			ID: ids.UUID(s.ids.New(now)), SubjectType: f.SubjectType, SubjectID: ids.UUID(f.SubjectID),
			Rule: f.Rule, Score: int32(f.Score), // #nosec G115 -- scores are 0–100 constants
			Details: mustJSON(f.Details), Now: now, Since: f.Since, Day: day,
		})
		if err != nil {
			return n, err
		}
		n += c
	}
	return n, nil
}

func (s *Service) findLockouts(ctx context.Context, now time.Time) ([]Finding, error) {
	limit, err := s.count(ctx, settings.RiskCodeLockoutsStorePerDay)
	if err != nil {
		return nil, err
	}
	today := clock.StartOfDayIST(now)
	var out []Finding
	// Yesterday too: a lockout in its last minutes is read by the first run after midnight.
	for _, day := range []time.Time{today.Add(-24 * time.Hour), today} {
		rows, err := s.q(ctx).LockoutCounts(ctx, riskdb.LockoutCountsParams{Since: day, Until: day.Add(24 * time.Hour)})
		if err != nil {
			return nil, err
		}
		cs := make([]StoreCount, 0, len(rows))
		for _, r := range rows {
			cs = append(cs, StoreCount{StoreID: mustID(r.StoreID), N: int(r.N)})
		}
		out = append(out, CodeLockouts(cs, limit, day)...)
	}
	return out, nil
}

func (s *Service) findClaimSpread(ctx context.Context, now time.Time) ([]Finding, error) {
	stores, err := s.count(ctx, settings.RiskBillClaimStores)
	if err != nil {
		return nil, err
	}
	window, err := s.settings.Duration(ctx, settings.RiskBillClaimWindowMin)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).BillClaimsSince(ctx, ptr(now.Add(-claimLookback-window)))
	if err != nil {
		return nil, err
	}
	cs := make([]Claim, 0, len(rows))
	for _, r := range rows {
		cs = append(cs, Claim{ID: mustID(r.ID), UserID: mustID(r.ClaimedBy), StoreID: mustID(r.StoreID), ClaimedAt: r.ClaimedAt})
	}
	return BillClaimSpread(cs, stores, window), nil
}

func (s *Service) findReversals(ctx context.Context, now time.Time) ([]Finding, error) {
	pct, err := s.count(ctx, settings.RiskReversalRatioPercent)
	if err != nil {
		return nil, err
	}
	minSales, err := s.count(ctx, settings.RiskReversalMinSales)
	if err != nil {
		return nil, err
	}
	window, err := s.settings.Duration(ctx, settings.RiskReversalWindowDays)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).ReversalCounts(ctx, now.Add(-window))
	if err != nil {
		return nil, err
	}
	rs := make([]Reversals, 0, len(rows))
	for _, r := range rows {
		rs = append(rs, Reversals{StoreID: mustID(r.StoreID), Sales: int(r.Sales), Reversed: int(r.Reversed)})
	}
	return ReversalRatio(rs, pct, minSales, window, now), nil
}

func (s *Service) findSelfLike(ctx context.Context, now time.Time) ([]Finding, error) {
	rows, err := s.q(ctx).SelfLikeCandidates(ctx, ptr(now.Add(-selfLikeLookback)))
	if err != nil {
		return nil, err
	}
	rs := make([]SelfLike, 0, len(rows))
	for _, r := range rows {
		rs = append(rs, SelfLike{
			ReferralID: mustID(r.ID), Kind: r.Kind, QualifyingRefType: derefStr(r.QualifyingRefType),
			QualifyingRefID: mustID(r.QualifyingRefID), OwnBusinessID: mustID(r.OwnBusinessID),
			OwnShop: r.OwnShop, RefereeStaff: r.RefereeStaff, SharedDevice: r.SharedDevice, StaffPhone: r.StaffPhone,
		})
	}
	return SelfLikeReferrals(rs), nil
}

func (s *Service) findOTPFanout(ctx context.Context, now time.Time) ([]Finding, error) {
	limit, err := s.count(ctx, settings.RiskOTPPhonesPerIPHour)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).OtpIPFanout(ctx, riskdb.OtpIPFanoutParams{Since: now.Add(-otpFanoutWindow), Lim: int32(limit)}) // #nosec G115 -- a setting bounded 2–100
	if err != nil {
		return nil, err
	}
	rs := make([]IPFanout, 0, len(rows))
	for _, r := range rows {
		rs = append(rs, IPFanout{IPHash: r.IpHash, Phones: int(r.Phones), Requests: int(r.Requests)})
	}
	return OTPIPFanout(rs, limit, now), nil
}

func (s *Service) findOTPFailed(ctx context.Context, now time.Time) ([]Finding, error) {
	if s.otpMax <= 0 {
		return nil, nil
	}
	limit, err := s.count(ctx, settings.RiskOTPFailedPerPhoneDay)
	if err != nil {
		return nil, err
	}
	rows, err := s.q(ctx).OtpFailedPhones(ctx, riskdb.OtpFailedPhonesParams{
		Since: now.Add(-otpFailedWindow), MaxAttempts: int32(s.otpMax), Lim: int32(limit), // #nosec G115 -- small bounded values
	})
	if err != nil {
		return nil, err
	}
	rs := make([]FailedPhone, 0, len(rows))
	for _, r := range rows {
		rs = append(rs, FailedPhone{PhoneHash: r.PhoneHash, Failed: int(r.Failed), UserID: mustID(r.UserID)})
	}
	return OTPFailedPhone(rs, limit, now), nil
}

// --- helpers -------------------------------------------------------------------------

func ptr[T any](v T) *T { return &v }

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

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
		return 0, nil, pgtype.UUID{}, fieldErr("query.cursor", "not a cursor from this list")
	}
	return lim, &at, ids.UUID(id), nil
}

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
		return time.Time{}, ids.Zero, errors.New("risk: cursor has no id")
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

func principal(ctx context.Context) (authn.Principal, error) {
	p, ok := authn.FromContext(ctx)
	if !ok || p.IsAnonymous() {
		return authn.Principal{}, problem.New(genproblems.Unauthorized, "")
	}
	return p, nil
}

func pathID(s string) (ids.ID, error) {
	id, err := ids.Parse(s)
	if err != nil {
		return ids.Zero, notFound()
	}
	return id, nil
}

// mustID is the id of a uuid column, zero for NULL.
func mustID(u pgtype.UUID) ids.ID {
	id, _ := ids.FromUUID(u)
	return id
}

// idPtr is a nullable uuid as an id string.
func idPtr(u pgtype.UUID) *string {
	if !u.Valid {
		return nil
	}
	return ptr(mustID(u).String())
}

func asProblem(err error) error {
	var p *problem.Error
	if errors.As(err, &p) {
		return p
	}
	return problem.Wrap(genproblems.Internal, err)
}

func fieldErr(path, reason string) *problem.Error {
	return problem.New(genproblems.ValidationFailed, "").WithField(path, reason)
}

func notFound() *problem.Error { return problem.New(genproblems.NotFound, "") }
