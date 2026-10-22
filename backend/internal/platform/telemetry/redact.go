package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"reflect"
	"regexp"
	"strings"
	"unicode"
)

// Redacted replaces a sensitive value.
const Redacted = "[redacted]"

// sensitiveTokens are key words that mark a log attribute as sensitive
// (05 §logs: phone, otp, code, token, authorization, cookie, address, pan,
// gstin, upi, name; plus secrets, keys and location). A key matches when any
// of its words (split on _ - . space and camelCase) is in this set.
var sensitiveTokens = map[string]bool{
	"phone": true, "mobile": true, "msisdn": true, "whatsapp": true,
	"otp": true, "code": true, "pin": true, "pincode": true, "totp": true,
	"token": true, "jwt": true, "bearer": true, "refresh": true, "session": true,
	"authorization": true, "auth": true, "cookie": true, "cookies": true, "credential": true, "credentials": true,
	"password": true, "passwd": true, "pwd": true, "secret": true, "key": true, "apikey": true, "dsn": true,
	"signature": true, "sig": true, "salt": true, "seed": true,
	"address": true, "line1": true, "line2": true, "landmark": true,
	"lat": true, "lng": true, "lon": true, "latitude": true, "longitude": true, "location": true,
	"pan": true, "gstin": true, "gst": true, "aadhaar": true, "upi": true, "vpa": true, "ifsc": true, "account": true,
	"name": true, "email": true, "dob": true, "utr": true,
	"enc": true, "hash": true,
}

// safeKeys are exact attribute keys that look sensitive but are not (05 log fields).
var safeKeys = map[string]bool{
	"err_code": true, "status_code": true, "http_status_code": true, "problem_code": true,
	"request_id": true, "trace_id": true, "span_id": true, "fingerprint": true,
	"route": true, "method": true, "status": true, "duration_ms": true,
	"user_kind": true, "role": true, "store_id": true, "service": true, "env": true, "version": true,
	"setting": true, "settings_key": true, "job_kind": true, "names": true,
}

// IsSensitiveKey reports whether an attribute key names sensitive data.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	if safeKeys[k] {
		return false
	}
	for _, w := range words(key) {
		if sensitiveTokens[w] {
			return true
		}
	}
	return false
}

// words splits "userPhone_e164.raw" into user, phone, e164, raw.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	rs := []rune(s)
	for i, r := range rs {
		switch {
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r) && i > 0 && (unicode.IsLower(rs[i-1]) || (i+1 < len(rs) && unicode.IsLower(rs[i+1]) && unicode.IsUpper(rs[i-1]))):
			flush()
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return out
}

