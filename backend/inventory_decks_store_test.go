package main

import (
	"errors"
	"math"
	"testing"
)

// Tests for ADR 0156: decks, zone outlines and bin pins. Written before the
// store methods (AGENTS.md test-first policy).

var triangle = [][2]float64{{0.1, 0.1}, {0.5, 0.1}, {0.3, 0.5}}

func mustZone(t *testing.T, s *documentStore, name string) inventoryZone {
	t.Helper()
	z, err := s.CreateZone(name)
	if err != nil {
		t.Fatalf("CreateZone(%q): %v", name, err)
	}
	return z
}

func mustBin(t *testing.T, s *documentStore, zoneID, code string) inventoryBin {
	t.Helper()
	b, err := s.CreateBin(zoneID, code, "")
	if err != nil {
		t.Fatalf("CreateBin(%q): %v", code, err)
	}
	return b
}

func zoneFromList(t *testing.T, s *documentStore, id string) inventoryZone {
	t.Helper()
	zones, err := s.ListZones()
	if err != nil {
		t.Fatalf("ListZones: %v", err)
	}
	for _, z := range zones {
		if z.ID == id {
			return z
		}
	}
	t.Fatalf("zone %s not listed", id)
	return inventoryZone{}
}

func TestDeckStore_CreateTrimsAndAssignsIncreasingSortIndex(t *testing.T) {
	s := newTestDocumentStore(t)
	a, err := s.CreateDeck("  Main deck ")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}
	if a.Name != "Main deck" || a.ID == "" || a.PlanDocumentID != nil {
		t.Fatalf("unexpected deck %+v", a)
	}
	b, err := s.CreateDeck("Lower deck")
	if err != nil {
		t.Fatalf("CreateDeck: %v", err)
	}
	if b.SortIndex <= a.SortIndex {
		t.Fatalf("expected increasing sort_index, got %d then %d", a.SortIndex, b.SortIndex)
	}
	decks, err := s.ListDecks()
	if err != nil {
		t.Fatalf("ListDecks: %v", err)
	}
	if len(decks) != 2 || decks[0].ID != a.ID || decks[1].ID != b.ID {
		t.Fatalf("expected creation order, got %+v", decks)
	}
}

func TestDeckStore_ListEmptyIsNonNil(t *testing.T) {
	s := newTestDocumentStore(t)
	decks, err := s.ListDecks()
	if err != nil || decks == nil {
		t.Fatalf("expected empty non-nil slice, got %v, %v", decks, err)
	}
}

func TestDeckStore_NameValidation(t *testing.T) {
	s := newTestDocumentStore(t)
	if _, err := s.CreateDeck("  "); !errors.Is(err, errDeckNameInvalid) {
		t.Fatalf("expected errDeckNameInvalid, got %v", err)
	}
	d, _ := s.CreateDeck("Main")
	if _, err := s.CreateDeck("main"); !errors.Is(err, errDeckNameTaken) {
		t.Fatalf("expected errDeckNameTaken, got %v", err)
	}
	e, _ := s.CreateDeck("Lower")
	if _, err := s.UpdateDeck(e.ID, "MAIN"); !errors.Is(err, errDeckNameTaken) {
		t.Fatalf("expected errDeckNameTaken on rename, got %v", err)
	}
	if _, err := s.UpdateDeck(d.ID, "main"); err != nil {
		t.Fatalf("renaming to its own name in another case should work: %v", err)
	}
	if _, err := s.UpdateDeck("nope", "X"); !errors.Is(err, errDeckNotFound) {
		t.Fatalf("expected errDeckNotFound, got %v", err)
	}
}

func TestDeckStore_SetDeckPlan(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	doc := mustInsertPhotoDocument(t, s, "sha-plan", "plan.jpg")
	got, err := s.SetDeckPlan(d.ID, doc.ID)
	if err != nil {
		t.Fatalf("SetDeckPlan: %v", err)
	}
	if got.PlanDocumentID == nil || *got.PlanDocumentID != doc.ID {
		t.Fatalf("expected plan document set, got %+v", got)
	}
	if _, err := s.SetDeckPlan("nope", doc.ID); !errors.Is(err, errDeckNotFound) {
		t.Fatalf("expected errDeckNotFound, got %v", err)
	}
	if _, err := s.SetDeckPlan(d.ID, "nope"); !errors.Is(err, errDocumentNotFound) {
		t.Fatalf("expected errDocumentNotFound, got %v", err)
	}
}

