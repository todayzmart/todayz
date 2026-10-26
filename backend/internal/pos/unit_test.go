package pos

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	syncapi "github.com/todayz/backend/gen/api/sync"
	"github.com/todayz/backend/gen/sqlc/posdb"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
)

// D-94: a bill rounds to the nearest ₹1, half up.
func TestRoundOff(t *testing.T) {
	for _, c := range []struct {
		payable int64
		want    int32
	}{
		{100, 0}, {101, -1}, {149, -49}, {150, 50}, {199, 1}, {12_345, -45}, {12_350, 50}, {99, 1}, {0, 0},
	} {
		got := roundOff(c.payable)
		require.Equal(t, c.want, got, "payable %d", c.payable)
		require.Zero(t, (c.payable+int64(got))%100, "lands on a rupee")
	}
}

// Cashback is whole credits, rounded down, without overflow.
func TestCashbackCredits(t *testing.T) {
	require.Equal(t, 0, cashbackCredits(0, 200))
	require.Equal(t, 0, cashbackCredits(10_000, 0))
	require.Equal(t, 2, cashbackCredits(10_000, 200))            // 2% of ₹100
	require.Equal(t, 2, cashbackCredits(14_999, 200))            // ₹2.99 → 2
	require.Equal(t, 0, cashbackCredits(4_999, 200))             // 99 paise → 0
	require.Equal(t, 10_000, cashbackCredits(1_000_000, 10_000)) // 100% of ₹10,000
	big := int64(9_000_000_000_000_000)
	require.Equal(t, int(big/1_000_000*5), cashbackCredits(big, 5), "no overflow at a huge total")
}

func TestPriceBill(t *testing.T) {
	t0 := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	id := func() ids.ID { return ids.New(t0) }
	lines := []lineInput{
		{ClientID: id(), Name: "Atta", Unit: 29_000, Qty: 1, Offer: 1_000},
		{ClientID: id(), Name: "Butter", Unit: 5_550, Qty: 2, Manual: 100},
	}
	b, perr := price(lines, 500, 200, 0, "body")
	require.Nil(t, perr)
	require.EqualValues(t, 28_000+11_100, b.ItemSubtotal)
	require.EqualValues(t, 500, b.BillOffer)
	require.EqualValues(t, 300, b.Discount)
	require.EqualValues(t, 39_100-500-300, b.Eligible)
	require.EqualValues(t, 38_300, b.Total)
	require.Zero(t, b.RoundOff)

	// Credits leave at least ₹1 to pay (₹390 eligible with the ₹1 line discount).
	_, perr = price(lines, 0, 0, 390, "body")
	require.NotNil(t, perr)
	require.Contains(t, perr.Fields, "body.credits")
	b, perr = price(lines, 0, 0, 389, "body")
	require.Nil(t, perr)
	require.EqualValues(t, 100, b.Total)

	// A manual discount above the line is refused on its field, never clamped.
	over := []lineInput{{ClientID: id(), Name: "Salt", Unit: 2_500, Qty: 1, Manual: 2_501, path: "body.lines[0]"}}
	_, perr = price(over, 0, 0, 0, "body")
	require.NotNil(t, perr)
	require.Contains(t, perr.Fields, "body.lines[0].manualDiscountPaise")

	// A duplicate line client id is refused.
	dup := id()
	_, perr = price([]lineInput{{ClientID: dup, Name: "A", Unit: 100, Qty: 1}, {ClientID: dup, Name: "B", Unit: 100, Qty: 1}}, 0, 0, 0, "body")
	require.NotNil(t, perr)
}

// RFC 6238 Appendix B's SHA-1 vectors, truncated to 4 digits (the same
// dynamic truncation, mod 10^4).
func TestTOTPVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, c := range []struct {
		unix int64
		want string // last 4 digits of the RFC's 8-digit code
	}{
		{59, "7082"}, {1_111_111_109, "1804"}, {1_111_111_111, "0471"},
		{1_234_567_890, "5924"}, {2_000_000_000, "9037"}, {20_000_000_000, "3130"},
	} {
		// The RFC's vectors use 30 s steps; ours are 60 s, so compute the
		// RFC's step and compare the truncation directly.
		require.Equal(t, c.want, totp(secret, c.unix/30), "T=%d", c.unix)
	}
}

