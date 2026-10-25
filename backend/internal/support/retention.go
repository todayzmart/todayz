package support

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/supportdb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// What leaves with an account, and uploads no message claimed (01 §13, D-226).

// deletedText replaces a deleted customer's subjects and messages. It fits
// both the subject (3..120) and the body (1..4000) bounds.
const deletedText = "Removed: the account was deleted."

// closeReasonDeleted is the close reason of a ticket its customer's account
// deletion closed.
const closeReasonDeleted = "account_deleted"

// OnAccountDelete is identity's deletion hook (identity/hooks.go): it runs in
// the deletion transaction and is idempotent. The customer's tickets that are
// not closed yet are closed (reason account_deleted, audited as the system);
// the subjects of their customer tickets and their own messages on them become
// deletedText; their uploads on those tickets, or claimed by none, are deleted,
// objects first, then rows. A business ticket stays with the business, and
// support's replies and notes stay. 01 has no longer support retention rule.
//
// An object delete that fails rolls the whole deletion back; one already
// deleted counts as done, so the retry finishes the rest.
//
// It reads no settings, so it needs only the database and the audit writer.
func (s *Service) OnAccountDelete(ctx context.Context, userID ids.ID) error {
	if s.db == nil || s.audit == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "support: cannot run the deletion hook (needs the database and the audit writer)")
		return problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	q := s.q(ctx)
	user := ids.UUID(userID)
	now := s.now()
	open, err := q.CustomerTicketsToClose(ctx, user)
	if err != nil {
		return err
	}
	reason := closeReasonDeleted
	for _, t := range open {
		if err := q.CloseTicket(ctx, supportdb.CloseTicketParams{Now: &now, Reason: &reason, ID: t.ID}); err != nil {
			return err
		}
		if _, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorSystem, Action: "support.ticket_close",
			SubjectType: "support_ticket", SubjectID: idString(t.ID),
			Before: map[string]any{"status": t.Status}, After: map[string]any{"status": "closed", "reason": reason},
		}); err != nil {
			return err
		}
	}
	if err := q.ScrubCustomerTickets(ctx, supportdb.ScrubCustomerTicketsParams{Placeholder: deletedText, UserID: user}); err != nil {
		return err
	}
	if err := q.ScrubCustomerMessages(ctx, supportdb.ScrubCustomerMessagesParams{Placeholder: deletedText, UserID: user}); err != nil {
		return err
	}
	files, err := q.CustomerAttachments(ctx, user)
	if err != nil || len(files) == 0 {
		return err
	}
	if s.presign == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "support: cannot delete a deleted account's attachments: storage is not configured")
		return problem.Wrap(genproblems.ServiceUnavailable, errNoStorage)
	}
	gone := make([]pgtype.UUID, 0, len(files))
	for _, f := range files {
		if err := s.presign.remove(ctx, f.ObjectKey, now); err != nil {
			return problem.Wrap(genproblems.ServiceUnavailable, err)
		}
		gone = append(gone, f.ID)
	}
	_, err = q.DeleteAttachments(ctx, gone)
	return err
}

// SweepResult is what one sweep did.
type SweepResult struct {
	Deleted, Failed int64
}

// sweepBatch is how many uploads one sweep transaction takes.
const sweepBatch = 100 //settings:ok batch size of the sweep, not a product rule

// SweepUploads deletes uploads no message claimed within
// support_upload_ttl_hours, objects first, then the rows whose object is
// gone (the support_uploads_sweep job). An object it cannot delete keeps its
// row for the next run. Batches run until one comes back short or with a
// failure, so a stuck object never loops.
func (s *Service) SweepUploads(ctx context.Context) (SweepResult, error) {
	var res SweepResult
	if err := s.ready(ctx); err != nil {
		return res, err
	}
	if s.presign == nil {
		return res, errNoStorage
	}
	ttl, err := s.settings.Duration(ctx, settings.SupportUploadTTLHours)
	if err != nil {
		return res, err
	}
	// A cancelled context (the job's timeout) ends the run with what it did;
	// the next run takes the rest.
	for ctx.Err() == nil {
		var n int
		var failed bool
		err := s.db.InTx(ctx, func(ctx context.Context) error {
			q := s.q(ctx)
			now := s.now()
			rows, err := q.StaleUploads(ctx, supportdb.StaleUploadsParams{Before: now.Add(-ttl), Lim: sweepBatch})
			if err != nil {
				return err
			}
			n = len(rows)
			gone := make([]pgtype.UUID, 0, len(rows))
			for _, r := range rows {
				if err := s.presign.remove(ctx, r.ObjectKey, now); err != nil {
					telemetry.Logger(ctx).WarnContext(ctx, "support: sweep could not delete an upload", "error", err)
					res.Failed++
					failed = true
					continue
				}
				gone = append(gone, r.ID)
			}
			if len(gone) == 0 {
				return nil
			}
			d, err := q.DeleteAttachments(ctx, gone)
			res.Deleted += d
			return err
		})
		if err != nil {
			return res, err
		}
		if n < sweepBatch || failed {
			break
		}
	}
	return res, nil
}
