// Command seed-places imports the GeoNames cities dataset.
//
// It lives with api-service because api-service owns the `places` table —
// the single-writer rule applies to seed data exactly as it does to user
// data.
//
// Usage:
//
//	go run ./cmd/seed-places --file cities500.txt
//
// The dataset is downloaded separately rather than vendored: it is 30 MB
// of tab-separated text that changes monthly, and unlike the ephemeris
// kernel a stale copy degrades gracefully — a missing village, not a
// wrong chart.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Vatsalya001/ai-astrology/services/api/internal/platform/db/dbgen"
)

// GeoNames column indices, from the published format.
//
// Named rather than inlined because a tab-separated file with nineteen
// columns is exactly where an off-by-one hides — and the symptom would be
// a whole dataset of places whose timezone is actually their elevation.
const (
	colGeonameID  = 0
	colName       = 1
	colASCIIName  = 2
	colLatitude   = 4
	colLongitude  = 5
	colCountry    = 8
	colAdmin1     = 10
	colPopulation = 14
	colTimezone   = 17

	expectedColumns = 19
)

func main() {
	file := flag.String("file", "", "path to the GeoNames cities dump (tab-separated)")
	batchSize := flag.Int("batch", 1000, "rows per progress report")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if *file == "" {
		logger.Error("--file is required",
			slog.String("hint", "curl -O https://download.geonames.org/export/dump/cities500.zip"))
		os.Exit(2)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL is not set")
		os.Exit(2)
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		logger.Error("connect", slog.Any("err", err))
		os.Exit(1)
	}
	defer pool.Close()

	imported, skipped, err := Import(ctx, dbgen.New(pool), *file, *batchSize, logger)
	if err != nil {
		logger.Error("import failed", slog.Any("err", err))
		os.Exit(1)
	}

	logger.Info("done", slog.Int("imported", imported), slog.Int("skipped", skipped))
}

// Import reads the dump and upserts every row.
//
// Exported so an integration test can drive it against a real database
// with a small fixture, rather than testing the parser and the SQL
// separately and hoping they agree.
func Import(
	ctx context.Context,
	q dbgen.Querier,
	path string,
	batchSize int,
	logger *slog.Logger,
) (imported int, skipped int, err error) {
	handle, err := os.Open(path)
	if err != nil {
		return 0, 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = handle.Close() }()

	scanner := bufio.NewScanner(handle)
	// GeoNames lines are short, but a long alternate-names column can
	// exceed bufio's 64 KB default and the scanner would stop silently
	// mid-file — an import that reports success having loaded half the
	// world.
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	started := time.Now()

	for line := 1; scanner.Scan(); line++ {
		params, parseErr := parseLine(scanner.Text())
		if parseErr != nil {
			skipped++
			// Logged individually at debug; a malformed row is normal in a
			// 200k-line third-party dump and must not abort the import.
			logger.Debug("skipping row", slog.Int("line", line), slog.Any("err", parseErr))
			continue
		}

		if err := q.UpsertPlace(ctx, params); err != nil {
			return imported, skipped, fmt.Errorf("upsert line %d: %w", line, err)
		}
		imported++

		if batchSize > 0 && imported%batchSize == 0 {
			logger.Info("progress",
				slog.Int("imported", imported),
				slog.Duration("elapsed", time.Since(started)))
		}
	}

	if err := scanner.Err(); err != nil {
		return imported, skipped, fmt.Errorf("read %s: %w", path, err)
	}

	// A file that produced nothing is a failure, not a quiet success.
	// Without this, a wrong path or a changed format reports "done,
	// imported 0" and place search silently returns nothing forever.
	if imported == 0 {
		return 0, skipped, errors.New("no rows imported; is the file the GeoNames tab-separated format?")
	}

	return imported, skipped, nil
}

func parseLine(line string) (dbgen.UpsertPlaceParams, error) {
	fields := strings.Split(line, "\t")
	if len(fields) < expectedColumns {
		return dbgen.UpsertPlaceParams{},
			fmt.Errorf("expected %d columns, got %d", expectedColumns, len(fields))
	}

	id, err := strconv.ParseInt(fields[colGeonameID], 10, 32)
	if err != nil {
		return dbgen.UpsertPlaceParams{}, fmt.Errorf("geonameid: %w", err)
	}

	latitude, err := strconv.ParseFloat(fields[colLatitude], 64)
	if err != nil {
		return dbgen.UpsertPlaceParams{}, fmt.Errorf("latitude: %w", err)
	}
	longitude, err := strconv.ParseFloat(fields[colLongitude], 64)
	if err != nil {
		return dbgen.UpsertPlaceParams{}, fmt.Errorf("longitude: %w", err)
	}

	// Population is blank for many small places. Blank is zero, not an
	// error — those places are exactly the villages the map fallback
	// exists for, and dropping them would defeat it.
	population := int64(0)
	if raw := strings.TrimSpace(fields[colPopulation]); raw != "" {
		population, err = strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return dbgen.UpsertPlaceParams{}, fmt.Errorf("population: %w", err)
		}
	}

	timezone := strings.TrimSpace(fields[colTimezone])
	if timezone == "" {
		// A place with no timezone cannot be used for a birth chart at
		// all, so it is skipped rather than imported with a blank that
		// would later resolve to UTC.
		return dbgen.UpsertPlaceParams{}, errors.New("no timezone")
	}

	var admin1 *string
	if value := strings.TrimSpace(fields[colAdmin1]); value != "" {
		admin1 = &value
	}

	return dbgen.UpsertPlaceParams{
		ID:          int32(id),
		Name:        fields[colName],
		AsciiName:   fields[colASCIIName],
		Admin1:      admin1,
		CountryCode: fields[colCountry],
		Latitude:    latitude,
		Longitude:   longitude,
		Timezone:    timezone,
		Population:  int32(population),
	}, nil
}
