package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

/*
The watch scheduler (ADR 0227).

Once a minute startWatches looks at every enabled scheduled watch and runs the
ones that are due, one at a time. A run is the deterministic report, then
Mate's review of it, then one row in watch_runs. Missed runs are never made up:
after downtime a due watch runs once.

A schedule is read on the boat's clock, which needs a boat position. Without
one a run is not made and not read as UTC: a skip is recorded with the reason,
at most once per missed instant, and the run is still owed when the position
returns.

If a run cannot be saved the scheduler stops every run until Helmcentral is
restarted, logs it, and says so on GET /api/watches. A history that cannot be
written is a failure to surface, not to retry silently.
*/

// watchRunTimeout is the longest a run, its report and Mate's review, may take.
const watchRunTimeout = 10 * time.Minute

type watchHost struct {
	store    func() *documentStore
	now      func() time.Time
	config   func() (watchConfig, error)
	position func() (longitude float64, err error)
	// report builds the deterministic report and the rules' findings.
	report func(ctx context.Context, w savedWatch, now time.Time, loc *time.Location) (watchReport, []watchFinding, error)
	// review has Mate read it.
	review func(ctx context.Context, cfg watchConfig, w savedWatch, rep watchReport, findings []watchFinding, now time.Time, loc *time.Location) watchReviewOutcome

	// dispatchReport hands a watch report to the notification dispatcher, to go
	// to channels. vesselName is the boat's name on it. Nil in a host that
	// notifies no one.
	dispatchReport func(report watchReportMessage, channels []string)
	vesselName     func() string

	// runMu serialises runs: the scheduler's, and "run now".
	runMu   sync.Mutex
	stopMu  sync.Mutex
	stopped string
}

// activeWatchHost is the running scheduler, for the REST routes and Mate's
// run_watch_now.
var activeWatchHost *watchHost

// halt stops every watch run until Helmcentral is restarted.
func (h *watchHost) halt(err error) {
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	if h.stopped == "" {
		h.stopped = fmt.Sprintf("Watches stopped: the run history could not be saved (%s). Restart Helmcentral after fixing it.", firstErrorLine(err))
	}
	log.Printf("watches: STOPPED until restart: %s", h.stopped)
}

// stopReason is why the scheduler is stopped, "" while it is running.
func (h *watchHost) stopReason() string {
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	return h.stopped
}

// save records a run and halts the scheduler when it cannot be recorded.
func (h *watchHost) save(run watchRun, countsAsRun bool) bool {
	err := h.store().RunTx("record watch run", func(tx *sql.Tx, _ time.Time) error {
		return insertWatchRunTx(tx, run, countsAsRun)
	})
	if errors.Is(err, errWatchNotFound) {
		// Deleted while it ran: its history went with it.
		return true
	}
	if err != nil {
		log.Printf("watches: could not record the run of %q: %v", run.WatchID, err)
		h.halt(err)
		return false
	}
	return true
}

// noteNoPosition leaves the no-position note. It is not a run.
func (h *watchHost) noteNoPosition(w savedWatch, at time.Time) {
	log.Printf("watches: %q skipped: %s", w.Name, watchNoPositionReason)
	h.save(watchRun{WatchID: w.ID, StartedAt: at, FinishedAt: at, Outcome: watchOutcomeSkipped, SkipReason: watchNoPositionReason,
		CostKnown: true, Findings: []watchFinding{}}, false)
}

// tick runs every due watch, one after another.
func (h *watchHost) tick(ctx context.Context) {
	if h.stopReason() != "" {
		return
	}
	store := h.store()
	if store == nil {
		log.Printf("watches: the record store is not open, so no watch ran")
		return
	}
	var listed []savedWatch
	if err := store.Read(func(q sqlQueryer) error {
		var err error
		listed, err = listSavedWatchesQ(q)
		return err
	}); err != nil {
		log.Printf("watches: could not read the watches: %v", err)
		return
	}

	// The boat's clock and Mate's readiness are read at most once a tick, for
	// every watch.
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
	var cfg watchConfig
	var cfgErr error
	cfgKnown := false
	readConfig := func() (watchConfig, error) {
		if !cfgKnown {
			cfgKnown = true
			cfg, cfgErr = h.config()
		}
		return cfg, cfgErr
	}

	for _, l := range listed {
		if l.Kind != watchKindScheduled || !l.Enabled {
			continue
		}
		if ctx.Err() != nil || h.stopReason() != "" {
			return
		}
		// An earlier run in this tick can take minutes, and the operator may
		// have switched this watch off or deleted it meanwhile.
		var w savedWatch
		if err := store.Read(func(q sqlQueryer) error {
			var err error
			w, err = savedWatchByIDQ(q, l.ID)
			return err
		}); err != nil {
			if !errors.Is(err, errWatchNotFound) {
				log.Printf("watches: could not re-read %q: %v", l.Name, err)
			}
			continue
		}
		if !w.Enabled {
			continue
		}
		now := h.now()
		zone, zoneErr := boatZone()
		if zoneErr != nil {
			if watchMissedInEveryZone(w, now) {
				h.noteNoPosition(w, now)
			}
			continue
		}
		due, err := watchDue(w, now, zone)
		if err != nil {
			log.Printf("watches: %v", err)
			continue
		}
		if !due {
			continue
		}
		c, err := readConfig()
		if err != nil {
			log.Printf("watches: could not read Mate's settings, so %q ran without a review: %v", w.Name, err)
			c = watchConfig{NotReady: "its settings could not be read: " + firstErrorLine(err)}
		}
		h.runLocked(ctx, c, w, now, zone, true)
	}
}

