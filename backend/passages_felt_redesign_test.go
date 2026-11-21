package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

// ── start-up recompute and the backfill (ADR 0298) ───────────────────────

// backfillPassage stores a passage of twelve under-way minutes recorded before
// the motion cutoff, with no motion on its points, and returns it with the time
// of each point.
func backfillPassage(t *testing.T, s *passageStore, from time.Time) (passageRecord, []time.Time) {
	t.Helper()
	var ats []time.Time
	for i := 0; i <= 12; i++ {
		at := from.Add(time.Duration(i) * time.Minute)
		lat := -25 + float64(i)*0.00167 // about 6 knots north
		addRecorded(t, s, at, func(p *recordedTrackPoint) { p.Lat = lat })
		ats = append(ats, at)
	}
	rec, err := s.InsertPassage(passageRecord{Start: ats[0], End: ats[len(ats)-1], Title: "backfilled"})
	if err != nil {
		t.Fatal(err)
	}
	return rec, ats
}

func storedFigures(t *testing.T, s *passageStore, id string) (sql.NullString, sql.NullFloat64) {
	t.Helper()
	var raw sql.NullString
	var felt sql.NullFloat64
	if err := s.db.QueryRow(`SELECT stats_json, felt_deg FROM passages WHERE id = ?`, id).Scan(&raw, &felt); err != nil {
		t.Fatal(err)
	}
	return raw, felt
}

func TestMotionBackfillClearsTheStoredFiguresOfEveryPassageItFills(t *testing.T) {
	s := newBackfillStore(t)
	rec, ats := backfillPassage(t, s, passageEpoch.Add(time.Hour))
	// A passage the backfill never touches: recorded after the cutoff.
	other, _ := backfillPassage(t, s, backfillNow.Add(time.Hour))

	for _, r := range []passageRecord{rec, other} {
		f, err := s.PassageFiguresFor(r)
		if err != nil {
			t.Fatal(err)
		}
		if f.Version != passageFiguresVersion || f.Stats.FeltDeg != nil {
			t.Fatalf("before the backfill: version %d felt %v", f.Version, f.Stats.FeltDeg)
		}
	}

	data := map[string][]normalMinuteRow{}
	for _, at := range ats {
		data = mergeData(data, attitude("gw", at))
	}
	(&fakeMotionInflux{data: data}).install(t)
	if _, err := runMotionBackfill(context.Background(), s, testInflux, backfillNow); err != nil {
		t.Fatal(err)
	}

	if raw, felt := storedFigures(t, s, rec.ID); raw.Valid || felt.Valid {
		t.Errorf("the filled passage keeps its old figures: stats %v felt %v", raw.Valid, felt.Valid)
	}
	if raw, _ := storedFigures(t, s, other.ID); !raw.Valid {
		t.Error("a passage the backfill did not fill lost its figures")
	}
	f, err := s.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	if f.Stats.FeltDeg == nil {
		t.Fatalf("felt is not worked out from the new sway: %q", f.Stats.FeltReason)
	}
}

func TestStartUpPassRunsAfterTheBackfillSoNothingIsRecomputedBeforeItIsFilled(t *testing.T) {
	s := newBackfillStore(t)
	rec, ats := backfillPassage(t, s, passageEpoch.Add(time.Hour))
	// No figures yet: the start-up pass has this passage to do.
	data := map[string][]normalMinuteRow{}
	for _, at := range ats {
		data = mergeData(data, attitude("gw", at))
	}
	(&fakeMotionInflux{data: data}).install(t)
	old := openNormalInflux
	t.Cleanup(func() { openNormalInflux = old })
	openNormalInflux = func(string) (normalInflux, error) { return testInflux, nil }

	startUpOnce(s, "settings.yaml")

	raw, felt := storedFigures(t, s, rec.ID)
	if !raw.Valid || !felt.Valid || felt.Float64 <= 0 {
		t.Fatalf("after start-up the passage should hold a felt figure from the filled points: stats %v felt %v", raw.Valid, felt)
	}
}

