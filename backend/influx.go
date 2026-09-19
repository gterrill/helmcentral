package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/influxdata/influxdb-client-go/v2/api"
)

type depthTrendPoint struct {
	Time   time.Time `json:"time"`
	DepthM float64   `json:"depth_m"`
}

// loadInfluxSettings reads the "influxdb" section of settings.yaml
// (url/org/bucket, only used if enabled: true) plus INFLUXDB_TOKEN from
// globalSecretsStore at point of use (ADR 0023 amendment, 2026-09-19) - the
// boot-time copy into the process environment (LoadIntoEnv) is retired, so
// there is no cache here to go stale when an operator rotates the token
// from the Secrets panel. ok is true only when enabled and all four values
// are non-empty.
//
// err is reserved for a real secrets-store read failure (a decrypt error, a
// SQLite error) - never for Influx simply being disabled, which is the
// overwhelmingly common case and returns ok=false, err=nil the same as
// before. A nil store is likewise not an error here: mirrors
// loadSignalKCredentials' own reasoning (signalk.go) for why a READ treats
// an unreachable store as "not configured" rather than failing. The token
// is read only once the settings say Influx is enabled, so a disabled
// Influx never touches the store at all.
func loadInfluxSettings(settingsPath string) (url, org, bucket, token string, ok bool, err error) {
	settings, readErr := readSettings(settingsPath)
	if readErr != nil {
		return "", "", "", "", false, nil
	}

	influxMap, isMap := settings["influxdb"].(map[string]any)
	if !isMap {
		return "", "", "", "", false, nil
	}

	enabled, _ := influxMap["enabled"].(bool)
	if !enabled {
		return "", "", "", "", false, nil
	}

	url = trimEnvValue(coerceString(influxMap["url"]))
	org = trimEnvValue(coerceString(influxMap["org"]))
	bucket = trimEnvValue(coerceString(influxMap["bucket"]))

	// Reached only once settings say Influx is enabled, so a nil store here
	// is a programming error rather than "Influx is off" - treat it as one.
	// See loadSignalKCredentials for the same reasoning.
	if globalSecretsStore == nil {
		return "", "", "", "", false, fmt.Errorf("secrets store unavailable, cannot read INFLUXDB_TOKEN")
	}
	storedToken, _, getErr := globalSecretsStore.Get("INFLUXDB_TOKEN")
	if getErr != nil {
		return "", "", "", "", false, fmt.Errorf("reading INFLUXDB_TOKEN: %w", getErr)
	}
	token = trimEnvValue(storedToken)

	ok = url != "" && org != "" && bucket != "" && token != ""
	return url, org, bucket, token, ok, nil
}

// influxTelemetryConfigured wraps loadInfluxSettings with the default
// settings path. A real secrets-store read error is logged loudly rather
// than folded silently into the same false returned for every other
// not-configured case - see loadInfluxSettings' own doc comment for why a
// store failure must not look identical to Influx simply being disabled.
// influxTelemetryConfigured stays a bare bool, not an (bool, error) pair:
// it and newInfluxClient below are the only two callers of
// loadInfluxSettings, and the false they already return for "not
// configured" already fans out, by design, to every Influx-backed query
// function and HTTP handler in this codebase as "fall back to the
// in-memory/sentinel path" - see e.g. telemetry_influx_cache.go's refresh
// doc comment. Re-plumbing a distinct error through that whole fan-out
// for one already-rare failure mode was judged not worth the blast radius;
// the log line is what keeps it from being silent.
func influxTelemetryConfigured() bool {
	_, _, _, _, ok, err := loadInfluxSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		log.Printf("influx: %v; telemetry unavailable this call", err)
		return false
	}
	return ok
}

// sharedInfluxClient holds the one long-lived influxdb2 client every query
// function shares, rebuilt only when the configured url/org/bucket/token
// change. Building a client opens a fresh http.Transport and TCP connection
// (influxdb2.NewClient), which used to happen on every single query -- four
// times per gust-ladder build, four more per solar-state build, per stream
// client, per second -- so this is the one long-lived Influx client the
// audit calls for. Callers must never Close() what newInfluxClient returns;
// it is shared, not owned by whichever caller happened to ask for it last.
type sharedInfluxClient struct {
	mu     sync.Mutex
	url    string
	org    string
	bucket string
	token  string
	client influxdb2.Client
}

