package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
)

// The ways a photo's suggested title is made (ADR 0283): the nearest named
// place to the boat; the nearest named island or bay in the direction the
// camera pointed, without Mate; or five of the nearest features handed to the
// vision model, with Mate.

// inboxCell is a position snapped to a grid, so photos taken close together
// share one lookup. The grid is coarse for the 10 nm feature fetch and fine
// for the place name; this is the one cell type for both.
type inboxCell struct{ lat, lon int64 }

const (
	// inboxPlaceCellsPerDeg makes a place-name cell about 100 m across.
	inboxPlaceCellsPerDeg = 1000
	// inboxFeatureCellsPerDeg makes a feature cell 0.02 degrees, about 1.2 nm.
	// The features within 10 nm of any point in the cell are all within 11 nm of
	// its centre, which is what inboxFeatureFetchPadNm adds to the fetch.
	inboxFeatureCellsPerDeg = 50
)

func inboxCellAt(lat, lon float64, perDeg int) inboxCell {
	return inboxCell{int64(math.Round(lat * float64(perDeg))), int64(math.Round(lon * float64(perDeg)))}
}

func (c inboxCell) centre(perDeg int) (lat, lon float64) {
	return float64(c.lat) / float64(perDeg), float64(c.lon) / float64(perDeg)
}

// inboxFeatureTTL is how long the features fetched around a cell are reused,
// across requests. Islands and bays do not move; this only bounds the memory
// and lets a provider that has been fixed be asked again.
const inboxFeatureTTL = 10 * time.Minute

type inboxFeatureEntry struct {
	once    sync.Once
	at      time.Time
	fetched inboxFetched
	err     error
}

// inboxFeatureCache fetches the features around a cell once, however many
// photos were taken in it, however many workers ask and however many requests
// come within the TTL. A failed fetch is not kept, so the next request tries
// again (inboxRequestFeatures remembers it within one request).
type inboxFeatureCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	cells map[inboxCell]*inboxFeatureEntry
}

func newInboxFeatureCache(ttl time.Duration) *inboxFeatureCache {
	return &inboxFeatureCache{ttl: ttl, cells: map[inboxCell]*inboxFeatureEntry{}}
}

// inboxFeatures is the process-wide cache the title handler uses.
var inboxFeatures = newInboxFeatureCache(inboxFeatureTTL)

func (fc *inboxFeatureCache) get(lat, lon float64) (inboxFetched, error) {
	cell := inboxCellAt(lat, lon, inboxFeatureCellsPerDeg)
	now := time.Now()
	fc.mu.Lock()
	e, ok := fc.cells[cell]
	if !ok || now.Sub(e.at) >= fc.ttl {
		for c, old := range fc.cells { // drop what has expired, so the map stays small
			if now.Sub(old.at) >= fc.ttl {
				delete(fc.cells, c)
			}
		}
		e = &inboxFeatureEntry{at: now}
		fc.cells[cell] = e
	}
	fc.mu.Unlock()
	e.once.Do(func() {
		centreLat, centreLon := cell.centre(inboxFeatureCellsPerDeg)
		e.fetched, e.err = inboxFeaturesNear(centreLat, centreLon)
	})
	if e.err != nil {
		fc.mu.Lock()
		if fc.cells[cell] == e {
			delete(fc.cells, cell)
		}
		fc.mu.Unlock()
	}
	return e.fetched, e.err
}

// inboxNearby is the named features within 10 nm of a fix, nearest first.
type inboxNearby struct {
	Features []inboxFeature
	// Truncated says the provider cut its list short, so a feature nearer than
	// the nearest listed may be missing.
	Truncated bool
}

// inboxTitleError is why a title could not be made, in words for the operator.
// The cause is for the log only: the plugin, network and model errors under it
// name paths, hosts and providers the operator cannot act on.
type inboxTitleError struct {
	msg   string
	cause error
}

func (e *inboxTitleError) Error() string { return e.msg }
func (e *inboxTitleError) Unwrap() error { return e.cause }

// detail is the full text for the log.
func (e *inboxTitleError) detail() string {
	if e.cause == nil {
		return e.msg
	}
	return e.msg + ": " + e.cause.Error()
}

// inboxTitleFailure turns any error from making a title into the line the
// operator sees and the line the log gets. An error that is not one of ours is
// not shown: it may carry internals.
func inboxTitleFailure(err error) (shown, logged string) {
	var te *inboxTitleError
	if errors.As(err, &te) {
		return te.msg, te.detail()
	}
	return "Helmcentral couldn't suggest a title; the Helmcentral log says why.", err.Error()
}

// inboxPlaceNamesSilent is what the operator is told when the place-name server
// failed or did not answer, whichever title is being made.
const inboxPlaceNamesSilent = "The place-name server didn't answer, so no title was suggested."

