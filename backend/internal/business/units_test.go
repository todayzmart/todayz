package business

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	businessapi "github.com/todayz/backend/gen/api/business"
	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/pii"
)

func ist(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, clock.IST)
	require.NoError(t, err)
	return v
}

func day(rs ...string) []Range {
	out := []Range{}
	for _, r := range rs {
		o, c, _ := strings.Cut(r, "-")
		out = append(out, Range{Open: o, Close: c})
	}
	return out
}

// 2026-10-23 is a Friday.
func TestOpenAt(t *testing.T) {
	h := Hours{
		Weekly: map[string][]Range{
			"mon": day("09:00-13:00", "16:00-21:00"), "tue": day("09:00-13:00", "16:00-21:00"),
			"wed": day("09:00-13:00", "16:00-21:00"), "thu": day("09:00-13:00", "16:00-21:00"),
			"fri": day("09:00-13:00", "16:00-21:00"),
			"sat": day("22:00-02:00"), // past midnight into Sunday
		},
		Holidays: []Holiday{
			{Date: "2026-10-26", Ranges: day()},              // Monday: closed all day
			{Date: "2026-10-27", Ranges: day("10:00-12:00")}, // Tuesday: short day
		},
	}
	cases := []struct {
		at   string
		open bool
	}{
		{"2026-10-23 08:59", false},
		{"2026-10-23 09:00", true},
		{"2026-10-23 12:59", true},
		{"2026-10-23 13:00", false}, // close is exclusive
		{"2026-10-23 16:00", true},
		{"2026-10-23 20:59", true},
		{"2026-10-23 21:00", false},
		{"2026-10-24 12:00", false}, // Saturday day time
		{"2026-10-24 21:59", false},
		{"2026-10-24 22:00", true},
		{"2026-10-24 23:59", true},
		{"2026-10-25 00:00", true}, // Saturday's range past midnight
		{"2026-10-25 01:59", true},
		{"2026-10-25 02:00", false},
		{"2026-10-25 12:00", false}, // Sunday: no hours
		{"2026-10-26 10:00", false}, // closed holiday replaces Monday
		{"2026-10-26 17:00", false},
		{"2026-10-27 09:30", false}, // short holiday replaces Tuesday
		{"2026-10-27 10:30", true},
		{"2026-10-27 17:00", false},
		{"2026-10-28 17:00", true}, // Wednesday as usual
	}
	for _, c := range cases {
		require.Equal(t, c.open, h.OpenAt(ist(t, c.at), nil), c.at)
	}

	// The instant decides, not the zone it is written in: 03:30 UTC is 09:00 IST.
	require.True(t, h.OpenAt(time.Date(2026, 10, 23, 3, 30, 0, 0, time.UTC), nil))
	require.False(t, h.OpenAt(time.Date(2026, 10, 23, 3, 29, 0, 0, time.UTC), nil))

	// Manual close: closed until the instant, open from it.
	until := ist(t, "2026-10-23 10:00")
	require.False(t, h.OpenAt(ist(t, "2026-10-23 09:30"), &until))
	require.True(t, h.OpenAt(ist(t, "2026-10-23 10:00"), &until))
}

func TestOpenAtEdges(t *testing.T) {
	allDay := Hours{Weekly: map[string][]Range{"fri": day("00:00-00:00")}}
	require.True(t, allDay.OpenAt(ist(t, "2026-10-23 00:00"), nil))
	require.True(t, allDay.OpenAt(ist(t, "2026-10-23 23:59"), nil))
	require.False(t, allDay.OpenAt(ist(t, "2026-10-24 00:00"), nil), "24 hours from Friday 00:00 ends at Saturday 00:00")

	toMidnight := Hours{Weekly: map[string][]Range{"fri": day("18:00-24:00")}}
	require.True(t, toMidnight.OpenAt(ist(t, "2026-10-23 23:59"), nil))
	require.False(t, toMidnight.OpenAt(ist(t, "2026-10-24 00:00"), nil))

	// A closed holiday on Saturday does not cut short Friday night's range
	// (documented in OpenAt; a question for the lead).
	night := Hours{
		Weekly:   map[string][]Range{"fri": day("20:00-03:00"), "sat": day("20:00-03:00")},
		Holidays: []Holiday{{Date: "2026-10-24", Ranges: day()}},
	}
	require.True(t, night.OpenAt(ist(t, "2026-10-24 02:00"), nil))
	require.False(t, night.OpenAt(ist(t, "2026-10-24 21:00"), nil))
	require.False(t, night.OpenAt(ist(t, "2026-10-25 02:00"), nil), "the holiday's own night is closed")

	// Stored hours: empty, '{}' and garbage are closed.
	at := ist(t, "2026-10-23 10:00")
	require.False(t, OpenAt(nil, nil, at))
	require.False(t, OpenAt([]byte(`{}`), nil, at))
	require.False(t, OpenAt([]byte(`not json`), nil, at))
	raw, err := Hours{Weekly: map[string][]Range{"fri": day("09:00-18:00")}}.Marshal()
	require.NoError(t, err)
	require.True(t, OpenAt(raw, nil, at))
}

