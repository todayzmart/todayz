package main

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The Counter sales tests (D-268): the list filters, the summary, one sale's
// detail and the CSV export on the real router. The fixture clock starts at
// 2026-10-23 06:00 UTC, 11:30 IST (setup moves it on a few minutes), so IST midnight is 18:30 UTC the day
// before.

// salesIDs is every sale id the list returns for the query, all pages.
func (p *posShop) salesIDs(tok string, q url.Values) []string {
	p.t.Helper()
	var out []string
	cursor := ""
	for range 20 {
		v := url.Values{}
		for k, xs := range q {
			v[k] = xs
		}
		v.Set("limit", "2")
		if cursor != "" {
			v.Set("cursor", cursor)
		}
		r := p.a.call(tok, http.MethodGet, p.S+"/pos/sales?"+v.Encode(), nil, nil)
		require.Equal(p.t, http.StatusOK, r.code, r.raw)
		for _, it := range r.body["items"].([]any) {
			out = append(out, it.(map[string]any)["id"].(string))
		}
		next, _ := r.body["nextCursor"].(string)
		if next == "" {
			return out
		}
		cursor = next
	}
	p.t.Fatal("too many pages")
	return nil
}

func q(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func TestCounterSalesFilters(t *testing.T) {
	p := newPosShop(t)
	now := p.a.clk.Now()
	require.Equal(t, "2026-10-23", now.UTC().Format(time.DateOnly), "the fixture clock is mid-morning IST on the 23rd")
	lateYesterday := time.Date(2026, 10, 22, 18, 29, 0, 0, time.UTC) // 23:59 IST on the 22nd
	earlyToday := time.Date(2026, 10, 22, 18, 31, 0, 0, time.UTC)    // 00:01 IST on the 23rd

	s := p.sync(p.cashier,
		p.saleEntry(1, lateYesterday, nil, line{p.salt, 1}),
		p.saleEntry(2, earlyToday, map[string]any{"method": "phone_unverified", "phone": custPhone}, line{p.salt, 2}),
	)
	require.Equal(t, http.StatusOK, s.code, "sync: status %d", s.code)
	yesterdayID, earlyID := results(s)[0]["saleId"].(string), results(s)[1]["saleId"].(string)

	cash := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 1}))
	require.Equal(t, http.StatusOK, cash.code, cash.raw)
	card := p.saleBody(p.newID(), line{p.atta, 2})
	card["paymentMethod"] = "card"
	cardSale := p.sell(p.manager, card)
	require.Equal(t, http.StatusOK, cardSale.code, cardSale.raw)
	cashID, cardID := cash.body["id"].(string), cardSale.body["id"].(string)
	rv := p.reverse(p.manager, cashID)
	require.Equal(t, http.StatusOK, rv.code, rv.raw)

	all := p.salesIDs(p.cashier, nil)
	require.ElementsMatch(t, []string{yesterdayID, earlyID, cashID, cardID}, all)

	// IST days: the 23rd starts at 18:30 UTC on the 22nd.
	require.ElementsMatch(t, []string{earlyID, cashID, cardID}, p.salesIDs(p.cashier, q("from", "2026-10-23", "to", "2026-10-23")))
	require.Equal(t, []string{yesterdayID}, p.salesIDs(p.cashier, q("from", "2026-10-22", "to", "2026-10-22")))
	require.Equal(t, []string{yesterdayID}, p.salesIDs(p.cashier, q("to", "2026-10-22")))
	require.ElementsMatch(t, []string{earlyID, cashID, cardID}, p.salesIDs(p.cashier, q("from", "2026-10-23")))

	require.Equal(t, []string{cardID}, p.salesIDs(p.cashier, q("payment", "card")))
	require.Equal(t, []string{cashID}, p.salesIDs(p.cashier, q("payment", "cash")))
	require.Equal(t, []string{cashID}, p.salesIDs(p.cashier, q("status", "reversed")))
	require.ElementsMatch(t, []string{yesterdayID, earlyID, cardID}, p.salesIDs(p.cashier, q("status", "completed")))
	require.Equal(t, []string{earlyID}, p.salesIDs(p.cashier, q("link", "pending")))
	require.ElementsMatch(t, []string{yesterdayID, cashID, cardID}, p.salesIDs(p.cashier, q("link", "none")))
	require.Empty(t, p.salesIDs(p.cashier, q("link", "linked")))
	require.Empty(t, p.salesIDs(p.cashier, q("link", "failed")))
	require.ElementsMatch(t, []string{yesterdayID, earlyID}, p.salesIDs(p.cashier, q("offline", "true")))
	require.ElementsMatch(t, []string{cashID, cardID}, p.salesIDs(p.cashier, q("offline", "false")))

	// The cashier on each row, and as a filter.
	lr := p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales?payment=card", nil, nil)
	require.Equal(t, http.StatusOK, lr.code, lr.raw)
	managerID := lr.body["items"].([]any)[0].(map[string]any)["cashier"].(map[string]any)["id"].(string)
	require.Equal(t, []string{cardID}, p.salesIDs(p.cashier, q("cashier", managerID)))
	require.NotContains(t, lr.raw, custPhone)

	for _, bad := range []string{"from=2026-10-23&to=2026-10-22", "cashier=nope", "link=maybe", "from=23-10-2026"} {
		r := p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales?"+bad, nil, nil)
		require.Equal(t, http.StatusUnprocessableEntity, r.code, "%s: %s", bad, r.raw)
	}
	p.noSecrets(custPhone)
}

