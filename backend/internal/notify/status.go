package notify

import (
	"context"

	"github.com/todayz/backend/internal/platform/notice"
)

// KeyStatus is one environment variable an integration needs: its name and
// whether it is set. The value is never read into it.
type KeyStatus struct {
	Name string `json:"name"`
	Set  bool   `json:"set"`
}

// ChannelStatus is one outside channel's integration status.
type ChannelStatus struct {
	Channel notice.Channel `json:"channel"`
	// Enabled is notify_<channel>_enabled.
	Enabled bool `json:"enabled"`
	// Configured is whether a provider is built for the channel.
	Configured bool `json:"configured"`
	// Active: enabled and configured; outside messages are queued.
	Active bool        `json:"active"`
	Keys   []KeyStatus `json:"keys"`
	// Note says why a channel cannot be configured yet.
	Note string `json:"note,omitempty"`
}

// IntegrationStatus reports each outside channel as set/missing (for the
// admin integrations view and the startup log), never a value.
func (s *Service) IntegrationStatus(ctx context.Context) ([]ChannelStatus, error) {
	var smsURL, smsKey, sender, waURL, waKey bool
	if c := s.d.Config; c != nil {
		p := c.Providers
		smsURL, smsKey, sender = p.SMSAPIURL != "", p.MSG91AuthKey.IsSet(), p.MSG91SenderID != ""
		waURL, waKey = p.WhatsAppAPIURL != "", p.WhatsAppAPIKey.IsSet()
	}
	out := []ChannelStatus{
		{Channel: notice.SMS, Keys: []KeyStatus{
			{"SMS_API_URL", smsURL}, {"MSG91_AUTH_KEY", smsKey}, {"MSG91_SENDER_ID", sender},
		}},
		{Channel: notice.WhatsApp, Keys: []KeyStatus{
			{"WHATSAPP_API_URL", waURL}, {"WHATSAPP_API_KEY", waKey},
		}, Note: "the WhatsApp sender number is not configurable yet"},
		{Channel: notice.Push, Note: "web push (VAPID) is a later slice"},
	}
	for i := range out {
		on, err := s.channelOn(ctx, out[i].Channel)
		if err != nil {
			return nil, err
		}
		out[i].Enabled = on
		out[i].Configured = s.providers[out[i].Channel] != nil && s.sealer != nil
		out[i].Active = out[i].Enabled && out[i].Configured
	}
	return out, nil
}
