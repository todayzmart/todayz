// Package democatalogue reads a local Open Food Facts search export for the
// local demo catalogue. The export is never embedded in the binary.
package democatalogue

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type Product struct {
	Code           string   `json:"code"`
	Name           string   `json:"product_name"`
	Brands         string   `json:"brands"`
	Quantity       string   `json:"quantity"`
	Categories     string   `json:"categories"`
	CategoriesTags []string `json:"categories_tags"`
	LabelsTags     []string `json:"labels_tags"`
	ImageFrontURL  string   `json:"image_front_url"`
	URL            string   `json:"url"`
	Creator        string   `json:"creator"`
	Contributors   []string `json:"contributors"`
}

type Search struct {
	Products []Product `json:"products"`
}

func Decode(r io.Reader) ([]Product, error) {
	var s Search
	if err := json.NewDecoder(r).Decode(&s); err != nil {
		return nil, fmt.Errorf("decode OFF search: %w", err)
	}
	out := make([]Product, 0, len(s.Products))
	seen := map[string]bool{}
	for _, p := range s.Products {
		p.Code = strings.TrimSpace(p.Code)
		p.Name = clean(p.Name)
		p.Brands = clean(p.Brands)
		p.Quantity = clean(p.Quantity)
		if p.Code == "" || p.Name == "" || ean(p.Name) || p.ImageFrontURL == "" || !ean(p.Code) || seen[p.Code] {
			continue
		}
		seen[p.Code] = true
		out = append(out, p)
	}
	return out, nil
}

func clean(s string) string { return strings.TrimSpace(strings.Join(strings.Fields(s), " ")) }

var eanPattern = regexp.MustCompile(`^(?:[0-9]{8}|[0-9]{12,14})$`)

func ean(s string) bool { return eanPattern.MatchString(s) }

// CategoryFor maps OFF's English category tags to TodayZ's existing catalogue.
// Health/OTC products are intentionally excluded from ingest.
func CategoryFor(tags []string, labels string) (string, bool) {
	all := strings.ToLower(strings.Join(tags, " ") + " " + labels)
	switch {
	case has(all, "baby-food", "infant-milk", "baby-care"):
		return "baby-care", true
	case has(all, "personal-care", "toothpaste", "oral-hygiene", "hygiene", "shampoo", "hair-care", "hair oil", "en:hair", "soaps", "soap", "deodorant", "skin-care", "skincare", "skin care", "cosmetics", "cosmetic products", "beauty-products", "makeup", "face wash", "cleansers"):
		return "personal-care", true
	case has(all, "household", "cleaning", "detergent", "dishwashing", "laundry", "household-cleaners", "household-chemicals", "cleaning-products", "home-care"):
		return "household", true
	case has(all, "spice", "masala", "oil", "ghee"):
		return "spices-oils", true
	case has(all, "rice", "lentil", "pulse", "dal", "flour", "atta", "staple", "pasta"):
		return "staples", true
	case has(all, "breakfast", "cereal", "oat", "muesli", "corn-flakes", "puffed-rice", "porridge"):
		return "staples", true
	case has(all, "biscuit", "cookie", "cracker", "snack", "chips", "namkeen", "chocolate", "confectionery"):
		return "snacks", true
	case has(all, "dairy", "milk", "yogurt", "cheese", "butter", "eggs"):
		return "dairy-eggs", true
	case has(all, "beverage", "drink", "juice", "tea", "coffee", "water"):
		return "beverages", true
	default:
		return "", false
	}
}

// CategoryForSource narrows the non-food Open Facts databases to the demo
// categories requested for each source. Open Beauty Facts supplies personal
// care; Open Products Facts supplies household and cleaning products.
func CategoryForSource(source string, tags []string, categories, labels string) (string, bool) {
	category, ok := CategoryFor(tags, categories+" "+labels)
	if !ok {
		return "", false
	}
	switch source {
	case "obf":
		if category == "personal-care" || category == "beauty" || category == "skin-care" || category == "hair-care" || category == "makeup" {
			return "personal-care", true
		}
	case "opf":
		if category == "household" {
			return category, true
		}
	case "off":
		return category, true
	}
	return "", false
}
func has(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}
