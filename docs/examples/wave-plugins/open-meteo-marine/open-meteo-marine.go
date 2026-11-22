// open-meteo-marine.go holds the parsing and HTTP logic for the Open-Meteo
// Marine wave-provider plugin, kept in a separate file from main.go
// deliberately: this file has no dependency on "github.com/extism/go-pdk",
// so it (and main_test.go, which exercises it) can be built and tested with
// the plain host Go toolchain (`go test ./...`, no TinyGo/wasm target
// needed) - see main.go's doc comment for why that split matters. main.go's
// //go:wasmexport functions call straight into these same functions; nothing
// here is reimplemented or duplicated there.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Marine wave data endpoint - pinned to NOAA's GFS-Wave (WaveWatch III)
	// model (modelGFSWave, appended by waveURL) so swell height, period and
	// direction line up with other WaveWatch III-based swell forecasts for a
	// given coastline; Open-Meteo's default "best_match" model blend runs
	// noticeably lower/different here.
	marineWaveURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&hourly=" + waveHourlyVariables + "&timezone=auto&forecast_days=%d"

	// waveHourlyVariables are the nine wave variables the forecast and the
	// hindcast both ask for, so the two read the same figures.
	waveHourlyVariables = "wave_height,wave_direction,wave_period,wind_wave_height,wind_wave_direction,wind_wave_period,swell_wave_height,swell_wave_direction,swell_wave_period"

	// Ocean current endpoint, for fetch_currents. Deliberately does not pin a
	// models= param: the GFS-Wave model the wave fetch is pinned to carries no
	// ocean currents (every value comes back null), so only the default blend
	// answers. It asks for the two current variables alone, for every point in
	// one request (comma-separated latitude and longitude lists), in GMT so the
	// times are UTC and one offset cannot be wrong across a daylight-saving
	// change, with yesterday's hours (past_days=1) so a window that starts earlier
	// today is bracketed. Velocity comes back in km/h, and the direction is where the water
	// flows toward.
	marineCurrentsURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%s&longitude=%s&hourly=ocean_current_velocity,ocean_current_direction&timezone=GMT&past_days=1&forecast_days=%d"

	// Past-hours endpoint, for fetch_wave_hindcast. The same variables and the
	// same GFS-Wave pin as the forecast, so a hindcast hour and a forecast hour
	// come from one model. It asks for named hours (start_hour, end_hour) in GMT,
	// for every point in one request (comma-separated latitude and longitude
	// lists, which Open-Meteo documents without a limit).
	marineHindcastURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%s&longitude=%s&hourly=" + waveHourlyVariables + "&timezone=GMT&start_hour=%s&end_hour=%s"

	// hindcastHourLayout is how Open-Meteo reads start_hour and end_hour.
	hindcastHourLayout = "2006-01-02T15:04"

	// maxHindcastHours is the most hours one fetch_wave_hindcast call carries,
	// 31 days. The host asks in shorter windows and errors above this.
	maxHindcastHours = 31 * 24

	// maxHindcastPoints is the most points one fetch_wave_hindcast call carries.
	// The two lists cost about 20 characters a point, so 100 keeps the URL near
	// 2 KB, as for fetch_currents. The host asks for more points in several calls.
	maxHindcastPoints = 100

	// How the plugin words each model for the operator. A sentence that starts
	// with one reads as a sentence.
	labelGFSWave = "GFS-Wave"
	labelBlend   = "The Open-Meteo best-match blend"

	// maxCurrentsPoints is the most points one fetch_currents call carries. The
	// two lists cost about 20 characters a point, so 100 keeps the URL near 2 KB,
	// well inside what servers accept. The host errors above it and never splits
	// the route into several requests.
	maxCurrentsPoints = 100

	// modelGFSWave is the model the wave fetch is pinned to. Its 0.25 degree
	// grid has land-masked cells, and over one Open-Meteo returns every wave
	// field as 0, not null.
	modelGFSWave = "ncep_gfswave025"

	// modelBlend names Open-Meteo's default model blend (no models= param),
	// which has a finer grid and real values over cells GFS-Wave masks.
	modelBlend = "best_match"

	// Marine sea temperature endpoint - Deliberately does not pin a models=
	// param (unlike fetchWaveData) - the wave-specific NOAA GFS-Wave model
	// that wave data is pinned to does not carry sea surface temperature at
	// all (returns null/"undefined" units); only Open-Meteo's default model
	// blend does.
	marineSeaTempURLFmt = "https://marine-api.open-meteo.com/v1/marine?latitude=%.4f&longitude=%.4f&current=sea_surface_temperature"

	// defaultForecastDays matches the original ported code's hardcoded window.
	defaultForecastDays = 10

	// maxForecastDays is Open-Meteo Marine API's documented maximum.
	maxForecastDays = 16
)

