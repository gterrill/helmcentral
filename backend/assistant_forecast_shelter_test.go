package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Thomas Island South (ADR 0243): the bay faces south and the island lies to
// the north of the anchorage, so a northerly is sheltered and a southerly is
// not. The shoreline here is synthetic: a long east-west coast 0.002 degrees
// (about 0.12 nm) north of the point, open water everywhere else.
const (
	thomasLat = -20.5446
	thomasLon = 149.118933
)

func thomasShoreline() []shorelineLine {
	return []shorelineLine{{
		ID: "way/island", Kind: "coastline", Name: "Thomas Island",
		Points: []shorelinePoint{{Lat: thomasLat + 0.002, Lon: thomasLon - 0.05}, {Lat: thomasLat + 0.002, Lon: thomasLon + 0.05}},
	}}
}

// shelterForecastDeps holds the coastline squares around Thomas Island South
// (lines in the square that contains it) and a forecast with a northerly row
// at local 12:00, a southerly at 15:00 and an unknown-direction row at 18:00.
func shelterForecastDeps(t *testing.T, lines []shorelineLine, enabled bool) assistantToolDeps {
	t.Helper()
	store := newTestCoastlineStore(t)
	needed, err := squaresWithin(thomasLat, thomasLon, shelterMaxFetchNm)
	if err != nil {
		t.Fatal(err)
	}
	own, _ := squareContaining(thomasLat, thomasLon)
	for _, sq := range needed {
		if squareInland(sq) {
			continue
		}
		var l []shorelineLine
		if sq == own {
			l = lines
		}
		if err := store.SaveSquareAsOf(sq, l, time.Now(), coastlineTestDataAsOf); err != nil {
			t.Fatal(err)
		}
	}
	reader := &offlineCoastlineReader{Enabled: func() (bool, error) { return enabled, nil }, Store: store}
	now := time.Date(2026, 9, 12, 2, 0, 0, 0, time.UTC)
	wx := &capturingWeatherProvider{bundle: weatherForecastBundle{Hourly: []weatherHourPoint{
		{Time: now, WindSpeedMS: 5, WindDirectionDeg: 5},
		{Time: now.Add(3 * time.Hour), WindSpeedMS: 5, WindDirectionDeg: 180},
		{Time: now.Add(6 * time.Hour)},
	}}}
	wv := &stubWaveProvider{id: "wv", bundle: waveForecastBundle{}}
	return assistantToolDeps{
		now:              func() time.Time { return now },
		weather:          func() (weatherProvider, string, error) { return wx, "wx", nil },
		waves:            func() (waveProvider, string, error) { return wv, "wv", nil },
		offlineCoastline: func() *offlineCoastlineReader { return reader },
	}
}

func runShelterForecast(t *testing.T, d assistantToolDeps) (string, assistantWindForecastResult) {
	t.Helper()
	raw, _, err := d.execute(context.Background(), "get_wind_forecast", json.RawMessage(
		fmt.Sprintf(`{"lat":%f,"lon":%f,"days":1}`, thomasLat, thomasLon)))
	if err != nil {
		t.Fatalf("get_wind_forecast: %v", err)
	}
	var res assistantWindForecastResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	return raw, res
}

func TestGetWindForecast_LabelsRowsWithShelterFromTheWindDirection(t *testing.T) {
	_, res := runShelterForecast(t, shelterForecastDeps(t, thomasShoreline(), true))
	if len(res.Hourly) != 3 {
		t.Fatalf("rows = %d: %+v", len(res.Hourly), res.Hourly)
	}
	if got := res.Hourly[0].Shelter; got != shelterSheltered {
		t.Errorf("northerly row shelter = %q, want %q", got, shelterSheltered)
	}
	if got := res.Hourly[1].Shelter; got != shelterExposed {
		t.Errorf("southerly row shelter = %q, want %q", got, shelterExposed)
	}
	if got := res.Hourly[2].Shelter; got != "" {
		t.Errorf("a row with no wind direction gets no label, got %q", got)
	}
	if res.ShelterSummary == "" || !strings.Contains(res.ShelterSummary, "Sheltered") {
		t.Errorf("shelter_summary = %q", res.ShelterSummary)
	}
	if !strings.Contains(res.ShelterReading, "Offline coastline downloaded") {
		t.Errorf("shelter_reading = %q", res.ShelterReading)
	}
	if res.ShelterUnavailable != "" {
		t.Errorf("shelter_unavailable = %q", res.ShelterUnavailable)
	}
	if len(res.Days) != 1 || res.Days[0].ShelterPrevailing == nil || *res.Days[0].ShelterPrevailing != shelterSheltered {
		t.Errorf("day shelter_prevailing (tie goes to the first seen) = %+v", res.Days)
	}
}

