package scrub

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// SC-AI-03: personal values never reach a model (11 §1 rule 5).

func TestTextReplacesPersonalValues(t *testing.T) {
	cases := []struct {
		name, in, want string
		kind           Kind
	}{
		{"phone plain", "call 9876543210 now", "call [phone] now", Phone},
		{"phone +91", "call +919876543210", "call [phone]", Phone},
		{"phone +91 spaced", "call +91 98765 43210", "call [phone]", Phone},
		{"phone 0 prefix", "call 09876543210", "call [phone]", Phone},
		{"phone dashed", "ph 98765-43210.", "ph [phone].", Phone},
		{"email", "mail ravi.k@example.com today", "mail [email] today", Email},
		{"upi", "pay to ravi@okhdfc", "pay to [upi]", UPI},
		{"pan", "PAN ABCDE1234F", "PAN [id]", ID},
		{"gstin", "GST 29ABCDE1234F1Z5 ok", "GST [id] ok", ID},
		{"aadhaar", "aadhaar 2345 6789 0123", "aadhaar [id]", ID},
		{"card", "card 4111 1111 1111 1111", "card [card]", Card},
		{"pin code", "Bengaluru 560001", "Bengaluru [pin]", Pin},
		{"otp", "your OTP is 482913", "your OTP is [code]", Code},
		{"pickup code", "pickup code: 4821", "pickup code: [code]", Code},
		{"hindi code", "कोड 7731 है", "कोड [code] है", Code},
		{"kannada code", "ಕೋಡ್ 5520", "ಕೋಡ್ [code]", Code},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Text(c.in, Options{})
			assert.Equal(t, c.want, r.Text)
			assert.Positive(t, r.Found[c.kind])
		})
	}
}

func TestTextKeepsQuantitiesAndPrices(t *testing.T) {
	for _, s := range []string{
		"10 kg rice, 24 packs Parle-G, 1,200 g jeera",
		"चावल १० किलो, तेल 5 लीटर",
		"Maggi 120 150 and 2.5 kg dal at ₹45",
		"₹1,25,000 sale today, 3 dozen eggs",
		"ಸಕ್ಕರೆ 3 kg ಬಂತು",
		"order 12345 is late", // order numbers below six digits are not pin codes
	} {
		assert.True(t, Clean(s), s)
	}
}

func TestTextKnownNamesAndAddresses(t *testing.T) {
	r := Text("Ravi Kumar from 12 MG Road wants Ravi's order; ravindra is someone else",
		Options{Names: []string{"Ravi", "Ravi Kumar", " "}, Addresses: []string{"12 MG Road"}})
	assert.Equal(t, "[name] from [address] wants [name]'s order; ravindra is someone else", r.Text)
	assert.Equal(t, 2, r.Found[Name])
	assert.Equal(t, 1, r.Found[Address])
}

func TestTextNothingToScrub(t *testing.T) {
	r := Text("", Options{})
	assert.Empty(t, r.Text)
	assert.Empty(t, r.Found)
}
