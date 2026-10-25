package search

import (
	"context"
	"sort"
	"strings"

	discoverapi "github.com/todayz/backend/gen/api/discover"
	"github.com/todayz/backend/gen/sqlc/searchdb"
	"github.com/todayz/backend/internal/platform/ids"
)

const defaultSuggestLimit = 8

// Suggest answers GET /v1/discover/suggest while the customer types: brands
// first, then categories, then master products (name, prefix first, typos
// last), at most limit in all. Nothing is logged: a suggestion is not a
// search.
func (s *Service) Suggest(ctx context.Context, p discoverapi.SearchSuggestParams) (discoverapi.DiscoverSuggestionList, error) {
	out := discoverapi.DiscoverSuggestionList{Items: []discoverapi.DiscoverSuggestion{}}
	q, err := s.ready(ctx)
	if err != nil {
		return out, err
	}
	limit := pageLimit(p.Limit, defaultSuggestLimit)
	text := typedText(p.Q)
	norm := Normalize(text)
	if norm == "" {
		return out, nil
	}
	esc := escapeLike(text)
	lim := i32(limit)
	add := func(sg discoverapi.DiscoverSuggestion) bool {
		if len(out.Items) >= limit {
			return false
		}
		out.Items = append(out.Items, sg)
		return true
	}

	brands, err := q.SuggestBrands(ctx, searchdb.SuggestBrandsParams{Pattern: "%" + esc + "%", Q: text, PrefixPattern: esc + "%", Lim: lim})
	if err != nil {
		return out, internal(err)
	}
	for _, b := range brands {
		slug := b.Slug
		if !add(discoverapi.DiscoverSuggestion{Kind: "brand", Text: b.Name, BrandSlug: &slug}) {
			return out, nil
		}
	}

	cats, err := loadCategories(ctx, q)
	if err != nil {
		return out, internal(err)
	}
	var catSlugs []string
	for name, idsOf := range cats.byName {
		if !strings.Contains(name, norm) {
			continue
		}
		for _, id := range idsOf {
			catSlugs = append(catSlugs, cats.slugOf[id.Bytes])
		}
	}
	sort.Strings(catSlugs)
	seen := map[string]bool{}
	for _, slug := range catSlugs {
		if seen[slug] {
			continue
		}
		seen[slug] = true
		sl := slug
		if !add(discoverapi.DiscoverSuggestion{Kind: "category", Text: cats.label[slug], CategorySlug: &sl}) {
			return out, nil
		}
	}

	products, err := q.SuggestProducts(ctx, searchdb.SuggestProductsParams{Pattern: "%" + esc + "%", Q: text, PrefixPattern: esc + "%", Lim: lim})
	if err != nil {
		return out, internal(err)
	}
	for _, pr := range products {
		id, ok := ids.FromUUID(pr.ID)
		if !ok {
			continue
		}
		label := pr.Name
		if pr.Variant != nil && *pr.Variant != "" {
			label += " " + *pr.Variant
		}
		label += " " + packText(pr.PackSize, pr.Unit)
		pid := id.String()
		if !add(discoverapi.DiscoverSuggestion{Kind: "product", Text: label, ProductId: &pid}) {
			return out, nil
		}
	}
	return out, nil
}