// runLocked makes one run of w and saves it. It returns the saved run, and
// nil when the run could not be made or saved. A scheduled run waits its turn
// behind any run in progress, and then checks again that it is still owed: a
// run made while it waited (Run now) has used up the instant it was queued for.
func (h *watchHost) runLocked(ctx context.Context, cfg watchConfig, w savedWatch, now time.Time, loc *time.Location, scheduled bool) *watchRun {
	h.runMu.Lock()
	defer h.runMu.Unlock()
	if h.stopReason() != "" {
		return nil
	}
	if scheduled {
		cur, owed, err := h.stillOwed(w, h.now(), loc)
		if err != nil {
			log.Printf("watches: %q: could not re-check that it is still due, so it did not run: %v", w.Name, err)
			return nil
		}
		if !owed {
			return nil
		}
		w = cur
	}
	ctx, cancel := context.WithTimeout(ctx, watchRunTimeout)
	defer cancel()
	report, findings, err := h.report(ctx, w, now, loc)
	if err != nil {
		log.Printf("watches: %q: could not build the report, so it did not run: %v", w.Name, err)
		return nil
	}
	raw, err := json.Marshal(report)
	if err != nil {
		log.Printf("watches: %q: could not encode the report: %v", w.Name, err)
		return nil
	}
	reviewCtx := ctx
	var previous watchRun
	var hadPrevious bool
	if err := h.store().Read(func(q sqlQueryer) error {
		var err error
		previous, hadPrevious, err = lastRanRunQ(q, w.ID)
		return err
	}); err != nil {
		log.Printf("watches: %q: could not read the previous run, so Mate is not told which issues it already raised and may word one differently: %v", w.Name, err)
	} else if hadPrevious {
		reviewCtx = withWatchPreviousMate(ctx, previous.Findings)
	}
	review := h.review(reviewCtx, cfg, w, report, findings, now, loc)
	all := append(append([]watchFinding{}, findings...), review.Added...)

	run := watchRun{
		WatchID: w.ID, StartedAt: now, FinishedAt: h.now(), Outcome: watchOutcomeRan,
		ReportKind: watchReportKindSections, Report: raw, Findings: all,
		CostKnown: review.CostKnown, Model: review.Model,
		BoatTime: watchBoatClock(now, loc), Timezone: watchBoatZone(now, loc),
	}
	if review.Reached {
		cost := review.CostUSD
		run.CostUSD = &cost
	}
	if review.Review != "" {
		r := review.Review
		run.Review = &r
	} else {
		problem := review.Problem
		if problem == "" {
			problem = "Mate did not review this report"
		}
		run.ReviewProblem = &problem
	}
	run.ID = uuid.NewString()
	if !h.save(run, true) {
		return nil
	}
	log.Printf("watches: %q ran: %d findings, review: %v", w.Name, len(all), review.Review != "")
	h.afterRun(run)
	return &run
}

// stillOwed re-reads w and says whether a scheduled run is still owed. A watch
// deleted, switched off or already run since it was queued is not.
func (h *watchHost) stillOwed(w savedWatch, now time.Time, loc *time.Location) (savedWatch, bool, error) {
	var cur savedWatch
	if err := h.store().Read(func(q sqlQueryer) error {
		var err error
		cur, err = savedWatchByIDQ(q, w.ID)
		return err
	}); err != nil {
		if errors.Is(err, errWatchNotFound) {
			return savedWatch{}, false, nil
		}
		return savedWatch{}, false, err
	}
	if !cur.Enabled {
		return savedWatch{}, false, nil
	}
	owed, err := watchDue(cur, now, loc)
	return cur, owed, err
}

