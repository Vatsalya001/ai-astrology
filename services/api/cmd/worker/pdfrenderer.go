package main

import (
	"context"
	"fmt"
	"log/slog"

	goredis "github.com/redis/go-redis/v9"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/charts"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/config"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/pdf"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/jobs"
	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/storage"
)

/*
Wiring for the PDF renderer, split out of main.go.

Four dependencies have to line up — a browser, object storage, Redis
for print tokens, and the queue — and any of them can be absent in a
given deployment. Keeping that decision in one named function means
main.go reads as a list of things the worker does rather than as a
paragraph of conditionals.

It is also where the honest failure mode lives: a worker missing any
one piece still refreshes transits and sweeps deletions, so it starts,
and it says loudly which capability it is starting without. A process
that refuses to boot because object storage is unconfigured takes down
two working jobs to protect a third.
*/
func registerPDFRenderer(
	ctx context.Context,
	cfg *config.Config,
	rdb goredis.UniversalClient,
	runtime *jobs.Runtime,
	log *slog.Logger,
) error {
	missing := missingPDFConfig(cfg)
	if len(missing) > 0 {
		log.Warn("pdf rendering is disabled",
			slog.Any("missing", missing),
			slog.String("consequence",
				"download requests will queue and never complete"))
		return nil
	}

	/*
	   The browser is checked at startup, not on the first render.

	   A worker whose image ships without a usable Chrome is otherwise
	   indistinguishable from a healthy one until somebody clicks
	   download — minutes or hours after a deploy that looked green, and
	   with the failure attributed to the feature rather than the image.

	   This costs about a second and is worth it. It is a hard error
	   rather than a warning because CHROME_PATH was set: the operator
	   asked for PDF rendering, and silently not providing it is worse
	   than refusing to start.
	*/
	browser, err := pdf.NewChrome(cfg.ChromePath)
	if err != nil {
		return fmt.Errorf("pdf renderer: %w", err)
	}
	if err := browser.StartupCheck(ctx); err != nil {
		return fmt.Errorf("pdf renderer: %w", err)
	}

	store, err := storage.New(storage.Config{
		Endpoint:  cfg.S3Endpoint,
		Bucket:    cfg.S3Bucket,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Region:    cfg.S3Region,
		PathStyle: cfg.S3ForcePathStyle,
	})
	if err != nil {
		return fmt.Errorf("pdf renderer: %w", err)
	}

	// The same token store the API's print route redeems from. One
	// Redis, one key namespace — the worker mints and the API spends.
	tokens := charts.NewPrintTokens(charts.NewRedisPrintTokens(rdb))

	renderer, err := pdf.NewRenderer(
		browser, store, tokens, pdf.NewStatusStore(rdb), cfg.WebURL, log)
	if err != nil {
		return fmt.Errorf("pdf renderer: %w", err)
	}

	runtime.Handle(pdf.TypeRender, renderer.Handle)

	log.Info("pdf rendering enabled",
		slog.String("bucket", store.Bucket()),
		slog.String("chrome", cfg.ChromePath))

	/*
	   No database handle, and that is the design rather than an
	   omission.

	   The renderer's chart data reaches it the same way a user's does:
	   the browser loads the print page, which calls the API. So the
	   worker needs Redis (to mint the token), storage (to put the
	   result) and a URL — and nothing else. Passing it a pool would let
	   a future change read a chart directly here and quietly create a
	   second path to the same data, with its own scoping bugs.
	*/

	return nil
}

// missingPDFConfig names what is absent, so the log line says which
// variable to set rather than "pdf disabled".
func missingPDFConfig(cfg *config.Config) []string {
	var missing []string
	if cfg.ChromePath == "" {
		missing = append(missing, "CHROME_PATH")
	}
	if cfg.S3Endpoint == "" {
		missing = append(missing, "S3_ENDPOINT")
	}
	if cfg.S3Bucket == "" {
		missing = append(missing, "S3_BUCKET")
	}
	if cfg.S3AccessKey == "" {
		missing = append(missing, "S3_ACCESS_KEY")
	}
	if cfg.S3SecretKey == "" {
		missing = append(missing, "S3_SECRET_KEY")
	}
	return missing
}
