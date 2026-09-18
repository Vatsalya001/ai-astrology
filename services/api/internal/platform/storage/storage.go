// Package storage puts objects in S3-compatible storage and hands out
// short-lived signed URLs for them.
//
// MinIO in development, S3 in production — the same API, which is why
// the config is `S3_*` and the endpoint is explicit rather than derived
// from a region. `S3_FORCE_PATH_STYLE` exists because MinIO serves
// buckets as a path segment and AWS serves them as a subdomain.
//
// Only api-service uses this. astro-service has no network client at
// all and ai-service is read-only, so there is no second writer to
// coordinate with.
package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Config is what the caller must supply. Every field is required —
// there are no defaults, because a default bucket name is how a
// developer's PDFs end up in production's bucket.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	PathStyle bool
}

type Client struct {
	mc     *minio.Client
	bucket string
}

// New validates the config and connects.
//
// It does NOT create the bucket. Creating one on demand hides a
// misconfigured bucket name behind a working-looking service, and the
// first sign is objects nobody can find. Compose provisions it; so does
// terraform in production.
func New(cfg Config) (*Client, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, fmt.Errorf("storage: endpoint, bucket, access key and secret key are all required")
	}

	parsed, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage: parse endpoint %q: %w", cfg.Endpoint, err)
	}
	if parsed.Host == "" {
		return nil, fmt.Errorf("storage: endpoint %q has no host", cfg.Endpoint)
	}

	mc, err := minio.New(parsed.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: parsed.Scheme == "https",
		Region: cfg.Region,
		// Path style for MinIO, virtual-host style for S3 proper.
		BucketLookup: bucketLookup(cfg.PathStyle),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: connect %s: %w", parsed.Host, err)
	}

	return &Client{mc: mc, bucket: cfg.Bucket}, nil
}

func bucketLookup(pathStyle bool) minio.BucketLookupType {
	if pathStyle {
		return minio.BucketLookupPath
	}
	return minio.BucketLookupDNS
}

// Put stores an object.
//
// `contentType` is set explicitly rather than sniffed: a PDF served as
// `application/octet-stream` downloads instead of opening, which for a
// document people want to read on a phone is the difference between the
// feature working and not.
func (c *Client) Put(
	ctx context.Context,
	key string,
	body io.Reader,
	size int64,
	contentType string,
) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, body, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		// The key, not the body. A failure here is almost always a
		// missing bucket or a wrong credential, and both are identified
		// by the key's prefix.
		return fmt.Errorf("storage: put %s: %w", key, err)
	}
	return nil
}

// SignedURL returns a URL that works for `ttl` and then does not.
//
// The object itself stays private. A signed URL is the whole access
// control: anyone holding it can read that one object until it expires,
// which is why the TTL is short and the key carries no PII.
func (c *Client) SignedURL(ctx context.Context, key string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", fmt.Errorf("storage: ttl must be positive, got %s", ttl)
	}

	signed, err := c.mc.PresignedGetObject(ctx, c.bucket, key, ttl, nil)
	if err != nil {
		return "", fmt.Errorf("storage: sign %s: %w", key, err)
	}
	return signed.String(), nil
}

// Bucket is exposed for the health probe and for log lines that need to
// say which bucket was being addressed.
func (c *Client) Bucket() string { return c.bucket }
