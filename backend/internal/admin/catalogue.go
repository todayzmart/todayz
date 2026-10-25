package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	api "github.com/todayz/backend/gen/api/admin-catalogue"
	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/gen/sqlc/admindb"
	"github.com/todayz/backend/internal/platform/audit"
	"github.com/todayz/backend/internal/platform/db"
	"github.com/todayz/backend/internal/platform/httpx/problem"
	"github.com/todayz/backend/internal/platform/ids"
	"github.com/todayz/backend/internal/platform/settings"
	"github.com/todayz/backend/internal/platform/telemetry"
)

// A-08 Master catalogue (04 §2.7, D-37). Products, the review queue for a
// product a store added (approve, merge into an active product, reject),
// categories and search synonyms. Every change writes an audit row in the
// same transaction. A product a store already lists cannot be merged away:
// store items keep pointing at their product.

const (
	prodActive  = "active"
	prodPending = "pending_review"
	prodMerged  = "merged"
	prodRetired = "retired"
)

var reBarcode = regexp.MustCompile(`^[0-9]{8,14}$`)

func productOut(r admindb.ListProductsRow) api.AdminProduct {
	return api.AdminProduct{
		Id: idString(r.ID), Name: r.Name, BrandName: r.BrandName, CategoryId: idString(r.CategoryID),
		CategoryName: r.CategoryName, Variant: r.Variant, PackSize: r.PackSize, Unit: api.AdminProductUnit(r.Unit),
		MrpPaise: r.MrpPaise, Barcodes: nonNil(r.Barcodes), Status: api.AdminProductStatus(r.Status),
		MergedInto: optID(r.MergedInto), StoreCount: int(r.StoreCount),
		CreatedAt: r.CreatedAt.UTC(), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (s *Service) listProducts(ctx context.Context, p api.ListAdminProductsParams) (api.AdminProductList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminProductList{}, err
	}
	lim, at, id, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminProductList{}, err
	}
	args := admindb.ListProductsParams{BeforeAt: at, BeforeID: id, Lim: int32(lim + 1)} // #nosec G115 -- lim is clamped to [1, maxLimit]
	if p.Status != nil {
		v := string(*p.Status)
		args.Status = &v
	}
	if p.CategoryId != nil {
		cid, err := ids.Parse(*p.CategoryId)
		if err != nil {
			return api.AdminProductList{}, fieldErr("query.categoryId", "not an id")
		}
		args.CategoryID = ids.UUID(cid)
	}
	if p.Q != nil {
		if q := strings.TrimSpace(*p.Q); reBarcode.MatchString(q) {
			args.Barcode = &q
		} else if q != "" {
			pat := likePattern(q)
			args.Pattern = &pat
		}
	}
	rows, err := s.q(ctx).ListProducts(ctx, args)
	if err != nil {
		return api.AdminProductList{}, err
	}
	out := api.AdminProductList{Items: []api.AdminProduct{}, ServerNow: s.now()}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, productOut(r))
	}
	return out, nil
}

func (s *Service) product(ctx context.Context, id ids.ID) (api.AdminProduct, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminProduct{}, err
	}
	r, err := s.q(ctx).GetProduct(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminProduct{}, notFound()
	}
	if err != nil {
		return api.AdminProduct{}, err
	}
	return productOut(admindb.ListProductsRow(r)), nil
}

// likePattern is the ILIKE pattern for q with %, _ and \ escaped.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

// cleanName trims a product name or variant; an empty one is refused at path.
func cleanName(path, v string, max int) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" || len([]rune(v)) > max {
		return "", fieldErr(path, "1 to the maximum characters")
	}
	return v, nil
}

// checkCategory refuses a category that does not exist.
func checkCategory(ctx context.Context, q *admindb.Queries, path string, id ids.ID) error {
	_, err := q.CategoryParent(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return fieldErr(path, "no such category")
	}
	return err
}

