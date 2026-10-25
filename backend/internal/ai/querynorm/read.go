package querynorm

import (
	"context"

	"github.com/todayz/backend/gen/sqlc/aiconsumerdb"
	"github.com/todayz/backend/internal/platform/db"
)

// ReadQuery is the refine-chip parser as search and voice search run it (01 §11.1, D-101): the
// deterministic patterns plus the master-catalogue brands the query names. No model, nothing
// stored, nothing logged.
func ReadQuery(ctx context.Context, d *db.DB, q string) (Parsed, error) {
	brands, err := BrandsNamedIn(ctx, d, q)
	if err != nil {
		return Parsed{}, err
	}
	return Parse(q, brands), nil
}

// BrandsNamedIn is the master-catalogue brands whose name is in the text, longest first.
func BrandsNamedIn(ctx context.Context, d *db.DB, text string) ([]Brand, error) {
	if text == "" {
		return nil, nil
	}
	rows, err := aiconsumerdb.New(d.Q(ctx)).BrandsNamedIn(ctx, text)
	if err != nil {
		return nil, err
	}
	brands := make([]Brand, 0, len(rows))
	for _, r := range rows {
		brands = append(brands, Brand{Name: r.Name, Slug: r.Slug})
	}
	return brands, nil
}
