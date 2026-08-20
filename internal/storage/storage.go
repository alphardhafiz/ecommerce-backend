package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client uploads/deletes objects to an S3-compatible endpoint (Supabase
// Storage) using AWS SigV4 signing from the stdlib — no AWS SDK dependency.
type Client struct {
	endpoint  string // e.g. https://<ref>.supabase.co/storage/v1/s3
	bucket    string
	accessKey string
	secretKey string
	http      *http.Client
	region    string
}

func New(endpoint, bucket, accessKey, secretKey string) *Client {
	return &Client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		bucket:    bucket,
		accessKey: accessKey,
		secretKey: secretKey,
		http:      &http.Client{Timeout: 15 * time.Second},
		region:    "us-east-1", // Supabase accepts any region for S3-compat
	}
}

// Upload puts the bytes at key and returns the public URL the frontend uses
// directly (PRD M.1). If credentials are empty (dev), the URL is logged and a
// fake public URL returned so the flow still works locally (mail pattern).
func (c *Client) Upload(ctx context.Context, key string, body []byte, contentType string) (string, error) {
	if c.accessKey == "" {
		publicURL := c.publicURL(key)
		slog.Info("image upload (dev mode, STORAGE_ACCESS_KEY_ID not set)", "url", publicURL)
		return publicURL, nil
	}

	objURL := c.objectURL(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, objURL, strings.NewReader(string(body)))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", contentType)
	payloadHash := sha256Hex(body)
	c.sign(req, payloadHash)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("storage upload failed: status %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return c.publicURL(key), nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	if c.accessKey == "" {
		return nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.objectURL(key), nil)
	if err != nil {
		return err
	}
	c.sign(req, sha256Hex(nil))

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("storage delete failed: status %d", resp.StatusCode)
}

func (c *Client) objectURL(key string) string {
	return fmt.Sprintf("%s/%s/%s", c.endpoint, c.bucket, key)
}

// publicURL is the CDN URL the frontend fetches directly; Supabase serves
// public objects under /storage/v1/object/public/<bucket>/<key>.
func (c *Client) publicURL(key string) string {
	base := strings.Replace(c.endpoint, "/storage/v1/s3", "/storage/v1/object/public", 1)
	return fmt.Sprintf("%s/%s/%s", base, c.bucket, key)
}

// sign applies AWS Signature Version 4 headers (S3) to the request.
func (c *Client) sign(req *http.Request, payloadHash string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	datestamp := now.Format("20060102")

	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"

	canonicalRequest := req.Method + "\n" +
		canonicalURI(req.URL.Path) + "\n" +
		"\n" + // empty canonical query string
		canonicalHeaders + "\n" +
		signedHeaders + "\n" +
		payloadHash

	scope := datestamp + "/" + c.region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))

	signingKey := hmacSHA256([]byte("AWS4"+c.secretKey), datestamp)
	signingKey = hmacSHA256(signingKey, c.region)
	signingKey = hmacSHA256(signingKey, "s3")
	signingKey = hmacSHA256(signingKey, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.accessKey+"/"+scope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// canonicalURI percent-encodes each path segment (RFC 3986), keeping the
// slashes — S3 requires the escaped path, not the raw one.
func canonicalURI(path string) string {
	segments := strings.Split(path, "/")
	for i, seg := range segments {
		segments[i] = url.PathEscape(seg)
	}
	return strings.Join(segments, "/")
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
