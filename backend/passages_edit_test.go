package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedPassage(t *testing.T, s *passageStore, from, to time.Time) passageRecord {
	t.Helper()
	r, err := s.InsertPassage(passageRecord{Start: from, End: to, FromName: "A", ToName: "B", Title: "T", Notes: "n"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ptr[T any](v T) *T { return &v }

func TestPatchPassageMarksEditedAndAppliesOnlyGivenFields(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedPassage(t, s, passageEpoch, passageEpoch.Add(time.Hour))
	got, err := s.PatchPassage(p.ID, passagePatch{Title: ptr("New"), Hidden: ptr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "New" || !got.Hidden || !got.Edited || got.Notes != "n" || got.FromName != "A" {
		t.Errorf("%+v", got)
	}
}

func TestPatchPassageCruiseMustExistAndNullUnfiles(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedPassage(t, s, passageEpoch, passageEpoch.Add(time.Hour))
	if _, err := s.PatchPassage(p.ID, passagePatch{CruiseID: ptr("nope"), CruiseIDSet: true}); !errors.Is(err, errCruiseNotFound) {
		t.Errorf("err %v", err)
	}
	c, _ := s.InsertCruise(cruiseRecord{Name: "Whitsundays"})
	got, err := s.PatchPassage(p.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})
	if err != nil || got.CruiseID == nil || *got.CruiseID != c.ID {
		t.Fatalf("%+v %v", got, err)
	}
	got, err = s.PatchPassage(p.ID, passagePatch{CruiseIDSet: true})
	if err != nil || got.CruiseID != nil {
		t.Fatalf("null must unfile: %+v %v", got, err)
	}
}

func TestPatchPassageRejectsOversizeText(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedPassage(t, s, passageEpoch, passageEpoch.Add(time.Hour))
	if _, err := s.PatchPassage(p.ID, passagePatch{Title: ptr(strings.Repeat("x", 300))}); !errors.Is(err, errPassageInvalid) {
		t.Errorf("err %v", err)
	}
}

func TestSplitPassage(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedPassage(t, s, passageEpoch, passageEpoch.Add(4*time.Hour))
	at := passageEpoch.Add(time.Hour)
	a, b, err := s.SplitPassage(p.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != p.ID || !a.End.Equal(at) || !b.Start.Equal(at) || !b.End.Equal(p.End) {
		t.Errorf("%+v %+v", a, b)
	}
	if a.ToName != "" || b.FromName != "" || a.FromName != "A" || b.ToName != "B" {
		t.Errorf("names: %+v %+v", a, b)
	}
	if !a.Edited || !b.Edited {
		t.Error("both halves are edited")
	}
	if a.Title != "T" || b.Title != "" {
		t.Errorf("titles %q %q: the second half starts untitled so the page shows From to To", a.Title, b.Title)
	}
	stored, _ := s.GetPassage(b.ID)
	if stored.Title != "" {
		t.Errorf("stored second half title %q", stored.Title)
	}
	for _, bad := range []time.Time{passageEpoch, p.End, passageEpoch.Add(-time.Hour), p.End.Add(time.Hour)} {
		if _, _, err := s.SplitPassage(b.ID, bad); !errors.Is(err, errPassageInvalid) {
			t.Errorf("split at %v: %v", bad, err)
		}
	}
}

func TestMergePassagesAdjacentOnly(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedPassage(t, s, passageEpoch, passageEpoch.Add(time.Hour))
	b := seedPassage(t, s, passageEpoch.Add(2*time.Hour), passageEpoch.Add(3*time.Hour))
	c := seedPassage(t, s, passageEpoch.Add(4*time.Hour), passageEpoch.Add(5*time.Hour))
	if _, err := s.MergePassages(a.ID, c.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("a and c have b between them: %v", err)
	}
	m, err := s.MergePassages(b.ID, a.ID) // order given does not matter
	if err != nil {
		t.Fatal(err)
	}
	if !m.Start.Equal(a.Start) || !m.End.Equal(b.End) || !m.Edited {
		t.Errorf("%+v", m)
	}
	if _, err := s.GetPassage(b.ID); !errors.Is(err, errPassageNotFound) && m.ID != b.ID {
		t.Error("the later record must be gone")
	}
	if list, _ := s.ListPassages(); len(list) != 2 {
		t.Errorf("passages %d", len(list))
	}
}

func TestMergeRefusesHiddenAndDifferentCruises(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedPassage(t, s, passageEpoch, passageEpoch.Add(time.Hour))
	b := seedPassage(t, s, passageEpoch.Add(2*time.Hour), passageEpoch.Add(3*time.Hour))
	c1, _ := s.InsertCruise(cruiseRecord{Name: "1"})
	c2, _ := s.InsertCruise(cruiseRecord{Name: "2"})
	s.PatchPassage(a.ID, passagePatch{CruiseID: &c1.ID, CruiseIDSet: true})
	s.PatchPassage(b.ID, passagePatch{CruiseID: &c2.ID, CruiseIDSet: true})
	if _, err := s.MergePassages(a.ID, b.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("different cruises: %v", err)
	}
	s.PatchPassage(b.ID, passagePatch{CruiseID: &c1.ID, CruiseIDSet: true, Hidden: ptr(true)})
	if _, err := s.MergePassages(a.ID, b.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("hidden: %v", err)
	}
}

func TestTrimPassageWithinRangeOnly(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedPassage(t, s, passageEpoch, passageEpoch.Add(4*time.Hour))
	got, err := s.TrimPassage(p.ID, passageEpoch.Add(time.Hour), passageEpoch.Add(3*time.Hour))
	if err != nil || !got.Start.Equal(passageEpoch.Add(time.Hour)) || !got.End.Equal(passageEpoch.Add(3*time.Hour)) || !got.Edited {
		t.Fatalf("%+v %v", got, err)
	}
	cases := [][2]time.Time{
		{passageEpoch, passageEpoch.Add(3 * time.Hour)},                              // earlier than the current start
		{passageEpoch.Add(time.Hour), passageEpoch.Add(5 * time.Hour)},               // later than the current end
		{passageEpoch.Add(3 * time.Hour), passageEpoch.Add(time.Hour + time.Minute)}, // inverted
		{passageEpoch.Add(2 * time.Hour), passageEpoch.Add(2 * time.Hour)},           // empty
	}
	for _, c := range cases {
		if _, err := s.TrimPassage(p.ID, c[0], c[1]); !errors.Is(err, errPassageInvalid) {
			t.Errorf("trim %v..%v: %v", c[0], c[1], err)
		}
	}
}

func TestCruiseCRUDAndDeleteUnfilesButKeepsPassages(t *testing.T) {
	s := newTestPassageStore(t)
	c, err := s.InsertCruise(cruiseRecord{Name: "Winter"})
	if err != nil {
		t.Fatal(err)
	}
	p := seedPassage(t, s, passageEpoch, passageEpoch.Add(time.Hour))
	s.PatchPassage(p.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true})
	got, err := s.PatchCruise(c.ID, cruisePatch{Name: ptr("Summer"), Notes: ptr("with friends")})
	if err != nil || got.Name != "Summer" || got.Notes != "with friends" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := s.PatchCruise(c.ID, cruisePatch{Name: ptr("  ")}); !errors.Is(err, errPassageInvalid) {
		t.Errorf("blank name: %v", err)
	}
	if err := s.DeleteCruise(c.ID); err != nil {
		t.Fatal(err)
	}
	kept, err := s.GetPassage(p.ID)
	if err != nil || kept.CruiseID != nil || !kept.Edited {
		t.Errorf("passage must survive unfiled and edited: %+v %v", kept, err)
	}
	if err := s.DeleteCruise(c.ID); !errors.Is(err, errCruiseNotFound) {
		t.Errorf("second delete: %v", err)
	}
	if cs, _ := s.ListCruises(); len(cs) != 0 {
		t.Errorf("cruises %d", len(cs))
	}
}

func TestEditedPassageIsNotRecreatedByDetectorAfterTrim(t *testing.T) {
	s := newTestPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, s, b.pts)
	runPassageDetection(s)
	list, _ := s.ListPassages()
	if len(list) != 1 {
		t.Fatal(len(list))
	}
	s.TrimPassage(list[0].ID, list[0].Start.Add(10*time.Minute), list[0].End)
	// force a rerun over the same points
	s.SetDetectorResume(b.pts[0].At)
	runPassageDetection(s)
	if list, _ := s.ListPassages(); len(list) != 1 {
		t.Errorf("detector re-created a trimmed passage: %d", len(list))
	}
}

func TestSplitHalvesInheritTheSourceAndASameSourceMergeKeepsIt(t *testing.T) {
	s := newTestPassageStore(t)
	a, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(4 * time.Hour), Source: trackSourceImported})
	first, second, err := s.SplitPassage(a.ID, passageEpoch.Add(time.Hour))
	if err != nil || first.Source != trackSourceImported || second.Source != trackSourceImported {
		t.Fatalf("%+v %+v %v", first, second, err)
	}
	if got, _ := s.GetPassage(second.ID); got.Source != trackSourceImported {
		t.Fatalf("stored second half: %+v", got)
	}
	m, err := s.MergePassages(first.ID, second.ID)
	if err != nil || m.Source != trackSourceImported {
		t.Fatalf("merge of one source: %+v %v", m, err)
	}

}

func TestMergeKeepsASharedSourceAndForgetsADisagreement(t *testing.T) {
	s := newTestPassageStore(t)
	h := func(n int) time.Time { return passageEpoch.Add(time.Duration(n) * time.Hour) }
	add := func(from int, src string) passageRecord {
		r, err := s.InsertPassage(passageRecord{Start: h(from), End: h(from + 1), Source: src})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	// All recorded: kept. All unknown: stays unknown.
	a, b := add(0, trackSourceRecorded), add(2, trackSourceRecorded)
	if m, err := s.MergePassages(a.ID, b.ID); err != nil || m.Source != trackSourceRecorded {
		t.Fatalf("shared source: %+v %v", m, err)
	}
	c, d := add(4, ""), add(6, "")
	if m, err := s.MergePassages(c.ID, d.ID); err != nil || m.Source != "" {
		t.Fatalf("both unknown: %+v %v", m, err)
	}
	// Different sources, or one unknown: the merge goes ahead and the result is unknown.
	e, f, g := add(8, trackSourceRecorded), add(10, trackSourceImported), add(12, trackSourceRecorded)
	m, err := s.MergePassages(e.ID, f.ID, g.ID)
	if err != nil || m.Source != "" {
		t.Fatalf("mixed: %+v %v", m, err)
	}
	if got, _ := s.GetPassage(m.ID); got.Source != "" || got.End != h(13).UTC() {
		t.Fatalf("stored: %+v", got)
	}
	x, y := add(14, ""), add(16, trackSourceRecorded)
	if m, err := s.MergePassages(x.ID, y.ID); err != nil || m.Source != "" {
		t.Fatalf("old with new: %+v %v", m, err)
	}
}

func TestBackfillPassageSourcesOnlyWhereTheTrackSaysExactly(t *testing.T) {
	s := newTestPassageStore(t)
	mk := func(h int, src string) passageRecord {
		r, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(time.Duration(h) * time.Hour), End: passageEpoch.Add(time.Duration(h)*time.Hour + 30*time.Minute)})
		if src != "" {
			s.AppendTrackPoint(recordedTrackPoint{At: r.Start.Add(time.Minute), Lat: -20, Lon: 149, Source: src})
		}
		return r
	}
	onlyRec, onlyImp, none := mk(0, trackSourceRecorded), mk(2, trackSourceImported), mk(4, "")
	both := mk(6, trackSourceRecorded)
	s.AppendTrackPoint(recordedTrackPoint{At: both.Start.Add(2 * time.Minute), Lat: -20, Lon: 149, Source: trackSourceImported})
	if err := s.backfillPassageSources(); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{onlyRec.ID: trackSourceRecorded, onlyImp.ID: trackSourceImported, none.ID: "", both.ID: ""} {
		if got, _ := s.GetPassage(id); got.Source != want {
			t.Errorf("%s: source %q, want %q", id, got.Source, want)
		}
	}
}

