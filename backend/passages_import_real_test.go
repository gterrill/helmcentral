package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These files are the operator's own signalk-daily-gpx-plugin output, which is
// Douglas-Peucker simplified: a straight leg is a few points many minutes
// apart, and nothing is written while the boat sits still.

func loadGPXFixtures(t *testing.T, names ...string) []gpxFile {
	t.Helper()
	var files []gpxFile
	for _, n := range names {
		f, err := os.Open(filepath.Join("testdata", "passages", "gpx", n))
		if err != nil {
			t.Fatal(err)
		}
		fixes, _, err := parseGPX(f, gpxMaxFileBytes, 0)
		f.Close()
		if err != nil {
			t.Fatal(n, err)
		}
		files = append(files, gpxFile{Name: n, Fixes: fixes})
	}
	return files
}

func polylineNM(fixes []gpxFix) float64 {
	var m float64
	for i := 1; i < len(fixes); i++ {
		m += haversineMeters(fixes[i-1].Lat, fixes[i-1].Lon, fixes[i].Lat, fixes[i].Lon)
	}
	return m / passageMetersPerMile
}

func TestRealImportStraightLegsWithLongGapsStayOnePassage(t *testing.T) {
	// 21 intervals of 10 to 28 minutes at 6 to 7 kn
	files := loadGPXFixtures(t, "2026-03-31-1615.gpx")
	s := newTestPassageStore(t)
	if _, err := s.ImportGPX(files); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListPassages()
	if len(list) != 1 {
		t.Fatalf("passages %d, want 1", len(list))
	}
	pts, _ := s.TrackPointsBetween(list[0].Start, list[0].End)
	segs := map[int]bool{}
	for _, p := range pts {
		segs[p.Segment] = true
	}
	if len(segs) != 1 {
		t.Errorf("imported track split into %d segments", len(segs))
	}
	st := computePassageStats(pts)
	want := polylineNM(files[0].Fixes)
	if !approx(st.DistanceNM, want, want*0.03) {
		t.Errorf("distance %.1f NM, want about the full polyline %.1f NM", st.DistanceNM, want)
	}
	if st.UnderwaySeconds < 0.8*list[0].End.Sub(list[0].Start).Seconds() {
		t.Errorf("underway %.0f s of %.0f s: gaps at speed are movement", st.UnderwaySeconds, list[0].End.Sub(list[0].Start).Seconds())
	}
}

func TestRealImportOvernightGapsEndPassages(t *testing.T) {
	files := loadGPXFixtures(t, "2026-09-14-1310.gpx", "2026-09-15-1334.gpx", "2026-09-16-1543.gpx")
	s := newTestPassageStore(t)
	if _, err := s.ImportGPX(files); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListPassages()
	if len(list) != 3 {
		t.Fatalf("passages %d, want one per night's run", len(list))
	}
	for i := 1; i < len(list); i++ {
		// newest first: each ends before the next one begins
		if !list[i].End.Before(list[i-1].Start) {
			t.Errorf("passages overlap: %v %v", list[i].End, list[i-1].Start)
		}
	}
	for _, p := range list {
		if d := p.End.Sub(p.Start); d < 3*time.Hour || d > 8*time.Hour {
			t.Errorf("passage %v lasts %v", p.Start, d)
		}
	}
}

func TestRealImportLongRestGapIsRestNotACrawl(t *testing.T) {
	fixes := []gpxFix{
		{At: passageEpoch, Lat: -20, Lon: 149},
		{At: passageEpoch.Add(3 * time.Hour), Lat: -20 - 100.0/111195, Lon: 149}, // 100 m in 3 h
		{At: passageEpoch.Add(3*time.Hour + 2*time.Minute), Lat: -20 - 100.0/111195 - 0.004, Lon: 149},
	}
	pts, _ := importedTrackPoints(fixes)
	if pts[0].SOGKts == nil || *pts[0].SOGKts >= passageRestSOGKts {
		t.Fatalf("first point sog %v: a 3 h gap with 100 m moved is rest", pts[0].SOGKts)
	}
	if pts[1].SOGKts == nil || *pts[1].SOGKts < passageStartSOGKts {
		t.Errorf("second point sog %v: the 2 min leg after the rest is its own speed", pts[1].SOGKts)
	}
	if pts[0].Segment != pts[2].Segment {
		t.Error("a long gap is not a segment break for imports")
	}
}

func TestRealImportTinyFilesMakeNoPassage(t *testing.T) {
	s := newTestPassageStore(t)
	sum, err := s.ImportGPX(loadGPXFixtures(t, "2026-03-03.gpx"))
	if err != nil {
		t.Fatal(err)
	}
	if sum.PassagesCreated != 0 {
		t.Errorf("%+v", sum)
	}
}

func TestRealImportReimportOfRealFilesAddsNothing(t *testing.T) {
	files := loadGPXFixtures(t, "2026-09-14-1310.gpx", "2026-09-15-1334.gpx")
	s := newTestPassageStore(t)
	s.ImportGPX(files)
	sum, _ := s.ImportGPX(files)
	if sum.PointsAdded != 0 || sum.PassagesCreated != 0 {
		t.Errorf("%+v", sum)
	}
}

func TestRealImportKeepsFortyKnotJumpRejection(t *testing.T) {
	fixes := loadGPXFixtures(t, "2026-07-15-1333.gpx")[0].Fixes
	fixes[40].Lat += 2
	_, rejected := importedTrackPoints(fixes)
	if rejected < 1 {
		t.Error("a 120 NM jump must be rejected")
	}
}
