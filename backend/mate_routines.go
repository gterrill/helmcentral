package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"unicode"
)

/*
The routine scheduler (ADR 0227).

Once a minute startMateRoutines looks at every enabled routine and runs the
ones that are due, one at a time. It only does anything while the operator has
switched "Let routines run on their own" on (assistant.routines_enabled). A
routine that comes due and cannot run leaves a note saying why and counts that
as its run for the hour or day, so a skip is recorded once, never every
minute, and never silently.

Missed runs are never made up: after downtime a due routine runs once.
A daily routine reads its time on the boat's clock, which needs a boat
position; without one it is skipped with that reason, never read as UTC.
*/

// An hourly routine is due a tick early (59 minutes), so the scheduler's own
// jitter never pushes a run a minute later each time round.
const mateRoutineHourlyGap = time.Hour - time.Minute

// A daily routine whose boat position is unknown can only be called missed
// once this long has passed since its last run, whatever the time zone.
const mateRoutineNoPositionGrace = 26 * time.Hour

// The routines daily spending limit, in US dollars (ADR 0227).
const (
	defaultRoutinesDailyCapUSD = 1.00
	minRoutinesDailyCapUSD     = 0.05
	maxRoutinesDailyCapUSD     = 20.0
)

// mateRoutineBusyWait is how long a run waits for a question the operator is
// asking in the routine's own conversation, before skipping.
var mateRoutineBusyWait = 2 * time.Minute

// mateRoutineConfig is read once per tick: the settings and, while routines
// are on, Mate's readiness and key, which each run then uses as given.
type mateRoutineConfig struct {
	Enabled      bool
	DailyCapUSD  float64
	SettingsPath string
	// NotReady is why Mate cannot answer right now, "" when it can.
	NotReady  string
	Readiness assistantReadiness
	APIKey    string
}

// mateRoutineOutcome is what a run came to.
type mateRoutineOutcome struct {
	// Note is the first line of Mate's reply, or the reason a run was skipped.
	Note    string
	Finding bool
	// Skipped marks a run that did not happen (the conversation stayed busy).
	Skipped bool
	// Reached says the model was asked, so the run belongs on the spend
	// ledger whether or not it produced a reply. CostUSD is what it was
	// charged; CostKnown false means a failure left that unreadable.
	Reached   bool
	CostUSD   float64
	CostKnown bool
	// Meter, when set, still has generations to price (a failed run).
	Meter *assistantCostMeter
}

type mateRoutineHost struct {
	store    func() *documentStore
	now      func() time.Time
	config   func() (mateRoutineConfig, error)
	position func() (longitude float64, err error)
	spend    func(since time.Time) (routineSpend, error)
	run      func(ctx context.Context, cfg mateRoutineConfig, r mateRoutine, now time.Time, loc *time.Location) (mateRoutineOutcome, error)

	stopMu  sync.Mutex
	stopped string
}

// activeMateRoutineHost is the running scheduler, for GET /api/assistant/routines
// to say whether it has stopped.
var activeMateRoutineHost *mateRoutineHost