func (fc *inboxFeatureCache) nearbyAt(pt *trackFix) (inboxNearby, error) {
	return nearbyFrom(fc.get, pt)
}

// nearbyFrom ranks what fetch returns around a fix, or says in the operator's
// words that the place-name server did not answer.
func nearbyFrom(fetch func(lat, lon float64) (inboxFetched, error), pt *trackFix) (inboxNearby, error) {
	fetched, err := fetch(pt.Lat, pt.Lon)
	if err != nil {
		return inboxNearby{}, &inboxTitleError{
			msg:   inboxPlaceNamesSilent,
			cause: fmt.Errorf("could not look up nearby features: %w", err),
		}
	}
	return inboxNearby{Features: nearbyFeatures(fetched.Features, pt.Lat, pt.Lon), Truncated: fetched.Truncated}, nil
}

// inboxNearbySource finds the named features near a fix.
type inboxNearbySource interface {
	nearbyAt(pt *trackFix) (inboxNearby, error)
}

// inboxRequestFeatures is the feature cache as one title request sees it. The
// shared cache keeps no failures, so a provider that is down would be asked
// again for every photo in a cell; here the first answer for a cell, a failure
// included, is the answer for the rest of the request. The next request asks
// again.
type inboxRequestFeatures struct {
	cache *inboxFeatureCache
	mu    sync.Mutex
	cells map[inboxCell]*inboxRequestCell
}

type inboxRequestCell struct {
	once    sync.Once
	fetched inboxFetched
	err     error
}

func newInboxRequestFeatures(cache *inboxFeatureCache) *inboxRequestFeatures {
	return &inboxRequestFeatures{cache: cache, cells: map[inboxCell]*inboxRequestCell{}}
}

func (r *inboxRequestFeatures) get(lat, lon float64) (inboxFetched, error) {
	cell := inboxCellAt(lat, lon, inboxFeatureCellsPerDeg)
	r.mu.Lock()
	e, ok := r.cells[cell]
	if !ok {
		e = &inboxRequestCell{}
		r.cells[cell] = e
	}
	r.mu.Unlock()
	e.once.Do(func() { e.fetched, e.err = r.cache.get(lat, lon) })
	return e.fetched, e.err
}

func (r *inboxRequestFeatures) nearbyAt(pt *trackFix) (inboxNearby, error) {
	return nearbyFrom(r.get, pt)
}

// courseAt is the boat's course over ground at a track fix while it was moving.
// At rest a course over ground is noise, so there is none.
func courseAt(pt *trackFix) *float64 {
	if pt.COGDeg == nil || pt.SOGKts == nil || *pt.SOGKts < passageRestSOGKts {
		return nil
	}
	return pt.COGDeg
}

// errInboxFeaturesCutShort is why a sector title is not guessed when the
// provider's list may lack the nearest feature.
var errInboxFeaturesCutShort = &inboxTitleError{msg: "too many named places nearby to pick one reliably"}

// sectorTitleAt is the title without Mate for a photo that carries a bearing:
// the nearest named feature in the direction it faced, else the nearest place
// to the boat. A feature lookup that fails, or that was cut short, is an error,
// not a quiet drop to the nearest place.
func sectorTitleAt(fc inboxNearbySource, it inboxItem, pt *trackFix) (string, error) {
	near, err := fc.nearbyAt(pt)
	if err != nil {
		return "", err
	}
	if near.Truncated {
		return "", errInboxFeaturesCutShort
	}
	if f, ok := nearestInSector(near.Features, *it.BearingDeg); ok {
		return joinTitle(f.Name), nil
	}
	return joinTitle(inboxPlaceLookup(pt.Lat, pt.Lon)), nil
}

// inboxSectorFeatureMax is the most features in the camera's sector that go to
// Mate; the rest of the five are the nearest others.
const inboxSectorFeatureMax = 2

// featuresForMate picks the features Mate is shown, nearest first: the ones in
// the camera's sector (at most two nearest) and the nearest others, five in all,
// so the feature the camera pointed at is never cut for being sixth. Without a
// camera bearing it is the five nearest.
func featuresForMate(ranked []inboxFeature, cameraTrueDeg *float64) []inboxFeature {
	if cameraTrueDeg == nil {
		if len(ranked) > inboxFeatureCount {
			ranked = ranked[:inboxFeatureCount]
		}
		return ranked
	}
	marked := markSector(ranked, *cameraTrueDeg)
	keep := make([]bool, len(marked))
	picked, inSector := 0, 0
	for i, f := range marked {
		if f.InSector && inSector < inboxSectorFeatureMax {
			keep[i], inSector = true, inSector+1
			picked++
		}
	}
	for i, f := range marked {
		if picked >= inboxFeatureCount {
			break
		}
		if !keep[i] && !f.InSector {
			keep[i] = true
			picked++
		}
	}
	for i, f := range marked { // few others: fill with the sector features left over
		if picked >= inboxFeatureCount {
			break
		}
		if !keep[i] && f.InSector {
			keep[i] = true
			picked++
		}
	}
	out := make([]inboxFeature, 0, picked)
	for i, f := range marked {
		if keep[i] {
			out = append(out, f)
		}
	}
	return out
}

