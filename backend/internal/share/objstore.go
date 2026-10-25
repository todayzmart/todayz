package share

// The public images bucket (R2 deployed, MinIO locally): one SigV4
// query-signed PUT per image, sent through the egress-guarded client. No SDK,
// the same as business/presign.go (D-97).
//
// TODO(lead): this and business/presign.go belong in one platform/storage
// package; it is here so W3-16 stays inside its own paths.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/clock"
	"github.com/todayz/backend/internal/platform/config"
)

// storeRegion is the SigV4 region: R2 takes "auto" and treats us-east-1 as an
// alias; MinIO's default is us-east-1.
const storeRegion = "us-east-1"

// putTTL is how long a signed PUT is valid; the request is sent at once.
const putTTL = 5 * time.Minute

// S3Store puts objects into the public images bucket.
type S3Store struct {
	scheme, host, bucket string
	accessKey, secretKey string
	client               *http.Client
	clock                clock.Clock
}

// NewS3Store returns nil unless the endpoint (or R2 account), both keys and
// the images bucket are configured.
func NewS3Store(c config.Storage, client *http.Client, clk clock.Clock) *S3Store {
	endpoint := c.Endpoint
	if endpoint == "" && c.AccountID != "" {
		endpoint = "https://" + c.AccountID + ".r2.cloudflarestorage.com"
	}
	if endpoint == "" || !c.AccessKeyID.IsSet() || !c.SecretAccessKey.IsSet() || c.BucketImages == "" || client == nil {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil
	}
	return &S3Store{
		scheme: u.Scheme, host: u.Host, bucket: c.BucketImages,
		accessKey: c.AccessKeyID.Reveal(), secretKey: c.SecretAccessKey.Reveal(),
		client: client, clock: clk,
	}
}

// Put implements ImageStore.
func (s *S3Store) Put(ctx context.Context, key, contentType string, body []byte) error {
	u := s.signedPut(key, contentType, s.clock.Now())
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("share: store request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = int64(len(body))
	res, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("share: store put: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("share: store put: status %d", res.StatusCode)
	}
	return nil
}

// signedPut is a path-style PUT of key, signing host and content-type.
func (s *S3Store) signedPut(key, contentType string, now time.Time) string {
	t := now.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	scope := day + "/" + storeRegion + "/s3/aws4_request"
	path := "/" + s.bucket + "/" + key

	signed := "content-type;host"
	canonHeaders := "content-type:" + strings.TrimSpace(contentType) + "\nhost:" + s.host + "\n"
	query := map[string]string{
		"X-Amz-Algorithm":     "AWS4-HMAC-SHA256",
		"X-Amz-Credential":    s.accessKey + "/" + scope,
		"X-Amz-Date":          amzDate,
		"X-Amz-Expires":       strconv.Itoa(int(putTTL / time.Second)),
		"X-Amz-SignedHeaders": signed,
	}
	canonQuery := sigQuery(query)
	canonPath := sigEscape(path, false)
	canonRequest := strings.Join([]string{http.MethodPut, canonPath, canonQuery, canonHeaders, signed, "UNSIGNED-PAYLOAD"}, "\n")
	sum := sha256.Sum256([]byte(canonRequest))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	k := sigHMAC([]byte("AWS4"+s.secretKey), day)
	k = sigHMAC(k, storeRegion)
	k = sigHMAC(k, "s3")
	k = sigHMAC(k, "aws4_request")
	sig := hex.EncodeToString(sigHMAC(k, toSign))
	return s.scheme + "://" + s.host + canonPath + "?" + canonQuery + "&X-Amz-Signature=" + sig
}

func sigHMAC(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func sigQuery(q map[string]string) string {
	names := make([]string, 0, len(q))
	for k := range q {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, sigEscape(k, true)+"="+sigEscape(q[k], true))
	}
	return strings.Join(parts, "&")
}

// sigEscape is SigV4's URI encoding: unreserved characters as they are, '/'
// kept in a path, everything else %XX.
func sigEscape(s string, slash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !slash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}
