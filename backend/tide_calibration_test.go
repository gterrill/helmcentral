package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// The Great Keppel stay of 2026-11-08, captured from the boat: the anchor went
// down at 10.15 AM (00:15 UTC) at the point below, in a 65 m radius. The files
// are the swing trail, the sounder's one-minute depth means and the station's
// highs and lows (Middle Island, metres).
const (
	keppelAnchorLat = -23.18470725894526
	keppelAnchorLon = 150.93224525362177
	keppelMarginM   = 0.5
)

var keppelDrop = time.Date(2026, 11, 8, 0, 15, 27, 0, time.UTC)

func loadKeppelCurve(t *testing.T) tideCurve {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "tide_calibration", "extremes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ex []tideExtremePoint
	if err := json.Unmarshal(raw, &ex); err != nil {
		t.Fatal(err)
	}
	return newTideCurve(ex)
}

// keppelMinutes builds the stored minutes of the stay the way the recorder
// would have: the depth of each minute at the boat's position then, with the
// station's height.
func keppelMinutes(t *testing.T, curve tideCurve) []tideCalReading {
	t.Helper()
	var trail struct {
		Points []struct {
			Lat       float64   `json:"lat"`
			Lon       float64   `json:"lon"`
			Timestamp time.Time `json:"timestamp"`
		} `json:"points"`
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "tide_calibration", "trail.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &trail); err != nil {
		t.Fatal(err)
	}
	var depth []struct {
		T    time.Time `json:"t"`
		Mean float64   `json:"mean"`
	}
	raw, err = os.ReadFile(filepath.Join("testdata", "tide_calibration", "depth_fine.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &depth); err != nil {
		t.Fatal(err)
	}
	pts := trail.Points
	var out []tideCalReading
	for _, d := range depth {
		at := d.T.Add(30 * time.Second)
		i := sort.Search(len(pts), func(i int) bool { return !pts[i].Timestamp.Before(at) })
		if i == 0 || i >= len(pts) || pts[i].Timestamp.Sub(pts[i-1].Timestamp) > 5*time.Minute {
			continue
		}
		a, b := pts[i-1], pts[i]
		f := at.Sub(a.Timestamp).Seconds() / b.Timestamp.Sub(a.Timestamp).Seconds()
		out = append(out, tideCalReading{At: at, Lat: a.Lat + f*(b.Lat-a.Lat), Lon: a.Lon + f*(b.Lon-a.Lon), DepthM: d.Mean})
	}
	return out
}

func readingsUntil(rs []tideCalReading, end time.Time) []tideCalReading {
	var out []tideCalReading
	for _, r := range rs {
		if !r.At.After(end) {
			out = append(out, r)
		}
	}
	return out
}

var keppelLow = time.Date(2026, 11, 8, 4, 45, 0, 0, time.UTC)
var keppelHigh = time.Date(2026, 11, 8, 10, 27, 0, 0, time.UTC)

// The replay goes through the store, as a restart would: the minutes are
// written, read back and fitted. It asserts what the prototype found (the design
// note, section 4): k about 0.83 over the whole stay; three hours in, the
// estimate for the 4.45 AM low (where the sounder read 2.36 m) within 0.15 m; and
// readiness reached between 1.5 and 4 hours in.
func TestTideCalibrationReplaysTheKeppelStay(t *testing.T) {
	curve := loadKeppelCurve(t)
	minutes := keppelMinutes(t, curve)
	store := newTestPassageStore(t)
	var extremes []tideExtremePoint
	extremes = append(extremes, curve...)
	if err := store.upsertTideCalExtremes(keppelDrop, bomMI, extremes); err != nil {
		t.Fatal(err)
	}
	for _, r := range minutes {
		if err := store.appendTideCalReading(keppelDrop, r); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := store.tideCalReadings(keppelDrop)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != len(minutes) {
		t.Fatalf("stored %d minutes, read back %d", len(minutes), len(stored))
	}

	// The stay is fitted on its own: the tide curve is rebuilt from the stored
	// highs and lows, not from any chart.
	storedExtremes, err := store.tideCalExtremes(keppelDrop, bomMI)
	if err != nil || len(storedExtremes) != len(extremes) {
		t.Fatalf("stored %d extremes, read %d, %v", len(extremes), len(storedExtremes), err)
	}
	curve = newTideCurve(storedExtremes)
	full, ferr := fitTideCalibration(stored, keppelAnchorLat, keppelAnchorLon, curve)
	if ferr != nil {
		t.Fatal(ferr)
	}
	t.Logf("full stay: k=%.3f ±%.3f offset=%d min members=%d cells=%d sigma=%.3f infl=%.2f", full.K, full.KHalf, full.OffsetMin, len(full.Members), len(full.Cells), full.Sigma, full.Inflation)
	if math.Abs(full.K-0.83) > 0.05 {
		t.Errorf("full-stay k = %.3f, want 0.83 +/- 0.05", full.K)
	}

	at3h := keppelDrop.Add(3 * time.Hour)
	est, err := estimateTideCalibration(readingsUntil(stored, at3h), keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, keppelMarginM, at3h)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("3 h: k=%.3f low here %.2f +/- %.2f shallowest %.2f +/- %.2f", est.Fit.K, est.LowHere.ValueM, est.LowHere.HalfWidthM, est.LowShallowest.ValueM, est.LowShallowest.HalfWidthM)
	if math.Abs(est.LowHere.ValueM-2.36) > 0.15 {
		t.Errorf("3 h estimate for the low = %.2f, want 2.36 +/- 0.15", est.LowHere.ValueM)
	}

	if est.LowHere.BottomM() > 2.36 || est.LowHere.ValueM+est.LowHere.HalfWidthM < 2.36 {
		t.Errorf("the 95%% interval %.2f +/- %.2f does not take in the sounder's 2.36 m", est.LowHere.ValueM, est.LowHere.HalfWidthM)
	}

	var ready time.Duration
	for m := 30; m <= 8*60 && ready == 0; m += 5 {
		now := keppelDrop.Add(time.Duration(m) * time.Minute)
		e, err := estimateTideCalibration(readingsUntil(stored, now), keppelAnchorLat, keppelAnchorLon, curve, keppelLow, keppelHigh, keppelMarginM, now)
		if err == nil && e != nil {
			ready = now.Sub(keppelDrop)
		}
	}
	t.Logf("ready after %s with a %.1f m margin", ready, keppelMarginM)
	if ready < 90*time.Minute || ready > 4*time.Hour {
		t.Errorf("ready after %s, want between 1.5 h and 4 h", ready)
	}
}
