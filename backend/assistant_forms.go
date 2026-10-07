package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// inspect_form and fill_form (ADR 0165): Mate reads a PDF form attached to the
// chat and fills it in. Mate sees a document's extracted text only, which does
// not say where the blanks and boxes are; inspect_form says. fill_form makes
// the filled PDF as a draft under Mate's reply. It writes nothing to the
// document library: the operator saves it from the card.

const (
	assistantInspectFormToolName = "inspect_form"
	assistantFillFormToolName    = "fill_form"
)

func assistantInspectFormToolDefinition() openRouterTool {
	return openRouterTool{
		Type: "function",
		Function: openRouterFunctionDef{
			Name: assistantInspectFormToolName,
			Description: "Read the layout of a PDF form the operator attached or stored as a document, so you can fill it in. " +
				"A form with fillable fields returns each field's name, kind (text, date, checkbox, radio, combobox, listbox), " +
				"options and current value. A flat form, one with no fields, returns for each page its lines of text with their " +
				"position and the tick boxes with the text beside each. Positions are PDF points from the lower left of the page; " +
				"y is the text baseline. For a flat form, the text of a line is the anchor you give to fill_form, so copy it exactly. " +
				"Read the lines in order to see which blank belongs to which question. Call it before fill_form. A big form comes " +
				"back a page at a time: pass page to read the next, and offset (with page) to read on in a page that was cut.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"document_id": {"type": "string", "description": "The id of the PDF document."},
					"page": {"type": "integer", "minimum": 1, "description": "Optional: read only this page of a flat form."},
					"offset": {"type": "integer", "minimum": 0, "description": "Optional: skip this many fields (fillable form) or lines of the page (flat form, with page). Use the next_offset a cut result gave."}
				},
				"required": ["document_id"]
			}`),
		},
	}
}

func assistantFillFormToolDefinition() openRouterTool {
	return openRouterTool{
		Type: "function",
		Function: openRouterFunctionDef{
			Name: assistantFillFormToolName,
			Description: "Fill in a PDF form and hand the result to the operator as a card under your reply, where they can open it, " +
				"download it, save it to Documents or dismiss it. Nothing is saved to Documents until they do. Give inspect_form's " +
				"names exactly: an entry is either {field, value} for a fillable field, {page, anchor, value} to write text after a " +
				"label on a flat form, or {page, box} to tick the box with that label (value no leaves it empty; a tick is the default). A check box field takes yes or no, a radio or " +
				"combo box one of its options. One bad entry fails the call and names it, and nothing is made. Never fill a " +
				"signature: leave it for the operator. Use only values you have from the operator or from get_vessel_particulars, " +
				"never guesses. After it succeeds, tell the operator to check the form and use the card to save it.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"document_id": {"type": "string", "description": "The id of the blank PDF form."},
					"entries": {
						"type": "array",
						"minItems": 1,
						"items": {
							"type": "object",
							"properties": {
								"field": {"type": "string", "description": "A fillable field's name."},
								"page": {"type": "integer", "minimum": 1, "description": "The page, for an anchor or a box."},
								"anchor": {"type": "string", "description": "The exact text of a line from inspect_form; the value is written after it."},
								"box": {"type": "string", "description": "The exact label of a tick box from inspect_form. It is ticked unless value is no."},
								"occurrence": {"type": "integer", "minimum": 1, "description": "Which one, counting from the top of the page, when the same anchor or box label appears more than once."},
								"value": {"type": "string", "description": "What to write. For a check box field or a tick box, yes or no (a tick box with no value is ticked)."}
							}
						}
					},
					"title": {"type": "string", "description": "A name for the filled-in document, such as \"Storm declaration 2026\"."},
					"folder": {"type": "string", "description": "Where to suggest saving it, a folder path such as \"Insurance/2026\"."}
				},
				"required": ["document_id", "entries"]
			}`),
		},
	}
}

// formSourcePath finds a stored PDF document and its file.
func (d assistantToolDeps) formSourcePath(tool, documentID string) (document, string, error) {
	store, err := d.documentStore(tool)
	if err != nil {
		return document{}, "", err
	}
	id := strings.TrimSpace(documentID)
	doc, err := store.Get(id)
	if err != nil {
		if errors.Is(err, errDocumentNotFound) {
			return document{}, "", fmt.Errorf("%s: unknown document %q", tool, id)
		}
		return document{}, "", fmt.Errorf("%s: %w", tool, err)
	}
	if doc.MIME != "application/pdf" {
		return document{}, "", fmt.Errorf("%s: %q is not a PDF (it is %s); only PDF forms can be read or filled in", tool, doc.Filename, doc.MIME)
	}
	return doc, filepath.Join(documentsDirPath(), doc.SHA256), nil
}

