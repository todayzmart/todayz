package search

import (
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// maxQueryRunes bounds the normalised query (search_log.query_norm is
// 1–200 characters; the contract allows 200 in).
const maxQueryRunes = 200

// digitZeros are the zero digits of the scripts whose numerals a customer
// may type (Devanagari, Bengali, Gurmukhi, Gujarati, Oriya, Tamil, Telugu,
// Kannada, Malayalam). Each script's digits are the ten code points from it.
var digitZeros = []rune{0x0966, 0x09E6, 0x0A66, 0x0AE6, 0x0B66, 0x0BE6, 0x0C66, 0x0CE6, 0x0D66}

// asciiDigit maps an Indic digit to '0'–'9'; ok is false for anything else.
func asciiDigit(r rune) (rune, bool) {
	if r >= '0' && r <= '9' {
		return r, true
	}
	for _, z := range digitZeros {
		if r >= z && r <= z+9 {
			return '0' + (r - z), true
		}
	}
	return 0, false
}

// unitAliases maps what people type after a number to the catalogue's unit
// (products.unit: g, kg, ml, l, pc, pack).
var unitAliases = map[string]string{
	"g": "g", "gm": "g", "gms": "g", "gr": "g", "gram": "g", "grams": "g", "gramme": "g",
	"kg": "kg", "kgs": "kg", "kilo": "kg", "kilos": "kg", "kilogram": "kg", "kilograms": "kg",
	"ml": "ml", "mls": "ml", "millilitre": "ml", "milliliter": "ml",
	"l": "l", "lt": "l", "ltr": "l", "ltrs": "l", "litre": "l", "litres": "l", "liter": "l", "liters": "l",
	"pc": "pc", "pcs": "pc", "piece": "pc", "pieces": "pc",
	"pack": "pack", "packs": "pack", "pkt": "pack", "packet": "pack",
}

// Normalize is the query normalisation shared by search, the search log and
// D-99 matching (01 §11): Unicode NFKC, lower case, Indic digits as ASCII,
// only letters, combining marks and digits kept (a '.' or ',' between two
// digits stays as '.'), everything else a single space, and a pack size
// written as "<number> <unit>" with the catalogue's unit ("100gm" → "100 g").
// The result is at most 200 characters, cut at a word.
func Normalize(s string) string {
	s = strings.ToLower(norm.NFKC.String(s))
	rs := []rune(s)
	var b strings.Builder
	b.Grow(len(s))
	space := true // drop leading spaces
	for i, r := range rs {
		if d, ok := asciiDigit(r); ok {
			b.WriteRune(d)
			space = false
			continue
		}
		if unicode.IsLetter(r) || unicode.IsMark(r) {
			b.WriteRune(r)
			space = false
			continue
		}
		if (r == '.' || r == ',') && i > 0 && i+1 < len(rs) {
			_, prev := asciiDigit(rs[i-1])
			_, next := asciiDigit(rs[i+1])
			if prev && next {
				b.WriteByte('.')
				continue
			}
		}
		if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	toks := strings.Fields(b.String())
	out := make([]string, 0, len(toks))
	for i, t := range toks {
		if num, unit, ok := gluedPack(t); ok {
			out = append(out, num, unit)
			continue
		}
		if u, ok := unitAliases[t]; ok && i > 0 && isNumber(toks[i-1]) {
			out = append(out, u)
			continue
		}
		out = append(out, t)
	}
	q := strings.Join(out, " ")
	if utf8.RuneCountInString(q) > maxQueryRunes {
		r := []rune(q)[:maxQueryRunes]
		q = strings.TrimSpace(string(r))
		if cut := strings.LastIndexByte(q, ' '); cut > 0 && utf8.RuneCountInString(q) == maxQueryRunes {
			q = q[:cut]
		}
	}
	return q
}

// gluedPack splits "100g" or "1.5kg" into number and catalogue unit.
func gluedPack(t string) (num, unit string, ok bool) {
	i := 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.') {
		i++
	}
	if i == 0 || i == len(t) || !isNumber(t[:i]) {
		return "", "", false
	}
	u, ok := unitAliases[t[i:]]
	if !ok {
		return "", "", false
	}
	return t[:i], u, true
}

// isNumber reports whether t is digits with at most one inner '.'.
func isNumber(t string) bool {
	if t == "" || t[0] == '.' || t[len(t)-1] == '.' {
		return false
	}
	dots := 0
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] >= '0' && t[i] <= '9':
		case t[i] == '.':
			dots++
		default:
			return false
		}
	}
	return dots <= 1
}

// pack is a pack size the query names ("100 g"), in base units (g, ml, pc,
// pack) so 1 kg equals 1000 g.
type pack struct {
	amount float64
	base   string
}

// toBase converts an amount in a catalogue unit to its base unit.
func toBase(amount float64, unit string) pack {
	switch unit {
	case "kg":
		return pack{amount * 1000, "g"}
	case "l":
		return pack{amount * 1000, "ml"}
	default:
		return pack{amount, unit}
	}
}

