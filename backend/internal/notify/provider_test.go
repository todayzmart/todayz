package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/config"
	"github.com/todayz/backend/internal/platform/egress"
	"github.com/todayz/backend/internal/platform/httpx"
	"github.com/todayz/backend/internal/platform/notice"
)

// The reserved test number (never a real person).
const testPhone = "+919000000011"

// loopback reaches httptest servers only (no egress).
var loopback = httpx.NewOutboundClient(httpx.OutboundOptions{NoEgress: true, Timeout: 5 * time.Second})

// captured is one request a fake MSG91 received.
type captured struct {
	Path, Query, AuthKey, ContentType string
	Body                              map[string]any
	Raw                               string
}

func fakeMSG91(t *testing.T, status int, reply string) (*httptest.Server, func() []captured) {
	t.Helper()
	var mu sync.Mutex
	var got []captured
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c := captured{Path: r.URL.Path, Query: r.URL.RawQuery, AuthKey: r.Header.Get("authkey"), ContentType: r.Header.Get("Content-Type"), Raw: string(b)}
		_ = json.Unmarshal(b, &c.Body)
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []captured {
		mu.Lock()
		defer mu.Unlock()
		return append([]captured(nil), got...)
	}
}

func unseenMessage() Message {
	return Message{
		To: testPhone, Template: "tz_order_unseen", Lang: "en",
		Vars: []Var{{"store", "Lakshmi Traders"}, {"minutes", "3"}},
		Text: "A new order at Lakshmi Traders has not been opened for 3 minutes.",
	}
}

func TestMSG91SMS(t *testing.T) {
	srv, got := fakeMSG91(t, http.StatusOK, `{"type":"success","message":"3567686e6f6d313233343536"}`)
	p := &MSG91SMS{Client: loopback, BaseURL: srv.URL + "/", AuthKey: "test-auth", SenderID: "TODAYZ"}
	require.Equal(t, "msg91", p.Name())
	res, err := p.Send(context.Background(), unseenMessage())
	require.NoError(t, err)
	require.Equal(t, "3567686e6f6d313233343536", res.ProviderID)
	require.Equal(t, 1, res.Units)

	reqs := got()
	require.Len(t, reqs, 1)
	r := reqs[0]
	require.Equal(t, "/api/v5/flow", r.Path)
	require.Empty(t, r.Query, "nothing in the URL")
	require.Equal(t, "test-auth", r.AuthKey)
	require.Equal(t, "application/json", r.ContentType)
	require.Equal(t, "tz_order_unseen", r.Body["template_id"])
	require.Equal(t, "TODAYZ", r.Body["sender"])
	rcpt := r.Body["recipients"].([]any)[0].(map[string]any)
	require.Equal(t, "919000000011", rcpt["mobiles"], "MSG91 takes the number without +")
	require.Equal(t, "Lakshmi Traders", rcpt["store"])
	require.Equal(t, "3", rcpt["minutes"])
}

func TestMSG91SMSFailuresNeverCarryThePhone(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		reply  string
	}{
		{"an error status", http.StatusInternalServerError, `{"type":"error","message":"invalid mobile 919000000011"}`},
		{"a 200 that is not success", http.StatusOK, `{"type":"error","message":"919000000011 is DND"}`},
		{"not JSON", http.StatusBadGateway, `<html>919000000011</html>`},
		{"a long type", http.StatusOK, `{"type":"` + strings.Repeat("x", 500) + `"}`},
	} {
		srv, _ := fakeMSG91(t, c.status, c.reply)
		p := &MSG91SMS{Client: loopback, BaseURL: srv.URL, AuthKey: "test-auth", SenderID: "TODAYZ"}
		_, err := p.Send(context.Background(), unseenMessage())
		require.Error(t, err, c.name)
		require.ErrorIs(t, err, ErrProvider, c.name)
		require.NotContains(t, err.Error(), "9000000011", c.name)
		require.NotContains(t, err.Error(), "test-auth", c.name)
		require.Less(t, len(err.Error()), 200, c.name)
	}

	// A provider that is down: the transport error names the URL, never the body.
	srv, _ := fakeMSG91(t, http.StatusOK, `{}`)
	url := srv.URL
	srv.Close()
	p := &MSG91SMS{Client: loopback, BaseURL: url, AuthKey: "test-auth"}
	_, err := p.Send(context.Background(), unseenMessage())
	require.ErrorIs(t, err, ErrProvider)
	require.NotContains(t, err.Error(), "9000000011")
}

