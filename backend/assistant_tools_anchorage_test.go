package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeAnchorageProvider is a place-names provider that also serves
// anchorage_features.
type fakeAnchorageProvider struct {
	fakeSearchPlacesProvider
	features anchorageFeatures
	err      error
	calls    [][2]float64
}

func (f *fakeAnchorageProvider) AnchorageFeatures(lat, lon float64) (anchorageFeatures, error) {
	f.calls = append(f.calls, [2]float64{lat, lon})
	return f.features, f.err
}

// butterflyBayFeatures is what the osm-overpass plugin writes for its Butterfly
// Bay capture (its golden file), decoded the way the host decodes an answer.
func butterflyBayFeatures(t *testing.T) anchorageFeatures {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "examples", "poi-plugins", "osm-overpass", "testdata", "anchorage_features_butterfly_bay_output.json"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := decodeAnchorageFeatures("osm-overpass", raw)
	if err != nil {
		t.Fatalf("the plugin's Butterfly Bay answer does not decode: %v", err)
	}
	return d
}

var anchorageNow = time.Date(2026, 11, 2, 1, 0, 0, 0, time.UTC) // 11:00 at Hook Island

// anchorageWeather is hourly from 00:00Z on the 2nd: a northerly through 11:00Z
// (21:00 local) and a southerly after.
func anchorageWeather() *capturingWeatherProvider {
	var hourly []weatherHourPoint
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 48; h++ {
		dir := 5.0
		if h > 11 {
			dir = 180
		}
		hourly = append(hourly, weatherHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WindSpeedMS: 7.5, WindGustMS: 10, WindDirectionDeg: dir})
	}
	return &capturingWeatherProvider{bundle: weatherForecastBundle{Hourly: hourly, CachedAt: anchorageNow}}
}

// anchorageWaves is a 0.8 m sea from the south-east every hour, with a period.
func anchorageWaves() *stubWaveProvider {
	var hourly []waveHourPoint
	height, period, dir := 0.8, 5.0, 135.0
	start := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	for h := 0; h < 48; h++ {
		hourly = append(hourly, waveHourPoint{Time: start.Add(time.Duration(h) * time.Hour), WaveHeightM: &height, WavePeriodS: &period, WaveDirectionDeg: &dir})
	}
	return &stubWaveProvider{id: "wv", bundle: waveForecastBundle{Hourly: hourly}}
}

func anchorageTides() *stubTideProvider {
	at := func(day, hour int) time.Time { return time.Date(2026, 11, day, hour, 0, 0, 0, time.UTC) }
	return &stubTideProvider{
		id:       "bom",
		stations: []tideStation{{StationID: "AIRLIE", Name: "Shute Harbour", Lat: -20.29, Lon: 148.78, Timezone: "Australia/Brisbane"}},
		result: tideChartResult{Extremes: []tideExtremePoint{
			{Time: at(2, 2), HeightM: 1.0}, {Time: at(2, 8), HeightM: 3.0, High: true}, {Time: at(2, 14), HeightM: 0.8},
			{Time: at(2, 20), HeightM: 3.2, High: true}, {Time: at(3, 2), HeightM: 1.1},
		}},
	}
}

// anchorageDeps is Thomas Island's coastline (a northerly is sheltered, a
// southerly is not) with a weather, wave, tide and anchorage_features source.
func anchorageDeps(t *testing.T, features anchorageFeatures) (assistantToolDeps, *fakeAnchorageProvider) {
	t.Helper()
	d := shelterForecastDeps(t, thomasShoreline(), true)
	p := &fakeAnchorageProvider{features: features}
	wx, wv, tides := anchorageWeather(), anchorageWaves(), anchorageTides()
	d.now = func() time.Time { return anchorageNow }
	d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
	d.waves = func() (waveProvider, string, error) { return wv, "wv", nil }
	d.tides = func() (tideProvider, string, error) { return tides, "bom", nil }
	d.placeNames = func() (placeNameProvider, string, error) { return p, "osm-overpass", nil }
	d.cards = newAssistantCardSink()
	return d, p
}

const anchorageArgs = `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T14:00:00+10:00","name":"Thomas Island South"}`

func runAnchorage(t *testing.T, d assistantToolDeps, args string) (string, assistantAnchorageDetailsResult) {
	t.Helper()
	raw, _, err := d.execute(context.Background(), "anchorage_details", json.RawMessage(args))
	if err != nil {
		t.Fatalf("anchorage_details: %v", err)
	}
	var res assistantAnchorageDetailsResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, raw)
	}
	return raw, res
}

