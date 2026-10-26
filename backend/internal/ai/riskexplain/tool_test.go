package riskexplain

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
)

func flag() Flag {
	return NewFlag("pos.reversal_ratio", "store", 60, "open", []Fact{
		{Key: "reversalPct", Value: "38"},
		{Key: "salesCount", Value: "42"},
		{Key: "refundPaise", Value: "125000"},
		{Key: "pinLike", Value: "PIN 560034"},
		{Key: "storeId", Value: "01J0000000000000000000000S"},
		{Key: "note", Value: "owner said call 9876543210"},
	}, History{Open: 1, Cleared: 2})
}

func TestNewFlag(t *testing.T) {
	f := flag()
	assert.NotContains(t, f.Facts, "storeId", "an id never reaches the model")
	assert.NotContains(t, string(f.JSON()), "01J0000000000000000000000S")
	assert.NotContains(t, f.Facts["note"], "9876543210")
	assert.Equal(t, "38", f.Facts["reversalPct"])
	assert.Equal(t, "125000", f.Facts["refundPaise"], "a bare amount is not a PIN code")
	assert.NotContains(t, f.Facts["pinLike"], "560034")
}

func TestGrounded(t *testing.T) {
	f := flag()
	assert.True(t, f.Grounded("38% of 42 sales were reversed; score 60."))
	assert.True(t, f.Grounded("Refunds came to ₹1250."), "money facts ground in rupees")
	assert.False(t, f.Grounded("40% of sales were reversed."))
	assert.False(t, f.Grounded("Refunds came to ₹1300."))
	assert.False(t, f.Grounded("Call the owner on 9876543210."))
}

func TestWrite(t *testing.T) {
	ctx := context.Background()
	llm := &retailtest.LLM{Reply: map[string]any{"text": "38% of 42 sales were reversed, with 2 earlier flags cleared."}}
	d, err := Write(ctx, llm, "fake", "ref", flag())
	require.NoError(t, err)
	require.Empty(t, d.Reject)
	require.Len(t, d.Output.Lines, 1)
	assert.Equal(t, "l1", d.Output.Lines[0].LineID)

	for text, reject := range map[string]string{
		"   ":                         client.RejectSchema,
		"55% of sales were reversed.": client.RejectGrounding,
	} {
		llm.Reply = map[string]any{"text": text}
		d, err := Write(ctx, llm, "fake", "ref", flag())
		require.NoError(t, err)
		assert.Equal(t, reject, d.Reject, text)
	}
}
