package schema

import (
	"bytes"
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Number grounding (11 §1 rule 5): free text a model writes (insight sentences, coach cards,
// reply drafts, weekly summaries) may only contain numbers that are in the backend input it
// was given. A number is compared by value, so "18,240", "₹18240" and "18240.00" are the same
// number, "1,00,000" (Indian grouping) is 100000, and digits of the Indian scripts count as
// their ASCII values. A number written as a word ("six") is not caught; prompts ask for digits.

// nativeZeros are the zero digits of the scripts the apps support or a model may answer in:
// Devanagari, Bengali, Gurmukhi, Gujarati, Oriya, Tamil, Telugu, Kannada, Malayalam.
var nativeZeros = []rune{'०', '০', '੦', '૦', '୦', '௦', '౦', '೦', '൦'}

func asciiDigits(s string) string {
	return strings.Map(func(r rune) rune {
		for _, z := range nativeZeros {
			if r >= z && r <= z+9 {
				return '0' + (r - z)
			}
		}
		return r
	}, s)
}

// reNumber is a run of digits with optional thousands commas and a decimal part. A hyphen is
// never a sign: "7-9 pm" is 7 and 9.
var reNumber = regexp.MustCompile(`[0-9][0-9,]*(?:\.[0-9]+)?`)

// Numbers returns the distinct numbers in s, each as its canonical decimal string.
func Numbers(s string) []string {
	s = asciiDigits(s)
	seen := map[string]bool{}
	var out []string
	for _, m := range reNumber.FindAllString(s, -1) {
		m = strings.TrimRight(m, ",")
		c, ok := canonical(m)
		if !ok || seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out
}

func canonical(m string) (string, bool) {
	r, ok := new(big.Rat).SetString(strings.ReplaceAll(m, ",", ""))
	if !ok {
		return "", false
	}
	if r.IsInt() {
		return r.Num().String(), true
	}
	return strings.TrimRight(strings.TrimRight(r.FloatString(6), "0"), "."), true
}

// Ungrounded returns the numbers in text that appear in none of inputs (nil: every number is
// grounded). inputs are the strings the model was given: the facts JSON, the review text,
// item names (so "Amul butter 100 g" grounds 100).
func Ungrounded(text string, inputs ...string) []string {
	have := map[string]bool{}
	for _, in := range inputs {
		for _, n := range Numbers(in) {
			have[n] = true
		}
	}
	var bad []string
	for _, n := range Numbers(text) {
		if !have[n] {
			bad = append(bad, n)
		}
	}
	return bad
}

// Unit is what a number measures.
type Unit uint8

const (
	Plain Unit = iota
	Percent
	Money
)

// Marks of a percent or a rupee amount around a number. Text uses the same forms the prompts
// ask for; a percent or an amount written another way ("das pratishat") reads as plain.
var (
	moneyBefore   = []string{"₹", "rs.", "rs", "inr", "रु.", "रु", "रू.", "रू", "ರೂ.", "ರೂ", "రూ.", "రూ", "ரூ.", "ரூ"}
	moneyAfter    = []string{"/-", "rupees", "rupee", "rs", "रुपये", "रुपए", "रुपया"}
	percentBefore = []string{"ಶೇ.", "ಶೇ"}
	percentAfter  = []string{"%", "percent", "per cent", "प्रतिशत", "శాతం", "சதவீதம்", "ಪ್ರತಿಶತ"}
)

type unitNumber struct {
	n string
	u Unit
}

// numbersWithUnits is Numbers with each number's unit read from the marks around it.
func numbersWithUnits(s string) []unitNumber {
	s = asciiDigits(s)
	var out []unitNumber
	for _, ix := range reNumber.FindAllStringIndex(s, -1) {
		m := strings.TrimRight(s[ix[0]:ix[1]], ",")
		c, ok := canonical(m)
		if !ok {
			continue
		}
		before := strings.ToLower(strings.TrimRightFunc(s[:ix[0]], unicode.IsSpace))
		after := strings.ToLower(strings.TrimLeftFunc(s[ix[0]+len(m):], unicode.IsSpace))
		u := Plain
		switch {
		case endsWithMark(before, percentBefore) || startsWithMark(after, percentAfter):
			u = Percent
		case endsWithMark(before, moneyBefore) || startsWithMark(after, moneyAfter):
			u = Money
		}
		out = append(out, unitNumber{c, u})
	}
	return out
}

// A mark that is a word ("rs") must stand alone: "hours 5" is not "rs 5".
func endsWithMark(s string, marks []string) bool {
	for _, w := range marks {
		if !strings.HasSuffix(s, w) {
			continue
		}
		rest := s[:len(s)-len(w)]
		first, _ := utf8.DecodeRuneInString(w)
		last, _ := utf8.DecodeLastRuneInString(rest)
		if rest == "" || !unicode.IsLetter(first) || !unicode.IsLetter(last) {
			return true
		}
	}
	return false
}

func startsWithMark(s string, marks []string) bool {
	for _, w := range marks {
		if !strings.HasPrefix(s, w) {
			continue
		}
		rest := s[len(w):]
		end, _ := utf8.DecodeLastRuneInString(w)
		next, _ := utf8.DecodeRuneInString(rest)
		if rest == "" || !unicode.IsLetter(end) || !unicode.IsLetter(next) {
			return true
		}
	}
	return false
}

// keyUnit is the unit of a fact by its JSON key; skip is an index or id, which grounds
// nothing (an item's place in the list is not a number the model may say).
func keyUnit(key string) (u Unit, skip bool) {
	k := strings.ToLower(key)
	switch {
	case k == "n" || k == "index" || k == "idx" || k == "rank" || k == "id" ||
		strings.HasSuffix(key, "Id") || strings.HasSuffix(key, "Ids"):
		return Plain, true
	case strings.Contains(k, "pct") || strings.Contains(k, "percent"):
		return Percent, false
	}
	for _, w := range []string{"paise", "rupees", "price", "mrp", "cost", "sales", "amount"} {
		if strings.Contains(k, w) {
			return Money, false
		}
	}
	return Plain, false
}

// grounds is the set of numbers a model was given, by unit.
type grounds map[Unit]map[string]bool

func (g grounds) add(n string, u Unit) {
	if g[u] == nil {
		g[u] = map[string]bool{}
	}
	g[u][n] = true
}

// has: a percent or an amount needs a fact of its unit; a plain number may be any fact's.
func (g grounds) has(x unitNumber) bool {
	if x.u != Plain {
		return g[x.u][x.n]
	}
	return g[Plain][x.n] || g[Percent][x.n] || g[Money][x.n]
}

// Units names the unit of fact keys whose names do not say it ("threshold" is a percent).
// A key listed here takes that unit and is never skipped; any other key is read by keyUnit.
type Units map[string]Unit

func (us Units) of(key string) (Unit, bool) {
	if u, ok := us[key]; ok {
		return u, false
	}
	return keyUnit(key)
}

func (g grounds) walk(us Units, key string, v any) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			g.walk(us, k, x)
		}
	case []any:
		for _, x := range t {
			g.walk(us, key, x)
		}
	case json.Number:
		u, skip := us.of(key)
		if skip {
			return
		}
		if c, ok := canonical(t.String()); ok {
			g.add(fromPaise(key, c), u)
		}
	case string:
		u, skip := us.of(key)
		if skip {
			return
		}
		ns := numbersWithUnits(t)
		// A string that is only a number ("55" under priceRupees) takes its key's unit;
		// otherwise (a name, a date) its numbers keep their own marks.
		bare := strings.TrimSpace(asciiDigits(t))
		whole := bare != "" && reNumber.FindString(bare) == bare
		for _, x := range ns {
			if whole {
				x.u, x.n = u, fromPaise(key, x.n)
			}
			g.add(x.n, x.u)
		}
	}
}

