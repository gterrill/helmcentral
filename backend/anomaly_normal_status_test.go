package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func statusModel() normalModelFile {
	at := time.Date(2026, 11, 9, 17, 0, 0, 0, time.UTC) // 3.00 AM Tue 10 Nov in Queensland
	reading := func(key, path string, used bool, reason string) normalFeatureVerdict {
		return normalFeatureVerdict{Key: key, Path: path, Used: used, Reason: reason, Required: key == "revolutions" || key == "temperature"}
	}
	return normalModelFile{Subsystems: map[string]normalSubsystemModel{normalSubsystemEngines: {
		TrainedAt: at, Source: normalSource{Kind: normalSourceMinuteBucket, Bucket: "SignalK_1m"},
		Subjects: map[string]normalSubjectModel{
			"port": {Name: "Port engine", Features: []normalFeatureVerdict{
				reading("revolutions", "propulsion.port.revolutions", true, ""),
				reading("temperature", "propulsion.port.temperature", true, ""),
				reading("oilPressure", "propulsion.port.oilPressure", true, ""),
				reading("transmission.oilTemperature", "propulsion.port.transmission.oilTemperature", false, "never changed while running"),
				{Key: "exhaustTemperature", Path: "propulsion.port.exhaustTemperature", Used: true, Joining: true, JoiningMinutes: 14, LateJoiner: true},
				reading("fuel.rate", "propulsion.port.fuel.rate", false, "two sources report it"),
			}},
			"starboard": {Name: "Starboard engine", Problem: "no steady running minutes in the last 90 days"},
		},
		Regimes: map[string]normalRegime{
			"port/rpm1800": {Subject: "port", Judged: true}, "port/rpm2000": {Subject: "port", Judged: true},
			"port/rpm1400": {Subject: "port", Judged: false},
		},
	}}}
}

func TestBuildNormalStatus_SaysWhenAndFromWhereWithTheBandsPerEngine(t *testing.T) {
	loc, _ := time.LoadLocation("Australia/Brisbane")
	s := buildNormalStatus(statusModel(), nil, nil, nil, loc)
	// The band counts are on the engine headings, not repeated here.
	want := `Learned Tue 10 Nov 3.00 AM from one-minute bucket "SignalK_1m".`
	if s.Summary != want {
		t.Fatalf("summary:\n got  %q\n want %q", s.Summary, want)
	}
	if !s.Configured || s.Note != "" {
		t.Fatalf("status = %+v", s)
	}
	if len(s.Engines) != 2 || s.Engines[0].ID != "port" || s.Engines[1].ID != "starboard" {
		t.Fatalf("engines = %+v", s.Engines)
	}
	if s.Engines[0].Bands != 2 || s.Engines[1].Bands != 0 {
		t.Fatalf("bands = %d, %d", s.Engines[0].Bands, s.Engines[1].Bands)
	}
	if s.Engines[1].Problem != "no steady running minutes in the last 90 days" {
		t.Fatalf("starboard = %+v", s.Engines[1])
	}
}

func TestBuildNormalStatus_EachReadingHasASwitchAndTheAutomaticVerdictBesideIt(t *testing.T) {
	s := buildNormalStatus(statusModel(), nil, nil, map[string]bool{"propulsion.port.oilPressure": true}, time.UTC)
	byKey := map[string]normalStatusReading{}
	for _, r := range s.Engines[0].Readings {
		byKey[r.Key] = r
	}
	cases := map[string]struct {
		on       bool
		status   string
		required bool
		name     string
	}{
		"revolutions":                 {true, "used", true, "rpm"},
		"oilPressure":                 {false, "switched off", false, "oil pressure"},
		"transmission.oilTemperature": {true, "never changed while running", false, "gear oil temperature"},
		"exhaustTemperature":          {true, "joins after 60 minutes, 14 so far", false, "exhaust temperature"},
		"fuel.rate":                   {true, "two sources report it", false, "fuel rate"},
	}
	for key, want := range cases {
		got, ok := byKey[key]
		if !ok {
			t.Errorf("no reading %s", key)
			continue
		}
		if got.On != want.on || got.Status != want.status || got.Required != want.required || got.Name != want.name {
			t.Errorf("%s = %+v, want %+v", key, got, want)
		}
		if got.Path == "" {
			t.Errorf("%s has no path for the ignore list", key)
		}
	}
}

