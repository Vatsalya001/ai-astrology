//go:build integration

package pdf_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/pdf"
)

/*
A real browser, producing a real PDF.

Everything else in this package tests the orchestration with the
browser faked, which is the right shape for logic — but it leaves the
one component that shells out to a 200MB binary entirely unexercised.
That component is also the likeliest to break for reasons unrelated to
this code: a base image without Chrome, a container without
/dev/shm headroom, a sandbox that cannot be disabled.

So these run the actual binary. They skip rather than fail when
CHROME_PATH names nothing, because a developer machine without Chrome
is a normal state and this is not the test that should block them.
*/

func chromePath(t *testing.T) string {
	t.Helper()

	path := os.Getenv("CHROME_PATH")
	if path == "" {
		// The usual places, so the test runs without arranging anything
		// on a machine that simply has a browser.
		for _, candidate := range []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		} {
			if _, err := os.Stat(candidate); err == nil {
				path = candidate
				break
			}
		}
	}
	if path == "" {
		t.Skip("no Chrome found; set CHROME_PATH to run the browser tests")
	}
	if _, err := os.Stat(path); err != nil {
		t.Skipf("CHROME_PATH=%s does not exist: %v", path, err)
	}
	return path
}

// The startup check is what stands between a worker image with no
// browser and a user discovering it by clicking download.
func TestTheStartupCheckPassesAgainstARealBrowser(t *testing.T) {
	browser, err := pdf.NewChrome(chromePath(t))
	if err != nil {
		t.Fatalf("NewChrome: %v", err)
	}

	// 90s, not the 20s default. A cold Chrome on a shared CI runner —
	// in a job that may have just downloaded it — has exceeded 20s and
	// turned this red on a docs-only commit, where the change could not
	// possibly have caused it.
	//
	// The deadline is the CALLER's to choose, which is why StartupCheck
	// no longer overrides it. Production still boots with the 20s
	// default because it passes a context with no deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	if err := browser.StartupCheck(ctx); err != nil {
		t.Fatalf("StartupCheck against a real browser failed: %v", err)
	}
}

// TestStartupCheckRespectsAShorterCallerDeadline is the other direction,
// and the one that proves the change is not simply "wait longer".
//
// A caller asking for 1ms must get 1ms. Without this, `StartupCheck`
// could go back to imposing its own timeout unconditionally and only the
// slow case would notice — which is how the flake arrived in the first
// place.
func TestStartupCheckRespectsAShorterCallerDeadline(t *testing.T) {
	browser, err := pdf.NewChrome(chromePath(t))
	if err != nil {
		t.Fatalf("NewChrome: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := browser.StartupCheck(ctx); err == nil {
		t.Fatal("a 1ms deadline should not have been enough to start a browser")
	}

	// Well under the 20s default, so this fails if the default is ever
	// re-imposed on top of the caller's context.
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("took %s — the caller's 1ms deadline was overridden", elapsed)
	}
}

