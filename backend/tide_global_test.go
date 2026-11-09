package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// positionTideStub is a position-based tide source with heights from mean sea
// level, the shape of the open-meteo plugin. It records the station id the
// host passed, which must be the "<lat>,<lon>" of the position.
type positionTideStub struct {
	*stubTideProvider
	datum string
	gotID string
	calls int
}

func (s *positionTideStub) PositionBased() bool { return true }
func (s *positionTideStub) HeightDatum() string { return s.datum }
func (s *positionTideStub) FetchTideChart(id string) (tideChartResult, error) {
	s.gotID = id
	s.calls++
	return s.result, s.fetchErr
}

func newMSLStub(now time.Time) *positionTideStub {
	return &positionTideStub{
		stubTideProvider: &stubTideProvider{id: defaultTideProviderID, result: tideChartResult{
			Station: tideStation{StationID: "plugin-says", Name: "Plugin name"},
			Extremes: []tideExtremePoint{
				{Time: now.Add(-3 * time.Hour), HeightM: -0.9, High: false},
				{Time: now.Add(3 * time.Hour), HeightM: -0.1, High: true},
				{Time: now.Add(9 * time.Hour), HeightM: -0.8, High: false},
			},
			CurrentHeightM: -0.5, Direction: "Rising",
		}},
		datum: tideDatumMeanSeaLevel,
	}
}

// buildTideTodayHere is buildTideToday with the boat's fix read from the seam
// withVesselPosition sets, as the /api/tide-today handler reads it.
func buildTideTodayHere(path string, now time.Time) (tideTodayResponse, tideTodayETagData, error) {
	return buildTideToday(path, now, tideBoatFixNow)
}

func withVesselPosition(t *testing.T, lat, lon float64, err error) {
	t.Helper()
	prev := tideVesselState
	// Through the real trust rule, so -1,-1 and 0,0 are refused as in production.
	tideVesselState = func() (vesselStateData, error) { return vesselStateData{Latitude: lat, Longitude: lon}, err }
	t.Cleanup(func() { tideVesselState = prev })
}

func writeBlankTideSettings(t *testing.T) string {
	t.Helper()
	return writeResolveTideProviderSettings(t, `""`)
}

// ── default provider ───────────────────────────────────────────────────

func TestResolveTideProvider_BlankDefaultsToOpenMeteo(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))

	p, id, err := resolveTideProvider(writeBlankTideSettings(t))
	if err != nil {
		t.Fatalf("a blank ui.tide_provider must resolve to the default: %v", err)
	}
	if id != "open-meteo" || p.ID() != "open-meteo" {
		t.Fatalf("resolved %q / %q, want open-meteo", id, p.ID())
	}
}

func TestResolveTideProvider_BlankWithoutThePluginNamesPluginsTides(t *testing.T) {
	withCleanTideProviderRegistry(t)

	_, id, err := resolveTideProvider(writeBlankTideSettings(t))
	if err == nil {
		t.Fatal("expected an error when the default plugin is not installed")
	}
	if id != "open-meteo" {
		t.Errorf("id = %q, want the default open-meteo", id)
	}
	if !strings.Contains(err.Error(), "plugins/tides") || !strings.Contains(err.Error(), "open-meteo") {
		t.Errorf("error should name open-meteo and plugins/tides, got %q", err)
	}
}

func TestResolveTideProvider_AnExplicitChoiceIsUnchanged(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})
	registerTideProvider(newMSLStub(time.Now()))

	_, id, err := resolveTideProvider(writeResolveTideProviderSettings(t, "bom"))
	if err != nil || id != "bom" {
		t.Fatalf("got %q, %v; an explicit bom must stay bom", id, err)
	}
}

// ── tide today ─────────────────────────────────────────────────────────

func TestBuildTideToday_PositionBasedNeedsNoStationAndFollowsTheBoat(t *testing.T) {
	withCleanTideProviderRegistry(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	stub := newMSLStub(now)
	registerTideProvider(stub)
	withVesselPosition(t, 45.4312, 12.3378, nil)

	resp, _, err := buildTideTodayHere(writeBlankTideSettings(t), now)
	if err != nil {
		t.Fatalf("buildTideToday: %v", err)
	}
	if stub.gotID != "45.45,12.35" {
		t.Errorf("the plugin was asked for %q, want the position rounded to 2 decimals", stub.gotID)
	}
	if resp.Provider != "open-meteo" || resp.StationName != "Boat's position" {
		t.Errorf("provider %q, station name %q", resp.Provider, resp.StationName)
	}
	if resp.HeightDatum != tideDatumMeanSeaLevel {
		t.Errorf("height_datum = %q, want mean sea level", resp.HeightDatum)
	}
	// MSL heights are negative here and must come through as given.
	if resp.CurrentTideHeightFt == nil || resp.LowTideHeightFt == nil || *resp.CurrentTideHeightFt >= 0 || *resp.LowTideHeightFt >= 0 {
		t.Errorf("negative MSL heights were lost: %+v", resp)
	}
}

func TestBuildTideToday_PositionBasedWithNoFixIsAnExplicitError(t *testing.T) {
	for name, pos := range map[string][2]float64{"sentinel": {-1, -1}, "origin": {0, 0}, "off the globe": {95, 0}} {
		t.Run(name, func(t *testing.T) {
			withCleanTideProviderRegistry(t)
			stub := newMSLStub(time.Now())
			registerTideProvider(stub)
			withVesselPosition(t, pos[0], pos[1], nil)

			_, _, err := buildTideTodayHere(writeBlankTideSettings(t), time.Now().UTC())
			var te *tideTodayError
			if !errors.As(err, &te) || te.reason != tideNoPosition {
				t.Fatalf("want a tideNoPosition error, got %v", err)
			}
			if !strings.Contains(te.message, "position") {
				t.Errorf("the message must name the cause, got %q", te.message)
			}
			if stub.calls != 0 {
				t.Errorf("the plugin must not be asked without a position (%d calls)", stub.calls)
			}
		})
	}
}

func TestBuildTideToday_PositionBasedWhenTheBoatCannotBeReadIsAnExplicitError(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	withVesselPosition(t, 0, 0, errors.New("signalk down"))

	_, _, err := buildTideTodayHere(writeBlankTideSettings(t), time.Now().UTC())
	var te *tideTodayError
	if !errors.As(err, &te) || te.reason != tideNoPosition || !strings.Contains(te.message, "signalk down") {
		t.Fatalf("want a tideNoPosition error carrying the cause, got %v", err)
	}
}

func TestBuildTideToday_BlankProviderWithoutThePluginIsNotSetUp(t *testing.T) {
	withCleanTideProviderRegistry(t)

	_, _, err := buildTideTodayHere(writeBlankTideSettings(t), time.Now().UTC())
	var te *tideTodayError
	if !errors.As(err, &te) || te.reason != tideNoPlugin {
		t.Fatalf("want tideNoPlugin, got %v", err)
	}
	if !strings.Contains(te.message, "plugins/tides") {
		t.Errorf("the message must name plugins/tides, got %q", te.message)
	}
}

// Guard: an install that chose a station-based provider is unchanged. It still
// needs its station, and its heights are still chart datum.
func TestBuildTideToday_StationBasedStillRequiresAStation(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})
	withVesselPosition(t, 45.4, 12.3, nil)

	_, _, err := buildTideTodayHere(writeTideTodaySettings(t, "bom", `""`), time.Now().UTC())
	var te *tideTodayError
	if !errors.As(err, &te) || te.reason != tideNoStation || !strings.Contains(te.message, "station") {
		t.Fatalf("a station-based provider with no station must say so, got %v", err)
	}
}