// mateRoutineDailyInstant is the moment daily_time falls on the boat's day
// containing now.
func mateRoutineDailyInstant(dailyTime string, now time.Time, loc *time.Location) (time.Time, error) {
	t, err := time.Parse("15:04", dailyTime)
	if err != nil {
		return time.Time{}, fmt.Errorf("routine time %q is not HH:MM", dailyTime)
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
}

// mateRoutineDue says whether r has a run owed at now.
//
// An hourly routine that has never run is due on the first tick after it is
// made: the operator has just asked for it. A daily routine runs at most once
// a boat-local day, and only for a time of day that falls after both its last
// run and its last schedule change, so moving the time never makes a routine
// run twice in a day. A daily routine that has never run counts from its
// creation, so it waits for its next time. loc is the boat's zone, read only
// for a daily routine.
func mateRoutineDue(r mateRoutine, now time.Time, loc *time.Location) (bool, error) {
	switch r.Cadence {
	case routineHourly:
		return r.LastRunAt == nil || now.Sub(*r.LastRunAt) >= mateRoutineHourlyGap, nil
	case routineDaily:
		instant, err := mateRoutineDailyInstant(r.DailyTime, now, loc)
		if err != nil {
			return false, err
		}
		if now.Before(instant) {
			return false, nil
		}
		since := r.CreatedAt
		if r.ScheduleChangedAt.After(since) {
			since = r.ScheduleChangedAt
		}
		if r.LastRunAt != nil {
			if r.LastRunAt.After(since) {
				since = *r.LastRunAt
			}
			y1, m1, d1 := r.LastRunAt.In(loc).Date()
			y2, m2, d2 := now.In(loc).Date()
			if y1 == y2 && m1 == m2 && d1 == d2 {
				return false, nil
			}
		}
		return since.Before(instant), nil
	}
	return false, fmt.Errorf("routine %q has cadence %q", r.Name, r.Cadence)
}

// record saves what a run came to and, first, its row on the spend ledger. A
// routine deleted while it ran still gets its ledger row. A failure to save is
// returned: the caller stops the scheduler, since runs it cannot count would
// not count toward the daily limit.
func (h *mateRoutineHost) record(r mateRoutine, at time.Time, outcome, note string, finding bool, ledger *mateRoutineRunRow) error {
	return h.store().RunTx("record routine run", func(tx *sql.Tx, _ time.Time) error {
		if ledger != nil {
			if err := insertMateRoutineRunTx(tx, *ledger); err != nil {
				return err
			}
		}
		if err := recordMateRoutineRunTx(tx, r.ID, at, outcome, note, finding); err != nil && !errors.Is(err, errMateRoutineNotFound) {
			return err
		}
		return nil
	})
}

// halt stops every routine run until Helmcentral is restarted.
func (h *mateRoutineHost) halt(err error) {
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	if h.stopped == "" {
		h.stopped = fmt.Sprintf("Routines stopped: the run log could not be saved (%s). Restart Helmcentral after fixing it.", firstErrorLine(err))
	}
	log.Printf("routines: STOPPED until restart: %s", h.stopped)
}

// stopReason is why the scheduler is stopped, "" while it is running.
func (h *mateRoutineHost) stopReason() string {
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	return h.stopped
}

// save records a run and halts the scheduler when it cannot be recorded.
func (h *mateRoutineHost) save(r mateRoutine, at time.Time, outcome, note string, finding bool, ledger *mateRoutineRunRow) {
	if err := h.record(r, at, outcome, note, finding, ledger); err != nil {
		log.Printf("routines: could not record the run of %q (%s): %v", r.Name, note, err)
		h.halt(err)
	}
}

func (h *mateRoutineHost) skip(r mateRoutine, at time.Time, reason string) {
	log.Printf("routines: %q skipped: %s", r.Name, reason)
	h.save(r, at, routineOutcomeSkipped, "Skipped: "+reason, false, nil)
}

// noteNoPosition leaves the no-position note without counting it as a run.
func (h *mateRoutineHost) noteNoPosition(r mateRoutine, at time.Time) {
	const reason = "no boat position, so the boat's time is not known"
	log.Printf("routines: %q skipped: %s", r.Name, reason)
	err := h.store().RunTx("note routine skip", func(tx *sql.Tx, _ time.Time) error {
		if err := recordMateRoutineSkipTx(tx, r.ID, at, "Skipped: "+reason); err != nil && !errors.Is(err, errMateRoutineNotFound) {
			return err
		}
		return nil
	})
	if err != nil {
		h.halt(err)
	}
}

// tick runs every due routine, one after another.
func (h *mateRoutineHost) tick(ctx context.Context) {
	if h.stopReason() != "" {
		return
	}
	cfg, err := h.config()
	if err != nil {
		log.Printf("routines: could not read the settings, so no routine ran: %v", err)
		return
	}
	if !cfg.Enabled {
		return
	}
	store := h.store()
	if store == nil {
		log.Printf("routines: the record store is not open, so no routine ran")
		return
	}
	var routines []mateRoutine
	if err := store.Read(func(q sqlQueryer) error {
		var err error
		routines, err = listMateRoutinesQ(q)
		return err
	}); err != nil {
		log.Printf("routines: could not read the routines: %v", err)
		return
	}

	// The boat's clock is fetched at most once a tick, for every routine.
	var boat *time.Location
	var boatErr error
	boatKnown := false
	boatZone := func() (*time.Location, error) {
		if !boatKnown {
			boatKnown = true
			lon, err := h.position()
			if err != nil {
				boatErr = err
			} else {
				boat = vesselLocalLocation(lon)
			}
		}
		return boat, boatErr
	}

	for _, listed := range routines {
		if ctx.Err() != nil || h.stopReason() != "" {
			return
		}
		// An earlier run in this tick can take minutes, and the operator may
		// have switched this routine off or deleted it meanwhile.
		var r mateRoutine
		if err := store.Read(func(q sqlQueryer) error {
			var err error
			r, err = mateRoutineByIDQ(q, listed.ID)
			return err
		}); err != nil {
			if !errors.Is(err, errMateRoutineNotFound) {
				log.Printf("routines: could not re-read %q: %v", listed.Name, err)
			}
			continue
		}
		if !r.Enabled {
			continue
		}
		now := h.now()
		zone, zoneErr := boatZone()
		if zoneErr != nil {
			if r.Cadence == routineDaily {
				// Without the boat's clock nothing daily can run, and UTC is
				// never used to decide that. The note is left only once the
				// last run is old enough to be missed on any time zone, at
				// most once a day, and it is not a run: the routine is still
				// owed when the position comes back.
				since := r.CreatedAt
				if r.LastRunAt != nil {
					since = *r.LastRunAt
				}
				if now.Sub(since) > mateRoutineNoPositionGrace && (r.LastSkipAt == nil || now.Sub(*r.LastSkipAt) >= 24*time.Hour) {
					h.noteNoPosition(r, now)
				}
				continue
			}
			zone = time.UTC
		}
		due, err := mateRoutineDue(r, now, zone)
		if err != nil {
			log.Printf("routines: %v", err)
			continue
		}
		if !due {
			continue
		}
		if cfg.NotReady != "" {
			h.skip(r, now, "Mate is not ready ("+strings.TrimRight(cfg.NotReady, ".")+")")
			continue
		}
		spent, err := h.spend(now.Add(-24 * time.Hour))
		if err != nil {
			h.skip(r, now, "could not read what routines have spent: "+firstErrorLine(err))
			continue
		}
		if spent.UnknownAt != nil {
			until := spent.UnknownAt.Add(24 * time.Hour).In(zone)
			h.skip(r, now, "a run failed and its cost is not known, so routines are paused until "+until.Format("15:04 on 2 Jan (MST)"))
			continue
		}
		if spent.USD >= cfg.DailyCapUSD {
			h.skip(r, now, fmt.Sprintf("routines spent $%.2f of the $%.2f daily limit", spent.USD, cfg.DailyCapUSD))
			continue
		}
		out, err := h.run(ctx, cfg, r, now, zone)
		var ledger *mateRoutineRunRow
		if out.Reached {
			// The cost is read back from OpenRouter when the run is
			// recorded, for a run that failed after a generation started.
			if out.Meter != nil {
				out.CostUSD, out.CostKnown = out.Meter.resolve(ctx, func(c context.Context, id string) (float64, error) {
					return routineCostLookup(c, cfg.APIKey, id)
				})
			}
			ledger = &mateRoutineRunRow{RoutineID: r.ID, StartedAt: now, CostUSD: out.CostUSD, CostKnown: out.CostKnown}
		}
		switch {
		case err != nil:
			note := "Skipped: " + firstErrorLine(err)
			log.Printf("routines: %q: %s", r.Name, note)
			if ledger != nil {
				ledger.Outcome, ledger.Note = routineOutcomeSkipped, note
			}
			h.save(r, now, routineOutcomeSkipped, note, false, ledger)
		case out.Skipped:
			h.skip(r, now, out.Note)
		default:
			log.Printf("routines: %q ran: %s", r.Name, out.Note)
			if ledger != nil {
				ledger.Outcome, ledger.Note = routineOutcomeRan, out.Note
			}
			h.save(r, now, routineOutcomeRan, out.Note, out.Finding, ledger)
		}
	}
}

// mateRoutineFirstLine is the headline of a reply: its first non-blank line,
// cut to fit a list row.
func mateRoutineFirstLine(reply string) string {
	for _, line := range strings.Split(reply, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if r := []rune(line); len(r) > 200 {
			line = string(r[:197]) + "..."
		}
		return line
	}
	return ""
}

const mateRoutineNothing = "Nothing to report."

// mateRoutineIsFinding is true for any headline that does not start with
// "Nothing to report", whatever the case, markdown emphasis or punctuation
// around it.
func mateRoutineIsFinding(headline string) bool {
	trimmed := strings.TrimFunc(headline, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("*_`#>-", r)
	})
	return !strings.HasPrefix(strings.ToLower(trimmed), "nothing to report")
}

