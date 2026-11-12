package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// Mate on the Inbox (ADR 0282). When Mate is set up, each photo that arrives is
// sorted by the document vision model, and a photo going onto a passage is
// titled by it. When Mate is not set up none of this runs and the Inbox works
// as it did.

const (
	inboxKindScenery   = "scenery"
	inboxKindDocument  = "document"
	inboxKindEquipment = "equipment"

	// inboxClassifyWorkers is how many photos are sorted at once.
	inboxClassifyWorkers = 2
	// inboxMateCallTimeout bounds one vision call.
	inboxMateCallTimeout = 90 * time.Second
	// inboxClassifyErrorMax keeps a recorded failure short enough for a tile.
	inboxClassifyErrorMax = 200
)

var inboxKinds = map[string]bool{inboxKindScenery: true, inboxKindDocument: true, inboxKindEquipment: true}

// inboxClassifyPrompt asks for one of three kinds. The document model reads
// the photo's small preview.
const inboxClassifyPrompt = `Look at this photo and say what it is for. Reply with a JSON object: {"kind": "scenery" | "document" | "equipment"}. ` +
	`"scenery" is a picture of the sea, the coast, wildlife, people or anything else seen from a boat on passage. ` +
	`"document" is a receipt, invoice, form, manual page, chart or other paperwork, photographed or scanned. ` +
	`"equipment" is a close picture of a piece of gear on the boat, such as an engine, a pump, a fitting, a label or a fault. ` +
	`If it could be two of these, pick the one the skipper would file it under. ` + documentEnrichJSONInstruction

// inboxTitlePrompt asks for one caption. The boat's heading and the five
// nearest named features follow it in the same message.
const inboxTitlePrompt = `Give this photo one title for a passage replay: a plain caption a skipper would write on a sign, at most 80 characters, such as "Dolphins off the bow" or "Anchored in the lee of the island". ` +
	`Reply with a JSON object: {"title": string}. Write one title and nothing else. No note and no explanation. ` +
	`Below are the boat's heading when the photo was taken and the five nearest named islands and bays, each with its distance and the side of the bow it lies on. ` +
	`Use a feature's name only if the photo shows it. If the photo shows no named feature, name what the photo shows instead. Never use a name just because it is on the list. ` + documentEnrichJSONInstruction

// inboxTitleSectorNote is added when the phone recorded the direction it faced.
const inboxTitleSectorNote = `A feature marked "(in the direction the camera pointed)" lies within 25° of where the phone faced, so it is the likeliest to be in the frame. Check the photo before you use its name.`

// inboxMate is how the Inbox reaches the document vision model: the readiness
// check and the HTTP client. main wires the real ones; tests wire fakes.
type inboxMate struct {
	readiness func() (assistantReadiness, string, error)
	doer      openRouterDoer
}

// globalInboxMate is nil until main wires it, which reads as Mate not set up.
var globalInboxMate *inboxMate

// inboxMateSession is one reading of the settings: the model and key to use.
type inboxMateSession struct {
	model  string
	apiKey string
	doer   openRouterDoer
}

// session says whether Mate is set up and, if so, what to call. An assistant
// that is switched off or has no key or no document model is a normal state,
// not an error; a settings file or secrets store that cannot be read is one.
func (m *inboxMate) session() (inboxMateSession, bool, error) {
	if m == nil {
		return inboxMateSession{}, false, nil
	}
	readiness, key, err := m.readiness()
	if err != nil {
		return inboxMateSession{}, false, fmt.Errorf("check Mate for the Inbox: %w", err)
	}
	if documentEnrichReadinessProblem(readiness) != "" {
		return inboxMateSession{}, false, nil
	}
	return inboxMateSession{model: strings.TrimSpace(readiness.DocumentModel), apiKey: key, doer: m.doer}, true, nil
}

// inboxMateOn says whether Mate is set up for the Inbox.
func inboxMateOn() (bool, error) {
	_, ok, err := globalInboxMate.session()
	return ok, err
}