var globalInfluxClient sharedInfluxClient

// clientFor returns the cached client if url/org/bucket/token are unchanged
// since it was built, otherwise closes the stale one (if any) and builds a
// fresh one. Close() on this client only closes idle connections and tears
// down write APIs this backend never uses (queries only), so swapping it out
// from under an in-flight query does not abort that query.
func (c *sharedInfluxClient) clientFor(url, org, bucket, token string) influxdb2.Client {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil && c.url == url && c.org == org && c.bucket == bucket && c.token == token {
		return c.client
	}

	if c.client != nil {
		c.client.Close()
	}

	c.client = influxdb2.NewClient(url, token)
	c.url, c.org, c.bucket, c.token = url, org, bucket, token
	return c.client
}

// reset closes and forgets the cached client, called when Influx settings
// report not-configured so a client built against a since-abandoned config
// does not sit open forever.
func (c *sharedInfluxClient) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	c.url, c.org, c.bucket, c.token = "", "", "", ""
}

func newInfluxClient() (influxdb2.Client, string, string, bool) {
	influxURL, org, bucket, token, ok, err := loadInfluxSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		// See influxTelemetryConfigured's doc comment: a real store read
		// failure is logged loudly rather than silently treated as "not
		// configured", even though the caller-facing result is the same.
		log.Printf("influx: %v; telemetry unavailable this call", err)
		globalInfluxClient.reset()
		return nil, org, bucket, false
	}
	if !ok {
		globalInfluxClient.reset()
		return nil, org, bucket, false
	}

	return globalInfluxClient.clientFor(influxURL, org, bucket, token), org, bucket, true
}

// fluxInterpolationChars are the characters that must never reach a Flux
// string literal verbatim. Flux has its OWN string-interpolation syntax,
// "${...}", which InfluxDB evaluates when it parses the query text -- after
// %q has already escaped the value for Go. %q's escaping (of '"' and '\\')
// stops a value from breaking out of the string literal into a new pipeline
// stage; it does nothing to stop InfluxDB from treating "${r._measurement}"
// inside that literal as an embedded expression rather than literal text.
// Rejecting outright, instead of trying to escape '$'/'{' into something
// inert, matches telemetryHistoryWindows' allowlist a few lines up the call
// stack (telemetry_history_api.go) and AGENTS.md's fail-fast policy: a
// caller that genuinely needs a literal '$' in a path/measurement/field name
// gets a clear error, not a silently mis-scoped query.
const fluxInterpolationChars = "${"

// containsFluxInterpolationSyntax is also used by telemetry_history_api.go
// to reject the path query parameter at the HTTP boundary, one layer above
// where fluxStringLiteral enforces the same rule at query construction. Both
// layers matter: path reaches queryInfluxPathTrend/queryInfluxPathRange from
// the HTTP API AND from Mate's estimate_passage tool (assistant_tools.go),
// so a check only in the HTTP handler would miss the second caller.
func containsFluxInterpolationSyntax(s string) bool {
	return strings.ContainsAny(s, fluxInterpolationChars)
}

// fluxStringLiteral renders s as a Flux double-quoted string literal,
// refusing to build one at all if it contains Flux interpolation syntax
// (see fluxInterpolationChars above). Every %q-built Flux query in this file
// goes through here instead of calling %q directly -- not just
// path/measurement in the two functions reachable from outside this file,
// but bucket/measurement/field everywhere, so a future call site that starts
// threading request data through one of those still-fixed-today parameters
// inherits the same protection instead of reopening this bug in a fourth
// place (E-4).
func fluxStringLiteral(s string) (string, error) {
	if containsFluxInterpolationSyntax(s) {
		return "", fmt.Errorf("value must not contain '$' or '{': %q", s)
	}
	return fmt.Sprintf("%q", s), nil
}

