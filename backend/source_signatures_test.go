package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// signatureHistoryFixture reads the paths under the equipment sources that the
// boat's InfluxDB held on 2026-10-21 (90 days, captured from the real server):
// the engines logged revolutions under propulsion.port and propulsion.starboard,
// and only exhaustTemperature under propulsion.0/1.
func signatureHistoryFixture(t *testing.T) sourceHistory {
	t.Helper()
	raw, err := os.ReadFile("testdata/source_signatures/influx-history-2026-10-21.csv")
	if err != nil {
		t.Fatal(err)
	}
	last := map[string]time.Time{}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) != 5 || f[1] != "" || f[3] == "_time" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, f[3])
		if err != nil {
			continue
		}
		last[f[4]] = at
	}
	if len(last) < 200 {
		t.Fatalf("fixture parsed %d paths", len(last))
	}
	return newSourceHistory(last)
}

// The boat as found on 2026-10-21: engines off, so propulsion.port and
// starboard are not in the live tree, and the two Cummins items were assigned
// to propulsion.0 and .1, which only ever carry exhaust temperature.
func exhaustOnlySnapshot(t *testing.T) *signalKSnapshot {
	t.Helper()
	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "propulsion.0.exhaustTemperature", 300, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "propulsion.1.exhaustTemperature", 301, anomalyDetectorTestNow)
	return snapshot
}

func cumminsItems() []sourceItem {
	return []sourceItem{
		{ID: "c0", Name: "Cummins QSB 6.7", Source: "propulsion.0", Group: sourceEngine, Instance: "0"},
		{ID: "c1", Name: "Cummins QSB 6.7", Source: "propulsion.1", Group: sourceEngine, Instance: "1"},
	}
}

func TestHistoryFluxRegexMatchesEveryPathUnderASource(t *testing.T) {
	flux, err := buildInfluxSourceHistoryFlux("SignalK_Data", "value", sourceHistoryLookbackDays)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flux, "${") {
		t.Fatalf("flux holds interpolation syntax: %s", flux)
	}
	re := regexp.MustCompile(historyFluxRegex())
	for _, path := range []string{
		"propulsion.port.revolutions", "propulsion.0.exhaustTemperature", "propulsion.port.transmission.oilPressure",
		"electrical.batteries.239.voltage", "electrical.batteries.3.capacity.timeRemaining", "tanks.fuel.2.currentLevel",
		"electrical.inverters.5.operatingState", "electrical.solar.288.panelPower",
	} {
		if !re.MatchString(path) {
			t.Errorf("%s is under an equipment source", path)
		}
	}
	for _, path := range []string{"navigation.speedOverGround", "electrical.ac.5.phase.A.realPower", "environment.depth.belowTransducer"} {
		if re.MatchString(path) {
			t.Errorf("%s is not under an equipment source", path)
		}
	}
}

func TestSourceOfPath(t *testing.T) {
	for path, want := range map[string]string{
		"propulsion.port.fuel.rate":                     "propulsion.port",
		"electrical.batteries.0.capacity.stateOfCharge": "electrical.batteries.0",
		"tanks.fuel.2.currentLevel":                     "tanks.fuel.2",
		"electrical.inverters.276.acout.power":          "electrical.inverters.276",
	} {
		if got, ok := sourceOfPath(path); !ok || got != want {
			t.Errorf("%s -> %q %v, want %q", path, got, ok, want)
		}
	}
	if _, ok := sourceOfPath("navigation.speedOverGround"); ok {
		t.Error("navigation is not a source")
	}
}

func TestDiscoveryOffersLoggedEnginesNotStrayInstances(t *testing.T) {
	got := vesselDiscovery(exhaustOnlySnapshot(t), vesselView{}, nil, signatureHistoryFixture(t))
	rows := discoveryGroupRows(got, "engine")
	var sources []string
	for _, r := range rows {
		sources = append(sources, r.Source)
		if r.Item != nil || r.Signature != signatureCarried {
			t.Errorf("unowned offer should carry its signature: %+v", r)
		}
	}
	if strings.Join(sources, ",") != "propulsion.port,propulsion.starboard" {
		t.Fatalf("engines offered = %v", sources)
	}
	port := rows[0]
	if port.Reporting || len(port.Metrics) != 0 {
		t.Errorf("port is off: %+v", port)
	}
	want := time.Date(2026, 10, 16, 0, 56, 35, 331000000, time.UTC)
	if port.LastLogged == nil || !port.LastLogged.Equal(want) {
		t.Errorf("port last logged = %v", port.LastLogged)
	}
	if got.HistoryError != "" {
		t.Errorf("history error = %q", got.HistoryError)
	}
	if label := discoveryGroupLabel(got, "engine"); label != "RPM" {
		t.Errorf("engine signature label = %q", label)
	}
}