// waveURL builds the wave-data request. fallback leaves out models= so
// Open-Meteo answers with its default blend.
func waveURL(lat, lon float64, days int, fallback bool) string {
	u := fmt.Sprintf(marineWaveURLFmt, lat, lon, days)
	if !fallback {
		u += "&models=" + modelGFSWave
	}
	return u
}

// coordinateLists joins the points' latitudes and longitudes into the two
// comma-separated lists Open-Meteo reads, in order.
func coordinateLists(points []currentsPoint) (lats, lons string) {
	la := make([]string, len(points))
	lo := make([]string, len(points))
	for i, p := range points {
		la[i] = fmt.Sprintf("%.4f", p.Lat)
		lo[i] = fmt.Sprintf("%.4f", p.Lon)
	}
	return strings.Join(la, ","), strings.Join(lo, ",")
}

// hindcastURL builds the one request for the past hours start to end inclusive at
// every point, in GMT so the times are UTC. It names its hours with start_hour
// and end_hour in place of forecast_days. fallback leaves out models=, as waveURL
// does.
func hindcastURL(points []currentsPoint, start, end time.Time, fallback bool) string {
	lats, lons := coordinateLists(points)
	u := fmt.Sprintf(marineHindcastURLFmt, lats, lons, start.UTC().Format(hindcastHourLayout), end.UTC().Format(hindcastHourLayout))
	if !fallback {
		u += "&models=" + modelGFSWave
	}
	return u
}

// validateHindcastInput reads what a fetch_wave_hindcast call asks for: at least
// one and at most maxHindcastPoints points, each a real position, and an RFC3339
// start and end, the end not before the start, no longer than maxHindcastHours.
// Both ends are cut down to the hour.
func validateHindcastInput(points []currentsPoint, startRaw, endRaw string) (start, end time.Time, err error) {
	if len(points) == 0 {
		return start, end, fmt.Errorf("fetch_wave_hindcast needs at least one point")
	}
	if len(points) > maxHindcastPoints {
		return start, end, fmt.Errorf("fetch_wave_hindcast carries at most %d points, got %d", maxHindcastPoints, len(points))
	}
	for i, p := range points {
		if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
			return start, end, fmt.Errorf("fetch_wave_hindcast point %d (%g, %g) is not a valid position", i, p.Lat, p.Lon)
		}
	}
	start, err = time.Parse(time.RFC3339, startRaw)
	if err != nil {
		return start, end, fmt.Errorf("fetch_wave_hindcast start %q is not RFC3339: %w", startRaw, err)
	}
	end, err = time.Parse(time.RFC3339, endRaw)
	if err != nil {
		return start, end, fmt.Errorf("fetch_wave_hindcast end %q is not RFC3339: %w", endRaw, err)
	}
	start, end = start.UTC().Truncate(time.Hour), end.UTC().Truncate(time.Hour)
	if end.Before(start) {
		return start, end, fmt.Errorf("fetch_wave_hindcast end %s is before its start %s", endRaw, startRaw)
	}
	if hours := int(end.Sub(start)/time.Hour) + 1; hours > maxHindcastHours {
		return start, end, fmt.Errorf("fetch_wave_hindcast carries at most %d hours, got %d", maxHindcastHours, hours)
	}
	return start, end, nil
}

// currentsPoint is one position a fetch_currents call asks about.
type currentsPoint struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// validateCurrentsInput checks the points a fetch_currents call carries: at
// least one, at most maxCurrentsPoints, each a real position.
func validateCurrentsInput(points []currentsPoint) error {
	if len(points) == 0 {
		return fmt.Errorf("fetch_currents needs at least one point")
	}
	if len(points) > maxCurrentsPoints {
		return fmt.Errorf("fetch_currents carries at most %d points, got %d", maxCurrentsPoints, len(points))
	}
	for i, p := range points {
		if p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
			return fmt.Errorf("fetch_currents point %d (%g, %g) is not a valid position", i, p.Lat, p.Lon)
		}
	}
	return nil
}