func TestCounterSalesSummary(t *testing.T) {
	p := newPosShop(t)
	a := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 1}))
	require.Equal(t, http.StatusOK, a.code, a.raw)
	upi := p.saleBody(p.newID(), line{p.salt, 3})
	upi["paymentMethod"] = "upi"
	b := p.sell(p.manager, upi)
	require.Equal(t, http.StatusOK, b.code, b.raw)
	c := p.sell(p.cashier, p.saleBody(p.newID(), line{p.atta, 1}))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	rv := p.reverse(p.manager, c.body["id"].(string))
	require.Equal(t, http.StatusOK, rv.code, rv.raw)

	total := func(r resp) int { return int(r.body["totalPaise"].(float64)) }
	sales := total(a) + total(b)

	r := p.a.call(p.manager, http.MethodGet, p.S+"/pos/sales-summary?from=2026-10-23&to=2026-10-23", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "2026-10-23", r.body["from"])
	require.EqualValues(t, sales, r.body["salesPaise"], "completed sales only")
	require.EqualValues(t, 2, r.body["billCount"])
	require.EqualValues(t, sales/2, r.body["averageBillPaise"])
	require.EqualValues(t, 4, r.body["unitCount"])
	require.EqualValues(t, total(c), r.body["reversedPaise"])
	require.EqualValues(t, 1, r.body["reversedCount"])
	require.EqualValues(t, 0, r.body["offlineCount"])
	byPay := map[string]float64{}
	for _, x := range r.body["byPayment"].([]any) {
		m := x.(map[string]any)
		byPay[m["method"].(string)] = m["totalPaise"].(float64)
	}
	require.Equal(t, map[string]float64{"cash": float64(total(a)), "upi": float64(total(b))}, byPay)
	require.Len(t, r.body["byCashier"].([]any), 2)

	// Another day is empty, with no average.
	r = p.a.call(p.owner, http.MethodGet, p.S+"/pos/sales-summary?from=2026-10-22&to=2026-10-22", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.EqualValues(t, 0, r.body["billCount"])
	require.EqualValues(t, 0, r.body["averageBillPaise"])

	// Store-wide totals are owner and manager only (as insights.view).
	r = p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales-summary", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.a.call(p.cashierS2, http.MethodGet, p.S+"/pos/sales-summary", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
}

