package main

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// A document in the library as a registered record type whose only writable
// field is its tags; a tag equal to a marina's name links the document to that marina.

const recordTypeDocument = "document"

func init() {
	defaultRecordRegistry.register(documentRecordType())
}

func documentRecordType() *recordType {
	return &recordType{
		Name:  recordTypeDocument,
		Label: "document",
		Summary: "A document in the library (a manual, a receipt, a marina's map). Only its tags can be changed: " +
			"the title, notes, folder and file are the operator's to edit in Documents, and a document cannot be added or deleted here. " +
			"A tag equal to a marina's name, exactly as marina_details gives it, links the document to that marina. " +
			"Find the document's id with search_documents.",
		Actions: []string{changeUpdate},
		Filters: map[string]string{"tag": "required: only documents carrying this tag (find a document by name or content with search_documents)"},
		Fields: []recordField{
			{Name: "title", Kind: kindString, Description: "the document's title (read only)"},
			{Name: "filename", Kind: kindString, Description: "the file's name (read only)"},
			{Name: "mime", Kind: kindString, Description: "the file's type (read only)"},
			{Name: "tags", Kind: kindStringList, Writable: true, Description: "the operator's tags on the document, the whole list: to add a tag send the current tags with it, to remove one send them without it. Tags the library suggested by reading the document are not here and are not changed."},
		},
		Get:      getDocumentRecord,
		List:     listDocumentRecords,
		Update:   updateDocumentRecord,
		Describe: describeDocument,
		AfterCommit: func(res changeOpResult) error {
			// The tag change rebuilt the meta chunk and dropped its embedding.
			wakeDocumentIndexer()
			return nil
		},
		Href: func(r recordSnapshot) string { return "/documents/" + r.ID },
	}
}

// documentRecordVersion is the operator's tags and nothing else. The indexer
// moves a document's updated_at at every stage, and the Documents page does not
// move it when tags change, so updated_at would stale a proposal by itself and
// miss the one edit that matters. tags must be sorted.
func documentRecordVersion(tags []string) string {
	return "tags:" + strings.Join(tags, "\x00")
}

func documentRecordSnapshot(d document, operatorTags []string) recordSnapshot {
	tags := append([]string{}, operatorTags...)
	sort.Strings(tags)
	return recordSnapshot{
		ID: d.ID, Label: assistantCitationTitle(d.Title, d.Filename), Version: documentRecordVersion(tags),
		Fields: map[string]any{"title": d.Title, "filename": d.Filename, "mime": d.MIME, "tags": tags},
	}
}

func getDocumentRecord(q sqlQueryer, id string) (recordSnapshot, error) {
	d, err := scanDocument(q.QueryRow(`SELECT `+documentColumns+` FROM documents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return recordSnapshot{}, errRecordNotFound
	}
	if err != nil {
		return recordSnapshot{}, fmt.Errorf("get document: %w", err)
	}
	operator, _, err := documentTagsOf(q, id)
	if err != nil {
		return recordSnapshot{}, err
	}
	return documentRecordSnapshot(d, operator), nil
}

func listDocumentRecords(s *documentStore, f map[string]string) ([]recordSnapshot, error) {
	// Every tagged document is returned; list_records does the cutting and says
	// so. Unfiltered, it would read every document's extracted text.
	tag := f["tag"]
	if tag == "" {
		return nil, fmt.Errorf("documents are listed by tag; to find one by its name or content use search_documents")
	}
	docs, err := s.documentsWithOperatorTag(tag)
	if err != nil {
		return nil, err
	}
	out := make([]recordSnapshot, len(docs))
	for i, d := range docs {
		out[i] = documentRecordSnapshot(d, d.OperatorTags)
	}
	return out, nil
}

// updateDocumentRecord replaces the operator's tags with the given list,
// through the same write the Documents page uses (trimmed, a suggested tag of
// the same name promoted to the operator's), then rebuilds the document's
// search text. Like the Documents page, it leaves updated_at alone.
func updateDocumentRecord(env changeEnv, before recordSnapshot, given fieldSet) error {
	tags, err := tagListOf(given["tags"])
	if err != nil {
		return fieldErr("tags", "%s", err.Error())
	}
	if tags == nil {
		return fieldErr("tags", "give the whole list of tags the document should carry (an empty list removes them all)")
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fieldErr("tags", "a tag cannot be blank")
		}
	}
	if err := updateMetaTx(env.tx, env.now, before.ID, nil, nil, tags); err != nil {
		if errors.Is(err, errDocumentNotFound) {
			return fieldErr("id", "no such document (find it with search_documents)")
		}
		return err
	}
	return rebuildMetaChunkTx(env.tx, before.ID)
}

// tagListOf reads a tags field, whether it holds the typed list a command was
// given or the generic one a stored snapshot decodes to.
func tagListOf(v any) ([]string, error) {
	switch list := v.(type) {
	case []string:
		return list, nil
	case []any:
		out := make([]string, 0, len(list))
		for _, e := range list {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("every tag must be text, but %v is not", e)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, nil
}

func describeDocument(d describeInput) string {
	was, _ := tagListOf(d.Before.Fields["tags"])
	now, _ := tagListOf(d.After.Fields["tags"])
	has := func(list []string, tag string) bool {
		for _, t := range list {
			if strings.EqualFold(strings.TrimSpace(t), strings.TrimSpace(tag)) {
				return true
			}
		}
		return false
	}
	var parts []string
	for _, tag := range now {
		if !has(was, tag) {
			parts = append(parts, fmt.Sprintf("add tag %q", tag))
		}
	}
	for _, tag := range was {
		if !has(now, tag) {
			parts = append(parts, fmt.Sprintf("remove tag %q", tag))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, "tags unchanged")
	}
	return "On " + d.Before.Label + ": " + strings.Join(parts, ", ")
}