// checkBarcodes takes the barcode lock and refuses a barcode another active
// or pending product already has.
func checkBarcodes(ctx context.Context, q *admindb.Queries, self ids.ID, codes []string) error {
	if len(codes) == 0 {
		return nil
	}
	if err := q.LockProductBarcodes(ctx); err != nil {
		return err
	}
	taken, err := q.BarcodeTaken(ctx, admindb.BarcodeTakenParams{Barcodes: codes, ID: ids.UUID(self)})
	if err != nil {
		return err
	}
	if taken {
		return problem.New(genproblems.Conflict, "another product has this barcode")
	}
	return nil
}

func (s *Service) createProduct(ctx context.Context, actor ids.ID, body api.AdminProductCreate) (api.AdminProduct, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminProduct{}, err
	}
	name, err := cleanName("body.name", body.Name, 120) //settings:ok contract bound (AdminProductCreate.name ≤ 120)
	if err != nil {
		return api.AdminProduct{}, err
	}
	var variant *string
	if body.Variant != nil {
		v, err := cleanName("body.variant", *body.Variant, 80) //settings:ok contract bound (variant ≤ 80)
		if err != nil {
			return api.AdminProduct{}, err
		}
		variant = &v
	}
	cat, err := ids.Parse(body.CategoryId)
	if err != nil {
		return api.AdminProduct{}, fieldErr("body.categoryId", "no such category")
	}
	var mrp *int64
	if body.MrpPaise != nil {
		v := int64(*body.MrpPaise)
		mrp = &v
	}
	codes := []string{}
	if body.Barcodes != nil {
		codes = *body.Barcodes
	}
	id := ids.New(s.now())
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := checkCategory(ctx, q, "body.categoryId", cat); err != nil {
			return err
		}
		if err := checkBarcodes(ctx, q, id, codes); err != nil {
			return err
		}
		if err := q.InsertProduct(ctx, admindb.InsertProductParams{
			ID: ids.UUID(id), Name: name, CategoryID: ids.UUID(cat), Variant: variant, PackSize: body.PackSize,
			Unit: string(body.Unit), MrpPaise: mrp, Barcodes: codes, Now: s.now(),
		}); err != nil {
			return err
		}
		_, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "catalogue.product.create",
			SubjectType: "product", SubjectID: id.String(),
			After: map[string]any{
				"name": name, "categoryId": cat.String(), "variant": variant, "packSize": body.PackSize,
				"unit": string(body.Unit), "mrpPaise": mrp, "barcodes": codes, "status": prodActive,
			},
		})
		return err
	})
	if err != nil {
		return api.AdminProduct{}, err
	}
	return s.product(ctx, id)
}

// productChange locks the product, runs change and writes the audit row in
// one transaction, then reads the product back.
func (s *Service) productChange(ctx context.Context, actor, id ids.ID, action string,
	change func(ctx context.Context, q *admindb.Queries, cur admindb.LockProductRow) (before, after map[string]any, err error),
) (api.AdminProduct, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminProduct{}, err
	}
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockProduct(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		before, after, err := change(ctx, q, cur)
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: action,
			SubjectType: "product", SubjectID: id.String(), Before: before, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminProduct{}, err
	}
	return s.product(ctx, id)
}

