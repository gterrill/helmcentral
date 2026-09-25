package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"regexp"
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

// queryInfluxPathStatRange reads one aggregate statistic - mean, min or max -
// of a SignalK path's history over an explicit [start, stop) range,
// aggregated to every-sized buckets, optionally restricted to one $source tag
// value. It generalises queryInfluxPathRange (which always mean-aggregates,
// with no source filter) two ways for get_path_history's per-bucket
// min/mean/max (Mate diagnostics, ADR 0131): aggFn picks which InfluxDB
// aggregate function runs per bucket, and source, when given, narrows to one
// $source rather than mixing every source that has ever published this path.
//
// aggFn is interpolated into the Flux query unquoted, same as every already
// is in queryInfluxPathRange - safe here for the same reason: it is never
// model-supplied text, only ever one of the three fixed literals ("min",
// "mean", "max") assistant_diagnostics.go calls this with.
//
// ctx is the caller's own context (executeGetPathHistory's tool ctx), not
// context.Background() - the 8s cap below is derived FROM it, not a
// standalone timeout that outlives the tool call's own cancellation. Before
// this (a code-review finding, 2026-09-25), get_path_history's three stat
// queries (min/mean/max) each built its own context.Background() timeout,
// so cancelling the tool call - or the model's own context deadline - never
// reached InfluxDB at all, and running them sequentially meant a genuinely
// slow bucket could cost up to three separate 8s waits instead of one.
// executeGetPathHistory now fires all of its Influx queries concurrently
// off this same ctx, so the worst case is one shared 8s timeout and a
// cancelled tool ctx stops every one of them immediately.
func queryInfluxPathStatRange(ctx context.Context, path, source string, start, stop time.Time, every, aggFn string) ([]telemetryPoint, error) {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}

	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxPathStatFlux(bucket, field, path, source, start, stop, every, aggFn)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
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

// buildInfluxPathStatFlux builds queryInfluxPathStatRange's Flux query text,
// pulled out as its own pure function so its string-escaping and clause
// construction can be unit-tested without a live InfluxDB connection - the
// same reasoning fluxStringLiteral's own tests already apply one layer down.
// Every value that can carry attacker- or model-chosen text (path, source,
// bucket, field) goes through fluxStringLiteral; every() and aggFn never do,
// since callers only ever pass one of a fixed set of literals for both (see
// queryInfluxPathStatRange's own doc comment).
func buildInfluxPathStatFlux(bucket, field, path, source string, start, stop time.Time, every, aggFn string) (string, error) {
	pathLiteral, err := fluxStringLiteral(path)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}

	filter := fmt.Sprintf("r._measurement == %s and r._field == %s", pathLiteral, fieldLiteral)
	if source != "" {
		sourceLiteral, err := fluxStringLiteral(source)
		if err != nil {
			return "", fmt.Errorf("source: %w", err)
		}
		filter += fmt.Sprintf(" and r.source == %s", sourceLiteral)
	}

	// timeSrc: "_start" - unlike every other aggregateWindow call in this
	// file, this one feeds computePathHistoryGaps (assistant_diagnostics.go),
	// which checks presence by truncating each point's own timestamp down to
	// a bucket boundary and comparing it against the SAME boundary the gap
	// loop steps through starting at range.start. Flux's own default
	// (timeSrc: "_stop") labels every bucket with its STOP time instead, one
	// whole bucket width later than range.start's own boundary - so with the
	// default, the gap loop's very first checked boundary would never have a
	// matching point (the real first bucket lands one width later), reporting
	// a false one-bucket gap at the start of every range regardless of actual
	// data completeness. Requesting "_start" instead makes each bucket's
	// reported time the boundary computePathHistoryGaps already assumes.
	return fmt.Sprintf(
		`from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => %s) |> aggregateWindow(every: %s, fn: %s, createEmpty: false, timeSrc: "_start") |> keep(columns: ["_time", "_value"])`,
		bucketLiteral, start.UTC().Format(time.RFC3339), stop.UTC().Format(time.RFC3339), filter, every, aggFn,
	), nil
}

