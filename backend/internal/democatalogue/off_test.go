package democatalogue

import (
	"os"
	"testing"
)

func TestDecodeFixture(t *testing.T) {
	f, err := os.Open("testdata/off-search.json")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("Decode() got %d products, want 5", len(got))
	}
	if got[0].Code != "8901234567890" || got[0].Name != "Demo Atta" {
		t.Fatalf("unexpected first product: %+v", got[0])
	}
}

func TestCategoryFor(t *testing.T) {
	cases := []struct {
		tags, want string
		ok         bool
	}{
		{"en:flours,en:wheat-flour", "staples", true},
		{"en:milk,en:dairy-products", "dairy-eggs", true},
		{"en:biscuits,en:snacks", "snacks", true},
		{"en:beverages,en:potato-chips", "snacks", true},
		{"en:beverages,en:vegetable-oils", "spices-oils", true},
		{"en:cleaning-products", "household", true},
		{"en:household-chemicals,en:detergents", "household", true},
		{"en:oral-hygiene-products,en:toothpastes", "personal-care", true},
		{"en:hair-care,en:shampoos", "personal-care", true},
		{"en:household-cleaners,en:dishwashing-products", "household", true},
		{"en:breakfast-cereals,en:oat-flakes", "staples", true},
		{"en:biscuits,en:breakfast", "staples", true},
		{"en:medicines,en:vitamins", "", false},
	}
	for _, tc := range cases {
		got, ok := CategoryFor([]string{tc.tags}, "")
		if got != tc.want || ok != tc.ok {
			t.Errorf("CategoryFor(%q)=(%q,%v), want (%q,%v)", tc.tags, got, ok, tc.want, tc.ok)
		}
	}
}

func TestCategoryForSource(t *testing.T) {
	cases := []struct {
		source, tags, want string
		ok                 bool
	}{
		{"obf", "en:shampoos,en:hair-care", "personal-care", true},
		{"obf", "en:beauty-products,en:skin-care", "personal-care", true},
		{"obf", "en:breakfast-cereals", "", false},
		{"opf", "en:cleaning-products,en:household-cleaners", "household", true},
		{"opf", "en:office-supplies", "", false},
		{"off", "en:breakfast-cereals", "staples", true},
	}
	for _, tc := range cases {
		got, ok := CategoryForSource(tc.source, []string{tc.tags}, "", "")
		if got != tc.want || ok != tc.ok {
			t.Errorf("CategoryForSource(%q,%q)=(%q,%v), want (%q,%v)", tc.source, tc.tags, got, ok, tc.want, tc.ok)
		}
	}
}
