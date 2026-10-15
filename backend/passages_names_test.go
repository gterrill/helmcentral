package main

import (
	"testing"
	"time"
)

func TestFillPassageNamesNamesBothEnds(t *testing.T) {
	s := newTestPassageStore(t)
	b := simpleTrip(2 * time.Hour)
	appendAll(t, s, b.pts)
	runPassageDetection(s)
	list, _ := s.ListPassages()

	var asked [][2]float64
	err := s.FillPassageNames(list[0].ID, func(lat, lon float64) string {
		asked = append(asked, [2]float64{lat, lon})
		if lat < -19.9 {
			return "Start Bay"
		}
		return "End Cove"
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPassage(list[0].ID)
	if got.FromName != "Start Bay" || got.ToName != "End Cove" {
		t.Errorf("%+v", got)
	}
	if got.Edited {
		t.Error("filling names is not an operator edit")
	}
	if len(asked) != 2 {
		t.Errorf("lookups %d", len(asked))
	}
}

func TestFillPassageNamesLeavesEditedAndUnnamedAlone(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	edited, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour), Edited: true, FromNameEdited: true, ToNameEdited: true})
	calls := 0
	s.FillPassageNames(edited.ID, func(float64, float64) string { calls++; return "X" })
	got, _ := s.GetPassage(edited.ID)
	if got.FromName != "" || got.ToName != "" || calls != 0 {
		t.Errorf("names the operator edited must not be touched: %+v (%d lookups)", got, calls)
	}

	plain, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(3 * time.Hour), End: passageEpoch.Add(4 * time.Hour), FromName: "Mine"})
	appendAll(t, s, []recordedTrackPoint{
		{At: passageEpoch.Add(3 * time.Hour), Lat: -20, Lon: 149, Segment: 50},
		{At: passageEpoch.Add(4 * time.Hour), Lat: -20.1, Lon: 149, Segment: 50},
	})
	s.FillPassageNames(plain.ID, func(float64, float64) string { return "" })
	got, _ = s.GetPassage(plain.ID)
	if got.FromName != "Mine" || got.ToName != "" {
		t.Errorf("no plugin answer leaves names empty and keeps what is there: %+v", got)
	}
	s.FillPassageNames(plain.ID, func(float64, float64) string { return "Other" })
	got, _ = s.GetPassage(plain.ID)
	if got.FromName != "Mine" || got.ToName != "Other" {
		t.Errorf("an existing name is never overwritten: %+v", got)
	}
}

func TestFillPassageNamesDoesNotClobberOperatorEditMadeDuringLookup(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour)})
	s.FillPassageNames(p.ID, func(float64, float64) string {
		// the operator renames while the plugin is still answering
		s.PatchPassage(p.ID, passagePatch{FromName: pstr("Home"), ToName: pstr("Away")})
		return "Plugin"
	})
	got, _ := s.GetPassage(p.ID)
	if got.FromName != "Home" || got.ToName != "Away" {
		t.Errorf("%+v", got)
	}
}

func TestRunnerAndImportWakeTheNamingWorker(t *testing.T) {
	wakes := 0
	prev := passageNamesWake
	passageNamesWake = func() { wakes++ }
	defer func() { passageNamesWake = prev }()

	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	runPassageDetection(s)
	if wakes != 1 {
		t.Fatalf("runner woke %d", wakes)
	}
	s2 := newTestPassageStore(t)
	s2.ImportGPX(importTrip(simpleTrip(2 * time.Hour).pts))
	if wakes != 2 {
		t.Errorf("import woke %d total", wakes)
	}
}

