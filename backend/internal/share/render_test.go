package share

import (
	"bytes"
	"image/png"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderCard(t *testing.T) {
	b, err := RenderCard(CardImage{Item: "Aashirvaad Shudh Chakki Atta 5 kg with a name long enough to wrap twice and more", Store: "Sri Lakshmi Stores", Locality: "Indiranagar", PricePaise: 24500})
	require.NoError(t, err)
	img, err := png.Decode(bytes.NewReader(b))
	require.NoError(t, err)
	require.Equal(t, 1200, img.Bounds().Dx())
	require.Equal(t, 630, img.Bounds().Dy())
	if p := os.Getenv("SHARE_CARD_PNG"); p != "" { // QA evidence: write the sample card
		require.NoError(t, os.WriteFile(p, b, 0o600))
	}
}

// A name the font cannot draw is left off, never drawn as blanks: the card is
// the one without it. Typographic punctuation is folded to ASCII.
func TestRenderCardLeavesOffIndicNames(t *testing.T) {
	require.Equal(t, "Raju's Store - Main Rd", latin("Raju’s Store – Main Rd"))
	require.Empty(t, latin("आशीर्वाद आटा 5 kg"))
	require.Empty(t, latin("ಶ್ರೀ ಲಕ್ಷ್ಮಿ ಸ್ಟೋರ್ಸ್"))
	require.Equal(t, "Indiranagar", latin("Indiranagar"))

	indic, err := RenderCard(CardImage{Item: "आशीर्वाद आटा", Store: "ಶ್ರೀ ಲಕ್ಷ್ಮಿ ಸ್ಟೋರ್ಸ್", Locality: "Indiranagar", PricePaise: 24500})
	require.NoError(t, err)
	without, err := RenderCard(CardImage{Locality: "Indiranagar", PricePaise: 24500})
	require.NoError(t, err)
	require.Equal(t, without, indic)
}

func TestRupees(t *testing.T) {
	require.Equal(t, "₹38", Rupees(3800))
	require.Equal(t, "₹38.50", Rupees(3850))
	require.Equal(t, "₹0.05", Rupees(5))
}

func TestWrap(t *testing.T) {
	require.Equal(t, []string{"ab cd"}, wrap("ab cd", 1, advance(1)*5, 2))
	lines := wrap("one two three four five six", 1, advance(1)*9, 2)
	require.Len(t, lines, 2)
	require.Equal(t, "one two", lines[0])
	require.Equal(t, "…", string([]rune(lines[1])[len([]rune(lines[1]))-1:]))
	require.Len(t, wrap("abcdefghijkl", 1, advance(1)*5, 3), 3)
}

func TestSignedPutIsStable(t *testing.T) {
	s := &S3Store{scheme: "http", host: "127.0.0.1:9100", bucket: "images", accessKey: "AK", secretKey: "SK"}
	u := s.signedPut("share/ABCDEFGHIJ.png", "image/png", testNow)
	require.Contains(t, u, "http://127.0.0.1:9100/images/share/ABCDEFGHIJ.png?X-Amz-Algorithm=AWS4-HMAC-SHA256")
	require.Contains(t, u, "X-Amz-SignedHeaders=content-type%3Bhost")
	require.Equal(t, u, s.signedPut("share/ABCDEFGHIJ.png", "image/png", testNow))
}
