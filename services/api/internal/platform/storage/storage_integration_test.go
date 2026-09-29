//go:build integration

package storage_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/storage"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

/*
The one property the object store is chosen for.

`storage.go` uses three operations, and only one of them is interesting:
`PresignedGetObject`. The PDF download hands a BROWSER a signed URL, so
possessing the URL is the authorisation — a store that accepts writes
and rejects signed reads would pass every smoke test and break the only
feature that touches object storage.

Written when MinIO was swapped for SeaweedFS (ADR-013), because that
swap turned entirely on this question and nothing in the suite asked it.
The e2e PDF test covers it end to end, but only when a browser, a worker
and a chart are all in play; this asks it directly, so a storage
regression reads as a storage failure rather than as a broken download.
*/

func endpoint(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("S3_ENDPOINT"); v != "" {
		return v
	}
	// The compose default. Skipping instead of failing: this file is
	// behind the `integration` tag, and a developer running it without
	// the stack up should be told why, not shown a connection error.
	return "http://localhost:9000"
}

func newClient(t *testing.T) *storage.Client {
	t.Helper()

	client, err := storage.New(storage.Config{
		Endpoint:  endpoint(t),
		Bucket:    "astro-dev",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}
	return client
}

func TestPresignedURLsWorkAgainstTheRealGateway(t *testing.T) {
	ctx := context.Background()
	client := newClient(t)

	key := "integration/presign-probe.pdf"
	body := []byte("%PDF-1.4\npresign probe\n%%EOF\n")

	if err := client.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "application/pdf"); err != nil {
		t.Skipf("object storage unreachable at %s (%v) — bring the stack up with ./scripts/ayana up", endpoint(t), err)
	}

	signed, err := client.SignedURL(ctx, key, 15*time.Minute)
	if err != nil {
		t.Fatalf("SignedURL: %v", err)
	}
	if !strings.Contains(signed, "X-Amz-Signature") {
		t.Fatalf("not a SigV4 presigned URL: %s", signed)
	}

	resp, err := http.Get(signed) //nolint:gosec,noctx // this is exactly what a browser does
	if err != nil {
		t.Fatalf("GET the signed URL: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the signed URL returned %d — the gateway rejected minio-go's signature", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if !bytes.Equal(got, body) {
		t.Fatalf("signed URL served %d bytes, expected %d", len(got), len(body))
	}
}

// TestAnUnsignedGetIsRefused is the control, and it carries as much
// weight as the test above.
//
// Without it, `TestPresignedURLsWorkAgainstTheRealGateway` would pass
// against a world-readable bucket — where the signature is decoration
// and every PDF ever generated is public to anyone who guesses a key.
func TestAnUnsignedGetIsRefused(t *testing.T) {
	ctx := context.Background()
	client := newClient(t)

	key := "integration/unsigned-probe.pdf"
	body := []byte("%PDF-1.4\nunsigned probe\n%%EOF\n")

	if err := client.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "application/pdf"); err != nil {
		t.Skipf("object storage unreachable at %s (%v)", endpoint(t), err)
	}

	plain := endpoint(t) + "/" + client.Bucket() + "/" + key
	resp, err := http.Get(plain) //nolint:gosec,noctx
	if err != nil {
		t.Fatalf("plain GET: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK {
		t.Fatalf(
			"%s is readable WITHOUT a signature — the bucket is public, so presigning "+
				"proves nothing and every generated PDF is world-readable",
			plain,
		)
	}
}

// TestTheBucketIsNotCreatedOnDemand pins a decision `storage.New`
// documents: it does not create the bucket.
//
// A bucket conjured by the first write hides a misconfigured bucket name
// behind a service that looks healthy, and the first symptom is objects
// nobody can find. Compose provisions it; terraform does in production.
func TestTheBucketIsNotCreatedOnDemand(t *testing.T) {
	ctx := context.Background()

	client, err := storage.New(storage.Config{
		Endpoint:  endpoint(t),
		Bucket:    "a-bucket-that-does-not-exist",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Region:    "us-east-1",
		PathStyle: true,
	})
	if err != nil {
		t.Fatalf("storage.New: %v", err)
	}

	body := []byte("x")
	err = client.Put(ctx, "probe", bytes.NewReader(body), int64(len(body)), "application/octet-stream")
	if err == nil {
		t.Fatal("writing to a nonexistent bucket succeeded — the bucket was created on demand")
	}

	// And the real bucket does exist, so the failure above is about the
	// missing bucket rather than about the store being unreachable.
	admin, mkErr := minio.New(strings.TrimPrefix(endpoint(t), "http://"), &minio.Options{
		Creds:        credentials.NewStaticV4("minioadmin", "minioadmin", ""),
		Secure:       false,
		BucketLookup: minio.BucketLookupPath,
	})
	if mkErr != nil {
		t.Fatalf("admin client: %v", mkErr)
	}
	exists, exErr := admin.BucketExists(ctx, "astro-dev")
	if exErr != nil {
		t.Skipf("object storage unreachable (%v)", exErr)
	}
	if !exists {
		t.Fatal("astro-dev does not exist — compose did not provision it")
	}
}
