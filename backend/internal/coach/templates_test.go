package coach

import (
	"encoding/json"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"github.com/todayz/backend/internal/ai/schema"
	"github.com/todayz/backend/internal/coach/detectors"
)

// Sample values of the text placeholders; every other placeholder is a count.
var sampleStrings = map[string]string{
	"item": "Amul Butter 100 g", "itemA": "Maggi 70 g", "itemB": "Amul Taaza 500 ml", "leaders": "Maggi 70 g · Amul Butter 100 g",
	"window": "7–9 pm", "weekday": "Thursday", "km": "1.5 km", "sales": "₹18,240", "cost": "₹52", "price": "₹48",
	"now": "3.8", "before": "4.4",
}

// TestTemplatesAreGrounded: every template of every detector, variant and
// language renders from its numbers alone, fits the card limits and has no
// number that is not one of the fact's (11 §1 rule 6), for counts of 1 and more.
func TestTemplatesAreGrounded(t *testing.T) {
	names := map[string]bool{}
	for _, n := range detectors.Names() {
		names[n] = true
		require.Contains(t, templates, n, "detector without a template")
	}
	for det, byVariant := range templates {
		require.True(t, names[det], "template of no detector: %s", det)
		for variant, byLang := range byVariant {
			en, ok := byLang["en"]
			require.True(t, ok, "%s/%s has no en", det, variant)
			keys := map[string]bool{}
			for _, s := range []string{en.Title, en.Body} {
				for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
					keys[m[1]] = true
				}
			}
			for _, lang := range Langs {
				lt, ok := byLang[lang]
				require.True(t, ok, "%s/%s has no %s", det, variant, lang)
				for _, s := range []string{lt.Title, lt.Body} {
					for _, m := range rePlaceholder.FindAllStringSubmatch(s, -1) {
						require.True(t, keys[m[1]], "%s/%s/%s uses %s, en does not", det, variant, lang, m[1])
					}
				}
				for _, count := range []int64{1, 7, 1234} {
					nums := map[string]any{}
					for k := range keys {
						if s, ok := sampleStrings[k]; ok {
							nums[k] = s
						} else {
							nums[k] = count
						}
					}
					f := detectors.Fact{Detector: det, Variant: variant, Numbers: nums}
					out, err := render(f, lang)
					require.NoError(t, err, "%s/%s/%s", det, variant, lang)
					require.NotEmpty(t, out.Title)
					require.NotEmpty(t, out.Body)
					require.LessOrEqual(t, utf8.RuneCountInString(out.Title), titleMax)
					require.LessOrEqual(t, utf8.RuneCountInString(out.Body), bodyMax)
					b, err := json.Marshal(nums)
					require.NoError(t, err)
					require.Empty(t, schema.Ungrounded(out.Title+"\n"+out.Body, string(b)),
						"%s/%s/%s: %q %q", det, variant, lang, out.Title, out.Body)
				}
			}
		}
	}
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
