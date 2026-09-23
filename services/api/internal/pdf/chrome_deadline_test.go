package pdf

import (
	"context"
	"testing"
	"time"
)

// TestWithStartupDeadline covers the decision that made CI intermittently
// red: StartupCheck used to impose 20s unconditionally, overriding a
// caller that had asked for longer.
//
// Tested here rather than through StartupCheck because the interesting
// case — a caller asking for MORE than the default — cannot be observed
// through the public function without hanging a browser for 20 seconds.
//
// The first attempt at this test drove StartupCheck with a 1ms deadline
// and asserted it returned quickly. That passes with or without the fix:
// `context.WithTimeout` already takes the earlier of two deadlines, so
// the short direction was never the bug.
func TestWithStartupDeadline(t *testing.T) {
	t.Run("imposes the default when the caller set none", func(t *testing.T) {
		ctx, cancel := withStartupDeadline(context.Background())
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("no deadline applied — a worker with no usable browser would hang at boot")
		}
		if d := time.Until(deadline); d > startupCheckTimeout+time.Second {
			t.Fatalf("deadline is %s, expected about %s", d, startupCheckTimeout)
		}
	})

	t.Run("does not shorten a caller that asked for longer", func(t *testing.T) {
		// The assertion the flake needed. CI runners cold-start Chrome in
		// more than 20s under contention, and capping them at 20s turned
		// a docs-only commit red.
		want := 90 * time.Second
		parent, cancelParent := context.WithTimeout(context.Background(), want)
		defer cancelParent()

		ctx, cancel := withStartupDeadline(parent)
		defer cancel()

		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatal("the caller's deadline was dropped entirely")
		}
		if d := time.Until(deadline); d < want-5*time.Second {
			t.Fatalf("caller asked for %s and got %s — the default was imposed on top", want, d)
		}
	})

	t.Run("does not lengthen a caller that asked for less", func(t *testing.T) {
		// Go gives this for free, and it is asserted anyway: it is the
		// control that stops "respect the caller" being implemented as
		// "never apply a deadline".
		parent, cancelParent := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancelParent()

		ctx, cancel := withStartupDeadline(parent)
		defer cancel()

		deadline, _ := ctx.Deadline()
		if d := time.Until(deadline); d > time.Second {
			t.Fatalf("caller asked for 50ms and got %s", d)
		}
	})
}
