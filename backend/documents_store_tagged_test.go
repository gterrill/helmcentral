package main

import (
	"fmt"
	"testing"
)

func TestDocumentsTaggedWith_MatchesTheWholeTagIgnoringCaseAndSpaces(t *testing.T) {
	store := newTestDocumentStore(t)
	mk := func(sha string, tags ...string) document {
		d, err := store.Insert(document{SHA256: sha, Filename: sha + ".pdf", MIME: "application/pdf", OperatorTags: tags})
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	exact := mk("a", "Coral Sea Marina Resort")
	shouty := mk("b", "  CORAL SEA marina resort ")
	mk("c", "Coral Sea Marina")          // a partial name
	mk("d", "Coral Sea Marina Resort 2") // longer
	mk("e", "manuals")

	got, err := store.DocumentsTaggedWith("coral sea marina resort", 10)
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, d := range got {
		ids[d.ID] = true
	}
	if len(got) != 2 || !ids[exact.ID] || !ids[shouty.ID] {
		t.Fatalf("got %d documents %v, want only the two whole-name matches", len(got), ids)
	}
}

func TestDocumentsTaggedWith_FoldsAccentedCapitals(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.Insert(document{SHA256: "a", Filename: "a.pdf", OperatorTags: []string{"Puerto Banús"}}); err != nil {
		t.Fatal(err)
	}
	got, err := store.DocumentsTaggedWith("PUERTO BANÚS", 5)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %d, err %v", len(got), err)
	}
}

func TestDocumentsTaggedWith_IsBoundedAndBlankMatchesNothing(t *testing.T) {
	store := newTestDocumentStore(t)
	for i := 0; i < 5; i++ {
		if _, err := store.Insert(document{SHA256: fmt.Sprintf("s%d", i), Filename: "f.pdf", OperatorTags: []string{"Marina"}}); err != nil {
			t.Fatal(err)
		}
	}
	if got, _ := store.DocumentsTaggedWith("Marina", 3); len(got) != 3 {
		t.Errorf("limit 3 gave %d", len(got))
	}
	if got, _ := store.DocumentsTaggedWith("  ", 3); len(got) != 0 {
		t.Errorf("a blank name matched %d documents", len(got))
	}
}
