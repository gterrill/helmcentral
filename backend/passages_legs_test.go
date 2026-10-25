package main

import (
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// seedLeg stores a passage with the given names, hours after the epoch.
func seedLeg(t *testing.T, s *passageStore, fromH, toH int, from, to string) passageRecord {
	t.Helper()
	r, err := s.InsertPassage(passageRecord{
		Start: passageEpoch.Add(time.Duration(fromH) * time.Hour), End: passageEpoch.Add(time.Duration(toH) * time.Hour),
		FromName: from, ToName: to,
	})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMergeTwoLegsWritesOneJoinSign(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "Low Isles")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	m, err := s.MergePassages(b.ID, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	signs, err := s.ListSigns(0, 0)
	if err != nil || len(signs) != 1 {
		t.Fatalf("%+v %v", signs, err)
	}
	sg := signs[0]
	if sg.TMs != a.End.UnixMilli() || sg.Title != "Low Isles" || sg.Visibility != signAfter || sg.ShowS != signShowDef || !sg.StartsLeg {
		t.Errorf("%+v", sg)
	}
	if m.ID != a.ID || m.ToName != "Cooktown" {
		t.Errorf("%+v", m)
	}
}

func TestMergeJoinTitleFallsBackToLaterFromName(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	if _, err := s.MergePassages(a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	signs, _ := s.ListSigns(0, 0)
	if len(signs) != 1 || signs[0].Title != "Mossman" {
		t.Errorf("%+v", signs)
	}
}

func TestMergeJoinWithNoNamesIsUntitledAndWakesNaming(t *testing.T) {
	wakes := 0
	prev := passageNamesWake
	passageNamesWake = func() { wakes++ }
	defer func() { passageNamesWake = prev }()
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "", "")
	b := seedLeg(t, s, 2, 3, "", "")
	if _, err := s.MergePassages(a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	signs, _ := s.ListSigns(0, 0)
	if len(signs) != 1 || signs[0].Title != "" || !signs[0].StartsLeg {
		t.Errorf("%+v", signs)
	}
	if wakes != 1 {
		t.Errorf("woke %d", wakes)
	}
}

func TestMergeThreeLegsWritesTwoJoinSigns(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "A", "B")
	b := seedLeg(t, s, 2, 3, "B", "C")
	c := seedLeg(t, s, 4, 5, "C", "D")
	m, err := s.MergePassages(c.ID, a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != a.ID || !m.Start.Equal(a.Start) || !m.End.Equal(c.End) || m.ToName != "D" || m.FromName != "A" {
		t.Errorf("%+v", m)
	}
	signs, _ := s.ListSigns(0, 0)
	if len(signs) != 2 || signs[0].TMs != a.End.UnixMilli() || signs[0].Title != "B" || signs[1].TMs != b.End.UnixMilli() || signs[1].Title != "C" {
		t.Errorf("%+v", signs)
	}
	if list, _ := s.ListPassages(); len(list) != 1 {
		t.Errorf("passages %d", len(list))
	}
}

func TestMergeManyRefusalsLeaveNothingBehind(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "A", "B")
	b := seedLeg(t, s, 2, 3, "B", "C")
	c := seedLeg(t, s, 4, 5, "C", "D")
	if _, err := s.MergePassages(a.ID, c.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("not neighbours: %v", err)
	}
	if _, err := s.MergePassages(a.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("one id: %v", err)
	}
	if _, err := s.MergePassages(a.ID, b.ID, b.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("repeated id: %v", err)
	}
	c1, _ := s.InsertCruise(cruiseRecord{Name: "1"})
	c2, _ := s.InsertCruise(cruiseRecord{Name: "2"})
	s.PatchPassage(a.ID, passagePatch{CruiseID: &c1.ID, CruiseIDSet: true})
	s.PatchPassage(c.ID, passagePatch{CruiseID: &c2.ID, CruiseIDSet: true})
	if _, err := s.MergePassages(a.ID, b.ID, c.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("different cruises: %v", err)
	}
	s.PatchPassage(c.ID, passagePatch{CruiseID: &c1.ID, CruiseIDSet: true})
	s.PatchPassage(b.ID, passagePatch{Hidden: ptr(true)})
	if _, err := s.MergePassages(a.ID, b.ID, c.ID); !errors.Is(err, errPassageInvalid) {
		t.Errorf("hidden: %v", err)
	}
	if signs, _ := s.ListSigns(0, 0); len(signs) != 0 {
		t.Errorf("a refused merge left signs: %+v", signs)
	}
	for _, id := range []string{a.ID, b.ID, c.ID} {
		if _, err := s.GetPassage(id); err != nil {
			t.Errorf("a refused merge lost %s: %v", id, err)
		}
	}
}

func TestSplitAtAJoinRestoresTheNames(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "Low Isles")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	m, err := s.MergePassages(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	wakes := 0
	prev := passageNamesWake
	passageNamesWake = func() { wakes++ }
	defer func() { passageNamesWake = prev }()
	first, second, err := s.SplitPassage(m.ID, a.End)
	if err != nil {
		t.Fatal(err)
	}
	if first.ToName != "Low Isles" || second.FromName != "Low Isles" {
		t.Errorf("%+v %+v", first, second)
	}
	for _, id := range []string{first.ID, second.ID} {
		got, _ := s.GetPassage(id)
		if got.FromName == "" || got.ToName == "" {
			t.Errorf("stored %+v", got)
		}
	}
	if ids, _ := s.PassagesNeedingNames(time.Now(), time.Hour); len(ids) != 0 {
		t.Errorf("queued for naming: %v", ids)
	}
	if wakes != 0 {
		t.Errorf("woke the naming worker %d times", wakes)
	}
}

func TestSplitAtAnUntitledJoinOrPlainSignStillQueuesNaming(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedLeg(t, s, 0, 4, "A", "B")
	at := passageEpoch.Add(time.Hour)
	if _, err := s.CreateSign(replaySign{TMs: at.UnixMilli(), Title: "Whale", Visibility: signPassing, ShowS: signShowDef}); err != nil {
		t.Fatal(err)
	}
	first, second, err := s.SplitPassage(p.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if first.ToName != "" || second.FromName != "" {
		t.Errorf("a plain sign names nothing: %+v %+v", first, second)
	}
}

func TestSignValidationTitleOptionalOnlyForJoins(t *testing.T) {
	s := newTestPassageStore(t)
	seedLeg(t, s, 0, 4, "A", "B")
	at := passageEpoch.Add(time.Hour).UnixMilli()
	base := replaySign{TMs: at, Visibility: signAfter, ShowS: signShowDef}
	if _, err := s.CreateSign(base); !errors.Is(err, errPassageInvalid) {
		t.Errorf("untitled plain sign: %v", err)
	}
	base.StartsLeg = true
	sg, err := s.CreateSign(base)
	if err != nil || !sg.StartsLeg {
		t.Fatalf("%+v %v", sg, err)
	}
	if _, err := s.PatchSign(sg.ID, signPatch{StartsLeg: ptr(false)}); !errors.Is(err, errPassageInvalid) {
		t.Errorf("clearing the flag on an untitled sign: %v", err)
	}
	got, err := s.PatchSign(sg.ID, signPatch{Title: ptr("Low Isles"), StartsLeg: ptr(false)})
	if err != nil || got.StartsLeg || got.Title != "Low Isles" {
		t.Errorf("%+v %v", got, err)
	}
}

func TestSignHandlersCarryStartsLeg(t *testing.T) {
	store := withPassageStore(t)
	seedLeg(t, store, 0, 4, "A", "B")
	at := strconv.FormatInt(passageEpoch.Add(time.Hour).UnixMilli(), 10)
	code, body := send(t, createSignHandler, http.MethodPost, `{"t_ms":`+at+`,"starts_leg":true}`)
	if code != http.StatusCreated {
		t.Fatal(code, body)
	}
	sign := body["sign"].(map[string]any)
	if sign["starts_leg"] != true || sign["title"] != "" {
		t.Fatalf("%v", sign)
	}
	if code, body = send(t, createSignHandler, http.MethodPost, `{"t_ms":`+at+`}`); code != http.StatusBadRequest {
		t.Errorf("no title and no starts_leg: %d %v", code, body)
	}
	if code, body = send(t, patchSignHandler, http.MethodPatch, `{"title":"Low Isles"}`, "id", sign["id"].(string)); code != 200 || body["sign"].(map[string]any)["starts_leg"] != true {
		t.Errorf("%d %v", code, body)
	}
	if code, body = send(t, patchSignHandler, http.MethodPatch, `{"starts_leg":false}`, "id", sign["id"].(string)); code != 200 || body["sign"].(map[string]any)["starts_leg"] != false {
		t.Errorf("%d %v", code, body)
	}
}

func TestMergeHandlerTakesTwoOrMoreIDs(t *testing.T) {
	store := withPassageStore(t)
	a := seedLeg(t, store, 0, 1, "A", "B")
	b := seedLeg(t, store, 2, 3, "B", "C")
	c := seedLeg(t, store, 4, 5, "C", "D")
	if code, _ := send(t, mergePassagesHandler, http.MethodPost, `{"ids":["`+a.ID+`"]}`); code != http.StatusBadRequest {
		t.Errorf("one id: %d", code)
	}
	code, body := send(t, mergePassagesHandler, http.MethodPost, `{"ids":["`+a.ID+`","`+b.ID+`","`+c.ID+`"]}`)
	if code != 200 {
		t.Fatal(code, body)
	}
	if signs, _ := store.ListSigns(0, 0); len(signs) != 2 {
		t.Errorf("%+v", signs)
	}
}

func TestNamingFillsUntitledJoinSignsFromTheTrack(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	p, _ := s.InsertPassage(passageRecord{Start: passageEpoch.Add(20 * time.Minute), End: passageEpoch.Add(2 * time.Hour), FromName: "A", ToName: "B"})
	at := passageEpoch.Add(time.Hour)
	sg, err := s.CreateSign(replaySign{TMs: at.UnixMilli(), StartsLeg: true, Visibility: signAfter, ShowS: signShowDef})
	if err != nil {
		t.Fatal(err)
	}
	_ = p
	now := time.Now()
	ids, err := s.SignsNeedingNames(now, 24*time.Hour)
	if err != nil || len(ids) != 1 || ids[0] != sg.ID {
		t.Fatalf("%v %v", ids, err)
	}
	// a lookup with no answer is not retried until the window passes
	if err := s.FillSignName(sg.ID, func(float64, float64) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if ids, _ = s.SignsNeedingNames(now.Add(time.Minute), 24*time.Hour); len(ids) != 0 {
		t.Errorf("attempted sign came straight back: %v", ids)
	}
	if ids, _ = s.SignsNeedingNames(now.Add(25*time.Hour), 24*time.Hour); len(ids) != 1 {
		t.Errorf("not retried after the window: %v", ids)
	}
	var lat, lon float64
	if err := s.FillSignName(sg.ID, func(la, lo float64) string { lat, lon = la, lo; return "Low Isles" }); err != nil {
		t.Fatal(err)
	}
	if lat == 0 && lon == 0 {
		t.Error("lookup got no position")
	}
	got, _ := s.ListSigns(0, 0)
	if len(got) != 1 || got[0].Title != "Low Isles" {
		t.Errorf("%+v", got)
	}
	if ids, _ = s.SignsNeedingNames(now.Add(48*time.Hour), 24*time.Hour); len(ids) != 0 {
		t.Errorf("named sign queued: %v", ids)
	}
}

func TestFillSignNameDoesNotClobberOperatorTitle(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	seedLeg(t, s, 0, 2, "", "")
	sg, err := s.CreateSign(replaySign{TMs: passageEpoch.Add(time.Hour).UnixMilli(), StartsLeg: true, Visibility: signAfter, ShowS: signShowDef})
	if err != nil {
		t.Fatal(err)
	}
	s.FillSignName(sg.ID, func(float64, float64) string {
		s.PatchSign(sg.ID, signPatch{Title: ptr("Mine")})
		return "Plugin"
	})
	if got, _ := s.ListSigns(0, 0); got[0].Title != "Mine" {
		t.Errorf("%+v", got)
	}
}

func TestMergeAgainAfterSplitAtJoinKeepsOneJoinSign(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "Low Isles")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	m, err := s.MergePassages(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, second, err := s.SplitPassage(m.ID, a.End)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergePassages(first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
	n := 0
	signs, _ := s.ListSigns(0, 0)
	for _, sg := range signs {
		if sg.TMs == a.End.UnixMilli() && sg.StartsLeg {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d join signs at the boundary: %+v", n, signs)
	}
}

func TestMergeStillWritesJoinBesideAPlainSignAtTheBoundary(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "Low Isles")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	if _, err := s.CreateSign(replaySign{TMs: a.End.UnixMilli(), Title: "Whale", Visibility: signPassing, ShowS: signShowDef}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergePassages(a.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if signs, _ := s.ListSigns(0, 0); len(signs) != 2 {
		t.Errorf("%+v", signs)
	}
}

func TestUntitledJoinCreatedOrPatchedIsQueuedAndWakesNaming(t *testing.T) {
	wakes := 0
	prev := passageNamesWake
	passageNamesWake = func() { wakes++ }
	defer func() { passageNamesWake = prev }()
	s := newTestPassageStore(t)
	seedLeg(t, s, 0, 4, "A", "B")
	at := passageEpoch.Add(time.Hour).UnixMilli()
	sg, err := s.CreateSign(replaySign{TMs: at, StartsLeg: true, Visibility: signAfter, ShowS: signShowDef})
	if err != nil {
		t.Fatal(err)
	}
	if wakes != 1 {
		t.Errorf("create woke %d", wakes)
	}
	// a past attempt must not hold back a patch that leaves a join untitled
	if _, err := s.db.Exec(`UPDATE replay_signs SET name_attempted_ms = ? WHERE id = ?`, time.Now().UnixMilli(), sg.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchSign(sg.ID, signPatch{Note: ptr("x")}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.SignsNeedingNames(time.Now(), time.Hour); len(ids) != 1 || wakes != 2 {
		t.Errorf("ids %v wakes %d", ids, wakes)
	}
	// a plain titled sign made into a join by a patch with a blank title
	plain, err := s.CreateSign(replaySign{TMs: at + 1000, Title: "Whale", Visibility: signPassing, ShowS: signShowDef})
	if err != nil {
		t.Fatal(err)
	}
	if wakes != 2 {
		t.Errorf("plain sign woke: %d", wakes)
	}
	if _, err := s.PatchSign(plain.ID, signPatch{Title: ptr(""), StartsLeg: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.SignsNeedingNames(time.Now(), time.Hour); len(ids) != 2 || wakes != 3 {
		t.Errorf("ids %v wakes %d", ids, wakes)
	}
	// a titled join does not wake
	if _, err := s.CreateSign(replaySign{TMs: at + 2000, Title: "Bay", StartsLeg: true, Visibility: signAfter, ShowS: signShowDef}); err != nil {
		t.Fatal(err)
	}
	if wakes != 3 {
		t.Errorf("titled join woke: %d", wakes)
	}
}

func TestNamingScanStillNamesSignsWhenThePassageQueryFails(t *testing.T) {
	s := newTestPassageStore(t)
	appendAll(t, s, simpleTrip(2*time.Hour).pts)
	seedLeg(t, s, 0, 2, "A", "B")
	sg, err := s.CreateSign(replaySign{TMs: passageEpoch.Add(time.Hour).UnixMilli(), StartsLeg: true, Visibility: signAfter, ShowS: signShowDef})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE passages`); err != nil {
		t.Fatal(err)
	}
	newNamingWorker(s, func(float64, float64) string { return "Low Isles" }, time.Hour).scan()
	got, _ := s.ListSigns(0, 0)
	if len(got) != 1 || got[0].ID != sg.ID || got[0].Title != "Low Isles" {
		t.Errorf("%+v", got)
	}
}

func TestSplitNearATitledJoinCutsAtTheJoin(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "Low Isles")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	m, err := s.MergePassages(a.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, second, err := s.SplitPassage(m.ID, a.End.Add(20*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !first.End.Equal(a.End) || !second.Start.Equal(a.End) || first.ToName != "Low Isles" || second.FromName != "Low Isles" {
		t.Errorf("%+v %+v", first, second)
	}
}

func TestSplitFarFromAJoinCutsWhereAsked(t *testing.T) {
	s := newTestPassageStore(t)
	a := seedLeg(t, s, 0, 1, "Cairns", "Low Isles")
	b := seedLeg(t, s, 2, 3, "Mossman", "Cooktown")
	m, _ := s.MergePassages(a.ID, b.ID)
	at := a.End.Add(5 * time.Minute)
	first, second, err := s.SplitPassage(m.ID, at)
	if err != nil {
		t.Fatal(err)
	}
	if !first.End.Equal(at) || first.ToName != "" || second.FromName != "" {
		t.Errorf("%+v %+v", first, second)
	}
}

func TestSplitPicksTheNearestOfSeveralJoins(t *testing.T) {
	s := newTestPassageStore(t)
	p := seedLeg(t, s, 0, 4, "A", "B")
	base := passageEpoch.Add(time.Hour)
	for _, j := range []struct {
		off   time.Duration
		title string
	}{{-40 * time.Second, "Far"}, {10 * time.Second, "Near"}} {
		if _, err := s.CreateSign(replaySign{TMs: base.Add(j.off).UnixMilli(), Title: j.title, StartsLeg: true, Visibility: signAfter, ShowS: signShowDef}); err != nil {
			t.Fatal(err)
		}
	}
	first, _, err := s.SplitPassage(p.ID, base)
	if err != nil {
		t.Fatal(err)
	}
	if !first.End.Equal(base.Add(10*time.Second)) || first.ToName != "Near" {
		t.Errorf("%+v", first)
	}
}
