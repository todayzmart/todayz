package querynorm

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func kinds(p Parsed) map[string]string {
	m := map[string]string{}
	for _, c := range p.Chips {
		m[c.Kind] = c.Value
	}
	return m
}

// 01 §11 refine chips: the deterministic parser in the five languages and their digits.
func TestParse(t *testing.T) {
	brands := []Brand{{Name: "Nandini", Slug: "nandini"}, {Name: "Amul", Slug: "amul"}}
	for _, tc := range []struct {
		q    string
		want map[string]string
		rest string
	}{
		{"milk under ₹200", map[string]string{KindMaxPrice: "20000"}, "milk"},
		{"atta below 300 rs open now", map[string]string{KindMaxPrice: "30000", KindOpenNow: ""}, "atta"},
		{"oil under 500 ml", map[string]string{}, "oil under 500 ml"},
		{"Nandini milk near me with home delivery", map[string]string{KindBrand: "nandini", KindNearest: "", KindDelivery: ""}, "milk with"},
		{"amul butter on offer", map[string]string{KindBrand: "amul", KindOffers: ""}, "butter"},
		{"deliver by 7 pm rice", map[string]string{KindDelivery: ""}, "by 7 pm rice"},
		{"doodh 50 rupaye se kam ghar pe", map[string]string{KindMaxPrice: "5000", KindDelivery: ""}, "doodh"},
		{"दूध ₹५० से कम पास में", map[string]string{KindMaxPrice: "5000", KindNearest: ""}, "दूध"},
		{"2 kg tak chawal", map[string]string{}, "2 kg tak chawal"},
		{"ಹಾಲು 100 ರೂಪಾಯಿ ಒಳಗೆ ಹತ್ತಿರದ ಅಂಗಡಿ", map[string]string{KindMaxPrice: "10000", KindNearest: ""}, "ಹಾಲು ಅಂಗಡಿ"},
		{"పాలు ౧౦౦ రూపాయల లోపు డెలివరీ", map[string]string{KindMaxPrice: "10000", KindDelivery: ""}, "పాలు"},
		{"பால் 100 ரூபாய்க்குள் திறந்த கடை", map[string]string{KindMaxPrice: "10000", KindOpenNow: ""}, "பால் கடை"},
		{"pickup amulya ghee", map[string]string{KindPickup: ""}, "amulya ghee"},
		{"dolo 650", map[string]string{}, "dolo 650"},
		{"salesman", map[string]string{}, "salesman"},
	} {
		t.Run(tc.q, func(t *testing.T) {
			p := Parse(tc.q, brands)
			assert.Equal(t, tc.want, kinds(p))
			assert.Equal(t, tc.rest, p.Rest)
			for _, c := range p.Chips {
				assert.NotEmpty(t, c.Text)
			}
		})
	}
}
