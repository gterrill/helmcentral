package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var mateToolReadHelp = mateTool{
	name: "read_help",
	function: openRouterFunctionDef{
		Description: "Read one page of Helmcentral's own in-app help, or one section of it by " +
			"heading. Use it before answering any question about how Helmcentral itself works, what a " +
			"panel or tile shows, or how to set something up; quote the help rather than guessing. " +
			"Pages describe Helmcentral's own screens and settings, not the state of this boat. A page has no link: name it in plain words. " +
			"A page id that is not known returns the list of valid page ids.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"page": {
							"type": "string",
							"description": "A help page id from the help index, e.g. \"features/forecast\"."
						},
						"section": {
							"type": "string",
							"description": "Optional: the text of a \"## \" heading on that page, to return just that section."
						}
					},
					"required": ["page"]
				}`),
	},
	execute: assistantToolDeps.executeReadHelp,
	describe: func(args json.RawMessage) string {
		var a assistantReadHelpArgs
		page := ""
		if json.Unmarshal(args, &a) == nil {
			page = strings.TrimSpace(a.Page)
		}
		if page == "" {
			page = "a page"
		}
		return fmt.Sprintf("Reading the help: %s…", page)
	},
}

var mateToolSearchDocuments = mateTool{
	name: "search_documents",
	function: openRouterFunctionDef{
		Description: "Search the boat's document library - manuals, receipts, logs, notes and photos " +
			"(ADR 0106) - by keyword. Returns the best-matching page or section per document with a " +
			"snippet; call read_document with a result's document_id to read more of it.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"query": {
							"type": "string",
							"description": "Keywords to search for, e.g. \"impeller\" or \"haulout invoice\"."
						},
						"tag": {
							"type": "string",
							"description": "Optional: restrict results to documents carrying this exact tag."
						},
						"folder": {
							"type": "string",
							"description": "Optional: restrict to this folder and its subfolders, given as a path such as \"Receipts/2026\"."
						},
						"limit": {
							"type": "integer",
							"description": "Maximum number of documents to return (default 5, maximum 10)."
						}
					},
					"required": ["query"]
				}`),
	},
	execute: assistantToolDeps.executeSearchDocuments,
	describe: func(args json.RawMessage) string {
		var a assistantSearchDocumentsArgs
		query := ""
		if json.Unmarshal(args, &a) == nil {
			query = strings.TrimSpace(a.Query)
		}
		if query == "" {
			return "Searching the documents…"
		}
		return fmt.Sprintf("Searching documents for %q…", query)
	},
}

var mateToolReadDocument = mateTool{
	name: "read_document",
	function: openRouterFunctionDef{
		Description: "Read a document from the boat's document library (ADR 0106), in order, from a " +
			"given point. Use search_documents first to find the document_id, or use the id of a document " +
			"attached to this conversation. Call this again with the previous result's next_chunk to keep " +
			"reading when a result comes back truncated.",
		Parameters: json.RawMessage(`{
					"type": "object",
					"properties": {
						"document_id": {
							"type": "string",
							"description": "A document id from a search_documents result or an attached document."
						},
						"from_chunk": {
							"type": "integer",
							"description": "Resume from this chunk sequence number (a previous result's next_chunk). Omit to start from the beginning of the document body."
						}
					},
					"required": ["document_id"]
				}`),
	},
	execute: assistantToolDeps.executeReadDocument,
	describe: func(args json.RawMessage) string {
		// No access to the document store here (describeAssistantToolCall is
		// a pure function of name+args, the same as read_help's case just
		// above it, which shows the help's own page id rather than
		// resolving a title) - the document id is what's shown, not a
		// filename.
		var a assistantReadDocumentArgs
		id := ""
		if json.Unmarshal(args, &a) == nil {
			id = strings.TrimSpace(a.DocumentID)
		}
		if id == "" {
			return "Reading a document…"
		}
		return fmt.Sprintf("Reading document %s…", id)
	},
}

// ── read_help ──────────────────────────────────────────────────────────

type assistantReadHelpArgs struct {
	Page    string `json:"page"`
	Section string `json:"section"`
}