// ask sends one prompt and one image to the document model and returns its text.
func (s inboxMateSession) ask(ctx context.Context, prompt, mime string, image []byte) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, inboxMateCallTimeout)
	defer cancel()
	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(image)
	req := openRouterChatRequest{
		Model: s.model,
		Messages: []openRouterMessage{openRouterUserMessage(
			openRouterContentBlock{Type: "text", Text: prompt},
			openRouterContentBlock{Type: "image_url", ImageURL: &openRouterImageURLBlock{URL: dataURL}},
		)},
	}
	resp, err := openRouterChatCompletionOnce(ctx, s.doer, s.apiKey, req)
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("openrouter returned no choices")
	}
	return string(resp.Choices[0].Message.Content), nil
}

// askJSON asks and decodes the reply, tolerating a markdown fence around it.
func (s inboxMateSession) askJSON(ctx context.Context, prompt, mime string, image []byte, into any) error {
	raw, err := s.ask(ctx, prompt, mime, image)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(stripDocumentEnrichJSONFence(raw)), into); err != nil {
		return fmt.Errorf("Mate's reply was not the JSON asked for: %s", truncateRunes(raw, 80))
	}
	return nil
}

// ── sorting on arrival ───────────────────────────────────────────────────

// inboxClassifierWake nudges the classifier after an item is stored. A no-op
// until main starts one, so uploads work the same before and in tests.
var inboxClassifierWake = func() {}

// inboxClassifier sorts waiting items, two at a time, off the upload request.
type inboxClassifier struct {
	store      *inboxStore
	workers    int
	retryDelay time.Duration
	wake       chan struct{}
}

func newInboxClassifier(store *inboxStore) *inboxClassifier {
	return &inboxClassifier{store: store, workers: inboxClassifyWorkers, retryDelay: 2 * time.Second, wake: make(chan struct{}, 1)}
}

// Wake asks Run for another pass; many wakes before the pass starts make one.
func (c *inboxClassifier) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run sorts what is waiting, then waits to be woken, until ctx ends.
func (c *inboxClassifier) Run(ctx context.Context) {
	for {
		if err := c.RunPending(ctx); err != nil && ctx.Err() == nil {
			log.Printf("inbox: sorting: %v", err)
		}
		select {
		case <-c.wake:
		case <-ctx.Done():
			return
		}
	}
}

// RunPending sorts every item Mate has not yet looked at, with bounded
// concurrency. Mate not being set up is a normal state: it does nothing. A
// failure to sort one item is recorded on that item, not returned.
func (c *inboxClassifier) RunPending(ctx context.Context) error {
	sess, ok, err := globalInboxMate.session()
	if err != nil || !ok {
		return err
	}
	items, err := c.store.Unclassified()
	if err != nil {
		return err
	}
	sem := make(chan struct{}, c.workers)
	var wg sync.WaitGroup
loop:
	for _, it := range items {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			c.classify(ctx, sess, it)
		}()
	}
	wg.Wait()
	return nil
}

// classify sorts one item. A PDF is a document without a call. Otherwise the
// model is asked, once more after a failure, and a second failure is recorded
// against the item so the Inbox shows it.
func (c *inboxClassifier) classify(ctx context.Context, sess inboxMateSession, it inboxItem) {
	if it.MIME == "application/pdf" {
		c.record(it.ID, c.store.SetSuggestedKind(it.ID, inboxKindDocument))
		return
	}
	mime, image, err := inboxPreviewBytes(it)
	var kind string
	if err == nil {
		for attempt := 0; attempt < 2; attempt++ {
			if kind, err = askKind(ctx, sess, mime, image); err == nil {
				break
			}
			if ctx.Err() != nil {
				return // shutting down: the item stays waiting for the next start
			}
			if attempt == 0 && !sleepCtx(ctx, c.retryDelay) {
				return
			}
		}
	}
	if err != nil {
		if _, gone := c.store.Get(it.ID); errors.Is(gone, errInboxItemNotFound) {
			return // filed or deleted meanwhile
		}
		log.Printf("inbox: Mate could not sort %s: %v", it.ID, err)
		c.record(it.ID, c.store.SetClassifyError(it.ID, truncateRunes(firstErrorLine(err), inboxClassifyErrorMax)))
		return
	}
	c.record(it.ID, c.store.SetSuggestedKind(it.ID, kind))
}