func TestDeckStore_DeletingPlanDocumentLeavesDeckWithoutPlan(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	doc := mustInsertPhotoDocument(t, s, "sha-plan2", "plan.jpg")
	if _, err := s.SetDeckPlan(d.ID, doc.ID); err != nil {
		t.Fatalf("SetDeckPlan: %v", err)
	}
	if _, err := s.db.Exec(`DELETE FROM documents WHERE id = ?`, doc.ID); err != nil {
		t.Fatalf("delete document: %v", err)
	}
	decks, _ := s.ListDecks()
	if len(decks) != 1 || decks[0].PlanDocumentID != nil {
		t.Fatalf("expected plan cleared, got %+v", decks)
	}
}

func TestDeckStore_SaveLayoutPlacesZonesAndPins(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	z := mustZone(t, s, "Engine room")
	b := mustBin(t, s, z.ID, "ENG-01")

	if err := s.SaveDeckLayout(d.ID,
		[]deckLayoutZone{{ID: z.ID, Polygon: triangle}},
		[]deckLayoutBin{{ID: b.ID, X: 0.25, Y: 0.2}},
	); err != nil {
		t.Fatalf("SaveDeckLayout: %v", err)
	}
	got := zoneFromList(t, s, z.ID)
	if got.DeckID == nil || *got.DeckID != d.ID {
		t.Fatalf("expected zone on deck, got %+v", got.DeckID)
	}
	if len(got.Polygon) != 3 || got.Polygon[2] != [2]float64{0.3, 0.5} {
		t.Fatalf("unexpected polygon %+v", got.Polygon)
	}
	if len(got.Bins) != 1 || got.Bins[0].Pin == nil || got.Bins[0].Pin.X != 0.25 || got.Bins[0].Pin.Y != 0.2 {
		t.Fatalf("unexpected pin %+v", got.Bins)
	}
}

func TestDeckStore_ZoneWithoutLayoutHasNullFields(t *testing.T) {
	s := newTestDocumentStore(t)
	z := mustZone(t, s, "Salon")
	mustBin(t, s, z.ID, "SAL-01")
	got := zoneFromList(t, s, z.ID)
	if got.DeckID != nil || got.Polygon != nil || got.Bins[0].Pin != nil {
		t.Fatalf("expected nulls, got %+v", got)
	}
}

func TestDeckStore_SaveLayoutTakesUnlistedZonesOffAndClearsTheirPins(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	za := mustZone(t, s, "A")
	zb := mustZone(t, s, "B")
	ba := mustBin(t, s, za.ID, "A-1")
	bb := mustBin(t, s, zb.ID, "B-1")
	if err := s.SaveDeckLayout(d.ID,
		[]deckLayoutZone{{ID: za.ID, Polygon: triangle}, {ID: zb.ID, Polygon: triangle}},
		[]deckLayoutBin{{ID: ba.ID, X: 0.2, Y: 0.2}, {ID: bb.ID, X: 0.3, Y: 0.3}},
	); err != nil {
		t.Fatalf("first save: %v", err)
	}
	// Second save lists only A, and does not list A's bin: its pin clears.
	if err := s.SaveDeckLayout(d.ID, []deckLayoutZone{{ID: za.ID, Polygon: triangle}}, nil); err != nil {
		t.Fatalf("second save: %v", err)
	}
	a := zoneFromList(t, s, za.ID)
	if a.Bins[0].Pin != nil {
		t.Fatalf("unlisted bin in a listed zone should lose its pin, got %+v", a.Bins[0].Pin)
	}
	b := zoneFromList(t, s, zb.ID)
	if b.DeckID != nil || b.Polygon != nil || b.Bins[0].Pin != nil {
		t.Fatalf("zone not listed should be off the plan with pins cleared, got %+v", b)
	}
}

func TestDeckStore_SaveLayoutMovesZoneFromAnotherDeck(t *testing.T) {
	s := newTestDocumentStore(t)
	d1, _ := s.CreateDeck("Main")
	d2, _ := s.CreateDeck("Lower")
	z := mustZone(t, s, "Engine room")
	b := mustBin(t, s, z.ID, "ENG-01")
	if err := s.SaveDeckLayout(d1.ID, []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: b.ID, X: 0.2, Y: 0.2}}); err != nil {
		t.Fatalf("save d1: %v", err)
	}
	if err := s.SaveDeckLayout(d2.ID, []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, nil); err != nil {
		t.Fatalf("save d2: %v", err)
	}
	got := zoneFromList(t, s, z.ID)
	if got.DeckID == nil || *got.DeckID != d2.ID {
		t.Fatalf("expected zone moved to d2, got %v", got.DeckID)
	}
	if got.Bins[0].Pin != nil {
		t.Fatalf("pin from the old deck must not survive the move")
	}
}

