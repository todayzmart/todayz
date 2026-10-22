package capture

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAddClipsEveryString(t *testing.T) {
	long := strings.Repeat("é", MaxField) // 2 bytes a rune: the cut must land on a boundary
	vars := map[string]string{"short": "ok", "long": long, long: "key"}
	list := []string{"ok", long}

	s := NewStore(4, nil)
	s.SMS.Add(SMS{To: "+919000000001", Body: long, Variables: vars, TemplateID: long})
	s.WhatsApp.Add(WhatsApp{Body: long, Variables: list})
	s.LLM.Add(LLM{Prompt: long, Response: long, Tools: list})

	sms := s.SMS.List(nil, 0)[0]
	for name, v := range map[string]string{"body": sms.Body, "template": sms.TemplateID, "var": sms.Variables["long"]} {
		if len(v) > MaxField || !strings.HasSuffix(v, clipMarker) || !utf8.ValidString(v) {
			t.Errorf("%s: %d bytes, valid=%v", name, len(v), utf8.ValidString(v))
		}
	}
	if sms.To != "+919000000001" || sms.Variables["short"] != "ok" {
		t.Errorf("short values changed: %+v", sms.Variables["short"])
	}
	for k := range sms.Variables {
		if len(k) > MaxField {
			t.Error("map key not clipped")
		}
	}
	wa := s.WhatsApp.List(nil, 0)[0]
	if len(wa.Body) > MaxField || len(wa.Variables[1]) > MaxField || wa.Variables[0] != "ok" {
		t.Errorf("whatsapp not clipped")
	}
	llm := s.LLM.List(nil, 0)[0]
	if len(llm.Prompt) > MaxField || len(llm.Response) > MaxField || len(llm.Tools[1]) > MaxField {
		t.Error("llm not clipped")
	}

	// The caller's values are untouched.
	if vars["long"] != long || list[1] != long || len(vars) != 3 {
		t.Error("Add modified the caller's map or slice")
	}
	// Short values pass through as they are.
	if got := ClipString("abc"); got != "abc" {
		t.Errorf("ClipString(short) = %q", got)
	}
	if got := ClipString(strings.Repeat("a", MaxField)); len(got) != MaxField || strings.HasSuffix(got, clipMarker) {
		t.Error("exactly MaxField bytes must not be clipped")
	}
}
