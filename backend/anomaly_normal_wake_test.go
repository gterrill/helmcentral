package main

import (
	"net/http"
	"net/http/httptest"
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
