package admin

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-audit"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-19 Audit log and the audit side panel on every console screen. The whole
// log is for admins; a panel shows one subject's history to a role that may
// view that subject (the subject's own view action decides).

// subjectView is the action that lets a caller see a subject, and so its
// history. A subject type missing here is refused.
var subjectView = map[api.AdminAuditSubjectType]string{
	api.AdminAuditSubjectTypeBusiness:             policy.AdminBusinessView,
	api.AdminAuditSubjectTypeOrder:                policy.AdminOrderView,
	api.AdminAuditSubjectTypeUser:                 policy.AdminCustomerView,
	api.AdminAuditSubjectTypeStore:                policy.AdminStoreView,
	api.AdminAuditSubjectTypeReview:               policy.AdminReviewModerate,
	api.AdminAuditSubjectTypeBanner:               policy.AdminBannerModerate,
	api.AdminAuditSubjectTypeLead:                 policy.AdminLeadView,
	api.AdminAuditSubjectTypePlatformSetting:      policy.AdminSettingsView,
	api.AdminAuditSubjectTypeAdminUser:            policy.AdminUsersView,
	api.AdminAuditSubjectTypeProduct:              policy.AdminCatalogueView,
	api.AdminAuditSubjectTypeCategory:             policy.AdminCatalogueView,
	api.AdminAuditSubjectTypeSynonym:              policy.AdminCatalogueView,
	api.AdminAuditSubjectTypeNotificationTemplate: policy.AdminNotifyView,
	api.AdminAuditSubjectTypePayout:               policy.AdminPayoutView,
	api.AdminAuditSubjectTypePayoutPeriod:         policy.AdminPayoutView,
	api.AdminAuditSubjectTypeCreditAdjustment:     policy.AdminLedgerView,
}

type auditFilter struct {
	actor            pgtype.UUID
	action, typ, sub *string
	from, to         *time.Time
	limit            *int
	cursor           *string
}

func (s *Service) auditLog(ctx context.Context, f auditFilter) (api.AdminAuditList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminAuditList{}, err
	}
	lim, at, id, err := pageArgs(f.limit, f.cursor)
	if err != nil {
		return api.AdminAuditList{}, err
	}
	rows, err := s.q(ctx).ListAudit(ctx, admindb.ListAuditParams{
		ActorID: f.actor, Action: f.action, SubjectType: f.typ, SubjectID: f.sub,
		FromAt: f.from, ToAt: f.to, BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminAuditList{}, err
	}
	out := api.AdminAuditList{Items: make([]api.AdminAuditEntry, 0, len(rows)), ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, api.AdminAuditEntry{
			Id: idString(r.ID), ActorKind: api.AdminAuditEntryActorKind(r.ActorKind), ActorId: optID(r.ActorID),
			ActorName: r.ActorName, Action: r.Action, SubjectType: r.SubjectType, SubjectId: r.SubjectID,
			Before: jsonObject(ctx, r.Before), After: jsonObject(ctx, r.After), CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return out, nil
}

// subjectAudit is one subject's history, after the subject's own view check.
func (s *Service) subjectAudit(ctx context.Context, p authn.Principal, typ api.AdminAuditSubjectType, sub string, limit *int, cursor *string) (api.AdminAuditList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminAuditList{}, err
	}
	action, ok := subjectView[typ]
	if !ok {
		return api.AdminAuditList{}, problem.New(genproblems.Forbidden, "")
	}
	if s.az == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: the subject audit check needs the authorizer")
		return api.AdminAuditList{}, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	if _, err := s.az.Check(ctx, p, action, authz.Scope{}); err != nil {
		return api.AdminAuditList{}, err
	}
	t := string(typ)
	return s.auditLog(ctx, auditFilter{typ: &t, sub: &sub, limit: limit, cursor: cursor})
}

// jsonObject decodes a stored before/after; anything that is not an object
// (null, or a scalar an older writer stored) comes back as null.
func jsonObject(ctx context.Context, b []byte) *map[string]any {
	if len(b) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || m == nil {
		if err != nil {
			telemetry.Logger(ctx).WarnContext(ctx, "admin: an audit row's JSON is not an object")
		}
		return nil
	}
	return &m
}
