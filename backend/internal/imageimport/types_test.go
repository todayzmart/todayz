package imageimport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/storage"
)

func TestKindWords(t *testing.T) {
	for _, c := range []struct {
		key  string
		want []string
		ok   bool
	}{
		{"toor dal", []string{"toor", "dal"}, true},
		{"garbage bags medium", []string{"garbage", "bags"}, true},
		{"diaper pants l", []string{"diaper", "pants"}, true},
		{"matchbox pack of", []string{"matchbox"}, true},
		{"500 g", nil, false},
		{"qa pack ₹120", []string{"qa"}, false},
	} {
		got, ok := kindWords(c.key)
		require.Equal(t, c.want, got, c.key)
		require.Equal(t, c.ok, ok, c.key)
	}
}

func TestQueries(t *testing.T) {
	search := func(qs []typeQuery) []string {
		out := []string{}
		for _, q := range qs {
			out = append(out, q.search)
		}
		return out
	}
	// A single word is searched only with its category's hint.
	require.Equal(t, []string{"intitle:conditioner hair"}, search(queries("conditioner", "hair-care", "beauty")))
	// An alias comes first, then the kind's own words, each with the hint and
	// then alone; there is no plain search.
	staples := categoryHints["staples"]
	require.Equal(t, []string{
		"intitle:pigeon intitle:peas " + staples, "intitle:pigeon intitle:peas",
		"intitle:toor intitle:dal " + staples, "intitle:toor intitle:dal",
	}, search(queries("toor dal", "staples", "grocery")))
	// A longer kind also tries its last two words; pack sizes are dropped.
	require.Equal(t, []string{
		"intitle:potato intitle:chips " + categoryHints["snacks"], "intitle:potato intitle:chips",
		"intitle:classic intitle:salted intitle:chips " + categoryHints["snacks"],
		"intitle:salted intitle:chips " + categoryHints["snacks"],
		"intitle:classic intitle:salted intitle:chips",
	}, search(queries("classic salted chips", "snacks", "grocery")))
	// A category without its own hint takes its parent's; nothing for a size.
	require.Equal(t, []string{"intitle:jam food"}, search(queries("jam", "spreads", "grocery")))
	require.Empty(t, queries("750 ml", "beverages", "grocery"))
}

func TestOnTopic(t *testing.T) {
	q := func(key, cat string) (typeQuery, []string) {
		qs := queries(key, cat, "")
		own, _ := kindWords(key)
		for _, x := range qs {
			own = append(own, x.words...)
		}
		return qs[len(qs)-1], own
	}
	paneer, own := q("paneer", "dairy-eggs")
	require.False(t, onTopic("Common chimpanzee (Pan troglodytes).jpg", paneer, own), "the search's rewrite, not paneer")
	require.True(t, onTopic("Malai paneer cubes.jpg", paneer, own))

	dal, own := q("toor dal", "staples")
	require.True(t, onTopic("Toor dal.jpg", dal, own))
	require.False(t, onTopic("Toor dal tadka.JPG", dal, own), "a dish, not the dal")
	require.False(t, onTopic("Toor dal field in Karnataka.jpg", dal, own))

	bm, own := q("buttermilk", "dairy-eggs")
	require.False(t, onTopic("Ithaca - Buttermilk Falls.jpg", bm, own))
	require.True(t, onTopic("Buttermilk-(right)-and-Milk-(left).jpg", bm, own))

	// Plurals and longer forms match; a word the kind uses is not off topic.
	biscuits, own := q("cream biscuits", "snacks")
	require.True(t, onTopic("Pran strawberry flavoured cream biscuit.jpg", biscuits, own))
	cake, own := q("fruit cake", "bakery")
	require.True(t, onTopic("Fruit Cake MET 36.jpg", typeQuery{words: []string{"fruit", "cake"}}, own) == false, "a museum piece")
	require.True(t, onTopic("Rich fruit cake.jpg", cake, own))
}

func TestSameShot(t *testing.T) {
	require.Equal(t, sameShot("Brown Bread from a Colonial Oven-Baughan (1912) - 1.jpg"), sameShot("Brown Bread from a Colonial Oven-Baughan (1912) - 2.jpg"))
	require.Equal(t, sameShot("Kidney beans (1).jpg"), sameShot("Kidney beans (2).jpg"))
	require.NotEqual(t, sameShot("Kidney beans (1).jpg"), sameShot("Red kidney beans.jpg"))
	require.Equal(t, "toothbrush", sameShot("Toothbrush (2021) 02.jpg"))
}

// fakeCommons answers the search API from a table of titles per search and
// serves every file as the same JPEG.
type fakeCommons struct {
	mu       sync.Mutex
	api      *httptest.Server
	files    *httptest.Server
	results  map[string][]string
	searches []string
}

