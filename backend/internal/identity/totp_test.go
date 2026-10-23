package identity

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// RFC 6238 Appendix B, SHA-1 rows: the 8-digit codes, of which a 6-digit
// TOTP is the last six digits (the same truncation mod 10^6).
func TestTOTPRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	cases := []struct {
		unix int64
		code string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, c := range cases {
		got := TOTPCode(secret, time.Unix(c.unix, 0))
		if want := c.code[2:]; got != want {
			t.Errorf("t=%d: got %s, want %s", c.unix, got, want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	cur := totpStep(now)
	for _, d := range []int64{-1, 0, 1} {
		code := hotp(secret, cur+d)
		step, ok := verifyTOTP(secret, code, now, nil)
		if !ok || step != cur+d {
			t.Errorf("drift %d: ok=%v step=%d", d, ok, step)
		}
	}
	for _, d := range []int64{-2, 2} {
		if _, ok := verifyTOTP(secret, hotp(secret, cur+d), now, nil); ok {
			t.Errorf("drift %d accepted", d)
		}
	}
	// A step at or before the last accepted one is a replay.
	last := cur
	if _, ok := verifyTOTP(secret, hotp(secret, cur), now, &last); ok {
		t.Error("replayed step accepted")
	}
	if _, ok := verifyTOTP(secret, hotp(secret, cur+1), now, &last); !ok {
		t.Error("next step refused")
	}
	for _, bad := range []string{"", "12345", "1234567", "12345a", " 12345"} {
		if _, ok := verifyTOTP(secret, bad, now, nil); ok {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestTOTPSecretRoundTripAndURI(t *testing.T) {
	s, err := newTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 20 {
		t.Fatalf("len %d", len(s))
	}
	enc := EncodeTOTPSecret(s)
	if strings.Contains(enc, "=") {
		t.Fatal("padding in secret")
	}
	back, err := DecodeTOTPSecret(strings.ToLower(enc) + "==")
	if err != nil || string(back) != string(s) {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := DecodeTOTPSecret("not base32!"); err == nil {
		t.Fatal("junk accepted")
	}
	if _, err := DecodeTOTPSecret("AAAA"); err == nil {
		t.Fatal("short secret accepted")
	}
	u, err := url.Parse(totpURI(s, "01KE0S1W6W8ZV7T1C3A6M9N0AA"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "otpauth" || u.Host != "totp" || q.Get("secret") != enc || q.Get("issuer") != "TodayZ" ||
		q.Get("digits") != "6" || q.Get("period") != "30" || q.Get("algorithm") != "SHA1" {
		t.Fatalf("uri %s", u)
	}
	if !strings.HasPrefix(u.Path, "/TodayZ:") {
		t.Fatalf("label %s", u.Path)
	}
}

// L5: recovery codes are 16 Crockford base32 symbols (80 bits), shown as
// XXXX-XXXX-XXXX-XXXX; typing ignores case, dashes and spaces and reads I and
// L as 1 and O as 0.
func TestRecoveryCodeFormat(t *testing.T) {
	require.Len(t, recoveryAlphabet, 32)
	seen := map[rune]bool{}
	for _, r := range recoveryAlphabet {
		require.False(t, seen[r], "unique symbols")
		seen[r] = true
	}
	require.GreaterOrEqual(t, recoveryCodeLen*5, 80, "at least 80 bits")

	shape := regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{4}(-[0-9A-HJKMNP-TV-Z]{4}){3}$`)
	codes := map[string]bool{}
	for range 200 {
		c, err := newRecoveryCode()
		require.NoError(t, err)
		require.Regexp(t, shape, c)
		norm, ok := normalizeRecoveryCode(c)
		require.True(t, ok)
		require.Equal(t, strings.ReplaceAll(c, "-", ""), norm)
		codes[c] = true
	}
	require.Len(t, codes, 200, "no repeats")

	for typed, want := range map[string]string{
		"abcd-efgh-jkmn-pqrs": "ABCDEFGHJKMNPQRS",
		"ABCD EFGH JKMN PQRS": "ABCDEFGHJKMNPQRS",
		"oIl0-0000-0000-0000": "0110000000000000",
		"tvwxyz01-23456789":   "TVWXYZ0123456789",
	} {
		norm, ok := normalizeRecoveryCode(typed)
		require.True(t, ok, typed)
		require.Equal(t, want, norm, typed)
	}
	for _, bad := range []string{"", "ABCDE-FGHJK", "ABCD-EFGH-JKMN-PQR", "ABCD-EFGH-JKMN-PQRST", "UBCD-EFGH-JKMN-PQRS", "ABCD-EFGH-JKMN-PQR!"} {
		_, ok := normalizeRecoveryCode(bad)
		require.False(t, ok, bad)
	}
}
