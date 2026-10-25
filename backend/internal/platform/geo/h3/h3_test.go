package h3

import (
	"bufio"
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// Golden vectors in testdata/ come from the H3 reference library
// (python h3 4.5.0); see the header line of each file.
func readTSV(t *testing.T, name string) [][]string {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows [][]string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rows = append(rows, strings.Split(line, "\t"))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatalf("%s: no rows", name)
	}
	return rows
}

func num(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// groundM is the haversine distance in metres.
func groundM(lat1, lng1, lat2, lng2 float64) float64 {
	const rad = math.Pi / 180
	dlat, dlng := (lat2-lat1)*rad, (lng2-lng1)*rad
	a := math.Sin(dlat/2)*math.Sin(dlat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dlng/2)*math.Sin(dlng/2)
	return 2 * 6371007.180918475 * math.Asin(math.Sqrt(a))
}

func TestLatLngToCellGolden(t *testing.T) {
	bad := 0
	for _, r := range readTSV(t, "latlng_to_cell.tsv") {
		res, _ := strconv.Atoi(r[2])
		got, err := LatLngToCell(num(t, r[0]), num(t, r[1]), res)
		if err != nil || got.String() != r[3] {
			bad++
			if bad <= 10 {
				t.Errorf("LatLngToCell(%s, %s, %d) = %v, %v; want %s", r[0], r[1], res, got, err, r[3])
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d mismatches", bad)
	}
}

func TestCellToLatLngGolden(t *testing.T) {
	bad := 0
	for _, r := range readTSV(t, "cell_to_latlng.tsv") {
		c, err := ParseCell(r[0])
		if err != nil {
			t.Fatal(err)
		}
		lat, lng, err := CellToLatLng(c)
		// Compare by ground distance: the file has 12 decimals, and right at
		// the poles longitude is ill-conditioned (two res-15 polar cells
		// differ from the reference by ~4e-9 degrees, under a millimetre).
		if err != nil || groundM(lat, lng, num(t, r[1]), num(t, r[2])) > 0.01 {
			bad++
			if bad <= 10 {
				t.Errorf("CellToLatLng(%s) = %.12f, %.12f, %v; want %s, %s", r[0], lat, lng, err, r[1], r[2])
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d mismatches", bad)
	}
}

func TestGridDiskGolden(t *testing.T) {
	bad := 0
	for _, r := range readTSV(t, "grid_disk.tsv") {
		c, err := ParseCell(r[0])
		if err != nil {
			t.Fatal(err)
		}
		k, _ := strconv.Atoi(r[1])
		cells, err := GridDisk(c, k)
		if err != nil {
			t.Fatal(err)
		}
		parts := make([]string, len(cells))
		for i, x := range cells {
			parts[i] = x.String()
		}
		if got := strings.Join(parts, ","); got != r[2] {
			bad++
			if bad <= 5 {
				t.Errorf("GridDisk(%s, %d): got %d cells, want %d", r[0], k, len(cells), strings.Count(r[2], ",")+1)
			}
		}
	}
	if bad > 0 {
		t.Errorf("%d mismatches", bad)
	}
}

func TestRoundTripAndForms(t *testing.T) {
	c, err := LatLngToCell(12.9719, 77.6412, 8)
	if err != nil {
		t.Fatal(err)
	}
	if c.String() != "8861892ecbfffff" || c.Resolution() != 8 || !c.IsValid() {
		t.Fatalf("cell %v res %d valid %v", c, c.Resolution(), c.IsValid())
	}
	if FromInt64(c.Int64()) != c || c.Int64() < 0 {
		t.Fatal("int64 round trip")
	}
	p, err := ParseCell("8861892ECBFFFFF")
	if err != nil || p != c {
		t.Fatalf("parse upper case: %v %v", p, err)
	}
	lat, lng, _ := CellToLatLng(c)
	back, _ := LatLngToCell(lat, lng, 8)
	if back != c {
		t.Fatalf("centre does not map back: %v", back)
	}
}

func TestErrors(t *testing.T) {
	if _, err := LatLngToCell(0, 0, 16); !errors.Is(err, ErrResolution) {
		t.Fatal(err)
	}
	if _, err := LatLngToCell(math.NaN(), 0, 8); !errors.Is(err, ErrLatLng) {
		t.Fatal(err)
	}
	for _, s := range []string{"", "zz", "0", "ffffffffffffffff", "8061fffffffffff0"} {
		if _, err := ParseCell(s); err == nil {
			t.Fatalf("ParseCell(%q) accepted", s)
		}
	}
	if _, err := GridDisk(0, 1); err == nil {
		t.Fatal("GridDisk on invalid cell")
	}
	if _, err := GridDisk(Cell(0x8861892ecbfffff), -1); !errors.Is(err, ErrK) {
		t.Fatal(err)
	}
}

func TestPentagonDisk(t *testing.T) {
	// base cell 4 at res 8 is a pentagon: its 1-ring has 5 neighbours.
	p := Cell(h3Init&^(modeMask|resMask) | uint64(cellMode)<<modeOffset | 8<<resOffset | 4<<bcOffset)
	for r := 1; r <= 8; r++ {
		p = Cell(setDigit(uint64(p), r, centerDigit))
	}
	if !p.IsValid() || !p.IsPentagon() {
		t.Fatalf("%v not a valid pentagon", p)
	}
	d, err := GridDisk(p, 1)
	if err != nil || len(d) != 6 {
		t.Fatalf("pentagon disk: %d cells, %v", len(d), err)
	}
}
