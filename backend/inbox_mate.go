package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
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

	// inboxClassifyPage is how many waiting items one read returns.
	inboxClassifyPage = 50
	// inboxClassifyWorkers is how many photos are sorted at once.
	inboxClassifyWorkers = 2
	// inboxMateCalls is the most Mate calls in flight at once across the whole
	// Inbox, sorting and titles together.
	inboxMateCalls = 3
	// inboxMateSessionTTL is how long a reading of the Mate settings is reused.
	inboxMateSessionTTL = 10 * time.Second
)

// inboxMateCallTimeout bounds one vision call. A var for tests.
var inboxMateCallTimeout = 90 * time.Second

var inboxKinds = map[string]bool{inboxKindScenery: true, inboxKindDocument: true, inboxKindEquipment: true}

// inboxMateSlots is the one limit on Mate calls, shared by sorting and titles.
var inboxMateSlots = make(chan struct{}, inboxMateCalls)

// inboxClassifyPrompt asks for one of three kinds. The document model reads
// the photo's small preview.
const inboxClassifyPrompt = `Look at this photo and say what it is for. Reply with a JSON object: {"kind": "scenery" | "document" | "equipment"}. ` +
	`"scenery" is a picture of the sea, the coast, wildlife, people or anything else seen from a boat on passage. ` +
	`"document" is a receipt, invoice, form, manual page, chart or other paperwork, photographed or scanned. ` +
	`"equipment" is a close picture of a piece of gear on the boat, such as an engine, a pump, a fitting, a label or a fault. ` +
	`If it could be two of these, pick the one the skipper would file it under. ` + documentEnrichJSONInstruction

// inboxTitlePrompt asks for one caption. The boat's course over ground and the
// five nearest named features follow it in the same message.
const inboxTitlePrompt = `Give this photo one title for a passage replay: a plain caption a skipper would write on a sign, at most 80 characters, such as "Dolphins off the bow" or "Anchored in the lee of the island". ` +
	`Reply with a JSON object: {"title": string}. Write one title and nothing else. No note and no explanation. ` +
	`Below are the boat's course over ground when the photo was taken and the five nearest named islands and bays, each with its distance and the side of the course it lies on. ` +
	`Use a feature's name only if the photo shows it. If the photo shows no named feature, name what the photo shows instead. Never use a name just because it is on the list. ` + documentEnrichJSONInstruction

// inboxTitleSectorNote is added when the phone recorded the direction it faced.
const inboxTitleSectorNote = `A feature marked "(in the direction the camera pointed)" lies within 25° of where the phone faced, so it is the likeliest to be in the frame. Check the photo before you use its name.`

// inboxTitleTruncatedNote is added when the provider cut the feature list short.
const inboxTitleTruncatedNote = `There are more named places nearby than could be listed, so the list may be incomplete: the photo may show a place that is not on it.`

