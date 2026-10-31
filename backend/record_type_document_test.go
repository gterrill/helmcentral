package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// Documents as a record type with one writable field, the operator's tags: how
// Mate links an uploaded map to a marina, through the ordinary Apply card.

func proposalDocument(t *testing.T, env *proposalEnv, title string, tags ...string) document {
	t.Helper()
	doc, err := env.docs.Insert(document{SHA256: "sha-" + title, Filename: "map.pdf", Title: title, MIME: "application/pdf", OperatorTags: tags})
	if err != nil {
		t.Fatal(err)
	}
	env.advance()
	return doc
}

func tagUpdate(id string, tags ...string) string {
	quoted := make([]string, len(tags))
	for i, tag := range tags {
		quoted[i] = fmt.Sprintf("%q", tag)
	}
	return fmt.Sprintf(`{"operations":[{"type":"document","action":"update","id":%q,"fields":{"tags":[%s]}}]}`, id, strings.Join(quoted, ","))
}

func TestDocumentRecord_AddsATagThroughAChangeset(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map", "map")

	p := env.propose(t, tagUpdate(doc.ID, "map", "Coral Sea Marina Resort"))
	if len(p.Ops) != 1 || p.Ops[0].Description != `On Berth map: add tag "Coral Sea Marina Resort"` {
		t.Fatalf("unexpected proposal op: %+v", p.Ops)
	}
	if got, _ := env.docs.Get(doc.ID); len(got.OperatorTags) != 1 {
		t.Fatalf("a proposal must change nothing, tags = %v", got.OperatorTags)
	}
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, err := env.docs.Get(doc.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.OperatorTags, []string{"Coral Sea Marina Resort", "map"}) {
		t.Fatalf("tags = %v", got.OperatorTags)
	}
	linked, err := env.docs.DocumentsTaggedWith("coral sea marina resort", 3)
	if err != nil || len(linked) != 1 || linked[0].ID != doc.ID {
		t.Fatalf("the marina's lookup should find the document: %v, %v", linked, err)
	}
	hits, err := env.docs.Search("Coral Sea Marina Resort", nil, false, "", 5, 0)
	if err != nil || len(hits) == 0 {
		t.Errorf("search should see the new tag (meta chunk rebuilt): %v, %v", hits, err)
	}
}

func TestDocumentRecord_RemovesATagAndKeepsTheRest(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map", "map", "Coral Sea Marina Resort")
	p := env.propose(t, tagUpdate(doc.ID, "map"))
	if p.Ops[0].Description != `On Berth map: remove tag "Coral Sea Marina Resort"` {
		t.Fatalf("description = %q", p.Ops[0].Description)
	}
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := env.docs.Get(doc.ID)
	if !reflect.DeepEqual(got.OperatorTags, []string{"map"}) {
		t.Fatalf("tags = %v", got.OperatorTags)
	}
}

func TestDocumentRecord_AnEmptyListRemovesEveryTag(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map", "map")
	p := env.propose(t, tagUpdate(doc.ID))
	if _, err := env.apply(p.ID); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got, _ := env.docs.Get(doc.ID); len(got.OperatorTags) != 0 {
		t.Fatalf("tags = %v", got.OperatorTags)
	}
}

func TestDocumentRecord_OnlyTagsAreWritable(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map")
	for _, field := range []string{"title", "filename", "mime", "notes", "folder_id", "summary"} {
		args := fmt.Sprintf(`{"operations":[{"type":"document","action":"update","id":%q,"fields":{%q:"x"}}]}`, doc.ID, field)
		msg := proposeError(t, env.deps, args)
		if !strings.Contains(msg, field) {
			t.Errorf("%s: expected the field to be refused by name, got %s", field, msg)
		}
	}
	got, _ := env.docs.Get(doc.ID)
	if got.Title != "Berth map" || got.Filename != "map.pdf" {
		t.Errorf("a refused proposal changed the document: %+v", got)
	}
}

func TestDocumentRecord_CannotBeCreatedOrDeleted(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map")
	if msg := proposeError(t, env.deps, `{"operations":[{"type":"document","action":"create","fields":{"tags":["x"]}}]}`); !strings.Contains(msg, "cannot be created") {
		t.Errorf("create: %s", msg)
	}
	if msg := proposeError(t, env.deps, fmt.Sprintf(`{"operations":[{"type":"document","action":"delete","id":%q}]}`, doc.ID)); !strings.Contains(msg, "cannot be deleted") {
		t.Errorf("delete: %s", msg)
	}
}

func TestDocumentRecord_RefusesABlankTagAndAMissingDocument(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map")
	if msg := proposeError(t, env.deps, tagUpdate(doc.ID, "  ")); !strings.Contains(msg, "tags") || !strings.Contains(msg, "blank") {
		t.Errorf("blank tag: %s", msg)
	}
	if msg := proposeError(t, env.deps, tagUpdate("nope", "x")); !strings.Contains(msg, `no document with id "nope"`) {
		t.Errorf("missing document: %s", msg)
	}
}

func TestDocumentRecord_IsStaleWhenTheOperatorChangedTheTagsMeanwhile(t *testing.T) {
	env := newProposalEnv(t)
	doc := proposalDocument(t, env, "Berth map", "map")
	p := env.propose(t, tagUpdate(doc.ID, "map", "Coral Sea Marina Resort"))
	env.advance()
	// The Documents page replaces the tags without moving updated_at.
	if err := env.docs.UpdateMeta(doc.ID, nil, nil, []string{"map", "berths"}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.apply(p.ID); err == nil {
		t.Fatal("expected a stale refusal after the operator edited the tags")
	}
	if got, _ := env.docs.Get(doc.ID); !reflect.DeepEqual(got.OperatorTags, []string{"berths", "map"}) {
		t.Fatalf("a stale apply changed the tags: %v", got.OperatorTags)
	}
}

func TestDocumentRecord_ReadsAndListsByTag(t *testing.T) {
	env := newProposalEnv(t)
	proposalDocument(t, env, "Berth map", "Coral Sea Marina Resort")
	proposalDocument(t, env, "Manual", "manuals")
	rt, _ := defaultRecordRegistry.lookup(recordTypeDocument)
	tagged, err := rt.List(env.docs, map[string]string{"tag": "Coral Sea Marina Resort"})
	if err != nil || len(tagged) != 1 || tagged[0].Label != "Berth map" {
		t.Fatalf("tagged = %+v, %v", tagged, err)
	}
	if tagged[0].Fields["tags"].([]string)[0] != "Coral Sea Marina Resort" {
		t.Errorf("fields = %v", tagged[0].Fields)
	}
}