// ── the production wiring ───────────────────────────────────────────────

var errMateRoutinesNoStore = errors.New("the assistant store is not open")

// readMateRoutineConfig reads the routine settings and, while routines are on,
// Mate's readiness and key. An unreadable limit is an error, so the scheduler
// does nothing rather than run on a guess.
func readMateRoutineConfig(settingsPath string) (mateRoutineConfig, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return mateRoutineConfig{}, err
	}
	capUSD, err := routinesDailyCapFromSettings(settings)
	if err != nil {
		return mateRoutineConfig{}, err
	}
	payload := buildSettingsPayload(settings)
	cfg := mateRoutineConfig{Enabled: payload.Assistant.RoutinesEnabled, DailyCapUSD: capUSD, SettingsPath: settingsPath}
	if !cfg.Enabled {
		return cfg, nil
	}
	readiness, apiKey, err := checkAssistantReadiness(settingsPath)
	if err != nil {
		return mateRoutineConfig{}, err
	}
	cfg.Readiness, cfg.APIKey, cfg.NotReady = readiness, apiKey, readiness.Problem
	return cfg, nil
}

// routineCostLookup reads a generation's cost from OpenRouter. Tests replace it.
var routineCostLookup = func(ctx context.Context, apiKey, id string) (float64, error) {
	return openRouterGenerationCost(ctx, openRouterHTTPClient, apiKey, id)
}

func newProductionMateRoutineHost(settingsPath string) *mateRoutineHost {
	return &mateRoutineHost{
		store:  func() *documentStore { return globalDocumentStore },
		now:    func() time.Time { return time.Now().UTC() },
		config: func() (mateRoutineConfig, error) { return readMateRoutineConfig(settingsPath) },
		position: func() (float64, error) {
			state, err := fetchSignalKVesselState()
			if err != nil {
				return 0, err
			}
			if !hasUsableVesselPosition(state.Latitude, state.Longitude) {
				return 0, errors.New("no usable vessel position")
			}
			return state.Longitude, nil
		},
		spend: func(since time.Time) (routineSpend, error) {
			if globalDocumentStore == nil {
				return routineSpend{}, errors.New("the record store is not open")
			}
			return mateRoutineSpendSince(globalDocumentStore, since)
		},
		run: runMateRoutineTurn,
	}
}

// startMateRoutines is the scheduler loop; main starts it beside the alarm
// evaluator.
func startMateRoutines(ctx context.Context, settingsPath string, interval time.Duration) {
	host := newProductionMateRoutineHost(settingsPath)
	activeMateRoutineHost = host
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			host.tick(ctx)
		}
	}
}