type assistantReadHelpResult struct {
	Page    string `json:"page"`
	Title   string `json:"title"`
	Section string `json:"section,omitempty"`
	Content string `json:"content"`
	// Truncated marks a content string capToolResultJSON's shrink had to
	// halve to fit the chat-context budget - a legible partial answer, not
	// the AGENTS.md fallback-policy kind of masked failure (see
	// capToolResultJSON's own doc comment).
	Truncated bool `json:"truncated,omitempty"`
}

// ── search_documents ──────────────────────────────────────────────────────────

type assistantSearchDocumentsArgs struct {
	Query  string `json:"query"`
	Tag    string `json:"tag"`
	Folder string `json:"folder"`
	Limit  int    `json:"limit"`
}

// assistantDocumentSearchHit is one row of search_documents' result: the
// best-matching chunk of one document (documentStore.Search already
// collapses several matching chunks of the same document down to its
// single best one), plus the folder path the tool resolves separately -
// documentSearchResult (documents_store.go) has no folder_path field of its
// own, since the B3 HTTP API this struct otherwise mirrors has no use for
// it (a folder-scoped request already knows what folder it asked for).
//
// Title is never blank - see assistantCitationTitle.
type assistantDocumentSearchHit struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	FolderPath string `json:"folder_path,omitempty"`
	Title      string `json:"title,omitempty"`
	Page       int    `json:"page,omitempty"`
	Snippet    string `json:"snippet"`
	Status     string `json:"status"`
	// MIME is the document's type, so Mate can tell a picture a plan can use
	// (image/jpeg, image/png) from a PDF without opening it.
	MIME string `json:"mime,omitempty"`
}

type assistantSearchDocumentsResult struct {
	Results []assistantDocumentSearchHit `json:"results"`
}

// assistantDocumentSnippetMarkers strips the \x02/\x03 highlight markers
// documentStore.Search's snippet() call wraps a matched term in
// (documents_store.go) - stripped to nothing rather than rendered as
// e.g. "**word**" markdown (a deliberate choice between the two options
// noted where this tool was specified): a model reading a raw snippet has
// no use for "this word was highlighted" as a fact, and stripping avoids
// teaching it to echo literal ** markers back at the operator.
var assistantDocumentSnippetMarkers = strings.NewReplacer("\x02", "", "\x03", "")

