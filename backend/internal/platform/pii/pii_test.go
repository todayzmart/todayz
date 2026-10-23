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
