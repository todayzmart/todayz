package share

// The preview image (01 §11.1): item, shop, price and the shop's area, on the
// design system's mint wash. Never a distance. 1200×630, the size link
// previews expect.

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
)

const (
	cardW = 1200
	cardH = 630
)

// Design tokens (design/source, @todayz/tokens).
var (
	colWash   = color.RGBA{0xe8, 0xf5, 0xec, 0xff} // --brand-soft
	colCard   = color.RGBA{0xff, 0xff, 0xff, 0xff} // --surface
	colBrand  = color.RGBA{0x06, 0x7a, 0x3c, 0xff} // --brand
	colInk    = color.RGBA{0x17, 0x17, 0x17, 0xff} // --ink
	colInk2   = color.RGBA{0x5c, 0x5c, 0x5c, 0xff} // --ink-2
	colLine   = color.RGBA{0xeb, 0xeb, 0xeb, 0xff} // --line
	colOnMark = color.RGBA{0xff, 0xff, 0xff, 0xff} // --on-brand
)

// CardImage is what a preview shows.
type CardImage struct {
	Item       string
	Store      string
	Locality   string
	PricePaise int64
}

// RenderCard draws the preview PNG.
func RenderCard(c CardImage) ([]byte, error) {
	c.Item, c.Store, c.Locality = latin(c.Item), latin(c.Store), latin(c.Locality)
	img := image.NewRGBA(image.Rect(0, 0, cardW, cardH))
	fill(img, img.Bounds(), colWash)

	const margin, pad = 48, 56
	card := image.Rect(margin, margin, cardW-margin, cardH-margin)
	roundRect(img, card, 32, colCard)

	x := card.Min.X + pad
	inner := card.Dx() - 2*pad

	// Brand mark: "TodayZ" on a brand pill.
	pill := image.Rect(x, card.Min.Y+pad, x+textWidth("TodayZ", 4)+48, card.Min.Y+pad+56)
	roundRect(img, pill, 28, colBrand)
	drawText(img, "TodayZ", pill.Min.X+24, pill.Min.Y+12, 4, colOnMark)

	// Item name, up to two lines.
	y := pill.Max.Y + 40 //settings:ok card layout pixels
	for _, line := range wrap(c.Item, 7, inner, 2) {
		drawText(img, line, x, y, 7, colInk)
		y += glyphH*7 + 14 //settings:ok card layout pixels
	}

	// Price.
	y += 10
	drawText(img, Rupees(c.PricePaise), x, y, 10, colBrand)
	y += glyphH*10 + 28

	// Rule, then shop and area.
	fill(img, image.Rect(x, y, x+inner, y+3), colLine)
	y += 28
	drawText(img, fit(c.Store, 5, inner), x, y, 5, colInk)
	if c.Locality != "" {
		y += glyphH*5 + 12
		drawText(img, fit(c.Locality, 4, inner), x, y, 4, colInk2)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("share: png: %w", err)
	}
	return buf.Bytes(), nil
}

// folds maps typographic punctuation to the font's ASCII.
var folds = strings.NewReplacer("‘", "'", "’", "'", "“", `"`, "”", `"`, "–", "-", "—", "-", "\u00a0", " ")

// latin is s as the card draws it: typographic punctuation folded to ASCII,
// or "" when a rune is still outside the font, so the field is left off the
// image. The font is Latin only, and no Go rasteriser shapes Indic scripts
// (x/image draws glyph by glyph: no conjuncts, no reordered vowel signs), so a
// Devanagari, Kannada, Telugu or Tamil name would be drawn wrong. The link
// preview's title and description carry the name in its own script.
func latin(s string) string {
	s = folds.Replace(s)
	for _, r := range s {
		if _, ok := glyphs[r]; !ok {
			return ""
		}
	}
	return s
}

// advance is one glyph's width plus its gap, in pixels at scale.
func advance(scale int) int { return (glyphW + 1) * scale }

func textWidth(s string, scale int) int {
	n := len([]rune(s))
	if n == 0 {
		return 0
	}
	return n*advance(scale) - scale
}

func drawText(img *image.RGBA, s string, x, y, scale int, c color.RGBA) {
	for _, r := range s {
		if g, ok := glyphs[r]; ok {
			for row := range glyphH {
				for col := range glyphW {
					if g[row][col] == '#' {
						fill(img, image.Rect(x+col*scale, y+row*scale, x+(col+1)*scale, y+(row+1)*scale), c)
					}
				}
			}
		}
		x += advance(scale)
	}
}

// wrap splits s into at most lines lines that fit width at scale, breaking
// at spaces where it can; the last line ends in "…" when s is longer.
func wrap(s string, scale, width, lines int) []string {
	per := max((width+scale)/advance(scale), 1)
	words := strings.Fields(s)
	var out []string
	cur := ""
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case cur == "" && len([]rune(w)) > per:
			r := []rune(w)
			out = append(out, string(r[:per]))
			words[i] = string(r[per:])
			i--
		case cur == "":
			cur = w
		case len([]rune(cur))+1+len([]rune(w)) <= per:
			cur += " " + w
		default:
			out = append(out, cur)
			cur = w
		}
		if len(out) == lines {
			last := out[lines-1]
			if cur != "" || i < len(words)-1 {
				out[lines-1] = ellipsis(last, per)
			}
			return out
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	if len(out) > lines {
		out = append(out[:lines-1], ellipsis(out[lines-1], per))
	}
	return out
}

// fit cuts s to one line at scale, ending in "…" when it is cut.
func fit(s string, scale, width int) string {
	per := max((width+scale)/advance(scale), 1)
	if len([]rune(s)) <= per {
		return s
	}
	return ellipsis(s, per)
}

func ellipsis(s string, per int) string {
	r := []rune(s)
	if len(r) >= per {
		r = r[:per-1]
	}
	return strings.TrimRight(string(r), " ") + "…"
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

// roundRect fills r with corners of radius rad.
func roundRect(img *image.RGBA, r image.Rectangle, rad int, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			cx, cy := x, y
			switch {
			case x < r.Min.X+rad:
				cx = r.Min.X + rad
			case x >= r.Max.X-rad:
				cx = r.Max.X - rad - 1
			}
			switch {
			case y < r.Min.Y+rad:
				cy = r.Min.Y + rad
			case y >= r.Max.Y-rad:
				cy = r.Max.Y - rad - 1
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= rad*rad {
				img.SetRGBA(x, y, c)
			}
		}
	}
}