// executeSearchDocuments answers search_documents: ftsMatchQuery sanitises
// the operator's free-typed query into an FTS5 MATCH string exactly the way
// the B3 HTTP API's own search does (documents_handlers.go), then
// store.Search runs it, optionally scoped to a folder (and its subtree)
// resolved from a "/"-separated path via ResolveFolderPath - the tool's own
// convenience over the HTTP API's raw folder id, since a model has no way
// to know a folder's uuid ahead of time.
func (d assistantToolDeps) executeSearchDocuments(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.documentStore("search_documents")
	if err != nil {
		return "", err
	}

	var args assistantSearchDocumentsArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse search_documents arguments: %w", err)
	}

	if _, ok := ftsMatchQuery(args.Query); !ok {
		return "", fmt.Errorf("search_documents: query must not be empty")
	}

	limit := args.Limit
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}

	var folderID *string
	if folder := strings.TrimSpace(args.Folder); folder != "" {
		id, err := store.ResolveFolderPath(folder)
		if err != nil {
			if errors.Is(err, errFolderNotFound) {
				names, lerr := store.TopLevelFolderNames()
				if lerr != nil || len(names) == 0 {
					return "", fmt.Errorf("search_documents: unknown folder %q", folder)
				}
				return "", fmt.Errorf("search_documents: unknown folder %q; top-level folders: %s", folder, strings.Join(names, ", "))
			}
			return "", fmt.Errorf("search_documents: resolve folder %q: %w", folder, err)
		}
		folderID = &id
	}

	// recursive=true unconditionally: with no folder given, folderID is nil
	// and hybridDocumentSearch/Search both treat that as no folder filter at
	// all regardless of this flag (documentStore.Search's own doc comment),
	// so it only ever takes effect when a folder path was actually resolved
	// above - where the tool's contract (assistant_tools.go's own spec)
	// requires it anyway.
	//
	// hybridDocumentSearch (E1d, documents_hybrid.go), not a bare
	// store.Search: semantic search is worth more to a model asking "what
	// did the marine supplies receipt come to" than to a human typing a
	// part number, and it's the identical search the HTTP API's own search
	// box runs. d.documentSearchReadiness nil (a test that never wired it,
	// same as every field on assistantToolDeps) just means semantic search
	// never runs here - keyword-only, exactly this tool's behaviour before
	// E1d. mode/semantic_problem are deliberately not surfaced in this
	// tool's own result shape (search_documents' spec, this file) - a model
	// has no use for knowing which retriever found what.
	outcome, err := hybridDocumentSearch(ctx, documentSearchParams{
		Store:     store,
		Query:     args.Query,
		FolderID:  folderID,
		Recursive: true,
		Tag:       strings.TrimSpace(args.Tag),
		Limit:     limit,
		Readiness: d.documentSearchReadiness,
	})
	if err != nil {
		return "", fmt.Errorf("search_documents: %w", err)
	}
	hits := outcome.Results

	// folderPaths caches one resolved path per distinct folder id across
	// this call's hits, rather than resolving it again for every hit that
	// happens to share a folder - the fix for the N+1 this tool used to run
	// (documentFolderPathString's old signature did a Get+FolderPath per
	// hit; the folder id now rides along on the search row itself, see
	// documentSearchResult).
	result := assistantSearchDocumentsResult{Results: make([]assistantDocumentSearchHit, 0, len(hits))}
	folderPaths := map[string]string{}
	for _, h := range hits {
		var path string
		if h.FolderID != nil {
			cached, ok := folderPaths[*h.FolderID]
			if !ok {
				resolved, err := documentFolderPathString(store, *h.FolderID)
				if err != nil {
					return "", fmt.Errorf("search_documents: resolve folder path for %s: %w", h.DocumentID, err)
				}
				folderPaths[*h.FolderID] = resolved
				cached = resolved
			}
			path = cached
		}
		result.Results = append(result.Results, assistantDocumentSearchHit{
			MIME:       h.Mime,
			DocumentID: h.DocumentID,
			Filename:   h.Filename,
			FolderPath: path,
			Title:      assistantCitationTitle(h.Title, h.Filename),
			Page:       h.PageStart,
			Snippet:    assistantDocumentSnippetMarkers.Replace(h.Snippet),
			Status:     h.Status,
		})
	}

	shrink := func() bool {
		if len(result.Results) == 0 {
			return false
		}
		result.Results = result.Results[:len(result.Results)-1]
		return true
	}
	return capToolResultJSON(&result, shrink)
}

// documentFolderPathString renders folderID's chain as a "/"-joined path
// ("Manuals/Engine"). Takes the folder id directly (documentSearchResult
// now carries it - see its doc comment) rather than a document id, so
// resolving it never needs its own document Get: the caller already has
// whatever document fields it needs from the search row itself. A
// store.FolderPath failure propagates rather than collapsing to "" - a
// review finding: silently swallowing it meant a real database failure
// looked identical to "this document has no folder" (AGENTS.md's fallback
// policy: fail fast, don't mask an upstream problem as an empty result).
func documentFolderPathString(store *documentStore, folderID string) (string, error) {
	chain, err := store.FolderPath(folderID)
	if err != nil {
		return "", err
	}
	names := make([]string, len(chain))
	for i, f := range chain {
		names[i] = f.Name
	}
	return strings.Join(names, "/"), nil
}

// ── read_document ──────────────────────────────────────────────────────────

type assistantReadDocumentArgs struct {
	DocumentID string `json:"document_id"`
	FromChunk  int    `json:"from_chunk"`
}

// assistantDocumentChunkOut is one row of read_document's chunks list -
// documentChunk (documents_store.go) trimmed to what the model needs: no
// internal rowid, document_id (already named once at the result's top
// level) or source ("local" vs "ocr" is an indexing detail, not something
// worth spending context on).
type assistantDocumentChunkOut struct {
	Seq       int    `json:"seq"`
	PageStart int    `json:"page_start,omitempty"`
	Heading   string `json:"heading,omitempty"`
	Text      string `json:"text"`
}

type assistantReadDocumentResult struct {
	DocumentID string `json:"document_id"`
	Filename   string `json:"filename"`
	// Title is the structured citation contract's other half
	// (assistantCitationTitle's doc comment) - never blank, so a citation
	// built from a read_document result always has a real name to show, the
	// same guarantee search_documents' own Title field already carries.
	Title  string                      `json:"title,omitempty"`
	Chunks []assistantDocumentChunkOut `json:"chunks"`
	// NextChunk is set only once capToolResultJSON's shrink has actually
	// dropped chunks to fit the budget - 0 otherwise, safe as an "absent"
	// sentinel because a real body chunk's seq is never 0 (seq 0 is always
	// the meta chunk, which FromChunk's own default skips).
	NextChunk int  `json:"next_chunk,omitempty"`
	Truncated bool `json:"truncated,omitempty"`
}