// queryInfluxPathFirstLast reads the ACTUAL first and last recorded sample
// times for a path over [start, stop) - not a bucket boundary. get_path_history
// (assistant_diagnostics.go) previously reported first_seen/last_seen from
// the min/mean/max aggregateWindow series' own first/last bucket, which -
// because those buckets are timeSrc: "_start" labelled (buildInfluxPathStatFlux's
// own doc comment) - names the START of whichever bucket happened to hold
// the real first/last point, not the point itself. On a 90-day range bucketed
// to 2-day buckets (assistantPathHistoryBucketWidth), that is up to two days
// off from the moment a source actually stopped - exactly the question this
// tool exists to answer precisely (a code-review finding, 2026-09-25). This
// query answers it directly instead of inferring it from the bucketed series.
//
// ctx is the tool's own ctx (see queryInfluxPathStatRange's doc comment) -
// executeGetPathHistory fires this concurrently alongside the min/mean/max
// queries, all sharing one derived timeout.
func queryInfluxPathFirstLast(ctx context.Context, path, source string, start, stop time.Time) (first, last time.Time, found bool, err error) {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return time.Time{}, time.Time{}, false, fmt.Errorf("influxdb is not configured")
	}

	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxFirstLastFlux(bucket, field, path, source, start, stop)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}

	qctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(qctx, flux)
	if err != nil {
		return time.Time{}, time.Time{}, false, err
	}
	defer result.Close()

	var times []time.Time
	for result.Next() {
		times = append(times, result.Record().Time())
	}
	if result.Err() != nil {
		return time.Time{}, time.Time{}, false, result.Err()
	}
	if len(times) == 0 {
		return time.Time{}, time.Time{}, false, nil
	}

	first, last = times[0], times[0]
	for _, t := range times[1:] {
		if t.Before(first) {
			first = t
		}
		if t.After(last) {
			last = t
		}
	}
	return first, last, true, nil
}

// buildInfluxFirstLastFlux builds queryInfluxPathFirstLast's Flux query text,
// pulled out as its own pure function for the same unit-testability reason
// buildInfluxPathStatFlux and buildInfluxLastRecordedFlux are. Every value
// that can carry model-chosen text goes through fluxStringLiteral, same as
// every other Flux builder in this file.
//
// group() (bare - drops every existing group key) before sort()/first()/
// last() combines whatever distinct raw series matched the filter (e.g.
// several $source values, when source is empty) into one table first, the
// same reasoning buildInfluxLastRecordedFlux's own group()-then-sort()-
// then-last() dance uses: first()/last() on a per-series table would only
// ever see one arbitrary series' own endpoint, not the path's genuine
// overall first/last point across every source that has ever written it.
func buildInfluxFirstLastFlux(bucket, field, path, source string, start, stop time.Time) (string, error) {
	pathLiteral, err := fluxStringLiteral(path)
	if err != nil {
		return "", fmt.Errorf("path: %w", err)
	}
	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}

	filter := fmt.Sprintf("r._measurement == %s and r._field == %s", pathLiteral, fieldLiteral)
	if source != "" {
		sourceLiteral, err := fluxStringLiteral(source)
		if err != nil {
			return "", fmt.Errorf("source: %w", err)
		}
		filter += fmt.Sprintf(" and r.source == %s", sourceLiteral)
	}

	return fmt.Sprintf(
		"data = from(bucket: %s) |> range(start: time(v: %q), stop: time(v: %q)) |> filter(fn: (r) => %s) |> group() |> sort(columns: [\"_time\"])\n"+
			"first = data |> first() |> keep(columns: [\"_time\", \"_value\"])\n"+
			"last = data |> last() |> keep(columns: [\"_time\", \"_value\"])\n"+
			"union(tables: [first, last])",
		bucketLiteral, start.UTC().Format(time.RFC3339), stop.UTC().Format(time.RFC3339), filter,
	), nil
}

