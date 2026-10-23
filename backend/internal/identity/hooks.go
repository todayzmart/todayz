package identity

import (
	"context"

	"github.com/todayz/backend/internal/platform/ids"
)

// Account deletion hooks (W1-01 phase B; 01 §2 account deletion, 02 §3).
//
// Phase A defines the types only. Phase B adds DELETE /v1/me, which asks every
// CanDeleteAccount whether the account may go (for example orders refuses
// while an order is active, credits while a balance is unpaid), then, in the
// deletion transaction, calls every OnAccountDelete so each domain scrubs or
// detaches its rows. Domains register through cmd/todayz (they never import
// each other, backend/CLAUDE.md).

// CanDeleteAccount refuses a deletion. A refusal is a *problem.Error the
// handler returns as is (a 409 with a domain code); any other error is 500.
type CanDeleteAccount interface {
	CanDeleteAccount(ctx context.Context, userID ids.ID) error
}

// OnAccountDelete runs inside the deletion transaction (the ctx carries it,
// db.DB.Q(ctx) joins it). It must be idempotent: a retried deletion calls it
// again.
type OnAccountDelete interface {
	OnAccountDelete(ctx context.Context, userID ids.ID) error
}

// Hooks are the registered deletion hooks.
type Hooks struct {
	Can []CanDeleteAccount
	On  []OnAccountDelete
}
