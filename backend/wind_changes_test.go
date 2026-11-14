package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// windSeriesFrom builds an hourly series from 2026-11-14 00:00 in loc.
// Each pair is (direction, speed in knots) for one hour.
func windSeriesFrom(loc *time.Location, hours [][2]float64) []windChangePoint {
	start := time.Date(2026, 11, 14, 0, 0, 0, 0, loc)
	out := make([]windChangePoint, 0, len(hours))
	for i, h := range hours {
		out = append(out, windChangePoint{Time: start.Add(time.Duration(i) * time.Hour), DirDeg: h[0], SpeedKts: h[1]})
	}
	return out
}

func repeatWind(n int, deg, kn float64) [][2]float64 {
	out := make([][2]float64, n)
	for i := range out {
		out[i] = [2]float64{deg, kn}
	}
	return out
}

func joinWind(parts ...[][2]float64) [][2]float64 {
	var out [][2]float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// windChangeHourly is hours of calm-direction north wind at calmKn that turns
// south-east at 18 kn from hour 20.
func windChangeHourly(start time.Time, hours int, calmKn float64) []weatherHourPoint {
	ms := func(kn float64) float64 { return kn / metersPerSecondToKnots }
	var out []weatherHourPoint
	for h := 0; h < hours; h++ {
		hp := weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), Condition: "clear", WindSpeedMS: ms(calmKn), WindDirectionDeg: 0}
		if h >= 20 {
			hp.WindSpeedMS, hp.WindDirectionDeg = ms(18), 135
		}
		out = append(out, hp)
	}
	return out
}

func TestDetectWindChangesNamesDirectionsTimeAndSpeedAfter(t *testing.T) {
	loc := queenslandLoc(t)
	// 14 h of north at 10 kn, then 2 h turning, then 12 h of south-east at 15-20 kn.
	pts := windSeriesFrom(loc, joinWind(
		repeatWind(14, 0, 10),
		[][2]float64{{45, 12}, {135, 15}},
		[][2]float64{{135, 15}, {135, 18}, {135, 20}, {135, 18}, {135, 17}, {135, 16}, {135, 15}, {135, 16}, {135, 17}, {135, 18}},
	))
	now := pts[0].Time
	got := detectWindChanges(pts, now, loc, 0)
	if len(got) != 1 {
		t.Fatalf("changes = %+v, want 1", got)
	}
	c := got[0]
	if c.From != "N" || c.To != "SE" {
		t.Errorf("from/to = %s/%s, want N/SE", c.From, c.To)
	}
	if c.Label != "3 PM" {
		t.Errorf("label = %q, want 3 PM", c.Label)
	}
	if c.DayKey != "2026-11-14" {
		t.Errorf("day key = %q", c.DayKey)
	}
	if c.SpeedMinKn != 15 || c.SpeedMaxKn != 20 {
		t.Errorf("speed after = %d-%d, want 15-20", c.SpeedMinKn, c.SpeedMaxKn)
	}
	if c.LowerConfidence {
		t.Error("a change this soon is not lower confidence")
	}
	if c.Prominent {
		t.Error("nothing is prominent when no limit is set")
	}
}

func TestDetectWindChangesCrossesDayBoundary(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(
		repeatWind(26, 0, 10),
		repeatWind(14, 180, 14),
	))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 {
		t.Fatalf("changes = %+v, want 1", got)
	}
	if got[0].DayKey != "2026-11-15" || got[0].From != "N" || got[0].To != "S" {
		t.Errorf("change = %+v", got[0])
	}
}

func TestDetectWindChangesIsHemisphereNeutral(t *testing.T) {
	loc := queenslandLoc(t)
	// S to NW: a veer in the north, something the detector has no opinion on.
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 180, 12), repeatWind(12, 315, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].From != "S" || got[0].To != "NW" {
		t.Fatalf("changes = %+v", got)
	}
}

func TestDetectWindChangesIgnoresSlowTurn(t *testing.T) {
	loc := queenslandLoc(t)
	// 0 to 75 over 6 hours, then it holds: the winds before and after are
	// less than 90 degrees apart, so the veer is not a change.
	var hours [][2]float64
	for i := 0; i < 6; i++ {
		hours = append(hours, [2]float64{float64(i) * 15, 12})
	}
	hours = joinWind(repeatWind(8, 0, 12), hours, repeatWind(12, 75, 12))
	pts := windSeriesFrom(loc, hours)
	if got := detectWindChanges(pts, pts[0].Time, loc, 0); len(got) != 0 {
		t.Fatalf("a slow drift is not a change: %+v", got)
	}
}

func TestDetectWindChangesIgnoresSwingThatDoesNotHold(t *testing.T) {
	loc := queenslandLoc(t)
	// South-east for 4 hours, then back to north.
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 12), repeatWind(4, 135, 12), repeatWind(12, 0, 12)))
	if got := detectWindChanges(pts, pts[0].Time, loc, 0); len(got) != 0 {
		t.Fatalf("a swing that lasts 4 hours is not a change: %+v", got)
	}
}

