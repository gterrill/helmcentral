package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// pathHistoryIO is the pair of InfluxDB reads a bucketed path history needs.
// Mate's get_path_history wires it from assistantToolDeps; the series endpoint
// wires the production queries directly.
type pathHistoryIO struct {
	stat      func(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error)
	firstLast func(ctx context.Context, path, source string, start, stop time.Time) (first, last time.Time, found bool, err error)
}

// pathHistoryBuckets is queryPathHistoryBuckets' result.
type pathHistoryBuckets struct {
	Every                 string
	Width                 time.Duration
	QueryStart, QueryStop time.Time
	FirstSeen, LastSeen   time.Time
	Found                 bool // any raw sample in [start, stop)
	Merged                []assistantPathHistoryPoint
	Gaps                  []assistantPathHistoryGap // every gap, uncapped
	GapCount              int
}

// queryPathHistoryBuckets is the one bucketed-history query behind both
// get_path_history and /api/telemetry/series. It finds the first and last raw
// sample, picks the bucket width with widthFor, runs one
// aggregate query per aggFns entry ("min", "mean", "max") concurrently, merges
// them and works out the gaps.
//
// With fitToData the width follows the span that holds data and the buckets
// are trimmed to it (get_path_history). Without it the width follows the
// requested span and the buckets cover all of [start, stop), so several paths
// queried over one range share a bucket width (the series endpoint).
func queryPathHistoryBuckets(ctx context.Context, io pathHistoryIO, path, source string, start, stop time.Time, aggFns []string, fitToData bool, widthFor func(time.Duration) (string, time.Duration)) (pathHistoryBuckets, error) {
	var out pathHistoryBuckets
	for _, fn := range aggFns {
		switch fn {
		case "min", "mean", "max":
		default:
			return out, fmt.Errorf("unknown aggregate %q (want min, mean or max)", fn)
		}
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}

	// With fitToData the first/last read runs first: the bucket width must
	// follow the span that actually holds data, not the span that was asked
	// for. A six-hour request over an engine that has run for twenty minutes
	// would otherwise get 15-minute buckets, hiding a spike that one-minute
	// buckets (whose max exposes it) show plainly. Without it the width follows
	// the requested span, the read is skipped, and Found comes from the buckets.
	if fitToData {
		first, last, found, err := io.firstLast(ctx, path, source, start, stop)
		if err != nil {
			return out, err
		}
		out.FirstSeen, out.LastSeen, out.Found = first, last, found
	}

	// QueryStart/QueryStop are the range the buckets cover. They equal the
	// requested range unless fitToData and data sits in only part of it.
	out.QueryStart, out.QueryStop = start, stop
	if out.Found && fitToData {
		out.Every, out.Width = widthFor(out.LastSeen.Sub(out.FirstSeen))
		if s := out.FirstSeen.Truncate(out.Width); s.After(out.QueryStart) {
			out.QueryStart = s
		}
		if e := out.LastSeen.Add(out.Width); e.Before(out.QueryStop) {
			out.QueryStop = e
		}
	} else {
		out.Every, out.Width = widthFor(stop.Sub(start))
	}

	if err := ctx.Err(); err != nil {
		return out, err
	}

	// The stat queries share the same range and filter and none depends on
	// another's result, so they run concurrently off the SAME ctx: the worst
	// case is one shared timeout, and cancelling ctx stops every one of them.
	pts := make([][]telemetryPoint, len(aggFns))
	errs := make([]error, len(aggFns))
	var wg sync.WaitGroup
	for i, fn := range aggFns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pts[i], errs[i] = io.stat(ctx, path, source, out.QueryStart, out.QueryStop, out.Every, fn)
		}()
	}
	wg.Wait()
	byFn := map[string][]telemetryPoint{}
	for i, fn := range aggFns {
		if errs[i] != nil {
			return out, errs[i]
		}
		byFn[fn] = pts[i]
	}

	out.Merged = mergePathHistoryPoints(byFn["min"], byFn["mean"], byFn["max"])
	if !fitToData {
		out.Found = len(out.Merged) > 0
	}
	// Gaps run from QueryStart to the REQUESTED stop, not QueryStop: silence
	// after last_seen is the answer to "when did it die".
	out.Gaps, out.GapCount = computePathHistoryGaps(out.QueryStart, stop, out.Width, out.Merged)
	return out, nil
}

