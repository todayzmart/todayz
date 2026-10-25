package notify

import (
	"maps"
	"slices"

	"github.com/todayz/backend/internal/platform/notice"
)

// Template is one catalogue kind as A-16 shows it (W3-05, D-224): read-only,
// since the texts stay code-owned until DLT re-registration has a process.
type Template struct {
	Kind     string
	App      string
	Channels []notice.Channel
	Params   []string
	// Texts has the kind's text, then each variant's, in every language of
	// Langs. Fallback marks a language that shows the English text.
	Texts []TemplateText
	// Providers are the SMS/WhatsApp template names per language.
	Providers []ProviderTemplate
}

// TemplateText is a kind's (or variant's) unrendered text in one language.
type TemplateText struct {
	Lang, Variant, Title, Body string
	Fallback                   bool
}

// ProviderTemplate names the provider template a language's message uses.
type ProviderTemplate struct{ Lang, Name string }

// Templates returns the catalogue sorted by kind, variants by name. Every
// slice is a fresh copy: a caller cannot change what Send uses.
func Templates() []Template {
	out := make([]Template, 0, len(catalogue))
	for _, kind := range Kinds() {
		def := catalogue[kind]
		t := Template{
			Kind:     kind,
			App:      def.App,
			Channels: slices.Clone(def.Channels),
			Params:   slices.Clone(def.Params),
		}
		t.Texts = appendTexts(t.Texts, "", def.Text, def.Text["en"])
		for _, v := range slices.Sorted(maps.Keys(def.Variants)) {
			t.Texts = appendTexts(t.Texts, v, def.Variants[v], def.Variants[v]["en"])
		}
		for _, l := range slices.Sorted(maps.Keys(def.Templates)) {
			t.Providers = append(t.Providers, ProviderTemplate{Lang: l, Name: def.Templates[l]})
		}
		out = append(out, t)
	}
	return out
}

// appendTexts adds one text per language, falling back as textFor does.
func appendTexts(out []TemplateText, variant string, texts map[string]text, en text) []TemplateText {
	for _, l := range Langs {
		tx, ok := texts[l]
		if !ok {
			tx = en
		}
		out = append(out, TemplateText{Lang: l, Variant: variant, Title: tx.Title, Body: tx.Body, Fallback: !ok})
	}
	return out
}