func TestBuildTideToday_StationBasedHeightsAreChartDatum(t *testing.T) {
	withCleanTideProviderRegistry(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	registerTideProvider(&stubTideProvider{id: "bom", result: tideChartResult{
		Station:        tideStation{StationID: "S", Name: "Harbour"},
		Extremes:       []tideExtremePoint{{Time: now.Add(time.Hour), HeightM: 2, High: true}},
		CurrentHeightM: 1, Direction: "Rising",
	}})

	resp, _, err := buildTideTodayHere(writeTideTodaySettings(t, "bom", "S"), now)
	if err != nil {
		t.Fatal(err)
	}
	if resp.HeightDatum != tideDatumChart || resp.StationName != "Harbour" {
		t.Errorf("datum %q, station %q", resp.HeightDatum, resp.StationName)
	}
}

// A height is a reading when it is present, whatever its size. Exactly -1 ft is
// a height like any other, and a NOAA minus tide below chart datum is real.
func TestBuildTideToday_AnyHeightIsAReadingAndOnlyNullIsNone(t *testing.T) {
	withCleanTideProviderRegistry(t)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	stub := newMSLStub(now)
	stub.result.Extremes = []tideExtremePoint{
		{Time: now.Add(time.Hour), HeightM: -1 / metersToFeet, High: false},
	}
	stub.result.CurrentHeightM = -0.6 / metersToFeet
	registerTideProvider(stub)
	withVesselPosition(t, 45.4, 12.3, nil)

	resp, _, err := buildTideTodayHere(writeBlankTideSettings(t), now)
	if err != nil {
		t.Fatal(err)
	}
	if resp.LowTideHeightFt == nil || math.Abs(*resp.LowTideHeightFt+1) > 1e-9 {
		t.Errorf("exactly -1 ft is a real reading, got %v", resp.LowTideHeightFt)
	}
	if resp.CurrentTideHeightFt == nil || math.Abs(*resp.CurrentTideHeightFt+0.6) > 1e-9 {
		t.Errorf("-0.6 ft is a real reading, got %v", resp.CurrentTideHeightFt)
	}
	if resp.HighTideHeightFt != nil || resp.HighTideTime != "" {
		t.Errorf("no high to come is null and an empty time: %v %q", resp.HighTideHeightFt, resp.HighTideTime)
	}
	raw, _ := json.Marshal(resp)
	if !strings.Contains(string(raw), `"high_tide_height_ft":null`) {
		t.Errorf("none travels as null: %s", raw)
	}
}

// ── review fixes ───────────────────────────────────────────────────────

func TestTidePositionFromState_RefusesAFlaggedFix(t *testing.T) {
	good := vesselStateData{Latitude: 45.43, Longitude: 12.34}
	if lat, lon, err := tidePositionFromState(good, nil); err != nil || lat != 45.43 || lon != 12.34 {
		t.Fatalf("a good fix: %v %v %v", lat, lon, err)
	}
	flagged := good
	flagged.GNSSCriticalAlert = true
	if _, _, err := tidePositionFromState(flagged, nil); err == nil || !strings.Contains(err.Error(), "GNSS") {
		t.Fatalf("a flagged fix must be an explicit error naming the GNSS alert, got %v", err)
	}
}

func TestBuildTideToday_PositionBasedWithAFlaggedFixIsNoPosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	stub := newMSLStub(time.Now())
	registerTideProvider(stub)
	withVesselPosition(t, 0, 0, errors.New("the GNSS fix is flagged unreliable"))

	_, _, err := buildTideTodayHere(writeBlankTideSettings(t), time.Now().UTC())
	var te *tideTodayError
	if !errors.As(err, &te) || te.reason != tideNoPosition || stub.calls != 0 {
		t.Fatalf("want tideNoPosition and no plugin call, got %v (%d calls)", err, stub.calls)
	}
}

func TestTideNearest_PositionBasedUsesTheSameBoatPositionAsTheTide(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	withVesselPosition(t, 0, 0, errors.New("the GNSS fix is flagged unreliable"))

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest?provider=open-meteo", nil), rec)
	if err := tideNearestHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "position") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestTideNearest_PositionBasedWithABoatFixAnswersWithTheRoundedPoint(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	withVesselPosition(t, 45.4312, 12.3378, nil)

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest?provider=open-meteo", nil), rec)
	if err := tideNearestHandler(c); err != nil {
		t.Fatal(err)
	}
	var st tideStation
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.StationID != "45.45,12.35" || st.Name != "Boat's position" || st.Timezone != "Europe/Rome" {
		t.Fatalf("station = %+v", st)
	}
}

func TestTidePointStation_IsTheOneBuilder(t *testing.T) {
	st := tidePointStation(45.4312, 12.3378, "X")
	if st.StationID != "45.45,12.35" || st.Lat != 45.45 || st.Lon != 12.35 || st.Name != "X" || st.Timezone != "Europe/Rome" {
		t.Fatalf("station = %+v", st)
	}
	if neg := tidePointStation(-0.001, 5, "X"); neg.StationID != "0.00,5.00" {
		t.Errorf("negative zero must not print: %q", neg.StationID)
	}
}

func TestTideChart_PositionBasedUsesTheRoundedPointAndItsZoneNotThePluginsOwn(t *testing.T) {
	withCleanTideProviderRegistry(t)
	stub := newMSLStub(time.Now())
	stub.result.Station = tideStation{StationID: "x", Name: "Plugin name", Lat: 1, Lon: 2, Timezone: "Pacific/Auckland"}
	registerTideProvider(stub)

	q := url.Values{"provider": {"open-meteo"}, "station_id": {"45.4312,12.3378"}}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-chart?"+q.Encode(), nil), rec)
	if err := tideChartHandler(c); err != nil {
		t.Fatal(err)
	}
	var resp tideChartResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if stub.gotID != "45.45,12.35" {
		t.Errorf("the plugin was asked for %q, want the rounded point", stub.gotID)
	}
	s := resp.Station
	if s.StationID != "45.45,12.35" || s.Lat != 45.45 || s.Lon != 12.35 || s.Timezone != "Europe/Rome" {
		t.Errorf("station = %+v", s)
	}
}

