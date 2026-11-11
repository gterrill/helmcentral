package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func drainNormalWake() {
	select {
	case <-normalTrainWake:
	default:
	}
}

func woke() bool {
	select {
	case <-normalTrainWake:
		return true
	default:
		return false
	}
}

func TestRequestNormalTraining_NeverBlocksAndCoalesces(t *testing.T) {
	drainNormalWake()
	requestNormalTraining()
	requestNormalTraining() // a second ask while one is waiting must not block
	if !woke() {
		t.Fatal("no wake token")
	}
	if woke() {
		t.Fatal("two asks make one run")
	}
}

func TestUpdateSettings_ChangingTheInfluxdbSettingsWakesTheTrainer(t *testing.T) {
	withTestSecretsStore(t)
	srv := trustedSignalKPayloadServer(t, -21.1, 149.2)
	defer srv.Close()
	host, port := hostPort(t, srv.URL)
	settingsPath := writeTestSettings(t, host, port)
	set := func(bucket string) {
		t.Helper()
		code, body := postSettings(t, settingsPath, func(p *settingsPayload) {
			p.Influxdb.Enabled = true
			p.Influxdb.URL = "http://192.168.50.20:8086"
			p.Influxdb.Org = "myorg"
			p.Influxdb.Bucket = "mybucket"
			p.Influxdb.MinuteBucket = bucket
		})
		if code != http.StatusOK {
			t.Fatalf("code %d %v", code, body)
		}
	}
	drainNormalWake()
	set("")
	if !woke() {
		t.Fatal("turning InfluxDB on is a change the trainer must hear")
	}
	set("")
	if woke() {
		t.Fatal("an unchanged save wakes nothing")
	}
	set("my_1m")
	if !woke() {
		t.Fatal("naming a one-minute bucket must wake the trainer")
	}
}

func TestNormalInputsChangedBySave_InfluxOnly(t *testing.T) {
	base := settingsPayload{}
	same := normalInputsChangedBySave(base, base)
	influx := base
	influx.Influxdb.URL = "http://x"
	if same || !normalInputsChangedBySave(base, influx) {
		t.Errorf("influx: unchanged %v, changed %v", same, normalInputsChangedBySave(base, influx))
	}
}

func TestSavingTheInfluxdbTokenWakesTheTrainerAndOtherSecretsDoNot(t *testing.T) {
	withTestSecretsStore(t)
	post := func(body string) {
		t.Helper()
		e := echo.New()
		req := httptest.NewRequest(http.MethodPost, "/api/settings/secrets", strings.NewReader(body))
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
		rec := httptest.NewRecorder()
		if err := updateSecretsSettingsHandler(e.NewContext(req, rec)); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("code %d err %v: %s", rec.Code, err, rec.Body)
		}
	}
	drainNormalWake()
	post(`{"OPENROUTER_API_KEY":"k"}`)
	if woke() {
		t.Fatal("another secret is not an input")
	}
	post(`{"INFLUXDB_TOKEN":"tok"}`)
	if !woke() {
		t.Fatal("a new token is an input the trainer must hear")
	}
}

func TestIgnoringAnEngineReadingWakesTheTrainerAndOtherSensorsDoNot(t *testing.T) {
	withTempIgnoreStore(t)
	drainNormalWake()
	if err := ignoreSensor("electrical.batteries.512.voltage"); err != nil {
		t.Fatal(err)
	}
	if woke() {
		t.Fatal("a battery sensor is not part of the engines' normal")
	}
	if err := ignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatal(err)
	}
	if !woke() {
		t.Fatal("switching an engine reading off must update the normal")
	}
	if err := unignoreSensor("propulsion.port.exhaustTemperature"); err != nil {
		t.Fatal(err)
	}
	if !woke() {
		t.Fatal("switching it on again must too")
	}
}

// Linking an engine to another equipment item moves where its repairs are read
// from, so it is a change the trainer must hear, though the instance is the same.
func TestWakeNormalIfEnginesChanged_NoticesAChangeOfEquipmentItem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anomaly-normal.json")
	t.Setenv("ANOMALY_NORMAL_FILE", path)
	if err := saveNormalModel(path, normalModelFile{Subsystems: map[string]normalSubsystemModel{
		normalSubsystemEngines: {Subjects: map[string]normalSubjectModel{"0": {Name: "Port engine", EquipmentID: "eq-a"}}},
	}}); err != nil {
		t.Fatal(err)
	}
	engines := func(instance, equipment string) vesselView {
		return vesselView{Engines: []vesselEngineSetting{{Instance: instance, EquipmentID: equipment}}}
	}
	drainNormalWake()
	wakeNormalIfEnginesChanged(engines("0", "eq-a"))
	if woke() {
		t.Fatal("the same engine on the same item wakes nothing")
	}
	wakeNormalIfEnginesChanged(engines("0", "eq-b"))
	if !woke() {
		t.Fatal("another equipment item must wake the trainer")
	}
	wakeNormalIfEnginesChanged(engines("1", "eq-a"))
	if !woke() {
		t.Fatal("another instance must wake the trainer")
	}
}

// Deleting an engine's equipment item takes its repairs with it, and the engine
// out of the vessel: either way the trainer has to hear.
func TestDeletingAnEnginesEquipmentItemWakesTheTrainer(t *testing.T) {
	settingsPath := writeTestSettings(t, "127.0.0.1", 3000)
	t.Setenv("SETTINGS_FILE", settingsPath)
	modelPath := filepath.Join(t.TempDir(), "anomaly-normal.json")
	t.Setenv("ANOMALY_NORMAL_FILE", modelPath)
	withTempAlarmRules(t)
	store := vesselFixtureStore(t)
	port, err := store.CreateEquipment(equipmentItem{Name: "Port engine", System: "propulsion"})
	if err != nil {
		t.Fatal(err)
	}
	port.SignalKSource = "propulsion.port"
	if _, err := store.UpdateEquipment(port.ID, port); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateMaintenanceLogEntry(maintenanceLogEntryInput{EquipmentID: &port.ID, PerformedAt: "2026-11-08", StartedAt: "2026-11-01", Kind: "repair"}); err != nil {
		t.Fatal(err)
	}
	// The model was learned with this engine linked to its item.
	modelEquipment = map[string]string{"port": port.ID}
	t.Cleanup(func() { modelEquipment = nil })
	saveModelWithEngines(t, modelPath, "port")
	drainNormalWake()

	c, rec := newDocumentEchoContext(http.MethodDelete, "/api/inventory/equipment/"+port.ID, "", port.ID)
	if err := deleteEquipmentHandler(c); err != nil || rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %v %d %s", err, rec.Code, rec.Body)
	}
	if !woke() {
		t.Fatal("deleting an engine's item must wake the trainer")
	}
}