// ±1 step (60 s) of drift passes, ±2 fails.
func TestTOTPDrift(t *testing.T) {
	secret := bytes.Repeat([]byte{7}, totpSecretLen)
	at := time.Date(2026, 10, 24, 5, 0, 30, 0, time.UTC)
	code := totp(secret, totpStep(at))
	require.Equal(t, totpStep(at), codeAt(secret, code, at))
	require.Equal(t, totpStep(at), codeAt(secret, code, at.Add(60*time.Second)), "the server one step ahead")
	require.Equal(t, totpStep(at), codeAt(secret, code, at.Add(-60*time.Second)), "the server one step behind")
	for _, d := range []time.Duration{2 * time.Minute, -2 * time.Minute} {
		if totp(secret, totpStep(at.Add(d))+1) == code || totp(secret, totpStep(at.Add(d))-1) == code || totp(secret, totpStep(at.Add(d))) == code {
			continue // a 1-in-10,000 collision of a neighbouring step
		}
		require.Zero(t, codeAt(secret, code, at.Add(d)), "two steps off fails (%v)", d)
	}
	require.Zero(t, codeAt(secret, "abcd", at))
	require.Regexp(t, `^\d{4}$`, code)
}

// A code that is also right for the step before is bound to its own step, so
// the sale rung a minute earlier with that step's code still links (the
// TestPOSOfflineOfferMatchesEngine flake: 19 sales a minute apart).
func TestTOTPExactStepWins(t *testing.T) {
	at := time.Date(2026, 10, 24, 5, 0, 30, 0, time.UTC)
	step := totpStep(at)
	var secret []byte
	for i := 0; secret == nil; i++ {
		s := bytes.Repeat([]byte{byte(i), byte(i >> 8), byte(i >> 16), 1}, totpSecretLen/4)
		if totp(s, step) == totp(s, step-1) {
			secret = s
		}
	}
	code := totp(secret, step)
	require.Equal(t, step, codeAt(secret, code, at), "its own step, not the one before")
	require.Equal(t, step-1, codeAt(secret, code, at.Add(-60*time.Second)), "the earlier sale keeps its own step too")
}

// A token verifies only against the row and key it was made for.
func TestClaimTokenTamper(t *testing.T) {
	s := &Service{clock: clock.NewFake(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)), billKey: bytes.Repeat([]byte{1}, 32)}
	id, store := ids.New(s.now()), ids.New(s.now())
	c := posdb.BillClaim{
		ID: ids.UUID(id), StoreID: ids.UUID(store), AmountPaise: 12_500,
		TokenNonce: bytes.Repeat([]byte{9}, tokenNonceLen), ExpiresAt: s.now().Add(claimTTL),
	}
	tok := s.claimToken(c)
	require.Len(t, tok, 64)
	gotID, nonce, tag, err := tokenClaim(tok)
	require.NoError(t, err)
	require.Equal(t, id, gotID)
	require.True(t, s.verify(c, nonce, tag))

	// A changed amount, store or expiry no longer verifies.
	for _, mut := range []func(*posdb.BillClaim){
		func(c *posdb.BillClaim) { c.AmountPaise++ },
		func(c *posdb.BillClaim) { c.StoreID = ids.UUID(ids.New(s.now())) },
		func(c *posdb.BillClaim) { c.ExpiresAt = c.ExpiresAt.Add(time.Second) },
	} {
		cc := c
		mut(&cc)
		require.False(t, s.verify(cc, nonce, tag))
	}
	// A flipped tag bit, or another key, does not verify.
	raw, _ := base64.RawURLEncoding.DecodeString(tok)
	raw[len(raw)-1] ^= 1
	_, nonce2, tag2, err := tokenClaim(base64.RawURLEncoding.EncodeToString(raw))
	require.NoError(t, err)
	require.False(t, s.verify(c, nonce2, tag2))
	other := &Service{clock: s.clock, billKey: bytes.Repeat([]byte{2}, 32)}
	require.False(t, other.verify(c, nonce, tag))

	// Junk is refused before any lookup.
	for _, junk := range []string{"", "abc", tok + "A", tok[:63]} {
		_, _, _, err := tokenClaim(junk)
		require.Error(t, err)
	}
}

func TestPurchasesCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 24, 5, 0, 0, 123_456_789, time.UTC)
	id := ids.New(at)
	gotAt, gotID, err := decodeCursor(encodeCursor(at, id))
	require.NoError(t, err)
	require.True(t, at.Equal(gotAt))
	require.Equal(t, id, gotID)
	_, _, err = decodeCursor("not-a-cursor")
	require.Error(t, err)
}

// Every spelling of one Indian mobile is one phone, and so one lock key
// (D-131); anything else is not a phone.
// Every spelling of one number counts and locks as one phone (D-131).
func TestGuardPhoneVariantsShareKey(t *testing.T) {
	h, err := pii.NewHasher(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	s := &Service{hasher: h}
	at := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	store, cashier := ids.New(at), ids.New(at.Add(time.Millisecond))
	want := s.guard(store, cashier, "+919876543210").phone
	require.NotNil(t, want)
	for _, v := range []string{"+91 98765 43210", "+91-98765-43210", "(+91) 98765.43210", "919876543210", "09876543210", "9876543210"} {
		require.Equal(t, want, s.guard(store, cashier, v).phone, v)
	}
	require.NotEqual(t, want, s.guard(store, cashier, "+919876543211").phone)
	require.Nil(t, s.guard(store, cashier, "+449876543210").phone)
}

// A sync locks the cashier up front whenever it carries a counter code, even
// when no phone parses, so no entry takes it after stock rows (D-131).
func TestSyncCodeKeys(t *testing.T) {
	h, err := pii.NewHasher(bytes.Repeat([]byte{7}, 32))
	require.NoError(t, err)
	s := &Service{hasher: h}
	at := time.Date(2026, 10, 23, 6, 0, 0, 0, time.UTC)
	store, cashier := ids.New(at), ids.New(at.Add(time.Millisecond))
	code := "1234"
	entry := func(method syncapi.SyncSaleLinkMethod, phone string, code *string) syncapi.SyncEntry {
		return syncapi.SyncEntry{Kind: syncapi.Sale, Sale: &syncapi.SyncSale{Link: &syncapi.SyncSaleLink{Method: method, Phone: phone, Code: code}}}
	}
	cashierKey := "pos.code.cashier:" + cashier.String()

	require.Nil(t, s.syncCodeKeys(store, cashier, []syncapi.SyncEntry{
		{Kind: syncapi.Restock}, entry(syncapi.PhoneUnverified, "+919876543210", nil), entry(syncapi.CounterCode, "+919876543210", nil),
	}), "no counter code, no lock")
	require.Equal(t, []string{cashierKey}, s.syncCodeKeys(store, cashier, []syncapi.SyncEntry{
		{Kind: syncapi.Restock}, entry(syncapi.CounterCode, "+449876543210", &code),
	}), "an unparseable phone still locks the cashier")

	keys := s.syncCodeKeys(store, cashier, []syncapi.SyncEntry{
		entry(syncapi.CounterCode, "+919876543211", &code), entry(syncapi.CounterCode, "bad", &code),
		entry(syncapi.CounterCode, "+919876543210", &code), entry(syncapi.CounterCode, "+91 98765 43211", &code),
	})
	require.Len(t, keys, 3, "cashier, then two phones once each")
	require.Equal(t, cashierKey, keys[0])
	require.Less(t, keys[1], keys[2], "phones sorted")
}

func TestCanonPhone(t *testing.T) {
	for _, v := range []string{"+919876543210", "+91 98765 43210", "+91-98765-43210", "(+91) 98765.43210",
		"919876543210", "09876543210", "9876543210"} {
		got, ok := canonPhone(v)
		require.True(t, ok, v)
		require.Equal(t, "+919876543210", got, v)
	}
	for _, v := range []string{"", "+9198765", "+915876543210", "+449876543210", "98765432100"} {
		_, ok := canonPhone(v)
		require.False(t, ok, v)
	}
}
