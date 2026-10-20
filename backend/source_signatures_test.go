package main

import (
	"encoding/json"
	"errors"
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

// signatureHistoryFixture reads the signature paths the boat's InfluxDB held
// on 2026-10-21 (90 days, captured from the real server): the engines logged
// under propulsion.port and propulsion.starboard, none under propulsion.0/1.
func signatureHistoryFixture(t *testing.T) sourceHistory {
	t.Helper()
	raw, err := os.ReadFile("testdata/source_signatures/influx-signature-history-2026-10-21.csv")
	if err != nil {
		t.Fatal(err)
	}
	h := sourceHistory{Last: map[string]time.Time{}}
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) != 5 || f[1] != "" || f[3] == "_time" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, f[3])
		if err != nil {
			continue
		}
		h.Last[f[4]] = at
	}
	if len(h.Last) < 30 {
		t.Fatalf("fixture parsed %d paths", len(h.Last))
	}
	return h
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

func TestSignatureFluxRegexMatchesSignaturesOnly(t *testing.T) {
	flux, err := buildInfluxSourceSignatureFlux("SignalK_Data", "value", sourceHistoryLookbackDays)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(flux, "${") {
		t.Fatalf("flux holds interpolation syntax: %s", flux)
	}
	re := regexp.MustCompile(signatureFluxRegex())
	for _, path := range []string{
		"propulsion.port.revolutions", "electrical.batteries.239.voltage", "tanks.fuel.2.currentLevel",
		"electrical.inverters.276.acout.power", "electrical.solar.288.panelPower", "electrical.generator.0.state",
	} {
		if !re.MatchString(path) {
			t.Errorf("%s should be a signature path", path)
		}
	}
	for _, path := range []string{
		"propulsion.0.exhaustTemperature", "propulsion.port.fuel.rate", "propulsion.port.transmission.oilPressure",
		"electrical.batteries.3.capacity.timeRemaining", "electrical.inverters.5.operatingState",
	} {
		if re.MatchString(path) {
			t.Errorf("%s is not a signature path", path)
		}
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
	if st.State != setupTodo || !strings.Contains(st.Detail, "2 engine sources in the boat's logged history") {
		t.Fatalf("step = %+v", st)
	}
}
