package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// loneMPPT is the boat's case: one Victron MPPT (a known charger) publishes
// only battery instance 1, whose voltage is really the solar panel's. The
// automatic test needs the charger to feed another bank too, so it does not
// recognise this instance; the operator's mark is what lets it through.
func loneMPPT(t *testing.T) *signalKSnapshot {
	t.Helper()
	snapshot := newAnomalyTestSnapshot()
	at := sensorNamesTestNow
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
		SourceRef: "YachtDevices.39", Timestamp: at.Format(time.RFC3339),
		Values: []signalKValue{{Path: "electrical.batteries.1.voltage", Value: 80.7}},
	}}}, at)
	globalSourceDevices.set(map[string]sourceDevice{
		"YachtDevices.39": {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: n2kFunctionCharger},
	})
	t.Cleanup(func() { globalSourceDevices.set(nil) })
	return snapshot
}

// withTempMarkStore installs an empty charger-input mark store in a temp
// database and removes it when the test ends.
func withTempMarkStore(t *testing.T) *chargerInputMarkStore {
	t.Helper()
	store, err := newChargerInputMarkStore(filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	if err != nil {
		t.Fatalf("newChargerInputMarkStore: %v", err)
	}
	prev := globalChargerInputMarks
	globalChargerInputMarks = store
	t.Cleanup(func() {
		globalChargerInputMarks = prev
		store.Close()
	})
	return store
}

func batteryRow(t *testing.T, snapshot *signalKSnapshot, id string) discoveryRow {
	t.Helper()
	for _, r := range discoveryGroupRows(vesselDiscovery(snapshot, vesselView{}, nil, sourceRecord{}), "battery") {
		if r.Instance == id {
			return r
		}
	}
	t.Fatalf("no battery row %q", id)
	return discoveryRow{}
}

func TestMarkedChargerInputIsSkippedByTheImpossibleReadingCheck(t *testing.T) {
	withTempMarkStore(t)
	snapshot := loneMPPT(t)
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	count := func() float64 {
		reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, sensorNamesTestNow)
		return reading.Values[anomalySensorOutOfRangeCountPath]
	}

	if got := count(); got != 1 {
		t.Fatalf("unmarked, 80.7 V on instance 1 must be an out-of-range reading, count = %v", got)
	}
	row := batteryRow(t, snapshot, "1")
	if row.ChargerInput || row.ChargerInputMark != "none" || !row.CanMark {
		t.Fatalf("unmarked row = %+v", row)
	}

	if err := markChargerInput("1", true); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 0 {
		t.Fatalf("marked, instance 1 must be skipped, count = %v", got)
	}
	row = batteryRow(t, snapshot, "1")
	if !row.ChargerInput || row.ChargerInputMark != "applies" {
		t.Fatalf("marked row = %+v", row)
	}

	if err := markChargerInput("1", false); err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("after unmarking the check applies again, count = %v", got)
	}
}

// A mark applies only while every publisher is a known charger. A battery
// monitor (or a source nothing is known about) on the same number means it may
// be a real battery, so it is range-checked again.
func TestMarkDoesNotApplyWhenANonChargerAlsoPublishesTheNumber(t *testing.T) {
	for name, devices := range map[string]map[string]sourceDevice{
		"a known non-charger": {
			"YachtDevices.39":  {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: n2kFunctionCharger},
			"YachtDevices.224": {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: 170},
		},
	} {
		t.Run(name, func(t *testing.T) {
			withTempMarkStore(t)
			snapshot := loneMPPT(t)
			globalSourceDevices.set(devices)
			at := sensorNamesTestNow
			snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
				SourceRef: "YachtDevices.224", Timestamp: at.Format(time.RFC3339),
				Values: []signalKValue{{Path: "electrical.batteries.1.voltage", Value: 80.7}},
			}}}, at)
			if err := markChargerInput("1", true); err != nil {
				t.Fatal(err)
			}
			settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
			reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, sensorNamesTestNow)
			if got := reading.Values[anomalySensorOutOfRangeCountPath]; got != 1 {
				t.Fatalf("a mark must not hide a possible battery, count = %v", got)
			}
			row := batteryRow(t, snapshot, "1")
			if row.ChargerInputMark != "not_chargers" || row.ChargerInput {
				t.Fatalf("row = %+v", row)
			}
		})
	}
}

