package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// This file is ADR 0160: Mate runs a timed watch on live telemetry. The
// start_watch tool samples up to six SignalK paths from the live snapshot
// once a second for 1-30 minutes, in memory. When the watch ends, the report
// is appended to the same conversation and one Mate turn is started through
// the ordinary run machinery (assistant_run_registry.go, beginAssistantTurn
// in assistant_handlers.go), so an open chat sees the reply arrive and a
// chat opened later finds it in its history.
//
// What this deliberately is not: no InfluxDB (the watch reads what the
// instruments say now, at 1 Hz, not what the logger kept), no alerting during
// the watch (alarms already do that), and no persistence (a restart cancels
// every watch; the tool result and the docs say so).

const (
	assistantWatchMaxPaths      = 6
	assistantWatchMinMinutes    = 1
	assistantWatchMaxMinutes    = 30
	assistantWatchMaxConcurrent = 3

	assistantWatchSampleInterval = time.Second

	// assistantWatchStaleAfter is how old a reading may be and still count
	// as a sample. Far tighter than assistantDiagnosticsStaleAfter (120 s):
	// a watch samples every second looking for spikes, and a value that has
	// not changed for ten seconds is a held value, not a measurement. The
	// same threshold decides whether a path is live enough to start on.
	assistantWatchStaleAfter = 10 * time.Second

	assistantWatchLabelMaxRunes  = 40
	assistantWatchReasonMaxRunes = 200

	// assistantWatchMedianWindow is the centred rolling-median window, in
	// samples (seconds), excursions are measured against: long enough that a
	// spike of a few seconds cannot drag its own baseline, short enough to
	// follow a deliberate throttle change within a minute.
	assistantWatchMedianWindow = 61
	// assistantWatchMADK is how many robust standard deviations
	// (1.4826 x MAD of the residuals) a sample must sit from its rolling
	// median to count as an excursion.
	assistantWatchMADK = 5.0
	// assistantWatchFloorFraction is the excursion threshold's floor, as a
	// fraction of the series' own range, so a mostly flat signal (MAD 0)
	// does not report movement that is small against what the series did.
	// Never a fraction of the level: that depends on the unit's zero point
	// (coolant at 358 K would get a 17.9 K floor, a 26.4 V bank a 1.3 V one).
	assistantWatchFloorFraction = 0.05
	// assistantWatchEpisodeBridge merges flagged samples this close together
	// into one episode.
	assistantWatchEpisodeBridge = 3 * time.Second

	assistantWatchMaxListedExcursions = 10
	assistantWatchMaxListedGaps       = 10

	assistantWatchStatusWatching  = "watching"
	assistantWatchStatusReporting = "reporting"
)

// assistantWatchFollowUpDeadline bounds how long the end-of-watch turn waits
// for questions the skipper is asking in the same conversation to finish.
// A var so a test can shorten it.
var assistantWatchFollowUpDeadline = 3 * (assistantRunTimeout + 30*time.Second)

// ── arguments ───────────────────────────────────────────────────────────

// assistantWatchPathArg is one watched path. There is deliberately no source
// filter: the live snapshot keeps only the last writer's value and $source
// per path, so a filter would record a second publisher's updates as gaps.
// A path fed by more than one source is reported as such instead
// (sources_seen, source_switches). Source is decoded only to refuse it.
type assistantWatchPathArg struct {
	Path   string `json:"path"`
	Label  string `json:"label,omitempty"`
	Source string `json:"source,omitempty"`
}

type assistantStartWatchArgs struct {
	Paths   []assistantWatchPathArg `json:"paths"`
	Minutes int                     `json:"minutes"`
	Reason  string                  `json:"reason"`
}

// parseAssistantStartWatchArgs decodes and validates start_watch's
// arguments. Out-of-range values are rejected, never clamped: the model
// asked for something specific and is told plainly what is allowed.
func parseAssistantStartWatchArgs(raw json.RawMessage) (assistantStartWatchArgs, error) {
	var args assistantStartWatchArgs
	if err := json.Unmarshal(raw, &args); err != nil {
		return args, fmt.Errorf("start_watch: parse arguments (minutes must be a whole number from %d to %d): %w", assistantWatchMinMinutes, assistantWatchMaxMinutes, err)
	}
	if len(args.Paths) < 1 || len(args.Paths) > assistantWatchMaxPaths {
		return args, fmt.Errorf("start_watch: give 1 to %d paths, got %d", assistantWatchMaxPaths, len(args.Paths))
	}
	if args.Minutes < assistantWatchMinMinutes || args.Minutes > assistantWatchMaxMinutes {
		return args, fmt.Errorf("start_watch: minutes must be a whole number from %d to %d, got %d", assistantWatchMinMinutes, assistantWatchMaxMinutes, args.Minutes)
	}
	args.Reason = strings.TrimSpace(args.Reason)
	if args.Reason == "" {
		return args, fmt.Errorf("start_watch: reason is required - say in a few words what you are looking for, so the report can be read against it")
	}
	if utf8.RuneCountInString(args.Reason) > assistantWatchReasonMaxRunes {
		return args, fmt.Errorf("start_watch: reason must be %d characters or fewer", assistantWatchReasonMaxRunes)
	}
	seen := map[string]bool{}
	for i := range args.Paths {
		p := &args.Paths[i]
		p.Path = strings.TrimSpace(p.Path)
		p.Label = strings.TrimSpace(p.Label)
		if p.Path == "" {
			return args, fmt.Errorf("start_watch: paths[%d]: path is required", i)
		}
		if strings.TrimSpace(p.Source) != "" {
			return args, fmt.Errorf("start_watch: %s: a watch cannot follow one source - the live feed keeps only the latest reading per path, whichever source sent it; drop source, and the report will say if more than one source fed the path", p.Path)
		}
		key := p.Path
		if seen[key] {
			return args, fmt.Errorf("start_watch: %s is listed more than once", p.Path)
		}
		seen[key] = true
		if utf8.RuneCountInString(p.Label) > assistantWatchLabelMaxRunes {
			return args, fmt.Errorf("start_watch: paths[%d]: label must be %d characters or fewer", i, assistantWatchLabelMaxRunes)
		}
		if p.Label == "" {
			p.Label = humanisePath(p.Path)
		}
	}
	return args, nil
}

