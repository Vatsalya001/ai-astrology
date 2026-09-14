package httpapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// probeClient is shared by the simple HTTP health probes.
//
// A short timeout is deliberate: the health endpoint bounds the whole
// check at 2s, and a probe that outlives that budget would be reported
// as an error anyway.
var probeClient = &http.Client{Timeout: 2 * time.Second}

// httpProbe reports a dependency healthy when it answers 2xx.
//
// Used for MinIO and Mailpit, which expose plain liveness endpoints and
// need no client library just to be pinged.
// plain adapts a probe that has no configuration to report.
//
// Most dependencies have nothing to say beyond up or down. Adapting them
// here keeps their own signatures free of a detail return they would
// always leave empty — Ping belongs to the database pool, not to this
// endpoint's rendering needs.
func plain(f func(context.Context) error) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) { return "", f(ctx) }
}

func httpProbe(url string) func(context.Context) error {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("build probe request: %w", err)
		}

		resp, err := probeClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()

		// Drain so the connection can be reused rather than closed.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("unexpected status %d", resp.StatusCode)
		}
		return nil
	}
}
