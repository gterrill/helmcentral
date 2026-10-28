package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
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
	// Weighted and Undefined are set by queryCircularHistoryBuckets: whether
	// the direction was averaged with a speed weight, and how many buckets had
	// no direction (the samples cancelled out, or it was calm).
	Weighted  bool
	Undefined int
	// UnweightedBuckets counts the buckets of a weighted read that had no wind
	// speed at all, so their mean is not weighted.
	UnweightedBuckets int
}

// planPathHistoryRange is the first half of a bucketed history read: it finds
// the first and last raw sample when fitToData, picks the bucket width and
// works out the range the buckets cover. Both the linear and the circular
// reads start here.
func planPathHistoryRange(ctx context.Context, io pathHistoryIO, path, source string, start, stop time.Time, fitToData bool, widthFor func(time.Duration) (string, time.Duration)) (pathHistoryBuckets, error) {
	var out pathHistoryBuckets
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

	return out, nil
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
	for _, fn := range aggFns {
		switch fn {
		case "min", "mean", "max":
		default:
			return pathHistoryBuckets{}, fmt.Errorf("unknown aggregate %q (want min, mean or max)", fn)
		}
	}
	out, err := planPathHistoryRange(ctx, io, path, source, start, stop, fitToData, widthFor)
	if err != nil {
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
	// telemetrySeriesSIUnit is the SI unit the boat declares for a path.
	telemetrySeriesSIUnit = func(path string) string { return unitForAlarmPath(globalSignalKSnapshot, path) }

	// telemetrySeriesPathSources lists the $source values that logged a path in
	// a range; show_telemetry_chart's source check, reused by the endpoint.
	telemetrySeriesPathSources = queryInfluxPathSources

	// The overall deadline on one request.
	telemetrySeriesTimeout = 20 * time.Second
)

// telemetrySeriesPoint is one bucket. Min and Max are the bucket's linear
// extremes, which a scalar has (a gust is a wind speed's max) and an angle
// does not: an angle's Mean is a vector mean and it carries neither.
type telemetrySeriesPoint struct {
	T    string   `json:"t"`
	Mean float64  `json:"mean"`
	Min  *float64 `json:"min,omitempty"`
	Max  *float64 `json:"max,omitempty"`
}

type telemetrySeriesEntry struct {
	Path  string `json:"path"`
	Found bool   `json:"found"`
	Unit  string `json:"unit"` // the unit the points are in; "" is the stored unit
	// Angle is "bearing" or "relative" for an angle path, "" for a scalar. The
	// points of an angle are in degrees, drawn as points on a circle's scale.
	Angle string `json:"angle"`
	// Method names how an angle was averaged; empty for a scalar.
	Method string `json:"method,omitempty"`
	// UndefinedBuckets counts an angle's buckets with no direction: the samples
	// cancelled out or it was calm. They are left out of Points.
	UndefinedBuckets int                       `json:"undefined_buckets"`
	Points           []telemetrySeriesPoint    `json:"points"`
	Gaps             []assistantPathHistoryGap `json:"gaps"`
}

type telemetrySeriesResponse struct {
	From   string                 `json:"from"`
	To     string                 `json:"to"`
	Bucket string                 `json:"bucket"`
	Series []telemetrySeriesEntry `json:"series"`
}

// multiSourceError is a path with no source chosen that more than one device
// logged in the requested range. Never averaged: the operator picks one.
type multiSourceError struct {
	path    string
	sources []string
}

func (e *multiSourceError) Error() string {
	return fmt.Sprintf("%s was logged by more than one source (%s); choose one", e.path, strings.Join(e.sources, ", "))
}

// ambiguousSources reports the sorted $sources that logged path in [from, to)
// when there is more than one, nil otherwise.
func ambiguousSources(ctx context.Context, list func(context.Context, string, time.Time, time.Time) ([]string, error), path string, from, to time.Time) ([]string, error) {
	srcs, err := list(ctx, path, from, to)
	if err != nil {
		return nil, err
	}
	if len(srcs) < 2 {
		return nil, nil
	}
	srcs = append([]string(nil), srcs...)
	sort.Strings(srcs)
	return srcs, nil
}

// runBounded runs fn(0..n-1) with at most limit in flight. Once ctx is done no
// further call starts and the remaining slots report ctx's error. Errors land
// by index, so the caller picks the first by order, not by time.
func runBounded(ctx context.Context, n, limit int, fn func(i int) error) []error {
	errs := make([]error, n)
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			errs[i] = ctx.Err()
			continue
		}
		if err := ctx.Err(); err != nil {
			<-sem
			errs[i] = err
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			errs[i] = fn(i)
		}()
	}
	wg.Wait()
	return errs
}

