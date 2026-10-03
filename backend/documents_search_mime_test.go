package main

import "testing"

func TestDocumentStore_SearchResultsCarryMime(t *testing.T) {
	store := newTestDocumentStore(t)
	doc := mustInsertDocument(t, store, "sha-mime", "impeller.pdf", nil)
	if err := store.ReplaceChunks(doc.ID, []string{"local"}, []documentChunk{
		{Seq: 1, Source: "local", Text: "Impeller replacement on the Yanmar."},
	}); err != nil {
		t.Fatalf("ReplaceChunks: %v", err)
	}
	q, _ := ftsMatchQuery("impeller")
	hits, err := store.Search(q, nil, false, "", 10, 0)
	if err != nil || len(hits) != 1 {
		t.Fatalf("Search: hits=%+v err=%v", hits, err)
	}
	if hits[0].Mime != "application/pdf" {
		t.Fatalf("expected FTS hit to carry mime, got %q", hits[0].Mime)
	}

	chunks, err := store.ChunksFrom(doc.ID, 1)
	if err != nil || len(chunks) != 1 {
		t.Fatalf("ChunksFrom: %+v %v", chunks, err)
	}
	if err := store.SetChunkEmbeddings("test-model", 2, map[int64][]float32{chunks[0].ID: {1, 0}}); err != nil {
		t.Fatalf("SetChunkEmbeddings: %v", err)
	}
	vec, err := store.SearchVector([]float32{1, 0}, "test-model", nil, false, "", 10)
	if err != nil || len(vec) != 1 {
		t.Fatalf("SearchVector: %+v %v", vec, err)
	}
	if vec[0].Mime != "application/pdf" {
		t.Fatalf("expected vector hit to carry mime, got %q", vec[0].Mime)
	}
}