// ── reading the live snapshot ───────────────────────────────────────────

// assistantWatchReading is one path's state in the live snapshot at one
// instant. AgeSeconds is -1 when the snapshot has no update time for it.
type assistantWatchReading struct {
	Present    bool
	Numeric    bool
	Value      float64
	Source     string
	Units      string
	AgeSeconds float64
}

type assistantWatchReader func(path string, now time.Time) assistantWatchReading

// assistantWatchReadFromGlobalSnapshot is the production reader: one node
// copy per path per second (nodeAt, not selfTree's whole-tree copy), aged
// the same way check_signalk_paths ages a leaf.
func assistantWatchReadFromGlobalSnapshot(path string, now time.Time) assistantWatchReading {
	node := globalSignalKSnapshot.nodeAt(path)
	if node == nil {
		return assistantWatchReading{}
	}
	raw, ok := node["value"]
	if !ok {
		return assistantWatchReading{}
	}
	reading := assistantWatchReading{Present: true, Units: unitsFor(node)}
	reading.Source, _ = node["$source"].(string)
	reading.Value, reading.Numeric = raw.(float64)
	reading.AgeSeconds = signalKPathSampleAge(globalSignalKSnapshot, globalSignalKSnapshot.selfContext(), path, node, now)
	return reading
}

// assistantWatchSampleProblem says why a reading is not a usable sample of
// spec right now, or "" when it is.
func assistantWatchSampleProblem(spec assistantWatchPathArg, r assistantWatchReading) string {
	switch {
	case !r.Present:
		return "missing"
	case !r.Numeric:
		return "not a number"
	case r.AgeSeconds < 0 || r.AgeSeconds > assistantWatchStaleAfter.Seconds():
		return "not updating"
	}
	return ""
}

// assistantWatchStartError words a reading that is not live enough to start
// on, naming the path, for the model to relay or correct.
func assistantWatchStartError(spec assistantWatchPathArg, r assistantWatchReading) error {
	switch {
	case !r.Present:
		return fmt.Errorf("start_watch: %s is not in the live instrument feed right now; check the exact path with check_signalk_paths", spec.Path)
	case !r.Numeric:
		return fmt.Errorf("start_watch: %s is not a number; a watch can only follow numeric readings", spec.Path)
	case r.AgeSeconds < 0:
		return fmt.Errorf("start_watch: %s has no update time, so a frozen reading could not be told from a live one", spec.Path)
	case r.AgeSeconds > assistantWatchStaleAfter.Seconds():
		return fmt.Errorf("start_watch: %s last updated %.0f s ago; a watch needs a reading that updates at least every %.0f s", spec.Path, r.AgeSeconds, assistantWatchStaleAfter.Seconds())
	}
	return nil
}

// ── the registry ────────────────────────────────────────────────────────

// assistantWatchTicker is the slice of *time.Ticker the sampler needs, so a
// test can deliver ticks by hand.
type assistantWatchTicker interface {
	C() <-chan time.Time
	Stop()
}

type realAssistantWatchTicker struct{ t *time.Ticker }

func (r realAssistantWatchTicker) C() <-chan time.Time { return r.t.C }
func (r realAssistantWatchTicker) Stop()               { r.t.Stop() }

type assistantWatchRegistryDeps struct {
	now       func() time.Time
	newTicker func(d time.Duration) assistantWatchTicker
	read      assistantWatchReader
	// finish receives every watch that ran to its end, on the watch's own
	// goroutine, before the watch leaves the registry: while it runs, GET
	// .../watch reports "reporting", so a page polling for the end sees the
	// follow-up run already registered by the time the watch disappears.
	finish func(info assistantWatchInfo, report assistantWatchReport)
}

// assistantWatchInfo is a watch as the chip, the tool and the follow-up see
// it. Paths and the zone stay server-side (json:"-"): the chip names what is
// watched by label only.
type assistantWatchInfo struct {
	ID             string    `json:"id"`
	ConversationID string    `json:"conversation_id"`
	Subject        string    `json:"subject"`
	Labels         []string  `json:"labels"`
	Minutes        int       `json:"minutes"`
	StartedAt      time.Time `json:"started_at"`
	EndsAt         time.Time `json:"ends_at"`
	Status         string    `json:"status"`

	Reason        string                  `json:"-"`
	Paths         []assistantWatchPathArg `json:"-"`
	Today         time.Time               `json:"-"`
	Location      *time.Location          `json:"-"`
	TimezoneLabel string                  `json:"-"`
}

