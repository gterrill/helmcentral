package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestCoastlineStore(t *testing.T) *coastlineStore {
	t.Helper()
	s, err := newCoastlineStore(filepath.Join(t.TempDir(), "helmcentral.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

var coastlineTestAt = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

func sampleCoastLines() []shorelineLine {
	ring := []shorelinePoint{{-20.5, 148.5}, {-20.5, 148.6}, {-20.4, 148.6}, {-20.5, 148.5}}
	return []shorelineLine{
		{ID: "way/1", Kind: "coastline", Name: "Cape", Points: []shorelinePoint{{-20.9, 148.1}, {-20.8, 148.2}}},
		{ID: "way/2", Kind: "reef", Name: "Reef", Closed: true, Points: ring},
	}
}

func TestCoastlineStore_RoundTripsASquare(t *testing.T) {
	s := newTestCoastlineStore(t)
	sq := coverageSquare{-21, 148}
	if err := s.SaveSquare(sq, sampleCoastLines(), coastlineTestAt); err != nil {
		t.Fatal(err)
	}
	rec, ok, err := s.Record(sq)
	if err != nil || !ok {
		t.Fatalf("record %v %v", ok, err)
	}
	if rec.Status != coastlineStatusReady || !rec.DownloadedAt.Equal(coastlineTestAt) || rec.EditsSince != 0 || rec.LastError != "" {
		t.Errorf("record %+v", rec)
	}
	lines, err := s.Lines([]coverageSquare{sq})
	if err != nil {
		t.Fatal(err)
	}
	want := sampleCoastLines()
	if len(lines) != 2 || lines[0].ID != want[0].ID || lines[1].Closed != true || lines[1].Points[1] != want[1].Points[1] || lines[0].Name != "Cape" {
		t.Errorf("lines %+v", lines)
	}
}

func TestCoastlineStore_EmptySquareIsValid(t *testing.T) {
	s := newTestCoastlineStore(t)
	sq := coverageSquare{-30, 160}
	if err := s.SaveSquare(sq, nil, coastlineTestAt); err != nil {
		t.Fatal(err)
	}
	rec, ok, _ := s.Record(sq)
	if !ok || rec.Status != coastlineStatusReady {
		t.Fatalf("an open-sea square is ready: %+v", rec)
	}
	lines, err := s.Lines([]coverageSquare{sq})
	if err != nil || len(lines) != 0 {
		t.Errorf("lines %v err %v", lines, err)
	}
}

func TestCoastlineStore_LinesRefusesASquareNotHeld(t *testing.T) {
	s := newTestCoastlineStore(t)
	_ = s.SaveSquare(coverageSquare{-21, 148}, sampleCoastLines(), coastlineTestAt)
	if _, err := s.Lines([]coverageSquare{{-21, 148}, {-21, 149}}); err == nil || !strings.Contains(err.Error(), "21°S 149°E") {
		t.Errorf("want an error naming the missing square, got %v", err)
	}
	// A failed square holds no geometry either.
	_ = s.RecordFailure(coverageSquare{-21, 149}, "overpass rate limited", coastlineTestAt, coastlineTestAt.Add(time.Minute))
	if _, err := s.Lines([]coverageSquare{{-21, 149}}); err == nil {
		t.Error("a failed square has no lines")
	}
}

func TestCoastlineStore_ReefsCrossingASquareEdgeAreDedupedByID(t *testing.T) {
	s := newTestCoastlineStore(t)
	reef := shorelineLine{ID: "way/9", Kind: "reef", Closed: true, Points: []shorelinePoint{{-20.9, 148.9}, {-20.9, 149.1}, {-20.8, 149.1}, {-20.9, 148.9}}}
	coastA := shorelineLine{ID: "way/5", Kind: "coastline", Points: []shorelinePoint{{-20.9, 148.1}, {-20.9, 148.99}}}
	coastB := shorelineLine{ID: "way/5", Kind: "coastline", Points: []shorelinePoint{{-20.9, 149.0}, {-20.9, 149.4}}}
	_ = s.SaveSquare(coverageSquare{-21, 148}, []shorelineLine{reef, coastA}, coastlineTestAt)
	_ = s.SaveSquare(coverageSquare{-21, 149}, []shorelineLine{reef, coastB}, coastlineTestAt)
	lines, err := s.Lines([]coverageSquare{{-21, 148}, {-21, 149}})
	if err != nil {
		t.Fatal(err)
	}
	reefs, coasts := 0, 0
	for _, l := range lines {
		switch l.Kind {
		case "reef":
			reefs++
		case "coastline":
			coasts++
		}
	}
	if reefs != 1 {
		t.Errorf("the reef appears in both squares and must be one ring, got %d", reefs)
	}
	if coasts != 2 {
		t.Errorf("clipped coastline pieces from two squares are both kept, got %d", coasts)
	}
}

func TestCoastlineStore_FailureEditsAndRetryState(t *testing.T) {
	s := newTestCoastlineStore(t)
	sq := coverageSquare{-21, 148}

	// A square that never downloaded: failed, no geometry, attempts count up.
	next := coastlineTestAt.Add(5 * time.Minute)
	if err := s.RecordFailure(sq, "overpass rate limited", coastlineTestAt, next); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordFailure(sq, "overpass rate limited", coastlineTestAt, next); err != nil {
		t.Fatal(err)
	}
	rec, _, _ := s.Record(sq)
	if rec.Status != coastlineStatusFailed || rec.Attempts != 2 || rec.LastError != "overpass rate limited" || !rec.NextAttemptAt.Equal(next) {
		t.Errorf("failed record %+v", rec)
	}

	// A good download clears the error and the attempts.
	_ = s.SaveSquare(sq, sampleCoastLines(), coastlineTestAt.Add(time.Hour))
	rec, _, _ = s.Record(sq)
	if rec.Status != coastlineStatusReady || rec.Attempts != 0 || rec.LastError != "" {
		t.Errorf("after download %+v", rec)
	}

	// Edits found by the weekly check.
	checked := coastlineTestAt.Add(24 * time.Hour)
	if err := s.RecordEdits(sq, 4, checked); err != nil {
		t.Fatal(err)
	}
	rec, _, _ = s.Record(sq)
	if rec.EditsSince != 4 || !rec.CheckedAt.Equal(checked) {
		t.Errorf("edits %+v", rec)
	}

	// A failure on a held square keeps its geometry and stays ready.
	_ = s.RecordFailure(sq, "timed out", checked, checked.Add(time.Hour))
	rec, _, _ = s.Record(sq)
	if rec.Status != coastlineStatusReady || rec.LastError != "timed out" || rec.EditsSince != 4 {
		t.Errorf("after failed update %+v", rec)
	}
	if lines, err := s.Lines([]coverageSquare{sq}); err != nil || len(lines) != 2 {
		t.Errorf("geometry must survive a failed update: %v %v", lines, err)
	}

	// Re-downloading resets the edits.
	_ = s.SaveSquare(sq, sampleCoastLines(), checked.Add(time.Hour))
	rec, _, _ = s.Record(sq)
	if rec.EditsSince != 0 {
		t.Errorf("edits not reset: %+v", rec)
	}
}

func TestCoastlineStore_RecordEditsOnASquareNotHeldIsAnError(t *testing.T) {
	s := newTestCoastlineStore(t)
	if err := s.RecordEdits(coverageSquare{1, 1}, 3, coastlineTestAt); err == nil {
		t.Error("want an error for an unknown square")
	}
}

func TestCoastlineStore_RecordsAndClear(t *testing.T) {
	s := newTestCoastlineStore(t)
	_ = s.SaveSquare(coverageSquare{-21, 148}, sampleCoastLines(), coastlineTestAt)
	_ = s.SaveSquare(coverageSquare{-21, 149}, nil, coastlineTestAt)
	_ = s.RecordFailure(coverageSquare{-22, 148}, "x", coastlineTestAt, coastlineTestAt)
	recs, err := s.Records()
	if err != nil || len(recs) != 3 {
		t.Fatalf("records %v %v", recs, err)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	recs, _ = s.Records()
	if len(recs) != 0 {
		t.Errorf("clear left %v", recs)
	}
}

// Record returns one square's row, ok false when the store has none. Only
// tests need a single-row read; production code reads Records.
func (s *coastlineStore) Record(sq coverageSquare) (coastlineSquareRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := scanCoastlineRecord(s.db.QueryRow(`SELECT `+coastlineRecordColumns+` FROM coastline_squares WHERE lat = ? AND lon = ?`, sq.Lat, sq.Lon))
	if err == sql.ErrNoRows {
		return coastlineSquareRecord{}, false, nil
	}
	if err != nil {
		return coastlineSquareRecord{}, false, fmt.Errorf("read coastline square: %w", err)
	}
	return r, true, nil
}
