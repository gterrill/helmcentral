package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

type depthTrendPoint struct {
	Time   time.Time `json:"time"`
	DepthM float64   `json:"depth_m"`
}

type nearbyVesselEncounterSummary struct {
	SeenCount  int
	LastSeenAt time.Time
}

func newInfluxClient() (influxdb2.Client, string, string, bool) {
	influxURL := trimEnvValue(getEnv("INFLUXDB_URL", ""))
	org := trimEnvValue(getEnv("INFLUXDB_ORG", ""))
	bucket := trimEnvValue(getEnv("INFLUXDB_BUCKET", ""))
	token := trimEnvValue(getEnv("INFLUXDB_TOKEN", ""))

	if influxURL == "" || org == "" || bucket == "" || token == "" {
		return nil, org, bucket, false
	}

	client := influxdb2.NewClient(influxURL, token)
	return client, org, bucket, true
}

func queryInfluxMaxWindGustKts(window string) float64 {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return -1
	}
	defer client.Close()

	measurement := trimEnvValue(getEnv("INFLUX_WIND_MEASUREMENT", "environment.wind.speedApparent"))
	field := trimEnvValue(getEnv("INFLUX_WIND_FIELD", "value"))

	flux := fmt.Sprintf(
		`from(bucket: %q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and r._field == %q) |> max(column: "_value") |> keep(columns: ["_value"])`,
		bucket, window, measurement, field,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
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

func queryInfluxDepthTrend(window string) []depthTrendPoint {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil
	}
	defer client.Close()

	measurement := trimEnvValue(getEnv("INFLUX_DEPTH_MEASUREMENT", "environment.depth.belowTransducer"))
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))

	flux := fmt.Sprintf(
		`from(bucket: %q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and r._field == %q) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false) |> keep(columns: ["_time", "_value"])`,
		bucket, window, measurement, field,
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

func queryInfluxDepthTrendSince(start time.Time) []depthTrendPoint {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return nil
	}
	defer client.Close()

	measurement := trimEnvValue(getEnv("INFLUX_DEPTH_MEASUREMENT", "environment.depth.belowTransducer"))
	field := trimEnvValue(getEnv("INFLUX_DEPTH_FIELD", "value"))

	flux := fmt.Sprintf(
		`from(bucket: %q) |> range(start: %s) |> filter(fn: (r) => r._measurement == %q and r._field == %q) |> aggregateWindow(every: 5m, fn: mean, createEmpty: false) |> keep(columns: ["_time", "_value"])`,
		bucket, start.UTC().Format(time.RFC3339), measurement, field,
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

// queryInfluxLastStationaryStart finds the timestamp when the vessel most recently
// transitioned into a stationary state (moored or anchored) within the last 48h.
// Returns zero time if not found.
func queryInfluxLastStationaryStart() time.Time {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return time.Time{}
	}
	defer client.Close()

	stateMeasurement := trimEnvValue(getEnv("INFLUX_STATE_MEASUREMENT", "navigation.state"))

	// Fetch all state records in the last 48h, then find the last transition
	// into moored/anchored by walking backwards from the most recent record.
	flux := fmt.Sprintf(
		`from(bucket: %q) |> range(start: -48h) |> filter(fn: (r) => r._measurement == %q and r._field == "value") |> keep(columns: ["_time", "_value"]) |> sort(columns: ["_time"], desc: false)`,
		bucket, stateMeasurement,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return time.Time{}
	}
	defer result.Close()

	type stateRecord struct {
		t time.Time
		v string
	}
	var records []stateRecord
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(string)
		if !ok {
			continue
		}
		records = append(records, stateRecord{t: rec.Time(), v: v})
	}
	if result.Err() != nil || len(records) == 0 {
		return time.Time{}
	}

	// Walk backwards: find where the current stationary run began.
	// The last record must be stationary, then we look for the earliest
	// contiguous stationary entry.
	last := records[len(records)-1]
	if last.v != "moored" && last.v != "anchored" {
		return time.Time{}
	}

	startTime := last.t
	for i := len(records) - 2; i >= 0; i-- {
		r := records[i]
		if r.v == "moored" || r.v == "anchored" {
			startTime = r.t
		} else {
			break
		}
	}
	return startTime
}