func seriesBadRequest(c echo.Context, msg string) error {
	return c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
}

// GET /api/telemetry/series?path=a&path=b&source=s&source=&unit=A&unit=&from=RFC3339&to=RFC3339
//
// source and unit, when given, are aligned with path by index. A source
// restricts that path to one $source; a unit converts it from SI into that
// display unit (telemetry_units.go) and is echoed on the series.
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

	// source and unit are aligned with path by index; "" is no source / no
	// conversion.
	sources := c.QueryParams()["source"]
	units := c.QueryParams()["unit"]
	if len(sources) == 0 {
		sources = make([]string, len(paths))
	}
	if len(units) == 0 {
		units = make([]string, len(paths))
	}
	if len(sources) != len(paths) {
		return seriesBadRequest(c, fmt.Sprintf("source must be given once per path (%d paths, %d sources); use an empty value for none", len(paths), len(sources)))
	}
	if len(units) != len(paths) {
		return seriesBadRequest(c, fmt.Sprintf("unit must be given once per path (%d paths, %d units); use an empty value for none", len(paths), len(units)))
	}
	// si_unit, when given, is the unit the points are stored in, taken from the
	// card; without it the boat's live declaration is used.
	siUnits := c.QueryParams()["si_unit"]
	if len(siUnits) != 0 && len(siUnits) != len(paths) {
		return seriesBadRequest(c, fmt.Sprintf("si_unit must be given once per path (%d paths, %d si_units); use an empty value for none", len(paths), len(siUnits)))
	}
	converters := make([]func(float64) float64, len(paths))
	angles := make([]angleKind, len(paths))
	for i := range paths {
		sources[i] = strings.TrimSpace(sources[i])
		units[i] = strings.TrimSpace(units[i])
		if len(sources[i]) > gaugePathMaxLen {
			return seriesBadRequest(c, "source is too long")
		}
		// Interpolated into Flux, like the path.
		if containsFluxInterpolationSyntax(sources[i]) {
			return seriesBadRequest(c, "source must not contain '$' or '{'")
		}
		si := ""
		if len(siUnits) != 0 {
			si = strings.TrimSpace(siUnits[i])
		} else {
			si = telemetrySeriesSIUnit(paths[i])
		}
		if kind := angleKindOf(paths[i], si); kind != angleNone {
			// An angle is drawn in degrees, whatever was asked. A card saved
			// before angles had a unit of their own asks for "rad".
			label := angleUnitLabel(paths[i])
			if units[i] != "" && units[i] != "rad" && units[i] != label {
				return seriesBadRequest(c, fmt.Sprintf("%s is an angle and is shown in %s, not %q", paths[i], label, units[i]))
			}
			angles[i] = kind
			units[i] = label
			converters[i] = func(rad float64) float64 { return angleDegrees(kind, rad) }
			continue
		}
		conv, err := chartConvertFromSI(si, paths[i], units[i])
		if err != nil {
			return seriesBadRequest(c, err.Error())
		}
		converters[i] = conv
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
	methods := make([]string, len(paths))
	errs := make([]error, len(paths))
	var wg sync.WaitGroup
	for i, p := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if sources[i] == "" {
				srcs, err := ambiguousSources(ctx, telemetrySeriesPathSources, p, from, to)
				if err != nil {
					errs[i] = err
					return
				}
				if srcs != nil {
					errs[i] = &multiSourceError{path: p, sources: srcs}
					return
				}
			}
			if angles[i] != angleNone {
				// The speed series asked for in the same request, with its source.
				weightSource := ""
				for j, q := range paths {
					if q == weightPathFor(p) {
						weightSource = sources[j]
					}
				}
				weight, ambiguous, err := circularWeightPath(ctx, telemetrySeriesPathSources, p, weightSource, from, to)
				if err != nil {
					errs[i] = err
					return
				}
				results[i], errs[i] = queryCircularHistoryBuckets(ctx, telemetrySeriesIO, p, sources[i], weight, weightSource, from, to, false, telemetrySeriesBucketWidth)
				methods[i] = circularMethod(weight, ambiguous, results[i].Weighted, results[i].UnweightedBuckets)
				return
			}
			results[i], errs[i] = queryPathHistoryBuckets(ctx, telemetrySeriesIO, p, sources[i], from, to, []string{"min", "mean", "max"}, false, telemetrySeriesBucketWidth)
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
	var multi *multiSourceError
	for _, err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, context.DeadlineExceeded):
			// An inner per-query timeout, not only the overall one.
			return tookTooLong()
		case errors.Is(err, context.Canceled):
			// The client went away; nobody is left to read a response.
			return nil
		case errors.As(err, &multi):
			return c.JSON(http.StatusConflict, map[string]string{"error": multi.Error()})
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
			pt := telemetrySeriesPoint{T: m.T.UTC().Format(time.RFC3339), Mean: converters[i](*m.Mean)}
			if angles[i] == angleNone {
				if m.Min != nil {
					v := converters[i](*m.Min)
					pt.Min = &v
				}
				if m.Max != nil {
					v := converters[i](*m.Max)
					pt.Max = &v
				}
			}
			points = append(points, pt)
		}
		gaps := h.Gaps
		if gaps == nil || !h.Found {
			gaps = []assistantPathHistoryGap{}
		}
		if !h.Found {
			missing = append(missing, p)
		}
		entries = append(entries, telemetrySeriesEntry{
			Path: p, Found: h.Found, Unit: units[i], Angle: string(angles[i]), Method: methods[i],
			UndefinedBuckets: h.Undefined, Points: points, Gaps: gaps,
		})
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