func TestTideChart_PositionBasedRefusesAStationIDThatIsNotAPoint(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	q := url.Values{"provider": {"open-meteo"}, "station_id": {"AAC123"}}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-chart?"+q.Encode(), nil), rec)
	if err := tideChartHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestBuildLowWater_NoPositionIsItsOwnReason(t *testing.T) {
	state := vesselStateData{Depth: 5, DraftM: 1.5, DepthLastUpdateAge: 1}
	tide := func() (tideTodayResponse, error) {
		return tideTodayResponse{}, &tideTodayError{status: 502, reason: tideNoPosition, message: "no position"}
	}
	out, err := buildLowWater(state, tide, func() (float64, error) { return 0.5, nil }, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if out.Reason != lowWaterNoPosition {
		t.Fatalf("reason = %q, want %q (%+v)", out.Reason, lowWaterNoPosition, out)
	}
}

func TestResolveTideProvider_BlankWithoutThePluginDoesNotClaimTheOperatorConfiguredIt(t *testing.T) {
	withCleanTideProviderRegistry(t)
	_, _, err := resolveTideProvider(writeBlankTideSettings(t))
	if err == nil || strings.Contains(err.Error(), "unknown tide provider configured") {
		t.Fatalf("got %v", err)
	}
	for _, want := range []string{"no tide provider is chosen", "open-meteo", "plugins/tides", "Settings"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
	// An explicit but missing choice still says it was configured.
	_, _, err = resolveTideProvider(writeResolveTideProviderSettings(t, "nowhere"))
	if err == nil || !strings.Contains(err.Error(), "unknown tide provider configured") {
		t.Fatalf("explicit choice: %v", err)
	}
}

func TestCheckPlanningTide_AnyHeightIsAReadingAndItCarriesItsDatum(t *testing.T) {
	for _, h := range []float64{-1, -0.6, 0, 3.2} {
		for _, d := range []string{"", tideDatumChart, tideDatumMeanSeaLevel} {
			h, d := h, d
			got, stored, msg := checkPlanningTide(&h, &d)
			want := d
			if want == "" {
				want = tideDatumChart
			}
			if msg != "" || got == nil || *got != h || stored != want {
				t.Errorf("%v on %q: stored %q, %q", h, d, stored, msg)
			}
		}
	}
	if got, stored, msg := checkPlanningTide(nil, nil); got != nil || stored != "" || msg != "" {
		t.Errorf("no height stores no datum: %q %q", stored, msg)
	}
	huge := 900.0
	if _, _, msg := checkPlanningTide(&huge, nil); msg == "" {
		t.Error("an absurd height is refused")
	}
}

func TestPlanningTide_ARequestCanSayNoneWithNullAndAMinusTideIsStamped(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)

	code, resp := postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": nil})
	if code != http.StatusOK || resp["planning_tide_height_ft"] != nil || resp["planning_tide_datum"] != "" {
		t.Fatalf("null is none: %d %+v", code, resp)
	}
	if reason, _ := resp["datum_depth_reason"].(string); !strings.Contains(reason, "unknown") {
		t.Errorf("an unknown tide says so: %q", reason)
	}
	resetAnchorWatchState(t)
	code, resp = postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": -0.6})
	if code != http.StatusOK || resp["planning_tide_height_ft"] != -0.6 || resp["planning_tide_datum"] != tideDatumChart {
		t.Fatalf("-0.6 ft below chart datum is stamped: %d %+v", code, resp)
	}
	// Typed in the rode planner, so no transducer is needed: Save depth's figure uses the minus tide.
	code, resp = patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": 4.0, "planning_tide_height_ft": -0.6})
	if m, ok := resp["datum_depth_m"].(float64); code != http.StatusOK || !ok || math.Abs(m-(4.0+0.6/metersToFeet)) > 0.01 {
		t.Errorf("Save depth's figure uses the minus tide: %d %v", code, resp["datum_depth_m"])
	}
}

func TestPlanningTide_ExactMinusOneFootSurvivesAReload(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	if code, resp := postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": -1.0, "planning_tide_datum": "chart_datum"}); code != http.StatusOK {
		t.Fatalf("%d %+v", code, resp)
	}
	resetAnchorWatchState(t)
	if err := loadAnchorWatch(); err != nil {
		t.Fatal(err)
	}
	anchorWatchMu.RLock()
	got := anchorWatchState
	anchorWatchMu.RUnlock()
	if got == nil || got.PlanningTideHeightFt == nil || *got.PlanningTideHeightFt != -1 {
		t.Fatalf("a real -1.0 ft reading was read back as none: %+v", got)
	}
}

// The one place the old marker is read: a watch file written before holds -1
// with no datum for "no tide height".
func TestLoadAnchorWatch_AnOldFileHoldingMinusOneReadsAsNone(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	old := `{"lat":-21.1,"lon":149.2,"radius_meters":30,"set_at":"2026-10-01T00:00:00Z","planning_depth_m":4,"planning_tide_height_ft":-1,"planning_depth_source":"sounder"}`
	if err := os.WriteFile(os.Getenv("ANCHOR_WATCH_FILE"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := loadAnchorWatch(); err != nil {
		t.Fatal(err)
	}
	anchorWatchMu.RLock()
	got := anchorWatchState
	anchorWatchMu.RUnlock()
	if got == nil || got.PlanningTideHeightFt != nil || got.PlanningDepthM != 4 {
		t.Fatalf("the old -1 is none and the depth is kept: %+v", got)
	}
}

// ── nearest, chart and providers handlers ──────────────────────────────

func TestTideNearest_PositionBasedAnswersWithThePoint(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))

	q := url.Values{"provider": {"open-meteo"}, "lat": {"37.7749"}, "lon": {"-122.4194"}}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest-station?"+q.Encode(), nil), rec)
	if err := tideNearestHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var st tideStation
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if st.StationID != "37.75,-122.40" || st.Lat != 37.75 || st.Lon != -122.4 {
		t.Errorf("station = %+v", st)
	}
	if st.Name == "" || st.Name == "Boat's position" {
		t.Errorf("a named place is not the boat's position, got %q", st.Name)
	}
}

func TestTideNearest_PositionBasedWithoutAPointUsesTheBoatOrSaysWhyNot(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	withVesselPosition(t, -1, -1, nil)

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest-station?provider=open-meteo", nil), rec)
	if err := tideNearestHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "position") {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestTideChart_ResponseCarriesTheHeightDatum(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	registerTideProvider(&stubTideProvider{id: "bom", result: tideChartResult{Station: tideStation{StationID: "S"}}})

	for provider, want := range map[string]string{"open-meteo": tideDatumMeanSeaLevel, "bom": tideDatumChart} {
		q := url.Values{"provider": {provider}, "station_id": {"45.45,12.35"}}
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-chart?"+q.Encode(), nil), rec)
		if err := tideChartHandler(c); err != nil {
			t.Fatal(err)
		}
		var resp tideChartResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if resp.HeightDatum != want {
			t.Errorf("%s: height_datum = %q, want %q", provider, resp.HeightDatum, want)
		}
	}
}

func TestTideProviders_ListSaysWhichArePositionBasedAndTheirDatum(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})
	registerTideProvider(newMSLStub(time.Now()))

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-providers", nil), rec)
	if err := tideProvidersHandler(c); err != nil {
		t.Fatal(err)
	}
	var list []tideProviderInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].PositionBased || list[0].HeightDatum != tideDatumChart ||
		!list[1].PositionBased || list[1].HeightDatum != tideDatumMeanSeaLevel {
		t.Fatalf("providers = %+v", list)
	}
}

// ── datum depth ────────────────────────────────────────────────────────

