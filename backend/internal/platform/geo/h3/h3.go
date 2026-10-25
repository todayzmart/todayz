// Package h3 is a small pure-Go port of the parts of the H3 hexagonal grid
// that TodayZ needs: LatLngToCell, CellToLatLng, GridDisk and the
// string/uint64 forms of a cell index.
//
// Ported from uber/h3 v4.1.0 (lib/h3Index.c, lib/faceijk.c, lib/coordijk.c,
// lib/baseCells.c, lib/algos.c, lib/latLng.c, lib/vec3d.c),
// Copyright 2016-2022 Uber Technologies, Inc., licensed under the Apache
// License, Version 2.0 (http://www.apache.org/licenses/LICENSE-2.0). The
// algorithms and tables follow the reference closely; names are adapted to Go.
// Checked against golden vectors from the reference library in testdata/.
package h3

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// Cell is an H3 cell index (mode 1). The high bit is always zero, so a valid
// cell also fits a Postgres bigint unchanged (see Int64 and FromInt64).
type Cell uint64

// MaxRes is the finest H3 resolution.
const MaxRes = 15

var (
	ErrResolution = errors.New("h3: resolution out of range")
	ErrLatLng     = errors.New("h3: latitude or longitude not finite")
	ErrCell       = errors.New("h3: invalid cell")
	ErrK          = errors.New("h3: k must be 0 or more")
)

const (
	numIcosaFaces = 20
	numBaseCells  = 122
	maxFaceCoord  = 2
	invalidBase   = 127

	epsilon       = 0.0000000000000001
	sqrt3over2    = 0.8660254037844386467637231707529361834714
	sin60         = sqrt3over2
	ap7RotRads    = 0.333473172251832115336090755351601070065900389
	res0UGnomonic = 0.38196601125010500003
	sqrt7         = 2.6457513110645905905016157536392604257102
	twoPi         = 6.28318530717958647692528676655900576839433

	cellMode     = 1
	modeOffset   = 59
	bcOffset     = 45
	resOffset    = 52
	reservedOff  = 56
	perDigitOff  = 3
	digitMask    = uint64(7)
	h3Init       = uint64(35184372088831) // all digits 7, everything else 0
	modeMask     = uint64(15) << modeOffset
	bcMask       = uint64(127) << bcOffset
	resMask      = uint64(15) << resOffset
	reservedMask = uint64(7) << reservedOff
	highBitMask  = uint64(1) << 63
)

type direction int

const (
	centerDigit direction = 0
	kAxesDigit  direction = 1
	jAxesDigit  direction = 2
	jkAxesDigit direction = 3
	iAxesDigit  direction = 4
	ikAxesDigit direction = 5
	ijAxesDigit direction = 6
	invalidDig  direction = 7
)

type coordIJK struct{ i, j, k int }

type faceIJK struct {
	face  int
	coord coordIJK
}

type faceOrientIJK struct {
	face      int
	translate coordIJK
	ccwRot60  int
}

type baseCellRotation struct{ baseCell, ccwRot60 int }

type baseCellInfo struct {
	homeFijk     faceIJK
	isPentagon   int
	cwOffsetPent [2]int
}

type latLng struct{ lat, lng float64 }

type vec3d struct{ x, y, z float64 }

var unitVecs = [7]coordIJK{{0, 0, 0}, {0, 0, 1}, {0, 1, 0}, {0, 1, 1}, {1, 0, 0}, {1, 0, 1}, {1, 1, 0}}

// gridDirections is the order the reference walks neighbours in.
var gridDirections = [6]direction{jAxesDigit, jkAxesDigit, kAxesDigit, ikAxesDigit, iAxesDigit, ijAxesDigit}

// ---- public API ----

// LatLngToCell returns the cell at res that contains the point (degrees).
func LatLngToCell(latDeg, lngDeg float64, res int) (Cell, error) {
	if res < 0 || res > MaxRes {
		return 0, ErrResolution
	}
	if math.IsNaN(latDeg) || math.IsInf(latDeg, 0) || math.IsNaN(lngDeg) || math.IsInf(lngDeg, 0) {
		return 0, ErrLatLng
	}
	g := latLng{latDeg * math.Pi / 180, lngDeg * math.Pi / 180}
	h := faceIjkToH3(geoToFaceIjk(g, res), res)
	if h == 0 {
		return 0, ErrCell
	}
	return Cell(h), nil
}