// currentsURL builds the one ocean-current request for every point.
func currentsURL(points []currentsPoint, days int) string {
	lats, lons := coordinateLists(points)
	return fmt.Sprintf(marineCurrentsURLFmt, lats, lons, days)
}

// currentHourOutput is one hour of fetch_currents output: the modelled
// current's speed in m/s and the direction it flows TOWARD, degrees true. Both
// keys are left out for an hour with no figure, never emitted as 0.
type currentHourOutput struct {
	Time              string   `json:"time"`
	CurrentVelocityMS *float64 `json:"current_velocity_ms,omitempty"`
	CurrentSetDeg     *float64 `json:"current_set_deg,omitempty"`
}

// currentPointOutput is one requested point's hours, in request order.
type currentPointOutput struct {
	Hourly []currentHourOutput `json:"hourly"`
}

// fetchCurrentsOutput mirrors the host's wasmFetchCurrentsOutput contract
// (backend/wasm_wave_provider.go).
type fetchCurrentsOutput struct {
	Points []currentPointOutput `json:"points"`
}

// parseOpenMeteoCurrentsResponse reads Open-Meteo's answer for `want` points:
// a JSON array with one object per location in request order, or a single
// object when one location was asked for. It returns one hourly series per
// point. The request is in GMT, so a response with any other offset is an error,
// not a time to guess at. Every hour is listed. Only a JSON null in both arrays
// means "no current that hour" (Open-Meteo returns null over land and near
// shore), and such an hour is never read as slack. A reported 0 is a real figure
// and is kept. Anything else that is not a clean answer is an error for the host
// to report: a wrong location count, a missing or mistyped array, arrays of
// unequal length, an unparseable time, a missing offset or velocity unit, a
// velocity unit other than km/h (it would be converted wrongly), a null in only
// one of the pair, or a figure out of range (negative velocity, direction
// outside 0 to 360).
func parseOpenMeteoCurrentsResponse(raw []byte, want int) ([][]currentHourOutput, error) {
	var locations []json.RawMessage
	if err := json.Unmarshal(raw, &locations); err != nil {
		var single map[string]any
		if json.Unmarshal(raw, &single) != nil {
			return nil, fmt.Errorf("failed to parse ocean current response: %w", err)
		}
		locations = []json.RawMessage{raw}
	}
	if len(locations) != want {
		return nil, fmt.Errorf("ocean current response has %d locations, asked for %d", len(locations), want)
	}
	out := make([][]currentHourOutput, len(locations))
	for i, loc := range locations {
		hours, err := parseCurrentsLocation(loc)
		if err != nil {
			return nil, fmt.Errorf("location %d: %w", i, err)
		}
		out[i] = hours
	}
	return out, nil
}

// parseCurrentsLocation reads one location's object.
func parseCurrentsLocation(raw json.RawMessage) ([]currentHourOutput, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse ocean current location: %w", err)
	}
	offset, ok := result["utc_offset_seconds"].(float64)
	if !ok {
		return nil, fmt.Errorf("utc_offset_seconds missing from ocean current response")
	}
	if offset != 0 {
		return nil, fmt.Errorf("utc_offset_seconds is %g, but the request is in GMT", offset)
	}
	units, ok := result["hourly_units"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hourly_units missing from ocean current response, so the velocity unit is unknown")
	}
	if u, ok := units["ocean_current_velocity"].(string); !ok || u != "km/h" {
		return nil, fmt.Errorf("ocean_current_velocity unit is %v, expected km/h", units["ocean_current_velocity"])
	}
	hourly, ok := result["hourly"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hourly missing from ocean current response")
	}
	times, ok := hourly["time"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.time missing from ocean current response")
	}
	velocities, ok := hourly["ocean_current_velocity"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.ocean_current_velocity missing from ocean current response")
	}
	directions, ok := hourly["ocean_current_direction"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.ocean_current_direction missing from ocean current response")
	}
	if len(velocities) != len(times) || len(directions) != len(times) {
		return nil, fmt.Errorf("ocean current arrays differ in length: %d times, %d velocities, %d directions", len(times), len(velocities), len(directions))
	}

	out := make([]currentHourOutput, 0, len(times))
	for i, rawTime := range times {
		timeStr, ok := rawTime.(string)
		if !ok {
			return nil, fmt.Errorf("hourly.time[%d] is not a string", i)
		}
		t, err := parseOpenMeteoLocalTime(timeStr, 0)
		if err != nil {
			return nil, err
		}
		hour := currentHourOutput{Time: t.UTC().Format(time.RFC3339)}
		rv, rd := velocities[i], directions[i]
		switch {
		case rv == nil && rd == nil:
			// The model has no current this hour (land, near shore).
		case rv == nil || rd == nil:
			return nil, fmt.Errorf("hourly[%d] has a null in one of velocity and direction but not the other", i)
		default:
			v, vok := rv.(float64)
			d, dok := rd.(float64)
			if !vok || !dok {
				return nil, fmt.Errorf("hourly[%d] velocity or direction is not a number", i)
			}
			if v < 0 || d < 0 || d > 360 {
				return nil, fmt.Errorf("hourly[%d] velocity %g km/h or direction %g degrees is out of range", i, v, d)
			}
			ms := v / 3.6
			hour.CurrentVelocityMS, hour.CurrentSetDeg = &ms, &d
		}
		out = append(out, hour)
	}
	return out, nil
}

