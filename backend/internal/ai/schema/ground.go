package schema

import (
	"math/big"
	"regexp"
	"strings"
	"unicode"
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

// HasDigits reports whether s has any digit of any script (a quick check before Ungrounded).
func HasDigits(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
