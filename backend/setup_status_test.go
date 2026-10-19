package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func connectedDiscovery(groups ...discoveryGroup) discoveryResponse {
	return discoveryResponse{Connected: true, Groups: groups}
}

func stepByID(t *testing.T, s setupStatus, id string) setupStep {
	t.Helper()
	for _, st := range s.Steps {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("no step %q in %+v", id, s.Steps)
	return setupStep{}
}

func fullPowerNone() map[string]bool {
	return map[string]bool{
		"port_alternator": true, "starboard_alternator": true, "shore_charger": true,
		"inverter": true, "generator": true, "generator_ac_instance": true,
	}
}

func TestSetupStatusStepOrderAndTitles(t *testing.T) {
	s := buildSetupStatus(vesselView{}, connectedDiscovery(), setupMate{})
	var ids []string
	for _, st := range s.Steps {
		ids = append(ids, st.ID)
	}
	if got := strings.Join(ids, ","); got != "connection,engines,house_bank,power" {
		t.Fatalf("step order = %s", got)
	}
	if s.Complete {
		t.Fatal("a fresh boat is not complete")
	}
	if len(s.Steps) > 5 {
		t.Fatalf("at most 5 steps, got %d", len(s.Steps))
	}
	for _, st := range s.Steps {
		if st.Target.Section == "" || st.Target.Control == "" {
			t.Errorf("step %s has no target: %+v", st.ID, st.Target)
		}
	}
	if got := stepByID(t, s, "house_bank").Target; got.Section != "signalk" || got.Control != "role-house_bank" {
		t.Errorf("house bank target = %+v", got)
	}
}

func TestSetupStatusNotConnectedBlocksTheRest(t *testing.T) {
	s := buildSetupStatus(vesselView{}, discoveryResponse{Connected: false}, setupMate{})
	if stepByID(t, s, "connection").State != "todo" {
		t.Fatalf("connection should be todo: %+v", s.Steps)
	}
	for _, id := range []string{"engines", "house_bank", "power"} {
		st := stepByID(t, s, id)
		if st.State != "blocked" {
			t.Errorf("%s should be blocked, got %s", id, st.State)
		}
		if !strings.Contains(st.Detail, "Connect to SignalK") {
			t.Errorf("%s detail should name the step that comes first: %q", id, st.Detail)
		}
	}
}

func TestSetupStatusProgressesAndNoneCountsAsDone(t *testing.T) {
	vessel := vesselView{
		Engines:   []vesselEngineSetting{{Instance: "port", EquipmentID: "e1"}},
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512", EquipmentID: "b1"},
	}
	s := buildSetupStatus(vessel, connectedDiscovery(), setupMate{})
	if stepByID(t, s, "connection").State != "done" || stepByID(t, s, "engines").State != "done" || stepByID(t, s, "house_bank").State != "done" {
		t.Fatalf("first three done: %+v", s.Steps)
	}
	power := stepByID(t, s, "power")
	if power.State != "todo" {
		t.Fatalf("power todo while roles are open: %+v", power)
	}
	for _, name := range []string{"Port alternator", "Starboard alternator", "Shore charger", "Inverter", "Generator", "Generator AC input"} {
		if !strings.Contains(power.Detail, name) {
			t.Errorf("detail should name %q: %q", name, power.Detail)
		}
	}

	// Answering "none" for everything, with no figures, completes it.
	vessel.RolesNone = fullPowerNone()
	s = buildSetupStatus(vessel, connectedDiscovery(), setupMate{})
	if st := stepByID(t, s, "power"); st.State != "done" {
		t.Fatalf("none counts as done: %+v", st)
	}
	if !s.Complete {
		t.Fatalf("all four done is complete: %+v", s)
	}
}

func TestSetupStatusDetailNamesOnlyTheOpenPowerRoles(t *testing.T) {
	vessel := vesselView{
		Alternators: vesselAlternatorSettings{PortInstance: "2"},
		RolesNone:   map[string]bool{"inverter": true, "generator": true, "generator_ac_instance": true},
	}
	st := stepByID(t, buildSetupStatus(vessel, connectedDiscovery(), setupMate{}), "power")
	if strings.Contains(st.Detail, "Port alternator") || strings.Contains(st.Detail, "Inverter") {
		t.Errorf("answered roles must not be listed: %q", st.Detail)
	}
	if !strings.Contains(st.Detail, "Starboard alternator") || !strings.Contains(st.Detail, "Shore charger") {
		t.Errorf("open roles must be listed: %q", st.Detail)
	}
	if st.Target.Control != "role-shore_charger" {
		t.Errorf("target is the first open control, got %q", st.Target.Control)
	}
}

// A step that becomes undone reappears: status is computed from live state.
func TestSetupStatusRegressesWhenAStepBecomesUndone(t *testing.T) {
	vessel := vesselView{
		Engines:   []vesselEngineSetting{{Instance: "port"}},
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512"},
		RolesNone: fullPowerNone(),
	}
	if !buildSetupStatus(vessel, connectedDiscovery(), setupMate{}).Complete {
		t.Fatal("expected complete")
	}
	vessel.HouseBank = nil
	s := buildSetupStatus(vessel, connectedDiscovery(), setupMate{})
	if s.Complete || stepByID(t, s, "house_bank").State != "todo" {
		t.Fatalf("a cleared house bank is todo again: %+v", s)
	}
	vessel.HouseBank = &vesselHouseBankSetting{Path: "electrical.batteries.512"}
	vessel.Engines = nil
	if buildSetupStatus(vessel, connectedDiscovery(), setupMate{}).Complete {
		t.Fatal("a deleted engine item reopens the engines step")
	}
}

func TestSetupStatusCountsOnlyEngineSourcesThatCarryFigures(t *testing.T) {
	fig := []discoveryMetric{{Label: "RPM", Text: "1800 rpm"}, {Label: "Coolant", Text: "77 °C"}}
	engines := discoveryGroup{Kind: "engine", Mappable: true, Rows: []discoveryRow{
		{Source: "propulsion.0", Instance: "0", Reporting: true},
		{Source: "propulsion.1", Instance: "1", Reporting: true},
		{Source: "propulsion.port", Instance: "port", Reporting: true, Metrics: fig},
		{Source: "propulsion.starboard", Instance: "starboard", Reporting: true, Metrics: fig},
	}}
	st := stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(engines), setupMate{}), "engines")
	if strings.Contains(st.Detail, "4") || strings.Contains(st.Detail, "propulsion.") {
		t.Errorf("figureless sources must not be counted, and no raw paths: %q", st.Detail)
	}
	if !strings.Contains(st.Detail, "2 engine sources are reporting rpm and coolant") || !strings.Contains(st.Detail, "for each one that shows figures") {
		t.Errorf("engines detail: %q", st.Detail)
	}

	// All carry figures: each one is an engine.
	engines.Rows = engines.Rows[2:3]
	st = stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(engines), setupMate{}), "engines")
	if !strings.Contains(st.Detail, "1 engine source is reporting rpm and coolant") {
		t.Errorf("engines detail: %q", st.Detail)
	}

	// Sources but no figures anywhere.
	engines.Rows = []discoveryRow{{Source: "propulsion.0", Instance: "0", Reporting: true}}
	st = stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(engines), setupMate{}), "engines")
	if !strings.Contains(st.Detail, "no figures") {
		t.Errorf("engines detail: %q", st.Detail)
	}
}

