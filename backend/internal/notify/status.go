package notify

import (
	"context"
	"slices"
	"strings"

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

// unmappedSMSTemplates lists, sorted, the catalogue's SMS template names
// that ids does not map to a DLT template id; their deliveries would fail.
func unmappedSMSTemplates(ids map[string]string) []string {
	var out []string
	for _, def := range catalogue {
		if !slices.Contains(def.Channels, notice.SMS) {
			continue
		}
		for _, name := range def.Templates {
			if ids[name] == "" && !slices.Contains(out, name) {
				out = append(out, name)
			}
		}
	}
	slices.Sort(out)
	return out
}

// IntegrationStatus reports each outside channel as set/missing (for the
// admin integrations view and the startup log), never a value.
func (s *Service) IntegrationStatus(ctx context.Context) ([]ChannelStatus, error) {
	var smsKeys []KeyStatus
	var smsNote string
	var waURL, waKey bool
	if c := s.d.Config; c != nil {
		p := c.Providers
		// D-212: deployed SMS signs with the instance role in AWS_REGION;
		// local and test send only to the mocks at SMS_API_URL.
		if c.Env.IsDeployed() {
			smsKeys = append(smsKeys, KeyStatus{"AWS_REGION", c.AWSRegion != ""})
		} else {
			smsKeys = append(smsKeys, KeyStatus{"SMS_API_URL", p.SMSAPIURL != ""})
		}
		smsKeys = append(smsKeys,
			KeyStatus{"SMS_SENDER_ID", p.SMSSenderID != ""},
			KeyStatus{"SMS_DLT_ENTITY_ID", p.SMSDLTEntityID != ""},
			KeyStatus{"SMS_NOTICE_TEMPLATE_IDS", len(p.SMSNoticeTemplateIDs) > 0},
		)
		if missing := unmappedSMSTemplates(p.SMSNoticeTemplateIDs); len(missing) > 0 {
			smsNote = "no DLT template id for: " + strings.Join(missing, ", ")
		}
		waURL, waKey = p.WhatsAppAPIURL != "", p.WhatsAppAPIKey.IsSet()
	}
	out := []ChannelStatus{
		{Channel: notice.SMS, Keys: smsKeys, Note: smsNote},
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
