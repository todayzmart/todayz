package db_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/db"
)

func TestIsRetryable(t *testing.T) {
	require.True(t, db.IsRetryable(&pgconn.PgError{Code: "40P01"}))
	require.True(t, db.IsRetryable(&pgconn.PgError{Code: "40001"}))
	require.True(t, db.IsRetryable(fmt.Errorf("pos: sale: %w", &pgconn.PgError{Code: "40P01"})), "wrapped")
	require.False(t, db.IsRetryable(&pgconn.PgError{Code: "23505"}))
	require.False(t, db.IsRetryable(errors.New("40P01")))
	require.False(t, db.IsRetryable(nil))
}
