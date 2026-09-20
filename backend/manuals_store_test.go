package main

import (
	"errors"
	"testing"
)

// ── CreateManual / FlagManual ───────────────────────────────────────────────

func TestDocumentStore_CreateManualCreatesTopLevelFolderWithRoleManual(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if manual.Name != "Operations Manual" {
		t.Fatalf("expected the given name, got %q", manual.Name)
	}
	if manual.DocumentCount != 0 {
		t.Fatalf("expected a brand new manual to have zero documents, got %d", manual.DocumentCount)
	}

	path, err := store.FolderPath(manual.ID)
	if err != nil {
		t.Fatalf("FolderPath: %v", err)
	}
	if len(path) != 1 || path[0].ParentID != nil {
		t.Fatalf("expected a manual to be a top-level folder, got path %+v", path)
	}
}

func TestDocumentStore_CreateManualRejectsDuplicateTopLevelName(t *testing.T) {
	store := newTestDocumentStore(t)

	if _, err := store.CreateFolder("Recipes", nil); err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.CreateManual("Recipes"); !errors.Is(err, errFolderNameTaken) {
		t.Fatalf("expected errFolderNameTaken against an existing top-level folder's name, got %v", err)
	}
}

func TestDocumentStore_FlagManualOnExistingTopLevelFolder(t *testing.T) {
	store := newTestDocumentStore(t)

	folder, err := store.CreateFolder("Crew Training", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-flag-1", "watchkeeping.pdf", &folder.ID)

	manual, err := store.FlagManual(folder.ID)
	if err != nil {
		t.Fatalf("FlagManual: %v", err)
	}
	if manual.ID != folder.ID || manual.Name != "Crew Training" {
		t.Fatalf("expected the flagged folder back, got %+v", manual)
	}
	if manual.DocumentCount != 1 {
		t.Fatalf("expected the existing document to be counted, got %d", manual.DocumentCount)
	}

	manuals, err := store.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 1 || manuals[0].ID != folder.ID {
		t.Fatalf("expected the flagged folder to show up in ListManuals, got %+v", manuals)
	}
}

// TestDocumentStore_FlagManualRejectsNonTopLevelFolder pins plan §2's first
// Go-enforced invariant: role<>” is only valid when parent_id IS NULL,
// something SQLite's own CHECK constraints cannot express (ALTER TABLE ADD
// COLUMN cannot add a table-level CHECK at all).
func TestDocumentStore_FlagManualRejectsNonTopLevelFolder(t *testing.T) {
	store := newTestDocumentStore(t)

	parent, err := store.CreateFolder("Manuals", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	child, err := store.CreateFolder("Engine", &parent.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	if _, err := store.FlagManual(child.ID); !errors.Is(err, errManualNotTopLevel) {
		t.Fatalf("expected errManualNotTopLevel for a non-top-level folder, got %v", err)
	}

	manuals, err := store.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 0 {
		t.Fatalf("expected the rejected flag to leave no manual behind, got %+v", manuals)
	}
}

func TestDocumentStore_FlagManualUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.FlagManual("does-not-exist"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

// ── ClearManual ──────────────────────────────────────────────────────────

// TestDocumentStore_ClearManualLeavesEveryNoteWhereItWas is the whole point
// of ClearManual (plan §2): demoting a manual back to a plain collection
// must move and delete nothing - not the notes filed directly in it, not a
// subfolder's own contents.
func TestDocumentStore_ClearManualLeavesEveryNoteWhereItWas(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	section, err := store.CreateFolder("Getting Underway", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	atRoot := mustInsertDocument(t, store, "sha-clear-root", "checklist.pdf", &manual.ID)
	inSection := mustInsertDocument(t, store, "sha-clear-section", "anchor-up.pdf", &section.ID)

	if err := store.ClearManual(manual.ID); err != nil {
		t.Fatalf("ClearManual: %v", err)
	}

	path, err := store.FolderPath(manual.ID)
	if err != nil {
		t.Fatalf("FolderPath: %v", err)
	}
	if len(path) != 1 || path[0].ParentID != nil {
		t.Fatalf("expected the folder to remain top-level, got path %+v", path)
	}

	gotRoot, err := store.Get(atRoot.ID)
	if err != nil {
		t.Fatalf("Get(atRoot): %v", err)
	}
	if gotRoot.FolderID == nil || *gotRoot.FolderID != manual.ID {
		t.Fatalf("expected the root-filed document to stay in %q, got %+v", manual.ID, gotRoot.FolderID)
	}

	gotSection, err := store.Get(inSection.ID)
	if err != nil {
		t.Fatalf("Get(inSection): %v", err)
	}
	if gotSection.FolderID == nil || *gotSection.FolderID != section.ID {
		t.Fatalf("expected the section-filed document to stay in %q, got %+v", section.ID, gotSection.FolderID)
	}

	subfolders, _, err := store.ListFolder(&manual.ID)
	if err != nil {
		t.Fatalf("ListFolder: %v", err)
	}
	if len(subfolders) != 1 || subfolders[0].ID != section.ID {
		t.Fatalf("expected the subfolder to remain in place, got %+v", subfolders)
	}

	manuals, err := store.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 0 {
		t.Fatalf("expected the cleared folder to no longer be a manual, got %+v", manuals)
	}
}

func TestDocumentStore_ClearManualOnAPlainFolderReturnsErrNotAManual(t *testing.T) {
	store := newTestDocumentStore(t)

	folder, err := store.CreateFolder("Recipes", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := store.ClearManual(folder.ID); !errors.Is(err, errNotAManual) {
		t.Fatalf("expected errNotAManual, got %v", err)
	}
}

func TestDocumentStore_ClearManualUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.ClearManual("does-not-exist"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

// ── ListManuals ──────────────────────────────────────────────────────────

// TestDocumentStore_ListManualsReturnsSeveralAtOnce pins plan §2: "any
// number of folders may carry [role='manual']" - the index behind it is
// plain, not unique.
func TestDocumentStore_ListManualsReturnsSeveralAtOnce(t *testing.T) {
	store := newTestDocumentStore(t)

	ops, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	crew, err := store.CreateManual("Crew Training")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	if _, err := store.CreateFolder("Recipes", nil); err != nil {
		t.Fatalf("CreateFolder (plain collection): %v", err)
	}

	manuals, err := store.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 2 {
		t.Fatalf("expected exactly the two flagged manuals, got %+v", manuals)
	}
	if manuals[0].ID != crew.ID || manuals[1].ID != ops.ID {
		t.Fatalf("expected alphabetical order (Crew Training, Operations Manual), got %+v", manuals)
	}
}

func TestDocumentStore_ListManualsCountsDocumentsRecursively(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	section, err := store.CreateFolder("Getting Underway", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-count-root", "a.pdf", &manual.ID)
	mustInsertDocument(t, store, "sha-count-section", "b.pdf", &section.ID)
	mustInsertDocument(t, store, "sha-count-elsewhere", "c.pdf", nil)

	manuals, err := store.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 1 || manuals[0].DocumentCount != 2 {
		t.Fatalf("expected a recursive count of 2 (root + section, not the unrelated document), got %+v", manuals)
	}
}

func TestDocumentStore_ListManualsEmptyReturnsNoRows(t *testing.T) {
	store := newTestDocumentStore(t)

	manuals, err := store.ListManuals()
	if err != nil {
		t.Fatalf("ListManuals: %v", err)
	}
	if len(manuals) != 0 {
		t.Fatalf("expected no manuals yet, got %+v", manuals)
	}
}

// ── ManualTree ───────────────────────────────────────────────────────────

func TestDocumentStore_ManualTreeUnknownIDReturnsNotFound(t *testing.T) {
	store := newTestDocumentStore(t)
	if _, err := store.ManualTree("does-not-exist"); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

func TestDocumentStore_ManualTreeOnAPlainFolderReturnsErrNotAManual(t *testing.T) {
	store := newTestDocumentStore(t)
	folder, err := store.CreateFolder("Recipes", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := store.ManualTree(folder.ID); !errors.Is(err, errNotAManual) {
		t.Fatalf("expected errNotAManual, got %v", err)
	}
}

// TestDocumentStore_ManualTreeInterleavesFoldersAndDocuments pins the tree
// shape plan §2/§4 describe: subfolders and directly-filed documents in ONE
// merged, ordered list, not two separate slices the way ListFolder returns
// them.
func TestDocumentStore_ManualTreeInterleavesFoldersAndDocuments(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	section, err := store.CreateFolder("Getting Underway", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	mustInsertDocument(t, store, "sha-tree-root", "checklist.pdf", &manual.ID)
	mustInsertDocument(t, store, "sha-tree-section", "anchor-up.pdf", &section.ID)

	tree, err := store.ManualTree(manual.ID)
	if err != nil {
		t.Fatalf("ManualTree: %v", err)
	}
	if tree.Type != "folder" || tree.ID != manual.ID {
		t.Fatalf("expected the root node to be the manual folder itself, got %+v", tree)
	}
	if len(tree.Children) != 2 {
		t.Fatalf("expected 2 children (the section folder and the root-filed document), got %+v", tree.Children)
	}

	var sawFolder, sawDocument bool
	for _, child := range tree.Children {
		switch child.Type {
		case "folder":
			sawFolder = true
			if child.ID != section.ID {
				t.Fatalf("expected the section folder, got %+v", child)
			}
			if len(child.Children) != 1 || child.Children[0].Type != "document" {
				t.Fatalf("expected the section's own document nested beneath it, got %+v", child.Children)
			}
		case "document":
			sawDocument = true
			if child.Name != "checklist.pdf" {
				t.Fatalf("expected checklist.pdf, got %q", child.Name)
			}
		default:
			t.Fatalf("unexpected node type %q", child.Type)
		}
	}
	if !sawFolder || !sawDocument {
		t.Fatalf("expected both a folder and a document child, got %+v", tree.Children)
	}
}

// TestDocumentStore_ManualTreeOrdersBySortIndexThenNameFallsBackToName is a
// required Verification case: the tree orders by sort_index then name, and
// ties fall back to name.
func TestDocumentStore_ManualTreeOrdersBySortIndexThenNameFallsBackToName(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	// Two items with an explicit, out-of-alphabetical sort_index: "Zebra"
	// sorts before "Anchor" once ordered.
	zebra, err := store.CreateFolder("Zebra Section", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	anchor, err := store.CreateFolder("Anchor Section", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := store.ReorderManualChildren(manual.ID, manual.ID, []manualReorderItem{
		{Kind: "folder", ID: zebra.ID, SortIndex: 0},
		{Kind: "folder", ID: anchor.ID, SortIndex: 1},
	}); err != nil {
		t.Fatalf("ReorderManualChildren (seed order): %v", err)
	}

	// Two documents tied at the same sort_index (both default 0) must fall
	// back to lower(filename).
	mustInsertDocument(t, store, "sha-order-b", "b-doc.pdf", &manual.ID)
	mustInsertDocument(t, store, "sha-order-a", "a-doc.pdf", &manual.ID)

	tree, err := store.ManualTree(manual.ID)
	if err != nil {
		t.Fatalf("ManualTree: %v", err)
	}
	if len(tree.Children) != 4 {
		t.Fatalf("expected 4 children, got %+v", tree.Children)
	}
	// sort_index 0: Zebra Section (folder) ties with a-doc.pdf/b-doc.pdf
	// (documents, also sort_index 0) - name breaks the tie across the whole
	// merged list, so a-doc.pdf sorts before b-doc.pdf sorts before "Zebra
	// Section", and Anchor Section (sort_index 1) comes last regardless of
	// its name.
	wantOrder := []string{"a-doc.pdf", "b-doc.pdf", "Zebra Section", "Anchor Section"}
	for i, want := range wantOrder {
		if tree.Children[i].Name != want {
			t.Fatalf("position %d: want %q, got %q (full order: %+v)", i, want, tree.Children[i].Name, tree.Children)
		}
	}
}

// ── ReorderManualChildren ───────────────────────────────────────────────

func TestDocumentStore_ReorderManualChildrenAppliesInOneTransaction(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	a, err := store.CreateFolder("A", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	b, err := store.CreateFolder("B", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	if err := store.ReorderManualChildren(manual.ID, manual.ID, []manualReorderItem{
		{Kind: "folder", ID: b.ID, SortIndex: 0},
		{Kind: "folder", ID: a.ID, SortIndex: 1},
	}); err != nil {
		t.Fatalf("ReorderManualChildren: %v", err)
	}

	tree, err := store.ManualTree(manual.ID)
	if err != nil {
		t.Fatalf("ManualTree: %v", err)
	}
	if len(tree.Children) != 2 || tree.Children[0].ID != b.ID || tree.Children[1].ID != a.ID {
		t.Fatalf("expected B then A after reorder, got %+v", tree.Children)
	}
}

// TestDocumentStore_ReorderManualChildrenRejectsWholeCallOnUnknownID is a
// required Verification case: an unknown id fails the whole call, following
// MoveDocuments' precedent for partial bulk operations - no id in the
// batch, known or not, ends up applied.
func TestDocumentStore_ReorderManualChildrenRejectsWholeCallOnUnknownID(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	a, err := store.CreateFolder("A", &manual.ID)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	err = store.ReorderManualChildren(manual.ID, manual.ID, []manualReorderItem{
		{Kind: "folder", ID: a.ID, SortIndex: 5},
		{Kind: "folder", ID: "does-not-exist", SortIndex: 6},
	})
	if !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound for the unknown id, got %v", err)
	}

	// The known id's write must not have landed either - the whole call
	// failed, not just the bad entry.
	tree, err := store.ManualTree(manual.ID)
	if err != nil {
		t.Fatalf("ManualTree: %v", err)
	}
	if len(tree.Children) != 1 || tree.Children[0].ID != a.ID {
		t.Fatalf("expected only A to still be there, got %+v", tree.Children)
	}
	if tree.Children[0].SortIndex == 5 {
		t.Fatalf("expected A's sort_index to be rolled back to its pre-call value, got %d", tree.Children[0].SortIndex)
	}
}

func TestDocumentStore_ReorderManualChildrenRejectsUnknownManualID(t *testing.T) {
	store := newTestDocumentStore(t)
	if err := store.ReorderManualChildren("does-not-exist", "does-not-exist", nil); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound, got %v", err)
	}
}

func TestDocumentStore_ReorderManualChildrenRejectsNonManualID(t *testing.T) {
	store := newTestDocumentStore(t)
	folder, err := store.CreateFolder("Recipes", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if err := store.ReorderManualChildren(folder.ID, folder.ID, nil); !errors.Is(err, errNotAManual) {
		t.Fatalf("expected errNotAManual, got %v", err)
	}
}

// TestDocumentStore_ReorderManualChildrenRejectsParentOutsideTheManual
// guards a reorder call from touching a sibling list that belongs to a
// different manual (or an ordinary collection) just because the caller
// supplied a parent_id that isn't actually inside this :id's own subtree.
func TestDocumentStore_ReorderManualChildrenRejectsParentOutsideTheManual(t *testing.T) {
	store := newTestDocumentStore(t)

	manual, err := store.CreateManual("Operations Manual")
	if err != nil {
		t.Fatalf("CreateManual: %v", err)
	}
	elsewhere, err := store.CreateFolder("Recipes", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}

	if err := store.ReorderManualChildren(manual.ID, elsewhere.ID, nil); !errors.Is(err, errFolderNotFound) {
		t.Fatalf("expected errFolderNotFound for a parent outside the manual's own subtree, got %v", err)
	}
}