// inboxMate is how the Inbox reaches the document vision model: the readiness
// check and the HTTP client. main wires the real ones; tests wire fakes. The
// reading of the settings is kept for inboxMateSessionTTL, so a list request or
// a burst of title lookups does not read the settings and the secrets store
// each time.
type inboxMate struct {
	readiness func() (assistantReadiness, string, error)
	doer      openRouterDoer

	mu       sync.Mutex
	cachedAt time.Time
	cached   inboxMateSession
	cachedOK bool
	// gen counts the times the reading was dropped, so a read that began before
	// a settings save is not kept after it.
	gen int
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
// not an error; a settings file or secrets store that cannot be read is one,
// and is not remembered. The read happens outside the lock, so a slow one does
// not hold up a settings save or another caller; two callers may both read.
func (m *inboxMate) session() (inboxMateSession, bool, error) {
	if m == nil {
		return inboxMateSession{}, false, nil
	}
	m.mu.Lock()
	if !m.cachedAt.IsZero() && time.Since(m.cachedAt) < inboxMateSessionTTL {
		sess, ok := m.cached, m.cachedOK
		m.mu.Unlock()
		return sess, ok, nil
	}
	gen := m.gen
	m.mu.Unlock()

	readiness, key, err := m.readiness()
	if err != nil {
		return inboxMateSession{}, false, fmt.Errorf("check Mate for the Inbox: %w", err)
	}
	sess, ok := inboxMateSession{}, false
	if documentEnrichReadinessProblem(readiness) == "" {
		sess, ok = inboxMateSession{model: strings.TrimSpace(readiness.DocumentModel), apiKey: key, doer: m.doer}, true
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.gen == gen { // the settings were not saved while this was read
		m.cachedAt, m.cached, m.cachedOK = time.Now(), sess, ok
	}
	return sess, ok, nil
}

// inboxMateOn says whether Mate is set up for the Inbox.
func inboxMateOn() (bool, error) {
	_, ok, err := globalInboxMate.session()
	return ok, err
}

// forget drops the remembered reading of the settings, so the next ask reads
// them again. A settings save calls it, so turning Mate on takes effect at once.
func (m *inboxMate) forget() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedAt = time.Time{}
	m.gen++
}

// inboxMateSettingsChanged is called when a save changed something Mate depends
// on (the assistant switch, the document model or the OpenRouter key): Mate may
// have been switched on or fixed, so every item that failed to sort is queued
// again at once, whatever its back-off said, and what is waiting is sorted
// without a new upload. The remembered reading is dropped before it returns; the
// requeue runs in the background, so a save never waits on the Inbox's store.
func inboxMateSettingsChanged() {
	globalInboxMate.forget()
	store := globalInboxStore
	go func() {
		if store != nil {
			if _, err := store.RequeueFailed(); err != nil {
				log.Printf("inbox: %v", err)
			}
		}
		inboxClassifierWake()
	}()
}

// errInboxMateTimedOut is a Mate call that ran out of its time once it held a
// slot.
var errInboxMateTimedOut = errors.New("Mate didn't answer in time")

// inboxMateReplyError is Mate answering with something that cannot be used: not
// the JSON asked for, or a kind that does not exist. Asking at once would pay
// for the same answer, so the classifier leaves it to the back-off.
type inboxMateReplyError struct{ msg string }

func (e *inboxMateReplyError) Error() string { return e.msg }

// askJSON sends one prompt and one image to the document model and decodes the
// JSON it replies with. It waits for one of the inboxMateCalls slots first, and
// the call's own timeout starts only once it holds one, so a long queue does not
// use up the time the call is allowed.
func (s inboxMateSession) askJSON(ctx context.Context, timeout time.Duration, prompt, mime string, image []byte, into any) error {
	select {
	case inboxMateSlots <- struct{}{}:
		defer func() { <-inboxMateSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	raw, err := openRouterImageText(callCtx, s.doer, s.apiKey, s.model, prompt, mime, image)
	if err != nil {
		if ctx.Err() == nil && errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return fmt.Errorf("%w (%v)", errInboxMateTimedOut, err)
		}
		return err
	}
	if err := decodeModelJSON(raw, into); err != nil {
		return &inboxMateReplyError{fmt.Sprintf("Mate's reply was not the JSON asked for: %s", truncateRunes(raw, 80))}
	}
	return nil
}

// ── sorting on arrival ───────────────────────────────────────────────────

// inboxClassifierWake nudges the classifier after an item is stored. A no-op
// until main starts one, so uploads work the same before and in tests.
var inboxClassifierWake = func() {}

// inboxSortFailedMessage is what a failed sort records on the item: one plain
// line, never the provider's words (the cause goes to the log). It is also the
// stored mark that the item failed, so it is never empty. The list adds when
// Mate will try again.
const inboxSortFailedMessage = "Mate couldn't sort this yet"

// inboxSortFailedLine is the line the tile shows. While a try is still ahead and
// Mate is on it gives the time on the boat's clock, "Mate couldn't sort this yet.
// Trying again at 3.40 PM". When that time has passed it says "Trying again
// shortly", and when Mate is off it says so, "Mate couldn't sort this. Mate is
// off", since no try is coming. An item with no try scheduled shows the failure
// alone.
func inboxSortFailedLine(it inboxItem, boat boatTime, mateOn bool, now time.Time) string {
	if it.ClassifyError == "" {
		return ""
	}
	if !mateOn {
		return "Mate couldn't sort this. Mate is off"
	}
	if it.NextTryMs <= 0 {
		return it.ClassifyError
	}
	at := time.UnixMilli(it.NextTryMs)
	if !at.After(now) {
		return it.ClassifyError + ". Trying again shortly"
	}
	clock := boatClock(at, boat.Loc)
	if boat.NoFix {
		clock += " UTC" // no position has been seen, so this is not the boat's clock
	}
	return it.ClassifyError + ". Trying again at " + clock
}

// inboxRetryDelay is how long a failed sort waits before the next try, given
// how many times the item has now failed: 1 minute, 5, 30, then hourly.
func inboxRetryDelay(attempts int) time.Duration {
	switch {
	case attempts <= 1:
		return time.Minute
	case attempts == 2:
		return 5 * time.Minute
	case attempts == 3:
		return 30 * time.Minute
	}
	return time.Hour
}

// inboxClassifier sorts waiting items, two at a time, off the upload request.
type inboxClassifier struct {
	store      *inboxStore
	workers    int
	retryDelay time.Duration
	// errorBackoff is how long Run waits after a failed pass before it tries again.
	errorBackoff time.Duration
	// backoff is how long a failure waits, given its attempts so far;
	// now is the clock. Both are fields so tests can move time.
	backoff func(attempts int) time.Duration
	now     func() time.Time
	wake    chan struct{}
}

func newInboxClassifier(store *inboxStore) *inboxClassifier {
	return &inboxClassifier{
		store: store, workers: inboxClassifyWorkers, retryDelay: 2 * time.Second,
		errorBackoff: documentsIndexerErrorBackoff, backoff: inboxRetryDelay, now: time.Now,
		wake: make(chan struct{}, 1),
	}
}

// Wake asks Run for another pass; many wakes before the pass starts make one.
func (c *inboxClassifier) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run sorts what is waiting, then waits to be woken, until ctx ends. A pass that
// fails (the settings or the store could not be read, an answer could not be
// saved) is tried again after the back-off, or sooner if woken, as the document
// indexer does. When a failed item is waiting to be retried, Run also wakes
// itself when the earliest comes due.
func (c *inboxClassifier) Run(ctx context.Context) {
	for {
		var wait <-chan time.Time // nil never fires: all sorted, so only a wake ends the wait
		if err := c.RunPending(ctx); err != nil && ctx.Err() == nil {
			log.Printf("inbox: sorting: %v", err)
			wait = time.After(c.errorBackoff)
		} else if due, ok := c.untilNextRetry(); ok {
			wait = time.After(due)
		}
		select {
		case <-c.wake:
		case <-wait:
		case <-ctx.Done():
			return
		}
	}
}

// untilNextRetry is how long until the earliest failed item is due again.
// A retry already due that the pass did not take (Mate was switched off) is
// looked at again after the error back-off rather than in a tight loop.
func (c *inboxClassifier) untilNextRetry() (time.Duration, bool) {
	next, err := c.store.NextRetryMs()
	if err != nil {
		log.Printf("inbox: sorting: %v", err)
		return c.errorBackoff, true
	}
	if next == 0 {
		return 0, false
	}
	if d := time.UnixMilli(next).Sub(c.now()); d > 0 {
		return d, true
	}
	return c.errorBackoff, true
}

// RunPending sorts every item Mate should look at now, a page at a time, with
// bounded concurrency. Mate not being set up is a normal state: it does nothing.
// A failure to sort one item is recorded on that item, not returned; a failure
// to record anything is returned, after the pass has gone through every item,
// so Run backs off.
func (c *inboxClassifier) RunPending(ctx context.Context) error {
	if _, ok, err := globalInboxMate.session(); err != nil || !ok {
		return err
	}
	var after inboxCursor
	var saveErr error
	for ctx.Err() == nil {
		page, err := c.store.UnclassifiedAfter(c.now(), after)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}
		// The next page starts after this one, whatever became of its items, so a
		// pass always moves on.
		last := page[len(page)-1]
		after = inboxCursor{createdMs: last.CreatedMs, id: last.ID}
		stopped, err := c.sortPage(ctx, page)
		if err != nil && saveErr == nil {
			saveErr = err
		}
		if stopped {
			break
		}
	}
	return saveErr
}

// sortPage sorts the items, and returns the first failure to record an answer.
// It reads Mate's settings before each item (they are remembered for a few
// seconds and dropped on a settings save), so switching Mate off or changing the
// model or key takes effect within the pass. stopped says Mate is no longer set
// up, so the pass ends.
func (c *inboxClassifier) sortPage(ctx context.Context, items []inboxItem) (stopped bool, first error) {
	sem := make(chan struct{}, c.workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var off atomic.Bool
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if first == nil {
			first = err
		}
	}
loop:
	for _, it := range items {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break loop
		}
		if off.Load() {
			<-sem
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			sess, ok, err := globalInboxMate.session()
			if err != nil {
				off.Store(true) // settings that cannot be read: stop, and back off
				fail(err)
				return
			}
			if !ok {
				off.Store(true)
				return
			}
			if err := c.classify(ctx, sess, it); err != nil {
				fail(err)
			}
		}()
	}
	wg.Wait()
	return off.Load(), first
}

