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
	// fallback). For SMS each name maps to its DLT template id through
	// SMS_NOTICE_TEMPLATE_IDS (D-212), never platform_settings, and the
	// kind's text in that language must stay exactly the registered DLT
	// text (docs/runbooks/aws-founder-steps.md, part C1).
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
	notice.KindDeliveryReturnPending: {
		// D-69: in-app only; the business inbox of the store.
		App: AppBusiness, Params: []string{"store"},
		Text: map[string]text{
			"en": {"Confirm a return", "A failed delivery at {store} is waiting for you to confirm the goods came back."},
			"hi": {"वापसी की पुष्टि करें", "{store} की एक असफल डिलीवरी का सामान वापस आया या नहीं, इसकी पुष्टि बाकी है।"},
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
			"accepted": {"en": {"Order accepted", "{store} accepted your order."}},
			"partial":  {"en": {"Order partly accepted", "{store} can give only part of your order. Open it to see what is available."}},
			"rejected": {"en": {"Order not accepted", "{store} could not take your order."}},
			"expired":  {"en": {"Order expired", "{store} did not answer in time, so your order was cancelled."}},
			"dispatch_timeout": {
				"en": {"Order cancelled", "{store} did not send your order out in time, so it was cancelled."},
				"hi": {"ऑर्डर रद्द हुआ", "{store} ने आपका ऑर्डर समय पर नहीं भेजा, इसलिए इसे रद्द कर दिया गया।"},
			},
			"ready":            {"en": {"Ready for pickup", "Your order from {store} is ready. Show your pickup code at the counter."}},
			"out_for_delivery": {"en": {"Out for delivery", "Your order from {store} is on its way."}},
			"delivered":        {"en": {"Delivered", "Your order from {store} was delivered."}},
			// D-274: a field agent or admin rejected the shop; its open
			// orders are cancelled with no strike and held credits go back.
			"shop_rejected": {
				"en": {"Order cancelled", "{store} can no longer take orders on TodayZ, so your order was cancelled. Any credits you used are back in your wallet."},
				"hi": {"ऑर्डर रद्द हुआ", "{store} अब TodayZ पर ऑर्डर नहीं ले सकती, इसलिए आपका ऑर्डर रद्द कर दिया गया। इस्तेमाल किए गए क्रेडिट आपके वॉलेट में वापस आ गए हैं।"},
				"kn": {"ಆರ್ಡರ್ ರದ್ದಾಗಿದೆ", "{store} ಇನ್ನು ಮುಂದೆ TodayZ ನಲ್ಲಿ ಆರ್ಡರ್ ಸ್ವೀಕರಿಸುವುದಿಲ್ಲ, ಆದ್ದರಿಂದ ನಿಮ್ಮ ಆರ್ಡರ್ ರದ್ದಾಗಿದೆ. ನೀವು ಬಳಸಿದ ಕ್ರೆಡಿಟ್‌ಗಳು ನಿಮ್ಮ ವಾಲೆಟ್‌ಗೆ ಮರಳಿವೆ."},
				"te": {"ఆర్డర్ రద్దయింది", "{store} ఇకపై TodayZలో ఆర్డర్లు తీసుకోదు, కాబట్టి మీ ఆర్డర్ రద్దయింది. మీరు వాడిన క్రెడిట్లు మీ వాలెట్‌కు తిరిగి వచ్చాయి."},
				"ta": {"ஆர்டர் ரத்து செய்யப்பட்டது", "{store} இனி TodayZ-இல் ஆர்டர்களை ஏற்காது, எனவே உங்கள் ஆர்டர் ரத்து செய்யப்பட்டது. நீங்கள் பயன்படுத்திய கிரெடிட்கள் உங்கள் வாலெட்டுக்குத் திரும்பி வந்துவிட்டன."},
			},
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
	notice.KindCreditsEarned: {
		// 01 §15: in-app (+ push) for the customer (W2-01).
		App: AppCustomer, Channels: []notice.Channel{notice.Push}, Params: []string{"credits"},
		Text: map[string]text{
			"en": {"Credits earned", "{credits} credits were added to your TodayZ wallet."},
			"hi": {"क्रेडिट मिले", "आपके TodayZ वॉलेट में {credits} क्रेडिट जोड़े गए।"},
		},
	},
	notice.KindCreditsExpiring: {
		// 01 §15: in-app (+ push) for the customer, credit_expiry_reminder_days
		// before the lot's expiry (W2-01).
		App: AppCustomer, Channels: []notice.Channel{notice.Push}, Params: []string{"credits", "date"},
		Text: map[string]text{
			"en": {"Credits expiring", "{credits} credits in your wallet expire after {date}. Use them on your next order."},
			"hi": {"क्रेडिट समाप्त हो रहे हैं", "आपके वॉलेट के {credits} क्रेडिट {date} के बाद समाप्त हो जाएंगे। अगले ऑर्डर पर उनका उपयोग करें।"},
		},
	},
	notice.KindReferralUnlocked: {
		// 01 §15: "Referral unlocked", in-app + push (W2-03).
		App: AppCustomer, Channels: []notice.Channel{notice.Push}, Params: []string{"credits"},
		Text: map[string]text{
			"en": {"Referral unlocked", "You earned {credits} credits from a referral."},
		},
	},
	notice.KindShopReferralUnlocked: {
		// W2-03: a referral reward reached the store's credit account (in-app + push).
		App: AppBusiness, Channels: []notice.Channel{notice.Push}, Params: []string{"credits"},
		Text: map[string]text{
			"en": {"Referral unlocked", "{credits} credits from a referral were added to your credit account."},
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

// templateFor is the provider template of kind in lang and the language it
// is in: English when lang has no approved template. The message's language
// and text follow the template, since a provider matches a template by name
// and language (a Kannada label on the English template is refused).
func templateFor(def kindDef, lang string) (name, tlang string) {
	if t, ok := def.Templates[lang]; ok {
		return t, lang
	}
	return def.Templates["en"], "en"
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
