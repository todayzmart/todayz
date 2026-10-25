package coach

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/todayz/backend/internal/coach/detectors"
)

// The rule text of every card (11 §2.1 step 4): one template per detector,
// variant and language. It is the card's text when ai_coach_enabled is off,
// when the AI cap is reached, and when the model's wording fails a check.
// A template uses only the fact's numbers ({key}), so its text is grounded
// by construction; TestTemplatesAreGrounded holds every one to that.
//
// {key|one|many} picks the singular when the value is 1 (English only).
//
// TODO(lead): hi, kn, te and ta wording is a first draft and has not been
// read by a native speaker.

type tmpl struct{ Title, Body string }

// Langs are the card languages (coach_suggestions.lang).
var Langs = []string{"en", "hi", "kn", "te", "ta"}

func langOf(s string) string {
	for _, l := range Langs {
		if s == l {
			return l
		}
	}
	return "en"
}

// templates: detector → variant → language.
var templates = map[string]map[string]map[string]tmpl{
	"top_seller_low_days_of_cover": {"": {
		"en": {"{item} is running out", "{days} {days|day|days} of stock left at the current pace. Restock {qty}."},
		"hi": {"{item} खत्म होने वाला है", "इसी रफ़्तार से {days} दिन का स्टॉक बचा है। {qty} मँगाइए।"},
		"kn": {"{item} ಮುಗಿಯುತ್ತಿದೆ", "ಈಗಿನ ವೇಗದಲ್ಲಿ {days} ದಿನಗಳ ಸ್ಟಾಕ್ ಉಳಿದಿದೆ. {qty} ತರಿಸಿ."},
		"te": {"{item} అయిపోతోంది", "ఇదే వేగంతో {days} రోజుల స్టాక్ మిగిలి ఉంది. {qty} తెప్పించండి."},
		"ta": {"{item} தீர்ந்து வருகிறது", "இதே வேகத்தில் {days} நாள் ஸ்டாக் உள்ளது. {qty} வாங்குங்கள்."},
	}},
	"slow_mover_21d": {
		"": {
			"en": {"{item} is not selling", "No sale in {days} days, {onHand} on the shelf. Try an offer?"},
			"hi": {"{item} बिक नहीं रहा", "{days} दिन से कोई बिक्री नहीं, शेल्फ़ पर {onHand}। ऑफ़र लगाएँ?"},
			"kn": {"{item} ಮಾರಾಟವಾಗುತ್ತಿಲ್ಲ", "{days} ದಿನಗಳಿಂದ ಮಾರಾಟವಿಲ್ಲ, ಶೆಲ್ಫ್‌ನಲ್ಲಿ {onHand}. ಆಫರ್ ಹಾಕುವಿರಾ?"},
			"te": {"{item} అమ్ముడవడం లేదు", "{days} రోజులుగా అమ్మకం లేదు, షెల్ఫ్‌పై {onHand}. ఆఫర్ పెడతారా?"},
			"ta": {"{item} விற்கவில்லை", "{days} நாளாக விற்பனை இல்லை, அலமாரியில் {onHand}. ஆஃபர் போடலாமா?"},
		},
		"pct": {
			"en": {"{item} is not selling", "No sale in {days} days, {onHand} on the shelf. Try {pct}% off?"},
			"hi": {"{item} बिक नहीं रहा", "{days} दिन से कोई बिक्री नहीं, शेल्फ़ पर {onHand}। {pct}% छूट दें?"},
			"kn": {"{item} ಮಾರಾಟವಾಗುತ್ತಿಲ್ಲ", "{days} ದಿನಗಳಿಂದ ಮಾರಾಟವಿಲ್ಲ, ಶೆಲ್ಫ್‌ನಲ್ಲಿ {onHand}. {pct}% ರಿಯಾಯಿತಿ ಕೊಡುವಿರಾ?"},
			"te": {"{item} అమ్ముడవడం లేదు", "{days} రోజులుగా అమ్మకం లేదు, షెల్ఫ్‌పై {onHand}. {pct}% తగ్గింపు ఇస్తారా?"},
			"ta": {"{item} விற்கவில்லை", "{days} நாளாக விற்பனை இல்லை, அலமாரியில் {onHand}. {pct}% தள்ளுபடி தரலாமா?"},
		},
	},
	"expiring_batch_value": {
		"": {
			"en": {"{item} expires soon", "A batch expires within {withinDays} days. A flash offer can sell it first."},
			"hi": {"{item} जल्द एक्सपायर होगा", "एक बैच {withinDays} दिन में एक्सपायर होगा। फ़्लैश ऑफ़र से पहले बिक सकता है।"},
			"kn": {"{item} ಬೇಗ ಅವಧಿ ಮೀರುತ್ತದೆ", "ಒಂದು ಬ್ಯಾಚ್ {withinDays} ದಿನಗಳಲ್ಲಿ ಅವಧಿ ಮೀರುತ್ತದೆ. ಫ್ಲ್ಯಾಶ್ ಆಫರ್‌ನಿಂದ ಮೊದಲೇ ಮಾರಬಹುದು."},
			"te": {"{item} త్వరలో గడువు తీరుతుంది", "ఒక బ్యాచ్ {withinDays} రోజుల్లో గడువు తీరుతుంది. ఫ్లాష్ ఆఫర్‌తో ముందే అమ్మవచ్చు."},
			"ta": {"{item} விரைவில் காலாவதியாகும்", "ஒரு பேட்ச் {withinDays} நாளில் காலாவதியாகும். ஃப்ளாஷ் ஆஃபரில் முன்பே விற்கலாம்."},
		},
		"many": {
			"en": {"{count} batches expire soon", "{count} batches expire within {withinDays} days, {item} first. A flash offer can sell them before then."},
			"hi": {"{count} बैच जल्द एक्सपायर होंगे", "{count} बैच {withinDays} दिन में एक्सपायर होंगे, पहले {item}। फ़्लैश ऑफ़र से उससे पहले बिक सकते हैं।"},
			"kn": {"{count} ಬ್ಯಾಚ್‌ಗಳು ಬೇಗ ಅವಧಿ ಮೀರುತ್ತವೆ", "{count} ಬ್ಯಾಚ್‌ಗಳು {withinDays} ದಿನಗಳಲ್ಲಿ ಅವಧಿ ಮೀರುತ್ತವೆ, ಮೊದಲು {item}. ಫ್ಲ್ಯಾಶ್ ಆಫರ್‌ನಿಂದ ಅದಕ್ಕೂ ಮುನ್ನ ಮಾರಬಹುದು."},
			"te": {"{count} బ్యాచ్‌లు త్వరలో గడువు తీరుతాయి", "{count} బ్యాచ్‌లు {withinDays} రోజుల్లో గడువు తీరుతాయి, ముందుగా {item}. ఫ్లాష్ ఆఫర్‌తో అంతకుముందే అమ్మవచ్చు."},
			"ta": {"{count} பேட்ச்கள் விரைவில் காலாவதியாகும்", "{count} பேட்ச்கள் {withinDays} நாளில் காலாவதியாகும், முதலில் {item}. ஃப்ளாஷ் ஆஃபரில் அதற்கு முன் விற்கலாம்."},
		},
	},
	"wanted_nearby": {
		"": {
			"en": {"People nearby want {item}", "{searches} searches within {km} in the last {periodDays} days. Add it to your shop?"},
			"hi": {"पास के लोग {item} ढूँढ रहे हैं", "पिछले {periodDays} दिन में {km} के अंदर {searches} खोजें। इसे अपनी दुकान में जोड़ें?"},
			"kn": {"ಹತ್ತಿರದವರು {item} ಹುಡುಕುತ್ತಿದ್ದಾರೆ", "ಕಳೆದ {periodDays} ದಿನಗಳಲ್ಲಿ {km} ಒಳಗೆ {searches} ಹುಡುಕಾಟಗಳು. ನಿಮ್ಮ ಅಂಗಡಿಗೆ ಸೇರಿಸುವಿರಾ?"},
			"te": {"దగ్గరివారు {item} కోసం చూస్తున్నారు", "గత {periodDays} రోజుల్లో {km} లోపల {searches} శోధనలు. మీ షాప్‌లో చేర్చుతారా?"},
			"ta": {"அருகில் உள்ளவர்கள் {item} தேடுகிறார்கள்", "கடந்த {periodDays} நாளில் {km} க்குள் {searches} தேடல்கள். உங்கள் கடையில் சேர்க்கலாமா?"},
		},
		"none_nearby": {
			"en": {"People nearby want {item}", "{searches} searches within {km} in the last {periodDays} days, and no shop nearby has it. Add it?"},
			"hi": {"पास के लोग {item} ढूँढ रहे हैं", "पिछले {periodDays} दिन में {km} के अंदर {searches} खोजें, और पास की किसी दुकान में नहीं है। जोड़ें?"},
			"kn": {"ಹತ್ತಿರದವರು {item} ಹುಡುಕುತ್ತಿದ್ದಾರೆ", "ಕಳೆದ {periodDays} ದಿನಗಳಲ್ಲಿ {km} ಒಳಗೆ {searches} ಹುಡುಕಾಟಗಳು, ಹತ್ತಿರದ ಯಾವ ಅಂಗಡಿಯಲ್ಲೂ ಇಲ್ಲ. ಸೇರಿಸುವಿರಾ?"},
			"te": {"దగ్గరివారు {item} కోసం చూస్తున్నారు", "గత {periodDays} రోజుల్లో {km} లోపల {searches} శోధనలు, దగ్గరి ఏ షాప్‌లోనూ లేదు. చేర్చుతారా?"},
			"ta": {"அருகில் உள்ளவர்கள் {item} தேடுகிறார்கள்", "கடந்த {periodDays} நாளில் {km} க்குள் {searches} தேடல்கள், அருகில் எந்தக் கடையிலும் இல்லை. சேர்க்கலாமா?"},
		},
	},
	"missed_sales": {"": {
		"en": {"{item} was out of stock", "Out of stock {zeroDays} {zeroDays|day|days}; {searches} searches nearby matched it. Restock?"},
		"hi": {"{item} स्टॉक में नहीं था", "{zeroDays} दिन स्टॉक खत्म रहा; पास में {searches} खोजें इससे मेल खाईं। मँगाएँ?"},
		"kn": {"{item} ಸ್ಟಾಕ್‌ನಲ್ಲಿ ಇರಲಿಲ್ಲ", "{zeroDays} ದಿನ ಸ್ಟಾಕ್ ಇರಲಿಲ್ಲ; ಹತ್ತಿರದ {searches} ಹುಡುಕಾಟಗಳು ಇದಕ್ಕೆ ಹೊಂದಿದ್ದವು. ತರಿಸುವಿರಾ?"},
		"te": {"{item} స్టాక్‌లో లేదు", "{zeroDays} రోజులు స్టాక్ లేదు; దగ్గరలో {searches} శోధనలు దీనికి సరిపోయాయి. తెప్పిస్తారా?"},
		"ta": {"{item} ஸ்டாக்கில் இல்லை", "{zeroDays} நாள் ஸ்டாக் இல்லை; அருகில் {searches} தேடல்கள் இதற்குப் பொருந்தின. வாங்கலாமா?"},
	}},
	"expired_on_shelf": {"": {
		"en": {"{item} has expired", "{qty} on the shelf expired {daysAgo} {daysAgo|day|days} ago. Mark them expired so nobody buys them."},
		"hi": {"{item} एक्सपायर हो गया", "शेल्फ़ पर {qty} पीस {daysAgo} दिन पहले एक्सपायर हुए। एक्सपायर मार्क करें ताकि कोई न खरीदे।"},
		"kn": {"{item} ಅವಧಿ ಮೀರಿದೆ", "ಶೆಲ್ಫ್‌ನಲ್ಲಿರುವ {qty} {daysAgo} ದಿನಗಳ ಹಿಂದೆ ಅವಧಿ ಮೀರಿವೆ. ಯಾರೂ ಕೊಳ್ಳದಂತೆ ಗುರುತಿಸಿ."},
		"te": {"{item} గడువు తీరింది", "షెల్ఫ్‌పై {qty} {daysAgo} రోజుల క్రితం గడువు తీరాయి. ఎవరూ కొనకుండా గడువు తీరినవిగా గుర్తించండి."},
		"ta": {"{item} காலாவதியானது", "அலமாரியில் உள்ள {qty} {daysAgo} நாள் முன் காலாவதியானது. யாரும் வாங்காதபடி குறியிடுங்கள்."},
	}},
	"selling_below_cost": {
		"": {
			"en": {"{item} sells below cost", "Price {price}, cost {cost}. Check the price?"},
			"hi": {"{item} लागत से कम पर बिक रहा है", "दाम {price}, लागत {cost}। दाम जाँचें?"},
			"kn": {"{item} ವೆಚ್ಚಕ್ಕಿಂತ ಕಡಿಮೆಗೆ ಮಾರಾಟ", "ಬೆಲೆ {price}, ವೆಚ್ಚ {cost}. ಬೆಲೆ ಪರಿಶೀಲಿಸುವಿರಾ?"},
			"te": {"{item} ఖర్చు కంటే తక్కువకు అమ్ముడవుతోంది", "ధర {price}, ఖర్చు {cost}. ధర చూస్తారా?"},
			"ta": {"{item} அடக்க விலைக்குக் கீழே விற்கிறது", "விலை {price}, அடக்கம் {cost}. விலையைச் சரிபார்க்கலாமா?"},
		},
		"check_cost": {
			"en": {"Check the cost of {item}", "Its cost {cost} is above its price {price}. Is the cost right?"},
			"hi": {"{item} की लागत जाँचें", "लागत {cost} दाम {price} से ज़्यादा है। क्या लागत सही है?"},
			"kn": {"{item} ವೆಚ್ಚ ಪರಿಶೀಲಿಸಿ", "ವೆಚ್ಚ {cost} ಬೆಲೆ {price} ಗಿಂತ ಹೆಚ್ಚು. ವೆಚ್ಚ ಸರಿಯೇ?"},
			"te": {"{item} ఖర్చు చూడండి", "ఖర్చు {cost} ధర {price} కంటే ఎక్కువ. ఖర్చు సరైనదేనా?"},
			"ta": {"{item} அடக்கத்தைச் சரிபாருங்கள்", "அடக்கம் {cost} விலை {price} ஐ விட அதிகம். அடக்கம் சரியா?"},
		},
	},
	"stale_stock_share": {"": {
		"en": {"{count} {count|item|items} not confirmed", "Not confirmed in {days} days, so customers see 'Stock not confirmed recently'. Count them?"},
		"hi": {"{count} आइटम पक्के नहीं", "{days} दिन से पक्के नहीं किए, ग्राहक 'हाल में स्टॉक पक्का नहीं' देखते हैं। गिनती करें?"},
		"kn": {"{count} ಐಟಂಗಳು ದೃಢಪಟ್ಟಿಲ್ಲ", "{days} ದಿನಗಳಿಂದ ದೃಢಪಡಿಸಿಲ್ಲ, ಗ್ರಾಹಕರಿಗೆ 'ಇತ್ತೀಚೆಗೆ ಸ್ಟಾಕ್ ದೃಢಪಟ್ಟಿಲ್ಲ' ಕಾಣುತ್ತದೆ. ಎಣಿಸುವಿರಾ?"},
		"te": {"{count} ఐటమ్‌లు నిర్ధారించలేదు", "{days} రోజులుగా నిర్ధారించలేదు, కస్టమర్లకు 'ఇటీవల స్టాక్ నిర్ధారించలేదు' కనిపిస్తుంది. లెక్కిస్తారా?"},
		"ta": {"{count} பொருட்கள் உறுதிசெய்யப்படவில்லை", "{days} நாளாக உறுதிசெய்யவில்லை, வாடிக்கையாளர்கள் 'சமீபத்தில் ஸ்டாக் உறுதிசெய்யப்படவில்லை' என்று பார்க்கிறார்கள். எண்ணலாமா?"},
	}},
	"bought_together": {"": {
		"en": {"{itemA} and {itemB} sell together", "One in every {oneIn} bills with {itemA} also has {itemB}. Try a combo offer?"},
		"hi": {"{itemA} और {itemB} साथ बिकते हैं", "{itemA} वाले हर {oneIn} बिल में से एक में {itemB} भी है। कॉम्बो ऑफ़र आज़माएँ?"},
		"kn": {"{itemA} ಮತ್ತು {itemB} ಜೊತೆಗೆ ಮಾರಾಟ", "{itemA} ಇರುವ ಪ್ರತಿ {oneIn} ಬಿಲ್‌ಗಳಲ್ಲಿ ಒಂದರಲ್ಲಿ {itemB} ಕೂಡ ಇದೆ. ಕಾಂಬೋ ಆಫರ್ ಹಾಕುವಿರಾ?"},
		"te": {"{itemA}, {itemB} కలిసి అమ్ముడవుతాయి", "{itemA} ఉన్న ప్రతి {oneIn} బిల్లుల్లో ఒకదానిలో {itemB} కూడా ఉంది. కాంబో ఆఫర్ పెడతారా?"},
		"ta": {"{itemA}, {itemB} சேர்ந்து விற்கின்றன", "{itemA} உள்ள ஒவ்வொரு {oneIn} பில்லில் ஒன்றில் {itemB} உம் உள்ளது. காம்போ ஆஃபர் போடலாமா?"},
	}},
	"regulars_slipping": {"": {
		"en": {"{count} {count|regular|regulars} stopped coming", "No visit in {lapseDays} days. See who they are?"},
		"hi": {"{count} नियमित ग्राहक नहीं आ रहे", "{lapseDays} दिन से नहीं आए। देखें कौन हैं?"},
		"kn": {"{count} ಖಾಯಂ ಗ್ರಾಹಕರು ಬರುತ್ತಿಲ್ಲ", "{lapseDays} ದಿನಗಳಿಂದ ಬಂದಿಲ್ಲ. ಯಾರೆಂದು ನೋಡುವಿರಾ?"},
		"te": {"{count} రెగ్యులర్ కస్టమర్లు రావడం లేదు", "{lapseDays} రోజులుగా రాలేదు. ఎవరో చూస్తారా?"},
		"ta": {"{count} வழக்கமான வாடிக்கையாளர்கள் வரவில்லை", "{lapseDays} நாளாக வரவில்லை. யார் என்று பார்க்கலாமா?"},
	}},
	"missed_orders_peak_hour": {
		"": {
			"en": {"{count} {count|order|orders} expired yesterday", "Nobody accepted within {minutes} minutes ({window}). Turn on auto-accept for in-stock items?"},
			"hi": {"कल {count} ऑर्डर एक्सपायर हुए", "{minutes} मिनट में किसी ने स्वीकार नहीं किया ({window})। स्टॉक वाले आइटम के लिए ऑटो-एक्सेप्ट चालू करें?"},
			"kn": {"ನಿನ್ನೆ {count} ಆರ್ಡರ್‌ಗಳು ಅವಧಿ ಮೀರಿದವು", "{minutes} ನಿಮಿಷಗಳಲ್ಲಿ ಯಾರೂ ಸ್ವೀಕರಿಸಲಿಲ್ಲ ({window}). ಸ್ಟಾಕ್ ಇರುವ ಐಟಂಗಳಿಗೆ ಆಟೋ-ಅಕ್ಸೆಪ್ಟ್ ಆನ್ ಮಾಡುವಿರಾ?"},
			"te": {"నిన్న {count} ఆర్డర్లు గడువు తీరాయి", "{minutes} నిమిషాల్లో ఎవరూ అంగీకరించలేదు ({window}). స్టాక్ ఉన్న ఐటమ్‌లకు ఆటో-యాక్సెప్ట్ ఆన్ చేస్తారా?"},
			"ta": {"நேற்று {count} ஆர்டர்கள் காலாவதியானது", "{minutes} நிமிடத்தில் யாரும் ஏற்கவில்லை ({window}). ஸ்டாக் உள்ள பொருட்களுக்கு ஆட்டோ-ஏற்பை இயக்கலாமா?"},
		},
		"auto_on": {
			"en": {"{count} {count|order|orders} expired yesterday", "Nobody accepted within {minutes} minutes ({window}). Keep the order sound on at busy hours."},
			"hi": {"कल {count} ऑर्डर एक्सपायर हुए", "{minutes} मिनट में किसी ने स्वीकार नहीं किया ({window})। भीड़ के समय ऑर्डर की आवाज़ चालू रखें।"},
			"kn": {"ನಿನ್ನೆ {count} ಆರ್ಡರ್‌ಗಳು ಅವಧಿ ಮೀರಿದವು", "{minutes} ನಿಮಿಷಗಳಲ್ಲಿ ಯಾರೂ ಸ್ವೀಕರಿಸಲಿಲ್ಲ ({window}). ಬ್ಯುಸಿ ಸಮಯದಲ್ಲಿ ಆರ್ಡರ್ ಸೌಂಡ್ ಆನ್ ಇಡಿ."},
			"te": {"నిన్న {count} ఆర్డర్లు గడువు తీరాయి", "{minutes} నిమిషాల్లో ఎవరూ అంగీకరించలేదు ({window}). రద్దీ సమయంలో ఆర్డర్ సౌండ్ ఆన్‌లో ఉంచండి."},
			"ta": {"நேற்று {count} ஆர்டர்கள் காலாவதியானது", "{minutes} நிமிடத்தில் யாரும் ஏற்கவில்லை ({window}). கூட்ட நேரத்தில் ஆர்டர் ஒலியை இயக்கத்தில் வையுங்கள்."},
		},
	},
	"best_day_record": {
		"": {
			"en": {"Best {weekday} in {weeks} weeks", "{sales} in sales, {pct}% above usual."},
			"hi": {"{weeks} हफ़्तों में सबसे अच्छा {weekday}", "{sales} की बिक्री, आम दिनों से {pct}% ज़्यादा।"},
			"kn": {"{weeks} ವಾರಗಳಲ್ಲಿ ಅತ್ಯುತ್ತಮ {weekday}", "{sales} ಮಾರಾಟ, ಸಾಮಾನ್ಯಕ್ಕಿಂತ {pct}% ಹೆಚ್ಚು."},
			"te": {"{weeks} వారాల్లో ఉత్తమ {weekday}", "{sales} అమ్మకాలు, సాధారణం కంటే {pct}% ఎక్కువ."},
			"ta": {"{weeks} வாரங்களில் சிறந்த {weekday}", "{sales} விற்பனை, வழக்கத்தை விட {pct}% அதிகம்."},
		},
		"leaders": {
			"en": {"Best {weekday} in {weeks} weeks", "{sales} in sales, {pct}% above usual. {leaders} led."},
			"hi": {"{weeks} हफ़्तों में सबसे अच्छा {weekday}", "{sales} की बिक्री, आम दिनों से {pct}% ज़्यादा। सबसे आगे: {leaders}।"},
			"kn": {"{weeks} ವಾರಗಳಲ್ಲಿ ಅತ್ಯುತ್ತಮ {weekday}", "{sales} ಮಾರಾಟ, ಸಾಮಾನ್ಯಕ್ಕಿಂತ {pct}% ಹೆಚ್ಚು. ಮುಂದಿದ್ದವು: {leaders}."},
			"te": {"{weeks} వారాల్లో ఉత్తమ {weekday}", "{sales} అమ్మకాలు, సాధారణం కంటే {pct}% ఎక్కువ. ముందున్నవి: {leaders}."},
			"ta": {"{weeks} வாரங்களில் சிறந்த {weekday}", "{sales} விற்பனை, வழக்கத்தை விட {pct}% அதிகம். முன்னணி: {leaders}."},
		},
	},
	"reviews_unanswered": {"": {
		"en": {"{count} {count|review needs|reviews need} a reply", "From the last {days} days. A short reply builds trust."},
		"hi": {"{count} रिव्यू का जवाब बाकी", "पिछले {days} दिन के। छोटा सा जवाब भरोसा बढ़ाता है।"},
		"kn": {"{count} ವಿಮರ್ಶೆಗಳಿಗೆ ಉತ್ತರ ಬೇಕು", "ಕಳೆದ {days} ದಿನಗಳದ್ದು. ಚಿಕ್ಕ ಉತ್ತರ ನಂಬಿಕೆ ಬೆಳೆಸುತ್ತದೆ."},
		"te": {"{count} రివ్యూలకు జవాబు కావాలి", "గత {days} రోజులవి. చిన్న జవాబు నమ్మకాన్ని పెంచుతుంది."},
		"ta": {"{count} மதிப்புரைகளுக்குப் பதில் தேவை", "கடந்த {days} நாளில் வந்தவை. சிறு பதில் நம்பிக்கையை வளர்க்கும்."},
	}},
	"weekly_summary": {
		"up": {
			"en": {"Last week: {sales} in sales", "{bills} bills, {pct}% above your average of the {weeks} weeks before."},
			"hi": {"पिछला हफ़्ता: {sales} की बिक्री", "{bills} बिल, उससे पहले के {weeks} हफ़्तों के औसत से {pct}% ज़्यादा।"},
			"kn": {"ಕಳೆದ ವಾರ: {sales} ಮಾರಾಟ", "{bills} ಬಿಲ್‌ಗಳು, ಅದರ ಹಿಂದಿನ {weeks} ವಾರಗಳ ಸರಾಸರಿಗಿಂತ {pct}% ಹೆಚ್ಚು."},
			"te": {"గత వారం: {sales} అమ్మకాలు", "{bills} బిల్లులు, అంతకుముందు {weeks} వారాల సగటు కంటే {pct}% ఎక్కువ."},
			"ta": {"கடந்த வாரம்: {sales} விற்பனை", "{bills} பில்கள், அதற்கு முந்தைய {weeks} வாரச் சராசரியை விட {pct}% அதிகம்."},
		},
		"down": {
			"en": {"Last week: {sales} in sales", "{bills} bills, {pct}% below your average of the {weeks} weeks before."},
			"hi": {"पिछला हफ़्ता: {sales} की बिक्री", "{bills} बिल, उससे पहले के {weeks} हफ़्तों के औसत से {pct}% कम।"},
			"kn": {"ಕಳೆದ ವಾರ: {sales} ಮಾರಾಟ", "{bills} ಬಿಲ್‌ಗಳು, ಅದರ ಹಿಂದಿನ {weeks} ವಾರಗಳ ಸರಾಸರಿಗಿಂತ {pct}% ಕಡಿಮೆ."},
			"te": {"గత వారం: {sales} అమ్మకాలు", "{bills} బిల్లులు, అంతకుముందు {weeks} వారాల సగటు కంటే {pct}% తక్కువ."},
			"ta": {"கடந்த வாரம்: {sales} விற்பனை", "{bills} பில்கள், அதற்கு முந்தைய {weeks} வாரச் சராசரியை விட {pct}% குறைவு."},
		},
		"flat": {
			"en": {"Last week: {sales} in sales", "{bills} bills, the same as your average of the {weeks} weeks before."},
			"hi": {"पिछला हफ़्ता: {sales} की बिक्री", "{bills} बिल, उससे पहले के {weeks} हफ़्तों के औसत जितना।"},
			"kn": {"ಕಳೆದ ವಾರ: {sales} ಮಾರಾಟ", "{bills} ಬಿಲ್‌ಗಳು, ಅದರ ಹಿಂದಿನ {weeks} ವಾರಗಳ ಸರಾಸರಿಯಷ್ಟೇ."},
			"te": {"గత వారం: {sales} అమ్మకాలు", "{bills} బిల్లులు, అంతకుముందు {weeks} వారాల సగటుతో సమానం."},
			"ta": {"கடந்த வாரம்: {sales} விற்பனை", "{bills} பில்கள், அதற்கு முந்தைய {weeks} வாரச் சராசரிக்குச் சமம்."},
		},
	},
	"rating_drop": {"": {
		"en": {"Your rating fell to {now}", "It was {before}. {count} {count|review|reviews} in the last {days} days. Reply to the low ones?"},
		"hi": {"आपकी रेटिंग {now} पर आ गई", "पहले {before} थी। पिछले {days} दिन में {count} रिव्यू। कम रेटिंग वालों को जवाब दें?"},
		"kn": {"ನಿಮ್ಮ ರೇಟಿಂಗ್ {now} ಗೆ ಇಳಿದಿದೆ", "ಮೊದಲು {before} ಇತ್ತು. ಕಳೆದ {days} ದಿನಗಳಲ್ಲಿ {count} ವಿಮರ್ಶೆಗಳು. ಕಡಿಮೆ ರೇಟಿಂಗ್‌ಗಳಿಗೆ ಉತ್ತರಿಸುವಿರಾ?"},
		"te": {"మీ రేటింగ్ {now} కి పడిపోయింది", "ముందు {before} ఉండేది. గత {days} రోజుల్లో {count} రివ్యూలు. తక్కువ రేటింగ్‌లకు జవాబిస్తారా?"},
		"ta": {"உங்கள் மதிப்பீடு {now} ஆகக் குறைந்தது", "முன்பு {before} இருந்தது. கடந்த {days} நாளில் {count} மதிப்புரைகள். குறைந்தவற்றுக்குப் பதில் சொல்லலாமா?"},
	}},
	"accept_time_vs_benchmark": {"": {
		"en": {"You take {mine} minutes to accept", "Shops nearby take {nearby}. Faster accepts mean fewer missed orders."},
		"hi": {"आप स्वीकार करने में {mine} मिनट लेते हैं", "पास की दुकानें {nearby} मिनट लेती हैं। जल्दी स्वीकार करने से कम ऑर्डर छूटते हैं।"},
		"kn": {"ಸ್ವೀಕರಿಸಲು ನೀವು {mine} ನಿಮಿಷ ತೆಗೆದುಕೊಳ್ಳುತ್ತೀರಿ", "ಹತ್ತಿರದ ಅಂಗಡಿಗಳು {nearby} ನಿಮಿಷ. ಬೇಗ ಸ್ವೀಕರಿಸಿದರೆ ಕಡಿಮೆ ಆರ್ಡರ್ ತಪ್ಪುತ್ತವೆ."},
		"te": {"అంగీకరించడానికి మీకు {mine} నిమిషాలు", "దగ్గరి షాపులకు {nearby} నిమిషాలు. త్వరగా అంగీకరిస్తే తక్కువ ఆర్డర్లు తప్పుతాయి."},
		"ta": {"ஏற்க நீங்கள் {mine} நிமிடம் எடுக்கிறீர்கள்", "அருகிலுள்ள கடைகள் {nearby} நிமிடம். விரைவாக ஏற்றால் குறைவான ஆர்டர்கள் தவறும்."},
	}},
	"setup_incomplete": {
		"items": {
			"en": {"Add your products", "Customers find your shop only once your items are listed."},
			"hi": {"अपने प्रोडक्ट जोड़ें", "आइटम लिस्ट होने पर ही ग्राहक आपकी दुकान ढूँढ पाते हैं।"},
			"kn": {"ನಿಮ್ಮ ಉತ್ಪನ್ನಗಳನ್ನು ಸೇರಿಸಿ", "ಐಟಂಗಳು ಪಟ್ಟಿಯಾದ ಮೇಲೆಯೇ ಗ್ರಾಹಕರು ನಿಮ್ಮ ಅಂಗಡಿ ಕಾಣುತ್ತಾರೆ."},
			"te": {"మీ ప్రోడక్ట్‌లు చేర్చండి", "ఐటమ్‌లు లిస్ట్ అయ్యాకే కస్టమర్లు మీ షాప్‌ను కనుగొంటారు."},
			"ta": {"உங்கள் பொருட்களைச் சேர்க்கவும்", "பொருட்கள் பட்டியலிட்ட பிறகே வாடிக்கையாளர்கள் உங்கள் கடையைக் காண்பார்கள்."},
		},
		"hours": {
			"en": {"Add your opening hours", "Customers see when you are open, and orders come only then."},
			"hi": {"दुकान खुलने का समय जोड़ें", "ग्राहक देखते हैं कि आप कब खुले हैं, और ऑर्डर तभी आते हैं।"},
			"kn": {"ಅಂಗಡಿ ತೆರೆಯುವ ಸಮಯ ಸೇರಿಸಿ", "ನೀವು ಯಾವಾಗ ತೆರೆದಿರುತ್ತೀರಿ ಎಂದು ಗ್ರಾಹಕರು ನೋಡುತ್ತಾರೆ, ಆರ್ಡರ್‌ಗಳು ಆಗ ಮಾತ್ರ ಬರುತ್ತವೆ."},
			"te": {"షాప్ తెరిచే సమయం చేర్చండి", "మీరు ఎప్పుడు తెరిచి ఉంటారో కస్టమర్లు చూస్తారు, ఆర్డర్లు అప్పుడే వస్తాయి."},
			"ta": {"கடை திறக்கும் நேரத்தைச் சேர்க்கவும்", "நீங்கள் எப்போது திறந்திருப்பீர்கள் என வாடிக்கையாளர்கள் பார்ப்பார்கள், ஆர்டர்கள் அப்போதுதான் வரும்."},
		},
		"location": {
			"en": {"Pin your shop on the map", "Search shows shops by distance; without a pin nobody nearby finds you."},
			"hi": {"दुकान को मैप पर पिन करें", "खोज दूरी से दुकानें दिखाती है; पिन के बिना पास के लोग आपको नहीं ढूँढ पाते।"},
			"kn": {"ಅಂಗಡಿಯನ್ನು ನಕ್ಷೆಯಲ್ಲಿ ಪಿನ್ ಮಾಡಿ", "ಹುಡುಕಾಟ ದೂರದ ಪ್ರಕಾರ ಅಂಗಡಿ ತೋರಿಸುತ್ತದೆ; ಪಿನ್ ಇಲ್ಲದೆ ಹತ್ತಿರದವರು ನಿಮ್ಮನ್ನು ಕಾಣರು."},
			"te": {"షాప్‌ను మ్యాప్‌లో పిన్ చేయండి", "శోధన దూరం ప్రకారం షాపులు చూపిస్తుంది; పిన్ లేకపోతే దగ్గరివారు మిమ్మల్ని కనుగొనలేరు."},
			"ta": {"கடையை வரைபடத்தில் பின் செய்யுங்கள்", "தேடல் தூரப்படி கடைகளைக் காட்டும்; பின் இல்லாமல் அருகிலுள்ளவர்கள் உங்களைக் காண மாட்டார்கள்."},
		},
	},
	"expiry_penalty": {
		"": {
			"en": {"Missed orders are lowering you in search", "{pct}% of orders in the last {days} days expired unaccepted (limit {threshold}%). Turn on order sound and auto-accept."},
			"hi": {"छूटे ऑर्डर से आप खोज में नीचे हैं", "पिछले {days} दिन में {pct}% ऑर्डर बिना स्वीकार किए एक्सपायर हुए (सीमा {threshold}%)। ऑर्डर की आवाज़ और ऑटो-एक्सेप्ट चालू करें।"},
			"kn": {"ತಪ್ಪಿದ ಆರ್ಡರ್‌ಗಳಿಂದ ಹುಡುಕಾಟದಲ್ಲಿ ಕೆಳಗಿದ್ದೀರಿ", "ಕಳೆದ {days} ದಿನಗಳಲ್ಲಿ {pct}% ಆರ್ಡರ್‌ಗಳು ಸ್ವೀಕರಿಸದೆ ಅವಧಿ ಮೀರಿದವು (ಮಿತಿ {threshold}%). ಆರ್ಡರ್ ಸೌಂಡ್ ಮತ್ತು ಆಟೋ-ಅಕ್ಸೆಪ್ಟ್ ಆನ್ ಮಾಡಿ."},
			"te": {"తప్పిన ఆర్డర్ల వల్ల శోధనలో కిందకు", "గత {days} రోజుల్లో {pct}% ఆర్డర్లు అంగీకరించకుండా గడువు తీరాయి (పరిమితి {threshold}%). ఆర్డర్ సౌండ్, ఆటో-యాక్సెప్ట్ ఆన్ చేయండి."},
			"ta": {"தவறிய ஆர்டர்களால் தேடலில் கீழே உள்ளீர்கள்", "கடந்த {days} நாளில் {pct}% ஆர்டர்கள் ஏற்கப்படாமல் காலாவதியானது (வரம்பு {threshold}%). ஆர்டர் ஒலி, ஆட்டோ-ஏற்பை இயக்குங்கள்."},
		},
		"counts": {
			"en": {"Missed orders are lowering you in search", "{expired} of {orders} orders in the last {days} days expired unaccepted ({pct}%, limit {threshold}%). Turn on order sound and auto-accept."},
			"hi": {"छूटे ऑर्डर से आप खोज में नीचे हैं", "पिछले {days} दिन में {orders} में से {expired} ऑर्डर बिना स्वीकार किए एक्सपायर हुए ({pct}%, सीमा {threshold}%)। ऑर्डर की आवाज़ और ऑटो-एक्सेप्ट चालू करें।"},
			"kn": {"ತಪ್ಪಿದ ಆರ್ಡರ್‌ಗಳಿಂದ ಹುಡುಕಾಟದಲ್ಲಿ ಕೆಳಗಿದ್ದೀರಿ", "ಕಳೆದ {days} ದಿನಗಳಲ್ಲಿ {orders} ರಲ್ಲಿ {expired} ಆರ್ಡರ್‌ಗಳು ಸ್ವೀಕರಿಸದೆ ಅವಧಿ ಮೀರಿದವು ({pct}%, ಮಿತಿ {threshold}%). ಆರ್ಡರ್ ಸೌಂಡ್, ಆಟೋ-ಅಕ್ಸೆಪ್ಟ್ ಆನ್ ಮಾಡಿ."},
			"te": {"తప్పిన ఆర్డర్ల వల్ల శోధనలో కిందకు", "గత {days} రోజుల్లో {orders} లో {expired} ఆర్డర్లు అంగీకరించకుండా గడువు తీరాయి ({pct}%, పరిమితి {threshold}%). ఆర్డర్ సౌండ్, ఆటో-యాక్సెప్ట్ ఆన్ చేయండి."},
			"ta": {"தவறிய ஆர்டர்களால் தேடலில் கீழே உள்ளீர்கள்", "கடந்த {days} நாளில் {orders} இல் {expired} ஆர்டர்கள் ஏற்கப்படாமல் காலாவதியானது ({pct}%, வரம்பு {threshold}%). ஆர்டர் ஒலி, ஆட்டோ-ஏற்பை இயக்குங்கள்."},
		},
	},
}

