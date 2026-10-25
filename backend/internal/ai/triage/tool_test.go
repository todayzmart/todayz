package triage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/client"
	"github.com/todayz/backend/internal/ai/retail/retailtest"
	"github.com/todayz/backend/internal/platform/ids"
)

func ticket(t *testing.T, replies int) Ticket {
	t.Helper()
	id, err := ids.Parse("01J0000000000000000000000R")
	require.NoError(t, err)
	total := int64(45000)
	reason := "Shop closed early, call 9876543210"
	tk := Ticket{
		Number: "SUP-000123", Subject: "Order TZ-104233 not ready, my number is 9876543210", Opener: "customer",
		Category: "other", Priority: "normal",
		Orders: []Order{{ID: id, Ref: "o1", Number: "TZ-104233", Status: "cancelled", TotalPaise: &total, Linked: true,
			Timeline: []Event{{To: "cancelled", By: "store", Reason: &reason, At: "2026-10-20 18:05"}}}},
	}
	for i := 0; i < replies; i++ {
		tk.Thread = append(tk.Thread, Message{From: "opener", Text: "Where is my refund for TZ-104233? Pin 560034."})
	}
	return tk.Finish()
}

func TestFinishScrubsAndKeepsNumbers(t *testing.T) {
	tk := ticket(t, 1)
	assert.Contains(t, tk.Subject, "TZ-104233", "the order number survives the PIN rule")
	assert.NotContains(t, tk.Subject, "9876543210")
	assert.NotContains(t, tk.Thread[0].Text, "560034")
	assert.Contains(t, tk.Thread[0].Text, "TZ-104233")
	assert.NotContains(t, *tk.Orders[0].Timeline[0].Reason, "9876543210")
	assert.NotContains(t, string(tk.JSON()), "01J0000000000000000000000R", "the model never sees an id")

	empty := Ticket{Number: "SUP-000124"}.Finish()
	assert.NotNil(t, empty.Thread)
	assert.NotNil(t, empty.Orders)
}

func TestGrounded(t *testing.T) {
	tk := ticket(t, 1)
	assert.True(t, tk.Grounded("Your order TZ-104233 was cancelled by the shop."))
	assert.True(t, tk.Grounded("The refund of ₹450 is on its way."), "450 rupees is the order's 45000 paise")
	assert.False(t, tk.Grounded("The refund of ₹500 is on its way."))
	assert.False(t, tk.Grounded("Please call us on 9876543210."))
}

func TestWrite(t *testing.T) {
	ctx := context.Background()
	summary := "The customer asks about a refund."
	ok := map[string]any{"category": "order", "priority": "high", "orderRef": "o1", "reason": "A cancelled order.",
		"summary": summary, "reply": "Sorry, order TZ-104233 was cancelled; your refund is on its way."}
	llm := &retailtest.LLM{Reply: ok}

	d, err := Write(ctx, llm, "fake", "ref", ticket(t, 1))
	require.NoError(t, err)
	require.Empty(t, d.Reject)
	cl, _ := d.Output.Line(KindClassify)
	require.NotNil(t, cl.OrderID)
	assert.Equal(t, "01J0000000000000000000000R", *cl.OrderID)
	assert.Equal(t, "TZ-104233", *cl.OrderNumber)
	_, has := d.Output.Line(KindSummary)
	assert.False(t, has, "a short thread gets no summary")

	d, err = Write(ctx, llm, "fake", "ref", ticket(t, SummaryMinReplies))
	require.NoError(t, err)
	s, has := d.Output.Line(KindSummary)
	require.True(t, has)
	assert.Equal(t, summary, s.Text)

	for name, c := range map[string]struct {
		key, value, reject string
	}{
		"order not shown":   {"orderRef", "o7", client.RejectSchema},
		"invented amount":   {"reply", "Your refund of ₹999 is on its way.", client.RejectGrounding},
		"phone in a reply":  {"reply", "Call the shop on 9876543210.", client.RejectGrounding},
		"number in reason":  {"reason", "Late by 3 days.", client.RejectGrounding},
		"invented category": {"category", "refunds", client.RejectSchema},
	} {
		r := map[string]any{}
		for k, v := range ok {
			r[k] = v
		}
		r[c.key] = c.value
		llm.Reply = r
		d, err := Write(ctx, llm, "fake", "ref", ticket(t, SummaryMinReplies))
		require.NoError(t, err, name)
		assert.Equal(t, c.reject, d.Reject, name)
	}
}

func TestAtIsIST(t *testing.T) {
	assert.Equal(t, "2026-10-20 18:05", At(time.Date(2026, 10, 20, 12, 35, 0, 0, time.UTC)))
	assert.Equal(t, "support", FromOpener("admin"))
	assert.Equal(t, "opener", FromOpener("business"))
}
