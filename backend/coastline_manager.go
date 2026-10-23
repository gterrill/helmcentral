package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
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

	mu        sync.Mutex
	queue     []coverageSquare // operator-requested updates, in order
	lastError string           // a problem with the whole job, not one square
	wake      chan struct{}
}

func newCoastlineManager(cfg coastlineManagerConfig) *coastlineManager {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &coastlineManager{cfg: cfg, wake: make(chan struct{}, 1)}
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

// step does one unit of work and reports whether it called the plugin.
func (m *coastlineManager) step() bool {
	enabled, err := m.cfg.Enabled()
	if err != nil {
		m.setError(fmt.Sprintf("could not read the offline coastline setting: %v", err))
		return false
	}
	if !enabled {
		m.mu.Lock()
		m.queue = nil
		m.lastError = ""
		m.mu.Unlock()
		return false
	}
	src, err := m.cfg.Source()
	if err != nil {
		m.setError(err.Error())
		return false
	}
	m.setError("")

	now := m.cfg.Now()
	recs, err := m.cfg.Store.Records()
	if err != nil {
		m.setError(err.Error())
		return false
	}
	byKey := make(map[coverageSquare]coastlineSquareRecord, len(recs))
	for _, r := range recs {
		byKey[r.Square] = r
	}

	if sq, ok := m.nextQueued(); ok {
		m.download(src, sq, byKey[sq], now)
		return true
	}

	if lat, lon, ok := m.cfg.Position(); ok {
		wanted, err := squaresWithin(lat, lon, coastlineCoverageRadiusNm)
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
				m.download(src, sq, rec, now)
				return true
			}
		}
	}

	var due []coastlineSquareRecord
	for _, r := range recs {
		if r.Status == coastlineStatusReady && now.Sub(r.CheckedAt) >= coastlineCheckInterval && !now.Before(r.NextAttemptAt) {
			due = append(due, r)
		}
	}
	if len(due) > 0 {
		sort.Slice(due, func(i, j int) bool { return due[i].CheckedAt.Before(due[j].CheckedAt) })
		m.check(src, due[0], now)
		return true
	}
	return false
}

func (m *coastlineManager) download(src coastlineSource, sq coverageSquare, prev coastlineSquareRecord, now time.Time) {
	lines, err := src.ShorelineSquare(sq)
	if err != nil {
		log.Printf("offline coastline: download of %s failed: %v", sq.Label(), err)
		if rerr := m.cfg.Store.RecordFailure(sq, err.Error(), now, now.Add(coastlineRetryDelay(prev.Attempts+1))); rerr != nil {
			m.setError(rerr.Error())
		}
		return
	}
	if err := m.cfg.Store.SaveSquare(sq, lines, now); err != nil {
		m.setError(err.Error())
	}
}

func (m *coastlineManager) check(src coastlineSource, rec coastlineSquareRecord, now time.Time) {
	edits, err := src.ShorelineChanges(rec.Square, rec.DownloadedAt)
	if err != nil {
		log.Printf("offline coastline: edit check of %s failed: %v", rec.Square.Label(), err)
		if rerr := m.cfg.Store.RecordFailure(rec.Square, err.Error(), now, now.Add(coastlineRetryDelay(rec.Attempts+1))); rerr != nil {
			m.setError(rerr.Error())
		}
		return
	}
	if err := m.cfg.Store.RecordEdits(rec.Square, edits, now); err != nil {
		m.setError(err.Error())
	}
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
	m.mu.Lock()
	m.queue = nil
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
	Lat           int        `json:"lat"`
	Lon           int        `json:"lon"`
	State         string     `json:"state"` // ready, failed or pending
	InRange       bool       `json:"in_range"`
	DownloadedAt  *time.Time `json:"downloaded_at,omitempty"`
	EditsSince    int        `json:"edits_since"`
	CheckedAt     *time.Time `json:"checked_at,omitempty"`
	Error         string     `json:"error,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
}

type coastlineStatus struct {
	Enabled bool `json:"enabled"`
	// State is off, error, waiting_for_fix, updating, downloading, retrying
	// or idle.
	State              string                  `json:"state"`
	Error              string                  `json:"error,omitempty"`
	HasFix             bool                    `json:"has_fix"`
	Position           *coastlinePosition      `json:"position,omitempty"`
	RadiusNm           float64                 `json:"radius_nm"`
	Squares            []coastlineStatusSquare `json:"squares"`
	Held               int                     `json:"held"`
	Wanted             int                     `json:"wanted"`
	WantedHeld         int                     `json:"wanted_held"`
	Failed             int                     `json:"failed"`
	EditsSince         int                     `json:"edits_since"`
	SquaresWithEdits   int                     `json:"squares_with_edits"`
	Updating           int                     `json:"updating"`
	OldestDownloadedAt *time.Time              `json:"oldest_downloaded_at,omitempty"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Status is what the settings page shows.
func (m *coastlineManager) Status() (coastlineStatus, error) {
	st := coastlineStatus{RadiusNm: coastlineCoverageRadiusNm, Squares: []coastlineStatusSquare{}}
	enabled, err := m.cfg.Enabled()
	if err != nil {
		return st, err
	}
	st.Enabled = enabled
	recs, err := m.cfg.Store.Records()
	if err != nil {
		return st, err
	}
	now := m.cfg.Now()

	m.mu.Lock()
	st.Error = m.lastError
	st.Updating = len(m.queue)
	m.mu.Unlock()

	lat, lon, fix := m.cfg.Position()
	st.HasFix = fix
	var wanted []coverageSquare
	if fix {
		st.Position = &coastlinePosition{Lat: lat, Lon: lon}
		wanted, err = squaresWithin(lat, lon, coastlineCoverageRadiusNm)
		if err != nil {
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
			DownloadedAt: timePtr(r.DownloadedAt), EditsSince: r.EditsSince, CheckedAt: timePtr(r.CheckedAt),
			Error: r.LastError, NextAttemptAt: timePtr(r.NextAttemptAt),
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
		}
		if r.LastError != "" {
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