func (s *Service) updateProduct(ctx context.Context, actor, id ids.ID, body api.AdminProductUpdate) (api.AdminProduct, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminProduct{}, err
	}
	return s.productChange(ctx, actor, id, "catalogue.product.update",
		func(ctx context.Context, q *admindb.Queries, cur admindb.LockProductRow) (map[string]any, map[string]any, error) {
			if cur.Status == prodMerged {
				return nil, nil, invalidTransition("a merged product is final")
			}
			next := admindb.UpdateProductParams{
				ID: cur.ID, Name: cur.Name, CategoryID: cur.CategoryID, Variant: cur.Variant, PackSize: cur.PackSize,
				Unit: cur.Unit, MrpPaise: cur.MrpPaise, Barcodes: nonNil(cur.Barcodes), Status: cur.Status, Now: s.now(),
			}
			before, after := map[string]any{}, map[string]any{"reason": reason}
			if body.Name != nil {
				v, err := cleanName("body.name", *body.Name, 120) //settings:ok contract bound (name ≤ 120)
				if err != nil {
					return nil, nil, err
				}
				before["name"], after["name"], next.Name = cur.Name, v, v
			}
			if sent(ctx, "variant") {
				var v *string
				if body.Variant != nil {
					t, err := cleanName("body.variant", *body.Variant, 80) //settings:ok contract bound (variant ≤ 80)
					if err != nil {
						return nil, nil, err
					}
					v = &t
				}
				before["variant"], after["variant"], next.Variant = cur.Variant, v, v
			}
			if body.CategoryId != nil {
				cat, err := ids.Parse(*body.CategoryId)
				if err != nil {
					return nil, nil, fieldErr("body.categoryId", "no such category")
				}
				if err := checkCategory(ctx, q, "body.categoryId", cat); err != nil {
					return nil, nil, err
				}
				before["categoryId"], after["categoryId"], next.CategoryID = idString(cur.CategoryID), cat.String(), ids.UUID(cat)
			}
			if body.PackSize != nil {
				before["packSize"], after["packSize"], next.PackSize = cur.PackSize, *body.PackSize, *body.PackSize
			}
			if body.Unit != nil {
				before["unit"], after["unit"], next.Unit = cur.Unit, string(*body.Unit), string(*body.Unit)
			}
			if sent(ctx, "mrpPaise") {
				before["mrpPaise"], after["mrpPaise"], next.MrpPaise = cur.MrpPaise, body.MrpPaise, body.MrpPaise
			}
			if body.Barcodes != nil {
				codes := *body.Barcodes
				if err := checkBarcodes(ctx, q, id, codes); err != nil {
					return nil, nil, err
				}
				before["barcodes"], after["barcodes"], next.Barcodes = nonNil(cur.Barcodes), codes, codes
			}
			if body.Status != nil {
				if cur.Status == prodPending {
					return nil, nil, invalidTransition("a pending product is decided in the review queue")
				}
				before["status"], after["status"], next.Status = cur.Status, string(*body.Status), string(*body.Status)
			}
			if err := q.UpdateProduct(ctx, next); err != nil {
				return nil, nil, err
			}
			if body.Name != nil || sent(ctx, "variant") {
				if err := q.RefreshStoreItemSearch(ctx, cur.ID); err != nil {
					return nil, nil, err
				}
			}
			return before, after, nil
		})
}

// --- review queue -------------------------------------------------------------

func (s *Service) listPending(ctx context.Context, limit *int, cursor *string) (api.AdminPendingProductList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminPendingProductList{}, err
	}
	lim, at, id, err := pageArgs(limit, cursor)
	if err != nil {
		return api.AdminPendingProductList{}, err
	}
	q := s.q(ctx)
	rows, err := q.ListPendingProducts(ctx, admindb.ListPendingProductsParams{
		AfterAt: at, AfterID: id, Lim: int32(lim + 1), // #nosec G115 -- lim is clamped to [1, maxLimit]
	})
	if err != nil {
		return api.AdminPendingProductList{}, err
	}
	out := api.AdminPendingProductList{Items: []api.AdminPendingProduct{}, ServerNow: s.now()}
	if out.AssistEnabled, err = s.flag(ctx, settings.AiCatalogueEnabled); err != nil {
		return api.AdminPendingProductList{}, err
	}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		m := api.AdminPendingProduct{
			Product: productOut(admindb.ListProductsRow{
				ID: r.ID, Name: r.Name, BrandName: r.BrandName, CategoryID: r.CategoryID, CategoryName: r.CategoryName,
				Variant: r.Variant, PackSize: r.PackSize, Unit: r.Unit, MrpPaise: r.MrpPaise, Barcodes: r.Barcodes,
				Status: r.Status, MergedInto: r.MergedInto, StoreCount: r.StoreCount, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
			}),
			CreatedByStoreId: optID(r.CreatedByStoreID), CreatedByStoreName: r.CreatedByStoreName,
			Suggestions: []api.AdminDuplicateSuggestion{},
		}
		sugg, err := q.ProductSuggestions(ctx, admindb.ProductSuggestionsParams{Name: r.Name, ID: r.ID})
		if err != nil {
			return api.AdminPendingProductList{}, err
		}
		for _, g := range sugg {
			m.Suggestions = append(m.Suggestions, api.AdminDuplicateSuggestion{
				Product: productOut(admindb.ListProductsRow{
					ID: g.ID, Name: g.Name, BrandName: g.BrandName, CategoryID: g.CategoryID, CategoryName: g.CategoryName,
					Variant: g.Variant, PackSize: g.PackSize, Unit: g.Unit, MrpPaise: g.MrpPaise, Barcodes: g.Barcodes,
					Status: g.Status, MergedInto: g.MergedInto, StoreCount: g.StoreCount, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
				}),
				Similarity: min(max(g.Similarity, 0), 1),
			})
		}
		out.Items = append(out.Items, m)
	}
	return out, nil
}

