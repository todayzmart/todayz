package identity

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- RFC 6238 TOTP is HMAC-SHA1 for every authenticator app; not a collision use
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/ids"
)

// Admin TOTP (02 §3.2, 01 §2): RFC 6238 with the parameters every
// authenticator app supports: HMAC-SHA1, 30-second steps, 6 digits, a
// 160-bit secret. One step either side is accepted for clock drift, and a
// step is accepted once (admin_users.totp_last_step).
//
// 02 §1 names pquerna/otp for this. It is not in go.mod, and the algorithm is
// twenty lines of the standard library checked against the RFC's own test
// vectors (totp_test.go), so no module is added for it.
const (
	TOTPDigits     = 6
	TOTPPeriod     = 30 * time.Second
	TOTPSkew       = 1  // steps accepted either side of now
	totpSecretLen  = 20 // bytes (RFC 4226 §4 recommends 160 bits)
	TOTPIssuer     = "TodayZ"
	totpModulo     = 1_000_000 // 10^TOTPDigits
	totpCodeFormat = "%06d"
)

// TOTPSealFieldFor is the pii.Sealer field (the AES-GCM associated data) of
// admin userID's admin_users.totp_secret_enc: "totp_secret:<user id>". The
// seal is bound to its admin, so a sealed secret copied to another admin's
// row does not open. The admin seed seals with it too.
func TOTPSealFieldFor(userID ids.ID) string { return "totp_secret:" + userID.String() }

// TOTPMaxFailures is how many wrong TOTP or recovery codes in a row end every
// admin session of that admin (they sign in again with OTP).
// TODO(lead): 01 §2 gives no number for TOTP; this reuses the OTP's five
// tries (OTPMaxAttempts), the conservative reading.
const TOTPMaxFailures = OTPMaxAttempts

var totpB32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns a random 160-bit secret.
func newTOTPSecret() ([]byte, error) {
	b := make([]byte, totpSecretLen)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("identity: totp secret: %w", err)
	}
	return b, nil
}

// EncodeTOTPSecret is the base32 form an authenticator app takes (no padding).
func EncodeTOTPSecret(secret []byte) string { return totpB32.EncodeToString(secret) }

// DecodeTOTPSecret reads the base32 form (case and spaces ignored, padding optional).
func DecodeTOTPSecret(s string) ([]byte, error) {
	s = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	s = strings.TrimRight(s, "=")
	b, err := totpB32.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("identity: totp secret is not base32")
	}
	if len(b) < 10 {
		return nil, fmt.Errorf("identity: totp secret is shorter than 80 bits")
	}
	return b, nil
}

// totpStep is the RFC 6238 counter for t.
func totpStep(t time.Time) int64 { return t.Unix() / int64(TOTPPeriod/time.Second) }

// hotp is RFC 4226 §5.3 with 6 digits.
func hotp(secret []byte, counter int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter)) // #nosec G115 -- counters are positive Unix steps
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf(totpCodeFormat, v%totpModulo)
}

// TOTPCode is the code for secret at t (tests and the admin seed's fixtures).
func TOTPCode(secret []byte, t time.Time) string { return hotp(secret, totpStep(t)) }

// verifyTOTP checks code against the steps around now and returns the step
// it matched. Every candidate is compared in constant time, and a step at or
// before lastStep (already used) never matches.
func verifyTOTP(secret []byte, code string, now time.Time, lastStep *int64) (int64, bool) {
	if len(code) != TOTPDigits {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	cur := totpStep(now)
	var (
		matched int64
		ok      bool
	)
	for d := int64(-TOTPSkew); d <= TOTPSkew; d++ {
		step := cur + d
		eq := subtle.ConstantTimeCompare([]byte(hotp(secret, step)), []byte(code)) == 1
		if eq && (lastStep == nil || step > *lastStep) && !ok {
			matched, ok = step, true
		}
	}
	return matched, ok
}

// totpURI is the otpauth:// URI the admin app shows as a QR code. The label
// is the admin's user id, never the phone (D-15).
func totpURI(secret []byte, account string) string {
	label := url.PathEscape(TOTPIssuer + ":" + account)
	q := url.Values{}
	q.Set("secret", EncodeTOTPSecret(secret))
	q.Set("issuer", TOTPIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(TOTPDigits))
	q.Set("period", strconv.Itoa(int(TOTPPeriod/time.Second)))
	return "otpauth://totp/" + label + "?" + q.Encode()
}
