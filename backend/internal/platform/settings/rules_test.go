package settings

// SC-PL-06 (08-testing) parts 1 and 2:
//  1. platform_settings seed == 01 §17, both directions, matching unit and default.
//  2. no key, token or secret is ever a setting (D-19).
// Part 3 (no hard-coded copy of a configurable default) is the settingslint
// command in internal/devtools/settingslint.

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	dbschema "github.com/todayz/backend/db"
)

type docRow struct {
	Unit    Unit
	Default string // canonical JSON
}

// productRulesPath finds docs/plan/01-product-rules.md. Inside the monorepo it
// is two levels above the backend module; TODAYZ_ROOT (set by make) wins.
func productRulesPath(t *testing.T) string {
	t.Helper()
	var roots []string
	if r := os.Getenv("TODAYZ_ROOT"); r != "" {
		roots = append(roots, r)
	}
	wd, _ := os.Getwd()
	for d := wd; ; d = filepath.Dir(d) {
		roots = append(roots, d)
		if filepath.Dir(d) == d {
			break
		}
	}
	for _, r := range roots {
		p := filepath.Join(r, "docs", "plan", "01-product-rules.md")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if os.Getenv("TODAYZ_ROOT") != "" {
		t.Fatal("docs/plan/01-product-rules.md not found under TODAYZ_ROOT")
	}
	t.Skip("docs/plan/01-product-rules.md not found (backend outside the monorepo); SC-PL-06 part 1 needs it")
	return ""
}

var (
	reDocRow = regexp.MustCompile("^\\| `([a-z0-9_]+)`(?: \\([^)]*\\))? \\| (\\w+) \\| `?([^|`]+?)`?(?: \\([^|]*\\))? \\|")
	reSQLRow = regexp.MustCompile(`^\s*\('([a-z0-9_]+)', '([^']*)', '(\w+)', (\S+), (\S+), '`)
)

func canonicalDefault(unit Unit, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch unit {
	case UnitBool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case UnitText:
		b, err := json.Marshal(raw)
		return string(b), err
	default:
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(n, 10), nil
	}
}

func parseProductRules(t *testing.T, path string) map[Key]docRow {
	t.Helper()
	b, err := os.ReadFile(path) // #nosec G304 -- repo doc
	require.NoError(t, err)
	text := string(b)
	start := strings.Index(text, "\n## 17. Settings keys")
	require.NotEqual(t, -1, start, "01 §17 heading not found")
	section := text[start+1:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	out := map[Key]docRow{}
	for _, line := range strings.Split(section, "\n") {
		m := reDocRow.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		unit := Unit(m[2])
		def, err := canonicalDefault(unit, m[3])
		require.NoError(t, err, "01 §17 %s default %q", m[1], m[3])
		_, dup := out[Key(m[1])]
		require.False(t, dup, "01 §17 lists %s twice", m[1])
		out[Key(m[1])] = docRow{Unit: unit, Default: def}
	}
	require.NotEmpty(t, out)
	return out
}

// seedMigrationRows collects the platform_settings rows inserted by every
// migration's Up section. A key added to 01 §17 after W0-03 is seeded by its
// own migration (D-95), so the check cannot read 00002 alone.
func seedMigrationRows(t *testing.T) map[Key]docRow {
	t.Helper()
	entries, err := fs.ReadDir(dbschema.Migrations, "migrations")
	require.NoError(t, err)
	out := map[Key]docRow{}
	from := map[Key]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := dbschema.Migrations.ReadFile("migrations/" + e.Name())
		require.NoError(t, err)
		up := strings.SplitN(string(b), "-- +goose Down", 2)[0]
		if !strings.Contains(up, "INSERT INTO platform_settings") {
			continue
		}
		for _, line := range strings.Split(up, "\n") {
			m := reSQLRow.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			k := Key(m[1])
			require.Empty(t, from[k], "%s is seeded by both %s and %s", k, from[k], e.Name())
			from[k] = e.Name()
			var v any
			require.NoError(t, json.Unmarshal([]byte(m[2]), &v), "seed %s value", m[1])
			canon, _ := json.Marshal(v)
			out[k] = docRow{Unit: Unit(m[3]), Default: string(canon)}
		}
	}
	require.NotEmpty(t, out)
	return out
}

func TestSettingsMatchProductRules(t *testing.T) {
	doc := parseProductRules(t, productRulesPath(t))
	seed := seedMigrationRows(t)
	for k, d := range doc {
		s, ok := seed[k]
		if !assertOK(t, ok, "%s is in 01 §17 but not in the seed migration", k) {
			continue
		}
		require.Equal(t, d.Unit, s.Unit, "%s unit", k)
		require.Equal(t, d.Default, s.Default, "%s default", k)
	}
	for k := range seed {
		_, ok := doc[k]
		require.True(t, ok, "%s is in the seed migration but not in 01 §17 (a key that is not in 01 does not exist)", k)
	}
}

func assertOK(t *testing.T, ok bool, msg string, args ...any) bool {
	t.Helper()
	if !ok {
		t.Errorf(msg, args...)
	}
	return ok
}

func TestKeysMatchMigration(t *testing.T) {
	seed := seedMigrationRows(t)
	require.Len(t, AllKeys, len(seed))
	seen := map[Key]bool{}
	for _, k := range AllKeys {
		require.False(t, seen[k], "duplicate constant %s", k)
		seen[k] = true
		_, ok := seed[k]
		require.True(t, ok, "keys.go constant %s is not seeded", k)
	}
}

// secretWordPattern is the same pattern as ck_platform_settings_no_secrets
// (TestNoSecretSettings checks the migration carries it verbatim).
const secretWordPattern = `(^|_)(key|keys|apikey|authkey|auth|secret|secrets|token|tokens|password|passwd|pwd|dsn|credential|credentials|privkey|cert|pem|hmac|salt|seed|signing|webhook)(_|$)`

var reSecretWord = regexp.MustCompile(secretWordPattern)

func TestNoSecretSettings(t *testing.T) {
	// D-19: settings hold flags and numbers only, never keys, tokens or secrets.
	for k, r := range seedMigrationRows(t) {
		require.False(t, reSecretWord.MatchString(string(k)), "%s looks like a secret", k)
		if r.Unit == UnitText {
			var s string
			require.NoError(t, json.Unmarshal([]byte(r.Default), &s))
			require.Regexp(t, `^[a-z0-9][a-z0-9.\-]{0,39}$`, s, "%s: text settings are short names, never secret material", k)
		}
	}
	// The word list catches the obvious provider names.
	for _, bad := range []string{"anthropic_api_key", "msg91_auth_key", "msg91_authkey", "sentry_dsn", "jwt_signing_keys",
		"sms_token", "admin_password", "whatsapp_secret", "basic_auth", "tls_pem", "otp_hmac", "pii_salt", "totp_seed",
		"signing_material", "razorpay_webhook"} {
		require.True(t, reSecretWord.MatchString(bad), bad)
	}
	for _, ok := range []string{"search_w_text", "order_accept_timeout_min", "seeding_enabled", "authors_count"} {
		require.False(t, reSecretWord.MatchString(ok), ok)
	}
	// The migration enforces the same rule in the schema, with the same list.
	b, err := dbschema.Migrations.ReadFile("migrations/00001_w0-03_baseline.sql")
	require.NoError(t, err)
	require.Contains(t, string(b), "CONSTRAINT ck_platform_settings_no_secrets")
	require.Contains(t, string(b), "key !~ '"+secretWordPattern+"'")
}
