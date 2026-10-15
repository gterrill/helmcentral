package main

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

// fakeTelemetry is the second adapter behind telemetryReader (ADR 0178): a
// map of plain SignalK paths to the samples the view would report for them,
// so a consumer can be tested without building a snapshot. Timestamps holds
// the newest declared source timestamp per path, for consumers that read a
// subtree's own clock rather than a leaf's age.
type fakeTelemetry struct {
	Samples    map[string]telemetrySample
	Timestamps map[string]time.Time
}

var _ telemetryReader = fakeTelemetry{}

func (f fakeTelemetry) Sample(path string) telemetrySample { return f.Samples[path] }

func (f fakeTelemetry) Number(path string) (telemetryNumber, bool) {
	s := f.Samples[path]
	if !s.Present {
		return telemetryNumber{}, false
	}
	v, ok := numericTelemetryValue(s.Value)
	if !ok {
		return telemetryNumber{}, false
	}
	return telemetryNumber{Value: v, Age: s.Age, AgeKnown: s.AgeKnown, Source: s.Source}, true
}

func (f fakeTelemetry) Value(path string) (any, bool) {
	s := f.Samples[path]
	return s.Value, s.Present
}

func (f fakeTelemetry) String(path string) (string, bool) {
	s, ok := f.Samples[path].Value.(string)
	return s, ok && f.Samples[path].Present
}

func (f fakeTelemetry) Bool(path string) (bool, bool) {
	s := f.Samples[path]
	if !s.Present {
		return false, false
	}
	return boolTelemetryValue(s.Value)
}

func (f fakeTelemetry) Position() (telemetryPosition, bool) {
	s := f.Samples["navigation.position"]
	m, _ := s.Value.(map[string]any)
	lat, latOK := numericTelemetryValue(m["latitude"])
	lon, lonOK := numericTelemetryValue(m["longitude"])
	if !s.Present || !latOK || !lonOK {
		return telemetryPosition{}, false
	}
	return telemetryPosition{Latitude: lat, Longitude: lon, Age: s.Age, AgeKnown: s.AgeKnown, Source: s.Source}, true
}

func (f fakeTelemetry) Timestamp(path string) (time.Time, bool) {
	ts, ok := f.Timestamps[path]
	return ts, ok
}

func (f fakeTelemetry) Children(path string) []string {
	seen := map[string]bool{}
	prefix := path + "."
	for p := range f.Samples {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			child, _, _ := strings.Cut(rest, ".")
			seen[child] = true
		}
	}
	out := make([]string, 0, len(seen))
	for child := range seen {
		out = append(out, child)
	}
	sort.Strings(out)
	return out
}