// An item that owns the number makes the mark inert at read time, however the
// item got the source, so a real bank's reading is range-checked again.
func TestMarkIsInertOnceAnItemOwnsTheNumber(t *testing.T) {
	withTempMarkStore(t)
	snapshot := loneMPPT(t)
	store := vesselFixtureStore(t)
	if err := markChargerInput("1", true); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	t.Setenv("SETTINGS_FILE", settingsPath)
	count := func() float64 {
		reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, sensorNamesTestNow)
		return reading.Values[anomalySensorOutOfRangeCountPath]
	}
	if got := count(); got != 0 {
		t.Fatalf("marked and unowned must be skipped, count = %v", got)
	}
	if _, err := store.CreateEquipment(equipmentItem{Name: "Solar battery", SignalKSource: "electrical.batteries.1"}); err != nil {
		t.Fatal(err)
	}
	invalidateSourceItems()
	if got := count(); got != 1 {
		t.Fatalf("an item owns it now, so the reading is checked, count = %v", got)
	}
	items, err := listSourceItems()
	if err != nil {
		t.Fatal(err)
	}
	row := discoveryRowFor(t, vesselDiscovery(snapshot, vesselView{}, items, sourceRecord{}), "1")
	if row.ChargerInputMark != "item" || row.ChargerInput {
		t.Fatalf("row = %+v", row)
	}
}

func discoveryRowFor(t *testing.T, resp discoveryResponse, id string) discoveryRow {
	t.Helper()
	for _, r := range discoveryGroupRows(resp, "battery") {
		if r.Instance == id {
			return r
		}
	}
	t.Fatalf("no battery row %q", id)
	return discoveryRow{}
}

// The operator vouched for the number; only a known non-charger lifts the mark.
// A publisher missing from the device map (sources not loaded yet after a
// restart) leaves it in effect.
func TestMarkSurvivesDevicesThatAreNotKnownYet(t *testing.T) {
	withTempMarkStore(t)
	snapshot := loneMPPT(t)
	globalSourceDevices.set(nil)
	if err := markChargerInput("1", true); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, sensorNamesTestNow)
	if got := reading.Values[anomalySensorOutOfRangeCountPath]; got != 0 {
		t.Fatalf("an unknown device must not lift the mark, count = %v", got)
	}
	if row := batteryRow(t, snapshot, "1"); row.ChargerInputMark != "applies" || !row.ChargerInput || row.CanMark {
		t.Fatalf("row = %+v", row)
	}
}

func TestCanMarkOnlyWhereTheMarkWouldApply(t *testing.T) {
	withTempMarkStore(t)
	snapshot := loneMPPT(t)
	at := sensorNamesTestNow
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
		SourceRef: "YachtDevices.224", Timestamp: at.Format(time.RFC3339),
		Values: []signalKValue{{Path: "electrical.batteries.2.voltage", Value: 13.1}},
	}}}, at)
	globalSourceDevices.set(map[string]sourceDevice{
		"YachtDevices.39":  {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: n2kFunctionCharger},
		"YachtDevices.224": {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: 170},
	})
	if !batteryRow(t, snapshot, "1").CanMark {
		t.Fatal("a lone charger's number can be marked")
	}
	if batteryRow(t, snapshot, "2").CanMark {
		t.Fatal("a battery monitor's number can't be marked")
	}
	items := []sourceItem{{ID: "i", Name: "Solar", Source: "electrical.batteries.1", Group: sourceBattery, Instance: "1"}}
	if r := discoveryRowFor(t, vesselDiscovery(snapshot, vesselView{}, items, sourceRecord{}), "1"); r.CanMark {
		t.Fatalf("an owned number can't be marked: %+v", r)
	}
}

