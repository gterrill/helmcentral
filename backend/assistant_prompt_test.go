package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// basePromptContext is a fully-sentinel-guarded context (every live field at
// its -1 "unknown" default, longitude fixed at 149 so the time/zone tests
// have a stable, non-zero offset to check) that the tests below mutate one
// field at a time.
func basePromptContext() assistantPromptContext {
	return assistantPromptContext{
		Now:                  time.Date(2026, 9, 11, 4, 5, 0, 0, time.UTC),
		Latitude:             -1,
		Longitude:            149,
		HeadingTrue:          -1,
		SpeedOverGroundKts:   -1,
		WindSpeedApparentKts: -1,
		WindAngleApparentDeg: -1,
	}
}

func TestBuildAssistantSystemPrompt_TimeLabelAtLon149(t *testing.T) {
	pc := basePromptContext()

	prompt := buildAssistantSystemPrompt(pc)

	loc := vesselLocalLocation(149)
	wantTimeLabel := pc.Now.In(loc).Format("Monday 2 January 2006 15:04")
	if !strings.Contains(prompt, wantTimeLabel) {
		t.Fatalf("expected the prompt to contain the local time label %q, got:\n%s", wantTimeLabel, prompt)
	}
	if !strings.Contains(prompt, "UTC+10, derived from the vessel's longitude") {
		t.Fatalf("expected the prompt to name the UTC+10 zone and its derivation, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_PositionAndPlaceName(t *testing.T) {
	pc := basePromptContext()
	pc.Latitude, pc.Longitude = -20.123, 149.456
	pc.PlaceName = "Tongue Bay"

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "-20.1230, 149.4560") {
		t.Fatalf("expected the prompt to contain the formatted position, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "near Tongue Bay") {
		t.Fatalf("expected the prompt to name the place, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_SentinelsPrintAsUnknownAndNeverLiteralMinusOne(t *testing.T) {
	pc := basePromptContext()
	pc.Longitude = -1 // stress every sentinel-derived section at once

	prompt := buildAssistantSystemPrompt(pc)

	if strings.Contains(prompt, "-1") {
		t.Fatalf(`the literal string "-1" must never appear in the prompt, got:%s`+"\n", prompt)
	}
	if !strings.Contains(prompt, "Position unknown") {
		t.Fatalf("expected the position section to say unknown, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Heading: unknown") {
		t.Fatalf("expected heading to print as unknown, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Speed over ground: unknown") {
		t.Fatalf("expected speed over ground to print as unknown, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Apparent wind: unknown") {
		t.Fatalf("expected apparent wind to print as unknown, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_WarningLevelTwoMentionsGale(t *testing.T) {
	pc := basePromptContext()
	pc.WarningOK = true
	pc.WarningLevel = 2
	pc.WarningFetchedAt = pc.Now

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(strings.ToLower(prompt), "gale") {
		t.Fatalf("expected the warning line to mention a gale warning, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_NoWarningInForce(t *testing.T) {
	pc := basePromptContext()
	pc.WarningOK = true
	pc.WarningLevel = 0
	pc.WarningFetchedAt = pc.Now

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "no official marine warning") {
		t.Fatalf("expected the no-warning-in-force line, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_WarningFeedHasNotReported(t *testing.T) {
	pc := basePromptContext()
	pc.WarningOK = false

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "has not reported") {
		t.Fatalf("expected the not-reported line when the slot is empty, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_NotesVerbatimOrNone(t *testing.T) {
	pc := basePromptContext()
	pc.Notes = "Queenfish bite best on a rising tide.\nSnorkel Blue Pearl Bay in the last 2h of flood."

	prompt := buildAssistantSystemPrompt(pc)
	want := "Operator standing notes:\nQueenfish bite best on a rising tide.\nSnorkel Blue Pearl Bay in the last 2h of flood."
	if !strings.Contains(prompt, want) {
		t.Fatalf("expected the notes to appear verbatim, got:\n%s", prompt)
	}

	pc2 := basePromptContext()
	pc2.Notes = "   "
	prompt2 := buildAssistantSystemPrompt(pc2)
	if !strings.Contains(prompt2, "Operator standing notes:\n(none)") {
		t.Fatalf("expected (none) for blank notes, got:\n%s", prompt2)
	}
}

func TestBuildAssistantSystemPrompt_ToolGuidancePresent(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	for _, want := range []string{
		"bare feature name",
		"lee shore",
		"general knowledge",
		"standing notes",
		"check against the chart",
		"No exclamation marks",
		"fetch both get_wind_forecast and get_tides",
		"do not guess or fabricate",
		"knots for wind speed, nautical miles for distance, and metres",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the tool guidance to mention %q, got:\n%s", want, prompt)
		}
	}
	for _, notWant := range []string{
		"Be concise",
		"Prefer the operator's standing notes below over general knowledge",
	} {
		if strings.Contains(prompt, notWant) {
			t.Errorf("expected the tool guidance NOT to contain %q, got:\n%s", notWant, prompt)
		}
	}
}

func TestBuildAssistantSystemPrompt_ProviderLine(t *testing.T) {
	pc := basePromptContext()
	pc.WeatherProvider = "open-meteo"
	pc.WaveProvider = "open-meteo-marine"
	pc.TideProvider = "bom"
	pc.TideStationName = "Cid Harbour"

	prompt := buildAssistantSystemPrompt(pc)
	want := "Weather provider: open-meteo. Wave provider: open-meteo-marine. Tide provider: bom. Configured tide station: Cid Harbour."
	if !strings.Contains(prompt, want) {
		t.Fatalf("expected the provider line, got:\n%s", prompt)
	}

	prompt2 := buildAssistantSystemPrompt(basePromptContext())
	if !strings.Contains(prompt2, "not configured") || !strings.Contains(prompt2, "none configured") {
		t.Fatalf("expected unconfigured providers/station to say so plainly, got:\n%s", prompt2)
	}
}

// ── collectAssistantPromptContext ──────────────────────────────────────

func writeAssistantPromptSettings(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "settings.yaml")
	content := "boat:\n" +
		"  model: Test Cat\n" +
		"anchor:\n" +
		"  loa_m: 12.5\n" +
		"ui:\n" +
		"  weather_provider: open-meteo\n" +
		"  wave_provider: open-meteo-marine\n" +
		"  tide_provider: bom\n" +
		"  tide_station_name: Cid Harbour\n" +
		"assistant:\n" +
		"  notes: |\n" +
		"    Queenfish bite best on a rising tide in Hill Inlet.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
	return path
}

func TestCollectAssistantPromptContext_ReadsSettingsOnce(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)
	pc := collectAssistantPromptContext(settingsPath, time.Now())

	if pc.BoatModel != "Test Cat" {
		t.Errorf("expected boat model %q, got %q", "Test Cat", pc.BoatModel)
	}
	if pc.LOAM != 12.5 {
		t.Errorf("expected LOA 12.5, got %v", pc.LOAM)
	}
	if pc.WeatherProvider != "open-meteo" || pc.WaveProvider != "open-meteo-marine" || pc.TideProvider != "bom" {
		t.Errorf("expected provider ids from settings, got weather=%q wave=%q tide=%q", pc.WeatherProvider, pc.WaveProvider, pc.TideProvider)
	}
	if pc.TideStationName != "Cid Harbour" {
		t.Errorf("expected tide station name %q, got %q", "Cid Harbour", pc.TideStationName)
	}
	if !strings.Contains(pc.Notes, "Queenfish") {
		t.Errorf("expected the standing notes to come through, got %q", pc.Notes)
	}
}

func TestCollectAssistantPromptContext_PinnedAnchorPlaceNameBeatsRoaming(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)

	withAnchorWatchState(t, &anchorWatchData{Lat: -20.1, Lon: 149.1, PlaceName: "Pinned Anchorage"})
	origName := getCurrentPlaceName()
	t.Cleanup(func() { setCurrentPlaceName(origName) })
	setCurrentPlaceName("Roaming Place")

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	if pc.PlaceName != "Pinned Anchorage" {
		t.Fatalf("expected the pinned anchor watch place name to win over the roaming one, got %q", pc.PlaceName)
	}
}

func TestCollectAssistantPromptContext_NoAnchorWatchUsesRoamingPlaceName(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)

	withAnchorWatchState(t, nil)
	origName := getCurrentPlaceName()
	t.Cleanup(func() { setCurrentPlaceName(origName) })
	setCurrentPlaceName("Roaming Place")

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	if pc.PlaceName != "Roaming Place" {
		t.Fatalf("expected the roaming place name with no anchor watch active, got %q", pc.PlaceName)
	}
}

func TestCollectAssistantPromptContext_FailedVesselStateFetchLeavesSentinels(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)
	resetGNSSPositionValidationState()
	t.Cleanup(resetGNSSPositionValidationState)
	withGlobalSnapshot(t, newSignalKSnapshot())

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	if pc.Latitude != -1 || pc.Longitude != -1 {
		t.Fatalf("expected sentinel lat/lon on a failed vessel state fetch, got %v,%v", pc.Latitude, pc.Longitude)
	}
	if pc.HeadingTrue != -1 || pc.SpeedOverGroundKts != -1 || pc.WindSpeedApparentKts != -1 {
		t.Fatalf("expected sentinel live values on a failed vessel state fetch, got %+v", pc)
	}
}