func TestMSG91SMSRefusedUnderNoEgress(t *testing.T) {
	// The egress guard refuses any host that is not loopback.
	p := &MSG91SMS{Client: loopback, BaseURL: "https://control.msg91.com", AuthKey: "test-auth"}
	_, err := p.Send(context.Background(), unseenMessage())
	require.ErrorIs(t, err, ErrProvider)
	require.ErrorIs(t, err, egress.ErrBlocked, "refused before any lookup")
	require.NotContains(t, err.Error(), "9000000011")
}

func TestMSG91WhatsApp(t *testing.T) {
	srv, got := fakeMSG91(t, http.StatusOK, `{"status":"success","data":{"request_id":"wa-123"}}`)
	p := &MSG91WhatsApp{Client: loopback, BaseURL: srv.URL, AuthKey: "test-wa", IntegratedNumber: "918000000000"}
	m := unseenMessage()
	m.Lang = "hi"
	m.Template = "tz_order_unseen_hi"
	res, err := p.Send(context.Background(), m)
	require.NoError(t, err)
	require.Equal(t, Result{ProviderID: "wa-123", Units: 1}, res)

	r := got()[0]
	require.Equal(t, "/api/v5/whatsapp/whatsapp-outbound-message/bulk/", r.Path)
	require.Equal(t, "test-wa", r.AuthKey)
	require.Equal(t, "918000000000", r.Body["integrated_number"])
	tpl := r.Body["payload"].(map[string]any)["template"].(map[string]any)
	require.Equal(t, "tz_order_unseen_hi", tpl["name"])
	require.Equal(t, "hi", tpl["language"].(map[string]any)["code"])
	tc := tpl["to_and_components"].([]any)[0].(map[string]any)
	require.Equal(t, []any{"919000000011"}, tc["to"])
	comps := tc["components"].(map[string]any)
	require.Equal(t, "Lakshmi Traders", comps["body_1"].(map[string]any)["value"])
	require.Equal(t, "3", comps["body_2"].(map[string]any)["value"])

	srv2, _ := fakeMSG91(t, http.StatusUnauthorized, `{"status":"fail","errors":"bad key for 919000000011"}`)
	p.BaseURL = srv2.URL
	_, err = p.Send(context.Background(), m)
	require.ErrorIs(t, err, ErrProvider)
	require.NotContains(t, err.Error(), "9000000011")
}

func TestProvidersFromConfig(t *testing.T) {
	require.Empty(t, providersFromConfig(nil))

	local := &config.Config{Env: config.EnvLocal, NoEgress: true, Providers: config.Providers{SMSAPIURL: "https://mocks.todayz.localhost/msg91"}}
	ps := providersFromConfig(local)
	require.Len(t, ps, 1, "SMS only: WhatsApp needs the sender number, push is a later slice")
	sms := ps[notice.SMS].(*MSG91SMS)
	require.Equal(t, mockAuthKey, sms.AuthKey, "the local mock checks the header's presence only")

	withKey := *local
	withKey.Providers.MSG91AuthKey = config.NewSecret([]byte("k-from-env"))
	require.Equal(t, "k-from-env", providersFromConfig(&withKey)[notice.SMS].(*MSG91SMS).AuthKey)

	prod := &config.Config{Env: config.EnvProd, Providers: config.Providers{SMSAPIURL: "https://control.msg91.com"}}
	require.Empty(t, providersFromConfig(prod), "deployed without the key: not configured, never the mock key")

	none := &config.Config{Env: config.EnvLocal}
	require.Empty(t, providersFromConfig(none))
}

func TestErrProviderIsTheOnlyWrap(t *testing.T) {
	srv, _ := fakeMSG91(t, http.StatusTeapot, `{"type":"error"}`)
	p := &MSG91SMS{Client: loopback, BaseURL: srv.URL, AuthKey: "k"}
	_, err := p.Send(context.Background(), unseenMessage())
	require.True(t, errors.Is(err, ErrProvider))
	require.Contains(t, err.Error(), "status 418")
}