type assistantInspectFormArgs struct {
	DocumentID string `json:"document_id"`
	Page       int    `json:"page"`
	// Offset skips that many fields (a fillable form) or lines of the page
	// (a flat form, with page): how a result that was cut to fit is read on.
	Offset int `json:"offset"`
}

type assistantInspectFormResult struct {
	DocumentID string `json:"document_id"`
	Kind       string `json:"kind"` // fields or flat
	PageCount  int    `json:"page_count"`
	// Fields, for a form that has them.
	Fields []pdfFormField `json:"fields,omitempty"`
	// Pages, for a flat form: each page's lines and tick boxes.
	Pages []pdfLayoutPage `json:"pages,omitempty"`
	Notes []string        `json:"notes,omitempty"`
	// NextPage, when the result was cut to fit, is the page to ask for next.
	NextPage int `json:"next_page,omitempty"`
	// NextOffset is the offset to ask for, with next_page, to read on.
	NextOffset int  `json:"next_offset,omitempty"`
	Truncated  bool `json:"truncated,omitempty"`
}

func (d assistantToolDeps) executeInspectForm(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	var args assistantInspectFormArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse %s arguments: %w", assistantInspectFormToolName, err)
	}
	doc, path, err := d.formSourcePath(assistantInspectFormToolName, args.DocumentID)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: read the document: %w", assistantInspectFormToolName, err)
	}
	fields, err := listAcroFormFields(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", assistantInspectFormToolName, err)
	}
	if args.Offset < 0 {
		return "", fmt.Errorf("%s: offset cannot be negative", assistantInspectFormToolName)
	}
	const cutNote = "The result was cut to fit. Ask again with next_page (when given) and offset set to next_offset to read the rest."
	if len(fields) > 0 {
		if args.Offset >= len(fields) {
			return "", fmt.Errorf("%s: offset %d is past the last of the %d fields", assistantInspectFormToolName, args.Offset, len(fields))
		}
		fields = fields[args.Offset:]
		pages, perr := pdfPageCountOf(data)
		if perr != nil {
			return "", fmt.Errorf("%s: %w", assistantInspectFormToolName, perr)
		}
		res := assistantInspectFormResult{DocumentID: doc.ID, Kind: "fields", PageCount: pages, Fields: fields,
			Notes: []string{"This form has fillable fields. Use {field, value} entries. Signature fields are not listed and are never filled in."}}
		noted := false
		return capToolResultJSON(&res, func() bool {
			n := len(res.Fields)
			if n <= 1 {
				return false
			}
			keep := n / 2
			res.NextOffset = args.Offset + keep
			res.Fields = res.Fields[:keep]
			res.Truncated = true
			if !noted {
				res.Notes, noted = append(res.Notes, cutNote), true
			}
			return true
		})
	}

	layout, err := analysePDFLayout(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", assistantInspectFormToolName, err)
	}
	res := assistantInspectFormResult{DocumentID: doc.ID, Kind: "flat", PageCount: len(layout.Pages), Notes: layout.Notes}
	res.Notes = append(res.Notes, "This form has no fillable fields. Use {page, anchor, value} to write after a line and {page, box} to tick a box. x, y, w, h are PDF points from the lower left; y is the baseline of a line and the bottom of a box.")
	pages := layout.Pages
	if args.Page != 0 {
		if args.Page < 1 || args.Page > len(pages) {
			return "", fmt.Errorf("%s: page %d does not exist, the document has %d", assistantInspectFormToolName, args.Page, len(pages))
		}
		pages = pages[args.Page-1 : args.Page]
	} else if args.Offset != 0 {
		return "", fmt.Errorf("%s: offset counts lines within one page, so give page too", assistantInspectFormToolName)
	}
	if args.Offset != 0 {
		pg := pages[0]
		if args.Offset >= len(pg.Lines) {
			return "", fmt.Errorf("%s: offset %d is past the last of the %d lines on page %d", assistantInspectFormToolName, args.Offset, len(pg.Lines), pg.Page)
		}
		pg.Lines = pg.Lines[args.Offset:]
		pages = []pdfLayoutPage{pg}
	}
	res.Pages = pages
	hasBoxes := false
	for _, p := range layout.Pages {
		if len(p.Boxes) > 0 {
			hasBoxes = true
		}
	}
	if !hasBoxes {
		res.Notes = append(res.Notes, "No tick boxes were detected. If the form has any, they could not be found reliably, so only lines are listed; ask the operator which to tick rather than guessing.")
	}
	noted := false
	note := func() {
		if !noted {
			res.Notes, noted = append(res.Notes, cutNote), true
		}
	}
	return capToolResultJSON(&res, func() bool {
		if n := len(res.Pages); n > 1 {
			res.NextPage, res.NextOffset = res.Pages[n-1].Page, 0
			res.Pages = res.Pages[:n-1]
			res.Truncated = true
			note()
			return true
		}
		if len(res.Pages) == 1 && len(res.Pages[0].Lines) > 1 {
			p := &res.Pages[0]
			keep := len(p.Lines) * 3 / 4
			if keep < 1 {
				keep = 1
			}
			res.NextPage, res.NextOffset = p.Page, args.Offset+keep
			p.Lines = p.Lines[:keep]
			res.Truncated = true
			note()
			return true
		}
		return false
	})
}

