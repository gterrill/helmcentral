package main

import (
	"net/http"
	"net/http/httptest"
	"os"
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
		Ahead:           passageSectorLimits{WindKn: passageLimitBand{Marginal: fp(10), NoGo: fp(15)}, WaveM: passageLimitBand{NoGo: fp(1.5)}},
		Astern:          passageSectorLimits{GustKn: passageLimitBand{Marginal: fp(25), NoGo: fp(30)}},
		WindAgainstTide: passageWindAgainstTide{WindKn: passageLimitBand{NoGo: fp(12)}},
		DaylightArrival: true,
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
	if got.Ahead.WindKn.Marginal == nil || *got.Ahead.WindKn.Marginal != 10 || *got.Ahead.WindKn.NoGo != 15 {
		t.Fatalf("ahead wind lost: %+v", got.Ahead.WindKn)
	}
	if got.Ahead.WindKn.NoGo == nil || got.Ahead.GustKn.NoGo != nil || got.Abeam.WindKn.NoGo != nil {
		t.Fatalf("unset bands must stay nil: %+v", got)
	}
	if got.Ahead.WaveM.NoGo == nil || *got.Ahead.WaveM.NoGo != 1.5 || got.Ahead.WaveM.Marginal != nil {
		t.Fatalf("one-sided band lost: %+v", got.Ahead.WaveM)
	}
	if got.Astern.GustKn.Marginal == nil || *got.Astern.GustKn.NoGo != 30 {
		t.Fatalf("astern gust lost: %+v", got.Astern.GustKn)
	}
	if got.WindAgainstTide.WindKn.NoGo == nil || *got.WindAgainstTide.WindKn.NoGo != 12 || !got.DaylightArrival {
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
		{"negative", func(l *passageLimits) { l.Ahead.WindKn.NoGo = fp(-1) }, "ahead"},
		{"marginal not below no-go", func(l *passageLimits) { l.Abeam.GustKn = passageLimitBand{Marginal: fp(20), NoGo: fp(20)} }, "abeam"},
		{"astern wave inverted", func(l *passageLimits) { l.Astern.WaveM = passageLimitBand{Marginal: fp(3), NoGo: fp(2)} }, "astern"},
		{"tide inverted", func(l *passageLimits) { l.WindAgainstTide.WindKn = passageLimitBand{Marginal: fp(9), NoGo: fp(8)} }, "wind against tide"},
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
	if _, err := passageLimitsFromSettings(map[string]any{"ahead": map[string]any{"wind_kn": map[string]any{"no_go": "15kn"}}}); err == nil {
		t.Fatal("a bad stored value must be an error")
	}
	got, err := passageLimitsFromSettings(nil)
	if err != nil || got != (passageLimits{}) {
		t.Fatalf("nil block = %+v, %v", got, err)
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
