package risk

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	riskapi "github.com/todayz/backend/gen/api/admin-risk"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/riskdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

// The A-13 queue: list, one flag with its evidence, clear and confirm.

const (
	maxReviewNote = 500 //settings:ok contract bound (RiskReviewNote maxLength)
	maxLabel      = 200 //settings:ok contract bound (RiskFlag.subjectLabel maxLength)
	maxNote       = 80  //settings:ok contract bound (RiskEvidence.note maxLength)
)

// flagRow is the part of a risk_flags row the list, the detail and the
// history share.
type flagRow struct {
	ID           pgtype.UUID
	Rule         string
	SubjectType  string
	SubjectID    pgtype.UUID
	Score        int32
	Status       string
	CreatedAt    time.Time
	ReviewedAt   *time.Time
	ReviewNote   *string
	ReviewerName *string
}

type subjectRef struct {
	typ string
	id  ids.ID
}

const (
	partyReferee  = "referee"
	partyReferrer = "referrer"
)

func sideOf(p *string) *riskapi.RiskReferralParty {
	if p == nil {
		return nil
	}
	v := riskapi.RiskReferralParty(*p)
	return &v
}

// subjectInfo is what A-13 shows about a flag's subject.
type subjectInfo struct {
	label, status                   *string
	storeID, businessID, customerID *string
	// customerParty and businessParty: on a referral, which side each id is.
	customerParty, businessParty *string
}

func flagOf(r flagRow, subj map[subjectRef]subjectInfo) riskapi.RiskFlag {
	info := subj[subjectRef{r.SubjectType, mustID(r.SubjectID)}]
	return riskapi.RiskFlag{
		Id: mustID(r.ID).String(), Rule: r.Rule, SubjectType: r.SubjectType, SubjectId: mustID(r.SubjectID).String(),
		SubjectLabel: info.label, Score: int(r.Score), Status: riskapi.RiskFlagStatus(r.Status),
		CreatedAt: r.CreatedAt.UTC(), ReviewedAt: utcPtr(r.ReviewedAt), ReviewedBy: r.ReviewerName, ReviewNote: r.ReviewNote,
	}
}

func (s *Service) listFlags(ctx context.Context, p riskapi.ListRiskFlagsParams) (riskapi.RiskFlagList, error) {
	if err := s.ready(ctx); err != nil {
		return riskapi.RiskFlagList{}, err
	}
	lim, at, id, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return riskapi.RiskFlagList{}, err
	}
	var status *string
	if p.Status != nil {
		status = ptr(string(*p.Status))
	}
	q := s.q(ctx)
	rows, err := q.ListFlags(ctx, riskdb.ListFlagsParams{
		Status: status, Rule: p.Rule, SubjectType: p.SubjectType, BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return riskapi.RiskFlagList{}, err
	}
	frs := make([]flagRow, 0, len(rows))
	for i, r := range rows {
		if i == lim {
			break
		}
		frs = append(frs, flagRow(r))
	}
	subj, err := s.subjects(ctx, frs)
	if err != nil {
		return riskapi.RiskFlagList{}, err
	}
	out := riskapi.RiskFlagList{Items: make([]riskapi.RiskFlag, 0, len(frs)), OpenByRule: []riskapi.RiskRuleCount{}, ServerNow: s.now()}
	for _, r := range frs {
		out.Items = append(out.Items, flagOf(r, subj))
	}
	if len(rows) > lim {
		last := rows[lim-1]
		out.NextCursor = ptr(encodeCursor(last.CreatedAt, mustID(last.ID)))
	}
	counts, err := q.OpenByRule(ctx)
	if err != nil {
		return riskapi.RiskFlagList{}, err
	}
	for _, c := range counts {
		out.OpenByRule = append(out.OpenByRule, riskapi.RiskRuleCount{Rule: c.Rule, Open: int(c.Open)})
	}
	return out, nil
}

