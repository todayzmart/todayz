package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/guard"
	"github.com/todayz/backend/internal/catalogue"
	"github.com/todayz/backend/internal/jobs"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/voice"
)

// --- fakes ------------------------------------------------------------------------

// fakeSTT hears the recording's bytes as the transcript; "ERR" fails the call.
type fakeSTT struct {
	mu    sync.Mutex
	langs []string
}

func (f *fakeSTT) Transcribe(_ context.Context, a client.Audio) (client.Transcript, error) {
	f.mu.Lock()
	f.langs = append(f.langs, a.Language)
	f.mu.Unlock()
	if string(a.Data) == "ERR" {
		return client.Transcript{}, &client.ProviderError{Provider: "sarvam", Status: 503, Type: "unavailable"}
	}
	return client.Transcript{Text: string(a.Data), Language: a.Language}, nil
}

// said is one line the fake model answers: item is a name the fake looks up in the
// candidate list (so refs follow the list), ref a literal ref, neither an unmatched line.
type said struct {
	kind, item, ref, heard string
	qty                    *int
	rupees                 *float64
	conf                   float64
}

type reply struct {
	lines []said
	raw   string // a literal tool input instead of lines
	err   error
}

// fakeLLM answers by transcript and keeps every user message it saw.
type fakeLLM struct {
	mu      sync.Mutex
	replies map[string]reply
	seen    []string
}

var (
	reTranscript = regexp.MustCompile(`(?s)<transcript>\n(.*)\n</transcript>`)
	reItemRow    = regexp.MustCompile(`(?m)^(c[0-9]+) \| (.*)$`)
)

func (f *fakeLLM) Complete(_ context.Context, req client.Request) (client.Response, error) {
	user := req.Messages[0].Content[0].Text
	f.mu.Lock()
	f.seen = append(f.seen, user)
	m := reTranscript.FindStringSubmatch(user)
	r, ok := reply{}, false
	if m != nil {
		r, ok = f.replies[m[1]]
	}
	f.mu.Unlock()
	resp := client.Response{Model: req.Model, StopReason: "tool_use", CostPaise: 2}
	switch {
	case !ok:
		return resp, client.ErrNoToolCall
	case r.err != nil:
		return resp, r.err
	case r.raw != "":
		resp.ToolInput = json.RawMessage(r.raw)
		return resp, nil
	}
	refs := map[string]string{}
	for _, row := range reItemRow.FindAllStringSubmatch(user, -1) {
		refs[row[2]] = row[1]
	}
	lines := make([]map[string]any, 0, len(r.lines))
	for _, l := range r.lines {
		var item, unmatched any
		switch {
		case l.ref != "":
			item = l.ref
		case l.item != "":
			for name, ref := range refs {
				if strings.HasPrefix(name, l.item) {
					item = ref
				}
			}
		default:
			unmatched = l.heard
		}
		lines = append(lines, map[string]any{
			"kind": l.kind, "item": item, "unmatched": unmatched, "heard": l.heard,
			"quantity": l.qty, "price_rupees": l.rupees, "confidence": l.conf,
		})
	}
	b, _ := json.Marshal(map[string]any{"lines": lines})
	resp.ToolInput = b
	return resp, nil
}

func qty(n int) *int            { return &n }
func rupees(r float64) *float64 { return &r }

// --- shop -------------------------------------------------------------------------

// voiceShop is the POS shop with voice stock update switched on, the speech and model
// providers faked and the background work run before the upload answers (unless held).
type voiceShop struct {
	*posShop
	llm  *fakeLLM
	stt  *fakeSTT
	mu   sync.Mutex
	hold bool
	held []func()
}

func newVoiceShop(t *testing.T) *voiceShop {
	t.Helper()
	v := &voiceShop{llm: &fakeLLM{replies: map[string]reply{}}, stt: &fakeSTT{}}
	voiceSeams = func(d *voice.Deps) {
		d.LLM, d.STT = v.llm, v.stt
		// The mocks count as configured providers in test (D-17); the fakes stand in for them.
		d.Guard = guard.New(d.DB, d.Settings, d.Clock, guard.Providers{LLM: true, STT: true})
		d.Go = func(f func()) {
			v.mu.Lock()
			hold := v.hold
			if hold {
				v.held = append(v.held, f)
			}
			v.mu.Unlock()
			if !hold {
				f()
			}
		}
	}
	t.Cleanup(func() { voiceSeams = nil })
	v.posShop = newPosShopWith(t, nil)
	v.setting("ai_voice_stock_enabled", "true")
	return v
}

func (v *voiceShop) setting(key, value string) {
	v.t.Helper()
	v.a.exec(`UPDATE platform_settings SET value = $1 WHERE key = $2`, value, key)
	v.settings.Invalidate()
}

func (v *voiceShop) drafts() string { return v.S + "/ai/voice-stock/drafts" }

