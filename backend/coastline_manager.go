package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ── offline coastline manager (ADR 0203) ─────────────────────────────────
//
// One background worker keeps the coverage squares current. Each step does at
// most one unit of work, in this order:
//
//  1. a square the operator asked to update (Update),
//  2. the nearest wanted square not yet held (needs a position fix),
//  3. the weekly edit check of a held square.
//
// Wanted squares are every square within coastlineCoverageRadiusNm of the
// boat. Squares downloaded earlier are never dropped; only Clear removes
// them. Downloads run one at a time with a pause between, to keep within the
// Overpass usage policy. Nothing runs, and nothing is read from the plugin,
// while the switch is off.

const (
	// coastlineDownloadPause is the wait between two plugin calls.
	coastlineDownloadPause = 10 * time.Second

	// coastlineIdleInterval is how often an idle worker looks again: for a
	// switch turned on, a position fix, or a square falling due.
	coastlineIdleInterval = 30 * time.Second

	// coastlineCheckInterval is how often each held square is asked whether
	// its coastline has been edited.
	coastlineCheckInterval = 7 * 24 * time.Hour

	coastlineRetryBase = 5 * time.Minute
	coastlineRetryMax  = 6 * time.Hour
)

// coastlineRetryDelay is the wait before attempt n+1 after n failures: 5
// minutes, doubling, up to 6 hours.
func coastlineRetryDelay(failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := coastlineRetryBase
	for i := 1; i < failures; i++ {
		d *= 2
		if d >= coastlineRetryMax {
			return coastlineRetryMax
		}
	}
	return d
}

// coastlineServiceProblem is a failure of the whole map server (Overpass) as
// opposed to one square: rate limiting, refusing the network, an overloaded
// or unreachable server. It pauses the whole job; marking the square bad
// would be wrong, and trying the next square 10 s later would only hammer a
// server that has just said no.
type coastlineServiceProblem struct {
	// Summary is the plain sentence for the settings page.
	Summary string
	// RetryAfter is the server's own Retry-After, zero when it gave none.
	RetryAfter time.Duration
	// Detail is the underlying error text.
	Detail string
}

var (
	coastlineRetryAfterRe = regexp.MustCompile(`retry after (\d+)s`)
	coastline5xxRe        = regexp.MustCompile(`status 5\d\d`)
	coastlineRefusedRe    = regexp.MustCompile(`status (401|403|451)\b`)
)

// classifyCoastlineError returns the service problem behind err, or nil when
// the error is about the square itself (bad data, a query that failed for
// this box). The plugin reports errors as text only, so this reads the text
// its transport and status errors use.
func classifyCoastlineError(err error) *coastlineServiceProblem {
	if err == nil {
		return nil
	}
	msg := err.Error()
	low := strings.ToLower(msg)
	p := &coastlineServiceProblem{Detail: msg}
	if m := coastlineRetryAfterRe.FindStringSubmatch(low); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			p.RetryAfter = time.Duration(n) * time.Second
		}
	}
	switch {
	case strings.Contains(low, "rate limited") || strings.Contains(low, "rate_limited") || strings.Contains(low, "status 429"):
		p.Summary = "The map server is rate-limiting requests from this boat"
	// 401, 403 and 451 are the server turning this boat away (for example
	// "only available to white-listed usages"), not a fault of one square:
	// every other square would get the same answer. The fix is a different
	// server, not the network, so the summary says so.
	case coastlineRefusedRe.MatchString(low):
		p.Summary = "The map server turned the boat away. Choose another Overpass server in the plugin settings"
	case strings.Contains(low, "connection refused") || strings.Contains(low, "connection reset") || strings.Contains(low, "no such host"):
		p.Summary = "The map server is refusing connections"
	case strings.Contains(low, "i/o timeout") || strings.Contains(low, "deadline exceeded") || strings.Contains(low, "timeout awaiting"):
		p.Summary = "The map server is not answering"
	case strings.Contains(low, "too busy") || coastline5xxRe.MatchString(low):
		p.Summary = "The map server is overloaded or down"
	default:
		return nil
	}
	return p
}

// coastlineSource is the plugin side: the two optional exports of the
// osm-overpass plugin.
type coastlineSource interface {
	SupportsShoreline() bool
	ShorelineSquare(sq coverageSquare) ([]shorelineLine, error)
	ShorelineChanges(sq coverageSquare, since time.Time) (int, error)
}

// coastlinePluginID is the plugin the offline coastline downloads through.
// The Overpass server stays that plugin's own setting.
const coastlinePluginID = "osm-overpass"