// classify sorts one item and records the answer, or the failure so the Inbox
// shows it. Every failure is tried again after a back-off, whatever its cause:
// the cause goes to the log and the tile gets one plain line. An item that was
// shut down on stays waiting for the next start. It returns an error only when
// the outcome could not be recorded.
func (c *inboxClassifier) classify(ctx context.Context, sess inboxMateSession, it inboxItem) error {
	kind, err := c.sort(ctx, sess, it)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		if _, gone := c.store.Get(it.ID); errors.Is(gone, errInboxItemNotFound) {
			return nil // filed or deleted meanwhile
		}
		log.Printf("inbox: Mate could not sort %s: %v", it.ID, err)
		attempts := it.ClassifyAttempts + 1
		err = c.store.SetClassifyRetry(it.ID, inboxSortFailedMessage, attempts, c.now().Add(c.backoff(attempts)).UnixMilli())
		if err != nil {
			log.Printf("inbox: record Mate's answer for %s: %v", it.ID, err)
		}
		return err
	}
	if err := c.store.SetSuggestedKind(it.ID, kind); err != nil {
		log.Printf("inbox: record Mate's answer for %s: %v", it.ID, err)
		return err
	}
	return nil
}

// sort says what an item is. A PDF is a document without a call. A photo is
// sorted from its thumbnail, asking the model once more after a failure that is
// not a timeout or an unusable answer; a photo with no thumbnail is not sent
// full size.
func (c *inboxClassifier) sort(ctx context.Context, sess inboxMateSession, it inboxItem) (string, error) {
	if it.MIME == "application/pdf" {
		return inboxKindDocument, nil
	}
	if !it.HasThumb {
		return "", errors.New("no preview to sort from")
	}
	mime, image, err := inboxImageBytes(it, true)
	if err != nil {
		return "", err
	}
	kind, err := askKind(ctx, sess, mime, image)
	var unusable *inboxMateReplyError
	if err == nil || ctx.Err() != nil || errors.As(err, &unusable) || errors.Is(err, errInboxMateTimedOut) {
		return kind, err // an unusable answer or a call that ran its full time would cost the same again; the back-off asks later
	}
	if !sleepCtx(ctx, c.retryDelay) {
		return "", ctx.Err()
	}
	return askKind(ctx, sess, mime, image)
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
	if err := sess.askJSON(ctx, inboxMateCallTimeout, inboxClassifyPrompt, mime, image, &reply); err != nil {
		return "", err
	}
	kind := strings.ToLower(strings.TrimSpace(reply.Kind))
	if !inboxKinds[kind] {
		return "", &inboxMateReplyError{fmt.Sprintf("Mate answered %q, which is not scenery, document or equipment", truncateRunes(reply.Kind, 40))}
	}
	return kind, nil
}

