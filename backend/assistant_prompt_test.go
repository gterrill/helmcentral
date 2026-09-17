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

// ── hull type ───────────────────────────────────────────────────────────

func TestAssistantHullTypePhrase(t *testing.T) {
	cases := []struct{ in, want string }{
		{"power_cat", "power catamaran"},
		{"sail_cat", "sailing catamaran"},
		{"power_mono", "power monohull"},
		{"sail_mono", "sailing monohull"},
		{"", ""},
		{"hovercraft", ""},
	}
	for _, tc := range cases {
		if got := assistantHullTypePhrase(tc.in); got != tc.want {
			t.Errorf("assistantHullTypePhrase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildAssistantSystemPrompt_IdentityNamesMate(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	if !strings.HasPrefix(prompt, "You are Mate, the onboard passage-planning assistant aboard") {
		t.Fatalf("expected the identity line to open with Mate's name, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "The crew address you as Mate.") {
		t.Fatalf("expected the identity line to tell Mate how the crew addresses it, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_IdentityLineIncludesHullPhrase(t *testing.T) {
	pc := basePromptContext()
	pc.BoatModel = "2025 Granocean W-60"
	pc.HullType = "power_cat"

	prompt := buildAssistantSystemPrompt(pc)
	want := "a 2025 Granocean W-60 power catamaran (LOA unknown)"
	if !strings.Contains(prompt, want) {
		t.Fatalf("expected the identity line to include the hull phrase %q, got:\n%s", want, prompt)
	}
}

func TestBuildAssistantSystemPrompt_IdentityLineOmitsHullPhraseWhenBlank(t *testing.T) {
	pc := basePromptContext()
	pc.BoatModel = "2025 Granocean W-60"
	pc.HullType = ""

	prompt := buildAssistantSystemPrompt(pc)
	// The identity line itself must read exactly as it did before this hull
	// phrase existed - no trailing space, no stray phrase. Section 7's tool
	// guidance separately mentions "power catamaran" as a fixed illustrative
	// example regardless of this vessel's own hull type, so the assertion
	// is scoped to the identity sentence rather than the whole prompt.
	identityLine := strings.SplitN(prompt, "\n", 2)[0]
	want := "You are Mate, the onboard passage-planning assistant aboard the vessel, a 2025 Granocean W-60 (LOA unknown). The crew address you as Mate."
	if identityLine != want {
		t.Fatalf("expected the identity line unchanged when hull type is blank, got:\n%s", identityLine)
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
		"estimate_passage",
		"following sea",
		"course_deg",
		"rel_wind",
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

// The operator asked "Can you cross Bass Strait in day hops? What anchorage
// would you depart from?" while lying off Townsville, meaning a trip months
// away. Mate opened with the boat's position, quoted today's wind warning,
// and tried to work fuel from the tanks, because every rule assumed a
// departure from here, now. A trip beyond the forecast, or from somewhere
// else, is a planning question and gets seasonal reasoning instead.
func TestBuildAssistantSystemPrompt_PlanningHorizonGuidance(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	for _, want := range []string{
		"work out when and from where the plan starts",
		"later than the forecast covers",
		"somewhere other than where the boat is now",
		"do not fetch today's wind forecast or tides",
		"do not cite the current marine warnings",
		"fuel on board",
		"prevailing winds and weather systems for that region in that month",
		"between the planned points themselves, not from the vessel's position",
		"say which you assumed",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the planning-horizon guidance to mention %q, got:\n%s", want, prompt)
		}
	}
	// The live-data rules must be scoped to a near-term departure, not
	// stated as unconditional.
	for _, want := range []string{
		"When the plan starts now or within the forecast range, for every candidate anchorage",
		"For a passage leaving from the boat's position within the forecast range",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("expected the live-data rule to be scoped with %q, got:\n%s", want, prompt)
		}
	}
}

func TestBuildAssistantSystemPrompt_AsksToCreateRouteForPassage(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	if !strings.Contains(prompt, "Would you like me to create a route for this passage?") {
		t.Fatalf("expected the prompt to tell Mate to offer route creation as a follow-up, got:\n%s", prompt)
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

// ── screen context (mate-voice-assistant plan, "app-wide voice") ───────

func TestBuildAssistantSystemPrompt_ScreenSentenceForKnownPanel(t *testing.T) {
	pc := basePromptContext()
	pc.Screen = assistantScreenContext{Panel: "forecast"}

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "The operator is looking at the Forecast panel.") {
		t.Fatalf("expected the screen sentence for the forecast panel, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_ScreenSentenceForDashboardPage(t *testing.T) {
	pc := basePromptContext()
	pc.Screen = assistantScreenContext{Page: "Engine Room"}

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "The operator is looking at the dashboard page named Engine Room.") {
		t.Fatalf("expected the screen sentence for a dashboard page, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_ScreenSentenceForSettingsSection(t *testing.T) {
	pc := basePromptContext()
	pc.Screen = assistantScreenContext{Panel: "settings", Section: "network"}

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "Settings (section network) panel") {
		t.Fatalf("expected the screen sentence to name the settings section, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_ScreenSentenceUnknownPanelPrintsVerbatim(t *testing.T) {
	pc := basePromptContext()
	pc.Screen = assistantScreenContext{Panel: "custom-widget"}

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "The operator is looking at the custom-widget panel.") {
		t.Fatalf("expected an unrecognised panel id to print verbatim, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_ScreenSentenceOmittedWhenAbsent(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	if strings.Contains(prompt, "The operator is looking at") {
		t.Fatalf("expected no screen sentence when the screen context is empty, got:\n%s", prompt)
	}
}

// ── spoken summary instruction ──────────────────────────────────────────

func TestBuildAssistantSystemPrompt_SpokenAddsSummaryInstruction(t *testing.T) {
	pc := basePromptContext()
	pc.Spoken = true

	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "## Spoken summary") {
		t.Fatalf("expected the spoken-summary instruction to name the heading, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "at most three sentences") {
		t.Fatalf("expected the spoken-summary instruction to cap the length, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_SpokenFalseOmitsSummaryInstruction(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	if strings.Contains(prompt, "Spoken summary") {
		t.Fatalf("expected no spoken-summary instruction when spoken is false, got:\n%s", prompt)
	}
}

// ── operator manual (read_manual tool, mate-voice-assistant plan) ──────

func TestBuildAssistantSystemPrompt_ManualIndexLinePresentWhenPagesExist(t *testing.T) {
	pc := basePromptContext()
	pc.ManualPages = []manualPage{
		{ID: "features/alarms", Title: "Alarms"},
		{ID: "features/forecast", Title: "Forecast"},
	}

	prompt := buildAssistantSystemPrompt(pc)
	want := "Manual pages: features/alarms (Alarms), features/forecast (Forecast)"
	if !strings.Contains(prompt, want) {
		t.Fatalf("expected the manual index line, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_ManualIndexLineOmittedWhenNoPages(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	if strings.Contains(prompt, "Manual pages:") {
		t.Fatalf("expected no manual index line when no pages are embedded, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_ReadManualGuidancePresent(t *testing.T) {
	prompt := buildAssistantSystemPrompt(basePromptContext())
	if !strings.Contains(prompt, "call read_manual for the relevant page first") {
		t.Fatalf("expected the read_manual tool-use guidance, got:\n%s", prompt)
	}
}

// ── document library (ADR 0106) ──────────────────────────────────────────

func TestBuildAssistantSystemPrompt_DocumentLibraryToolGuidanceInStablePrefix(t *testing.T) {
	stable, live := assistantSystemPromptParts(basePromptContext())
	if !strings.Contains(stable, "search_documents") || !strings.Contains(stable, "read_document") {
		t.Fatalf("expected the document library tool guidance in the stable prefix, got:\n%s", stable)
	}
	if strings.Contains(live, "search_documents") {
		t.Fatalf("expected the tool guidance NOT to be duplicated in the live suffix, got:\n%s", live)
	}
}

func TestBuildAssistantSystemPrompt_DocumentLibraryLiveLineOmittedWhenNoDocuments(t *testing.T) {
	pc := basePromptContext()
	pc.DocumentCount = 0
	prompt := buildAssistantSystemPrompt(pc)
	if strings.Contains(prompt, "Document library:") {
		t.Fatalf("expected no document library line when the store is empty, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_DocumentLibraryLiveLineListsTopLevelFolders(t *testing.T) {
	pc := basePromptContext()
	pc.DocumentCount = 7
	pc.DocumentFolderNames = []string{"Manuals", "Receipts"}
	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "Document library: 7 documents in folders: Manuals, Receipts") {
		t.Fatalf("expected the document library live line, got:\n%s", prompt)
	}
}

func TestBuildAssistantSystemPrompt_DocumentLibraryLiveLineWithNoTopLevelFolders(t *testing.T) {
	pc := basePromptContext()
	pc.DocumentCount = 3
	pc.DocumentFolderNames = nil
	prompt := buildAssistantSystemPrompt(pc)
	if !strings.Contains(prompt, "Document library: 3 documents.") {
		t.Fatalf("expected a folderless document library line, got:\n%s", prompt)
	}
}

func TestAssistantSystemPromptParts_DocumentLibraryLineInLiveSuffix(t *testing.T) {
	pc := basePromptContext()
	pc.DocumentCount = 2
	pc.DocumentFolderNames = []string{"Manuals"}
	stable, live := assistantSystemPromptParts(pc)
	if strings.Contains(stable, "Document library:") {
		t.Fatalf("expected the document library line NOT to be in the stable prefix, got:\n%s", stable)
	}
	if !strings.Contains(live, "Document library:") {
		t.Fatalf("expected the document library line in the live suffix, got:\n%s", live)
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
		"  hull_type: power_cat\n" +
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
	if pc.HullType != "power_cat" {
		t.Errorf("expected hull type %q, got %q", "power_cat", pc.HullType)
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

func TestCollectAssistantPromptContext_CopiesGlobalManual(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)

	prevManual := globalManual
	globalManual = []manualPage{{ID: "features/forecast", Title: "Forecast"}}
	t.Cleanup(func() { globalManual = prevManual })

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	if len(pc.ManualPages) != 1 || pc.ManualPages[0].ID != "features/forecast" {
		t.Fatalf("expected collectAssistantPromptContext to copy globalManual, got %+v", pc.ManualPages)
	}
}

func TestCollectAssistantPromptContext_NilDocumentStoreLeavesZeroDocumentCount(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)

	prev := globalDocumentStore
	globalDocumentStore = nil
	t.Cleanup(func() { globalDocumentStore = prev })

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	if pc.DocumentCount != 0 || len(pc.DocumentFolderNames) != 0 {
		t.Fatalf("expected no document library context with a nil store, got count=%d folders=%v", pc.DocumentCount, pc.DocumentFolderNames)
	}
}

func TestCollectAssistantPromptContext_DocumentLibraryCountsAndTopLevelFolderNames(t *testing.T) {
	settingsPath := writeAssistantPromptSettings(t)
	docs := withTestDocumentStore(t)

	manuals, err := docs.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := docs.CreateFolder("Receipts", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := docs.Insert(document{SHA256: "sha-1", Filename: "a.pdf", MIME: "application/pdf", FolderID: &manuals.ID}); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := docs.Insert(document{SHA256: "sha-2", Filename: "b.pdf", MIME: "application/pdf"}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	pc := collectAssistantPromptContext(settingsPath, time.Now())
	if pc.DocumentCount != 2 {
		t.Fatalf("expected DocumentCount 2, got %d", pc.DocumentCount)
	}
	if len(pc.DocumentFolderNames) != 2 || pc.DocumentFolderNames[0] != "Manuals" || pc.DocumentFolderNames[1] != "Receipts" {
		t.Fatalf("expected top-level folder names [Manuals Receipts], got %v", pc.DocumentFolderNames)
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

// ── prompt-caching split (backend perf audit, Tier 3 item 3) ───────────

func TestAssistantSystemPromptParts_ManualIndexBeforeLiveContext(t *testing.T) {
	pc := basePromptContext()
	pc.ManualPages = []manualPage{{ID: "features/forecast", Title: "Forecast"}}
	pc.Latitude, pc.Longitude = -20.1, 149.1
	pc.PlaceName = "Tongue Bay"

	stable, live := assistantSystemPromptParts(pc)
	if !strings.Contains(stable, "Manual pages:") {
		t.Fatalf("expected the manual index in the stable prefix, got:\n%s", stable)
	}
	if strings.Contains(live, "Manual pages:") {
		t.Fatalf("expected the manual index NOT to be in the live suffix, got:\n%s", live)
	}
	if !strings.Contains(live, "Position:") {
		t.Fatalf("expected the position line in the live suffix, got:\n%s", live)
	}
	if strings.Contains(stable, "Position:") {
		t.Fatalf("expected the position line NOT to be in the stable prefix, got:\n%s", stable)
	}
}

func TestAssistantSystemPromptParts_IdentityAndGuidanceInStablePrefix(t *testing.T) {
	stable, live := assistantSystemPromptParts(basePromptContext())
	if !strings.HasPrefix(stable, "You are Mate,") {
		t.Fatalf("expected the stable prefix to open with Mate's identity, got:\n%s", stable)
	}
	if !strings.Contains(stable, "fetch both get_wind_forecast and get_tides") {
		t.Fatalf("expected the fixed tool-use guidance in the stable prefix, got:\n%s", stable)
	}
	if !strings.Contains(stable, "Operator standing notes:") {
		t.Fatalf("expected standing notes in the stable prefix, got:\n%s", stable)
	}
	if strings.Contains(live, "fetch both get_wind_forecast and get_tides") {
		t.Fatalf("expected the guidance NOT to be duplicated in the live suffix, got:\n%s", live)
	}
	if !strings.Contains(live, "Heading:") {
		t.Fatalf("expected the live heading/speed/wind line in the live suffix, got:\n%s", live)
	}
}

func TestAssistantSystemPromptParts_StablePrefixByteIdenticalAcrossTurns(t *testing.T) {
	pc1 := basePromptContext()
	pc1.Latitude, pc1.Longitude = -20.1, 149.1
	pc1.HeadingTrue = 45

	pc2 := basePromptContext()
	pc2.Now = pc2.Now.Add(3 * time.Hour)
	pc2.Latitude, pc2.Longitude = -19.5, 148.7
	pc2.HeadingTrue = 210
	pc2.SpeedOverGroundKts = 6.2

	stable1, _ := assistantSystemPromptParts(pc1)
	stable2, _ := assistantSystemPromptParts(pc2)
	if stable1 != stable2 {
		t.Fatalf("expected the stable prefix to be byte-identical across turns with different live context, got:\n%s\n---\n%s", stable1, stable2)
	}
}