func TestNamingScanFindsOnlyUnnamedUneditedUnattempted(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	mk := func(r passageRecord) string {
		r.Start, r.End = passageEpoch.Add(20*time.Minute), passageEpoch.Add(2*time.Hour)
		got, err := s.InsertPassage(r)
		if err != nil {
			t.Fatal(err)
		}
		return got.ID
	}
	need := mk(passageRecord{})
	half := mk(passageRecord{FromName: "A"})
	mk(passageRecord{FromName: "A", ToName: "B"})
	mk(passageRecord{Edited: true, FromNameEdited: true, ToNameEdited: true})

	now := time.Now()
	ids, err := s.PassagesNeedingNames(now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || !(idsContain(ids, need) && idsContain(ids, half)) {
		t.Fatalf("ids %v", ids)
	}

	// a failed lookup is not retried until the retry window passes
	s.FillPassageNames(need, func(float64, float64) string { return "" })
	ids, _ = s.PassagesNeedingNames(now.Add(time.Minute), 24*time.Hour)
	if idsContain(ids, need) {
		t.Error("attempted passage came straight back")
	}
	ids, _ = s.PassagesNeedingNames(now.Add(25*time.Hour), 24*time.Hour)
	if !idsContain(ids, need) {
		t.Error("attempted passage should be retried after 24 h")
	}
}

func idsContain(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func TestNamingWorkerNamesBacklogOnStartAndOnWake(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	// a passage stored before the worker existed, as after a restart
	old, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour)})

	done := make(chan struct{})
	w := newNamingWorker(s, func(float64, float64) string { return "Somewhere" }, time.Hour)
	go func() { w.run(done) }()
	defer close(done)

	waitUntilTrue(t, func() bool {
		p, _ := s.GetPassage(old.ID)
		return p.FromName == "Somewhere" && p.ToName == "Somewhere"
	})

	later, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(90 * time.Minute)})
	w.wake()
	w.wake() // coalesces
	waitUntilTrue(t, func() bool {
		p, _ := s.GetPassage(later.ID)
		return p.FromName == "Somewhere"
	})
}

func waitUntilTrue(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

// Review finding 4: filing or hiding a passage must not stop it being named;
// only typing a name protects that end.
func TestFilingAPassageDoesNotStopNaming(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour)})
	c, _ := s.InsertCruise(cruiseRecord{Name: "C"})
	s.PatchPassage(p.ID, passagePatch{CruiseID: &c.ID, CruiseIDSet: true, Hidden: ptr(false)})

	ids, _ := s.PassagesNeedingNames(time.Now(), 24*time.Hour)
	if !idsContain(ids, p.ID) {
		t.Fatal("a filed passage still needs names")
	}
	s.FillPassageNames(p.ID, func(float64, float64) string { return "Named" })
	got, _ := s.GetPassage(p.ID)
	if got.FromName != "Named" || got.ToName != "Named" || !got.Edited {
		t.Errorf("%+v", got)
	}
}

func TestTypingOneNameProtectsOnlyThatEnd(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour)})
	s.PatchPassage(p.ID, passagePatch{FromName: pstr("Home")})
	s.FillPassageNames(p.ID, func(float64, float64) string { return "Plugin" })
	got, _ := s.GetPassage(p.ID)
	if got.FromName != "Home" || got.ToName != "Plugin" || !got.FromNameEdited || got.ToNameEdited {
		t.Errorf("%+v", got)
	}
	// clearing a name the operator chose keeps it protected
	s.PatchPassage(p.ID, passagePatch{FromName: pstr("")})
	s.FillPassageNames(p.ID, func(float64, float64) string { return "Plugin" })
	if got, _ := s.GetPassage(p.ID); got.FromName != "" {
		t.Errorf("an operator-cleared name was refilled: %+v", got)
	}
}

func TestSplitAndMergeCarryNameProtection(t *testing.T) {
	s := newTestPassageStore(t)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(4 * time.Hour), FromName: "A", ToName: "B", FromNameEdited: true, ToNameEdited: true})
	a, b, err := s.SplitPassage(p.ID, passageEpoch.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !a.FromNameEdited || a.ToNameEdited || b.FromNameEdited || !b.ToNameEdited {
		t.Errorf("split: %+v %+v", a, b)
	}
	m, err := s.MergePassages(a.ID, b.ID)
	if err != nil || !m.FromNameEdited || !m.ToNameEdited {
		t.Errorf("merge: %+v %v", m, err)
	}
	stored, _ := s.GetPassage(m.ID)
	if !stored.FromNameEdited || !stored.ToNameEdited {
		t.Errorf("stored: %+v", stored)
	}
}

// Review finding 7: the name lookups must stay inside the passage's span.
func TestFillPassageNamesStaysInsideTheSpan(t *testing.T) {
	s := newTestPassageStore(t)
	// points exist only well before and well after the passage
	appendAll(t, s, []recordedTrackPoint{
		{At: passageEpoch, Lat: -20, Lon: 149, Segment: 1},
		{At: passageEpoch.Add(10 * time.Hour), Lat: -21, Lon: 149, Segment: 2},
	})
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(3 * time.Hour), End: passageEpoch.Add(4 * time.Hour)})
	calls := 0
	if err := s.FillPassageNames(p.ID, func(float64, float64) string { calls++; return "Wrong Place" }); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetPassage(p.ID)
	if calls != 0 || got.FromName != "" || got.ToName != "" {
		t.Errorf("no points in the span must leave names empty without asking: %+v (%d lookups)", got, calls)
	}
}

