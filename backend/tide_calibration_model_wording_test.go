package main

import (
	"strings"
	"testing"
)

// A tide that comes from the position model is flagged on the verdict, with or
// without a calibration, so the page and Mate stop calling it a station.
func TestLowWaterFlagsAPositionTide(t *testing.T) {
	state := vesselStateData{Depth: 4, DepthLastUpdateAge: 2, DraftM: 1.2}
	margin := func() (float64, error) { return 0.5, nil }
	origRegistry, origOrder := tideProviderRegistry, tideProviderOrder
	tideProviderRegistry, tideProviderOrder = map[string]tideProvider{}, nil
	t.Cleanup(func() { tideProviderRegistry, tideProviderOrder = origRegistry, origOrder })
	registerTideProvider(newMSLStub(lwNow))
	for provider, want := range map[string]bool{"open-meteo": true, "": false} {
		tide := func() (tideTodayResponse, error) {
			return *lwTide(func(r *tideTodayResponse) { r.Provider = provider }), nil
		}
		got, err := buildLowWater(state, tide, margin, nil, lwNow)
		if err != nil || got.Status != lowWaterOK {
			t.Fatalf("%q: %+v, %v", provider, got, err)
		}
		if got.TideIsPoint != want {
			t.Errorf("provider %q: TideIsPoint = %v, want %v", provider, got.TideIsPoint, want)
		}
	}
}

func TestMateWhyNamesTheTideModelForAPosition(t *testing.T) {
	for _, code := range []string{tideCalTooFewReadings, tideCalImplausibleFit, tideCalNoTideCurve} {
		station := calibrationWhy(code, false)
		model := calibrationWhy(code, true)
		if !strings.Contains(station, "station") {
			t.Errorf("%s: station wording lost: %q", code, station)
		}
		if strings.Contains(model, "station") || !strings.Contains(model, "tide model") {
			t.Errorf("%s: model wording = %q", code, model)
		}
	}
	for _, code := range []string{tideCalTooScattered, tideCalNoRecentReading, tideCalNoClearanceSet} {
		if calibrationWhy(code, true) != calibrationNotReadyWhy[code] {
			t.Errorf("%s changed for a position", code)
		}
	}
}

func TestMateCalibrationUsesModelWordingForAPosition(t *testing.T) {
	c := &lowWaterCalibration{NotReady: tideCalImplausibleFit, StationIsPoint: true}
	out, err := assistantToolDeps{}.vesselStatusCalibrationFor(c, func(*float64) (*statusValue, error) { return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.NotReadyWhy, "station") || !strings.Contains(out.NotReadyWhy, "tide model") {
		t.Errorf("why = %q", out.NotReadyWhy)
	}
}