// upload sends one recording part (and a language part unless lang is "").
func (v *voiceShop) upload(tok string, audio []byte, mediaType, lang string, extra ...string) resp {
	v.t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	if audio != nil {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="audio"; filename="voice.webm"`)
		h.Set("Content-Type", mediaType)
		p, err := w.CreatePart(h)
		require.NoError(v.t, err)
		_, err = p.Write(audio)
		require.NoError(v.t, err)
	}
	if lang != "" {
		require.NoError(v.t, w.WriteField("language", lang))
	}
	for _, name := range extra {
		require.NoError(v.t, w.WriteField(name, "x"))
	}
	require.NoError(v.t, w.Close())
	return v.a.call(tok, http.MethodPost, v.drafts(), b.String(), map[string]string{"Content-Type": w.FormDataContentType()})
}

// speak uploads transcript (the fake hears the bytes) with the model's reply and returns
// the draft id.
func (v *voiceShop) speak(tok, transcript string, r reply) string {
	v.t.Helper()
	v.llm.mu.Lock()
	v.llm.replies[strings.TrimSpace(transcript)] = r
	v.llm.mu.Unlock()
	w := v.upload(tok, []byte(transcript), "audio/webm;codecs=opus", "hi-IN")
	require.Equal(v.t, http.StatusAccepted, w.code, w.raw)
	require.Equal(v.t, "processing", w.body["status"])
	return w.body["draftId"].(string)
}

func (v *voiceShop) get(tok, id string) resp {
	v.t.Helper()
	return v.a.call(tok, http.MethodGet, v.drafts()+"/"+id, nil, nil)
}

func (v *voiceShop) confirm(tok, id string, decisions ...map[string]any) resp {
	v.t.Helper()
	return v.a.call(tok, http.MethodPost, v.drafts()+"/"+id+"/confirm", map[string]any{"lines": decisions}, idem(v.a))
}

func accept(lineID string) map[string]any {
	return map[string]any{"lineId": lineID, "outcome": "accepted"}
}

func discard(lineID string) map[string]any {
	return map[string]any{"lineId": lineID, "outcome": "discarded"}
}

func edit(lineID string, value map[string]any) map[string]any {
	return map[string]any{"lineId": lineID, "outcome": "edited", "value": value}
}

func linesOf(r resp) []map[string]any {
	raw, _ := r.body["lines"].([]any)
	out := make([]map[string]any, len(raw))
	for i, l := range raw {
		out[i], _ = l.(map[string]any)
	}
	return out
}

func outcomesOf(r resp) map[string]string {
	out := map[string]string{}
	for _, l := range linesOf(r) {
		out[l["lineId"].(string)] = l["outcome"].(string)
	}
	return out
}

// writes counts what a confirm writes: POS sales, voice-made movements and price changes.
func (v *voiceShop) writes() int {
	v.t.Helper()
	return v.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(v.t, v.sid)) +
		v.a.count(`SELECT count(*) FROM stock_movements WHERE store_id = $1 AND source <> 'opening'`, uuidOf(v.t, v.sid))
}

// saleID is the POS client id of the draft's sale that starts at lineID.
func (v *voiceShop) saleID(draft, lineID string) pgtype.UUID {
	v.t.Helper()
	d, err := ids.Parse(draft)
	require.NoError(v.t, err)
	return ids.UUID(voice.SaleClientID(d, lineID))
}

func (v *voiceShop) onHand(item string) int {
	return v.a.count(`SELECT on_hand FROM store_items WHERE id = $1`, uuidOf(v.t, item))
}

func (v *voiceShop) priceOf(item string) int {
	return v.a.count(`SELECT price_paise FROM store_items WHERE id = $1`, uuidOf(v.t, item))
}

// fourKinds is one utterance with a line of every kind: 3 butter sold, 12 salt in, atta out
// of stock and butter at ₹58 (its MRP is ₹60).
const fourKinds = "teen Amul butter becha, 12 Tata salt aaye, atta khatam, butter ka rate 58 karo"

var fourKindsReply = reply{lines: []said{
	{kind: "sale", item: "Amul Butter", heard: "teen Amul butter becha", qty: qty(3), conf: 0.92},
	{kind: "restock", item: "Tata Salt", heard: "12 Tata salt aaye", qty: qty(12), conf: 0.9},
	{kind: "count", item: "Aashirvaad Atta", heard: "atta khatam", qty: qty(0), conf: 0.85},
	{kind: "set_price", item: "Amul Butter", heard: "butter ka rate 58 karo", rupees: rupees(58), conf: 0.88},
}}

// --- scenarios --------------------------------------------------------------------

// SC-VO-01: a recording answers 202 with a processing draft; the background work makes it
// ready with one line per spoken change (item, kind, number, confidence, the item's price and
// stock now), and nothing is written before a person confirms (0 unconfirmed writes).
// SC-VO-04: a manager confirms all four kinds.
// SC-VO-02: each kind is written through its own domain on confirm: one POS sale with no
// customer (sale_pos), a restock movement naming the draft, "out of stock" as a count of 0,
// the new price; the sale is at the price after the draft's own price change.
func TestVoiceStockConfirmAllKinds(t *testing.T) {
	v := newVoiceShop(t)
	before := v.writes()
	id := v.speak(v.manager, fourKinds, fourKindsReply)

	r := v.get(v.manager, id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, "ready", r.body["status"])
	assert.Equal(t, "voice_stock", r.body["feature"])
	assert.Equal(t, fourKinds, r.body["transcript"])
	assert.Equal(t, "hi-IN", r.body["language"])
	ls := linesOf(r)
	require.Len(t, ls, 4)
	want := []struct {
		kind, item string
	}{{"sale", v.butter}, {"restock", v.salt}, {"count", v.atta}, {"set_price", v.butter}}
	for i, w := range want {
		assert.Equal(t, w.kind, ls[i]["kind"], i)
		assert.Equal(t, w.item, ls[i]["storeItemId"], i)
		assert.Nil(t, ls[i]["disabled"], "a manager may confirm every kind")
	}
	assert.Equal(t, "Amul Butter 100 g", ls[0]["itemName"])
	assert.EqualValues(t, 3, ls[0]["quantity"])
	// "teen" (3) is a word while the transcript has digits: the ungrounded number caps the
	// line's confidence so the person looks at it; "12" is in the words and keeps its own.
	assert.EqualValues(t, voice.LowConfidence, ls[0]["confidence"])
	assert.EqualValues(t, 0.9, ls[1]["confidence"])
	assert.EqualValues(t, 5500, ls[0]["currentPricePaise"])
	assert.EqualValues(t, 500, ls[0]["onHand"])
	assert.EqualValues(t, 0, ls[2]["quantity"], "out of stock is a count of 0")
	assert.EqualValues(t, 5800, ls[3]["pricePaise"])
	assert.Nil(t, ls[3]["quantity"])
	require.Equal(t, before, v.writes(), "nothing is written before the confirm")
	assert.Equal(t, 5500, v.priceOf(v.butter))

	c := v.confirm(v.manager, id, accept("l1"), accept("l2"), accept("l3"), accept("l4"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, "confirmed", c.body["status"])
	assert.Equal(t, map[string]string{"l1": "accepted", "l2": "accepted", "l3": "accepted", "l4": "accepted"}, outcomesOf(c))

	sid := uuidOf(t, v.sid)
	sale := v.saleID(id, "l1")
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1 AND customer_id IS NULL AND client_id = $2`, sid, sale))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM stock_movements WHERE store_item_id = $1 AND source = 'sale_pos' AND delta = -3`, uuidOf(t, v.butter)))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM stock_movements WHERE store_item_id = $1 AND source = 'restock' AND delta = 12
	  AND ref_type = 'ai_draft' AND ref_id = $2`, uuidOf(t, v.salt), uuidOf(t, id)))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM stock_movements WHERE store_item_id = $1 AND source = 'count_correction' AND delta = -500`, uuidOf(t, v.atta)))
	assert.Equal(t, 0, v.onHand(v.atta))
	assert.Equal(t, 512, v.onHand(v.salt))
	assert.Equal(t, 497, v.onHand(v.butter))
	assert.Equal(t, 5800, v.priceOf(v.butter))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM pos_sale_lines l JOIN pos_sales s ON s.id = l.sale_id
	  WHERE s.client_id = $1 AND l.unit_price_paise = 5800 AND l.quantity = 3`, sale), "the sale is at the price after the draft's own change")

	// SC-VO-11: a repeat confirm answers the first result and writes nothing again.
	after := v.writes()
	again := v.confirm(v.manager, id, accept("l1"), discard("l2"), discard("l3"), discard("l4"))
	require.Equal(t, http.StatusOK, again.code, again.raw)
	assert.Equal(t, outcomesOf(c), outcomesOf(again))
	assert.Equal(t, after, v.writes())

	r = v.get(v.manager, id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, "confirmed", r.body["status"])
	assert.Len(t, r.body["outcomes"], 4)
}