func TestHoursValidate(t *testing.T) {
	reasons := func(h Hours) map[string]string {
		out := map[string]string{}
		for _, e := range h.Validate("body.hours") {
			out[e.Path] = e.Reason
		}
		return out
	}
	require.Empty(t, reasons(Hours{}))
	require.Empty(t, reasons(Hours{Weekly: map[string][]Range{
		"mon": day("09:00-13:00", "16:00-21:00"), "sat": day("22:00-02:00"), "sun": day("00:00-24:00"),
	}}))

	cases := []struct {
		name string
		h    Hours
		path string
	}{
		{"open past 23:59", Hours{Weekly: map[string][]Range{"mon": day("24:00-10:00")}}, "body.hours.weekly.mon[0]"},
		{"bad close", Hours{Weekly: map[string][]Range{"mon": day("09:00-25:00")}}, "body.hours.weekly.mon[0]"},
		{"not HH:MM", Hours{Weekly: map[string][]Range{"mon": day("9:00-18:00")}}, "body.hours.weekly.mon[0]"},
		{"signed", Hours{Weekly: map[string][]Range{"mon": day("+9:00-18:00")}}, "body.hours.weekly.mon[0]"},
		{"overlap", Hours{Weekly: map[string][]Range{"mon": day("09:00-13:00", "12:00-18:00")}}, "body.hours.weekly.mon[1]"},
		{"wraps onto the morning", Hours{Weekly: map[string][]Range{"mon": day("08:00-12:00", "20:00-09:00")}}, "body.hours.weekly.mon[1]"},
		{"too many", Hours{Weekly: map[string][]Range{"mon": day("01:00-02:00", "03:00-04:00", "05:00-06:00", "07:00-08:00", "09:00-10:00")}}, "body.hours.weekly.mon"},
		{"unknown day", Hours{Weekly: map[string][]Range{"funday": day()}}, "body.hours.weekly.funday"},
		{"bad date", Hours{Holidays: []Holiday{{Date: "2026-02-30"}}}, "body.hours.holidays[0].date"},
		{"repeated date", Hours{Holidays: []Holiday{{Date: "2026-10-26"}, {Date: "2026-10-26"}}}, "body.hours.holidays[1].date"},
		{"holiday range", Hours{Holidays: []Holiday{{Date: "2026-10-26", Ranges: day("10:00-xx")}}}, "body.hours.holidays[0].ranges[0]"},
	}
	for _, c := range cases {
		require.Contains(t, reasons(c.h), c.path, c.name)
	}
}

func TestHoursAPIRoundTrip(t *testing.T) {
	mon := businessapi.HoursDay{{Open: "09:00", Close: "18:00"}}
	in := businessapi.StoreHours{
		Weekly:   businessapi.StoreWeek{Mon: &mon},
		Holidays: []businessapi.StoreHoliday{{Date: "2026-10-26", Ranges: businessapi.HoursDay{}}},
	}
	h := HoursFromAPI(in)
	require.Len(t, h.Weekly, 7)
	require.Empty(t, h.Weekly["tue"])
	out := HoursToAPI(h)
	require.Equal(t, mon, *out.Weekly.Mon)
	require.NotNil(t, out.Weekly.Sun)
	require.Empty(t, *out.Weekly.Sun)
	require.Len(t, out.Holidays, 1)
	require.NotNil(t, out.Holidays[0].Ranges)
}

func TestValidPAN(t *testing.T) {
	for _, pan := range []string{"AAPFU0939F", "ABCPE1234F", "AAACT1234Z"} {
		require.True(t, ValidPAN(pan), pan)
	}
	for _, pan := range []string{"", "AAPFU0939", "aapfu0939f", "AAPXU0939F", "AAPFU093AF", "AAPFU09391", "1APFU0939F"} {
		require.False(t, ValidPAN(pan), pan)
	}
}