// subjects reads the label and ids of every flag's subject, a query per type.
func (s *Service) subjects(ctx context.Context, rows []flagRow) (map[subjectRef]subjectInfo, error) {
	byType := map[string][]pgtype.UUID{}
	for _, r := range rows {
		byType[r.SubjectType] = append(byType[r.SubjectType], r.SubjectID)
	}
	out := map[subjectRef]subjectInfo{}
	q := s.q(ctx)
	if us := byType["store"]; len(us) > 0 {
		rs, err := q.StoreLabels(ctx, us)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out[subjectRef{"store", mustID(r.ID)}] = subjectInfo{
				label: ptr(cut(r.Name, maxLabel)), status: ptr(r.Status),
				storeID: ptr(mustID(r.ID).String()), businessID: idPtr(r.BusinessID),
			}
		}
	}
	if us := byType["business"]; len(us) > 0 {
		rs, err := q.BusinessLabels(ctx, us)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out[subjectRef{"business", mustID(r.ID)}] = subjectInfo{
				label: ptr(cut(r.BrandName, maxLabel)), status: ptr(r.Status), businessID: ptr(mustID(r.ID).String()),
			}
		}
	}
	if us := byType["user"]; len(us) > 0 {
		rs, err := q.UserLabels(ctx, us)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			info := subjectInfo{status: ptr(r.Status)}
			if r.Name != nil && *r.Name != "" {
				info.label = ptr(cut(*r.Name, maxLabel))
			}
			if r.IsCustomer {
				info.customerID = ptr(mustID(r.ID).String())
			}
			out[subjectRef{"user", mustID(r.ID)}] = info
		}
	}
	if us := byType["referral"]; len(us) > 0 {
		rs, err := q.ReferralLabels(ctx, us)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			info := subjectInfo{label: referralLabel(r.ReferrerLabel, r.RefereeLabel), status: ptr(r.Status)}
			switch r.RefereeType {
			case "customer":
				info.customerID, info.customerParty = ptr(mustID(r.RefereeID).String()), ptr(partyReferee)
			case "business":
				info.businessID, info.businessParty = ptr(mustID(r.RefereeID).String()), ptr(partyReferee)
			}
			switch r.ReferrerType {
			case "store":
				info.storeID = ptr(mustID(r.ReferrerID).String())
			case "business":
				if info.businessID == nil {
					info.businessID, info.businessParty = ptr(mustID(r.ReferrerID).String()), ptr(partyReferrer)
				}
			}
			out[subjectRef{"referral", mustID(r.ID)}] = info
		}
	}
	return out, nil
}

// referralLabel is "referrer → new person"; a side without a name shows as "?".
func referralLabel(from, to string) *string {
	if from == "" && to == "" {
		return nil
	}
	if from == "" {
		from = "?"
	}
	if to == "" {
		to = "?"
	}
	return ptr(cut(from+" → "+to, maxLabel))
}

func (s *Service) getFlag(ctx context.Context, flagID ids.ID) (riskapi.RiskFlagDetail, error) {
	if err := s.ready(ctx); err != nil {
		return riskapi.RiskFlagDetail{}, err
	}
	q := s.q(ctx)
	g, err := q.GetFlag(ctx, ids.UUID(flagID))
	if errors.Is(err, pgx.ErrNoRows) {
		return riskapi.RiskFlagDetail{}, notFound()
	}
	if err != nil {
		return riskapi.RiskFlagDetail{}, err
	}
	fr := flagRow{
		ID: g.ID, Rule: g.Rule, SubjectType: g.SubjectType, SubjectID: g.SubjectID, Score: g.Score, Status: g.Status,
		CreatedAt: g.CreatedAt, ReviewedAt: g.ReviewedAt, ReviewNote: g.ReviewNote, ReviewerName: g.ReviewerName,
	}
	hist, err := q.SubjectFlags(ctx, riskdb.SubjectFlagsParams{SubjectType: g.SubjectType, SubjectID: g.SubjectID, ID: g.ID})
	if err != nil {
		return riskapi.RiskFlagDetail{}, err
	}
	all := []flagRow{fr}
	for _, h := range hist {
		all = append(all, flagRow(h))
	}
	subj, err := s.subjects(ctx, all)
	if err != nil {
		return riskapi.RiskFlagDetail{}, err
	}
	info := subj[subjectRef{g.SubjectType, mustID(g.SubjectID)}]
	if b := detail(g.Details, "businessId"); b != "" && ids.Valid(b) {
		info.businessID, info.businessParty = ptr(b), ptr(partyReferrer) // referral.self_like: the referrer's business
	}
	out := riskapi.RiskFlagDetail{
		Flag: flagOf(fr, subj),
		Subject: riskapi.RiskSubject{
			Type: g.SubjectType, Id: mustID(g.SubjectID).String(), Label: info.label, Status: info.status,
			StoreId: info.storeID, BusinessId: info.businessID, CustomerId: info.customerID,
			CustomerParty: sideOf(info.customerParty), BusinessParty: sideOf(info.businessParty),
		},
		Facts: []riskapi.RiskFact{}, History: make([]riskapi.RiskFlag, 0, len(hist)), ServerNow: s.now(),
	}
	for _, f := range Facts(g.Details) {
		out.Facts = append(out.Facts, riskapi.RiskFact{Key: f.Key, Value: f.Value})
	}
	for _, h := range all[1:] {
		out.History = append(out.History, flagOf(h, subj))
	}
	if out.Evidence, err = s.evidence(ctx, g); err != nil {
		return riskapi.RiskFlagDetail{}, err
	}
	if out.AssistEnabled, err = s.settings.Bool(ctx, settings.AiRiskexplainEnabled); err != nil {
		return riskapi.RiskFlagDetail{}, err
	}
	return out, nil
}