// nearbyWithin finds the features near a fix, giving up after d: a place-name
// server that hangs is reported as such, not as Mate being slow. The fetch itself
// cannot be cancelled and is left to finish unheard.
func nearbyWithin(ctx context.Context, src inboxNearbySource, pt *trackFix, d time.Duration) (inboxNearby, error) {
	type result struct {
		near inboxNearby
		err  error
	}
	done := make(chan result, 1)
	go func() {
		near, err := src.nearbyAt(pt)
		done <- result{near, err}
	}()
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case r := <-done:
		return r.near, r.err
	case <-timer.C:
		return inboxNearby{}, &inboxTitleError{msg: inboxPlaceNamesSilent, cause: fmt.Errorf("the feature lookup took over %v", d)}
	case <-ctx.Done():
		return inboxNearby{}, ctx.Err()
	}
}

// mateTitleAt asks the vision model for a title for one photo with the boat's
// course over ground and five features, those in the camera's sector marked (see
// featuresForMate). A title made before, kept on the item, is returned without a
// call; a title just made is kept there, so a restart does not pay for it again.
func mateTitleAt(ctx context.Context, sess inboxMateSession, fc inboxNearbySource, store *inboxStore, it inboxItem, pt *trackFix) (string, error) {
	if it.SuggestedTitle != "" {
		return it.SuggestedTitle, nil
	}
	near, err := nearbyWithin(ctx, fc, pt, inboxPlaceTitleTimeout)
	if err != nil {
		return "", err
	}
	in := inboxTitleInput{CourseDeg: courseAt(pt), CameraDeg: it.BearingDeg, Features: featuresForMate(near.Features, it.BearingDeg), Truncated: near.Truncated}
	title, err := mateTitle(ctx, sess, it, in)
	if err != nil {
		return "", err
	}
	if err := store.SetSuggestedTitle(it.ID, title); err != nil {
		log.Printf("inbox: keep Mate's title for %s: %v", it.ID, err) // the title still goes to the operator
	}
	return title, nil
}

// ── the tasks that make the titles ───────────────────────────────────────

const (
	inboxTitleWorkers = 4
	// inboxMateTitleMax is the most photos one request has Mate title; the rest
	// are told to ask again. Titles already made cost no call and do not count.
	inboxMateTitleMax = 20
)

// How long titles may take. Vars for tests.
var (
	// inboxTitleDeadline bounds a whole request: what is not done by then is
	// reported as out of time, so a big batch cannot hold the connection open.
	inboxTitleDeadline = 90 * time.Second
	// inboxMateTitleTimeout allows for a vision call, which takes seconds. It
	// starts when the call holds a Mate slot, not when it joins the queue.
	inboxMateTitleTimeout = 25 * time.Second
	// inboxPlaceTitleTimeout allows for a place lookup, which a public Overpass
	// server may leave waiting.
	inboxPlaceTitleTimeout = 8 * time.Second
)

// inboxTitleTask is one lookup whose answer goes to every id in ids: a place
// name shared by the photos in one spot, or one photo's Mate or sector title.
type inboxTitleTask struct {
	ids []string
	// timeout is how long the lookup may take, and timeoutMsg what the operator
	// is told when it takes longer or the request's deadline passes first. A
	// zero timeout leaves the lookup to bound itself: a Mate call starts its own
	// clock once it holds a slot, so the wait for one is not counted against it.
	timeout    time.Duration
	timeoutMsg string
	run        func(ctx context.Context) (string, error)
	// refuse, when set, is the failure reported for these ids without running
	// anything.
	refuse string
}

// execute runs the lookup within its own timeout. A lookup that cannot be
// cancelled (a place name) is left to finish unheard.
func (t *inboxTitleTask) execute(parent context.Context) (string, error) {
	ctx := parent
	if t.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(parent, t.timeout)
		defer cancel()
	}
	type result struct {
		title string
		err   error
	}
	done := make(chan result, 1)
	go func() {
		title, err := t.run(ctx)
		done <- result{title, err}
	}()
	expired := func() error {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return &inboxTitleError{msg: t.timeoutMsg, cause: ctx.Err()}
		}
		return ctx.Err()
	}
	select {
	case r := <-done:
		if r.err != nil && ctx.Err() != nil {
			return "", expired() // it failed because its time ran out
		}
		return r.title, r.err
	case <-ctx.Done():
		return "", expired()
	}
}

