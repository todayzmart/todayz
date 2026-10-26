package imageclean

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

// halves is a w×h image, the left half red and the right half blue.
func halves(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: 255, A: 255}
			if x >= w/2 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}))
	return b.Bytes()
}

// withExif puts an APP1 EXIF segment right after SOI: IFD0 holds the
// orientation and a GPS-looking ASCII value, so the test can see both go.
func withExif(t *testing.T, j []byte, orientation uint16, marker string) []byte {
	t.Helper()
	tiff := new(bytes.Buffer)
	tiff.WriteString("II")
	_ = binary.Write(tiff, binary.LittleEndian, uint16(42))
	_ = binary.Write(tiff, binary.LittleEndian, uint32(8))
	_ = binary.Write(tiff, binary.LittleEndian, uint16(2)) // two entries
	// 0x0112 Orientation, SHORT, count 1, value inline.
	_ = binary.Write(tiff, binary.LittleEndian, []uint16{0x0112, 3})
	_ = binary.Write(tiff, binary.LittleEndian, uint32(1))
	_ = binary.Write(tiff, binary.LittleEndian, []uint16{orientation, 0})
	// 0x010E ImageDescription, ASCII, pointing after the IFD.
	off := uint32(8 + 2 + 2*12 + 4)
	_ = binary.Write(tiff, binary.LittleEndian, []uint16{0x010E, 2})
	_ = binary.Write(tiff, binary.LittleEndian, uint32(len(marker)+1))
	_ = binary.Write(tiff, binary.LittleEndian, off)
	_ = binary.Write(tiff, binary.LittleEndian, uint32(0)) // no next IFD
	tiff.WriteString(marker + "\x00")

	seg := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1}
	out = binary.BigEndian.AppendUint16(out, uint16(len(seg)+2)) //nolint:gosec // a test segment of a few bytes
	out = append(out, seg...)
	return append(out, j[2:]...)
}

func near(t *testing.T, img image.Image, x, y int, want color.RGBA, msg string) {
	t.Helper()
	r, g, b, _ := img.At(x, y).RGBA()
	got := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8)}
	d := func(a, b uint8) int {
		if a > b {
			return int(a - b)
		}
		return int(b - a)
	}
	require.Less(t, d(got.R, want.R)+d(got.G, want.G)+d(got.B, want.B), 60, "%s: got %v", msg, got)
}

func TestSniff(t *testing.T) {
	require.Equal(t, JPEG, Sniff([]byte{0xFF, 0xD8, 0xFF, 0xE0}))
	require.Equal(t, PNG, Sniff([]byte("\x89PNG\r\n\x1a\n....")))
	require.Equal(t, WebP, Sniff([]byte("RIFF\x00\x00\x00\x00WEBPVP8 ")))
	require.Empty(t, Sniff([]byte("%PDF-1.7")))
	require.Empty(t, Sniff(nil))
}

func TestCleanDropsExifAndAppliesOrientation(t *testing.T) {
	const secret = "GPS 12.9719N 77.6412E"
	in := withExif(t, encodeJPEG(t, halves(80, 40)), 6, secret)
	require.Contains(t, string(in), secret)
	require.Equal(t, 6, orientation(in))

	res, err := Clean(in, 512, 1_000_000)
	require.NoError(t, err)
	require.Equal(t, JPEG, res.Format)
	require.NotContains(t, string(res.JPEG), secret, "metadata must not survive")
	require.NotContains(t, string(res.JPEG), "Exif\x00\x00", "no EXIF segment at all")
	require.Equal(t, 1, orientation(res.JPEG))

	// Turned a quarter right: 80×40 becomes 40×80, the red left half on top.
	require.Equal(t, 40, res.Width)
	require.Equal(t, 80, res.Height)
	out, err := jpeg.Decode(bytes.NewReader(res.JPEG))
	require.NoError(t, err)
	require.Equal(t, image.Rect(0, 0, 40, 80), out.Bounds())
	near(t, out, 20, 10, color.RGBA{R: 255}, "top")
	near(t, out, 20, 70, color.RGBA{B: 255}, "bottom")
}

func TestCleanOrientations(t *testing.T) {
	src := encodeJPEG(t, halves(60, 30))
	for o := uint16(1); o <= 8; o++ {
		res, err := Clean(withExif(t, src, o, "x"), 512, 1_000_000)
		require.NoError(t, err, "orientation %d", o)
		if o >= 5 {
			require.Equal(t, [2]int{30, 60}, [2]int{res.Width, res.Height}, "orientation %d swaps the sides", o)
		} else {
			require.Equal(t, [2]int{60, 30}, [2]int{res.Width, res.Height}, "orientation %d keeps the sides", o)
		}
	}
}

func TestCleanScalesDownNeverUp(t *testing.T) {
	res, err := Clean(encodeJPEG(t, halves(1200, 600)), 512, 10_000_000)
	require.NoError(t, err)
	require.Equal(t, [2]int{512, 256}, [2]int{res.Width, res.Height})

	res, err = Clean(encodeJPEG(t, halves(100, 50)), 512, 10_000_000)
	require.NoError(t, err)
	require.Equal(t, [2]int{100, 50}, [2]int{res.Width, res.Height})
}

func TestCleanPNGTransparencyOnWhite(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 20, 20)) // fully transparent
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	res, err := Clean(b.Bytes(), 512, 1_000_000)
	require.NoError(t, err)
	require.Equal(t, PNG, res.Format)
	out, err := jpeg.Decode(bytes.NewReader(res.JPEG))
	require.NoError(t, err)
	near(t, out, 10, 10, color.RGBA{R: 255, G: 255, B: 255}, "transparent is white")
}

func TestCleanWebP(t *testing.T) {
	// A 1×1 lossless WebP.
	in, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	require.NoError(t, err)
	res, err := Clean(in, 512, 1_000_000)
	require.NoError(t, err)
	require.Equal(t, WebP, res.Format)
	require.Equal(t, [2]int{1, 1}, [2]int{res.Width, res.Height})
}

func TestCleanRefuses(t *testing.T) {
	_, err := Clean([]byte("%PDF-1.7 not an image"), 512, 1_000_000)
	require.ErrorIs(t, err, ErrNotImage)

	j := encodeJPEG(t, halves(40, 40))
	_, err = Clean(j[:len(j)/2], 512, 1_000_000)
	require.ErrorIs(t, err, ErrNotImage, "a truncated file")

	_, err = Clean(j, 512, 1_000)
	require.ErrorIs(t, err, ErrTooLarge, "more pixels than allowed, refused before decoding")
}
