package banners

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	bannersapi "github.com/todayz/backend/gen/api/banners"
	"github.com/todayz/backend/internal/platform/ids"
)

func tmpl() *bannersapi.BannerTemplate { return &bannersapi.BannerTemplate{Tint: bannersapi.Green} }

func keysSent(keys ...string) func(string) bool {
	return func(k string) bool {
		for _, s := range keys {
			if s == k {
				return true
			}
		}
		return false
	}
}

func (w *world) create(store ids.ID, mod ...func(*bannersapi.BannerCreate)) (bannersapi.Banner, error) {
	b := bannersapi.BannerCreate{Headline: "Diwali sale", TargetType: "store", Template: tmpl()}
	for _, m := range mod {
		m(&b)
	}
	out, _, err := w.svc.createBanner(w.ctx, store.String(), &b)
	return out, err
}

// SC-BN: a template banner goes live at once; a scheduled one waits for its time.
func TestCreateTemplateBannerLiveAndScheduled(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	live, err := w.create(store)
	require.NoError(t, err)
	require.Equal(t, bannersapi.BannerStatusLive, live.Status)
	require.Equal(t, bannersapi.AutoOk, live.Moderation)

	later := testNow.Add(48 * time.Hour)
	sched, err := w.create(store, func(b *bannersapi.BannerCreate) { b.StartsAt = &later })
	require.NoError(t, err)
	require.Equal(t, bannersapi.BannerStatusScheduled, sched.Status)

	got, err := w.svc.LiveBanners(w.ctx, []ids.ID{store}, testNow)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, live.Id, got[0].ID.String())
	require.Equal(t, "green", got[0].Template["tint"])

	// By the injected clock the scheduled banner is live later, with no job having run.
	got, err = w.svc.LiveBanners(w.ctx, []ids.ID{store}, later.Add(time.Minute))
	require.NoError(t, err)
	require.Len(t, got, 2)
}

// SC-BN: an uploaded image is held for review and never shows until approved.
func TestUploadedImageIsHeld(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	key := "banners/x.jpg"
	b, err := w.create(store, func(b *bannersapi.BannerCreate) { b.Template = nil; b.ImageKey = &key })
	require.NoError(t, err)
	require.Equal(t, bannersapi.BannerStatusPendingReview, b.Status)
	require.Equal(t, bannersapi.NeedsReview, b.Moderation)
	got, err := w.svc.LiveBanners(w.ctx, []ids.ID{store}, testNow.Add(time.Hour))
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestCreateValidation(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	key := "banners/x.jpg"
	for name, mod := range map[string]func(*bannersapi.BannerCreate){
		"both image and template": func(b *bannersapi.BannerCreate) { b.ImageKey = &key },
		"neither":                 func(b *bannersapi.BannerCreate) { b.Template = nil },
		"blank headline":          func(b *bannersapi.BannerCreate) { b.Headline = "  " },
		"offer needs a target id": func(b *bannersapi.BannerCreate) { b.TargetType = "offer" },
		"items needs items":       func(b *bannersapi.BannerCreate) { b.TargetType = "items" },
		"ends before it starts": func(b *bannersapi.BannerCreate) {
			e := testNow.Add(-time.Hour)
			b.EndsAt = &e
		},
	} {
		_, err := w.create(store, mod)
		require.Error(t, err, name)
	}
}

// SC-BN: store_banners_max counts live, scheduled and held banners, and the
// count is serialised per store, so concurrent creates never pass the limit.
func TestSlotLimit(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	var made []bannersapi.Banner
	for range 3 {
		b, err := w.create(store)
		require.NoError(t, err)
		made = append(made, b)
	}
	_, err := w.create(store)
	require.Error(t, err, "the fourth banner is over the limit")

	// Pausing keeps the slot; ending frees it.
	paused := bannersapi.BannerUpdateStatus("paused")
	v1 := `"v1"`
	_, _, err = w.svc.updateBanner(w.ctx, store.String(), made[0].Id, &v1, &bannersapi.BannerUpdate{Status: &paused}, keysSent("status"))
	require.NoError(t, err)
	_, err = w.create(store)
	require.NoError(t, err, "a paused banner frees its slot")
}

func TestSlotLimitConcurrent(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	var ok, over atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := w.create(store); err == nil {
				ok.Add(1)
			} else {
				over.Add(1)
			}
		}()
	}
	wg.Wait()
	require.EqualValues(t, 3, ok.Load())
	require.EqualValues(t, 9, over.Load())
}

