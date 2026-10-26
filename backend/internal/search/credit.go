package search

import (
	discoverapi "github.com/todayz/backend/gen/api/discover"
)

// licenceURLs are the deeds of the licences imported images carry: CC BY-SA
// 3.0 from Open Food Facts (D-252), and the free licences the item-type photos
// from Wikimedia Commons may carry (D-253, imageimport.CommonsLicences).
var licenceURLs = map[string]string{
	"CC BY-SA 2.0":  "https://creativecommons.org/licenses/by-sa/2.0/",
	"CC BY-SA 2.5":  "https://creativecommons.org/licenses/by-sa/2.5/",
	"CC BY-SA 3.0":  "https://creativecommons.org/licenses/by-sa/3.0/",
	"CC BY-SA 4.0":  "https://creativecommons.org/licenses/by-sa/4.0/",
	"CC BY 2.0":     "https://creativecommons.org/licenses/by/2.0/",
	"CC BY 2.5":     "https://creativecommons.org/licenses/by/2.5/",
	"CC BY 3.0":     "https://creativecommons.org/licenses/by/3.0/",
	"CC BY 4.0":     "https://creativecommons.org/licenses/by/4.0/",
	"CC0":           "https://creativecommons.org/publicdomain/zero/1.0/",
	"Public domain": "https://creativecommons.org/publicdomain/mark/1.0/",
}

// imageCredit is the credit a product image's licence asks for: only stock
// photos imported from Open Food Facts and its sister projects (off, obf,
// opf) and item-type photos from Wikimedia Commons (type, D-253) carry one.
// A shop's or an admin's own photo needs none.
func imageCredit(source, licence, author, page *string) *discoverapi.DiscoverImageCredit {
	if source == nil || licence == nil || author == nil || page == nil {
		return nil
	}
	switch *source {
	case "off", "obf", "opf", "type":
	default:
		return nil
	}
	return &discoverapi.DiscoverImageCredit{
		Author: *author, Licence: *licence, LicenceUrl: licenceURLs[*licence], SourceUrl: *page,
	}
}