// fallbackEnabled reads the use_blend_fallback config value. Anything but
// "true" is off.
func fallbackEnabled(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

// parseWaveBody takes the UTC offset from a wave response and parses its
// hourly series.
func parseWaveBody(body []byte) ([]waveHourOutput, error) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse wave response: %w", err)
	}
	offset := 0
	if o, ok := raw["utc_offset_seconds"].(float64); ok {
		offset = int(o)
	}
	return parseOpenMeteoWaveResponse(body, offset)
}

// parseWaveBodies reads Open-Meteo's answer for want positions: a JSON list with
// one object per location in request order, or a single object when one location
// was asked for. It returns one hourly series per position. A wrong location
// count is an error, not something to line up by guesswork.
func parseWaveBodies(body []byte, want int) ([][]waveHourOutput, error) {
	var locations []json.RawMessage
	if err := json.Unmarshal(body, &locations); err != nil {
		locations = []json.RawMessage{body}
	}
	if len(locations) != want {
		return nil, fmt.Errorf("wave response has %d locations, asked for %d", len(locations), want)
	}
	out := make([][]waveHourOutput, len(locations))
	for i, loc := range locations {
		hours, err := parseWaveBody(loc)
		if err != nil {
			return nil, fmt.Errorf("location %d: %w", i, err)
		}
		out[i] = hours
	}
	return out, nil
}

// refusedError is Open-Meteo's definite no to a hindcast request: an HTTP 400
// saying a date is outside its archive. Asking again gives the same answer, so
// fetch_wave_hindcast reports it as a refusal the host settles with the sentence,
// not as an error the host retries.
type refusedError struct {
	Status int
	Reason string
}

func (e *refusedError) Error() string {
	return fmt.Sprintf("open-meteo marine hindcast API refused the request (%d): %s", e.Status, e.Reason)
}

// Sentence is how the refusal reads in the logbook, in place of a sea state.
func (e *refusedError) Sentence() string {
	why := strings.TrimSpace(e.Reason)
	if why == "" {
		why = fmt.Sprintf("it answered %d", e.Status)
	}
	if !strings.HasSuffix(why, ".") {
		why += "."
	}
	return "Open-Meteo wouldn't give the sea for this hour: " + why
}

// isTimeOutOfRange says an Open-Meteo reason is about the requested time or date
// being outside what it holds, such as "Parameter 'start_hour' is out of allowed
// range from 2022-01-01 to 2026-11-30".
func isTimeOutOfRange(reason string) bool {
	r := strings.ToLower(reason)
	if !strings.Contains(r, "range") && !strings.Contains(r, "allowed") {
		return false
	}
	for _, param := range []string{"start_hour", "end_hour", "start_date", "end_date"} {
		if strings.Contains(r, param) {
			return true
		}
	}
	return false
}

// hindcastStatusError is nil for a 2xx. Only an HTTP 400 whose JSON reason says
// the requested time is out of range is a refusal (*refusedError): asking again
// gives the same answer. A 429 says it was rate limited, and every other status
// (auth, WAF, quota, URL length, a bad position, a timeout, a 5xx) is transient.
// Those stay plain errors, which the host does not settle: it asks again at its
// next hourly sweep (and stops the sweep at a rate limit).
func hindcastStatusError(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	if status == 400 {
		var parsed struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(body, &parsed)
		if isTimeOutOfRange(parsed.Reason) {
			return &refusedError{Status: status, Reason: parsed.Reason}
		}
	}
	if status == 429 {
		return fmt.Errorf("open-meteo marine hindcast API rate limited the request (429)")
	}
	return fmt.Errorf("open-meteo marine hindcast API returned %d", status)
}

