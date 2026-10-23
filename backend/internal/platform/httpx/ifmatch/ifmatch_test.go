package ifmatch

import "testing"

func TestParse(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int32
	}{{`"v1"`, 1}, {" \"v42\" ", 42}, {`W/"v7"`, 7}, {`"v3-gzip"`, 3}, {`"v3-zstd"`, 3}, {`W/"v9-br"`, 9}, {`"v0"`, 0}} {
		got, valid := Parse(c.in)
		if !valid || got != c.want {
			t.Errorf("Parse(%q) = %d, %v; want %d", c.in, got, valid, c.want)
		}
	}
	for _, in := range []string{``, `v1`, `"1"`, `"v"`, `"v-1"`, `"v+1"`, `"v1-"`, `"v1-GZIP"`, `"v1-gz ip"`, `w/"v1"`, `"v99999999999"`, `*`, `"v1", "v2"`} {
		if v, valid := Parse(in); valid {
			t.Errorf("Parse(%q) = %d, true; want false", in, v)
		}
	}
}
