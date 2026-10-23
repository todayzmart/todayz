package notify

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	notificationsapi "github.com/todayz/backend/gen/api/notifications"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/notice"
)

func TestCatalogueIsComplete(t *testing.T) {
	require.Equal(t, []string{
		notice.KindAskReply, notice.KindNewAsk, notice.KindNewOrder, notice.KindOrderUnseen, notice.KindOrderUpdate,
	}, Kinds())
	for kind, def := range catalogue {
		require.Contains(t, []string{AppCustomer, AppBusiness}, def.App, kind)
		require.Contains(t, def.Text, "en", "%s: English is the fallback", kind)
		for _, p := range def.Params {
			require.Regexp(t, reParamKey, p, kind)
		}
		check := func(where string, tx text) {
			for _, m := range rePlace.FindAllString(tx.Title+" "+tx.Body, -1) {
				require.Contains(t, def.Params, m[1:len(m)-1], "%s %s: %s is not a declared param", kind, where, m)
			}
			require.NotEmpty(t, tx.Title, "%s %s", kind, where)
			require.NotEmpty(t, tx.Body, "%s %s", kind, where)
		}
		for lang, tx := range def.Text {
			require.Contains(t, Langs, lang, kind)
			check(lang, tx)
		}
		for v, byLang := range def.Variants {
			require.Contains(t, byLang, "en", "%s variant %s", kind, v)
			for lang, tx := range byLang {
				check(v+"/"+lang, tx)
			}
		}
		outside := slices.Contains(def.Channels, notice.SMS) || slices.Contains(def.Channels, notice.WhatsApp)
		if outside {
			require.Contains(t, def.Templates, "en", "%s: an SMS/WhatsApp kind needs an English template", kind)
		}
		for _, ch := range def.Channels {
			require.Contains(t, notice.Channels(), ch, kind)
		}
	}
	// 01 §15 and D-101: SMS is for the unseen order only; asks are push + in-app.
	require.Equal(t, []notice.Channel{notice.SMS, notice.WhatsApp}, catalogue[notice.KindOrderUnseen].Channels)
	require.Equal(t, []notice.Channel{notice.Push}, catalogue[notice.KindNewAsk].Channels)
	require.Equal(t, []notice.Channel{notice.Push}, catalogue[notice.KindAskReply].Channels)
}

func TestLanguages(t *testing.T) {
	for _, c := range [][2]string{{"hi", "hi"}, {" HI ", "hi"}, {"kn", "kn"}, {"fr", "en"}, {"", "en"}, {"hi-IN", "en"}} {
		require.Equal(t, c[1], pickLang(c[0]), c[0])
	}
	for in, want := range map[string]string{
		"hi-IN,hi;q=0.9,en;q=0.8": "hi",
		"fr-FR, ta;q=0.5":         "ta",
		"en-GB,hi":                "en",
		"de, fr":                  "en",
		"":                        "en",
		"KN":                      "kn",
	} {
		require.Equal(t, want, langFromHeader(in), in)
	}
}

func TestTextFor(t *testing.T) {
	p := map[string]string{"store": "Lakshmi Traders"}
	tx, ok := textFor(notice.KindNewOrder, "hi", p)
	require.True(t, ok)
	require.Equal(t, "नया ऑर्डर", tx.Title)
	require.Contains(t, tx.Body, "Lakshmi Traders")

	tx, _ = textFor(notice.KindNewOrder, "kn", p)
	require.Equal(t, "New order", tx.Title, "kn falls back to English")

	tx, _ = textFor(notice.KindOrderUpdate, "hi", map[string]string{"store": "Ganesh", "variant": "ready"})
	require.Equal(t, "Ready for pickup", tx.Title, "a variant without Hindi uses its English, not the kind's Hindi")
	tx, _ = textFor(notice.KindOrderUpdate, "en", map[string]string{"store": "Ganesh", "variant": "nope"})
	require.Equal(t, "Order update", tx.Title, "an unknown variant uses the kind's text")

	tx, _ = textFor(notice.KindNewOrder, "en", nil)
	require.Equal(t, "A new order is waiting at .", tx.Body, "a missing param renders as nothing")
	require.NotContains(t, tx.Body, "{")

	tx, ok = textFor("retired_kind", "en", nil)
	require.False(t, ok)
	require.Equal(t, "retired_kind", tx.Title)

	// A param is text, never markup or a placeholder of its own.
	tx, _ = textFor(notice.KindNewOrder, "en", map[string]string{"store": "{store}"})
	require.Equal(t, "A new order is waiting at {store}.", tx.Body, "rendered once, not recursively")
}

