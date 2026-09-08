package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func pathsFrom(t *testing.T, body string) []signalKPath {
	t.Helper()
	seedSelfTree(t, body)
	return collectSignalKPaths(globalSignalKSnapshot.selfTree())
}

func pathNames(paths []signalKPath) []string {
	names := make([]string, 0, len(paths))
	for _, path := range paths {
		names = append(names, path.Path)
	}
	return names
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// Nothing enumerated paths before this: every accessor was a fixed key chain,
// so a widget could only ever show a value a developer had already wired.
func TestCollectSignalKPathsFindsNestedLeaves(t *testing.T) {
	paths := pathsFrom(t, `{
		"environment": {"depth": {"belowTransducer": {"value": 3.2}}},
		"propulsion": {"port": {"oilPressure": {"value": 241325.0}}}
	}`)

	names := pathNames(paths)
	for _, want := range []string{"environment.depth.belowTransducer", "propulsion.port.oilPressure"} {
		if !contains(names, want) {
			t.Fatalf("expected %q in %v", want, names)
		}
	}
}

func TestCollectSignalKPathsIsSortedForAStablePicker(t *testing.T) {
	paths := pathsFrom(t, `{
		"tanks": {"fuel": {"0": {"currentLevel": {"value": 0.8}}}},
		"environment": {"depth": {"belowTransducer": {"value": 3.2}}}
	}`)

	names := pathNames(paths)
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Fatalf("paths must be sorted, got %v", names)
		}
	}
}

// SignalK declares units in meta; surfacing them is what lets the picker offer
// sensible conversions instead of making the operator know oil pressure is in
// pascals.
func TestCollectSignalKPathsSurfacesUnitsFromMeta(t *testing.T) {
	paths := pathsFrom(t, `{
		"propulsion": {"port": {"oilPressure": {"value": 241325.0, "meta": {"units": "Pa"}}}}
	}`)

	if len(paths) != 1 {
		t.Fatalf("expected 1 path, got %v", pathNames(paths))
	}
	if paths[0].Units != "Pa" {
		t.Fatalf("units: got %q, want %q", paths[0].Units, "Pa")
	}
}

func TestCollectSignalKPathsCarriesCurrentValue(t *testing.T) {
	paths := pathsFrom(t, `{"environment": {"depth": {"belowTransducer": {"value": 3.25}}}}`)

	if len(paths) != 1 || paths[0].Value == nil {
		t.Fatalf("expected a value to preview, got %+v", paths)
	}
	if *paths[0].Value != 3.25 {
		t.Fatalf("value: got %v, want 3.25", *paths[0].Value)
	}
}

// A position is a container, not something a gauge can render, so its members
// are offered individually.
func TestCollectSignalKPathsDescendsIntoObjectValues(t *testing.T) {
	paths := pathsFrom(t, `{
		"navigation": {"position": {"value": {"latitude": -21.11, "longitude": 149.22}}}
	}`)

	names := pathNames(paths)
	for _, want := range []string{"navigation.position.latitude", "navigation.position.longitude"} {
		if !contains(names, want) {
			t.Fatalf("expected %q in %v", want, names)
		}
	}
	if contains(names, "navigation.position") {
		t.Fatalf("the container itself is not bindable, got %v", names)
	}
}

func TestCollectSignalKPathsSkipsMetadataSiblings(t *testing.T) {
	paths := pathsFrom(t, `{
		"environment": {"depth": {"belowTransducer": {
			"value": 3.2, "timestamp": "2026-08-13T00:00:00Z", "$source": "n2k.1"
		}}}
	}`)

	names := pathNames(paths)
	if len(names) != 1 || names[0] != "environment.depth.belowTransducer" {
		t.Fatalf("timestamp and $source are not bindable paths, got %v", names)
	}
}

func TestCollectSignalKPathsHandlesStringValues(t *testing.T) {
	paths := pathsFrom(t, `{"navigation": {"state": {"value": "anchored"}}}`)

	if len(paths) != 1 || paths[0].Path != "navigation.state" {
		t.Fatalf("expected navigation.state, got %v", pathNames(paths))
	}
	if paths[0].Value != nil {
		t.Fatalf("a string has no numeric preview, got %v", *paths[0].Value)
	}
}