func TestSetupStatusGeneratorNoneAnswersItsACInput(t *testing.T) {
	vessel := vesselView{RolesNone: map[string]bool{
		"port_alternator": true, "starboard_alternator": true, "shore_charger": true, "inverter": true, "generator": true,
	}}
	st := stepByID(t, buildSetupStatus(vessel, connectedDiscovery(), setupMate{}), "power")
	if st.State != "done" {
		t.Fatalf("no generator means no generator AC input to answer: %+v", st)
	}
	vessel.RolesNone["generator"] = false
	st = stepByID(t, buildSetupStatus(vessel, connectedDiscovery(), setupMate{}), "power")
	if st.State != "todo" || !strings.Contains(st.Detail, "Generator") {
		t.Fatalf("with a generator unanswered the AC input is open too: %+v", st)
	}
}

func TestSetupStatusDetailSaysWhatTheBoatReports(t *testing.T) {
	var st setupStep
	st = stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(), setupMate{}), "engines")
	if !strings.Contains(st.Detail, "isn't reporting any engines") {
		t.Errorf("engines detail with nothing reporting: %q", st.Detail)
	}
	st = stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(), setupMate{}), "house_bank")
	if !strings.Contains(st.Detail, "isn't reporting any batteries") {
		t.Errorf("house bank detail with nothing reporting: %q", st.Detail)
	}
}