// matches reports whether an item of packSize unit is this pack.
func (p pack) matches(packSize float64, unit string) bool {
	o := toBase(packSize, unit)
	return o.base == p.base && math.Abs(o.amount-p.amount) < 1e-6
}

// parsed is a normalised query split into the words to match and the pack
// size it names, if any.
type parsed struct {
	norm  string   // the whole normalised query (search_log.query_norm)
	words []string // the words to match (without the pack size)
	pack  *pack
}

// core is the words to match as one string.
func (p parsed) core() string { return strings.Join(p.words, " ") }

// parse splits a normalised query. The first "<number> <unit>" pair is the
// pack size, a hard filter (TODO(lead): a hard filter means "amul butter
// 500 g" hides the 100 g pack; the plan only says search matches pack size).
// A query that is only a pack size keeps it as words.
func parse(normalised string) parsed {
	toks := strings.Fields(normalised)
	p := parsed{norm: normalised, words: toks}
	for i := 0; i+1 < len(toks); i++ {
		if !isNumber(toks[i]) {
			continue
		}
		u := toks[i+1]
		if _, ok := unitAliases[u]; !ok || unitAliases[u] != u {
			continue
		}
		amount, err := strconv.ParseFloat(toks[i], 64)
		if err != nil || amount <= 0 {
			continue
		}
		rest := append(append([]string{}, toks[:i]...), toks[i+2:]...)
		if len(rest) == 0 {
			break
		}
		pk := toBase(amount, u)
		p.words, p.pack = rest, &pk
		break
	}
	return p
}

// packText is a product's pack size as the normaliser writes it ("100 g",
// "0.5 kg").
func packText(size float64, unit string) string {
	return strconv.FormatFloat(size, 'f', -1, 64) + " " + unit
}

// escapeLike escapes s for ILIKE with the default backslash escape.
func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// tsToken is one word as a prefix tsquery operand. The normaliser left only
// letters, marks, digits and inner dots, so quoting cannot break out. A
// plural Latin word also matches its singular ("eggs" finds "Egg").
func tsToken(t string) string {
	op := "'" + t + "':*"
	if n := len(t); n >= 4 && t[n-1] == 's' && t[n-2] != 's' && isASCIILetters(t) {
		return "(" + op + " | '" + t[:n-1] + "':*)"
	}
	return op
}

// tsPhrase is every word of a phrase (AND), as prefixes.
func tsPhrase(words []string) string {
	ops := make([]string, len(words))
	for i, w := range words {
		ops[i] = tsToken(w)
	}
	if len(ops) == 1 {
		return ops[0]
	}
	return "(" + strings.Join(ops, " & ") + ")"
}

// tsExact is every word of a phrase (AND), whole words only (D-99 candidates).
func tsExact(words []string) string {
	ops := make([]string, len(words))
	for i, w := range words {
		ops[i] = "'" + w + "'"
	}
	return strings.Join(ops, " & ")
}

func isASCIILetters(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return s != ""
}

// slot is one position of the query: its words and the phrases that may
// stand for it (itself first, then its synonyms).
type slot struct {
	alts     [][]string
	expanded bool
}

// tsquery joins the slots: every slot must match, any of its phrases.
func tsquery(slots []slot) string {
	parts := make([]string, 0, len(slots))
	for _, s := range slots {
		ops := make([]string, 0, len(s.alts))
		seen := map[string]bool{}
		for _, a := range s.alts {
			op := tsPhrase(a)
			if !seen[op] {
				seen[op] = true
				ops = append(ops, op)
			}
		}
		if len(ops) == 1 {
			parts = append(parts, ops[0])
		} else {
			parts = append(parts, "("+strings.Join(ops, " | ")+")")
		}
	}
	return strings.Join(parts, " & ")
}

// ngrams lists every run of 1 to 3 consecutive words (the synonym lookup).
func ngrams(words []string) []string {
	var out []string
	for i := range words {
		for n := 1; n <= 3 && i+n <= len(words); n++ {
			out = append(out, strings.Join(words[i:i+n], " "))
		}
	}
	return out
}

// buildSlots matches the words against the synonym phrases, longest first
// and left to right. alts maps a phrase to its alternatives (synonyms and
// their second hop), not including the phrase itself.
func buildSlots(words []string, alts map[string][]string) []slot {
	var slots []slot
	for i := 0; i < len(words); {
		matched := false
		for n := 3; n >= 1; n-- {
			if i+n > len(words) {
				continue
			}
			phrase := strings.Join(words[i:i+n], " ")
			a, ok := alts[phrase]
			if !ok {
				continue
			}
			sl := slot{alts: [][]string{words[i : i+n]}, expanded: len(a) > 0}
			for _, x := range a {
				sl.alts = append(sl.alts, strings.Fields(x))
			}
			slots = append(slots, sl)
			i += n
			matched = true
			break
		}
		if !matched {
			slots = append(slots, slot{alts: [][]string{{words[i]}}})
			i++
		}
	}
	return slots
}
