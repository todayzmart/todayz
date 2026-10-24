package reviews

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	adminapi "github.com/todayz/backend/gen/api/admin-reviews"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/reviewsdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// The admin side (A-x Reviews, 01 §10, D-41): list, hide, restore, hide or
// restore a reply, the report queue and resolving a report. Hide and restore
// both need a reason and both write an audit_log row in the same transaction.

func (s *Service) auditReady(ctx context.Context) error {
	if err := s.ready(ctx); err != nil {
		return err
	}
	if s.audit == nil {
		return problem.Wrap(genproblems.ServiceUnavailable, errors.New("reviews: the audit writer is not configured"))
	}
	return nil
}

func cleanModReason(r string) (string, error) {
	r = strings.TrimSpace(r)
	if r == "" || utf8.RuneCountInString(r) > 500 { //settings:ok contract bound (ModerationNote ≤ 500)
		return "", fieldErr("body.reason", "1 to 500 characters")
	}
	return r, nil
}

func (s *Service) listAdmin(ctx context.Context, state *string, storeID *string, limit *int, cursor *string) (adminapi.AdminReviewList, error) {
	if err := s.ready(ctx); err != nil {
		return adminapi.AdminReviewList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return adminapi.AdminReviewList{}, err
	}
	st := "visible"
	if state != nil && *state != "" {
		st = *state
	}
	switch st {
	case "visible", "hidden", "reported":
	default:
		return adminapi.AdminReviewList{}, fieldErr("query.state", "visible, hidden or reported")
	}
	var store pgtype.UUID
	if storeID != nil && *storeID != "" {
		sid, perr := ids.Parse(*storeID)
		if perr != nil {
			return adminapi.AdminReviewList{}, fieldErr("query.storeId", "invalid")
		}
		store = ids.UUID(sid)
	}
	rows, err := s.q(ctx).ListAdminReviews(ctx, reviewsdb.ListAdminReviewsParams{
		StoreID: store, State: st, BeforeAt: at, BeforeID: id, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return adminapi.AdminReviewList{}, err
	}
	out := adminapi.AdminReviewList{Items: make([]adminapi.AdminReview, 0, len(rows)), ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			out.NextCursor = ptr(encodeCursor(rows[lim-1].CreatedAt, mustID(rows[lim-1].ID)))
			break
		}
		out.Items = append(out.Items, adminReviewOf(r.ID, r.StoreID, r.StoreName, r.CustomerID, r.OrderID, r.PosSaleID,
			r.Stars, r.Text, r.Tags, r.CreatedAt, r.HiddenAt, r.HideReason, r.OpenReports,
			r.ReplyText, r.ReplyCreatedAt, r.ReplyDeletedAt, r.ReplyDeleteReason))
	}
	return out, nil
}

func (s *Service) adminReview(ctx context.Context, id pgtype.UUID) (adminapi.AdminReview, error) {
	r, err := s.q(ctx).GetAdminReview(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return adminapi.AdminReview{}, notFound()
	}
	if err != nil {
		return adminapi.AdminReview{}, err
	}
	return adminReviewOf(r.ID, r.StoreID, r.StoreName, r.CustomerID, r.OrderID, r.PosSaleID,
		r.Stars, r.Text, r.Tags, r.CreatedAt, r.HiddenAt, r.HideReason, r.OpenReports,
		r.ReplyText, r.ReplyCreatedAt, r.ReplyDeletedAt, r.ReplyDeleteReason), nil
}

func adminReviewOf(id, store pgtype.UUID, storeName string, customer, order, sale pgtype.UUID, stars int16, text *string, tags []string,
	created time.Time, hiddenAt *time.Time, hideReason *string, open int32,
	replyText *string, replyAt, replyDeleted *time.Time, replyReason *string) adminapi.AdminReview {
	a := adminapi.AdminReview{
		Id: idString(id), StoreId: idString(store), StoreName: storeName, CustomerId: idString(customer),
		Stars: int(stars), Text: text, Tags: append([]string{}, tags...), CreatedAt: created.UTC(),
		Hidden: hiddenAt != nil, HiddenAt: utcPtr(hiddenAt), HideReason: hideReason, OpenReports: int(open),
	}
	if order.Valid {
		a.OrderId = ptr(idString(order))
	}
	if sale.Valid {
		a.PosSaleId = ptr(idString(sale))
	}
	if replyText != nil && replyAt != nil {
		a.Reply = &adminapi.AdminReviewReply{Text: *replyText, CreatedAt: replyAt.UTC(), Hidden: replyDeleted != nil, HideReason: replyReason}
	}
	return a
}