// influxLastRecordedRow is one measurement+source pair's most recent
// recorded point, as get_last_recorded (Mate diagnostics, ADR 0131) reports
// it. This answers "when did X stop" for a path that queryInfluxPathTrend/
// queryInfluxPathRange cannot: both need the path named up front and a
// window to look in, and return nothing once no point falls in that window -
// indistinguishable from "never existed" without already knowing when to
// stop looking.
//
// Value is `any`, not float64: signalk-to-influxdb2 (the upstream plugin
// that writes this bucket - src/influx.ts, tkurki/signalk-to-influxdb2,
// checked live 2026-09-25) always writes to a field literally named
// "value" regardless of the SignalK value's own type - point.floatField
// ('value', v) for a number, point.stringField('value', v) for a string,
// point.booleanField('value', v) for a boolean (and stringField with
// JSON.stringify for anything else, e.g. an object). So the _field == "value"
// filter this file already uses is correct for every type; the bug was
// entirely on the Go side, only ever accepting a float64 record value and
// silently dropping the row otherwise (see influxRecordValueOK) - which
// made every string path (a mode/state enum), boolean path (an alarm flag)
// or whole-number path decoded as an integer type look permanently
// unrecorded to get_last_recorded, a data/source problem this tool exists
// specifically to catch (AGENTS.md's fallback policy).
type influxLastRecordedRow struct {
	Path   string
	Source string
	Time   time.Time
	Value  any
}

// influxRecordValueOK reports whether v (an Influx query record's decoded
// field value) is one of the types get_last_recorded can report as JSON,
// returning it unchanged when it is. float64 covers every SignalK number
// (signalk-to-influxdb2 always writes numbers via floatField - see
// influxLastRecordedRow's own doc comment); string and bool cover SignalK's
// other two JSON-native value types; int64/uint64 are accepted defensively
// for any other integer-field writer, even though nothing in this fleet's
// own write path produces one today. Anything else (nil - a genuinely
// missing value - or an exotic decoded type) is not something get_last_recorded
// can represent honestly, so it is rejected rather than coerced.
func influxRecordValueOK(v any) (any, bool) {
	switch v.(type) {
	case float64, string, bool, int64, uint64:
		return v, true
	default:
		return nil, false
	}
}

// queryInfluxLastRecorded finds, for every measurement (SignalK path) and
// $source combination matching pathPrefix and/or source within the last
// lookbackDays, that combination's most recent recorded point.
//
// At least one of pathPrefix/source must be non-empty - the caller
// (assistant_diagnostics.go) enforces that before calling this, since neither
// filter bounds the query's own cost the way a single named path does for
// queryInfluxPathRange: an empty prefix would scan the bucket's entire
// measurement set.
func queryInfluxLastRecorded(pathPrefix, source string, lookbackDays int) ([]influxLastRecordedRow, error) {
	if pathPrefix == "" && source == "" {
		return nil, fmt.Errorf("path_prefix or source is required")
	}

	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil, fmt.Errorf("influxdb is not configured")
	}

	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))
	flux, err := buildInfluxLastRecordedFlux(bucket, field, pathPrefix, source, lookbackDays)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return nil, err
	}
	defer result.Close()

	var rows []influxLastRecordedRow
	for result.Next() {
		rec := result.Record()
		v, ok := influxRecordValueOK(rec.Value())
		if !ok {
			continue
		}
		source, _ := rec.ValueByKey("source").(string)
		rows = append(rows, influxLastRecordedRow{
			Path:   rec.Measurement(),
			Source: source,
			Time:   rec.Time(),
			Value:  v,
		})
	}
	if result.Err() != nil {
		return nil, result.Err()
	}
	return rows, nil
}

// assistantDiagnosticsIdentifierPattern is the character allowlist
// get_last_recorded's path_prefix/source must match before being turned
// into an anchored Flux regex literal (^prefix) by buildInfluxLastRecordedFlux.
// A Flux regex literal is delimited by '/', so a value containing '/' would
// otherwise break out of it - rejecting outright, rather than trying to
// escape '/' into something inert, matches this file's existing
// fluxStringLiteral rule for Flux's OWN '${...}' interpolation syntax
// (AGENTS.md's fallback policy): a caller that genuinely needs a value
// outside this charset gets a clear error, not a silently mis-scoped query.
//
// Every $source label seen on this fleet (YachtDevices.6,
// venus.com.victronenergy.gps, Vesper_Cortex, WLN10.GP) and every SignalK
// path prefix fits this charset (letters, digits, and the handful of
// separators - dot, underscore, colon, hyphen - that actually turn up in a
// path or a source label); widen only once a real one does not.
var assistantDiagnosticsIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// validateAssistantDiagnosticsIdentifier checks value (a get_last_recorded
// path_prefix or source argument) against
// assistantDiagnosticsIdentifierPattern, naming which argument failed. An
// empty value always passes - both arguments are optional; the "at least one
// of the two" rule is enforced separately by buildInfluxLastRecordedFlux and
// executeGetLastRecorded.
func validateAssistantDiagnosticsIdentifier(label, value string) error {
	if value == "" {
		return nil
	}
	if !assistantDiagnosticsIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s %q contains characters not valid in a SignalK path or source label", label, value)
	}
	return nil
}