// inboxTitleBuilder turns photos into title tasks. It picks the way to title a
// photo once, when it is made: with Mate every image goes to the vision model;
// without, a photo with a bearing is titled from the sector and any other from
// the nearest place, one lookup shared by the photos in a cell.
type inboxTitleBuilder struct {
	features inboxNearbySource
	titleFor func(it inboxItem, pt *trackFix)
	tasks    []*inboxTitleTask
	places   map[inboxCell]*inboxTitleTask
}

// newInboxTitleBuilder makes the builder for one request; mate is nil when Mate
// is not set up. The features come through a request-scoped view of the shared
// cache, so a failed fetch is made once for the request, not once per photo.
func newInboxTitleBuilder(mate *inboxMateSession, shared *inboxFeatureCache, store *inboxStore) *inboxTitleBuilder {
	fc := newInboxRequestFeatures(shared)
	b := &inboxTitleBuilder{features: fc, places: map[inboxCell]*inboxTitleTask{}}
	if mate != nil {
		sess := *mate
		calls := 0 // photos that need a call; one titled before costs none
		b.titleFor = func(it inboxItem, pt *trackFix) {
			// Mate sees photos; a PDF has no title to suggest.
			if !strings.HasPrefix(it.MIME, "image/") {
				return
			}
			task := &inboxTitleTask{
				ids: []string{it.ID}, timeoutMsg: "Mate didn't answer in time",
				run: func(ctx context.Context) (string, error) { return mateTitleAt(ctx, sess, fc, store, it, pt) },
			}
			if it.SuggestedTitle == "" {
				if calls++; calls > inboxMateTitleMax {
					task.refuse = fmt.Sprintf("Mate titles up to %d photos at a time; open again for the rest", inboxMateTitleMax)
				}
			}
			b.tasks = append(b.tasks, task)
		}
		return b
	}
	b.titleFor = func(it inboxItem, pt *trackFix) {
		if it.BearingDeg != nil {
			b.tasks = append(b.tasks, &inboxTitleTask{
				ids: []string{it.ID}, timeout: inboxPlaceTitleTimeout, timeoutMsg: "The place names didn't answer in time",
				run: func(context.Context) (string, error) { return sectorTitleAt(fc, it, pt) },
			})
			return
		}
		cell := inboxCellAt(pt.Lat, pt.Lon, inboxPlaceCellsPerDeg)
		task, seen := b.places[cell]
		if !seen {
			lat, lon := pt.Lat, pt.Lon // the first photo's own position in the cell
			task = &inboxTitleTask{
				timeout: inboxPlaceTitleTimeout, timeoutMsg: "The place names didn't answer in time",
				run: func(context.Context) (string, error) { return joinTitle(inboxPlaceLookup(lat, lon)), nil },
			}
			b.places[cell] = task
			b.tasks = append(b.tasks, task)
		}
		task.ids = append(task.ids, it.ID)
	}
	return b
}

// add queues the title of one photo taken at pt.
func (b *inboxTitleBuilder) add(it inboxItem, pt *trackFix) { b.titleFor(it, pt) }

// runInboxTitleTasks runs the lookups a few at once, each within its own
// timeout, and all within inboxTitleDeadline. A lookup that fails or runs out of
// time is reported against its ids, never swapped for another kind of answer;
// the others still return theirs. Once the deadline passes, lookups not yet
// started are reported as out of time without being run.
func runInboxTitleTasks(parent context.Context, tasks []*inboxTitleTask) (titles, failures map[string]string) {
	titles, failures = map[string]string{}, map[string]string{}
	var mu sync.Mutex
	fail := func(task *inboxTitleTask, shown string) {
		mu.Lock()
		defer mu.Unlock()
		for _, id := range task.ids {
			failures[id] = shown
		}
	}
	ctx, cancel := context.WithTimeout(parent, inboxTitleDeadline)
	defer cancel()
	sem := make(chan struct{}, inboxTitleWorkers)
	var wg sync.WaitGroup
	for _, task := range tasks {
		if task.refuse != "" {
			fail(task, task.refuse)
			continue
		}
		held := false
		select {
		case sem <- struct{}{}:
			held = true
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			if held {
				<-sem
			}
			if parent.Err() != nil {
				break // the request is gone
			}
			fail(task, task.timeoutMsg) // the deadline passed before this one started
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			title, err := task.execute(ctx)
			if parent.Err() != nil {
				return // the request is gone
			}
			if err != nil {
				shown, logged := inboxTitleFailure(err)
				log.Printf("inbox: title for %v: %s", task.ids, logged)
				fail(task, shown)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, id := range task.ids {
				titles[id] = title
			}
		}()
	}
	wg.Wait()
	return titles, failures
}