// moderate runs one hide or restore of a review (or its reply). It locks the
// review, then the store's reviews, applies fn, recomputes the rating and
// audits, all in one transaction.
func (s *Service) moderate(ctx context.Context, actor, reviewID ids.ID, action string,
	fn func(ctx context.Context, r reviewsdb.LockReviewForModerationRow, now time.Time) (before, after map[string]any, err error)) (adminapi.AdminReview, error) {
	if err := s.auditReady(ctx); err != nil {
		return adminapi.AdminReview{}, err
	}
	var out adminapi.AdminReview
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		r, err := s.q(ctx).LockReviewForModeration(ctx, ids.UUID(reviewID))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if err := s.lockStore(ctx, r.StoreID); err != nil {
			return err
		}
		now := s.now()
		before, after, err := fn(ctx, r, now)
		if err != nil {
			return err
		}
		if err := s.recompute(ctx, r.StoreID); err != nil {
			return err
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "review", SubjectID: reviewID.String(), Before: before, After: after,
		}); err != nil {
			return err
		}
		out, err = s.adminReview(ctx, ids.UUID(reviewID))
		return err
	})
	return out, err
}

func (s *Service) hide(ctx context.Context, actor, reviewID ids.ID, reason string) (adminapi.AdminReview, error) {
	reason, err := cleanModReason(reason)
	if err != nil {
		return adminapi.AdminReview{}, err
	}
	return s.moderate(ctx, actor, reviewID, "review.hide", func(ctx context.Context, r reviewsdb.LockReviewForModerationRow, now time.Time) (map[string]any, map[string]any, error) {
		if r.HiddenAt != nil {
			return nil, nil, problem.New(genproblems.InvalidTransition, "")
		}
		q := s.q(ctx)
		if err := q.HideReview(ctx, reviewsdb.HideReviewParams{ID: r.ID, Now: &now, Actor: ids.UUID(actor), Reason: &reason}); err != nil {
			return nil, nil, err
		}
		upheld, err := q.UpholdOpenReports(ctx, reviewsdb.UpholdOpenReportsParams{ReviewID: r.ID, Actor: ids.UUID(actor), Now: &now})
		if err != nil {
			return nil, nil, err
		}
		return map[string]any{"hidden": false},
			map[string]any{"hidden": true, "reason": reason, "reportsUpheld": upheld}, nil
	})
}

func (s *Service) restore(ctx context.Context, actor, reviewID ids.ID, reason string) (adminapi.AdminReview, error) {
	reason, err := cleanModReason(reason)
	if err != nil {
		return adminapi.AdminReview{}, err
	}
	return s.moderate(ctx, actor, reviewID, "review.restore", func(ctx context.Context, r reviewsdb.LockReviewForModerationRow, now time.Time) (map[string]any, map[string]any, error) {
		if r.HiddenAt == nil {
			return nil, nil, problem.New(genproblems.InvalidTransition, "")
		}
		if err := s.q(ctx).RestoreReview(ctx, reviewsdb.RestoreReviewParams{ID: r.ID, Now: now}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"hidden": true, "hideReason": derefStr(r.HideReason)},
			map[string]any{"hidden": false, "reason": reason}, nil
	})
}

func (s *Service) hideReply(ctx context.Context, actor, reviewID ids.ID, reason string) (adminapi.AdminReview, error) {
	reason, err := cleanModReason(reason)
	if err != nil {
		return adminapi.AdminReview{}, err
	}
	return s.moderate(ctx, actor, reviewID, "review.reply_hide", func(ctx context.Context, r reviewsdb.LockReviewForModerationRow, now time.Time) (map[string]any, map[string]any, error) {
		cur, err := s.q(ctx).LockReply(ctx, r.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, notFound() // no reply to hide
		}
		if err != nil {
			return nil, nil, err
		}
		if cur.DeletedAt != nil {
			return nil, nil, problem.New(genproblems.InvalidTransition, "")
		}
		if _, err := s.q(ctx).HideReply(ctx, reviewsdb.HideReplyParams{ReviewID: r.ID, Now: &now, Actor: ids.UUID(actor), Reason: &reason}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"replyHidden": false}, map[string]any{"replyHidden": true, "reason": reason}, nil
	})
}

func (s *Service) restoreReply(ctx context.Context, actor, reviewID ids.ID, reason string) (adminapi.AdminReview, error) {
	reason, err := cleanModReason(reason)
	if err != nil {
		return adminapi.AdminReview{}, err
	}
	return s.moderate(ctx, actor, reviewID, "review.reply_restore", func(ctx context.Context, r reviewsdb.LockReviewForModerationRow, now time.Time) (map[string]any, map[string]any, error) {
		cur, err := s.q(ctx).LockReply(ctx, r.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, notFound()
		}
		if err != nil {
			return nil, nil, err
		}
		if cur.DeletedAt == nil {
			return nil, nil, problem.New(genproblems.InvalidTransition, "")
		}
		if _, err := s.q(ctx).RestoreReply(ctx, reviewsdb.RestoreReplyParams{ReviewID: r.ID, Now: now}); err != nil {
			return nil, nil, err
		}
		return map[string]any{"replyHidden": true}, map[string]any{"replyHidden": false, "reason": reason}, nil
	})
}