// CellToLatLng returns the centre of the cell in degrees.
func CellToLatLng(c Cell) (latDeg, lngDeg float64, err error) {
	if !c.IsValid() {
		return 0, 0, ErrCell
	}
	f := h3ToFaceIjk(uint64(c))
	g := faceIjkToGeo(f, c.Resolution())
	return g.lat * 180 / math.Pi, g.lng * 180 / math.Pi, nil
}

// GridDisk returns every cell within k grid steps of origin, origin included,
// sorted by index. Pentagon distortion is handled the way the reference's
// safe traversal handles it.
func GridDisk(origin Cell, k int) ([]Cell, error) {
	if k < 0 {
		return nil, ErrK
	}
	if !origin.IsValid() {
		return nil, ErrCell
	}
	seen := map[uint64]bool{uint64(origin): true}
	frontier := []uint64{uint64(origin)}
	for step := 0; step < k; step++ {
		var next []uint64
		for _, h := range frontier {
			for _, d := range gridDirections {
				rot := 0
				n, ok, err := neighborRotations(h, d, &rot)
				if err != nil {
					return nil, err
				}
				if !ok || seen[n] {
					continue
				}
				seen[n] = true
				next = append(next, n)
			}
		}
		frontier = next
	}
	out := make([]Cell, 0, len(seen))
	for h := range seen {
		out = append(out, Cell(h))
	}
	sort.Slice(out, func(a, b int) bool { return out[a] < out[b] })
	return out, nil
}

// ParseCell reads the canonical lower-case hex form (case-insensitive).
func ParseCell(s string) (Cell, error) {
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: %q", ErrCell, s)
	}
	c := Cell(v)
	if !c.IsValid() {
		return 0, fmt.Errorf("%w: %q", ErrCell, s)
	}
	return c, nil
}

// String is the canonical hex form, e.g. "8861892ecbfffff".
func (c Cell) String() string { return strconv.FormatUint(uint64(c), 16) }

// Int64 is the bigint form stored in Postgres.
func (c Cell) Int64() int64 { return int64(c) } // #nosec G115 -- a cell index never sets the high bit

// FromInt64 reads the bigint form back; it does not validate.
func FromInt64(v int64) Cell { return Cell(uint64(v)) } // #nosec G115 -- bit-for-bit reinterpretation of the stored bigint

// Resolution of the cell.
func (c Cell) Resolution() int { return int((uint64(c) & resMask) >> resOffset) }

// BaseCell number (0-121).
func (c Cell) BaseCell() int { return getBaseCell(uint64(c)) }

// IsPentagon reports whether the cell is one of the 12 pentagons at its res.
func (c Cell) IsPentagon() bool {
	return isBaseCellPentagon(c.BaseCell()) && leadingNonZeroDigit(uint64(c)) == centerDigit
}

// IsValid reports whether c is a well-formed cell index.
func (c Cell) IsValid() bool {
	h := uint64(c)
	if h&highBitMask != 0 || (h&modeMask)>>modeOffset != cellMode || (h&reservedMask)>>reservedOff != 0 {
		return false
	}
	bc := getBaseCell(h)
	if bc >= numBaseCells {
		return false
	}
	res := c.Resolution()
	found := false
	for r := 1; r <= res; r++ {
		d := getDigit(h, r)
		if !found && d != centerDigit {
			found = true
			if isBaseCellPentagon(bc) && d == kAxesDigit {
				return false
			}
		}
		if d >= invalidDig {
			return false
		}
	}
	for r := res + 1; r <= MaxRes; r++ {
		if getDigit(h, r) != invalidDig {
			return false
		}
	}
	return true
}

// ---- index bit helpers ----

func getBaseCell(h uint64) int { return int((h & bcMask) >> bcOffset) }