func TestDiscoveryFlagsAnItemOnASourceThatNeverReportsItsSignature(t *testing.T) {
	got := vesselDiscovery(exhaustOnlySnapshot(t), vesselView{}, cumminsItems(), signatureHistoryFixture(t))
	by := map[string]discoveryRow{}
	for _, r := range discoveryGroupRows(got, "engine") {
		by[r.Source] = r
	}
	if len(by) != 4 {
		t.Fatalf("two offers plus the two owned sources, got %+v", by)
	}
	for _, src := range []string{"propulsion.0", "propulsion.1"} {
		r := by[src]
		if r.Item == nil || r.Signature != signatureMissing || r.LastLogged != nil {
			t.Errorf("%s should be owned and missing RPM: %+v", src, r)
		}
	}
	if by["propulsion.port"].Signature != signatureCarried {
		t.Errorf("port: %+v", by["propulsion.port"])
	}
}

func TestDiscoveryQualifiesOnTheLiveTreeWithoutHistory(t *testing.T) {
	snapshot := newVesselCandidatesSnapshot(t)
	got := vesselDiscovery(snapshot, vesselView{}, nil, sourceHistory{})
	rows := discoveryGroupRows(got, "engine")
	if len(rows) != 2 || !rows[0].Reporting || rows[0].LastLogged != nil || rows[0].Signature != signatureCarried {
		t.Fatalf("live engines: %+v", rows)
	}
}

func TestDiscoveryWithoutHistoryListsLiveOnlyAndSaysSo(t *testing.T) {
	snapshot := exhaustOnlySnapshot(t)
	applyNumeric(snapshot, "vessels.self", "propulsion.port.revolutions", 30, anomalyDetectorTestNow)
	h := sourceHistory{Err: errors.New("influxdb is not configured")}
	got := vesselDiscovery(snapshot, vesselView{}, cumminsItems(), h)
	if !strings.Contains(got.HistoryError, "not configured") {
		t.Fatalf("history error = %q", got.HistoryError)
	}
	by := map[string]discoveryRow{}
	for _, r := range discoveryGroupRows(got, "engine") {
		by[r.Source] = r
	}
	if by["propulsion.port"].Signature != signatureCarried || by["propulsion.starboard"].Source != "" {
		t.Errorf("only the live engine is offered: %+v", by)
	}
	// Unknown is not bad: the item's source is not live and history is gone.
	if by["propulsion.0"].Signature != signatureUnknown {
		t.Errorf("propulsion.0 = %+v", by["propulsion.0"])
	}
}

func TestDiscoveryOffersEveryKindByItsSignature(t *testing.T) {
	got := vesselDiscovery(newAnomalyTestSnapshot(), vesselView{}, nil, signatureHistoryFixture(t))
	count := func(kind string) int { return len(discoveryGroupRows(got, kind)) }
	// batteries.3 logged a voltage; batteries.N with only timeRemaining would not be.
	if n := count("battery"); n != 9 {
		t.Errorf("batteries = %d", n)
	}
	if n := count("tank"); n != 10 {
		t.Errorf("tanks = %d", n)
	}
	if n := count("inverter"); n != 1 {
		t.Errorf("inverters = %d (inverters.5 has no AC output)", n)
	}
	if n := count("solar"); n != 4 {
		t.Errorf("solar = %d", n)
	}
}