// A reading the operator has just switched on, and which the model still holds
// as off, says it will be used at the next update. It is told by a code on the
// verdict, not by matching its words.
func TestBuildNormalStatus_ASwitchChangedSinceTheLastRunSaysWhenItTakesEffect(t *testing.T) {
	m := statusModel()
	eng := m.Subsystems[normalSubsystemEngines]
	port := eng.Subjects["port"]
	port.Features = append(port.Features,
		normalFeatureVerdict{Key: "boostPressure", Path: "propulsion.port.boostPressure", Reason: "switched off", SwitchedOff: true},
		normalFeatureVerdict{Key: "engineLoad", Path: "propulsion.port.engineLoad", Reason: "switched off"})
	eng.Subjects["port"] = port
	m.Subsystems[normalSubsystemEngines] = eng
	s := buildNormalStatus(m, nil, nil, nil, time.UTC)
	got := map[string]normalStatusReading{}
	for _, r := range s.Engines[0].Readings {
		got[r.Key] = r
	}
	if r := got["boostPressure"]; !r.On || r.Status != "switched on, used from the next update" {
		t.Fatalf("boost = %+v", r)
	}
	if r := got["engineLoad"]; r.Status != "switched off" {
		t.Fatalf("a reason that merely reads like it is not the code: %+v", r)
	}
}

// The finished sentence is the one the watch report uses.
func TestBuildNormalStatus_NotesAreTheReportsOwnSentences(t *testing.T) {
	empty := normalModelFile{Subsystems: map[string]normalSubsystemModel{}}
	cases := []struct {
		name    string
		file    normalModelFile
		fileErr error
		srcErr  error
		want    string
	}{
		{"no influx", empty, nil, errNormalNoInflux, "No learned normal: InfluxDB is not set up."},
		{"unreadable", normalModelFile{}, errors.New("parsing x: bad"), nil, "The learned normal could not be read: parsing x: bad."},
		{"nothing learned yet", empty, nil, nil, normalNoModelYet},
		{"last run failed, no model", normalModelFile{LastAttempt: &normalAttempt{Problem: `the one-minute bucket "b" could not be read: nope`}, Subsystems: map[string]normalSubsystemModel{}}, nil, nil,
			`No learned normal: the one-minute bucket "b" could not be read: nope.`},
		{"the secrets could not be read", empty, nil, errors.New("reading INFLUXDB_TOKEN: boom"), "Unusual readings could not be checked: reading INFLUXDB_TOKEN: boom."},
	}
	for _, c := range cases {
		s := buildNormalStatus(c.file, c.fileErr, c.srcErr, nil, time.UTC)
		if s.Note != c.want || s.Summary != "" || len(s.Engines) != 0 {
			t.Errorf("%s: %+v, want note %q", c.name, s, c.want)
		}
		if c.srcErr != nil && s.Configured {
			t.Errorf("%s: configured", c.name)
		}
	}
}

func TestBuildNormalStatus_AFailedLatestRunIsGivenBesideTheGoodModel(t *testing.T) {
	m := statusModel()
	m.LastAttempt = &normalAttempt{At: time.Now(), OK: false, Problem: "InfluxDB could not be read: timeout"}
	s := buildNormalStatus(m, nil, nil, nil, time.UTC)
	if s.Summary == "" || s.Note != "The learned normal was not updated: InfluxDB could not be read: timeout." {
		t.Fatalf("status = %+v", s)
	}
}

func TestNormalStatusHandler(t *testing.T) {
	useFreshBoatZone(t)
	dir := t.TempDir()
	settings := filepath.Join(dir, "settings.yaml")
	if err := writeNormalTestFile(settings, "influxdb:\n  enabled: false\n"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SETTINGS_FILE", settings)
	t.Setenv("ANOMALY_NORMAL_FILE", filepath.Join(dir, "anomaly-normal.json"))

	e := echo.New()
	rec := httptest.NewRecorder()
	if err := normalStatusHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anomaly/normal/status", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d: %s", rec.Code, rec.Body)
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["configured"] != false || got["note"] != "No learned normal: InfluxDB is not set up." {
		t.Fatalf("body = %s", rec.Body)
	}
	if _, has := got["engines"]; !has {
		t.Fatalf("engines is always a list: %s", rec.Body)
	}

	if err := saveNormalModel(filepath.Join(dir, "anomaly-normal.json"), statusModel()); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	_ = normalStatusHandler(e.NewContext(httptest.NewRequest(http.MethodGet, "/api/anomaly/normal/status", nil), rec))
	if !strings.Contains(rec.Body.String(), `"oilPressure"`) || !strings.Contains(rec.Body.String(), `"bands":2`) {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestNormalStatusRouteIsReadTier(t *testing.T) {
	for _, r := range anomalyNormalRoutes() {
		if r.Path == "/api/anomaly/normal/status" && r.Method == http.MethodGet && r.Tier == tierRead {
			return
		}
	}
	t.Fatal("GET /api/anomaly/normal/status must be a read-tier route")
}
