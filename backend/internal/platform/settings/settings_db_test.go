package settings

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/money"
	"github.com/todayz/backend/internal/platform/pgnotify"
	"github.com/todayz/backend/internal/testkit"
)

func TestMain(m *testing.M) { testkit.Main(m) }

func TestCacheWithNotify(t *testing.T) {
	db := testkit.NewDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// A long TTL, so only NOTIFY can explain a fresh value.
	c := NewCache(PGLoader(db.Pool), Options{TTL: time.Hour, Log: log})
	l := pgnotify.New(db.Pool, log)
	c.Attach(l)
	done := make(chan struct{})
	go func() { _ = l.Run(ctx); close(done) }()
	select {
	case <-l.Ready():
	case <-time.After(10 * time.Second):
		t.Fatal("listener not ready")
	}

	// Every 01 §17 key is readable through its typed getter.
	for _, k := range AllKeys {
		v, err := c.Get(ctx, k)
		require.NoError(t, err, k)
		switch v.Unit {
		case UnitBool:
			_, err = c.Bool(ctx, k)
		case UnitText:
			_, err = c.String(ctx, k)
		case UnitBP:
			_, err = c.BP(ctx, k)
		case UnitPaise:
			_, err = c.Paise(ctx, k)
		default:
			_, err = c.Int(ctx, k)
		}
		require.NoError(t, err, k)
	}
	d, err := c.Duration(ctx, PickupHoldMin)
	require.NoError(t, err)
	require.Equal(t, 30*time.Minute, d)
	p, err := c.Paise(ctx, CreditMinOrderValuePaise)
	require.NoError(t, err)
	require.Equal(t, money.Paise(10000), p)

	_, err = db.Pool.Exec(ctx, `UPDATE platform_settings SET value = '45' WHERE key = $1`, string(PickupHoldMin))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		d, err := c.Duration(ctx, PickupHoldMin)
		return err == nil && d == 45*time.Minute
	}, 10*time.Second, 20*time.Millisecond, "NOTIFY invalidates the cache")

	_, err = c.Int(ctx, "not_a_setting")
	require.ErrorIs(t, err, ErrUnknownKey)

	cancel()
	<-done
}

func TestSchemaRefusesSecretsAndBadValues(t *testing.T) {
	db := testkit.NewDB(t)
	ctx := context.Background()
	cases := map[string]string{
		"ck_platform_settings_no_secrets": `INSERT INTO platform_settings (key, value, unit) VALUES ('sarvam_api_key', '"x"', 'text')`,
		"ck_platform_settings_paise":      `INSERT INTO platform_settings (key, value, unit, min) VALUES ('fee_amount', '5', 'paise', 0)`,
		"ck_platform_settings_value":      `INSERT INTO platform_settings (key, value, unit) VALUES ('some_flag_enabled', '"yes"', 'bool')`,
		"ck_platform_settings_unit":       `INSERT INTO platform_settings (key, value, unit) VALUES ('odd_unit', '1', 'percent')`,
		"ck_platform_settings_range":      `UPDATE platform_settings SET value = '10001' WHERE key = 'search_w_text'`,
		"ck_platform_settings_key":        `INSERT INTO platform_settings (key, value, unit) VALUES ('Bad-Key', '1', 'count')`,
	}
	for i, k := range []string{"msg91_authkey", "basic_auth", "tls_pem", "otp_hmac", "pii_salt", "totp_seed", "jwt_signing", "razorpay_webhook"} {
		cases[fmt.Sprintf("ck_platform_settings_no_secrets#%d", i)] = `INSERT INTO platform_settings (key, value, unit) VALUES ('` + k + `', '"x"', 'text')`
	}
	// A basis-points setting needs both bounds (an unbounded bp is a typo away
	// from 100x).
	cases["ck_platform_settings_bp#nomax"] = `INSERT INTO platform_settings (key, value, unit, min) VALUES ('test_w_extra', '10', 'bp', 0)`
	cases["ck_platform_settings_bp#nomin"] = `INSERT INTO platform_settings (key, value, unit, max) VALUES ('test_w_extra', '10', 'bp', 100)`
	for name, sql := range cases {
		constraint, _, _ := strings.Cut(name, "#")
		_, err := db.Pool.Exec(ctx, sql)
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, name)
		require.Equal(t, "23514", pgErr.Code, name)
		require.Equal(t, constraint, pgErr.ConstraintName, name)
	}
	// Fractional numbers are refused too.
	_, err := db.Pool.Exec(ctx, `UPDATE platform_settings SET value = '2.5' WHERE key = 'noshow_strikes'`)
	require.Error(t, err)
	// No column can hold a secret by name.
	var n int
	require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name = 'platform_settings' AND column_name ~ '(secret|token|password|key_value|api_key)'`).Scan(&n))
	require.Zero(t, n)
}