func TestSetupEnginesStepFlagsAnEngineOnASourceWithoutRPM(t *testing.T) {
	vessel := vesselView{Engines: []vesselEngineSetting{
		{Instance: "0", Name: "Cummins QSB 6.7", EquipmentID: "c0"},
		{Instance: "1", Name: "Cummins QSB 6.7", EquipmentID: "c1"},
	}}
	disc := vesselDiscovery(exhaustOnlySnapshot(t), vessel, cumminsItems(), signatureHistoryFixture(t))
	disc.Connected = true
	st := stepByID(t, buildSetupStatus(vessel, disc, setupMate{}), "engines")
	if st.State != setupTodo {
		t.Fatalf("state = %s (%s)", st.State, st.Detail)
	}
	for _, want := range []string{"Cummins QSB 6.7", "propulsion.0", "propulsion.1", "RPM"} {
		if !strings.Contains(st.Detail, want) {
			t.Errorf("detail %q lacks %q", st.Detail, want)
		}
	}
	if st.Target.Section != "signalk" || st.Target.Control != "discovery-engine" {
		t.Errorf("target = %+v", st.Target)
	}
}

func TestSetupEnginesStepDoesNotReopenOnAnUncheckableSource(t *testing.T) {
	vessel := vesselView{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port", EquipmentID: "p"}}}
	items := []sourceItem{{ID: "p", Name: "Port", Source: "propulsion.port", Group: sourceEngine, Instance: "port"}}
	disc := vesselDiscovery(newAnomalyTestSnapshot(), vessel, items, sourceHistory{Err: errors.New("down")})
	disc.Connected = true
	st := stepByID(t, buildSetupStatus(vessel, disc, setupMate{}), "engines")
	if st.State != setupDone || !strings.Contains(st.Detail, "couldn't check") {
		t.Fatalf("step = %+v", st)
	}
}

func TestSetupEnginesStepStaysDoneOnALoggedSource(t *testing.T) {
	vessel := vesselView{Engines: []vesselEngineSetting{{Instance: "port", Name: "Port", EquipmentID: "p"}}}
	items := []sourceItem{{ID: "p", Name: "Port", Source: "propulsion.port", Group: sourceEngine, Instance: "port"}}
	disc := vesselDiscovery(newAnomalyTestSnapshot(), vessel, items, signatureHistoryFixture(t))
	disc.Connected = false // a SignalK dropout does not reopen a finished setup
	st := stepByID(t, buildSetupStatus(vessel, disc, setupMate{}), "engines")
	if st.State != setupDone || strings.Contains(st.Detail, "couldn't") {
		t.Fatalf("step = %+v", st)
	}
}

func discoveryGroupLabel(r discoveryResponse, kind string) string {
	for _, g := range r.Groups {
		if g.Kind == kind {
			return g.SignatureLabel
		}
	}
	return ""
}

// Both read endpoints share one candidate list, so the equipment page's
// Source select, Discovery and Roles cannot disagree.
func TestDiscoveryHandlerOffersLoggedEnginesAndSaysWhenHistoryIsGone(t *testing.T) {
	prevSnapshot := globalSignalKSnapshot
	globalSignalKSnapshot = exhaustOnlySnapshot(t)
	prevLoad := loadSourceHistory
	t.Cleanup(func() { globalSignalKSnapshot, loadSourceHistory = prevSnapshot, prevLoad })
	t.Setenv("SETTINGS_FILE", filepath.Join(t.TempDir(), "settings.yaml"))
	store := vesselFixtureStore(t)
	if _, err := store.CreateEquipment(equipmentItem{Name: "Cummins QSB 6.7", SignalKSource: "propulsion.0"}); err != nil {
		t.Fatal(err)
	}

	read := func() discoveryResponse {
		rec := httptest.NewRecorder()
		c := echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/vessel/discovery", nil), rec)
		if err := getVesselDiscoveryHandler(c); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("handler: %v %d %s", err, rec.Code, rec.Body.String())
		}
		var body discoveryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}

	h := signatureHistoryFixture(t)
	loadSourceHistory = func() sourceHistory { return h }
	rows := discoveryGroupRows(read(), "engine")
	if len(rows) != 3 || rows[0].Signature != signatureMissing || rows[1].Source != "propulsion.port" || rows[1].LastLogged == nil {
		t.Fatalf("rows = %+v", rows)
	}

	loadSourceHistory = func() sourceHistory { return sourceHistory{Err: errors.New("influx: connection refused")} }
	body := read()
	if body.HistoryError == "" || discoveryGroupRows(body, "engine")[0].Signature != signatureUnknown {
		t.Fatalf("history down: %+v", body)
	}
}

