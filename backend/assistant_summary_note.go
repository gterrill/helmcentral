package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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
	// Caps keep a long conversation inside the model's context and the
	// timeout. A message over the per-message cap is cut with a visible
	// marker. Over the total cap, the opening exchange (what the operator
	// came to ask) and the most recent messages (where it was settled) are
	// kept and the middle is replaced by a marker saying how many were left
	// out; nothing is dropped silently.
	assistantSummaryMessageRunes    = 4000
	assistantSummaryAttachmentRunes = 1500
	assistantSummaryTranscriptRunes = 60000
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
- Invent nothing. If a section has nothing, return it empty.
- Ruled out holds only actions or options for the boat that the operator or the conversation rejected, each with why. It never holds Mate's own limitations, unreadable documents, tool failures or anything about the assistant itself.
- Leave Mate's own limitations, unreadable documents, tool failures and anything about the assistant itself out of every section.`

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
func renderAssistantSummaryNote(d assistantSummaryDraft, convID string, when time.Time) (string, bool) {
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
	// No conversation title: it is the clipped first message and reads badly.
	fmt.Fprintf(&b, "From [a Mate conversation](/mate/%s) on %s.\n", convID, when.UTC().Format("2 Jan 2006"))
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
		// Several records with the same exact name, or the same alias, are
		// ambiguous: dropped, never guessed.
		var exact, aliased []*equipmentItem
		for i := range items {
			if strings.EqualFold(strings.TrimSpace(items[i].Name), name) {
				exact = append(exact, &items[i])
				continue
			}
			for _, alias := range items[i].Aliases {
				if strings.EqualFold(strings.TrimSpace(alias), name) {
					aliased = append(aliased, &items[i])
					break
				}
			}
		}
		var pick *equipmentItem
		switch {
		case len(exact) > 0:
			if len(exact) == 1 {
				pick = exact[0]
			}
		case len(aliased) > 0:
			if len(aliased) == 1 {
				pick = aliased[0]
			}
		case len(items) == 1:
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
	var blocks []string
	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		var b strings.Builder
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
		blocks = append(blocks, b.String())
	}

	size := func(s string) int { return len([]rune(s)) }
	total := 0
	for _, blk := range blocks {
		total += size(blk)
	}
	if total <= assistantSummaryTranscriptRunes {
		return strings.Join(blocks, ""), nil
	}
	// Keep the opening message, then as many of the latest as fit.
	budget := assistantSummaryTranscriptRunes - size(blocks[0])
	start := len(blocks)
	for start > 1 {
		n := size(blocks[start-1])
		if n > budget {
			break
		}
		budget -= n
		start--
	}
	if start <= 1 {
		return strings.Join(blocks, ""), nil
	}
	omitted := start - 1
	marker := fmt.Sprintf("[%d earlier messages left out to keep this short]\n\n", omitted)
	if omitted == 1 {
		marker = "[1 earlier message left out to keep this short]\n\n"
	}
	return blocks[0] + marker + strings.Join(blocks[start:], ""), nil
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

	body, ok := renderAssistantSummaryNote(draft, conv.ID, conv.UpdatedAt)
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
		cur, err := globalDocumentStore.Get(existing)
		if err != nil {
			return fail(http.StatusInternalServerError, err.Error())
		}
		patch := notePatch{Body: &req.Body}
		if t := strings.TrimSpace(req.Title); t != "" {
			patch.Title = &t
		}
		// Re-apply the suggested type only while the note's type is still an
		// automatic one; an operator's own choice in Documents wins.
		if t := strings.TrimSpace(req.Type); t != "" && cur.NoteTypeSource != "operator" {
			if !validNoteType(t) {
				return fail(http.StatusBadRequest, "invalid note type")
			}
			patch.Type = &t
		}
		if _, _, err := patchNote(cur, patch, true); err != nil {
			var se *noteStatusError
			if errors.As(err, &se) {
				return fail(se.status, se.msg)
			}
			return writeDocumentError(c, err)
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