type assistantFillFormArgs struct {
	DocumentID string      `json:"document_id"`
	Entries    []formEntry `json:"entries"`
	Title      string      `json:"title"`
	Folder     string      `json:"folder"`
}

type assistantFillFormResult struct {
	FormDraft assistantFormDraft `json:"form_draft"`
	Filled    []string           `json:"filled"`
	NextStep  string             `json:"next_step"`
}

func (d assistantToolDeps) executeFillForm(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if d.conversations == nil || d.conversations() == nil {
		return "", fmt.Errorf("%s: the chat store is not available", assistantFillFormToolName)
	}
	asst := d.conversations()

	var args assistantFillFormArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("parse %s arguments: %w", assistantFillFormToolName, err)
	}
	doc, path, err := d.formSourcePath(assistantFillFormToolName, args.DocumentID)
	if err != nil {
		return "", err
	}
	filled, err := fillPDFForm(path, args.Entries)
	if err != nil {
		return "", fmt.Errorf("%s: %w", assistantFillFormToolName, err)
	}

	title := strings.TrimSpace(args.Title)
	if title == "" {
		base := strings.TrimSpace(doc.Title)
		if base == "" {
			base = strings.TrimSuffix(doc.Filename, filepath.Ext(doc.Filename))
		}
		title = base + " (filled in)"
	}
	folder := strings.TrimSpace(args.Folder)
	if folder == "" && doc.FolderID != nil {
		if store := d.documents(); store != nil {
			if chain, ferr := store.FolderPath(*doc.FolderID); ferr == nil {
				names := make([]string, 0, len(chain))
				for _, f := range chain {
					names = append(names, f.Name)
				}
				folder = strings.Join(names, "/")
			}
		}
	}
	filename := strings.TrimSuffix(doc.Filename, filepath.Ext(doc.Filename)) + " (filled in).pdf"

	draft, err := asst.CreateFormDraft(assistantFormDraft{
		ID: uuid.NewString(), SourceDocumentID: doc.ID, Title: title, Folder: folder, Filename: filename, PageCount: filled.PageCount,
	}, filled.Data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", assistantFillFormToolName, err)
	}
	result := assistantFillFormResult{
		FormDraft: draft,
		Filled:    filled.Filled,
		NextStep: "The filled-in form is a draft, not a saved document. The operator sees it as a card under your reply with " +
			"Open, Download, Save to Documents and Dismiss. Tell them to check it, and that the card is where they save it. " +
			"Never say it is saved. Any signature is still theirs to add.",
	}
	body, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("%s: marshal result: %w", assistantFillFormToolName, err)
	}
	return string(body), nil
}

// assistantFormDraftFromToolResult reads the draft out of a successful
// fill_form result; a failed call's {"error": ...} body carries none.
func assistantFormDraftFromToolResult(result string) (*assistantFormDraft, error) {
	var decoded struct {
		FormDraft *assistantFormDraft `json:"form_draft"`
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(result)))
	if err := dec.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("read %s result: %w", assistantFillFormToolName, err)
	}
	if decoded.FormDraft == nil {
		return nil, nil
	}
	if decoded.FormDraft.ID == "" {
		return nil, fmt.Errorf("read %s result: the draft has no id", assistantFillFormToolName)
	}
	return decoded.FormDraft, nil
}
