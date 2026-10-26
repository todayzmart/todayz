package search

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageCredit(t *testing.T) {
	s := func(v string) *string { return &v }
	got := imageCredit(s("off"), s("CC BY-SA 3.0"), s("Open Food Facts contributors"), s("https://world.openfoodfacts.org/product/8901063010390"))
	require.NotNil(t, got)
	require.Equal(t, "Open Food Facts contributors", got.Author)
	require.Equal(t, "https://creativecommons.org/licenses/by-sa/3.0/", got.LicenceUrl)
	require.Equal(t, "https://world.openfoodfacts.org/product/8901063010390", got.SourceUrl)
	// A shop's or an admin's photo, or no image at all, carries no credit.
	require.Nil(t, imageCredit(s("retailer"), nil, nil, nil))
	require.Nil(t, imageCredit(s("admin"), s("CC BY-SA 3.0"), s("x"), s("y")))
	require.Nil(t, imageCredit(nil, nil, nil, nil))
}