func setBaseCell(h uint64, bc int) uint64 {
	return (h &^ bcMask) | uint64(bc&0x7f)<<bcOffset // #nosec G115 -- masked to 7 bits
}

func getRes(h uint64) int { return int((h & resMask) >> resOffset) }

func getDigit(h uint64, r int) direction {
	return direction((h >> (uint(MaxRes-r) * perDigitOff)) & digitMask)
}

func setDigit(h uint64, r int, d direction) uint64 {
	shift := uint(MaxRes-r) * perDigitOff
	return (h &^ (digitMask << shift)) | (uint64(d) & digitMask << shift) // #nosec G115 -- a digit is 0-7
}

func leadingNonZeroDigit(h uint64) direction {
	for r := 1; r <= getRes(h); r++ {
		if d := getDigit(h, r); d != centerDigit {
			return d
		}
	}
	return centerDigit
}

func rotate60ccwIndex(h uint64) uint64 {
	for r, res := 1, getRes(h); r <= res; r++ {
		h = setDigit(h, r, rotate60ccw(getDigit(h, r)))
	}
	return h
}

func rotate60cwIndex(h uint64) uint64 {
	for r, res := 1, getRes(h); r <= res; r++ {
		h = setDigit(h, r, rotate60cw(getDigit(h, r)))
	}
	return h
}

func rotatePent60ccw(h uint64) uint64 {
	found := false
	for r, res := 1, getRes(h); r <= res; r++ {
		h = setDigit(h, r, rotate60ccw(getDigit(h, r)))
		if !found && getDigit(h, r) != centerDigit {
			found = true
			if leadingNonZeroDigit(h) == kAxesDigit {
				h = rotate60ccwIndex(h)
			}
		}
	}
	return h
}

// ---- base cells ----

func isBaseCellPentagon(bc int) bool {
	if bc < 0 || bc >= numBaseCells {
		return false
	}
	return baseCellData[bc].isPentagon == 1
}

func isBaseCellPolarPentagon(bc int) bool { return bc == 4 || bc == 117 }

func baseCellIsCwOffset(bc, face int) bool {
	o := baseCellData[bc].cwOffsetPent
	return o[0] == face || o[1] == face
}

// ---- ijk coordinates ----

func (c *coordIJK) normalize() {
	if c.i < 0 {
		c.j -= c.i
		c.k -= c.i
		c.i = 0
	}
	if c.j < 0 {
		c.i -= c.j
		c.k -= c.j
		c.j = 0
	}
	if c.k < 0 {
		c.i -= c.k
		c.j -= c.k
		c.k = 0
	}
	m := c.i
	if c.j < m {
		m = c.j
	}
	if c.k < m {
		m = c.k
	}
	if m > 0 {
		c.i -= m
		c.j -= m
		c.k -= m
	}
}

func ijkAdd(a, b coordIJK) coordIJK { return coordIJK{a.i + b.i, a.j + b.j, a.k + b.k} }

func ijkSub(a, b coordIJK) coordIJK { return coordIJK{a.i - b.i, a.j - b.j, a.k - b.k} }

func ijkScale(c coordIJK, f int) coordIJK { return coordIJK{c.i * f, c.j * f, c.k * f} }

func unitIjkToDigit(c coordIJK) direction {
	c.normalize()
	for d := centerDigit; d < invalidDig; d++ {
		if c == unitVecs[d] {
			return d
		}
	}
	return invalidDig
}

// combine returns i*iv + j*jv + k*kv, normalised.
func combine(c coordIJK, iv, jv, kv coordIJK) coordIJK {
	out := ijkAdd(ijkAdd(ijkScale(iv, c.i), ijkScale(jv, c.j)), ijkScale(kv, c.k))
	out.normalize()
	return out
}

func upAp7(c *coordIJK) {
	i, j := c.i-c.k, c.j-c.k
	*c = coordIJK{int(math.Round(float64(3*i-j) / 7)), int(math.Round(float64(i+2*j) / 7)), 0}
	c.normalize()
}

func upAp7r(c *coordIJK) {
	i, j := c.i-c.k, c.j-c.k
	*c = coordIJK{int(math.Round(float64(2*i+j) / 7)), int(math.Round(float64(3*j-i) / 7)), 0}
	c.normalize()
}