func TestComputeDatumDepth_AMeanSeaLevelTideGivesNoDatumDepth(t *testing.T) {
	got, reason := computeDatumDepth(5, ptr(3.0), planningDepthFromOperator, 0, tideDatumMeanSeaLevel)
	if got != nil {
		t.Fatalf("a depth must not be worked out from mean-sea-level heights, got %v", *got)
	}
	if !strings.Contains(reason, "mean sea level") || !strings.Contains(reason, "chart datum") {
		t.Errorf("the reason must say why, got %q", reason)
	}
}

func TestComputeDatumDepth_AChartDatumTideStillGivesOne(t *testing.T) {
	for _, datum := range []string{"", tideDatumChart} {
		got, reason := computeDatumDepth(5, ptr(3.28084), planningDepthFromOperator, 0, datum)
		if got == nil || reason != "" {
			t.Fatalf("datum %q: got %v, %q", datum, got, reason)
		}
		if d := *got - 4; d < -0.001 || d > 0.001 {
			t.Errorf("datum %q: depth = %v, want 4", datum, *got)
		}
	}
}

// ── plugin metadata ────────────────────────────────────────────────────

func TestWasmTidePlugin_DeclaresPositionBasedAndMeanSeaLevel(t *testing.T) {
	p := mustNewWasmTideProvider(t, "testdata/tide_plugins/positionbased.wasm")
	if !tideProviderPositionBased(p) {
		t.Error("the fixture exports position_based and must be position-based")
	}
	if got := tideProviderHeightDatum(p); got != tideDatumMeanSeaLevel {
		t.Errorf("datum = %q, want mean sea level", got)
	}
	if st := p.SearchStations("", 10); len(st) != 0 {
		t.Errorf("a position-based plugin has no catalog, got %+v", st)
	}
}

func TestWasmTidePlugin_PassesThePositionAsTheStationID(t *testing.T) {
	withCleanTideProviderRegistry(t)
	p := mustNewWasmTideProvider(t, "testdata/tide_plugins/positionbased.wasm")
	res, err := positionTideAdapter{p}.FetchTideChart("45.4312,12.3378")
	if err != nil {
		t.Fatalf("FetchTideChart: %v", err)
	}
	// The plugin echoed the id it was given into its own station; the host
	// replaces that station with the point, so the id it sent is what is left.
	if res.Station.StationID != "45.45,12.35" || res.Station.Name != "Selected place" {
		t.Errorf("station = %+v", res.Station)
	}
	if len(res.Extremes) != 2 || res.Extremes[0].HeightM >= 0 {
		t.Errorf("negative mean-sea-level heights must come through as given: %+v", res.Extremes)
	}
}

func TestWasmTidePlugin_StationBasedFixtureIsStillChartDatum(t *testing.T) {
	p := mustNewWasmTideProvider(t, validFixtureWasm)
	if tideProviderPositionBased(p) {
		t.Error("a plugin without the export is station-based")
	}
	if got := tideProviderHeightDatum(p); got != tideDatumChart {
		t.Errorf("datum = %q, want chart datum when nothing is declared", got)
	}
}

func TestWasmTidePlugin_AnUnknownDatumFailsTheLoad(t *testing.T) {
	_, err := newWasmTideProvider("testdata/tide_plugins/baddatum.wasm")
	if err == nil || !strings.Contains(err.Error(), "height_datum") {
		t.Fatalf("a plugin naming a datum the host does not know must not load, got %v", err)
	}
}

// The shipped plugin, built by packaging/build-plugins.sh into plugins/tides.
// Skipped where it has not been built.
func TestShippedOpenMeteoTidePlugin(t *testing.T) {
	const path = "../plugins/tides/open-meteo.wasm"
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s not built (run packaging/build-plugins.sh)", path)
	}
	p := mustNewWasmTideProvider(t, path)
	if p.ID() != defaultTideProviderID {
		t.Errorf("id = %q, want the default %q", p.ID(), defaultTideProviderID)
	}
	if !tideProviderPositionBased(p) || tideProviderHeightDatum(p) != tideDatumMeanSeaLevel {
		t.Errorf("open-meteo must be position-based on mean sea level: %v %q", tideProviderPositionBased(p), tideProviderHeightDatum(p))
	}
	if st := p.SearchStations("", 10); len(st) != 0 {
		t.Errorf("no catalog expected, got %+v", st)
	}
	hosts, err := os.ReadFile("../plugins/tides/open-meteo.allowed_hosts.json")
	if err != nil || !strings.Contains(string(hosts), "marine-api.open-meteo.com") {
		t.Errorf("allowed hosts sidecar: %q, %v", hosts, err)
	}
}

// ── Mate ───────────────────────────────────────────────────────────────

func mslDeps(now time.Time, stub *positionTideStub) assistantToolDeps {
	return assistantToolDeps{
		now:   func() time.Time { return now },
		tides: func() (tideProvider, string, error) { return positionTideAdapter{stub}, stub.ID(), nil },
	}
}

func TestResolveTideStation_PositionBasedUsesThePlaceNotACatalog(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	stub := newMSLStub(now)
	st, err := mslDeps(now, stub).resolveTideStation(45.4312, 12.3378)
	if err != nil {
		t.Fatalf("resolveTideStation: %v", err)
	}
	if stub.gotID != "45.45,12.35" {
		t.Errorf("asked for %q", stub.gotID)
	}
	if st.info.DistanceNm != nil || st.info.BearingDeg != nil || st.loc == nil {
		t.Errorf("info %+v loc %v", st.info, st.loc)
	}
	if st.datum != tideDatumMeanSeaLevel {
		t.Errorf("datum = %q", st.datum)
	}
}

func TestGetTides_SaysTheHeightsAreFromMeanSeaLevel(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	raw, _, err := mslDeps(now, newMSLStub(now)).execute(context.Background(), "get_tides", json.RawMessage(`{"lat":45.43,"lon":12.34,"days":2}`))
	if err != nil {
		t.Fatalf("get_tides: %v", err)
	}
	var res struct {
		HeightDatum string `json:"height_datum"`
		DatumNote   string `json:"datum_note"`
		Reading     string `json:"reading"`
	}
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if res.HeightDatum != tideDatumMeanSeaLevel {
		t.Errorf("height_datum = %q", res.HeightDatum)
	}
	for _, want := range []string{"mean sea level", "not chart datum", "not a depth"} {
		if !strings.Contains(res.DatumNote, want) {
			t.Errorf("datum_note %q lacks %q", res.DatumNote, want)
		}
	}
	if strings.Contains(res.Reading, "0.0 nm from the place asked about, bearing") {
		t.Errorf("a modelled point at the place has no station to be 0.0 nm away: %q", res.Reading)
	}
}

