package main

import (
	"errors"
	"testing"
)

// --- Fixtures below mirror the real api.weather.gov response shapes
// confirmed live during planning of this plugin (see
// /Users/gavinator/.claude/plans/i-want-to-create-wild-fountain.md's
// "Confirmed live API shapes" section) - hand-built realistic JSON, not
// paraphrased prose, since the field names/nesting are what this plugin's
// parsing logic depends on exactly.

const gmz554ZoneFixture = `{
  "type": "FeatureCollection",
  "features": [
    {
      "type": "Feature",
      "properties": {
        "id": "GMZ554",
        "name": "Coastal Waters from Boothville LA to Southwest Pass of the Mississippi River out 20 nm"
      }
    }
  ]
}`

const emptyZoneFixture = `{
  "type": "FeatureCollection",
  "features": []
}`

const gmz554ActiveAlertsFixture = `{
  "type": "FeatureCollection",
  "features": [
    {
      "properties": {
        "id": "urn:oid:2.49.0.1.840.0.abc123.1.1",
        "@id": "https://api.weather.gov/alerts/urn:oid:2.49.0.1.840.0.abc123.1.1",
        "event": "Small Craft Advisory",
        "headline": "Small Craft Advisory issued July 19 at 5:00AM CDT until July 20 at 7:00AM CDT",
        "severity": "Moderate",
        "certainty": "Likely",
        "urgency": "Expected",
        "sent": "2026-07-19T05:00:00-05:00",
        "effective": "2026-07-19T05:00:00-05:00",
        "onset": "2026-07-19T05:00:00-05:00",
        "expires": "2026-07-19T12:00:00-05:00",
        "ends": "2026-07-20T07:00:00-05:00",
        "status": "Actual",
        "messageType": "Alert",
        "category": "Met",
        "description": "A Small Craft Advisory means winds or seas are expected to create hazardous conditions."
      }
    },
    {
      "properties": {
        "id": "urn:oid:2.49.0.1.840.0.def456.1.1",
        "@id": "https://api.weather.gov/alerts/urn:oid:2.49.0.1.840.0.def456.1.1",
        "event": "Gale Warning",
        "headline": "Gale Warning cancelled",
        "sent": "2026-07-19T06:00:00-05:00",
        "status": "Actual",
        "messageType": "Cancel"
      }
    },
    {
      "properties": {
        "id": "urn:oid:2.49.0.1.840.0.ghi789.1.1",
        "@id": "https://api.weather.gov/alerts/urn:oid:2.49.0.1.840.0.ghi789.1.1",
        "event": "Storm Warning",
        "headline": "TEST Storm Warning",
        "sent": "2026-07-19T07:00:00-05:00",
        "status": "Test",
        "messageType": "Alert"
      }
    },
    {
      "properties": {
        "id": "urn:oid:2.49.0.1.840.0.jkl012.1.1",
        "event": "Rip Current Statement",
        "headline": "",
        "sent": "2026-07-19T08:00:00-05:00",
        "status": "Actual",
        "messageType": "Alert"
      }
    }
  ]
}`

// --- categorizeNWSEvent ---

func TestCategorizeNWSEvent_MappingTable(t *testing.T) {
	cases := map[string]string{
		"Small Craft Advisory":         "wind",
		"Gale Warning":                 "wind",
		"Storm Warning":                "wind",
		"Hurricane Force Wind Warning": "wind",
		"High Surf Advisory":           "surf",
		"Rip Current Statement":        "surf",
		// No "wind"/"gale"/"storm"/"craft"/"surf"/"swell"/"rip" substring in
		// any of these, so they fall back to a slug of the event name.
		"Coastal Flood Statement":      "coastal-flood-statement",
		"Special Marine Warning":       "special-marine-warning",
		"Marine Weather Statement":     "marine-weather-statement",
		"Heavy Freezing Spray Warning": "heavy-freezing-spray-warning",
	}

	for event, want := range cases {
		got := categorizeNWSEvent(event)
		if got != want {
			t.Errorf("categorizeNWSEvent(%q) = %q, want %q", event, got, want)
		}
	}
}