// queryInfluxMaxWindGustKtsFor returns the max wind gust (in knots) for each
// requested window, reusing newInfluxClient's shared client across all
// queries in windows rather than one client per window. Each window still
// gets its own -1 sentinel on error/no data, mirroring the single-window
// contract this replaces.
func queryInfluxMaxWindGustKtsFor(windows []string) map[string]float64 {
	results := make(map[string]float64, len(windows))

	client, org, bucket, ok := newInfluxClient()
	if !ok {
		for _, window := range windows {
			results[window] = -1
		}
		return results
	}

	measurement := trimEnvValue(getEnv("INFLUX_WIND_MEASUREMENT", "environment.wind.speedApparent"))
	field := trimEnvValue(getEnv("INFLUX_WIND_FIELD", "value"))
	queryAPI := client.QueryAPI(org)

	for _, window := range windows {
		results[window] = queryInfluxMaxWindGustKtsForWindow(queryAPI, bucket, measurement, field, window)
	}

	return results
}

func queryInfluxMaxWindGustKtsForWindow(queryAPI api.QueryAPI, bucket, measurement, field, window string) float64 {
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return -1
	}
	measurementLiteral, err := fluxStringLiteral(measurement)
	if err != nil {
		return -1
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return -1
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> max(column: "_value") |> keep(columns: ["_value"])`,
		bucketLiteral, window, measurementLiteral, fieldLiteral,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := queryAPI.Query(ctx, flux)
	if err != nil {
		return -1
	}
	defer result.Close()

	maxMS := -1.0
	for result.Next() {
		if v, ok := result.Record().Value().(float64); ok {
			maxMS = v
		}
	}

	if result.Err() != nil || maxMS < 0 {
		return -1
	}
	return math.Round((maxMS*metersPerSecondToKnots)*10) / 10
}

// queryInfluxPathTrend reads any SignalK path's history. The
// signalk-to-influxdb-v2 plugin writes the path as the measurement name, so
// this is the depth query with the measurement as a parameter (ADR 0051).
//
// Unlike queryInfluxDepthTrend it returns an error rather than nil, because
// its caller has to tell an empty series apart from a failed query.
func queryInfluxPathTrend(path, window string) ([]telemetryPoint, error) {
	// Validated before touching the client/config at all: path is the one
	// value here that can come straight from an HTTP request or from Mate's
	// estimate_passage tool, so a bad value is rejected on its own terms
	// rather than as a side effect of whatever newInfluxClient happens to
	// report (E-4).
	pathLiteral, err := fluxStringLiteral(path)
	if err != nil {
		return nil, fmt.Errorf("path: %w", err)
	}

	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket: %w", err)
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return nil, fmt.Errorf("field: %w", err)
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> aggregateWindow(every: %s, fn: mean, createEmpty: false) |> keep(columns: ["_time", "_value"])`,
		bucketLiteral, window, pathLiteral, fieldLiteral, influxTrendResolution(window),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var points []telemetryPoint
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(float64)
		if !ok {
			continue
		}
		points = append(points, telemetryPoint{Timestamp: rec.Time(), Value: v})
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return points, nil
}

