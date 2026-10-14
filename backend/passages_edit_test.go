package main

import (
	"errors"
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
