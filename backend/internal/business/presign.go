package business

import (
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

// Presigned uploads into the private bucket (R2 deployed, MinIO locally):
// AWS Signature Version 4 query signing, which both speak. No SDK: the
// standard library is enough for one presigned PUT and keeps the AWS default
// credential chain out of the binary (D-97).
//
// TODO(lead): this belongs in a platform/storage package once a second
// domain needs uploads (W1-05 product photos); it is here because W1-04 is
// the first user.

// uploadTTL is how long an upload URL is valid (04 createBusinessDocument:
// 15 minutes). A product constant of the contract, not a setting.
const uploadTTL = 15 * time.Minute

// storageRegion is the SigV4 region. R2 takes "auto" and treats us-east-1 as
// an alias of it; MinIO's default region is us-east-1.
const storageRegion = "us-east-1"

type presigner struct {
	scheme, host string
	bucket       string
	accessKey    string
	secretKey    string
	// client makes the HEAD call (httpx.NewOutboundClient; set by New).
	client *http.Client
}

// newPresigner returns nil unless the endpoint (or R2 account), both keys
// and the private bucket are configured.
func newPresigner(c config.Storage) *presigner {
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
	return &presigner{
		scheme: u.Scheme, host: u.Host, bucket: c.BucketPrivate,
		accessKey: c.AccessKeyID.Reveal(), secretKey: c.SecretAccessKey.Reveal(),
	}
}

// putURL is a presigned path-style PUT of key with the given Content-Type
// and Content-Length, both signed: the upload must send exactly those
// headers, so it can be neither another type nor a larger file.
func (p *presigner) putURL(key, contentType string, size int64, now time.Time) string {
	return presignURL(presignInput{
		Method: "PUT", Scheme: p.scheme, Host: p.host, Path: "/" + p.bucket + "/" + key,
		Region: storageRegion, AccessKey: p.accessKey, SecretKey: p.secretKey,
		Time: now, Expires: uploadTTL, Headers: map[string]string{
			"content-type": contentType, "content-length": strconv.FormatInt(size, 10),
		},
	})
}

// errNoObject is HEAD's 404: nothing was uploaded under the key.
var errNoObject = errors.New("business: no such object")

// object is what HEAD says about an uploaded object.
type object struct {
	size        int64
	contentType string
}

// head reads the object's size and type (a signed HEAD), as support's
// claim step does (W3-08).
func (p *presigner) head(ctx context.Context, key string, now time.Time) (object, error) {
	if p.client == nil {
		return object{}, errors.New("business: no storage client")
	}
	u := presignURL(presignInput{
		Method: http.MethodHead, Scheme: p.scheme, Host: p.host, Path: "/" + p.bucket + "/" + key,
		Region: storageRegion, AccessKey: p.accessKey, SecretKey: p.secretKey,
		Time: now, Expires: uploadTTL,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return object{}, err
	}
	res, err := p.client.Do(req)
	if err != nil {
		return object{}, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusNotFound:
		return object{}, errNoObject
	case res.StatusCode != http.StatusOK:
		return object{}, errors.New("business: storage HEAD answered " + res.Status)
	}
	return object{size: res.ContentLength, contentType: res.Header.Get("Content-Type")}, nil
}

// remove deletes the object (a signed DELETE); an object already gone is
// fine.
func (p *presigner) remove(ctx context.Context, key string, now time.Time) error {
	if p.client == nil {
		return errors.New("business: no storage client")
	}
	u := presignURL(presignInput{
		Method: http.MethodDelete, Scheme: p.scheme, Host: p.host, Path: "/" + p.bucket + "/" + key,
		Region: storageRegion, AccessKey: p.accessKey, SecretKey: p.secretKey,
		Time: now, Expires: uploadTTL,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	res, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	switch res.StatusCode {
	case http.StatusOK, http.StatusNoContent, http.StatusNotFound:
		return nil
	}
	return errors.New("business: storage DELETE answered " + res.Status)
}

type presignInput struct {
	Method, Scheme, Host, Path string
	Region                     string
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
	scope := day + "/" + in.Region + "/s3/aws4_request"

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
	canonPath := encodePath(in.Path)

	canonRequest := strings.Join([]string{
		in.Method, canonPath, canonQuery, canonHeaders.String(), signed, "UNSIGNED-PAYLOAD",
	}, "\n")
	sum := sha256.Sum256([]byte(canonRequest))
	toSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(sum[:])

	key := hmacSHA256([]byte("AWS4"+in.SecretKey), day)
	key = hmacSHA256(key, in.Region)
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

func encodePath(p string) string { return awsEscape(p, false) }

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