// errInboxPhotoTooBig is a stored photo over the size Mate takes.
var errInboxPhotoTooBig = errors.New("the photo is over the size limit for Mate")

// inboxImageBytes reads the picture Mate looks at: the thumbnail when useThumb
// is set and the item has one, else the stored photo (at most 2560 px across),
// which must be within the size Mate takes.
func inboxImageBytes(it inboxItem, useThumb bool) (mime string, data []byte, err error) {
	path, mime := inboxFilePath(it.SHA256), it.MIME
	if useThumb && it.HasThumb {
		path, mime = inboxThumbPath(it.SHA256), "image/jpeg"
	} else if it.SizeBytes > documentsImageMaxBytes {
		return "", nil, fmt.Errorf("%w: %d bytes, limit %d", errInboxPhotoTooBig, it.SizeBytes, documentsImageMaxBytes)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("read the photo: %w", err)
	}
	return mime, data, nil
}

// ── titles ───────────────────────────────────────────────────────────────

// inboxTitleInput is what Mate is told about where a photo was taken.
type inboxTitleInput struct {
	// CourseDeg is the boat's course over ground, true; nil when it was at rest
	// and the course meant nothing.
	CourseDeg *float64
	// CameraDeg is the true bearing the phone faced, when it recorded one.
	CameraDeg *float64
	Features  []inboxFeature
	// Truncated says the provider cut the feature list short.
	Truncated bool
}