func (c *inboxClassifier) record(id string, err error) {
	if err != nil {
		log.Printf("inbox: record Mate's answer for %s: %v", id, err)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func askKind(ctx context.Context, sess inboxMateSession, mime string, image []byte) (string, error) {
	var reply struct {
		Kind string `json:"kind"`
	}
	if err := sess.askJSON(ctx, inboxClassifyPrompt, mime, image, &reply); err != nil {
		return "", err
	}
	kind := strings.ToLower(strings.TrimSpace(reply.Kind))
	if !inboxKinds[kind] {
		return "", fmt.Errorf("Mate answered %q, which is not scenery, document or equipment", truncateRunes(reply.Kind, 40))
	}
	return kind, nil
}

// inboxPreviewBytes is the small picture Mate sorts from: the thumbnail the
// phone sent, else the stored photo. Sorting a receipt from scenery does not
// need more than the thumbnail's 400 px, and it is about a tenth the bytes.
func inboxPreviewBytes(it inboxItem) (mime string, data []byte, err error) {
	path, mime := inboxFilePath(it.SHA256), it.MIME
	if it.HasThumb {
		path, mime = inboxThumbPath(it.SHA256), "image/jpeg"
	} else if it.SizeBytes > documentsImageMaxBytes {
		return "", nil, fmt.Errorf("the photo is %d bytes, over the %d byte limit for Mate", it.SizeBytes, documentsImageMaxBytes)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read the photo: %w", err)
	}
	return mime, data, nil
}

// ── titles ───────────────────────────────────────────────────────────────

// inboxTitleMessage is the text sent with the photo: the prompt, then the
// boat's heading and the nearest features. headingDeg is nil when the boat was
// at rest and its course over ground meant nothing. cameraDeg, when not nil, is
// the true bearing the phone faced.
func inboxTitleMessage(headingDeg, cameraDeg *float64, features []inboxFeature) string {
	var b strings.Builder
	b.WriteString(inboxTitlePrompt)
	if cameraDeg != nil {
		b.WriteString("\n\n" + inboxTitleSectorNote)
	}
	b.WriteString("\n\n")
	if headingDeg != nil {
		fmt.Fprintf(&b, "The boat was on heading %03d° true.", int(*headingDeg+0.5)%360)
	} else {
		b.WriteString("The boat's heading is not known (it was barely moving), so each bearing below is from true north.")
	}
	if cameraDeg != nil {
		fmt.Fprintf(&b, "\nThe phone faced %03d° true.", int(*cameraDeg+0.5)%360)
	}
	if len(features) == 0 {
		b.WriteString("\nNo named islands or bays within 10 nm.")
		return b.String()
	}
	b.WriteString("\nNearest named features:")
	for _, f := range features {
		b.WriteString("\n- " + f.describe(headingDeg))
		if f.InSector {
			b.WriteString(" (in the direction the camera pointed)")
		}
	}
	return b.String()
}

// mateTitle asks the document model for one title for a photo. The stored
// photo, at most 2560 px across, is sent rather than the thumbnail: a headland
// or an island is a few pixels in a 400 px preview, and only the photos the
// operator is putting on a passage are asked about.
func mateTitle(ctx context.Context, sess inboxMateSession, it inboxItem, headingDeg, cameraDeg *float64, features []inboxFeature) (string, error) {
	if it.SizeBytes > documentsImageMaxBytes {
		return "", fmt.Errorf("the photo is %d bytes, over the %d byte limit for Mate", it.SizeBytes, documentsImageMaxBytes)
	}
	image, err := os.ReadFile(inboxFilePath(it.SHA256))
	if err != nil {
		return "", fmt.Errorf("read the photo: %w", err)
	}
	var reply struct {
		Title string `json:"title"`
	}
	if err := sess.askJSON(ctx, inboxTitleMessage(headingDeg, cameraDeg, features), it.MIME, image, &reply); err != nil {
		return "", err
	}
	title := joinTitle(strings.TrimSpace(reply.Title))
	if title == "" {
		return "", errors.New("Mate gave no title")
	}
	return title, nil
}