func TestCounterSaleDetail(t *testing.T) {
	p := newPosShop(t)
	now := p.a.clk.Now()
	online := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 2}))
	require.Equal(t, http.StatusOK, online.code, online.raw)
	id := online.body["id"].(string)

	r := p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales/"+id, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.NotContains(t, r.raw, "receiptToken")
	require.Equal(t, false, r.body["offline"])
	require.Equal(t, "completed", r.body["status"])
	require.NotEmpty(t, r.body["deviceId"])
	require.NotEmpty(t, r.body["cashier"].(map[string]any)["id"])
	lines := r.body["lines"].([]any)
	require.Len(t, lines, 1)
	require.EqualValues(t, 2, lines[0].(map[string]any)["quantity"])
	until := mustParse(t, r.body["reversibleUntil"])
	require.True(t, until.After(now), "an online sale is reversible from sold_at")
	require.Nil(t, r.body["reversedBy"])

	rv := p.reverse(p.manager, id)
	require.Equal(t, http.StatusOK, rv.code, rv.raw)
	r = p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales/"+id, nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Equal(t, "reversed", r.body["status"])
	require.Equal(t, "rang the wrong item", r.body["reversalReason"])
	require.NotEmpty(t, r.body["reversedBy"].(map[string]any)["id"])
	require.NotEqual(t, r.body["cashier"].(map[string]any)["id"], r.body["reversedBy"].(map[string]any)["id"], "the manager reversed it")
	require.Nil(t, r.body["reversibleUntil"], "a reversed sale has no window")

	// An offline sale's window runs from when it reached the server (D-131).
	s := p.sync(p.cashier, p.saleEntry(1, now.Add(-3*time.Hour), map[string]any{"method": "phone_unverified", "phone": custPhone}, line{p.salt, 1}))
	require.Equal(t, http.StatusOK, s.code, "sync: status %d", s.code)
	off := results(s)[0]["saleId"].(string)
	r = p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales/"+off, nil, nil)
	require.Equal(t, http.StatusOK, r.code, "status %d", r.code)
	require.NotContains(t, r.raw, custPhone)
	require.Equal(t, true, r.body["offline"])
	require.Nil(t, r.body["link"].(map[string]any)["customerName"], "an unverified phone names no customer")
	window := mustParse(t, r.body["reversibleUntil"]).Sub(mustParse(t, r.body["receivedAt"]))
	require.Equal(t, window, until.Sub(mustParse(t, online.body["soldAt"])), "the same window, from received_at")

	r = p.a.call(p.cashierS2, http.MethodGet, p.S+"/pos/sales/"+id, nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	r = p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales/"+p.newID(), nil, nil)
	require.Equal(t, http.StatusNotFound, r.code, r.raw)
	p.noSecrets(custPhone)
}

func TestCounterSalesExport(t *testing.T) {
	p := newPosShop(t)
	now := p.a.clk.Now()
	s := p.sync(p.cashier, p.saleEntry(1, now.Add(-time.Hour), map[string]any{"method": "phone_unverified", "phone": custPhone}, line{p.salt, 2}))
	require.Equal(t, http.StatusOK, s.code, "sync: status %d", s.code)
	on := p.sell(p.cashier, p.saleBody(p.newID(), line{p.butter, 1}))
	require.Equal(t, http.StatusOK, on.code, on.raw)
	rv := p.a.call(p.manager, http.MethodPost, p.S+"/pos/sales/"+on.body["id"].(string)+"/reverse",
		map[string]any{"reason": "=HYPERLINK(\"x\")"}, idem(p.a))
	require.Equal(t, http.StatusOK, rv.code, rv.raw)

	r := p.a.call(p.manager, http.MethodGet, p.S+"/pos/sales-export?from=2026-10-23&to=2026-10-23", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Contains(t, r.hdr.Get("Content-Type"), "text/csv")
	rows := strings.Split(strings.TrimSpace(r.raw), "\n")
	require.Len(t, rows, 3, "a header and two sales")
	require.True(t, strings.HasPrefix(rows[0], "sale_id,sold_at_ist,received_at_ist,status,payment_method"))
	require.NotContains(t, r.raw, custPhone)
	require.NotContains(t, r.raw, custPhone[len(custPhone)-10:])
	require.Contains(t, r.raw, "'=HYPERLINK", "a formula reason is quoted (D-93)")
	require.Contains(t, r.raw, ","+now.Add(5*time.Hour+30*time.Minute).UTC().Format("2006-01-02 15:04")+",", "times in IST")
	require.Contains(t, r.raw, ",reversed,cash,")

	r = p.a.call(p.manager, http.MethodGet, p.S+"/pos/sales-export?status=completed", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	require.Len(t, strings.Split(strings.TrimSpace(r.raw), "\n"), 2)

	r = p.a.call(p.cashier, http.MethodGet, p.S+"/pos/sales-export", nil, nil)
	require.Equal(t, http.StatusForbidden, r.code, r.raw)
	p.noSecrets(custPhone)
}