func resolveCoastlineSource() (coastlineSource, error) {
	p, ok := getPOIProvider(coastlinePluginID)
	if !ok {
		return nil, fmt.Errorf("the %s plugin is not installed, so the offline coastline cannot download", coastlinePluginID)
	}
	src, ok := p.(coastlineSource)
	if !ok || !src.SupportsShoreline() {
		return nil, fmt.Errorf("the %s plugin is too old for the offline coastline: install the updated plugin", coastlinePluginID)
	}
	return src, nil
}

type coastlineManagerConfig struct {
	Store    *coastlineStore
	Enabled  func() (bool, error)
	Position func() (lat, lon float64, ok bool)
	Source   func() (coastlineSource, error)
	Now      func() time.Time
}

type coastlineManager struct {
	cfg coastlineManagerConfig

	// gateMu orders every write to the store against Clear and Update, and
	// guards gen. A plugin call can take minutes; whatever the operator did
	// meanwhile (Clear, or the switch going off) must win, so a result is
	// saved only if gen is unchanged and the switch is still on, checked and
	// written under this lock.
	gateMu sync.Mutex
	gen    uint64

	mu        sync.Mutex
	queue     []coverageSquare // operator-requested updates, in order
	lastError string           // a problem with the whole job, not one square
	wake      chan struct{}

	// pause is set when the map server refused the whole job (rate limit,
	// refused connection, overload). No square is tried until pausedUntil.
	pause         *coastlineServiceProblem
	pausedUntil   time.Time
	pauseFailures int // consecutive service failures, for the backoff

	// snap is what Status serves: the switch and the boat's position as the
	// worker last saw them, so a GET polled every few seconds by every open
	// page costs a store read and nothing else.
	snap coastlineSnapshot
}

type coastlineSnapshot struct {
	valid      bool
	enabled    bool
	enabledErr error
	lat, lon   float64
	fix        bool
}

func newCoastlineManager(cfg coastlineManagerConfig) *coastlineManager {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &coastlineManager{cfg: cfg, wake: make(chan struct{}, 1)}
}

// refresh reads the switch and the boat's position once and keeps them for
// Status. The worker calls it at the top of every step, and SettingsChanged
// calls it after a settings save so the page sees the new switch at once.
func (m *coastlineManager) refresh() coastlineSnapshot {
	snap := coastlineSnapshot{valid: true}
	snap.enabled, snap.enabledErr = m.cfg.Enabled()
	snap.lat, snap.lon, snap.fix = m.cfg.Position()
	m.mu.Lock()
	m.snap = snap
	m.mu.Unlock()
	return snap
}

// SettingsChanged re-reads the switch after the operator saved settings and
// wakes the worker.
func (m *coastlineManager) SettingsChanged() {
	m.refresh()
	m.Wake()
}

func (m *coastlineManager) setError(msg string) {
	m.mu.Lock()
	changed := m.lastError != msg
	m.lastError = msg
	m.mu.Unlock()
	if changed && msg != "" {
		log.Printf("offline coastline: %s", msg)
	}
}

func (m *coastlineManager) nextQueued() (coverageSquare, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.queue) == 0 {
		return coverageSquare{}, false
	}
	sq := m.queue[0]
	m.queue = m.queue[1:]
	return sq, true
}

// commit runs save only if the work that produced it is still wanted: no
// Clear has happened since gen was read, and Offline coastline is still on.
// The check and the write share gateMu, so Clear cannot slip in between.
func (m *coastlineManager) commit(gen uint64, what string, save func() error) {
	m.gateMu.Lock()
	defer m.gateMu.Unlock()
	if gen != m.gen {
		log.Printf("offline coastline: %s discarded: coverage was cleared while it ran", what)
		return
	}
	enabled, err := m.cfg.Enabled()
	if err != nil {
		m.setError(fmt.Sprintf("could not read the offline coastline setting: %v", err))
		return
	}
	if !enabled {
		log.Printf("offline coastline: %s discarded: Offline coastline was switched off while it ran", what)
		return
	}
	if err := save(); err != nil {
		m.setError(err.Error())
	}
}

