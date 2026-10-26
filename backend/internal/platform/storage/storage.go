// Package storage is object storage for the domains (R2 deployed, MinIO
// locally): presigned uploads into the private bucket, signed reads, and the
// copy of an approved private object into the public images bucket.
//
// AWS Signature Version 4 query signing, which both speak. No SDK (D-97): the
// standard library is enough and keeps the AWS default credential chain out
// of the binary. internal/business and internal/support still carry their own
// copies of this code (W1-04, W3-08); new users take this package.
package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/todayz/backend/internal/platform/config"
)

// URLTTL is how long a presigned URL is valid (04: 15 minutes). A constant
// of the contract, not a setting.
const URLTTL = 15 * time.Minute

// region is the SigV4 region. R2 takes "auto" and treats us-east-1 as an
// alias of it; MinIO's default region is us-east-1.
const region = "us-east-1"

var (
	// ErrNoObject is a 404: nothing is stored under the key.
	ErrNoObject = errors.New("storage: no such object")
	// ErrNoImages: the public images bucket is not configured.
	ErrNoImages = errors.New("storage: no images bucket")
	// ErrTooLarge: the object is larger than the caller allows.
	ErrTooLarge = errors.New("storage: the object is too large")
)

// Client signs and makes storage calls. A nil *Client is "not configured".
type Client struct {
	scheme, host string
	private      string
	images       string
	accessKey    string
	secretKey    string
	http         *http.Client
}

// New returns nil unless the endpoint (or R2 account), both keys and the
// private bucket are configured. hc makes the server's own calls
// (httpx.NewOutboundClient).
func New(c config.Storage, hc *http.Client) *Client {
	endpoint := c.Endpoint
	if endpoint == "" && c.AccountID != "" {
		endpoint = "https://" + c.AccountID + ".r2.cloudflarestorage.com"
	}
	if endpoint == "" || !c.AccessKeyID.IsSet() || !c.SecretAccessKey.IsSet() || c.BucketPrivate == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil
	}
	return &Client{
		scheme: u.Scheme, host: u.Host, private: c.BucketPrivate, images: c.BucketImages,
		accessKey: c.AccessKeyID.Reveal(), secretKey: c.SecretAccessKey.Reveal(), http: hc,
	}
}

// HasImages reports whether the public images bucket is configured.
func (c *Client) HasImages() bool { return c != nil && c.images != "" }

func (c *Client) sign(method, bucket, key string, now time.Time, headers map[string]string) string {
	return presignURL(presignInput{
		Method: method, Scheme: c.scheme, Host: c.host, Path: "/" + bucket + "/" + key,
		AccessKey: c.accessKey, SecretKey: c.secretKey, Time: now, Expires: URLTTL, Headers: headers,
	})
}

// PutURL is a presigned PUT of key into the private bucket with the given
// Content-Type and Content-Length, both signed: the upload must send exactly
// those headers, so it can be neither another type nor a larger file.
func (c *Client) PutURL(key, contentType string, size int64, now time.Time) string {
	return c.sign(http.MethodPut, c.private, key, now, map[string]string{
		"content-type": contentType, "content-length": strconv.FormatInt(size, 10),
	})
}

// GetURL is a presigned GET of a private object (a preview for the shop that
// uploaded it or for an admin).
func (c *Client) GetURL(key string, now time.Time) string {
	return c.sign(http.MethodGet, c.private, key, now, nil)
}

// Object is what HEAD says about a stored object.
type Object struct {
	Size        int64
	ContentType string
}

// Head reads a private object's size and type.
func (c *Client) Head(ctx context.Context, key string, now time.Time) (Object, error) {
	res, err := c.do(ctx, http.MethodHead, c.sign(http.MethodHead, c.private, key, now, nil), nil, "")
	if err != nil {
		return Object{}, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return Object{}, ErrNoObject
	case res.StatusCode != http.StatusOK:
		return Object{}, errors.New("storage: HEAD answered " + res.Status)
	}
	return Object{Size: res.ContentLength, ContentType: res.Header.Get("Content-Type")}, nil
}

// Put stores body under key in the private bucket (the server's own upload,
// such as an imported image).
func (c *Client) Put(ctx context.Context, key, contentType string, body []byte, now time.Time) error {
	return c.put(ctx, c.private, key, contentType, body, now)
}

// Publish copies a private object into the public images bucket under dst (a
// signed GET, then a signed PUT of the same bytes). An object larger than
// maxBytes is refused, so it is read into memory.
func (c *Client) Publish(ctx context.Context, src, dst, contentType string, maxBytes int64, now time.Time) error {
	if !c.HasImages() {
		return ErrNoImages
	}
	body, err := c.Get(ctx, src, maxBytes, now)
	if err != nil {
		return err
	}
	return c.put(ctx, c.images, dst, contentType, body, now)
}