func TestDetectWindChangesIgnoresOneHourSpike(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 12), repeatWind(1, 180, 12), repeatWind(12, 0, 12)))
	if got := detectWindChanges(pts, pts[0].Time, loc, 0); len(got) != 0 {
		t.Fatalf("a one-hour spike is not a change, and neither is its return: %+v", got)
	}
}

func TestDetectWindChangesNeedsSixHoursOfForecastAfter(t *testing.T) {
	loc := queenslandLoc(t)
	// The forecast ends 3 hours after the swing, so it cannot be shown to hold.
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 12), repeatWind(3, 135, 12)))
	if got := detectWindChanges(pts, pts[0].Time, loc, 0); len(got) != 0 {
		t.Fatalf("unconfirmed change reported: %+v", got)
	}
}

func TestDetectWindChangesFindsSeveralAcrossTenDays(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(
		repeatWind(30, 0, 10),
		repeatWind(40, 135, 16),
		repeatWind(60, 270, 12),
	))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 2 {
		t.Fatalf("changes = %+v, want 2", got)
	}
	if got[0].To != "SE" || got[1].From != "SE" || got[1].To != "W" {
		t.Errorf("changes = %+v", got)
	}
}

func TestDetectWindChangesLowerConfidenceBeyondThreeDays(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(24, 0, 10), repeatWind(24, 0, 10), repeatWind(24, 0, 10), repeatWind(24, 0, 10), repeatWind(24, 135, 14)))
	now := pts[0].Time
	got := detectWindChanges(pts, now, loc, 0)
	if len(got) != 1 || !got[0].LowerConfidence {
		t.Fatalf("a change on day 5 is lower confidence: %+v", got)
	}
	pts = windSeriesFrom(loc, joinWind(repeatWind(24, 0, 10), repeatWind(24, 135, 14)))
	got = detectWindChanges(pts, now, loc, 0)
	if len(got) != 1 || got[0].LowerConfidence {
		t.Fatalf("a change on day 2 is not lower confidence: %+v", got)
	}
}

func TestDetectWindChangesProminentFromOperatorLimit(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 8), repeatWind(12, 135, 16)))
	now := pts[0].Time
	if got := detectWindChanges(pts, now, loc, 14); len(got) != 1 || !got[0].Prominent {
		t.Fatalf("16 kn after a 14 kn marginal wind is prominent: %+v", got)
	}
	if got := detectWindChanges(pts, now, loc, 18); len(got) != 1 || got[0].Prominent {
		t.Fatalf("16 kn under an 18 kn marginal wind is quiet: %+v", got)
	}
	if got := detectWindChanges(pts, now, loc, 0); len(got) != 1 || got[0].Prominent {
		t.Fatalf("no limit, nothing prominent: %+v", got)
	}
}

// Prominence is judged on the speed shown, as the plan table does: 19.5 kn is
// shown as 20 kn and is at a 20 kn marginal wind; 19.4 kn is shown as 19.
func TestDetectWindChangesProminentComparesTheRoundedMax(t *testing.T) {
	loc := queenslandLoc(t)
	at := func(kn float64) []windChangePoint {
		return windSeriesFrom(loc, joinWind(repeatWind(10, 0, 8), repeatWind(12, 135, kn)))
	}
	if got := detectWindChanges(at(19.5), at(19.5)[0].Time, loc, 20); len(got) != 1 || !got[0].Prominent || got[0].SpeedMaxKn != 20 {
		t.Fatalf("19.5 kn shows as 20 and is at a 20 kn marginal wind: %+v", got)
	}
	if got := detectWindChanges(at(19.4), at(19.4)[0].Time, loc, 20); len(got) != 1 || got[0].Prominent {
		t.Fatalf("19.4 kn shows as 19 and is under a 20 kn marginal wind: %+v", got)
	}
}