func TestStartUpPassBringsEveryStalePassageToTheCurrentVersionAndSurvivesAFailure(t *testing.T) {
	store, recs := feltPassages(t)
	if _, err := store.PassageFiguresFor(recs[0]); err != nil {
		t.Fatal(err)
	}
	old := `{"version":2,"stats":{}}`
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, old, recs[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = NULL WHERE id = ?`, recs[2].ID); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	var tried []string
	work := func(r passageRecord) (passageFigures, error) {
		tried = append(tried, r.ID)
		if r.ID == recs[2].ID {
			return passageFigures{}, errors.New("disk on fire")
		}
		return store.PassageFiguresFor(r)
	}
	recomputed, failed := recomputeStalePassages(context.Background(), store, work)
	if recomputed != 1 || failed != 1 {
		t.Fatalf("recomputed %d failed %d, want 1 and 1", recomputed, failed)
	}
	if len(tried) != 2 {
		t.Errorf("tried %v: the current passage should be left alone and the failure should not stop the rest", tried)
	}
	if _, felt := storedFigures(t, store, recs[1].ID); !felt.Valid {
		t.Error("the stale passage was not brought to the current version")
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("want one summary line, got %d: %q", len(lines), buf.String())
	}
	for _, want := range []string{"recomputed 1", "1 failed", "disk on fire", recs[2].ID} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("summary %q lacks %q", lines[0], want)
		}
	}
}

func staleOnlyPassages(t *testing.T, n int) *passageStore {
	t.Helper()
	store := withPassageStore(t)
	for i := 0; i < n; i++ {
		start := passageEpoch.Add(time.Duration(i) * 24 * time.Hour)
		if _, err := store.InsertPassage(passageRecord{Start: start, End: start.Add(time.Hour), Title: "p"}); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestStartUpRecomputeStopsWhenTheContextIsCancelledAndSaysHowManyWereLeft(t *testing.T) {
	store := staleOnlyPassages(t, 4)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	work := func(r passageRecord) (passageFigures, error) {
		calls++
		cancel()
		return passageFigures{}, nil
	}
	recomputeStalePassages(ctx, store, work)
	if calls != 1 {
		t.Fatalf("worked on %d passages after the context was cancelled, want 1", calls)
	}
	line := buf.String()
	for _, want := range []string{"stopped early", "3 left"} {
		if !strings.Contains(line, want) {
			t.Errorf("summary %q lacks %q", line, want)
		}
	}
}

func TestStartUpRecomputeCapsTheFailuresItListsInOneLine(t *testing.T) {
	store := staleOnlyPassages(t, 9)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	work := func(r passageRecord) (passageFigures, error) { return passageFigures{}, errors.New("disk on fire") }
	_, failed := recomputeStalePassages(context.Background(), store, work)
	if failed != 9 {
		t.Fatalf("failed %d, want 9", failed)
	}
	line := buf.String()
	if got := strings.Count(line, "disk on fire"); got != 5 {
		t.Errorf("line lists %d failures, want 5: %q", got, line)
	}
	if !strings.Contains(line, "and 4 more") || !strings.Contains(line, "9 failed") {
		t.Errorf("line %q should count all 9 and say 'and 4 more'", line)
	}
}

// ── the request path runs no scan ────────────────────────────────────────

func TestMalformedStatsOnOnePassageStopsNeitherTheListNorAnotherPage(t *testing.T) {
	store, recs := feltPassages(t)
	withHelmHeight(t, nil)
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = '{not json' WHERE id = ?`, recs[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ComfortBands(); err != nil {
		t.Fatalf("bands: %v", err)
	}
	code, body := getJSON(t, listPassagesHandler, "/api/passages", "")
	if code != 200 || len(body["passages"].([]any)) != 3 {
		t.Fatalf("list %d %v", code, body)
	}
	code, body = getJSON(t, getPassageHandler, "/api/passages/x", recs[1].ID)
	if code != 200 {
		t.Fatalf("page %d %v", code, body)
	}
}

// breakBands makes the bands query fail after every passage has stored figures.
func breakBands(t *testing.T, store *passageStore) {
	t.Helper()
	if _, err := store.db.Exec(`ALTER TABLE passages DROP COLUMN felt_deg`); err != nil {
		t.Fatal(err)
	}
}

func TestABandsFailureIsAReasonOnThePageAndTheListNotA500(t *testing.T) {
	store, recs := feltPassages(t)
	withHelmHeight(t, nil)
	if code, _ := getJSON(t, listPassagesHandler, "/api/passages", ""); code != 200 {
		t.Fatal(code)
	}
	breakBands(t, store)
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	code, body := getJSON(t, getPassageHandler, "/api/passages/x", recs[1].ID)
	if code != 200 {
		t.Fatalf("page %d %v", code, body)
	}
	felt := body["passage"].(map[string]any)["felt"].(map[string]any)
	bands := felt["bands"].(map[string]any)
	if bands["learned"] != false || bands["reason"] == nil || bands["reason"] == "" {
		t.Errorf("bands %v", bands)
	}
	if felt["deg"] == nil {
		t.Error("the felt figure is still shown")
	}
	code, body = getJSON(t, listPassagesHandler, "/api/passages", "")
	if code != 200 {
		t.Fatalf("list %d %v", code, body)
	}
	for _, p := range body["passages"].([]any) {
		m := p.(map[string]any)
		if m["felt_band"] != nil || m["felt_hint"] == nil || m["felt_hint"] == "" {
			t.Errorf("row %v", m)
		}
	}
	if !strings.Contains(buf.String(), "felt_deg") {
		t.Errorf("the error should be logged: %q", buf.String())
	}
}

// ── a pointless passage is never stored with blank reasons ──────────────

func TestAPassageWithNoPointsAlwaysCarriesReasons(t *testing.T) {
	s := computePassageStats(nil)
	if s.FeltDeg != nil || s.FeltReason == "" || s.SnapReason == "" || s.PoundingReason == "" || s.FeltSpells == nil {
		t.Errorf("no points: %+v", s)
	}

	store := withPassageStore(t)
	rec, err := store.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(time.Hour), Title: "empty"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := store.PassageFiguresFor(rec)
	if err != nil {
		t.Fatal(err)
	}
	if f.Stats.FeltReason == "" || f.Stats.SnapReason == "" || f.Stats.PoundingReason == "" {
		t.Errorf("uncached: %+v", f.Stats)
	}

	// The cached branch: stats kept from an older shape, and no points to rework.
	cachedRec, err := store.InsertPassage(passageRecord{Start: passageEpoch.Add(48 * time.Hour), End: passageEpoch.Add(49 * time.Hour), Title: "cached"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE passages SET stats_json = ? WHERE id = ?`, `{"version":2,"stats":{"distance_nm":4},"start_lon":149}`, cachedRec.ID); err != nil {
		t.Fatal(err)
	}
	f, err = store.PassageFiguresFor(cachedRec)
	if err != nil {
		t.Fatal(err)
	}
	if f.Stats.DistanceNM != 4 || f.Stats.FeltReason == "" || f.Stats.SnapReason == "" || f.Stats.PoundingReason == "" {
		t.Errorf("cached: %+v", f.Stats)
	}
	if f.StartLon != nil {
		t.Errorf("a longitude with no latitude should be dropped: %v", *f.StartLon)
	}
}

// ── each figure has its own reason (redesign B) ─────────────────────────

func withPoints(mutate func(p *recordedTrackPoint, i int)) []recordedTrackPoint {
	pts := feltRun()
	for i := 1; i <= 60; i++ {
		mutate(&pts[i], i)
	}
	return pts
}

func TestEachFigureSaysWhyItIsMissingFromItsOwnInputs(t *testing.T) {
	roll := func(p *recordedTrackPoint) { p.RollRMSDeg, p.PitchRMSDeg = pfloat(1), pfloat(1) }
	cases := []struct {
		name     string
		pts      []recordedTrackPoint
		felt     string // a substring, or "" when the felt figure should exist
		snap     string
		pounding string
	}{
		{"imported", withPoints(func(p *recordedTrackPoint, _ int) { p.Source = trackSourceImported }),
			"imported", "imported", "imported"},
		{"no motion at all", feltRun(),
			"No motion was recorded", "No motion was recorded", "No heave was recorded"},
		{"roll peaks from before sway was kept", withPoints(func(p *recordedTrackPoint, _ int) { p.RollPeakDeg = pfloat(5) }),
			"before sway was kept", "before 10 Hz capture", "No heave was recorded"},
		{"sway filled from history, no rate", withPoints(func(p *recordedTrackPoint, _ int) { roll(p) }),
			"", "filled from history", "No heave was recorded"},
		{"slow attitude feed and no heave", withPoints(func(p *recordedTrackPoint, _ int) { roll(p); p.MotionRateHz = pfloat(4) }),
			"", "below 8 Hz", "No heave was recorded"},
		{"10 Hz attitude feed with no acceleration", withPoints(func(p *recordedTrackPoint, _ int) { roll(p); p.MotionRateHz = pfloat(10) }),
			"", "no point has snap", "No heave was recorded"},
		{"slow heave", withPoints(func(p *recordedTrackPoint, _ int) { p.HeaveRateHz = pfloat(4); p.HeaveSDM = pfloat(0.2) }),
			"No motion was recorded", "No motion was recorded", "Heave arrived below 8 Hz"},
		{"10 Hz heave with no vertical acceleration", withPoints(func(p *recordedTrackPoint, _ int) { p.HeaveRateHz = pfloat(10); p.HeaveSDM = pfloat(0.2) }),
			"No motion was recorded", "No motion was recorded", "no point has pounding"},
		{"heave spread with no rate and no roll acceleration", withPoints(func(p *recordedTrackPoint, _ int) { roll(p); p.MotionRateHz = pfloat(1); p.HeaveSDM = pfloat(0.2) }),
			"", "below 8 Hz", "filled from history"},
		{"heave peaks from before 10 Hz capture", withPoints(func(p *recordedTrackPoint, _ int) { p.HeavePeakM = pfloat(0.5) }),
			"No motion was recorded", "No motion was recorded", "heave peaks but no pounding"},
		{"roll sway only", withPoints(func(p *recordedTrackPoint, _ int) { p.RollRMSDeg = pfloat(1) }),
			"Only roll was recorded", "filled from history", "No heave was recorded"},
		{"pitch sway only", withPoints(func(p *recordedTrackPoint, _ int) { p.PitchRMSDeg = pfloat(1) }),
			"Only pitch was recorded", "filled from history", "No heave was recorded"},
		{"under five minutes of sway", func() []recordedTrackPoint { p := feltRun(); setSway(p, 1, 4, 1, 1); return p }(),
			"five minutes", "filled from history", "No heave was recorded"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			st := computePassageStats(c.pts)
			f := buildPassageFelt(st, passageSidewaysG{}, comfortBands{}, nil)
			if c.felt == "" {
				if st.FeltDeg == nil {
					t.Errorf("felt: want a figure, got reason %q", st.FeltReason)
				}
			} else if st.FeltDeg != nil || !strings.Contains(st.FeltReason, c.felt) {
				t.Errorf("felt reason %q, want %q", st.FeltReason, c.felt)
			}
			if f.Snap.Value != nil || !strings.Contains(f.Snap.Reason, c.snap) {
				t.Errorf("snap reason %q, want %q", f.Snap.Reason, c.snap)
			}
			if f.Pounding.ValueG != nil || !strings.Contains(f.Pounding.Reason, c.pounding) {
				t.Errorf("pounding reason %q, want %q", f.Pounding.Reason, c.pounding)
			}
			for _, r := range []string{st.FeltReason, f.Snap.Reason, f.Pounding.Reason} {
				if strings.Contains(r, "{figure}") {
					t.Errorf("unfilled placeholder: %q", r)
				}
			}
			if f.Snap.Reason == "" || f.Pounding.Reason == "" {
				t.Errorf("a missing figure carries no reason: %q %q", f.Snap.Reason, f.Pounding.Reason)
			}
		})
	}
}

func TestASlowAttitudeFeedDoesNotExplainAMissingHeave(t *testing.T) {
	pts := withPoints(func(p *recordedTrackPoint, _ int) { p.MotionRateHz = pfloat(4) })
	f := buildPassageFelt(computePassageStats(pts), passageSidewaysG{}, comfortBands{}, nil)
	if strings.Contains(f.Pounding.Reason, "8 Hz") {
		t.Errorf("pounding reason %q blames a feed it does not use", f.Pounding.Reason)
	}
}

func TestSnapReasonDoesNotSurviveOnTheWire(t *testing.T) {
	st := computePassageStats(feltRun()).forWire()
	if st.SnapReason != "" || st.PoundingReason != "" {
		t.Errorf("wire stats carry %q %q", st.SnapReason, st.PoundingReason)
	}
}
