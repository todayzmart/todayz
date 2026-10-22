package server

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// NormalizePhone is the lenient parser for /_mock filters: it turns any
// Indian-style number into E.164 ("+919000000001").
// MSG91 sends country code + number without "+" ("919000000001"); a bare
// 10-digit number is taken as Indian. It returns "" when there are no digits.
// Note that "+" in a query string decodes to a space; digits survive either way.
func NormalizePhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	switch {
	case d == "":
		return ""
	case len(d) == 10:
		return "+91" + d
	case len(d) == 11 && d[0] == '0':
		return "+91" + d[1:]
	default:
		return "+" + d
	}
}

// ProviderPhone is the strict parser for provider routes, which (like MSG91
// and Gupshup) take the country code and number as digits: "919000000001".
// A leading "+" and surrounding spaces are allowed ("+" in a query string
// decodes to a space). A number without a country code is refused, so a
// backend that drops the "91" fails here instead of in production. Rules:
// 11 to 15 digits, no leading 0, and a "91" number has exactly 12 digits.
// It returns the E.164 form ("+919000000001").
func ProviderPhone(s string) (string, bool) {
	d := strings.TrimPrefix(strings.TrimSpace(s), "+")
	if d == "" || d[0] == '0' || len(d) < 11 || len(d) > 15 {
		return "", false
	}
	for _, r := range d {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	if strings.HasPrefix(d, "91") && len(d) != 12 {
		return "", false
	}
	return "+" + d, true
}

var (
	sixDigitsRe = regexp.MustCompile(`(?:^|[^0-9])([0-9]{6})(?:[^0-9]|$)`)
	otpWordRe   = regexp.MustCompile(`(?i)(otp|code|verification|one[- ]time)`)
	digitsRe    = regexp.MustCompile(`^[0-9]{4,9}$`)
)

// ExtractOTP finds the one-time code in a message, in this order:
//  1. explicit: the MSG91 otp parameter;
//  2. a template variable whose name contains "otp" (any case), digits only;
//  3. the body, when it mentions an OTP/code and holds exactly one standalone
//     6-digit number (so phone numbers, which are longer, never match).
//
// It returns "" when no code is found.
func ExtractOTP(explicit string, vars map[string]string, body string) string {
	if digitsRe.MatchString(explicit) {
		return explicit
	}
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if strings.Contains(strings.ToLower(k), "otp") && digitsRe.MatchString(vars[k]) {
			return vars[k]
		}
	}
	if otpWordRe.MatchString(body) {
		m := sixDigitsRe.FindAllStringSubmatch(body, -1)
		if len(m) == 1 {
			return m[0][1]
		}
	}
	return ""
}

// RenderMSG91 fills a DLT-style template ("##OTP##", "##var1##"). Variable names
// match case-insensitively. Unknown templates render as "template <id>: K=V, ...".
func RenderMSG91(templates map[string]string, id string, vars map[string]string, fallback string) string {
	tpl, ok := templates[id]
	if !ok {
		tpl = fallback
	}
	if tpl == "" {
		keys := make([]string, 0, len(vars))
		for k := range vars {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+vars[k])
		}
		name := id
		if name == "" {
			name = "(none)"
		}
		return "template " + name + ": " + strings.Join(parts, ", ")
	}
	lower := make(map[string]string, len(vars))
	for k, v := range vars {
		lower[strings.ToLower(k)] = v
	}
	return msg91VarRe.ReplaceAllStringFunc(tpl, func(m string) string {
		name := strings.ToLower(strings.Trim(m, "#"))
		if v, ok := lower[name]; ok {
			return v
		}
		return m
	})
}

var msg91VarRe = regexp.MustCompile(`##[A-Za-z0-9_]+##`)

var waVarRe = regexp.MustCompile(`\{\{([0-9]+)\}\}`)

// RenderWhatsApp fills "{{1}}", "{{2}}" placeholders from params (1-based) in
// one pass, so a parameter that itself contains "{{2}}" is not substituted
// again. A placeholder without a parameter stays as it is. Unknown templates
// render as "<name>: p1 | p2".
func RenderWhatsApp(templates map[string]string, name string, params []string) string {
	tpl, ok := templates[name]
	if !ok {
		return name + ": " + strings.Join(params, " | ")
	}
	return waVarRe.ReplaceAllStringFunc(tpl, func(m string) string {
		n, err := strconv.Atoi(m[2 : len(m)-2])
		if err != nil || n < 1 || n > len(params) {
			return m
		}
		return params[n-1]
	})
}