// hindcastHourOutput is one hour of fetch_wave_hindcast output: a fetch_waves hour
// and, when the model has no value for it, the sentence the host shows in place
// of a sea state.
type hindcastHourOutput struct {
	waveHourOutput
	MissingReason string `json:"missing_reason,omitempty"`
}

// hindcastPointOutput is one requested point's answer, in request order: the
// model that gave it (its id, and how the logbook words it) and its hours.
type hindcastPointOutput struct {
	Model      string               `json:"model"`
	ModelLabel string               `json:"model_label"`
	Hourly     []hindcastHourOutput `json:"hourly"`
}

// fetchHindcastOutput mirrors the host's wasmFetchWaveHindcastOutput contract
// (backend/wasm_wave_provider.go).
type fetchHindcastOutput struct {
	Points []hindcastPointOutput `json:"points"`
	// Refused is the sentence for a request Open-Meteo definitely refused; Points
	// is then empty and the host settles every hour it asked for with it.
	Refused string `json:"refused,omitempty"`
}

// hindcastHours words each hour's gap. An hour with no combined figures and no
// swell or wind wave is one the model has no value for, and an hour with a
// combined sea but no split into swell and wind wave is one it could not split.
// Both carry the model's label in the sentence. Every other hour carries none,
// and a swell or wind wave with no period in it is a system the model says is
// not there.
func hindcastHours(hours []waveHourOutput, label string) []hindcastHourOutput {
	out := make([]hindcastHourOutput, len(hours))
	for i, h := range hours {
		out[i] = hindcastHourOutput{waveHourOutput: h}
		if h.SwellWavePeriodS > 0 || h.WindWavePeriodS > 0 {
			continue
		}
		if h.WavePeriodS == nil {
			out[i].MissingReason = label + " has no value at this cell."
		} else {
			out[i].MissingReason = label + " gave no swell and wind-wave split at this cell."
		}
	}
	return out
}

// hindcastPointFor is one point's answer from the model that gave it, labelled
// from the one table (labelForModel) the forecast export uses too.
func hindcastPointFor(model string, hours []waveHourOutput) (hindcastPointOutput, error) {
	label, err := labelForModel(model)
	if err != nil {
		return hindcastPointOutput{}, err
	}
	return hindcastPointOutput{Model: model, ModelLabel: label, Hourly: hindcastHours(hours, label)}, nil
}

// refusedPointFor is a point whose hours all carry the refusal's sentence.
func refusedPointFor(model string, hours []waveHourOutput, sentence string) (hindcastPointOutput, error) {
	label, err := labelForModel(model)
	if err != nil {
		return hindcastPointOutput{}, err
	}
	return hindcastPointOutput{Model: model, ModelLabel: label, Hourly: refusedHours(hours, sentence)}, nil
}

// refusedHours is a point's hours, each with no figures and the sentence for why.
func refusedHours(hours []waveHourOutput, sentence string) []hindcastHourOutput {
	out := make([]hindcastHourOutput, len(hours))
	for i, h := range hours {
		out[i] = hindcastHourOutput{waveHourOutput: waveHourOutput{Time: h.Time}, MissingReason: sentence}
	}
	return out
}