func TestAnchorageDetails_AssessesTheStay(t *testing.T) {
	d, p := anchorageDeps(t, butterflyBayFeatures(t))
	raw, res := runAnchorage(t, d, anchorageArgs)

	if res.Arrive != "2026-11-02T14:00:00+10:00" || res.Leave != "2026-11-03T08:00:00+10:00" || !res.LeaveDefaulted || res.Timezone != "Australia/Lindeman" {
		t.Errorf("stay = %s to %s defaulted %v in %s", res.Arrive, res.Leave, res.LeaveDefaulted, res.Timezone)
	}
	if !strings.Contains(res.Reading, "08:00 local time, the first at least six hours after arrival") {
		t.Errorf("a defaulted leave must be named in the reading: %q", res.Reading)
	}
	if res.Shelter == nil || len(res.Shelter.Arcs) == 0 {
		t.Fatalf("shelter = %+v", res.Shelter)
	}

	f := res.Forecast
	if f.Error != "" || f.AnchorageForecastBody == nil || len(f.Hourly) != 7 {
		t.Fatalf("forecast = %+v", f)
	}
	for i, want := range []string{shelterSheltered, shelterSheltered, shelterSheltered, shelterExposed, shelterExposed, shelterExposed, shelterExposed} {
		if f.Hourly[i].Shelter != want {
			t.Errorf("row %d (%s) shelter = %q, want %q", i, f.Hourly[i].T, f.Hourly[i].Shelter, want)
		}
	}
	if len(f.Days) != 2 || f.Days[0].ShelterPrevailing == nil || *f.Days[0].ShelterPrevailing != shelterSheltered || *f.Days[1].ShelterPrevailing != shelterExposed {
		t.Errorf("days = %+v", f.Days)
	}
	if f.Note != "" {
		t.Errorf("the forecast covers the whole stay, but the note says %q", f.Note)
	}

	tide := res.Tide
	if tide.Error != "" || tide.AnchorageTideBody == nil {
		t.Fatalf("tide = %+v", tide)
	}
	if tide.AtArrival == nil || tide.AtArrival.HeightM != 1.5 || tide.AtArrival.Direction != "Rising" {
		t.Errorf("height at arrival = %+v, want 1.5 m rising", tide.AtArrival)
	}
	if len(tide.Extremes) != 3 || tide.RangeM == nil || *tide.RangeM != 2.4 || tide.RangeNote != anchorageTideRangeInWindow {
		t.Errorf("tide extremes %d, range %v, note %q", len(tide.Extremes), tide.RangeM, tide.RangeNote)
	}
	if tide.Station.Name != "Shute Harbour" || tide.Station.DistanceNm <= 0 {
		t.Errorf("station = %+v", tide.Station)
	}

	feat := res.Features
	if feat.Error != "" || feat.AnchorageFeaturesBody == nil {
		t.Fatalf("features = %+v", feat)
	}
	if feat.Moorings.Count != 23 || len(feat.Moorings.Nearest) != anchorageNearestMoorings || len(feat.Moorings.VesselSizes) == 0 {
		t.Errorf("moorings = %+v", feat.Moorings)
	}
	if len(p.calls) != 1 || p.calls[0] != [2]float64{-20.5446, 149.118933} {
		t.Errorf("the plugin was asked about %v", p.calls)
	}
	for _, banned := range []string{`"geometry"`, `"points"`} {
		if strings.Contains(raw, banned) {
			t.Errorf("the model's result carries geometry (%s)", banned)
		}
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Errorf("result %d chars exceeds the cap", len(raw))
	}
	if !strings.Contains(res.Card, "anchorage card") {
		t.Errorf("with a card sink the result says a card is shown: %q", res.Card)
	}

	cards := d.takeCards(raw)
	if len(cards) != 1 || cards[0].Kind != anchorageCardKind {
		t.Fatalf("cards = %+v", cards)
	}
	card := decodeAnchorageCard(t, cards[0])
	if len(card.Forecast.Rows) != 7 || len(card.Features.Moorings) != 23 || len(card.Shelter.Bearings) != 36 {
		t.Errorf("card rows %d, moorings %d, bearings %d", len(card.Forecast.Rows), len(card.Features.Moorings), len(card.Shelter.Bearings))
	}
	if card.Name != "Thomas Island South" || card.DataAsOf != "2026-10-31T21:22:14Z" {
		t.Errorf("name %q data_as_of %q", card.Name, card.DataAsOf)
	}
}

func decodeAnchorageCard(t *testing.T, c mateCard) anchorageCardPayload {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(c.Payload))
	dec.DisallowUnknownFields()
	var p anchorageCardPayload
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("card payload: %v", err)
	}
	return p
}

func TestAnchorageDetails_NoCardSinkMeansNoCardAndNoInstruction(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	d.cards = nil
	raw, res := runAnchorage(t, d, anchorageArgs)
	if res.Card != "" || strings.Contains(raw, "card is shown") {
		t.Errorf("an outside assistant has no card: %q", res.Card)
	}
	if len(res.Forecast.Hourly) == 0 || res.Features.Moorings.Count == 0 {
		t.Error("the sections stay in the result without a card")
	}
}