// queryInfluxPathRange reads any SignalK path's history over an explicit
// [start, stop] range rather than a relative "last N" window, aggregated to
// every-sized buckets. Modelled on queryInfluxSolarEnergyKWhRange's RFC3339
// range query, generalised to an arbitrary measurement the way
// queryInfluxPathTrend generalised queryInfluxDepthTrend.
//
// The overnight model (electrical_overnight.go) needs an explicit
// sunset-to-sunrise range per night, which telemetryHistoryHandler's
// relative "-1h"/"-24h" style window can't express.
//
// every is interpolated into the Flux query unquoted, same as the other
// aggregateWindow call sites in this file - safe here because every only
// ever comes from a fixed duration literal chosen by this backend
// (overnightQueryEvery), never from an HTTP request. If a future caller
// needs to take every from a request, it needs the same allowlist
// telemetryHistoryWindows gives window.
//
// Like queryInfluxPathTrend, this returns an error rather than nil so the
// caller can tell "no discharge happened" apart from "couldn't ask".
func queryInfluxPathRange(path string, start, stop time.Time, every string) ([]telemetryPoint, error) {
	// Same up-front rejection as queryInfluxPathTrend, and for the same
	// reason: this is the call estimate_passage (assistant_tools.go) makes
	// directly, with no HTTP handler in between to have already checked it.
	pathLiteral, err := fluxStringLiteral(path)
	if err != nil {
		return nil, fmt.Errorf("path: %w", err)
	}

	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket: %w", err)
	}
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return nil, fmt.Errorf("field: %w", err)
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> aggregateWindow(every: %s, fn: mean, createEmpty: false) |> keep(columns: ["_time", "_value"])`,
		bucketLiteral, start.UTC().Format(time.RFC3339), stop.UTC().Format(time.RFC3339), pathLiteral, fieldLiteral, every,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var points []telemetryPoint
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(float64)
		if !ok {
			continue
		}
		points = append(points, telemetryPoint{Timestamp: rec.Time(), Value: v})
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return points, nil
}

// influxTrendResolution keeps a long window from returning thousands of points
// for a sparkline a few hundred pixels wide.
func influxTrendResolution(window string) string {
	switch window {
	case "1h":
		return "1m"
	case "3h", "6h":
		return "5m"
	case "24h":
		return "15m"
	default:
		return "1h"
	}
}

func queryInfluxDepthTrend(window string) []depthTrendPoint {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil
	}

	measurement := trimEnvValue(getEnv("INFLUX_DEPTH_MEASUREMENT", "environment.depth.belowTransducer"))
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return nil
	}
	measurementLiteral, err := fluxStringLiteral(measurement)
	if err != nil {
		return nil
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return nil
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false) |> keep(columns: ["_time", "_value"])`,
		bucketLiteral, window, measurementLiteral, fieldLiteral,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil
	}
	defer result.Close()

	var points []depthTrendPoint
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(float64)
		if !ok || v < 0 {
			continue
		}
		points = append(points, depthTrendPoint{
			Time:   rec.Time(),
			DepthM: math.Round(v*100) / 100,
		})
	}

	if result.Err() != nil {
		return nil
	}
	return points
}

func queryInfluxSolarTodayKWh(now time.Time) float64 {
	start := now.UTC().Truncate(24 * time.Hour)
	return queryInfluxSolarEnergyKWhRange(start, now.UTC())
}

func queryInfluxSolarYesterdayKWh(now time.Time) float64 {
	stop := now.UTC().Truncate(24 * time.Hour)
	start := stop.Add(-24 * time.Hour)
	return queryInfluxSolarEnergyKWhRange(start, stop)
}

func queryInfluxSolarPeakTodayW(now time.Time) float64 {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return -1
	}

	measurement := trimEnvValue(getEnv("INFLUX_SOLAR_MEASUREMENT", "electrical.venus.totalPanelPower"))
	field := trimEnvValue(getEnv("INFLUX_SOLAR_FIELD", "value"))
	start := now.UTC().Truncate(24 * time.Hour)

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return -1
	}
	measurementLiteral, err := fluxStringLiteral(measurement)
	if err != nil {
		return -1
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return -1
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> max(column: "_value") |> keep(columns: ["_value"])`,
		bucketLiteral, start.Format(time.RFC3339), now.UTC().Format(time.RFC3339), measurementLiteral, fieldLiteral,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return -1
	}
	defer result.Close()

	peakW := -1.0
	for result.Next() {
		if v, ok := result.Record().Value().(float64); ok {
			peakW = v
		}
	}

	if result.Err() != nil || peakW < 0 {
		return -1
	}

	return math.Round(peakW*10) / 10
}

func queryInfluxSolarEnergyKWhRange(start time.Time, stop time.Time) float64 {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return -1
	}

	measurement := trimEnvValue(getEnv("INFLUX_SOLAR_MEASUREMENT", "electrical.venus.totalPanelPower"))
	field := trimEnvValue(getEnv("INFLUX_SOLAR_FIELD", "value"))

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return -1
	}
	measurementLiteral, err := fluxStringLiteral(measurement)
	if err != nil {
		return -1
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return -1
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> integral(unit: 1h) |> group() |> sum(column: "_value") |> keep(columns: ["_value"])`,
		bucketLiteral, start.Format(time.RFC3339), stop.Format(time.RFC3339), measurementLiteral, fieldLiteral,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return -1
	}
	defer result.Close()

	wh := -1.0
	for result.Next() {
		if v, ok := result.Record().Value().(float64); ok {
			wh = v
		}
	}

	if result.Err() != nil || wh < 0 {
		return -1
	}

	return math.Round((wh/1000)*1000) / 1000
}