func TestGetTides_ChartDatumResultStaysAsItWas(t *testing.T) {
	deps := tidalDeps(tidalFixtureExtremes(3, 20))
	raw, _, err := deps.execute(context.Background(), "get_tides", json.RawMessage(`{"lat":-27.0,"lon":153.0}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"height_datum":"chart_datum"`) || strings.Contains(raw, "datum_note") {
		t.Errorf("a chart-datum tide states its datum and needs no note: %s", raw)
	}
}

func TestAnchorageDepth_MeanSeaLevelTideSkipsTheChecksThatNeedChartDatum(t *testing.T) {
	d, _ := depthDeps(t)
	msl := &positionTideStub{stubTideProvider: anchorageTides(), datum: tideDatumMeanSeaLevel}
	d.tides = func() (tideProvider, string, error) { return positionTideAdapter{msl}, "open-meteo", nil }

	_, res := runAnchorage(t, d, anchorageWithDepth("4"))
	if res.Tide.Error != "" {
		t.Fatalf("the tide itself is fine: %s", res.Tide.Error)
	}
	if res.Tide.HeightDatum != tideDatumMeanSeaLevel || !strings.Contains(res.Tide.DatumNote, "mean sea level") || !res.Tide.Modelled {
		t.Errorf("the tide section must say what its heights are and that it is modelled: %+v", res.Tide.AnchorageTideBody)
	}
	for name, ck := range map[string]anchorageCheck{"clearance": res.Depth.Clearance.anchorageCheck, "rode": res.Depth.Rode.anchorageCheck, "swing": res.Depth.Swing.anchorageCheck} {
		if ck.Status != checkSkipped || !strings.Contains(ck.SkippedReason, "mean sea level") {
			t.Errorf("%s = %+v", name, ck)
		}
	}
}

func TestTidePositionFromState_RefusesTheSentinelPairAndAnError(t *testing.T) {
	withAnchorWatch(t, nil)
	if _, _, err := tidePositionFromState(vesselStateData{Latitude: -1, Longitude: -1}, nil); err == nil {
		t.Error("-1,-1 is not a position")
	}
	if _, _, err := tidePositionFromState(vesselStateData{Latitude: 45, Longitude: 12}, errors.New("signalk down")); err == nil || !strings.Contains(err.Error(), "signalk down") {
		t.Errorf("a failed read must carry its cause, got %v", err)
	}
}

func TestTidePoint_ABoatUnderWayStaysOnOneCacheEntry(t *testing.T) {
	a := tidePointStation(45.431, 12.341, "")
	b := tidePointStation(45.449, 12.359, "")
	if a.StationID != b.StationID || a.StationID != "45.45,12.35" {
		t.Errorf("points a kilometre or two apart share a point: %q vs %q", a.StationID, b.StationID)
	}
	if c := tidePointStation(45.5, 12.35, ""); c.StationID == a.StationID {
		t.Errorf("a point 5 km on is its own entry: %q", c.StationID)
	}
}

// ── the anchor watch's point ───────────────────────────────────────────

// withAnchorWatch sets the anchor watch, with no load error, for one test and
// puts both back afterwards: the tide's point comes from the watch, so a watch
// or load error left by an earlier test would otherwise decide it.
func withAnchorWatch(t *testing.T, w *anchorWatchData) {
	t.Helper()
	anchorWatchMu.Lock()
	prev, prevErr := anchorWatchState, anchorWatchLoadErr
	anchorWatchState, anchorWatchLoadErr = w, ""
	anchorWatchMu.Unlock()
	t.Cleanup(func() {
		anchorWatchMu.Lock()
		anchorWatchState, anchorWatchLoadErr = prev, prevErr
		anchorWatchMu.Unlock()
	})
}

// withAnchorWatchLoadErr sets the anchor watch's load error for one test, under
// the lock currentAnchorWatch reads it with, and puts it back afterwards.
func withAnchorWatchLoadErr(t *testing.T, msg string) {
	t.Helper()
	anchorWatchMu.Lock()
	prev := anchorWatchLoadErr
	anchorWatchLoadErr = msg
	anchorWatchMu.Unlock()
	t.Cleanup(func() {
		anchorWatchMu.Lock()
		anchorWatchLoadErr = prev
		anchorWatchMu.Unlock()
	})
}

func TestTidePosition_WithAWatchSetTheAnchorIsThePoint(t *testing.T) {
	withAnchorWatch(t, &anchorWatchData{Lat: 45.5, Lon: 12.4})
	// The boat is swinging on a line of the rounding grid; the anchor is not.
	lat, lon, err := tidePositionFromState(vesselStateData{Latitude: 45.4749, Longitude: 12.3749}, nil)
	if err != nil || lat != 45.5 || lon != 12.4 {
		t.Fatalf("got %v %v %v, want the anchor's 45.5 12.4", lat, lon, err)
	}
	// Even with no boat fix at all, the anchor is there to take the tide for.
	if lat, lon, err = tidePositionFromState(vesselStateData{GNSSCriticalAlert: true}, errors.New("signalk down")); err != nil || lat != 45.5 || lon != 12.4 {
		t.Fatalf("anchored with no fix: %v %v %v", lat, lon, err)
	}
}

func TestTidePosition_AWatchWithoutAUsableAnchorIsAnExplicitError(t *testing.T) {
	withAnchorWatch(t, &anchorWatchData{Lat: -1, Lon: -1})
	_, _, err := tidePositionFromState(vesselStateData{Latitude: 45.43, Longitude: 12.34}, nil)
	if err == nil || !strings.Contains(err.Error(), "anchor") {
		t.Fatalf("a watch with no anchor position must not fall back to the boat's fix, got %v", err)
	}
}

func TestTidePosition_WithNoWatchTheBoatsFixIsThePoint(t *testing.T) {
	withAnchorWatch(t, nil)
	lat, lon, err := tidePositionFromState(vesselStateData{Latitude: 45.43, Longitude: 12.34}, nil)
	if err != nil || lat != 45.43 || lon != 12.34 {
		t.Fatalf("got %v %v %v", lat, lon, err)
	}
}

func TestTidePosition_AnAdjustedAnchorIsReadFresh(t *testing.T) {
	w := &anchorWatchData{Lat: 45.5, Lon: 12.4}
	withAnchorWatch(t, w)
	if lat, _, _ := tidePositionFromState(vesselStateData{}, nil); lat != 45.5 {
		t.Fatalf("lat = %v", lat)
	}
	withAnchorWatch(t, &anchorWatchData{Lat: 45.6, Lon: 12.4}) // Adjust moved it
	if lat, _, _ := tidePositionFromState(vesselStateData{}, nil); lat != 45.6 {
		t.Fatalf("a moved anchor must be read fresh, got %v", lat)
	}
}

// ── the server picks the tide ──────────────────────────────────────────

func writeTideSettings(t *testing.T, yamlUI string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("ui:\n"+yamlUI), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", path)
}

func getConfiguredChart(t *testing.T, query string) (int, map[string]any) {
	t.Helper()
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-chart?"+query, nil), rec)
	if err := tideChartHandler(c); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("%v: %s", err, rec.Body)
	}
	return rec.Code, body
}

func TestConfiguredChart_BlankProviderIsTheDefaultAtTheBoatsPoint(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withAnchorWatch(t, nil)
	stub := newMSLStub(time.Now())
	registerTideProvider(stub)
	withVesselPosition(t, 45.4312, 12.3378, nil)
	writeTideSettings(t, "  tide_provider: \"\"\n")

	code, body := getConfiguredChart(t, "")
	if code != http.StatusOK {
		t.Fatalf("status %d: %v", code, body)
	}
	st := body["station"].(map[string]any)
	if stub.gotID != "45.45,12.35" || st["name"] != "Boat's position" || body["height_datum"] != tideDatumMeanSeaLevel ||
		body["position_based"] != true || body["provider"] != "open-meteo" || body["provider_name"] == "" {
		t.Errorf("asked %q, body %v", stub.gotID, body)
	}
}

