package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
)

// The open-sea fixtures put the boat in the Coral Sea, far from any coast,
// so a run's routed distance is its straight-line distance and the figures
// in the older estimate_passage tests stay exact.
const (
	openSeaLat = -20.0
	openSeaLon = 160.0
)

// openSeaDeps adds a vessel position and an offline coastline holding open
// sea (squares with no shoreline) around it to deps.
func openSeaDeps(t *testing.T, deps assistantToolDeps) assistantToolDeps {
	t.Helper()
	store := newTestCoastlineStore(t)
	for la := -23; la <= -16; la++ {
		for lo := 158; lo <= 162; lo++ {
			if err := store.SaveSquare(coverageSquare{la, lo}, nil, coastlineTestAt); err != nil {
				t.Fatal(err)
			}
		}
	}
	deps.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Latitude: openSeaLat, Longitude: openSeaLon}, nil
	}
	deps.offlineCoastline = func() *offlineCoastlineReader {
		return &offlineCoastlineReader{Enabled: func() (bool, error) { return true, nil }, Store: store}
	}
	return deps
}

// openSeaArgs is estimate_passage arguments for a destination nm due north
// of the open-sea position, with any extra JSON members appended.
func openSeaArgs(nm float64, rest string) json.RawMessage {
	lat := openSeaLat + nm*metersPerNauticalMile/(6371000*math.Pi/180)
	extra := ""
	if rest != "" {
		extra = "," + rest
	}
	return json.RawMessage(fmt.Sprintf(`{"to_lat":%.6f,"to_lon":%.6f%s}`, lat, openSeaLon, extra))
}

// whitsundayDeps is a boat at Haslewood Island with the real coastline held.
func whitsundayDeps(t *testing.T) assistantToolDeps {
	t.Helper()
	r, store := newRouteReader(t, true)
	holdWhitsundays(t, store, coverageSquare{-21, 149}, coverageSquare{-21, 148})
	deps := openSeaDeps(t, assistantToolDeps{
		now:               func() time.Time { return time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC) },
		influxRange:       stubInfluxRangeByPath(singleBandPerformanceSeries()),
		fuelRateInstances: func() ([]vesselEngineSetting, error) { return testEngines("port", "starboard"), nil },
	})
	deps.vesselState = func() (vesselStateData, error) {
		return vesselStateData{Latitude: haslewood.Lat, Longitude: haslewood.Lon}, nil
	}
	deps.offlineCoastline = func() *offlineCoastlineReader { return r }
	return deps
}

func TestExecuteEstimatePassage_RoutesRoundAnIslandInsteadOfThroughIt(t *testing.T) {
	deps := whitsundayDeps(t)
	raw, err := deps.execute(context.Background(), "estimate_passage",
		json.RawMessage(fmt.Sprintf(`{"to_lat":%v,"to_lon":%v,"speed_kts":10}`, cidHarbour.Lat, cidHarbour.Lon)))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantEstimatePassageResult
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if res.StraightLineNm > 8 || res.DistanceNm < 12 {
		t.Fatalf("straight %.1f nm, routed %.1f nm", res.StraightLineNm, res.DistanceNm)
	}
	if want := roundTo1(res.DistanceNm / 10); res.Hours != want {
		t.Errorf("hours %.1f, want %.1f from the routed distance", res.Hours, want)
	}
	if len(res.Route) < 3 {
		t.Errorf("a route round an island has bends: %+v", res.Route)
	}
	// The first leg heads south of the straight 284 degrees line.
	if res.FirstLegBearingDeg < 150 || res.FirstLegBearingDeg > 250 {
		t.Errorf("first leg %03d, expected it to head round the south of Whitsunday Island", res.FirstLegBearingDeg)
	}
	if len(res.Legs) != len(res.Route)-1 || res.Legs[0].BearingDeg != res.FirstLegBearingDeg {
		t.Errorf("legs %+v do not match the route %+v", res.Legs, res.Route)
	}
	// The course is the longest leg's, not the first's.
	longest := res.Legs[0]
	for _, l := range res.Legs {
		if l.DistanceNm > longest.DistanceNm {
			longest = l
		}
	}
	if res.CourseDeg != longest.BearingDeg {
		t.Errorf("course %03d, the longest leg runs %03d", res.CourseDeg, longest.BearingDeg)
	}
	if res.From.Lat != haslewood.Lat || res.To.Lat != cidHarbour.Lat {
		t.Errorf("from %+v to %+v", res.From, res.To)
	}
}

func TestExecuteEstimatePassage_PlannedLegStartsWhereGiven(t *testing.T) {
	deps := whitsundayDeps(t)
	deps.vesselState = func() (vesselStateData, error) { return vesselStateData{}, fmt.Errorf("no fix") }
	raw, err := deps.execute(context.Background(), "estimate_passage",
		json.RawMessage(fmt.Sprintf(`{"from_lat":%v,"from_lon":%v,"to_lat":%v,"to_lon":%v,"speed_kts":10}`,
			haslewood.Lat, haslewood.Lon, cidHarbour.Lat, cidHarbour.Lon)))
	if err != nil {
		t.Fatalf("a planned leg needs no vessel fix: %v", err)
	}
	if !strings.Contains(raw, `"route"`) {
		t.Errorf("result %s", raw)
	}
}

