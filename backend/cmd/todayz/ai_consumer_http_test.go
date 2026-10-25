package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/ids"
)

// --- fakes ------------------------------------------------------------------------

// consumerLLM answers each consumer feature with a fixed tool input. A feature set to
// hang waits for the caller's deadline (the search assist timeout); one set to fail
// answers a provider error.
type consumerLLM struct {
	mu      sync.Mutex
	replies map[string]any
	hang    map[string]bool
	fail    map[string]bool
	calls   map[string]int
}

func (f *consumerLLM) Complete(ctx context.Context, req client.Request) (client.Response, error) {
	f.mu.Lock()
	f.calls[req.Feature]++
	r, ok := f.replies[req.Feature]
	hang, fail := f.hang[req.Feature], f.fail[req.Feature]
	f.mu.Unlock()
	switch {
	case hang:
		<-ctx.Done()
		return client.Response{}, ctx.Err()
	case fail:
		return client.Response{}, &client.ProviderError{Provider: "anthropic", Status: 529, Type: "overloaded_error"}
	case !ok:
		return client.Response{}, client.ErrNoToolCall
	}
	b, err := json.Marshal(r)
	if err != nil {
		return client.Response{}, err
	}
	return client.Response{Model: req.Model, StopReason: "tool_use", CostPaise: 2, ToolInput: b}, nil
}

func (f *consumerLLM) set(feature string, reply any, hang, fail bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[feature], f.hang[feature], f.fail[feature] = reply, hang, fail
}

func (f *consumerLLM) count(feature string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[feature]
}

// --- shop -------------------------------------------------------------------------

// consumerShop is the order shop with the three W3-12 helpers switched on, the model
// and speech faked and the background work run before the create answers.
type consumerShop struct {
	*orderShop
	llm *consumerLLM
	stt *fakeSTT
}

var consumerFlags = []string{"ai_listparse_enabled", "ai_querynorm_enabled", "ai_voicesearch_enabled"}

func newConsumerShop(t *testing.T, on bool) *consumerShop {
	t.Helper()
	cs := &consumerShop{
		llm: &consumerLLM{replies: map[string]any{}, hang: map[string]bool{}, fail: map[string]bool{}, calls: map[string]int{}},
		stt: &fakeSTT{},
	}
	aiRetailSeams = func(d *deps, kit *retail.Kit, llm *client.LLM) {
		*llm = cs.llm
		// The mocks count as configured providers in test (D-17); the fakes stand in.
		kit.Guard = guard.New(d.DB, d.Settings, d.Clock, guard.Providers{LLM: true, STT: true})
		kit.Go = func(f func()) { f() }
	}
	aiConsumerSeams = func(_ *deps, stt *client.STT) { *stt = cs.stt }
	t.Cleanup(func() { aiRetailSeams, aiConsumerSeams = nil, nil })
	cs.orderShop = newOrderShop(t)
	if on {
		for _, k := range consumerFlags {
			cs.setting(k, "true")
		}
	}
	cs.a.exec(`INSERT INTO brands (id, name, slug) VALUES ($1, 'Amul', 'amul'), ($2, 'Nandini', 'nandini')`,
		uuidOf(t, ids.New(cs.a.clk.Now()).String()), uuidOf(t, ids.New(cs.a.clk.Now()).String()))
	return cs
}

func (o *consumerShop) setting(key, value string) {
	o.t.Helper()
	o.a.exec(`UPDATE platform_settings SET value = $1 WHERE key = $2`, value, key)
	o.settings.Invalidate()
}

const (
	listDrafts   = "/v1/me/ai/list-parse/drafts"
	voiceSearch  = "/v1/me/ai/voice-search"
	searchAssist = "/v1/me/ai/search-assist"
	bsk15List    = "1 kg atta, 2 Nandini milk, Dolo 650"
)

func bsk15Reply() map[string]any {
	return map[string]any{"lines": []any{
		map[string]any{"text": "atta 1 kg", "quantity": 1},
		map[string]any{"text": "Nandini milk", "quantity": 2},
		map[string]any{"text": "Dolo 650", "quantity": 1},
	}}
}

// parse posts text and returns the draft as read back.
func (o *consumerShop) parse(tok, text string) (string, resp) {
	o.t.Helper()
	r := o.a.call(tok, http.MethodPost, listDrafts, map[string]any{"text": text}, idem(o.a))
	require.Equal(o.t, http.StatusAccepted, r.code, r.raw)
	id := r.body["draftId"].(string)
	g := o.a.call(tok, http.MethodGet, listDrafts+"/"+id, nil, nil)
	require.Equal(o.t, http.StatusOK, g.code, g.raw)
	return id, g
}

