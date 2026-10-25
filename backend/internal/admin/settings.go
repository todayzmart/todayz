package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"

	api "github.com/todayz/backend/gen/api/admin-settings"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/authn"
	"github.com/todayz/backend/internal/platform/authz"
	"github.com/todayz/backend/internal/platform/authz/policy"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
)

// A-17 Settings: every platform_settings row (01 §17) with its unit, range and
// group. A change needs a fresh step-up (slot 15) and a reason; a money
// setting (paise, bp, credits) is super_admin only (D-64). Each change is one
// audit row ("settings.update" on platform_setting/<key>) in the same
// transaction, and the table's NOTIFY trigger reloads every settings cache.

const maxTextValue = 200 //settings:ok contract bound (AdminSettingValue text ≤ 200)

// ProviderKey is one provider key's environment variable name and whether it
// is set (D-19); never its value.
type ProviderKey struct {
	Name string
	Set  bool
}

// groupPrefixes follows the 01 §17 sections, most specific first; a key no
// prefix claims is "other".
var groupPrefixes = []struct {
	prefix string
	group  api.AdminSettingGroup
}{
	{"order_", "orders"}, {"pickup_", "orders"}, {"partial_", "orders"}, {"delivery_", "orders"},
	{"noshow_", "orders"}, {"shop_expiry", "orders"}, {"store_strike", "orders"}, {"risk_code", "orders"},
	{"store_banners", "stock"}, {"stock_", "stock"}, {"low_stock", "stock"},
	{"store_credit", "credits"}, {"credit_", "credits"}, {"admin_adjust", "credits"},
	{"review_", "reviews"},
	{"ref_", "referrals"},
	{"pos_", "pos"},
	{"search_w_", "search"},
	{"insight_", "insights"}, {"coach_", "insights"},
	{"field_agent", "field"},
	{"job_", "jobs"},
	{"ask_", "asks"}, {"site_marquee", "asks"}, {"refine_ai", "asks"}, {"share_card", "asks"},
	{"basket_", "baskets"},
	{"customer_", "customers"},
	{"risk_", "risk"},
	{"feature_", "features"}, {"notify_", "features"},
	{"ai_", "ai"},
	{"min_client_version", "clients"},
}

// groupOrder is the A-17 table's order of groups.
var groupOrder = []api.AdminSettingGroup{
	"orders", "stock", "credits", "reviews", "referrals", "pos", "search", "insights", "field", "jobs",
	"asks", "baskets", "customers", "risk", "features", "ai", "clients", "other",
}

func settingGroup(key string) api.AdminSettingGroup {
	for _, g := range groupPrefixes {
		if strings.HasPrefix(key, g.prefix) {
			return g.group
		}
	}
	return "other"
}

func moneyUnit(unit string) bool { return unit == "paise" || unit == "bp" || unit == "credits" }

// semver is the form min_client_version_* takes (the apps compare it).
// unitRange is the widest value any key of a unit may take, whatever its own min and max say: a
// timer is at least a minute and at most a week, an hour count a year, a day count ten years, and
// every other number fits an int32 column (W3-05 review). 30503 gives each key its own range
// inside these.
var unitRange = map[string][2]int64{
	"minutes": {1, 10080},         //settings:ok unit cap (one week)
	"hours":   {1, 8760},          //settings:ok unit cap (one year)
	"days":    {1, 3650},          //settings:ok unit cap (ten years)
	"bp":      {0, 10000},         //settings:ok unit cap (100%)
	"count":   {0, math.MaxInt32}, //settings:ok unit cap (int32)
	"credits": {0, math.MaxInt32}, //settings:ok unit cap (int32)
	"paise":   {0, math.MaxInt32}, //settings:ok unit cap (int32)
	"meters":  {0, math.MaxInt32}, //settings:ok unit cap (int32)
}

// aiModels are the model ids ai_model_fast and ai_model_smart may name; a typo would break every
// AI call (W3-05 review). A new model is added here with a code review.
var aiModels = []string{"claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5"}

var semver = regexp.MustCompile(`^(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})\.(0|[1-9][0-9]{0,3})$`)

// canEdit says whether p's roles may change a setting of this unit; a stale
// step-up does not count against it (the save asks for one).
func (s *Service) canEdit(ctx context.Context, p authn.Principal, unit string) bool {
	if s.az == nil {
		return false
	}
	action := policy.AdminSettingsEdit
	if moneyUnit(unit) {
		action = policy.AdminSettingsEditMoney
	}
	_, err := s.az.Check(ctx, p, action, authz.Scope{})
	return err == nil || errors.Is(err, problem.New(genproblems.StepUpRequired, ""))
}

