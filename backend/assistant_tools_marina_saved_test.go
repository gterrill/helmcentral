package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// A document the operator tagged with the marina's name is the marina's own
// map: marina_details names it, so Mate reads it, and the card links to it.

const coralArgs = `{"lat":-20.2663,"lon":148.7102}`

func tagDocument(t *testing.T, d assistantToolDeps, sha, title string, tags ...string) document {
	t.Helper()
	doc, err := d.documents().Insert(document{SHA256: sha, Filename: sha + ".pdf", Title: title, MIME: "application/pdf", OperatorTags: tags})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestMarinaDetails_NamesTheDocumentsTaggedWithTheMarina(t *testing.T) {
	d := marinaSavedDeps(t, &fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	map1 := tagDocument(t, d, "a", "Coral Sea berth map", "coral sea marina resort ")
	tagDocument(t, d, "b", "Other marina", "Coral Sea Marina")
	tagDocument(t, d, "c", "Unrelated", "manuals")

	text, cards := runMarinaForCard(t, d, coralArgs)
	var res assistantMarinaDetailsResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.SavedDocuments) != 1 || res.SavedDocuments[0] != (marinaSavedDocument{DocumentID: map1.ID, Title: "Coral Sea berth map"}) {
		t.Fatalf("saved_documents = %+v", res.SavedDocuments)
	}
	want := "The operator has saved \"Coral Sea berth map\" for this marina; read it with read_document for berth numbers and the layout."
	if !strings.Contains(res.Reading, want) {
		t.Errorf("reading lacks %q: %s", want, res.Reading)
	}
	card := decodeMarinaCard(t, cards[0])
	if len(card.SavedDocuments) != 1 || card.SavedDocuments[0].ID != map1.ID || card.SavedDocuments[0].Title != "Coral Sea berth map" {
		t.Errorf("card saved_documents = %+v", card.SavedDocuments)
	}
}

func TestMarinaDetails_WithNoSavedDocumentsSaysNothingOfThem(t *testing.T) {
	d := marinaSavedDeps(t, &fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	tagDocument(t, d, "b", "Other marina", "Coral Sea Marina")
	text, cards := runMarinaForCard(t, d, coralArgs)
	if strings.Contains(text, "saved_documents") || strings.Contains(text, "read_document") {
		t.Errorf("a marina with no linked document mentions one: %s", text)
	}
	if card := decodeMarinaCard(t, cards[0]); card.SavedDocuments == nil || len(card.SavedDocuments) != 0 {
		t.Errorf("card saved_documents = %#v, want an empty list", card.SavedDocuments)
	}
}

func TestMarinaDetails_SavedDocumentsAreBounded(t *testing.T) {
	d := marinaSavedDeps(t, &fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	for i := 0; i < marinaMaxSavedDocuments+4; i++ {
		tagDocument(t, d, fmt.Sprintf("s%d", i), fmt.Sprintf("Map %d", i), "Coral Sea Marina Resort")
	}
	text, cards := runMarinaForCard(t, d, coralArgs)
	var res assistantMarinaDetailsResult
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.SavedDocuments) != marinaMaxSavedDocuments || len(decodeMarinaCard(t, cards[0]).SavedDocuments) != marinaMaxSavedDocuments {
		t.Errorf("model has %d, card has %d, want %d each", len(res.SavedDocuments), len(decodeMarinaCard(t, cards[0]).SavedDocuments), marinaMaxSavedDocuments)
	}
}

func TestMarinaDetails_AnUntitledDocumentIsNamedByItsFile(t *testing.T) {
	d := marinaSavedDeps(t, &fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
	tagDocument(t, d, "plan", "", "Coral Sea Marina Resort")
	text, _ := runMarinaForCard(t, d, coralArgs)
	if !strings.Contains(text, `"title":"plan.pdf"`) {
		t.Errorf("an untitled document should be named by its filename: %s", text)
	}
}

// marinaSavedDeps is marinaCardDeps with a real document library, for the tests
// that look documents up.
func marinaSavedDeps(t *testing.T, p placeNameProvider, state vesselStateData) assistantToolDeps {
	t.Helper()
	d := marinaCardDeps(p, state)
	store := newTestDocumentStore(t)
	d.documents = func() *documentStore { return store }
	return d
}

// The library only enriches the lookup: without one, the marina is still
// described, with nothing saved to name.
func TestMarinaDetails_WithNoDocumentLibraryStillAnswers(t *testing.T) {
	for name, documents := range map[string]func() *documentStore{
		"no library func":     nil,
		"library returns nil": func() *documentStore { return nil },
	} {
		t.Run(name, func(t *testing.T) {
			d := marinaCardDeps(&fakeMarinaProvider{details: coralSeaWire(t)}, vesselStateData{LengthOverallM: 15, DraftM: 1.4})
			d.documents = documents
			text, cards := runMarinaForCard(t, d, coralArgs)
			if strings.Contains(text, "saved_documents") {
				t.Errorf("result names saved documents with no library: %s", text)
			}
			if len(cards) != 1 {
				t.Fatalf("cards = %d, want 1", len(cards))
			}
		})
	}
}
