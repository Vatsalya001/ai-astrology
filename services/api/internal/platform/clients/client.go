// Package clients holds the HTTP clients api-service uses to reach the
// internal Python services.
//
// NOTE ON SCOPE: this file provides the shared transport concerns —
// timeout, internal-token auth, trace propagation, health checks.
// Domain calls (compute a chart, run a chat turn) will use clients
// GENERATED from each service's OpenAPI document, per Phase 0 task 0.9.
// Do not hand-write those; a hand-written cross-service client drifts
// silently from the contract it is supposed to implement.
package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
)

const (
	headerInternalToken = "X-Internal-Token"
	headerTraceID       = "X-Trace-Id"

	// Responses from internal services are bounded so a misbehaving
	// service cannot exhaust this process's memory.
	maxResponseBytes = 8 << 20 // 8 MiB
)

// Service is a thin HTTP client for one internal service.
type Service struct {
	name    string
	baseURL string
	token   string
	http    *http.Client
}

func New(name, baseURL, token string, timeout time.Duration) *Service {
	return &Service{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (s *Service) Name() string { return s.name }

// Do issues a request, attaching the internal token and propagating the
// trace ID so one user request is correlatable across all three services.
func (s *Service) Do(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", s.name, err)
	}

	req.Header.Set(headerInternalToken, s.token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if id := logging.TraceIDFrom(ctx); id != "" {
		req.Header.Set(headerTraceID, id)
	}

	resp, err := s.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s: request failed: %w", s.name, err)
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxResponseBytes)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read a little of the body for the log, but never surface it to
		// the end user — an internal service's error text is not a
		// client-facing message.
		snippet, _ := io.ReadAll(io.LimitReader(limited, 512))
		return fmt.Errorf("%s: unexpected status %d: %s",
			s.name, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, limited)
		return nil
	}

	if err := json.NewDecoder(limited).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", s.name, err)
	}
	return nil
}

// HealthResponse is the shape every service in this system returns from
// GET /health. Keeping it identical across Go and Python means the
// aggregate health check needs no per-service special casing.
type HealthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
	Version string `json:"version"`
}

// Health probes the service. The caller supplies the timeout via ctx.
func (s *Service) Health(ctx context.Context) error {
	var out HealthResponse
	if err := s.Do(ctx, http.MethodGet, "/health", nil, &out); err != nil {
		return err
	}
	if out.Status != "ok" {
		return fmt.Errorf("%s: reported status %q", s.name, out.Status)
	}
	return nil
}
