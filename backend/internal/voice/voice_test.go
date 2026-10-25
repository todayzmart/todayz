package voice

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/voicedb"
	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/drafts"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
)

func ptr[T any](v T) *T { return &v }

func fieldOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	return pe.Fields
}

func TestValidate(t *testing.T) {
	ok := ToolLine{Kind: KindSale, Item: ptr("c1"), Heard: "teen Maggi", Quantity: ptr(3), Confidence: 0.9}
	require.NoError(t, (&ToolOutput{Lines: []ToolLine{ok}}).Validate())
	bad := map[string]func(l *ToolLine){
		"kind":                func(l *ToolLine) { l.Kind = "steal" },
		"ref":                 func(l *ToolLine) { l.Item = ptr("item1") },
		"ref zero":            func(l *ToolLine) { l.Item = ptr("c0") },
		"neither":             func(l *ToolLine) { l.Item = nil },
		"blank unmatched":     func(l *ToolLine) { l.Item, l.Unmatched = nil, ptr("  ") },
		"long unmatched":      func(l *ToolLine) { l.Item, l.Unmatched = nil, ptr(strings.Repeat("a", maxUnmatched+1)) },
		"no heard":            func(l *ToolLine) { l.Heard = "" },
		"long heard":          func(l *ToolLine) { l.Heard = strings.Repeat("अ", maxHeard+1) },
		"confidence":          func(l *ToolLine) { l.Confidence = 1.2 },
		"sale of 0":           func(l *ToolLine) { l.Quantity = ptr(0) },
		"no quantity":         func(l *ToolLine) { l.Quantity = nil },
		"too many":            func(l *ToolLine) { l.Quantity = ptr(MaxQuantity + 1) },
		"price on a sale":     func(l *ToolLine) { l.PriceRupees = ptr(10.0) },
		"set_price with qty":  func(l *ToolLine) { l.Kind, l.PriceRupees = KindSetPrice, ptr(10.0) },
		"set_price no price":  func(l *ToolLine) { l.Kind, l.Quantity = KindSetPrice, nil },
		"set_price 3 decimal": func(l *ToolLine) { l.Kind, l.Quantity, l.PriceRupees = KindSetPrice, nil, ptr(10.005) },
		"set_price too high": func(l *ToolLine) {
			l.Kind, l.Quantity, l.PriceRupees = KindSetPrice, nil, ptr(float64(maxPriceRupees)+1)
		},
	}
	for name, f := range bad {
		l := ok
		f(&l)
		assert.Error(t, (&ToolOutput{Lines: []ToolLine{l}}).Validate(), name)
	}
	count0 := ok
	count0.Kind, count0.Quantity = KindCount, ptr(0)
	assert.NoError(t, (&ToolOutput{Lines: []ToolLine{count0}}).Validate(), "out of stock is a count of 0")
	price := ok
	price.Kind, price.Quantity, price.PriceRupees = KindSetPrice, nil, ptr(14.5)
	assert.NoError(t, (&ToolOutput{Lines: []ToolLine{price}}).Validate())
	unmatched := ok
	unmatched.Item, unmatched.Unmatched = nil, ptr("Parle-G")
	assert.NoError(t, (&ToolOutput{Lines: []ToolLine{unmatched}}).Validate())
	many := make([]ToolLine, MaxLines+1)
	for i := range many {
		many[i] = ok
	}
	assert.Error(t, (&ToolOutput{Lines: many}).Validate())
}