// chooseHindcast builds the answer for points. fetch asks Open-Meteo once for
// the points it is given, with the GFS-Wave pin when its second argument is false
// and without it (the blend) when true, and returns one series per point it was
// given. The GFS-Wave series stands for every point unless the operator turned
// the fallback on and a point's series holds no waves (a land-masked cell); those
// points, and only those, are asked again in ONE blend request. A failed blend
// request is an error: it does not drop back to the land cell as though the model
// had answered. A request Open-Meteo definitely refused (*refusedError) is the
// answer: when it is the first request, as Refused with no points; when it is the
// blend request, the open-water answers stand and the land points carry the
// sentence on every hour, as their missing reason. Any other failure is an error.
func chooseHindcast(points []currentsPoint, fallback bool, fetch func([]currentsPoint, bool) ([][]waveHourOutput, error)) (fetchHindcastOutput, error) {
	var refused *refusedError
	primary, err := fetch(points, false)
	if errors.As(err, &refused) {
		return fetchHindcastOutput{Refused: refused.Sentence()}, nil
	}
	if err != nil {
		return fetchHindcastOutput{}, err
	}
	out := fetchHindcastOutput{Points: make([]hindcastPointOutput, len(points))}
	var landIdx []int
	var landPoints []currentsPoint
	for i, hours := range primary {
		pt, err := hindcastPointFor(modelGFSWave, hours)
		if err != nil {
			return fetchHindcastOutput{}, err
		}
		out.Points[i] = pt
		if fallback && hoursHaveNoWaves(hours) {
			landIdx = append(landIdx, i)
			landPoints = append(landPoints, points[i])
		}
	}
	if len(landPoints) == 0 {
		return out, nil
	}
	blend, err := fetch(landPoints, true)
	if errors.As(err, &refused) {
		// Open-Meteo's no to the blend covers only the land points. The GFS-Wave
		// answers already in hand for the open-water points stand; each land
		// point keeps its hours, every one carrying the refusal sentence as its
		// missing reason, so the host settles them and shows no sea.
		for _, i := range landIdx {
			pt, err := refusedPointFor(modelGFSWave, primary[i], refused.Sentence())
			if err != nil {
				return fetchHindcastOutput{}, err
			}
			out.Points[i] = pt
		}
		return out, nil
	}
	if err != nil {
		return fetchHindcastOutput{}, fmt.Errorf("GFS-Wave has no data here and the blend fallback failed: %w", err)
	}
	for j, i := range landIdx {
		pt, err := hindcastPointFor(modelBlend, blend[j])
		if err != nil {
			return fetchHindcastOutput{}, err
		}
		out.Points[i] = pt
	}
	return out, nil
}

// hoursHaveNoWaves reports a series with no real wave reading: every hour's
// period is absent. parseOpenMeteoWaveResponse already leaves the combined
// figures out of an hour whose period is 0, which is what a land-masked
// GFS-Wave cell returns.
func hoursHaveNoWaves(hours []waveHourOutput) bool {
	for _, h := range hours {
		if h.WavePeriodS != nil {
			return false
		}
	}
	return true
}

// chooseWaveSeries keeps the GFS-Wave series unless it holds no waves and the
// operator turned the fallback on, in which case fetchBlend supplies the
// default-blend series. A failed blend fetch is an error: it does not drop
// back to the land cell as though the model had answered.
func chooseWaveSeries(primary []waveHourOutput, fallback bool, fetchBlend func() ([]waveHourOutput, error)) ([]waveHourOutput, string, bool, error) {
	if !fallback || !hoursHaveNoWaves(primary) {
		return primary, modelGFSWave, false, nil
	}
	blend, err := fetchBlend()
	if err != nil {
		return nil, "", false, err
	}
	return blend, modelBlend, true, nil
}

