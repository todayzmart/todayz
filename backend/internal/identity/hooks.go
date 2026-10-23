package identity

import (
	"context"
	"slices"

	"github.com/todayz/backend/internal/platform/ids"
)

// Account deletion hooks (W1-01 phase B; 01 §2 account deletion, D-31, 08
// AU-18).
//
// DELETE /v1/me runs in one transaction:
//
//  1. every CanDeleteAccount, in registration order, inside a savepoint.
//     Identity's own check comes first: no deletion while no-show strikes
//     limit the account (D-106, 409 conflict with extensions.limitedUntil).
//     The first refusal stops the deletion: nothing of the account changes,
//     the OTP code stays unused and the refusal does not count as one of its
//     tries (the code was right). Orders refuses while an order is open
//     (BK-44, `409 account_has_active_order`).
//  2. every OnAccountDelete, in registration order. Each scrubs or detaches
//     its own rows (credits forfeits the wallet, CR-28; asks deletes the
//     asks, ASK-28). Any error rolls the whole deletion back, identity's part
//     included, and answers that error (a *problem.Error as is, anything else
//     500).
//  3. identity anonymises the user (status deleted, name cleared), deletes the
//     phone identity and addresses, scrubs device push keys and device ids,
//     clears every session's user agent and IP hash, revokes every session
//     and, for a disabled admin, drops the TOTP secret and recovery codes.
//
// Domains never import each other (backend/CLAUDE.md): a domain implements
// the interfaces in its own package and its mountX in cmd/todayz registers
// them on d.Identity:
//
//	d.Identity.AddDeletionCheck(identity.CanDeleteFunc(orders.CanDeleteAccount))
//	d.Identity.AddDeletionHook(identity.OnDeleteFunc(credits.ForfeitOnDelete))
//
// Both run with the deletion transaction in ctx; db.DB.Q(ctx) joins it, so a
// hook's reads see the locked user and its writes commit or roll back with
// the deletion.

// CanDeleteAccount refuses a deletion. A refusal is a *problem.Error the
// handler returns as is (a 409 with a domain code); any other error is 500.
// It must not write: its savepoint is rolled back on a refusal, but its
// writes would commit with a deletion that goes ahead.
type CanDeleteAccount interface {
	CanDeleteAccount(ctx context.Context, userID ids.ID) error
}

// OnAccountDelete runs inside the deletion transaction (the ctx carries it,
// db.DB.Q(ctx) joins it). It must be idempotent: a retried deletion calls it
// again.
type OnAccountDelete interface {
	OnAccountDelete(ctx context.Context, userID ids.ID) error
}

// CanDeleteFunc adapts a function to CanDeleteAccount.
type CanDeleteFunc func(ctx context.Context, userID ids.ID) error

// CanDeleteAccount implements CanDeleteAccount.
func (f CanDeleteFunc) CanDeleteAccount(ctx context.Context, userID ids.ID) error {
	return f(ctx, userID)
}

// OnDeleteFunc adapts a function to OnAccountDelete.
type OnDeleteFunc func(ctx context.Context, userID ids.ID) error

// OnAccountDelete implements OnAccountDelete.
func (f OnDeleteFunc) OnAccountDelete(ctx context.Context, userID ids.ID) error {
	return f(ctx, userID)
}

// Hooks are the registered deletion hooks.
type Hooks struct {
	Can []CanDeleteAccount
	On  []OnAccountDelete
}

// AddDeletionCheck registers a check that may refuse an account deletion.
// Safe for concurrent use; call it while mounting routes.
func (s *Service) AddDeletionCheck(c CanDeleteAccount) {
	if c == nil {
		return
	}
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.hooks.Can = append(s.hooks.Can, c)
}

// AddDeletionHook registers a hook that runs in every account deletion.
// Safe for concurrent use; call it while mounting routes.
func (s *Service) AddDeletionHook(h OnAccountDelete) {
	if h == nil {
		return
	}
	s.hooksMu.Lock()
	defer s.hooksMu.Unlock()
	s.hooks.On = append(s.hooks.On, h)
}

// deletionHooks returns a snapshot of the registered hooks.
func (s *Service) deletionHooks() Hooks {
	s.hooksMu.RLock()
	defer s.hooksMu.RUnlock()
	return Hooks{Can: slices.Clone(s.hooks.Can), On: slices.Clone(s.hooks.On)}
}