func downAp7(c *coordIJK) { *c = combine(*c, coordIJK{3, 0, 1}, coordIJK{1, 3, 0}, coordIJK{0, 1, 3}) }

func downAp7r(c *coordIJK) { *c = combine(*c, coordIJK{3, 1, 0}, coordIJK{0, 3, 1}, coordIJK{1, 0, 3}) }

func ijkRotate60ccw(c *coordIJK) {
	*c = combine(*c, coordIJK{1, 1, 0}, coordIJK{0, 1, 1}, coordIJK{1, 0, 1})
}

func ijkRotate60cw(c *coordIJK) {
	*c = combine(*c, coordIJK{1, 0, 1}, coordIJK{1, 1, 0}, coordIJK{0, 1, 1})
}

func neighbor(c *coordIJK, d direction) {
	if d > centerDigit && d < invalidDig {
		*c = ijkAdd(*c, unitVecs[d])
		c.normalize()
	}
}

func rotate60ccw(d direction) direction {
	switch d {
	case kAxesDigit:
		return ikAxesDigit
	case ikAxesDigit:
		return iAxesDigit
	case iAxesDigit:
		return ijAxesDigit
	case ijAxesDigit:
		return jAxesDigit
	case jAxesDigit:
		return jkAxesDigit
	case jkAxesDigit:
		return kAxesDigit
	}
	return d
}

func rotate60cw(d direction) direction {
	switch d {
	case kAxesDigit:
		return jkAxesDigit
	case jkAxesDigit:
		return jAxesDigit
	case jAxesDigit:
		return ijAxesDigit
	case ijAxesDigit:
		return iAxesDigit
	case iAxesDigit:
		return ikAxesDigit
	case ikAxesDigit:
		return kAxesDigit
	}
	return d
}

func hex2dToCoordIJK(x, y float64) coordIJK {
	var h coordIJK
	a1, a2 := math.Abs(x), math.Abs(y)
	x2 := a2 / sin60
	x1 := a1 + x2/2
	m1, m2 := int(x1), int(x2)
	r1, r2 := x1-float64(m1), x2-float64(m2)

	if r1 < 0.5 {
		if r1 < 1.0/3.0 {
			if r2 < (1+r1)/2 {
				h.i, h.j = m1, m2
			} else {
				h.i, h.j = m1, m2+1
			}
		} else {
			if r2 < 1-r1 {
				h.j = m2
			} else {
				h.j = m2 + 1
			}
			if 1-r1 <= r2 && r2 < 2*r1 {
				h.i = m1 + 1
			} else {
				h.i = m1
			}
		}
	} else {
		if r1 < 2.0/3.0 {
			if r2 < 1-r1 {
				h.j = m2
			} else {
				h.j = m2 + 1
			}
			if 2*r1-1 < r2 && r2 < 1-r1 {
				h.i = m1
			} else {
				h.i = m1 + 1
			}
		} else {
			if r2 < r1/2 {
				h.i, h.j = m1+1, m2
			} else {
				h.i, h.j = m1+1, m2+1
			}
		}
	}

	// fold across the axes if necessary
	if x < 0 {
		if h.j%2 == 0 {
			axisi := h.j / 2
			diff := h.i - axisi
			h.i -= 2 * diff
		} else {
			axisi := (h.j + 1) / 2
			diff := h.i - axisi
			h.i -= 2*diff + 1
		}
	}
	if y < 0 {
		h.i -= (2*h.j + 1) / 2
		h.j = -h.j
	}
	h.normalize()
	return h
}

func ijkToHex2d(c coordIJK) (x, y float64) {
	i, j := c.i-c.k, c.j-c.k
	return float64(i) - 0.5*float64(j), float64(j) * sqrt3over2
}

// ---- spherical helpers ----

func posAngleRads(r float64) float64 {
	t := r
	if r < 0 {
		t = r + twoPi
	}
	if r >= twoPi {
		t -= twoPi
	}
	return t
}

func constrainLng(l float64) float64 {
	for l > math.Pi {
		l -= 2 * math.Pi
	}
	for l < -math.Pi {
		l += 2 * math.Pi
	}
	return l
}

