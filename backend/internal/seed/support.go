package seed

import (
	"context"
	"fmt"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// seedTicket is one support ticket with its opening message (W3-08, D-226).
// Numbers are fixed below the sequence's start (1001), so a reseed gives the
// same SUP-000101… and never collides with a ticket opened later.
type seedTicket struct {
	ID, MessageID string
	Number        int
	Opener        string // identity user key
	Business      string // business key; empty for a customer ticket
	Store         string // store key, optional
	Category      string
	Priority      string
	Status        string // open or resolved
	Subject, Body string
	Age           time.Duration // opened this long before the seed clock
}

var seedTickets = []seedTicket{
	{ID: "01KE0S1W6W8ZV7T1C3A6MS0001", MessageID: "01KE0S1W6W8ZV7T1C3A6MS1001", Number: 101, Opener: "staff.owner", Business: "lakshmi", Store: "SL",
		Category: "payouts", Priority: "normal", Status: "open", Age: 3 * time.Hour,
		Subject: "Last week's payout has not arrived", Body: "The payout for last week shows as sent but it is not in our bank account yet. Can you check?"},
	{ID: "01KE0S1W6W8ZV7T1C3A6MS0002", MessageID: "01KE0S1W6W8ZV7T1C3A6MS1002", Number: 102, Opener: "customer.asha",
		Category: "order", Priority: "urgent", Status: "open", Age: 2 * time.Hour,
		Subject: "My pickup was not ready", Body: "I went to collect my order and the shop said it was not packed. I waited 20 minutes."},
	{ID: "01KE0S1W6W8ZV7T1C3A6MS0003", MessageID: "01KE0S1W6W8ZV7T1C3A6MS1003", Number: 103, Opener: "customer.ravi",
		Category: "credits", Priority: "high", Status: "open", Age: 30 * time.Minute,
		Subject: "Credits from my referral are missing", Body: "My friend joined with my code last week but I did not get the credits."},
	{ID: "01KE0S1W6W8ZV7T1C3A6MS0004", MessageID: "01KE0S1W6W8ZV7T1C3A6MS1004", Number: 104, Opener: "staff.manager", Business: "lakshmi",
		Category: "account", Priority: "normal", Status: "resolved", Age: 3 * day,
		Subject: "How do we add a second counter?", Body: "We want a second billing counter in the same store. Where do we set it up?"},
}

// seedsupport writes a few business and customer tickets so the A-14 inbox and
// B-15 list have both kinds. Due times come from the seeded SLA settings.
func seedsupport(ctx context.Context, d Deps, _ Profile) error {
	q := d.DB.Q(ctx)
	now := d.Clock.Now().UTC()
	for _, t := range seedTickets {
		opened := now.Add(-t.Age)
		var business, store any
		if t.Business != "" {
			business = ids.UUID(businessSeedID(t.Business))
		}
		if t.Store != "" {
			store = ids.UUID(storeSeedID(t.Store))
		}
		openerType := "customer"
		if t.Business != "" {
			openerType = "business"
		}
		var resolvedAt any
		var reason *string
		if t.Status == "resolved" {
			resolvedAt = opened.Add(2 * time.Hour)
			reason = opt("Answered: counters are added under Settings, Counters.")
		}
		// Users are seeded only with the PII keys set; without them there is no opener.
		if _, err := q.Exec(ctx, `INSERT INTO support_tickets
			(id, number, opener_type, opener_user_id, business_id, store_id, category, subject, status, priority,
			 sla_started_at, sla_due_at, last_message_at, resolved_at, close_reason, created_at, updated_at)
			SELECT $1::uuid, $2::bigint, $3::text, u.id, $5::uuid, $6::uuid, $7::text, $8::text, $9::text, $10::text,
			 CASE WHEN $9::text = 'open' THEN $11::timestamptz END,
			 CASE WHEN $9::text = 'open' THEN $11::timestamptz + (SELECT (value #>> '{}')::int FROM platform_settings
			   WHERE key = 'support_sla_' || $10::text || '_min') * interval '1 minute' END,
			 $11::timestamptz, $12::timestamptz, $13::text, $11::timestamptz, $11::timestamptz
			FROM users u WHERE u.id = $4::uuid
			ON CONFLICT DO NOTHING`,
			ids.UUID(mustID(t.ID)), t.Number, openerType, ids.UUID(identityUserID(t.Opener)), business, store,
			t.Category, t.Subject, t.Status, t.Priority, opened, resolvedAt, reason); err != nil {
			return fmt.Errorf("ticket %d: %w", t.Number, err)
		}
		if _, err := q.Exec(ctx, `INSERT INTO support_messages (id, ticket_id, author_type, author_user_id, kind, body, created_at)
			SELECT $1::uuid, t.id, $3::text, t.opener_user_id, 'reply', $4::text, $5::timestamptz
			FROM support_tickets t WHERE t.id = $2::uuid
			ON CONFLICT DO NOTHING`,
			ids.UUID(mustID(t.MessageID)), ids.UUID(mustID(t.ID)), openerType, t.Body, opened); err != nil {
			return fmt.Errorf("ticket %d: message: %w", t.Number, err)
		}
	}
	return nil
}
