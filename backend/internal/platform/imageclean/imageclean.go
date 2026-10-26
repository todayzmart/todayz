// Package imageclean re-encodes an uploaded photo so that only its pixels
// survive (02 §11: uploads are sniffed, size-limited and re-encoded): EXIF
// (with any GPS position), XMP, comments and anything else riding in the file
// are dropped. A JPEG's EXIF orientation is applied first, so a phone photo
// stays upright once the tag is gone.
package imageclean

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
	"golang.org/x/image/webp"
)

// The formats Clean reads, as content types.
const (
	JPEG = "image/jpeg"
	PNG  = "image/png"
	WebP = "image/webp"
)

var (
	// ErrNotImage: the bytes are not a JPEG, PNG or WebP image this package can decode.
	ErrNotImage = errors.New("imageclean: not a JPEG, PNG or WebP image")
	// ErrTooLarge: the image has more pixels than the caller allows.
	ErrTooLarge = errors.New("imageclean: the image has too many pixels")
)

// quality is the JPEG quality of the output.
const quality = 85

// Sniff names the format from the file's first bytes ("" when it is none of
// the three).
func Sniff(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return JPEG
	case len(b) >= 8 && bytes.Equal(b[:8], []byte("\x89PNG\r\n\x1a\n")):
		return PNG
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return WebP
	}
	return ""
}

// Result is a cleaned image.
type Result struct {
	// Format is the input's format (Sniff).
	Format string
	// JPEG is the re-encoded image; Width and Height are its size.
	JPEG          []byte
	Width, Height int
}

// Clean decodes b, refuses an image of more than maxPixels before decoding
// it, scales it down to fit maxSide by maxSide (never up), puts transparency
// on white and encodes a fresh JPEG.
func Clean(b []byte, maxSide, maxPixels int) (Result, error) {
	format := Sniff(b)
	var decodeConfig func([]byte) (image.Config, error)
	var decode func([]byte) (image.Image, error)
	switch format {
	case JPEG:
		decodeConfig = func(b []byte) (image.Config, error) { return jpeg.DecodeConfig(bytes.NewReader(b)) }
		decode = func(b []byte) (image.Image, error) { return jpeg.Decode(bytes.NewReader(b)) }
	case PNG:
		decodeConfig = func(b []byte) (image.Config, error) { return png.DecodeConfig(bytes.NewReader(b)) }
		decode = func(b []byte) (image.Image, error) { return png.Decode(bytes.NewReader(b)) }
	case WebP:
		decodeConfig = func(b []byte) (image.Config, error) { return webp.DecodeConfig(bytes.NewReader(b)) }
		decode = func(b []byte) (image.Image, error) { return webp.Decode(bytes.NewReader(b)) }
	default:
		return Result{}, ErrNotImage
	}
	cfg, err := decodeConfig(b)
	if err != nil || cfg.Width < 1 || cfg.Height < 1 {
		return Result{}, ErrNotImage
	}
	if int64(cfg.Width)*int64(cfg.Height) > int64(maxPixels) {
		return Result{}, ErrTooLarge
	}
	src, err := decode(b)
	if err != nil {
		return Result{}, ErrNotImage
	}

	w, h := fit(src.Bounds().Dx(), src.Bounds().Dy(), maxSide)
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	if format == JPEG {
		dst = orient(dst, orientation(b))
	}

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: quality}); err != nil {
		return Result{}, err
	}
	return Result{Format: format, JPEG: out.Bytes(), Width: dst.Bounds().Dx(), Height: dst.Bounds().Dy()}, nil
}

// fit scales w×h down to fit side×side, keeping the ratio.
func fit(w, h, side int) (int, int) {
	if w <= side && h <= side {
		return w, h
	}
	if w >= h {
		return side, max(1, h*side/w)
	}
	return max(1, w*side/h), side
}

// orientation reads a JPEG's EXIF Orientation tag (1–8); 1 when there is none.
func orientation(b []byte) int {
	i := 2 // after SOI
	for i+4 <= len(b) {
		if b[i] != 0xFF {
			return 1
		}
		marker := b[i+1]
		if marker == 0xDA || marker == 0xD9 { // start of scan, end of image
			return 1
		}
		n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if n < 2 || i+2+n > len(b) {
			return 1
		}
		seg := b[i+4 : i+2+n]
		if marker == 0xE1 && len(seg) >= 6 && bytes.Equal(seg[:6], []byte("Exif\x00\x00")) {
			return tiffOrientation(seg[6:])
		}
		i += 2 + n
	}
	return 1
}

// tiffOrientation finds tag 0x0112 in IFD0 of a TIFF header.
func tiffOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return 1
	}
	ifd := int(bo.Uint32(t[4:8]))
	if ifd < 8 || ifd+2 > len(t) {
		return 1
	}
	count := int(bo.Uint16(t[ifd : ifd+2]))
	for k := 0; k < count; k++ {
		e := ifd + 2 + 12*k
		if e+12 > len(t) {
			return 1
		}
		if bo.Uint16(t[e:e+2]) == 0x0112 {
			if v := int(bo.Uint16(t[e+8 : e+10])); v >= 1 && v <= 8 {
				return v
			}
			return 1
		}
	}
	return 1
}

// orient turns the pixels the way EXIF orientation o says the camera meant.
func orient(src *image.RGBA, o int) *image.RGBA {
	if o <= 1 || o > 8 {
		return src
	}
	w, h := src.Bounds().Dx(), src.Bounds().Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var dx, dy int
			switch o {
			case 2: // mirrored
				dx, dy = w-1-x, y
			case 3: // upside down
				dx, dy = w-1-x, h-1-y
			case 4: // mirrored upside down
				dx, dy = x, h-1-y
			case 5: // transposed
				dx, dy = y, x
			case 6: // turned a quarter right
				dx, dy = h-1-y, x
			case 7: // transversed
				dx, dy = h-1-y, w-1-x
			case 8: // turned a quarter left
				dx, dy = y, w-1-x
			}
			dst.SetRGBA(dx, dy, src.RGBAAt(x, y))
		}
	}
	return dst
}
