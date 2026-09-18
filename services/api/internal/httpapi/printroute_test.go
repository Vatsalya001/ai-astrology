package httpapi

import (
	"testing"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/pdf"
)

/*
Mounting the PDF routes without a limiter is refused at construction.

Not a style check. Deps.Limiter is a concrete *ratelimit.Limiter and
pdf.Create takes an interface, so a nil pointer becomes a NON-NIL
interface holding a nil value. The handler's own `limiter != nil` guard
passes, and Allow is called on a nil receiver.

Without this check the symptom is a panic on the first PDF request in
production, with a stack trace instead of a reason. With it, the
process refuses to start.

Written as a unit test rather than an integration one because it needs
no dependencies at all — that is the point: everything else is nil.
*/
func TestMountingPDFWithoutALimiterIsRefusedAtConstruction(t *testing.T) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("NewRouter accepted a PDF handler with no limiter. The first " +
				"download in production would panic on a nil receiver: Deps.Limiter " +
				"is a concrete pointer and pdf.Create takes an interface, so a nil " +
				"one is a non-nil interface that the handler's own guard lets through")
		}
		message, ok := recovered.(string)
		if !ok || !contains(message, "Limiter") {
			t.Fatalf("panicked with %v; the message should name the missing dependency", recovered)
		}
	}()

	newChiRouter(Deps{
		PDF: pdf.NewHandler(nil, nil, AuthErrorWriter),
	})
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