// buildInfluxLastRecordedFlux builds queryInfluxLastRecorded's Flux query
// text, pulled out as its own pure function for the same reason
// buildInfluxPathStatFlux is: unit-testable string construction and escaping
// with no live InfluxDB connection required. At least one of pathPrefix/
// source must be non-empty, and both must pass
// assistantDiagnosticsIdentifierPattern before being turned into an anchored
// regex literal (=~ /^.../), since both can carry model-chosen text.
//
// A regex, not strings.hasPrefix: hasPrefix cannot be pushed down to
// InfluxDB's storage engine, so a broad filter like source "YachtDevices"
// over a 30-180 day lookback would materialise and scan every point in the
// bucket in Flux's own execution engine, risking this query's own timeout
// (a code-review finding, 2026-09-25). An anchored regex comparison against
// a tag (=~ /^prefix/) DOES push down. regexp.QuoteMeta on the
// already-allowlisted value is still required even though the input is
// already restricted to a safe charset: '.' is itself a regex metacharacter
// (matches any character), so an unescaped "tanks.fuel" would match
// "tanksXfuel" too, not just a genuine dotted prefix.
func buildInfluxLastRecordedFlux(bucket, field, pathPrefix, source string, lookbackDays int) (string, error) {
	if pathPrefix == "" && source == "" {
		return "", fmt.Errorf("path_prefix or source is required")
	}
	if err := validateAssistantDiagnosticsIdentifier("path_prefix", pathPrefix); err != nil {
		return "", err
	}
	if err := validateAssistantDiagnosticsIdentifier("source", source); err != nil {
		return "", err
	}

	bucketLiteral, err := fluxStringLiteral(bucket)
	if err != nil {
		return "", fmt.Errorf("bucket: %w", err)
	}
	fieldLiteral, err := fluxStringLiteral(field)
	if err != nil {
		return "", fmt.Errorf("field: %w", err)
	}

	filters := []string{fmt.Sprintf("r._field == %s", fieldLiteral)}
	if pathPrefix != "" {
		filters = append(filters, fmt.Sprintf("r._measurement =~ /^%s/", regexp.QuoteMeta(pathPrefix)))
	}
	if source != "" {
		filters = append(filters, fmt.Sprintf("(exists r.source and r.source =~ /^%s/)", regexp.QuoteMeta(source)))
	}

	// last() runs twice, deliberately. The first, right after filter(), runs
	// once per raw series (each series being one full underlying tag set,
	// e.g. distinguished by a context tag this query does not group on) and
	// pushes down to storage - InfluxDB can answer "the last point of each
	// series" cheaply. group(columns: ["_measurement", "source"]) then
	// regroups those already-reduced rows by measurement+source alone,
	// combining rows from what may be several distinct raw series into one
	// new table - in whatever order Flux happens to concatenate them, not
	// necessarily chronological. Taking last() there without sorting first
	// could return whichever series Flux processed last, not the
	// actually-newest point (a code-review finding, 2026-09-25); sort(columns:
	// ["_time"]) orders that combined table chronologically first, so the
	// second last() is the genuine most-recent point.
	return fmt.Sprintf(
		"from(bucket: %s)\n"+
			"  |> range(start: -%dd)\n"+
			"  |> filter(fn: (r) => %s)\n"+
			"  |> last()\n"+
			"  |> group(columns: [\"_measurement\", \"source\"])\n"+
			"  |> sort(columns: [\"_time\"])\n"+
			"  |> last()\n"+
			"  |> keep(columns: [\"_measurement\", \"source\", \"_time\", \"_value\"])",
		bucketLiteral, lookbackDays, strings.Join(filters, " and "),
	), nil
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
