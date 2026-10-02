package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Equipment, locations, bins and decks as registered types (ADR 0158): the
// operations run the same commands the REST handlers do, in one transaction.

func (e *proposalEnv) photo(t *testing.T, sha, name string) document {
	t.Helper()
	return mustInsertPhotoDocument(t, e.docs, sha, name)
}

func (e *proposalEnv) applyOK(t *testing.T, args string) assistantProposal {
	t.Helper()
	p := e.propose(t, args)
	e.advance()
	if _, err := e.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := e.asst.GetProposal(p.ID)
	return got
}

func TestInventory_CreateDeckAndMoveALocationOntoItInOneChangeset(t *testing.T) {
	env := newProposalEnv(t)
	zone, err := env.docs.CreateZone("Salon")
	if err != nil {
		t.Fatalf("CreateZone: %v", err)
	}
	plan := env.photo(t, "sha-plan", "salon.png")
	env.advance()

	p := env.propose(t, fmt.Sprintf(`{"operations":[
		{"type":"deck","action":"create","fields":{"name":"Main deck","plan_document_id":%q}},
		{"type":"location","action":"update","id":%q,"fields":{"deck_id":"$1","polygon":[[0.1,0.1],[0.5,0.1],[0.5,0.5]]}}]}`, plan.ID, zone.ID))
	if got := p.Ops[0].Description; got != "Add deck Main deck with a plan picture" {
		t.Errorf("deck description %q", got)
	}
	if got := p.Ops[1].Description; got != "Change location Salon: put it on a deck plan with an outline" {
		t.Errorf("location description %q", got)
	}
	if decks, _ := env.docs.ListDecks(); len(decks) != 0 {
		t.Fatalf("propose writes nothing, got %+v", decks)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	decks, _ := env.docs.ListDecks()
	zones, _ := env.docs.ListZones()
	if len(decks) != 1 || decks[0].PlanDocumentID == nil || *decks[0].PlanDocumentID != plan.ID {
		t.Fatalf("expected the deck with its plan, got %+v", decks)
	}
	if zones[0].DeckID == nil || *zones[0].DeckID != decks[0].ID || len(zones[0].Polygon) != 3 {
		t.Fatalf("expected the location on the new deck, got %+v", zones[0])
	}
	applied, _ := env.asst.GetProposal(p.ID)
	if !strings.Contains(string(applied.Result), `"href":"/inventory/decks/`+decks[0].ID) {
		t.Fatalf("the result should link the new deck, got %s", applied.Result)
	}
}

func TestInventory_DeckPlanMustBeAJPEGOrPNGAndAPDFGetsAnOperatorRefusal(t *testing.T) {
	env := newProposalEnv(t)
	pdf := mustInsertDocument(t, env.docs, "sha-pdf", "drawing.pdf", nil)
	png := env.photo(t, "sha-png", "plan.png")

	msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"deck","action":"create","fields":{"name":"Main","plan_document_id":%q}}]}`, pdf.ID))
	for _, want := range []string{"plan_document_id:", "a page of a PDF cannot be a deck plan yet", "upload a picture of that page"} {
		if !strings.Contains(msg, want) {
			t.Errorf("expected %q in %q", want, msg)
		}
	}
	msg = proposeError(t, env.deps, `{"operations":[{"type":"deck","action":"create","fields":{"name":"Main","plan_document_id":"nope"}}]}`)
	if !strings.Contains(msg, "plan_document_id: no such document") {
		t.Errorf("got %q", msg)
	}
	if decks, _ := env.docs.ListDecks(); len(decks) != 0 {
		t.Fatalf("a refused changeset writes nothing, got %+v", decks)
	}
	// A PNG is fine, and the card's after shows the document.
	p := runPropose(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"deck","action":"create","fields":{"name":"Main","plan_document_id":%q}}]}`, png.ID))
	if p.Ops[0].After["plan_document_id"] != png.ID {
		t.Fatalf("after should carry the plan document, got %v", p.Ops[0].After)
	}
}

func TestInventory_DeckPlanRuleIsSharedWithTheUploadHandler(t *testing.T) {
	for mime, want := range map[string]string{
		"image/jpeg":      "",
		"image/png":       "",
		"application/pdf": "a page of a PDF cannot be a deck plan yet",
		"image/heic":      documentHEICRejectionMessage,
		"text/plain":      "a deck plan must be a JPEG or PNG image",
	} {
		got := deckPlanRefusal(mime)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s: got %q, want %q", mime, got, want)
		}
	}
	store := withTestDocumentStore(t)
	d, _ := store.CreateDeck("Main")
	pdf := mustInsertDocument(t, store, "sha-pdf", "d.pdf", nil)
	var verr *inventoryValidationError
	if _, err := store.SetDeckPlan(d.ID, pdf.ID); !errors.As(err, &verr) || verr.Field != "plan_document_id" {
		t.Fatalf("SetDeckPlan must apply the same rule, got %v", err)
	}
	_ = http.StatusOK
}

