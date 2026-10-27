package imageimport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/clock"
)

// The fast search service returns brands as an array and a hits list, unlike
// the legacy /cgi/search.pl API. One product must still come out whole.
func TestSearchAPIMapsHits(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/search", r.URL.Path)
		require.Equal(t, "amul butter", r.URL.Query().Get("q"))
		require.Equal(t, "5", r.URL.Query().Get("page_size"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[
			{"code":"8901262010153","product_name":"Amul Butter","brands":["Amul","GCMMF"],
			 "image_front_url":"https://images.openfoodfacts.org/images/products/890/126/201/0153/front_en.3.400.jpg"},
			{"code":"8901262010999","product_name":"Amul Cheese","brands":["Amul"],
			 "image_url":"https://images.openfoodfacts.org/images/products/890/126/201/0999/front_en.1.400.jpg"}
		]}`))
	}))
	t.Cleanup(srv.Close)

	im := &Importer{
		d: Deps{
			Client: srv.Client(),
			Clock:  clock.NewFake(t0),
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
		o:    Options{},
		last: map[string]time.Time{},
	}
	got, err := im.search(context.Background(), Source{Code: "off", BaseURL: "https://world.openfoodfacts.org", SearchURL: srv.URL + "/search"}, "amul butter")
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "8901262010153", got[0].Code)
	require.Equal(t, "Amul, GCMMF", got[0].Brands)
	require.Equal(t, "https://images.openfoodfacts.org/images/products/890/126/201/0153/front_en.3.400.jpg", got[0].image())
	require.Equal(t, "8901262010999", got[1].Code)
}

// The legacy endpoint still answers the old shape (tests and the sister
// databases), so a source without SearchURL must keep working.
func TestSearchLegacyShape(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/cgi/search.pl", r.URL.Path)
		require.Equal(t, "amul butter", r.URL.Query().Get("search_terms"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"products":[{"code":"8901262010153","product_name":"Amul Butter","brands":"Amul","image_front_url":"https://images.openfoodfacts.org/a.jpg"}]}`))
	}))
	t.Cleanup(srv.Close)

	im := &Importer{
		d: Deps{
			Client: srv.Client(),
			Clock:  clock.NewFake(t0),
			Sleep:  func(context.Context, time.Duration) error { return nil },
		},
		o:    Options{},
		last: map[string]time.Time{},
	}
	got, err := im.search(context.Background(), Source{Code: "obf", BaseURL: srv.URL}, "amul butter")
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "Amul", got[0].Brands)
}
