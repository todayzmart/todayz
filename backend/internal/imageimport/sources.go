package imageimport

import "strings"

// Source is one of the Open Food Facts family of open databases. Their images
// are CC BY-SA 3.0: we keep the source, the product page, the licence and the
// author with every copy and show the credit wherever the image is shown.
type Source struct {
	Code    string // product_images.source
	Name    string
	BaseURL string // API and product pages
	Author  string // the attribution the licence asks for
}

// Licence is the licence of every imported image.
const Licence = "CC BY-SA 3.0"

// Sources are the three databases, in fallback order.
var Sources = []Source{
	{Code: "off", Name: "Open Food Facts", BaseURL: "https://world.openfoodfacts.org", Author: "Open Food Facts contributors"},
	{Code: "obf", Name: "Open Beauty Facts", BaseURL: "https://world.openbeautyfacts.org", Author: "Open Beauty Facts contributors"},
	{Code: "opf", Name: "Open Products Facts", BaseURL: "https://world.openproductsfacts.org", Author: "Open Products Facts contributors"},
}

// ImageHosts are the hosts an image may be copied from.
var ImageHosts = []string{"images.openfoodfacts.org", "images.openbeautyfacts.org", "images.openproductsfacts.org"}

// skipped reports whether a category stays on the placeholder: a stock photo
// of a medicine pack could show the wrong strength or maker, so medicines get
// only a photo the shop or an admin takes.
func skipped(category, parent string) bool {
	return category == "medicines" || parent == "medicines"
}

// primary is the database most likely to know a product of the category.
func primary(category, parent string) string {
	switch {
	case category == "personal-care" || category == "beauty" || parent == "beauty":
		return "obf"
	case category == "household",
		category == "electronics" || parent == "electronics",
		category == "stationery" || parent == "stationery",
		category == "hardware" || parent == "hardware":
		return "opf"
	}
	return "off"
}

// barcode reports whether s looks like an EAN/UPC (8 to 14 digits).
func barcode(s string) bool {
	if len(s) < 8 || len(s) > 14 { //settings:ok EAN-8 to EAN-14 lengths
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// tokens are the lower-case words of s, without pack sizes and filler.
func tokens(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := f[:0]
	for _, w := range f {
		if len(w) < 2 || filler[w] || unitWord(w) {
			continue
		}
		out = append(out, w)
	}
	return out
}

var filler = map[string]bool{"and": true, "the": true, "of": true, "with": true, "pack": true, "new": true}

// unitWord is a pack size such as 500g, 1l, 200ml or a bare number.
func unitWord(w string) bool {
	i := 0
	for i < len(w) && w[i] >= '0' && w[i] <= '9' {
		i++
	}
	if i == 0 {
		return false
	}
	switch w[i:] {
	case "", "g", "gm", "gms", "kg", "ml", "l", "ltr", "pc", "pcs", "x":
		return true
	}
	return false
}

// nameMatch reports whether a search hit is the product: the brand must be
// among the hit's brands and most of the product's own words (beyond the
// brand) must be in the hit's name. It is strict on purpose; a miss leaves
// the placeholder, a wrong photo misleads a customer.
func nameMatch(name, brand, hitName, hitBrands string) bool {
	bt := tokens(brand)
	if len(bt) == 0 || !containsAll(tokens(hitBrands), bt) {
		return false
	}
	brandWord := map[string]bool{}
	for _, w := range bt {
		brandWord[w] = true
	}
	var want []string
	for _, w := range tokens(name) {
		if !brandWord[w] {
			want = append(want, w)
		}
	}
	if len(want) == 0 {
		return false
	}
	have := map[string]bool{}
	for _, w := range tokens(hitName) {
		have[w] = true
	}
	n := 0
	for _, w := range want {
		if have[w] {
			n++
		}
	}
	return n*3 >= len(want)*2 // at least two thirds
}

func containsAll(have, want []string) bool {
	set := map[string]bool{}
	for _, w := range have {
		set[w] = true
	}
	for _, w := range want {
		if !set[w] {
			return false
		}
	}
	return true
}
