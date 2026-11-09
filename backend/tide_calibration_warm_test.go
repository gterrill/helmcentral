package main

import (
	"math"
	"testing"
	"time"
)

// sameFit says whether two fits agree to the tolerance a converged fit is good
// for: the iteration stops when k moves by less than 1e-7, so two routes to the
// same optimum differ by about that, not exactly.
func sameFit(t *testing.T, got, want *tideCalFit) {
	t.Helper()
	if got.OffsetMin != want.OffsetMin {
		t.Errorf("offset = %d min, cold fit has %d", got.OffsetMin, want.OffsetMin)
	}
	if len(got.Members) != len(want.Members) {
		t.Errorf("%d plausible offsets, cold fit has %d", len(got.Members), len(want.Members))
	}
	for name, pair := range map[string][2]float64{
		"k": {got.K, want.K}, "k half width": {got.KHalf, want.KHalf}, "sigma": {got.Sigma, want.Sigma},
	} {
		if math.Abs(pair[0]-pair[1]) > 1e-4 {
			t.Errorf("%s = %.6f, cold fit has %.6f", name, pair[0], pair[1])
		}
	}
	if len(got.Cells) != len(want.Cells) {
		t.Fatalf("%d cells, cold fit has %d", len(got.Cells), len(want.Cells))
	}
	for c, w := range want.Cells {
		g, ok := got.Cells[c]
		if !ok {
			t.Errorf("cell %v missing from the warm fit", c)
			continue
		}
		if math.Abs(g.SeabedM-w.SeabedM) > 1e-3 {
			t.Errorf("cell %v seabed = %.5f, cold fit has %.5f", c, g.SeabedM, w.SeabedM)
		}
	}
}

// A refit started from the previous fit's weights lands where a cold fit on the
// same readings does, minute after minute, and a spike in the new minutes does
// not pull it off.
func TestAWarmStartedFitMatchesAColdFit(t *testing.T) {
	curve := loadKeppelCurve(t)
	all := keppelMinutes(t, curve)
	start := keppelDrop.Add(3 * time.Hour)
	rs := readingsUntil(all, start)
	_, ferr, warm := fitTideCalibrationWarm(rs, keppelAnchorLat, keppelAnchorLon, curve, "chart", nil)
	if ferr != nil {
		t.Fatal(ferr)
	}
	for step := 1; step <= 6; step++ {
		rs = readingsUntil(all, start.Add(time.Duration(step)*7*time.Minute))
		if step == 3 {
			rs[len(rs)-1].DepthM += 3 // a spike, as a passing boat gives
		}
		var got *tideCalFit
		got, ferr, warm = fitTideCalibrationWarm(rs, keppelAnchorLat, keppelAnchorLon, curve, "chart", warm)
		if ferr != nil {
			t.Fatal(ferr)
		}
		cold, cerr := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve)
		if cerr != nil {
			t.Fatal(cerr)
		}
		sameFit(t, got, cold)
	}
}

// A warm start is only for the same chart and the same anchor over the same
// stay: anything else is fitted cold, and still correct.
func TestAWarmStartIsDroppedWhenTheChartOrTheReadingsChange(t *testing.T) {
	curve := loadKeppelCurve(t)
	all := keppelMinutes(t, curve)
	rs := readingsUntil(all, keppelDrop.Add(3*time.Hour))
	_, _, warm := fitTideCalibrationWarm(rs, keppelAnchorLat, keppelAnchorLon, curve, "chart-a", nil)
	cold, cerr := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve)
	if cerr != nil {
		t.Fatal(cerr)
	}
	if got, _, _ := fitTideCalibrationWarm(rs, keppelAnchorLat, keppelAnchorLon, curve, "chart-b", warm); got == nil {
		t.Fatal("no fit on another chart")
	} else {
		sameFit(t, got, cold)
	}
	// Readings that are not the old ones with minutes added (a stay reloaded
	// from a later start) cannot line up with the old weights.
	later := readingsUntil(all, keppelDrop.Add(3*time.Hour))[30:]
	want, _ := fitTideCalibration(later, keppelAnchorLat, keppelAnchorLon, curve)
	got, _, _ := fitTideCalibrationWarm(later, keppelAnchorLat, keppelAnchorLon, curve, "chart-a", warm)
	if got == nil || want == nil {
		t.Fatal("no fit")
	}
	sameFit(t, got, want)
}