// --- evidence --------------------------------------------------------------------------

func ev(kind riskapi.RiskEvidenceKind, id pgtype.UUID, label string, store *string, at time.Time, paise *int64, note string) riskapi.RiskEvidence {
	e := riskapi.RiskEvidence{Kind: kind, Id: idPtr(id), Label: cut(label, maxLabel), StoreName: store, At: at.UTC()}
	if paise != nil {
		e.AmountPaise = ptr(int(*paise))
	}
	if note != "" {
		e.Note = ptr(cut(note, maxNote))
	}
	return e
}

func orderLabel(n *string) string {
	if n == nil || *n == "" {
		return "Order"
	}
	return *n
}

// evidence reads the rows a flag's rule counted, newest first, at most 50.
// A rule this package does not know shows none.
func (s *Service) evidence(ctx context.Context, g riskdb.GetFlagRow) ([]riskapi.RiskEvidence, error) {
	out := []riskapi.RiskEvidence{}
	q := s.q(ctx)
	subject := g.SubjectID
	switch g.Rule {
	case RuleCodeLockouts:
		day, err := time.ParseInLocation(time.DateOnly, detail(g.Details, "day"), clock.IST)
		if err != nil {
			day = clock.StartOfDayIST(g.CreatedAt)
		}
		rs, err := q.LockoutEvidence(ctx, riskdb.LockoutEvidenceParams{StoreID: subject, Since: day.UTC(), Until: day.UTC().Add(24 * time.Hour)})
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, ev(riskapi.CodeLockout, r.OrderID, orderLabel(r.DisplayNumber), ptr(r.StoreName), r.LockedAt, &r.TotalPaise, r.Status))
		}
	case RuleBillClaimSpread:
		claimIDs := detailIDs(g.Details, "claimIds")
		us := make([]pgtype.UUID, 0, len(claimIDs))
		for _, id := range claimIDs {
			us = append(us, ids.UUID(id))
		}
		rs, err := q.BillClaimEvidence(ctx, us)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, ev(riskapi.BillClaim, r.ID, "Bill claim", ptr(r.StoreName), r.ClaimedAt, &r.AmountPaise, r.Status))
		}
	case RuleReversalRatio:
		days := max(detailInt(g.Details, "windowDays"), 1)
		rs, err := q.ReversedSaleEvidence(ctx, riskdb.ReversedSaleEvidenceParams{StoreID: subject, Since: g.CreatedAt.Add(-time.Duration(days) * 24 * time.Hour)})
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, ev(riskapi.PosSale, r.ID, "POS sale ("+r.LinkMethod+")", ptr(r.StoreName), r.ReversedAt, &r.TotalPaise, "reversed"))
		}
	case "pos.offline_links", "pos.offline_offer_without_discount_role":
		day := clock.StartOfDayIST(g.CreatedAt)
		rs, err := q.OfflineLinkEvidence(ctx, riskdb.OfflineLinkEvidenceParams{StoreID: subject, Since: day, Until: day.Add(24 * time.Hour)})
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, ev(riskapi.PosSale, r.ID, "Offline POS sale", ptr(r.StoreName), r.ReceivedAt, &r.TotalPaise, r.LinkStatus))
		}
	case "store_strikes":
		since := g.CreatedAt.Add(-time.Duration(max(detailInt(g.Details, "windowDays"), 1)) * 24 * time.Hour)
		if t, err := time.Parse(time.RFC3339Nano, detail(g.Details, "oldestStrikeAt")); err == nil && t.Before(since) {
			since = t
		}
		rs, err := q.StrikeEvidence(ctx, riskdb.StrikeEvidenceParams{StoreID: subject, Since: since})
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			note := r.Reason
			if r.ResetAt != nil {
				note += " (reset)"
			}
			out = append(out, ev(riskapi.Strike, r.ID, orderLabel(r.DisplayNumber), ptr(r.StoreName), r.CreatedAt, nil, note))
		}
	case "return_unconfirmed":
		if id, ok := detailID(g.Details, "orderId"); ok {
			return s.orderEvidence(ctx, []ids.ID{id})
		}
	case "credits.store_daily_cap", "credits.customer_monthly_cap":
		id, ok := detailID(g.Details, "ref_id")
		switch {
		case !ok:
		case detail(g.Details, "ref_type") == "pos_sale":
			return s.saleEvidence(ctx, []ids.ID{id})
		default:
			return s.orderEvidence(ctx, []ids.ID{id})
		}
	case RuleSelfLike, "referral.reversed_after_reward", "referral.reward_refused":
		return s.referralEvidence(ctx, subject)
	case "referral.device_accounts":
		rs, err := q.DeviceReferralEvidence(ctx, subject)
		if err != nil {
			return nil, err
		}
		us := make([]pgtype.UUID, 0, len(rs))
		for _, r := range rs {
			us = append(us, r.ID)
		}
		labels, err := s.referralLabels(ctx, us)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			out = append(out, ev(riskapi.Referral, r.ID, labels[mustID(r.ID)], nil, r.CreatedAt, nil, r.Status))
		}
	case RuleOTPIPFanout, RuleOTPFailedPhone:
		return s.otpEvidence(ctx, g)
	}
	return out, nil
}