// SC-BN: the tap records the last tap per customer and shop; attribution is
// last click within 24 h (D-77), read from banner_taps by W3-07.
func TestTap(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	b, err := w.create(store)
	require.NoError(t, err)
	cust := w.customer()
	require.NoError(t, w.svc.tap(w.as(cust), b.Id))
	w.clk.Advance(time.Hour)
	require.NoError(t, w.svc.tap(w.as(cust), b.Id))
	var n int
	var at time.Time
	require.NoError(t, w.tdb.OwnerPool.QueryRow(w.ctx, `SELECT count(*), max(tapped_at) FROM banner_taps WHERE banner_id = $1`, ids.UUID(mustID(t, b.Id))).Scan(&n, &at))
	require.Equal(t, 1, n, "one row per customer and banner")
	require.True(t, at.Equal(testNow.Add(time.Hour)))
	require.Error(t, w.svc.tap(w.ctx, ids.New(testNow).String()), "a missing banner is a 404")
}

func mustID(t *testing.T, s string) ids.ID {
	id, err := ids.Parse(s)
	require.NoError(t, err)
	return id
}

// SC-BN: takedown by moderation is final, frees the slot and hides the banner.
func TestRemove(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	b, err := w.create(store)
	require.NoError(t, err)
	admin := w.customer()
	require.Error(t, w.svc.Remove(w.ctx, mustID(t, b.Id), admin, " "), "a reason is required")
	require.NoError(t, w.svc.Remove(w.ctx, mustID(t, b.Id), admin, "misleading claim"))
	got, err := w.svc.LiveBanners(w.ctx, []ids.ID{store}, testNow)
	require.NoError(t, err)
	require.Empty(t, got)
	after, _, err := w.svc.getBanner(w.ctx, store.String(), b.Id)
	require.NoError(t, err)
	require.Equal(t, bannersapi.BannerStatusRemoved, after.Status)
	require.NotNil(t, after.RemoveReason)
	live := bannersapi.BannerUpdateStatus("live")
	v := `"v2"`
	_, _, err = w.svc.updateBanner(w.ctx, store.String(), b.Id, &v, &bannersapi.BannerUpdate{Status: &live}, keysSent("status"))
	require.Error(t, err, "a removed banner stays removed")
}

// SC-BN: the tidy job moves labels by date and is a no-op the second time.
func TestTidy(t *testing.T) {
	w := newWorld(t)
	store := w.store("a")
	later := testNow.Add(time.Hour)
	end := testNow.Add(2 * time.Hour)
	_, err := w.create(store, func(b *bannersapi.BannerCreate) { b.StartsAt = &later; b.EndsAt = &end })
	require.NoError(t, err)
	_, err = Tidy(w.ctx, w.svc.db, w.clk)
	require.NoError(t, err)

	w.clk.Advance(90 * time.Minute)
	res, err := Tidy(w.ctx, w.svc.db, w.clk)
	require.NoError(t, err)
	require.Equal(t, TidyResult{Started: 1}, res)
	w.clk.Advance(time.Hour)
	res, err = Tidy(w.ctx, w.svc.db, w.clk)
	require.NoError(t, err)
	require.Equal(t, TidyResult{Ended: 1}, res)
	res, err = Tidy(w.ctx, w.svc.db, w.clk)
	require.NoError(t, err)
	require.Equal(t, TidyResult{}, res)
}
