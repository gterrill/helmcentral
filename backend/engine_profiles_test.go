package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func writeProfile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", name, err)
	}
}

func setupEngineProfiles(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		writeProfile(t, dir, name, body)
	}
	t.Setenv("ENGINE_PROFILES_DIR", dir)
	loadEngineProfiles()
}

const goodProfile = `{
  "id": "test-engine",
  "name": "Test Engine",
  "manufacturer": "Acme",
  "gauges": [
    {
      "path_suffix": "oilPressure", "label": "Oil Press", "display": "radial",
      "quantity": "pressure", "unit": "psi", "min": 0, "max": 100,
      "zones": [
        {"direction": "above", "threshold": 40, "state": "normal", "source": "a manual"},
        {"direction": "below", "threshold": null, "state": "alarm", "note": "from your manual"}
      ]
    }
  ],
  "service": [
    {"id": "engine-oil", "description": "Engine oil & filter", "interval_hours": 250, "interval_months": 12}
  ]
}`

const goodGeneratorProfile = `{
	"schema_version": 1,
	"kind": "generator",
	"id": "test-generator",
	"name": "Test Generator",
	"gauges": [
		{
			"path_suffix": "phase.A.frequency",
			"label": "Hz",
			"display": "numeric",
			"quantity": "frequency",
			"unit": "Hz"
		}
	]
}`

const goodAlternatorProfile = `{
	"schema_version": 1,
	"kind": "alternator",
	"id": "test-alternator",
	"name": "Test Alternator",
	"gauges": [
		{
			"path_suffix": "voltage",
			"label": "Output Voltage",
			"hero": true,
			"display": "numeric",
			"quantity": "potential",
			"unit": "V",
			"min": 0,
			"max": 60
		}
	]
}`

func TestLoadEngineProfiles(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"test.json": goodProfile})

	profiles, problems := engineProfiles()
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got %+v", problems)
	}
	if len(profiles) != 1 {
		t.Fatalf("expected one profile, got %d", len(profiles))
	}

	p := profiles[0]
	if p.ID != "test-engine" || p.Name != "Test Engine" {
		t.Fatalf("unexpected identity: %+v", p)
	}
	if len(p.Gauges) != 1 || len(p.Gauges[0].Zones) != 2 {
		t.Fatalf("expected the gauge and both zones to survive, got %+v", p.Gauges)
	}
	// A null threshold is a slot, not a zone: it must survive as one so the
	// dialog can show "not set" rather than silently dropping it here.
	if p.Gauges[0].Zones[1].Threshold != nil {
		t.Fatalf("expected the alarm slot to stay unset, got %+v", p.Gauges[0].Zones[1])
	}
	if len(p.Service) != 1 || p.Service[0].ID != "engine-oil" {
		t.Fatalf("expected the service item to survive, got %+v", p.Service)
	}
}

// One bad drop-in file must not take the others down with it, and must not
// vanish silently either.
func TestLoadEngineProfilesReportsBadFilesWithoutLosingGoodOnes(t *testing.T) {
	cases := map[string]string{
		"good.json":          goodProfile,
		"malformed.json":     `{"id": "broken", `,
		"no-id.json":         `{"name": "Nameless", "gauges": []}`,
		"bad-unit.json":      `{"id":"u","name":"U","gauges":[{"path_suffix":"a","display":"numeric","quantity":"pressure","unit":"furlongs"}]}`,
		"bad-display.json":   `{"id":"d","name":"D","gauges":[{"path_suffix":"a","display":"hologram","quantity":"raw","unit":"raw"}]}`,
		"bad-state.json":     `{"id":"s","name":"S","gauges":[{"path_suffix":"a","display":"radial","quantity":"raw","unit":"raw","min":0,"max":10,"zones":[{"direction":"below","threshold":1,"state":"spicy"}]}]}`,
		"bad-range.json":     `{"id":"r","name":"R","gauges":[{"path_suffix":"a","display":"radial","quantity":"raw","unit":"raw","min":100,"max":10}]}`,
		"bad-direction.json": `{"id":"x","name":"X","gauges":[{"path_suffix":"a","display":"radial","quantity":"raw","unit":"raw","min":0,"max":10,"zones":[{"direction":"sideways","threshold":1,"state":"warn"}]}]}`,
		"bad-interval.json":  `{"id":"i","name":"I","gauges":[],"service":[{"id":"oil","description":"Oil","interval_hours":-5}]}`,
		"dup-service.json":   `{"id":"ds","name":"DS","gauges":[],"service":[{"id":"oil","interval_hours":250},{"id":"oil","interval_hours":500}]}`,
		"two-hero.json":      `{"id":"h","name":"H","gauges":[{"path_suffix":"a","label":"A","hero":true,"display":"numeric","quantity":"raw","unit":"raw"},{"path_suffix":"b","label":"B","hero":true,"display":"numeric","quantity":"raw","unit":"raw"}]}`,
		"no-gauges.json":     `{"id":"g","name":"G"}`,
	}
	setupEngineProfiles(t, cases)

	profiles, problems := engineProfiles()
	if len(profiles) != 1 || profiles[0].ID != "test-engine" {
		t.Fatalf("expected only the good profile to load, got %+v", profiles)
	}
	if len(problems) != len(cases)-1 {
		t.Fatalf("expected %d problems, got %d: %+v", len(cases)-1, len(problems), problems)
	}
	for _, problem := range problems {
		if problem.File == "" || problem.Error == "" {
			t.Fatalf("a problem must name the file and the reason, got %+v", problem)
		}
	}
}

