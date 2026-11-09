// open-meteo.go holds the parsing and extremes logic for the Open-Meteo Marine
// tide-provider plugin. It has no dependency on "github.com/extism/go-pdk", so
// it and main_test.go build and test with the plain host Go toolchain
// (`go test ./...`); main.go holds only the //go:wasmexport wrappers, behind
// the `tinygo` build tag, and calls straight into these functions. The same
// split the bom plugin uses.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

const (
	marineURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%s&longitude=%s&hourly=sea_level_height_msl&timezone=GMT&cell_selection=sea&past_days=1&forecast_days=7"

	// stationName is shown to the operator beside the tide. The host replaces
	// it with its own wording for the boat's position; it is set here so a
	// result read on its own is still honest about what it is.
	stationName = "Open-Meteo sea-level model"
)

// sample is one hourly sea-level reading, in metres above mean sea level.
type sample struct {
	T       time.Time
	HeightM float64
}

// extreme is a derived high or low water.
type extreme struct {
	T       time.Time
	HeightM float64
	High    bool
}

// --- Helmcentral plugin contract shapes (backend/wasm_tide_provider.go) ---

type stationOut struct {
	StationID string  `json:"station_id"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
}

type extremeOut struct {
	Time    string  `json:"time"`
	HeightM float64 `json:"height_m"`
	High    bool    `json:"high"`
}

type tideChartOut struct {
	Station  stationOut   `json:"station"`
	Extremes []extremeOut `json:"extremes"`
}

// marineResponse is the shape of a good response. A bad request answers
// {"error":true,"reason":"..."} instead.
type marineResponse struct {
	Error  bool   `json:"error"`
	Reason string `json:"reason"`
	Hourly struct {
		Time    []string   `json:"time"`
		HeightM []*float64 `json:"sea_level_height_msl"`
	} `json:"hourly"`
}

// parsePosition reads the "<lat>,<lon>" the host passes as the station id
// (ADR 0033: a position-based plugin takes its position as the station id).
func parsePosition(stationID string) (lat, lon float64, err error) {
	parts := strings.Split(strings.TrimSpace(stationID), ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("station id %q is not \"<lat>,<lon>\"", stationID)
	}
	lat, latErr := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, lonErr := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if latErr != nil || lonErr != nil || math.IsNaN(lat) || math.IsNaN(lon) || math.IsInf(lat, 0) || math.IsInf(lon, 0) {
		return 0, 0, fmt.Errorf("station id %q is not \"<lat>,<lon>\" numbers", stationID)
	}
	if lat < -90 || lat > 90 || lon < -180 || lon > 180 {
		return 0, 0, fmt.Errorf("position %q is outside -90..90 / -180..180", stationID)
	}
	return lat, lon, nil
}

func requestURL(lat, lon float64) string {
	return fmt.Sprintf(marineURLFmt,
		strconv.FormatFloat(lat, 'f', -1, 64), strconv.FormatFloat(lon, 'f', -1, 64))
}

// parseMarine reads the hourly series, skipping hours the model left null. An
// error body, a malformed series, or a series with no usable hour is an
// error: nothing is invented.
func parseMarine(body []byte) ([]sample, error) {
	var resp marineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("open-meteo marine: unparseable response: %w", err)
	}
	if resp.Error {
		return nil, fmt.Errorf("open-meteo marine: %s", resp.Reason)
	}
	if len(resp.Hourly.Time) != len(resp.Hourly.HeightM) {
		return nil, fmt.Errorf("open-meteo marine: %d times but %d heights", len(resp.Hourly.Time), len(resp.Hourly.HeightM))
	}
	samples := make([]sample, 0, len(resp.Hourly.Time))
	for i, ts := range resp.Hourly.Time {
		if resp.Hourly.HeightM[i] == nil {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02T15:04", ts, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("open-meteo marine: bad time %q: %w", ts, err)
		}
		samples = append(samples, sample{T: t, HeightM: *resp.Hourly.HeightM[i]})
	}
	if len(samples) == 0 {
		return nil, fmt.Errorf("open-meteo marine: the response has no sea-level heights for this position (no sea grid cell nearby?)")
	}
	return samples, nil
}

// minSwingM is the smallest rise or fall between a high and the next low that
// counts as a tide. The service rounds to a centimetre, so a wiggle of a
// centimetre or two on a slope is rounding, not water.
const minSwingM = 0.03

// deriveExtremes finds the highs and lows of the series.
//
// A candidate is a run of equal readings (usually one hour; the service
// rounds to a centimetre, so a turn can sit across two or three) with a
// lower neighbour on both sides (a high) or a higher one on both sides (a
// low). A run on a slope is not a turn. A single-hour candidate is refined
// with a parabola through it and its two neighbours, so the time and height
// are not stuck on the hour; a longer run is placed at its middle. A candidate
// needs hourly neighbours all the way across: over a gap left by a null hour a
// parabola would invent a time, so none is reported there.
//
// Candidates are then reduced to a strict alternation of highs and lows: two
// of a kind in a row keep the more extreme, and a turn that moves less than
// minSwingM from the one before is dropped as rounding.
func deriveExtremes(samples []sample) []extreme {
	var candidates []extreme
	for start := 0; start < len(samples); {
		end := start
		for end+1 < len(samples) && samples[end+1].HeightM == samples[start].HeightM && samples[end+1].T.Sub(samples[end].T) == time.Hour {
			end++
		}
		if start > 0 && end < len(samples)-1 && contiguous(samples[start-1:end+2]) {
			before, after, level := samples[start-1].HeightM, samples[end+1].HeightM, samples[start].HeightM
			high := level > before && level > after
			low := level < before && level < after
			if high || low {
				candidates = append(candidates, turn(samples, start, end, high))
			}
		}
		start = end + 1
	}

	var out []extreme
	for _, c := range candidates {
		if n := len(out); n > 0 {
			last := out[n-1]
			if last.High == c.High {
				if (c.High && c.HeightM > last.HeightM) || (!c.High && c.HeightM < last.HeightM) {
					out[n-1] = c
				}
				continue
			}
			if math.Abs(c.HeightM-last.HeightM) < minSwingM {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// contiguous says the samples are exactly an hour apart throughout.
func contiguous(s []sample) bool {
	for i := 1; i < len(s); i++ {
		if s[i].T.Sub(s[i-1].T) != time.Hour {
			return false
		}
	}
	return true
}

// turn places the extreme for the run samples[start..end].
func turn(samples []sample, start, end int, high bool) extreme {
	if end > start {
		mid := samples[start].T.Add(samples[end].T.Sub(samples[start].T) / 2)
		return extreme{T: mid, HeightM: samples[start].HeightM, High: high}
	}
	prev, cur, next := samples[start-1], samples[start], samples[start+1]
	t, h := cur.T, cur.HeightM
	// Parabola through (-1, prev), (0, cur), (1, next).
	if denom := prev.HeightM - 2*cur.HeightM + next.HeightM; denom != 0 {
		offset := 0.5 * (prev.HeightM - next.HeightM) / denom // hours, within +-0.5 for a true extreme
		if math.Abs(offset) <= 0.5 {
			t = cur.T.Add(time.Duration(math.Round(offset * float64(time.Hour))))
			h = cur.HeightM - 0.25*(prev.HeightM-next.HeightM)*offset
		}
	}
	return extreme{T: t, HeightM: h, High: high}
}

// buildChart is fetch_tide_chart's answer for a station id and the response
// body fetched for it.
func buildChart(stationID string, body []byte) (tideChartOut, error) {
	lat, lon, err := parsePosition(stationID)
	if err != nil {
		return tideChartOut{}, err
	}
	samples, err := parseMarine(body)
	if err != nil {
		return tideChartOut{}, err
	}
	extremes := deriveExtremes(samples)
	if len(extremes) == 0 {
		return tideChartOut{}, fmt.Errorf("open-meteo marine: no high or low water found in the series (is the sea level flat here?)")
	}
	out := tideChartOut{
		Station:  stationOut{StationID: strings.TrimSpace(stationID), Name: stationName, Lat: lat, Lon: lon},
		Extremes: make([]extremeOut, 0, len(extremes)),
	}
	for _, e := range extremes {
		out.Extremes = append(out.Extremes, extremeOut{Time: e.T.UTC().Format(time.RFC3339), HeightM: e.HeightM, High: e.High})
	}
	return out, nil
}