// Each settled wind is measured against the wind the last change ended on, not
// against its neighbour.
func TestDetectWindChangesMeasuresFromTheAnchorNotTheNeighbour(t *testing.T) {
	loc := queenslandLoc(t)
	// N 12 h, ENE 6 h (60 degrees round), then SSE 24 h. ENE to SSE is only 90 from
	// the neighbour; N to SSE is 150, and N is the wind the forecast started on.
	pts := windSeriesFrom(loc, joinWind(repeatWind(12, 0, 12), repeatWind(6, 60, 12), repeatWind(24, 150, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].From != "N" || got[0].To != "SSE" {
		t.Fatalf("one change N to SSE: %+v", got)
	}
}

func TestDetectWindChangesStepwiseVeerIsOneChangeWhenTheTurnReachesNinety(t *testing.T) {
	loc := queenslandLoc(t)
	// 0, 60, 120, 180 in 6 h steps. The turn from the anchor first reaches 90 at
	// 120 (N to ESE). 180 is only 60 from the new anchor, so it is no second change.
	pts := windSeriesFrom(loc, joinWind(repeatWind(12, 0, 12), repeatWind(6, 60, 12), repeatWind(6, 120, 12), repeatWind(24, 180, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].From != "N" || got[0].To != "ESE" || got[0].Label != "6 PM" {
		t.Fatalf("one change N to ESE at 6 PM, none after: %+v", got)
	}
}

// Half-hour zones: the hour that has begun is not yet past, in the boat's clock.
func TestDetectWindChangesKeepsAChangeAtTheStartOfTheCurrentLocalHour(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Adelaide")
	if err != nil {
		t.Skip("no tz database")
	}
	pts := windSeriesFrom(loc, joinWind(repeatWind(9, 0, 12), repeatWind(14, 135, 12)))
	now := time.Date(2026, 11, 14, 9, 40, 0, 0, loc) // the change sets in at 9:00
	got := detectWindChanges(pts, now, loc, 0)
	if len(got) != 1 || got[0].Label != "9 AM" {
		t.Fatalf("a change at 9 AM is still news at 9:40 AM: %+v", got)
	}
}

func TestDetectWindChangesDropsChangesBeforeNow(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 12), repeatWind(20, 135, 12)))
	now := pts[0].Time.Add(20 * time.Hour)
	if got := detectWindChanges(pts, now, loc, 0); len(got) != 0 {
		t.Fatalf("a change already past is not news: %+v", got)
	}
}

func TestDetectWindChangesSkipsCalmAndUnknownDirection(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 12), repeatWind(2, -1, 1), repeatWind(12, 135, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].From != "N" || got[0].To != "SE" {
		t.Fatalf("a calm gap between the two winds is still one change: %+v", got)
	}
}

func TestLowestMarginalWind(t *testing.T) {
	ten, twenty := 10.0, 20.0
	if got := lowestMarginalWind(passageLimits{}); got != 0 {
		t.Errorf("no limits, no marginal wind, got %v", got)
	}
	got := lowestMarginalWind(passageLimits{Ahead: passageSectorLimits{WindKnNoGo: &twenty}, Abeam: passageSectorLimits{WindKnNoGo: &ten}})
	if got != marginalOf(10) {
		t.Errorf("lowest marginal = %v, want %v", got, marginalOf(10))
	}
}

func TestWindChangesByDay(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(26, 0, 10), repeatWind(30, 180, 14)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	by := windChangesByDay(got)
	if len(by["2026-11-15"]) != 1 || len(by["2026-11-14"]) != 0 {
		t.Fatalf("by day = %+v", by)
	}
}

func TestDetectWindChangesCalmHourNearTheEndOfTheHoldDoesNotCancelIt(t *testing.T) {
	loc := queenslandLoc(t)
	// North for 14 h, south-east from 2 PM, a calm hour at 8 PM, south-east again after.
	hours := joinWind(repeatWind(14, 0, 10), repeatWind(6, 135, 15), repeatWind(1, -1, 1), repeatWind(10, 135, 15))
	pts := windSeriesFrom(loc, hours)
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].From != "N" || got[0].To != "SE" || got[0].Label != "2 PM" {
		t.Fatalf("a calm hour inside the hold must not cancel the change: %+v", got)
	}
}

func TestDetectWindChangesLightAirBetweenTwoSettledWindsIsPartOfTheChange(t *testing.T) {
	loc := queenslandLoc(t)
	// North 10 h, then 8 h of light air from the west, then south-east 12 h. The
	// time is when the south-east sets in, hour 18 (6 PM).
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 10), repeatWind(8, 270, 3), repeatWind(12, 135, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].From != "N" || got[0].To != "SE" || got[0].Label != "6 PM" {
		t.Fatalf("light air between two settled winds is part of one change: %+v", got)
	}
}

func TestDetectWindChangesTurnsDatedFromTheFirstFirmHourOfTheNewWind(t *testing.T) {
	loc := queenslandLoc(t)
	// North 10 h, then 2 h of south at 8 kn on the way to south-east, which
	// settles for 12 h. The new wind set in with the south, at 10 AM.
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 10), repeatWind(2, 190, 8), repeatWind(12, 135, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 || got[0].To != "SE" || got[0].Label != "10 AM" {
		t.Fatalf("the change is dated from when the turn began: %+v", got)
	}
}

func TestDetectWindChangesTooMuchUnsettledAirBetweenIsNotOneChange(t *testing.T) {
	loc := queenslandLoc(t)
	// 20 h of light air from the east between the two winds is more than the
	// 12 h a change may take, so the second wind is a new regime, not a change.
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 10), repeatWind(20, 90, 3), repeatWind(12, 180, 12)))
	if got := detectWindChanges(pts, pts[0].Time, loc, 0); len(got) != 0 {
		t.Fatalf("20 h of light air between is not part of a change: %+v", got)
	}
}

func TestDetectWindChangesBurstInsideASettledWindIsNotAChange(t *testing.T) {
	loc := queenslandLoc(t)
	// A 4 h south-west burst at 14 kn inside a north wind, which carries on.
	pts := windSeriesFrom(loc, joinWind(repeatWind(12, 340, 8), repeatWind(4, 225, 14), repeatWind(12, 340, 8)))
	if got := detectWindChanges(pts, pts[0].Time, loc, 0); len(got) != 0 {
		t.Fatalf("a burst that comes back is not a change: %+v", got)
	}
}