func TestLoadEngineProfilesAcceptsSingleHeroGauge(t *testing.T) {
	setupEngineProfiles(t, map[string]string{
		"hero.json": `{"id":"h","name":"H","gauges":[{"path_suffix":"oilPressure","label":"Oil","hero":true,"display":"numeric","quantity":"pressure","unit":"psi"}]}`,
	})

	profiles, problems := engineProfiles()
	if len(problems) != 0 {
		t.Fatalf("expected a single hero gauge to be accepted, got %+v", problems)
	}
	if len(profiles) != 1 || len(profiles[0].Gauges) != 1 || !profiles[0].Gauges[0].Hero {
		t.Fatalf("expected hero gauge to survive, got %+v", profiles)
	}
}

// A service item with no interval is a slot, not an error: the profile names a
// service the engine has without inventing how often it wants it.
func TestLoadEngineProfilesAcceptsServiceSlots(t *testing.T) {
	setupEngineProfiles(t, map[string]string{
		"slots.json": `{"id":"s","name":"S","gauges":[],"service":[{"id":"oil","description":"Engine oil"}]}`,
	})

	profiles, problems := engineProfiles()
	if len(problems) != 0 {
		t.Fatalf("expected an unset interval to be accepted, got %+v", problems)
	}
	if len(profiles) != 1 || len(profiles[0].Service) != 1 {
		t.Fatalf("expected the service slot to survive, got %+v", profiles)
	}
	item := profiles[0].Service[0]
	if item.IntervalHours != nil || item.IntervalMonths != nil {
		t.Fatalf("expected both intervals unset, got %+v", item)
	}
}

func TestLoadEngineProfilesRejectsDuplicateIDs(t *testing.T) {
	setupEngineProfiles(t, map[string]string{
		"a.json": goodProfile,
		"b.json": goodProfile,
	})

	profiles, problems := engineProfiles()
	if len(profiles) != 1 {
		t.Fatalf("expected the duplicate to be refused, got %d profiles", len(profiles))
	}
	if len(problems) != 1 {
		t.Fatalf("expected the duplicate to be reported, got %+v", problems)
	}
}

func TestLoadEngineProfilesToleratesAMissingDirectory(t *testing.T) {
	t.Setenv("ENGINE_PROFILES_DIR", filepath.Join(t.TempDir(), "not-there"))
	loadEngineProfiles()

	profiles, problems := engineProfiles()
	if len(profiles) != 0 || len(problems) != 0 {
		t.Fatalf("no profiles directory is not an error, got %+v / %+v", profiles, problems)
	}
}

