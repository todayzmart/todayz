package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"todayz.local/e2e/mocks/internal/capture"
	"todayz.local/e2e/mocks/internal/fixtures"
)

func (s *Server) waTemplates(w http.ResponseWriter) (map[string]string, bool) {
	t, err := fixtures.LoadTemplates(s.cfg.Fixtures, "whatsapp/templates.json")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": "tz-mock: " + err.Error()})
		return nil, false
	}
	return t, true
}

// --- MSG91 WhatsApp ---

func msg91WAError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"status": "fail", "hasError": true, "data": nil, "errors": msg})
}

func msg91WAOK(w http.ResponseWriter, requestID string) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success", "hasError": false,
		"data":   map[string]string{"message": "Message sent successfully", "request_id": requestID},
		"errors": nil,
	})
}

// POST /api/v5/whatsapp/whatsapp-outbound-message/bulk/ — template messages.
// Docs: https://docs.msg91.com/whatsapp/send-template-message
func (s *Server) waMSG91Bulk(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("authkey")) == "" {
		msg91WAError(w, http.StatusUnauthorized, "Authentication failure")
		return
	}
	raw, status := readBody(w, r, smsBodyLimit)
	if status != 0 {
		msg91WAError(w, status, bodyErrText(status))
		return
	}
	var req struct {
		IntegratedNumber string `json:"integrated_number"`
		ContentType      string `json:"content_type"`
		Payload          struct {
			Type     string `json:"type"`
			Template struct {
				Name     string `json:"name"`
				Language struct {
					Code string `json:"code"`
				} `json:"language"`
				ToAndComponents []struct {
					To         []string                  `json:"to"`
					Components map[string]map[string]any `json:"components"`
				} `json:"to_and_components"`
			} `json:"template"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		msg91WAError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	t := req.Payload.Template
	switch {
	case req.IntegratedNumber == "":
		msg91WAError(w, http.StatusBadRequest, "integrated_number is required")
		return
	case t.Name == "":
		msg91WAError(w, http.StatusBadRequest, "payload.template.name is required")
		return
	case len(t.ToAndComponents) == 0:
		msg91WAError(w, http.StatusBadRequest, "payload.template.to_and_components is required")
		return
	}
	from, ok := ProviderPhone(req.IntegratedNumber)
	if !ok {
		msg91WAError(w, http.StatusBadRequest, "integrated_number is not valid (country code and number)")
		return
	}
	type msg struct {
		to     string
		params []string
	}
	var msgs []msg
	for i, tc := range t.ToAndComponents {
		params := orderedComponents(tc.Components)
		if len(tc.To) == 0 {
			msg91WAError(w, http.StatusBadRequest, fmt.Sprintf("to_and_components[%d].to is required", i))
			return
		}
		for _, raw := range tc.To {
			to, ok := ProviderPhone(raw)
			if !ok {
				msg91WAError(w, http.StatusBadRequest, fmt.Sprintf("to_and_components[%d].to: number is not valid (country code and number)", i))
				return
			}
			msgs = append(msgs, msg{to, params})
		}
	}
	tpls, ok := s.waTemplates(w)
	if !ok {
		return
	}
	reqID := randomHex(12)
	for _, m := range msgs {
		s.store.WhatsApp.Add(capture.WhatsApp{
			Meta: s.store.Next(), Provider: "msg91", Kind: "template", To: m.to, From: from,
			Template: t.Name, Language: t.Language.Code, Variables: m.params,
			Body: RenderWhatsApp(tpls, t.Name, m.params), MessageID: reqID,
		})
	}
	msg91WAOK(w, reqID)
}

// orderedComponents returns body_1, body_2, ... values in order, then any
// other components (header_1, button_1) as "name=value".
func orderedComponents(c map[string]map[string]any) []string {
	type kv struct {
		n    int
		name string
		val  string
	}
	var body, other []kv
	for name, comp := range c {
		val := ""
		if v, ok := comp["value"]; ok {
			val = stringMap(map[string]any{"v": v})["v"]
		}
		if strings.HasPrefix(name, "body_") {
			n, _ := strconv.Atoi(strings.TrimPrefix(name, "body_"))
			body = append(body, kv{n, name, val})
		} else {
			other = append(other, kv{0, name, val})
		}
	}
	sort.Slice(body, func(i, j int) bool { return body[i].n < body[j].n })
	sort.Slice(other, func(i, j int) bool { return other[i].name < other[j].name })
	out := make([]string, 0, len(c))
	for _, b := range body {
		out = append(out, b.val)
	}
	for _, o := range other {
		out = append(out, o.name+"="+o.val)
	}
	return out
}

// POST /api/v5/whatsapp/whatsapp-outbound-message/ — session (free-form) message.
// Docs: https://docs.msg91.com/whatsapp
func (s *Server) waMSG91Session(w http.ResponseWriter, r *http.Request) {
	if strings.TrimSpace(r.Header.Get("authkey")) == "" {
		msg91WAError(w, http.StatusUnauthorized, "Authentication failure")
		return
	}
	raw, status := readBody(w, r, smsBodyLimit)
	if status != 0 {
		msg91WAError(w, status, bodyErrText(status))
		return
	}
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		msg91WAError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	str := func(v any) string { s, _ := v.(string); return s }
	if str(req["recipient_number"]) == "" || str(req["integrated_number"]) == "" {
		msg91WAError(w, http.StatusBadRequest, "integrated_number and recipient_number are required")
		return
	}
	to, ok1 := ProviderPhone(str(req["recipient_number"]))
	from, ok2 := ProviderPhone(str(req["integrated_number"]))
	if !ok1 || !ok2 {
		msg91WAError(w, http.StatusBadRequest, "integrated_number and recipient_number need the country code and number")
		return
	}
	body := str(req["text"])
	if body == "" {
		if p, ok := req["payload"].(map[string]any); ok {
			switch t := p["text"].(type) {
			case string:
				body = t
			case map[string]any:
				body = str(t["body"])
			}
		}
	}
	if body == "" {
		body = str(req["caption"])
	}
	kind := str(req["content_type"])
	if kind == "" {
		kind = "text"
	}
	reqID := randomHex(12)
	s.store.WhatsApp.Add(capture.WhatsApp{
		Meta: s.store.Next(), Provider: "msg91", Kind: kind, To: to,
		From: from, Body: body, MessageID: reqID,
	})
	msg91WAOK(w, reqID)
}

// --- Gupshup ---

func gupshupError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"status": "error", "message": msg})
}

// gupshupMsg is the common part of a Gupshup send: the form and both numbers
// in E.164.
type gupshupMsg struct {
	form     url.Values
	from, to string
}

func (s *Server) gupshupForm(w http.ResponseWriter, r *http.Request) (gupshupMsg, bool) {
	if strings.TrimSpace(r.Header.Get("apikey")) == "" {
		gupshupError(w, http.StatusUnauthorized, "Authentication Failed")
		return gupshupMsg{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, smsBodyLimit)
	if err := r.ParseForm(); err != nil {
		status := bodyErrStatus(err)
		msg := "Invalid form body"
		if status == http.StatusRequestEntityTooLarge {
			msg = bodyErrText(status)
		}
		gupshupError(w, status, msg)
		return gupshupMsg{}, false
	}
	f := r.PostForm
	if f.Get("channel") != "" && f.Get("channel") != "whatsapp" {
		gupshupError(w, http.StatusBadRequest, "Invalid channel")
		return gupshupMsg{}, false
	}
	from, ok := ProviderPhone(f.Get("source"))
	if !ok {
		gupshupError(w, http.StatusBadRequest, "Invalid Source")
		return gupshupMsg{}, false
	}
	to, ok := ProviderPhone(f.Get("destination"))
	if !ok {
		gupshupError(w, http.StatusBadRequest, "Invalid Destination")
		return gupshupMsg{}, false
	}
	return gupshupMsg{form: f, from: from, to: to}, true
}

// POST /wa/api/v1/msg (form): channel, source, destination, message, src.name.
// message is JSON ({"type":"text","text":"..."}) or plain text.
// Docs: https://docs.gupshup.io/reference/msg
func (s *Server) waGupshupMsg(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gupshupForm(w, r)
	if !ok {
		return
	}
	msg := g.form.Get("message")
	if msg == "" {
		gupshupError(w, http.StatusBadRequest, "Invalid Message")
		return
	}
	kind, body := "text", msg
	var m map[string]any
	if json.Unmarshal([]byte(msg), &m) == nil {
		if t, _ := m["type"].(string); t != "" {
			kind = t
		}
		body, _ = m["text"].(string)
		if body == "" {
			body, _ = m["caption"].(string)
		}
	}
	id := uuidV4()
	s.store.WhatsApp.Add(capture.WhatsApp{
		Meta: s.store.Next(), Provider: "gupshup", Kind: kind,
		To: g.to, From: g.from,
		Body: body, MessageID: id,
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "submitted", "messageId": id})
}

// POST /wa/api/v1/template/msg (form): source, destination, template={"id":"...","params":[...]}.
// Docs: https://docs.gupshup.io/reference/sending-a-text-template
func (s *Server) waGupshupTemplate(w http.ResponseWriter, r *http.Request) {
	g, ok := s.gupshupForm(w, r)
	if !ok {
		return
	}
	var tpl struct {
		ID     string   `json:"id"`
		Params []string `json:"params"`
	}
	if err := json.Unmarshal([]byte(g.form.Get("template")), &tpl); err != nil || tpl.ID == "" {
		gupshupError(w, http.StatusBadRequest, "Invalid Template")
		return
	}
	tpls, ok := s.waTemplates(w)
	if !ok {
		return
	}
	id := uuidV4()
	s.store.WhatsApp.Add(capture.WhatsApp{
		Meta: s.store.Next(), Provider: "gupshup", Kind: "template",
		To: g.to, From: g.from,
		Template: tpl.ID, Variables: tpl.Params, Body: RenderWhatsApp(tpls, tpl.ID, tpl.Params), MessageID: id,
	})
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "submitted", "messageId": id})
}

func uuidV4() string {
	h := []byte(randomHex(16))
	h[12] = '4'
	h[16] = "89ab"[h[16]%4]
	return string(h[0:8]) + "-" + string(h[8:12]) + "-" + string(h[12:16]) + "-" + string(h[16:20]) + "-" + string(h[20:32])
}