func TestAnchorageDetails_ShelterFailureFailsTheCall(t *testing.T) {
	d, p := anchorageDeps(t, butterflyBayFeatures(t))
	d.offlineCoastline = emptyCoastlineDeps(t).offlineCoastline
	if _, _, err := d.execute(context.Background(), "anchorage_details", json.RawMessage(anchorageArgs)); err == nil || !strings.Contains(err.Error(), "anchorage_details") {
		t.Fatalf("a missing shelter must fail the whole call, got %v", err)
	}
	if len(d.cards.byResult) != 0 {
		t.Error("a failed call puts no card")
	}
	if len(p.calls) != 0 {
		t.Errorf("shelter failed first, so no network lookup should have started: %v", p.calls)
	}
	d.offlineCoastline = nil
	if _, _, err := d.execute(context.Background(), "anchorage_details", json.RawMessage(anchorageArgs)); err == nil || !strings.Contains(err.Error(), "Offline coastline") {
		t.Fatalf("no coastline reader: %v", err)
	}
}

func TestAnchorageDetails_ASectionFailureIsThatSectionsAlone(t *testing.T) {
	cases := map[string]struct {
		mutate func(*assistantToolDeps, *fakeAnchorageProvider)
		check  func(*testing.T, assistantAnchorageDetailsResult)
	}{
		"forecast": {
			func(d *assistantToolDeps, _ *fakeAnchorageProvider) {
				wx := anchorageWeather()
				wx.err = errors.New("simulated upstream failure")
				d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
			},
			func(t *testing.T, r assistantAnchorageDetailsResult) {
				if !strings.HasPrefix(r.Forecast.Error, "Forecast unavailable: weather provider \"wx\": simulated upstream failure") || r.Forecast.AnchorageForecastBody != nil {
					t.Errorf("forecast = %+v", r.Forecast)
				}
				if r.Tide.Error != "" || r.Features.Error != "" || r.Shelter == nil {
					t.Error("the other sections must be untouched")
				}
			},
		},
		"tide": {
			func(d *assistantToolDeps, _ *fakeAnchorageProvider) {
				tp := anchorageTides()
				tp.fetchErr = errors.New("station offline")
				d.tides = func() (tideProvider, string, error) { return tp, "bom", nil }
			},
			func(t *testing.T, r assistantAnchorageDetailsResult) {
				if !strings.Contains(r.Tide.Error, "Tide unavailable") || !strings.Contains(r.Tide.Error, "station offline") || r.Tide.AnchorageTideBody != nil {
					t.Errorf("tide = %+v", r.Tide)
				}
				if r.Forecast.Error != "" || r.Features.Error != "" {
					t.Error("the other sections must be untouched")
				}
			},
		},
		"features, plugin error": {
			func(_ *assistantToolDeps, p *fakeAnchorageProvider) { p.err = errors.New("overpass is busy") },
			func(t *testing.T, r assistantAnchorageDetailsResult) {
				if r.Features.Error != "Features unavailable: overpass is busy" || r.Features.AnchorageFeaturesBody != nil {
					t.Errorf("features = %+v", r.Features)
				}
			},
		},
		"features, missing export": {
			func(_ *assistantToolDeps, p *fakeAnchorageProvider) {
				p.err = fmt.Errorf("plugin %q: %w", "osm-overpass", errNoAnchorageFeaturesExport)
			},
			func(t *testing.T, r assistantAnchorageDetailsResult) {
				if !strings.Contains(r.Features.Error, `plugin "osm-overpass" does not provide anchorage features`) {
					t.Errorf("features = %+v", r.Features)
				}
			},
		},
		"features, plugin without the interface": {
			func(d *assistantToolDeps, _ *fakeAnchorageProvider) {
				plain := &fakeSearchPlacesProvider{id: "google-places"}
				d.placeNames = func() (placeNameProvider, string, error) { return plain, "google-places", nil }
			},
			func(t *testing.T, r assistantAnchorageDetailsResult) {
				if !strings.Contains(r.Features.Error, `plugin "google-places" does not provide anchorage features`) {
					t.Errorf("features = %+v", r.Features)
				}
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d, p := anchorageDeps(t, butterflyBayFeatures(t))
			tc.mutate(&d, p)
			raw, res := runAnchorage(t, d, anchorageArgs)
			tc.check(t, res)
			if !strings.Contains(res.Reading, "was not checked") {
				t.Errorf("the reading must say a section was not checked: %q", res.Reading)
			}
			// The card carries the same error, and no list that could read as "nothing there".
			cards := d.takeCards(raw)
			if len(cards) != 1 {
				t.Fatalf("cards = %d", len(cards))
			}
			card := decodeAnchorageCard(t, cards[0])
			if card.Forecast.Error+card.Tide.Error+card.Features.Error == "" {
				t.Error("the card must carry the section's error")
			}
		})
	}
}

