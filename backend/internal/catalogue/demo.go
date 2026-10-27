package catalogue

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/ids"
)

// DemoProduct is one locally imported packaged product. Barcode is retained
// as its stable OFF identity and is also how D-252 image import matches it.
type DemoProduct struct {
	Name, Brand, CategorySlug, Barcode, Unit string
	PackSize                                 float64
	MRPPaise                                 int64
}

// ImportDemoProduct inserts a local demo product in the catalogue. The caller
// owns the transaction; the normal generated product search index is built by
// the database from the product fields.
func (s *Service) ImportDemoProduct(ctx context.Context, p DemoProduct) (ids.ID, bool, error) {
	if s.db == nil {
		return ids.Zero, false, fmt.Errorf("catalogue: not configured")
	}
	if p.Name == "" || len(p.Name) > 120 || p.Barcode == "" || p.PackSize <= 0 || p.MRPPaise <= 0 {
		return ids.Zero, false, fmt.Errorf("catalogue: invalid demo product")
	}
	q := s.db.Q(ctx)
	var existing pgtype.UUID
	err := q.QueryRow(ctx, `SELECT id FROM products WHERE barcodes @> ARRAY[$1::text] AND status='active' LIMIT 1`, p.Barcode).Scan(&existing)
	if err == nil {
		id, _ := ids.FromUUID(existing)
		return id, false, nil
	}
	if !db.IsNoRows(err) {
		return ids.Zero, false, err
	}
	var category pgtype.UUID
	if err := q.QueryRow(ctx, `SELECT id FROM categories WHERE slug=$1`, p.CategorySlug).Scan(&category); err != nil {
		return ids.Zero, false, err
	}
	var brand *pgtype.UUID
	if strings.TrimSpace(p.Brand) != "" {
		b := strings.TrimSpace(p.Brand)
		if len(b) > 80 {
			b = b[:80]
		}
		var id pgtype.UUID
		if err := q.QueryRow(ctx, `INSERT INTO brands(id,name,slug,created_at,updated_at) VALUES($1,$2,$3,$4,$4) ON CONFLICT(slug) DO UPDATE SET name=brands.name RETURNING id`, ids.UUID(s.ids.New(s.clock.Now())), b, demoBrandSlug(b), s.clock.Now()).Scan(&id); err != nil {
			return ids.Zero, false, err
		}
		brand = &id
	}
	id := s.ids.New(s.clock.Now())
	_, err = q.Exec(ctx, `INSERT INTO products(id,name,brand_id,category_id,pack_size,unit,mrp_paise,barcodes,status,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,ARRAY[$8::text],'active',$9,$9)`, ids.UUID(id), p.Name, brand, category, p.PackSize, p.Unit, p.MRPPaise, p.Barcode, s.clock.Now())
	if err != nil {
		return ids.Zero, false, err
	}
	return id, true, nil
}

func demoBrandSlug(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	dash := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