// The Roles per-check statuses read the same readiness as the steps.
func TestDetectorsAgreeWithSetupSteps(t *testing.T) {
	cases := []vesselView{
		{},
		{Engines: []vesselEngineSetting{{Instance: "port"}}},
		{HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.1"}},
		{Engines: []vesselEngineSetting{{Instance: "a"}, {Instance: "b"}}, HouseBank: &vesselHouseBankSetting{}},
	}
	for i, v := range cases {
		disc := vesselDiscovery(newAnomalyTestSnapshot(), v, nil)
		disc.Connected = true // the steps are blocked without a feed; the checks do not care
		s := buildSetupStatus(v, disc, setupMate{})
		if got, want := disc.Detectors["frozen"].Ready, stepByID(t, s, "engines").State == "done"; got != want {
			t.Errorf("case %d: frozen ready=%v but engines step done=%v", i, got, want)
		}
		chosen := stepByID(t, s, "house_bank").State == "done"
		if !chosen && disc.Detectors["battery"].Ready {
			t.Errorf("case %d: battery check ready with no house bank chosen", i)
		}
		if !chosen && !strings.Contains(disc.Detectors["battery"].Missing, "house bank") {
			t.Errorf("case %d: battery missing line: %q", i, disc.Detectors["battery"].Missing)
		}
	}
}

func TestGetSetupStatusHandlerReadsRolesWithNone(t *testing.T) {
	prev := globalSignalKSnapshot
	globalSignalKSnapshot = newVesselCandidatesSnapshot(t)
	t.Cleanup(func() { globalSignalKSnapshot = prev })

	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	store := vesselFixtureStore(t)
	eng, err := store.CreateEquipment(equipmentItem{Name: "Port engine", SignalKSource: "propulsion.port"})
	if err != nil {
		t.Fatal(err)
	}
	bank, err := store.CreateEquipment(equipmentItem{Name: "House bank", SignalKSource: "electrical.batteries.512"})
	if err != nil {
		t.Fatal(err)
	}
	_ = eng
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Roles = &vesselRoles{
			HouseBank: bank.ID, PortAlternator: roleNone, StarboardAlternator: roleNone, ShoreCharger: roleNone,
			Inverter: roleNone, Generator: roleNone, GeneratorACInstance: roleNone,
		}
	})
	if code != http.StatusOK {
		t.Fatalf("saving none roles: %d %v", code, body)
	}

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := getSetupStatusHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/setup/status", nil), rec)); err != nil {
		t.Fatal(err)
	}
	var got setupStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if !got.Complete {
		t.Fatalf("expected complete, got %s", rec.Body.String())
	}
}

func TestSetupRoutesAreRegistered(t *testing.T) {
	found := false
	for _, r := range setupRoutes() {
		if r.Method == http.MethodGet && r.Path == "/api/setup/status" {
			found = true
		}
	}
	if !found {
		t.Fatal("GET /api/setup/status is not in setupRoutes")
	}
}

func TestSetupStatusHouseBankCountMatchesThePicker(t *testing.T) {
	batteries := discoveryGroup{Kind: "battery", Mappable: true, Rows: []discoveryRow{
		{Source: "electrical.batteries.0", Reporting: true},
		{Source: "electrical.batteries.1", Reporting: true, ChargerInput: true},
		{Source: "electrical.batteries.2", Reporting: true, Item: &discoveryItemRef{ID: "x", Name: "Starter"}},
		{Source: "electrical.batteries.3", Reporting: true},
	}}
	st := stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(batteries), setupMate{}), "house_bank")
	if !strings.Contains(st.Detail, "3 batteries") {
		t.Errorf("battery items and unassigned non-charger sources are offered, charger inputs are not: %q", st.Detail)
	}
}

// The Roles picker lists battery items first, then unowned sources. When every
// battery already has an item the step still has something to choose from.
func TestSetupStatusHouseBankCountsBatteryItemsTheRolesPickerOffers(t *testing.T) {
	batteries := discoveryGroup{Kind: "battery", Mappable: true, Rows: []discoveryRow{
		{Source: "electrical.batteries.0", Reporting: true, Item: &discoveryItemRef{ID: "a", Name: "Bank"}},
		{Source: "electrical.batteries.1", Reporting: true, Item: &discoveryItemRef{ID: "b", Name: "Starter"}},
		{Source: "electrical.batteries.2", Reporting: true, ChargerInput: true},
	}}
	st := stepByID(t, buildSetupStatus(vesselView{}, connectedDiscovery(batteries), setupMate{}), "house_bank")
	if st.State != setupTodo || strings.Contains(st.Detail, "isn't reporting") || !strings.Contains(st.Detail, "2 batteries") {
		t.Errorf("two battery items are choices: %+v", st)
	}
}