func (o *consumerShop) speak(tok string, audio []byte, lang string) resp {
	o.t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="audio"; filename="voice.webm"`)
	h.Set("Content-Type", "audio/webm;codecs=opus")
	p, err := w.CreatePart(h)
	require.NoError(o.t, err)
	_, err = p.Write(audio)
	require.NoError(o.t, err)
	if lang != "" {
		require.NoError(o.t, w.WriteField("language", lang))
	}
	require.NoError(o.t, w.Close())
	return o.a.call(tok, http.MethodPost, voiceSearch, b.String(), map[string]string{"Content-Type": w.FormDataContentType()})
}

func (o *consumerShop) assist(tok, q string) resp {
	o.t.Helper()
	return o.a.call(tok, http.MethodPost, searchAssist, map[string]any{"q": q}, nil)
}

func (o *consumerShop) search(q string) resp {
	o.t.Helper()
	return o.a.call("", http.MethodGet, "/v1/discover/search?q="+strings.ReplaceAll(q, " ", "+")+"&lat=12.9719&lng=77.6412", nil, nil)
}

// customerCalls is the calls the guard counted against the signed-in customer today.
func (o *consumerShop) customerCalls() int {
	o.t.Helper()
	return o.a.count(`SELECT coalesce(sum(calls), 0) FROM ai_usage_daily WHERE scope = 'customer'`)
}

func (o *consumerShop) rows(table string) int {
	o.t.Helper()
	return o.a.count(`SELECT count(*) FROM ` + table)
}

// --- list parse (AI-C1) -----------------------------------------------------------

// BSK-15: the list becomes draft lines; nothing is saved to a basket and nothing is
// searched; confirm answers only the accepted and edited lines.
func TestAIConsumerListParseBSK15(t *testing.T) {
	o := newConsumerShop(t, true)
	o.llm.set("list_parse", bsk15Reply(), false, false)
	id, g := o.parse(o.cust, bsk15List)
	require.Equal(t, "ready", g.body["status"], g.raw)
	lines := linesOf(g)
	require.Len(t, lines, 3)
	assert.Equal(t, "atta 1 kg", lines[0]["text"])
	assert.EqualValues(t, 2, lines[1]["quantity"])
	assert.Zero(t, o.rows("baskets"), "parsing saves no basket")
	assert.Zero(t, o.rows("basket_lines"))
	assert.Zero(t, o.rows("search_log"), "parsing runs no search")
	assert.NotContains(t, o.logs.String(), "Nandini milk", "the list is never logged")
	var input string
	require.NoError(t, o.a.tdb.OwnerPool.QueryRow(context.Background(),
		`SELECT coalesce(input_ref, '') || input_meta::text FROM ai_drafts WHERE id = $1`, uuidOf(t, id)).Scan(&input))
	assert.NotContains(t, input, "Dolo", "the list text is not stored")

	r := o.a.call(o.cust, http.MethodPost, listDrafts+"/"+id+"/confirm", map[string]any{"lines": []any{
		accept(lines[0]["lineId"].(string)),
		edit(lines[1]["lineId"].(string), map[string]any{"text": "Nandini toned milk 500 ml", "quantity": 2}),
		discard(lines[2]["lineId"].(string)),
	}}, idem(o.a))
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, "confirmed", r.body["status"])
	basket, _ := r.body["basketLines"].([]any)
	require.Len(t, basket, 2, "only the accepted and edited lines")
	assert.Equal(t, "atta 1 kg", basket[0].(map[string]any)["text"])
	assert.Equal(t, "Nandini toned milk 500 ml", basket[1].(map[string]any)["text"])
	assert.Zero(t, o.rows("baskets"), "confirm saves no basket")
	assert.Zero(t, o.rows("search_log"), "confirm runs no search")
	assert.Equal(t, 1, o.customerCalls())
}

// Another customer's draft is 404, read or confirm; signed out is 401.
func TestAIConsumerListParseOwnership(t *testing.T) {
	o := newConsumerShop(t, true)
	o.llm.set("list_parse", bsk15Reply(), false, false)
	id, g := o.parse(o.cust, bsk15List)
	l1 := linesOf(g)[0]["lineId"].(string)
	r := o.a.call(o.cust2, http.MethodGet, listDrafts+"/"+id, nil, nil)
	assert.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call(o.cust2, http.MethodPost, listDrafts+"/"+id+"/confirm", map[string]any{"lines": []any{accept(l1)}}, idem(o.a))
	assert.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call("", http.MethodPost, listDrafts, map[string]any{"text": bsk15List}, idem(o.a))
	assert.Equal(t, http.StatusUnauthorized, r.code, r.raw)
}

// A list with nothing to buy fails the draft; nothing is confirmed.
func TestAIConsumerListParseEmpty(t *testing.T) {
	o := newConsumerShop(t, true)
	o.llm.set("list_parse", map[string]any{"lines": []any{}}, false, false)
	_, g := o.parse(o.cust, "hello how are you")
	assert.Equal(t, "failed", g.body["status"], g.raw)
	assert.Empty(t, linesOf(g))
}

// --- voice search (AI-C5) ---------------------------------------------------------

// ASK-16 and ASK-26: a voice search answers the transcript and chips and keeps nothing:
// no draft, no search, no ask, no event and no log line with the transcript. An abandoned
// one (the customer never searches) is the same on the server. One call is counted.
func TestAIConsumerVoiceSearchKeepsNothing(t *testing.T) {
	o := newConsumerShop(t, true)
	const said = "amul butter under 60 rupees"
	r := o.speak(o.cust, []byte(said), "en-IN")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, said, r.body["transcript"])
	assert.Equal(t, "en", r.body["language"])
	chips, _ := r.body["chips"].([]any)
	kinds := map[string]any{}
	for _, c := range chips {
		m := c.(map[string]any)
		kinds[m["kind"].(string)] = m["value"]
	}
	assert.Equal(t, "6000", kinds["maxPrice"], r.raw)
	assert.Equal(t, "amul", kinds["brand"], r.raw)
	assert.Empty(t, r.hdr.Get("Idempotency-Replayed"))
	assert.Contains(t, r.hdr.Get("Cache-Control"), "no-store")

	assert.Zero(t, o.rows("ai_drafts"), "no draft")
	assert.Zero(t, o.rows("search_log"), "it never searches")
	assert.Zero(t, o.rows("asks"))
	assert.Zero(t, o.a.count(`SELECT count(*) FROM events WHERE props::text LIKE '%butter%'`))
	assert.NotContains(t, o.logs.String(), "butter", "no log line carries the transcript")
	assert.Equal(t, 1, o.customerCalls(), "the cost guard counted one call")
}

// A speech provider failure is 503 (C-23 hides the mic); the call still counts.
func TestAIConsumerVoiceSearchProviderFailure(t *testing.T) {
	o := newConsumerShop(t, true)
	r := o.speak(o.cust, []byte("ERR"), "")
	assert.Equal(t, http.StatusServiceUnavailable, r.code, r.raw)
	assert.Equal(t, "service_unavailable", r.body["code"])
	assert.Zero(t, o.rows("ai_drafts"))
	assert.Equal(t, 1, o.customerCalls())
}

// At the daily cap voice search is 429 ai_daily_cap_reached and speech is not called.
func TestAIConsumerVoiceSearchCap(t *testing.T) {
	o := newConsumerShop(t, true)
	o.setting("ai_daily_calls_customer", "1")
	r := o.speak(o.cust, []byte("tata salt"), "")
	require.Equal(t, http.StatusOK, r.code, r.raw)
	r = o.speak(o.cust, []byte("tata salt"), "")
	assert.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	assert.Equal(t, "ai_daily_cap_reached", r.body["code"])
	o.stt.mu.Lock()
	heard := len(o.stt.langs)
	o.stt.mu.Unlock()
	assert.Equal(t, 1, heard, "speech is not called past the cap")
	assert.Equal(t, 1, o.customerCalls())
}

// --- search assist (AI-C2) --------------------------------------------------------

// Valid output answers the cleaned query and chips of the allowed kinds; nothing is stored.
func TestAIConsumerSearchAssist(t *testing.T) {
	o := newConsumerShop(t, true)
	const q = "amul butter under 60 rupees open now"
	o.llm.set("search_assist", map[string]any{"query": "amul butter", "chips": []any{
		map[string]any{"kind": "maxPrice", "value": "6000", "text": "under 60 rupees"},
		map[string]any{"kind": "openNow", "value": nil, "text": "open now"},
		map[string]any{"kind": "brand", "value": "amul", "text": "amul"},
	}}, false, false)
	r := o.assist(o.cust, q)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, "amul butter", r.body["query"])
	chips, _ := r.body["chips"].([]any)
	assert.Len(t, chips, 3)
	assert.Zero(t, o.rows("ai_drafts"), "no draft (D-101)")
	assert.Zero(t, o.rows("search_log"), "it never searches")
	assert.NotContains(t, o.logs.String(), "60 rupees")
	assert.Equal(t, 1, o.customerCalls())
}

// ASK-27: a timeout past 3 s, a provider error, invalid output or the cap answers 204 or
// 429, and search answers the same results and status before and after.
func TestAIConsumerSearchAssistFailuresLeaveSearchAlone(t *testing.T) {
	o := newConsumerShop(t, true)
	const q = "amul butter open now"
	before := o.search(q)
	require.Equal(t, http.StatusOK, before.code, before.raw)

	cases := []struct {
		name       string
		reply      any
		hang, fail bool
	}{
		{"timeout", nil, true, false},
		{"provider error", nil, false, true},
		{"chip kind not allowed", map[string]any{"query": nil, "chips": []any{map[string]any{"kind": "category", "value": "dairy", "text": "amul"}}}, false, false},
		{"unknown brand", map[string]any{"query": nil, "chips": []any{map[string]any{"kind": "brand", "value": "amulya", "text": "amul"}}}, false, false},
		{"invented price", map[string]any{"query": nil, "chips": []any{map[string]any{"kind": "maxPrice", "value": "5000", "text": "open now"}}}, false, false},
	}
	for _, c := range cases {
		o.llm.set("search_assist", c.reply, c.hang, c.fail)
		r := o.assist(o.cust, q)
		assert.Equal(t, http.StatusNoContent, r.code, "%s: %s", c.name, r.raw)
		assert.Empty(t, r.raw, c.name)
	}
	o.setting("ai_daily_calls_customer", "5")
	calls := o.llm.count("search_assist")
	r := o.assist(o.cust, q)
	assert.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	assert.Equal(t, "ai_daily_cap_reached", r.body["code"])
	assert.Equal(t, calls, o.llm.count("search_assist"), "the model is not called past the cap")

	after := o.search(q)
	require.Equal(t, before.code, after.code)
	assert.Equal(t, before.body["results"], after.body["results"], "search answers the same results")
	assert.Equal(t, before.body["chips"], after.body["chips"])
	assert.Equal(t, 2, o.rows("search_log"), "only the two searches are logged")
	assert.Zero(t, o.rows("ai_drafts"))
}

// --- flags and features -----------------------------------------------------------

// Each helper is 404 while its flag is off; the features read says which are on.
func TestAIConsumerFlags(t *testing.T) {
	o := newConsumerShop(t, false)
	r := o.a.call(o.cust, http.MethodGet, "/v1/me/ai/features", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, k := range []string{"listParse", "voiceSearch", "searchAssist"} {
		assert.Equal(t, false, r.body[k], k)
	}
	r = o.a.call(o.cust, http.MethodPost, listDrafts, map[string]any{"text": bsk15List}, idem(o.a))
	assert.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.a.call(o.cust, http.MethodGet, listDrafts+"/"+ids.New(o.a.clk.Now()).String(), nil, nil)
	assert.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.speak(o.cust, []byte("amul butter"), "")
	assert.Equal(t, http.StatusNotFound, r.code, r.raw)
	r = o.assist(o.cust, "amul butter")
	assert.Equal(t, http.StatusNotFound, r.code, r.raw)
	assert.Zero(t, o.llm.count("list_parse")+o.llm.count("search_assist"))
	assert.Zero(t, o.customerCalls(), "nothing counted while off")

	for _, k := range consumerFlags {
		o.setting(k, "true")
	}
	r = o.a.call(o.cust, http.MethodGet, "/v1/me/ai/features", nil, nil)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	for _, k := range []string{"listParse", "voiceSearch", "searchAssist"} {
		assert.Equal(t, true, r.body[k], k)
	}
	assert.EqualValues(t, 15, r.body["maxAudioSeconds"])
	r = o.a.call("", http.MethodGet, "/v1/me/ai/features", nil, nil)
	assert.Equal(t, http.StatusUnauthorized, r.code, r.raw)
}