func TestAnchorageDetails_NilProvidersAreSectionErrors(t *testing.T) {
	d := shelterForecastDeps(t, thomasShoreline(), true)
	d.weather, d.tides, d.placeNames = nil, nil, nil
	_, res := runAnchorage(t, d, anchorageArgs)
	if res.Forecast.Error == "" || res.Tide.Error == "" || res.Features.Error == "" {
		t.Errorf("sections = %q %q %q", res.Forecast.Error, res.Tide.Error, res.Features.Error)
	}
}

func TestAnchorageDetails_ForecastOutsideTheStayIsAnError(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-12-20T14:00:00+10:00","leave":"2026-12-21T08:00:00+10:00"}`)
	if !strings.Contains(res.Forecast.Error, "does not cover arrival") {
		t.Errorf("forecast = %+v", res.Forecast)
	}
}

func TestAnchorageDetails_AForecastThatEndsBeforeTheStayDoesIsNotedNotStretched(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	wx := anchorageWeather()
	wx.bundle.Hourly = wx.bundle.Hourly[:18] // to 17:00Z, 03:00 on the 3rd local
	d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
	_, res := runAnchorage(t, d, anchorageArgs)
	if !strings.Contains(res.Forecast.Note, "The forecast ends at Tue 3 Nov 03:00, before the stay does.") {
		t.Errorf("note = %q", res.Forecast.Note)
	}
	if n := len(res.Forecast.Hourly); n != 5 {
		t.Errorf("rows = %d: nothing past the forecast is invented", n)
	}
	if wx.gotDays < 2 {
		t.Errorf("the provider was asked for %d days; the stay runs into the next day", wx.gotDays)
	}
}

func TestAnchorageDetails_LeaveGivenIsKept(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T14:00:00+10:00","leave":"2026-11-02T19:00:00+10:00"}`)
	if res.LeaveDefaulted || res.Leave != "2026-11-02T19:00:00+10:00" || strings.Contains(res.Reading, "08:00") {
		t.Errorf("leave %s defaulted %v reading %q", res.Leave, res.LeaveDefaulted, res.Reading)
	}
	if len(res.Forecast.Hourly) != 3 {
		t.Errorf("rows to 19:00 = %d, want 14:00, 17:00 and 19:00", len(res.Forecast.Hourly))
	}
	if res.Forecast.Note != "" {
		t.Errorf("the forecast covers a stay ending at 19:00: %q", res.Forecast.Note)
	}
}

func TestAnchorageDetails_ADefaultLeaveIsTheBoatsMorningNotUTC(t *testing.T) {
	// 22:00 UTC on the 2nd is 08:00 on the 3rd at Hook Island; the "morning
	// after" an arrival at 23:30 local on the 2nd is the 3rd at 08:00 local.
	stay, err := parseAnchorageStay(assistantAnchorageDetailsArgs{Lat: f64p(-20.5), Lon: f64p(149.1), Arrive: "2026-11-02T23:30:00+10:00"})
	if err != nil {
		t.Fatal(err)
	}
	if got := stay.leave.Format(time.RFC3339); got != "2026-11-03T08:00:00+10:00" {
		t.Errorf("default leave = %s", got)
	}
	// The default is the first 08:00 at least six hours after arrival.
	for _, c := range []struct{ arrive, leave string }{
		{"2026-11-03T02:30:00+10:00", "2026-11-04T08:00:00+10:00"}, // 5.5 h to 08:00 the same day: the next one
		{"2026-11-03T01:00:00+10:00", "2026-11-03T08:00:00+10:00"}, // 7 h
		{"2026-11-03T02:00:00+10:00", "2026-11-03T08:00:00+10:00"}, // exactly 6 h counts
		{"2026-11-02T16:00:00+10:00", "2026-11-03T08:00:00+10:00"},
	} {
		stay, err := parseAnchorageStay(assistantAnchorageDetailsArgs{Lat: f64p(-20.5), Lon: f64p(149.1), Arrive: c.arrive})
		if err != nil {
			t.Fatal(err)
		}
		if got := stay.leave.Format(time.RFC3339); got != c.leave {
			t.Errorf("arrive %s: default leave = %s, want %s", c.arrive, got, c.leave)
		}
	}
}

