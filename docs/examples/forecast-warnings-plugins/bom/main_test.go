package main

import (
	"errors"
	"testing"
)

// --- Fixtures below were captured live from the BOM anonymous FTP mirror
// (ftp://ftp.bom.gov.au/anon/gen/fwo/<PRODUCT_ID>.txt) during planning of
// the formerly-native backend/bom_marine_warnings_test.go - reused verbatim
// here, not paraphrased, since this plugin's parsing logic is a verbatim
// port of that file's.

const qldMarineWindWarningFixture = `IDQ20085
Australian Government Bureau of Meteorology
Queensland

Marine Wind Warning Summary for Queensland
Issued at 11:51 am EST on Sunday 5 July 2026
for the period until midnight EST Monday 6 July 2026.

Wind Warnings for Sunday 5 July
Strong Wind Warning for the following area:
Capricornia Coast

Wind Warnings for Monday 6 July
Strong Wind Warning for the following areas:
Peninsula Coast, Cooktown Coast, Cairns Coast, Townsville Coast, Mackay Coast,
Capricornia Coast, K'gari Coast, Sunshine Coast Waters and Gold Coast Waters

The next marine wind warning summary will be issued by 3:00 pm EST Sunday.

================================================================================
Check the latest Coastal Waters Forecast or Local Waters Forecast at
http://www.bom.gov.au/qld/forecasts/map.shtml for information on wind, wave and
weather conditions for these coastal zones.
================================================================================


Copyright Commonwealth of Australia 2011, Bureau of Meteorology (ABN 92 637 533
532).  Users of these web pages are deemed to have read and accepted the
conditions described in the Copyright, Disclaimer, and Privacy statements
(http://www.bom.gov.au/other/copyright.shtml).
`

const qldHazardousSurfWarningFixture = `IDQ28522
Australian Government Bureau of Meteorology
Queensland

Hazardous Surf Warning for Queensland

Issued at 11:45 am EST on Sunday 5 July 2026
for the period until midnight EST Monday 6 July 2026.

Surf and swell conditions are expected to be hazardous for coastal activities
such as rock fishing, boating, and swimming in the following areas.

Monday 6 July

Hazardous Surf Warning for:
K'gari Coast, Sunshine Coast Waters and Gold Coast Waters

Safety Advice
Surf Life Saving Queensland advise that:
  - People should consider staying out of the water and avoid walking near
    surf-exposed areas.

The next warning will be issued by 5:00 pm EST Sunday.

================================================================================
Check the Coastal Waters Forecast for information on wind, wave and weather
conditions for these areas at http://www.bom.gov.au/qld/ or on marine radio.
================================================================================

Copyright Commonwealth of Australia 2011, Bureau of Meteorology (ABN 92 637 533
532).  Users of these web pages are deemed to have read and accepted the
conditions described in the Copyright, Disclaimer, and Privacy statements
(http://www.bom.gov.au/other/copyright.shtml).
`

const vicMarineWindWarningCancellationFixture = `IDV20600
Australian Government Bureau of Meteorology
Victoria

Marine Wind Warning Summary for Victoria
Issued at 10:00 am EST on Sunday 5 July 2026
for the period until midnight EST Sunday 5 July 2026.

Wind Warnings for Sunday 5 July
Cancellation for the following area:
East Gippsland Coast

The next Marine Wind Warning Summary will be issued when required.

================================================================================
Check the latest Coastal Waters Forecast or Local Waters Forecast at
http://www.bom.gov.au/vic/forecasts/map.shtml for information on wind, wave and
weather conditions for these coastal zones.
================================================================================


Copyright Commonwealth of Australia 2011, Bureau of Meteorology (ABN 92 637 533
532).  Users of these web pages are deemed to have read and accepted the
conditions described in the Copyright, Disclaimer, and Privacy statements
(http://www.bom.gov.au/other/copyright.shtml).
`

// tasMarineWindWarningFixture is a small synthetic fixture (TAS has no real
// public sample captured) used only to prove that a fetch can succeed and
// parse a non-cancellation section - buildFetchWarningsOutput must still
// return zero bulletins for TAS since tasZoneForPosition is an unresolved
// stub (see bom_zones.go).
const tasMarineWindWarningFixture = `IDT20100
Australian Government Bureau of Meteorology
Tasmania

Marine Wind Warning Summary for Tasmania
Issued at 9:00 am EST on Sunday 5 July 2026
for the period until midnight EST Sunday 5 July 2026.

Wind Warnings for Sunday 5 July
Strong Wind Warning for the following area:
Some Tasmanian Coast

The next Marine Wind Warning Summary will be issued when required.
`