func TestSetupEnginesStepOffersLoggedEnginesWhenNoneIsAssigned(t *testing.T) {
	disc := vesselDiscovery(newAnomalyTestSnapshot(), vesselView{}, nil, signatureHistoryFixture(t))
	disc.Connected = true
	st := stepByID(t, buildSetupStatus(vesselView{}, disc, setupMate{}), "engines")
	if st.State != setupTodo || !strings.Contains(st.Detail, "2 engine sources are available") {
		t.Fatalf("step = %+v", st)
	}
}

// "Missing" means alive in the logged history and never carrying the signature.
// A source with nothing logged at all (laid up, short retention) is unknown:
// it is never flagged and never reopens setup.
func TestSilentSourceIsUnknownNotMissing(t *testing.T) {
	h := signatureHistoryFixture(t)
	if got := signatureState(false, h, sourceEngine, "propulsion.0"); got != signatureMissing {
		t.Errorf("propulsion.0 logged exhaust temperature and never RPM: %s", got)
	}
	if got := signatureState(false, h, sourceEngine, "propulsion.2"); got != signatureUnknown {
		t.Errorf("propulsion.2 logged nothing: %s", got)
	}

	vessel := vesselView{Engines: []vesselEngineSetting{{Instance: "2", Name: "Laid-up engine", EquipmentID: "e2"}}}
	items := []sourceItem{{ID: "e2", Name: "Laid-up engine", Source: "propulsion.2", Group: sourceEngine, Instance: "2"}}
	disc := vesselDiscovery(newAnomalyTestSnapshot(), vessel, items, h)
	disc.Connected = true
	var row discoveryRow
	for _, r := range discoveryGroupRows(disc, "engine") {
		if r.Source == "propulsion.2" {
			row = r
		}
	}
	if row.Signature != signatureUnknown {
		t.Fatalf("row = %+v", row)
	}
	st := stepByID(t, buildSetupStatus(vessel, disc, setupMate{}), "engines")
	if st.State != setupDone || !strings.Contains(st.Detail, "couldn't check") {
		t.Fatalf("a silent source must not reopen setup: %+v", st)
	}
}

// A charger's own battery input that was logged but is not in the live tree
// stays marked as a charger input, so it is not offered as a house bank.
func TestLoggedChargerInputKeepsItsFlag(t *testing.T) {
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	// Instance 77 is the charger input "1" once more, seen only in the log.
	ctx := snapshot.selfContext()
	snapshot.mu.Lock()
	snapshot.batteryPublishers[ctx+"|77"] = snapshot.batteryPublishers[ctx+"|1"]
	snapshot.mu.Unlock()
	h := newSourceHistory(map[string]time.Time{"electrical.batteries.77.voltage": time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)})

	var row discoveryRow
	for _, r := range discoveryGroupRows(vesselDiscovery(snapshot, vesselView{}, nil, h), "battery") {
		if r.Instance == "77" {
			row = r
		}
	}
	if row.Source == "" || row.Reporting || !row.ChargerInput {
		t.Fatalf("logged charger input: %+v", row)
	}
}

// ── history loading ─────────────────────────────────────────────────────

func resetSourceHistoryLoader(t *testing.T) {
	t.Helper()
	prevFetch, prevNow := fetchSourceHistory, sourceHistoryNow
	sourceHistoryState.mu.Lock()
	sourceHistoryState.have, sourceHistoryState.inflight, sourceHistoryState.failing = false, nil, false
	sourceHistoryState.mu.Unlock()
	t.Cleanup(func() {
		fetchSourceHistory, sourceHistoryNow = prevFetch, prevNow
		sourceHistoryState.mu.Lock()
		sourceHistoryState.have, sourceHistoryState.inflight, sourceHistoryState.failing = false, nil, false
		sourceHistoryState.mu.Unlock()
	})
}

