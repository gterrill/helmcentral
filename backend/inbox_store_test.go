package main

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestInboxClaimIsHeldByOneCallerAndGoesStale(t *testing.T) {
	s := withInboxStore(t)
	it, err := s.Insert(inboxItem{SHA256: "a", Filename: "a.jpg", MIME: "image/jpeg", SizeBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if _, err := s.claimAt(it.ID, now); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if _, err := s.claimAt(it.ID, now.Add(time.Minute)); err != errInboxBusy {
		t.Fatalf("second claim: %v", err)
	}
	// A claim older than the stale limit is taken over.
	token, err := s.claimAt(it.ID, now.Add(inboxClaimStale+time.Minute))
	if err != nil {
		t.Fatalf("stale claim: %v", err)
	}
	if err := s.Release(it.ID, token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(it.ID); err != nil {
		t.Fatalf("claim after release: %v", err)
	}
	if _, err := s.Claim("ghost"); err != errInboxItemNotFound {
		t.Fatalf("unknown id: %v", err)
	}
}

func TestInboxReleaseOfATakenOverClaimLeavesTheNewHoldersClaim(t *testing.T) {
	s := withInboxStore(t)
	it, _ := s.Insert(inboxItem{SHA256: "a", Filename: "a.jpg", MIME: "image/jpeg", SizeBytes: 3})
	now := time.Now()
	tokenA, err := s.claimAt(it.ID, now) // A
	if err != nil {
		t.Fatal(err)
	}
	later := now.Add(inboxClaimStale + time.Minute)
	if _, err := s.claimAt(it.ID, later); err != nil { // B takes over
		t.Fatal(err)
	}
	if err := s.Release(it.ID, tokenA); err != nil { // A finally releases
		t.Fatal(err)
	}
	if _, err := s.claimAt(it.ID, later.Add(time.Second)); err != errInboxBusy { // C
		t.Fatalf("B's claim was wiped by A's release: %v", err)
	}
}

func TestInboxClaimHasOneWinnerAmongRacingCallers(t *testing.T) {
	s := withInboxStore(t)
	it, _ := s.Insert(inboxItem{SHA256: "a", Filename: "a.jpg", MIME: "image/jpeg", SizeBytes: 3})
	var wg sync.WaitGroup
	wins := make(chan bool, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Claim(it.ID); wins <- err == nil }()
	}
	wg.Wait()
	close(wins)
	n := 0
	for w := range wins {
		if w {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d callers hold the claim", n)
	}
}

func TestInboxStoreGetManyAndDeleteMany(t *testing.T) {
	s := withInboxStore(t)
	a, _ := s.Insert(inboxItem{SHA256: "a", Filename: "a.jpg", MIME: "image/jpeg", SizeBytes: 3})
	b, _ := s.Insert(inboxItem{SHA256: "b", Filename: "b.jpg", MIME: "image/jpeg", SizeBytes: 3, HasThumb: true})
	got, err := s.GetMany([]string{a.ID, b.ID, "ghost"})
	if err != nil || len(got) != 2 || got[a.ID].SHA256 != "a" || !got[b.ID].HasThumb {
		t.Fatalf("%+v %v", got, err)
	}
	if n, err := s.DeleteMany([]string{a.ID, b.ID, "ghost"}); err != nil || n != 2 {
		t.Fatalf("deleted %d %v", n, err)
	}
	if n, _ := s.Count(); n != 0 {
		t.Errorf("count %d", n)
	}
}

func TestDocumentStoreFindOrCreateRootFolder(t *testing.T) {
	docs := withTestDocumentStore(t)
	first, err := docs.FindOrCreateRootFolder("Passage photos")
	if err != nil {
		t.Fatal(err)
	}
	again, err := docs.FindOrCreateRootFolder("passage PHOTOS")
	if err != nil || again.ID != first.ID {
		t.Fatalf("second call: %+v %v", again, err)
	}
	if _, err := docs.FindOrCreateRootFolder(" "); err == nil {
		t.Error("a blank name should be refused")
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = docs.FindOrCreateRootFolder("Racing") }()
	}
	wg.Wait()
	folders, _, _ := docs.ListFolder(nil)
	if len(folders) != 2 {
		t.Fatalf("folders %+v", folders)
	}
}

func TestInboxTitlesLookUpOncePerPlace(t *testing.T) {
	w := newInboxWorld(t)
	at := passageEpoch.Add(time.Hour)
	// One anchorage: three shots within a few metres, and one a mile away.
	addTrack(t, w.passages, at.Add(-time.Minute), -20.2500, 149.5000, 0)
	addTrack(t, w.passages, at.Add(time.Hour-time.Minute), -20.2700, 149.5000, 0)
	var mu sync.Mutex
	var asked [][2]float64
	prev := inboxPlaceLookup
	inboxPlaceLookup = func(lat, lon float64) string {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, [2]float64{lat, lon})
		return "Cove"
	}
	t.Cleanup(func() { inboxPlaceLookup = prev })
	var ids []string
	for i, off := range []time.Duration{0, 10 * time.Second, 20 * time.Second, time.Hour} {
		ids = append(ids, uploadForFiling(t, uint8(160+i), map[string]string{"capture_ms": ms(at.Add(off))}))
	}
	q := `{"ids":["` + ids[0] + `","` + ids[1] + `","` + ids[2] + `","` + ids[3] + `","` + ids[0] + `"]}`
	code, body := send(t, inboxSuggestedTitlesHandler, http.MethodPost, q)
	if code != http.StatusOK {
		t.Fatal(code, body)
	}
	titles := body["titles"].(map[string]any)
	for _, id := range ids {
		if titles[id] != "Cove" {
			t.Errorf("title of %s: %v", id, titles[id])
		}
	}
	if len(asked) != 2 {
		t.Errorf("two places, got %d lookups: %v", len(asked), asked)
	}
}