// queryInfluxLastMotoringToStationaryTransition finds the most recent time the
// vessel transitioned from a motoring state into a stationary state (moored/anchored)
// within the last 48h.
func queryInfluxLastMotoringToStationaryTransition() time.Time {
	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return time.Time{}
	}
	defer client.Close()

	stateMeasurement := trimEnvValue(getEnv("INFLUX_STATE_MEASUREMENT", "navigation.state"))

	flux := fmt.Sprintf(
		`from(bucket: %q) |> range(start: -48h) |> filter(fn: (r) => r._measurement == %q and r._field == "value") |> keep(columns: ["_time", "_value"]) |> sort(columns: ["_time"], desc: false)`,
		bucket, stateMeasurement,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	result, err := client.QueryAPI(org).Query(ctx, flux)
	if err != nil {
		return time.Time{}
	}
	defer result.Close()

	type stateRecord struct {
		t time.Time
		v string
	}
	var records []stateRecord
	for result.Next() {
		rec := result.Record()
		v, ok := rec.Value().(string)
		if !ok {
			continue
		}
		records = append(records, stateRecord{t: rec.Time(), v: strings.TrimSpace(strings.ToLower(v))})
	}
	if result.Err() != nil || len(records) < 2 {
		return time.Time{}
	}

	for i := len(records) - 1; i >= 1; i-- {
		curr := records[i]
		prev := records[i-1]
		if isStationaryNavState(curr.v) && isMotoringNavState(prev.v) {
			return curr.t
		}
	}

	return time.Time{}
}

// queryInfluxPositionTrailRange returns vessel position points from InfluxDB in
// the half-open interval [start, end). It supports both lat/lon and
// latitude/longitude field naming.
func queryInfluxPositionTrailRange(start, end time.Time) []trailPoint {
	client, org, bucket, ok := newInfluxClient()
	if !ok || start.IsZero() || end.IsZero() || !end.After(start) {
		return nil
	}
	defer client.Close()

	measurementCandidates := []string{
		trimEnvValue(getEnv("INFLUX_POSITION_MEASUREMENT", "navigation.position")),
		"navigation.position.value",
	}
	fieldPairs := [][2]string{
		{"lat", "lon"},
		{"latitude", "longitude"},
	}

	for _, measurement := range measurementCandidates {
		if measurement == "" {
			continue
		}

		for _, pair := range fieldPairs {
			latField := pair[0]
			lonField := pair[1]
			flux := fmt.Sprintf(
				`from(bucket: %q) |> range(start: %s, stop: %s) |> filter(fn: (r) => r._measurement == %q and (r._field == %q or r._field == %q)) |> pivot(rowKey:["_time"], columnKey:["_field"], valueColumn:"_value") |> keep(columns:["_time",%q,%q]) |> sort(columns:["_time"], desc: false)`,
				bucket, start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339), measurement, latField, lonField, latField, lonField,
			)

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			result, err := client.QueryAPI(org).Query(ctx, flux)
			if err != nil {
				cancel()
				continue
			}

			var points []trailPoint
			for result.Next() {
				rec := result.Record()
				lat := coerceFloat(rec.ValueByKey(latField))
				lon := coerceFloat(rec.ValueByKey(lonField))
				if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
					continue
				}
				points = append(points, trailPoint{
					Lat:       lat,
					Lon:       lon,
					Timestamp: rec.Time(),
				})
				if len(points) >= maxTrailPoints {
					points = points[len(points)-maxTrailPoints:]
				}
			}
			result.Close()
			cancel()
			if result.Err() != nil {
				continue
			}
			if len(points) > 0 {
				return points
			}
		}
	}

	return nil
}

func isStationaryNavState(state string) bool {
	return state == "moored" || state == "anchored"
}