func newFakeCommons(t *testing.T, results map[string][]string) *fakeCommons {
	t.Helper()
	c := &fakeCommons{results: results}
	c.files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "testdata/images/amul.jpg")
	}))
	t.Cleanup(c.files.Close)
	c.api = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		require.Equal(t, "6", q.Get("gsrnamespace"))
		search := strings.TrimSuffix(q.Get("gsrsearch"), " filetype:bitmap")
		c.mu.Lock()
		c.searches = append(c.searches, search)
		c.mu.Unlock()
		type info struct {
			ThumbURL       string                    `json:"thumburl"`
			DescriptionURL string                    `json:"descriptionurl"`
			Mime           string                    `json:"mime"`
			Width          int                       `json:"width"`
			ExtMetadata    map[string]map[string]any `json:"extmetadata"`
		}
		type page struct {
			Title     string `json:"title"`
			Index     int    `json:"index"`
			ImageInfo []info `json:"imageinfo"`
		}
		pages := []page{}
		for i, title := range c.results[search] {
			name := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
			pages = append(pages, page{Title: "File:" + title, Index: i + 1, ImageInfo: []info{{
				ThumbURL: c.files.URL + "/" + name, DescriptionURL: "https://commons.wikimedia.org/wiki/File:" + name,
				Mime: "image/jpeg", Width: 1200,
				ExtMetadata: map[string]map[string]any{"LicenseShortName": {"value": "CC BY-SA 4.0"}, "Artist": {"value": "<a href='x'>A. Photographer</a>"}},
			}}})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"query": map[string]any{"pages": pages}})
	}))
	t.Cleanup(c.api.Close)
	return c
}

func TestTypesSearchAgain(t *testing.T) {
	w := newWorld(t)
	w.product("Tata Sampann Toor Dal 1 kg", "Tata Sampann", catStaples)
	w.product("Shalimar Garbage Bags Medium 30 pcs", "Shalimar", catHousehold)
	// Toor dal's candidates were all turned down; garbage bags have one
	// waiting that nobody has looked at.
	w.exec(`INSERT INTO type_photos (id, category_id, type_key, status, rank, object_key, content_type, size_bytes, source_ref, source_url, page_url, licence, attribution, reject_reason)
	  VALUES ($1, $2, 'toor dal', 'rejected', 1, 'type-photos/old1.jpg', 'image/jpeg', 10, 'Pigeon pea.jpg', 'https://x', 'https://y', 'CC0', 'Someone', 'A plant')`,
		ids.UUID(ids.New(t0)), catStaples)
	w.exec(`INSERT INTO type_photos (id, category_id, type_key, rank, object_key, content_type, size_bytes, source_ref, source_url, page_url, licence, attribution)
	  VALUES ($1, $2, 'garbage bags medium', 1, 'type-photos/old2.jpg', 'image/jpeg', 10, 'US Navy water bags.jpg', 'https://x', 'https://y', 'CC0', 'Someone')`,
		ids.UUID(ids.New(t0)), catHousehold)
	w.fake.PutPrivate("type-photos/old2.jpg", "image/jpeg", []byte("old"))

	staples, household := categoryHints["staples"], categoryHints["household"]
	commons := newFakeCommons(t, map[string][]string{
		"intitle:pigeon intitle:peas " + staples:   {"Pigeon pea.jpg", "Pigeon pea pods.jpg", "Split pigeon peas.jpg", "Cajanus cajan (Pigeon peas) in a field.jpg"},
		"intitle:toor intitle:dal " + staples:      {"Toor dal tadka.JPG", "Toor dal.jpg", "Toor dal (2).jpg"},
		"intitle:garbage intitle:bag " + household: {"Garbage bag.jpg", "Black garbage bag on a kitchen floor.jpg"},
	})
	u, err := url.Parse(commons.files.URL)
	require.NoError(t, err)
	ti, err := NewTypes(Deps{
		DB: db.New(w.tdb.Pool), Store: storage.New(w.fake.Config(), w.fake.Client()), Client: httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: 5 * time.Second}),
		Clock: w.clk, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sleep: func(_ context.Context, d time.Duration) error { w.clk.Advance(d); return nil },
	}, TypeOptions{UserAgent: UserAgent("test", "https://todayz.in"), Limit: 10, Again: true, APIURL: commons.api.URL, FileHosts: []string{u.Host}})
	require.NoError(t, err)

	rep, err := ti.Run(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, rep.Cleared)
	require.Equal(t, 2, rep.Searched)
	require.Len(t, rep.Found, 2)

	refs := func(kind string) []string {
		rows, err := w.tdb.OwnerPool.Query(context.Background(), `SELECT source_ref FROM type_photos WHERE type_key = $1 AND status = 'pending' ORDER BY rank`, kind)
		require.NoError(t, err)
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var s string
			require.NoError(t, rows.Scan(&s))
			out = append(out, s)
		}
		return out
	}
	// The turned-down file, a title without "peas", the field, the dish and the
	// second of a series stay out.
	require.Equal(t, []string{"Split pigeon peas.jpg", "Toor dal.jpg"}, refs("toor dal"))
	require.Equal(t, []string{"Garbage bag.jpg", "Black garbage bag on a kitchen floor.jpg"}, refs("garbage bags medium"))
	// The dropped candidate's copy is gone; the turned-down row stays.
	_, ok := w.fake.Private("type-photos/old2.jpg")
	require.False(t, ok)
	require.EqualValues(t, 1, w.scalar(`SELECT count(*) FROM type_photos WHERE status = 'rejected'`))
	// No plain or hint-less single-word search was made.
	for _, s := range commons.searches {
		require.True(t, strings.HasPrefix(s, "intitle:"), s)
	}

	// Without --again a kind with waiting candidates is not searched again.
	ti.o.Again = false
	rep, err = ti.Run(context.Background())
	require.NoError(t, err)
	require.Zero(t, rep.Searched)
}