// Setup is about saved configuration, not feed health: a finished boat whose
// feed drops stays complete.
func TestSetupStatusFinishedBoatStaysCompleteWhenSignalKDrops(t *testing.T) {
	vessel := vesselView{
		Engines:   []vesselEngineSetting{{Instance: "port"}},
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512"},
		RolesNone: fullPowerNone(),
	}
	s := buildSetupStatus(vessel, discoveryResponse{Connected: false}, setupMate{})
	if !s.Complete {
		t.Fatalf("a dropped feed must not reopen setup: %+v", s.Steps)
	}
	if stepByID(t, s, "connection").State != "done" {
		t.Fatalf("connection is done once setup exists: %+v", s.Steps)
	}
}

func TestSetupStatusPartlyDoneAndDisconnectedBlocksOnlyWhatIsUndone(t *testing.T) {
	vessel := vesselView{Engines: []vesselEngineSetting{{Instance: "port"}}}
	s := buildSetupStatus(vessel, discoveryResponse{Connected: false}, setupMate{})
	if stepByID(t, s, "engines").State != "done" {
		t.Fatalf("a done step stays done: %+v", s.Steps)
	}
	if st := stepByID(t, s, "house_bank"); st.State != "blocked" {
		t.Fatalf("an undone step that needs the feed is blocked: %+v", st)
	}
	if s.Complete {
		t.Fatal("not complete")
	}
}

func TestSetupStatusAnItemOwningASourceCountsAsSetUp(t *testing.T) {
	disc := discoveryResponse{Connected: false, Groups: []discoveryGroup{{Kind: "tank", Rows: []discoveryRow{
		{Source: "tanks.fuel.0", Item: &discoveryItemRef{ID: "t", Name: "Fuel"}},
	}}}}
	if stepByID(t, buildSetupStatus(vesselView{}, disc, setupMate{}), "connection").State != "done" {
		t.Fatal("an item that owns a source means setup exists")
	}
}

func TestSetupStatusHouseBankItemWithoutASourceSaysSo(t *testing.T) {
	for _, connected := range []bool{true, false} {
		st := stepByID(t, buildSetupStatus(vesselView{HouseBankUnresolved: true}, discoveryResponse{Connected: connected}, setupMate{}), "house_bank")
		if st.State != setupTodo || !strings.Contains(st.Detail, "has no SignalK source") {
			t.Errorf("connected=%v: %+v", connected, st)
		}
	}
}

func setupBoatComplete() vesselView {
	return vesselView{
		Engines:   []vesselEngineSetting{{Instance: "port"}},
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.512"},
		RolesNone: fullPowerNone(),
	}
}

func limitsWithNoGo(sectors ...string) passageLimits {
	var l passageLimits
	for _, s := range sectors {
		switch s {
		case "ahead":
			l.Ahead.WindKn.NoGo = fp(15)
		case "abeam":
			l.Abeam.WindKn.NoGo = fp(20)
		case "astern":
			l.Astern.WindKn.NoGo = fp(25)
		}
	}
	return l
}

func TestSetupStatusPassageLimitsAbsentWhileMateIsOff(t *testing.T) {
	s := buildSetupStatus(setupBoatComplete(), connectedDiscovery(), setupMate{Enabled: false})
	for _, st := range s.Steps {
		if st.ID == "passage_limits" {
			t.Fatalf("step must not be listed with Mate off: %+v", st)
		}
	}
}

func TestSetupStatusPassageLimitsOptionalTodoLeavesSetupComplete(t *testing.T) {
	s := buildSetupStatus(setupBoatComplete(), connectedDiscovery(), setupMate{Enabled: true})
	st := stepByID(t, s, "passage_limits")
	if st.State != "todo" || !st.Optional || st.Title != "Passage limits" {
		t.Fatalf("step = %+v", st)
	}
	if st.Target.Section != "assistant" || st.Target.Control != "passage-limits" {
		t.Fatalf("target = %+v", st.Target)
	}
	if !s.Complete {
		t.Fatal("an open optional step must not hold setup open")
	}
	for _, id := range []string{"connection", "engines", "house_bank", "power"} {
		if stepByID(t, s, id).Optional {
			t.Errorf("%s must not be optional", id)
		}
	}
}