func TestMarkedInstanceIsListedEvenWhenQuiet(t *testing.T) {
	withTempMarkStore(t)
	if err := markChargerInput("9", true); err != nil {
		t.Fatal(err)
	}
	row := batteryRow(t, newAnomalyTestSnapshot(), "9")
	if row.Reporting || row.ChargerInputMark != "no_publishers" || row.ChargerInput {
		t.Fatalf("row = %+v", row)
	}
}

func TestAutomaticChargerInputIsNotReportedAsMarked(t *testing.T) {
	withTempMarkStore(t)
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	row := batteryRow(t, snapshot, "1")
	if !row.ChargerInput || row.ChargerInputMark != "none" {
		t.Fatalf("automatic row must be ChargerInput without the operator mark: %+v", row)
	}
}

// A marked number the automatic test already recognises: the mark adds nothing,
// whether or not an item owns the number, and the row must not say the number
// is checked as a battery.
func TestMarkOnAnAutomaticChargerInputAddsNothing(t *testing.T) {
	withTempMarkStore(t)
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	if err := markChargerInput("1", true); err != nil {
		t.Fatal(err)
	}
	if row := batteryRow(t, snapshot, "1"); !row.ChargerInput || row.ChargerInputMark != "automatic" || row.CanMark {
		t.Fatalf("marked automatic row = %+v", row)
	}
	items := []sourceItem{{ID: "i", Name: "Solar", Source: "electrical.batteries.1", Group: sourceBattery, Instance: "1"}}
	if r := discoveryRowFor(t, vesselDiscovery(snapshot, vesselView{}, items, sourceRecord{}), "1"); !r.ChargerInput || r.ChargerInputMark != "automatic" {
		t.Fatalf("owned and automatic row = %+v", r)
	}
}

// The handler refuses exactly where discovery says can_mark is false.
func TestChargerInputHandlerRefusesANumberTheAutomaticTestRecognises(t *testing.T) {
	store := withTempMarkStore(t)
	globalSourceDevices.set(loadSensorSourcesFixture(t))
	t.Cleanup(func() { globalSourceDevices.set(nil) })
	snapshot := newAnomalyTestSnapshot()
	loadSensorBatteriesFixture(t, snapshot)
	prev := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = prev })
	vesselFixtureStore(t)
	if batteryRow(t, snapshot, "1").CanMark {
		t.Fatal("fixture: instance 1 must not be markable")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/vessel/charger-inputs/1", strings.NewReader(`{"marked":true}`))
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("instance")
	c.SetParamValues("1")
	if err := putChargerInputHandler(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Helmcentral already treats battery instance 1 as a charger's input.") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if store.has("1") {
		t.Fatal("a refused mark must not be stored")
	}
}