// RunNow makes a run of w straight away, as the operator asked, with the boat's
// clock and Mate's readiness read now. The run counts as the watch's latest.
func (h *watchHost) RunNow(ctx context.Context, w savedWatch) (*watchRun, error) {
	if reason := h.stopReason(); reason != "" {
		return nil, errors.New(reason)
	}
	if w.Kind != watchKindScheduled {
		return nil, errors.New("only a scheduled watch can be run now")
	}
	lon, err := h.position()
	if err != nil {
		return nil, errors.New(watchNoPositionReason)
	}
	cfg, err := h.config()
	if err != nil {
		cfg = watchConfig{NotReady: "its settings could not be read: " + firstErrorLine(err)}
	}
	// The operator leaving the page, or a chat turn being stopped, must not
	// cancel a review that is already costing money.
	run := h.runLocked(context.WithoutCancel(ctx), cfg, w, h.now(), vesselLocalLocation(lon), false)
	if run == nil {
		if reason := h.stopReason(); reason != "" {
			return nil, errors.New(reason)
		}
		return nil, errors.New("the run could not be made; see the server log")
	}
	return run, nil
}

// ── the production wiring ───────────────────────────────────────────────

func newProductionWatchHost(settingsPath string) *watchHost {
	return &watchHost{
		store:    func() *documentStore { return globalDocumentStore },
		now:      func() time.Time { return time.Now().UTC() },
		config:   func() (watchConfig, error) { return readWatchConfig(settingsPath) },
		position: func() (float64, error) { return watchBoatLongitude() },
		report: func(ctx context.Context, w savedWatch, now time.Time, loc *time.Location) (watchReport, []watchFinding, error) {
			return productionWatchReport(settingsPath, w, now, loc)
		},
		review: runWatchReview,
		dispatchReport: func(report watchReportMessage, channels []string) {
			if globalAlarmDispatcher == nil {
				log.Printf("watches: %q was not sent: the notification dispatcher is not running", report.Title)
				return
			}
			globalAlarmDispatcher.enqueueWatchReport(report, channels)
		},
		vesselName: vesselNameForNotifications,
	}
}

// watchBoatLongitudeOf is the boat's longitude from the vessel state, or why
// there is none. It applies the checks the rest of the app applies to a
// position (usableOwnShipFix): the -1,-1 sentinel and a critical GNSS alert
// are both refused, not just an out-of-range coordinate.
func watchBoatLongitudeOf(state vesselStateData, err error) (float64, error) {
	if err != nil {
		return 0, err
	}
	_, lon, ok := usableOwnShipFix(state, nil)
	if !ok {
		return 0, errors.New("no usable vessel position")
	}
	return lon, nil
}

// watchBoatLongitude reads the boat's longitude for the scheduler and for the
// schedule preview. A var so a test can supply one.
var watchBoatLongitude = func() (float64, error) {
	state, err := fetchSignalKVesselState()
	return watchBoatLongitudeOf(state, err)
}

// productionWatchReport reads the boat and builds the report.
func productionWatchReport(settingsPath string, w savedWatch, now time.Time, loc *time.Location) (watchReport, []watchFinding, error) {
	store := globalDocumentStore
	if store == nil {
		return watchReport{}, nil, errWatchesNoStore
	}
	local := now.In(loc)
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	d := assistantProductionToolDeps(settingsPath)
	d.today = today
	in, err := gatherWatchReportInput(d, store, w, now, loc)
	if err != nil {
		return watchReport{}, nil, err
	}
	report, findings := buildWatchReport(w, in)
	return report, findings, nil
}

// startWatches is the scheduler loop; main starts it beside the alarm
// evaluator, after resuming any one-off watch that was running when
// Helmcentral stopped.
func startWatches(ctx context.Context, settingsPath string, interval time.Duration) {
	host := newProductionWatchHost(settingsPath)
	activeWatchHost = host
	if store := host.store(); store != nil {
		resumed, ended, err := resumeOneOffWatches(globalAssistantWatches, store, host.now())
		if err != nil {
			log.Printf("watches: some one-off watches could not be picked up after the restart: %v", err)
		}
		if resumed+ended > 0 {
			log.Printf("watches: after the restart, %d one-off watches resumed and %d had already run out of time", resumed, ended)
		}
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	prune := time.NewTicker(24 * time.Hour)
	defer prune.Stop()
	pruneWatchHistory(host)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			host.tick(ctx)
		case <-prune.C:
			pruneWatchHistory(host)
		}
	}
}

// pruneWatchHistory drops runs a year old that found nothing.
func pruneWatchHistory(h *watchHost) {
	store := h.store()
	if store == nil {
		return
	}
	var n int64
	err := store.RunTx("prune watch history", func(tx *sql.Tx, now time.Time) error {
		var err error
		n, err = pruneWatchRunsTx(tx, now.Add(-watchRunRetention))
		return err
	})
	if err != nil {
		log.Printf("watches: could not prune the run history: %v", err)
		return
	}
	if n > 0 {
		log.Printf("watches: dropped %d runs older than a year that found nothing", n)
	}
}