func TestSetupStatusRequiredTodoStillBlocksCompleteWithOptionalPresent(t *testing.T) {
	s := buildSetupStatus(vesselView{}, connectedDiscovery(), setupMate{Enabled: true, Limits: limitsWithNoGo("ahead", "abeam", "astern")})
	if s.Complete {
		t.Fatal("required steps open: not complete")
	}
}

func TestSetupStatusPassageLimitsNamesOpenSectors(t *testing.T) {
	s := buildSetupStatus(setupBoatComplete(), connectedDiscovery(), setupMate{Enabled: true, Limits: limitsWithNoGo("abeam")})
	st := stepByID(t, s, "passage_limits")
	if st.State != "todo" || st.Detail != "Still open: ahead, astern." {
		t.Fatalf("detail = %q (%s)", st.Detail, st.State)
	}
	// A marginal alone is not enough: the no-go wind limit is what counts.
	l := limitsWithNoGo("ahead", "abeam")
	l.Astern.WindKn.Marginal = fp(10)
	st = stepByID(t, buildSetupStatus(setupBoatComplete(), connectedDiscovery(), setupMate{Enabled: true, Limits: l}), "passage_limits")
	if st.Detail != "Still open: astern." {
		t.Fatalf("detail = %q", st.Detail)
	}
}

func TestSetupStatusPassageLimitsDoneWhenEverySectorHasNoGoWind(t *testing.T) {
	s := buildSetupStatus(setupBoatComplete(), connectedDiscovery(), setupMate{Enabled: true, Limits: limitsWithNoGo("ahead", "abeam", "astern")})
	st := stepByID(t, s, "passage_limits")
	if st.State != "done" || st.Detail != "Limits are set for every wind sector." {
		t.Fatalf("step = %+v", st)
	}
}

func TestGetSetupStatusHandlerListsPassageLimitsWhileMateIsOn(t *testing.T) {
	prev := globalSignalKSnapshot
	globalSignalKSnapshot = newVesselCandidatesSnapshot(t)
	t.Cleanup(func() { globalSignalKSnapshot = prev })
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	vesselFixtureStore(t)

	get := func() setupStatus {
		rec := httptest.NewRecorder()
		if err := getSetupStatusHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/setup/status", nil), rec)); err != nil {
			t.Fatal(err)
		}
		var got setupStatus
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	has := func(s setupStatus) bool {
		for _, st := range s.Steps {
			if st.ID == "passage_limits" {
				return true
			}
		}
		return false
	}
	if has(get()) {
		t.Fatal("Mate is off: no passage limits step")
	}
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
		p.Assistant.Enabled = true
		p.Assistant.Model = "m"
		p.Assistant.PassageLimits = limitsWithNoGo("ahead")
	})
	if code != http.StatusOK {
		t.Fatalf("save: %d %v", code, body)
	}
	got := get()
	if !has(got) || !strings.Contains(stepByID(t, got, "passage_limits").Detail, "abeam, astern") {
		t.Fatalf("expected the step with open sectors: %+v", got.Steps)
	}
}

func TestSetupStatusPassageLimitsBlockedWhenLimitsUnreadable(t *testing.T) {
	s := buildSetupStatus(setupBoatComplete(), connectedDiscovery(), setupMate{Enabled: true, LimitsErr: errors.New("bad")})
	st := stepByID(t, s, "passage_limits")
	if st.State != "blocked" || !strings.Contains(st.Detail, "couldn't be read") {
		t.Fatalf("step = %+v", st)
	}
	if !s.Complete {
		t.Fatal("an optional step must not hold setup open")
	}
}

func TestGetSetupStatusHandlerSurvivesUnreadablePassageLimits(t *testing.T) {
	prev := globalSignalKSnapshot
	globalSignalKSnapshot = newVesselCandidatesSnapshot(t)
	t.Cleanup(func() { globalSignalKSnapshot = prev })
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	appendSettings(t, settingsPath, "assistant:\n    enabled: true\n    passage_limits:\n        ahead:\n            wind_kn:\n                no_go: 15kn\n")
	t.Setenv("SETTINGS_FILE", settingsPath)
	vesselFixtureStore(t)
	rec := httptest.NewRecorder()
	if err := getSetupStatusHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/setup/status", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got setupStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if st := stepByID(t, got, "passage_limits"); st.State != "blocked" {
		t.Fatalf("step = %+v", st)
	}
	stepByID(t, got, "connection")
}
