package risk

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
)

// The rules risk_scan runs itself (D-225). Each takes the facts its query
// read and the setting, and returns the flags to raise; none touches the
// database, so the fixtures in rules_test.go pin them.

// Rules this package raises.
const (
	RuleCodeLockouts    = "orders.code_lockouts"
	RuleBillClaimSpread = "pos.bill_claim_spread"
	RuleReversalRatio   = "pos.reversal_ratio"
	RuleSelfLike        = "referral.self_like"
	RuleOTPIPFanout     = "identity.otp_ip_fanout"
	RuleOTPFailedPhone  = "identity.otp_failed_phone"
)

// Scores (0–100) order the A-13 queue; they change through code review, not in
// the console (D-225). A flag never acts by itself, whatever its score.
const (
	scoreCodeLockouts    = 60 //settings:ok D-225 fixed score
	scoreBillClaimSpread = 50 //settings:ok D-225 fixed score
	scoreReversalRatio   = 50 //settings:ok D-225 fixed score
	scoreSelfLike        = 70 //settings:ok D-225 fixed score
	scoreOTPIPFanout     = 40 //settings:ok D-225 fixed score
	scoreOTPFailedPhone  = 30 //settings:ok D-225 fixed score
)

// Subject types this package uses besides store, user and referral: an IP
// address or a phone number with no account, both known only by a keyed hash.
const (
	SubjectIP    = "ip"
	SubjectPhone = "phone"
)

// Finding is one flag a rule wants raised.
type Finding struct {
	SubjectType string
	SubjectID   ids.ID
	Rule        string
	Score       int
	Details     map[string]any
	// Day, when set, is the IST day (2006-01-02) the flag is for: one flag per
	// subject, rule and day. It is also details.day.
	Day string
	// Since is the dedupe window: no second flag for the subject and rule
	// created at or after it.
	Since time.Time
}

// dayKey is t's IST calendar day.
func dayKey(t time.Time) string { return t.In(clock.IST).Format(time.DateOnly) }

// HashID is the subject id of an ip or phone flag: the first 16 bytes of its
// keyed hash. It is not reversible and holds no digit of the number.
func HashID(h []byte) (ids.ID, bool) {
	var id ids.ID
	if len(h) < len(id) {
		return id, false
	}
	copy(id[:], h[:len(id)])
	return id, true
}

// --- orders.code_lockouts -----------------------------------------------------------

// StoreCount is one store's count from a scan query.
type StoreCount struct {
	StoreID ids.ID
	N       int
}

// CodeLockouts flags each store whose code lockouts on the IST day starting at
// day pass limit (risk_code_lockouts_store_per_day, 01 §6): "more than", so
// with the setting at 2 the third lockout flags (ABUSE-M.05).
func CodeLockouts(counts []StoreCount, limit int, day time.Time) []Finding {
	var out []Finding
	key := dayKey(day)
	for _, c := range counts {
		if c.N <= limit {
			continue
		}
		out = append(out, Finding{
			SubjectType: "store", SubjectID: c.StoreID, Rule: RuleCodeLockouts, Score: scoreCodeLockouts,
			Details: map[string]any{"lockouts": c.N, "limit": limit, "day": key},
			Day:     key, Since: day.Add(-24 * time.Hour),
		})
	}
	return out
}

// --- pos.bill_claim_spread ----------------------------------------------------------

// Claim is one claimed bill.
type Claim struct {
	ID        ids.ID
	UserID    ids.ID
	StoreID   ids.ID
	ClaimedAt time.Time
}

// BillClaimSpread flags a customer who claimed bills at stores or more
// different stores within any window (risk_bill_claim_stores in
// risk_bill_claim_window_minutes, 01 §6). claims are ordered by user, then
// time. The flag lists the claims of the busiest window and is for the IST
// day of its last claim.
func BillClaimSpread(claims []Claim, stores int, window time.Duration) []Finding {
	var out []Finding
	for start := 0; start < len(claims); {
		end := start
		for end < len(claims) && claims[end].UserID == claims[start].UserID {
			end++
		}
		if f, ok := spreadOf(claims[start:end], stores, window); ok {
			out = append(out, f)
		}
		start = end
	}
	return out
}

