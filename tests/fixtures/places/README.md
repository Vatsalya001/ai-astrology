# Place fixtures

`cities-e2e.txt` is twenty places in GeoNames `cities500` format, for the end-to-end
suite and local development.

**It is not the real dataset.** Production seeds from GeoNames' `cities500` dump — 30 MB
of tab-separated text, ~200,000 rows, regenerated monthly. That is downloaded rather
than vendored: unlike the ephemeris kernel, a stale copy degrades gracefully (a missing
village, not a wrong chart), and committing 30 MB that changes every month to test a
prefix scan is a bad trade.

Twenty rows is enough to test what the search has to get right:

| Rows | What they cover |
|---|---|
| **Two Jaipurs** | Population ranking. Rajasthan (2.7 M) must outrank Odisha (612), and it is the reason "jaip" is useful at all. |
| Delhi and New Delhi | Two results for one prefix, both real |
| London, New York, Sydney, Lima | Non-Indian timezones, including southern hemisphere and the Americas |
| Kathmandu | The UTC+05:45 case, so a quarter-hour offset reaches the chart pipeline end to end |

## Regenerating

There is nothing to regenerate — the file is hand-written and stable on purpose. The
coordinates and populations are real GeoNames values so a chart computed against them is
a real chart; the twenty were chosen, not sampled.

To use the full dataset locally:

```bash
curl -O https://download.geonames.org/export/dump/cities500.zip
unzip cities500.zip
go run ./cmd/seed-places --file cities500.txt   # from services/api
```