func TestSourceHistoryLoaderIsSingleFlight(t *testing.T) {
	resetSourceHistoryLoader(t)
	release := make(chan struct{})
	var calls atomic.Int32
	fetchSourceHistory = func() (map[string]time.Time, error) {
		calls.Add(1)
		<-release
		return map[string]time.Time{"propulsion.port.revolutions": time.Unix(1, 0)}, nil
	}
	var wg sync.WaitGroup
	results := make([]sourceHistory, 5)
	for i := range results {
		wg.Add(1)
		go func() { defer wg.Done(); results[i] = readSourceHistory() }()
	}
	time.Sleep(50 * time.Millisecond) // let every caller reach the wait
	close(release)
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("queries = %d, want 1 in flight for all callers", n)
	}
	for i, r := range results {
		if r.Err != nil || len(r.Last) != 1 {
			t.Errorf("caller %d: %+v", i, r)
		}
	}
}

func TestSourceHistoryLoaderCachesFailuresBrieflyAndLogsOncePerStreak(t *testing.T) {
	resetSourceHistoryLoader(t)
	var logs bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	sourceHistoryNow = func() time.Time { return now }
	var calls int
	var fail = true
	fetchSourceHistory = func() (map[string]time.Time, error) {
		calls++
		if fail {
			return nil, errors.New("connection refused")
		}
		return map[string]time.Time{}, nil
	}

	for i := 0; i < 3; i++ {
		if r := readSourceHistory(); r.Err == nil {
			t.Fatal("a failure must be returned, not an empty history")
		}
	}
	if calls != 1 {
		t.Fatalf("a failure is reused for %v: queries = %d", sourceHistoryFailureTTL, calls)
	}
	now = now.Add(sourceHistoryFailureTTL + time.Second)
	readSourceHistory()
	if calls != 2 {
		t.Fatalf("after the failure window the loader asks again: %d", calls)
	}
	if n := strings.Count(logs.String(), "connection refused"); n != 1 {
		t.Fatalf("a failure streak logs once, got %d:\n%s", n, logs.String())
	}

	fail = false
	now = now.Add(sourceHistoryFailureTTL + time.Second)
	if r := readSourceHistory(); r.Err != nil {
		t.Fatal(r.Err)
	}
	if !strings.Contains(logs.String(), "readable again") {
		t.Fatalf("recovery is logged once:\n%s", logs.String())
	}
}

// One noun table serves the setup sentences and the conversion; a kind
// without a noun or a signature is a coding error caught here, not a panic
// or a blank in a sentence.
func TestEverySourceKindHasANounAndASignature(t *testing.T) {
	for _, group := range sourceGroupOrder {
		if sourceKindNouns[group] == "" {
			t.Errorf("%s has no noun", group)
		}
		if sig := sourceSignatures[group]; sig.label == "" || len(sig.leaves) == 0 {
			t.Errorf("%s has no signature", group)
		}
	}
	if got := withArticle(""); got != "" {
		t.Errorf("withArticle of nothing = %q", got)
	}
	if got := withArticle("engine"); got != "an engine" {
		t.Errorf("withArticle = %q", got)
	}
}

// The power step judges the items chosen for the power roles, not every
// alternator, charger, inverter or generator item.
func TestSetupPowerStepJudgesOnlyChosenItems(t *testing.T) {
	vessel := vesselView{
		Alternators: vesselAlternatorSettings{PortInstance: "1"},
		RolesNone:   fullPowerNone(),
	}
	alternators := discoveryGroup{Kind: "alternator", Mappable: true, SignatureLabel: "output current", Rows: []discoveryRow{
		{Source: "electrical.alternator.1", Instance: "1", Item: &discoveryItemRef{ID: "a1", Name: "Port alternator"}, Signature: signatureCarried},
		{Source: "electrical.alternator.9", Instance: "9", Item: &discoveryItemRef{ID: "a9", Name: "Old alternator"}, Signature: signatureMissing},
	}}
	st := stepByID(t, buildSetupStatus(vessel, connectedDiscovery(alternators), setupMate{}), "power")
	if st.State != setupDone || strings.Contains(st.Detail, "Old alternator") {
		t.Fatalf("an item no role reads must not hold the step open: %+v", st)
	}

	alternators.Rows[0].Signature = signatureMissing
	st = stepByID(t, buildSetupStatus(vessel, connectedDiscovery(alternators), setupMate{}), "power")
	if st.State != setupTodo || !strings.Contains(st.Detail, "Port alternator") || strings.Contains(st.Detail, "Old alternator") {
		t.Fatalf("the chosen port alternator is judged: %+v", st)
	}
}
