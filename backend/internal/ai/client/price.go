package client

import (
	"math"
	"time"
)

// Price is a model's list price in paise per million tokens.
type Price struct {
	InputPerMTok  int64
	OutputPerMTok int64
	// LongInputPerMTok and LongOutputPerMTok apply to a call whose prompt is longer than
	// LongPromptTokens (0: the model has one price).
	LongPromptTokens  int
	LongInputPerMTok  int64
	LongOutputPerMTok int64
}

// Prices are the provider list prices converted at ₹88 to the US dollar (W3-10 REPORT open
// question: the rate and the table are code, not settings, because 01 §17 lists neither; a
// price change is a code change). Cache writes cost 1.25× input and cache reads 0.1× input.
var Prices = map[string]Price{
	"claude-haiku-5-5":  {InputPerMTok: 880, OutputPerMTok: 4400, LongPromptTokens: 100_000, LongInputPerMTok: 4400, LongOutputPerMTok: 22_000}, //settings:ok provider list price ($0.10/$0.50, $0.50/$2.50 past 100K)
	"claude-sonnet-5-5": {InputPerMTok: 17_600, OutputPerMTok: 88_000},                                                                          //settings:ok provider list price ($2/$10)
	"claude-opus-5-5":   {InputPerMTok: 35_200, OutputPerMTok: 176_000},                                                                         //settings:ok provider list price ($4/$20)
	"claude-fable-5-1":  {InputPerMTok: 88_000, OutputPerMTok: 440_000},                                                                         //settings:ok provider list price ($10/$50)
}

// fallbackModel prices a model the table does not know at the dearest known price, so an
// unknown model can only make the cost guard stricter.
const fallbackModel = "claude-fable-5-1"

// CostPaise is what a call cost, rounded up to the next paisa.
func CostPaise(model string, u Usage) int64 {
	p, ok := Prices[model]
	if !ok {
		p = Prices[fallbackModel]
	}
	in, out := p.InputPerMTok, p.OutputPerMTok
	prompt := u.InputTokens + u.CacheReadTokens + u.CacheWriteTokens
	if p.LongPromptTokens > 0 && prompt > p.LongPromptTokens {
		in, out = p.LongInputPerMTok, p.LongOutputPerMTok
	}
	// In millionths of a paisa: input, cache writes at 5/4, cache reads at 1/10, output.
	micro := float64(u.InputTokens)*float64(in) +
		float64(u.CacheWriteTokens)*float64(in)*1.25 +
		float64(u.CacheReadTokens)*float64(in)*0.1 +
		float64(u.OutputTokens)*float64(out)
	if micro <= 0 {
		return 0
	}
	return int64(math.Ceil(micro / 1e6))
}

// SpeechPaisePerHour is the speech-to-text list price (Sarvam, ₹30 an hour of audio). Like
// Prices it is code, not a setting: 01 §17 does not list it.
const SpeechPaisePerHour = 3000 //settings:ok provider list price (₹30 per hour)

// SpeechCostPaise is what transcribing d of audio costs, rounded up to the next paisa.
func SpeechCostPaise(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(math.Ceil(float64(SpeechPaisePerHour) * d.Hours()))
}
