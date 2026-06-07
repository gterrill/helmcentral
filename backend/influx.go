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