// A short stay gets the hour nearest arrival and the hour nearest leaving, not
// only the hours that divide by three.
func TestAnchorageDetails_ForecastRowsFollowArrivalNotTheClock(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T13:00:00+10:00","leave":"2026-11-02T14:30:00+10:00"}`)
	if res.Forecast.Error != "" {
		t.Fatalf("a short stay is not an error: %q", res.Forecast.Error)
	}
	var ts []string
	for _, r := range res.Forecast.Hourly {
		ts = append(ts, r.T)
	}
	// Leave at 14:30 is a tie between 14:00 and 15:00: the one at or before.
	if len(ts) != 2 || !strings.Contains(ts[0], "13:00") || !strings.Contains(ts[1], "14:00") {
		t.Errorf("rows = %v, want 13:00 and 14:00", ts)
	}

	_, res = runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T14:40:00+10:00","leave":"2026-11-02T21:00:00+10:00"}`)
	ts = nil
	for _, r := range res.Forecast.Hourly {
		ts = append(ts, r.T)
	}
	// Nearest to 14:40 is 15:00; then every three hours, then the hour of leaving.
	want := []string{"15:00", "18:00", "21:00"}
	if len(ts) != len(want) {
		t.Fatalf("rows = %v, want %v", ts, want)
	}
	for i, w := range want {
		if !strings.Contains(ts[i], w) {
			t.Errorf("row %d = %q, want %s", i, ts[i], w)
		}
	}
}

func TestAnchorageDetails_ForecastNotCoveringArrivalIsTheOnlyForecastError(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-12-20T14:00:00+10:00","leave":"2026-12-20T15:00:00+10:00"}`)
	if !strings.Contains(res.Forecast.Error, "does not cover arrival") {
		t.Errorf("forecast = %+v", res.Forecast)
	}
}

func TestAnchorageTide_PredictionEndingBeforeLeaveIsSaidAndClaimsNoExtremeAfter(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	// The prediction is cut after the 06:00 local high water on the 3rd; the
	// stay runs 01:00 to 09:00, so one extreme falls inside and none follows.
	tp := anchorageTides()
	tp.result.Extremes = tp.result.Extremes[:4] // last is the 20:00Z high, 06:00 local on the 3rd
	d.tides = func() (tideProvider, string, error) { return tp, "bom", nil }
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-03T01:00:00+10:00","leave":"2026-11-03T09:00:00+10:00"}`)
	tide := res.Tide
	if tide.Error != "" {
		t.Fatalf("tide = %+v", tide)
	}
	if !strings.Contains(tide.Note, "ends at") || !strings.Contains(tide.Note, "before the stay does") {
		t.Errorf("note = %q: the prediction ends before leaving and must say when", tide.Note)
	}
	if strings.Contains(tide.RangeNote, "after leaving") {
		t.Errorf("range note claims an extreme after leaving: %q", tide.RangeNote)
	}
}