// step does one unit of work and reports whether it called the plugin.
func (m *coastlineManager) step() bool {
	snap := m.refresh()
	if snap.enabledErr != nil {
		m.setError(fmt.Sprintf("could not read the offline coastline setting: %v", snap.enabledErr))
		return false
	}
	if !snap.enabled {
		m.mu.Lock()
		m.queue = nil
		m.lastError = ""
		m.pause, m.pauseFailures, m.pausedUntil = nil, 0, time.Time{}
		m.mu.Unlock()
		return false
	}
	src, err := m.cfg.Source()
	if err != nil {
		m.setError(err.Error())
		return false
	}
	m.setError("")

	m.gateMu.Lock()
	gen := m.gen
	m.gateMu.Unlock()

	now := m.cfg.Now()
	m.mu.Lock()
	paused := now.Before(m.pausedUntil)
	m.mu.Unlock()
	if paused {
		return false
	}
	recs, err := m.cfg.Store.Records()
	if err != nil {
		m.setError(err.Error())
		return false
	}
	// A square that failed before it was known to be all land holds nothing
	// and will never be tried again: drop its row.
	kept := recs[:0:0]
	for _, r := range recs {
		if r.Status == coastlineStatusFailed && squareInland(r.Square) {
			if err := m.cfg.Store.DeleteFailed(r.Square); err != nil {
				m.setError(err.Error())
				return false
			}
			continue
		}
		kept = append(kept, r)
	}
	recs = kept
	byKey := make(map[coverageSquare]coastlineSquareRecord, len(recs))
	for _, r := range recs {
		byKey[r.Square] = r
	}

	if sq, ok := m.nextQueued(); ok {
		m.download(src, gen, sq, byKey[sq], now, &sq)
		return true
	}

	if snap.fix {
		wanted, _, err := coverageWanted(snap.lat, snap.lon)
		if err != nil {
			m.setError(err.Error())
		} else {
			for _, sq := range wanted {
				rec, held := byKey[sq]
				if held && rec.Status == coastlineStatusReady {
					continue
				}
				if held && now.Before(rec.NextAttemptAt) {
					continue
				}
				m.download(src, gen, sq, rec, now, nil)
				return true
			}
		}
	}

	var due []coastlineSquareRecord
	for _, r := range recs {
		if r.Status == coastlineStatusReady && !squareInland(r.Square) && now.Sub(r.CheckedAt) >= coastlineCheckInterval && !now.Before(r.NextAttemptAt) {
			due = append(due, r)
		}
	}
	if len(due) > 0 {
		sort.Slice(due, func(i, j int) bool { return due[i].CheckedAt.Before(due[j].CheckedAt) })
		m.check(src, gen, due[0], now)
		return true
	}
	return false
}

// serviceFailed pauses the whole job after the map server refused it, if the
// work that hit the refusal is still wanted, and reports whether it was (the
// generation it started under is still current). The wait is the longer of the
// server's Retry-After and the usual backoff, which doubles while refusals
// keep coming.
//
// Like commit, it drops the refusal when Offline coastline has been switched
// off since the work began (switching off does not bump the generation). A
// non-nil requeue is put back at the front of the queue in the same critical
// section.
func (m *coastlineManager) serviceFailed(gen uint64, what string, p *coastlineServiceProblem, now time.Time, requeue *coverageSquare) bool {
	m.gateMu.Lock()
	defer m.gateMu.Unlock()
	if gen != m.gen {
		return false
	}
	enabled, err := m.cfg.Enabled()
	if err != nil {
		m.setError(fmt.Sprintf("could not read the offline coastline setting: %v", err))
		return false
	}
	if !enabled {
		log.Printf("offline coastline: %s refusal discarded: Offline coastline was switched off while it ran", what)
		return false
	}
	m.mu.Lock()
	if requeue != nil {
		m.queue = append([]coverageSquare{*requeue}, m.queue...)
	}
	m.pauseFailures++
	wait := coastlineRetryDelay(m.pauseFailures)
	if p.RetryAfter > wait {
		wait = p.RetryAfter
	}
	m.pause = p
	m.pausedUntil = now.Add(wait)
	m.mu.Unlock()
	log.Printf("offline coastline: %s: %s; pausing until %s (%s)", what, p.Summary, now.Add(wait).Format(time.RFC3339), p.Detail)
	return true
}

func (m *coastlineManager) serviceWorked() {
	m.mu.Lock()
	m.pause, m.pauseFailures = nil, 0
	m.mu.Unlock()
}

// download fetches one square. requeue is the operator-requested square to put
// back at the front of the queue if the map server refuses the whole job; it
// is nil for a square the top-up picked itself. The requeue happens inside
// serviceFailed, under the same lock as the Clear/switch-off check, so a Clear
// cannot land between the check and the requeue.
func (m *coastlineManager) download(src coastlineSource, gen uint64, sq coverageSquare, prev coastlineSquareRecord, now time.Time, requeue *coverageSquare) {
	lines, err := src.ShorelineSquare(sq)
	if err != nil {
		log.Printf("offline coastline: download of %s failed: %v", sq.Label(), err)
		if p := classifyCoastlineError(err); p != nil {
			m.serviceFailed(gen, "download of "+sq.Label(), p, now, requeue)
			return
		}
		m.commit(gen, "failure of "+sq.Label(), func() error {
			return m.cfg.Store.RecordFailure(sq, err.Error(), now, now.Add(coastlineRetryDelay(prev.Attempts+1)))
		})
		return
	}
	m.serviceWorked()
	m.commit(gen, "download of "+sq.Label(), func() error {
		return m.cfg.Store.SaveSquare(sq, lines, now)
	})
}