// SC-VO-03: a cashier confirms sale and count lines; restock and set_price lines show
// disabled "ask_manager" and their confirm records them not_permitted without writing them.
func TestVoiceStockCashierAskManager(t *testing.T) {
	v := newVoiceShop(t)
	id := v.speak(v.cashier, fourKinds, fourKindsReply)

	r := v.get(v.cashier, id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	ls := linesOf(r)
	require.Len(t, ls, 4)
	assert.Nil(t, ls[0]["disabled"], "sale: pos.sell")
	assert.Equal(t, "ask_manager", ls[1]["disabled"], "restock: stock.restock")
	assert.Nil(t, ls[2]["disabled"], "count: stock.count")
	assert.Equal(t, "ask_manager", ls[3]["disabled"], "set_price: item.price.edit")

	// The manager reading the cashier's draft sees every line enabled.
	m := v.get(v.manager, id)
	require.Equal(t, http.StatusOK, m.code, m.raw)
	for _, l := range linesOf(m) {
		assert.Nil(t, l["disabled"])
	}

	before := v.writes()
	c := v.confirm(v.cashier, id, accept("l1"), accept("l2"), accept("l3"), accept("l4"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, "ready", c.body["status"], "the manager's lines keep the draft open")
	assert.Equal(t, map[string]string{"l1": "accepted", "l2": "not_permitted", "l3": "accepted", "l4": "not_permitted"}, outcomesOf(c))
	assert.Equal(t, 0, v.a.count(`SELECT count(*) FROM stock_movements WHERE ref_id = $1`, uuidOf(t, id)), "no restock")
	assert.Equal(t, 500, v.onHand(v.salt))
	assert.Equal(t, 5500, v.priceOf(v.butter), "no price change")
	assert.Equal(t, 0, v.onHand(v.atta), "the count is applied")
	assert.Equal(t, 497, v.onHand(v.butter), "the sale is applied")
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM pos_sale_lines l JOIN pos_sales s ON s.id = l.sale_id
	  WHERE s.client_id = $1 AND l.unit_price_paise = 5500`, v.saleID(id, "l1")))
	afterCashier := v.writes()
	assert.Equal(t, before+3, afterCashier, "one sale, its movement and the count")

	// The draft stays open with the two lines: the cashier sees them still asking for a
	// manager, a repeat by the cashier applies nothing again.
	r = v.get(v.cashier, id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	assert.Equal(t, "ready", r.body["status"])
	assert.Len(t, r.body["outcomes"], 4)
	again := v.confirm(v.cashier, id, accept("l1"), accept("l2"), accept("l3"), accept("l4"))
	require.Equal(t, http.StatusOK, again.code, again.raw)
	assert.Equal(t, "ready", again.body["status"])
	assert.Equal(t, outcomesOf(c), outcomesOf(again))
	assert.Equal(t, afterCashier, v.writes())

	// The manager's confirm applies only the open lines; the settled ones are not applied twice.
	m = v.confirm(v.manager, id, accept("l1"), accept("l2"), accept("l3"), accept("l4"))
	require.Equal(t, http.StatusOK, m.code, m.raw)
	assert.Equal(t, "confirmed", m.body["status"])
	assert.Equal(t, map[string]string{"l1": "accepted", "l2": "accepted", "l3": "accepted", "l4": "accepted"}, outcomesOf(m))
	assert.Equal(t, 512, v.onHand(v.salt), "the restock")
	assert.Equal(t, 5800, v.priceOf(v.butter), "the price")
	assert.Equal(t, 497, v.onHand(v.butter), "the sale once")
	assert.Equal(t, 0, v.onHand(v.atta), "the count once")
	assert.Equal(t, afterCashier+1, v.writes(), "only the restock movement")
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM ai_drafts WHERE id = $1 AND confirmed_by IS NOT NULL`, uuidOf(t, id)))

	// A replay after the draft closed, by either of them, answers the closed result.
	closed := v.writes()
	for _, tok := range []string{v.manager, v.cashier} {
		rp := v.confirm(tok, id, accept("l1"), accept("l2"), accept("l3"), accept("l4"))
		require.Equal(t, http.StatusOK, rp.code, rp.raw)
		assert.Equal(t, "confirmed", rp.body["status"])
		assert.Equal(t, outcomesOf(m), outcomesOf(rp))
	}
	assert.Equal(t, closed, v.writes())

	// An edit cannot turn a refused kind into an allowed one's write either: a cashier's
	// edited restock is not_permitted too, and the draft waits for a manager.
	id2 := v.speak(v.cashier, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})
	c = v.confirm(v.cashier, id2, edit("l1", map[string]any{"kind": "restock", "storeItemId": v.salt, "quantity": 40}))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, "ready", c.body["status"])
	assert.Equal(t, map[string]string{"l1": "not_permitted"}, outcomesOf(c))
	assert.Equal(t, 512, v.onHand(v.salt))
	// Discarding the open line closes it.
	c = v.confirm(v.manager, id2, discard("l1"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, "discarded", c.body["status"])
	assert.Equal(t, 512, v.onHand(v.salt))
}