func TestConfiguredChart_StationProviderGetsItsSavedStationAndNeverReadsThePosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom", result: tideChartResult{Station: tideStation{StationID: "S1", Name: "Harbour"}}})
	prev := tideBoatFixNow
	tideBoatFixNow = func() tideBoatFix {
		t.Error("a station provider must not read the boat's position")
		return tideBoatFix{}
	}
	t.Cleanup(func() { tideBoatFixNow = prev })
	writeTideSettings(t, "  tide_provider: bom\n  tide_station_id: S1\n")

	code, body := getConfiguredChart(t, "")
	if code != http.StatusOK || body["position_based"] != false || body["height_datum"] != tideDatumChart {
		t.Fatalf("status %d: %v", code, body)
	}
}

func TestConfiguredChart_StationProviderWithNoStationSaysNotSetUp(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})
	writeTideSettings(t, "  tide_provider: bom\n")

	code, body := getConfiguredChart(t, "")
	if code != http.StatusBadGateway || body["reason"] != string(tideNoStation) || !strings.Contains(body["error"].(string), "station") {
		t.Fatalf("status %d: %v", code, body)
	}
}

func TestConfiguredChart_ABlankProviderWithoutThePluginSaysNotSetUp(t *testing.T) {
	withCleanTideProviderRegistry(t)
	writeTideSettings(t, "  tide_provider: \"\"\n")
	code, body := getConfiguredChart(t, "")
	if code != http.StatusBadGateway || body["reason"] != string(tideNoPlugin) || !strings.Contains(body["error"].(string), "plugins/tides") {
		t.Fatalf("status %d: %v", code, body)
	}
}

func TestConfiguredChart_NoBoatPositionIsAnExplicitError(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withAnchorWatch(t, nil)
	registerTideProvider(newMSLStub(time.Now()))
	withVesselPosition(t, -1, -1, nil)
	writeTideSettings(t, "  tide_provider: \"\"\n")
	code, body := getConfiguredChart(t, "")
	if code != http.StatusBadGateway || body["reason"] != string(tideNoPosition) {
		t.Fatalf("status %d: %v", code, body)
	}
}

func TestConfiguredChart_APlaceUsesTheNearestStationOrThePoint(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom",
		stations: []tideStation{{StationID: "NEAR", Name: "Near Bar", Lat: -27.0, Lon: 153.0}},
		result:   tideChartResult{Station: tideStation{StationID: "NEAR", Name: "Near Bar", Lat: -27.0, Lon: 153.0}}})
	writeTideSettings(t, "  tide_provider: bom\n  tide_station_id: OTHER\n")
	code, body := getConfiguredChart(t, "lat=-27.1&lon=153.1")
	if code != http.StatusOK || body["station"].(map[string]any)["name"] != "Near Bar" {
		t.Fatalf("a station provider uses the station nearest the place, not the saved one: %d %v", code, body)
	}

	withCleanTideProviderRegistry(t)
	stub := newMSLStub(time.Now())
	registerTideProvider(stub)
	writeTideSettings(t, "  tide_provider: \"\"\n")
	code, body = getConfiguredChart(t, "lat=45.4312&lon=12.3378")
	if code != http.StatusOK || stub.gotID != "45.45,12.35" || body["position_based"] != true {
		t.Fatalf("a position-based provider uses the place's point: %d %q %v", code, stub.gotID, body)
	}
}

func TestTideToday_StationProviderNeverReadsTheBoatPosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	now := time.Now().UTC()
	registerTideProvider(&stubTideProvider{id: "bom", result: tideChartResult{
		Station: tideStation{StationID: "S1"}, Extremes: []tideExtremePoint{{Time: now.Add(time.Hour), HeightM: 2, High: true}}}})
	prev := tideBoatFixNow
	tideBoatFixNow = func() tideBoatFix { t.Error("read the position for a station provider"); return tideBoatFix{} }
	t.Cleanup(func() { tideBoatFixNow = prev })
	writeTideSettings(t, "  tide_provider: bom\n  tide_station_id: S1\n")

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-today", nil), rec)
	if err := tideToday(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%v %d %s", err, rec.Code, rec.Body)
	}
}

func TestSettingsSave_RefusesAStationForAProviderThatFollowsThePosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	var current, next settingsPayload
	next.UI.TideStationID = "45.45,12.35"
	if inv := validateSettingsChange(current, next); inv == nil || inv.Field != "ui.tide_station_id" {
		t.Fatalf("a position-based provider has no station to save, got %v", inv)
	}
	// An unchanged station (left over from an earlier provider) is not a new save.
	current.UI.TideStationID = "OLD"
	next.UI.TideStationID = "OLD"
	if inv := validateSettingsChange(current, next); inv != nil && inv.Field == "ui.tide_station_id" {
		t.Fatalf("an unchanged station must pass: %v", inv)
	}
	// A station provider still takes one.
	registerTideProvider(&stubTideProvider{id: "bom"})
	current.UI.TideStationID, next.UI.TideProvider, next.UI.TideStationID = "", "bom", "AAC1"
	if inv := validateSettingsChange(current, next); inv != nil && inv.Field == "ui.tide_station_id" {
		t.Fatalf("a station provider may save a station: %v", inv)
	}
}

func TestAutoStation_SkipsAProviderThatFollowsThePosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	stub := newMSLStub(time.Now())
	registerTideProvider(stub)
	writeTideSettings(t, "  tide_provider: \"\"\n  tide_auto_station: true\n")
	updateNearestTideStation()
	b, _ := os.ReadFile(os.Getenv("SETTINGS_FILE"))
	if strings.Contains(string(b), "tide_station_id: 4") || stub.calls != 0 {
		t.Fatalf("nothing synthetic may be saved: %s", b)
	}
}

func TestGetPluginInfo_WorksThroughThePositionAdapter(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withTestPluginOverridesStore(t)
	registerTideProvider(mustNewWasmTideProvider(t, "testdata/tide_plugins/positionbased.wasm"))
	if _, wrapped := tideProviderRegistry["position-fixture"].(positionTideAdapter); !wrapped {
		t.Fatal("a position-based plugin is wrapped at registration")
	}
	c, rec := newPluginTestEchoContext(http.MethodGet, "/api/plugins/tide/position-fixture", "", "tide", "position-fixture")
	if err := getPluginInfoHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("the adapter must not hide the plugin's path: %d %s", rec.Code, rec.Body)
	}
}

// ── third review ───────────────────────────────────────────────────────