func TestToLines(t *testing.T) {
	maggi, butter := ids.New(time.Unix(1, 0)), ids.New(time.Unix(2, 0))
	cands := []Candidate{{Ref: "c1", ID: maggi, Name: "Maggi Noodles 70 g"}, {Ref: "c2", ID: butter, Name: "Amul Butter 100 g"}}
	out := ToolOutput{Lines: []ToolLine{
		{Kind: KindSale, Item: ptr("c1"), Heard: "3 Maggi becha", Quantity: ptr(3), Confidence: 0.934},
		{Kind: KindRestock, Item: ptr("c9"), Heard: "12 Parle aaye", Quantity: ptr(12), Confidence: 0.8},
		{Kind: KindSetPrice, Item: ptr("c2"), Heard: "butter 58.5", PriceRupees: ptr(58.5), Confidence: 0.9},
		{Kind: KindRestock, Item: nil, Unmatched: ptr("Good Day"), Heard: "Good Day aaye", Quantity: ptr(4), Confidence: 0.6},
		{Kind: KindSale, Item: ptr("c1"), Heard: "Maggi 70 bika", Quantity: ptr(70), Confidence: 0.9},
		{Kind: KindSale, Item: ptr("c2"), Heard: "butter 7", Quantity: ptr(7), Confidence: 0.9},
	}}
	ls := toLines(out, "3 Maggi becha, 12 Parle aaye, butter 58.5, Good Day aaye, Maggi bika, butter 7 karo", cands)
	require.Len(t, ls, 6)
	assert.Equal(t, "l1", ls[0].LineID)
	assert.Equal(t, maggi.String(), *ls[0].StoreItemID)
	assert.Equal(t, "Maggi Noodles 70 g", *ls[0].ItemName)
	assert.InDelta(t, 0.93, ls[0].Confidence, 1e-9)

	assert.Nil(t, ls[1].StoreItemID, "a ref the list does not have is unmatched")
	assert.Equal(t, "12 Parle aaye", *ls[1].ItemName)
	assert.Zero(t, ls[1].Confidence)

	assert.EqualValues(t, 5850, *ls[2].PricePaise, "rupees to paise")
	assert.Nil(t, ls[2].Quantity)
	assert.InDelta(t, 0.9, ls[2].Confidence, 1e-9)

	assert.Equal(t, "Good Day", *ls[3].ItemName)
	assert.Zero(t, ls[3].Confidence)

	assert.InDelta(t, 0.9, ls[4].Confidence, 1e-9, "70 is in the item's name")
	assert.InDelta(t, 0.9, ls[5].Confidence, 1e-9, "7 is in the transcript")

	// A number neither in the transcript nor the item's name caps the confidence.
	ls = toLines(ToolOutput{Lines: []ToolLine{{Kind: KindSale, Item: ptr("c2"), Heard: "butter", Quantity: ptr(9), Confidence: 0.95}}}, "butter 2 bika", cands)
	assert.InDelta(t, LowConfidence, ls[0].Confidence, 1e-9)
	// A transcript in words has nothing to compare with.
	ls = toLines(ToolOutput{Lines: []ToolLine{{Kind: KindSale, Item: ptr("c2"), Heard: "teen butter", Quantity: ptr(3), Confidence: 0.95}}}, "teen butter bika", cands)
	assert.InDelta(t, 0.95, ls[0].Confidence, 1e-9)
}

type fakeLLM struct {
	resp client.Response
	err  error
	req  client.Request
}

func (f *fakeLLM) Complete(_ context.Context, r client.Request) (client.Response, error) {
	f.req = r
	return f.resp, f.err
}

func TestParse(t *testing.T) {
	model := Model(func(k settings.Key) string {
		assert.Equal(t, settings.AiModelFast, k)
		return "claude-haiku-5-5"
	})
	cands := []Candidate{{Ref: "c1", ID: ids.New(time.Unix(1, 0)), Name: "Tata Salt 1 kg"}}
	f := &fakeLLM{resp: client.Response{ToolInput: json.RawMessage(`{"lines":[{"kind":"count","item":"c1","unmatched":null,"heard":"salt khatam","quantity":0,"price_rupees":null,"confidence":0.9}]}`), CostPaise: 3}}
	p, err := Parse(context.Background(), f, model, "salt khatam", cands)
	require.NoError(t, err)
	require.Empty(t, p.Reject)
	require.Len(t, p.Lines, 1)
	assert.Equal(t, KindCount, p.Lines[0].Kind)
	assert.Equal(t, 0, *p.Lines[0].Quantity)
	assert.Equal(t, "claude-haiku-5-5", f.req.Model)
	assert.Equal(t, Feature, f.req.Feature)
	assert.Equal(t, &Tool, f.req.Tool)
	assert.Contains(t, f.req.Messages[0].Content[0].Text, "c1 | Tata Salt 1 kg")
	assert.Contains(t, f.req.Messages[0].Content[0].Text, "<transcript>\nsalt khatam\n</transcript>")
	assert.Contains(t, f.req.System, "record_voice_stock_lines")

	for name, c := range map[string]struct {
		resp   client.Response
		err    error
		reject string
		hard   bool
	}{
		"schema":     {resp: client.Response{ToolInput: json.RawMessage(`{"lines":[{"kind":"steal"}]}`)}, reject: client.RejectSchema},
		"no tool":    {err: client.ErrNoToolCall, reject: client.OutcomeInvalid},
		"refused":    {err: client.ErrRefused, reject: client.OutcomeRefused},
		"truncated":  {err: client.ErrTruncated, reject: client.OutcomeTruncated},
		"overloaded": {err: &client.ProviderError{Status: 529}, reject: client.OutcomeError},
		"limited":    {err: &client.ProviderError{Status: 429}, reject: client.OutcomeRateLimited},
		"our bug":    {err: &client.ProviderError{Status: 400}, hard: true},
	} {
		p, err := Parse(context.Background(), &fakeLLM{resp: c.resp, err: c.err}, model, "x", cands)
		if c.hard {
			assert.Error(t, err, name)
			continue
		}
		require.NoError(t, err, name)
		assert.Equal(t, c.reject, p.Reject, name)
		assert.Empty(t, p.Lines, name)
	}
}

