package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// The lightning schedule (ADR 0284).
//
// Only the cell at the boat is wanted, but each window is a whole grid
// download, so the host decides how much to buy and when:
//
//   - the near windows (the next 48 hours) are refreshed once the provider's
//     TTL has passed, which is about once per forecast run;
//   - the far windows (the rest of the trace) are refreshed once a day.
//
// The host holds one Met Office grid cell, the one the boat is in, and asks the
// plugin about the centre of that cell, never the raw position, so swinging at
// anchor reads the same cell every time. Leaving the cell drops everything
// held: the new cell's near windows are fetched at once and its far windows at
// the next daily refresh. The far clock belongs to the scheduler, so a passage
// does not buy the whole trace at every cell it crosses. Returning to a cell
// fetches its near windows again.
//
// The trace is walked in spans of 24 hours (eight windows) because each window
// is a whole-grid download and a plugin call has a 60 second background
// budget; a refresh of the whole range is about 15 calls. The first span of a
// refresh finds the forecast run and the rest are told to read it. A failed
// span keeps the windows already held for it while they are still in the
// future, and a window the run does not have stays empty: nothing is
// interpolated or borrowed from a neighbour.
const (
	lightningSpanLength  = 24 * time.Hour
	lightningNearHorizon = 48 * time.Hour
	// lightningFarHorizon is how far past its run the last window of a run
	// ends (14.5 days). Once a refresh knows the run it stops at run + this,
	// not now + this, so no span is asked for windows the run does not have.
	lightningFarHorizon   = 348 * time.Hour
	lightningFarInterval  = 24 * time.Hour
	lightningDefaultTTL   = 6 * time.Hour
	lightningMaxSpanFails = 3
)

// lightningRetryAfterFailure is how long the schedule waits after a refresh
// that left its near windows out of date before trying again.
var lightningRetryAfterFailure = 10 * time.Minute

// lightningNow is the schedule's clock; tests replace it.
var lightningNow = func() time.Time { return time.Now().UTC() }

// lightningHeldWindow is a window the host holds for the cell, with the run it
// was read from.
type lightningHeldWindow struct {
	Start       time.Time `json:"start"`
	End         time.Time `json:"end"`
	Probability float64   `json:"probability"`
	Run         time.Time `json:"run"`
}

// lightningCell is the one grid cell the schedule holds.
type lightningCell struct {
	Row     int                   `json:"row"`
	Col     int                   `json:"col"`
	Windows []lightningHeldWindow `json:"windows"`
	// NearAt is when every near span last refreshed without a failure.
	NearAt time.Time `json:"near_at"`
}

// lightningState is what is saved to disk: the held windows are a cache of
// data that can be fetched again, so a file that cannot be read is logged and
// dropped, and a field this build does not know is ignored.
type lightningState struct {
	// FarAt is when the far windows were last refreshed.
	FarAt time.Time      `json:"far_at"`
	Cell  *lightningCell `json:"cell"`
}

// lightningView is what a request is answered from.
type lightningView struct {
	Windows      []lightningHeldWindow
	NearAt       time.Time
	Refreshing   bool
	RefreshError string
}

type lightningScheduler struct {
	mu    sync.Mutex
	path  string
	state lightningState
	// refreshing is true while a refresh for the held cell runs.
	refreshing bool
	// gen counts the cells held. A refresh carries the number it started under
	// and its results are dropped if the cell has changed since.
	gen uint64
	// failedAt is when the last refresh left something out of date.
	failedAt time.Time
	// lastFailure is why that refresh failed. It outlives a change of cell, so
	// inside the shared retry wait the new cell is still told the error.
	lastFailure string
	// nearErr and farErr are why the near and far ranges last came back
	// incomplete, kept apart so a near-only refresh cannot clear the far one.
	nearErr, farErr string
	// spawn runs a refresh off the request; tests run it inline.
	spawn func(func())

	// saveMu serialises writes of the state file, which happen outside mu so a
	// slow disk does not hold up requests. snapSeq (under mu) numbers the
	// snapshots and savedSeq (under saveMu) is the newest written: an older
	// one that loses the race to the file is not written.
	saveMu   sync.Mutex
	snapSeq  uint64
	savedSeq uint64
}