func queryInfluxSolarTrend24h(now time.Time) []solarTrendPoint {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil
	}

	measurement := trimEnvValue(getEnv("INFLUX_SOLAR_MEASUREMENT", "electrical.venus.totalPanelPower"))
	field := trimEnvValue(getEnv("INFLUX_SOLAR_FIELD", "value"))
	start := now.UTC().Add(-24 * time.Hour)

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return nil
	}
	measurementLiteral, err := fluxStringLiteral(measurement)
	if err != nil {
		return nil
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return nil
	}

	flux := fmt.Sprintf(
		`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => r._measurement == %s and r._field == %s) |> aggregateWindow(every: 15m, fn: mean, createEmpty: false) |> keep(columns: ["_time", "_value"])`,
		bucketLiteral, start.Format(time.RFC3339), now.UTC().Format(time.RFC3339), measurementLiteral, fieldLiteral,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil
	}
	defer result.Close()

	points := make([]solarTrendPoint, 0)
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(float64)
		if !ok || v < 0 {
			continue
		}
		points = append(points, solarTrendPoint{
			Time:   rec.Time(),
			TotalW: math.Round(v*10) / 10,
		})
	}

	if result.Err() != nil {
		return nil
	}

	return points
}

// tideTurnThresholdM is the minimum depth change required to confirm a
// reversal in tide direction.  0.3m filters out typical sonar noise from
// boat swing at anchor (≤0.2m) while still detecting real tidal movements
// (tidal ranges in these waters are 1m+).
const tideTurnThresholdM = 0.3

type tideTurningPoint struct {
	Time   time.Time
	DepthM float64
	IsHigh bool
}

// findLastTideTurningPoint scans chronologically-ordered depth points for the
// most recent local extremum (a reversal from rising to falling, or vice
// versa), using tideTurnThresholdM to ignore small fluctuations that aren't a
// genuine change in tide direction. Returns false if no reversal is found.
func findLastTideTurningPoint(points []depthTrendPoint) (tideTurningPoint, bool) {
	if len(points) < 3 {
		return tideTurningPoint{}, false
	}

	extremeIdx := 0
	direction := 0 // 0 = unknown, 1 = rising, -1 = falling
	var lastTurn tideTurningPoint
	found := false

	for i := 1; i < len(points); i++ {
		switch direction {
		case 1:
			if points[i].DepthM > points[extremeIdx].DepthM {
				extremeIdx = i
			} else if points[extremeIdx].DepthM-points[i].DepthM >= tideTurnThresholdM {
				lastTurn = tideTurningPoint{Time: points[extremeIdx].Time, DepthM: points[extremeIdx].DepthM, IsHigh: true}
				found = true
				direction = -1
				extremeIdx = i
			}
		case -1:
			if points[i].DepthM < points[extremeIdx].DepthM {
				extremeIdx = i
			} else if points[i].DepthM-points[extremeIdx].DepthM >= tideTurnThresholdM {
				lastTurn = tideTurningPoint{Time: points[extremeIdx].Time, DepthM: points[extremeIdx].DepthM, IsHigh: false}
				found = true
				direction = 1
				extremeIdx = i
			}
		default:
			if diff := points[i].DepthM - points[extremeIdx].DepthM; diff >= tideTurnThresholdM {
				direction = 1
				extremeIdx = i
			} else if diff <= -tideTurnThresholdM {
				direction = -1
				extremeIdx = i
			}
		}
	}

	return lastTurn, found
}

func trimEnvValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"`)
	return strings.TrimSpace(trimmed)
}

func isRecentTimestamp(value string, maxAge time.Duration) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return time.Since(parsed.UTC()) <= maxAge
}