var (
	// Indian mobile numbers, with or without +91 / 91 / 0 and separators. The
	// number must not touch a letter or digit, but "_" is a boundary, so
	// user_9876543210 is caught (Go's \b treats "_" as a word character).
	rePhone = regexp.MustCompile(`(^|[^0-9A-Za-z+])((?:\+91|91|0)?[\s-]?[6-9]\d{4}[\s-]?\d{5})($|[^0-9A-Za-z])`)
	// Bearer tokens and JWTs.
	reBearer = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._~+/=-]+`)
	reJWT    = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]*`)
	// "otp 123456", "your otp is 123456", "code: 4821", "pin=1234" written into
	// text, and the other way round: "482913 is your OTP".
	reCodeInText    = regexp.MustCompile(`(?i)\b(otp|code|pin|passcode)\b([^0-9\n]{0,12}?)\d{4,8}\b`)
	reCodeBeforeKey = regexp.MustCompile(`(?i)\b\d{4,8}\b([^0-9\n]{0,12}?\b(?:otp|code|pin|passcode)\b)`)
	// A word that makes every bare 4–8 digit number in the same record suspect.
	reCodeWord = regexp.MustCompile(`(?i)(^|[^a-z])(otp|code|pin|passcode)([^a-z]|$)`)
	reBareCode = regexp.MustCompile(`(^|[^0-9A-Za-z.])\d{4,8}($|[^0-9A-Za-z.])`)
	// key=value / "key":"value" pairs with a sensitive key, inside free text.
	reKVInText = regexp.MustCompile(`(?i)\b(password|passwd|secret|token|api[_-]?key|authorization|otp)("?\s*[:=]\s*"?)[^\s",&]+`)
	// URL userinfo: scheme://user:password@ keeps the user, masks the password.
	reURLUserinfo = regexp.MustCompile(`(?i)\b([a-z][a-z0-9+.-]*://[^\s:/@]*:)[^\s/@]+@`)
	// Credential-looking query parameters.
	reQuerySecret = regexp.MustCompile(`(?i)([?&](?:key|api_?key|token|access_token|auth|sig|signature|secret|password|otp)=)[^&#\s"']*`)
	// Email addresses.
	reEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`)
)

// RedactString masks phone numbers, emails, bearer tokens, JWTs, URL
// passwords, credential query parameters and inline codes in free text.
func RedactString(s string) string { return redactText(s, false) }

// redactText is RedactString; with codeCtx (the record mentions an OTP, code
// or PIN) every bare 4–8 digit number is masked too.
func redactText(s string, codeCtx bool) string {
	if s == "" {
		return s
	}
	s = reBearer.ReplaceAllString(s, "Bearer "+Redacted)
	s = reJWT.ReplaceAllString(s, Redacted)
	s = reURLUserinfo.ReplaceAllString(s, "${1}***@")
	s = reQuerySecret.ReplaceAllString(s, "${1}***")
	s = reEmail.ReplaceAllString(s, "[email]")
	s = reKVInText.ReplaceAllString(s, "${1}${2}"+Redacted)
	s = reCodeInText.ReplaceAllString(s, "${1}${2}"+Redacted)
	s = reCodeBeforeKey.ReplaceAllString(s, Redacted+"${1}")
	s = replaceAllRepeat(rePhone, s, "${1}[phone]${3}")
	if codeCtx {
		s = replaceAllRepeat(reBareCode, s, "${1}"+Redacted+"${2}")
	}
	return s
}

// replaceAllRepeat applies a pattern that consumes its boundary characters
// until nothing changes, so adjacent matches ("9876543210 9876543211") are
// all replaced.
func replaceAllRepeat(re *regexp.Regexp, s, repl string) string {
	for range 4 {
		next := re.ReplaceAllString(s, repl)
		if next == s {
			break
		}
		s = next
	}
	return s
}

// mentionsCode reports whether text names an OTP, code or PIN.
func mentionsCode(s string) bool { return reCodeWord.MatchString(s) }

// phoneShaped reports whether n looks like an Indian mobile number: 10 digits
// starting 6–9, or 91 followed by such a number.
func phoneShaped(n uint64) bool {
	return (n >= 6_000_000_000 && n <= 9_999_999_999) || (n >= 916_000_000_000 && n <= 919_999_999_999)
}

// codeShaped reports whether n has 4 to 8 digits, the shape of an OTP or PIN.
func codeShaped(n uint64) bool { return n >= 1_000 && n <= 99_999_999 }

// redactNumber masks an integer attribute: always when it is phone-shaped,
// and when it is code-shaped in a record that mentions an OTP, code or PIN.
func redactNumber(key string, n uint64, codeCtx bool) (slog.Attr, bool) {
	switch {
	case phoneShaped(n):
		return slog.String(key, "[phone]"), true
	case codeCtx && codeShaped(n):
		return slog.String(key, Redacted), true
	}
	return slog.Attr{}, false
}

// RedactHandler wraps a slog.Handler and masks sensitive data before it is
// written: attributes whose key is sensitive (at any depth, including inside
// groups, WithAttrs and structured values), and phone numbers, tokens and
// codes inside any string and the message. It is a backstop, not a licence
// to log PII (backend/CLAUDE.md).
type RedactHandler struct {
	next slog.Handler
	// all is true inside a group whose name is itself sensitive.
	all bool
}

// NewRedactHandler wraps next.
func NewRedactHandler(next slog.Handler) *RedactHandler { return &RedactHandler{next: next} }

// Enabled implements slog.Handler.
func (h *RedactHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.next.Enabled(ctx, l)
}

// Handle implements slog.Handler.
//
// Code context: when the message, a top-level attribute key (other than the
// safe keys such as err_code) or a top-level string value names an OTP, code,
// PIN or passcode (a whole word, any case), the record is treated as carrying
// a code, and every bare 4–8 digit number in it is masked, in strings and in
// integer attributes whatever their key: log.Info("otp sent",
// slog.Int("value", 482913)) and log.Info("otp delivery", "detail",
// "sent 482913 to user") both lose the number. Phone-shaped integers are
// masked in every record.
func (h *RedactHandler) Handle(ctx context.Context, r slog.Record) error {
	codeCtx := mentionsCode(r.Message)
	if !codeCtx {
		r.Attrs(func(a slog.Attr) bool {
			if !safeKeys[strings.ToLower(a.Key)] && mentionsCode(a.Key) {
				codeCtx = true
			} else if v := a.Value.Resolve(); v.Kind() == slog.KindString && mentionsCode(v.String()) {
				codeCtx = true
			}
			return !codeCtx
		})
	}
	nr := slog.NewRecord(r.Time, r.Level, redactText(r.Message, codeCtx), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		nr.AddAttrs(redactAttr(a, h.all, codeCtx))
		return true
	})
	return h.next.Handle(ctx, nr)
}

// WithAttrs implements slog.Handler.
func (h *RedactHandler) WithAttrs(as []slog.Attr) slog.Handler {
	out := make([]slog.Attr, len(as))
	for i, a := range as {
		out[i] = redactAttr(a, h.all, false)
	}
	return &RedactHandler{next: h.next.WithAttrs(out), all: h.all}
}

// WithGroup implements slog.Handler.
func (h *RedactHandler) WithGroup(name string) slog.Handler {
	return &RedactHandler{next: h.next.WithGroup(name), all: h.all || IsSensitiveKey(name)}
}

func redactAttr(a slog.Attr, all, codeCtx bool) slog.Attr {
	a.Value = a.Value.Resolve()
	if a.Value.Kind() == slog.KindGroup {
		inner := all || (a.Key != "" && IsSensitiveKey(a.Key))
		as := a.Value.Group()
		out := make([]slog.Attr, len(as))
		for i, g := range as {
			out[i] = redactAttr(g, inner, codeCtx)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	}
	if all || IsSensitiveKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redactText(a.Value.String(), codeCtx))
	case slog.KindInt64:
		if n := a.Value.Int64(); n >= 0 {
			if r, ok := redactNumber(a.Key, uint64(n), codeCtx); ok {
				return r
			}
		}
		return a
	case slog.KindUint64:
		if r, ok := redactNumber(a.Key, a.Value.Uint64(), codeCtx); ok {
			return r
		}
		return a
	case slog.KindAny:
		return slog.Any(a.Key, redactAnyCtx(a.Value.Any(), codeCtx))
	default:
		return a
	}
}

// redactAny cleans a structured value. Errors become their redacted message;
// maps, structs and slices go through JSON and are cleaned key by key.
func redactAny(v any) any { return redactAnyCtx(v, false) }

func redactAnyCtx(v any, codeCtx bool) any {
	switch x := v.(type) {
	case nil:
		return nil
	case error:
		return redactText(x.Error(), codeCtx)
	case string:
		return redactText(x, codeCtx)
	case []byte:
		return Redacted
	case json.RawMessage:
		var dec any
		if json.Unmarshal(x, &dec) != nil {
			return Redacted
		}
		return redactJSON(dec, false, codeCtx)
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	switch rv.Kind() {
	case reflect.Map, reflect.Struct, reflect.Slice, reflect.Array:
		b, err := json.Marshal(v)
		if err != nil {
			return Redacted
		}
		var dec any
		if err := json.Unmarshal(b, &dec); err != nil {
			return Redacted
		}
		return redactJSON(dec, false, codeCtx)
	case reflect.String:
		return redactText(rv.String(), codeCtx)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if n := rv.Int(); n >= 0 {
			if r, ok := redactNumber("", uint64(n), codeCtx); ok {
				return r.Value.String()
			}
		}
		return v
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if r, ok := redactNumber("", rv.Uint(), codeCtx); ok {
			return r.Value.String()
		}
		return v
	default:
		return v
	}
}

func redactJSON(v any, all, codeCtx bool) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if all || IsSensitiveKey(k) {
				out[k] = Redacted
				continue
			}
			out[k] = redactJSON(val, false, codeCtx)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = redactJSON(val, all, codeCtx)
		}
		return out
	case string:
		if all {
			return Redacted
		}
		return redactText(x, codeCtx)
	case float64:
		if all {
			return Redacted
		}
		if x >= 0 && x == float64(uint64(x)) {
			if r, ok := redactNumber("", uint64(x), codeCtx); ok {
				return r.Value.String()
			}
		}
		return x
	default:
		if all {
			return Redacted
		}
		return x
	}
}