func TestGSTINReason(t *testing.T) {
	// 27AAPFU0939F1ZV is the public sample GSTIN in GSTN's documentation.
	require.Empty(t, GSTINReason("27AAPFU0939F1ZV", ""))
	require.Empty(t, GSTINReason("27AAPFU0939F1ZV", "AAPFU0939F"))
	require.Equal(t, "must contain the PAN", GSTINReason("27AAPFU0939F1ZV", "ABCPE1234F"))
	require.Equal(t, "check digit does not match", GSTINReason("27AAPFU0939F1ZW", ""))
	require.Equal(t, "must start with a state code", GSTINReason("40AAPFU0939F1ZV", ""))
	require.Equal(t, "must start with a state code", GSTINReason("00AAPFU0939F1ZV", ""))
	require.Equal(t, "must be a GSTIN", GSTINReason("27AAPFU0939F1YV", ""))
	require.Equal(t, "must be a GSTIN", GSTINReason("27AAPFU0939F1Z", ""))
	require.Equal(t, "must be a GSTIN", GSTINReason("27AAPXU0939F1ZV", ""))

	c, ok := gstinCheck("27AAPFU0939F1Z")
	require.True(t, ok)
	require.Equal(t, byte('V'), c)
}

func TestLast4(t *testing.T) {
	require.Equal(t, "939F", *last4("AAPFU0939F"))
	require.Equal(t, "1ZV", (*last4("1ZV")))
	require.Equal(t, "shop", *upiLast4("lakshmishop@okaxis"))
	require.Equal(t, "ab", *upiLast4("ab@upi"))
}

// TestPresignAWSVector is the presigned GET example of the AWS S3 SigV4
// documentation ("Authenticating Requests: Using Query Parameters"), with
// AWS's published example credentials.
func TestPresignAWSVector(t *testing.T) {
	u := presignURL(presignInput{
		Method: "GET", Scheme: "https", Host: "examplebucket.s3.amazonaws.com", Path: "/test.txt",
		Region: "us-east-1", AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", //nolint:gosec // AWS's documented example key
		Time: time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC), Expires: 24 * time.Hour,
	})
	require.True(t, strings.HasSuffix(u, "&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"), u)
	require.True(t, strings.HasPrefix(u, "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&"), u)
}

func TestPresignPutSignsContentType(t *testing.T) {
	p := &presigner{scheme: "http", host: "127.0.0.1:9100", bucket: "private", accessKey: "k", secretKey: "s"}
	now := time.Date(2026, 10, 23, 4, 0, 0, 0, time.UTC)
	u := p.putURL("businesses/B/documents/D", "application/pdf", now)
	require.Contains(t, u, "http://127.0.0.1:9100/private/businesses/B/documents/D?")
	require.Contains(t, u, "X-Amz-SignedHeaders=content-type%3Bhost")
	require.Contains(t, u, "X-Amz-Expires=900")
	require.NotEqual(t, u, p.putURL("businesses/B/documents/D", "image/png", now))
}

func TestSlugify(t *testing.T) {
	cases := map[string][]string{
		"lakshmi-traders-jayanagar": {"Lakshmi Traders", "Jayanagar"},
		"sri-ganesh-kirana-4th-block": {"  Sri Ganesh   Kirana!! ", "4th Block"},
		"store":                       {"श्री गणेश", ""},
		"a-b":                         {"--a--", "--b--"},
	}
	for want, in := range cases {
		require.Equal(t, want, Slugify(in...), in)
	}
	long := Slugify(strings.Repeat("abcdefghij ", 10))
	require.LessOrEqual(t, len(long), slugBaseMax)
	require.False(t, strings.HasSuffix(long, "-"))
}

func TestLeadHashNormalises(t *testing.T) {
	h, err := pii.NewHasher(make([]byte, 32))
	require.NoError(t, err)
	a := leadHash(h, "+919000000021", "Lakshmi  Devi", "Lakshmi Traders", "Bengaluru")
	b := leadHash(h, "+919000000021", "lakshmi devi", " LAKSHMI traders ", "bengaluru")
	c := leadHash(h, "+919000000022", "lakshmi devi", "lakshmi traders", "bengaluru")
	require.Equal(t, a, b)
	require.NotEqual(t, a, c)
	// The fields are separated, so moving text between them changes the hash.
	require.NotEqual(t, leadHash(h, "+919000000021", "ab", "c", "x"), leadHash(h, "+919000000021", "a", "bc", "x"))
}