// A count and a sale on the same item apply in the order spoken: "sold 3, only 5 left"
// leaves 5, "5 left, then sold 3" leaves 2. Sales split by another kind are separate POS
// sales, each with its own client id.
func TestVoiceStockSpokenOrder(t *testing.T) {
	v := newVoiceShop(t)
	id := v.speak(v.manager, "teen butter becha, sirf paanch bache", reply{lines: []said{
		{kind: "sale", item: "Amul Butter", heard: "teen butter becha", qty: qty(3), conf: 0.9},
		{kind: "count", item: "Amul Butter", heard: "sirf paanch bache", qty: qty(5), conf: 0.9},
	}})
	c := v.confirm(v.manager, id, accept("l1"), accept("l2"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, 5, v.onHand(v.butter), "the count after the sale")

	id = v.speak(v.manager, "butter dus bache, phir teen becha", reply{lines: []said{
		{kind: "count", item: "Amul Butter", heard: "butter dus bache", qty: qty(10), conf: 0.9},
		{kind: "sale", item: "Amul Butter", heard: "phir teen becha", qty: qty(3), conf: 0.9},
	}})
	c = v.confirm(v.manager, id, accept("l1"), accept("l2"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, 7, v.onHand(v.butter), "the sale after the count")

	sales := v.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(t, v.sid))
	id = v.speak(v.manager, "do salt bika, bees salt aaya, ek salt bika", reply{lines: []said{
		{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9},
		{kind: "restock", item: "Tata Salt", heard: "bees salt aaya", qty: qty(20), conf: 0.9},
		{kind: "sale", item: "Tata Salt", heard: "ek salt bika", qty: qty(1), conf: 0.9},
	}})
	c = v.confirm(v.manager, id, accept("l1"), accept("l2"), accept("l3"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, 517, v.onHand(v.salt))
	assert.Equal(t, sales+2, v.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(t, v.sid)))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM pos_sales WHERE client_id = $1`, v.saleID(id, "l1")))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM pos_sales WHERE client_id = $1`, v.saleID(id, "l3")))
}

// Two confirms of one draft at once: the row lock lets one apply and the other answers the
// closed result, so the lines are written once.
func TestVoiceStockConcurrentConfirm(t *testing.T) {
	v := newVoiceShop(t)
	id := v.speak(v.manager, fourKinds, fourKindsReply)
	before := v.writes()
	const n = 4
	codes, statuses := make([]int, n), make([]string, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := v.confirm(v.manager, id, accept("l1"), accept("l2"), accept("l3"), accept("l4"))
			codes[i] = r.code
			statuses[i], _ = r.body["status"].(string)
		}()
	}
	wg.Wait()
	for i := range n {
		assert.Equal(t, http.StatusOK, codes[i], i)
		assert.Equal(t, "confirmed", statuses[i], i)
	}
	assert.Equal(t, before+4, v.writes(), "one sale, its movement, the restock and the count, once")
	assert.Equal(t, 512, v.onHand(v.salt))
	assert.Equal(t, 497, v.onHand(v.butter))
}