func TestCategorizeNWSEvent_NeverEmptyForNonEmptyInput(t *testing.T) {
	if got := categorizeNWSEvent("Some Unheard-Of Alert Type"); got == "" {
		t.Fatalf("expected a non-empty fallback category, got empty string")
	}
}

// --- isReportableAlert ---

func TestIsReportableAlert(t *testing.T) {
	cases := []struct {
		name string
		p    nwsAlertProperties
		want bool
	}{
		{"actual and alert", nwsAlertProperties{Status: "Actual", MessageType: "Alert"}, true},
		{"actual and update", nwsAlertProperties{Status: "Actual", MessageType: "Update"}, true},
		{"actual but cancelled", nwsAlertProperties{Status: "Actual", MessageType: "Cancel"}, false},
		{"test status", nwsAlertProperties{Status: "Test", MessageType: "Alert"}, false},
		{"exercise status", nwsAlertProperties{Status: "Exercise", MessageType: "Alert"}, false},
	}
	for _, c := range cases {
		if got := isReportableAlert(c.p); got != c.want {
			t.Errorf("%s: isReportableAlert() = %v, want %v", c.name, got, c.want)
		}
	}
}

// --- alertToBulletin ---

func TestAlertToBulletin_UsesHeadlineWhenPresent(t *testing.T) {
	p := nwsAlertProperties{
		ID:          "urn:oid:1",
		CanonicalID: "https://api.weather.gov/alerts/urn:oid:1",
		Event:       "Small Craft Advisory",
		Headline:    "Small Craft Advisory issued July 19",
		Sent:        "2026-07-19T05:00:00-05:00",
	}
	b := alertToBulletin(p, "GMZ554")
	if b.Title != "Small Craft Advisory issued July 19" {
		t.Errorf("Title = %q, want headline", b.Title)
	}
	if b.ID != "urn:oid:1" {
		t.Errorf("ID = %q, want %q", b.ID, "urn:oid:1")
	}
	if b.Category != "wind" {
		t.Errorf("Category = %q, want %q", b.Category, "wind")
	}
	if b.IssuedAt != "2026-07-19T05:00:00-05:00" {
		t.Errorf("IssuedAt = %q, want passthrough of Sent", b.IssuedAt)
	}
	if b.DetailsURL != "https://api.weather.gov/alerts/urn:oid:1" {
		t.Errorf("DetailsURL = %q, want the alert's @id", b.DetailsURL)
	}
	if len(b.Sections) != 1 || b.Sections[0].Day != "" || b.Sections[0].WarningType != "Small Craft Advisory" {
		t.Errorf("Sections = %+v, want exactly one section with empty Day and WarningType=event", b.Sections)
	}
}

func TestAlertToBulletin_FallsBackToEventWhenHeadlineEmpty(t *testing.T) {
	p := nwsAlertProperties{ID: "urn:oid:2", Event: "Rip Current Statement", Headline: ""}
	b := alertToBulletin(p, "GMZ554")
	if b.Title != "Rip Current Statement" {
		t.Errorf("Title = %q, want event as fallback", b.Title)
	}
}

func TestAlertToBulletin_FallsBackToSearchURLWhenCanonicalIDEmpty(t *testing.T) {
	p := nwsAlertProperties{ID: "urn:oid:3", Event: "Gale Warning", CanonicalID: ""}
	b := alertToBulletin(p, "GMZ554")
	want := "https://alerts.weather.gov/search?zone=GMZ554"
	if b.DetailsURL != want {
		t.Errorf("DetailsURL = %q, want %q", b.DetailsURL, want)
	}
}

// --- mapZonesResponse ---