// mooloolabaForecastHourly is the live forecast response captured on
// 2026-11-14 for Mooloolaba, as the provider's hourly points: the API's knots
// back to m/s, and each hour dated from its day_key and hour_of_day on the
// boat's clock.
func mooloolabaForecastHourly(t *testing.T, loc *time.Location) []weatherHourPoint {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "forecast-mooloolaba-2026-11-14.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Days []struct {
			DayKey     string `json:"day_key"`
			HourlyWind []struct {
				HourOfDay        int      `json:"hour_of_day"`
				WindSpeedKts     float64  `json:"wind_speed_kts"`
				WindDirectionDeg *float64 `json:"wind_direction_deg"`
			} `json:"hourly_wind"`
		} `json:"days"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	var out []weatherHourPoint
	for _, d := range payload.Days {
		midnight, err := time.ParseInLocation("2006-01-02", d.DayKey, loc)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range d.HourlyWind {
			deg := -1.0
			if h.WindDirectionDeg != nil {
				deg = *h.WindDirectionDeg
			}
			out = append(out, weatherHourPoint{
				Time:             midnight.Add(time.Duration(h.HourOfDay) * time.Hour),
				WindSpeedMS:      h.WindSpeedKts / metersPerSecondToKnots,
				WindDirectionDeg: deg,
			})
		}
	}
	if len(out) < 200 {
		t.Fatalf("fixture has %d hours, want the 10-day series", len(out))
	}
	return out
}

// The live Mooloolaba forecast: NNW 5-10 kn through Monday, a 4 h south-west
// burst and light-air wobbles on Monday, light and variable air from about
// midnight to 6 AM Tuesday, then south and south-east from 7 AM, holding
// through Wednesday. That is one change, on Tuesday.
func TestDetectWindChangesFindsTuesdaysChangeInTheLiveMooloolabaForecast(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := mooloolabaForecastHourly(t, loc)
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, loc)
	scan := forecastWindChanges(hourly, time.Time{}, now, loc, passageLimits{Ahead: passageSectorLimits{WindKnNoGo: pwF(20)}}, nil)
	got := scan.Changes
	if scan.Problem != "" {
		t.Fatalf("problem = %q", scan.Problem)
	}
	if scan.CheckedTo != "" {
		t.Errorf("the live forecast is hourly to its end, so nothing is cut short: %q", scan.CheckedTo)
	}
	if len(got) != 1 {
		t.Fatalf("changes = %+v, want exactly the Tuesday change", got)
	}
	c := got[0]
	t.Logf("change: %s -> %s at %s %s, %d-%d kn", c.From, c.To, c.DayLabel, c.Label, c.SpeedMinKn, c.SpeedMaxKn)
	if c.DayKey != "2026-11-17" {
		t.Errorf("day = %q, want 2026-11-17 (nothing on Sun 15 or Mon 16)", c.DayKey)
	}
	if c.From != "NNW" && c.From != "N" {
		t.Errorf("from = %q, want NNW or N", c.From)
	}
	if c.To != "SE" {
		t.Errorf("to = %q, want SE", c.To)
	}
	switch c.Label {
	case "7 AM", "8 AM", "9 AM", "10 AM":
	default:
		t.Errorf("label = %q, want the south setting in at 7 AM to 10 AM", c.Label)
	}
	if c.SpeedMinKn < 6 || c.SpeedMinKn > 9 || c.SpeedMaxKn < 12 || c.SpeedMaxKn > 14 {
		t.Errorf("speed after = %d-%d kn, want about 8-13", c.SpeedMinKn, c.SpeedMaxKn)
	}
	if c.DayLabel != "Tue 17" {
		t.Errorf("day label = %q", c.DayLabel)
	}
}

// forecastWithWindChange serves GET /api/weather-forecast for a provider whose
// hourly wind turns north to south-east 20 hours from now, with the given
// extra settings text, and returns the decoded response.
func forecastWithWindChange(t *testing.T, extraSettings string) (weatherForecastResponse, int) {
	t.Helper()
	return forecastWithHourly(t, extraSettings, func(start time.Time) []weatherHourPoint { return windChangeHourly(start, 60, 8) })
}

// forecastWithHourly is forecastWithWindChange for a provider whose hourly
// points are whatever build makes from the start of the current hour.
func forecastWithHourly(t *testing.T, extraSettings string, build func(start time.Time) []weatherHourPoint) (weatherForecastResponse, int) {
	t.Helper()
	withCleanWeatherProviderRegistry(t)
	loc := queenslandLoc(t)
	start := time.Now().In(loc).Truncate(time.Hour)
	hourly := build(start)
	midnight := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	var days []weatherDayPoint
	for d := 0; d < 4; d++ {
		days = append(days, weatherDayPoint{Start: midnight.AddDate(0, 0, d), Condition: "clear", TempMaxC: 24, TempMinC: 15, WindSpeedMS: 5})
	}
	registerWeatherProvider(&stubWeatherProvider{id: "open-meteo", name: "Open-Meteo", ttl: 900, bundle: weatherForecastBundle{Days: days, Hourly: hourly, CachedAt: time.Now().UTC()}})
	server := trustedSignalKPayloadServer(t, -27.4, 153.0)
	defer server.Close()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := fmt.Sprintf("signalk:\n  address: %s\n  port: 0\nui:\n  weather_provider: open-meteo\n%s", server.URL, extraSettings)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", path)
	rec := httptest.NewRecorder()
	if err := weatherForecast(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/weather-forecast", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var payload weatherForecastResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
	}
	return payload, rec.Code
}

func allDayChanges(p weatherForecastResponse) []windChangeResponse {
	var out []windChangeResponse
	for _, d := range p.Days {
		out = append(out, d.WindChanges...)
	}
	return out
}

func TestWeatherForecast_MarksProminentChangeFromPassageLimits(t *testing.T) {
	p, code := forecastWithWindChange(t, "assistant:\n  passage_limits:\n    ahead:\n      wind_kn_no_go: 20\n")
	changes := allDayChanges(p)
	if code != http.StatusOK || len(changes) != 1 || !changes[0].Prominent || p.WindChangesProblem != "" {
		t.Fatalf("code %d changes %+v problem %q", code, changes, p.WindChangesProblem)
	}
}

func TestWeatherForecast_UnreadableLimitsKeepTheForecastAndSayWhy(t *testing.T) {
	p, code := forecastWithWindChange(t, "assistant:\n  passage_limits: nonsense\n")
	changes := allDayChanges(p)
	if code != http.StatusOK {
		t.Fatalf("the forecast does not depend on limits: %d", code)
	}
	if len(changes) != 1 || changes[0].Prominent {
		t.Fatalf("nothing is prominent when limits can't be read: %+v", changes)
	}
	if p.WindChangesProblem == "" {
		t.Fatal("the reason must reach the response")
	}
}

func TestWindChangeDayLabel(t *testing.T) {
	loc := queenslandLoc(t)
	now := time.Date(2026, 11, 14, 9, 0, 0, 0, loc)
	cases := []struct {
		at   time.Time
		want string
	}{
		{time.Date(2026, 11, 14, 23, 0, 0, 0, loc), "today"},
		{time.Date(2026, 11, 15, 1, 0, 0, 0, loc), "tomorrow"},
		{time.Date(2026, 11, 17, 14, 0, 0, 0, loc), "Tue 17"},
	}
	for _, c := range cases {
		if got := windChangeDayLabel(c.at, now, loc); got != c.want {
			t.Errorf("%v = %q, want %q", c.at, got, c.want)
		}
	}
}

// hourlyThenCoarse is hourly points for the first 72 hours, north then
// south-east from hour 41, and then 3-hourly points to hour 240 that turn west.
func hourlyThenCoarse(loc *time.Location) []weatherHourPoint {
	start := time.Date(2026, 11, 14, 0, 0, 0, 0, loc)
	ms := func(kn float64) float64 { return kn / metersPerSecondToKnots }
	var out []weatherHourPoint
	for h := 0; h < 72; h++ {
		hp := weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WindSpeedMS: ms(10), WindDirectionDeg: 0}
		if h >= 41 {
			hp.WindSpeedMS, hp.WindDirectionDeg = ms(16), 135
		}
		out = append(out, hp)
	}
	for h := 75; h <= 240; h += 3 {
		out = append(out, weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WindSpeedMS: ms(14), WindDirectionDeg: 270})
	}
	return out
}

func TestForecastWindChangesStopsWhereTheSeriesStopsBeingHourly(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := hourlyThenCoarse(loc)
	scan := forecastWindChanges(hourly, time.Time{}, hourly[0].Time, loc, passageLimits{}, nil)
	if len(scan.Changes) != 1 || scan.Changes[0].From != "N" || scan.Changes[0].To != "SE" {
		t.Fatalf("only the change in the hourly stretch is found, not the one on the 3-hourly tail: %+v", scan.Changes)
	}
	if scan.CheckedTo != "Mon 16 11 PM" || scan.CheckedReason != "the forecast is no longer hourly" {
		t.Errorf("checked to %q because %q, want Mon 16 11 PM (the last hourly point) because the forecast is no longer hourly", scan.CheckedTo, scan.CheckedReason)
	}
}

// A wind that drifts under 90 degrees is not a new settled wind, but it is still
// the wind immediately before the next one: the gap before a new wind counts
// from where the settled wind just before it ended, not from the anchor's own
// last hour. From still names the anchor, the wind that set the pattern.
func TestDetectWindChangesCountsTheGapFromTheSettledWindJustBefore(t *testing.T) {
	loc := queenslandLoc(t)
	pts := windSeriesFrom(loc, joinWind(repeatWind(10, 0, 10), repeatWind(14, 60, 10), repeatWind(12, 135, 12)))
	got := detectWindChanges(pts, pts[0].Time, loc, 0)
	if len(got) != 1 {
		t.Fatalf("changes = %+v, want the one from N to SE", got)
	}
	if got[0].From != "N" || got[0].To != "SE" {
		t.Errorf("%s -> %s, want N -> SE (from names the anchor)", got[0].From, got[0].To)
	}
}

// hourlyWithGap is hourly points from 2026-11-14 00:00 that skip the hours
// between from and to, so the hour at from-1 is the last read.
func hourlyWithGap(loc *time.Location, total, skipFrom, skipTo int) []weatherHourPoint {
	start := time.Date(2026, 11, 14, 0, 0, 0, 0, loc)
	var out []weatherHourPoint
	for h := 0; h < total; h++ {
		if h >= skipFrom && h < skipTo {
			continue
		}
		out = append(out, weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WindSpeedMS: 10 / metersPerSecondToKnots, WindDirectionDeg: 0})
	}
	return out
}

func TestForecastWindChangesSaysAnHourIsSkippedForAShortGap(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := hourlyWithGap(loc, 120, 52, 53) // one hour missing: 3 AM read, then 5 AM
	scan := forecastWindChanges(hourly, time.Time{}, hourly[0].Time, loc, passageLimits{}, nil)
	if scan.CheckedTo != "Mon 16 3 AM" || scan.CheckedReason != "the forecast skips an hour here" {
		t.Errorf("checked to %q because %q", scan.CheckedTo, scan.CheckedReason)
	}
}

func TestForecastWindChangesSaysNoLongerHourlyForALongGap(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := hourlyWithGap(loc, 120, 52, 55) // 3 h missing between points 51 and 55
	scan := forecastWindChanges(hourly, time.Time{}, hourly[0].Time, loc, passageLimits{}, nil)
	if scan.CheckedTo != "Mon 16 3 AM" || scan.CheckedReason != "the forecast is no longer hourly" {
		t.Errorf("checked to %q because %q", scan.CheckedTo, scan.CheckedReason)
	}
}

func TestForecastWindChangesCountsAZeroTimeEntryAsAGap(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := hourlyWithGap(loc, 120, 0, 0)
	hourly[60].Time = time.Time{}
	scan := forecastWindChanges(hourly, time.Time{}, hourly[0].Time, loc, passageLimits{}, nil)
	if scan.CheckedTo != "Mon 16 11 AM" || scan.CheckedReason != "the forecast skips an hour here" {
		t.Errorf("a zero-time entry must not vanish: checked to %q because %q", scan.CheckedTo, scan.CheckedReason)
	}
}

func TestForecastWindChangesReportsACutInTheFinalDay(t *testing.T) {
	loc := queenslandLoc(t)
	start := time.Date(2026, 11, 14, 0, 0, 0, 0, loc)
	hourly := windChangeHourly(start, 20, 10)
	// One more entry two hours on, on the same day as the last hourly point.
	hourly = append(hourly, weatherHourPoint{Time: start.Add(21 * time.Hour), WindSpeedMS: 5, WindDirectionDeg: 135})
	scan := forecastWindChanges(hourly, time.Time{}, start, loc, passageLimits{}, nil)
	if scan.CheckedTo != "Sat 14 7 PM" || scan.CheckedReason != "the forecast skips an hour here" {
		t.Errorf("a cut inside the last day is still a cut: %q because %q", scan.CheckedTo, scan.CheckedReason)
	}
}

func TestForecastWindChangesReportsAnHourlyEndBeforeTheLastDay(t *testing.T) {
	loc := queenslandLoc(t)
	start := time.Date(2026, 11, 14, 0, 0, 0, 0, loc)
	hourly := windChangeHourly(start, 30, 10) // ends Sun 15 5 AM
	lastDay := start.AddDate(0, 0, 4)
	scan := forecastWindChanges(hourly, lastDay, start, loc, passageLimits{}, nil)
	if scan.CheckedTo != "Sun 15 5 AM" || scan.CheckedReason != "the forecast is no longer hourly" {
		t.Errorf("checked to %q because %q", scan.CheckedTo, scan.CheckedReason)
	}
	if scan := forecastWindChanges(hourly, start.AddDate(0, 0, 1), start, loc, passageLimits{}, nil); scan.CheckedTo != "" {
		t.Errorf("hourly to the last day's own day is read to the end: %q", scan.CheckedTo)
	}
}

func TestForecastWindChangesSaysNothingWhenTheSeriesIsHourlyToTheEnd(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := windChangeHourly(time.Date(2026, 11, 14, 0, 0, 0, 0, loc), 240, 10)
	if scan := forecastWindChanges(hourly, hourly[len(hourly)-1].Time, hourly[0].Time, loc, passageLimits{}, nil); scan.CheckedTo != "" || scan.CheckedReason != "" {
		t.Fatalf("the whole forecast was read: %+v", scan)
	}
}

func TestWeatherForecast_SaysWhereTheHourlyForecastStops(t *testing.T) {
	loc := queenslandLoc(t)
	p, code := forecastWithHourly(t, "", func(start time.Time) []weatherHourPoint {
		out := windChangeHourly(start, 30, 8)
		for h := 33; h <= 100; h += 3 {
			out = append(out, weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WindSpeedMS: 5, WindDirectionDeg: 270})
		}
		return out
	})
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	last := time.Now().In(loc).Truncate(time.Hour).Add(29 * time.Hour)
	want := last.Format("Mon 2") + " " + boatAxisClock(last, loc)
	if p.WindChangesCheckedTo != want || p.WindChangesCheckedReason != "the forecast is no longer hourly" {
		t.Errorf("checked to %q because %q, want %q because the forecast is no longer hourly", p.WindChangesCheckedTo, p.WindChangesCheckedReason, want)
	}
	if got := allDayChanges(p); len(got) != 1 {
		t.Errorf("the change in the hourly stretch is still listed: %+v", got)
	}
}

// The wire carries no relative day word: the client cached the response for up
// to an hour, and "tomorrow" is not true for that long. The day is the day the
// change sits under.
func TestWindChangeResponseCarriesNoRelativeDay(t *testing.T) {
	raw, err := json.Marshal(mapWindChangeResponses([]windChange{{DayLabel: "tomorrow", From: "N", To: "SE"}}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "day_label") || strings.Contains(string(raw), "tomorrow") {
		t.Fatalf("the response says a relative day: %s", raw)
	}
}

// The detector's clock is the wall clock, as the watch report's is, not the
// GNSS datetime: a fix that is days old must not bring a past change back.
func TestWeatherForecast_DetectorUsesTheWallClockNotAStaleFix(t *testing.T) {
	// The wall clock stands 30 h on, so the wind turning 20 h from the start of
	// the series is ten hours past. The GNSS datetime, which a stale fix would
	// leave behind, still sees it ahead.
	realStart := time.Now().Truncate(time.Hour)
	forecastNow = func() time.Time { return realStart.Add(30 * time.Hour) }
	t.Cleanup(func() { forecastNow = time.Now })
	p, code := forecastWithHourly(t, "", func(start time.Time) []weatherHourPoint {
		return windChangeHourly(start, 60, 8)
	})
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got := allDayChanges(p); len(got) != 0 {
		t.Errorf("a change ten hours ago is past by the wall clock: %+v", got)
	}
}

// A change on a day the forecast has no day card for is counted, never dropped
// without a word.
func TestWeatherForecast_CountsAChangeWithNoDayCard(t *testing.T) {
	p, code := forecastWithHourly(t, "", func(start time.Time) []weatherHourPoint {
		ms := func(kn float64) float64 { return kn / metersPerSecondToKnots }
		var out []weatherHourPoint
		for h := 0; h < 180; h++ {
			hp := weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WindSpeedMS: ms(10), WindDirectionDeg: 0}
			if h >= 130 {
				hp.WindDirectionDeg = 135
			}
			out = append(out, hp)
		}
		return out
	})
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if got := allDayChanges(p); len(got) != 0 {
		t.Fatalf("the test needs the change beyond the day cards: %+v", got)
	}
	if p.WindChangesUnplaced != 1 {
		t.Errorf("unplaced = %d, want 1", p.WindChangesUnplaced)
	}
}

// ── get_wind_forecast carries the changes (Mate) ──

// mateChangeTool runs get_wind_forecast over a north-to-south-east forecast at
// Brisbane. now is 2026-11-14 10.00 AM there. limits is the operator's passage
// limits, nil for a install that wires none.
func mateChangeTool(t *testing.T, days int, limits func() (passageLimits, error)) assistantWindForecastResult {
	t.Helper()
	loc := queenslandLoc(t)
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, loc)
	stub := &capturingWeatherProvider{bundle: weatherForecastBundle{Hourly: windChangeHourly(now, 60, 8), CachedAt: now}}
	deps := assistantToolDeps{
		now:           func() time.Time { return now },
		weather:       func() (weatherProvider, string, error) { return stub, "capturing-weather", nil },
		passageLimits: limits,
	}
	raw, _, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(fmt.Sprintf(`{"lat":-27.4,"lon":153.0,"days":%d}`, days)))
	if err != nil {
		t.Fatal(err)
	}
	var out assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	return out
}

func TestGetWindForecast_ReturnsTheWindChangesFromTheSharedDetector(t *testing.T) {
	limits := func() (passageLimits, error) {
		return passageLimits{Ahead: passageSectorLimits{WindKnNoGo: pwF(20)}}, nil
	}
	got := mateChangeTool(t, 3, limits)
	if len(got.WindChanges) != 1 {
		t.Fatalf("changes = %+v, want the one north-to-south-east change", got.WindChanges)
	}
	c := got.WindChanges[0]
	if c.From != "N" || c.To != "SE" || c.SpeedMinKn != 18 || c.SpeedMaxKn != 18 {
		t.Errorf("change = %+v, want N to SE at 18 kn", c)
	}
	if !strings.HasPrefix(c.Time, "Sun 15 Nov ") {
		t.Errorf("time = %q, want a boat-clock label on Sun 15 Nov", c.Time)
	}
	// 18 kn is past the 14 kn marginal wind (70% of the 20 kn no-go).
	if !c.Prominent || c.LowerConfidence {
		t.Errorf("change = %+v, want prominent and not lower confidence", c)
	}
	if got.WindChangesProblem != "" {
		t.Errorf("problem = %q", got.WindChangesProblem)
	}
}

func TestGetWindForecast_NoWindChangeIsAnEmptyListNotAMissingField(t *testing.T) {
	loc := queenslandLoc(t)
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, loc)
	var steady []weatherHourPoint
	for h := 0; h < 48; h++ {
		steady = append(steady, weatherHourPoint{Time: now.Add(time.Duration(h) * time.Hour), WindSpeedMS: 5, WindDirectionDeg: 0})
	}
	stub := &capturingWeatherProvider{bundle: weatherForecastBundle{Hourly: steady, CachedAt: now}}
	deps := assistantToolDeps{now: func() time.Time { return now }, weather: func() (weatherProvider, string, error) { return stub, "w", nil }}
	raw, _, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(`{"lat":-27.4,"lon":153.0}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"wind_changes":[]`) {
		t.Fatalf("the field must say none were found: %s", raw)
	}
}

func TestGetWindForecast_ChangesAreKeptWhenTheLimitsCannotBeReadAndNoneIsProminent(t *testing.T) {
	unreadable := func() (passageLimits, error) {
		return passageLimits{}, errors.New("assistant.passage_limits is unreadable")
	}
	got := mateChangeTool(t, 3, unreadable)
	if len(got.WindChanges) != 1 || got.WindChanges[0].Prominent {
		t.Fatalf("changes = %+v, want the change listed and not prominent", got.WindChanges)
	}
	if got.WindChangesProblem != "assistant.passage_limits is unreadable" {
		t.Errorf("problem = %q: Mate must be told the changes were not judged", got.WindChangesProblem)
	}
	// An install that wires no limits reader says so as well.
	if none := mateChangeTool(t, 3, nil); none.WindChangesProblem == "" || none.WindChanges[0].Prominent {
		t.Errorf("no limits reader: problem %q changes %+v", none.WindChangesProblem, none.WindChanges)
	}
}

func TestGetWindForecast_SaysSoWhenReadableLimitsSetNoWindNoGo(t *testing.T) {
	unset := func() (passageLimits, error) { return passageLimits{}, nil }
	got := mateChangeTool(t, 3, unset)
	if len(got.WindChanges) != 1 || got.WindChanges[0].Prominent {
		t.Fatalf("changes = %+v, want the change listed and not prominent", got.WindChanges)
	}
	if got.WindChangesProblem != "your passage limits aren't set (Settings → Mate → Passage limits)" {
		t.Errorf("problem = %q: Mate must be told the changes were not judged", got.WindChangesProblem)
	}
}

func TestGetWindForecast_SaysHowFarTheChangesWereCheckedWhenTheForecastStopsBeingHourly(t *testing.T) {
	loc := queenslandLoc(t)
	hourly := hourlyThenCoarse(loc)
	now := hourly[0].Time
	stub := &capturingWeatherProvider{bundle: weatherForecastBundle{Hourly: hourly, CachedAt: now}}
	deps := assistantToolDeps{now: func() time.Time { return now }, weather: func() (weatherProvider, string, error) { return stub, "w", nil }}
	raw, _, err := deps.execute(context.Background(), "get_wind_forecast", json.RawMessage(`{"lat":-27.4,"lon":153.0,"days":10}`))
	if err != nil {
		t.Fatal(err)
	}
	var out assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatal(err)
	}
	if out.WindChangesCheckedTo == "" || out.WindChangesCheckedReason != windChangeCutNotHourly {
		t.Fatalf("checked to %q because %q", out.WindChangesCheckedTo, out.WindChangesCheckedReason)
	}
	// Mate quotes the label as given, so it is the full form tools hand Mate
	// ("Mon 16 Nov 11.00 PM"), not the chart axis's "Mon 16 11 PM".
	if !regexp.MustCompile(`^[A-Z][a-z]{2} \d{1,2} [A-Z][a-z]{2} \d{1,2}\.\d{2} [AP]M$`).MatchString(out.WindChangesCheckedTo) {
		t.Fatalf("checked to %q, want the full day-and-clock form", out.WindChangesCheckedTo)
	}
}

func TestGetWindForecastDefinition_DescribesTheWindChanges(t *testing.T) {
	d := mateToolGetWindForecast.function.Description
	for _, want := range []string{"wind_changes", "wind_changes_problem", "wind_changes_checked_to", "prominent", "lower_confidence"} {
		if !strings.Contains(d, want) {
			t.Errorf("description does not mention %q", want)
		}
	}
}