func (s *Service) orderEvidence(ctx context.Context, idList []ids.ID) ([]riskapi.RiskEvidence, error) {
	out := []riskapi.RiskEvidence{}
	us := make([]pgtype.UUID, 0, len(idList))
	for _, id := range idList {
		us = append(us, ids.UUID(id))
	}
	if len(us) == 0 {
		return out, nil
	}
	rs, err := s.q(ctx).OrderEvidence(ctx, us)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		out = append(out, ev(riskapi.Order, r.ID, orderLabel(r.DisplayNumber), ptr(r.StoreName), r.PlacedAt, &r.TotalPaise, r.Status))
	}
	return out, nil
}

func (s *Service) saleEvidence(ctx context.Context, idList []ids.ID) ([]riskapi.RiskEvidence, error) {
	out := []riskapi.RiskEvidence{}
	us := make([]pgtype.UUID, 0, len(idList))
	for _, id := range idList {
		us = append(us, ids.UUID(id))
	}
	if len(us) == 0 {
		return out, nil
	}
	rs, err := s.q(ctx).SaleEvidence(ctx, us)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		out = append(out, ev(riskapi.PosSale, r.ID, "POS sale", ptr(r.StoreName), r.SoldAt, &r.TotalPaise, r.Status))
	}
	return out, nil
}

func (s *Service) referralLabels(ctx context.Context, us []pgtype.UUID) (map[ids.ID]string, error) {
	out := map[ids.ID]string{}
	if len(us) == 0 {
		return out, nil
	}
	rs, err := s.q(ctx).ReferralLabels(ctx, us)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		l := referralLabel(r.ReferrerLabel, r.RefereeLabel)
		if l == nil {
			l = ptr("Referral")
		}
		out[mustID(r.ID)] = *l
	}
	return out, nil
}