// flag reads a bool setting; a missing one is off.
func (s *Service) flag(ctx context.Context, key settings.Key) (bool, error) {
	r, err := s.q(ctx).GetSetting(ctx, string(key))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var on bool
	if err := json.Unmarshal(r.Value, &on); err != nil {
		return false, fmt.Errorf("admin: setting %s is not a bool: %w", key, err)
	}
	return on, nil
}

// optReason cleans an optional reason.
func optReason(r *string) (*string, error) {
	if r == nil {
		return nil, nil
	}
	v, err := cleanReason(*r)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// decidePending moves a pending product to active or retired.
func (s *Service) decidePending(ctx context.Context, actor, id ids.ID, to, action string, reason *string) (api.AdminProduct, error) {
	return s.productChange(ctx, actor, id, action,
		func(ctx context.Context, q *admindb.Queries, cur admindb.LockProductRow) (map[string]any, map[string]any, error) {
			if cur.Status != prodPending {
				return nil, nil, invalidTransition("the product is not waiting for review")
			}
			if to == prodActive {
				if err := checkBarcodes(ctx, q, id, nonNil(cur.Barcodes)); err != nil {
					return nil, nil, err
				}
			}
			if err := q.SetProductStatus(ctx, admindb.SetProductStatusParams{ID: cur.ID, Status: to, Now: s.now()}); err != nil {
				return nil, nil, err
			}
			after := map[string]any{"status": to}
			if reason != nil {
				after["reason"] = *reason
			}
			return map[string]any{"status": cur.Status}, after, nil
		})
}

func (s *Service) approvePending(ctx context.Context, actor, id ids.ID, body api.AdminPendingApprove) (api.AdminProduct, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminProduct{}, err
	}
	return s.decidePending(ctx, actor, id, prodActive, "catalogue.pending.approve", reason)
}

func (s *Service) rejectPending(ctx context.Context, actor, id ids.ID, body api.AdminPendingReject) (api.AdminProduct, error) {
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminProduct{}, err
	}
	return s.decidePending(ctx, actor, id, prodRetired, "catalogue.pending.reject", &reason)
}

