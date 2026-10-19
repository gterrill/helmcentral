package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
)

func fp(v float64) *float64 { return &v }

func TestPassageLimitsAbsentIsAllNil(t *testing.T) {
	for _, settings := range []map[string]any{
		{},
		{"assistant": map[string]any{"enabled": true}},
	} {
		got := buildSettingsPayload(settings).Assistant.PassageLimits
		if got != (passageLimits{}) {
			t.Fatalf("untouched settings must yield empty limits, got %+v", got)
		}
	}
}

func TestPassageLimitsRoundTrip(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	want := passageLimits{
		Ahead:                   passageSectorLimits{WindKnNoGo: fp(15), WaveMNoGo: fp(1.5)},
		Astern:                  passageSectorLimits{WindKnNoGo: fp(35)},
		WindAgainstTideOffsetKn: fp(5),
		DaylightArrival:         true,
	}
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) { p.Assistant.PassageLimits = want })
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", code, body)
	}
	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := buildSettingsPayload(saved).Assistant.PassageLimits
	if got.Ahead.WindKnNoGo == nil || *got.Ahead.WindKnNoGo != 15 || got.Abeam.WindKnNoGo != nil {
		t.Fatalf("ahead wind lost or abeam not nil: %+v", got)
	}
	if got.Ahead.WaveMNoGo == nil || *got.Ahead.WaveMNoGo != 1.5 || got.Astern.WaveMNoGo != nil {
		t.Fatalf("wave no-go lost: %+v", got)
	}
	if got.Astern.WindKnNoGo == nil || *got.Astern.WindKnNoGo != 35 {
		t.Fatalf("astern wind lost: %+v", got.Astern)
	}
	if got.WindAgainstTideOffsetKn == nil || *got.WindAgainstTideOffsetKn != 5 || !got.DaylightArrival {
		t.Fatalf("tide/daylight lost: %+v", got)
	}
}

func TestPassageLimitsEmptySaveYieldsAllNil(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {})
	if code != http.StatusOK {
		t.Fatalf("expected 200, got %d (%v)", code, body)
	}
	saved, _ := readSettings(settingsPath)
	if got := buildSettingsPayload(saved).Assistant.PassageLimits; got != (passageLimits{}) {
		t.Fatalf("expected empty limits, got %+v", got)
	}
}

func TestPostSettings_RejectsBadPassageLimits(t *testing.T) {
	cases := []struct {
		name  string
		set   func(*passageLimits)
		field string
	}{
		{"negative wind", func(l *passageLimits) { l.Ahead.WindKnNoGo = fp(-1) }, "ahead"},
		{"negative wave", func(l *passageLimits) { l.Astern.WaveMNoGo = fp(-2) }, "astern"},
		{"offset zero", func(l *passageLimits) { l.WindAgainstTideOffsetKn = fp(0) }, "wind against tide"},
		{"offset not below a sector no-go", func(l *passageLimits) {
			l.Ahead.WindKnNoGo, l.Abeam.WindKnNoGo = fp(15), fp(22)
			l.WindAgainstTideOffsetKn = fp(15)
		}, "ahead"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
			code, body := postSettings(t, settingsPath, func(p *settingsPayload) { tc.set(&p.Assistant.PassageLimits) })
			if code == http.StatusOK {
				t.Fatalf("expected a rejection, got 200")
			}
			msg, _ := body["error"].(string)
			if !strings.Contains(strings.ToLower(msg), tc.field) {
				t.Fatalf("message %q should name %q", msg, tc.field)
			}
			if f, _ := body["field"].(string); !strings.HasPrefix(f, "assistant.passage_limits") {
				t.Fatalf("field = %q", f)
			}
		})
	}
}

func appendSettings(t *testing.T, path, extra string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, []byte(extra)...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGetSettingsFailsWhenStoredPassageLimitsAreUnreadable(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	appendSettings(t, settingsPath, "assistant:\n    enabled: true\n    passage_limits:\n        ahead:\n            wind_kn:\n                no_go: 15kn\n")
	t.Setenv("SETTINGS_FILE", settingsPath)
	rec := httptest.NewRecorder()
	if err := getSettingsHandler(echo.New().NewContext(httptest.NewRequest(http.MethodGet, "/api/settings", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code == http.StatusOK {
		t.Fatalf("expected an error, got 200: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "passage_limits") {
		t.Fatalf("error should name passage_limits: %s", rec.Body.String())
	}
}

func TestPassageLimitsConversionReturnsErrors(t *testing.T) {
	for name, raw := range map[string]map[string]any{
		"bad new value":    {"ahead": map[string]any{"wind_kn_no_go": "15kn"}},
		"bad legacy value": {"ahead": map[string]any{"wind_kn": map[string]any{"no_go": "15kn"}}},
	} {
		if _, err := passageLimitsFromSettings(raw); err == nil {
			t.Fatalf("%s must be an error", name)
		}
	}
	got, err := passageLimitsFromSettings(nil)
	if err != nil || got != (passageLimits{}) {
		t.Fatalf("nil block = %+v, %v", got, err)
	}
}

// The v0.49.0 shape stored marginal, no-go and gust figures. The no-go wind
// and wave figures carry over; marginal, gust and the old tide band do not.
func TestPassageLimitsReadsTheOldShape(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	appendSettings(t, settingsPath, `assistant:
    enabled: true
    passage_limits:
        ahead:
            wind_kn: {marginal: 12, no_go: 15}
            gust_kn: {marginal: 20, no_go: 25}
            wave_m: {marginal: 1, no_go: 1.3}
        abeam:
            wind_kn: {marginal: 15, no_go: 22}
            gust_kn: {marginal: 25, no_go: 30}
            wave_m: {marginal: 1.2, no_go: 2}
        astern:
            wind_kn: {marginal: 25, no_go: 35}
            gust_kn: {marginal: 30, no_go: 40}
        wind_against_tide:
            wind_kn: {marginal: 8, no_go: 12}
        daylight_arrival: true
`)
	saved, err := readSettings(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	am, _ := saved["assistant"].(map[string]any)
	got, err := passageLimitsFromSettings(am["passage_limits"])
	if err != nil {
		t.Fatal(err)
	}
	want := passageLimits{
		Ahead:           passageSectorLimits{WindKnNoGo: fp(15), WaveMNoGo: fp(1.3)},
		Abeam:           passageSectorLimits{WindKnNoGo: fp(22), WaveMNoGo: fp(2)},
		Astern:          passageSectorLimits{WindKnNoGo: fp(35)},
		DaylightArrival: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	// The settings page shows the carried-over values.
	if p := buildSettingsPayload(saved).Assistant.PassageLimits; !reflect.DeepEqual(p, want) {
		t.Fatalf("payload %+v", p)
	}
}

func TestSaveOmitsPassageLimitsKeyWhenNoneSet(t *testing.T) {
	settingsPath := writeTestSettings(t, "203.0.113.1", 3000)
	code, body := postSettings(t, settingsPath, func(p *settingsPayload) {})
	if code != http.StatusOK {
		t.Fatalf("save: %d %v", code, body)
	}
	saved, _ := readSettings(settingsPath)
	assistant, _ := saved["assistant"].(map[string]any)
	if _, present := assistant["passage_limits"]; present {
		t.Fatalf("untouched limits must not write the key: %v", assistant)
	}
}