func TestTrimClearsAndRequeuesMovedEndsNotOperatorNamed(t *testing.T) {
	wakes := 0
	prev := passageNamesWake
	passageNamesWake = func() { wakes++ }
	defer func() { passageNamesWake = prev }()

	s := newTestPassageStore(t)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(4 * time.Hour), FromName: "Old A", ToName: "Old B"})
	s.FillPassageNames(p.ID, func(float64, float64) string { return "" }) // stamps an attempt

	// only the start moves
	got, err := s.TrimPassage(p.ID, passageEpoch.Add(time.Hour), p.End)
	if err != nil {
		t.Fatal(err)
	}
	if got.FromName != "" || got.ToName != "Old B" {
		t.Errorf("only the moved end clears: %+v", got)
	}
	stored, _ := s.GetPassage(p.ID)
	if stored.FromName != "" || stored.ToName != "Old B" {
		t.Errorf("stored %+v", stored)
	}
	ids, _ := s.PassagesNeedingNames(time.Now(), 24*time.Hour)
	if !idsContain(ids, p.ID) {
		t.Error("the cleared end must be queued again despite the earlier attempt")
	}
	if wakes != 1 {
		t.Errorf("wakes %d", wakes)
	}

	// a name the operator typed is never cleared, even when its end moves
	q, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(4 * time.Hour), FromName: "Mine", ToName: "Plugin", FromNameEdited: true})
	got, _ = s.TrimPassage(q.ID, passageEpoch.Add(time.Hour), passageEpoch.Add(3*time.Hour))
	if got.FromName != "Mine" || got.ToName != "" {
		t.Errorf("%+v", got)
	}

	// nothing moved on that end, nothing cleared
	r, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(4 * time.Hour), FromName: "A", ToName: "B"})
	wakes = 0
	got, _ = s.TrimPassage(r.ID, r.Start, r.End)
	if got.FromName != "A" || got.ToName != "B" || wakes != 0 {
		t.Errorf("%+v wakes %d", got, wakes)
	}
}

func TestSplitRequeuesTheFirstHalfAndWakesTheNamer(t *testing.T) {
	wakes := 0
	prev := passageNamesWake
	passageNamesWake = func() { wakes++ }
	defer func() { passageNamesWake = prev }()

	s := newTestPassageStore(t)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch, End: passageEpoch.Add(4 * time.Hour), FromName: "A", ToName: "B"})
	s.FillPassageNames(p.ID, func(float64, float64) string { return "" }) // stamps an attempt

	first, second, err := s.SplitPassage(p.ID, passageEpoch.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	ids, _ := s.PassagesNeedingNames(time.Now(), 24*time.Hour)
	if !idsContain(ids, first.ID) {
		t.Error("the first half's cleared destination must be queued again despite the earlier attempt")
	}
	if !idsContain(ids, second.ID) {
		t.Error("the second half's empty origin must be queued")
	}
	if wakes != 1 {
		t.Errorf("wakes %d, want 1", wakes)
	}
}

// A naming worker that read the passage before a split or trim must not write
// the old end's place into the edited passage: the edit clears the attempt
// stamp the worker set, its write no-ops, and the passage stays queued.
func TestStaleNamingWorkerDoesNotNameAnEditedEnd(t *testing.T) {
	prev := passageNamesWake
	passageNamesWake = nil
	defer func() { passageNamesWake = prev }()

	for _, edit := range []string{"split", "trim"} {
		t.Run(edit, func(t *testing.T) {
			s := newTestPassageStore(t)
			trip := simpleTrip(4 * time.Hour)
			for _, p := range trip.pts {
				if err := s.AppendTrackPoint(p); err != nil {
					t.Fatal(err)
				}
			}
			start, end := trip.pts[0].At, trip.pts[len(trip.pts)-1].At
			p, _ := s.InsertPassage(passageRecord{Start: start, End: end, FromName: "Origin"})
			mid := start.Add(end.Sub(start) / 2)

			err := s.FillPassageNames(p.ID, func(float64, float64) string {
				// the operator edits the passage while the lookup is out
				var err error
				if edit == "split" {
					_, _, err = s.SplitPassage(p.ID, mid)
				} else {
					_, err = s.TrimPassage(p.ID, start, mid)
				}
				if err != nil {
					t.Fatal(err)
				}
				return "Old end place"
			})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetPassage(p.ID)
			if got.ToName != "" {
				t.Errorf("stale worker named the moved end %q", got.ToName)
			}
			ids, _ := s.PassagesNeedingNames(time.Now(), 24*time.Hour)
			if !idsContain(ids, p.ID) {
				t.Error("the edited passage must stay queued for naming")
			}
		})
	}
}
