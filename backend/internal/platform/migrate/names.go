package migrate

import (
	"fmt"
	"regexp"
	"strconv"
)

// Migration numbering (D-89). A file is NNNNN_<wp-id>_<what>.sql, and the
// five-digit number is <wave><wp:2><seq:2>: W1-02 owns 10201–10299, W3-07
// owns 30701–30799. Each work package numbers inside its own block, so two
// WPs built in parallel never pick the same number. Wave 0 keeps the old
// 00001–00099 (00003 is River's Go migration).
//
// goose refuses to apply a file numbered below one a database already has
// (blocks merge in any order). Locally `make reset` fixes such a database.
var reFileName = regexp.MustCompile(`^([0-9]{5})_(w([0-9])-([0-9]{2}))_([a-z0-9]+(?:[_-][a-z0-9]+)*)\.sql$`)

// Block returns the numbers a work package owns: wave 0 shares 1–99, every
// other WP gets <wave><wp>01–<wave><wp>99.
func Block(wave, wp int) (lo, hi int64) {
	if wave == 0 {
		return 1, 99
	}
	base := int64(wave)*10000 + int64(wp)*100
	return base + 1, base + 99
}

// CheckFileName reports whether name follows D-89: the shape, and a number
// inside the block its wp-id owns.
func CheckFileName(name string) error {
	m := reFileName.FindStringSubmatch(name)
	if m == nil {
		return fmt.Errorf("%s: want NNNNN_<wp-id>_<what>.sql, e.g. 10201_w1-02_users.sql (D-89)", name)
	}
	num, _ := strconv.ParseInt(m[1], 10, 64) // five digits by the pattern
	wave, _ := strconv.Atoi(m[3])            // one digit
	wp, _ := strconv.Atoi(m[4])              // two digits
	if num == RiverGooseVersion {
		return fmt.Errorf("%s: version %d is River's Go migration", name, RiverGooseVersion)
	}
	if wave > 0 && wp == 0 {
		return fmt.Errorf("%s: there is no WP %s", name, m[2])
	}
	lo, hi := Block(wave, wp)
	if num < lo || num > hi {
		return fmt.Errorf("%s: %s numbers its files %05d–%05d (D-89)", name, m[2], lo, hi)
	}
	return nil
}