// --- Zone/state resolution tests (ported from backend/bom_marine_zones_test.go) ---

func TestQldZoneForPosition_MackayCapricorniaBoundary(t *testing.T) {
	// 21°35'43.1"S 149°47'47.3"E - a real vessel fix south of Mackay,
	// north of St Lawrence.
	lat, lon := -21.5953, 149.7965

	zone, ok := qldZoneForPosition(lat, lon)
	if !ok {
		t.Fatalf("qldZoneForPosition(%v, %v) = not ok, want Mackay Coast", lat, lon)
	}
	if zone != "Mackay Coast" {
		t.Errorf("qldZoneForPosition(%v, %v) = %q, want %q", lat, lon, zone, "Mackay Coast")
	}
}

func TestQldZoneForPosition_JustSouthOfStLawrenceIsCapricornia(t *testing.T) {
	lat, lon := -22.4, 150.0

	zone, ok := qldZoneForPosition(lat, lon)
	if !ok {
		t.Fatalf("qldZoneForPosition(%v, %v) = not ok, want Capricornia Coast", lat, lon)
	}
	if zone != "Capricornia Coast" {
		t.Errorf("qldZoneForPosition(%v, %v) = %q, want %q", lat, lon, zone, "Capricornia Coast")
	}
}

func TestStateForPosition_ResolvesEachMappedState(t *testing.T) {
	cases := []struct {
		name      string
		lat, lon  float64
		wantState string
	}{
		{"QLD Capricornia Coast", -22.4, 150.0, "QLD"},
		{"VIC East Gippsland Coast", -38.0, 149.0, "VIC"},
		{"TAS", -42.0, 146.0, "TAS"},
		{"WA", -20.0, 115.0, "WA"},
		{"SA (unmapped zone table)", -34.0, 137.0, "SA"},
		{"NT (unmapped zone table)", -15.0, 133.0, "NT"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, ok := stateForPosition(tc.lat, tc.lon)
			if !ok {
				t.Fatalf("stateForPosition(%v, %v) = not ok, want %q", tc.lat, tc.lon, tc.wantState)
			}
			if state != tc.wantState {
				t.Errorf("stateForPosition(%v, %v) = %q, want %q", tc.lat, tc.lon, state, tc.wantState)
			}
		})
	}
}

func TestStateForPosition_UnresolvedForPositionOutsideAnyBox(t *testing.T) {
	// Mid-Pacific, northern hemisphere - nowhere near an Australian bounding
	// box.
	if _, ok := stateForPosition(10.0, -150.0); ok {
		t.Fatalf("expected stateForPosition to fail for a position far outside Australia")
	}
}

func TestZoneForPosition_TasAndWaAreUnresolvedStubs(t *testing.T) {
	if _, ok := zoneForPosition("TAS", -42.0, 146.0); ok {
		t.Fatalf("expected TAS zone resolution to remain an unresolved stub")
	}
	if _, ok := zoneForPosition("WA", -20.0, 115.0); ok {
		t.Fatalf("expected WA zone resolution to remain an unresolved stub")
	}
}

func TestZoneForPosition_SaAndNtHaveNoZoneTable(t *testing.T) {
	if _, ok := zoneForPosition("SA", -34.0, 137.0); ok {
		t.Fatalf("expected SA to have no zone table")
	}
	if _, ok := zoneForPosition("NT", -15.0, 133.0); ok {
		t.Fatalf("expected NT to have no zone table")
	}
}

// --- Parsing tests (ported from backend/bom_marine_warnings_test.go) ---

func TestParseBomMarineWarningText_QLDMarineWindWarning_ParsesTitleAndIssuedAt(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)

	if bulletin.Title != "Marine Wind Warning Summary for Queensland" {
		t.Fatalf("expected title %q, got %q", "Marine Wind Warning Summary for Queensland", bulletin.Title)
	}
	if bulletin.IssuedAtRaw != "11:51 am EST on Sunday 5 July 2026" {
		t.Fatalf("expected raw issued-at %q, got %q", "11:51 am EST on Sunday 5 July 2026", bulletin.IssuedAtRaw)
	}
	if bulletin.IssuedAt.IsZero() {
		t.Fatalf("expected issued-at to parse to a non-zero time")
	}
}