func TestValidParams(t *testing.T) {
	def := catalogue[notice.KindOrderUnseen]
	_, ok := validParams(def, map[string]string{"store": "Lakshmi", "minutes": "3"})
	require.True(t, ok)
	_, ok = validParams(def, nil)
	require.True(t, ok)
	k, ok := validParams(def, map[string]string{"phone": "x"})
	require.False(t, ok)
	require.Equal(t, "phone", k, "an undeclared key is refused")
	k, ok = validParams(def, map[string]string{"store": strings.Repeat("a", maxParamRunes+1)})
	require.False(t, ok)
	require.Equal(t, "store", k)
	_, ok = validParams(def, map[string]string{"store": strings.Repeat("क", maxParamRunes)})
	require.True(t, ok, "the limit is in runes")
	_, ok = validParams(def, map[string]string{"store": "\xff"})
	require.False(t, ok, "invalid UTF-8")
	many := map[string]string{}
	for i := 0; i <= maxParams; i++ {
		many[string(rune('a'+i))] = "x"
	}
	k, ok = validParams(def, many)
	require.False(t, ok)
	require.Equal(t, "params", k)
}

func TestTemplateFor(t *testing.T) {
	def := catalogue[notice.KindOrderUnseen]
	for lang, want := range map[string][2]string{
		"hi": {"tz_order_unseen_hi", "hi"},
		"ta": {"tz_order_unseen", "en"}, // English fallback, labelled English
		"en": {"tz_order_unseen", "en"},
	} {
		name, tl := templateFor(def, lang)
		require.Equal(t, want, [2]string{name, tl}, lang)
	}
}

// Every operation of the notifications tag has an action, keyed as the
// strict server names it (an operation missing from Ops fails closed with
// 500, as the first HTTP test found).
func TestOpsCoverEveryOperation(t *testing.T) {
	it := reflect.TypeOf((*notificationsapi.StrictServerInterface)(nil)).Elem()
	var names []string
	for i := 0; i < it.NumMethod(); i++ {
		names = append(names, it.Method(i).Name)
	}
	require.Len(t, Ops, len(names))
	for _, n := range names {
		require.Contains(t, Ops, n)
	}
	require.Equal(t, policy.InboxView, Ops["ListBusinessInbox"])
	require.Equal(t, policy.MeNotifications, Ops["ListMyNotifications"])
}

func TestSMSSegments(t *testing.T) {
	require.Equal(t, 1, smsSegments(""))
	require.Equal(t, 1, smsSegments(strings.Repeat("a", 160)))
	require.Equal(t, 2, smsSegments(strings.Repeat("a", 161)))
	require.Equal(t, 3, smsSegments(strings.Repeat("a", 307)))
	require.Equal(t, 1, smsSegments(strings.Repeat("क", 70)))
	require.Equal(t, 2, smsSegments(strings.Repeat("क", 71)), "UCS-2")
	tx, _ := textFor(notice.KindOrderUnseen, "hi", map[string]string{"store": "Lakshmi Traders", "minutes": "3"})
	require.GreaterOrEqual(t, smsSegments(tx.Body), 2, "the Hindi text is UCS-2")
}

func TestSanitizeError(t *testing.T) {
	got := sanitizeError("msg91 sms: rejected +919000000011 for 9000000011\nretry")
	require.NotContains(t, got, "90000")
	require.NotContains(t, got, "\n")
	require.Contains(t, got, "msg91 sms: rejected +9190")
	require.LessOrEqual(t, len([]rune(sanitizeError(strings.Repeat("é", 500)))), maxErrorLen)
	require.Equal(t, "status 503", sanitizeError("status 503"))
	require.Equal(t, "abc", clip("abcdef", 3))
	require.Equal(t, "ab", clip("ab", 3))
}