type assistantWatchRequest struct {
	ConversationID string
	Args           assistantStartWatchArgs
	// Today is the operator's own date from the run that started the watch,
	// carried to the follow-up turn (the maintenance tools need it, and the
	// server's clock is not the boat's date).
	Today         time.Time
	Location      *time.Location
	TimezoneLabel string
}

type assistantWatch struct {
	info     assistantWatchInfo
	stop     chan struct{}
	stopOnce sync.Once
}

type assistantWatchRegistry struct {
	deps    assistantWatchRegistryDeps
	mu      sync.Mutex
	watches map[string]*assistantWatch
}

func newAssistantWatchRegistry(deps assistantWatchRegistryDeps) *assistantWatchRegistry {
	return &assistantWatchRegistry{deps: deps, watches: map[string]*assistantWatch{}}
}

// globalAssistantWatches is the process-wide registry. In memory only: a
// restart cancels every watch.
var globalAssistantWatches = newAssistantWatchRegistry(assistantWatchRegistryDeps{
	now: time.Now,
	newTicker: func(d time.Duration) assistantWatchTicker {
		return realAssistantWatchTicker{t: time.NewTicker(d)}
	},
	read: assistantWatchReadFromGlobalSnapshot,
})

// The follow-up is wired in init, not in the var above: it reaches the run
// launcher, which reaches the tool deps, which reach this registry, and Go
// refuses that as an initialisation cycle.
func init() {
	globalAssistantWatches.deps.finish = assistantWatchDeliverReport
}

// assistantWatchDeliverReport is the production finish hook.
func assistantWatchDeliverReport(info assistantWatchInfo, report assistantWatchReport) {
	content, err := assistantWatchReportMessage(info, report)
	if err != nil {
		log.Printf("assistant: watch %s for conversation %s finished but its report could not be built: %v", info.ID, info.ConversationID, err)
		return
	}
	assistantWatchFollowUp(info, content)
}

// assistantWatchSubject joins labels the way a sentence would.
func assistantWatchSubject(labels []string) string {
	switch len(labels) {
	case 0:
		return ""
	case 1:
		return labels[0]
	default:
		return strings.Join(labels[:len(labels)-1], ", ") + " and " + labels[len(labels)-1]
	}
}

// start validates that every path is live now, then registers and launches
// the watch. One watch per conversation, assistantWatchMaxConcurrent in all.
func (reg *assistantWatchRegistry) start(req assistantWatchRequest) (assistantWatchInfo, error) {
	loc := req.Location
	if loc == nil {
		loc = time.UTC
	}
	now := reg.deps.now()

	reg.mu.Lock()
	if existing, ok := reg.watches[req.ConversationID]; ok {
		reg.mu.Unlock()
		return assistantWatchInfo{}, fmt.Errorf("start_watch: a watch is already running in this conversation (%s, until %s); wait for its report, or the skipper can stop it from the chat",
			existing.info.Subject, existing.info.EndsAt.In(loc).Format("15:04"))
	}
	if len(reg.watches) >= assistantWatchMaxConcurrent {
		reg.mu.Unlock()
		return assistantWatchInfo{}, fmt.Errorf("start_watch: %d watches are already running across Helmcentral, the most it runs at once; try again when one has reported", assistantWatchMaxConcurrent)
	}
	reg.mu.Unlock()

	for _, spec := range req.Args.Paths {
		if err := assistantWatchStartError(spec, reg.deps.read(spec.Path, now)); err != nil {
			return assistantWatchInfo{}, err
		}
	}

	labels := make([]string, len(req.Args.Paths))
	for i, p := range req.Args.Paths {
		labels[i] = p.Label
	}
	info := assistantWatchInfo{
		ID:             uuid.NewString(),
		ConversationID: req.ConversationID,
		Subject:        assistantWatchSubject(labels),
		Labels:         labels,
		Minutes:        req.Args.Minutes,
		StartedAt:      now,
		EndsAt:         now.Add(time.Duration(req.Args.Minutes) * time.Minute),
		Status:         assistantWatchStatusWatching,
		Reason:         req.Args.Reason,
		Paths:          req.Args.Paths,
		Today:          req.Today,
		Location:       loc,
		TimezoneLabel:  req.TimezoneLabel,
	}
	w := &assistantWatch{info: info, stop: make(chan struct{})}

	// Checked again under the lock: the liveness reads above ran without it.
	reg.mu.Lock()
	if _, ok := reg.watches[req.ConversationID]; ok {
		reg.mu.Unlock()
		return assistantWatchInfo{}, fmt.Errorf("start_watch: a watch is already running in this conversation")
	}
	if len(reg.watches) >= assistantWatchMaxConcurrent {
		reg.mu.Unlock()
		return assistantWatchInfo{}, fmt.Errorf("start_watch: %d watches are already running across Helmcentral, the most it runs at once; try again when one has reported", assistantWatchMaxConcurrent)
	}
	reg.watches[req.ConversationID] = w
	reg.mu.Unlock()

	ticker := reg.deps.newTicker(assistantWatchSampleInterval)
	go reg.run(w, ticker)
	log.Printf("assistant: watch %s started for conversation %s: %d paths for %d min", info.ID, info.ConversationID, len(info.Paths), info.Minutes)
	return info, nil
}