func TestBackfillRunsOnEveryOpenAndLeavesMixedSpansUnknown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.sqlite")
	s, err := newPassageStore(path)
	if err != nil {
		t.Fatal(err)
	}
	later, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(time.Hour)})
	mixed, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(3 * time.Hour), End: passageEpoch.Add(4 * time.Hour)})
	s.AppendTrackPoint(recordedTrackPoint{At: mixed.Start.Add(time.Minute), Lat: -20, Lon: 149, Source: trackSourceRecorded})
	s.AppendTrackPoint(recordedTrackPoint{At: mixed.Start.Add(2 * time.Minute), Lat: -20, Lon: 149, Source: trackSourceImported})
	s.close()

	s, err = newPassageStore(path) // first open: nothing to name later yet, mixed stays mixed
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPassage(later.ID); got.Source != "" {
		t.Fatalf("no points yet: %q", got.Source)
	}
	s.AppendTrackPoint(recordedTrackPoint{At: later.Start.Add(time.Minute), Lat: -20, Lon: 149, Source: trackSourceImported})
	s.close()

	s, err = newPassageStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.close()
	if got, _ := s.GetPassage(later.ID); got.Source != trackSourceImported {
		t.Fatalf("a later open fills what the first could not: %q", got.Source)
	}
	if got, _ := s.GetPassage(mixed.ID); got.Source != "" {
		t.Fatalf("a genuinely mixed span stays unknown: %q", got.Source)
	}
}
