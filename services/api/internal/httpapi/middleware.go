package httpapi

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/logging"
	"github.com/google/uuid"
)

const headerTraceID = "X-Trace-Id"

// TraceID assigns a trace ID to every request and puts it on the context.
//
// An inbound X-Trace-Id is honoured so a trace started elsewhere (the web
// app, or a future gateway) stays intact. It is length-capped because it
// is attacker-controlled input that ends up in log lines.
func TraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerTraceID)
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}

		ctx := logging.WithTraceID(r.Context(), id)
		w.Header().Set(headerTraceID, id)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// statusRecorder captures the status code so the access log can report it.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Flush forwards to the underlying writer when it supports flushing.
//
// Phase 5 streams SSE through this stack; without this passthrough the
// recorder would silently swallow the Flusher interface and nothing would
// stream. Cheap to add now, confusing to debug later.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// AccessLog emits one structured line per request.
//
// Note what is absent: no query string, no request body, no headers.
// Any of those can carry PII in this product.
func AccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Int64("duration_ms", time.Since(start).Milliseconds()),
		)
	})
}

// Recover turns a panic into a 500 instead of killing the process.
//
// The stack trace is logged, never returned.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.ErrorContext(r.Context(), "panic recovered",
					slog.Any("panic", rec),
					slog.String("stack", string(debug.Stack())),
					slog.String("path", r.URL.Path),
				)
				WriteError(w, r, http.StatusInternalServerError,
					CodeInternal, "Something went wrong on our side.", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// SecurityHeaders sets baseline protective headers.
//
// The CSP here is intentionally strict: this API serves JSON only, so it
// needs no scripts, styles or frames of its own.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