func (m *coastlineManager) check(src coastlineSource, gen uint64, rec coastlineSquareRecord, now time.Time) {
	edits, err := src.ShorelineChanges(rec.Square, rec.DownloadedAt)
	if err != nil {
		log.Printf("offline coastline: edit check of %s failed: %v", rec.Square.Label(), err)
		if p := classifyCoastlineError(err); p != nil {
			m.serviceFailed(gen, "edit check of "+rec.Square.Label(), p, now, nil)
			return
		}
		m.commit(gen, "failure of edit check of "+rec.Square.Label(), func() error {
			return m.cfg.Store.RecordFailure(rec.Square, err.Error(), now, now.Add(coastlineRetryDelay(rec.Attempts+1)))
		})
		return
	}
	m.serviceWorked()
	m.commit(gen, "edit check of "+rec.Square.Label(), func() error {
		return m.cfg.Store.RecordEdits(rec.Square, edits, now)
	})
}

// Update queues a re-download of every held square with edits since, and
// returns how many. It is the only way edits are applied.
func (m *coastlineManager) Update() (int, error) {
	enabled, err := m.cfg.Enabled()
	if err != nil {
		return 0, err
	}
	if !enabled {
		return 0, errors.New("Offline coastline is off: turn it on in Settings before updating")
	}
	// Held across the read and the queueing so a Clear cannot land between.
	m.gateMu.Lock()
	defer m.gateMu.Unlock()
	recs, err := m.cfg.Store.Records()
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	queued := map[coverageSquare]bool{}
	for _, sq := range m.queue {
		queued[sq] = true
	}
	n := 0
	for _, r := range recs {
		if r.Status == coastlineStatusReady && r.EditsSince > 0 {
			if !queued[r.Square] {
				m.queue = append(m.queue, r.Square)
			}
			n++
		}
	}
	m.mu.Unlock()
	if n == 0 {
		return 0, errors.New("no squares have edits since their download")
	}
	m.Wake()
	return n, nil
}

// Clear deletes all coverage and forgets any queued update.
func (m *coastlineManager) Clear() error {
	m.gateMu.Lock()
	defer m.gateMu.Unlock()
	m.gen++ // any plugin call already in flight is now stale
	m.mu.Lock()
	m.queue = nil
	m.pause, m.pauseFailures, m.pausedUntil = nil, 0, time.Time{}
	m.mu.Unlock()
	return m.cfg.Store.Clear()
}

