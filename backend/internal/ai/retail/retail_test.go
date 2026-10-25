package retail_test

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	genproblems "github.com/todayz/backend/gen/problems"
	"github.com/todayz/backend/internal/ai/retail"
	"github.com/todayz/backend/internal/platform/httpx/problem"
)

var (
	jpeg = []byte{0xff, 0xd8, 0xff, 0xe0, 0, 0x10}
	png  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0}
	webp = []byte("RIFF\x10\x00\x00\x00WEBPVP8 ")
)

func form(t *testing.T, mediaType string, data []byte) *multipart.Reader {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="photo"; filename="p"`)
	h.Set("Content-Type", mediaType)
	p, err := w.CreatePart(h)
	require.NoError(t, err)
	_, err = p.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return multipart.NewReader(&buf, w.Boundary())
}

func TestSniff(t *testing.T) {
	assert.Equal(t, "image/jpeg", retail.Sniff(jpeg))
	assert.Equal(t, "image/png", retail.Sniff(png))
	assert.Equal(t, "image/webp", retail.Sniff(webp))
	assert.Empty(t, retail.Sniff([]byte("GIF89a")))
	assert.Empty(t, retail.Sniff([]byte("RIFF\x10\x00\x00\x00WAVE")))
	assert.Empty(t, retail.Sniff([]byte{0xff, 0xd8}))
}

// The part's Content-Type must agree with the bytes (W3-11 review): a script or a GIF
// labelled image/jpeg is refused before the model sees it.
func TestReadPhotoChecksTheBytes(t *testing.T) {
	for _, c := range []struct {
		mt   string
		data []byte
	}{{"image/jpeg", jpeg}, {"image/png", png}, {"image/webp", webp}} {
		img, err := retail.ReadPhoto(form(t, c.mt, c.data))
		require.NoError(t, err, c.mt)
		assert.Equal(t, c.mt, img.MediaType)
	}
	for _, c := range []struct {
		mt   string
		data []byte
	}{
		{"image/jpeg", []byte("<script>alert(1)</script>")},
		{"image/jpeg", []byte("GIF89a....")},
		{"image/png", jpeg},
		{"image/webp", png},
		{"image/gif", []byte("GIF89a....")},
	} {
		_, err := retail.ReadPhoto(form(t, c.mt, c.data))
		var p *problem.Error
		require.ErrorAs(t, err, &p, c.mt)
		assert.Equal(t, genproblems.ValidationFailed, p.Code, c.mt)
	}
}