// SC-VO-05: an item the model could not match (no candidate, or a ref the list does not
// have) is a line with no item, the spoken name and confidence 0; accepting it is refused
// with "pick_item", and editing in an item applies it.
// SC-VO-06: edited lines follow the per-kind ranges, and an item not in this store is not
// found (a price above MRP is allowed, D-245); a refused confirm writes nothing and keeps the draft.
func TestVoiceStockPickAndEdit(t *testing.T) {
	v := newVoiceShop(t)
	before := v.writes()
	id := v.speak(v.owner, "chhe Parle-G aaye, paanch Maggi bika, butter sattar rupaye", reply{lines: []said{
		{kind: "restock", heard: "chhe Parle-G aaye", qty: qty(6), conf: 0.8},
		{kind: "sale", ref: "c99", heard: "paanch Maggi bika", qty: qty(5), conf: 0.7},
		{kind: "set_price", item: "Amul Butter", heard: "butter sattar rupaye", rupees: rupees(70), conf: 0.9},
	}})
	r := v.get(v.owner, id)
	require.Equal(t, http.StatusOK, r.code, r.raw)
	ls := linesOf(r)
	require.Len(t, ls, 3)
	for _, l := range ls[:2] {
		assert.Nil(t, l["storeItemId"])
		assert.EqualValues(t, 0, l["confidence"])
		assert.Nil(t, l["currentPricePaise"])
	}
	assert.Equal(t, "chhe Parle-G aaye", ls[0]["itemName"])
	assert.EqualValues(t, 7000, ls[2]["pricePaise"])

	bad := []struct {
		name  string
		lines []map[string]any
		field string
		why   string
	}{
		{"accepting an unmatched line", []map[string]any{accept("l1"), discard("l2"), discard("l3")}, "body.lines[0].outcome", "pick_item"},
		{"a restock of 0", []map[string]any{edit("l1", map[string]any{"kind": "restock", "storeItemId": v.salt, "quantity": 0}), discard("l2"), discard("l3")}, "body.lines[0].value.quantity", "range"},
		{"a sale with no quantity", []map[string]any{discard("l1"), edit("l2", map[string]any{"kind": "sale", "storeItemId": v.salt}), discard("l3")}, "body.lines[1].value.quantity", "required"},
		{"a price on a count", []map[string]any{edit("l1", map[string]any{"kind": "count", "storeItemId": v.salt, "quantity": 3, "pricePaise": 100}), discard("l2"), discard("l3")}, "body.lines[0].value.pricePaise", "set_price_only"},
		{"an item of no store", []map[string]any{edit("l1", map[string]any{"kind": "restock", "storeItemId": ids.New(v.a.clk.Now()).String(), "quantity": 6}), discard("l2"), discard("l3")}, "body.lines[0].value.storeItemId", "not_found"},
	}
	for _, b := range bad {
		c := v.confirm(v.owner, id, b.lines...)
		require.Equal(t, http.StatusUnprocessableEntity, c.code, "%s: %s", b.name, c.raw)
		assert.Equal(t, b.why, fieldsOf(c)[b.field], "%s: %s", b.name, c.raw)
	}
	require.Equal(t, before, v.writes(), "a refused confirm writes nothing")
	assert.Equal(t, "ready", v.get(v.owner, id).body["status"])

	c := v.confirm(v.owner, id,
		edit("l1", map[string]any{"kind": "restock", "storeItemId": v.salt, "quantity": 6}),
		discard("l2"),
		edit("l3", map[string]any{"kind": "set_price", "storeItemId": v.butter, "pricePaise": 6100}))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, map[string]string{"l1": "edited", "l2": "discarded", "l3": "edited"}, outcomesOf(c))
	assert.Equal(t, 506, v.onHand(v.salt))
	assert.Equal(t, 6100, v.priceOf(v.butter), "above the MRP of ₹60 (D-245)")
	assert.Equal(t, 0, v.a.count(`SELECT count(*) FROM pos_sales WHERE store_id = $1`, uuidOf(t, v.sid)), "the discarded sale is not written")
}

// SC-VO-07: every voice endpoint is the common 404 while ai_voice_stock_enabled or
// feature_voice_enabled is off, and nothing is stored.
func TestVoiceStockHidden(t *testing.T) {
	v := newVoiceShop(t)
	id := v.speak(v.owner, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})
	drafts := v.a.count(`SELECT count(*) FROM ai_drafts`)
	avail := v.a.call(v.cashier, http.MethodGet, v.S+"/ai/voice-stock", nil, nil)
	require.Equal(t, http.StatusOK, avail.code, avail.raw)
	assert.EqualValues(t, voice.MaxSeconds, avail.body["maxSeconds"])
	assert.Len(t, avail.body["languages"], 5)
	for _, key := range []string{"ai_voice_stock_enabled", "feature_voice_enabled"} {
		v.setting(key, "false")
		assert.Equal(t, http.StatusNotFound, v.a.call(v.cashier, http.MethodGet, v.S+"/ai/voice-stock", nil, nil).code, key)
		assert.Equal(t, http.StatusNotFound, v.upload(v.owner, []byte("do salt bika"), "audio/webm", "").code, key)
		r := v.get(v.owner, id)
		assert.Equal(t, http.StatusNotFound, r.code, key)
		assert.Equal(t, "not_found", r.body["code"])
		assert.Equal(t, http.StatusNotFound, v.confirm(v.owner, id, accept("l1")).code, key)
		v.setting(key, "true")
	}
	assert.Equal(t, drafts, v.a.count(`SELECT count(*) FROM ai_drafts`))
	assert.Equal(t, 0, v.a.count(`SELECT count(*) FROM pos_sales`))
	assert.Equal(t, http.StatusOK, v.get(v.owner, id).code, "back on")
	assert.Equal(t, http.StatusOK, v.a.call(v.owner, http.MethodGet, v.S+"/ai/voice-stock", nil, nil).code, "back on")
}