func TestMapZonesResponse_ReturnsFirstMatchingZone(t *testing.T) {
	zone, ok, err := mapZonesResponse([]byte(gmz554ZoneFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true for a non-empty features array")
	}
	if zone.ID != "GMZ554" {
		t.Errorf("zone.ID = %q, want %q", zone.ID, "GMZ554")
	}
	if zone.Name != "Coastal Waters from Boothville LA to Southwest Pass of the Mississippi River out 20 nm" {
		t.Errorf("unexpected zone.Name: %q", zone.Name)
	}
}

func TestMapZonesResponse_EmptyFeaturesIsNotAnError(t *testing.T) {
	zone, ok, err := mapZonesResponse([]byte(emptyZoneFixture))
	if err != nil {
		t.Fatalf("expected no error for zero-coverage point, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for zero features, got zone %+v", zone)
	}
}

func TestMapZonesResponse_MalformedJSONIsAnError(t *testing.T) {
	_, _, err := mapZonesResponse([]byte("not json"))
	if err == nil {
		t.Fatalf("expected an error for malformed JSON")
	}
}

// --- mapAlertsResponse ---

func TestMapAlertsResponse_FiltersCancelledAndNonActualAlerts(t *testing.T) {
	bulletins, err := mapAlertsResponse([]byte(gmz554ActiveAlertsFixture), "GMZ554")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Fixture has 4 alerts: Actual/Alert (keep), Actual/Cancel (drop),
	// Test/Alert (drop), Actual/Alert with empty headline (keep).
	if len(bulletins) != 2 {
		t.Fatalf("expected 2 surviving bulletins, got %d: %+v", len(bulletins), bulletins)
	}
	if bulletins[0].ID != "urn:oid:2.49.0.1.840.0.abc123.1.1" {
		t.Errorf("bulletins[0].ID = %q, want the Small Craft Advisory alert", bulletins[0].ID)
	}
	if bulletins[0].Category != "wind" {
		t.Errorf("bulletins[0].Category = %q, want %q", bulletins[0].Category, "wind")
	}
	if bulletins[1].ID != "urn:oid:2.49.0.1.840.0.jkl012.1.1" {
		t.Errorf("bulletins[1].ID = %q, want the Rip Current Statement alert", bulletins[1].ID)
	}
	if bulletins[1].Title != "Rip Current Statement" {
		t.Errorf("bulletins[1].Title = %q, want event fallback since headline is empty", bulletins[1].Title)
	}
	if bulletins[1].DetailsURL != "https://alerts.weather.gov/search?zone=GMZ554" {
		t.Errorf("bulletins[1].DetailsURL = %q, want the zone search fallback since @id is absent", bulletins[1].DetailsURL)
	}
}

func TestMapAlertsResponse_MalformedJSONIsAnError(t *testing.T) {
	_, err := mapAlertsResponse([]byte("not json"), "GMZ554")
	if err == nil {
		t.Fatalf("expected an error for malformed JSON")
	}
}

// --- buildFetchWarningsOutput (fetch orchestration) ---

type fakeResponse struct {
	status int
	body   string
}

type fakeFetcher struct {
	responses map[string]fakeResponse
	errors    map[string]error
	calls     []string
}

func (f *fakeFetcher) fetch(url string) (int, []byte, error) {
	f.calls = append(f.calls, url)
	if err, ok := f.errors[url]; ok {
		return 0, nil, err
	}
	if resp, ok := f.responses[url]; ok {
		return resp.status, []byte(resp.body), nil
	}
	return 0, nil, errors.New("fakeFetcher: no response configured for " + url)
}

func TestBuildFetchWarningsOutput_ResolvesZoneThenFetchesAlerts(t *testing.T) {
	lat, lon := 28.9, -89.4 // Gulf of Mexico, inside GMZ554 in the fixture

	f := &fakeFetcher{responses: map[string]fakeResponse{
		"https://api.weather.gov/zones?type=marine&point=28.9000,-89.4000": {200, gmz554ZoneFixture},
		"https://api.weather.gov/alerts/active?zone=GMZ554":                {200, gmz554ActiveAlertsFixture},
	}}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Region != "Coastal Waters from Boothville LA to Southwest Pass of the Mississippi River out 20 nm" {
		t.Errorf("unexpected Region: %q", out.Region)
	}
	if len(out.Bulletins) != 2 {
		t.Fatalf("expected 2 bulletins, got %d: %+v", len(out.Bulletins), out.Bulletins)
	}
	if len(f.calls) != 2 {
		t.Fatalf("expected exactly 2 fetch calls (zone lookup + alerts), got %d: %+v", len(f.calls), f.calls)
	}
}

func TestBuildFetchWarningsOutput_ZeroZonesReturnsEmptyWithoutFetchingAlerts(t *testing.T) {
	lat, lon := 40.0, -100.0 // landlocked, no marine zone

	f := &fakeFetcher{responses: map[string]fakeResponse{
		"https://api.weather.gov/zones?type=marine&point=40.0000,-100.0000": {200, emptyZoneFixture},
	}}

	out, err := buildFetchWarningsOutput(lat, lon, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Region != "" || len(out.Bulletins) != 0 {
		t.Fatalf("expected an empty result for a point outside NWS marine zone coverage, got %+v", out)
	}
	if len(f.calls) != 1 {
		t.Fatalf("expected only the zone lookup to be called (no alerts fetch), got calls %+v", f.calls)
	}
}

func TestBuildFetchWarningsOutput_ZoneLookupNetworkErrorIsAnError(t *testing.T) {
	f := &fakeFetcher{errors: map[string]error{
		"https://api.weather.gov/zones?type=marine&point=28.9000,-89.4000": errors.New("dial tcp: timeout"),
	}}

	_, err := buildFetchWarningsOutput(28.9, -89.4, f.fetch)
	if err == nil {
		t.Fatalf("expected an error when the zone lookup transport fails")
	}
}

func TestBuildFetchWarningsOutput_ZoneLookupNon2xxIsAnError(t *testing.T) {
	f := &fakeFetcher{responses: map[string]fakeResponse{
		"https://api.weather.gov/zones?type=marine&point=28.9000,-89.4000": {500, "internal server error"},
	}}

	_, err := buildFetchWarningsOutput(28.9, -89.4, f.fetch)
	if err == nil {
		t.Fatalf("expected an error for a non-2xx zone lookup response")
	}
}

func TestBuildFetchWarningsOutput_AlertsLookupNetworkErrorIsAnError(t *testing.T) {
	f := &fakeFetcher{
		responses: map[string]fakeResponse{
			"https://api.weather.gov/zones?type=marine&point=28.9000,-89.4000": {200, gmz554ZoneFixture},
		},
		errors: map[string]error{
			"https://api.weather.gov/alerts/active?zone=GMZ554": errors.New("dial tcp: timeout"),
		},
	}

	_, err := buildFetchWarningsOutput(28.9, -89.4, f.fetch)
	if err == nil {
		t.Fatalf("expected an error when the alerts lookup transport fails")
	}
}

func TestBuildFetchWarningsOutput_AlertsLookupNon2xxIsAnError(t *testing.T) {
	f := &fakeFetcher{responses: map[string]fakeResponse{
		"https://api.weather.gov/zones?type=marine&point=28.9000,-89.4000": {200, gmz554ZoneFixture},
		"https://api.weather.gov/alerts/active?zone=GMZ554":                {503, "service unavailable"},
	}}

	_, err := buildFetchWarningsOutput(28.9, -89.4, f.fetch)
	if err == nil {
		t.Fatalf("expected an error for a non-2xx alerts lookup response")
	}
}

func TestBuildFetchWarningsOutput_NoActiveAlertsReturnsZeroBulletinsNoError(t *testing.T) {
	f := &fakeFetcher{responses: map[string]fakeResponse{
		"https://api.weather.gov/zones?type=marine&point=28.9000,-89.4000": {200, gmz554ZoneFixture},
		"https://api.weather.gov/alerts/active?zone=GMZ554":                {200, `{"type":"FeatureCollection","features":[]}`},
	}}

	out, err := buildFetchWarningsOutput(28.9, -89.4, f.fetch)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Bulletins) != 0 {
		t.Fatalf("expected zero bulletins when there are no active alerts, got %+v", out.Bulletins)
	}
	if out.Region == "" {
		t.Fatalf("expected Region to still be populated from the resolved zone")
	}
}
