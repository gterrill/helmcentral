package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

func newTestNearbyContactStore(t *testing.T) *nearbyContactStore {
	t.Helper()
	dir := t.TempDir()
	store, err := newNearbyContactStore(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("newNearbyContactStore: %v", err)
	}
	t.Cleanup(func() { _ = store.close() })
	return store
}

func TestRecordContactIfNew_SameVesselWithinSessionGapInsertsOnlyOneRow(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base); err != nil {
		t.Fatalf("recordContactIfNew (1st tick): %v", err)
	}
	// Second tick 5 seconds later, well within the 30-minute session gap.
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(5*time.Second)); err != nil {
		t.Fatalf("recordContactIfNew (2nd tick): %v", err)
	}
	// Third tick 20 minutes later, still within the gap.
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base.Add(20*time.Minute)); err != nil {
		t.Fatalf("recordContactIfNew (3rd tick): %v", err)
	}

	seenCount, _, err := store.summary("316042555")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 1 {
		t.Fatalf("expected 1 row for a single encounter, got %d", seenCount)
	}
}

func TestRecordContactIfNew_AfterGapElapsedInsertsSecondRow(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 12, 0, 0, 0, time.UTC)

	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", base); err != nil {
		t.Fatalf("recordContactIfNew (encounter 1): %v", err)
	}
	// Gap of 31 minutes (> the 30-minute contactSessionGap) is a new encounter.
	second := base.Add(31 * time.Minute)
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.60, 149.80, "Airlie Beach", "motoring", second); err != nil {
		t.Fatalf("recordContactIfNew (encounter 2): %v", err)
	}

	seenCount, lastSeenAt, err := store.summary("316042555")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 2 {
		t.Fatalf("expected 2 rows for two distinct encounters, got %d", seenCount)
	}
	if !lastSeenAt.Equal(second) {
		t.Fatalf("expected last seen at %s, got %s", second, lastSeenAt)
	}
}

func TestSummary_ReturnsCorrectCountAndLastSeen(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)

	times := []time.Time{
		base,
		base.Add(1 * time.Hour),
		base.Add(2 * time.Hour),
	}
	for _, ts := range times {
		if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", ts); err != nil {
			t.Fatalf("recordContactIfNew: %v", err)
		}
	}

	seenCount, lastSeenAt, err := store.summary("316042555")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 3 {
		t.Fatalf("expected seenCount 3, got %d", seenCount)
	}
	want := times[len(times)-1]
	if !lastSeenAt.Equal(want) {
		t.Fatalf("expected lastSeenAt %s, got %s", want, lastSeenAt)
	}
}

func TestSummary_UnknownVesselReturnsZeroValue(t *testing.T) {
	store := newTestNearbyContactStore(t)

	seenCount, lastSeenAt, err := store.summary("no-such-vessel")
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if seenCount != 0 {
		t.Fatalf("expected seenCount 0 for unknown vessel, got %d", seenCount)
	}
	if !lastSeenAt.IsZero() {
		t.Fatalf("expected zero lastSeenAt for unknown vessel, got %s", lastSeenAt)
	}
}

func TestListSightings_ReturnsNewestFirst(t *testing.T) {
	store := newTestNearbyContactStore(t)
	base := time.Date(2026, time.July, 12, 8, 0, 0, 0, time.UTC)

	first := base
	second := base.Add(1 * time.Hour)
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", first); err != nil {
		t.Fatalf("recordContactIfNew (1st): %v", err)
	}
	if err := store.recordContactIfNew("316042555", "TAKU X", -21.61, 149.81, "Nara Inlet", "motoring", second.Add(31*time.Minute+time.Hour)); err != nil {
		t.Fatalf("recordContactIfNew (2nd): %v", err)
	}

	sightings, err := store.listSightings("316042555")
	if err != nil {
		t.Fatalf("listSightings: %v", err)
	}
	if len(sightings) != 2 {
		t.Fatalf("expected 2 sightings, got %d", len(sightings))
	}
	if !sightings[0].SeenAt.After(sightings[1].SeenAt) {
		t.Fatalf("expected newest-first ordering, got %s then %s", sightings[0].SeenAt, sightings[1].SeenAt)
	}
	if sightings[0].Geoname != "Nara Inlet" || sightings[0].NavContext != "motoring" {
		t.Fatalf("expected newest sighting to be the Nara Inlet/motoring one, got %+v", sightings[0])
	}
	if sightings[1].Geoname != "Airlie Beach" || sightings[1].NavContext != "anchored" {
		t.Fatalf("expected oldest sighting to be the Airlie Beach/anchored one, got %+v", sightings[1])
	}
}

// TestGetNearbyVesselSightingsHandler_DecodesURLEncodedKeyParam exercises the
// real Echo router (not manual c.SetParamValues, which bypasses URL
// decoding entirely) with a request path built the same way the frontend
// builds it: encodeURIComponent("name:TAKU X") -> "name%3ATAKU%20X". Echo's
// router matches on the request's escaped path and does NOT url-decode
// route params for you - c.Param("key") returns the raw
// "name%3ATAKU%20X" unless the handler decodes it itself, which would never
// match any vessel_key stored in the database (those are stored decoded,
// e.g. by the poller in tracks.go). This is a regression test for that.
func TestGetNearbyVesselSightingsHandler_DecodesURLEncodedKeyParam(t *testing.T) {
	store := newTestNearbyContactStore(t)
	seenAt := time.Date(2026, time.July, 10, 9, 14, 0, 0, time.UTC)
	if err := store.recordContactIfNew("name:TAKU X", "TAKU X", -21.59, 149.79, "Airlie Beach", "anchored", seenAt); err != nil {
		t.Fatalf("recordContactIfNew: %v", err)
	}

	e := echo.New()
	e.GET("/api/nearby-vessels/:key/sightings", getNearbyVesselSightingsHandler(store))

	req := httptest.NewRequest(http.MethodGet, "/api/nearby-vessels/name%3ATAKU%20X/sightings", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Sightings []nearbyVesselSightingWire `json:"sightings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(resp.Sightings) != 1 {
		t.Fatalf("expected 1 sighting for the URL-encoded key to resolve to the stored vessel_key, got %d: %s", len(resp.Sightings), rec.Body.String())
	}
	if resp.Sightings[0].Geoname != "Airlie Beach" {
		t.Fatalf("expected the Airlie Beach sighting, got %+v", resp.Sightings[0])
	}
}

func TestVesselContactKey_PrefersMMSIOverName(t *testing.T) {
	if got := vesselContactKey("316042555", "Taku X"); got != "316042555" {
		t.Fatalf("expected MMSI key, got %q", got)
	}
}

func TestVesselContactKey_FallsBackToNormalizedNameWhenMMSIEmpty(t *testing.T) {
	if got := vesselContactKey("", "Taku X"); got != "name:TAKU X" {
		t.Fatalf("expected name-based fallback key, got %q", got)
	}
}
