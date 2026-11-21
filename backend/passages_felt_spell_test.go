package main

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// ── what a spell keeps for the sea hindcast and the comfort fit (ADR 0299) ──

func TestSpellKeepsItsMidPositionAndRollAndPitchRmsSeparately(t *testing.T) {
	pts := feltRun()
	setSway(pts, 1, 15, 3, 4)
	setSway(pts, 16, 30, 1, 2)
	s := computePassageStats(pts)
	if len(s.FeltSpells) < 2 {
		t.Fatalf("spells: %+v", s.FeltSpells)
	}
	sp := s.FeltSpells[0]
	if !approx(sp.RollRMSDeg, 3, 1e-9) || !approx(sp.PitchRMSDeg, 4, 1e-9) {
		t.Errorf("roll %v pitch %v, want 3 and 4", sp.RollRMSDeg, sp.PitchRMSDeg)
	}
	if !approx(sp.SwayDeg, 5, 1e-9) {
		t.Errorf("sway %v, want 5", sp.SwayDeg)
	}
	wantLat := (pts[0].Lat + pts[15].Lat) / 2
	if sp.MidLat == nil || sp.MidLon == nil || !approx(*sp.MidLat, wantLat, 0.002) || *sp.MidLon != 149 {
		t.Fatalf("mid position %v, %v; want about %v, 149", sp.MidLat, sp.MidLon, wantLat)
	}
	later := s.FeltSpells[1]
	if later.MidLat == nil || *later.MidLat <= *sp.MidLat {
		t.Errorf("the second spell is further north: %v then %v", *sp.MidLat, later.MidLat)
	}
}

func TestSpellMeanHeadingIsAVectorMeanSoNorthStaysNorth(t *testing.T) {
	pts := feltRun()
	setSway(pts, 1, 15, 1, 1)
	for i := 1; i <= 15; i++ {
		h := 350.0
		if i%2 == 0 {
			h = 10
		}
		pts[i].HeadingDeg = pfloat(h)
	}
	s := computePassageStats(pts)
	if len(s.FeltSpells) == 0 || s.FeltSpells[0].MeanHeadingDeg == nil {
		t.Fatalf("spells: %+v", s.FeltSpells)
	}
	got := *s.FeltSpells[0].MeanHeadingDeg
	if math.Min(got, 360-got) > 1.5 {
		t.Errorf("mean of 350 and 10 degrees is north, got %v", got)
	}
}

func TestSpellWithoutHeadingHasNoMeanHeading(t *testing.T) {
	pts := feltRun()
	setSway(pts, 1, 15, 1, 1)
	s := computePassageStats(pts)
	if s.FeltSpells[0].MeanHeadingDeg != nil {
		t.Errorf("no heading was recorded, got %v", *s.FeltSpells[0].MeanHeadingDeg)
	}
}

func TestSpellSnapAndPoundingFiguresAreThePeaksInsideItAndNilWithout10HzData(t *testing.T) {
	pts := feltRun()
	setSway(pts, 1, 30, 1, 1)
	pts[0].RollAccelPeakDps2 = pfloat(500) // the interval before the passage
	pts[5].RollAccelPeakDps2 = pfloat(40)
	pts[9].RollAccelPeakDps2 = pfloat(70)
	pts[12].VertAccelPeakMs2 = pfloat(2 * gravityMPerS2)
	s := computePassageStats(pts)
	if len(s.FeltSpells) != 2 {
		t.Fatalf("spells: %+v", s.FeltSpells)
	}
	first, second := s.FeltSpells[0], s.FeltSpells[1]
	if first.RollAccelPeakDps2 == nil || *first.RollAccelPeakDps2 != 70 {
		t.Errorf("first spell peak roll acceleration %v, want 70", first.RollAccelPeakDps2)
	}
	if first.VertAccelPeakG == nil || !approx(*first.VertAccelPeakG, 2, 1e-9) {
		t.Errorf("first spell pounding %v, want 2 g", first.VertAccelPeakG)
	}
	if second.RollAccelPeakDps2 != nil || second.VertAccelPeakG != nil {
		t.Errorf("second spell had no 10 Hz data, got %v %v", second.RollAccelPeakDps2, second.VertAccelPeakG)
	}
}

func TestStatsVersionFiveRecomputesRowsStoredAtFour(t *testing.T) {
	if passageFiguresVersion != 5 {
		t.Fatalf("version %d, want 5", passageFiguresVersion)
	}
	store, recs := feltPassages(t)
	old, _ := json.Marshal(map[string]any{"version": 4, "stats": passageStats{}, "start_lat": -20.0, "start_lon": 149.0})
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, string(old), recs[0].ID); err != nil {
		t.Fatal(err)
	}
	ids, err := store.staleFigureIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != recs[0].ID {
		t.Errorf("stale %v, want only %s", ids, recs[0].ID)
	}
}

// The new figures are for the model, not the page: neither the list nor the
// passage page's spells carry them.
func TestSpellFiguresStayOffTheWire(t *testing.T) {
	pts := feltRun()
	setSway(pts, 1, 15, 3, 4)
	pts[5].RollAccelPeakDps2 = pfloat(40)
	pts[3].HeadingDeg = pfloat(10)
	st := computePassageStats(pts)
	listJSON, _ := json.Marshal(st.forWire())
	if strings.Contains(string(listJSON), "mid_lat") || strings.Contains(string(listJSON), "felt_spells") {
		t.Errorf("the list carries spell figures: %s", listJSON)
	}
	pageJSON, _ := json.Marshal(buildPassageFelt(st, passageSidewaysG{}, comfortBands{}, nil).Spells)
	for _, key := range []string{"mid_lat", "mean_heading", "roll_rms", "pitch_rms", "roll_accel", "vert_accel"} {
		if strings.Contains(string(pageJSON), key) {
			t.Errorf("the page's spells carry %s: %s", key, pageJSON)
		}
	}
	storedJSON, _ := json.Marshal(st)
	if !strings.Contains(string(storedJSON), "mid_lat") {
		t.Errorf("the stored stats must keep them: %s", storedJSON)
	}
}
