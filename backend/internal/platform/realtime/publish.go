package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/todayz/backend/internal/platform/db"
)

// PGChannel is the one Postgres NOTIFY channel every event travels on. The
// logical channel (store:…, user:…, admin) is inside the payload: pgnotify
// channel names cannot hold ':' and one LISTEN per instance is the design.
const PGChannel = "tz_realtime"

// publishSQL writes the outbox row and queues the notification in one
// statement, inside the caller's transaction: Postgres sends a NOTIFY only
// when its transaction commits, so a rolled-back transaction delivers
// nothing, and the outbox row (replay) and the notification (live) cannot
// disagree.
//
// The payload carries what the hub needs and nothing it could leak:
//
//	s  outbox seq (unique, so Postgres never folds two NOTIFYs into one)
//	c  logical channel
//	e  the event {type, id, version}
//	x  pg_snapshot_xmin of this statement's snapshot: every transaction with
//	   a smaller xid had ended before it, so its NOTIFY (commit order) is
//	   delivered before this one. After the hub has fanned this notification
//	   out, x is a valid stream cursor (cursor.go).
//	t  publish time in Unix ms, for the delivery-lag metric only
const publishSQL = `
WITH ins AS (
  INSERT INTO outbox_events (channel, payload) VALUES ($1, $2::jsonb) RETURNING seq
)
SELECT pg_notify('` + PGChannel + `', json_build_object(
  's', ins.seq,
  'c', $1::text,
  'e', $2::jsonb,
  'x', pg_snapshot_xmin(pg_current_snapshot())::text,
  't', (extract(epoch FROM clock_timestamp()) * 1000)::bigint
)::text) FROM ins`

// ErrNoTx is returned by PublishCtx when ctx carries no transaction.
var ErrNoTx = errors.New("realtime: publish needs the caller's transaction (db.InTx)")

// Publish records ev on channel c inside tx. The stream sees it only if tx
// commits. Call it in the same transaction as the change it announces, after
// the change, with the entity's new version.
//
//	err := d.InTx(ctx, func(ctx context.Context) error {
//	    tx, _ := db.TxFrom(ctx)
//	    // … UPDATE orders … RETURNING version
//	    return realtime.Publish(ctx, tx, realtime.StoreChannel(storeID),
//	        realtime.Event{Type: realtime.OrderUpdated, ID: orderID, Version: v})
//	})
func Publish(ctx context.Context, tx pgx.Tx, c Channel, ev Event) error {
	if tx == nil {
		return ErrNoTx
	}
	if err := Validate(c, ev); err != nil {
		return err
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("realtime: encode event: %w", err)
	}
	if _, err := tx.Exec(ctx, publishSQL, string(c), string(payload)); err != nil {
		return fmt.Errorf("realtime: publish %s: %w", ev.Type, err)
	}
	return nil
}

// PublishCtx is Publish with the transaction that db.InTx put in ctx. It
// refuses (ErrNoTx) outside a transaction instead of publishing on its own,
// so an event can never be delivered for a change that rolled back.
func PublishCtx(ctx context.Context, c Channel, ev Event) error {
	tx, ok := db.TxFrom(ctx)
	if !ok {
		return ErrNoTx
	}
	return Publish(ctx, tx, c, ev)
}