func spreadOf(cs []Claim, stores int, window time.Duration) (Finding, bool) {
	best, bestLo, bestHi := 0, 0, 0
	seen := map[ids.ID]int{}
	lo := 0
	for hi, c := range cs {
		seen[c.StoreID]++
		for c.ClaimedAt.Sub(cs[lo].ClaimedAt) > window {
			if seen[cs[lo].StoreID]--; seen[cs[lo].StoreID] == 0 {
				delete(seen, cs[lo].StoreID)
			}
			lo++
		}
		if len(seen) > best {
			best, bestLo, bestHi = len(seen), lo, hi
		}
	}
	if best < stores {
		return Finding{}, false
	}
	last := cs[bestHi].ClaimedAt
	claimIDs := make([]string, 0, bestHi-bestLo+1)
	for _, c := range cs[bestLo : bestHi+1] {
		claimIDs = append(claimIDs, c.ID.String())
	}
	key := dayKey(last)
	return Finding{
		SubjectType: "user", SubjectID: cs[0].UserID, Rule: RuleBillClaimSpread, Score: scoreBillClaimSpread,
		Details: map[string]any{
			"stores": best, "claims": len(claimIDs), "limit": stores,
			"windowMinutes": int(window / time.Minute), "day": key, "claimIds": claimIDs,
		},
		Day: key, Since: clock.StartOfDayIST(last).Add(-24 * time.Hour),
	}, true
}

// --- pos.reversal_ratio -------------------------------------------------------------

// Reversals is one store's linked POS sales in the window and how many of them
// were reversed.
type Reversals struct {
	StoreID  ids.ID
	Sales    int
	Reversed int
}

// ReversalRatio flags a store whose reversed share of linked POS sales (bill
// QR or counter code) in the window reaches percent, once it has minSales of
// them (risk_reversal_ratio_percent, risk_reversal_min_sales,
// risk_reversal_window_days, 01 §7). One flag per store per window.
func ReversalRatio(rs []Reversals, percent, minSales int, window time.Duration, now time.Time) []Finding {
	var out []Finding
	for _, r := range rs {
		if r.Sales < minSales || r.Sales == 0 || r.Reversed*100 < percent*r.Sales {
			continue
		}
		out = append(out, Finding{
			SubjectType: "store", SubjectID: r.StoreID, Rule: RuleReversalRatio, Score: scoreReversalRatio,
			Details: map[string]any{
				"sales": r.Sales, "reversed": r.Reversed, "ratioPercent": r.Reversed * 100 / r.Sales,
				"limitPercent": percent, "minSales": minSales, "windowDays": int(window / (24 * time.Hour)),
			},
			Since: now.Add(-window),
		})
	}
	return out
}

// --- referral.self_like -------------------------------------------------------------

// SelfLike is a qualified referral and the signals SelfLikeCandidates read.
type SelfLike struct {
	ReferralID        ids.ID
	Kind              string
	QualifyingRefType string
	QualifyingRefID   ids.ID
	OwnBusinessID     ids.ID // zero when the purchase was not at the referrer's side
	OwnShop           bool
	RefereeStaff      bool
	SharedDevice      bool
	StaffPhone        bool
}

// SelfLikeReferrals flags a referral that looks like the referrer referring
// themself (01 §8, D-225). For a customer referee: the qualifying purchase was
// at a shop of the referrer's side and one more signal holds (the referee
// works there, shares a device with that side, or was invited there as
// staff). For a new business (any_shop): the referrer's side works at it.
// One flag per referral, ever.
func SelfLikeReferrals(rs []SelfLike) []Finding {
	var out []Finding
	for _, r := range rs {
		var signals []string
		if r.OwnShop {
			signals = append(signals, "own_shop")
		}
		if r.RefereeStaff {
			signals = append(signals, "referee_staff")
		}
		if r.SharedDevice {
			signals = append(signals, "shared_device")
		}
		if r.StaffPhone {
			signals = append(signals, "staff_phone")
		}
		hit := r.RefereeStaff && r.Kind == "any_shop" ||
			r.OwnShop && (r.RefereeStaff || r.SharedDevice || r.StaffPhone)
		if !hit {
			continue
		}
		d := map[string]any{"kind": r.Kind, "signals": signals}
		if r.QualifyingRefType != "" {
			d["qualifyingRefType"] = r.QualifyingRefType
			d["qualifyingRefId"] = r.QualifyingRefID.String()
		}
		if r.OwnBusinessID != ids.Zero {
			d["businessId"] = r.OwnBusinessID.String()
		}
		out = append(out, Finding{
			SubjectType: "referral", SubjectID: r.ReferralID, Rule: RuleSelfLike, Score: scoreSelfLike, Details: d,
		})
	}
	return out
}

// --- identity.otp_* -------------------------------------------------------------------

// IPFanout is one IP that asked OTPs for many phones in the hour.
type IPFanout struct {
	IPHash   []byte
	Phones   int
	Requests int
}