// /api/telemetry/series: bucketed means for one to four paths over one range,
// the data behind Mate's telemetry chart card.
const (
	telemetrySeriesMaxPaths  = 4
	telemetrySeriesMaxSpan   = 60 * 24 * time.Hour
	telemetrySeriesMaxFuture = 5 * time.Minute
)

// Swapped in tests, like assistantToolDeps' fields.
var (
	telemetrySeriesIO         = pathHistoryIO{stat: queryInfluxPathStatRange, firstLast: queryInfluxPathFirstLast}
	telemetrySeriesConfigured = influxTelemetryConfigured
	telemetrySeriesNow        = time.Now

	// The overall deadline on one request.
	telemetrySeriesTimeout = 20 * time.Second
)

type telemetrySeriesPoint struct {
	T    string  `json:"t"`
	Mean float64 `json:"mean"`
}

type telemetrySeriesEntry struct {
	Path   string                    `json:"path"`
	Found  bool                      `json:"found"`
	Points []telemetrySeriesPoint    `json:"points"`
	Gaps   []assistantPathHistoryGap `json:"gaps"`
}

type telemetrySeriesResponse struct {
	From   string                 `json:"from"`
	To     string                 `json:"to"`
	Bucket string                 `json:"bucket"`
	Series []telemetrySeriesEntry `json:"series"`
}

func seriesBadRequest(c echo.Context, msg string) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
}

