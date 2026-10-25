package coach

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/coach/detectors"
)

// Sample values of the placeholders that are strings (names, a time window, rupees, a
// rating); every other placeholder is a count.
var sampleStrings = map[string]string{
	"item": "Amul Butter 100 g", "itemA": "Maggi 70 g", "itemB": "Amul Taaza 500 ml", "leaders": "Maggi 70 g · Amul Butter 100 g",
	"window": "7–9 pm", "weekday": "Thursday", "km": "1.5 km", "sales": "₹18,240", "cost": "₹52", "price": "₹48",
	"now": "3.8", "before": "4.4",
}

// textKeys hold names only; their numbers ("100 g") keep their own marks, so factUnits
// does not list them.
var textKeys = map[string]bool{"item": true, "itemA": true, "itemB": true, "leaders": true, "weekday": true}

// placeholders is every key en's title and body of one variant use.
func placeholders(byLang map[string]tmpl) map[string]bool {
	keys := map[string]bool{}
	for _, s := range []string{byLang["en"].Title, byLang["en"].Body} {
		for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
			keys[m[1]] = true
		}
	}
	return keys
}

// goldenFacts is every fact the detector goldens hold: each detector's real shapes.
func goldenFacts(t *testing.T) []detectors.Fact {
	t.Helper()
	files, err := filepath.Glob("detectors/testdata/*.golden.json")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	var out []detectors.Fact
	for _, f := range files {
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		var rows []struct {
			Detector string         `json:"detector"`
			Variant  string         `json:"variant"`
			Numbers  map[string]any `json:"numbers"`
		}
		require.NoError(t, json.Unmarshal(b, &rows), f)
		for _, r := range rows {
			out = append(out, detectors.Fact{Detector: r.Detector, Variant: r.Variant, Numbers: r.Numbers})
		}
	}
	return out
}

// grounded renders f in lang and checks the card the way Rewrite checks the model's.
func grounded(t *testing.T, f detectors.Fact, lang string) {
	t.Helper()
	out, err := render(f, lang)
	require.NoError(t, err, "%s/%s/%s", f.Detector, f.Variant, lang)
	require.NotEmpty(t, out.Title)
	require.NotEmpty(t, out.Body)
	require.LessOrEqual(t, utf8.RuneCountInString(out.Title), titleMax)
	require.LessOrEqual(t, utf8.RuneCountInString(out.Body), bodyMax)
	b, err := json.Marshal(f.Numbers)
	require.NoError(t, err)
	require.Empty(t, schema.UngroundedFactsIn(out.Title+"\n"+out.Body, b, factUnits[f.Detector]),
		"%s/%s/%s: %q %q", f.Detector, f.Variant, lang, out.Title, out.Body)
}

// TestTemplatesAreGrounded: every template of every detector, variant and
// language renders from its numbers alone, fits the card limits and has no
// number that is not one of the fact's in its unit (11 §1 rule 6, D-230):
// for every count 1 (the singular forms), for distinct counts (so a percent
// or an amount can only be grounded by its own fact), and for every fact the
// detector goldens hold.
func TestTemplatesAreGrounded(t *testing.T) {
	names := map[string]bool{}
	for _, n := range detectors.Names() {
		names[n] = true
		require.Contains(t, templates, n, "detector without a template")
	}
	for det, byVariant := range templates {
		require.True(t, names[det], "template of no detector: %s", det)
		for variant, byLang := range byVariant {
			_, ok := byLang["en"]
			require.True(t, ok, "%s/%s has no en", det, variant)
			keys := placeholders(byLang)
			sorted := make([]string, 0, len(keys))
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, lang := range Langs {
				lt, ok := byLang[lang]
				require.True(t, ok, "%s/%s has no %s", det, variant, lang)
				for _, s := range []string{lt.Title, lt.Body} {
					for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
						require.True(t, keys[m[1]], "%s/%s/%s uses %s, en does not", det, variant, lang, m[1])
					}
				}
				for _, base := range []int64{1, 7, 1234} {
					nums := map[string]any{}
					for i, k := range sorted {
						switch s, ok := sampleStrings[k]; {
						case ok:
							nums[k] = s
						case base == 1:
							nums[k] = int64(1)
						default:
							nums[k] = base + int64(i)*11 // distinct per key
						}
					}
					grounded(t, detectors.Fact{Detector: det, Variant: variant, Numbers: nums}, lang)
				}
			}
		}
	}
	for _, f := range goldenFacts(t) {
		for _, lang := range Langs {
			grounded(t, f, lang)
		}
	}
}

// TestFactUnitsCoverEveryNumber: every detector has a units entry, and every number a
// template or a golden fact carries is in it, so no fact is grounded by its key's name alone.
func TestFactUnitsCoverEveryNumber(t *testing.T) {
	for _, n := range detectors.Names() {
		require.Contains(t, factUnits, n, "detector without units")
	}
	for det := range factUnits {
		require.Contains(t, templates, det, "units of no detector")
	}
	for det, byVariant := range templates {
		for variant, byLang := range byVariant {
			for k := range placeholders(byLang) {
				if !textKeys[k] {
					require.Contains(t, factUnits[det], k, "%s/%s: %s has no unit", det, variant, k)
				}
			}
		}
	}
	for _, f := range goldenFacts(t) {
		for k := range f.Numbers {
			if !textKeys[k] {
				require.Contains(t, factUnits[f.Detector], k, "%s: golden %s has no unit", f.Detector, k)
			}
		}
	}
}

// The case the lead found: expiry_penalty's threshold reads "(limit 30%)" and its key
// name says nothing of a percent, so without factUnits a true card failed grounding.
func TestExpiryPenaltyThresholdIsAPercent(t *testing.T) {
	f := detectors.Fact{Detector: "expiry_penalty", Numbers: map[string]any{"pct": 40, "threshold": 30, "days": 30}}
	out, err := render(f, "en")
	require.NoError(t, err)
	b, err := json.Marshal(f.Numbers)
	require.NoError(t, err)
	text := out.Title + "\n" + out.Body
	require.Contains(t, text, "30%")
	require.NotEmpty(t, schema.UngroundedFacts(text, b), "by key names alone")
	require.Empty(t, schema.UngroundedFactsIn(text, b, factUnits["expiry_penalty"]))
}

func TestRenderNeedsEveryNumber(t *testing.T) {
	_, err := render(detectors.Fact{Detector: "reviews_unanswered", Numbers: map[string]any{"count": 2}}, "en")
	require.Error(t, err)
	_, err = render(detectors.Fact{Detector: "nope"}, "en")
	require.Error(t, err)
	out, err := render(detectors.Fact{Detector: "reviews_unanswered", Numbers: map[string]any{"count": 1, "days": 7}}, "xx")
	require.NoError(t, err, "an unknown language falls back to en")
	require.NotEmpty(t, out.Title)
}

func TestFit(t *testing.T) {
	require.Equal(t, "short", fit("short", 10))
	got := fit("one two three four five", 12)
	require.LessOrEqual(t, utf8.RuneCountInString(got), 12)
	require.Equal(t, "one two…", got)
}
