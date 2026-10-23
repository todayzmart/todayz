package identity

import (
	"context"
	"fmt"
	"slices"
	"time"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/identitydb"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// Me is the signed-in user as /v1/me shows it.
type Me struct {
	ID            ids.ID
	Name          *string
	PreferredLang string
	PhoneMasked   string
	App           authn.App
	CreatedAt     time.Time
	Version       int32
	// Roles: admin app only.
	Roles []string
	// Customer: customer app only; nil when the profile is missing.
	Customer *CustomerProfile
}

// CustomerProfile is the customer part of Me.
type CustomerProfile struct {
	ReferralCode string
	// LimitedUntil is set while no-show strikes cap active orders (C-21).
	LimitedUntil *time.Time
}

// MePatch is a profile change. SetName with a nil Name clears the name.
type MePatch struct {
	SetName       bool
	Name          *string
	PreferredLang *string
}

// GetMe reads the principal's profile.
func (s *Service) GetMe(ctx context.Context, p authn.Principal) (Me, error) {
	if err := s.ready(); err != nil {
		return Me{}, unavailable(ctx, err)
	}
	m, err := s.readMe(ctx, identitydb.New(s.db.Q(ctx)), p)
	if err != nil {
		return Me{}, asProblem(err)
	}
	return m, nil
}

// UpdateMe changes name and/or language if version is still current
// (If-Match, 04 §1.7): a stale version is 412 precondition_failed.
func (s *Service) UpdateMe(ctx context.Context, p authn.Principal, version int32, patch MePatch) (Me, error) {
	if err := s.ready(); err != nil {
		return Me{}, unavailable(ctx, err)
	}
	var out Me
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := identitydb.New(s.db.Q(ctx))
		_, err := q.UpdateMe(ctx, identitydb.UpdateMeParams{
			SetName: patch.SetName, Name: patch.Name, PreferredLang: patch.PreferredLang,
			ID: ids.UUID(p.UserID), Version: version,
		})
		if db.IsNoRows(err) {
			// Stale version, or a user who is no longer active.
			cur, gerr := q.GetMe(ctx, ids.UUID(p.UserID))
			switch {
			case db.IsNoRows(gerr):
				return problem.New(genproblems.Unauthorized, "")
			case gerr != nil:
				return gerr
			}
			if refusal := statusRefusal(cur.Status); refusal != nil {
				return refusal
			}
			return problem.New(genproblems.PreconditionFailed, "")
		}
		if err != nil {
			return err
		}
		out, err = s.readMe(ctx, q, p)
		return err
	})
	if err != nil {
		return Me{}, asProblem(err)
	}
	return out, nil
}

func (s *Service) readMe(ctx context.Context, q *identitydb.Queries, p authn.Principal) (Me, error) {
	row, err := q.GetMe(ctx, ids.UUID(p.UserID))
	if db.IsNoRows(err) {
		return Me{}, problem.New(genproblems.Unauthorized, "")
	}
	if err != nil {
		return Me{}, err
	}
	if refusal := statusRefusal(row.Status); refusal != nil {
		return Me{}, refusal
	}
	plain, err := s.seal.Open("phone", row.PhoneEnc)
	if err != nil {
		return Me{}, fmt.Errorf("identity: open phone: %w", err)
	}
	m := Me{
		ID: p.UserID, Name: row.Name, PreferredLang: row.PreferredLang,
		PhoneMasked: pii.MaskPhone(string(plain)), App: p.App, CreatedAt: row.CreatedAt.UTC(), Version: row.Version,
	}
	switch p.App {
	case authn.AppAdmin:
		m.Roles = slices.Clone(p.Roles)
	case authn.AppCustomer:
		cp, err := q.GetCustomerProfile(ctx, ids.UUID(p.UserID))
		switch {
		case db.IsNoRows(err):
		case err != nil:
			return Me{}, err
		default:
			m.Customer = &CustomerProfile{ReferralCode: cp.ReferralCode}
			if cp.LimitedUntil != nil && cp.LimitedUntil.After(s.clock.Now()) {
				until := cp.LimitedUntil.UTC()
				m.Customer.LimitedUntil = &until
			}
		}
	}
	return m, nil
}
