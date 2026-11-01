package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateMarinaGolden = flag.Bool("update", false, "rewrite the golden output files under testdata/")

// The host's marina card is built from this exact answer (backend
// assistant_marina_card_test.go reads the file), so it is what the export
// writes for the Coral Sea Marina fixtures, byte for byte.
func TestRunMarinaDetails_CoralSeaWireGolden(t *testing.T) {
	post, _ := coralSeaPost(t)
	out, err := runMarinaDetails(post, marinaDetailsInput{Lat: coralSeaLat, Lon: coralSeaLon})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	path := filepath.Join("testdata", "marina_details_coral_sea_output.json")
	if *updateMarinaGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("export output drifted from %s; run go test -update if the change is intended", path)
	}
}
