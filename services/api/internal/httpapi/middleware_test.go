package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
)

func TestTraceIDAdoptsInboundHeader(t *testing.T) {
	var seen string
	h := TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = logging.TraceIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(headerTraceID, "upstream-trace-1")
	h.ServeHTTP(rec, req)

	if seen != "upstream-trace-1" {
		t.Errorf("context trace = %q, want the inbound value", seen)
	}
	if got := rec.Header().Get(headerTraceID); got != "upstream-trace-1" {
		t.Errorf("response header = %q, want it echoed", got)
	}
}

func TestTraceIDMintsWhenAbsent(t *testing.T) {
	var seen string
	h := TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = logging.TraceIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if seen == "" {
		t.Fatal("no trace ID was minted")
	}
	if len(seen) != 36 { // UUID
		t.Errorf("minted trace %q is not a UUID", seen)
	}
}

// TestTraceIDRejectsUnsafeContent is the important one.
//
// The header is attacker-controlled and lands in every log line for the
// request. Length-capping alone still lets a caller write an email
// address, a token or a log-injection payload into logs that are
// otherwise carefully PII-free.
func TestTraceIDRejectsUnsafeContent(t *testing.T) {
	unsafe := []struct {
		name  string
		value string
	}{
		{"email address", "victim@example.com"},
		{"whitespace", "trace with spaces"},
		{"newline injection", "abc\nlevel=ERROR msg=fake"},
		{"json breakout", `abc","pii":"leaked`},
		{"path traversal", "../../etc/passwd"},
		{"ansi escape", "abc\x1b[31mred"},
		{"empty", ""},
	}

	for _, tc := range unsafe {
		t.Run(tc.name, func(t *testing.T) {
			var seen string
			h := TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				seen = logging.TraceIDFrom(r.Context())
			}))

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set(headerTraceID, tc.value)
			h.ServeHTTP(rec, req)

			if seen == tc.value {
				t.Errorf("unsafe trace ID %q was accepted verbatim", tc.value)
			}
			if !safeTraceID.MatchString(seen) {
				t.Errorf("replacement %q is itself not well-formed", seen)
			}
		})
	}
}

// TestTraceIDAcceptsCommonFormats: the constraint must not break
// interoperability with real tracing systems.
func TestTraceIDAcceptsCommonFormats(t *testing.T) {
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000", // UUID
		"4bf92f3577b34da6a3ce929d0e0e4736",     // W3C trace-context
		"req_01HQ8Z",                           // prefixed ID
		"a.b.c",                                // dotted
	}

	for _, v := range valid {
		var seen string
		h := TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			seen = logging.TraceIDFrom(r.Context())
		}))
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(headerTraceID, v)
		h.ServeHTTP(rec, req)

		if seen != v {
			t.Errorf("valid trace ID %q was rejected (got %q)", v, seen)
		}
	}
}

func TestTraceIDRejectsOverlongInbound(t *testing.T) {
	overlong := strings.Repeat("A", 65)

	var seen string
	h := TraceID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = logging.TraceIDFrom(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(headerTraceID, overlong)
	h.ServeHTTP(rec, req)

	if seen == overlong {
		t.Error("an over-length trace ID was accepted; it must be replaced")
	}
	if len(seen) != 36 {
		t.Errorf("expected a minted UUID, got %q", seen)
	}
}

// TestRecoverReturns500WithoutLeaking is the important one: a panic must
// become a generic 500. A stack trace in a response body hands an
// attacker the file layout and dependency versions.
func TestRecoverReturns500WithoutLeaking(t *testing.T) {
	h := TraceID(Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("database credentials are hunter2")
	})))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}

	body := rec.Body.String()
	for _, leak := range []string{"hunter2", "panic", "goroutine", ".go:"} {
		if strings.Contains(body, leak) {
			t.Errorf("response leaked %q:\n%s", leak, body)
		}
	}

	var env struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("panic response is not the standard error envelope: %v", err)
	}
	if env.Error.Code != CodeInternal {
		t.Errorf("code = %q, want %q", env.Error.Code, CodeInternal)
	}
}

func TestRecoverPassesThroughNormalResponses(t *testing.T) {
	h := Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("status = %d, want 418 — Recover must not alter normal responses", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := SecurityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP = %q; this API serves JSON only and needs nothing", csp)
	}
}

// TestStatusRecorderForwardsFlush guards a property Phase 5 depends on.
// The SSE stream goes through this middleware stack; if the recorder
// swallowed http.Flusher, nothing would stream and the chat UI would
// appear to hang until the response completed.
func TestStatusRecorderForwardsFlush(t *testing.T) {
	rec := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: rec}

	if _, ok := any(sr).(http.Flusher); !ok {
		t.Fatal("statusRecorder does not implement http.Flusher — SSE would not stream")
	}

	sr.WriteHeader(http.StatusOK)
	if _, err := sr.Write([]byte("chunk")); err != nil {
		t.Fatalf("write: %v", err)
	}
	sr.Flush()

	if !rec.Flushed {
		t.Error("Flush was not forwarded to the underlying ResponseWriter")
	}
}

func TestStatusRecorderTracksStatusAndBytes(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	sr.WriteHeader(http.StatusCreated)
	if _, err := sr.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}

	if sr.status != http.StatusCreated {
		t.Errorf("status = %d, want 201", sr.status)
	}
	if sr.bytes != 5 {
		t.Errorf("bytes = %d, want 5", sr.bytes)
	}
}

func TestStatusRecorderDefaultsTo200OnBareWrite(t *testing.T) {
	sr := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	if _, err := sr.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if sr.status != http.StatusOK {
		t.Errorf("status = %d, want 200 for a write with no explicit header", sr.status)
	}
}

// TestWriteErrorDoesNotLeakCause: the cause is for the log, never the
// client. This separation is the whole reason WriteError exists.
func TestWriteErrorDoesNotLeakCause(t *testing.T) {
	secret := errors.New(`pq: password authentication failed for user "astro"`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(logging.WithTraceID(req.Context(), "trace-xyz"))

	WriteError(rec, req, http.StatusInternalServerError, CodeInternal,
		"Something went wrong on our side.", secret)

	body := rec.Body.String()
	if strings.Contains(body, "password") || strings.Contains(body, "astro") {
		t.Errorf("internal error text leaked to the client:\n%s", body)
	}

	var env struct {
		Error ErrorBody `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("invalid envelope: %v", err)
	}
	if env.Error.TraceID != "trace-xyz" {
		t.Errorf("trace_id = %q; the client needs it to reference the server-side log",
			env.Error.TraceID)
	}
	if !env.Error.RetryAble {
		t.Error("a 500 should be marked retryable")
	}
}

func TestWriteErrorRetryableFlag(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusNotFound, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusServiceUnavailable, true},
	}

	for _, tc := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		WriteError(rec, req, tc.status, CodeBadRequest, "msg", nil)

		var env struct {
			Error ErrorBody `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatalf("status %d: invalid envelope: %v", tc.status, err)
		}
		if env.Error.RetryAble != tc.want {
			t.Errorf("status %d: retryable = %v, want %v", tc.status, env.Error.RetryAble, tc.want)
		}
	}
}

func TestWriteJSONSetsContentType(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteJSON(rec, http.StatusOK, map[string]string{"ok": "yes"})

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}