// SC-VO-08: a recording counts one call against the store's daily AI cap; past it the
// upload is 429 ai_daily_cap_reached with Retry-After and no draft.
func TestVoiceStockDailyCap(t *testing.T) {
	v := newVoiceShop(t)
	v.setting("ai_daily_calls_store", "1")
	v.speak(v.cashier, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})
	r := v.upload(v.cashier, []byte("do salt bika"), "audio/webm", "")
	require.Equal(t, http.StatusTooManyRequests, r.code, r.raw)
	assert.Equal(t, "ai_daily_cap_reached", r.body["code"])
	assert.NotEmpty(t, r.hdr.Get("Retry-After"))
	assert.Equal(t, 1, v.a.count(`SELECT count(*) FROM ai_drafts WHERE feature = 'voice_stock'`))
}

// SC-VO-09: the upload takes one recording of an allowed type within 1 MB and an optional
// language from the list; anything else is refused before any provider call.
func TestVoiceStockUploadRules(t *testing.T) {
	v := newVoiceShop(t)
	cases := []struct {
		name      string
		audio     []byte
		mediaType string
		lang      string
		extra     []string
		code      int
		field     string
	}{
		{"no recording", nil, "", "hi-IN", nil, http.StatusUnprocessableEntity, "body.audio"},
		{"an empty recording", []byte{}, "audio/webm", "", nil, http.StatusUnprocessableEntity, "body.audio"},
		{"not audio", []byte("hello"), "text/plain", "", nil, http.StatusUnprocessableEntity, "body.audio"},
		{"a language off the list", []byte("hello"), "audio/ogg", "fr-FR", nil, http.StatusUnprocessableEntity, "body.language"},
		{"another field", []byte("hello"), "audio/ogg", "", []string{"note"}, http.StatusUnprocessableEntity, "body"},
		{"past 1 MB", bytes.Repeat([]byte("a"), voice.MaxAudioBytes+1), "audio/webm", "", nil, http.StatusRequestEntityTooLarge, ""},
		{"past 15 s by its header", wav(16), "audio/wav", "", nil, http.StatusUnprocessableEntity, "body.audio"},
		{"past 15 s at the top bit rate", bytes.Repeat([]byte("a"), voice.MaxBitrate["audio/webm"]/8*voice.MaxSeconds+1), "audio/webm", "", nil, http.StatusUnprocessableEntity, "body.audio"},
	}
	for _, c := range cases {
		r := v.upload(v.cashier, c.audio, c.mediaType, c.lang, c.extra...)
		require.Equal(t, c.code, r.code, "%s: %s", c.name, r.raw)
		if c.field != "" {
			assert.Contains(t, fieldsOf(r), c.field, c.name)
		}
	}
	assert.Equal(t, 0, v.a.count(`SELECT count(*) FROM ai_drafts`))
	assert.Empty(t, v.stt.langs, "no provider call")
	assert.Equal(t, http.StatusUnauthorized, v.upload("", []byte("x"), "audio/webm", "").code)

	// Every allowed type, and the language reaches the speech provider.
	for _, mt := range voice.MediaTypes {
		v.speak(v.cashier, "ek salt bika "+mt, reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "ek salt bika", qty: qty(1), conf: 0.9}}})
	}
	assert.Equal(t, "hi-IN", v.stt.langs[0])
}

// wav is a 16 kHz 16-bit mono wav of secs seconds of silence.
func wav(secs int) []byte {
	const rate = 32_000
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt \x10\x00\x00\x00\x01\x00\x01\x00\x80\x3e\x00\x00")
	b = binary.LittleEndian.AppendUint32(b, rate)
	b = append(b, 2, 0, 16, 0)
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(secs*rate)) // #nosec G115 -- a test length
	return append(b, make([]byte, secs*rate)...)
}

