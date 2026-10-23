package search

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/gen/sqlc/searchdb"
)

// expand matches the words against the synonym table (product_synonyms) and
// returns the query's slots. A phrase of one to three words that is a term or
// a synonym stands for its canonical terms and every synonym of those terms
// (the second hop: "dahi" → curd → "thayir", "ಮೊಸರು"). The table is the only
// source of synonyms; no model is asked (01 §11.1).
func expand(ctx context.Context, q *searchdb.Queries, words []string) ([]slot, error) {
	if len(words) == 0 {
		return nil, nil
	}
	phrases := ngrams(words)
	inQuery := make(map[string]bool, len(phrases))
	for _, p := range phrases {
		inQuery[p] = true
	}
	rows, err := q.SynonymsFor(ctx, phrases)
	if err != nil {
		return nil, err
	}
	canon := map[string]map[string]bool{} // phrase -> its canonical terms
	add := func(phrase, term string) {
		if canon[phrase] == nil {
			canon[phrase] = map[string]bool{}
		}
		canon[phrase][term] = true
	}
	var terms []string
	seenTerm := map[string]bool{}
	for _, r := range rows {
		if inQuery[r.Synonym] {
			add(r.Synonym, r.Term)
		}
		if inQuery[r.Term] {
			add(r.Term, r.Term)
		}
		if (inQuery[r.Synonym] || inQuery[r.Term]) && !seenTerm[r.Term] {
			seenTerm[r.Term] = true
			terms = append(terms, r.Term)
		}
	}
	alts := map[string][]string{}
	if len(terms) > 0 {
		second, err := q.SynonymsOfTerms(ctx, terms)
		if err != nil {
			return nil, err
		}
		synOf := map[string][]string{}
		for _, r := range second {
			synOf[r.Term] = append(synOf[r.Term], r.Synonym)
		}
		for phrase, ts := range canon {
			// Every alternative goes through the query's own normaliser, so a
			// table row can only ever add letters, marks and digits to the
			// tsquery (never a quote or an operator).
			set := map[string]bool{}
			for t := range ts {
				set[Normalize(t)] = true
				for _, s := range synOf[t] {
					set[Normalize(s)] = true
				}
			}
			delete(set, phrase)
			delete(set, "")
			list := make([]string, 0, len(set))
			for a := range set {
				list = append(list, a)
			}
			sort.Strings(list)
			alts[phrase] = list
		}
	}
	return buildSlots(words, alts), nil
}

// catIndex is the category tree for matching and filtering.
type catIndex struct {
	bySlug   map[string]pgtype.UUID
	slugOf   map[[16]byte]string
	label    map[string]string // slug -> English name (the slug as words without one)
	children map[[16]byte][]pgtype.UUID
	byName   map[string][]pgtype.UUID // normalised name in any language, and the slug as words
}

// loadCategories reads the category tree (a few hundred rows at most).
func loadCategories(ctx context.Context, q *searchdb.Queries) (*catIndex, error) {
	rows, err := q.ListCategories(ctx)
	if err != nil {
		return nil, err
	}
	c := &catIndex{
		bySlug:   make(map[string]pgtype.UUID, len(rows)),
		slugOf:   make(map[[16]byte]string, len(rows)),
		label:    make(map[string]string, len(rows)),
		children: map[[16]byte][]pgtype.UUID{},
		byName:   map[string][]pgtype.UUID{},
	}
	for _, r := range rows {
		c.bySlug[r.Slug] = r.ID
		c.slugOf[r.ID.Bytes] = r.Slug
		c.label[r.Slug] = strings.ReplaceAll(r.Slug, "-", " ")
		if r.ParentID.Valid {
			c.children[r.ParentID.Bytes] = append(c.children[r.ParentID.Bytes], r.ID)
		}
		names := map[string]bool{strings.ReplaceAll(r.Slug, "-", " "): true}
		var langs map[string]string
		if len(r.Names) > 0 && json.Unmarshal(r.Names, &langs) == nil {
			if en := strings.TrimSpace(langs["en"]); en != "" {
				c.label[r.Slug] = en
			}
			for _, n := range langs {
				if nn := Normalize(n); nn != "" {
					names[nn] = true
				}
			}
		}
		for n := range names {
			c.byName[n] = append(c.byName[n], r.ID)
		}
	}
	return c, nil
}

// subtree is the category and every category under it.
func (c *catIndex) subtree(roots ...pgtype.UUID) []pgtype.UUID {
	var out []pgtype.UUID
	seen := map[[16]byte]bool{}
	stack := append([]pgtype.UUID{}, roots...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id.Bytes] {
			continue
		}
		seen[id.Bytes] = true
		out = append(out, id)
		stack = append(stack, c.children[id.Bytes]...)
	}
	return out
}

// slugTree is the filter for a category slug: the category and everything
// under it; ok is false for an unknown slug.
func (c *catIndex) slugTree(slug string) ([]pgtype.UUID, bool) {
	id, ok := c.bySlug[slug]
	if !ok {
		return nil, false
	}
	return c.subtree(id), true
}

// named is the categories (with their subtrees) whose name is exactly the
// query words: searching "dairy eggs" finds what is in that category.
func (c *catIndex) named(core string) []pgtype.UUID {
	ids := c.byName[core]
	if len(ids) == 0 {
		return nil
	}
	return c.subtree(ids...)
}