func geoAzimuthRads(p1, p2 latLng) float64 {
	return math.Atan2(math.Cos(p2.lat)*math.Sin(p2.lng-p1.lng),
		math.Cos(p1.lat)*math.Sin(p2.lat)-math.Sin(p1.lat)*math.Cos(p2.lat)*math.Cos(p2.lng-p1.lng))
}

func geoAzDistanceRads(p1 latLng, az, distance float64) latLng {
	if distance < epsilon {
		return p1
	}
	var p2 latLng
	az = posAngleRads(az)
	if az < epsilon || math.Abs(az-math.Pi) < epsilon {
		if az < epsilon {
			p2.lat = p1.lat + distance
		} else {
			p2.lat = p1.lat - distance
		}
		switch {
		case math.Abs(p2.lat-math.Pi/2) < epsilon:
			p2 = latLng{math.Pi / 2, 0}
		case math.Abs(p2.lat+math.Pi/2) < epsilon:
			p2 = latLng{-math.Pi / 2, 0}
		default:
			p2.lng = constrainLng(p1.lng)
		}
		return p2
	}
	sinlat := math.Sin(p1.lat)*math.Cos(distance) + math.Cos(p1.lat)*math.Sin(distance)*math.Cos(az)
	sinlat = math.Max(-1, math.Min(1, sinlat))
	p2.lat = math.Asin(sinlat)
	switch {
	case math.Abs(p2.lat-math.Pi/2) < epsilon:
		return latLng{math.Pi / 2, 0}
	case math.Abs(p2.lat+math.Pi/2) < epsilon:
		return latLng{-math.Pi / 2, 0}
	}
	sinlng := math.Sin(az) * math.Sin(distance) / math.Cos(p2.lat)
	coslng := (math.Cos(distance) - math.Sin(p1.lat)*math.Sin(p2.lat)) / math.Cos(p1.lat) / math.Cos(p2.lat)
	sinlng = math.Max(-1, math.Min(1, sinlng))
	coslng = math.Max(-1, math.Min(1, coslng))
	p2.lng = constrainLng(p1.lng + math.Atan2(sinlng, coslng))
	return p2
}

func isClassIII(res int) bool { return res%2 == 1 }

// ---- face ijk <-> geo ----

func geoToClosestFace(g latLng) (face int, sqd float64) {
	r := math.Cos(g.lat)
	v := vec3d{math.Cos(g.lng) * r, math.Sin(g.lng) * r, math.Sin(g.lat)}
	sqd = 5.0
	for f := 0; f < numIcosaFaces; f++ {
		p := faceCenterPoint[f]
		d := (p.x-v.x)*(p.x-v.x) + (p.y-v.y)*(p.y-v.y) + (p.z-v.z)*(p.z-v.z)
		if d < sqd {
			face, sqd = f, d
		}
	}
	return face, sqd
}

func geoToFaceIjk(g latLng, res int) faceIJK {
	face, sqd := geoToClosestFace(g)
	r := math.Acos(1 - sqd/2)
	if r < epsilon {
		return faceIJK{face, hex2dToCoordIJK(0, 0)}
	}
	theta := posAngleRads(faceAxesAzRadsCII[face][0] - posAngleRads(geoAzimuthRads(faceCenterGeo[face], g)))
	if isClassIII(res) {
		theta = posAngleRads(theta - ap7RotRads)
	}
	r = math.Tan(r)
	r /= res0UGnomonic
	for i := 0; i < res; i++ {
		r *= sqrt7
	}
	return faceIJK{face, hex2dToCoordIJK(r*math.Cos(theta), r*math.Sin(theta))}
}

func faceIjkToGeo(f faceIJK, res int) latLng {
	x, y := ijkToHex2d(f.coord)
	r := math.Sqrt(x*x + y*y)
	if r < epsilon {
		return faceCenterGeo[f.face]
	}
	theta := math.Atan2(y, x)
	for i := 0; i < res; i++ {
		r /= sqrt7
	}
	r *= res0UGnomonic
	r = math.Atan(r)
	if isClassIII(res) {
		theta = posAngleRads(theta + ap7RotRads)
	}
	theta = posAngleRads(faceAxesAzRadsCII[f.face][0] - theta)
	return geoAzDistanceRads(faceCenterGeo[f.face], theta, r)
}

