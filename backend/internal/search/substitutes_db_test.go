package search

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

// subs reads the substitutes of a shop's item at the QA point.
func (f *qa) subs(slug, item string, mod ...func(*discoverapi.ListDiscoverSubstitutesParams)) (discoverapi.DiscoverSubstitutes, error) {
	f.t.Helper()
	p := discoverapi.ListDiscoverSubstitutesParams{ItemId: ulidOf(f.t, item), Lat: qaLat, Lng: qaLng}
	for _, m := range mod {
		m(&p)
	}
	return f.svc.Substitutes(f.ctx, slug, p)
}

// idOf is a fixture row's id.
func (f *qa) idOf(sql string, args ...any) string {
	f.t.Helper()
	var id string
	require.NoError(f.t, f.tdb.OwnerPool.QueryRow(f.ctx, sql, args...).Scan(&id))
	return id
}

// AI-C3 (W3-12, D-233): rule-based substitutes for an out-of-stock item.
func TestSubstitutes(t *testing.T) {
	f := newQA(t)
	nandini := f.idOf(`SELECT id::text FROM brands WHERE slug = 'nandini'`)
	instore := f.idOf(`SELECT id::text FROM stores WHERE slug = 'instore'`)
	gone := f.idOf(`SELECT id::text FROM store_items WHERE store_id = $1 AND product_id = $2`, instore, f.butter100)

	// The out-of-stock Amul Butter 100 g at "instore" and what else the shop has.
	f.item(instore, f.product("Amul Butter", &f.amul, catDairy, 120, "g"), 6800, 3, time.Hour, nil)     // same brand, 20 g off
	f.item(instore, f.product("Amul Butter", &f.amul, catDairy, 90, "g"), 5200, 3, time.Hour, nil)      // same brand, 10 g off
	f.item(instore, f.product("Nandini Butter", &nandini, catDairy, 100, "g"), 5400, 3, time.Hour, nil) // other brand, same pack
	f.item(instore, f.product("Amul Cheese", &f.amul, catDairy, 0.1, "kg"), 9000, 3, time.Hour, nil)    // 100 g in kg
	f.item(instore, f.product("Amul Butter", &f.amul, catDairy, 200, "g"), 11000, 3, time.Hour, nil)    // pack too far
	f.item(instore, f.product("Amul Butter", &f.amul, catDairy, 110, "g"), 6000, 0, time.Hour, nil)     // out of stock
	f.item(instore, f.product("Amul Taaza", &f.amul, catDairy, 100, "ml"), 2000, 3, time.Hour, nil)     // another unit
	f.item(instore, f.product("Rice 100 g", nil, catStaples, 100, "g"), 2000, 3, time.Hour, nil)        // another category

	r, err := f.subs("instore", gone)
	require.NoError(t, err)
	names := make([]string, 0, len(r.SameShop))
	for _, it := range r.SameShop {
		names = append(names, it.Name)
		assert.True(t, it.InStock)
	}
	require.Len(t, r.SameShop, 4)
	// Same brand first, then the closest pack in base units (0.1 kg is 100 g), then price.
	assert.Equal(t, []string{"Amul Cheese", "Amul Butter", "Amul Butter", "Nandini Butter"}, names)
	assert.InDelta(t, 90, r.SameShop[1].PackSize, 0.001)
	assert.InDelta(t, 120, r.SameShop[2].PackSize, 0.001)

	// The same product in stock at other live shops of verified businesses in the radius,
	// nearest first: "near" (300 m); hidden, unverified and out-of-stock listings never.
	require.Len(t, r.Nearby, 1)
	assert.Equal(t, "near", r.Nearby[0].Store.StoreSlug)
	require.Len(t, r.Nearby[0].Items, 1)
	assert.Equal(t, ulidOf(t, f.butter100), r.Nearby[0].Items[0].ProductId)
	r, err = f.subs("instore", gone, func(p *discoverapi.ListDiscoverSubstitutesParams) {
		v := discoverapi.ListDiscoverSubstitutesParamsRadiusN5000
		p.Radius = &v
	})
	require.NoError(t, err)
	slugs := []string{}
	for _, s := range r.Nearby {
		slugs = append(slugs, s.Store.StoreSlug)
	}
	assert.Equal(t, []string{"near", "outside"}, slugs)

	// The shop's own listing is never a nearby result.
	r, err = f.subs("near", f.nearButter)
	require.NoError(t, err)
	for _, s := range r.Nearby {
		assert.NotEqual(t, "near", s.Store.StoreSlug)
	}

	// Nothing similar: both lists empty, never null.
	r, err = f.subs("far", f.idOf(`SELECT si.id::text FROM store_items si JOIN stores s ON s.id = si.store_id WHERE s.slug = 'far' AND si.product_id = $1`, f.atta))
	require.NoError(t, err)
	assert.NotNil(t, r.SameShop)
	assert.Equal(t, []string{"near"}, func() []string {
		out := []string{}
		for _, s := range r.Nearby {
			out = append(out, s.Store.StoreSlug)
		}
		return out
	}())

	// 404: a shop that is not live, an item the shop does not list, an id that is no id.
	_, err = f.subs("hidden-paused", gone)
	assert.Equal(t, 404, problem.Status(err))
	_, err = f.subs("instore", f.nearButter)
	assert.Equal(t, 404, problem.Status(err))
	_, err = f.svc.Substitutes(f.ctx, "instore", discoverapi.ListDiscoverSubstitutesParams{ItemId: "not-an-id", Lat: qaLat, Lng: qaLng})
	assert.Equal(t, 404, problem.Status(err))

	// Read-only: no search is logged.
	assert.Empty(t, f.logRows())
}