// Wake makes an idle worker look again now.
func (m *coastlineManager) Wake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// Run is the worker loop; it returns when ctx ends.
func (m *coastlineManager) Run(ctx context.Context) {
	for {
		wait := coastlineIdleInterval
		if m.step() {
			wait = coastlineDownloadPause
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-m.wake:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// ── status ───────────────────────────────────────────────────────────────

type coastlinePosition struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type coastlineStatusSquare struct {
	Lat          int        `json:"lat"`
	Lon          int        `json:"lon"`
	State        string     `json:"state"` // ready, failed or pending
	InRange      bool       `json:"in_range"`
	DownloadedAt *time.Time `json:"downloaded_at,omitempty"`
	EditsSince   int        `json:"edits_since"`
	CheckedAt    *time.Time `json:"checked_at,omitempty"`
	// Error is why a download failed (state failed). CheckError is why the
	// weekly edit check of a held square failed; the square is still held.
	Error         string     `json:"error,omitempty"`
	CheckError    string     `json:"check_error,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

type coastlineStatus struct {
	Enabled bool `json:"enabled"`
	// State is off, error, paused, waiting_for_fix, updating, downloading,
	// retrying or idle.
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// While paused: what the map server did, the error behind it, and when
	// the worker will try again.
	PauseReason string                  `json:"pause_reason,omitempty"`
	PauseDetail string                  `json:"pause_detail,omitempty"`
	RetryAt     *time.Time              `json:"retry_at,omitempty"`
	HasFix      bool                    `json:"has_fix"`
	Position    *coastlinePosition      `json:"position,omitempty"`
	RadiusNm    float64                 `json:"radius_nm"`
	Squares     []coastlineStatusSquare `json:"squares"`
	Held        int                     `json:"held"`
	Wanted      int                     `json:"wanted"`
	WantedHeld  int                     `json:"wanted_held"`
	// InlandSkipped counts squares in range that are all land and so are
	// not downloaded.
	InlandSkipped int `json:"inland_skipped"`
	// Failed counts wanted squares whose download failed and will be retried.
	Failed int `json:"failed"`
	// CheckFailed counts held squares whose weekly edit check failed.
	CheckFailed        int        `json:"check_failed"`
	EditsSince         int        `json:"edits_since"`
	SquaresWithEdits   int        `json:"squares_with_edits"`
	Updating           int        `json:"updating"`
	OldestDownloadedAt *time.Time `json:"oldest_downloaded_at,omitempty"`
}

// Status is what the settings page shows. It reads the store and the state
// the worker cached on its last step; it never calls SignalK or the settings
// file itself, because every open settings page polls it.
func (m *coastlineManager) Status() (coastlineStatus, error) {
	st := coastlineStatus{RadiusNm: coastlineCoverageRadiusNm, Squares: []coastlineStatusSquare{}}
	m.mu.Lock()
	snap := m.snap
	m.mu.Unlock()
	if !snap.valid {
		snap = m.refresh() // only before the worker's first step
	}
	if snap.enabledErr != nil {
		return st, snap.enabledErr
	}
	enabled := snap.enabled
	st.Enabled = enabled
	recs, err := m.cfg.Store.Records()
	if err != nil {
		return st, err
	}
	now := m.cfg.Now()

	m.mu.Lock()
	st.Error = m.lastError
	st.Updating = len(m.queue)
	paused := m.pause != nil && now.Before(m.pausedUntil)
	if paused {
		st.PauseReason, st.PauseDetail = m.pause.Summary, m.pause.Detail
		until := m.pausedUntil
		st.RetryAt = &until
	}
	m.mu.Unlock()

	fix := snap.fix
	st.HasFix = fix
	var wanted []coverageSquare
	if fix {
		st.Position = &coastlinePosition{Lat: snap.lat, Lon: snap.lon}
		wanted, st.InlandSkipped, err = coverageWanted(snap.lat, snap.lon)
		if err != nil {
			st.InlandSkipped = 0
			st.Error = err.Error()
			wanted = nil
		}
	}
	inRange := make(map[coverageSquare]bool, len(wanted))
	for _, sq := range wanted {
		inRange[sq] = true
	}
	st.Wanted = len(wanted)

	byKey := make(map[coverageSquare]coastlineSquareRecord, len(recs))
	missingDue, missingWaiting := 0, 0
	for _, r := range recs {
		byKey[r.Square] = r
		s := coastlineStatusSquare{
			Lat: r.Square.Lat, Lon: r.Square.Lon, State: r.Status, InRange: inRange[r.Square],
			DownloadedAt: timePtrUnlessZero(r.DownloadedAt), EditsSince: r.EditsSince, CheckedAt: timePtrUnlessZero(r.CheckedAt),
			NextAttemptAt: timePtrUnlessZero(r.NextAttemptAt),
		}
		if r.Status == coastlineStatusReady {
			s.CheckError = r.LastError
		} else {
			s.Error = r.LastError
		}
		st.Squares = append(st.Squares, s)
		if r.Status == coastlineStatusReady {
			st.Held++
			st.EditsSince += r.EditsSince
			if r.EditsSince > 0 {
				st.SquaresWithEdits++
			}
			if st.OldestDownloadedAt == nil || r.DownloadedAt.Before(*st.OldestDownloadedAt) {
				d := r.DownloadedAt
				st.OldestDownloadedAt = &d
			}
			if r.LastError != "" {
				st.CheckFailed++
			}
		} else if r.LastError != "" && inRange[r.Square] {
			st.Failed++
		}
	}
	for _, sq := range wanted {
		r, held := byKey[sq]
		switch {
		case held && r.Status == coastlineStatusReady:
			st.WantedHeld++
		case held && now.Before(r.NextAttemptAt):
			missingWaiting++
		default:
			missingDue++
			if !held {
				st.Squares = append(st.Squares, coastlineStatusSquare{Lat: sq.Lat, Lon: sq.Lon, State: "pending", InRange: true})
			}
		}
	}

	switch {
	case !enabled:
		st.State = "off"
	case st.Error != "":
		st.State = "error"
	case paused:
		st.State = "paused"
	case st.Updating > 0:
		st.State = "updating"
	case !fix:
		st.State = "waiting_for_fix"
	case missingDue > 0:
		st.State = "downloading"
	case missingWaiting > 0:
		st.State = "retrying"
	default:
		st.State = "idle"
	}
	return st, nil
}