func TestTidePluginNotInstalled_HasItsOwnReasonThroughTheTideLowWaterAndMate(t *testing.T) {
	withCleanTideProviderRegistry(t)
	_, _, _, te := configuredTideChart(map[string]any{}, tideBoatFixNow)
	if te == nil || te.reason != tideNoPlugin {
		t.Fatalf("a missing plugin is tideNoPlugin, got %+v", te)
	}
	// Mate's words do not call it a missing station.
	if s := tideUnavailableSentence(te); !strings.Contains(s, "plugin") || strings.Contains(s, "station") {
		t.Errorf("sentence = %q", s)
	}
	// The low-water check carries it as its own reason.
	state := vesselStateData{Depth: 5, DraftM: 1.5, DepthLastUpdateAge: 1}
	out, err := buildLowWater(state, func() (tideTodayResponse, error) { return tideTodayResponse{}, te }, func() (float64, error) { return 0.5, nil }, nil, time.Now())
	if err != nil || out.Reason != lowWaterNoPlugin {
		t.Fatalf("reason = %q (%v)", out.Reason, err)
	}
	if why := lowWaterWhy[lowWaterNoPlugin]; !strings.Contains(why, "plugin") {
		t.Errorf("why = %q", why)
	}
}

func TestTideNearest_AStationProviderUsesTheBoatsOwnFixNotTheAnchor(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom", stations: []tideStation{{StationID: "S", Name: "Bar", Lat: -27.0, Lon: 153.0}}})
	withAnchorWatch(t, &anchorWatchData{Lat: -1, Lon: -1}) // unusable anchor
	withVesselPosition(t, -27.1, 153.1, nil)

	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest?provider=bom", nil), rec)
	if err := tideNearestHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("a station provider must not fail on an unusable anchor: %d %s", rec.Code, rec.Body)
	}
}

func TestTidePointID_IsTheOneFormatter(t *testing.T) {
	if id := tidePointID(45.4312, 12.3378); id != "45.45,12.35" {
		t.Errorf("id = %q", id)
	}
	if id := tidePointID(-0.001, -0.001); id != "0.00,0.00" {
		t.Errorf("negative zero must not print: %q", id)
	}
	if tidePointStation(45.4312, 12.3378, "").StationID != tidePointID(45.4312, 12.3378) {
		t.Error("the station's id is the same formatter")
	}
}

func TestPlanTidalDeparture_AModelledTideHasNoDistanceAndSaysItsDatum(t *testing.T) {
	now := tidalFixtureLW0.Add(time.Hour)
	stub := &positionTideStub{
		stubTideProvider: &stubTideProvider{id: "open-meteo", result: tideChartResult{Extremes: tidalFixtureExtremes(-2, 40)}},
		datum:            tideDatumMeanSeaLevel,
	}
	deps := assistantToolDeps{now: func() time.Time { return now }, tides: func() (tideProvider, string, error) { return positionTideAdapter{stub}, "open-meteo", nil }}
	res := runTidalDeparture(t, deps, tidalArgs(180, 180, 6.2, tidalOneCycleWindow))
	if !res.Modelled || res.HeightDatum != tideDatumMeanSeaLevel {
		t.Fatalf("modelled %v datum %q", res.Modelled, res.HeightDatum)
	}
	if res.Station.DistanceNm != nil || res.Station.BearingDeg != nil {
		t.Errorf("a modelled tide has no distance or bearing: %+v", res.Station)
	}
	// And a station tide says it is not modelled, with its datum.
	res = runTidalDeparture(t, tidalDeps(tidalFixtureExtremes(-2, 40)), tidalArgs(180, 180, 6.2, tidalOneCycleWindow))
	if res.Modelled || res.HeightDatum != tideDatumChart {
		t.Errorf("modelled %v datum %q", res.Modelled, res.HeightDatum)
	}
}

// ── the calibration's source ───────────────────────────────────────────

// The calibration stores a tide by (provider id, station id). With a watch set,
// a position-based provider's station id is the anchor's rounded point, so a
// boat swinging across a rounding line does not start another source. Only a
// moved anchor (Adjust) does.
func TestTideCalSource_PositionBasedFollowsTheAnchorNotTheSwingingBoat(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(newMSLStub(time.Now()))
	writeTideSettings(t, "  tide_provider: \"\"\n")
	withAnchorWatch(t, &anchorWatchData{Lat: 45.51, Lon: 12.41})

	sourceAtBoat := func(lat, lon float64) string {
		withVesselPosition(t, lat, lon, nil)
		c, err := liveTideCalChart()
		if err != nil {
			t.Fatal(err)
		}
		if !c.PositionBased {
			t.Fatal("the chart must say it follows a position")
		}
		return c.Source
	}
	// 45.475 is a rounding line of the 0.05 degree grid.
	west, east := sourceAtBoat(45.474, 12.41), sourceAtBoat(45.476, 12.41)
	if west != east || west != "open-meteo/45.50,12.40" {
		t.Fatalf("the boat crossing a rounding line changed the source: %q then %q", west, east)
	}

	withAnchorWatch(t, &anchorWatchData{Lat: 45.60, Lon: 12.41}) // Adjust moved it
	if moved := sourceAtBoat(45.476, 12.41); moved == east || moved != "open-meteo/45.60,12.40" {
		t.Fatalf("a moved anchor is another source, got %q", moved)
	}
}

func TestTideCalSource_StationProviderKeepsItsSavedStationWhateverTheAnchorDoes(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom", result: tideChartResult{Station: tideStation{StationID: "S1", Name: "Harbour"}}})
	writeTideSettings(t, "  tide_provider: bom\n  tide_station_id: S1\n")
	for _, w := range []*anchorWatchData{{Lat: 45.51, Lon: 12.41}, {Lat: 45.60, Lon: 12.41}, {Lat: -1, Lon: -1}} {
		withAnchorWatch(t, w)
		c, err := liveTideCalChart()
		if err != nil || c.Source != "bom/S1" || c.PositionBased {
			t.Fatalf("anchor %+v: source %q positionBased %v err %v", w, c.Source, c.PositionBased, err)
		}
	}
}

// ── fourth review ──────────────────────────────────────────────────────

func TestTidePosition_AnUnreadableWatchFileIsAnExplicitErrorNotTheBoatsFix(t *testing.T) {
	withAnchorWatch(t, nil)
	withAnchorWatchLoadErr(t, "reading anchor watch state: permission denied")
	_, _, err := tidePositionFromState(vesselStateData{Latitude: 45.43, Longitude: 12.34}, nil)
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("got %v", err)
	}
}

func TestTidePosition_AFlaggedFixNamesWhyWhenTheValidationSaysSo(t *testing.T) {
	withAnchorWatch(t, nil)
	_, _, err := tidePositionFromState(vesselStateData{GNSSCriticalAlert: true, GNSSValidationReason: "signalk unreachable: stream has no self vessel data"}, nil)
	if err == nil || !strings.Contains(err.Error(), "signalk unreachable") {
		t.Fatalf("the reason must name the SignalK failure, got %v", err)
	}
}

func TestTideNearest_PositionBasedReadsTheVesselOnce(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withAnchorWatch(t, nil)
	registerTideProvider(newMSLStub(time.Now()))
	reads := 0
	prev := tideVesselState
	tideVesselState = func() (vesselStateData, error) {
		reads++
		return vesselStateData{Latitude: 45.43, Longitude: 12.34}, nil
	}
	t.Cleanup(func() { tideVesselState = prev })
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest?provider=open-meteo", nil), rec)
	if err := tideNearestHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%v %d %s", err, rec.Code, rec.Body)
	}
	if reads != 1 {
		t.Errorf("read the vessel state %d times, want 1", reads)
	}
}