func TestDeckStore_SaveLayoutValidation(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	z := mustZone(t, s, "A")
	other := mustZone(t, s, "B")
	b := mustBin(t, s, z.ID, "A-1")
	ob := mustBin(t, s, other.ID, "B-1")

	many := make([][2]float64, 65)
	for i := range many {
		many[i] = [2]float64{0.5, 0.5}
	}
	cases := []struct {
		name  string
		zones []deckLayoutZone
		bins  []deckLayoutBin
		want  error
	}{
		{"two points", []deckLayoutZone{{ID: z.ID, Polygon: triangle[:2]}}, nil, errPolygonInvalid},
		{"65 points", []deckLayoutZone{{ID: z.ID, Polygon: many}}, nil, errPolygonInvalid},
		{"x above 1", []deckLayoutZone{{ID: z.ID, Polygon: [][2]float64{{0, 0}, {1.01, 0}, {0, 1}}}}, nil, errPolygonInvalid},
		{"negative", []deckLayoutZone{{ID: z.ID, Polygon: [][2]float64{{0, 0}, {-0.1, 0}, {0, 1}}}}, nil, errPolygonInvalid},
		{"NaN", []deckLayoutZone{{ID: z.ID, Polygon: [][2]float64{{0, 0}, {math.NaN(), 0}, {0, 1}}}}, nil, errPolygonInvalid},
		{"unknown zone", []deckLayoutZone{{ID: "nope", Polygon: triangle}}, nil, errZoneNotFound},
		{"duplicate zone", []deckLayoutZone{{ID: z.ID, Polygon: triangle}, {ID: z.ID, Polygon: triangle}}, nil, errLayoutInvalid},
		{"pin out of range", []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: b.ID, X: 1.5, Y: 0.5}}, errPinInvalid},
		{"unknown bin", []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: "nope", X: 0.5, Y: 0.5}}, errBinNotFound},
		{"bin zone not listed", []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: ob.ID, X: 0.5, Y: 0.5}}, errLayoutBinZoneNotListed},
		{"duplicate bin", []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: b.ID, X: 0.5, Y: 0.5}, {ID: b.ID, X: 0.6, Y: 0.6}}, errLayoutInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.SaveDeckLayout(d.ID, tc.zones, tc.bins); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
			if got := zoneFromList(t, s, z.ID); got.DeckID != nil {
				t.Fatalf("a rejected save must change nothing, zone is on deck %v", *got.DeckID)
			}
		})
	}
	if err := s.SaveDeckLayout("nope", nil, nil); !errors.Is(err, errDeckNotFound) {
		t.Fatalf("expected errDeckNotFound, got %v", err)
	}
}

func TestDeckStore_PinOutsideOutlineIsAllowed(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	z := mustZone(t, s, "A")
	b := mustBin(t, s, z.ID, "A-1")
	if err := s.SaveDeckLayout(d.ID, []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: b.ID, X: 0.95, Y: 0.95}}); err != nil {
		t.Fatalf("pin outside the outline should be allowed: %v", err)
	}
}

func TestDeckStore_DeleteDeckClearsZonesAndPins(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	z := mustZone(t, s, "A")
	b := mustBin(t, s, z.ID, "A-1")
	if err := s.SaveDeckLayout(d.ID, []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: b.ID, X: 0.2, Y: 0.2}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	n, err := s.DeleteDeck(d.ID)
	if err != nil || n != 1 {
		t.Fatalf("expected 1 zone cleared, got %d, %v", n, err)
	}
	got := zoneFromList(t, s, z.ID)
	if got.DeckID != nil || got.Polygon != nil || got.Bins[0].Pin != nil {
		t.Fatalf("expected zone and pin cleared, got %+v", got)
	}
	if decks, _ := s.ListDecks(); len(decks) != 0 {
		t.Fatalf("expected deck gone, got %+v", decks)
	}
	if _, err := s.DeleteDeck(d.ID); !errors.Is(err, errDeckNotFound) {
		t.Fatalf("expected errDeckNotFound, got %v", err)
	}
}

func TestDeckStore_UpdateBinKeepsPin(t *testing.T) {
	s := newTestDocumentStore(t)
	d, _ := s.CreateDeck("Main")
	z := mustZone(t, s, "A")
	b := mustBin(t, s, z.ID, "A-1")
	if err := s.SaveDeckLayout(d.ID, []deckLayoutZone{{ID: z.ID, Polygon: triangle}}, []deckLayoutBin{{ID: b.ID, X: 0.2, Y: 0.2}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := s.UpdateBin(b.ID, "A-9", "renamed")
	if err != nil || got.Pin == nil || got.Pin.X != 0.2 {
		t.Fatalf("expected pin to survive a rename, got %+v, %v", got, err)
	}
}