// parseOpenMeteoWaveResponse parses the raw JSON wave-data response from
// Open-Meteo Marine API into a flat chronological list of hourly wave
// forecasts (NOT bucketed by day - bucketing is host-side now). All times
// are converted from naive local-time strings (via utc_offset_seconds) to
// proper RFC3339 format per the plugin contract.
//
// IMPORTANT: Open-Meteo returns naive local time in "2006-01-02T15:04"
// format (e.g. "2026-07-19T00:00"), without UTC offset. The response's
// top-level utc_offset_seconds field (e.g. 36000 for +10:00, can be negative)
// tells us how many seconds ahead of UTC the local time is. To get the true
// UTC instant: parse the naive string with time.Parse("2006-01-02T15:04", s),
// which yields a time.Time whose wall-clock fields equal local time but whose
// Location is UTC; since local = UTC + offset, true UTC instant =
// parsed.Add(-time.Duration(utcOffsetSeconds) * time.Second). This is a
// deliberate correctness improvement over the original host code
// (weather_tide.go's parseOpenMeteoMarineResponse), which parsed the naive
// time AS-IS without offset correction - that worked there only because the
// old code bucketed by local calendar date, so the incorrect absolute instant
// didn't matter for its purposes. This plugin's contract requires correct
// RFC3339 instants, so we cannot repeat that omission.
//
// If the hourly object is missing from the response, this is a hard error
// (wave data is required). If hourly.time is missing or the arrays are
// misaligned, it's also a hard error. Null (or non-numeric) entries in the
// combined height, period and direction arrays, which Open-Meteo returns when
// queried over land with no wave model coverage, leave that figure out of the
// hour so the host reads it as absent. A reported 0 is a real figure and is
// kept. The per-component fields keep their zero encoding, which the host
// reads as "no such wave train".
func parseOpenMeteoWaveResponse(raw []byte, utcOffsetSeconds int) ([]waveHourOutput, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse marine wave response: %w", err)
	}

	hourly, ok := result["hourly"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("hourly missing from marine wave response")
	}

	times, ok := hourly["time"].([]any)
	if !ok {
		return nil, fmt.Errorf("hourly.time missing from marine wave response")
	}

	heights, _ := hourly["wave_height"].([]any)
	periods, _ := hourly["wave_period"].([]any)
	directions, _ := hourly["wave_direction"].([]any)
	windWaveHeights, _ := hourly["wind_wave_height"].([]any)
	swellWaveHeights, _ := hourly["swell_wave_height"].([]any)

	// Per-component direction and period, so the host can tell a cross sea
	// from one system. Open-Meteo reports all three of a flat component's
	// fields as 0 rather than null, so a zero period is what marks a
	// component as absent - the host relies on that.
	windWaveDirections, _ := hourly["wind_wave_direction"].([]any)
	windWavePeriods, _ := hourly["wind_wave_period"].([]any)
	swellWaveDirections, _ := hourly["swell_wave_direction"].([]any)
	swellWavePeriods, _ := hourly["swell_wave_period"].([]any)

	hourly_out := make([]waveHourOutput, 0, len(times))
	for i, rawTime := range times {
		timeStr, ok := rawTime.(string)
		if !ok {
			continue
		}

		rfc3339Time, err := parseOpenMeteoLocalTime(timeStr, utcOffsetSeconds)
		if err != nil {
			continue
		}

		point := waveHourOutput{Time: rfc3339Time.UTC().Format(time.RFC3339)}
		if i < len(heights) {
			if h, ok := heights[i].(float64); ok {
				point.WaveHeightM = &h
			}
		}
		if i < len(periods) {
			if p, ok := periods[i].(float64); ok {
				point.WavePeriodS = &p
			}
		}
		if i < len(directions) {
			if d, ok := directions[i].(float64); ok {
				point.WaveDirectionDeg = &d
			}
		}
		if i < len(windWaveHeights) {
			if w, ok := windWaveHeights[i].(float64); ok {
				point.WindWaveHeightM = w
			}
		}
		if i < len(swellWaveHeights) {
			if s, ok := swellWaveHeights[i].(float64); ok {
				point.SwellWaveHeightM = s
			}
		}
		if i < len(windWaveDirections) {
			if d, ok := windWaveDirections[i].(float64); ok {
				point.WindWaveDirectionDeg = d
			}
		}
		if i < len(windWavePeriods) {
			if p, ok := windWavePeriods[i].(float64); ok {
				point.WindWavePeriodS = p
			}
		}
		if i < len(swellWaveDirections) {
			if d, ok := swellWaveDirections[i].(float64); ok {
				point.SwellWaveDirectionDeg = d
			}
		}
		if i < len(swellWavePeriods) {
			if p, ok := swellWavePeriods[i].(float64); ok {
				point.SwellWavePeriodS = p
			}
		}
		// A wave with no period is not a wave: a land-masked cell reports
		// every figure as 0, so the combined figures are left out, which the
		// host reads as absent (the same rule it applies itself).
		if point.WavePeriodS != nil && *point.WavePeriodS == 0 {
			point.WaveHeightM, point.WavePeriodS, point.WaveDirectionDeg = nil, nil, nil
		}

		hourly_out = append(hourly_out, point)
	}

	return hourly_out, nil
}

// parseOpenMeteoSeaTemperatureResponse parses the raw JSON sea-temperature
// response from Open-Meteo Marine API. Returns a *float64 (nil when
// unavailable) rather than an error - sea temperature is optional per the
// plugin contract, so an absent/null value is not a failure condition.
func parseOpenMeteoSeaTemperatureResponse(raw []byte) (*float64, error) {
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("failed to parse sea temperature response: %w", err)
	}

	current, ok := result["current"].(map[string]any)
	if !ok {
		// No current object at all - treat as "no data"
		return nil, nil
	}

	// Check for sea_surface_temperature key
	tempRaw, ok := current["sea_surface_temperature"]
	if !ok {
		// Key not present - treat as "no data"
		return nil, nil
	}

	// If the key is present but null in JSON, tempRaw will be nil
	if tempRaw == nil {
		return nil, nil
	}

	temp, ok := tempRaw.(float64)
	if !ok {
		return nil, fmt.Errorf("sea_surface_temperature is not a number")
	}

	return &temp, nil
}

