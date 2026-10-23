package seed

import (
	"context"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/business"
	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/pii"
	"github.com/todayz/backend/internal/testkit"
)

// The schema's checks (ck_stores_slug, ck_stores_short_code,
// ck_businesses_referral_code), repeated so a bad fixed value fails here
// with its name rather than as a constraint error.
var (
	slugRE = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	codeRE = regexp.MustCompile(`^[A-HJKMNP-Z2-9]{6}$`)
)

func TestBusinessSeedFixedValues(t *testing.T) {
	seen := map[string]string{}
	unique := func(kind, v, who string) {
		t.Helper()
		prev, dup := seen[kind+":"+v]
		require.False(t, dup, "%s reuses the %s of %s", who, kind, prev)
		seen[kind+":"+v] = who
	}
	for _, u := range identityUsers {
		unique("id", mustID(u.ID).String(), u.Key)
		unique("id", phoneIdentityID(u.ID).String(), u.Key)
		if u.Referral != "" {
			unique("code", u.Referral, u.Key)
		}
	}
	for _, b := range seedBusinesses {
		unique("id", mustID(b.ID).String(), b.Key)
		unique("business", b.Key, b.Key)
		require.Regexp(t, codeRE, b.Referral, b.Key)
		unique("code", b.Referral, b.Key) // also unlike every customer referral code
		require.Equal(t, mustID(b.ID), businessSeedID(b.Key))
		if b.KYC != nil {
			require.True(t, business.ValidPAN(b.KYC.PAN), b.Key)
		}
	}
	for _, s := range seedStores {
		unique("id", mustID(s.ID).String(), s.Key)
		unique("store", s.Key, s.Key)
		require.Regexp(t, slugRE, s.Slug, s.Key)
		require.LessOrEqual(t, len(s.Slug), 64, s.Key)
		unique("slug", s.Slug, s.Key)
		require.Regexp(t, codeRE, s.ShortCode, s.Key)
		unique("short", s.ShortCode, s.Key)
		require.Equal(t, mustID(s.ID), storeSeedID(s.Key))
		businessSeedID(s.Business) // panics on an unknown business
		_, err := s.hours()
		require.NoError(t, err, s.Key)
		require.Equal(t, s.Status == "live" || s.Status == "paused" || s.Status == "suspended", s.LiveFor > 0,
			"%s: a store that has been live has a live_at", s.Key)
	}
	for _, st := range seedStaff {
		unique("id", mustID(st.ID).String(), st.ID)
		identityUserID(st.User)
		for _, k := range st.Stores {
			storeSeedID(k)
		}
	}
	require.Panics(t, func() { businessSeedID("nobody") })
	require.Panics(t, func() { storeSeedID("XX") })
}

func TestBusinessSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	keys := testKeys(t)
	d := Deps{DB: db.New(tdb.Pool), Keys: keys}
	require.NoError(t, Run(ctx, config.EnvTest, Dev, d))
	require.NoError(t, Run(ctx, config.EnvTest, Test, d), "a second run is a no-op")

	count := func(sql string, args ...any) int {
		t.Helper()
		var n int
		require.NoError(t, tdb.Pool.QueryRow(ctx, sql, args...).Scan(&n))
		return n
	}
	require.Equal(t, len(seedBusinesses), count(`SELECT count(*) FROM businesses`))
	require.Equal(t, len(seedStores), count(`SELECT count(*) FROM stores`))
	require.Equal(t, len(seedStaff), count(`SELECT count(*) FROM staff`))
	require.Equal(t, 3, count(`SELECT count(DISTINCT city) FROM stores`), "Bengaluru, Hyderabad and Chennai")
	for _, status := range []string{"draft", "submitted", "needs_info", "verified", "suspended"} {
		require.Positive(t, count(`SELECT count(*) FROM businesses WHERE status = $1`, status), status)
	}
	for _, status := range []string{"draft", "pending_review", "live", "paused", "suspended"} {
		require.Positive(t, count(`SELECT count(*) FROM stores WHERE status = $1`, status), status)
	}

	// The book's distances from the QA point (§6): which live stores of
	// verified businesses each radius shows.
	shown := func(km float64) []string {
		t.Helper()
		rows, err := tdb.Pool.Query(ctx, `SELECT s.id FROM stores s JOIN businesses b ON b.id = s.business_id
			WHERE s.status = 'live' AND b.status = 'verified'
			  AND ST_DWithin(s.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $3)`,
			QAPoint.Lng, QAPoint.Lat, km*1000)
		require.NoError(t, err)
		defer rows.Close()
		var keys []string
		for rows.Next() {
			var id [16]byte
			require.NoError(t, rows.Scan(&id))
			for _, s := range seedStores {
				if mustID(s.ID) == ids.ID(id) {
					keys = append(keys, s.Key)
				}
			}
		}
		require.NoError(t, rows.Err())
		sort.Strings(keys)
		return keys
	}
	require.Equal(t, []string{"NH", "SL"}, shown(1))
	require.Equal(t, []string{"AP", "NH", "SL", "SL2"}, shown(2))
	require.Equal(t, []string{"AM", "AP", "KF", "NH", "SL", "SL2"}, shown(5))
	require.Equal(t, shown(5), shown(20))

	km := func(a, b string) float64 {
		t.Helper()
		var m float64
		require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT ST_Distance(x.location, y.location) FROM stores x, stores y WHERE x.id = $1 AND y.id = $2`,
			ids.UUID(storeSeedID(a)), ids.UUID(storeSeedID(b))).Scan(&m))
		return m / 1000
	}
	require.InDelta(t, 2.4, km("SL", "SL2"), 0.05, "SL and SL2 on opposite sides of the QA point")
	require.Less(t, km("AP", "SL2"), 1.0)
	require.Less(t, km("AP", "SL"), 2.0)
	for i := 1; i <= chennaiNeighbours; i++ {
		k := "CH" + string(rune('0'+i/10)) + string(rune('0'+i%10))
		require.Less(t, km("MM", k), 2.0, k)
	}
	require.Equal(t, 1, count(`SELECT count(*) FROM stores WHERE status = 'live' AND city = 'Chennai' AND id = $1`, ids.UUID(storeSeedID("MM"))))
	require.Equal(t, chennaiNeighbours, count(`SELECT count(DISTINCT business_id) FROM stores WHERE city = 'Chennai' AND name LIKE 'Chennai Store %'`))

	// Hours read back as the service reads them: SL is open at the seed clock
	// (Monday 10:00 IST); a draft store has none.
	var raw []byte
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT hours FROM stores WHERE id = $1`, ids.UUID(storeSeedID("SL"))).Scan(&raw))
	require.True(t, business.OpenAt(raw, nil, Epoch))
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT hours FROM stores WHERE id = $1`, ids.UUID(storeSeedID("DS"))).Scan(&raw))
	require.False(t, business.OpenAt(raw, nil, Epoch), "a draft store has no hours yet")

	var radius, cashback int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT delivery_radius_m, cashback_bp FROM stores WHERE id = $1`, ids.UUID(storeSeedID("KF"))).Scan(&radius, &cashback))
	require.Equal(t, 5000, radius)
	require.Equal(t, 200, cashback)

	// Sealed values open with the service's field names, bound to their row.
	seal, err := pii.NewSealer(keys.PIIEncKey.Bytes())
	require.NoError(t, err)
	var phone []byte
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT phone_enc FROM stores WHERE id = $1`, ids.UUID(storeSeedID("SL"))).Scan(&phone))
	plain, err := seal.Open(business.SealField(business.FieldPhone, storeSeedID("SL")), phone)
	require.NoError(t, err)
	require.Equal(t, identityPhone("staff.owner"), string(plain))
	var pan []byte
	var panLast4, status string
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT pan_enc, pan_last4, status FROM business_kyc WHERE business_id = $1`,
		ids.UUID(businessSeedID("lakshmi"))).Scan(&pan, &panLast4, &status))
	plain, err = seal.Open(business.SealField(business.FieldPAN, businessSeedID("lakshmi")), pan)
	require.NoError(t, err)
	// Bound to that business: the bare field name or another business's id
	// does not open it.
	_, err = seal.Open(business.FieldPAN, pan)
	require.ErrorIs(t, err, pii.ErrOpen)
	other := seedBusinesses[len(seedBusinesses)-1].Key
	require.NotEqual(t, "lakshmi", other)
	_, err = seal.Open(business.SealField(business.FieldPAN, businessSeedID(other)), pan)
	require.ErrorIs(t, err, pii.ErrOpen)
	require.Equal(t, "verified", status)
	require.Equal(t, string(plain[len(plain)-4:]), panLast4)

	// The staff resolver sees the identity seed's users as Lakshmi Traders'
	// owner, manager and a cashier scoped to Sri Lakshmi.
	r := business.NewStaffResolver(db.New(tdb.Pool))
	for user, want := range map[string]string{"staff.owner": "owner", "staff.manager": "manager", "staff.cashier": "cashier"} {
		m, ok, err := r.Membership(ctx, identityUserID(user), businessSeedID("lakshmi"))
		require.NoError(t, err, user)
		require.True(t, ok, user)
		require.Equal(t, want, string(m.Role), user)
		if want == "cashier" {
			require.Equal(t, []ids.ID{storeSeedID("SL")}, m.StoreIDs)
		} else {
			require.Nil(t, m.StoreIDs, user)
		}
	}
}

func TestBusinessSeedWithoutKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("needs the database")
	}
	tdb := testkit.NewDB(t)
	ctx := context.Background()
	require.NoError(t, Run(ctx, config.EnvTest, Dev, Deps{DB: db.New(tdb.Pool)}))
	var businesses, stores, staff, kyc, phones int
	require.NoError(t, tdb.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM businesses), (SELECT count(*) FROM stores),
		(SELECT count(*) FROM staff), (SELECT count(*) FROM business_kyc), (SELECT count(*) FROM stores WHERE phone_enc IS NOT NULL)`).
		Scan(&businesses, &stores, &staff, &kyc, &phones))
	require.Equal(t, len(seedBusinesses), businesses)
	require.Equal(t, len(seedStores), stores)
	require.Zero(t, staff+kyc+phones, "nothing personal without the keys")
}
