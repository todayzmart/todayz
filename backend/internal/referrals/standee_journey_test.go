package referrals

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/business/standee"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/db"
)

// SC-ST-04 (W3-09): a standee made in B-10 is scanned from its link, one
// scanner signs up and makes a first qualifying order, another only scans;
// the standee's counts read 2 scans → 1 sign-up → 1 qualified.
func TestStandeeJourney(t *testing.T) {
	e := newEnv(t)
	_, store, _, storeCode, slug := e.shopWithStore()
	staff, _ := e.newCustomer()
	campaigns, err := standee.New(standee.Deps{DB: db.New(e.tdb.Pool), Clock: e.clk, PublicURL: "https://api.todayz.localhost"})
	require.NoError(t, err)
	p := authn.Principal{UserID: staff, App: authn.AppBusiness}

	st, err := campaigns.Create(e.ctx, p, store, "Counter")
	require.NoError(t, err)
	link, err := url.Parse(st.Link)
	require.NoError(t, err)
	require.Equal(t, "/v1/public/ref/"+storeCode, link.Path)
	scan := func() string {
		t.Helper()
		e.clk.Set(testNow.Add(-2 * time.Hour))
		defer e.clk.Set(testNow)
		l, err := e.svc.Open(e.ctx, Click{Code: strings.TrimPrefix(link.Path, "/v1/public/ref/"), Standee: link.Query().Get("s")})
		require.NoError(t, err)
		assert.Equal(t, "https://app.todayz.localhost/s/"+slug+"?s="+st.Id, l.Location)
		return cookieValue(t, l.SetCookie)
	}

	u, _ := e.newCustomer()
	res, err := e.claim(u, ClaimInput{Cookie: scan()})
	require.NoError(t, err)
	assert.Equal(t, KindRetailerCustomer, res.Kind)
	_ = scan() // a second phone scans and leaves

	l, err := campaigns.List(e.ctx, store)
	require.NoError(t, err)
	require.Len(t, l.Items, 1)
	assert.Equal(t, 2, l.Items[0].Counts.Scans)
	assert.Equal(t, 1, l.Items[0].Counts.SignUps)
	assert.Equal(t, 0, l.Items[0].Counts.Qualified)

	e.complete(e.order(u, store, 50_000, 0))
	l, err = campaigns.List(e.ctx, store)
	require.NoError(t, err)
	assert.Equal(t, 1, l.Items[0].Counts.Qualified)
	assert.Equal(t, l.Items[0].Counts, l.Totals)
}
