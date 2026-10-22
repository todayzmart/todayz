package capture

import (
	"reflect"
	"unicode/utf8"
)

// MaxField is the most bytes any captured string keeps (about 4 KB). A caller
// that sends a huge body or template variable can not grow the ring past
// capacity × fields × MaxField, and the UI stays responsive.
const MaxField = 4096

// clipMarker ends a clipped string.
const clipMarker = "…[clipped]"

// ClipString cuts s to at most MaxField bytes on a rune boundary.
func ClipString(s string) string {
	if len(s) <= MaxField {
		return s
	}
	n := MaxField - len(clipMarker)
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + clipMarker
}

// clip returns v with every string field, []string element and
// map[string]string key and value clipped. Slices and maps are copied only
// when something changes, so the caller's values are never modified.
func clip[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	clipValue(rv)
	return v
}

var (
	stringSlice   = reflect.TypeFor[[]string]()
	stringMapType = reflect.TypeFor[map[string]string]()
)

func clipValue(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				clipValue(f)
			}
		}
	case reflect.String:
		if v.Len() > MaxField {
			v.SetString(ClipString(v.String()))
		}
	case reflect.Slice:
		if v.Type() != stringSlice || v.IsNil() {
			return
		}
		in := v.Interface().([]string)
		var out []string
		for i, s := range in {
			if len(s) > MaxField {
				if out == nil {
					out = append([]string(nil), in...)
				}
				out[i] = ClipString(s)
			}
		}
		if out != nil {
			v.Set(reflect.ValueOf(out))
		}
	case reflect.Map:
		if v.Type() != stringMapType || v.IsNil() {
			return
		}
		in := v.Interface().(map[string]string)
		long := false
		for k, s := range in {
			if len(k) > MaxField || len(s) > MaxField {
				long = true
				break
			}
		}
		if !long {
			return
		}
		out := make(map[string]string, len(in))
		for k, s := range in {
			out[ClipString(k)] = ClipString(s)
		}
		v.Set(reflect.ValueOf(out))
	}
}