type settingRow struct {
	key, unit, description string
	value                  json.RawMessage
	min, max               *int64
	updatedBy              *string
	updatedByName          *string
	updatedAt              api.Timestamp
	version                int32
}

func (s *Service) settingView(ctx context.Context, p authn.Principal, r settingRow, edit map[string]bool) (api.AdminSetting, error) {
	var v any
	if err := json.Unmarshal(r.value, &v); err != nil {
		return api.AdminSetting{}, err
	}
	money := moneyUnit(r.unit)
	editable, ok := edit[r.unit]
	if !ok {
		editable = s.canEdit(ctx, p, r.unit)
		edit[r.unit] = editable
	}
	return api.AdminSetting{
		Key: r.key, Group: settingGroup(r.key), Unit: api.AdminSettingUnit(r.unit), Value: v,
		Min: r.min, Max: r.max, Description: r.description, Money: money, Editable: editable,
		UpdatedBy: r.updatedBy, UpdatedByName: r.updatedByName, UpdatedAt: r.updatedAt.UTC(),
		Version: int(r.version),
	}, nil
}

func (s *Service) listSettings(ctx context.Context, p authn.Principal) (api.AdminSettingList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminSettingList{}, err
	}
	rows, err := s.q(ctx).ListSettings(ctx)
	if err != nil {
		return api.AdminSettingList{}, err
	}
	edit := map[string]bool{}
	items := make([]api.AdminSetting, 0, len(rows))
	for _, r := range rows {
		it, err := s.settingView(ctx, p, settingRow{
			key: r.Key, unit: r.Unit, description: r.Description, value: r.Value, min: r.Min, max: r.Max,
			updatedBy: optID(r.UpdatedBy), updatedByName: r.UpdatedByName, updatedAt: r.UpdatedAt, version: r.Version,
		}, edit)
		if err != nil {
			return api.AdminSettingList{}, err
		}
		items = append(items, it)
	}
	// Rows come by key; a stable sort by group keeps that order within a group.
	sortByGroup(items)
	providers := make([]api.AdminProviderKey, 0, len(s.providers))
	for _, k := range s.providers {
		providers = append(providers, api.AdminProviderKey{Name: k.Name, Set: k.Set})
	}
	return api.AdminSettingList{Items: items, Providers: providers, ServerNow: s.now()}, nil
}

func sortByGroup(items []api.AdminSetting) {
	slices.SortStableFunc(items, func(a, b api.AdminSetting) int {
		return slices.Index(groupOrder, a.Group) - slices.Index(groupOrder, b.Group)
	})
}

func (s *Service) setting(ctx context.Context, p authn.Principal, key string) (api.AdminSetting, error) {
	r, err := s.q(ctx).GetSetting(ctx, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminSetting{}, notFound()
	}
	if err != nil {
		return api.AdminSetting{}, err
	}
	return s.settingView(ctx, p, settingRow{
		key: r.Key, unit: r.Unit, description: r.Description, value: r.Value, min: r.Min, max: r.Max,
		updatedBy: optID(r.UpdatedBy), updatedByName: r.UpdatedByName, updatedAt: r.UpdatedAt, version: r.Version,
	}, map[string]bool{})
}

// settingValue checks v against the setting's unit and range and returns its
// JSON form.
func settingValue(key, unit string, lo, hi *int64, v any) (json.RawMessage, error) {
	bad := func(reason string) error { return fieldErr("body.value", reason) }
	switch unit {
	case "bool":
		b, ok := v.(bool)
		if !ok {
			return nil, bad("must be true or false")
		}
		return json.Marshal(b)
	case "text":
		t, ok := v.(string)
		if !ok {
			return nil, bad("must be text")
		}
		t = strings.TrimSpace(t)
		if t == "" || len([]rune(t)) > maxTextValue {
			return nil, bad("1 to 200 characters")
		}
		if strings.IndexFunc(t, unicode.IsControl) >= 0 {
			return nil, bad("must not hold control characters")
		}
		if strings.HasPrefix(key, "min_client_version") && !semver.MatchString(t) {
			return nil, bad("must be a version like 1.4.0")
		}
		if strings.HasPrefix(key, "ai_model_") && !slices.Contains(aiModels, t) {
			return nil, bad("must be one of " + strings.Join(aiModels, ", "))
		}
		return json.Marshal(t)
	}
	wide, ok := unitRange[unit]
	if !ok {
		return nil, bad("this unit cannot be edited here")
	}
	f, ok := v.(float64)
	if !ok {
		if n, isNum := v.(json.Number); isNum {
			var err error
			if f, err = n.Float64(); err != nil {
				return nil, bad("must be a whole number")
			}
		} else {
			return nil, bad("must be a whole number")
		}
	}
	// 2^53 bounds what a JSON number carries exactly.
	if f != math.Trunc(f) || math.Abs(f) > 1<<53 { //settings:ok float64 exact-integer bound
		return nil, bad("must be a whole number")
	}
	n := int64(f)
	if n < wide[0] || n > wide[1] {
		return nil, bad("outside the allowed range")
	}
	if lo != nil && n < *lo {
		return nil, bad("below the minimum")
	}
	if hi != nil && n > *hi {
		return nil, bad("above the maximum")
	}
	return json.Marshal(n)
}