// Get reads a private object into memory (a signed GET). An object larger
// than maxBytes is refused with ErrTooLarge.
func (c *Client) Get(ctx context.Context, key string, maxBytes int64, now time.Time) ([]byte, error) {
	res, err := c.do(ctx, http.MethodGet, c.sign(http.MethodGet, c.private, key, now, nil), nil, "")
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBytes+1))
	_ = res.Body.Close()
	switch {
	case err != nil:
		return nil, err
	case res.StatusCode == http.StatusNotFound:
		return nil, ErrNoObject
	case res.StatusCode != http.StatusOK:
		return nil, errors.New("storage: GET answered " + res.Status)
	case int64(len(body)) > maxBytes:
		return nil, ErrTooLarge
	}
	return body, nil
}

// Remove deletes a private object; one already gone is fine.
func (c *Client) Remove(ctx context.Context, key string, now time.Time) error {
	return c.remove(ctx, c.private, key, now)
}

// RemoveImage deletes an object from the images bucket, as Remove does.
func (c *Client) RemoveImage(ctx context.Context, key string, now time.Time) error {
	if !c.HasImages() {
		return ErrNoImages
	}
	return c.remove(ctx, c.images, key, now)
}

func (c *Client) put(ctx context.Context, bucket, key, contentType string, body []byte, now time.Time) error {
	u := c.sign(http.MethodPut, bucket, key, now, map[string]string{"content-type": contentType})
	res, err := c.do(ctx, http.MethodPut, u, body, contentType)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	if res.StatusCode/100 != 2 {
		return errors.New("storage: PUT answered " + res.Status)
	}
	return nil
}

func (c *Client) remove(ctx context.Context, bucket, key string, now time.Time) error {
	res, err := c.do(ctx, http.MethodDelete, c.sign(http.MethodDelete, bucket, key, now, nil), nil, "")
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	switch res.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	}
	return errors.New("storage: DELETE answered " + res.Status)
}

func (c *Client) do(ctx context.Context, method, u string, body []byte, contentType string) (*http.Response, error) {
	if c.http == nil {
		return nil, errors.New("storage: no HTTP client")
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", contentType)
		req.ContentLength = int64(len(body))
	}
	return c.http.Do(req)
}

type presignInput struct {
	Method, Scheme, Host, Path string
	AccessKey, SecretKey       string
	Time                       time.Time
	Expires                    time.Duration
	// Headers are signed besides host (lower-case names); the client must
	// send them with these values.
	Headers map[string]string
}

// presignURL signs a request in the query string (SigV4, service s3,
// UNSIGNED-PAYLOAD).
func presignURL(in presignInput) string {
	t := in.Time.UTC()
	amzDate := t.Format("20060102T150405Z")
	day := t.Format("20060102")
	scope := day + "/" + region + "/s3/aws4_request"

	headers := map[string]string{"host": in.Host}
	for k, v := range in.Headers {
		headers[strings.ToLower(k)] = strings.TrimSpace(v)
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	signed := strings.Join(names, ";")
	var canonHeaders strings.Builder
	for _, k := range names {
		canonHeaders.WriteString(k + ":" + headers[k] + "\n")
	}

	query := map[string]string{
		"X-Amz-Algorithm":     "AWS4-HMAC-SHA256",
		"X-Amz-Credential":    in.AccessKey + "/" + scope,
		"X-Amz-Date":          amzDate,
		"X-Amz-Expires":       strconv.Itoa(int(in.Expires / time.Second)),
		"X-Amz-SignedHeaders": signed,
	}
	canonQuery := canonicalQuery(query)
	canonPath := awsEscape(in.Path, false)

	canonRequest := strings.Join([]string{
		in.Method, canonPath, canonQuery, canonHeaders.String(), signed, "UNSIGNED-PAYLOAD",
	}, "\n")
	sum := sha256.Sum256([]byte(canonRequest))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	key := hmacSHA256([]byte("AWS4"+in.SecretKey), day)
	key = hmacSHA256(key, region)
	key = hmacSHA256(key, "s3")
	key = hmacSHA256(key, "aws4_request")
	sig := hex.EncodeToString(hmacSHA256(key, toSign))

	return in.Scheme + "://" + in.Host + canonPath + "?" + canonQuery + "&X-Amz-Signature=" + sig
}

func hmacSHA256(key []byte, s string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(s))
	return m.Sum(nil)
}

func canonicalQuery(q map[string]string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, awsEscape(k, true)+"="+awsEscape(q[k], true))
	}
	return strings.Join(parts, "&")
}

// awsEscape is SigV4's URI encoding: everything but A–Z, a–z, 0–9 and -._~
// as %XX (upper case); '/' is kept in a path.
func awsEscape(s string, slash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !slash:
			b.WriteByte(c)
		default:
			b.WriteString("%" + strings.ToUpper(hex.EncodeToString([]byte{c})))
		}
	}
	return b.String()
}
