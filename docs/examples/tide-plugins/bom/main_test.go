package main

import (
	"os"
	"testing"
	"time"
)

func TestParseBomTidesTable(t *testing.T) {
	html, err := os.ReadFile("testdata/bom_tides_table_sample.html")
	if err != nil {
		t.Fatalf("failed to read fixture: %v", err)
	}

	extremes, err := parseBomTidesTable(string(html))
	if err != nil {
		t.Fatalf("parseBomTidesTable returned error: %v", err)
	}

	if len(extremes) != 31 {
		t.Fatalf("expected 31 extremes, got %d", len(extremes))
	}

	first := extremes[0]
	wantTime := "2026-06-15T17:12:00Z"
	if first.Time != wantTime {
		t.Errorf("expected first extreme time %v, got %v", wantTime, first.Time)
	}
	if first.High {
		t.Errorf("expected first extreme to be a low tide")
	}
	if first.HeightM != 0.22 {
		t.Errorf("expected first extreme height 0.22, got %v", first.HeightM)
	}

	for i := 1; i < len(extremes); i++ {
		ti, err := time.Parse(time.RFC3339, extremes[i].Time)
		if err != nil {
			t.Fatalf("failed to parse extreme time at index %d: %v", i, err)
		}
		tprev, err := time.Parse(time.RFC3339, extremes[i-1].Time)
		if err != nil {
			t.Fatalf("failed to parse extreme time at index %d: %v", i-1, err)
		}
		if ti.Before(tprev) {
			t.Errorf("extremes not sorted by time at index %d", i)
		}
	}
}

func TestParseBomTidesTable_ErrorsOnMismatchedTimeAndHeightCounts(t *testing.T) {
	// Only a time match, no matching height match - the native version
	// treats mismatched counts as an unexpected/broken page format and
	// errors rather than silently truncating or zero-filling.
	broken := `<td data-time-utc="2026-06-15T17:12:00Z" class="localtime low-tide">5:12 pm</td>`
	if _, err := parseBomTidesTable(broken); err == nil {
		t.Fatalf("expected an error when time/height match counts differ")
	}
}

func TestParseBomStations_FiltersToTideTypeStationsWithAAC(t *testing.T) {
	raw, err := os.ReadFile("data/bom_tide_sites.json")
	if err != nil {
		t.Fatalf("failed to read embedded station data fixture: %v", err)
	}

	stations, err := parseBomStations(raw)
	if err != nil {
		t.Fatalf("parseBomStations returned error: %v", err)
	}

	if len(stations) == 0 {
		t.Fatalf("expected at least one station in the embedded BOM site list")
	}
	for _, s := range stations {
		if s.StationID == "" {
			t.Errorf("expected every returned station to have a non-empty station id (AAC code)")
		}
	}
}

func TestSearchBomStations_EmptyQueryReturnsFullCatalogUpToLimit(t *testing.T) {
	stations := []stationOut{
		{StationID: "A1", Name: "Alpha Bay", State: "QLD"},
		{StationID: "B2", Name: "Bravo Point", State: "NSW"},
		{StationID: "C3", Name: "Charlie Inlet", State: "VIC"},
	}

	got := searchBomStations(stations, "", 2)
	if len(got) != 2 {
		t.Fatalf("expected empty query to return up to limit (2) unfiltered stations, got %d", len(got))
	}
}

func TestSearchBomStations_FiltersCaseInsensitivelyOnNameStationIDAndState(t *testing.T) {
	stations := []stationOut{
		{StationID: "A1", Name: "Alpha Bay", State: "QLD"},
		{StationID: "B2", Name: "Bravo Point", State: "NSW"},
	}

	byName := searchBomStations(stations, "alpha", 20)
	if len(byName) != 1 || byName[0].StationID != "A1" {
		t.Fatalf("expected name substring match to find Alpha Bay, got %+v", byName)
	}

	byState := searchBomStations(stations, "nsw", 20)
	if len(byState) != 1 || byState[0].StationID != "B2" {
		t.Fatalf("expected state substring match to find B2, got %+v", byState)
	}
}
