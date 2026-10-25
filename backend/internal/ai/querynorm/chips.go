package querynorm

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Chip kinds (DiscoverChipKind, AiSearchChipKind): nothing else is ever suggested.
const (
	KindMaxPrice = "maxPrice"
	KindOpenNow  = "openNow"
	KindPickup   = "pickup"
	KindDelivery = "delivery"
	KindOffers   = "offers"
	KindNearest  = "nearest"
	KindBrand    = "brand"
)

// Kinds are the allowed chip kinds, in the order chips are shown.
var Kinds = []string{KindMaxPrice, KindOpenNow, KindPickup, KindDelivery, KindOffers, KindNearest, KindBrand}

// Fixed numbers (the contract's limits, not settings).
const (
	// MaxChipText bounds a chip's words (AiSearchChip.text maxLength).
	MaxChipText = 80
	// MaxPriceRupees is the highest top price a chip takes (₹1,00,000).
	MaxPriceRupees = 100000
)

// Chip is one refine chip: a filter the query's words name.
type Chip struct {
	Kind string
	// Value is the paise for maxPrice and the brand slug for brand; "" for the others.
	Value string
	// Text is the words the chip came from, as written.
	Text string
}

// Brand is one master-catalogue brand.
type Brand struct{ Name, Slug string }

// Parsed is what the parser read in a query.
type Parsed struct {
	Chips []Chip
	// Rest is the query without the chips' words (what is left to search for).
	Rest string
}

// The deterministic refine-chip parser (01 §11 "Refine chips", D-101): patterns in the five
// languages, in their own scripts and in Latin letters, with the digits of every script.
// Latin words match whole; a word in an Indian script matches at a word start, so a case
// ending ("ಮನೆಗೆ", "வீட்டுக்கு") still matches.

var phrases = map[string][]string{
	KindOpenNow: {
		"open now", "opened now", "open right now", "still open", "currently open", "open shops", "open shop", "open stores",
		"abhi khula", "abhi khuli", "khula hua", "khuli hui", "khuli dukan", "खुला", "खुली", "खुले",
		"tereda", "terediruva", "ತೆರೆದ", "ತೆರೆದಿರುವ", "terichi unna", "తెరిచి", "తెరిచిన", "thirandha", "thiranthirukkum", "திறந்த", "திறந்திருக்கும்",
	},
	KindPickup: {
		"pickup", "pick up", "pick-up", "self pickup", "collect", "takeaway", "take away", "le jaunga", "le jaungi",
		"पिकअप", "ले जाऊंगा", "ले जाऊँगा", "ಪಿಕಪ್", "పికప్", "பிக்கப்",
	},
	KindDelivery: {
		"delivery", "deliver", "delivered", "home delivery", "door delivery", "ghar pe", "ghar par", "ghar tak",
		"डिलीवरी", "होम डिलीवरी", "घर पर", "घर पे", "घर तक", "ಡೆಲಿವರಿ", "ಮನೆಗೆ", "mane ge", "manege",
		"డెలివరీ", "ఇంటికి", "intiki", "டெலிவரி", "வீட்டுக்கு", "veetuku", "veettukku",
	},
	KindOffers: {
		"offer", "offers", "on offer", "discount", "discounts", "deal", "deals", "on sale", "sale", "chhoot", "chhut",
		"ऑफर", "छूट", "डिस्काउंट", "ಆಫರ್", "ರಿಯಾಯಿತಿ", "ಡಿಸ್ಕೌಂಟ್", "ఆఫర్", "తగ్గింపు", "డిస్కౌంట్", "ஆஃபர்", "தள்ளுபடி",
	},
	KindNearest: {
		"near me", "nearby", "nearest", "closest", "close by", "paas mein", "paas me", "aas paas", "nazdeek", "najdeek",
		"पास में", "पास", "नज़दीक", "नजदीक", "आस पास", "hattira", "ಹತ್ತಿರ", "ಹತ್ತಿರದ", "daggara", "దగ్గర", "దగ్గరలో",
		"pakkathula", "pakkathil", "arugil", "பக்கத்துல", "பக்கத்தில்", "அருகில்", "அருகே",
	},
}

