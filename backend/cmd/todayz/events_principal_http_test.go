package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEventsCarryTheSignedInUser: POST /v1/events stores the caller's user id
// when a token is sent, and none without one (W1-07 follow-up to W1-01).
func TestEventsCarryTheSignedInUser(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	a := newBizAPI(t)
	tok := a.signIn(strangerPhone, "customer")
	batch := func(eventID string) string {
		return `{"app":"customer","appVersion":"3f2a9c1","platform":"web",
"anonId":"01JB0S1W6W8ZV7T1C3A6M9N4PQ","sessionId":"01JB0S1W6W8ZV7T1C3A6M9N4PR",
"events":[{"id":"` + eventID + `","name":"app_opened","t":"2026-10-23T05:59:00Z","props":{"launch":"cold"}}]}`
	}

	w := a.call(tok, http.MethodPost, "/v1/events", batch("01JB0S1W6W8ZV7T1C3A6M9N4PS"), nil)
	require.Equal(t, http.StatusAccepted, w.code, w.raw)
	w = a.call("", http.MethodPost, "/v1/events", batch("01JB0S1W6W8ZV7T1C3A6M9N4PT"), nil)
	require.Equal(t, http.StatusAccepted, w.code, w.raw)

	var signedIn, anonymous int
	require.NoError(t, a.tdb.OwnerPool.QueryRow(context.Background(), `
		SELECT count(*) FILTER (WHERE e.user_id = p.user_id), count(*) FILTER (WHERE e.user_id IS NULL)
		FROM events e CROSS JOIN (SELECT user_id FROM sessions ORDER BY created_at DESC LIMIT 1) p`).
		Scan(&signedIn, &anonymous))
	require.Equal(t, 1, signedIn, "the signed-in event names its user")
	require.Equal(t, 1, anonymous, "the anonymous event names none")
}