// fromPaise reads a …Paise fact as rupees, the way text says it.
func fromPaise(key, c string) string {
	if !strings.HasSuffix(strings.ToLower(key), "paise") {
		return c
	}
	r, ok := new(big.Rat).SetString(c)
	if !ok {
		return c
	}
	out, _ := canonical(r.Quo(r, big.NewRat(100, 1)).FloatString(2))
	return out
}

// UngroundedFacts is Ungrounded with units (rule 5, D-230). facts is the JSON the model was
// given: a number under a percent key (pct, …Pct, percentOff) grounds only a percent, one
// under a money key (…Paise as rupees, …Rupees, price, mrp, cost, sales, amount) only an
// amount, and an index or id (n, …Id) nothing, so banner copy cannot turn item 2 into "2%
// off" or a 10 % offer into "₹10 off". In text, "N%" must match a percent fact and "₹N" a
// money fact; a plain number may match any fact. extra are free strings the model was given
// (a review's text), whose numbers carry their own marks.
func UngroundedFacts(text string, facts []byte, extra ...string) []string {
	return UngroundedFactsIn(text, facts, nil, extra...)
}

// UngroundedFactsIn is UngroundedFacts with the units of keys a caller names (us), for
// facts whose key names do not carry their unit.
func UngroundedFactsIn(text string, facts []byte, us Units, extra ...string) []string {
	g := grounds{}
	if len(bytes.TrimSpace(facts)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(facts))
		dec.UseNumber()
		var v any
		if dec.Decode(&v) != nil {
			return Numbers(text) // facts that do not parse ground nothing
		}
		g.walk(us, "", v)
	}
	for _, s := range extra {
		for _, x := range numbersWithUnits(s) {
			g.add(x.n, x.u)
		}
	}
	seen := map[unitNumber]bool{}
	var bad []string
	for _, x := range numbersWithUnits(text) {
		if seen[x] {
			continue
		}
		seen[x] = true
		if !g.has(x) {
			bad = append(bad, x.n)
		}
	}
	return bad
}

// HasDigits reports whether s has any digit of any script (a quick check before Ungrounded).
func HasDigits(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