// OTPIPFanout flags each IP that asked OTPs for limit or more different
// phones in the hour before now (risk_otp_phones_per_ip_hour, 01 §2). One
// flag per IP per IST day. The details hold counts only, never a hash.
func OTPIPFanout(rows []IPFanout, limit int, now time.Time) []Finding {
	var out []Finding
	key := dayKey(now)
	for _, r := range rows {
		id, ok := HashID(r.IPHash)
		if !ok || r.Phones < limit {
			continue
		}
		out = append(out, Finding{
			SubjectType: SubjectIP, SubjectID: id, Rule: RuleOTPIPFanout, Score: scoreOTPIPFanout,
			Details: map[string]any{"phones": r.Phones, "requests": r.Requests, "limit": limit, "windowMinutes": 60, "day": key},
			Day:     key, Since: clock.StartOfDayIST(now),
		})
	}
	return out
}

// FailedPhone is one phone whose OTPs ran out of attempts.
type FailedPhone struct {
	PhoneHash []byte
	Failed    int
	UserID    ids.ID // zero when the phone has no account
}

// OTPFailedPhone flags a phone with limit or more OTPs in 24 h that used every
// attempt without a right code (risk_otp_failed_per_phone_day, 01 §2): on the
// account when the phone has one, else on the phone's hash. One flag per
// subject per IST day.
func OTPFailedPhone(rows []FailedPhone, limit int, now time.Time) []Finding {
	var out []Finding
	key := dayKey(now)
	for _, r := range rows {
		if r.Failed < limit {
			continue
		}
		f := Finding{
			Rule: RuleOTPFailedPhone, Score: scoreOTPFailedPhone,
			Details: map[string]any{"failedOtps": r.Failed, "limit": limit, "windowHours": 24, "day": key},
			Day:     key, Since: clock.StartOfDayIST(now),
		}
		if r.UserID != ids.Zero {
			f.SubjectType, f.SubjectID = "user", r.UserID
		} else {
			id, ok := HashID(r.PhoneHash)
			if !ok {
				continue
			}
			f.SubjectType, f.SubjectID = SubjectPhone, id
		}
		out = append(out, f)
	}
	return out
}

// --- facts -----------------------------------------------------------------------------

var factKey = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,40}$`)

const (
	maxFacts     = 30  //settings:ok contract bound (RiskFlagDetail.facts maxItems)
	maxFactValue = 200 //settings:ok contract bound (RiskFact.value maxLength)
)

// Fact is one recorded number or code of a flag.
type Fact struct{ Key, Value string }

// Facts flattens a flag's details for A-13: the top-level keys in order, a
// scalar as text, anything else as JSON, each cut to 200 characters. A key
// the contract cannot carry is left out, and so is a list of ids (the
// evidence shows those rows).
func Facts(details []byte) []Fact {
	var m map[string]json.RawMessage
	if json.Unmarshal(details, &m) != nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		if factKey.MatchString(k) && !isIDList(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	out := make([]Fact, 0, min(len(keys), maxFacts))
	for _, k := range keys {
		if len(out) == maxFacts {
			break
		}
		out = append(out, Fact{Key: k, Value: cut(scalar(m[k]), maxFactValue)})
	}
	return out
}

func isIDList(k string) bool { return len(k) > 3 && k[len(k)-3:] == "Ids" }

func scalar(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case nil:
		return ""
	default:
		return string(raw)
	}
}

func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n])
}

// detail reads one string field of a flag's details.
func detail(details []byte, key string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal(details, &m) != nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	return scalar(raw)
}

// detailIDs reads a list of ids from a flag's details.
func detailIDs(details []byte, key string) []ids.ID {
	var m map[string]json.RawMessage
	if json.Unmarshal(details, &m) != nil {
		return nil
	}
	var ss []string
	if json.Unmarshal(m[key], &ss) != nil {
		return nil
	}
	out := make([]ids.ID, 0, len(ss))
	for _, s := range ss {
		if id, err := ids.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// detailInt reads one integer field of a flag's details, 0 when absent.
func detailInt(details []byte, key string) int {
	n, err := strconv.Atoi(detail(details, key))
	if err != nil {
		return 0
	}
	return n
}

// mustJSON encodes a rule's details (maps of strings, numbers and lists).
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("risk: details: %v", err))
	}
	return b
}

// detailID is the id at key in a flag's details; false when it is missing or
// not an id (the flag then shows no evidence lines).
func detailID(details []byte, key string) (ids.ID, bool) {
	id, err := ids.Parse(detail(details, key))
	return id, err == nil
}