func TestTidePointZone_AFailureIsRememberedForAWhileThenTriedAgain(t *testing.T) {
	tidePointZonesMu.Lock()
	tidePointZones = map[string]tideZoneEntry{}
	tidePointZonesMu.Unlock()
	prevLookup, prevNow := tideZoneLookup, tideZoneNow
	t.Cleanup(func() { tideZoneLookup, tideZoneNow = prevLookup, prevNow })
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tideZoneNow = func() time.Time { return now }
	calls, fail := 0, true
	tideZoneLookup = func(lat, lon float64) (placeZone, error) {
		calls++
		if fail {
			return placeZone{}, errors.New("no zone offshore")
		}
		return placeZone{Name: "Pacific/Honolulu"}, nil
	}
	var logs strings.Builder
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	for i := 0; i < 5; i++ {
		if z := tidePointZone("0.05,-150.00", 0.05, -150); z != "" {
			t.Fatalf("zone = %q", z)
		}
	}
	if calls != 1 || strings.Count(logs.String(), "no time zone") != 1 {
		t.Fatalf("a failure is looked up once and logged once per period: %d calls, log %q", calls, logs.String())
	}
	now = now.Add(11 * time.Minute)
	fail = false
	if z := tidePointZone("0.05,-150.00", 0.05, -150); z != "Pacific/Honolulu" || calls != 2 {
		t.Fatalf("a failure is tried again after the retry period: %q after %d calls", z, calls)
	}
	// A zone found is kept.
	if z := tidePointZone("0.05,-150.00", 0.05, -150); z != "Pacific/Honolulu" || calls != 2 {
		t.Fatalf("a found zone is not looked up again: %q after %d calls", z, calls)
	}
}

func TestConfiguredChart_AnErrorSaysWhatKindOfProviderFailed(t *testing.T) {
	withCleanTideProviderRegistry(t)
	withAnchorWatch(t, nil)
	registerTideProvider(newMSLStub(time.Now()))
	withVesselPosition(t, -1, -1, nil)
	writeTideSettings(t, "  tide_provider: \"\"\n")
	_, body := getConfiguredChart(t, "")
	if body["position_based"] != true {
		t.Errorf("a position-based provider's error says so: %v", body)
	}
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})
	writeTideSettings(t, "  tide_provider: bom\n")
	_, body = getConfiguredChart(t, "")
	if body["position_based"] != false {
		t.Errorf("a station provider's error says so: %v", body)
	}
}

func TestPlanningTide_AMinusOneFootHeightIsAReadingWithOrWithoutADatum(t *testing.T) {
	anchorTestEnv(t, 0)
	resetAnchorWatchState(t)
	code, resp := postAnchorWatch(t, map[string]any{"lat": -21.1, "lon": 149.2, "planning_depth_m": 4.0, "planning_tide_height_ft": -1.0})
	if code != http.StatusOK || resp["planning_tide_height_ft"] != -1.0 || resp["planning_tide_datum"] != tideDatumChart {
		t.Fatalf("-1.0 on a drop is a minus tide at chart datum: %d %+v", code, resp)
	}
	code, resp = patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": 5.0, "planning_tide_height_ft": -1.0})
	if code != http.StatusOK || resp["planning_tide_height_ft"] != -1.0 || resp["planning_tide_datum"] != tideDatumChart {
		t.Fatalf("and on a patch: %d %+v", code, resp)
	}
	code, resp = patchAnchorWatchForTest(t, map[string]any{"planning_depth_m": 5.0, "planning_tide_height_ft": -1.0, "planning_tide_datum": "chart_datum"})
	if code != http.StatusOK || resp["planning_tide_height_ft"] != -1.0 || resp["planning_tide_datum"] != tideDatumChart {
		t.Fatalf("-1.0 with a datum is real: %d %+v", code, resp)
	}
}

// ── fifth review ───────────────────────────────────────────────────────

func TestConfiguredChart_APlaceKeepsTheCatalogStationsPositionWhenThePluginLeavesItAtZero(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom",
		stations: []tideStation{{StationID: "NEAR", Name: "Near Bar", Lat: -27.0, Lon: 153.0}},
		result:   tideChartResult{Station: tideStation{StationID: "NEAR", Name: "Near Bar"}}}) // 0,0
	writeTideSettings(t, "  tide_provider: bom\n")
	code, body := getConfiguredChart(t, "lat=-27.1&lon=153.1")
	st, _ := body["station"].(map[string]any)
	if code != http.StatusOK || st["lat"] != -27.0 || st["lon"] != 153.0 {
		t.Fatalf("the distance and the far warning need the station's position: %d %v", code, body)
	}
}

func TestTideStationName_WhileAnAnchorWatchIsSetItIsTheAnchorPosition(t *testing.T) {
	withCleanTideProviderRegistry(t)
	stub := newMSLStub(time.Now())
	registerTideProvider(stub)
	withVesselPosition(t, 45.4312, 12.3378, nil)
	writeTideSettings(t, "  tide_provider: \"\"\n")

	withAnchorWatch(t, &anchorWatchData{Lat: 45.51, Lon: 12.41})
	_, body := getConfiguredChart(t, "")
	if body["station"].(map[string]any)["name"] != "Anchor position" {
		t.Errorf("the chart names the anchor: %v", body["station"])
	}
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-nearest?provider=open-meteo", nil), rec)
	if err := tideNearestHandler(c); err != nil {
		t.Fatal(err)
	}
	var st tideStation
	_ = json.Unmarshal(rec.Body.Bytes(), &st)
	if st.Name != "Anchor position" {
		t.Errorf("tide-nearest names the anchor: %+v", st)
	}

	withAnchorWatch(t, nil)
	_, body = getConfiguredChart(t, "")
	if body["station"].(map[string]any)["name"] != "Boat's position" {
		t.Errorf("with no watch it is the boat's: %v", body["station"])
	}
}

func TestTideBoatFixNow_WithAWatchSetTheVesselStateIsNotRead(t *testing.T) {
	withAnchorWatch(t, &anchorWatchData{Lat: 45.51, Lon: 12.41})
	prev := tideVesselState
	tideVesselState = func() (vesselStateData, error) {
		t.Error("read the vessel state for a point the anchor decides")
		return vesselStateData{}, nil
	}
	t.Cleanup(func() { tideVesselState = prev })
	fix := tideBoatFixNow()
	if fix.Err != nil || !fix.Anchored || fix.Lat != 45.51 {
		t.Fatalf("fix = %+v", fix)
	}
	// An unreadable watch file is still an explicit error, with no read.
	withAnchorWatch(t, nil)
	withAnchorWatchLoadErr(t, "permission denied")
	if fix = tideBoatFixNow(); fix.Err == nil {
		t.Fatal("an unreadable watch file is an error")
	}
}

func TestTideProviders_ListMarksTheDefault(t *testing.T) {
	withCleanTideProviderRegistry(t)
	registerTideProvider(&stubTideProvider{id: "bom"})
	registerTideProvider(newMSLStub(time.Now()))
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/tide-providers", nil), rec)
	if err := tideProvidersHandler(c); err != nil {
		t.Fatal(err)
	}
	var list []tideProviderInfo
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list) != 2 || list[0].Default || !list[1].Default {
		t.Fatalf("providers = %+v", list)
	}
}