func (s *Service) updateSetting(ctx context.Context, p authn.Principal, key string, body api.AdminSettingChange) (api.AdminSettingDetail, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminSettingDetail{}, err
	}
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminSettingDetail{}, err
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.GetSettingForUpdate(ctx, key)
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		if moneyUnit(cur.Unit) {
			if s.az == nil {
				return problem.New(genproblems.Forbidden, "")
			}
			if _, err := s.az.Check(ctx, p, policy.AdminSettingsEditMoney, authz.Scope{}); err != nil {
				return err
			}
		}
		next, err := settingValue(key, cur.Unit, cur.Min, cur.Max, body.Value)
		if err != nil {
			return err
		}
		if jsonEqual(cur.Value, next) {
			return nil // the current value: nothing changes, nothing is audited
		}
		if err := q.UpdateSetting(ctx, admindb.UpdateSettingParams{Value: next, UpdatedBy: ids.UUID(p.UserID), Key: key}); err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: p.UserID, Action: "settings.update",
			SubjectType: "platform_setting", SubjectID: key,
			Before: map[string]any{"value": cur.Value, "version": cur.Version},
			After:  map[string]any{"value": next, "version": cur.Version + 1, "reason": reason},
		})
		return err
	})
	if err != nil {
		return api.AdminSettingDetail{}, err
	}
	out, err := s.setting(ctx, p, key)
	if err != nil {
		return api.AdminSettingDetail{}, err
	}
	return api.AdminSettingDetail{Setting: out, ServerNow: s.now()}, nil
}

func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return bytes.Equal(a, b)
	}
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return bytes.Equal(xa, ya)
}

func (s *Service) settingHistory(ctx context.Context, key string, limit *int, cursor *string) (api.AdminSettingHistory, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminSettingHistory{}, err
	}
	lim, beforeAt, beforeID, err := pageArgs(limit, cursor)
	if err != nil {
		return api.AdminSettingHistory{}, err
	}
	q := s.q(ctx)
	ok, err := q.SettingExists(ctx, key)
	if err != nil {
		return api.AdminSettingHistory{}, err
	}
	if !ok {
		return api.AdminSettingHistory{}, notFound()
	}
	typ, action := "platform_setting", "settings.update"
	rows, err := q.ListAudit(ctx, admindb.ListAuditParams{
		Action: &action, SubjectType: &typ, SubjectID: &key,
		BeforeAt: beforeAt, BeforeID: beforeID, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminSettingHistory{}, err
	}
	var next *string
	if len(rows) > lim {
		last := rows[lim-1]
		lid, _ := ids.FromUUID(last.ID)
		c := encodeCursor(last.CreatedAt, lid)
		next, rows = &c, rows[:lim]
	}
	items := make([]api.AdminSettingChangeEntry, 0, len(rows))
	for _, r := range rows {
		var before, after struct {
			Value  any     `json:"value"`
			Reason *string `json:"reason"`
		}
		_ = json.Unmarshal(r.Before, &before)
		_ = json.Unmarshal(r.After, &after)
		items = append(items, api.AdminSettingChangeEntry{
			Id: idString(r.ID), ActorId: optID(r.ActorID), ActorName: r.ActorName,
			From: before.Value, To: after.Value, Reason: after.Reason, CreatedAt: r.CreatedAt.UTC(),
		})
	}
	return api.AdminSettingHistory{Items: items, NextCursor: next, ServerNow: s.now()}, nil
}
