package realtime

// The stream cursor (the SSE `id`, sent back as Last-Event-ID).
//
// A cursor is a Postgres transaction id X (xid8, decimal) with one meaning:
// every event written by a transaction whose xid is below X has already been
// written to this client. Resuming is then `SELECT … WHERE xid >= X`, which
// may repeat an event but can never skip one. Clients only refetch on an
// event, so a repeat costs one request and a skip would be a stale screen.
//
// Why not the outbox seq: a bigserial is taken when the row is inserted, not
// when its transaction commits, so a transaction holding seq 10 can commit
// after one holding seq 11 has been streamed. A client resuming "after 11"
// would lose 10 forever.
//
// Where X comes from, each valid by construction:
//   - at connect: pg_snapshot_xmin of the snapshot the replay ran in. Every
//     transaction below it had ended before the snapshot, so its rows were
//     visible to the replay (or it rolled back and wrote nothing), and every
//     transaction that commits later notifies the hub after this subscriber
//     was registered.
//   - per notification: the publisher's own snapshot xmin (the `x` in the
//     NOTIFY payload). Postgres delivers NOTIFYs in commit order, so every
//     transaction below x had already notified, and the hub had queued its
//     event for this subscriber before this one.
//   - idle: the hub's mark, the largest x it has fanned out. Valid for a
//     subscriber whose queue is empty, read before checking the queue.
//
// Cursors never decrease (max of the above), and a cursor is meaningful on
// any instance because xids are the database's, not the instance's.

import (
	"strconv"
)

// maxCursorLen bounds Last-Event-ID (an xid8 is at most 20 decimal digits).
const maxCursorLen = 20

// parseCursor reads a Last-Event-ID. ok is false for an empty, malformed or
// zero value: the client then gets `reset` (refetch everything).
func parseCursor(s string) (uint64, bool) {
	if s == "" || len(s) > maxCursorLen {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil || v == 0 {
		return 0, false
	}
	return v, true
}

func formatCursor(v uint64) string { return strconv.FormatUint(v, 10) }
