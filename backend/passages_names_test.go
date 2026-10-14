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
	edited, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour), Edited: true})
	calls := 0
	s.FillPassageNames(edited.ID, func(float64, float64) string { calls++; return "X" })
	got, _ := s.GetPassage(edited.ID)
	if got.FromName != "" || got.ToName != "" || calls != 0 {
		t.Errorf("an edited passage must not be touched: %+v (%d lookups)", got, calls)
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
	mk(passageRecord{Edited: true})

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