// A word character: a letter, a combining mark (the vowel signs of the Indian scripts) or a
// digit.
const wordClass = `\pL\pM\pN`

type pattern struct {
	kind string
	re   *regexp.Regexp
}

var kindPatterns = func() []pattern {
	var out []pattern
	for _, k := range Kinds {
		ps := append([]string(nil), phrases[k]...)
		if len(ps) == 0 {
			continue
		}
		// Longest first, so "home delivery" is read before "delivery".
		sort.Slice(ps, func(i, j int) bool { return len(ps[i]) > len(ps[j]) })
		var latin, native []string
		for _, p := range ps {
			q := strings.ReplaceAll(regexp.QuoteMeta(p), " ", `[\s-]+`)
			if isLatin(p) {
				latin = append(latin, q)
			} else {
				native = append(native, q)
			}
		}
		var alts []string
		if len(latin) > 0 {
			alts = append(alts, `(?:`+strings.Join(latin, "|")+`)(?:$|[^`+wordClass+`])`)
		}
		if len(native) > 0 {
			alts = append(alts, `(?:`+strings.Join(native, "|")+`)[`+wordClass+`]*`)
		}
		out = append(out, pattern{kind: k, re: regexp.MustCompile(`(?i)(?:^|[^` + wordClass + `])((?:` + strings.Join(alts, "|") + `))`)})
	}
	return out
}()

// Currency words around a price, and the words that make it a top price.
const (
	currency = `(?:₹|rs\.?|inr|rupees?|rupaye|rupay|rupaiya|rupaiye|रुपये|रुपए|रुपया|रु\.?|rupayi|ರೂಪಾಯಿ[` + wordClass + `]*|ರೂ\.?|rupayalu|rupayala|రూపాయల[` + wordClass + `]*|రూ\.?|rupai|ரூபாய்[` + wordClass + `]*|ரூ\.?|/-)`
	amount   = `([0-9०-९೦-೯౦-౯௦-௯][0-9०-९೦-೯౦-౯௦-௯,]*)`
	// A unit after the number makes it a pack size, not a price.
	unit   = `(?:kg|kgs|g|gm|gms|gram|grams|ml|l|ltr|litre|litres|liter|pc|pcs|piece|pieces|pack|packs|%|किलो|ग्राम|लीटर|ಕೆಜಿ|ಗ್ರಾಂ|ಲೀಟರ್|కిలో|గ్రాము|లీటర్|கிலோ|கிராம்|லிட்டர்)`
	before = `(?:under|below|less than|lesser than|within|upto|up to|max|maximum|not more than|cheaper than|budget|<)`
	after  = `(?:se kam|se neeche|tak|ke andar|ke bheetar|से कम|से नीचे|तक|के अंदर|के भीतर|olage|kadime|ಒಳಗೆ|ಯೊಳಗೆ|ಒಳಗಿನ|ಕಡಿಮೆ|lopu|lopala|takkuva|లోపు|లోపల|తక్కువ|kulla|kulle|keezh|kammi|க்குள்|குள்|கீழ்|கம்மி[` + wordClass + `]*|குறைவ[` + wordClass + `]*)`
)

var (
	// "under ₹200", "below 200 rs", "budget 150".
	rePriceBefore = regexp.MustCompile(`(?i)(?:^|[^` + wordClass + `])(` + before + `\s*` + currency + `?\s*` + amount + `(?:\s*` + currency + `)?)(?:\s*` + unit + `(?:$|[^` + wordClass + `]))?`)
	// "200 rupaye se kam", "₹200 tak", "200 ರೂಪಾಯಿ ಒಳಗೆ", "200 ரூபாய்க்குள்" (a currency word
	// is required: "2 kg tak" is no price).
	rePriceAfter = regexp.MustCompile(`(?i)(?:^|[^` + wordClass + `])((?:` + currency + `\s*` + amount + `(?:\s*` + currency + `)?|` + amount + `\s*` + currency + `)\s*` + after + `[` + wordClass + `]*)`)
)

