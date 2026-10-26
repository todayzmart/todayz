package search

import (
	discoverapi "github.com/todayz/backend/gen/api/discover"
)

// licenceURLs are the deeds of the licences imported images carry (D-252).
var licenceURLs = map[string]string{
	"CC BY-SA 3.0": "https://creativecommons.org/licenses/by-sa/3.0/",
	"CC BY-SA 4.0": "https://creativecommons.org/licenses/by-sa/4.0/",
}

// imageCredit is the credit a product image's licence asks for: only stock
// photos imported from Open Food Facts and its sister projects (off, obf,
// opf) carry one. A shop's or an admin's own photo needs none.
func imageCredit(source, licence, author, page *string) *discoverapi.DiscoverImageCredit {
	if source == nil || licence == nil || author == nil || page == nil {
		return nil
	}
	switch *source {
	case "off", "obf", "opf":
	default:
		return nil
	}
	return &discoverapi.DiscoverImageCredit{
		Author: *author, Licence: *licence, LicenceUrl: licenceURLs[*licence], SourceUrl: *page,
	}
}