func TestInventory_EquipmentFiledIntoABinByTheBinAlone(t *testing.T) {
	env := newProposalEnv(t)
	z1, _ := env.docs.CreateZone("Salon")
	z2, _ := env.docs.CreateZone("Lazarette")
	b2, _ := env.docs.CreateBin(z2.ID, "B-02", "Fuses")
	item := env.equipment(t, equipmentItem{Name: "Fuse kit", System: "electrical", ZoneID: &z1.ID})
	env.advance()

	p := env.applyOK(t, fmt.Sprintf(`{"operations":[{"type":"equipment","action":"update","id":%q,"fields":{"bin_id":%q}}]}`, item.ID, b2.ID))
	if got, want := p.Ops[0].Description, "Move Fuse kit to bin B-02 (Lazarette)"; got != want {
		t.Errorf("description %q, want %q", got, want)
	}
	got, _ := env.docs.GetEquipment(item.ID)
	if got.BinID == nil || *got.BinID != b2.ID || got.ZoneID == nil || *got.ZoneID != z2.ID {
		t.Fatalf("expected the bin's location to follow, got %+v", got)
	}
}

func TestInventory_EquipmentCreateUpdateAndRefusals(t *testing.T) {
	env := newProposalEnv(t)
	p := env.applyOK(t, `{"operations":[{"type":"equipment","action":"create","fields":{"name":"Liferaft","system":"safety","quantity":1,"aliases":["raft"]}}]}`)
	items, _ := env.docs.ListEquipment(equipmentFilter{})
	if len(items) != 1 || items[0].Name != "Liferaft" || items[0].System != "safety" || len(items[0].Aliases) != 1 {
		t.Fatalf("got %+v (%s)", items, p.Result)
	}
	for _, tc := range []struct{ args, want string }{
		{`{"operations":[{"type":"equipment","action":"create","fields":{"name":"X","system":"galley"}}]}`, "system: must be one of"},
		{`{"operations":[{"type":"equipment","action":"create","fields":{"name":" "}}]}`, "name:"},
		{`{"operations":[{"type":"equipment","action":"create","fields":{"name":"X","quantity":-1}}]}`, "quantity:"},
		{`{"operations":[{"type":"equipment","action":"create","fields":{"name":"X","quantity":1.5}}]}`, "quantity: must be a whole number"},
		{`{"operations":[{"type":"equipment","action":"create","fields":{"name":"X","category":"general"}}]}`, "category: cannot be changed"},
		{`{"operations":[{"type":"equipment","action":"create","fields":{"name":"X","bin_id":"nope"}}]}`, "ops[0] (equipment create)"},
	} {
		if msg := proposeError(t, env.deps, tc.args); !strings.Contains(msg, tc.want) {
			t.Errorf("%s: expected %q in %q", tc.args, tc.want, msg)
		}
	}
}

