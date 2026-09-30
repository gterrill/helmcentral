package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// Vessel particulars are the facts about the boat SignalK does not publish.
// One row, read whole and replaced whole.

func TestVesselParticulars_EmptyBeforeAnythingIsSet(t *testing.T) {
	store := newTestDocumentStore(t)
	got, err := store.GetVesselParticulars()
	if err != nil {
		t.Fatalf("GetVesselParticulars: %v", err)
	}
	if got.Builder != "" || got.Year != nil || got.DisplacementKG != nil || got.UpdatedAt != nil {
		t.Fatalf("expected the empty record, got %+v", got)
	}
}

func TestVesselParticulars_RoundTripsAndReplacesWhole(t *testing.T) {
	store := newTestDocumentStore(t)
	year := 2024
	kg := 24500.0
	saved, err := store.SetVesselParticulars(vesselParticulars{
		Builder: " Granocean ", Model: "W-60", Year: &year, HIN: "XXTST00001A000", Flag: "Cook Islands",
		HailingPort: "Sydney", HullType: "Catamaran", HullMaterial: "Fiberglass", DisplacementKG: &kg,
		ShorePower: "Dual 30/50 A", SystemVoltage: "24v", Registration: "R1", IMO: "1234567",
		EPIRBID: "ABC", DateAcquired: "2025-01-27",
	})
	if err != nil {
		t.Fatalf("SetVesselParticulars: %v", err)
	}
	if saved.Builder != "Granocean" {
		t.Fatalf("text fields are trimmed, got %q", saved.Builder)
	}
	if saved.UpdatedAt == nil {
		t.Fatalf("updated_at should be set")
	}

	got, err := store.GetVesselParticulars()
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Model != "W-60" || got.Year == nil || *got.Year != 2024 || got.DisplacementKG == nil || *got.DisplacementKG != 24500 ||
		got.HIN != "XXTST00001A000" || got.DateAcquired != "2025-01-27" || got.EPIRBID != "ABC" {
		t.Fatalf("round trip = %+v", got)
	}

	// A PUT replaces the record: what is left out is cleared.
	cleared, err := store.SetVesselParticulars(vesselParticulars{Model: "Only this"})
	if err != nil {
		t.Fatalf("second set: %v", err)
	}
	if cleared.Builder != "" || cleared.Year != nil || cleared.DisplacementKG != nil || cleared.Model != "Only this" {
		t.Fatalf("expected a whole replace, got %+v", cleared)
	}
	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM vessel_particulars`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("there is only ever one row: n=%d err=%v", n, err)
	}
}

func TestVesselParticulars_Validation(t *testing.T) {
	store := newTestDocumentStore(t)
	badYear, goodYear, neg := 1500, 1999, -5.0
	cases := map[string]vesselParticulars{
		"year":            {Year: &badYear},
		"displacement_kg": {DisplacementKG: &neg},
		"date_acquired":   {DateAcquired: "27 Jan 2025"},
	}
	for field, v := range cases {
		_, err := store.SetVesselParticulars(v)
		var verr *vesselParticularsError
		if err == nil {
			t.Fatalf("%s: expected an error", field)
		}
		if !asVesselParticularsError(err, &verr) || verr.Field != field {
			t.Fatalf("%s: expected a field error naming it, got %v", field, err)
		}
	}
	if _, err := store.SetVesselParticulars(vesselParticulars{Year: &goodYear}); err != nil {
		t.Fatalf("a plausible year is fine: %v", err)
	}
}

func TestVesselParticularsHandlers(t *testing.T) {
	withTestDocumentStore(t)

	c, rec := newDocumentEchoContext(http.MethodGet, "/api/vessel/particulars", "", "")
	if err := getVesselParticularsHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("GET: %v %d", err, rec.Code)
	}
	// Every key is present on the empty record (ADR 0115 section 7).
	for _, key := range []string{`"builder":""`, `"year":null`, `"displacement_kg":null`, `"hin":""`, `"date_acquired":""`, `"updated_at":null`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Fatalf("GET body lacks %s: %s", key, rec.Body.String())
		}
	}

	c, rec = newDocumentEchoContext(http.MethodPut, "/api/vessel/particulars",
		`{"builder":"Granocean","model":"W-60","year":2024,"hin":"X","flag":"","hailing_port":"","hull_type":"Catamaran","hull_material":"","displacement_kg":24500,"shore_power":"","system_voltage":"24v","registration":"","imo":"","epirb_id":"","date_acquired":"2025-01-27"}`, "")
	if err := putVesselParticularsHandler(c); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("PUT: %v %d %s", err, rec.Code, rec.Body.String())
	}
	var got vesselParticulars
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Builder != "Granocean" || got.UpdatedAt == nil {
		t.Fatalf("PUT response = %+v (%v)", got, err)
	}

	c, rec = newDocumentEchoContext(http.MethodPut, "/api/vessel/particulars", `{"year":1500}`, "")
	if err := putVesselParticularsHandler(c); err != nil {
		t.Fatalf("PUT: %v", err)
	}
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"year"`) {
		t.Fatalf("expected a 400 naming year, got %d %s", rec.Code, rec.Body.String())
	}
}