// inboxTitleMessage is the text sent with the photo: the prompt, then the
// boat's course and the nearest features.
func inboxTitleMessage(in inboxTitleInput) string {
	var b strings.Builder
	b.WriteString(inboxTitlePrompt)
	if in.CameraDeg != nil {
		b.WriteString("\n\n" + inboxTitleSectorNote)
	}
	if in.Truncated {
		b.WriteString("\n\n" + inboxTitleTruncatedNote)
	}
	b.WriteString("\n\n")
	if in.CourseDeg != nil {
		fmt.Fprintf(&b, "The boat was under way, course over ground %s true.", bearingText(*in.CourseDeg))
	} else {
		b.WriteString("The boat's course over ground is not known (it was barely moving), so each bearing below is from true north.")
	}
	if in.CameraDeg != nil {
		fmt.Fprintf(&b, "\nThe phone faced %s true.", bearingText(*in.CameraDeg))
	}
	if len(in.Features) == 0 {
		b.WriteString("\nNo named islands or bays within 10 nm.")
		return b.String()
	}
	b.WriteString("\nNearest named features:")
	for _, f := range in.Features {
		b.WriteString("\n- " + f.describe(in.CourseDeg))
		if f.InSector {
			b.WriteString(" (in the direction the camera pointed)")
		}
	}
	return b.String()
}

// mateTitle asks the document model for one title for a photo. The 2560 px
// stored photo is sent when there is a named feature to check against, since a
// headland or an island is a few pixels in a 400 px preview; with none in range
// the thumbnail is enough to say what the photo shows. A failure comes back in
// words for the operator, with the cause kept for the log.
func mateTitle(ctx context.Context, sess inboxMateSession, it inboxItem, in inboxTitleInput) (string, error) {
	mime, image, err := inboxImageBytes(it, len(in.Features) == 0)
	if errors.Is(err, errInboxPhotoTooBig) {
		return "", &inboxTitleError{msg: "This photo is too big for Mate to look at, so no title was suggested.", cause: err}
	} else if err != nil {
		return "", &inboxTitleError{msg: "Helmcentral couldn't read this photo, so no title was suggested.", cause: err}
	}
	var reply struct {
		Title string `json:"title"`
	}
	if err := sess.askJSON(ctx, inboxMateTitleTimeout, inboxTitleMessage(in), mime, image, &reply); err != nil {
		var badReply *inboxMateReplyError
		switch {
		case errors.Is(err, errInboxMateTimedOut):
			return "", &inboxTitleError{msg: errInboxMateTimedOut.Error(), cause: err}
		case errors.As(err, &badReply):
			return "", &inboxTitleError{msg: "Mate's answer couldn't be used, so no title was suggested.", cause: err}
		case ctx.Err() != nil:
			return "", err // the request's own deadline or cancellation, told by the caller
		}
		return "", &inboxTitleError{msg: "Mate couldn't reach the model, so no title was suggested.", cause: err}
	}
	title := joinTitle(strings.TrimSpace(reply.Title))
	if title == "" {
		return "", &inboxTitleError{msg: "Mate's answer couldn't be used, so no title was suggested.", cause: errors.New("Mate gave no title")}
	}
	return title, nil
}