func (reg *assistantWatchRegistry) get(conversationID string) (assistantWatchInfo, bool) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	w, ok := reg.watches[conversationID]
	if !ok {
		return assistantWatchInfo{}, false
	}
	return w.info, true
}

// errAssistantWatchReporting is cancel's answer for a watch that has already
// ended and is handing its report to Mate: too late to stop.
var errAssistantWatchReporting = errors.New("the watch has already finished; Mate is reading its report")

// cancel stops conversationID's watch without a report. It reports whether
// there was one to stop. A watch already reporting cannot be cancelled
// (errAssistantWatchReporting): its follow-up turn is already on its way,
// and the chat's own Stop on a reply covers that.
func (reg *assistantWatchRegistry) cancel(conversationID string) bool {
	stopped, _ := reg.stop(conversationID)
	return stopped
}

func (reg *assistantWatchRegistry) stop(conversationID string) (bool, error) {
	reg.mu.Lock()
	w, ok := reg.watches[conversationID]
	if !ok {
		reg.mu.Unlock()
		return false, nil
	}
	if w.info.Status != assistantWatchStatusWatching {
		reg.mu.Unlock()
		return false, errAssistantWatchReporting
	}
	delete(reg.watches, conversationID)
	reg.mu.Unlock()
	w.stopOnce.Do(func() { close(w.stop) })
	log.Printf("assistant: watch %s for conversation %s stopped before it ended", w.info.ID, conversationID)
	return true, nil
}

// assistantWatchSample is one tick of one series: a value when OK, or the
// reason it is missing.
type assistantWatchSample struct {
	T      time.Time
	Value  float64
	OK     bool
	Reason string
	Source string
	Units  string
}

func (reg *assistantWatchRegistry) run(w *assistantWatch, ticker assistantWatchTicker) {
	defer ticker.Stop()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("assistant: watch %s for conversation %s panicked: %v", w.info.ID, w.info.ConversationID, r)
			reg.remove(w)
		}
	}()

	series := make([][]assistantWatchSample, len(w.info.Paths))
	for {
		select {
		case <-w.stop:
			return
		case t := <-ticker.C():
			if t.After(w.info.EndsAt) {
				reg.finishWatch(w, series)
				return
			}
			for i, spec := range w.info.Paths {
				r := reg.deps.read(spec.Path, t)
				s := assistantWatchSample{T: t, Source: r.Source, Units: r.Units}
				if problem := assistantWatchSampleProblem(spec, r); problem != "" {
					s.Reason = problem
				} else {
					s.Value, s.OK = r.Value, true
				}
				series[i] = append(series[i], s)
			}
			if !t.Before(w.info.EndsAt) {
				reg.finishWatch(w, series)
				return
			}
		}
	}
}

func (reg *assistantWatchRegistry) finishWatch(w *assistantWatch, series [][]assistantWatchSample) {
	reg.mu.Lock()
	if reg.watches[w.info.ConversationID] != w {
		// Cancelled between the last tick and here.
		reg.mu.Unlock()
		return
	}
	w.info.Status = assistantWatchStatusReporting
	info := w.info
	reg.mu.Unlock()
	defer reg.remove(w)

	report := buildAssistantWatchReport(info, series)
	log.Printf("assistant: watch %s for conversation %s ended; handing the report to Mate", info.ID, info.ConversationID)
	if reg.deps.finish != nil {
		reg.deps.finish(info, report)
	}
}

func (reg *assistantWatchRegistry) remove(w *assistantWatch) {
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if reg.watches[w.info.ConversationID] == w {
		delete(reg.watches, w.info.ConversationID)
	}
}

// ── the report ──────────────────────────────────────────────────────────

type assistantWatchGap struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Seconds int    `json:"seconds"`
	Reason  string `json:"reason"`
}

type assistantWatchExcursion struct {
	Start     string  `json:"start"`
	Seconds   int     `json:"seconds"`
	Peak      float64 `json:"peak"`
	Baseline  float64 `json:"baseline"`
	Direction string  `json:"direction"`
}

type assistantWatchSeriesReport struct {
	Path        string   `json:"path,omitempty"`
	Label       string   `json:"label"`
	SourcesSeen []string `json:"sources_seen,omitempty"`
	// SourceSwitches counts samples whose source differs from the sample
	// before: more than zero means two publishers took turns on this path,
	// and a jump between them is not the reading changing.
	SourceSwitches int                       `json:"source_switches,omitempty"`
	Units          string                    `json:"units,omitempty"`
	Ticks          int                       `json:"ticks"`
	Samples        int                       `json:"samples"`
	Min            *float64                  `json:"min,omitempty"`
	Mean           *float64                  `json:"mean,omitempty"`
	Max            *float64                  `json:"max,omitempty"`
	Stdev          *float64                  `json:"stdev,omitempty"`
	First          *float64                  `json:"first,omitempty"`
	Last           *float64                  `json:"last,omitempty"`
	Gaps           []assistantWatchGap       `json:"gaps,omitempty"`
	GapCount       int                       `json:"gap_count,omitempty"`
	GapSeconds     int                       `json:"gap_seconds,omitempty"`
	Excursions     []assistantWatchExcursion `json:"excursions,omitempty"`
	ExcursionCount int                       `json:"excursion_count"`
}