func (s *Service) mergePending(ctx context.Context, actor, id ids.ID, body api.AdminPendingMerge) (api.AdminProduct, error) {
	reason, err := optReason(body.Reason)
	if err != nil {
		return api.AdminProduct{}, err
	}
	into, err := ids.Parse(body.IntoProductId)
	if err != nil || into == id {
		return api.AdminProduct{}, fieldErr("body.intoProductId", "an active product other than this one")
	}
	return s.productChange(ctx, actor, id, "catalogue.pending.merge",
		func(ctx context.Context, q *admindb.Queries, cur admindb.LockProductRow) (map[string]any, map[string]any, error) {
			if cur.Status != prodPending {
				return nil, nil, invalidTransition("the product is not waiting for review")
			}
			st, err := q.ProductStatus(ctx, ids.UUID(into))
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && st != prodActive) {
				return nil, nil, fieldErr("body.intoProductId", "an active product other than this one")
			}
			if err != nil {
				return nil, nil, err
			}
			listed, err := q.ProductListed(ctx, cur.ID)
			if err != nil {
				return nil, nil, err
			}
			if listed {
				return nil, nil, invalidTransition("a store already lists this product")
			}
			if _, err := q.DropDuplicateSynonyms(ctx, admindb.DropDuplicateSynonymsParams{FromID: cur.ID, IntoID: ids.UUID(into)}); err != nil {
				return nil, nil, err
			}
			moved, err := q.MoveSynonyms(ctx, admindb.MoveSynonymsParams{FromID: cur.ID, IntoID: ids.UUID(into)})
			if err != nil {
				return nil, nil, err
			}
			if err := q.SetProductStatus(ctx, admindb.SetProductStatusParams{
				ID: cur.ID, Status: prodMerged, MergedInto: ids.UUID(into), Now: s.now(),
			}); err != nil {
				return nil, nil, err
			}
			after := map[string]any{"status": prodMerged, "mergedInto": into.String(), "synonymsMoved": moved}
			if reason != nil {
				after["reason"] = *reason
			}
			return map[string]any{"status": cur.Status}, after, nil
		})
}

// --- categories ---------------------------------------------------------------

func categoryOut(ctx context.Context, r admindb.ListCategoriesRow) api.AdminCategory {
	var names api.AdminCategoryNames
	if err := json.Unmarshal(r.Names, &names); err != nil {
		telemetry.Logger(ctx).WarnContext(ctx, "admin: stored category names did not parse")
		names = api.AdminCategoryNames{En: r.Slug}
	}
	return api.AdminCategory{
		Id: idString(r.ID), ParentId: optID(r.ParentID), Slug: r.Slug, Names: names, Tint: api.AdminCategoryTint(r.Tint),
		Icon: r.Icon, Sort: int(r.Sort), ProductCount: int(r.ProductCount), UpdatedAt: r.UpdatedAt.UTC(),
	}
}

func (s *Service) listCategories(ctx context.Context) (api.AdminCategoryList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminCategoryList{}, err
	}
	rows, err := s.q(ctx).ListCategories(ctx)
	if err != nil {
		return api.AdminCategoryList{}, err
	}
	out := api.AdminCategoryList{Items: []api.AdminCategory{}}
	for _, r := range rows {
		out.Items = append(out.Items, categoryOut(ctx, r))
	}
	return out, nil
}

func (s *Service) category(ctx context.Context, id ids.ID) (api.AdminCategory, error) {
	r, err := s.q(ctx).GetCategory(ctx, ids.UUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return api.AdminCategory{}, notFound()
	}
	if err != nil {
		return api.AdminCategory{}, err
	}
	return categoryOut(ctx, admindb.ListCategoriesRow(r)), nil
}

// cleanNames trims every name; an empty one is refused.
func cleanNames(n api.AdminCategoryNames) (api.AdminCategoryNames, error) {
	en := strings.TrimSpace(n.En)
	if en == "" {
		return n, fieldErr("body.names.en", "1 to 60 characters")
	}
	out := api.AdminCategoryNames{En: en}
	for _, f := range []struct {
		path string
		in   *string
		out  **string
	}{{"body.names.hi", n.Hi, &out.Hi}, {"body.names.kn", n.Kn, &out.Kn}, {"body.names.te", n.Te, &out.Te}, {"body.names.ta", n.Ta, &out.Ta}} {
		if f.in == nil {
			continue
		}
		v := strings.TrimSpace(*f.in)
		if v == "" {
			return n, fieldErr(f.path, "1 to 60 characters")
		}
		*f.out = &v
	}
	return out, nil
}