func isMotoringNavState(state string) bool {
	return state == "motoring" || state == "under way using engine" || state == "under_way_using_engine"
}

// queryInfluxMotoringTrailDownsampled fetches a downsampled vessel track for
// the given time window. It uses aggregateWindow to reduce very high-frequency
// SignalK position data to one fix per interval, ensuring the full span of the
// approach is visible rather than just the last N raw fixes.
// intervalSecs controls the bucket size; 15 gives ~480 points over 2 hours.
func queryInfluxMotoringTrailDownsampled(start, end time.Time, intervalSecs int) []trailPoint {
	client, org, bucket, ok := newInfluxClient()
	if !ok || start.IsZero() || end.IsZero() || !end.After(start) {
		return nil
	}
	defer client.Close()

	measurement := trimEnvValue(getEnv("INFLUX_POSITION_MEASUREMENT", "navigation.position"))
	if measurement == "" {
		measurement = "navigation.position"
	}

	fieldPairs := [][2]string{
		{"lat", "lon"},
		{"latitude", "longitude"},
	}

	positionSource := trimEnvValue(getEnv("INFLUX_POSITION_SOURCE", "WLN10.GP"))
	sourceFilters := []string{""}
	if positionSource != "" {
		// Try the configured source first, but fall back to all sources if no points
		// are returned so reposition mode does not go blank on tag-name/source drift.
		sourceFilters = append([]string{fmt.Sprintf(
			` |> filter(fn: (r) => (exists r.source and r.source == %q) or (exists r._source and r._source == %q))`,
			positionSource,
			positionSource,
		)}, sourceFilters...)
	}

	for _, pair := range fieldPairs {
		latField := pair[0]
		lonField := pair[1]

		for _, sourceFilter := range sourceFilters {
			// aggregateWindow before pivot collapses all sources to one point per
			// interval, eliminating duplicates from multiple SignalK position sources.
			flux := fmt.Sprintf(
				`from(bucket: %q)`+
					` |> range(start: %s, stop: %s)`+
					` |> filter(fn: (r) => r._measurement == %q and (r._field == %q or r._field == %q))`+
					`%s`+
					` |> aggregateWindow(every: %ds, fn: last, createEmpty: false)`+
					` |> pivot(rowKey:["_time"], columnKey:["_field"], valueColumn:"_value")`+
					` |> keep(columns:["_time",%q,%q])`+
					` |> sort(columns:["_time"], desc: false)`,
				bucket,
				start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
				measurement, latField, lonField,
				sourceFilter,
				intervalSecs,
				latField, lonField,
			)

			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			result, err := client.QueryAPI(org).Query(ctx, flux)
			if err != nil {
				cancel()
				continue
			}

			var points []trailPoint
			for result.Next() {
				rec := result.Record()
				lat := coerceFloat(rec.ValueByKey(latField))
				lon := coerceFloat(rec.ValueByKey(lonField))
				if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
					continue
				}
				points = append(points, trailPoint{
					Lat:       lat,
					Lon:       lon,
					Timestamp: rec.Time(),
				})
			}
			result.Close()
			cancel()
			if result.Err() != nil {
				continue
			}
			if len(points) > 0 {
				return points
			}
		}
	}

	return nil
}

func trimEnvValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"`)
	return strings.TrimSpace(trimmed)
}