func TestParseBomMarineWarningText_QLDMarineWindWarning_ParsesDaySectionsAndZones(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)

	if len(bulletin.Sections) != 2 {
		t.Fatalf("expected 2 day-sections, got %d: %+v", len(bulletin.Sections), bulletin.Sections)
	}

	sunday := bulletin.Sections[0]
	if sunday.Day != "Sunday 5 July" || sunday.WarningType != "Strong Wind Warning" {
		t.Fatalf("unexpected Sunday section: %+v", sunday)
	}
	if !containsZoneFold(sunday.Zones, "Capricornia Coast") || len(sunday.Zones) != 1 {
		t.Fatalf("expected exactly [Capricornia Coast] zones for Sunday, got %+v", sunday.Zones)
	}

	monday := bulletin.Sections[1]
	if monday.Day != "Monday 6 July" || monday.WarningType != "Strong Wind Warning" {
		t.Fatalf("unexpected Monday section: %+v", monday)
	}
	expectedMondayZones := []string{
		"Peninsula Coast", "Cooktown Coast", "Cairns Coast", "Townsville Coast",
		"Mackay Coast", "Capricornia Coast", "K'gari Coast", "Sunshine Coast Waters",
		"Gold Coast Waters",
	}
	if len(monday.Zones) != len(expectedMondayZones) {
		t.Fatalf("expected %d zones for Monday, got %d: %+v", len(expectedMondayZones), len(monday.Zones), monday.Zones)
	}
	for _, zone := range expectedMondayZones {
		if !containsZoneFold(monday.Zones, zone) {
			t.Fatalf("expected zone %q in Monday zones, got %+v", zone, monday.Zones)
		}
	}
}

func TestParseBomMarineWarningText_QLDMarineWindWarning_HasActiveWarning(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)
	if !hasActiveWarning(bulletin) {
		t.Fatalf("expected QLD marine wind warning bulletin to have an active warning")
	}
}

func TestParseBomMarineWarningText_QLDHazardousSurfWarning_ParsesSectionAndZones(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldHazardousSurfWarningFixture)

	if bulletin.Title != "Hazardous Surf Warning for Queensland" {
		t.Fatalf("expected title %q, got %q", "Hazardous Surf Warning for Queensland", bulletin.Title)
	}
	if len(bulletin.Sections) != 1 {
		t.Fatalf("expected 1 day-section, got %d: %+v", len(bulletin.Sections), bulletin.Sections)
	}

	section := bulletin.Sections[0]
	if section.Day != "Monday 6 July" || section.WarningType != "Hazardous Surf Warning" {
		t.Fatalf("unexpected section: %+v", section)
	}
	expectedZones := []string{"K'gari Coast", "Sunshine Coast Waters", "Gold Coast Waters"}
	for _, zone := range expectedZones {
		if !containsZoneFold(section.Zones, zone) {
			t.Fatalf("expected zone %q, got %+v", zone, section.Zones)
		}
	}
}

func TestParseBomMarineWarningText_VICCancellation_ParsesButIsNotActive(t *testing.T) {
	bulletin := parseBomMarineWarningText(vicMarineWindWarningCancellationFixture)

	if len(bulletin.Sections) != 1 {
		t.Fatalf("expected 1 day-section, got %d: %+v", len(bulletin.Sections), bulletin.Sections)
	}
	section := bulletin.Sections[0]
	if section.WarningType != "Cancellation" {
		t.Fatalf("expected warning type %q, got %q", "Cancellation", section.WarningType)
	}
	if !containsZoneFold(section.Zones, "East Gippsland Coast") {
		t.Fatalf("expected East Gippsland Coast zone, got %+v", section.Zones)
	}
	if hasActiveWarning(bulletin) {
		t.Fatalf("expected VIC cancellation-only bulletin to NOT be an active warning")
	}
}

func TestHasActiveWarningForZone_OnlyTrueForNamedZoneAndNotCancelled(t *testing.T) {
	windBulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)
	if !hasActiveWarningForZone(windBulletin, "Capricornia Coast") {
		t.Fatalf("expected an active warning for Capricornia Coast")
	}
	if hasActiveWarningForZone(windBulletin, "Gulf Waters") {
		t.Fatalf("expected no active warning for Gulf Waters (not named in this bulletin)")
	}
	if hasActiveWarningForZone(windBulletin, "") {
		t.Fatalf("expected no active warning when zone is empty")
	}

	cancelledBulletin := parseBomMarineWarningText(vicMarineWindWarningCancellationFixture)
	if hasActiveWarningForZone(cancelledBulletin, "East Gippsland Coast") {
		t.Fatalf("expected a cancellation-only section to not be an active warning, even for the named zone")
	}
}

