package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
)

type depthTrendPoint struct {
	Time   time.Time `json:"time"`
	DepthM float64   `json:"depth_m"`
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

	for _, pair := range fieldPairs {
		latField := pair[0]
		lonField := pair[1]

		// aggregateWindow before pivot collapses all sources to one point per
		// interval, eliminating duplicates from multiple SignalK position sources.
		flux := fmt.Sprintf(
			`from(bucket: %q)`+
				` |> range(start: %s, stop: %s)`+
				` |> filter(fn: (r) => r._measurement == %q and (r._field == %q or r._field == %q))`+
				` |> aggregateWindow(every: %ds, fn: last, createEmpty: false)`+
				` |> pivot(rowKey:["_time"], columnKey:["_field"], valueColumn:"_value")`+
				` |> keep(columns:["_time",%q,%q])`+
				` |> sort(columns:["_time"], desc: false)`,
			bucket,
			start.UTC().Format(time.RFC3339), end.UTC().Format(time.RFC3339),
			measurement, latField, lonField,
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

	return nil
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