func TestGetWindForecast_ShelterIsCompactNoBearingTable(t *testing.T) {
	raw, _ := runShelterForecast(t, shelterForecastDeps(t, thomasShoreline(), true))
	if strings.Contains(raw, "bearing_deg") || strings.Contains(raw, "fetch_nm") {
		t.Errorf("the 36-row bearing table must not ride along: %s", raw)
	}
}

func TestGetWindForecast_OpenWaterAddsNoShelterFields(t *testing.T) {
	raw, res := runShelterForecast(t, shelterForecastDeps(t, nil, true))
	if strings.Contains(raw, "shelter") {
		t.Errorf("open water must add nothing: %s", raw)
	}
	if res.Hourly[0].Shelter != "" {
		t.Errorf("label = %q", res.Hourly[0].Shelter)
	}
}

func TestGetWindForecast_CoastlineErrorIsReportedAndForecastStays(t *testing.T) {
	d := shelterForecastDeps(t, thomasShoreline(), false)
	_, res := runShelterForecast(t, d)
	if len(res.Hourly) != 3 {
		t.Fatalf("the forecast must still return: %+v", res.Hourly)
	}
	_, want := d.offlineCoastline().ShorelineAround(thomasLat, thomasLon)
	if want == nil || res.ShelterUnavailable != want.Error() {
		t.Errorf("shelter_unavailable = %q, want the error verbatim %v", res.ShelterUnavailable, want)
	}
	if res.ShelterSummary != "" || res.Hourly[0].Shelter != "" {
		t.Errorf("no labels when unavailable: %+v", res)
	}
}

func TestGetWindForecast_NoCoastlineOnInstallAddsNothing(t *testing.T) {
	d := shelterForecastDeps(t, thomasShoreline(), true)
	d.offlineCoastline = nil
	raw, _ := runShelterForecast(t, d)
	if strings.Contains(raw, "shelter") {
		t.Errorf("an install with no coastline reader adds nothing: %s", raw)
	}
}

func TestShelterGuard_LabelledForecastSatisfiesIt(t *testing.T) {
	d := shelterForecastDeps(t, thomasShoreline(), true)
	raw, _ := runShelterForecast(t, d)
	var c openRouterToolCall
	c.Function.Name = mateToolGetWindForecast.name
	c.Function.Arguments = `{"lat":-20.5446,"lon":149.118933,"course_deg":140}`
	calls := []openRouterToolCall{c}
	if got, _ := d.checkDraft(thomasIslandAnswer, calls, []string{raw}); got != "" {
		t.Errorf("a forecast that delivered shelter labels satisfies the guard, trigger = %q", got)
	}
	if got, _ := d.checkDraft(thomasIslandAnswer, calls, nil); got != "anchorage shelter" {
		t.Errorf("no results known: trigger = %q", got)
	}
}

func TestShelterGuard_UnavailableOpenWaterOrFailedForecastDoNotSatisfyIt(t *testing.T) {
	var c openRouterToolCall
	c.Function.Name = mateToolGetWindForecast.name
	c.Function.Arguments = `{"lat":-20.5446,"lon":149.118933}`
	calls := []openRouterToolCall{c}
	for name, d := range map[string]assistantToolDeps{
		"unavailable": shelterForecastDeps(t, thomasShoreline(), false),
		"open water":  shelterForecastDeps(t, nil, true),
	} {
		raw, _ := runShelterForecast(t, d)
		if got, _ := d.checkDraft(thomasIslandAnswer, calls, []string{raw}); got != "anchorage shelter" {
			t.Errorf("%s: trigger = %q, want the guard to fire", name, got)
		}
	}
	d := shelterForecastDeps(t, thomasShoreline(), true)
	if got, _ := d.checkDraft(thomasIslandAnswer, calls, []string{"error: get_wind_forecast: weather provider down"}); got != "anchorage shelter" {
		t.Errorf("failed forecast: trigger = %q", got)
	}
}

func TestGetWindForecastDefinition_DescribesShelterFields(t *testing.T) {
	d := mateToolGetWindForecast.function.Description
	for _, want := range []string{"shelter", "FROM", "estimate"} {
		if !strings.Contains(d, want) {
			t.Errorf("description lacks %q", want)
		}
	}
}