func TestEngineProfilesHandler(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile, "bad.json": `{`})

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/engine-profiles", nil), rec)

	if err := engineProfilesHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var body struct {
		Profiles []engineProfile        `json:"profiles"`
		Problems []engineProfileProblem `json:"problems"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if len(body.Profiles) != 1 || len(body.Problems) != 1 {
		t.Fatalf("expected one profile and one problem, got %+v", body)
	}
}

func TestEquipmentProfilesHandlerSupportsKindFilter(t *testing.T) {
	setupEngineProfiles(t, map[string]string{
		"engine.json":    goodProfile,
		"generator.json": goodGeneratorProfile,
	})

	e := echo.New()
	allRec := httptest.NewRecorder()
	allCtx := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/equipment-profiles", nil), allRec)

	if err := equipmentProfilesHandler(allCtx); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if allRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", allRec.Code)
	}

	var allBody struct {
		Profiles []engineProfile `json:"profiles"`
	}
	if err := json.Unmarshal(allRec.Body.Bytes(), &allBody); err != nil {
		t.Fatalf("failed to decode all response: %v", err)
	}
	if len(allBody.Profiles) != 2 {
		t.Fatalf("expected both profiles, got %+v", allBody.Profiles)
	}

	engineRec := httptest.NewRecorder()
	engineReq := httptest.NewRequest(http.MethodGet, "/api/equipment-profiles?kind=engine", nil)
	engineCtx := e.NewContext(engineReq, engineRec)

	if err := equipmentProfilesHandler(engineCtx); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	var engineBody struct {
		Profiles []engineProfile `json:"profiles"`
	}
	if err := json.Unmarshal(engineRec.Body.Bytes(), &engineBody); err != nil {
		t.Fatalf("failed to decode engine response: %v", err)
	}
	if len(engineBody.Profiles) != 1 || engineBody.Profiles[0].Kind != "engine" {
		t.Fatalf("expected one engine profile, got %+v", engineBody.Profiles)
	}
}

func TestUpdateEngineProfileHandler(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	body := `{
		"id": "test-engine",
		"name": "Updated Engine",
		"manufacturer": "Acme",
		"gauges": [
			{
				"path_suffix": "oilPressure",
				"label": "Oil Press",
				"display": "radial",
				"quantity": "pressure",
				"unit": "psi",
				"min": 0,
				"max": 100
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/engine-profiles/test-engine", bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/engine-profiles/:id")
	c.SetParamNames("id")
	c.SetParamValues("test-engine")

	if err := updateEngineProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	profiles, problems := engineProfiles()
	if len(problems) != 0 {
		t.Fatalf("expected no profile load problems after update, got %+v", problems)
	}
	if len(profiles) != 1 || profiles[0].Name != "Updated Engine" {
		t.Fatalf("expected updated profile in memory, got %+v", profiles)
	}
}

func TestUpdateEngineProfileHandlerRejectsInvalidBody(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPut, "/api/engine-profiles/test-engine", bytes.NewBufferString(`{"id":`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/engine-profiles/:id")
	c.SetParamNames("id")
	c.SetParamValues("test-engine")

	if err := updateEngineProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateEngineProfileHandlerRejectsIDMismatch(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	body := `{"id":"other-engine","name":"Updated","gauges":[{"path_suffix":"oilPressure","label":"Oil Press","display":"radial","quantity":"pressure","unit":"psi","min":0,"max":100}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/engine-profiles/test-engine", bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/engine-profiles/:id")
	c.SetParamNames("id")
	c.SetParamValues("test-engine")

	if err := updateEngineProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestUpdateEquipmentProfileHandlerRejectsSchemaMismatch(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	body := `{
		"schema_version": 1,
		"kind": "generator",
		"id": "test-engine",
		"name": "Bad Generator",
		"gauges": [
			{
				"path_suffix": "oilPressure",
				"label": "Oil",
				"display": "radial",
				"quantity": "pressure",
				"unit": "psi"
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPut, "/api/equipment-profiles/test-engine", bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetPath("/api/equipment-profiles/:id")
	c.SetParamNames("id")
	c.SetParamValues("test-engine")

	if err := updateEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Error  string                   `json:"error"`
		Errors []profileValidationError `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if payload.Error == "" || len(payload.Errors) == 0 {
		t.Fatalf("expected structured schema errors, got %+v", payload)
	}
}

func TestCreateEquipmentProfileHandler(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	body := `{
		"schema_version": 1,
		"kind": "generator",
		"id": "new-generator",
		"name": "New Generator",
		"gauges": [
			{
				"path_suffix": "phase.A.frequency",
				"label": "Hz",
				"display": "numeric",
				"quantity": "frequency",
				"unit": "Hz"
			}
		]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/equipment-profiles", bytes.NewBufferString(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := createEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d (%s)", rec.Code, rec.Body.String())
	}

	profiles, _ := engineProfiles()
	if len(profiles) != 2 {
		t.Fatalf("expected two profiles after create, got %+v", profiles)
	}

	if _, err := os.Stat(filepath.Join(engineProfilesDir(), "new-generator.json")); err != nil {
		t.Fatalf("expected created file on disk: %v", err)
	}
}

func TestCreateEquipmentProfileHandlerAcceptsAlternatorKind(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/equipment-profiles", bytes.NewBufferString(goodAlternatorProfile))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := createEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for alternator kind, got %d (%s)", rec.Code, rec.Body.String())
	}

	profiles, _ := engineProfiles()
	if len(profiles) != 2 {
		t.Fatalf("expected two profiles after alternator create, got %+v", profiles)
	}
	var found bool
	for _, profile := range profiles {
		if profile.Kind == "alternator" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected alternator profile kind, got %+v", profiles)
	}
}

func TestCreateEquipmentProfileHandlerRejectsDuplicateID(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	req := httptest.NewRequest(http.MethodPost, "/api/equipment-profiles", bytes.NewBufferString(goodProfile))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	if err := createEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestGetEquipmentProfileHandler(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/equipment-profiles/test-engine", nil), rec)
	c.SetPath("/api/equipment-profiles/:id")
	c.SetParamNames("id")
	c.SetParamValues("test-engine")

	if err := getEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	var payload struct {
		Profile engineProfile `json:"profile"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if payload.Profile.ID != "test-engine" {
		t.Fatalf("expected fetched profile, got %+v", payload.Profile)
	}
}

func TestDownloadEquipmentProfileHandler(t *testing.T) {
	setupEngineProfiles(t, map[string]string{"good.json": goodProfile})

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodGet, "/api/equipment-profiles/test-engine/download", nil), rec)
	c.SetPath("/api/equipment-profiles/:id/download")
	c.SetParamNames("id")
	c.SetParamValues("test-engine")

	if err := downloadEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get(echo.HeaderContentDisposition), "test-engine.json") {
		t.Fatalf("expected content-disposition filename, got %q", rec.Header().Get(echo.HeaderContentDisposition))
	}

	var decoded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("response is not valid json: %v", err)
	}
}

func TestDeleteEquipmentProfileHandler(t *testing.T) {
	setupEngineProfiles(t, map[string]string{
		"good.json":      goodProfile,
		"generator.json": goodGeneratorProfile,
	})

	e := echo.New()
	rec := httptest.NewRecorder()
	c := e.NewContext(httptest.NewRequest(http.MethodDelete, "/api/equipment-profiles/test-generator", nil), rec)
	c.SetPath("/api/equipment-profiles/:id")
	c.SetParamNames("id")
	c.SetParamValues("test-generator")

	if err := deleteEquipmentProfileHandler(c); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d (%s)", rec.Code, rec.Body.String())
	}

	profiles, _ := engineProfiles()
	if len(profiles) != 1 || profiles[0].ID != "test-engine" {
		t.Fatalf("expected generator deleted, got %+v", profiles)
	}
}

/*
A bundled non-normal threshold needs a citation, not silence.

ADR 0053 started from one profile, Cummins QSB 6.7, whose setpoints Cummins
does not publish - any number there would have been a guess off a forum. That
narrow finding got generalised into a blanket ban on every bundled warn or
alarm threshold, which does not hold for the alternator profile: its
thresholds come straight from the Prestolite Electric / Leece-Neville spec
for the Cummins 5285862, and a cited number is not a guess.

So the rule checks provenance, not absence. A warn or alarm zone may ship a
real threshold if it says where the number came from. A warn or alarm zone
with no threshold is still the empty slot the operator fills from their own
manual, and that stays fine. An advisory (normal) zone was always required
to cite its source, and still is.
*/
func TestBundledThresholdsCiteTheirSource(t *testing.T) {
	t.Setenv("ENGINE_PROFILES_DIR", "../plugins/engine-profiles")
	loadEngineProfiles()

	profiles, problems := engineProfiles()
	if len(problems) != 0 {
		t.Fatalf("bundled profiles must load clean, got %+v", problems)
	}
	if len(profiles) == 0 {
		t.Fatal("expected at least one bundled profile")
	}

	for _, profile := range profiles {
		for _, gauge := range profile.Gauges {
			for _, zone := range gauge.Zones {
				if zone.State != alarmStateNormal && zone.Threshold != nil && zone.Source == "" {
					t.Fatalf(
						"%s/%s: a bundled %s threshold (%v) has to say where it came from",
						profile.ID, gauge.PathSuffix, zone.State, *zone.Threshold)
				}
				if zone.State == alarmStateNormal && zone.Source == "" {
					t.Fatalf("%s/%s: an advisory band must cite its source", profile.ID, gauge.PathSuffix)
				}
			}
		}
	}
}

/*
Every bundled suffix must resolve against a real vessel.

The first version of the Cummins profile shipped `coolantTemperature`,
`oilTemperature` and `exhaustTemperature` — none of which that engine
publishes. It applied cleanly and produced three permanently dashed gauges,
which is the structural dash doing its job about a mistake in a file we wrote.

The fixture is a captured path list, not an assumed one. The assertion is
prefix-agnostic: a profile passes when there is at least one instance under
which every one of its suffixes resolves, which is exactly what applying it to
that instance would do.
*/
func TestBundledProfileSuffixesResolveAgainstTheVessel(t *testing.T) {
	published := []string{
		// propulsion, per engine
		"propulsion.port.alternatorVoltage",
		"propulsion.port.boostPressure",
		"propulsion.port.engineLoad",
		"propulsion.port.engineTorque",
		"propulsion.port.fuel.economy",
		"propulsion.port.fuel.rate",
		"propulsion.port.oilPressure",
		"propulsion.port.revolutions",
		"propulsion.port.runTime",
		"propulsion.port.state",
		"propulsion.port.temperature",
		"propulsion.port.transmission.oilPressure",
		"propulsion.port.transmission.oilTemperature",
		// AC circuits, either of which may be the genset
		"electrical.ac.0.phase.A.current",
		"electrical.ac.0.phase.A.frequency",
		"electrical.ac.0.phase.A.lineNeutralVoltage",
		"electrical.ac.0.phase.A.realPower",
		"electrical.ac.0.total.realPower",
		"electrical.ac.1.phase.A.current",
		"electrical.ac.1.phase.A.frequency",
		"electrical.ac.1.phase.A.lineNeutralVoltage",
		"electrical.ac.1.phase.A.realPower",
		"electrical.ac.1.total.realPower",
		// alternators, per engine
		"electrical.alternator.0.chargingMode",
		"electrical.alternator.0.chargingModeNumber",
		"electrical.alternator.0.current",
		"electrical.alternator.0.engineSpeed",
		"electrical.alternator.0.engineSpeedHz",
		"electrical.alternator.0.fieldDrive",
		"electrical.alternator.0.name",
		"electrical.alternator.0.power",
		"electrical.alternator.0.speed",
		"electrical.alternator.0.temperature",
		"electrical.alternator.0.voltage",
		"electrical.alternator.1.chargingMode",
		"electrical.alternator.1.chargingModeNumber",
		"electrical.alternator.1.current",
		"electrical.alternator.1.engineSpeed",
		"electrical.alternator.1.engineSpeedHz",
		"electrical.alternator.1.fieldDrive",
		"electrical.alternator.1.name",
		"electrical.alternator.1.power",
		"electrical.alternator.1.speed",
		"electrical.alternator.1.temperature",
		"electrical.alternator.1.voltage",
	}

	// Every instance prefix each suffix could hang off.
	prefixesFor := func(suffix string) map[string]bool {
		out := map[string]bool{}
		for _, path := range published {
			if strings.HasSuffix(path, "."+suffix) {
				out[strings.TrimSuffix(path, "."+suffix)] = true
			}
		}
		return out
	}

	t.Setenv("ENGINE_PROFILES_DIR", "../plugins/engine-profiles")
	loadEngineProfiles()

	profiles, problems := engineProfiles()
	if len(problems) != 0 {
		t.Fatalf("bundled profiles must load clean, got %+v", problems)
	}
	if len(profiles) == 0 {
		t.Fatal("expected at least one bundled profile")
	}

	for _, profile := range profiles {
		var shared map[string]bool
		for _, gauge := range profile.Gauges {
			found := prefixesFor(gauge.PathSuffix)
			if len(found) == 0 {
				t.Errorf("%s: suffix %q resolves to nothing this vessel publishes",
					profile.ID, gauge.PathSuffix)
				shared = nil
				break
			}
			if shared == nil {
				shared = found
				continue
			}
			for prefix := range shared {
				if !found[prefix] {
					delete(shared, prefix)
				}
			}
		}
		if shared != nil && len(shared) == 0 {
			t.Errorf("%s: no single instance publishes every one of its suffixes", profile.ID)
		}
	}
}
