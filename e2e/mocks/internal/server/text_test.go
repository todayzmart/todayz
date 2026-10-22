package server

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"9000000001":      "+919000000001",
		"919000000001":    "+919000000001",
		"+91 90000 00001": "+919000000001",
		" 91 90000 00001": "+919000000001", // "+" decoded to a space in a query string
		"09000000001":     "+919000000001",
		"+14155550100":    "+14155550100",
		"":                "",
		"abc":             "",
	}
	for in, want := range cases {
		if got := NormalizePhone(in); got != want {
			t.Errorf("NormalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

// Provider routes take the country code and number; anything else is refused.
func TestProviderPhone(t *testing.T) {
	ok := map[string]string{
		"919000000001":    "+919000000001",
		"+919000000001":   "+919000000001",
		" 919000000001":   "+919000000001", // "+" decoded to a space
		"14155550100":     "+14155550100",
		"447700900123":    "+447700900123",
		"123456789012345": "+123456789012345",
	}
	for in, want := range ok {
		if got, valid := ProviderPhone(in); !valid || got != want {
			t.Errorf("ProviderPhone(%q) = %q %v, want %q", in, got, valid, want)
		}
	}
	for _, in := range []string{
		"", "9000000001", "09000000001", "+91 90000 00001", "91900000000", "9190000000012",
		"0919000000001", "91900000000a", "1234567890123456", "+", "++919000000001",
	} {
		if got, valid := ProviderPhone(in); valid {
			t.Errorf("ProviderPhone(%q) = %q, want refused", in, got)
		}
	}
}

// A parameter that itself holds "{{2}}" is not filled in again, and a
// placeholder without a parameter stays.
func TestRenderWhatsAppSinglePass(t *testing.T) {
	tpls := map[string]string{"t": "Hi {{1}}, order {{2}} {{3}}"}
	got := RenderWhatsApp(tpls, "t", []string{"{{2}}", "TZ-1"})
	if got != "Hi {{2}}, order TZ-1 {{3}}" {
		t.Errorf("render = %q", got)
	}
	if got := RenderWhatsApp(tpls, "nope", []string{"a", "b"}); got != "nope: a | b" {
		t.Errorf("unknown = %q", got)
	}
}

func TestExtractOTP(t *testing.T) {
	cases := []struct {
		name     string
		explicit string
		vars     map[string]string
		body     string
		want     string
	}{
		{"explicit wins", "4821", map[string]string{"otp": "999999"}, "123456 is your code", "4821"},
		{"explicit must be digits", "12ab", nil, "", ""},
		{"var named otp", "", map[string]string{"name": "Asha", "OTP_CODE": "765432"}, "", "765432"},
		{"var otp not digits", "", map[string]string{"otp": "abc"}, "", ""},
		{"body six digits with keyword", "", nil, "Your TodayZ code is 314159. Do not share.", "314159"},
		{"body keyword case-insensitive", "", nil, "One-time password: 271828", "271828"},
		{"body without keyword", "", nil, "Order 314159 is ready", ""},
		{"body two candidates is ambiguous", "", nil, "Code 111111 or 222222", ""},
		{"phone number is not an OTP", "", nil, "Verification sent to 919000000001", ""},
		{"seven digits are not six", "", nil, "code 1234567", ""},
		{"nothing", "", nil, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ExtractOTP(c.explicit, c.vars, c.body); got != c.want {
				t.Errorf("ExtractOTP = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRenderMSG91(t *testing.T) {
	tpls := map[string]string{"t1": "Hi ##Name##, code ##otp## (##missing##)"}
	if got := RenderMSG91(tpls, "t1", map[string]string{"name": "Asha", "OTP": "1234"}, ""); got != "Hi Asha, code 1234 (##missing##)" {
		t.Errorf("got %q", got)
	}
	if got := RenderMSG91(tpls, "nope", map[string]string{"b": "2", "a": "1"}, ""); got != "template nope: a=1, b=2" {
		t.Errorf("got %q", got)
	}
	if got := RenderMSG91(tpls, "", map[string]string{"OTP": "9999"}, "##OTP## is it"); got != "9999 is it" {
		t.Errorf("got %q", got)
	}
}

func TestRenderWhatsApp(t *testing.T) {
	tpls := map[string]string{"order_ready": "Your order {{1}} at {{2}} is ready."}
	if got := RenderWhatsApp(tpls, "order_ready", []string{"TZ-1", "Ravi Stores"}); got != "Your order TZ-1 at Ravi Stores is ready." {
		t.Errorf("got %q", got)
	}
	if got := RenderWhatsApp(tpls, "other", []string{"a", "b"}); got != "other: a | b" {
		t.Errorf("got %q", got)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo", 2); got != "h…" {
		t.Errorf("truncate on a rune boundary: %q", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("got %q", got)
	}
	if got := chunks("aéb", 2); len(got) != 3 || got[0] != "a" || got[1] != "é" || got[2] != "b" {
		t.Errorf("chunks = %q", got)
	}
}