// GET /api/telemetry/series?path=a&path=b&from=RFC3339&to=RFC3339
func telemetrySeriesHandler(c echo.Context) error {
	var paths []string
	for _, p := range c.QueryParams()["path"] {
		p = strings.TrimSpace(p)
		if p == "" {
			return seriesBadRequest(c, "path must not be blank")
		}
		if len(p) > gaugePathMaxLen {
			return seriesBadRequest(c, "path is too long")
		}
		// Interpolated into Flux; see telemetryHistoryHandler.
		if containsFluxInterpolationSyntax(p) {
			return seriesBadRequest(c, "path must not contain '$' or '{'")
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return seriesBadRequest(c, "at least one path is required")
	}
	if len(paths) > telemetrySeriesMaxPaths {
		return seriesBadRequest(c, fmt.Sprintf("at most %d paths", telemetrySeriesMaxPaths))
	}

	from, err := time.Parse(time.RFC3339, c.QueryParam("from"))
	if err != nil {
		return seriesBadRequest(c, "from must be RFC3339 (e.g. 2026-09-21T00:00:00Z)")
	}
	to, err := time.Parse(time.RFC3339, c.QueryParam("to"))
	if err != nil {
		return seriesBadRequest(c, "to must be RFC3339 (e.g. 2026-09-22T00:00:00Z)")
	}
	if !to.After(from) {
		return seriesBadRequest(c, "to must be after from")
	}
	if to.Sub(from) > telemetrySeriesMaxSpan {
		return seriesBadRequest(c, fmt.Sprintf("range must not exceed %d days", int(telemetrySeriesMaxSpan/(24*time.Hour))))
	}
	if to.After(telemetrySeriesNow().Add(telemetrySeriesMaxFuture)) {
		return seriesBadRequest(c, "to must not be in the future")
	}

	if !telemetrySeriesConfigured() {
		return c.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "history needs InfluxDB, which is not configured. Enable it in Settings.",
		})
	}

	// Nothing has happened after now: query and report gaps only up to it.
	if now := telemetrySeriesNow(); to.After(now) {
		to = now
	}
	if !to.After(from) {
		return seriesBadRequest(c, "from must not be in the future")
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), telemetrySeriesTimeout)
	defer cancel()

	// Paths (at most telemetrySeriesMaxPaths) are queried concurrently, and
	// results land by index so the first error is the first by path order.
	results := make([]pathHistoryBuckets, len(paths))
	errs := make([]error, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = queryPathHistoryBuckets(ctx, telemetrySeriesIO, p, "", from, to, []string{"mean"}, false, telemetrySeriesBucketWidth)
		}()
	}
	wg.Wait()
	tookTooLong := func() error {
		return c.JSON(http.StatusGatewayTimeout, map[string]string{
			"error": "the history query took too long. Try a shorter range.",
		})
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return tookTooLong()
	}
	for _, err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, context.DeadlineExceeded):
			// An inner per-query timeout, not only the overall one.
			return tookTooLong()
		case errors.Is(err, context.Canceled):
			// The client went away; nobody is left to read a response.
			return nil
		default:
			return c.JSON(http.StatusBadGateway, map[string]string{"error": "InfluxDB query failed: " + err.Error()})
		}
	}

	bucket, _ := telemetrySeriesBucketWidth(to.Sub(from))
	entries := make([]telemetrySeriesEntry, 0, len(paths))
	var missing []string
	for i, p := range paths {
		h := results[i]
		points := make([]telemetrySeriesPoint, 0, len(h.Merged))
		for _, m := range h.Merged {
			if m.Mean == nil {
				continue
			}
			points = append(points, telemetrySeriesPoint{T: m.T.UTC().Format(time.RFC3339), Mean: *m.Mean})
		}
		gaps := h.Gaps
		if gaps == nil || !h.Found {
			gaps = []assistantPathHistoryGap{}
		}
		if !h.Found {
			missing = append(missing, p)
		}
		entries = append(entries, telemetrySeriesEntry{Path: p, Found: h.Found, Points: points, Gaps: gaps})
	}
	if len(missing) == len(paths) {
		return c.JSON(http.StatusNotFound, map[string]string{
			"error": "no data recorded in that range for: " + strings.Join(missing, ", "),
		})
	}

	return c.JSON(http.StatusOK, telemetrySeriesResponse{
		From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339),
		Bucket: bucket, Series: entries,
	})
}

// telemetrySeriesRoutes returns the /api route for charting recorded telemetry
// (docs/adr/0177).
func telemetrySeriesRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/telemetry/series", tierRead, telemetrySeriesHandler},
	}
}

// telemetrySeriesBucketWidths is the chart allowlist, finest first. The width
// is interpolated into Flux, so it comes from this list and nothing else.
var telemetrySeriesBucketWidths = []struct {
	label string
	width time.Duration
}{
	{"1m", time.Minute}, {"2m", 2 * time.Minute}, {"5m", 5 * time.Minute},
	{"10m", 10 * time.Minute}, {"15m", 15 * time.Minute}, {"30m", 30 * time.Minute},
	{"1h", time.Hour}, {"2h", 2 * time.Hour}, {"3h", 3 * time.Hour}, {"6h", 6 * time.Hour},
}

const telemetrySeriesMaxBuckets = 400

// telemetrySeriesBucketWidth picks the smallest allowlisted width that keeps
// the span at or under telemetrySeriesMaxBuckets buckets: finer than the LLM
// tool's ~60-bucket ladder, because a chart can draw them.
func telemetrySeriesBucketWidth(span time.Duration) (string, time.Duration) {
	for _, w := range telemetrySeriesBucketWidths {
		if int((span+w.width-1)/w.width) <= telemetrySeriesMaxBuckets {
			return w.label, w.width
		}
	}
	last := telemetrySeriesBucketWidths[len(telemetrySeriesBucketWidths)-1]
	return last.label, last.width
}
