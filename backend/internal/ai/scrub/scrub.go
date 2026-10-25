// Package scrub removes personal data from text before it goes to a model (11 §1 rule 6).
//
// Customer text (search lists, support messages, review text) passes through Text first.
// What it replaces, each with a bracketed tag the prompt explains:
//   - email addresses and UPI ids → [email], [upi]
//   - Indian mobile numbers, with or without +91 / 0, spaced or not → [phone]
//   - card-like digit runs (13–19 digits) → [card]; Aadhaar-like 12-digit numbers → [id]
//   - GSTIN and PAN → [id]
//   - six-digit PIN codes → [pin]
//   - a 4–8 digit number right after a code word (OTP, code, PIN, कोड, ಕೋಡ್…) → [code]
//   - every name or address line the caller passes in Options → [name], [address]
//
// A bare short number is kept: "2 kg", "500 g" and prices must reach the model. Callers still
// never put customer names, phones, addresses, PAN/GST or codes into a prompt themselves;
// Text is the backstop for what people type. Nothing here logs.
package scrub

import (
	"regexp"
	"sort"
	"strings"
)

// Kind is what was replaced.
type Kind string

// Kinds.
const (
	Email   Kind = "email"
	UPI     Kind = "upi"
	Phone   Kind = "phone"
	Card    Kind = "card"
	ID      Kind = "id"
	Pin     Kind = "pin"
	Code    Kind = "code"
	Name    Kind = "name"
	Address Kind = "address"
)

// Options name known personal values to remove as well.
type Options struct {
	Names     []string // the customer's or staff member's names
	Addresses []string // address lines
}

// Result is the scrubbed text and what was found.
type Result struct {
	Text  string
	Found map[Kind]int
}

type rule struct {
	kind Kind
	re   *regexp.Regexp
	// group is the submatch to replace (0: the whole match).
	group int
}

// Order matters: wider patterns first, so a GSTIN is not half-read as a PAN and a card
// number is not read as a phone.
var rules = []rule{
	{Email, regexp.MustCompile(`(?i)[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}`), 0},
	{UPI, regexp.MustCompile(`(?i)\b[a-z0-9._-]{2,}@[a-z]{2,}\b`), 0},
	{ID, regexp.MustCompile(`(?i)\b[0-9]{2}[a-z]{5}[0-9]{4}[a-z][1-9a-z]z[0-9a-z]\b`), 0}, // GSTIN
	{ID, regexp.MustCompile(`(?i)\b[a-z]{5}[0-9]{4}[a-z]\b`), 0},                          // PAN
	{Code, regexp.MustCompile(`(?i)(?:otp|code|pin|passcode|कोड|ಕೋಡ್|கோட்|కోడ్)[^0-9\n]{0,12}([0-9]{4,8})\b`), 1},
	{Card, regexp.MustCompile(`\b[0-9](?:[ -]?[0-9]){12,18}\b`), 0},
	{Phone, regexp.MustCompile(`(?:(?:\+91|\b91|\b0)[ -]?|\b)[6-9](?:[ -]?[0-9]){9}\b`), 0},
	{ID, regexp.MustCompile(`\b[2-9][0-9]{3}[ -]?[0-9]{4}[ -]?[0-9]{4}\b`), 0}, // Aadhaar
	{Pin, regexp.MustCompile(`\b[1-9][0-9]{5}\b`), 0},                          // no spaced form: "120 500" is two quantities
}

// Text scrubs s.
func Text(s string, o Options) Result {
	res := Result{Text: s, Found: map[Kind]int{}}
	res.Text = replaceKnown(res.Text, o.Addresses, Address, res.Found)
	res.Text = replaceKnown(res.Text, o.Names, Name, res.Found)
	for _, r := range rules {
		tag := "[" + string(r.kind) + "]"
		res.Text = r.re.ReplaceAllStringFunc(res.Text, func(m string) string {
			res.Found[r.kind]++
			if r.group == 0 {
				return tag
			}
			sub := r.re.FindStringSubmatchIndex(m)
			if len(sub) <= 2*r.group || sub[2*r.group] < 0 {
				return tag
			}
			return m[:sub[2*r.group]] + tag + m[sub[2*r.group+1]:]
		})
	}
	return res
}

// replaceKnown removes each value (case-insensitive, whole words), longest first so "Ravi
// Kumar" goes before "Ravi".
func replaceKnown(s string, values []string, k Kind, found map[Kind]int) string {
	vs := make([]string, 0, len(values))
	for _, v := range values {
		if v = strings.TrimSpace(v); len([]rune(v)) >= 2 {
			vs = append(vs, v)
		}
	}
	sort.Slice(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	for _, v := range vs {
		re := regexp.MustCompile(`(?i)(^|[^\pL\pN])` + regexp.QuoteMeta(v) + `($|[^\pL\pN])`)
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			found[k]++
			sub := re.FindStringSubmatch(m)
			return sub[1] + "[" + string(k) + "]" + sub[2]
		})
	}
	return s
}

// Clean reports whether Text would change nothing in s (for tests and for asserting that a
// prompt built from our own data carries no personal value).
func Clean(s string) bool {
	return Text(s, Options{}).Text == s
}