// parseOpenMeteoLocalTime converts an Open-Meteo naive local-time string
// (format "2006-01-02T15:04") and UTC offset (seconds) into a properly-offset
// RFC3339 time.Time in UTC. This is the corrected version - the original
// host code did NOT apply the offset, which worked only because it bucketed
// by local date (so the incorrect instant didn't matter). The plugin contract
// requires correct RFC3339 instants.
func parseOpenMeteoLocalTime(localStr string, utcOffsetSeconds int) (time.Time, error) {
	// Parse as if the string represents wall-clock local time, but in UTC location
	parsed, err := time.Parse("2006-01-02T15:04", localStr)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to parse time %q: %w", localStr, err)
	}

	// parsed is now a time.Time with wall-clock values equal to local time,
	// but Location=UTC. The actual UTC instant is:
	// utc = local - offset = parsed - offset_duration
	offset := time.Duration(utcOffsetSeconds) * time.Second
	utc := parsed.Add(-offset)

	return utc, nil
}

// waveHourOutput mirrors the host's wasmWaveHourOutput contract exactly
// (backend/wasm_wave_provider.go).
type waveHourOutput struct {
	Time string `json:"time"`

	// The combined figures are left out when the model has no value for the
	// hour. The host reads a missing key as absent, which is not 0 m, 0 s or a
	// sea from due north.
	WaveHeightM      *float64 `json:"wave_height_m,omitempty"`
	WavePeriodS      *float64 `json:"wave_period_s,omitempty"`
	WaveDirectionDeg *float64 `json:"wave_direction_deg,omitempty"`
	WindWaveHeightM  float64  `json:"wind_wave_height_m"`
	SwellWaveHeightM float64  `json:"swell_wave_height_m"`

	WindWaveDirectionDeg  float64 `json:"wind_wave_direction_deg"`
	WindWavePeriodS       float64 `json:"wind_wave_period_s"`
	SwellWaveDirectionDeg float64 `json:"swell_wave_direction_deg"`
	SwellWavePeriodS      float64 `json:"swell_wave_period_s"`
}

// fetchWavesOutput mirrors the host's wasmFetchWavesOutput contract
// (backend/wasm_wave_provider.go).
type fetchWavesOutput struct {
	Hourly                 []waveHourOutput `json:"hourly"`
	SeaSurfaceTemperatureC *float64         `json:"sea_surface_temperature_c,omitempty"`
	// Model names the forecast model the figures came from, and ModelLabel is how
	// the logbook words it. Both are always present, so the host can match the
	// forecast to the sea it learned the comfort model from.
	Model        string `json:"model"`
	ModelLabel   string `json:"model_label"`
	FallbackUsed bool   `json:"fallback_used,omitempty"`
}

// labelForModel is how the plugin words a model it can answer with. A model with
// no label is a bug in the plugin, so it is an error and never a blank.
func labelForModel(model string) (string, error) {
	switch model {
	case modelGFSWave:
		return labelGFSWave, nil
	case modelBlend:
		return labelBlend, nil
	}
	return "", fmt.Errorf("no label for wave model %q", model)
}

// buildWavesOutput is fetch_waves' answer for the series chosen: its hours, the
// optional sea temperature, and the model that gave them with its label.
func buildWavesOutput(hours []waveHourOutput, model string, fellBack bool, seaTemp *float64) (fetchWavesOutput, error) {
	label, err := labelForModel(model)
	if err != nil {
		return fetchWavesOutput{}, err
	}
	return fetchWavesOutput{Hourly: hours, SeaSurfaceTemperatureC: seaTemp, Model: model, ModelLabel: label, FallbackUsed: fellBack}, nil
}

// clampForecastDays returns a clamped forecast_days value: default to 10 if
// <=0, cap at Open-Meteo's max of 16 otherwise.
func clampForecastDays(days int) int {
	if days <= 0 {
		return defaultForecastDays
	}
	if days > maxForecastDays {
		return maxForecastDays
	}
	return days
}