// While the chart is unchanged the stored extremes are not read again: the stay
// holds them. Emptying the table between two fits shows it, since a read would
// find nothing and the fit would fail or move.
func TestTheStoredExtremesAreNotReadAgainWhileTheChartIsUnchanged(t *testing.T) {
	resetTideCalState(t)
	store := newTestPassageStore(t)
	curve := loadKeppelCurve(t)
	all := keppelMinutes(t, curve)
	rs := readingsUntil(all, keppelDrop.Add(3*time.Hour))
	for _, r := range rs {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	chart, _ := keppelChart(t)()
	first, err := tideCalFitNow(keppelWatch(), store, chart)
	if err != nil || first.fit == nil {
		t.Fatalf("first fit: %v %+v", err, first)
	}
	if _, err := store.db.Exec(`DELETE FROM tide_calibration_extremes`); err != nil {
		t.Fatal(err)
	}
	next := all[len(rs)]
	if err := store.appendTideCalReading(keppelDrop, next); err != nil {
		t.Fatal(err)
	}
	tideCalStayAppend(store, keppelDrop, next)
	second, err := tideCalFitNow(keppelWatch(), store, chart)
	if err != nil || second.fit == nil {
		t.Fatalf("second fit with the table emptied: %v %+v", err, second)
	}
	if second.fit.LastAt != next.At {
		t.Errorf("second fit ends at %v, want the new minute %v", second.fit.LastAt, next.At)
	}

	// A revised chart is stored and read again.
	revised := chart
	revised.Extremes = append([]tideExtremePoint(nil), chart.Extremes...)
	revised.Extremes[3].HeightM += 0.05
	if _, err := tideCalFitNow(keppelWatch(), store, revised); err != nil {
		t.Fatal(err)
	}
	got, err := store.tideCalExtremes(keppelDrop, chart.Source)
	if err != nil || len(got) != len(revised.Extremes) {
		t.Fatalf("a revised chart left %d extremes stored (%v), want %d", len(got), err, len(revised.Extremes))
	}
}

// weekStay is a week at anchor: a minute of depth for 7 days over a synthetic
// semi-diurnal tide, the boat swinging over a few dozen cells.
func weekStay() ([]tideCalReading, tideCurve) {
	t0 := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	var ex []tideExtremePoint
	for i := 0; i < 7*4+8; i++ {
		high := i%2 == 0
		h := 0.8
		if high {
			h = 3.1
		}
		ex = append(ex, tideExtremePoint{Time: t0.Add(-6*time.Hour + time.Duration(i)*6*time.Hour + 12*time.Minute), HeightM: h + 0.1*math.Sin(float64(i)), High: high})
	}
	curve := newTideCurve(ex)
	var rs []tideCalReading
	for m := 0; m < 7*24*60; m++ {
		at := t0.Add(time.Duration(m) * time.Minute)
		h, _ := curve.heightAt(at.Add(-9 * time.Minute))
		a := float64(m) / 97
		lat := keppelAnchorLat + 0.00018*math.Sin(a)
		lon := keppelAnchorLon + 0.00018*math.Cos(a*1.3)
		c := tideCalCellAt(keppelAnchorLat, keppelAnchorLon, lat, lon)
		noise := 0.03 * math.Sin(float64(m)*1.7)
		if m%37 == 0 { // a passing boat's wake, the sounder reading short
			noise -= 0.6
		}
		rs = append(rs, tideCalReading{At: at, Lat: lat, Lon: lon, DepthM: 2.5 + 0.1*float64((c.E+3*c.N)%5) + 0.85*h + noise})
	}
	return rs, curve
}

// BenchmarkRefitAWeekAtAnchor is what the worker does on every stored minute of
// a long stay: cold is the fit from nothing, warm the fit started from the one
// before.
func BenchmarkRefitAWeekAtAnchor(b *testing.B) {
	rs, curve := weekStay()
	b.Run("cold", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := fitTideCalibration(rs, keppelAnchorLat, keppelAnchorLon, curve); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("warm", func(b *testing.B) {
		_, _, warm := fitTideCalibrationWarm(rs[:len(rs)-1], keppelAnchorLat, keppelAnchorLon, curve, "chart", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err, w := fitTideCalibrationWarm(rs, keppelAnchorLat, keppelAnchorLon, curve, "chart", warm)
			if err != nil {
				b.Fatal(err)
			}
			_ = w // each run starts from the same previous fit, as one minute on
		}
	})
}