// The speech call is charged against ai_daily_cost_cap_paise by length whether it answered,
// failed or heard nothing (a recording whose container does not say is charged 15 s), and
// the draft's cost is both calls'.
func TestVoiceStockSpeechCost(t *testing.T) {
	v := newVoiceShop(t)
	spend := func() int {
		return v.a.count(`SELECT coalesce(sum(cost_paise), 0) FROM ai_usage_daily WHERE scope = 'platform'`)
	}
	draftCost := func(id string) int {
		return v.a.count(`SELECT cost_paise FROM ai_drafts WHERE id = $1`, uuidOf(t, id))
	}
	speech := int(client.SpeechCostPaise(voice.MaxSeconds * time.Second))
	require.Positive(t, speech)

	id := v.speak(v.cashier, "ERR", reply{})
	assert.Equal(t, "failed", v.get(v.cashier, id).body["status"])
	assert.Equal(t, speech, spend(), "a speech error is charged")
	assert.Equal(t, speech, draftCost(id))

	id = v.speak(v.cashier, "   ", reply{})
	assert.Equal(t, "empty", v.get(v.cashier, id).body["failure"])
	assert.Equal(t, 2*speech, spend(), "nothing heard is charged")

	id = v.speak(v.cashier, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})
	assert.Equal(t, "ready", v.get(v.cashier, id).body["status"])
	assert.Equal(t, 3*speech+2, spend(), "speech and the model")
	assert.Equal(t, speech+2, draftCost(id))

	// A recording whose header states 3 s is charged 3 s, and the length is kept.
	w := v.upload(v.cashier, wav(3), "audio/wav", "")
	require.Equal(t, http.StatusAccepted, w.code, w.raw)
	assert.Equal(t, 3*speech+2+int(client.SpeechCostPaise(3*time.Second))+2, spend())
	assert.Equal(t, 3000, v.a.count(`SELECT (input_meta->>'durationMs')::int FROM ai_drafts WHERE id = $1`, uuidOf(t, w.body["draftId"].(string))))
}

// A draft that cannot be stored does not use up one of the store's calls: the call is
// counted in the draft's transaction.
func TestVoiceStockReserveRollsBack(t *testing.T) {
	v := newVoiceShop(t)
	calls := func() int {
		return v.a.count(`SELECT coalesce(sum(calls), 0) FROM ai_usage_daily WHERE scope = $1 AND subject = $2`, guard.ScopeStore, uuidOf(t, v.sid))
	}
	v.a.exec(`ALTER TABLE ai_drafts ADD CONSTRAINT tz_test_refuse CHECK (false) NOT VALID`)
	r := v.upload(v.cashier, []byte("do salt bika"), "audio/webm", "")
	assert.Equal(t, http.StatusInternalServerError, r.code, r.raw)
	assert.Equal(t, 0, calls())
	assert.Empty(t, v.stt.langs, "no provider call")

	v.a.exec(`ALTER TABLE ai_drafts DROP CONSTRAINT tz_test_refuse`)
	v.speak(v.cashier, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})
	assert.Equal(t, 1, calls())
}

// SC-VO-10: a failed step fails the draft with a short code and no lines: speech error,
// no tool call, a reply that fails the schema, nothing heard, nothing that is a stock change;
// a draft still processing after 3 minutes is closed as a timeout when read. A failed draft
// cannot be confirmed.
func TestVoiceStockFailures(t *testing.T) {
	v := newVoiceShop(t)
	fails := []struct {
		name, transcript string
		r                reply
		code             string
	}{
		{"speech error", "ERR", reply{}, "error"},
		{"no tool call", "kuch bhi", reply{err: client.ErrNoToolCall}, "invalid"},
		{"refused", "mana kar do", reply{err: client.ErrRefused}, "refused"},
		{"schema", "galat jawab", reply{raw: `{"lines":[{"kind":"steal","item":"c1","unmatched":null,"heard":"x","quantity":1,"price_rupees":null,"confidence":0.5}]}`}, "schema"},
		{"nothing heard", "   ", reply{}, "empty"},
		{"no stock change", "namaste ji", reply{lines: []said{}}, "empty"},
	}
	for _, f := range fails {
		id := v.speak(v.cashier, f.transcript, f.r)
		r := v.get(v.cashier, id)
		require.Equal(t, http.StatusOK, r.code, r.raw)
		assert.Equal(t, "failed", r.body["status"], f.name)
		assert.Equal(t, f.code, r.body["failure"], f.name)
		assert.Empty(t, linesOf(r), f.name)
		c := v.confirm(v.cashier, id, accept("l1"))
		assert.Equal(t, http.StatusConflict, c.code, f.name)
		assert.Equal(t, "invalid_transition", c.body["code"], f.name)
	}

	v.mu.Lock()
	v.hold = true
	v.mu.Unlock()
	id := v.speak(v.cashier, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})
	assert.Equal(t, "processing", v.get(v.cashier, id).body["status"])
	v.a.clk.Advance(voice.StaleAfter + time.Second)
	v.refresh()
	r := v.get(v.cashier, id)
	assert.Equal(t, "failed", r.body["status"])
	assert.Equal(t, "timeout", r.body["failure"])
	assert.Equal(t, 0, v.writes(), "nothing was written")
}