func newLightningScheduler(path string) *lightningScheduler {
	s := &lightningScheduler{
		path:  path,
		spawn: func(f func()) { go f() },
	}
	s.load()
	return s
}

func (s *lightningScheduler) load() {
	if s.path == "" {
		return
	}
	loaded, ok, err := readJSONFile[lightningState](s.path)
	if err != nil {
		log.Printf("lightning: discarding unreadable state: %v", err)
		return
	}
	if !ok {
		return
	}
	if loaded.Cell == nil {
		// A file in an older shape holds no cell; its far clock would suppress
		// the far range for a day for windows that are not held.
		log.Printf("lightning: dropping state file %s: no held cell (older shape)", s.path)
		return
	}
	s.state = loaded
}

var (
	lightningSchedulersMu sync.Mutex
	lightningSchedulers   = map[string]*lightningScheduler{}
)

// lightningSchedulerFor returns the provider's schedule, loading its saved
// state on first use.
func lightningSchedulerFor(p lightningProvider) *lightningScheduler {
	lightningSchedulersMu.Lock()
	defer lightningSchedulersMu.Unlock()
	if s, ok := lightningSchedulers[p.ID()]; ok {
		return s
	}
	s := newLightningScheduler(lightningStateFilePath(p.ID()))
	lightningSchedulers[p.ID()] = s
	return s
}

// lightningStateFilePath is where a provider's held windows are saved. Under
// data/ because that is the one directory a container recreate keeps.
func lightningStateFilePath(providerID string) string {
	return cacheFilePath(
		"LIGHTNING_STATE_FILE_"+strings.ToUpper(strings.ReplaceAll(providerID, "-", "_")),
		fmt.Sprintf("data/lightning_%s_state.json", providerID),
	)
}

// lightningCellCentre is the latitude and longitude at the middle of a cell.
func lightningCellCentre(row, col int) (lat, lon float64) {
	return -90 + (float64(row)+0.5)*lightningCellLatStep, -180 + (float64(col)+0.5)*lightningCellLonStep
}

// view answers from what is held for the boat's cell and, if anything is due,
// starts a refresh in the background. It never waits on the plugin.
func (s *lightningScheduler) view(p lightningProvider, lat, lon float64) lightningView {
	now := lightningNow()

	ttl := time.Duration(p.TTLSeconds()) * time.Second
	if ttl <= 0 {
		ttl = lightningDefaultTTL
	}

	s.mu.Lock()
	// At anchor the position wanders across a cell edge; keep the held cell
	// until the boat is clearly outside it, and drop it then.
	cell := s.state.Cell
	if cell != nil && !lightningWithinCellMargin(cell.Row, cell.Col, lat, lon) {
		cell = nil
	}
	if cell == nil {
		row, col := lightningCellIndex(lat, lon)
		cell = &lightningCell{Row: row, Col: col}
		s.state.Cell = cell
		s.gen++
		// A refresh still running belongs to the cell just left; it is let go
		// and its results are dropped when it ends.
		s.refreshing = false
		s.nearErr, s.farErr = "", ""
	}
	needNear := cell.NearAt.IsZero() || now.Sub(cell.NearAt) >= ttl
	needFar := s.state.FarAt.IsZero() || now.Sub(s.state.FarAt) >= lightningFarInterval
	start := false
	var gen uint64
	var row, col int
	if (needNear || needFar) && !s.refreshing && now.Sub(s.failedAt) >= lightningRetryAfterFailure {
		s.refreshing = true
		start = true
		gen, row, col = s.gen, cell.Row, cell.Col
	}
	s.mu.Unlock()

	if start {
		s.spawn(func() { s.refresh(p, gen, row, col, now, needNear, needFar) })
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	view := lightningView{
		Refreshing:   s.refreshing,
		RefreshError: joinProblems(s.nearErr, s.farErr),
	}
	// A cell change clears nearErr and farErr but not the retry wait; while it
	// holds, the failure that started it is still the answer.
	if view.RefreshError == "" && !s.failedAt.IsZero() && now.Sub(s.failedAt) < lightningRetryAfterFailure {
		view.RefreshError = s.lastFailure
	}
	if held := s.state.Cell; held != nil {
		view.Windows = liveWindows(held.Windows, now)
		view.NearAt = held.NearAt
	}
	return view
}

func joinProblems(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "; ")
}