func (s *Service) createCategory(ctx context.Context, actor ids.ID, body api.AdminCategoryCreate) (api.AdminCategory, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminCategory{}, err
	}
	names, err := cleanNames(body.Names)
	if err != nil {
		return api.AdminCategory{}, err
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return api.AdminCategory{}, err
	}
	sort := 0
	if body.Sort != nil {
		sort = *body.Sort
	}
	var parent pgtype.UUID
	if body.ParentId != nil {
		pid, err := ids.Parse(*body.ParentId)
		if err != nil {
			return api.AdminCategory{}, fieldErr("body.parentId", "a top-level category")
		}
		parent = ids.UUID(pid)
	}
	id := ids.New(s.now())
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if parent.Valid {
			pp, err := q.CategoryParent(ctx, parent)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && pp.Valid) {
				return fieldErr("body.parentId", "a top-level category")
			}
			if err != nil {
				return err
			}
		}
		if err := q.InsertCategory(ctx, admindb.InsertCategoryParams{
			ID: ids.UUID(id), ParentID: parent, Slug: body.Slug, Names: raw, Tint: string(body.Tint),
			Icon: body.Icon, Sort: int32(sort), Now: s.now(), // #nosec G115 -- the contract bounds sort to [0, 10000]
		}); err != nil {
			return err
		}
		_, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "catalogue.category.create",
			SubjectType: "category", SubjectID: id.String(),
			After: map[string]any{
				"slug": body.Slug, "parentId": optID(parent), "names": names, "tint": string(body.Tint),
				"icon": body.Icon, "sort": sort,
			},
		})
		return err
	})
	if db.IsUniqueViolation(err, "ux_categories_slug") {
		return api.AdminCategory{}, problem.New(genproblems.Conflict, "the slug is in use")
	}
	if err != nil {
		return api.AdminCategory{}, err
	}
	return s.category(ctx, id)
}

func (s *Service) updateCategory(ctx context.Context, actor, id ids.ID, body api.AdminCategoryUpdate) (api.AdminCategory, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminCategory{}, err
	}
	reason, err := cleanReason(body.Reason)
	if err != nil {
		return api.AdminCategory{}, err
	}
	err = s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		cur, err := q.LockCategory(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		next := admindb.UpdateCategoryParams{ID: cur.ID, Names: cur.Names, Tint: cur.Tint, Icon: cur.Icon, Sort: cur.Sort, Now: s.now()}
		before, after := map[string]any{}, map[string]any{"reason": reason}
		if body.Names != nil {
			names, err := cleanNames(*body.Names)
			if err != nil {
				return err
			}
			raw, err := json.Marshal(names)
			if err != nil {
				return err
			}
			before["names"], after["names"], next.Names = cur.Names, names, raw
		}
		if body.Tint != nil {
			before["tint"], after["tint"], next.Tint = cur.Tint, string(*body.Tint), string(*body.Tint)
		}
		if body.Icon != nil {
			before["icon"], after["icon"], next.Icon = cur.Icon, *body.Icon, *body.Icon
		}
		if body.Sort != nil {
			next.Sort = int32(*body.Sort) // #nosec G115 -- the contract bounds sort to [0, 10000]
			before["sort"], after["sort"] = cur.Sort, *body.Sort
		}
		if err := q.UpdateCategory(ctx, next); err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "catalogue.category.update",
			SubjectType: "category", SubjectID: id.String(), Before: before, After: after,
		})
		return err
	})
	if err != nil {
		return api.AdminCategory{}, err
	}
	return s.category(ctx, id)
}

// --- synonyms -----------------------------------------------------------------

func synonymOut(r admindb.ListSynonymsRow) api.AdminSynonym {
	return api.AdminSynonym{
		Id: idString(r.ID), Term: r.Term, Synonym: r.Synonym, Lang: api.AdminSynonymLang(r.Lang),
		ProductId: optID(r.ProductID), ProductName: r.ProductName, CreatedAt: r.CreatedAt.UTC(),
	}
}