// SC-VO-11: a draft belongs to its store and speaker: another cashier, another store's staff
// and an id that is no draft get the common 404; the owner and managers may read and confirm
// staff drafts. Discarding every line closes the draft as discarded with no writes.
func TestVoiceStockOwnership(t *testing.T) {
	v := newVoiceShop(t)
	id := v.speak(v.owner, "do salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}})

	assert.Equal(t, http.StatusNotFound, v.get(v.cashier, id).code, "the owner's draft, read by a cashier")
	assert.Equal(t, http.StatusNotFound, v.confirm(v.cashier, id, accept("l1")).code)
	assert.Equal(t, http.StatusNotFound, v.get(v.owner, ids.New(v.a.clk.Now()).String()).code)
	assert.Equal(t, http.StatusUnprocessableEntity, v.get(v.owner, "not-an-id").code, "the contract's path check")
	other := v.a.call(v.cashierS2, http.MethodGet, v.S2+"/ai/voice-stock/drafts/"+id, nil, nil)
	assert.Equal(t, http.StatusNotFound, other.code, "the draft is in store S")
	// The owner may act in both stores, but store S's draft is not in S2.
	viaS2 := v.a.call(v.owner, http.MethodPost, v.S2+"/ai/voice-stock/drafts/"+id+"/confirm", map[string]any{"lines": []any{accept("l1")}}, idem(v.a))
	assert.Equal(t, http.StatusNotFound, viaS2.code, viaS2.raw)
	assert.Equal(t, http.StatusNotFound, v.a.call(v.owner, http.MethodGet, v.S2+"/ai/voice-stock/drafts/"+id, nil, nil).code)
	assert.Equal(t, 0, v.a.count(`SELECT count(*) FROM pos_sales`))
	stranger := v.a.call(v.owner2, http.MethodGet, v.drafts()+"/"+id, nil, nil)
	assert.Contains(t, []int{http.StatusForbidden, http.StatusNotFound}, stranger.code)

	mine := v.speak(v.cashier, "teen salt bika", reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "teen salt bika", qty: qty(3), conf: 0.9}}})
	assert.Equal(t, http.StatusOK, v.get(v.manager, mine).code)

	c := v.confirm(v.manager, id, discard("l1"))
	require.Equal(t, http.StatusOK, c.code, c.raw)
	assert.Equal(t, "discarded", c.body["status"])
	assert.Equal(t, map[string]string{"l1": "discarded"}, outcomesOf(c))
	assert.Equal(t, 0, v.a.count(`SELECT count(*) FROM pos_sales`))
}

// SC-VO-12: the transcript is scrubbed before the model sees it or the draft keeps it (a
// phone becomes [phone]); the recording is never stored; logs carry no phone; the hourly
// retention job removes transcripts and heard words after 90 days and keeps the lines.
func TestVoiceStockPrivacyAndRetention(t *testing.T) {
	v := newVoiceShop(t)
	const phone = "9876543210"
	heard := "do salt bika, number 98450 12345"
	scrubbed := "do salt bika, number [phone]"
	id := v.speak(v.cashier, heard, reply{})
	// The fake looks the reply up by the scrubbed transcript the model saw.
	v.llm.mu.Lock()
	require.Len(t, v.llm.seen, 1)
	assert.NotContains(t, strings.ReplaceAll(v.llm.seen[0], " ", ""), phone)
	assert.Contains(t, v.llm.seen[0], scrubbed)
	v.llm.replies[scrubbed] = reply{lines: []said{{kind: "sale", item: "Tata Salt", heard: "do salt bika", qty: qty(2), conf: 0.9}}}
	v.llm.mu.Unlock()
	r := v.get(v.cashier, id)
	assert.Equal(t, "failed", r.body["status"], "the first reply was for the raw words")

	id = v.speak(v.cashier, heard, reply{})
	r = v.get(v.cashier, id)
	require.Equal(t, "ready", r.body["status"], r.raw)
	assert.Equal(t, scrubbed, r.body["transcript"])
	assert.NotContains(t, strings.ReplaceAll(r.raw, " ", ""), phone)
	var meta string
	require.NoError(t, v.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT input_meta::text || coalesce(output::text, '') FROM ai_drafts WHERE id = $1`, uuidOf(t, id)).Scan(&meta))
	assert.NotContains(t, strings.ReplaceAll(meta, " ", ""), phone, "neither the recording nor the phone is stored")
	assert.Contains(t, meta, `"mediaType": "audio/webm"`)
	v.noSecrets(phone)

	// 90 days on, the hourly job drops the words and keeps the lines.
	v.a.clk.Advance(voice.TranscriptRetention + time.Hour)
	run, err := v.runner.RunScheduled(context.Background(), jobs.KindVoiceTranscriptRetention)
	require.NoError(t, err)
	require.NoError(t, run.Err)
	var transcript, heardNow *string
	var lines int
	require.NoError(t, v.a.tdb.OwnerPool.QueryRow(context.Background(), `SELECT output->>'transcript', output->'lines'->0->>'heard', jsonb_array_length(output->'lines')
	  FROM ai_drafts WHERE id = $1`, uuidOf(t, id)).Scan(&transcript, &heardNow, &lines))
	assert.Nil(t, transcript)
	require.NotNil(t, heardNow)
	assert.Empty(t, *heardNow)
	assert.Equal(t, 1, lines)
	n, err := voice.DropTranscripts(context.Background(), v.a.tdb.Pool, v.a.clk.Now())
	require.NoError(t, err)
	assert.Zero(t, n, "the job is idempotent")
}

// The catalogue's errors reach voice as its own: a price edit that lost a race is a 409
// conflict on confirm, not a 500.
func TestVoiceStockErrMapping(t *testing.T) {
	assert.NoError(t, voiceStockErr(nil))
	assert.ErrorIs(t, voiceStockErr(catalogue.ErrStale), voice.ErrStale)
	assert.ErrorIs(t, voiceStockErr(fmt.Errorf("set price: %w", catalogue.ErrStale)), voice.ErrStale)
	assert.ErrorIs(t, voiceStockErr(catalogue.ErrItemNotFound), voice.ErrItemGone)
	assert.ErrorIs(t, voiceStockErr(catalogue.ErrQuantity), voice.ErrQuantity)
	other := errors.New("boom")
	assert.Equal(t, other, voiceStockErr(other))
}
