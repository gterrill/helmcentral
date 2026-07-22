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

// loadInfluxSettings reads the "influxdb" section of settings.yaml
// (url/org/bucket, only used if enabled: true) plus INFLUXDB_TOKEN from the
// environment. ok is true only when enabled and all four values are
// non-empty.
func loadInfluxSettings(settingsPath string) (url, org, bucket, token string, ok bool) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return "", "", "", "", false
	}

	influxMap, isMap := settings["influxdb"].(map[string]any)
	if !isMap {
		return "", "", "", "", false
	}

	enabled, _ := influxMap["enabled"].(bool)
	if !enabled {
		return "", "", "", "", false
	}

	url = trimEnvValue(coerceString(influxMap["url"]))
	org = trimEnvValue(coerceString(influxMap["org"]))
	bucket = trimEnvValue(coerceString(influxMap["bucket"]))
	token = trimEnvValue(getEnv("INFLUXDB_TOKEN", ""))

	ok = url != "" && org != "" && bucket != "" && token != ""
	return url, org, bucket, token, ok
}

// influxTelemetryConfigured wraps loadInfluxSettings with the default
// settings path.
func influxTelemetryConfigured() bool {
	_, _, _, _, ok := loadInfluxSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	return ok
}

func newInfluxClient() (influxdb2.Client, string, string, bool) {
	influxURL, org, bucket, token, ok := loadInfluxSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if !ok {
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