// executeReadDocument answers read_document: every chunk of doc from
// from_chunk onward (default 1, skipping the seq-0 meta chunk - a model
// reading a document's body has no use for its own title and tags restated
// as a "chunk"), shrunk to fit assistantMaxToolResultChars.
func (d assistantToolDeps) executeReadDocument(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	store, err := d.documentStore("read_document")
	if err != nil {
		return "", err
	}

	var args assistantReadDocumentArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse read_document arguments: %w", err)
	}

	docID := strings.TrimSpace(args.DocumentID)
	doc, err := store.Get(docID)
	if err != nil {
		if errors.Is(err, errDocumentNotFound) {
			return "", fmt.Errorf("read_document: unknown document %q", docID)
		}
		return "", fmt.Errorf("read_document: %w", err)
	}

	from := args.FromChunk
	if from < 1 {
		from = 1
	}
	chunks, err := store.ChunksFrom(docID, from)
	if err != nil {
		return "", fmt.Errorf("read_document: %w", err)
	}

	result := assistantReadDocumentResult{DocumentID: doc.ID, Filename: doc.Filename, Title: assistantCitationTitle(doc.Title, doc.Filename)}
	for _, c := range chunks {
		result.Chunks = append(result.Chunks, assistantDocumentChunkOut{
			Seq: c.Seq, PageStart: c.PageStart, Heading: c.Heading, Text: c.Text,
		})
	}

	// Shrink by halving - the same strategy executeReadHelp applies to its
	// (single, prose) content string above, adapted to a list: halve the
	// number of chunks kept rather than the characters of each, since a
	// chunk is already bounded to ~2000 characters by B2's own chunking
	// rule and next_chunk needs a whole chunk boundary to resume from
	// cleanly, not a byte offset partway through one.
	shrink := func() bool {
		n := len(result.Chunks)
		if n <= 1 {
			return false
		}
		keep := n / 2
		result.NextChunk = result.Chunks[keep].Seq
		result.Chunks = result.Chunks[:keep]
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}

func (d assistantToolDeps) executeReadHelp(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var args assistantReadHelpArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse read_help arguments: %w", err)
	}

	// d.help is a func field: assistantToolDeps' zero value (a test that
	// never sets it, same as documents below) leaves it nil, and calling a
	// nil func panics - the doc comment on the help field promises a
	// plain error instead, so this is checked before ever calling it.
	if d.help == nil {
		return "", fmt.Errorf("help is not embedded in this build (run make help-stage)")
	}
	pages := d.help()
	if len(pages) == 0 {
		return "", fmt.Errorf("help is not embedded in this build (run make help-stage)")
	}

	page := strings.TrimSpace(args.Page)
	ids := make([]string, 0, len(pages))
	var found *helpPage
	for i := range pages {
		ids = append(ids, pages[i].ID)
		if pages[i].ID == page {
			found = &pages[i]
		}
	}
	if found == nil {
		return "", fmt.Errorf("read_help: unknown page %q; valid ids are: %s", page, strings.Join(ids, ", "))
	}

	result := assistantReadHelpResult{Page: found.ID, Title: found.Title, Content: found.Body}

	if section := strings.TrimSpace(args.Section); section != "" {
		text, ok := helpSection(found.Body, section)
		if !ok {
			headings := helpPageHeadings(found.Body)
			return "", fmt.Errorf("read_help: unknown section %q on page %q; valid headings are: %s", section, found.ID, strings.Join(headings, ", "))
		}
		result.Section = section
		result.Content = text
	}

	// Halve the content string until the encoding fits the tool-result
	// budget, rather than one of the list-trimming shrinks the other tools
	// use above - a help page's content is prose, not a list of rows.
	shrink := func() bool {
		runes := []rune(result.Content)
		if len(runes) == 0 {
			return false
		}
		result.Content = string(runes[:len(runes)/2])
		result.Truncated = true
		return true
	}
	return capToolResultJSON(&result, shrink)
}
