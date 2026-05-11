package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func queryInfluxMaxWindGustKts(window string) float64 {
	influxURL := trimEnvValue(getEnv("INFLUXDB_URL", ""))
	org := trimEnvValue(getEnv("INFLUXDB_ORG", ""))
	bucket := trimEnvValue(getEnv("INFLUXDB_BUCKET", ""))
	token := trimEnvValue(getEnv("INFLUXDB_TOKEN", ""))
	measurement := trimEnvValue(getEnv("INFLUX_WIND_MEASUREMENT", "environment.wind.speedApparent"))
	field := trimEnvValue(getEnv("INFLUX_WIND_FIELD", "value"))

	if influxURL == "" || org == "" || bucket == "" || token == "" {
		return -1
	}

	flux := fmt.Sprintf(`from(bucket: %q) |> range(start: -%s) |> filter(fn: (r) => r._measurement == %q and r._field == %q) |> max(column: "_value") |> keep(columns: ["_value"])`, bucket, window, measurement, field)
	bodyBytes, err := json.Marshal(map[string]string{"query": flux, "type": "flux"})
	if err != nil {
		return -1
	}

	queryURL := strings.TrimRight(influxURL, "/") + "/api/v2/query?org=" + url.QueryEscape(org)
	request, err := http.NewRequest(http.MethodPost, queryURL, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return -1
	}

	request.Header.Set("Authorization", "Token "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/csv")

	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return -1
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return -1
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return -1
	}

	csvReader := csv.NewReader(strings.NewReader(string(body)))
	records, err := csvReader.ReadAll()
	if err != nil {
		return -1
	}

	maxMS := -1.0
	for _, record := range records {
		if len(record) == 0 || strings.HasPrefix(record[0], "#") {
			continue
		}
		valueCandidate := strings.TrimSpace(record[len(record)-1])
		value, parseErr := parseFloat(valueCandidate)
		if parseErr == nil {
			maxMS = value
		}
	}

	if maxMS < 0 {
		return -1
	}
	return math.Round((maxMS*metersPerSecondToKnots)*10) / 10
}

func trimEnvValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"`)
	return strings.TrimSpace(trimmed)
}

func parseFloat(value string) (float64, error) {
	var parsed float64
	_, err := fmt.Sscanf(value, "%f", &parsed)
	if err != nil {
		return 0, err
	}
	return parsed, nil
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