func (f fakeTelemetry) Paths() []signalKPath {
	out := make([]signalKPath, 0, len(f.Samples))
	for p, s := range f.Samples {
		entry := signalKPath{Path: p, Units: s.Units}
		if v, ok := s.Value.(float64); ok {
			entry.Value = &v
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// telemetryViewFromTree is a view over a tree that never came from the
// snapshot, so it has no arrival times: ages come from source timestamps
// alone.
func telemetryViewFromTree(tree map[string]any, now time.Time) *telemetryView {
	return &telemetryView{tree: tree, now: now, hasSelf: tree != nil}
}

var telemetryTestNow = time.Date(2026, 10, 15, 2, 0, 0, 0, time.UTC)

func telemetrySnapshot(t *testing.T, body string, seen map[string]time.Time) *signalKSnapshot {
	t.Helper()
	snapshot := snapshotFromSelfTreeJSON(t, body)
	for path, at := range seen {
		snapshot.pathSeen["vessels.self|"+path] = at
	}
	return snapshot
}

func TestTelemetryViewMissingPathIsNotOK(t *testing.T) {
	view := telemetrySnapshot(t, `{"environment":{"depth":{"belowTransducer":{"value":4.2}}}}`, nil).selfTelemetryAt(telemetryTestNow)

	if v, ok := view.Number("environment.depth.belowKeel"); ok {
		t.Fatalf("missing path must report ok=false, got %+v", v)
	}
	if s, ok := view.String("navigation.state"); ok || s != "" {
		t.Fatalf("missing string must report ok=false, got %q", s)
	}
	if sample := view.Sample("environment.depth.belowKeel"); sample.Present || sample.AgeKnown {
		t.Fatalf("missing sample must be absent with no age, got %+v", sample)
	}
}

func TestTelemetryViewAgeComesFromTheSourceTimestampFirst(t *testing.T) {
	// Arrival time says 2 s, the source's own timestamp says 30 s: a reconnect
	// replay resets arrival but not the timestamp, so the timestamp wins.
	view := telemetrySnapshot(t,
		`{"environment":{"depth":{"belowTransducer":{"value":4.2,"$source":"nmea.SD","timestamp":"2026-10-15T01:59:30Z"}}}}`,
		map[string]time.Time{"environment.depth.belowTransducer": telemetryTestNow.Add(-2 * time.Second)},
	).selfTelemetryAt(telemetryTestNow)

	v, ok := view.Number("environment.depth.belowTransducer")
	if !ok {
		t.Fatal("expected depth to be present")
	}
	if v.Value != 4.2 || !v.AgeKnown || v.Age != 30*time.Second || v.Source != "nmea.SD" {
		t.Fatalf("got %+v, want 4.2 aged 30s from nmea.SD", v)
	}
}

func TestTelemetryViewAgeFallsBackToArrivalTime(t *testing.T) {
	view := telemetrySnapshot(t,
		`{"environment":{"depth":{"belowTransducer":{"value":4.2}}}}`,
		map[string]time.Time{"environment.depth.belowTransducer": telemetryTestNow.Add(-7 * time.Second)},
	).selfTelemetryAt(telemetryTestNow)

	v, ok := view.Number("environment.depth.belowTransducer")
	if !ok || !v.AgeKnown || v.Age != 7*time.Second {
		t.Fatalf("got %+v ok=%v, want an arrival-time age of 7s", v, ok)
	}
}

func TestTelemetryViewAgeUnknownWithNoTimestampAndNoArrival(t *testing.T) {
	view := telemetrySnapshot(t, `{"environment":{"depth":{"belowTransducer":{"value":4.2}}}}`, nil).selfTelemetryAt(telemetryTestNow)

	v, ok := view.Number("environment.depth.belowTransducer")
	if !ok {
		t.Fatal("expected depth to be present")
	}
	if v.AgeKnown || v.Age != 0 {
		t.Fatalf("an age with no evidence must be unknown, never -1 and never fresh: %+v", v)
	}
}

func TestTelemetryViewReturnsARealNegativeOne(t *testing.T) {
	view := telemetrySnapshot(t, `{"electrical":{"batteries":{"0":{"current":{"value":-1}}}}}`, nil).selfTelemetryAt(telemetryTestNow)

	v, ok := view.Number("electrical.batteries.0.current")
	if !ok || v.Value != -1 {
		t.Fatalf("a real -1 A is a reading, got %+v ok=%v", v, ok)
	}
}

func TestTelemetryViewNullLeafIsNotANumber(t *testing.T) {
	view := telemetrySnapshot(t, `{"electrical":{"alternator":{"0":{"voltage":{"value":null,"timestamp":"2026-10-15T01:59:00Z"}}}}}`, nil).selfTelemetryAt(telemetryTestNow)

	if _, ok := view.Number("electrical.alternator.0.voltage"); ok {
		t.Fatal("a null leaf must not read as a number")
	}
	sample := view.Sample("electrical.alternator.0.voltage")
	if !sample.Present || sample.Value != nil || !sample.AgeKnown || sample.Age != time.Minute {
		t.Fatalf("a null leaf is still a leaf with an age: %+v", sample)
	}
}

func TestTelemetryViewUnwrapsValueThenBare(t *testing.T) {
	view := telemetrySnapshot(t, `{
		"name": "Pikorua",
		"design": {"length": {"value": {"overall": 17.9}}, "draft": {"maximum": 1.2}},
		"navigation": {"state": "anchored", "datetime": {"value": "2026-10-15T01:59:59Z"}},
		"electrical": {"generator": {"0": {"manualStart": {"value": 1}}}}
	}`, nil).selfTelemetryAt(telemetryTestNow)

	if v, ok := view.Number("design.length.overall"); !ok || v.Value != 17.9 {
		t.Fatalf("value inside an object leaf: got %+v ok=%v", v, ok)
	}
	if v, ok := view.Number("design.draft.maximum"); !ok || v.Value != 1.2 {
		t.Fatalf("bare value: got %+v ok=%v", v, ok)
	}
	if s, ok := view.String("navigation.state"); !ok || s != "anchored" {
		t.Fatalf("bare string: got %q ok=%v", s, ok)
	}
	if s, ok := view.String("navigation.datetime"); !ok || s != "2026-10-15T01:59:59Z" {
		t.Fatalf("wrapped string: got %q ok=%v", s, ok)
	}
	if s, ok := view.String("name"); !ok || s != "Pikorua" {
		t.Fatalf("top-level string: got %q ok=%v", s, ok)
	}
	if b, ok := view.Bool("electrical.generator.0.manualStart"); !ok || !b {
		t.Fatalf("numeric bool: got %v ok=%v", b, ok)
	}
}

func TestTelemetryViewSampleIsStrictAboutLeaves(t *testing.T) {
	// The alarm reader and gauges bind a leaf exactly; a bare value or a
	// field inside an object leaf is not a bindable leaf.
	view := telemetrySnapshot(t, `{"design":{"draft":{"maximum":1.2}},"navigation":{"position":{"value":{"latitude":-19.2,"longitude":146.8}}}}`, nil).selfTelemetryAt(telemetryTestNow)

	if s := view.Sample("design.draft.maximum"); s.Present {
		t.Fatalf("a bare value is not a leaf: %+v", s)
	}
	if s := view.Sample("navigation.position.latitude"); s.Present {
		t.Fatalf("a field of an object leaf is not a leaf: %+v", s)
	}
	if s := view.Sample("navigation.position"); !s.Present {
		t.Fatalf("navigation.position is a leaf: %+v", s)
	}
}

func TestTelemetryViewPositionIsAbsentNotMinusOne(t *testing.T) {
	view := telemetrySnapshot(t, `{"navigation":{"speedOverGround":{"value":0.1}}}`, nil).selfTelemetryAt(telemetryTestNow)
	if p, ok := view.Position(); ok || p.Latitude == -1 || p.Longitude == -1 {
		t.Fatalf("no fix must be ok=false with no -1 sentinel, got %+v ok=%v", p, ok)
	}

	view = telemetrySnapshot(t,
		`{"navigation":{"position":{"value":{"latitude":-19.25,"longitude":146.82},"$source":"gps.GP","timestamp":"2026-10-15T01:59:58Z"}}}`,
		nil,
	).selfTelemetryAt(telemetryTestNow)
	p, ok := view.Position()
	if !ok || p.Latitude != -19.25 || p.Longitude != 146.82 || !p.AgeKnown || p.Age != 2*time.Second || p.Source != "gps.GP" {
		t.Fatalf("got %+v ok=%v", p, ok)
	}
}

func TestTelemetryViewWithNoSelfTreeReadsNothing(t *testing.T) {
	view := newSignalKSnapshot().selfTelemetryAt(telemetryTestNow)
	if view.HasSelf() {
		t.Fatal("no hello frame yet: there is no self tree")
	}
	if _, ok := view.Number("navigation.speedOverGround"); ok {
		t.Fatal("expected nothing to read")
	}
	if got := view.Children("electrical.batteries"); len(got) != 0 {
		t.Fatalf("expected no children, got %v", got)
	}
}

func TestTelemetryViewChildrenAndTimestamp(t *testing.T) {
	view := telemetrySnapshot(t, `{"electrical":{"batteries":{
		"1": {"voltage": {"value": 12.8, "timestamp": "2026-10-15T01:59:00Z"}},
		"0": {"voltage": {"value": 26.4, "timestamp": "2026-10-15T01:59:50Z"}, "current": {"value": -3}},
		"note": "not a branch"
	}}}`, nil).selfTelemetryAt(telemetryTestNow)

	if got, want := view.Children("electrical.batteries"), []string{"0", "1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("children: got %v, want %v", got, want)
	}
	ts, ok := view.Timestamp("electrical")
	if !ok || !ts.Equal(time.Date(2026, 10, 15, 1, 59, 50, 0, time.UTC)) {
		t.Fatalf("newest timestamp in the subtree: got %v ok=%v", ts, ok)
	}
	if _, ok := view.Timestamp("electrical.batteries.0.current"); ok {
		t.Fatal("a leaf with no timestamp has none")
	}
}

func TestLiveTelemetryViewCopiesNoWholeTree(t *testing.T) {
	snapshot := telemetrySnapshot(t, `{"navigation":{"speedOverGround":{"value":2.5}}}`, nil)
	before := snapshotWholeTreeCopies
	view := snapshot.liveSelfTelemetry(telemetryTestNow)
	if v, ok := view.Number("navigation.speedOverGround"); !ok || v.Value != 2.5 {
		t.Fatalf("got %+v ok=%v", v, ok)
	}
	if after := snapshotWholeTreeCopies; after != before {
		t.Fatalf("a live view must copy only what it returns, not the tree (%d copies)", after-before)
	}
}

// A real -1 A on the house bank is a reading all the way to the payload:
// the Battery & Power tile gets -1, not "unknown".
func TestRealNegativeOneAmpReachesTheElectricalPayload(t *testing.T) {
	t.Setenv("SETTINGS_FILE", t.TempDir()+"/settings.yaml")
	seedSelfTree(t, `{"electrical":{"batteries":{"0":{
		"capacity": {"stateOfCharge": {"value": 0.8}},
		"current": {"value": -1},
		"voltage": {"value": 26.4}
	}}}}`)

	if v, ok := globalSignalKSnapshot.selfTelemetry().Number("electrical.batteries.0.current"); !ok || v.Value != -1 {
		t.Fatalf("view: got %+v ok=%v, want -1 ok=true", v, ok)
	}
	state, err := fetchSignalKElectricalState(vesselSettings{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0"}})
	if err != nil {
		t.Fatal(err)
	}
	if state.ChargingCurrentA == nil || *state.ChargingCurrentA != -1 {
		t.Fatalf("payload: got %v, want -1 A", state.ChargingCurrentA)
	}
}

// A fallback chain stops at the first path that reports, even when it
// reports exactly -1; it no longer skips on to the next path as if -1 meant
// missing.
func TestFallbackChainStopsAtARealNegativeOne(t *testing.T) {
	reader := fakeTelemetry{Samples: map[string]telemetrySample{
		"electrical.venus.dcCurrent":      {Present: true, Value: -1.0},
		"electrical.dc.12v.current":       {Present: true, Value: 5.0},
		"electrical.venus.dcPower":        {Present: true, Value: 24.0},
		"electrical.batteries.51.voltage": {Present: true, Value: 12.0},
	}}

	if v, ok := firstNumber(reader, "electrical.venus.dcCurrent", "electrical.dc.12v.current"); !ok || v != -1 {
		t.Fatalf("firstNumber: got %v ok=%v, want -1 ok=true", v, ok)
	}

	// The 12 V current is unsigned on the tile, so a -1 is not shown as a
	// current; it is worked out from power and voltage instead, never taken
	// from the next path in the chain.
	state := electricalStateFromTelemetry(reader, electricalStateData{DC12VPowerW: -1, DC12VCurrentA: -1, VoltageReadoutV: -1, ACOutputW: -1, ACLoadsW: -1, SolarOutputW: -1, BatterySocPercent: -1, BatteryCapacityAh: -1, LastUpdateAge: -1}, vesselSettings{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.51"}})
	if state.DC12VCurrentA != 2 {
		t.Fatalf("DC12VCurrentA: got %v, want 2 (24 W / 12 V), not 5 from the next path", state.DC12VCurrentA)
	}
}

// Half a position is no position: Position reports nothing and the vessel
// state carries no fix, rather than a latitude paired with a -1 longitude.
func TestHalfAPositionIsNoFix(t *testing.T) {
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)

	view := telemetryViewFromTree(map[string]any{
		"navigation": map[string]any{"position": map[string]any{"value": map[string]any{"latitude": -19.25}}},
	}, telemetryTestNow)
	if p, ok := view.Position(); ok {
		t.Fatalf("expected no position, got %+v", p)
	}

	state := vesselStateFromTelemetry(view, vesselStateData{Status: "Unknown", Datetime: telemetryTestNow, Depth: -1, Latitude: -1, Longitude: -1}, vesselSettings{})
	if state.Latitude != -1 || state.Longitude != -1 {
		t.Fatalf("expected no fix (-1, -1) on the wire, got (%v, %v)", state.Latitude, state.Longitude)
	}
}