// lightningCellMargin is how far outside the held cell the boat may drift
// before it counts as having moved: 0.05 degrees of latitude, about 5.5 km, and
// the same distance in longitude.
const lightningCellMargin = 0.05

// lightningWithinCellMargin reports whether a position is inside the cell
// (row, col) or within the margin of its edge.
func lightningWithinCellMargin(row, col int, lat, lon float64) bool {
	centreLat, centreLon := lightningCellCentre(row, col)
	if math.Abs(lat-centreLat) > lightningCellLatStep/2+lightningCellMargin {
		return false
	}
	cosLat := math.Max(math.Cos(lat*math.Pi/180), 0.01)
	lonMargin := lightningCellMargin / cosLat
	dx := math.Mod(lon-centreLon, 360)
	if dx > 180 {
		dx -= 360
	} else if dx < -180 {
		dx += 360
	}
	return math.Abs(dx) <= lightningCellLonStep/2+lonMargin
}

// liveWindows returns the windows that have not yet ended.
func liveWindows(ws []lightningHeldWindow, now time.Time) []lightningHeldWindow {
	live := ws[:0:0]
	for _, w := range ws {
		if w.End.After(now) {
			live = append(live, w)
		}
	}
	return live
}

type lightningRangeResult struct {
	spans, failed int
	err           string
}

func (r lightningRangeResult) allOK() bool { return r.failed == 0 }
func (r lightningRangeResult) anyOK() bool { return r.spans > r.failed }

// refresh fetches the due ranges for the cell (row, col), which the schedule
// held as generation gen when it started. The plugin is asked about the centre
// of the cell.
func (s *lightningScheduler) refresh(p lightningProvider, gen uint64, row, col int, now time.Time, doNear, doFar bool) {
	lat, lon := lightningCellCentre(row, col)
	var near, far lightningRangeResult
	// run is found by the first span that succeeds and then told to the rest of
	// the refresh, near and far alike.
	var run time.Time
	if doNear {
		near = s.fetchRange(p, gen, lat, lon, now, now.Add(lightningNearHorizon), &run)
	}
	if doFar && !s.stale(gen) {
		far = s.fetchRange(p, gen, lat, lon, now.Add(lightningNearHorizon), now.Add(lightningFarHorizon), &run)
	}

	done := lightningNow()
	s.mu.Lock()
	if s.gen != gen {
		// The boat moved on while this ran. What it fetched was dropped, and the
		// state it would clear belongs to the new cell's refresh.
		s.mu.Unlock()
		return
	}
	s.refreshing = false
	cell := s.state.Cell
	retry := false
	if doNear {
		if near.allOK() {
			cell.NearAt = done
			s.nearErr = ""
		} else {
			retry = true
			s.nearErr = near.err
		}
	}
	// A far error stands until a later far refresh clears it. A partial far
	// refresh still moves FarAt on: the spans that failed stay gaps until the
	// next daily refresh rather than re-downloading the range.
	if doFar {
		if far.anyOK() {
			s.state.FarAt = done
		} else {
			retry = true
		}
		if far.allOK() {
			s.farErr = ""
		} else {
			s.farErr = far.err
		}
	}
	if retry {
		s.failedAt = done
		s.lastFailure = joinProblems(near.err, far.err)
	} else {
		s.failedAt = time.Time{}
		s.lastFailure = ""
	}
	snap, seq := s.snapshotLocked(done)
	s.mu.Unlock()
	s.save(snap, seq)
}