// Parse reads the refine chips in q: at most one of each kind, a brand only from brands
// (the master-catalogue brands named in q, longest first).
func Parse(q string, brands []Brand) Parsed {
	norm := q
	type span struct{ from, to int }
	var taken []span
	free := func(from, to int) bool {
		for _, s := range taken {
			if from < s.to && to > s.from {
				return false
			}
		}
		return true
	}
	var chips []Chip
	have := map[string]bool{}
	add := func(kind, value string, from, to int) {
		if have[kind] || !free(from, to) {
			return
		}
		have[kind] = true
		taken = append(taken, span{from, to})
		chips = append(chips, Chip{Kind: kind, Value: value, Text: truncate(strings.TrimSpace(q[from:to]), MaxChipText)})
	}

	for _, re := range []*regexp.Regexp{rePriceBefore, rePriceAfter} {
		for _, m := range re.FindAllStringSubmatchIndex(norm, -1) {
			if re == rePriceBefore && m[1] > m[3] {
				continue // a unit follows: a pack size, not a price
			}
			// Group 1 is the chip's words; the first amount group that matched is the price.
			num := ""
			for g := 2; 2*g+1 < len(m); g++ {
				if m[2*g] >= 0 {
					num = norm[m[2*g]:m[2*g+1]]
					break
				}
			}
			if v, ok := pricePaise(num); ok {
				add(KindMaxPrice, v, m[2], m[3])
			}
		}
	}
	for _, p := range kindPatterns {
		for _, m := range p.re.FindAllStringSubmatchIndex(norm, -1) {
			from, to := m[2], m[3]
			to = from + len(strings.TrimRightFunc(norm[from:to], func(r rune) bool { return !isWord(r) }))
			add(p.kind, "", from, to)
		}
	}
	low := strings.ToLower(norm)
	for _, b := range brands {
		name := strings.ToLower(strings.TrimSpace(b.Name))
		if name == "" {
			continue
		}
		for i := 0; ; {
			j := strings.Index(low[i:], name)
			if j < 0 {
				break
			}
			from, to := i+j, i+j+len(name)
			if boundary(low, from, to) {
				add(KindBrand, b.Slug, from, to)
				break
			}
			i = from + 1
		}
	}

	sort.SliceStable(chips, func(i, j int) bool { return order(chips[i].Kind) < order(chips[j].Kind) })
	sort.Slice(taken, func(i, j int) bool { return taken[i].from < taken[j].from })
	var rest strings.Builder
	at := 0
	for _, s := range taken {
		if s.from < at {
			continue
		}
		rest.WriteString(q[at:s.from])
		rest.WriteByte(' ')
		at = s.to
	}
	rest.WriteString(q[at:])
	if chips == nil {
		chips = []Chip{}
	}
	return Parsed{Chips: chips, Rest: strings.Join(strings.Fields(rest.String()), " ")}
}

func pricePaise(num string) (string, bool) {
	n, err := strconv.Atoi(strings.Map(func(r rune) rune {
		if r == ',' {
			return -1
		}
		return asciiDigit(r)
	}, num))
	if err != nil || n < 1 || n > MaxPriceRupees {
		return "", false
	}
	return strconv.Itoa(n * 100), true
}

func order(kind string) int {
	for i, k := range Kinds {
		if k == kind {
			return i
		}
	}
	return len(Kinds)
}

// nativeZeros are the zero digits of the Indian scripts the apps ship (Devanagari, Kannada,
// Telugu, Tamil).
var nativeZeros = []rune{'०', '೦', '౦', '௦'}

func asciiDigit(r rune) rune {
	for _, z := range nativeZeros {
		if r >= z && r <= z+9 {
			return '0' + (r - z)
		}
	}
	return r
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) }

func boundary(s string, from, to int) bool {
	if from > 0 {
		r := lastRune(s[:from])
		if isWord(r) {
			return false
		}
	}
	if to < len(s) {
		for _, r := range s[to:] {
			return !isWord(r)
		}
	}
	return true
}

func lastRune(s string) rune {
	r := []rune(s)
	return r[len(r)-1]
}

func isLatin(s string) bool {
	for _, r := range s {
		if r > unicode.MaxLatin1 {
			return false
		}
	}
	return true
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