func TestAnchorageDetails_RefusesBadArguments(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	for name, args := range map[string]string{
		"no arrive":      `{"lat":-20.5,"lon":149.1}`,
		"bad arrive":     `{"lat":-20.5,"lon":149.1,"arrive":"tomorrow"}`,
		"bad leave":      `{"lat":-20.5,"lon":149.1,"arrive":"2026-11-02T14:00:00+10:00","leave":"later"}`,
		"leave = arrive": `{"lat":-20.5,"lon":149.1,"arrive":"2026-11-02T14:00:00+10:00","leave":"2026-11-02T14:00:00+10:00"}`,
		"leave before":   `{"lat":-20.5,"lon":149.1,"arrive":"2026-11-02T14:00:00+10:00","leave":"2026-11-02T08:00:00+10:00"}`,
		"too long":       `{"lat":-20.5,"lon":149.1,"arrive":"2026-11-02T14:00:00+10:00","leave":"2026-11-10T14:00:00+10:00"}`,
		"no position":    `{"arrive":"2026-11-02T14:00:00+10:00"}`,
		"bad position":   `{"lat":95,"lon":10,"arrive":"2026-11-02T14:00:00+10:00"}`,
		"null island":    `{"lat":0,"lon":0,"arrive":"2026-11-02T14:00:00+10:00"}`,
	} {
		if _, _, err := d.execute(context.Background(), "anchorage_details", json.RawMessage(args)); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	if len(d.cards.byResult) != 0 {
		t.Error("a refused call puts no card")
	}
}

func TestAnchorageTide_RangeIncludesBracketingExtremesWhenFewerThanTwoFallInTheStay(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	// 03:00Z to 06:00Z: no high or low water falls inside it.
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T13:00:00+10:00","leave":"2026-11-02T16:00:00+10:00"}`)
	tide := res.Tide
	if len(tide.Extremes) != 2 || tide.Extremes[0].InWindow || tide.Extremes[1].InWindow {
		t.Fatalf("extremes = %+v, want the low before and the high after, both outside the stay", tide.Extremes)
	}
	if tide.RangeM == nil || *tide.RangeM != 2 || tide.RangeNote != anchorageTideRangeBracket {
		t.Errorf("range %v note %q", tide.RangeM, tide.RangeNote)
	}

	// One high water inside the stay: still fewer than two, so both neighbours join.
	_, res = runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T17:00:00+10:00","leave":"2026-11-02T20:00:00+10:00"}`)
	tide = res.Tide
	if len(tide.Extremes) != 3 || !tide.Extremes[1].InWindow || tide.Extremes[0].InWindow || tide.Extremes[2].InWindow {
		t.Fatalf("extremes = %+v", tide.Extremes)
	}
	if tide.RangeM == nil || *tide.RangeM != 2.2 {
		t.Errorf("range = %v, want 3.0 - 0.8", tide.RangeM)
	}
}

func TestAnchorageTide_ArrivalOutsideThePredictionHasNoHeight(t *testing.T) {
	d, _ := anchorageDeps(t, butterflyBayFeatures(t))
	_, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-03T13:00:00+10:00","leave":"2026-11-03T16:00:00+10:00"}`)
	if res.Tide.AtArrival != nil || !strings.Contains(res.Tide.HeightNote, "outside the predicted tides") {
		t.Errorf("tide = %+v", res.Tide.AnchorageTideBody)
	}
	if res.Tide.RangeM != nil {
		t.Errorf("a single bracketing extreme is no range: %v", *res.Tide.RangeM)
	}
}

func TestAnchorageFeatures_RestrictionsAndMooringSummary(t *testing.T) {
	size := func(v float64) *float64 { return &v }
	dist := func(v int) *int { return &v }
	found := anchorageFeatures{
		RestrictedAreas: []anchorageRestrictedWire{
			{anchorageRestrictedArea: anchorageRestrictedArea{OSM: "way/1", Name: "Reef zone", Source: "seamark", Restriction: "anchoring_prohibited", DistanceM: dist(120)}},
			{anchorageRestrictedArea: anchorageRestrictedArea{OSM: "way/2", Name: "Slow zone", Source: "seamark", Restriction: "anchoring_restricted", DistanceM: dist(300)}},
			{anchorageRestrictedArea: anchorageRestrictedArea{OSM: "relation/3", Name: "Marine Park", Source: "protected_area", Restriction: "Marine Park"}},
		},
		Moorings: []anchorageMooringWire{
			{anchorageMooringRecord: anchorageMooringRecord{OSM: "node/1", VesselSizeM: size(25), Class: "C", DistanceM: 10}},
			{anchorageMooringRecord: anchorageMooringRecord{OSM: "node/2", VesselSizeM: size(12), DistanceM: 20}},
			{anchorageMooringRecord: anchorageMooringRecord{OSM: "node/3", VesselSizeM: size(25), DistanceM: 30}},
			{anchorageMooringRecord: anchorageMooringRecord{OSM: "node/4", DetailsUntagged: true, DistanceM: 40}},
		},
	}
	d, _ := anchorageDeps(t, found)
	_, res := runAnchorage(t, d, anchorageArgs)
	f := res.Features
	if !strings.Contains(f.Reading, "Anchoring is prohibited in: Reef zone") || strings.Contains(f.Reading, "Slow zone") || strings.Contains(f.Reading, "Marine Park") {
		t.Errorf("reading = %q: only the prohibited area is a no-go", f.Reading)
	}
	if f.Moorings.Count != 4 || f.Moorings.UntaggedCount != 1 || len(f.Moorings.VesselSizes) != 2 ||
		f.Moorings.VesselSizes[0] != (anchorageMooringSize{VesselSizeM: 12, Count: 1}) || f.Moorings.VesselSizes[1] != (anchorageMooringSize{VesselSizeM: 25, Count: 2}) {
		t.Errorf("moorings = %+v", f.Moorings)
	}
	if f.RestrictedAreas[2].DistanceM != nil {
		t.Error("a protected area has no measured distance")
	}
}

func TestAnchorageFeatures_AnEmptyAnswerSaysNoneMappedNotNone(t *testing.T) {
	d, _ := anchorageDeps(t, anchorageFeatures{})
	raw, res := runAnchorage(t, d, anchorageArgs)
	if res.Features.Error != "" || len(res.Features.RestrictedAreas) != 0 || !strings.Contains(res.Features.Reading, "UNKNOWN, not absent") {
		t.Errorf("features = %+v", res.Features)
	}
	if !strings.Contains(raw, `"restricted_areas":[]`) || !strings.Contains(raw, `"bay":null`) {
		t.Errorf("an empty answer must show empty lists, not missing ones: %s", raw)
	}
}

func TestAnchorageDetails_ShrinksToTheCapAndKeepsTheCardWhole(t *testing.T) {
	features := butterflyBayFeatures(t)
	for len(features.Moorings) < 200 {
		features.Moorings = append(features.Moorings, features.Moorings[0])
	}
	d, _ := anchorageDeps(t, features)
	raw, res := runAnchorage(t, d, `{"lat":-20.5446,"lon":149.118933,"arrive":"2026-11-02T14:00:00+10:00","leave":"2026-11-09T14:00:00+10:00"}`)
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("result %d chars exceeds the cap", len(raw))
	}
	if res.Features.Moorings.Count != 200 {
		t.Errorf("the moorings are counted before the list is cut: %d", res.Features.Moorings.Count)
	}
	card := decodeAnchorageCard(t, d.takeCards(raw)[0])
	if len(card.Features.Moorings) != 200 {
		t.Errorf("the card keeps all the moorings: %d", len(card.Features.Moorings))
	}
}