func TestInventory_DeleteEquipmentSaysWhatGoesWithIt(t *testing.T) {
	env := newProposalEnv(t)
	gen := env.equipment(t, equipmentItem{Name: "Generator", System: "electrical"})
	mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Oil", IntervalMonths: iptr(12)})
	mustToolRule(t, env.docs, maintenanceRuleInput{EquipmentID: &gen.ID, Description: "Belts", IntervalMonths: iptr(12)})
	env.advance()

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"equipment","action":"delete","id":%q}]}`, gen.ID))
	if got, want := p.Ops[0].Description, "Delete Generator (removes its 2 maintenance rules)"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
	if _, err := env.docs.GetEquipment(gen.ID); err != nil {
		t.Fatalf("propose must not delete: %v", err)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := env.docs.GetEquipment(gen.ID); !errors.Is(err, errEquipmentNotFound) {
		t.Fatalf("expected the item gone, got %v", err)
	}
}

func TestInventory_LocationAndBinCreatedTogetherByReference(t *testing.T) {
	env := newProposalEnv(t)
	env.applyOK(t, `{"operations":[
		{"type":"location","action":"create","fields":{"name":"Cockpit"}},
		{"type":"bin","action":"create","fields":{"zone_id":"$1","code":"c-01","name":"Lines"}}]}`)
	zones, _ := env.docs.ListZones()
	if len(zones) != 1 || len(zones[0].Bins) != 1 || zones[0].Bins[0].Code != "c-01" {
		t.Fatalf("got %+v", zones)
	}
}

func TestInventory_LocationRenameDeleteAndRefusals(t *testing.T) {
	env := newProposalEnv(t)
	salon, _ := env.docs.CreateZone("Salon")
	env.docs.CreateZone("Galley")
	binZone, _ := env.docs.CreateZone("Lazarette")
	env.docs.CreateBin(binZone.ID, "L-1", "")
	deck, _ := env.docs.CreateDeck("Main")
	env.advance()

	for _, tc := range []struct{ args, want string }{
		{fmt.Sprintf(`{"operations":[{"type":"location","action":"update","id":%q,"fields":{"name":"galley"}}]}`, salon.ID), "name: a location with this name already exists"},
		{fmt.Sprintf(`{"operations":[{"type":"location","action":"delete","id":%q}]}`, binZone.ID), "still in use"},
		{fmt.Sprintf(`{"operations":[{"type":"location","action":"update","id":%q,"fields":{"deck_id":%q}}]}`, salon.ID, deck.ID), "polygon: a location on a deck needs an outline"},
		{fmt.Sprintf(`{"operations":[{"type":"location","action":"update","id":%q,"fields":{"polygon":[[0,0],[1,1],[0,1]]}}]}`, salon.ID), "polygon: a location must be on a deck"},
		{fmt.Sprintf(`{"operations":[{"type":"location","action":"update","id":%q,"fields":{"deck_id":%q,"polygon":[[0,0],[1,1]]}}]}`, salon.ID, deck.ID), "polygon:"},
		{`{"operations":[{"type":"bin","action":"create","fields":{"zone_id":"nope","code":"x"}}]}`, "ops[0] (bin create)"},
	} {
		if msg := proposeError(t, env.deps, tc.args); !strings.Contains(msg, tc.want) {
			t.Errorf("%s: expected %q in %q", tc.args, tc.want, msg)
		}
	}
	env.applyOK(t, fmt.Sprintf(`{"operations":[{"type":"location","action":"update","id":%q,"fields":{"name":"Main salon"}}]}`, salon.ID))
	if _, err := env.docs.CreateZone("Main salon"); !errors.Is(err, errZoneNameTaken) {
		t.Fatalf("expected the rename to have happened already, got %v", err)
	}
}

func TestInventory_DeleteDeckSaysWhichOutlinesGo(t *testing.T) {
	env := newProposalEnv(t)
	deck, _ := env.docs.CreateDeck("Main")
	for _, n := range []string{"A", "B", "C"} {
		z, _ := env.docs.CreateZone(n)
		if err := env.docs.SaveDeckLayout(deck.ID, []deckLayoutZone{{ID: z.ID, Polygon: [][2]float64{{0, 0}, {1, 0}, {1, 1}}}}, nil); err != nil {
			// each save replaces the layout; keep the last three placed below
			t.Fatalf("layout: %v", err)
		}
	}
	// SaveDeckLayout replaces, so place all three at once.
	zones, _ := env.docs.ListZones()
	var lz []deckLayoutZone
	for _, z := range zones {
		lz = append(lz, deckLayoutZone{ID: z.ID, Polygon: [][2]float64{{0, 0}, {1, 0}, {1, 1}}})
	}
	if err := env.docs.SaveDeckLayout(deck.ID, lz, nil); err != nil {
		t.Fatalf("layout: %v", err)
	}
	env.advance()

	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"deck","action":"delete","id":%q}]}`, deck.ID))
	if got, want := p.Ops[0].Description, "Delete deck Main (removes the outlines of 3 locations and the pins of their bins)"; got != want {
		t.Fatalf("description %q, want %q", got, want)
	}
	env.advance()
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	zones, _ = env.docs.ListZones()
	for _, z := range zones {
		if z.DeckID != nil || z.Polygon != nil {
			t.Fatalf("outlines should be cleared, got %+v", z)
		}
	}
}

func TestInventory_AStaleLocationEditIsRefusedWithItsName(t *testing.T) {
	env := newProposalEnv(t)
	z, _ := env.docs.CreateZone("Salon")
	env.advance()
	p := env.propose(t, fmt.Sprintf(`{"operations":[{"type":"location","action":"update","id":%q,"fields":{"name":"Saloon"}}]}`, z.ID))
	env.advance()
	if _, err := env.docs.UpdateZone(z.ID, "Main salon"); err != nil {
		t.Fatalf("UpdateZone: %v", err)
	}
	env.advance()
	_, err := env.apply(p.ID)
	if !errors.Is(err, errAssistantProposalStale) || !strings.Contains(err.Error(), "Salon changed since Mate proposed this") {
		t.Fatalf("expected a stale refusal naming the location, got %v", err)
	}
}
