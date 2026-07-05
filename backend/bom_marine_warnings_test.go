package main

import "testing"

// Fixtures below were captured live from the BOM anonymous FTP mirror
// (ftp://ftp.bom.gov.au/anon/gen/fwo/<PRODUCT_ID>.txt) during planning.
// They are used verbatim, not paraphrased.

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
  - Rock fishers should avoid coastal rock platforms exposed to the ocean and seek
    a safe location that is sheltered from the surf.
  - Boaters planning to cross shallow water and ocean bars should consider changing
    or delaying their voyage.
  - Boaters already on the water should carry the appropriate safety equipment and
    wear a lifejacket.
  - Boaters should remember to log on with their local radio base and consider
    their safety management plan.

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

func TestParseBomMarineWarningText_QLDMarineWindWarning_ParsesTitleAndIssuedAt(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)

	if bulletin.Title != "Marine Wind Warning Summary for Queensland" {
		t.Fatalf("expected title %q, got %q", "Marine Wind Warning Summary for Queensland", bulletin.Title)
	}
	if bulletin.IssuedAtRaw != "11:51 am EST on Sunday 5 July 2026" {
		t.Fatalf("expected raw issued-at %q, got %q", "11:51 am EST on Sunday 5 July 2026", bulletin.IssuedAtRaw)
	}
}

func TestParseBomMarineWarningText_QLDMarineWindWarning_ParsesDaySections(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)

	if len(bulletin.Sections) != 2 {
		t.Fatalf("expected 2 day-sections, got %d: %+v", len(bulletin.Sections), bulletin.Sections)
	}

	sunday := bulletin.Sections[0]
	if sunday.Day != "Sunday 5 July" {
		t.Fatalf("expected day %q, got %q", "Sunday 5 July", sunday.Day)
	}
	if sunday.WarningType != "Strong Wind Warning" {
		t.Fatalf("expected warning type %q, got %q", "Strong Wind Warning", sunday.WarningType)
	}
	if !containsZone(sunday.Zones, "Capricornia Coast") {
		t.Fatalf("expected Capricornia Coast in Sunday zones, got %+v", sunday.Zones)
	}
	if len(sunday.Zones) != 1 {
		t.Fatalf("expected exactly 1 zone for Sunday, got %+v", sunday.Zones)
	}

	monday := bulletin.Sections[1]
	if monday.Day != "Monday 6 July" {
		t.Fatalf("expected day %q, got %q", "Monday 6 July", monday.Day)
	}
	if monday.WarningType != "Strong Wind Warning" {
		t.Fatalf("expected warning type %q, got %q", "Strong Wind Warning", monday.WarningType)
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
		if !containsZone(monday.Zones, zone) {
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
	if section.Day != "Monday 6 July" {
		t.Fatalf("expected day %q, got %q", "Monday 6 July", section.Day)
	}
	if section.WarningType != "Hazardous Surf Warning" {
		t.Fatalf("expected warning type %q, got %q", "Hazardous Surf Warning", section.WarningType)
	}

	expectedZones := []string{"K'gari Coast", "Sunshine Coast Waters", "Gold Coast Waters"}
	if len(section.Zones) != len(expectedZones) {
		t.Fatalf("expected %d zones, got %d: %+v", len(expectedZones), len(section.Zones), section.Zones)
	}
	for _, zone := range expectedZones {
		if !containsZone(section.Zones, zone) {
			t.Fatalf("expected zone %q, got %+v", zone, section.Zones)
		}
	}

	if !hasActiveWarning(bulletin) {
		t.Fatalf("expected hazardous surf warning bulletin to have an active warning")
	}
}

func TestParseBomMarineWarningText_VICCancellation_ParsesButIsNotActive(t *testing.T) {
	bulletin := parseBomMarineWarningText(vicMarineWindWarningCancellationFixture)

	if bulletin.Title != "Marine Wind Warning Summary for Victoria" {
		t.Fatalf("expected title %q, got %q", "Marine Wind Warning Summary for Victoria", bulletin.Title)
	}

	if len(bulletin.Sections) != 1 {
		t.Fatalf("expected 1 day-section, got %d: %+v", len(bulletin.Sections), bulletin.Sections)
	}

	section := bulletin.Sections[0]
	if section.Day != "Sunday 5 July" {
		t.Fatalf("expected day %q, got %q", "Sunday 5 July", section.Day)
	}
	if section.WarningType != "Cancellation" {
		t.Fatalf("expected warning type %q, got %q", "Cancellation", section.WarningType)
	}
	if !containsZone(section.Zones, "East Gippsland Coast") {
		t.Fatalf("expected East Gippsland Coast zone, got %+v", section.Zones)
	}

	if hasActiveWarning(bulletin) {
		t.Fatalf("expected VIC cancellation-only bulletin to NOT be an active warning")
	}
}

func TestHasActiveWarning_EmptyBulletinIsNotActive(t *testing.T) {
	if hasActiveWarning(bomMarineWarningBulletin{}) {
		t.Fatalf("expected empty bulletin to not be an active warning")
	}
}

func TestHasActiveWarningForZone_OnlyTrueForNamedZone(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)

	// Capricornia Coast is named in an active section - the vessel's zone.
	if !hasActiveWarningForZone(bulletin, "Capricornia Coast") {
		t.Fatalf("expected an active warning for Capricornia Coast")
	}

	// A QLD zone not named anywhere in this bulletin must not alert - a
	// warning for the east coast doesn't mean anything for a vessel in the
	// Gulf of Carpentaria, on the other side of the state.
	if hasActiveWarningForZone(bulletin, "Gulf Waters") {
		t.Fatalf("expected no active warning for Gulf Waters (not named in this bulletin)")
	}

	// No matched zone (e.g. TAS/WA today) must never alert.
	if hasActiveWarningForZone(bulletin, "") {
		t.Fatalf("expected no active warning when zone is empty")
	}
}

func TestHasActiveWarningForZone_CancellationDoesNotCountAsActive(t *testing.T) {
	bulletin := parseBomMarineWarningText(vicMarineWindWarningCancellationFixture)

	if hasActiveWarningForZone(bulletin, "East Gippsland Coast") {
		t.Fatalf("expected a cancellation-only section to not be an active warning, even for the named zone")
	}
}

func TestParseBomMarineWarningText_QLDMarineWindWarning_DetailsURLIsEmpty(t *testing.T) {
	bulletin := parseBomMarineWarningText(qldMarineWindWarningFixture)
	if bulletin.DetailsURL != "" {
		t.Fatalf("expected bulletin details URL to be empty (set by caller, not parser), got %q", bulletin.DetailsURL)
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

func containsZone(zones []string, target string) bool {
	for _, z := range zones {
		if z == target {
			return true
		}
	}
	return false
}