// weekdays are the day names a template says, Sunday first.
var weekdays = map[string][7]string{
	"en": {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	"hi": {"रविवार", "सोमवार", "मंगलवार", "बुधवार", "गुरुवार", "शुक्रवार", "शनिवार"},
	"kn": {"ಭಾನುವಾರ", "ಸೋಮವಾರ", "ಮಂಗಳವಾರ", "ಬುಧವಾರ", "ಗುರುವಾರ", "ಶುಕ್ರವಾರ", "ಶನಿವಾರ"},
	"te": {"ఆదివారం", "సోమవారం", "మంగళవారం", "బుధవారం", "గురువారం", "శుక్రవారం", "శనివారం"},
	"ta": {"ஞாயிறு", "திங்கள்", "செவ்வாய்", "புதன்", "வியாழன்", "வெள்ளி", "சனி"},
}

// localWeekday turns an English weekday name into the language's.
func localWeekday(en, lang string) string {
	for d := time.Sunday; d <= time.Saturday; d++ {
		if d.String() == en {
			return weekdays[lang][d]
		}
	}
	return en
}

var rePlaceholder = regexp.MustCompile(`\{([a-zA-Z]+)(?:\|([^|}]*)\|([^}]*))?\}`)

// Text is a card's title and body.
type Text struct{ Title, Body string }

// render fills the fact's template in lang (English when the language or the
// variant has none). A placeholder without a number is an error: the card is
// not written rather than shown with a gap.
func render(f detectors.Fact, lang string) (Text, error) {
	byVariant, ok := templates[f.Detector]
	if !ok {
		return Text{}, fmt.Errorf("coach: no template for %s", f.Detector)
	}
	byLang, ok := byVariant[f.Variant]
	if !ok {
		return Text{}, fmt.Errorf("coach: no template for %s/%s", f.Detector, f.Variant)
	}
	lang = langOf(lang)
	t, ok := byLang[lang]
	if !ok {
		t, lang = byLang["en"], "en"
	}
	var missing []string
	fill := func(s string) string {
		return rePlaceholder.ReplaceAllStringFunc(s, func(m string) string {
			sm := rePlaceholder.FindStringSubmatch(m)
			v, ok := f.Numbers[sm[1]]
			if !ok || v == nil {
				missing = append(missing, sm[1])
				return ""
			}
			if sm[2] != "" || sm[3] != "" {
				if fmt.Sprint(v) == "1" {
					return sm[2]
				}
				return sm[3]
			}
			s := fmt.Sprint(v)
			if sm[1] == "weekday" {
				s = localWeekday(s, lang)
			}
			return s
		})
	}
	out := Text{Title: fit(fill(t.Title), titleMax), Body: fit(fill(t.Body), bodyMax)}
	if len(missing) > 0 {
		return Text{}, fmt.Errorf("coach: %s/%s needs %s", f.Detector, f.Variant, strings.Join(missing, ", "))
	}
	return out, nil
}

// Card text limits (the coach_suggestions checks).
const (
	titleMax = 60  //settings:ok the coach_suggestions title check
	bodyMax  = 200 //settings:ok the coach_suggestions body check
)

// fit keeps s within n characters, cutting at a space so no number is split.
func fit(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	cut := string(r[:n-1])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:") + "…"
}