func TestChargerInputHandlerSetsAndClearsTheMark(t *testing.T) {
	store := withTempMarkStore(t)
	snapshot := loneMPPT(t)
	prev := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = prev })
	prevRecord := currentSourceRecord
	currentSourceRecord = func() sourceRecord { return sourceRecord{} }
	t.Cleanup(func() { currentSourceRecord = prevRecord })

	put := func(instance, body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/api/vessel/charger-inputs/x", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		c := echo.New().NewContext(req, rec)
		c.SetParamNames("instance")
		c.SetParamValues(instance)
		if err := putChargerInputHandler(c); err != nil {
			t.Fatalf("handler: %v", err)
		}
		return rec
	}

	if rec := put("1", `{"marked":true}`); rec.Code != http.StatusOK {
		t.Fatalf("mark: %d %s", rec.Code, rec.Body.String())
	} else {
		var body struct {
			Instance string `json:"instance"`
			Marked   bool   `json:"marked"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Instance != "1" || !body.Marked {
			t.Fatalf("body = %s (%v)", rec.Body.String(), err)
		}
	}
	if !store.has("1") {
		t.Fatal("the mark must be stored")
	}
	// An instance an item owns can't be marked, but a mark can still be cleared.
	if _, err := vesselFixtureStore(t).CreateEquipment(equipmentItem{Name: "Solar battery", SignalKSource: "electrical.batteries.1"}); err != nil {
		t.Fatal(err)
	}
	if rec := put("1", `{"marked":true}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "Solar battery") {
		t.Fatalf("owned: %d %s", rec.Code, rec.Body.String())
	}
	if rec := put("1", `{"marked":false}`); rec.Code != http.StatusOK || store.has("1") {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body.String())
	}

	// Only known chargers may publish a number that gets marked.
	at := sensorNamesTestNow
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: []signalKUpdate{{
		SourceRef: "YachtDevices.224", Timestamp: at.Format(time.RFC3339),
		Values: []signalKValue{{Path: "electrical.batteries.2.voltage", Value: 13.1}},
	}}}, at)
	globalSourceDevices.set(map[string]sourceDevice{
		"YachtDevices.39":  {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: n2kFunctionCharger},
		"YachtDevices.224": {DeviceClass: n2kClassElectricalGeneration, DeviceFunction: 170},
	})
	if rec := put("2", `{"marked":true}`); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "isn't a charger") {
		t.Fatalf("not chargers: %d %s", rec.Code, rec.Body.String())
	}

	for name, tc := range map[string]struct{ instance, body string }{
		"bad id":          {"a.b", `{"marked":true}`},
		"never reported":  {"77", `{"marked":true}`},
		"missing field":   {"1", `{}`},
		"not json":        {"1", `nope`},
		"unknown field":   {"1", `{"marked":true,"x":1}`},
		"id with slash":   {"1%2F2", `{"marked":true}`},
		"whitespace only": {" ", `{"marked":true}`},
	} {
		if rec := put(tc.instance, tc.body); rec.Code != http.StatusBadRequest && rec.Code != http.StatusConflict {
			t.Errorf("%s: want 400 or 409, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if len(store.all()) != 0 {
		t.Fatalf("rejected requests must not store a mark: %+v", store.all())
	}
}

func TestMarkedChargerInputsSurviveReopen(t *testing.T) {
	store := withTempMarkStore(t)
	if err := markChargerInput("1", true); err != nil {
		t.Fatal(err)
	}
	reopened, err := newChargerInputMarkStore(store.path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reopened.has("1") {
		t.Fatal("mark lost on reopen")
	}
	if err := markChargerInput("1", false); err != nil {
		t.Fatal(err)
	}
	if again, err := newChargerInputMarkStore(store.path); err != nil || again.has("1") {
		t.Fatalf("clear lost on reopen: %v", err)
	}
}

func TestMarkStoreFailureIsPlainToTheOperatorAndLoggedWithDetail(t *testing.T) {
	store := withTempMarkStore(t)
	snapshot := loneMPPT(t)
	prev := globalSignalKSnapshot
	globalSignalKSnapshot = snapshot
	t.Cleanup(func() { globalSignalKSnapshot = prev })
	vesselFixtureStore(t)
	store.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/vessel/charger-inputs/1", strings.NewReader(`{"marked":true}`))
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("instance")
	c.SetParamValues("1")
	if err := putChargerInputHandler(c); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if rec.Code != http.StatusInternalServerError || !strings.Contains(body, "Couldn't save the charger-input mark. Try again; if it keeps failing, see Settings → Logs.") {
		t.Fatalf("%d %s", rec.Code, body)
	}
	if strings.Contains(strings.ToLower(body), "sql") || strings.Contains(body, "closed") {
		t.Fatalf("the operator must not see the database error: %s", body)
	}
	if err := markChargerInput("1", true); err == nil || strings.Contains(err.Error(), "Couldn't") {
		t.Fatalf("the store's own error is lowercase Go, got %v", err)
	}
	if store.has("1") {
		t.Fatal("a failed save must not leave the mark in memory")
	}
}
