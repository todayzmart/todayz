package admin

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"

	api "github.com/todayz/backend/gen/api/admin-notifications"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/notice"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-16 Notifications, read-only for v1 (D-224): the catalogue's texts per kind
// and language, each channel's switch, keys (set or missing, D-19) and last
// error, and a test send that writes only to the caller's own in-app inbox.
// The texts stay code-owned in internal/notify until DLT re-registration has
// a process, so there is no edit.

// NotifyTemplate is one notification kind (internal/notify's catalogue,
// copied by cmd/todayz).
type NotifyTemplate struct {
	Kind      string
	App       string // "customer" or "business"
	Channels  []string
	Params    []string
	Texts     []NotifyText
	Providers []NotifyProviderTemplate
}

// NotifyText is a kind's (or a variant's) unrendered text in one language.
type NotifyText struct {
	Lang, Variant, Title, Body string
	Fallback                   bool
}

// NotifyProviderTemplate is the provider template a language uses, and
// whether its DLT template id is configured.
type NotifyProviderTemplate struct {
	Lang, Name string
	IDSet      bool
}

// Integration is one outside channel: whether this build has a sender for it
// and the variables it needs, by name.
type Integration struct {
	Channel     string
	SenderBuilt bool
	Vars        []ProviderKey
}

// channelSwitch is each channel's platform_settings switch.
var channelSwitch = map[string]string{
	string(notice.Push):     "notify_push_enabled",
	string(notice.SMS):      "notify_sms_enabled",
	string(notice.WhatsApp): "notify_whatsapp_enabled",
}

const appCustomer = "customer"

func (s *Service) listNotificationTemplates(app *api.AdminNotificationApp) api.AdminNotificationTemplateList {
	items := []api.AdminNotificationTemplate{}
	for _, t := range s.templates() {
		if app != nil && t.App != string(*app) {
			continue
		}
		items = append(items, templateOut(t))
	}
	return api.AdminNotificationTemplateList{Items: items, ServerNow: s.now()}
}

func (s *Service) templates() []NotifyTemplate {
	if s.notifyTemplates == nil {
		return nil
	}
	return s.notifyTemplates()
}

func templateOut(t NotifyTemplate) api.AdminNotificationTemplate {
	out := api.AdminNotificationTemplate{
		Kind: t.Kind, App: api.AdminNotificationApp(t.App), Params: slices.Clone(t.Params),
		Channels: []api.AdminNotificationChannel{}, Texts: []api.AdminNotificationText{},
		ProviderTemplates: []api.AdminNotificationProviderTemplate{},
		Testable:          t.App == appCustomer,
	}
	if out.Params == nil {
		out.Params = []string{}
	}
	for _, c := range t.Channels {
		out.Channels = append(out.Channels, api.AdminNotificationChannel(c))
	}
	for _, tx := range t.Texts {
		var v *string
		if tx.Variant != "" {
			v = &tx.Variant
		}
		out.Texts = append(out.Texts, api.AdminNotificationText{
			Lang: api.AdminNotificationLang(tx.Lang), Variant: v, Title: tx.Title, Body: tx.Body, Fallback: tx.Fallback,
		})
	}
	for _, p := range t.Providers {
		out.ProviderTemplates = append(out.ProviderTemplates, api.AdminNotificationProviderTemplate{
			Lang: api.AdminNotificationLang(p.Lang), Name: p.Name, SmsTemplateIdSet: p.IDSet,
		})
	}
	return out
}

func (s *Service) listIntegrations(ctx context.Context) (api.AdminIntegrationList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminIntegrationList{}, err
	}
	q := admindb.New(s.db.Q(ctx))
	rows, err := q.NotifyChannelSwitches(ctx)
	if err != nil {
		return api.AdminIntegrationList{}, err
	}
	on := map[string]bool{}
	for _, r := range rows {
		on[r.Key] = r.Enabled
	}
	items := make([]api.AdminIntegration, 0, len(s.integrations))
	for _, in := range s.integrations {
		it := api.AdminIntegration{
			Channel: api.AdminNotificationChannel(in.Channel), Enabled: on[channelSwitch[in.Channel]],
			SenderBuilt: in.SenderBuilt, Vars: []api.AdminIntegrationVar{},
		}
		for _, v := range in.Vars {
			it.Vars = append(it.Vars, api.AdminIntegrationVar{Name: v.Name, Set: v.Set})
		}
		last, err := q.LastFailedDelivery(ctx, in.Channel)
		switch {
		case err == nil:
			it.LastError = &api.AdminIntegrationError{At: last.UpdatedAt.UTC(), Error: last.Error}
		case !db.IsNoRows(err):
			return api.AdminIntegrationList{}, err
		}
		items = append(items, it)
	}
	return api.AdminIntegrationList{Items: items, ServerNow: s.now()}, nil
}

// testSend writes one notification of a customer kind to actor's own
// customer inbox. Outside is empty: nothing goes by SMS, WhatsApp or push.
func (s *Service) testSend(ctx context.Context, actor ids.ID, in api.AdminNotificationTestSend) (api.AdminNotificationTestResult, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminNotificationTestResult{}, err
	}
	if s.notices == nil || s.notifyTemplates == nil {
		telemetry.Logger(ctx).ErrorContext(ctx, "admin: the notification sender is not configured")
		return api.AdminNotificationTestResult{}, problem.Wrap(genproblems.ServiceUnavailable, errUnavailable)
	}
	var tmpl *NotifyTemplate
	for _, t := range s.notifyTemplates() {
		if t.Kind == in.Kind {
			tmpl = &t
			break
		}
	}
	switch {
	case tmpl == nil:
		return api.AdminNotificationTestResult{}, fieldErr("body.kind", "unknown kind")
	case tmpl.App != appCustomer:
		return api.AdminNotificationTestResult{}, fieldErr("body.kind", "a business kind has no personal inbox")
	}
	params := map[string]string{}
	for _, p := range tmpl.Params {
		params[p] = "{" + p + "}"
	}
	variant := ""
	if in.Variant != nil {
		variant = *in.Variant
	}
	if _, ok := params["variant"]; ok {
		params["variant"] = variant
	}
	if variant != "" && !slices.ContainsFunc(tmpl.Texts, func(t NotifyText) bool { return t.Variant == variant }) {
		return api.AdminNotificationTestResult{}, fieldErr("body.variant", "not a variant of this kind")
	}

	now := s.now()
	var id ids.ID
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		tx, ok := db.TxFrom(ctx)
		if !ok {
			return pgx.ErrTxClosed
		}
		var err error
		id, err = s.notices.Send(ctx, tx, notice.Notification{Kind: tmpl.Kind, UserID: actor, Params: params})
		if err != nil {
			return err
		}
		after := map[string]any{"notificationId": id.String(), "inbox": appCustomer}
		if variant != "" {
			after["variant"] = variant
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "notification.test_send",
			SubjectType: "notification_template", SubjectID: tmpl.Kind, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminNotificationTestResult{}, err
	}
	out := api.AdminNotificationTestResult{NotificationId: id.String(), Kind: tmpl.Kind, SentAt: now}
	if variant != "" {
		out.Variant = &variant
	}
	return out, nil
}