func TestDecodeAnchorageFeatures_IsStrict(t *testing.T) {
	good, err := os.ReadFile(filepath.Join("..", "docs", "examples", "poi-plugins", "osm-overpass", "testdata", "anchorage_features_moreton_bay_output.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeAnchorageFeatures("p", good); err != nil {
		t.Fatalf("the plugin's Moreton Bay answer: %v", err)
	}
	for name, js := range map[string]string{
		"not json":               `{`,
		"bad date":               `{"data_as_of":"yesterday"}`,
		"no osm id":              `{"moorings":[{"osm":"","lat":-20,"lon":148}]}`,
		"mooring off planet":     `{"moorings":[{"osm":"node/1","lat":-200,"lon":148}]}`,
		"mooring huge size":      `{"moorings":[{"osm":"node/1","lat":-20,"lon":148,"vessel_size_m":9999}]}`,
		"seamark no distance":    `{"restricted_areas":[{"osm":"way/1","source":"seamark"}]}`,
		"unknown source":         `{"restricted_areas":[{"osm":"way/1","source":"guess","distance_m":5}]}`,
		"bad shape":              `{"anchorage_areas":[{"osm":"way/1","distance_m":5,"geometry":[{"kind":"polygon","points":[[-20,148],[-20,149],[-21,149]]}]}]}`,
		"seabed with no surface": `{"seabed_areas":[{"osm":"node/1","distance_m":5,"geometry":[]}]}`,
		"bay with no name":       `{"bay":{"osm":"node/1","distance_m":5}}`,
		"too far":                `{"anchorage_areas":[{"osm":"node/1","distance_m":99999,"geometry":[]}]}`,
	} {
		if _, err := decodeAnchorageFeatures("p", []byte(js)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestDecodeAnchorageFeatures_CleansTextAndDerivesUntagged(t *testing.T) {
	d, err := decodeAnchorageFeatures("p", []byte(`{"moorings":[{"osm":"node/1","name":"A\nB","class":"","lat":-20,"lon":148,"details_untagged":false,"distance_m":4}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if d.Moorings[0].Name != "AB" || !d.Moorings[0].DetailsUntagged {
		t.Errorf("mooring = %+v: text is cleaned and untagged is derived, not trusted", d.Moorings[0])
	}
}

func TestDecodeAnchorageFeatures_DropsProtectClassFromProtectedAreas(t *testing.T) {
	d, err := decodeAnchorageFeatures("p", []byte(`{"restricted_areas":[`+
		`{"osm":"relation/1","name":"Park","source":"protected_area","restriction":"Marine Park","category":"1;2;4;6","geometry":[]},`+
		`{"osm":"way/2","name":"Zone","source":"seamark","restriction":"no anchoring","category":"nature_reserve","distance_m":5,"geometry":[]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := d.RestrictedAreas[0].Category; got != "" {
		t.Errorf("protected area category = %q, want it dropped (an IUCN code list)", got)
	}
	if got := d.RestrictedAreas[1].Category; got != "nature_reserve" {
		t.Errorf("seamark category = %q, want it kept", got)
	}
}

func TestAnchorageDetailsTool_IsRegisteredAndDescribed(t *testing.T) {
	tool := lookupMateTool("anchorage_details")
	if tool == nil {
		t.Fatal("anchorage_details is not registered")
	}
	if lookupMateTool("anchorage_shelter") != nil {
		t.Fatal("anchorage_shelter is replaced by anchorage_details")
	}
	for _, want := range []string{"find_places", "arrive", "RFC 3339", "mapped", "no-go", "08:00"} {
		if !strings.Contains(tool.function.Description, want) && !strings.Contains(strings.ToLower(tool.function.Description), strings.ToLower(want)) {
			t.Errorf("description lacks %q", want)
		}
	}
	var schema struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(tool.function.Parameters, &schema); err != nil || strings.Join(schema.Required, ",") != "lat,lon,arrive" {
		t.Errorf("required = %v (%v)", schema.Required, err)
	}
}

func TestAnchorageCardGolden(t *testing.T) {
	// Hook Island: the Stonehaven shoreline fixture, the plugin's Butterfly
	// Bay answer, and a fixed weather and tide.
	w := newShelterWorld(t)
	w.hold(t, loadShorelineFixture(t, "stonehaven"), coverageSquare{-21, 148}, stonehavenSquares...)
	d := w.deps()
	p := &fakeAnchorageProvider{features: butterflyBayFeatures(t)}
	wx, wv, tides := anchorageWeather(), anchorageWaves(), anchorageTides()
	d.now = func() time.Time { return anchorageNow }
	d.weather = func() (weatherProvider, string, error) { return wx, "wx", nil }
	d.waves = func() (waveProvider, string, error) { return wv, "wv", nil }
	d.tides = func() (tideProvider, string, error) { return tides, "bom", nil }
	d.placeNames = func() (placeNameProvider, string, error) { return p, "osm-overpass", nil }
	d.cards = newAssistantCardSink()
	// A depth the operator gave, and a boat with generic settings, so the card
	// carries the whole depth section.
	d.vesselState = func() (vesselStateData, error) { return vesselStateData{DraftM: 1.4, LengthOverallM: 13}, nil }
	d.anchorSettings = func() (anchorageBoatSettings, error) { return anchorageBoat(), nil }

	text, _, err := d.execute(context.Background(), "anchorage_details", json.RawMessage(
		`{"lat":-20.0765,"lon":148.925,"arrive":"2026-11-02T14:00:00+10:00","name":"West Butterfly Bay","depth_m":4}`))
	if err != nil {
		t.Fatal(err)
	}
	cards := d.takeCards(text)
	if len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
	envelope, err := json.MarshalIndent(map[string]any{"kind": cards[0].Kind, "payload": cards[0].Payload}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	envelope = append(envelope, '\n')
	path := filepath.Join("testdata", "mate_cards", "anchorage_butterfly_bay.json")
	if *updateMateFixtures {
		if err := os.WriteFile(path, envelope, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(envelope, want) {
		t.Errorf("anchorage card drifted from %s; run go test -run AnchorageCardGolden -update if intended", path)
	}
}

func TestWasmPOIProvider_AnchorageFeaturesWithoutTheExportIsAnError(t *testing.T) {
	p := mustNewWasmPOIProvider(t, poiValidFixtureWasm)
	if p.SupportsAnchorageFeatures() {
		t.Fatal("the fixture plugin exports no anchorage_features")
	}
	if _, err := p.AnchorageFeatures(-20.0765, 148.925); err == nil || !strings.Contains(err.Error(), "no anchorage_features export") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnchorageDetails_ACancelledContextReturnsPromptlyWithoutAWriteAfterReturn(t *testing.T) {
	d, p := anchorageDeps(t, butterflyBayFeatures(t))
	release := make(chan struct{})
	d.placeNames = func() (placeNameProvider, string, error) { <-release; return p, "osm-overpass", nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := d.execute(ctx, "anchorage_details", json.RawMessage(anchorageArgs))
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the call waited for a section that was still running")
	}
	close(release)
}

func TestAnchorageCard_AnAreaWithNoShapesEmitsAnEmptyList(t *testing.T) {
	f := butterflyBayFeatures(t)
	f.RestrictedAreas = []anchorageRestrictedWire{{anchorageRestrictedArea: anchorageRestrictedArea{OSM: "relation/1", Name: "Zone", Source: "seamark"}}}
	f.AnchorageAreas = []anchorageAreaWire{{anchorageAreaRecord: anchorageAreaRecord{OSM: "way/2"}}}
	f.SeabedAreas = []anchorageSeabedWire{{anchorageSeabedRecord: anchorageSeabedRecord{OSM: "way/3"}}}
	d, _ := anchorageDeps(t, f)
	raw, _ := runAnchorage(t, d, anchorageArgs)
	cards := d.takeCards(raw)
	if len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
	if bytes.Contains(cards[0].Payload, []byte(`"shapes":null`)) {
		t.Errorf("a missing geometry must be [], not null: %s", cards[0].Payload)
	}
	if got := bytes.Count(cards[0].Payload, []byte(`"shapes":[]`)); got != 3 {
		t.Errorf(`"shapes":[] appears %d times, want 3`, got)
	}
}

func TestAnchorageDetails_ACachedLookupSaysSoAndKeepsOverpassDataTime(t *testing.T) {
	f := butterflyBayFeatures(t)
	f.Cached, f.CachedAt = true, time.Date(2026, 11, 1, 1, 2, 3, 0, time.UTC)
	d, _ := anchorageDeps(t, f)
	_, res := runAnchorage(t, d, anchorageArgs)
	b := res.Features.AnchorageFeaturesBody
	if b == nil || !b.Cached || b.CachedAt != "2026-11-01T01:02:03Z" || b.DataAsOf != f.DataAsOf {
		t.Errorf("features = %+v", b)
	}
	if !strings.Contains(b.Reading, "lookup made at 2026-11-01T01:02:03Z, kept for six hours") {
		t.Errorf("reading must say the lookup was kept: %q", b.Reading)
	}
}

func TestReadingAnchorageFeatures_FreshLookupSaysNothingAboutKeeping(t *testing.T) {
	if got := readingAnchorageFeatures(nil, false, time.Time{}); strings.Contains(got, "kept for six hours") {
		t.Errorf("fresh reading mentions a kept lookup: %q", got)
	}
}
