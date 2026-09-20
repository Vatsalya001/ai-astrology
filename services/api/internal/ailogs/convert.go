package ailogs

import (
	"math"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/clients/aiclient"
)

// The generated client types every optional field as a pointer, because
// OpenAPI cannot distinguish "absent" from "zero". These helpers collapse
// that back to a value with an explicit default, in one place, so the
// INSERT above reads as a list of columns rather than twenty nil checks.
//
// Zero IS the right default for every token count here: a field ai-service
// omitted is a count it did not make, and `0` sums correctly where a NULL
// would poison every aggregate it touched.

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}

// derefBool takes an explicit default, because the two booleans here
// want opposite ones.
//
// `validation_passed` defaults TRUE — an absent field means nothing
// reported a failure. `regenerated` defaults FALSE — an absent field
// means no retry was spent. Defaulting both the same way would either
// mark every legacy row as a validation failure or hide every retry.
func derefBool(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}

// clampInt32 narrows to the INTEGER columns without wrapping.
//
// A plain int32(x) conversion on a value above 2^31 WRAPS, and the
// result is frequently negative — which would then fail the column's
// `>= 0` CHECK and reject the whole row, losing a real cost record over
// an implausible token count. Clamping keeps the row, with a number
// obviously wrong enough to investigate.
func clampInt32(v int) int32 {
	switch {
	case v < 0:
		return 0
	case v > math.MaxInt32:
		return math.MaxInt32
	default:
		return int32(v)
	}
}

// finishReason flattens the generated enum to its string.
//
// Defaults to "stop" rather than "" because the column is NOT NULL and
// an empty finish reason is not a value any dashboard can group by.
func finishReason(p *aiclient.TelemetryFinishReason) string {
	if p == nil || *p == "" {
		return "stop"
	}
	return string(*p)
}

// uuidString renders a pgtype.UUID for JSON.
//
// Empty for an invalid UUID rather than the zero UUID: rendering
// "00000000-0000-0000-0000-000000000000" would look like a real ID and
// send someone looking for a row that does not exist.
func uuidString(u pgtype.UUID) string {
	if !u.Valid {
		return ""
	}
	s, err := u.Value()
	if err != nil {
		return ""
	}
	str, ok := s.(string)
	if !ok {
		return ""
	}
	return str
}
