package notify

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/notice"
)

func TestTemplatesListEveryKindInEveryLanguage(t *testing.T) {
	ts := Templates()
	kinds := make([]string, 0, len(ts))
	for _, tm := range ts {
		kinds = append(kinds, tm.Kind)
		def := catalogue[tm.Kind]
		require.Len(t, tm.Texts, len(Langs)*(1+len(def.Variants)), tm.Kind)
		for _, tx := range tm.Texts {
			want, ok := textFor(tm.Kind, tx.Lang, map[string]string{"variant": tx.Variant})
			require.True(t, ok)
			// Unrendered, the text is what textFor renders with no params
			// once the placeholders are dropped.
			require.Equal(t, want.Title, render(tx.Title, nil), "%s %s %s", tm.Kind, tx.Variant, tx.Lang)
			require.Equal(t, want.Body, render(tx.Body, nil), "%s %s %s", tm.Kind, tx.Variant, tx.Lang)
		}
	}
	require.Equal(t, Kinds(), kinds)

	unseen := find(t, ts, notice.KindOrderUnseen)
	require.Equal(t, AppBusiness, unseen.App)
	require.Equal(t, []ProviderTemplate{{"en", "tz_order_unseen"}, {"hi", "tz_order_unseen_hi"}}, unseen.Providers)
	require.False(t, text4(t, unseen, "hi", "").Fallback)
	require.True(t, text4(t, unseen, "kn", "").Fallback)
	require.Equal(t, catalogue[notice.KindOrderUnseen].Text["en"].Body, text4(t, unseen, "kn", "").Body)

	upd := find(t, ts, notice.KindOrderUpdate)
	require.False(t, text4(t, upd, "hi", "dispatch_timeout").Fallback)
	require.True(t, text4(t, upd, "hi", "ready").Fallback)
}

func TestTemplatesAreCopies(t *testing.T) {
	ts := Templates()
	unseen := find(t, ts, notice.KindOrderUnseen)
	unseen.Channels[0] = notice.Push
	unseen.Params[0] = "phone"
	unseen.Texts[0].Body = "changed"
	unseen.Providers[0].Name = "changed"

	def := catalogue[notice.KindOrderUnseen]
	require.Equal(t, notice.SMS, def.Channels[0])
	require.Equal(t, "store", def.Params[0])
	require.Equal(t, "tz_order_unseen", def.Templates["en"])
	again := find(t, Templates(), notice.KindOrderUnseen)
	require.Equal(t, def.Text["en"].Body, text4(t, again, "en", "").Body)
}

func find(t *testing.T, ts []Template, kind string) Template {
	t.Helper()
	for _, tm := range ts {
		if tm.Kind == kind {
			return tm
		}
	}
	t.Fatalf("no template %s", kind)
	return Template{}
}

func text4(t *testing.T, tm Template, lang, variant string) TemplateText {
	t.Helper()
	for _, tx := range tm.Texts {
		if tx.Lang == lang && tx.Variant == variant {
			return tx
		}
	}
	t.Fatalf("%s: no %s text %q", tm.Kind, lang, variant)
	return TemplateText{}
}
