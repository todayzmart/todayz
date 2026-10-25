package admin

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-leads"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/ids"
)

// A-03 Leads tab (04 §2.7): shop leads from the site's form, worked through
// new → contacted → closed. Any status can move to any other (a closed lead
// that calls back reopens); the phone is masked for every role.

type leadRow struct {
	ID              pgtype.UUID
	Name            string
	PhoneEnc        []byte
	ShopName        string
	City            string
	Source          string
	Status          string
	StatusChangedAt *time.Time
	CreatedAt       time.Time
}

func (s *Service) leadOut(ctx context.Context, r leadRow) api.AdminLead {
	lid, _ := ids.FromUUID(r.ID)
	return api.AdminLead{
		Id: idString(r.ID), Name: r.Name, ShopName: r.ShopName, City: r.City, Source: r.Source,
		PhoneMasked: s.maskSealed(ctx, sealAAD("phone", lid), r.PhoneEnc),
		Status:      api.AdminLeadStatus(r.Status), StatusChangedAt: utcPtr(r.StatusChangedAt), CreatedAt: r.CreatedAt.UTC(),
	}
}

func (s *Service) listLeads(ctx context.Context, p api.ListAdminLeadsParams) (api.AdminLeadList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminLeadList{}, err
	}
	lim, at, id, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminLeadList{}, err
	}
	var st *string
	if p.Status != nil {
		v := string(*p.Status)
		st = &v
	}
	rows, err := s.q(ctx).ListLeads(ctx, admindb.ListLeadsParams{
		Status: st, BeforeAt: at, BeforeID: id,
		Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminLeadList{}, err
	}
	out := api.AdminLeadList{Items: []api.AdminLead{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, s.leadOut(ctx, leadRow(r)))
	}
	return out, nil
}

func (s *Service) setLeadStatus(ctx context.Context, actor, id ids.ID, body api.AdminLeadStatusChange) (api.AdminLead, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminLead{}, err
	}
	note, err := optNote(body.Note, "body.note")
	if err != nil {
		return api.AdminLead{}, err
	}
	to := string(body.Status)
	var out leadRow
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockLead(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if cur.Status == to {
			return invalidTransition("")
		}
		now := s.now()
		r, err := q.SetLeadStatus(ctx, admindb.SetLeadStatusParams{Status: to, Now: &now, By: ids.UUID(actor), ID: ids.UUID(id)})
		if err != nil {
			return err
		}
		out = leadRow(r)
		after := map[string]any{"status": to}
		if note != nil {
			after["note"] = *note
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "lead.status",
			SubjectType: "lead", SubjectID: id.String(), Before: map[string]any{"status": cur.Status}, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminLead{}, err
	}
	return s.leadOut(ctx, out), nil
}