// ---- face ijk <-> index ----

func faceIjkToH3(f faceIJK, res int) uint64 {
	h := h3Init
	h = (h &^ modeMask) | uint64(cellMode)<<modeOffset
	h = (h &^ resMask) | uint64(res&0xf)<<resOffset // #nosec G115 -- res is checked to 0-15 by the caller

	if res == 0 {
		if f.coord.i > maxFaceCoord || f.coord.j > maxFaceCoord || f.coord.k > maxFaceCoord {
			return 0
		}
		return setBaseCell(h, faceIjkBaseCells[f.face][f.coord.i][f.coord.j][f.coord.k].baseCell)
	}

	bc := f
	for r := res - 1; r >= 0; r-- {
		last := bc.coord
		var center coordIJK
		if isClassIII(r + 1) {
			upAp7(&bc.coord)
			center = bc.coord
			downAp7(&center)
		} else {
			upAp7r(&bc.coord)
			center = bc.coord
			downAp7r(&center)
		}
		diff := ijkSub(last, center)
		diff.normalize()
		h = setDigit(h, r+1, unitIjkToDigit(diff))
	}

	if bc.coord.i > maxFaceCoord || bc.coord.j > maxFaceCoord || bc.coord.k > maxFaceCoord {
		return 0
	}
	rot := faceIjkBaseCells[bc.face][bc.coord.i][bc.coord.j][bc.coord.k]
	h = setBaseCell(h, rot.baseCell)

	if isBaseCellPentagon(rot.baseCell) {
		if leadingNonZeroDigit(h) == kAxesDigit {
			if baseCellIsCwOffset(rot.baseCell, bc.face) {
				h = rotate60cwIndex(h)
			} else {
				h = rotate60ccwIndex(h)
			}
		}
		for i := 0; i < rot.ccwRot60; i++ {
			h = rotatePent60ccw(h)
		}
	} else {
		for i := 0; i < rot.ccwRot60; i++ {
			h = rotate60ccwIndex(h)
		}
	}
	return h
}

type overage int

const (
	noOverage overage = iota
	faceEdge
	newFace
)

// Quadrant indexes into faceNeighbors.
const (
	quadIJ = 1
	quadKI = 2
	quadJK = 3
)

func adjustOverageClassII(f *faceIJK, res int, pentLeading4 bool) overage {
	// substrate is always 0 for the cell centre path ported here
	maxDim := maxDimByCIIres[res]
	ijk := &f.coord
	if ijk.i+ijk.j+ijk.k <= maxDim {
		return noOverage
	}
	var orient faceOrientIJK
	if ijk.k > 0 {
		if ijk.j > 0 {
			orient = faceNeighbors[f.face][quadJK]
		} else {
			orient = faceNeighbors[f.face][quadKI]
			if pentLeading4 {
				origin := coordIJK{maxDim, 0, 0}
				tmp := ijkSub(*ijk, origin)
				ijkRotate60cw(&tmp)
				*ijk = ijkAdd(tmp, origin)
			}
		}
	} else {
		orient = faceNeighbors[f.face][quadIJ]
	}
	f.face = orient.face
	for i := 0; i < orient.ccwRot60; i++ {
		ijkRotate60ccw(ijk)
	}
	*ijk = ijkAdd(*ijk, ijkScale(orient.translate, unitScaleByCIIres[res]))
	ijk.normalize()
	return newFace
}