func TestDecide(t *testing.T) {
	item := ids.New(time.Unix(5, 0)).String()
	matched := Line{LineID: "l1", Kind: KindRestock, StoreItemID: &item, Quantity: ptr(4)}
	c, err := decide(drafts.Decision{LineID: "l1", Outcome: drafts.Accepted}, matched, "body.lines[0]")
	require.NoError(t, err)
	assert.Equal(t, KindRestock, c.kind)
	assert.Equal(t, 4, c.qty)

	_, err = decide(drafts.Decision{LineID: "l1", Outcome: drafts.Accepted}, Line{LineID: "l1", Kind: KindSale, Quantity: ptr(1)}, "body.lines[2]")
	assert.Equal(t, map[string]string{"body.lines[2].outcome": "pick_item"}, fieldOf(t, err))

	val := func(v string) drafts.Decision {
		return drafts.Decision{LineID: "l1", Outcome: drafts.Edited, Value: json.RawMessage(v)}
	}
	c, err = decide(val(`{"kind":"set_price","storeItemId":"`+item+`","pricePaise":1450}`), matched, "body.lines[0]")
	require.NoError(t, err)
	assert.Equal(t, KindSetPrice, c.kind)
	assert.EqualValues(t, 1450, c.price)

	for v, want := range map[string]map[string]string{
		`{"kind":"sale","storeItemId":"nope","quantity":1}`:                             {"body.lines[0].value.storeItemId": "ulid"},
		`{"kind":"sale","storeItemId":"` + item + `","quantity":0}`:                     {"body.lines[0].value.quantity": "range"},
		`{"kind":"count","storeItemId":"` + item + `","quantity":10000}`:                {"body.lines[0].value.quantity": "range"},
		`{"kind":"restock","storeItemId":"` + item + `"}`:                               {"body.lines[0].value.quantity": "required"},
		`{"kind":"set_price","storeItemId":"` + item + `"}`:                             {"body.lines[0].value.pricePaise": "required"},
		`{"kind":"set_price","storeItemId":"` + item + `","pricePaise":0}`:              {"body.lines[0].value.pricePaise": "range"},
		`{"kind":"set_price","storeItemId":"` + item + `","pricePaise":5,"quantity":1}`: {"body.lines[0].value.quantity": "not_for_set_price"},
		`{"kind":"count","storeItemId":"` + item + `","quantity":1,"pricePaise":5}`:     {"body.lines[0].value.pricePaise": "set_price_only"},
		`{"kind":"swap","storeItemId":"` + item + `","quantity":1}`:                     {"body.lines[0].value.kind": "enum"},
	} {
		_, err := decide(val(v), matched, "body.lines[0]")
		assert.Equal(t, want, fieldOf(t, err), v)
	}
	c, err = decide(val(`{"kind":"count","storeItemId":"`+item+`","quantity":0}`), matched, "body.lines[0]")
	require.NoError(t, err, "a count of 0 is out of stock")
	assert.Zero(t, c.qty)
}

func TestKindActionCoversEveryKind(t *testing.T) {
	for _, k := range Kinds {
		assert.NotEmpty(t, KindAction[k], k)
	}
	assert.Len(t, KindAction, len(Kinds))
}