// referralEvidence is the referral and the purchase that qualified it.
func (s *Service) referralEvidence(ctx context.Context, id pgtype.UUID) ([]riskapi.RiskEvidence, error) {
	out := []riskapi.RiskEvidence{}
	r, err := s.q(ctx).ReferralEvidence(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	labels, err := s.referralLabels(ctx, []pgtype.UUID{id})
	if err != nil {
		return nil, err
	}
	note := r.Status
	if r.RejectReason != nil {
		note = *r.RejectReason
	}
	out = append(out, ev(riskapi.Referral, r.ID, labels[mustID(r.ID)], nil, r.CreatedAt, nil, note))
	if r.QualifyingRefID.Valid {
		var more []riskapi.RiskEvidence
		if derefStr(r.QualifyingRefType) == "pos_sale" {
			more, err = s.saleEvidence(ctx, []ids.ID{mustID(r.QualifyingRefID)})
		} else {
			more, err = s.orderEvidence(ctx, []ids.ID{mustID(r.QualifyingRefID)})
		}
		if err != nil {
			return nil, err
		}
		out = append(out, more...)
	}
	return out, nil
}

// otpEvidence lists the OTP requests behind an ip or phone flag (kept 24 h):
// when and whether they were verified, never the number or the code.
func (s *Service) otpEvidence(ctx context.Context, g riskdb.GetFlagRow) ([]riskapi.RiskEvidence, error) {
	out := []riskapi.RiskEvidence{}
	q := s.q(ctx)
	since := s.now().Add(-otpEvidenceWindow)
	id := mustID(g.SubjectID)
	var prefixes [][]byte
	byIP := g.Rule == RuleOTPIPFanout
	switch g.SubjectType {
	case SubjectIP, SubjectPhone:
		prefixes = [][]byte{id[:]}
	case "user":
		ps, err := q.UserPhonePrefixes(ctx, g.SubjectID)
		if err != nil {
			return nil, err
		}
		prefixes = ps
	}
	for _, p := range prefixes {
		rs, err := q.OtpEvidence(ctx, riskdb.OtpEvidenceParams{Since: since, ByIp: byIP, Prefix: p})
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			note := "unused"
			switch {
			case r.Used:
				note = "verified"
			case s.otpMax > 0 && int(r.Attempts) >= s.otpMax:
				note = "failed"
			}
			if len(out) < 50 { //settings:ok contract bound (RiskFlagDetail.evidence maxItems)
				out = append(out, ev(riskapi.OtpRequest, r.ID, "OTP request", nil, r.CreatedAt, nil, note))
			}
		}
	}
	return out, nil
}

// --- clear and confirm -------------------------------------------------------------------

// resolvable says whether a flag in status from may move to to: confirm only
// from open, clear from open or confirmed (a confirmed finding can turn out
// wrong later, and its payout hold must not be stuck).
func resolvable(from, to string) bool {
	return from == "open" || (from == "confirmed" && to == "cleared")
}

func cleanReason(r string, limit int) (string, error) {
	r = strings.TrimSpace(r)
	if r == "" || utf8.RuneCountInString(r) > limit {
		return "", fieldErr("body.reason", "1 to "+strconv.Itoa(limit)+" characters")
	}
	return r, nil
}

// resolve confirms an open flag, or clears an open or confirmed one, with a
// reason, audited in the same transaction. from is the status the reviewer
// saw; a flag that has moved since is a 409, so two reviewers never stack a
// clear on a confirm (or the reverse) unseen. It blocks or suspends no one; the
// only money effect is the payout hold a business or store flag keeps while
// it is open or confirmed (01 §256), which a clear releases.
func (s *Service) resolve(ctx context.Context, actor, flagID ids.ID, from, to, reason string) (riskapi.RiskFlag, error) {
	if err := s.auditReady(ctx); err != nil {
		return riskapi.RiskFlag{}, err
	}
	reason, err := cleanReason(reason, maxReviewNote)
	if err != nil {
		return riskapi.RiskFlag{}, err
	}
	action := map[string]string{"cleared": "risk.flag_clear", "confirmed": "risk.flag_confirm"}[to]
	var out riskapi.RiskFlag
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		l, err := q.LockFlag(ctx, ids.UUID(flagID))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if l.Status != from || !resolvable(from, to) {
			return problem.New(genproblems.InvalidTransition, "")
		}
		now := s.now()
		if err := q.ResolveFlag(ctx, riskdb.ResolveFlagParams{
			Status: to, ReviewedBy: ids.UUID(actor), Now: &now, Note: &reason, ID: l.ID,
		}); err != nil {
			return err
		}
		g, err := q.GetFlag(ctx, l.ID)
		if err != nil {
			return err
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "risk_flag", SubjectID: flagID.String(),
			Before: map[string]any{"status": l.Status},
			After: map[string]any{"status": to, "reason": reason, "rule": g.Rule,
				"subjectType": g.SubjectType, "subjectId": mustID(g.SubjectID).String()},
		}); err != nil {
			return err
		}
		fr := flagRow{
			ID: g.ID, Rule: g.Rule, SubjectType: g.SubjectType, SubjectID: g.SubjectID, Score: g.Score, Status: g.Status,
			CreatedAt: g.CreatedAt, ReviewedAt: g.ReviewedAt, ReviewNote: g.ReviewNote, ReviewerName: g.ReviewerName,
		}
		subj, err := s.subjects(ctx, []flagRow{fr})
		if err != nil {
			return err
		}
		out = flagOf(fr, subj)
		return nil
	})
	return out, err
}