func h3ToFaceIjk(h uint64) faceIJK {
	bc := getBaseCell(h)
	if isBaseCellPentagon(bc) && leadingNonZeroDigit(h) == ikAxesDigit {
		h = rotate60cwIndex(h)
	}
	f := baseCellData[bc].homeFijk
	res := getRes(h)

	possibleOverage := isBaseCellPentagon(bc) || (res != 0 && f.coord != (coordIJK{}))
	for r := 1; r <= res; r++ {
		if isClassIII(r) {
			downAp7(&f.coord)
		} else {
			downAp7r(&f.coord)
		}
		neighbor(&f.coord, getDigit(h, r))
	}
	if !possibleOverage {
		return f
	}

	orig := f.coord
	adjRes := res
	if isClassIII(res) {
		downAp7r(&f.coord)
		adjRes++
	}
	pentLeading4 := isBaseCellPentagon(bc) && leadingNonZeroDigit(h) == iAxesDigit
	if adjustOverageClassII(&f, adjRes, pentLeading4) != noOverage {
		if isBaseCellPentagon(bc) {
			for adjustOverageClassII(&f, adjRes, false) != noOverage {
			}
		}
		if adjRes != res {
			upAp7r(&f.coord)
		}
	} else if adjRes != res {
		f.coord = orig
	}
	return f
}

// ---- neighbours ----

// neighborRotations ports h3NeighborRotations. ok is false where the
// reference returns E_PENTAGON (the deleted k direction of a pentagon).
func neighborRotations(origin uint64, dir direction, rotations *int) (out uint64, ok bool, err error) {
	current := origin
	*rotations %= 6
	for i := 0; i < *rotations; i++ {
		dir = rotate60ccw(dir)
	}
	newRotations := 0
	oldBaseCell := getBaseCell(current)
	if oldBaseCell >= numBaseCells {
		return 0, false, ErrCell
	}
	oldLeading := leadingNonZeroDigit(current)

	r := getRes(current) - 1
	for {
		if r == -1 {
			current = setBaseCell(current, baseCellNeighbors[oldBaseCell][dir])
			newRotations = baseCellNeighbor60CCWRots[oldBaseCell][dir]
			if getBaseCell(current) == invalidBase {
				current = setBaseCell(current, baseCellNeighbors[oldBaseCell][ikAxesDigit])
				newRotations = baseCellNeighbor60CCWRots[oldBaseCell][ikAxesDigit]
				current = rotate60ccwIndex(current)
				*rotations++
			}
			break
		}
		oldDigit := getDigit(current, r+1)
		if oldDigit == invalidDig {
			return 0, false, ErrCell
		}
		var nextDir direction
		if isClassIII(r + 1) {
			current = setDigit(current, r+1, newDigitII[oldDigit][dir])
			nextDir = newAdjustmentII[oldDigit][dir]
		} else {
			current = setDigit(current, r+1, newDigitIII[oldDigit][dir])
			nextDir = newAdjustmentIII[oldDigit][dir]
		}
		if nextDir == centerDigit {
			break
		}
		dir = nextDir
		r--
	}

	newBaseCell := getBaseCell(current)
	if isBaseCellPentagon(newBaseCell) {
		alreadyAdjusted := false
		if leadingNonZeroDigit(current) == kAxesDigit {
			if oldBaseCell != newBaseCell {
				if baseCellIsCwOffset(newBaseCell, baseCellData[oldBaseCell].homeFijk.face) {
					current = rotate60cwIndex(current)
				} else {
					current = rotate60ccwIndex(current)
				}
				alreadyAdjusted = true
			} else {
				switch oldLeading {
				case centerDigit:
					return 0, false, nil
				case jkAxesDigit:
					current = rotate60ccwIndex(current)
					*rotations++
				case ikAxesDigit:
					current = rotate60cwIndex(current)
					*rotations += 5
				default:
					return 0, false, ErrCell
				}
			}
		}
		for i := 0; i < newRotations; i++ {
			current = rotatePent60ccw(current)
		}
		if oldBaseCell != newBaseCell {
			if isBaseCellPolarPentagon(newBaseCell) {
				if oldBaseCell != 118 && oldBaseCell != 8 && leadingNonZeroDigit(current) != jkAxesDigit {
					*rotations++
				}
			} else if leadingNonZeroDigit(current) == ikAxesDigit && !alreadyAdjusted {
				*rotations++
			}
		}
	} else {
		for i := 0; i < newRotations; i++ {
			current = rotate60ccwIndex(current)
		}
	}
	*rotations = (*rotations + newRotations) % 6
	return current, true, nil
}
