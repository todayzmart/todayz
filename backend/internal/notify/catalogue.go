package notify

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/todayz/backend/internal/platform/notice"
)

// The apps an inbox row belongs to (notifications.app).
const (
	AppCustomer = "customer"
	AppBusiness = "business"
)

// Langs are the notification languages (users.preferred_lang); anything else
// is English.
var Langs = []string{"en", "hi", "kn", "te", "ta"}

// text is a notification's title and body in one language. {name} is
// replaced by the notification's param of that name.
type text struct{ Title, Body string }

// kindDef is one kind of notification (01 §15).
type kindDef struct {
	App string
	// Channels are the outside channels the kind may use; each is a switch
	// in the app's preferences. The in-app inbox has no switch.
	Channels []notice.Channel
	// Params are the names the text may use, in the order a WhatsApp
	// template takes them (body_1, body_2, …). Send refuses any other key.
	Params []string
	// Text per language; "en" is required and is the fallback.
	Text map[string]text
	// Variants replace Text when params["variant"] names one (an order's
	// new status), per language with the same fallback.
	Variants map[string]map[string]text
	// Templates name the SMS/WhatsApp template per language ("en" is the
	// fallback). TODO(lead): these are logical names; the DLT template ids
	// registered with MSG91 replace them in Wave 5 (a mapping from these
	// names, through config, never platform_settings).
	Templates map[string]string
}

// catalogue lists every kind Send accepts. A work package that sends a new
// kind (reviews, payouts, referrals, the hold reminder) adds it here with its
// English text and its channels from 01 §15.
//
// TODO(lead): the Hindi strings are a first draft for review; kn, te and ta
// fall back to English until translations are supplied.
var catalogue = map[string]kindDef{
	notice.KindNewOrder: {
		App: AppBusiness, Channels: []notice.Channel{notice.Push}, Params: []string{"store"},
		Text: map[string]text{
			"en": {"New order", "A new order is waiting at {store}."},
			"hi": {"नया ऑर्डर", "{store} पर एक नया ऑर्डर इंतज़ार कर रहा है।"},
		},
	},
	notice.KindOrderUnseen: {
		// 01 §15: SMS (WhatsApp when enabled, SMS the fallback), once per order.
		App: AppBusiness, Channels: []notice.Channel{notice.SMS, notice.WhatsApp}, Params: []string{"store", "minutes"},
		Text: map[string]text{
			"en": {"Order waiting", "A new order at {store} has not been opened for {minutes} minutes. Open the TodayZ business app to accept it."},
			"hi": {"ऑर्डर इंतज़ार में", "{store} पर एक नया ऑर्डर {minutes} मिनट से खोला नहीं गया है। स्वीकार करने के लिए TodayZ बिज़नेस ऐप खोलें।"},
		},
		Templates: map[string]string{"en": "tz_order_unseen", "hi": "tz_order_unseen_hi"},
	},
	notice.KindNewAsk: {
		// D-101: push + in-app (Asks inbox), no SMS. The push sender is deferred.
		App: AppBusiness, Channels: []notice.Channel{notice.Push}, Params: []string{"store"},
		Text: map[string]text{
			"en": {"New ask", "A customer nearby is asking {store} for an item."},
		},
	},
	notice.KindOrderUpdate: {
		App: AppCustomer, Channels: []notice.Channel{notice.Push}, Params: []string{"store", "variant"},
		Text: map[string]text{
			"en": {"Order update", "Your order from {store} has an update."},
		},
		Variants: map[string]map[string]text{
			"accepted":         {"en": {"Order accepted", "{store} accepted your order."}},
			"partial":          {"en": {"Order partly accepted", "{store} can give only part of your order. Open it to see what is available."}},
			"rejected":         {"en": {"Order not accepted", "{store} could not take your order."}},
			"expired":          {"en": {"Order expired", "{store} did not answer in time, so your order was cancelled."}},
			"ready":            {"en": {"Ready for pickup", "Your order from {store} is ready. Show your pickup code at the counter."}},
			"out_for_delivery": {"en": {"Out for delivery", "Your order from {store} is on its way."}},
			"delivered":        {"en": {"Delivered", "Your order from {store} was delivered."}},
		},
	},
	notice.KindAskReply: {
		// D-101: push for the first Yes of an ask only (the asks domain
		// decides which Send carries the push), in-app for every Yes.
		App: AppCustomer, Channels: []notice.Channel{notice.Push}, Params: []string{"store", "product"},
		Text: map[string]text{
			"en": {"A shop has it", "{store} has {product}."},
		},
	},
}

var (
	reParamKey = regexp.MustCompile(`^[a-z][a-z_]{0,30}$`)
	reRefType  = regexp.MustCompile(`^[a-z][a-z_]{0,30}$`)
	rePlace    = regexp.MustCompile(`\{[a-z][a-z_]{0,30}\}`)
)

// Limits on params: they are stored with the row and rendered on read.
const (
	maxParams     = 8
	maxParamRunes = 120
)

// pickLang returns lang if it is a notification language, else "en".
func pickLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	for _, v := range Langs {
		if l == v {
			return v
		}
	}
	return "en"
}

// langFromHeader picks the first supported language of an Accept-Language
// header in the order the client listed them (browsers list by preference);
// English otherwise.
func langFromHeader(h string) string {
	for _, part := range strings.Split(h, ",") {
		tag, _, _ := strings.Cut(strings.TrimSpace(part), ";")
		primary, _, _ := strings.Cut(tag, "-")
		primary = strings.ToLower(strings.TrimSpace(primary))
		for _, v := range Langs {
			if primary == v {
				return v
			}
		}
	}
	return "en"
}

// textFor returns the kind's text in lang (English fallback). ok is false for
// an unknown kind: a row written before a kind was retired still renders,
// with its kind name as the title.
func textFor(kind, lang string, params map[string]string) (text, bool) {
	def, ok := catalogue[kind]
	if !ok {
		return text{Title: kind}, false
	}
	t := def.Text["en"]
	if l, ok := def.Text[lang]; ok {
		t = l
	}
	if v, ok := def.Variants[params["variant"]]; ok {
		if e, ok := v["en"]; ok {
			t = e
		}
		if l, ok := v[lang]; ok {
			t = l
		}
	}
	return text{Title: render(t.Title, params), Body: render(t.Body, params)}, true
}

// render replaces each {name} with params[name]; a missing param renders as
// nothing rather than as the placeholder.
func render(s string, params map[string]string) string {
	return rePlace.ReplaceAllStringFunc(s, func(m string) string {
		return params[m[1:len(m)-1]]
	})
}

// templateFor is the provider template of kind in lang (English fallback).
func templateFor(def kindDef, lang string) string {
	if t, ok := def.Templates[lang]; ok {
		return t
	}
	return def.Templates["en"]
}

// validParams checks params against the kind's declared names and the size
// limits. It returns the offending key, or "".
func validParams(def kindDef, params map[string]string) (string, bool) {
	if len(params) > maxParams {
		return "params", false
	}
	for k, v := range params {
		if !reParamKey.MatchString(k) || !slices.Contains(def.Params, k) {
			return k, false
		}
		if !utf8.ValidString(v) || utf8.RuneCountInString(v) > maxParamRunes {
			return k, false
		}
	}
	return "", true
}

// Kinds lists the catalogue's kinds, sorted (label registry, tests).
func Kinds() []string { return slices.Sorted(maps.Keys(catalogue)) }