func TestExecuteEstimatePassage_MissingSquareIsReportedNotAStraightLine(t *testing.T) {
	deps := whitsundayDeps(t)
	r, store := newRouteReader(t, true)
	holdWhitsundays(t, store, coverageSquare{-21, 149}) // -21,148 missing
	deps.offlineCoastline = func() *offlineCoastlineReader { return r }
	raw, err := deps.execute(context.Background(), "estimate_passage",
		json.RawMessage(fmt.Sprintf(`{"to_lat":%v,"to_lon":%v}`, coralSea.Lat, coralSea.Lon)))
	if err == nil {
		t.Fatalf("expected an error, got %s", raw)
	}
	for _, want := range []string{"no passage distance", "21°S 148°E", "not downloaded"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

func TestExecuteEstimatePassage_NoVesselFixAndNoStartIsAnError(t *testing.T) {
	deps := whitsundayDeps(t)
	deps.vesselState = func() (vesselStateData, error) { return vesselStateData{}, fmt.Errorf("no fix") }
	_, err := deps.execute(context.Background(), "estimate_passage",
		json.RawMessage(fmt.Sprintf(`{"to_lat":%v,"to_lon":%v}`, cidHarbour.Lat, cidHarbour.Lon)))
	if err == nil || !strings.Contains(err.Error(), "from_lat") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteEstimatePassage_OnlyOneStartCoordinateIsAnError(t *testing.T) {
	deps := whitsundayDeps(t)
	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"from_lat":-20.1,"to_lat":-20.2,"to_lon":149}`))
	if err == nil || !strings.Contains(err.Error(), "both from_lat and from_lon") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteEstimatePassage_EndOnLandIsAnError(t *testing.T) {
	deps := whitsundayDeps(t)
	// Inside Whitsunday Island, kilometres from either shore.
	_, err := deps.execute(context.Background(), "estimate_passage", json.RawMessage(`{"to_lat":-20.28,"to_lon":148.99}`))
	if err == nil || !strings.Contains(err.Error(), "on land") {
		t.Fatalf("got %v", err)
	}
}

func TestExecuteEstimatePassage_DistanceIsNoLongerAnArgument(t *testing.T) {
	// Mate cannot hand in its own figure: the schema has no distance.
	if strings.Contains(string(mateToolEstimatePassage.function.Parameters), "distance_nm") {
		t.Error("estimate_passage must not take a distance")
	}
}

func TestExecuteRouteDistance_RoutesAndNamesTheStraightLineAsSuch(t *testing.T) {
	deps := whitsundayDeps(t)
	raw, err := deps.execute(context.Background(), "route_distance",
		json.RawMessage(fmt.Sprintf(`{"to_lat":%v,"to_lon":%v}`, coralSea.Lat, coralSea.Lon)))
	if err != nil {
		t.Fatal(err)
	}
	var res assistantRoutedPassage
	if err := json.Unmarshal([]byte(raw), &res); err != nil {
		t.Fatal(err)
	}
	if res.StraightLineNm < 20 || res.StraightLineNm > 21 || res.DistanceNm < res.StraightLineNm*1.1 {
		t.Errorf("straight %.1f nm, routed %.1f nm", res.StraightLineNm, res.DistanceNm)
	}
	if len(res.Route) < 3 {
		t.Errorf("route %+v", res.Route)
	}
}

func TestExecuteRouteDistance_ErrorsNameWhatIsWrong(t *testing.T) {
	deps := whitsundayDeps(t)
	deps.offlineCoastline = nil
	_, err := deps.execute(context.Background(), "route_distance", json.RawMessage(`{"to_lat":-20.25,"to_lon":148.95}`))
	if err == nil || !strings.Contains(err.Error(), "no passage distance") {
		t.Fatalf("got %v", err)
	}
	_, err = whitsundayDeps(t).execute(context.Background(), "route_distance", json.RawMessage(`{"to_lat":-20.25}`))
	if err == nil || !strings.Contains(err.Error(), "to_lat and to_lon are required") {
		t.Fatalf("got %v", err)
	}
}

func TestFindPlacesLabelsItsDistanceAsAStraightLine(t *testing.T) {
	raw, err := json.Marshal(assistantPlaceCandidate{Name: "x", StraightLineNm: 7.3, StraightLineBearingDeg: 284})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"straight_line_nm":7.3`, `"straight_line_bearing_deg":284`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("%s lacks %s", raw, want)
		}
	}
	if strings.Contains(string(raw), `"distance_nm"`) || strings.Contains(string(raw), `"bearing_deg"`) {
		t.Errorf("a candidate must not carry a bare distance or bearing: %s", raw)
	}
}

func TestRouteDistance_ATooLargeResultLosesTheWaypointsNotTheDistance(t *testing.T) {
	p := assistantRoutedPassage{DistanceNm: 15, StraightLineNm: 7, CourseDeg: 200, FirstLegBearingDeg: 197}
	for i := 0; i < 3000; i++ {
		p.Route = append(p.Route, assistantLatLon{Lat: -20.123456 + float64(i)*1e-4, Lon: 149.123456})
		p.Legs = append(p.Legs, assistantRouteLeg{BearingDeg: 123, DistanceNm: 0.1})
	}
	raw, err := capToolResultJSON(&p, p.dropRoute)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > assistantMaxToolResultChars {
		t.Fatalf("%d chars", len(raw))
	}
	var got assistantRoutedPassage
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	if got.DistanceNm != 15 || got.CourseDeg != 200 || !got.RouteOmitted || len(got.Route) != 0 {
		t.Errorf("got %+v", got)
	}
}
