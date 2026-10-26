package admin

import (
	"context"

	api "github.com/todayz/backend/gen/api/admin-catalogue"
	"github.com/todayz/backend/internal/platform/ids"
)

// The writes the A-08 catalogue assistant (W3-13, AI-A1, D-233) applies once
// a reviewer confirms its draft: the same product edit, merge and category
// edit A-08 makes, with the same checks and audit rows. They run inside the
// draft's confirm transaction (InTx joins it as a savepoint). The assistant
// never clears a field: a nil value keeps what is stored.

// AssistReason is the reason the assistant's changes carry in the audit log.
const AssistReason = "AI catalogue assistant, confirmed by the reviewer"

// AssistUpdateProduct edits a product's name, variant, pack size, unit or
// category.
func (s *Service) AssistUpdateProduct(ctx context.Context, actor, id ids.ID, body api.AdminProductUpdate) error {
	if body.Variant != nil {
		// updateProduct reads a variant only when the PATCH body sent the key.
		ctx = context.WithValue(ctx, keysKey{}, map[string]bool{"variant": true})
	} else {
		ctx = context.WithValue(ctx, keysKey{}, map[string]bool{})
	}
	body.Reason = AssistReason
	body.MrpPaise, body.Barcodes, body.Status = nil, nil, nil
	_, err := s.updateProduct(ctx, actor, id, body)
	return err
}

// AssistMergePending merges a pending product into an active one.
func (s *Service) AssistMergePending(ctx context.Context, actor, id, into ids.ID) error {
	reason := AssistReason
	_, err := s.mergePending(ctx, actor, id, api.AdminPendingMerge{IntoProductId: into.String(), Reason: &reason})
	return err
}

// AssistCategoryNames sets a category's display names; the caller sends every
// language, and the stored English name stays the reviewer's.
func (s *Service) AssistCategoryNames(ctx context.Context, actor, id ids.ID, names api.AdminCategoryNames) error {
	_, err := s.updateCategory(ctx, actor, id, api.AdminCategoryUpdate{Reason: AssistReason, Names: &names})
	return err
}
