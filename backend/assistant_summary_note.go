package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

// Summarise a conversation into a note (ADR 0162). Mate proposes, the
// operator applies: the draft endpoint spends model tokens and writes
// nothing; the save endpoint is what the Save button calls.

const (
	assistantSummaryNothingToKeep = "Nothing in this conversation to keep yet."
	assistantSummaryTimeout       = 120 * time.Second
	// Per-message and per-attachment caps keep a long conversation inside the
	// model's context; the oldest material is what a conversation settled
	// least recently, so nothing is dropped silently - a message is cut with
	// a visible marker.
	assistantSummaryMessageRunes    = 4000
	assistantSummaryAttachmentRunes = 1500
)

const assistantSummarySystemPrompt = `You write the record of what one conversation between a boat's operator and the onboard assistant (Mate) established about THIS boat, so the operator can keep it as a note.

Return ONLY a JSON object, no prose and no code fence:
{"title": string, "established": [string], "ruled_out": [{"option": string, "why": string}], "procedure": [string], "equipment": [string]}

- title: a short name for what the note is about, in the operator's words.
- established: facts about this boat the conversation settled: fittings, part numbers, what is and is not aboard, how something is plumbed or wired. Short, plain sentences.
- ruled_out: options that were considered and rejected or that must not be done, each with the reason.
- procedure: the steps that were actually followed and worked, in order, one step per entry. Leave it empty if no procedure was carried out.
- equipment: names of the boat's equipment the conversation is about, as the operator calls them.

Rules:
- Include only what this conversation established about this boat. Never present general knowledge as a fact about the boat.
- Skip general advice that nobody confirmed.
- The operator's own observations and photos outrank anything Mate said earlier. If Mate said something and the operator corrected it, the correction wins, and the wrong claim is not recorded as fact.
- Photo descriptions are given under each attachment; treat them as what the operator showed.
- Invent nothing. If a section has nothing, return it empty.`

type assistantSummaryRuledOut struct {
	Option string `json:"option"`
	Why    string `json:"why"`
}

// assistantSummaryDraft is the strict JSON the model is asked for.
type assistantSummaryDraft struct {
	Title       string                     `json:"title"`
	Established []string                   `json:"established"`
	RuledOut    []assistantSummaryRuledOut `json:"ruled_out"`
	Procedure   []string                   `json:"procedure"`
	Equipment   []string                   `json:"equipment"`
}

func cleanSummaryLines(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func cleanSummaryRuledOut(in []assistantSummaryRuledOut) []assistantSummaryRuledOut {
	out := make([]assistantSummaryRuledOut, 0, len(in))
	for _, r := range in {
		r.Option, r.Why = strings.TrimSpace(r.Option), strings.TrimSpace(r.Why)
		if r.Option != "" {
			out = append(out, r)
		}
	}
	return out
}

// renderAssistantSummaryNote builds the note body from the model's JSON.
// Empty sections are omitted; ok is false when every section is empty, so no
// empty note is ever offered. The body ends with a link back to the
// conversation.
func renderAssistantSummaryNote(d assistantSummaryDraft, convTitle, convID string, when time.Time) (string, bool) {
	established := cleanSummaryLines(d.Established)
	ruledOut := cleanSummaryRuledOut(d.RuledOut)
	procedure := cleanSummaryLines(d.Procedure)
	if len(established) == 0 && len(ruledOut) == 0 && len(procedure) == 0 {
		return "", false
	}

	var b strings.Builder
	if len(established) > 0 {
		b.WriteString("## What we established\n\n")
		for _, s := range established {
			b.WriteString("- " + s + "\n")
		}
		b.WriteString("\n")
	}
	if len(ruledOut) > 0 {
		b.WriteString("## Ruled out\n\n")
		for _, r := range ruledOut {
			if r.Why != "" {
				b.WriteString("- " + r.Option + ": " + r.Why + "\n")
			} else {
				b.WriteString("- " + r.Option + "\n")
			}
		}
		b.WriteString("\n")
	}
	if len(procedure) > 0 {
		b.WriteString("## Procedure that worked\n\n")
		for i, s := range procedure {
			fmt.Fprintf(&b, "%d. %s\n", i+1, s)
		}
		b.WriteString("\n")
	}
	title := strings.NewReplacer("[", "(", "]", ")").Replace(strings.TrimSpace(convTitle))
	fmt.Fprintf(&b, "From the Mate conversation [%s](/mate/%s), %s.\n", title, convID, when.UTC().Format("2006-01-02"))
	return b.String(), true
}

// assistantSummaryNoteType suggests "quirk" (a fact about this boat) unless
// the procedure outweighs everything else, then "procedure".
func assistantSummaryNoteType(d assistantSummaryDraft) string {
	if len(cleanSummaryLines(d.Procedure)) > len(cleanSummaryLines(d.Established))+len(cleanSummaryRuledOut(d.RuledOut)) {
		return noteTypeProcedure
	}
	return noteTypeQuirk
}

type assistantSummaryEquipment struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Linked bool   `json:"linked,omitempty"`
}

