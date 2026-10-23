// Package pii seals and hashes personal data (D-18): phones, IPs and the
// other values 03 marks as PII. A stored phone is two columns: *_enc, sealed
// with AES-256-GCM under PII_ENC_KEY, and *_hash, an HMAC-SHA256 under
// PII_HASH_KEY that finds the row without decrypting anything.
//
// Nothing here logs. Never log what goes in or comes out.
package pii

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// KeySize is the size of PII_ENC_KEY and PII_HASH_KEY.
const KeySize = 32

// ErrOpen is every failure to open a sealed value (wrong key, wrong field,
// tampered or truncated). The cause is not distinguished on purpose.
var ErrOpen = errors.New("pii: cannot open sealed value")

// Sealer encrypts values with AES-256-GCM. A sealed value is the 12-byte
// random nonce followed by the ciphertext and tag. The field name is bound as
// additional data, so a value sealed as one field does not open as another.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer returns a sealer on a 32-byte key (PII_ENC_KEY).
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("pii: the encryption key must be %d bytes", KeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("pii: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("pii: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plain for field (for example "phone").
func (s *Sealer) Seal(field string, plain []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pii: nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plain, []byte(field)), nil
}

// Open decrypts a value sealed for field.
func (s *Sealer) Open(field string, sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, ErrOpen
	}
	out, err := s.aead.Open(nil, sealed[:n], sealed[n:], []byte(field))
	if err != nil {
		return nil, ErrOpen
	}
	return out, nil
}

// Hasher computes keyed lookup hashes (HMAC-SHA256 under PII_HASH_KEY).
type Hasher struct {
	key []byte
}

// NewHasher returns a hasher on a 32-byte key (PII_HASH_KEY).
func NewHasher(key []byte) (*Hasher, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("pii: the hash key must be %d bytes", KeySize)
	}
	return &Hasher{key: append([]byte(nil), key...)}, nil
}

// Phone is the lookup hash of an E.164 phone (phone_identities.phone_hash,
// otp_requests.phone_hash). Normalise the phone first.
func (h *Hasher) Phone(e164 string) []byte { return h.Sum("phone:" + e164) }

// IP is the hash of a client IP, the same as audit_log.ip_hash (no prefix, so
// the two can be joined). Empty in, nil out.
func (h *Hasher) IP(ip string) []byte {
	if ip == "" {
		return nil
	}
	return h.Sum(ip)
}

// IPv6LimitBits is the prefix an IPv6 client is limited by: one subscriber
// is routinely given a whole /64, so per-address limits would not bind.
const IPv6LimitBits = 64

// IPLimit is the hash a per-IP limit counts by: an IPv4 address as itself
// (equal to IP, so it still joins audit_log.ip_hash), an IPv6 address by its
// /64 network. An IPv4-mapped IPv6 address is IPv4. A string that is not an
// address is hashed as given. Empty in, nil out.
func (h *Hasher) IPLimit(ip string) []byte {
	if ip == "" {
		return nil
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return h.IP(ip)
	}
	a = a.Unmap()
	if a.Is4() {
		return h.IP(a.String())
	}
	p, err := a.Prefix(IPv6LimitBits)
	if err != nil {
		return h.IP(ip)
	}
	// "2001:db8:1:2::/64": a prefix never equals an address string, so the
	// two kinds of key cannot collide.
	return h.Sum(p.String())
}

// Sum is HMAC-SHA256(key, s). Callers prefix s with a purpose ("otp:…") so
// two uses never collide.
func (h *Hasher) Sum(s string) []byte {
	m := hmac.New(sha256.New, h.key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

var reIndianMobile = regexp.MustCompile(`^\+91[6-9][0-9]{9}$`)

// NormalizePhone accepts an Indian mobile in E.164 (`+91` and 10 digits
// starting 6–9, the contract's IndianMobile) and returns it unchanged. There
// is deliberately no lenient parsing: the client sends E.164.
func NormalizePhone(s string) (string, bool) {
	if !reIndianMobile.MatchString(s) {
		return "", false
	}
	return s, true
}

// MaskPhone masks a phone as 01 R-16.2 shows it: the first two and last three
// digits of the national number (`98•••••521`). A value that is not an Indian
// mobile masks to all dots.
func MaskPhone(e164 string) string {
	if _, ok := NormalizePhone(e164); !ok {
		return strings.Repeat("•", 10)
	}
	n := e164[3:]
	return n[:2] + strings.Repeat("•", 5) + n[7:]
}

// reservedPrefix is the reserved test range +91 90000 000xx (01 §2): in local
// and test only, those numbers also accept the code 000000.
const reservedPrefix = "+9190000000"

// IsReservedTestPhone reports whether e164 is in the reserved test range. The
// caller decides whether the environment honours it.
func IsReservedTestPhone(e164 string) bool {
	_, ok := NormalizePhone(e164)
	return ok && strings.HasPrefix(e164, reservedPrefix)
}
