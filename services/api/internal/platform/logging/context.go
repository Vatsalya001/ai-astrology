package logging

import "context"

// ctxKey is unexported so no other package can collide with our context
// keys. This is the standard Go idiom for context values.
type ctxKey int

const traceIDKey ctxKey = iota

// WithTraceID returns a context carrying the given trace ID.
//
// The HTTP middleware calls this once per request; everything downstream
// — including the clients that call astro-service and ai-service — reads
// it back out so a single request is correlatable across all three
// processes.
func WithTraceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, traceIDKey, id)
}

// TraceIDFrom extracts the trace ID, or "" if the context has none.
func TraceIDFrom(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(traceIDKey).(string)
	return id
}