// resolveSummaryEquipment maps equipment names the model gave to inventory
// records using find_equipment's matching (ListEquipment's text query:
// name, manufacturer, model and aliases). A name that matches exactly one
// record, or whose name or alias equals it, counts; one that matches several
// records without an exact name is ambiguous and one that matches none is
// unknown - both are dropped, never guessed. The result is deduplicated.
func resolveSummaryEquipment(store *documentStore, names []string) ([]assistantSummaryEquipment, error) {
	out := []assistantSummaryEquipment{}
	seen := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		items, err := store.ListEquipment(equipmentFilter{Query: name})
		if err != nil {
			return nil, fmt.Errorf("match equipment %q: %w", name, err)
		}
		var pick *equipmentItem
		for i := range items {
			if strings.EqualFold(items[i].Name, name) {
				pick = &items[i]
				break
			}
		}
		if pick == nil && len(items) == 1 {
			pick = &items[0]
		}
		if pick == nil || seen[pick.ID] {
			continue
		}
		seen[pick.ID] = true
		out = append(out, assistantSummaryEquipment{ID: pick.ID, Name: pick.Name})
	}
	return out, nil
}

// equipmentLinkedToDocument is the set of equipment ids a document is linked
// to, so an update can show which suggested links already exist.
func equipmentLinkedToDocument(store *documentStore, docID string) (map[string]bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	rows, err := store.db.Query(`SELECT equipment_id FROM equipment_documents WHERE document_id = ?`, docID)
	if err != nil {
		return nil, fmt.Errorf("list equipment links: %w", err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// liveAssistantSummaryNoteID returns the conversation's summary note id, or
// "" when it has none or the note has since been deleted. A deleted note is
// plain state (the operator removed it), not an upstream error, so it reads
// as "no note yet" and the next save creates a fresh one; any other lookup
// failure is returned.
func liveAssistantSummaryNoteID(conv assistantConversation) (string, error) {
	if conv.SummaryNoteID == nil || *conv.SummaryNoteID == "" {
		return "", nil
	}
	if globalDocumentStore == nil {
		return "", errors.New("the document store is not available")
	}
	doc, err := globalDocumentStore.Get(*conv.SummaryNoteID)
	if err != nil {
		if errors.Is(err, errDocumentNotFound) {
			return "", nil
		}
		return "", err
	}
	if doc.Kind != "note" {
		return "", nil
	}
	return doc.ID, nil
}

func assistantSummaryTranscript(messages []assistantMessage) (string, error) {
	var b strings.Builder
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		who := "Operator"
		if m.Role == "assistant" {
			who = "Mate"
		}
		fmt.Fprintf(&b, "[%s]\n", who)
		content := strings.TrimSpace(m.Content)
		if r := []rune(content); len(r) > assistantSummaryMessageRunes {
			content = string(r[:assistantSummaryMessageRunes]) + " [cut]"
		}
		if content != "" {
			b.WriteString(content + "\n")
		}
		for _, a := range m.Attachments {
			fmt.Fprintf(&b, "Attachment: %s\n", a.Filename)
			if globalDocumentStore == nil {
				return "", errors.New("the document store is not available")
			}
			doc, err := globalDocumentStore.Get(a.DocumentID)
			if errors.Is(err, errDocumentNotFound) {
				b.WriteString("  (document since deleted)\n")
				continue
			}
			if err != nil {
				return "", fmt.Errorf("read attachment %s: %w", a.Filename, err)
			}
			desc := strings.TrimSpace(strings.Join(nonEmptyStrings(doc.Title, doc.Summary), ". "))
			if desc != "" {
				fmt.Fprintf(&b, "  Description: %s\n", truncateRunes(desc, assistantSummaryAttachmentRunes))
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

func nonEmptyStrings(in ...string) []string {
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// POST /api/assistant/conversations/:id/summary-draft
func postAssistantSummaryDraftHandler(c echo.Context) error {
	id := c.Param("id")
	fail := func(status int, msg string) error { return c.JSON(status, map[string]string{"error": msg}) }

	conv, ok, err := globalAssistantStore.GetConversation(id)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	if !ok {
		return fail(http.StatusNotFound, "conversation not found")
	}
	messages, err := globalAssistantStore.ListMessages(id)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	hasReply := false
	for _, m := range messages {
		if m.Role == "assistant" && strings.TrimSpace(m.Content) != "" {
			hasReply = true
		}
	}
	if !hasReply {
		return fail(http.StatusUnprocessableEntity, assistantSummaryNothingToKeep)
	}
	existing, err := liveAssistantSummaryNoteID(conv)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}

	settingsPath := assistantSettingsPath()
	readiness, apiKey, err := checkAssistantReadiness(settingsPath)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	if readiness.Problem != "" {
		return fail(http.StatusServiceUnavailable, readiness.Problem)
	}
	params, err := assistantTurnParamsFromSettings(settingsPath, readiness, apiKey)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}

	transcript, err := assistantSummaryTranscript(messages)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	req := openRouterChatRequest{
		Model: params.model,
		Messages: []openRouterMessage{
			{Role: "system", Content: openRouterContent(assistantSummarySystemPrompt)},
			{Role: "user", Content: openRouterContent("Conversation title: " + conv.Title + "\n\n" + transcript)},
		},
		Usage: &openRouterUsageOption{Include: true},
	}
	if plugin := autoRouterPluginForModel(params.model, params.autoRouter); plugin != nil {
		req.Plugins = []openRouterPlugin{*plugin}
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), assistantSummaryTimeout)
	defer cancel()
	resp, err := openRouterChatCompletionOnce(ctx, assistantOpenRouterDoer, apiKey, req)
	if err != nil {
		return fail(http.StatusBadGateway, "The summary could not be written: "+err.Error())
	}
	raw := string(resp.Choices[0].Message.Content)
	var draft assistantSummaryDraft
	if err := json.Unmarshal([]byte(stripDocumentEnrichJSONFence(raw)), &draft); err != nil {
		return fail(http.StatusBadGateway, "The summary could not be read: invalid JSON reply: "+truncateRunes(raw, 200))
	}

	body, ok := renderAssistantSummaryNote(draft, conv.Title, conv.ID, conv.UpdatedAt)
	if !ok {
		return fail(http.StatusUnprocessableEntity, assistantSummaryNothingToKeep)
	}
	title := strings.TrimSpace(draft.Title)
	if title == "" {
		title = conv.Title
	}

	equipment := []assistantSummaryEquipment{}
	if globalDocumentStore != nil {
		equipment, err = resolveSummaryEquipment(globalDocumentStore, draft.Equipment)
		if err != nil {
			return fail(http.StatusInternalServerError, err.Error())
		}
		if existing != "" {
			linked, err := equipmentLinkedToDocument(globalDocumentStore, existing)
			if err != nil {
				return fail(http.StatusInternalServerError, err.Error())
			}
			for i := range equipment {
				equipment[i].Linked = linked[equipment[i].ID]
			}
		}
	}

	out := map[string]any{
		"title":     title,
		"body":      body,
		"type":      assistantSummaryNoteType(draft),
		"equipment": equipment,
	}
	if existing != "" {
		out["existing_note_id"] = existing
	}
	return c.JSON(http.StatusOK, out)
}

type saveAssistantSummaryNoteRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Type  string `json:"type"`
	// EquipmentIDs is the full set the dialog shows ticked, linked already or
	// not; the server diffs it against the note's real links.
	EquipmentIDs       []string `json:"equipment_ids"`
	RemoveEquipmentIDs []string `json:"remove_equipment_ids"`
}

// patchNoteInProcess runs patchNoteHandler against a synthetic request so a
// summary update goes through exactly the note edit path (frontmatter
// re-render, blob swap, reindex) instead of a second copy of it.
func patchNoteInProcess(noteID string, payload map[string]any, suggestedType ...bool) (int, []byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	req := httptest.NewRequest(http.MethodPatch, "/api/notes/"+noteID, bytes.NewReader(raw))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(noteID)
	if len(suggestedType) > 0 && suggestedType[0] {
		c.Set(noteTypeIsSuggestionKey, true)
	}
	if err := patchNoteHandler(c); err != nil {
		return 0, nil, err
	}
	return rec.Code, rec.Body.Bytes(), nil
}

// POST /api/assistant/conversations/:id/summary-note
// The Save button: creates the conversation's note, or replaces the one it
// already has, adjusts the equipment links and remembers the note on the
// conversation, in one request. The note is written first and remembered
// before the links, so a link failure leaves a retry that updates the same
// note rather than creating a second.
func saveAssistantSummaryNoteHandler(c echo.Context) error {
	limitNoteRequestBody(c)
	id := c.Param("id")
	fail := func(status int, msg string) error { return c.JSON(status, map[string]string{"error": msg}) }

	var req saveAssistantSummaryNoteRequest
	if err := c.Bind(&req); err != nil {
		return fail(http.StatusBadRequest, "invalid body")
	}
	if strings.TrimSpace(req.Body) == "" {
		return fail(http.StatusBadRequest, "note body is required")
	}
	conv, ok, err := globalAssistantStore.GetConversation(id)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	if !ok {
		return fail(http.StatusNotFound, "conversation not found")
	}
	if globalDocumentStore == nil {
		return fail(http.StatusInternalServerError, "the document store is not available")
	}
	for _, eid := range req.EquipmentIDs {
		if _, err := globalDocumentStore.GetEquipment(eid); err != nil {
			if errors.Is(err, errEquipmentNotFound) {
				return fail(http.StatusNotFound, fmt.Sprintf("equipment %s not found", eid))
			}
			return fail(http.StatusInternalServerError, err.Error())
		}
	}

	existing, err := liveAssistantSummaryNoteID(conv)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}

	noteID, created := existing, false
	if existing == "" {
		doc, err := createNoteDocument(createNoteRequest{Body: req.Body, Title: req.Title, Type: req.Type, typeIsSuggestion: true})
		switch {
		case errors.Is(err, errNoteTypeInvalid):
			return fail(http.StatusBadRequest, "invalid note type")
		case err != nil:
			return writeDocumentError(c, err)
		}
		noteID, created = doc.ID, true
	} else {
		payload := map[string]any{"body": req.Body}
		if t := strings.TrimSpace(req.Title); t != "" {
			payload["title"] = t
		}
		// Re-apply the suggested type only while the note's type is still an
		// automatic one; an operator's own choice in Documents wins.
		if t := strings.TrimSpace(req.Type); t != "" {
			cur, err := globalDocumentStore.Get(existing)
			if err != nil {
				return fail(http.StatusInternalServerError, err.Error())
			}
			if cur.NoteTypeSource != "operator" {
				payload["type"] = t
			}
		}
		status, respBody, err := patchNoteInProcess(existing, payload, true)
		if err != nil {
			return fail(http.StatusInternalServerError, err.Error())
		}
		if status != http.StatusOK {
			return c.JSONBlob(status, respBody)
		}
	}

	if err := globalAssistantStore.SetSummaryNote(id, noteID); err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	// Links follow what the dialog showed: every ticked equipment is linked
	// and every removal unlinked, diffed against the note's real links (a
	// note just created has none, even if the draft showed some as linked).
	current, err := equipmentLinkedToDocument(globalDocumentStore, noteID)
	if err != nil {
		return fail(http.StatusInternalServerError, err.Error())
	}
	for _, eid := range req.EquipmentIDs {
		if current[eid] {
			continue
		}
		if err := globalDocumentStore.PatchEquipmentDocuments(eid, []string{noteID}, nil); err != nil {
			log.Printf("assistant: summary note %s: link %s: %v", noteID, eid, err)
			return fail(http.StatusInternalServerError, "The note was saved but linking it to equipment failed: "+err.Error())
		}
	}
	for _, eid := range req.RemoveEquipmentIDs {
		if !current[eid] {
			continue
		}
		if err := globalDocumentStore.PatchEquipmentDocuments(eid, nil, []string{noteID}); err != nil {
			log.Printf("assistant: summary note %s: unlink %s: %v", noteID, eid, err)
			return fail(http.StatusInternalServerError, "The note was saved but unlinking it from equipment failed: "+err.Error())
		}
	}
	return c.JSON(http.StatusOK, map[string]any{"note_id": noteID, "created": created})
}