func TestBomWarningDetailsURL_BuildsCorrectDeepLink(t *testing.T) {
	if got := bomWarningDetailsURL(bomMarineWindWarningSlug, "IDQ20085"); got != "https://www.bom.gov.au/warning/marine-wind-warning/IDQ20085" {
		t.Fatalf("expected marine wind warning URL, got %q", got)
	}
	if got := bomWarningDetailsURL(bomHazardousSurfWarningSlug, "IDQ28522"); got != "https://www.bom.gov.au/warning/hazardous-surf-warning/IDQ28522" {
		t.Fatalf("expected hazardous surf warning URL, got %q", got)
	}
}

// --- buildFetchWarningsOutput orchestration tests ---

// fakeFetcher builds an ftpFetcher backed by a map of path -> (body, error),
// and records every (host, path) it was called with.
type fakeFetcher struct {
	responses map[string]string
	errors    map[string]error
	calls     []string
}

func (f *fakeFetcher) fetch(host, path string) (string, error) {
	f.calls = append(f.calls, host+path)
	if err, ok := f.errors[path]; ok {
		return "", err
	}
	return f.responses[path], nil
}

func TestBuildFetchWarningsOutput_QLD_ReturnsOnlyActiveSectionsForResolvedZone(t *testing.T) {
	lat, lon := -22.4, 150.0 // resolves to QLD / Capricornia Coast

	f := &fakeFetcher{responses: map[string]string{
		"/anon/gen/fwo/IDQ20085.txt": qldMarineWindWarningFixture,
		"/anon/gen/fwo/IDQ28522.txt": qldHazardousSurfWarningFixture,
	}}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Region != "QLD — Capricornia Coast" {
		t.Fatalf("expected region %q, got %q", "QLD — Capricornia Coast", out.Region)
	}

	// The surf warning fixture only names K'gari/Sunshine/Gold Coast Waters,
	// not Capricornia Coast, so only the wind bulletin should survive.
	if len(out.Bulletins) != 1 {
		t.Fatalf("expected 1 bulletin (surf warning doesn't name Capricornia Coast), got %d: %+v", len(out.Bulletins), out.Bulletins)
	}

	wind := out.Bulletins[0]
	if wind.ID != "IDQ20085" || wind.Category != "wind" {
		t.Fatalf("unexpected wind bulletin: %+v", wind)
	}
	if wind.DetailsURL != "https://www.bom.gov.au/warning/marine-wind-warning/IDQ20085" {
		t.Fatalf("unexpected details url: %q", wind.DetailsURL)
	}
	if wind.IssuedAt == "" {
		t.Fatalf("expected a non-empty issued_at")
	}
	// Both Sunday and Monday sections name Capricornia Coast, so both survive.
	if len(wind.Sections) != 2 {
		t.Fatalf("expected 2 surviving sections for Capricornia Coast, got %d: %+v", len(wind.Sections), wind.Sections)
	}

	if len(f.calls) != 2 {
		t.Fatalf("expected both QLD product IDs to be fetched, got calls %+v", f.calls)
	}
}

func TestBuildFetchWarningsOutput_QLD_ZoneNotNamedInSurfWarningExcludesIt(t *testing.T) {
	// A QLD position whose zone (Gold Coast Waters) IS named in the surf
	// warning fixture - confirms the surf bulletin is included when its zone
	// matches, proving the exclusion in the test above is zone-driven, not a
	// bug that always drops surf bulletins.
	lat, lon := -27.5, 153.4 // Gold Coast Waters band

	f := &fakeFetcher{responses: map[string]string{
		"/anon/gen/fwo/IDQ20085.txt": qldMarineWindWarningFixture,
		"/anon/gen/fwo/IDQ28522.txt": qldHazardousSurfWarningFixture,
	}}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var gotSurf bool
	for _, b := range out.Bulletins {
		if b.ID == "IDQ28522" {
			gotSurf = true
			if b.Category != "surf" {
				t.Fatalf("expected surf category, got %q", b.Category)
			}
		}
	}
	if !gotSurf {
		t.Fatalf("expected surf bulletin for Gold Coast Waters, got %+v", out.Bulletins)
	}
}

