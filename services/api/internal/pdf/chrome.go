package pdf

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Chrome is the real Browser: headless Chromium via CDP.
//
// One browser process per render rather than a pooled one. A render is
// seconds and a browser is ~100ms to start, so pooling buys little — and
// it costs the thing that matters most here: a fresh process cannot
// carry state from the previous user's document. Given that each render
// loads a page holding somebody's birth data behind a credential in the
// URL, "the process is discarded afterwards" is worth far more than the
// startup time.
type Chrome struct {
	// execPath is the browser binary. Explicit rather than discovered,
	// so a worker image that ships without one fails at startup with a
	// named path instead of on the first user's download.
	execPath string
}

func NewChrome(execPath string) (*Chrome, error) {
	if execPath == "" {
		return nil, fmt.Errorf("pdf: chrome executable path is required " +
			"(set CHROME_PATH; the worker image installs one)")
	}
	return &Chrome{execPath: execPath}, nil
}

// PrintToPDF loads a page and returns its printed bytes.
func (c *Chrome) PrintToPDF(ctx context.Context, pageURL string) ([]byte, error) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(c.execPath),
		chromedp.Headless,

		// --no-sandbox is required inside a container without user
		// namespaces, which is where this runs. It is safe HERE and would
		// not be in general: the only thing this browser ever loads is our
		// own print page, on our own origin, and the process is discarded
		// immediately afterwards. It never sees third-party content.
		chromedp.NoSandbox,

		// /dev/shm is 64MB in a default container and Chrome will crash
		// when it fills. The symptom is an empty PDF rather than an error,
		// which is the worst way to learn about it.
		chromedp.Flag("disable-dev-shm-usage", true),

		// Nothing here needs GPU, extensions or the first-run experience.
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("no-first-run", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	var body []byte
	err := chromedp.Run(browserCtx,
		chromedp.Navigate(pageURL),

		/*
		   Wait for the page to SAY it is ready, not for a timer.

		   The print page sets `data-print-ready` on <body> once the chart
		   SVG has laid out and its fonts have loaded. A fixed sleep is the
		   obvious alternative and it is wrong in both directions: too
		   short on a cold worker and the PDF is missing the chart, too
		   long and every render pays for the worst case.
		*/
		chromedp.WaitVisible(`body[data-print-ready="true"]`, chromedp.ByQuery),

		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			body, _, err = page.PrintToPDF().
				// A4 in inches, because that is the unit CDP takes.
				WithPaperWidth(8.27).
				WithPaperHeight(11.69).
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				// The chart's colours ARE the content — a house highlighted
				// in gold is meaningless printed as grey. Without this,
				// Chrome drops backgrounds by default.
				WithPrintBackground(true).
				WithPreferCSSPageSize(false).
				Do(ctx)
			if err != nil {
				return fmt.Errorf("print to pdf: %w", err)
			}
			return nil
		}),
	)
	if err != nil {
		// pageURL is deliberately absent: it carries a print token, and
		// this error is logged.
		return nil, fmt.Errorf("pdf: chrome: %w", err)
	}

	if len(body) == 0 {
		return nil, fmt.Errorf("pdf: chrome returned an empty document")
	}
	return body, nil
}

// StartupCheck renders a trivial page, to prove the browser works before
// a user ever asks for one.
//
// Worth its second of startup time: every other way of discovering that
// the worker image has no usable Chrome involves a user clicking
// download and getting an error, minutes after a deploy that looked
// green.
func (c *Chrome) StartupCheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(c.execPath),
		chromedp.Headless,
		chromedp.NoSandbox,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("disable-gpu", true),
	)

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	var body []byte
	err := chromedp.Run(browserCtx,
		chromedp.Navigate("about:blank"),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			body, _, err = page.PrintToPDF().Do(ctx)
			return err
		}),
	)
	if err != nil {
		return fmt.Errorf("pdf: chrome startup check at %s: %w", c.execPath, err)
	}
	if len(body) == 0 {
		return fmt.Errorf("pdf: chrome startup check at %s produced no bytes", c.execPath)
	}
	return nil
}