func parseDurationEnv(key string, fallback time.Duration) time.Duration {
	value := trimEnvValue(getEnv(key, ""))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func splitCSVEnv(key string, fallback []string) []string {
	value := trimEnvValue(getEnv(key, ""))
	if value == "" {
		return fallback
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return fallback
	}
	return result
}

func summarizeEncounterSessions(timestamps []time.Time, now time.Time, sessionGap, activeWindow time.Duration) nearbyVesselEncounterSummary {
	if len(timestamps) == 0 {
		return nearbyVesselEncounterSummary{}
	}

	copyTimes := make([]time.Time, 0, len(timestamps))
	seen := make(map[time.Time]struct{}, len(timestamps))
	for _, ts := range timestamps {
		if ts.IsZero() {
			continue
		}
		t := ts.UTC().Truncate(time.Second)
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		copyTimes = append(copyTimes, t)
	}
	if len(copyTimes) == 0 {
		return nearbyVesselEncounterSummary{}
	}

	sort.Slice(copyTimes, func(i, j int) bool {
		return copyTimes[i].Before(copyTimes[j])
	})

	type span struct {
		start time.Time
		end   time.Time
	}
	sessions := []span{{start: copyTimes[0], end: copyTimes[0]}}
	for i := 1; i < len(copyTimes); i++ {
		last := &sessions[len(sessions)-1]
		if copyTimes[i].Sub(last.end) > sessionGap {
			sessions = append(sessions, span{start: copyTimes[i], end: copyTimes[i]})
			continue
		}
		last.end = copyTimes[i]
	}

	summary := nearbyVesselEncounterSummary{}
	if len(sessions) == 0 {
		return summary
	}

	latest := sessions[len(sessions)-1]
	currentActive := latest.end.After(now.Add(-activeWindow))
	if currentActive {
		summary.SeenCount = len(sessions) - 1
		if len(sessions) >= 2 {
			summary.LastSeenAt = sessions[len(sessions)-2].end
		}
		return summary
	}

	summary.SeenCount = len(sessions)
	summary.LastSeenAt = latest.end
	return summary
}

func queryInfluxNearbyVesselHistory(vesselNames []string, now time.Time) map[string]nearbyVesselEncounterSummary {
	out := make(map[string]nearbyVesselEncounterSummary)
	if len(vesselNames) == 0 {
		return out
	}

	client, org, bucket, ok := newInfluxClient()
	if !ok {
		return out
	}
	defer client.Close()

	measurement := trimEnvValue(getEnv("INFLUX_NEARBY_HISTORY_MEASUREMENT", "navigation.position"))
	lookback := trimEnvValue(getEnv("INFLUX_NEARBY_HISTORY_LOOKBACK", "365d"))
	nameTags := splitCSVEnv("INFLUX_NEARBY_HISTORY_NAME_TAGS", []string{"name", "vessel_name", "design_name", "context"})
	sessionGap := parseDurationEnv("INFLUX_NEARBY_HISTORY_SESSION_GAP", 6*time.Hour)
	activeWindow := parseDurationEnv("INFLUX_NEARBY_HISTORY_ACTIVE_WINDOW", 2*time.Hour)

	queryAPI := client.QueryAPI(org)
	loggedInfluxError := false

	for _, vesselName := range vesselNames {
		normalizedName := strings.TrimSpace(strings.ToUpper(vesselName))
		if normalizedName == "" {
			continue
		}

		var timestamps []time.Time
		for _, nameTag := range nameTags {
			flux := fmt.Sprintf(
				`from(bucket: %q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and (r._field == "lat" or r._field == "lon" or r._field == "latitude" or r._field == "longitude")) |> filter(fn: (r) => strings.toUpper(v: string(v: r[%q])) == %q) |> keep(columns: ["_time"]) |> sort(columns: ["_time"], desc: false)`,
				bucket, lookback, measurement, nameTag, normalizedName,
			)

			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			result, err := queryAPI.Query(ctx, `import "strings" `+flux)
			if err != nil {
				cancel()
				if !loggedInfluxError {
					log.Printf("nearby-vessels history: influx query unavailable, assuming zero prior contacts: %v", err)
					loggedInfluxError = true
				}
				break
			}

			var found []time.Time
			for result.Next() {
				found = append(found, result.Record().Time())
			}
			result.Close()
			cancel()
			if result.Err() != nil {
				if !loggedInfluxError {
					log.Printf("nearby-vessels history: influx result error, assuming zero prior contacts: %v", result.Err())
					loggedInfluxError = true
				}
				break
			}

			if len(found) > 0 {
				timestamps = found
				break
			}
		}

		out[vesselName] = summarizeEncounterSessions(timestamps, now, sessionGap, activeWindow)
	}

	return out
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