func (s *Service) listSynonyms(ctx context.Context, p api.ListAdminSynonymsParams) (api.AdminSynonymList, error) {
	if err := s.ready(ctx); err != nil {
		return api.AdminSynonymList{}, err
	}
	lim, at, id, err := pageArgs(p.Limit, p.Cursor)
	if err != nil {
		return api.AdminSynonymList{}, err
	}
	args := admindb.ListSynonymsParams{BeforeAt: at, BeforeID: id, Lim: int32(lim + 1)} // #nosec G115 -- lim is clamped to [1, maxLimit]
	if p.Q != nil {
		if q := strings.ToLower(strings.TrimSpace(*p.Q)); q != "" {
			pre := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
			args.Prefix = &pre
		}
	}
	rows, err := s.q(ctx).ListSynonyms(ctx, args)
	if err != nil {
		return api.AdminSynonymList{}, err
	}
	out := api.AdminSynonymList{Items: []api.AdminSynonym{}}
	for i, r := range rows {
		if i == lim {
			last := rows[lim-1]
			lid, _ := ids.FromUUID(last.ID)
			c := encodeCursor(last.CreatedAt, lid)
			out.NextCursor = &c
			break
		}
		out.Items = append(out.Items, synonymOut(r))
	}
	return out, nil
}

func (s *Service) createSynonym(ctx context.Context, actor ids.ID, body api.AdminSynonymCreate) (api.AdminSynonym, error) {
	if err := s.writeReady(ctx); err != nil {
		return api.AdminSynonym{}, err
	}
	term := strings.ToLower(strings.TrimSpace(body.Term))
	syn := strings.ToLower(strings.TrimSpace(body.Synonym))
	if term == "" {
		return api.AdminSynonym{}, fieldErr("body.term", "1 to 80 characters")
	}
	if syn == "" || syn == term {
		return api.AdminSynonym{}, fieldErr("body.synonym", "1 to 80 characters, not the term")
	}
	var product pgtype.UUID
	if body.ProductId != nil {
		pid, err := ids.Parse(*body.ProductId)
		if err != nil {
			return api.AdminSynonym{}, fieldErr("body.productId", "an active product")
		}
		product = ids.UUID(pid)
	}
	id := ids.New(s.now())
	err := s.db.InTx(ctx, func(ctx context.Context) error {
		q := s.q(ctx)
		if product.Valid {
			st, err := q.ProductStatus(ctx, product)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && st != prodActive) {
				return fieldErr("body.productId", "an active product")
			}
			if err != nil {
				return err
			}
		}
		if err := q.InsertSynonym(ctx, admindb.InsertSynonymParams{
			ID: ids.UUID(id), Term: term, Synonym: syn, Lang: string(body.Lang), ProductID: product, Now: s.now(),
		}); err != nil {
			return err
		}
		_, err := s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "catalogue.synonym.create",
			SubjectType: "synonym", SubjectID: id.String(),
			After: map[string]any{"term": term, "synonym": syn, "lang": string(body.Lang), "productId": optID(product)},
		})
		return err
	})
	if db.IsUniqueViolation(err, "ux_product_synonyms_pair") {
		return api.AdminSynonym{}, problem.New(genproblems.Conflict, "the pair exists")
	}
	if err != nil {
		return api.AdminSynonym{}, err
	}
	r, err := s.q(ctx).GetSynonym(ctx, ids.UUID(id))
	if err != nil {
		return api.AdminSynonym{}, err
	}
	return synonymOut(admindb.ListSynonymsRow(r)), nil
}

func (s *Service) deleteSynonym(ctx context.Context, actor, id ids.ID) error {
	if err := s.writeReady(ctx); err != nil {
		return err
	}
	return s.db.InTx(ctx, func(ctx context.Context) error {
		r, err := s.q(ctx).DeleteSynonym(ctx, ids.UUID(id))
		if errors.Is(err, pgx.ErrNoRows) {
			return notFound()
		}
		if err != nil {
			return err
		}
		_, err = s.audit.Write(ctx, audit.Entry{
			ActorKind: audit.ActorAdmin, ActorID: actor, Action: "catalogue.synonym.delete",
			SubjectType: "synonym", SubjectID: id.String(),
			Before: map[string]any{"term": r.Term, "synonym": r.Synonym, "lang": r.Lang, "productId": optID(r.ProductID)},
		})
		return err
	})
}
