package admin

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/platform/settings"
)

func TestSettingGroupCoversEveryKey(t *testing.T) {
	for _, k := range settings.AllKeys {
		require.NotEqual(t, "other", string(settingGroup(string(k))), "key %s has no 01 §17 group", k)
	}
	require.Equal(t, "stock", string(settingGroup("store_banners_max")))
	require.Equal(t, "credits", string(settingGroup("store_credit_max_paise")))
	require.Equal(t, "other", string(settingGroup("brand_new_key")))
}

func TestSettingValue(t *testing.T) {
	lo, hi := int64(1), int64(20)
	ok := func(unit string, v any, want string) {
		t.Helper()
		got, err := settingValue("ask_max_stores", unit, &lo, &hi, v)
		require.NoError(t, err)
		require.JSONEq(t, want, string(got))
	}
	bad := func(key, unit string, v any) {
		t.Helper()
		_, err := settingValue(key, unit, &lo, &hi, v)
		require.Error(t, err, "%v", v)
	}
	ok("count", float64(20), "20")
	ok("bool", true, "true")
	ok("text", " x ", `"x"`)
	bad("ask_max_stores", "count", float64(21))
	bad("ask_max_stores", "count", float64(0))
	bad("ask_max_stores", "count", 2.5)
	bad("ask_max_stores", "count", "3")
	bad("ask_max_stores", "count", nil)
	bad("feature_x", "bool", "true")
	bad("ai_model_fast", "text", "")
	bad("ai_model_fast", "text", "a\nb")
	bad("min_client_version_admin", "text", "1.4")
	got, err := settingValue("min_client_version_admin", "text", nil, nil, "1.4.0")
	require.NoError(t, err)
	require.JSONEq(t, `"1.4.0"`, string(got))
}