func TestBuildFetchWarningsOutput_VICCancellationOnly_ReturnsZeroBulletins(t *testing.T) {
	lat, lon := -38.0, 149.0 // resolves to VIC / East Gippsland Coast

	f := &fakeFetcher{responses: map[string]string{
		"/anon/gen/fwo/IDV20600.txt": vicMarineWindWarningCancellationFixture,
	}}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Bulletins) != 0 {
		t.Fatalf("expected zero bulletins for a cancellation-only product, got %+v", out.Bulletins)
	}
}

func TestBuildFetchWarningsOutput_TAS_UnresolvedZoneAlwaysReturnsZeroBulletins(t *testing.T) {
	lat, lon := -42.0, 146.0 // resolves to TAS, zone always unresolved

	f := &fakeFetcher{responses: map[string]string{
		"/anon/gen/fwo/IDT20100.txt": tasMarineWindWarningFixture,
	}}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Region != "TAS" {
		t.Fatalf("expected region to fall back to bare state code %q, got %q", "TAS", out.Region)
	}
	if len(out.Bulletins) != 0 {
		t.Fatalf("expected zero bulletins for TAS (zone table is an unresolved stub), got %+v", out.Bulletins)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected the TAS wind product to still be fetched, got calls %+v", f.calls)
	}
}

func TestBuildFetchWarningsOutput_UnmappedPosition_ReturnsEmptyWithoutFetching(t *testing.T) {
	f := &fakeFetcher{}

	out, err := buildFetchWarningsOutput(10.0, -150.0, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Region != "" || len(out.Bulletins) != 0 {
		t.Fatalf("expected an empty result for an unmapped position, got %+v", out)
	}
	if len(f.calls) != 0 {
		t.Fatalf("expected no fetches for an unmapped position, got calls %+v", f.calls)
	}
}

func TestBuildFetchWarningsOutput_SAHasNoProductsSoNoFetchIsAttempted(t *testing.T) {
	f := &fakeFetcher{}

	out, err := buildFetchWarningsOutput(-34.0, 137.0, f.fetch) // SA
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Region != "SA" {
		t.Fatalf("expected region %q, got %q", "SA", out.Region)
	}
	if len(out.Bulletins) != 0 {
		t.Fatalf("expected zero bulletins for SA (no registered product IDs), got %+v", out.Bulletins)
	}
	if len(f.calls) != 0 {
		t.Fatalf("expected no fetches for SA, got calls %+v", f.calls)
	}
}

func TestBuildFetchWarningsOutput_OneProductFailsButOtherSucceeds_NoError(t *testing.T) {
	lat, lon := -22.4, 150.0 // QLD / Capricornia Coast

	f := &fakeFetcher{
		responses: map[string]string{
			"/anon/gen/fwo/IDQ20085.txt": qldMarineWindWarningFixture,
		},
		errors: map[string]error{
			"/anon/gen/fwo/IDQ28522.txt": errors.New("550 no such file"),
		},
	}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("expected no error when only one of two applicable products fails, got %v", err)
	}
	if len(out.Bulletins) != 1 || out.Bulletins[0].ID != "IDQ20085" {
		t.Fatalf("expected only the successfully-fetched wind bulletin, got %+v", out.Bulletins)
	}
}

func TestBuildFetchWarningsOutput_AllApplicableProductsFail_ReturnsError(t *testing.T) {
	lat, lon := -22.4, 150.0 // QLD / Capricornia Coast

	f := &fakeFetcher{errors: map[string]error{
		"/anon/gen/fwo/IDQ20085.txt": errors.New("dial tcp: timeout"),
		"/anon/gen/fwo/IDQ28522.txt": errors.New("dial tcp: timeout"),
	}}

	_, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err == nil {
		t.Fatalf("expected an error when every applicable product's fetch fails")
	}
}

func TestBuildFetchWarningsOutput_UsesCorrectFTPHostAndPath(t *testing.T) {
	lat, lon := -22.4, 150.0 // QLD

	f := &fakeFetcher{responses: map[string]string{
		"/anon/gen/fwo/IDQ20085.txt": qldMarineWindWarningFixture,
		"/anon/gen/fwo/IDQ28522.txt": qldHazardousSurfWarningFixture,
	}}

	if _, err := buildFetchWarningsOutput(lat, lon, f.fetch); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		bomFTPHost + "/anon/gen/fwo/IDQ20085.txt": true,
		bomFTPHost + "/anon/gen/fwo/IDQ28522.txt": true,
	}
	for _, call := range f.calls {
		if !want[call] {
			t.Errorf("unexpected fetch call %q", call)
		}
		delete(want, call)
	}
	if len(want) != 0 {
		t.Errorf("missing expected fetch calls: %+v", want)
	}
}