// circularWeightPath decides what weights an angle's average. Wind direction is
// weighted by wind speed when one device logs it. If two devices log it,
// averaging them would chart a figure neither reported, so the speed is left
// out of the average and the method says why.
func circularWeightPath(ctx context.Context, list func(context.Context, string, time.Time, time.Time) ([]string, error), path, weightSource string, from, to time.Time) (weight string, ambiguous bool, err error) {
	weight = weightPathFor(path)
	if weight == "" {
		return "", false, nil
	}
	// The operator already chose a device for the speed: use it, never check
	// for others.
	if weightSource != "" {
		return weight, false, nil
	}
	srcs, err := ambiguousSources(ctx, list, weight, from, to)
	if err != nil {
		return "", false, err
	}
	if srcs != nil {
		return "", true, nil
	}
	return weight, false, nil
}

// circularMethod says how an angle was averaged, for the reader. weightPath is
// what circularWeightPath chose ("" for none) and weighted is whether the read
// found a speed to weight by.
func circularMethod(weightPath string, ambiguous, weighted bool, unweightedBuckets int) string {
	switch {
	case weighted && unweightedBuckets == 1:
		return "vector mean weighted by wind speed, except 1 bucket with no wind speed logged"
	case weighted && unweightedBuckets > 1:
		return fmt.Sprintf("vector mean weighted by wind speed, except %d buckets with no wind speed logged", unweightedBuckets)
	case weighted:
		return "vector mean weighted by wind speed"
	case ambiguous:
		return "vector mean, not weighted by wind speed (the speed is logged by more than one source)"
	case weightPath != "":
		return "vector mean, not weighted by wind speed (no wind speed is logged in this range)"
	}
	return "vector mean"
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