func multipartOf(t *testing.T, parts ...[3]string) *multipart.Reader {
	t.Helper()
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	for _, p := range parts {
		h := textproto.MIMEHeader{}
		h.Set("Content-Disposition", `form-data; name="`+p[0]+`"`)
		if p[1] != "" {
			h.Set("Content-Type", p[1])
		}
		pw, err := w.CreatePart(h)
		require.NoError(t, err)
		_, err = pw.Write([]byte(p[2]))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return multipart.NewReader(&b, w.Boundary())
}

func TestReadRecording(t *testing.T) {
	rec, err := readRecording(multipartOf(t, [3]string{"audio", "audio/webm;codecs=opus", "abc"}, [3]string{"language", "", "kn-IN"}))
	require.NoError(t, err)
	assert.Equal(t, Recording{Data: []byte("abc"), MediaType: "audio/webm", Language: "kn-IN"}, rec)

	for name, c := range map[string]struct {
		parts [][3]string
		want  map[string]string
	}{
		"no audio":   {[][3]string{{"language", "", "hi-IN"}}, map[string]string{"body.audio": "required"}},
		"empty":      {[][3]string{{"audio", "audio/ogg", ""}}, map[string]string{"body.audio": "required"}},
		"text":       {[][3]string{{"audio", "text/plain", "x"}}, map[string]string{"body.audio": "media_type"}},
		"no type":    {[][3]string{{"audio", "", "x"}}, map[string]string{"body.audio": "media_type"}},
		"language":   {[][3]string{{"audio", "audio/ogg", "x"}, {"language", "", "en"}}, map[string]string{"body.language": "enum"}},
		"duplicate":  {[][3]string{{"audio", "audio/ogg", "x"}, {"audio", "audio/ogg", "y"}}, map[string]string{"body.audio": "duplicate"}},
		"extra part": {[][3]string{{"audio", "audio/ogg", "x"}, {"note", "", "y"}}, map[string]string{"body": "additional_property"}},
	} {
		_, err := readRecording(multipartOf(t, c.parts...))
		assert.Equal(t, c.want, fieldOf(t, err), name)
	}
	_, err = readRecording(multipartOf(t, [3]string{"audio", "audio/wav", strings.Repeat("a", MaxAudioBytes+1)}))
	var pe *problem.Error
	require.ErrorAs(t, err, &pe)
	assert.Equal(t, genproblems.PayloadTooLarge, pe.Code)
}

func TestCandidateName(t *testing.T) {
	assert.Equal(t, "Amul Butter 100 g", candidateName(voicedb.CandidatesRow{Name: "Amul Butter", PackSize: "100.000", Unit: "g"}))
	assert.Equal(t, "Maggi Masala 70 g", candidateName(voicedb.CandidatesRow{Name: "Maggi", Variant: ptr("Masala"), PackSize: "70", Unit: "g"}))
	assert.Equal(t, "Maggi Masala 70 g", candidateName(voicedb.CandidatesRow{Name: "Maggi Masala 70 g", Variant: ptr("masala"), PackSize: "70.000", Unit: "g"}))
	assert.Equal(t, "Maggi 70g", candidateName(voicedb.CandidatesRow{Name: "Maggi 70g", PackSize: "70", Unit: "g"}))
	assert.Equal(t, "Aavin Milk 0.5 l", candidateName(voicedb.CandidatesRow{Name: "Aavin Milk", PackSize: "0.500", Unit: "l"}))
	assert.Equal(t, "Bread call [phone] 1 pc", candidateName(voicedb.CandidatesRow{Name: "Bread call 9876543210", PackSize: "1", Unit: "pc"}), "a phone typed into a name is scrubbed")
}

func TestSmallHelpers(t *testing.T) {
	assert.Equal(t, 0.5, matchedRatio([]Line{{StoreItemID: ptr("x")}, {}}))
	assert.Zero(t, matchedRatio(nil))
	assert.Equal(t, "अब", truncate("अबक", 2))
	assert.Equal(t, "ab", truncate("ab", 5))
	for mt, ext := range map[string]string{"audio/webm": ".webm", "audio/ogg": ".ogg", "audio/wav": ".wav", "audio/mpeg": ".mp3", "audio/mp4": ".m4a"} {
		assert.Equal(t, ext, extension(mt))
	}
	assert.Len(t, MediaTypes, 5)
}
