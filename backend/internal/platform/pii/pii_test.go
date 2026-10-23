package pii

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func key(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, KeySize)
	_, err := rand.Read(k)
	require.NoError(t, err)
	return k
}

func TestSealer(t *testing.T) {
	s, err := NewSealer(key(t))
	require.NoError(t, err)
	const phone = "+919876543210"
	a, err := s.Seal("phone", []byte(phone))
	require.NoError(t, err)
	b, err := s.Seal("phone", []byte(phone))
	require.NoError(t, err)
	require.NotEqual(t, a, b, "a fresh nonce every time")
	require.False(t, bytes.Contains(a, []byte(phone)))

	got, err := s.Open("phone", a)
	require.NoError(t, err)
	require.Equal(t, phone, string(got))

	_, err = s.Open("address", a)
	require.ErrorIs(t, err, ErrOpen, "the field is bound")
	tampered := bytes.Clone(a)
	tampered[len(tampered)-1] ^= 1
	_, err = s.Open("phone", tampered)
	require.ErrorIs(t, err, ErrOpen)
	_, err = s.Open("phone", a[:10])
	require.ErrorIs(t, err, ErrOpen)
	other, err := NewSealer(key(t))
	require.NoError(t, err)
	_, err = other.Open("phone", a)
	require.ErrorIs(t, err, ErrOpen, "another key")

	_, err = NewSealer(make([]byte, 16))
	require.Error(t, err)
}

func TestHasher(t *testing.T) {
	k := key(t)
	h, err := NewHasher(k)
	require.NoError(t, err)
	k[0] ^= 1 // the hasher keeps its own copy
	h2, err := NewHasher(k)
	require.NoError(t, err)

	p := h.Phone("+919876543210")
	require.Len(t, p, 32)
	require.Equal(t, p, h.Phone("+919876543210"), "deterministic")
	require.NotEqual(t, p, h2.Phone("+919876543210"), "keyed")
	require.NotEqual(t, p, h.Phone("+919876543211"))
	require.NotEqual(t, h.Sum("+919876543210"), p, "purpose-prefixed")
	require.Nil(t, h.IP(""))
	require.Equal(t, h.Sum("10.0.0.1"), h.IP("10.0.0.1"), "same as audit_log.ip_hash")

	_, err = NewHasher(nil)
	require.Error(t, err)
}

// The per-IP limit key: IPv4 by address (equal to IP), IPv6 by its /64.
func TestIPLimit(t *testing.T) {
	h, err := NewHasher(key(t))
	require.NoError(t, err)

	require.Nil(t, h.IPLimit(""))
	require.Equal(t, h.IP("10.0.0.1"), h.IPLimit("10.0.0.1"), "IPv4 is the full address")
	require.NotEqual(t, h.IPLimit("10.0.0.1"), h.IPLimit("10.0.0.2"))
	require.Equal(t, h.IPLimit("10.0.0.1"), h.IPLimit("::ffff:10.0.0.1"), "IPv4-mapped is IPv4")

	a := h.IPLimit("2001:db8:1:2::1")
	require.Len(t, a, 32)
	for _, same := range []string{"2001:db8:1:2:ffff:ffff:ffff:ffff", "2001:0db8:0001:0002:abcd::9", "2001:db8:1:2::1%eth0"} {
		require.Equal(t, a, h.IPLimit(same), "one /64: %s", same)
	}
	for _, other := range []string{"2001:db8:1:3::1", "2001:db8:2:2::1", "2001:db9:1:2::1"} {
		require.NotEqual(t, a, h.IPLimit(other), "another /64: %s", other)
	}
	require.NotEqual(t, h.IP("2001:db8:1:2::1"), a, "IPv6 limits are not per address")
	require.Equal(t, h.IP("not-an-ip"), h.IPLimit("not-an-ip"))
}

func TestPhones(t *testing.T) {
	for _, ok := range []string{"+919876543210", "+916000000000", "+919000000001"} {
		got, valid := NormalizePhone(ok)
		require.True(t, valid, ok)
		require.Equal(t, ok, got)
	}
	for _, bad := range []string{"", "9876543210", "+91 9876543210", "+915876543210", "+9198765432101", "+14155550123", "+91987654321x"} {
		_, valid := NormalizePhone(bad)
		require.False(t, valid, bad)
	}
	require.Equal(t, "98•••••210", MaskPhone("+919876543210"))
	require.Equal(t, "••••••••••", MaskPhone("garbage"))

	require.True(t, IsReservedTestPhone("+919000000001"))
	require.True(t, IsReservedTestPhone("+919000000099"))
	require.False(t, IsReservedTestPhone("+919000000101"))
	require.False(t, IsReservedTestPhone("+919876543210"))
	require.False(t, IsReservedTestPhone("+9190000000"))
}