func TestCollectSignalKPathsEmptyTree(t *testing.T) {
	if paths := collectSignalKPaths(map[string]any{}); len(paths) != 0 {
		t.Fatalf("expected no paths, got %v", pathNames(paths))
	}
}

// A derived path has to reach the browser through the same stream as any other
// bound path, or nothing can render it.
func TestGaugeValuesPayloadCarriesDerivedPaths(t *testing.T) {
	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{
		"a": {ID: "a", Widgets: []dashboardLayoutItem{
			gaugeWidget("gauge:aaaa1111", &dashboardGaugeConfig{
				Path: vesselFuelEconomyPath, Display: "numeric", Quantity: "fuelEconomy", Unit: "nmpl",
			}),
		}},
	}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})

	payload := buildGaugeValuesPayload()
	values, ok := payload["values"].(map[string]any)
	if !ok {
		t.Fatalf("unexpected payload shape: %+v", payload)
	}
	if _, present := values[vesselFuelEconomyPath]; !present {
		t.Fatalf("expected the derived path in the payload, got keys %v", values)
	}
}

// It also has to be findable, or an operator cannot bind it in the first place.
func TestSignalKPathsHandlerListsDerivedPaths(t *testing.T) {
	e := echo.New()
	rec := httptest.NewRecorder()
	if err := signalKPathsHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/signalk/paths", nil), rec)); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var body struct {
		Paths []signalKPath `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	found := false
	for _, p := range body.Paths {
		if p.Path == vesselFuelEconomyPath {
			found = true
			if p.Units != "m/m3" {
				t.Errorf("expected the derived path to declare its units, got %q", p.Units)
			}
		}
	}
	if !found {
		t.Fatalf("expected %q among the listed paths", vesselFuelEconomyPath)
	}
}

/*
Ages ride the same gauge-values stream as the values themselves (ADR 0083):
every bound path carries how long ago its last update arrived, so a widget
can tell a frozen reading from a live one without a source-specific age of
its own.
*/

// setPagesWithGaugePaths installs one page carrying one gauge widget per
// path, so buildGaugeValuesPayload has something bound to walk.
func setPagesWithGaugePaths(t *testing.T, paths ...string) {
	t.Helper()
	widgets := make([]dashboardLayoutItem, 0, len(paths))
	for i, path := range paths {
		widgets = append(widgets, gaugeWidget(fmt.Sprintf("gauge:age%04d", i+1), &dashboardGaugeConfig{
			Path: path, Display: "numeric", Quantity: "raw", Unit: "raw",
		}))
	}

	dashboardPagesMu.Lock()
	previous := dashboardPagesState
	dashboardPagesState = map[string]*dashboardPageData{"a": {ID: "a", Widgets: widgets}}
	dashboardPagesMu.Unlock()
	t.Cleanup(func() {
		dashboardPagesMu.Lock()
		dashboardPagesState = previous
		dashboardPagesMu.Unlock()
	})
}

func TestGaugeValuesPayloadCarriesAges(t *testing.T) {
	seedSelfTree(t, `{"propulsion": {"port": {"oilPressure": {"value": 241325.0}}}}`)
	setPagesWithGaugePaths(t, "propulsion.port.oilPressure")

	payload := buildGaugeValuesPayload()
	ages, ok := payload["ages"].(map[string]float64)
	if !ok {
		t.Fatalf("expected an ages map alongside values, got %+v", payload)
	}
	if _, present := ages["propulsion.port.oilPressure"]; !present {
		t.Fatalf("expected an age for the bound path, got %v", ages)
	}
}

// A node that never carried a SignalK timestamp (this delta's update sets
// none) falls back to the alarm engine's own arrival-time record (pathSeen)
// rather than a client-side clock: arrival time is all the evidence there is.
func TestGaugeValuesPayloadAgeFromPathSeenWhenNodeHasNoTimestamp(t *testing.T) {
	snapshot := newSignalKSnapshot()
	seenAt := time.Now().UTC().Add(-300 * time.Second)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "propulsion.port.oilPressure", Value: 241325.0}}}},
	}, seenAt)
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)
	setPagesWithGaugePaths(t, "propulsion.port.oilPressure")

	ages := buildGaugeValuesPayload()["ages"].(map[string]float64)
	age := ages["propulsion.port.oilPressure"]
	if math.Abs(age-300) > 5 {
		t.Fatalf("expected an age of about 300s, got %v", age)
	}
}

// The case that actually produced "Stale 16m" against a source silent for a
// day: a resubscribe (a backend restart or an ordinary reconnect) replays
// every retained delta, which resets pathSeen to the arrival time -- now --
// even though the replayed delta still carries the source's original
// declared timestamp. The node's own timestamp must win, or a resubscribe
// makes every long-dead path look freshly arrived for two minutes.
func TestGaugeValuesPayloadAgePrefersNodeTimestampOverReplayedPathSeen(t *testing.T) {
	snapshot := newSignalKSnapshot()
	now := time.Now().UTC()
	oldTimestamp := now.Add(-20 * time.Hour).Format(time.RFC3339)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: oldTimestamp,
			Values:    []signalKValue{{Path: "propulsion.port.fuel.rate", Value: 4.1666666e-7}},
		}},
	}, now) // arrival is "now": a replay, not a fresh reading from the source
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)
	setPagesWithGaugePaths(t, "propulsion.port.fuel.rate")

	ages := buildGaugeValuesPayload()["ages"].(map[string]float64)
	age := ages["propulsion.port.fuel.rate"]
	if math.Abs(age-20*3600) > 5 {
		t.Fatalf("expected about 72000s (20h, the node's own timestamp), got %v", age)
	}
}

// A bound path that has never reported, and carries no node timestamp
// either, is unknown -- never a guessed zero.
func TestGaugeValuesPayloadAgeUnknownForNeverSeenPath(t *testing.T) {
	seedSelfTree(t, `{}`)
	setPagesWithGaugePaths(t, "propulsion.port.oilPressure")

	ages := buildGaugeValuesPayload()["ages"].(map[string]float64)
	if age := ages["propulsion.port.oilPressure"]; age != -1 {
		t.Fatalf("expected -1 for a path with no data at all, got %v", age)
	}
}

// A node seeded with only a SignalK timestamp -- no pathSeen entry, the
// shape a REST-seeded tree would have if this backend ever built one -- still
// reports an age, from that timestamp rather than the alarm engine's own
// record.
func TestGaugeValuesPayloadAgeFromNodeTimestampWithoutPathSeen(t *testing.T) {
	old := time.Now().UTC().Add(-90 * time.Second).Format(time.RFC3339)
	seedSelfTree(t, fmt.Sprintf(`{"propulsion": {"port": {"oilPressure": {"value": 241325.0, "timestamp": %q}}}}`, old))
	setPagesWithGaugePaths(t, "propulsion.port.oilPressure")

	ages := buildGaugeValuesPayload()["ages"].(map[string]float64)
	age := ages["propulsion.port.oilPressure"]
	if age < 0 || math.Abs(age-90) > 5 {
		t.Fatalf("expected an age of about 90s from the node's own timestamp, got %v", age)
	}
}

// A derived value carries the oldest age among the inputs that actually
// contributed to it: SOG was current but the burn rate's own SignalK
// timestamp was twenty hours old, so the economy figure it produced is
// exactly as stale as that engine is -- even though both deltas arrive in
// this same tick, the shape a resubscribe replay takes.
func TestGaugeValuesPayloadDerivedFuelEconomyAgeIsOldestInput(t *testing.T) {
	snapshot := newSignalKSnapshot()
	now := time.Now().UTC()
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{Values: []signalKValue{{Path: "navigation.speedOverGround", Value: 5.0}}}},
	}, now)
	oldTimestamp := now.Add(-20 * time.Hour).Format(time.RFC3339)
	snapshot.applyDelta(signalKDelta{
		Context: "vessels.self",
		Updates: []signalKUpdate{{
			Timestamp: oldTimestamp,
			Values:    []signalKValue{{Path: "propulsion.port.fuel.rate", Value: 1e-05}},
		}},
	}, now)
	snapshot.setSelfContext("vessels.self")
	withGlobalSnapshot(t, snapshot)
	setPagesWithGaugePaths(t, vesselFuelEconomyPath)

	ages := buildGaugeValuesPayload()["ages"].(map[string]float64)
	age := ages[vesselFuelEconomyPath]
	if math.Abs(age-20*3600) > 5 {
		t.Fatalf("expected the fuel-rate input's ~20h age from its own timestamp, got %v", age)
	}
}