type assistantWatchReport struct {
	WatchID         string                       `json:"watch_id"`
	Reason          string                       `json:"reason"`
	Timezone        string                       `json:"timezone"`
	Started         string                       `json:"started"`
	Ended           string                       `json:"ended"`
	Minutes         int                          `json:"minutes"`
	SampleIntervalS int                          `json:"sample_interval_s"`
	Series          []assistantWatchSeriesReport `json:"series"`
	Difference      *assistantWatchSeriesReport  `json:"difference,omitempty"`
	Truncated       bool                         `json:"truncated,omitempty"`
	Note            string                       `json:"note"`
}

const assistantWatchReportNote = "Values are SignalK units, sampled once a second from the live feed. A gap is seconds with no usable reading (missing, not updating for 10 s, or not a number); gaps are not filled. source_switches above zero means more than one source took turns on that path, so a jump may be one source differing from the other rather than the reading changing. An excursion is a run of samples more than 5 robust standard deviations (or 5% of the series' range, whichever is larger) from the rolling 61 s median; baseline is that median at the peak. Times are vessel local."

func roundTo4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func f64p(v float64) *float64 { v = roundTo4(v); return &v }

func buildAssistantWatchReport(info assistantWatchInfo, series [][]assistantWatchSample) assistantWatchReport {
	loc := info.Location
	if loc == nil {
		loc = time.UTC
	}
	report := assistantWatchReport{
		WatchID:         info.ID,
		Reason:          info.Reason,
		Timezone:        info.TimezoneLabel,
		Started:         info.StartedAt.In(loc).Format("Mon 2 Jan 15:04:05"),
		Ended:           info.EndsAt.In(loc).Format("Mon 2 Jan 15:04:05"),
		Minutes:         info.Minutes,
		SampleIntervalS: int(assistantWatchSampleInterval / time.Second),
		Note:            assistantWatchReportNote,
	}
	if report.Timezone == "" {
		report.Timezone = loc.String()
	}
	for i, spec := range info.Paths {
		var samples []assistantWatchSample
		if i < len(series) {
			samples = series[i]
		}
		r := summarizeAssistantWatchSeries(samples, loc)
		r.Path, r.Label = spec.Path, spec.Label
		report.Series = append(report.Series, r)
	}
	if len(info.Paths) == 2 && len(series) == 2 {
		d := summarizeAssistantWatchSeries(assistantWatchDifference(series[0], series[1]), loc)
		d.Label = info.Paths[0].Label + " minus " + info.Paths[1].Label
		d.SourcesSeen, d.SourceSwitches = nil, 0
		report.Difference = &d
	}
	return report
}

// assistantWatchDifference pairs two series tick by tick: a minus b where
// both have a sample, otherwise a gap carrying whichever side's reason.
func assistantWatchDifference(a, b []assistantWatchSample) []assistantWatchSample {
	n := min(len(a), len(b))
	out := make([]assistantWatchSample, n)
	for i := 0; i < n; i++ {
		s := assistantWatchSample{T: a[i].T, Units: a[i].Units}
		switch {
		case !a[i].OK:
			s.Reason = a[i].Reason
		case !b[i].OK:
			s.Reason = b[i].Reason
		default:
			s.Value, s.OK = a[i].Value-b[i].Value, true
		}
		out[i] = s
	}
	return out
}

func summarizeAssistantWatchSeries(samples []assistantWatchSample, loc *time.Location) assistantWatchSeriesReport {
	r := assistantWatchSeriesReport{Ticks: len(samples)}
	sources := map[string]bool{}
	var sum, sumSq float64
	var lo, hi float64
	var first, last *float64
	lastSource := ""
	for _, s := range samples {
		if s.Units != "" && r.Units == "" {
			r.Units = s.Units
		}
		if !s.OK {
			continue
		}
		if s.Source != "" && !sources[s.Source] {
			sources[s.Source] = true
			r.SourcesSeen = append(r.SourcesSeen, s.Source)
		}
		if lastSource != "" && s.Source != "" && s.Source != lastSource {
			r.SourceSwitches++
		}
		if s.Source != "" {
			lastSource = s.Source
		}
		if r.Samples == 0 || s.Value < lo {
			lo = s.Value
		}
		if r.Samples == 0 || s.Value > hi {
			hi = s.Value
		}
		r.Samples++
		sum += s.Value
		sumSq += s.Value * s.Value
		if first == nil {
			first = f64p(s.Value)
		}
		last = f64p(s.Value)
	}
	if r.Samples > 0 {
		mean := sum / float64(r.Samples)
		variance := math.Max(0, sumSq/float64(r.Samples)-mean*mean)
		r.Min, r.Max, r.Mean, r.Stdev = f64p(lo), f64p(hi), f64p(mean), f64p(math.Sqrt(variance))
		r.First, r.Last = first, last
	}

	r.Gaps, r.GapCount, r.GapSeconds = assistantWatchGaps(samples, loc)
	r.Excursions, r.ExcursionCount = detectAssistantWatchExcursions(samples, loc)
	return r
}