func TestTheStartupCheckFailsOnAPathThatIsNotABrowser(t *testing.T) {
	chromePath(t) // skip early on a machine with no browser at all

	browser, err := pdf.NewChrome("/bin/false")
	if err != nil {
		t.Fatalf("NewChrome: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := browser.StartupCheck(ctx); err == nil {
		t.Fatal("the startup check passed against /bin/false. It is the only thing " +
			"between a worker image with no usable browser and a user finding out " +
			"by clicking download")
	}
}

func TestAnEmptyPathIsRefusedAtConstruction(t *testing.T) {
	if _, err := pdf.NewChrome(""); err == nil {
		t.Fatal("NewChrome accepted an empty path; the worker would discover this " +
			"on the first render rather than at startup")
	}
}

/*
The full render, against a page that behaves like the print route.

The wait is asserted DIFFERENTIALLY: the same page is rendered twice,
once signalling immediately and once after a delay, and the second must
take measurably longer.

An absolute threshold was the first attempt and it does not work.
Launching Chrome costs the better part of a second on its own, so
"elapsed > 700ms" is satisfied by startup alone — the assertion passed
with the WaitVisible deleted, which is a guard that cannot fire.
Rendering both variants in one test subtracts that fixed cost instead
of guessing at it.
*/
func TestPrintToPDFWaitsForThePageToSayItIsReady(t *testing.T) {
	path := chromePath(t)

	const delay = 2 * time.Second

	page := func(afterMs int) string {
		return `<!doctype html><html><head><title>print</title></head><body>
  <h1>Kundli</h1>
  <script>
    setTimeout(function () {
      document.body.setAttribute('data-print-ready', 'true')
    }, ` + strconv.Itoa(afterMs) + `)
  </script>
</body></html>`
	}

	render := func(t *testing.T, afterMs int) (time.Duration, []byte) {
		t.Helper()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(page(afterMs)))
		}))
		defer server.Close()

		browser, err := pdf.NewChrome(path)
		if err != nil {
			t.Fatalf("NewChrome: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		started := time.Now()
		body, err := browser.PrintToPDF(ctx, server.URL)
		if err != nil {
			t.Fatalf("PrintToPDF: %v", err)
		}
		return time.Since(started), body
	}

	immediate, body := render(t, 0)
	delayed, _ := render(t, int(delay/time.Millisecond))

	// A real PDF, not an empty buffer or a rendered error page.
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Fatalf("the output does not start with %%PDF-; got %q",
			string(body[:min(16, len(body))]))
	}
	if len(body) < 1_000 {
		t.Fatalf("the PDF is %d bytes, too small to contain a rendered page", len(body))
	}

	/*
	   Half the delay, not the whole of it: the point is to distinguish
	   "waited" from "did not wait", and a threshold sitting right on the
	   real value is a flaky test on a loaded machine. With the
	   WaitVisible removed the difference is near zero, which is nowhere
	   near this bar.
	*/
	if delayed-immediate < delay/2 {
		t.Fatalf("signalling ready %s later cost only %s (%s vs %s). The browser is "+
			"not waiting for data-print-ready — for the real print route that means "+
			"printing a page before the chart is on it",
			delay, delayed-immediate, immediate, delayed)
	}
}

/*
A page that never signals is bounded by the caller's context.

The real print route sets the flag in every terminal state, including
its failures — but a page that hangs is exactly the case the renderer's
deadline exists for, and "the browser respects the context" is the
assumption that makes it work.
*/
func TestPrintToPDFGivesUpWhenThePageNeverSignals(t *testing.T) {
	path := chromePath(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// No data-print-ready, ever.
		_, _ = w.Write([]byte(`<!doctype html><html><body><h1>stuck</h1></body></html>`))
	}))
	defer server.Close()

	browser, err := pdf.NewChrome(path)
	if err != nil {
		t.Fatalf("NewChrome: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	started := time.Now()
	_, err = browser.PrintToPDF(ctx, server.URL)
	if err == nil {
		t.Fatal("PrintToPDF returned a document for a page that never signalled ready")
	}
	if elapsed := time.Since(started); elapsed > 20*time.Second {
		t.Fatalf("PrintToPDF took %s to honour a 4s context. If the browser ignores "+
			"the deadline, the renderer's own timeout cannot bound a render and a "+
			"stuck job holds a worker slot indefinitely", elapsed)
	}
}

// The error must not carry the URL, because the URL carries a live
// print token and this error is logged.
func TestAFailedRenderDoesNotLogTheURL(t *testing.T) {
	path := chromePath(t)

	browser, err := pdf.NewChrome(path)
	if err != nil {
		t.Fatalf("NewChrome: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// A port nothing is listening on, with a token-shaped query string.
	const secret = "aVerySecretPrintToken22"
	_, err = browser.PrintToPDF(ctx, "http://127.0.0.1:1/kundli/print?token="+secret)
	if err == nil {
		t.Fatal("PrintToPDF succeeded against a closed port")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("the render error contains the print token: %v", err)
	}
}