// --- reports -------------------------------------------------------------------------

func (s *Service) listReports(ctx context.Context, status *string, limit *int, cursor *string) (adminapi.AdminReviewReportList, error) {
	if err := s.ready(ctx); err != nil {
		return adminapi.AdminReviewReportList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return adminapi.AdminReviewReportList{}, err
	}
	st := "open"
	if status != nil && *status != "" {
		st = *status
	}
	out := adminapi.AdminReviewReportList{ServerNow: s.now()}
	type row struct {
		id, review pgtype.UUID
		fromStore  bool
		reason     string
		status     string
		resolution *string
		note       *string
		created    time.Time
		resolved   *time.Time
	}
	var rows []row
	switch st {
	case "open":
		rs, err := s.q(ctx).ListAdminReportsOpen(ctx, reviewsdb.ListAdminReportsOpenParams{AfterAt: at, AfterID: id, Lim: int32(lim + 1)}) // #nosec G115 -- lim is clamped to [1, maxLimit]
		if err != nil {
			return out, err
		}
		for _, r := range rs {
			rows = append(rows, row{r.ID, r.ReviewID, r.FromStore, r.Reason, r.Status, r.Resolution, r.ResolutionNote, r.CreatedAt, r.ResolvedAt})
		}
	case "resolved":
		rs, err := s.q(ctx).ListAdminReportsResolved(ctx, reviewsdb.ListAdminReportsResolvedParams{BeforeAt: at, BeforeID: id, Lim: int32(lim + 1)}) // #nosec G115 -- lim is clamped to [1, maxLimit]
		if err != nil {
			return out, err
		}
		for _, r := range rs {
			rows = append(rows, row{r.ID, r.ReviewID, r.FromStore, r.Reason, r.Status, r.Resolution, r.ResolutionNote, r.CreatedAt, r.ResolvedAt})
		}
	default:
		return out, fieldErr("query.status", "open or resolved")
	}
	out.Items = make([]adminapi.AdminReviewReport, 0, len(rows))
	for i, r := range rows {
		if i == lim {
			out.NextCursor = ptr(encodeCursor(rows[lim-1].created, mustID(rows[lim-1].id)))
			break
		}
		out.Items = append(out.Items, reportOf(r.id, r.review, r.fromStore, r.reason, r.status, r.resolution, r.note, r.created, r.resolved))
	}
	return out, nil
}

func reportOf(id, review pgtype.UUID, fromStore bool, reason, status string, resolution, note *string, created time.Time, resolved *time.Time) adminapi.AdminReviewReport {
	kind := adminapi.AdminReviewReportReporterKind("customer")
	if fromStore {
		kind = "store"
	}
	r := adminapi.AdminReviewReport{
		Id: idString(id), ReviewId: idString(review), ReporterKind: kind, Reason: reason,
		Status: adminapi.AdminReviewReportStatus(status), ResolutionNote: note, CreatedAt: created.UTC(), ResolvedAt: utcPtr(resolved),
	}
	if resolution != nil {
		r.Resolution = ptr(adminapi.AdminReviewReportResolution(*resolution))
	}
	return r
}

func (s *Service) resolveReport(ctx context.Context, actor, reportID ids.ID, resolution string, note *string) (adminapi.AdminReviewReport, error) {
	if err := s.auditReady(ctx); err != nil {
		return adminapi.AdminReviewReport{}, err
	}
	if resolution != "dismissed" && resolution != "upheld" {
		return adminapi.AdminReviewReport{}, fieldErr("body.resolution", "dismissed or upheld")
	}
	var n *string
	if note != nil {
		t := strings.TrimSpace(*note)
		if utf8.RuneCountInString(t) > 500 { //settings:ok contract bound (note ≤ 500)
			return adminapi.AdminReviewReport{}, fieldErr("body.note", "at most 500 characters")
		}
		if t != "" {
			n = &t
		}
	}
	var out adminapi.AdminReviewReport
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		cur, err := s.q(ctx).LockReport(ctx, ids.UUID(reportID))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.Status != "open" {
			return problem.New(genproblems.InvalidTransition, "")
		}
		now := s.now()
		row, err := s.q(ctx).ResolveReport(ctx, reviewsdb.ResolveReportParams{
			ID: cur.ID, Resolution: &resolution, Note: n, Actor: ids.UUID(actor), Now: &now,
		})
		if err != nil {
			return err
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "review.report_resolve",
			SubjectType: "review_report", SubjectID: reportID.String(),
			Before: map[string]any{"status": "open"},
			After:  map[string]any{"status": "resolved", "resolution": resolution, "reviewId": idString(cur.ReviewID)},
		}); err != nil {
			return err
		}
		out = reportOf(row.ID, row.ReviewID, row.FromStore, row.Reason, row.Status, row.Resolution, row.ResolutionNote, row.CreatedAt, row.ResolvedAt)
		return nil
	})
	return out, err
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