// fetchRange walks (from, to] in spans. It gives up on the range after a few
// spans in a row fail, which means the source is down rather than one file
// being bad, and when the cell changes under it. *run is the forecast run to
// read: zero asks the plugin to find it, and the run a span returns is kept in
// it for the spans after.
func (s *lightningScheduler) fetchRange(p lightningProvider, gen uint64, lat, lon float64, from, to time.Time, run *time.Time) lightningRangeResult {
	var res lightningRangeResult
	consecutive := 0
	for spanFrom := from; spanFrom.Before(to); spanFrom = spanFrom.Add(lightningSpanLength) {
		// Once the run is known the walk ends at its last window.
		end := to
		if !run.IsZero() {
			if runEnd := run.Add(lightningFarHorizon); runEnd.Before(end) {
				end = runEnd
			}
		}
		if !spanFrom.Before(end) || s.stale(gen) {
			break
		}
		spanTo := spanFrom.Add(lightningSpanLength)
		if spanTo.After(end) {
			spanTo = end
		}
		res.spans++
		span, err := p.FetchLightning(lat, lon, spanFrom, spanTo, *run)
		if err == nil {
			err = validateLightningSpan(span, spanFrom, spanTo)
		}
		if err != nil {
			log.Printf("lightning provider %q: span %s to %s failed: %v", p.ID(), spanFrom.Format(time.RFC3339), spanTo.Format(time.RFC3339), err)
			res.failed++
			res.err = fmt.Sprintf("%s to %s: %s", spanFrom.Format(time.RFC3339), spanTo.Format(time.RFC3339), firstErrorLine(err))
			consecutive++
			if consecutive >= lightningMaxSpanFails {
				break
			}
			continue
		}
		consecutive = 0
		*run = span.Run
		if !s.merge(gen, spanFrom, spanTo, span) {
			break
		}
	}
	return res
}

// stale reports whether the cell a refresh started under has been left.
func (s *lightningScheduler) stale(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen != gen
}

// validateLightningSpan holds the plugin to its contract: every window ends
// inside the span asked for, none repeats and probabilities are 0..1.
func validateLightningSpan(span lightningSpan, from, to time.Time) error {
	if span.Run.IsZero() {
		return errors.New("plugin returned no run time")
	}
	seen := map[time.Time]bool{}
	for _, w := range span.Windows {
		if !w.End.After(from) || w.End.After(to) {
			return fmt.Errorf("window ending %s is outside the span asked for", w.End.Format(time.RFC3339))
		}
		if !w.End.After(w.Start) {
			return fmt.Errorf("window ending %s does not start before it ends", w.End.Format(time.RFC3339))
		}
		if seen[w.End] {
			return fmt.Errorf("window ending %s is returned twice", w.End.Format(time.RFC3339))
		}
		seen[w.End] = true
		if !(w.Probability >= 0 && w.Probability <= 1) {
			return fmt.Errorf("window ending %s has probability %v, outside 0..1", w.End.Format(time.RFC3339), w.Probability)
		}
	}
	return nil
}

// merge replaces the held windows ending in (from, to] with the span's. It
// reports false, and keeps nothing, when the cell the span was fetched for is
// no longer the held one.
func (s *lightningScheduler) merge(gen uint64, from, to time.Time, span lightningSpan) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen || s.state.Cell == nil {
		return false
	}
	cell := s.state.Cell
	kept := cell.Windows[:0:0]
	for _, w := range cell.Windows {
		if w.End.After(from) && !w.End.After(to) {
			continue
		}
		kept = append(kept, w)
	}
	for _, w := range span.Windows {
		kept = append(kept, lightningHeldWindow{Start: w.Start.UTC(), End: w.End.UTC(), Probability: w.Probability, Run: span.Run.UTC()})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].End.Before(kept[j].End) })
	cell.Windows = kept
	return true
}

// snapshotLocked drops windows that have passed and copies the state for
// saving, with a number that orders it among the snapshots. Callers hold s.mu.
func (s *lightningScheduler) snapshotLocked(now time.Time) (lightningState, uint64) {
	snap := lightningState{FarAt: s.state.FarAt}
	if cell := s.state.Cell; cell != nil {
		cell.Windows = liveWindows(cell.Windows, now)
		copied := *cell
		copied.Windows = slices.Clone(cell.Windows)
		snap.Cell = &copied
	}
	s.snapSeq++
	return snap, s.snapSeq
}

// save writes a snapshot without holding s.mu, so a slow disk never holds up a
// request. A snapshot older than one already written is skipped.
func (s *lightningScheduler) save(snap lightningState, seq uint64) {
	if s.path == "" {
		return
	}
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if seq <= s.savedSeq {
		return
	}
	if err := writeJSONFileAtomic(s.path, snap); err != nil {
		log.Printf("lightning: failed to save %s: %v", s.path, err)
		return
	}
	s.savedSeq = seq
}