// assistantWatchGaps merges consecutive missing ticks into ranges. The first
// assistantWatchMaxListedGaps are listed; the count and total cover all.
func assistantWatchGaps(samples []assistantWatchSample, loc *time.Location) ([]assistantWatchGap, int, int) {
	var gaps []assistantWatchGap
	count, total := 0, 0
	for i := 0; i < len(samples); {
		if samples[i].OK {
			i++
			continue
		}
		j := i
		for j+1 < len(samples) && !samples[j+1].OK {
			j++
		}
		seconds := j - i + 1
		count++
		total += seconds
		if len(gaps) < assistantWatchMaxListedGaps {
			gaps = append(gaps, assistantWatchGap{
				From:    samples[i].T.In(loc).Format("15:04:05"),
				To:      samples[j].T.In(loc).Format("15:04:05"),
				Seconds: seconds,
				Reason:  samples[i].Reason,
			})
		}
		i = j + 1
	}
	return gaps, count, total
}

func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// detectAssistantWatchExcursions finds runs of samples that sit well away
// from their centred rolling median, merged into episodes. Only real samples
// take part; gaps are skipped, never filled. Episodes are ranked by size,
// the largest assistantWatchMaxListedExcursions kept, then listed in time
// order; the count covers every episode.
func detectAssistantWatchExcursions(samples []assistantWatchSample, loc *time.Location) ([]assistantWatchExcursion, int) {
	var ok []assistantWatchSample
	for _, s := range samples {
		if s.OK {
			ok = append(ok, s)
		}
	}
	n := len(ok)
	if n < 5 {
		return nil, 0
	}

	values := make([]float64, n)
	for i, s := range ok {
		values[i] = s.Value
	}
	half := assistantWatchMedianWindow / 2
	medians := make([]float64, n)
	residuals := make([]float64, n)
	absResiduals := make([]float64, n)
	for i := range ok {
		lo, hi := max(0, i-half), min(n, i+half+1)
		medians[i] = medianOf(values[lo:hi])
		residuals[i] = values[i] - medians[i]
		absResiduals[i] = math.Abs(residuals[i])
	}
	sigma := 1.4826 * medianOf(absResiduals)
	lo, hi := values[0], values[0]
	for _, v := range values {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	floor := assistantWatchFloorFraction*(hi-lo) + 1e-9
	threshold := math.Max(assistantWatchMADK*sigma, floor)

	type episode struct {
		start, end time.Time
		peakIdx    int
	}
	var episodes []episode
	for i := range ok {
		if absResiduals[i] <= threshold {
			continue
		}
		if len(episodes) > 0 && ok[i].T.Sub(episodes[len(episodes)-1].end) <= assistantWatchEpisodeBridge {
			e := &episodes[len(episodes)-1]
			e.end = ok[i].T
			if absResiduals[i] > absResiduals[e.peakIdx] {
				e.peakIdx = i
			}
			continue
		}
		episodes = append(episodes, episode{start: ok[i].T, end: ok[i].T, peakIdx: i})
	}

	count := len(episodes)
	if count > assistantWatchMaxListedExcursions {
		sort.SliceStable(episodes, func(a, b int) bool {
			return absResiduals[episodes[a].peakIdx] > absResiduals[episodes[b].peakIdx]
		})
		episodes = episodes[:assistantWatchMaxListedExcursions]
		sort.SliceStable(episodes, func(a, b int) bool { return episodes[a].start.Before(episodes[b].start) })
	}

	out := make([]assistantWatchExcursion, 0, len(episodes))
	for _, e := range episodes {
		direction := "above"
		if residuals[e.peakIdx] < 0 {
			direction = "below"
		}
		out = append(out, assistantWatchExcursion{
			Start:     e.start.In(loc).Format("15:04:05"),
			Seconds:   int(e.end.Sub(e.start)/time.Second) + 1,
			Peak:      roundTo4(values[e.peakIdx]),
			Baseline:  roundTo4(medians[e.peakIdx]),
			Direction: direction,
		})
	}
	return out, count
}

// assistantWatchReportMessage is the persisted watch row: one headline line
// the chat shows the operator, then the model-facing report, bounded by the
// same budget a tool result gets.
func assistantWatchReportMessage(info assistantWatchInfo, report assistantWatchReport) (string, error) {
	shrink := func() bool {
		report.Truncated = true
		// Least essential first: listed excursions on the longest list, then
		// listed gaps. Counts always survive.
		lists := make([]*[]assistantWatchExcursion, 0, len(report.Series)+1)
		gapLists := make([]*[]assistantWatchGap, 0, len(report.Series)+1)
		for i := range report.Series {
			lists = append(lists, &report.Series[i].Excursions)
			gapLists = append(gapLists, &report.Series[i].Gaps)
		}
		if report.Difference != nil {
			lists = append(lists, &report.Difference.Excursions)
			gapLists = append(gapLists, &report.Difference.Gaps)
		}
		var longest *[]assistantWatchExcursion
		for _, l := range lists {
			if len(*l) > 0 && (longest == nil || len(*l) > len(*longest)) {
				longest = l
			}
		}
		if longest != nil {
			*longest = (*longest)[:len(*longest)-1]
			return true
		}
		for _, g := range gapLists {
			if len(*g) > 0 {
				*g = (*g)[:len(*g)-1]
				return true
			}
		}
		return false
	}
	body, err := capToolResultJSON(&report, shrink)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Watch finished: %s (%d min)\n", info.Subject, info.Minutes)
	b.WriteString("[Automatic watch report from Helmcentral, not a message from the skipper. You started this watch " +
		"earlier in this conversation with start_watch. Tell the skipper what it showed, read against the reason you " +
		"gave, in plain words and their units. Say what you could not see, such as gaps. Do not start another watch " +
		"unless they ask.]\n")
	b.WriteString(body)
	return b.String(), nil
}

// ── the end-of-watch handoff ────────────────────────────────────────────

// assistantWatchFollowUp appends the report to the watch's conversation and
// starts one Mate turn on it, through the same run registry and turn
// launcher a posted question uses. It reports whether a turn started.
//
// Nothing is appended and no turn starts when Mate has been switched off
// (or lost its key or model) since the watch began, or the conversation has
// been deleted: Mate being on is the consent to send anything to the model,
// and a deleted conversation has nowhere to put an answer. Both are logged.
func assistantWatchFollowUp(info assistantWatchInfo, content string) bool {
	id := info.ConversationID
	settingsPath := assistantSettingsPath()
	readiness, apiKey, err := checkAssistantReadiness(settingsPath)
	if err != nil {
		log.Printf("assistant: watch %s for conversation %s finished but readiness could not be read; no follow-up: %v", info.ID, id, err)
		return false
	}
	if readiness.Problem != "" {
		log.Printf("assistant: watch %s for conversation %s finished but Mate cannot answer (%s); no follow-up", info.ID, id, readiness.Problem)
		return false
	}
	params, err := assistantTurnParamsFromSettings(settingsPath, readiness, apiKey)
	if err != nil {
		log.Printf("assistant: watch %s for conversation %s finished but settings could not be read; no follow-up: %v", info.ID, id, err)
		return false
	}
	params.today = info.Today

	if _, ok, err := globalAssistantStore.GetConversation(id); err != nil {
		log.Printf("assistant: watch %s finished but conversation %s could not be read; no follow-up: %v", info.ID, id, err)
		return false
	} else if !ok {
		log.Printf("assistant: watch %s finished but conversation %s has been deleted; no follow-up", info.ID, id)
		return false
	}

	runCtx, run, ok := acquireAssistantRunForWatch(id, assistantWatchFollowUpDeadline)
	if !ok {
		// Not dropped: the report is kept in the conversation, where the
		// skipper sees it and Mate reads it with the next question.
		if _, err := globalAssistantStore.AppendMessage(assistantMessage{ConversationID: id, Role: "watch", Content: content}); err != nil {
			log.Printf("assistant: watch %s: conversation %s stayed busy and the report could not be kept: %v", info.ID, id, err)
		} else {
			log.Printf("assistant: watch %s: conversation %s stayed busy; report kept without a follow-up turn", info.ID, id)
		}
		return false
	}

	if _, err := globalAssistantStore.AppendMessage(assistantMessage{ConversationID: id, Role: "watch", Content: content}); err != nil {
		globalAssistantRuns.remove(id, run)
		if errors.Is(err, errAssistantConversationNotFound) {
			log.Printf("assistant: watch %s finished but conversation %s has been deleted; no follow-up", info.ID, id)
		} else {
			log.Printf("assistant: watch %s: persist report for conversation %s: %v", info.ID, id, err)
		}
		return false
	}
	messages, err := globalAssistantStore.ListMessages(id)
	if err != nil {
		globalAssistantRuns.remove(id, run)
		log.Printf("assistant: watch %s: list messages for conversation %s: %v", info.ID, id, err)
		return false
	}
	if err := beginAssistantTurn(runCtx, run, id, messages, params); err != nil {
		globalAssistantRuns.remove(id, run)
		log.Printf("assistant: watch %s: start the follow-up turn for conversation %s: %v", info.ID, id, err)
		return false
	}
	log.Printf("assistant: watch %s report delivered to conversation %s; Mate is answering", info.ID, id)
	return true
}

// acquireAssistantRunForWatch registers a run for id, waiting for questions
// the skipper is asking in the same conversation to finish first, until
// deadline has passed.
func acquireAssistantRunForWatch(id string, deadline time.Duration) (context.Context, *assistantRun, bool) {
	timeout := time.NewTimer(deadline)
	defer timeout.Stop()
	for {
		if runCtx, run, ok := globalAssistantRuns.start(id); ok {
			return runCtx, run, true
		}
		busy, exists := globalAssistantRuns.get(id)
		if !exists {
			continue
		}
		select {
		case <-busy.released:
		case <-timeout.C:
			return nil, nil, false
		}
	}
}

// ── the tool ────────────────────────────────────────────────────────────

func assistantStartWatchToolDefinition() openRouterTool {
	return openRouterTool{
		Type: "function",
		Function: openRouterFunctionDef{
			Name: "start_watch",
			Description: "Start a background watch on 1 to 6 live SignalK paths for 1 to 30 minutes, sampled once a " +
				"second from the live feed. Use it when the skipper asks you to watch or monitor something over " +
				"time, such as intermittent spikes, dropouts or one engine against the other. It returns at once " +
				"with the end time; tell the skipper when you will report back. When the watch ends, its report " +
				"arrives in this conversation by itself and you will be asked to explain it: per path count, " +
				"min/mean/max, stdev, first/last, gaps, excursions from a rolling median and the sources seen, and " +
				"for exactly two paths the same for their difference. Every path must be live now or the call fails naming it; " +
				"find exact paths with check_signalk_paths first. One watch per conversation. A Helmcentral " +
				"restart cancels it, and the skipper can stop it from the chat.",
			Parameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"paths": {
						"type": "array",
						"minItems": 1,
						"maxItems": 6,
						"items": {
							"type": "object",
							"properties": {
								"path": {"type": "string", "description": "The exact SignalK path, e.g. \"propulsion.port.engineLoad\"."},
								"label": {"type": "string", "description": "Short name the skipper would use, at most 40 characters, e.g. \"Port engine load\". Shown in the chat while the watch runs."}
							},
							"required": ["path"]
						}
					},
					"minutes": {"type": "integer", "minimum": 1, "maximum": 30, "description": "How long to watch, whole minutes from 1 to 30."},
					"reason": {"type": "string", "description": "In a few words, what you are looking for, e.g. \"port load spikes vs starboard at matched rpm\". Carried into the report so you can read it against this."}
				},
				"required": ["paths", "minutes", "reason"]
			}`),
		},
	}
}

type assistantStartWatchResultPath struct {
	Path     string  `json:"path"`
	Label    string  `json:"label"`
	ValueNow float64 `json:"value_now"`
	Units    string  `json:"units,omitempty"`
}

type assistantStartWatchResult struct {
	WatchID   string                          `json:"watch_id"`
	Minutes   int                             `json:"minutes"`
	EndsAt    string                          `json:"ends_at"`
	EndsAtISO string                          `json:"ends_at_iso"`
	Timezone  string                          `json:"timezone"`
	Paths     []assistantStartWatchResultPath `json:"paths"`
	Note      string                          `json:"note"`
}

type assistantConversationIDKey struct{}

// withAssistantConversationID marks a run's context with the conversation it
// answers, so a tool with a per-conversation effect (start_watch) knows where
// it belongs without widening every runner constructor.
func withAssistantConversationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, assistantConversationIDKey{}, id)
}

func assistantConversationIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(assistantConversationIDKey{}).(string)
	return id
}

func (d assistantToolDeps) executeStartWatch(ctx context.Context, raw json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	conversationID := assistantConversationIDFrom(ctx)
	if conversationID == "" {
		return "", fmt.Errorf("start_watch: no conversation to report back to")
	}
	if d.watches == nil {
		return "", fmt.Errorf("start_watch: watches are not available")
	}
	args, err := parseAssistantStartWatchArgs(raw)
	if err != nil {
		return "", err
	}

	loc, tzLabel := time.UTC, "UTC"
	if d.vesselState != nil {
		if state, verr := d.vesselState(); verr == nil && hasUsableVesselPosition(state.Latitude, state.Longitude) {
			loc = vesselLocalLocation(state.Longitude)
			tzLabel = assistantTimeZoneLabel(state.Longitude)
		}
	}

	info, err := d.watches.start(assistantWatchRequest{
		ConversationID: conversationID,
		Args:           args,
		Today:          d.today,
		Location:       loc,
		TimezoneLabel:  tzLabel,
	})
	if err != nil {
		return "", err
	}

	result := assistantStartWatchResult{
		WatchID:   info.ID,
		Minutes:   info.Minutes,
		EndsAt:    info.EndsAt.In(loc).Format("15:04"),
		EndsAtISO: info.EndsAt.UTC().Format(time.RFC3339),
		Timezone:  tzLabel,
		Note: "The watch is running. Tell the skipper you will report back at ends_at. Its report arrives in this " +
			"conversation by itself when it ends. A Helmcentral restart cancels it, and the skipper can stop it " +
			"from the chat.",
	}
	now := info.StartedAt
	for _, p := range info.Paths {
		r := d.watches.deps.read(p.Path, now)
		result.Paths = append(result.Paths, assistantStartWatchResultPath{Path: p.Path, Label: p.Label, ValueNow: roundTo4(r.Value), Units: r.Units})
	}
	return capToolResultJSON(&result, nil)
}

// ── HTTP ────────────────────────────────────────────────────────────────

// GET /api/assistant/conversations/:id/watch: the running watch for the
// chat's chip, or 204 when there is none.
func getAssistantWatchHandler(c echo.Context) error {
	info, ok := globalAssistantWatches.get(c.Param("id"))
	if !ok {
		return c.NoContent(http.StatusNoContent)
	}
	return c.JSON(http.StatusOK, map[string]any{"watch": info})
}

// DELETE /api/assistant/conversations/:id/watch: the chip's Stop. 204 when
// it stopped a watch or there was none, so a double tap is harmless; 409
// when the watch has already ended, so the chat keeps following it to
// Mate's answer rather than clearing the chip as if it had been stopped.
func deleteAssistantWatchHandler(c echo.Context) error {
	if _, err := globalAssistantWatches.stop(c.Param("id")); errors.Is(err, errAssistantWatchReporting) {
		return c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
	}
	return c.NoContent(http.StatusNoContent)
}
