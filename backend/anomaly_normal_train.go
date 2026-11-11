package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"slices"
	"sort"
	"time"
)

// Training the learned normal (ADR 0276). Every run starts blank: it reads the
// stored minutes of the last normalTrainWindow, takes out the minutes that
// overlap an alarm for the same subject, and fits each regime with enough
// history. A band with no new data keeps its last normal, dated, until it
// expires. A failed run leaves the saved model alone and records why.

// normalTrainDeps is everything a training run reads, so a test can supply it.
type normalTrainDeps struct {
	Now    func() time.Time
	Source func() (normalSource, error)
	// Minutes reads the one-minute means of several readings over [start, stop),
	// by path, in one query per chunk.
	Minutes func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error)
	Vessel  func() (vesselView, error)
	// Ignored is the operator's ignored-sensor list. A reading on it is off.
	Ignored func() map[string]bool
	// Alarms lists the occurrences open at some moment of [from, to]. A log that
	// is not open is an error: training without it would learn faults as normal.
	Alarms func(from, to time.Time) ([]alarmLogEntry, error)
	// Answers are the operator's answers about unusual stretches. A store that
	// cannot be read is an error: training without it would forget what was said.
	Answers func() ([]normalAnswer, error)
	// Repairs are the maintenance log's repairs that say when the fault started
	// showing, each a fault range for the item it is on. A log that is not open
	// is an error, as the alarm log is.
	Repairs func() ([]maintenanceRepair, error)
	// BoatLoc is the boat's clock, which a repair's dates are days of. An unknown
	// or unreadable zone is an error, asked for only when a repair needs placing.
	BoatLoc  func() (*time.Location, error)
	Classify func(path string, v float64) rangeVerdict
}

func productionNormalTrainDeps(settingsPath string) normalTrainDeps {
	return normalTrainDeps{
		Now:     func() time.Time { return time.Now().UTC() },
		Source:  func() (normalSource, error) { return resolveNormalSource(settingsPath, false) },
		Minutes: func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
			return queryNormalMinutes(ctx, settingsPath, src, paths, start, stop)
		},
		Vessel:  func() (vesselView, error) { return loadVesselView(settingsPath) },
		Ignored: ignoredSensorSet,
		Alarms: func(from, to time.Time) ([]alarmLogEntry, error) {
			if globalAlarmLogStore == nil {
				return nil, errors.New("the alarm log is not open")
			}
			return globalAlarmLogStore.OverlappingBetween(from, to)
		},
		Answers: listNormalAnswers,
		Repairs: func() ([]maintenanceRepair, error) {
			if globalDocumentStore == nil {
				return nil, errors.New("the maintenance log is not open")
			}
			return globalDocumentStore.ListMaintenanceRepairs()
		},
		BoatLoc: func() (*time.Location, error) {
			bt := boatTimeLastFix(false)
			if bt.Err != nil {
				return nil, bt.Err
			}
			if bt.NoFix {
				return nil, errors.New("no position fix has been seen yet")
			}
			return bt.Loc, nil
		},
		Classify: classifyRange,
	}
}

// normalSubsystems are the subsystems this build trains.
func normalSubsystems() []normalSubsystem {
	return []normalSubsystem{normalEnginesSubsystem()}
}

// trainNormal runs every subsystem and saves the model. A run that fails, or
// that learns no band where the saved model has some, leaves the saved model as
// it was and records the attempt. A file that cannot be read is returned and
// left untouched: overwriting it would hide the corruption.
func trainNormal(ctx context.Context, path string, deps normalTrainDeps) error {
	normalModelMu.Lock()
	defer normalModelMu.Unlock()

	file, err := loadNormalModel(path)
	if err != nil {
		return err
	}
	now := deps.Now().UTC()
	// The source is resolved once for the whole run, so the settings cannot change
	// between the record and the read. An error here is returned by the run and
	// recorded.
	src, srcErr := deps.Source()
	deps.Source = func() (normalSource, error) { return src, srcErr }
	// The answers and the maintenance log are read once for the whole run too.
	answers, answersErr := deps.Answers()
	deps.Answers = func() ([]normalAnswer, error) { return answers, answersErr }
	repairs, repairsErr := deps.Repairs()
	deps.Repairs = func() ([]maintenanceRepair, error) { return repairs, repairsErr }
	record := func(runErr error) error {
		// A shutdown is not a failed attempt: the record stays as it was, so the
		// operator is not shown "context canceled" and the next start does not
		// retrain for it.
		if runErr != nil && ctx.Err() != nil {
			return runErr
		}
		file.LastAttempt = &normalAttempt{At: now, OK: runErr == nil, Source: src}
		if runErr != nil {
			file.LastAttempt.Problem = firstErrorLine(runErr)
		}
		if saveErr := saveNormalModel(path, file); saveErr != nil {
			log.Printf("anomaly normal: could not save %s: %v", path, saveErr)
			if runErr == nil {
				return saveErr
			}
		}
		return runErr
	}
	for _, sub := range normalSubsystems() {
		previous := file.Subsystems[sub.Name]
		model, err := trainSubsystem(ctx, sub, deps, previous)
		if err != nil {
			return record(err)
		}
		if model.judgedRegimes() == 0 && previous.judgedRegimes() > 0 {
			return record(fmt.Errorf("this run learned no band for the %s, so the model from %s is kept", sub.Name, previous.TrainedAt.Format(time.RFC3339)))
		}
		file.Subsystems[sub.Name] = model
	}
	return record(nil)
}

// normalTrainInterval is how often the trainer runs. It also runs when the
// inputs change (normalInputsChanged).
const normalTrainInterval = 24 * time.Hour

// normalWakeDebounce is how long the trainer waits for the inputs to go quiet
// before it runs, so a burst of switch toggles is one run.
var normalWakeDebounce = 30 * time.Second

// normalTrainWake asks the trainer to run soon. Buffered so a settings save
// never waits for it.
var normalTrainWake = make(chan struct{}, 1)

func requestNormalTraining() {
	select {
	case normalTrainWake <- struct{}{}:
	default:
	}
}

// normalInputsChanged is the one hook for anything that changes what the
// learned normal is trained on: the InfluxDB settings or token, the engines
// that are set up, an engine reading switched on or off.
func normalInputsChanged() { requestNormalTraining() }

// wakeNormalIfEnginesChanged wakes the trainer when the engines now set up are
// not the ones the saved model was learned from: one added, removed or given a
// new instance. It is asked after an equipment item is saved or deleted. A model
// file that cannot be read is left for the trainer to report.
func wakeNormalIfEnginesChanged(vessel vesselView) {
	file, err := loadNormalModel(anomalyNormalPath())
	if err != nil {
		log.Printf("anomaly normal: not checking the engines against the saved model: %v", err)
		return
	}
	learned := file.Subsystems[normalSubsystemEngines].Subjects
	now := map[string]string{} // instance to its equipment item
	for _, e := range vessel.Engines {
		if e.Instance != "" {
			now[e.Instance] = e.EquipmentID
		}
	}
	changed := len(now) != len(learned)
	for id, equipment := range now {
		// A different item moves where the engine's repairs are read from.
		if sm, ok := learned[id]; !ok || sm.EquipmentID != equipment {
			changed = true
		}
	}
	if changed {
		normalInputsChanged()
	}
}

// drainWake empties the wake channel and says whether a wake was waiting.
func drainWake(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// normalPathMatters says whether a change to the ignored state of a SignalK
// path changes what is learned, asking each subsystem.
func normalPathMatters(path string) bool {
	for _, sub := range normalSubsystems() {
		if sub.PathMatters != nil && sub.PathMatters(path) {
			return true
		}
	}
	return false
}

// waitQuiet returns true once no wake has arrived for quiet, and false if ctx
// ends first. Each wake restarts the wait.
func waitQuiet(ctx context.Context, wake <-chan struct{}, quiet time.Duration) bool {
	t := time.NewTimer(quiet)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-wake:
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(quiet)
		case <-t.C:
			return true
		}
	}
}

// normalStartDelay is how long a starting trainer waits before its first run:
// the rest of the interval when the last run succeeded within it from the same
// source, and no wait otherwise. A restart does not read ninety days again.
func normalStartDelay(file normalModelFile, now time.Time, src normalSource, interval time.Duration) time.Duration {
	a := file.LastAttempt
	if a == nil || !a.OK || a.Source != src {
		return 0
	}
	if age := now.Sub(a.At); age >= 0 && age < interval {
		return interval - age
	}
	return 0
}

func startNormalTrainer(ctx context.Context, settingsPath string, interval time.Duration) {
	deps := productionNormalTrainDeps(settingsPath)
	run := func() {
		if err := trainNormal(ctx, anomalyNormalPath(), deps); err != nil && ctx.Err() == nil {
			log.Printf("anomaly normal: training failed, the saved model is kept: %v", err)
		}
	}
	// A short wait lets the stores and InfluxDB come up first.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	var delay time.Duration
	if file, err := loadNormalModel(anomalyNormalPath()); err == nil {
		if src, err := deps.Source(); err == nil {
			delay = normalStartDelay(file, deps.Now(), src, interval)
		}
	}
	// A change that came in during the wait is taken by the first run, so it
	// does not queue a second full training straight after it.
	if drainWake(normalTrainWake) {
		delay = 0
	}
	next := time.NewTimer(delay)
	defer next.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-next.C:
			run()
			next.Reset(interval)
		case <-normalTrainWake:
			if !waitQuiet(ctx, normalTrainWake, normalWakeDebounce) {
				return
			}
			run()
			if !next.Stop() {
				select {
				case <-next.C:
				default:
				}
			}
			next.Reset(interval)
		}
	}
}

// --- one subsystem -----------------------------------------------------------

func trainSubsystem(ctx context.Context, sub normalSubsystem, deps normalTrainDeps, prev normalSubsystemModel) (normalSubsystemModel, error) {
	stamp := deps.Now().UTC()
	end := stamp.Truncate(time.Minute)
	window := normalWindow{From: end.Add(-normalTrainWindow), To: end}

	src, err := deps.Source()
	if err != nil {
		return normalSubsystemModel{}, err
	}
	vessel, err := deps.Vessel()
	if err != nil {
		return normalSubsystemModel{}, fmt.Errorf("vessel settings: %w", err)
	}
	subjects := sub.Subjects(vessel)
	if len(subjects) == 0 {
		return normalSubsystemModel{}, fmt.Errorf("no %s are set up", sub.Name)
	}
	alarms, err := deps.Alarms(window.From.Add(-normalAlarmPadAfter), window.To.Add(normalAlarmPadBefore))
	if err != nil {
		return normalSubsystemModel{}, fmt.Errorf("the alarm log could not be read: %w", err)
	}
	answers, err := deps.Answers()
	if err != nil {
		return normalSubsystemModel{}, fmt.Errorf("the answers about unusual stretches could not be read: %w", err)
	}
	logged, err := deps.Repairs()
	if err != nil {
		return normalSubsystemModel{}, fmt.Errorf("the maintenance log could not be read: %w", err)
	}
	// Only a repair on one of these subjects' items can matter, so only those ask
	// for the boat's zone.
	items := map[string]bool{}
	for _, sj := range subjects {
		if sj.EquipmentID != "" {
			items[sj.EquipmentID] = true
		}
	}
	// A repair that ended before the oldest window this run or an old normal was
	// learned from has nothing to leave out and nothing to block. The zone is two
	// days of margin wide (zones are at most 14 hours from UTC), so these are judged
	// by date alone and never need it.
	oldest := window.From
	for _, r := range prev.Regimes {
		if r.Window.From.Before(oldest) {
			oldest = r.Window.From
		}
	}
	cutoff := oldest.AddDate(0, 0, -2).Format("2006-01-02")
	logged = slices.DeleteFunc(slices.Clone(logged), func(r maintenanceRepair) bool {
		return !items[r.EquipmentID] || r.PerformedAt < cutoff
	})
	repairs, err := normalRepairRanges(logged, deps.BoatLoc)
	if err != nil {
		return normalSubsystemModel{}, err
	}
	var ignored map[string]bool
	if deps.Ignored != nil {
		ignored = deps.Ignored()
	}

	model := normalSubsystemModel{
		TrainedAt: stamp, Window: window, Source: src,
		Subjects: map[string]normalSubjectModel{}, Regimes: map[string]normalRegime{}, Excluded: []normalExcluded{}, Labelled: []normalExcluded{},
	}
	for _, subject := range subjects {
		if err := ctx.Err(); err != nil {
			return normalSubsystemModel{}, err
		}
		if err := trainSubject(ctx, sub, subject, deps, trainContext{stamp: stamp, window: window, src: src, alarms: alarms, answers: answers, repairs: repairs, ignored: ignored, prev: prev}, &model); err != nil {
			return normalSubsystemModel{}, err
		}
	}
	return model, nil
}

type trainContext struct {
	stamp   time.Time
	window  normalWindow
	src     normalSource
	alarms  []alarmLogEntry
	answers []normalAnswer
	repairs []normalRepairRange
	ignored map[string]bool
	prev    normalSubsystemModel
}

// normalRepairRange is the fault range of one repair: from the start of the day
// the fault first showed to the end of the day it was repaired, on the boat's
// clock. To is the instant after, so a minute that starts there is not in it.
type normalRepairRange struct {
	EntryID     string
	EquipmentID string
	Title       string
	From, To    time.Time
}

// normalRepairZoneProblem is the reason a run cannot place a repair's dates.
const normalRepairZoneProblem = "The boat's time zone is unknown, so repair dates could not be placed."

// normalRepairRanges are the fault ranges the maintenance log's repairs give. A
// date that does not parse stops the run and names the entry.
//
// A date is a day on the boat's clock, read in the zone the boat is in at
// training (an IANA zone, so summer time on that date is right). Helmcentral keeps
// no history of the zones the boat has been in, so a repair dated before the boat
// crossed into another zone is read in the new one, up to a few hours out.
func normalRepairRanges(repairs []maintenanceRepair, boatLoc func() (*time.Location, error)) ([]normalRepairRange, error) {
	var out []normalRepairRange
	var loc *time.Location
	for _, e := range repairs {
		if loc == nil {
			var err error
			if loc, err = boatLoc(); err != nil {
				log.Printf("anomaly normal: could not place repair dates: %v", err)
				return nil, errors.New(normalRepairZoneProblem)
			}
		}
		from, err := time.ParseInLocation("2006-01-02", e.StartedAt, loc)
		if err != nil {
			return nil, fmt.Errorf("the maintenance log entry %s has a start date that is not a date: %w", e.ID, err)
		}
		last, err := time.ParseInLocation("2006-01-02", e.PerformedAt, loc)
		if err != nil {
			return nil, fmt.Errorf("the maintenance log entry %s has a date that is not a date: %w", e.ID, err)
		}
		out = append(out, normalRepairRange{EntryID: e.ID, EquipmentID: e.EquipmentID, Title: e.Description, From: from, To: last.AddDate(0, 0, 1)})
	}
	return out, nil
}

type bandMinutes struct {
	label string
	idx   []int // indexes into the joined minutes, usable ones
	// keep is parallel to idx: the operator said the minute is normal.
	keep     []bool
	excluded int // gated minutes in the band that an alarm, an answer or a repair took out
}

func trainSubject(ctx context.Context, sub normalSubsystem, subject normalSubject, deps normalTrainDeps, tc trainContext, model *normalSubsystemModel) error {
	// 1. Every reading's minutes, from the one source that carries it, in one
	// query. A reading switched off is not read, unless the gate needs it.
	var paths []string
	for _, f := range subject.Features {
		if f.Required || !tc.ignored[f.Path] {
			paths = append(paths, f.Path)
		}
	}
	rows, err := deps.Minutes(ctx, tc.src, paths, tc.window.From, tc.window.To)
	if err != nil {
		return err
	}
	evidence := make([]featureEvidence, len(subject.Features))
	values := map[string]map[time.Time]float64{}
	for i, f := range subject.Features {
		pick := chooseSender(rows[f.Path])
		evidence[i] = featureEvidence{Feature: f, Source: pick.Source, TwoSources: pick.Two, SenderChanged: pick.Changed, Values: pick.Values}
		values[f.Key] = pick.Values
	}

	// 2. Join: a minute is a minute of the subject when it has every reading the
	// gate needs. A minute that lacks one is not imputed and is not a minute.
	minutes := joinNormalMinutes(subject.Features, values, func(string) bool { return true })

	// 3. Gate, then take out the minutes of an alarm for this subject.
	gate := sub.Gate(minutes)
	gateCount := 0
	for _, g := range gate {
		if g {
			gateCount++
		}
	}
	// An alarm takes minutes out only when it is about a reading the model uses:
	// one of the used readings or one the gate needs. Which those are is decided
	// from the data before any alarm is taken out.
	relevant := map[string]bool{}
	if sub.AlarmReading != nil {
		var gatedAt, activeAt []time.Time
		for i, g := range gate {
			if g {
				gatedAt = append(gatedAt, minutes[i].At)
			}
			if sub.Active(minutes[i]) {
				activeAt = append(activeAt, minutes[i].At)
			}
		}
		for _, v := range selectFeatures(evidence, gatedAt, activeAt, tc.ignored, deps.Classify) {
			if v.Used || v.Required {
				relevant[v.Key] = true
			}
		}
	}
	// Everything that takes this subject's minutes out of training, as ranges that
	// include their start and not their end: an alarm, a stretch the operator
	// answered real (a fault), and a repair's fault range from the maintenance log.
	var out []normalExcluded
	inWindow := func(from, to time.Time) bool { return from.Before(tc.window.To) && to.After(tc.window.From) }
	for _, a := range tc.alarms {
		owner, ok := sub.AlarmOwns(a.Path)
		if !ok || owner != subject.ID {
			continue
		}
		if sub.AlarmReading != nil {
			if key, isReading := sub.AlarmReading(a.Path); isReading && !relevant[key] {
				continue
			}
		}
		from := a.RaisedAt.UTC().Add(-normalAlarmPadBefore)
		to := tc.window.To
		if a.ClearedAt != nil {
			to = a.ClearedAt.UTC().Add(normalAlarmPadAfter)
		} else if capped := a.RaisedAt.UTC().Add(normalOpenAlarmSpan); capped.Before(to) {
			// The alarm log keeps no last-seen time for an alarm that never cleared,
			// so one forgotten open would take out everything after it for good. Its
			// exclusion ends a day after it was raised.
			to = capped
		}
		// The padded end is the last minute taken out, so the range ends a minute on.
		out = append(out, normalExcluded{Subject: subject.ID, From: from, To: to.Truncate(time.Minute).Add(time.Minute), Reason: normalReasonAlarm, Ref: a.ID, Title: a.Label})
	}
	// faults are every real answer and repair for this subject, however old: they
	// decide whether an old normal may be reused (usableCarry), so they are not cut
	// to the current window. Those inside it are also left out of this run.
	var faults, labelled []normalExcluded
	for _, a := range tc.answers {
		if a.Subsystem != sub.Name || a.Subject != subject.ID {
			continue
		}
		switch a.Answer {
		case normalAnswerReal:
			faults = append(faults, normalExcluded{Subject: subject.ID, From: a.From, To: a.To, Reason: normalReasonAnsweredReal, Ref: a.StretchID})
		case normalAnswerNormal:
			if inWindow(a.From, a.To) {
				labelled = append(labelled, normalExcluded{Subject: subject.ID, From: a.From, To: a.To, Reason: normalReasonAnsweredNormal, Ref: a.StretchID})
			}
		}
	}
	for _, r := range tc.repairs {
		if subject.EquipmentID != "" && r.EquipmentID == subject.EquipmentID {
			faults = append(faults, normalExcluded{Subject: subject.ID, From: r.From, To: r.To, Reason: normalReasonRepair, Ref: r.EntryID, Title: r.Title})
		}
	}
	for _, f := range faults {
		if inWindow(f.From, f.To) {
			out = append(out, f)
		}
	}
	// span is the minutes [lo, hi) that fall in [from, to).
	span := func(from, to time.Time) (lo, hi int) {
		lo = sort.Search(len(minutes), func(i int) bool { return !minutes[i].At.Before(from) })
		hi = sort.Search(len(minutes), func(i int) bool { return !minutes[i].At.Before(to) })
		return lo, hi
	}
	excludedAt := make([]bool, len(minutes))
	for _, e := range out {
		model.Excluded = append(model.Excluded, e)
		lo, hi := span(e.From, e.To)
		for i := lo; i < hi; i++ {
			excludedAt[i] = true
		}
	}
	// A minute the operator called normal is kept through reweighting, unless one
	// of the above already took it out.
	keptAt := make([]bool, len(minutes))
	for _, n := range labelled {
		model.Labelled = append(model.Labelled, n)
		lo, hi := span(n.From, n.To)
		for i := lo; i < hi; i++ {
			keptAt[i] = !excludedAt[i]
		}
	}
	var usable []int
	var usableTimes []time.Time
	for i, g := range gate {
		if g && !excludedAt[i] {
			usable = append(usable, i)
			usableTimes = append(usableTimes, minutes[i].At)
		}
	}

	sm := normalSubjectModel{Name: subject.Name, EquipmentID: subject.EquipmentID, GatedMinutes: len(usable)}
	haveVerdicts := len(usable) > 0
	var used []string // keys in subject order
	switch {
	case gateCount == 0:
		sm.Problem = fmt.Sprintf("no steady running minutes in the last %d days", int(normalTrainWindow/(24*time.Hour)))
	case len(usable) == 0:
		sm.Problem = fmt.Sprintf("all %s steady running minutes were left out (alarms, answers marked real or repairs)", commaInt(gateCount))
	}
	var runningTimes []time.Time
	for i, m := range minutes {
		if !excludedAt[i] && sub.Active(m) {
			runningTimes = append(runningTimes, m.At)
		}
	}
	sm.Features = selectFeatures(evidence, usableTimes, runningTimes, tc.ignored, deps.Classify)
	byKey := map[string]normalFeatureVerdict{}
	for _, v := range sm.Features {
		byKey[v.Key] = v
		if v.Used {
			used = append(used, v.Key)
		}
	}
	if haveVerdicts {
		for _, v := range sm.Features {
			if v.Required && !v.Used {
				// A reading the gate needs that the operator ignored may be a dead
				// sender, and gating on it would tell nothing. The engine is not
				// judged, and the problem names the reading so it can be switched
				// back on.
				if v.SwitchedOff {
					sm.Problem = normalSwitchedOffProblem(v.Key, v.Path)
				} else {
					sm.Problem = fmt.Sprintf("%s: %s", normalFeatureName(v), v.Reason)
				}
				break
			}
		}
	}
	model.Subjects[subject.ID] = sm

	// 4. Fit what can be fitted.
	if haveVerdicts && sm.Problem == "" {
		runIDs := sub.Run(minutes)
		bands := map[string]*bandMinutes{}
		bandOf := func(i int) (*bandMinutes, bool) {
			key, label, ok := sub.Regime(subject, minutes[i])
			if !ok {
				return nil, false
			}
			b := bands[key]
			if b == nil {
				b = &bandMinutes{label: label}
				bands[key] = b
			}
			return b, true
		}
		for _, i := range usable {
			if b, ok := bandOf(i); ok {
				b.idx = append(b.idx, i)
				b.keep = append(b.keep, keptAt[i])
			}
		}
		for i, g := range gate {
			if g && excludedAt[i] {
				if b, ok := bandOf(i); ok {
					b.excluded++
				}
			}
		}
		minChange := map[string]float64{}
		for _, f := range subject.Features {
			minChange[f.Key] = f.MinChange
		}
		minSD := map[string]float64{}
		for _, key := range used {
			var vals []float64
			for _, i := range usable {
				if v, ok := minutes[i].Values[key]; ok {
					vals = append(vals, v)
				}
			}
			minSD[key] = normalMinSD(minChange[key], smallestStep(vals))
		}
		for key, b := range bands {
			model.Regimes[key] = fitBand(subject.ID, key, b, minutes, runIDs, used, byKey, minSD, faults, tc)
		}
	}

	// 5. Bands this run had no data for keep their last normal while it is fresh.
	for key, r := range tc.prev.Regimes {
		if r.Subject != subject.ID {
			continue
		}
		if _, done := model.Regimes[key]; done {
			continue
		}
		if usableCarry(r, tc.stamp, used, haveVerdicts, faults) {
			model.Regimes[key] = carried(r)
		}
	}
	return nil
}

// carried is a previous regime kept for another run: it says so and drops its
// shift, which belongs to a refit.
func carried(r normalRegime) normalRegime {
	r.CarriedForward = true
	r.Shift = nil
	return r
}

// usableCarry says a previous regime may be carried forward: it was judged, has
// not expired, and every reading it describes is still in use. When this run
// had no steady minutes at all, what is in use is not known and the regime is
// carried until it expires.
//
// It is never reused if a real answer or a repair for the subject (faults)
// overlaps the window it was learned from, however old that is: the fault was in
// it. An alarm never stops it, since the fit left the alarm out already. This is
// the one rule on every path that reuses an old regime.
func usableCarry(r normalRegime, now time.Time, used []string, usedKnown bool, faults []normalExcluded) bool {
	if !r.Judged || now.Sub(r.LearnedAt) > normalRegimeExpiry {
		return false
	}
	if slices.ContainsFunc(faults, func(f normalExcluded) bool { return f.From.Before(r.Window.To) && f.To.After(r.Window.From) }) {
		return false
	}
	if !usedKnown {
		return true
	}
	for _, f := range r.Features {
		if !slices.Contains(used, f) {
			return false
		}
	}
	return true
}

// fitBand fits one regime of one subject, or says why not.
func fitBand(subjectID, key string, b *bandMinutes, minutes []normalMinute, runIDs []int, used []string, verdicts map[string]normalFeatureVerdict, minSD map[string]float64, faults []normalExcluded, tc trainContext) normalRegime {
	collect := func(keys []string) (rows [][]float64, keep []bool, runs int) {
		seenRun := map[int]bool{}
		for n, i := range b.idx {
			row := make([]float64, len(keys))
			complete := true
			for j, k := range keys {
				v, ok := minutes[i].Values[k]
				if !ok {
					complete = false
					break
				}
				row[j] = v
			}
			if complete {
				rows = append(rows, row)
				keep = append(keep, b.keep[n])
				seenRun[runIDs[i]] = true
			}
		}
		return rows, keep, len(seenRun)
	}
	enough := func(rows [][]float64, runs int) bool { return len(rows) >= normalMinMinutes && runs >= normalMinRuns }

	base := normalRegime{Subject: subjectID, Label: b.label, LearnedAt: tc.stamp, Window: tc.window, ExcludedMinutes: b.excluded}
	prevR, hadPrev := tc.prev.Regimes[key]
	carry := hadPrev && usableCarry(prevR, tc.stamp, used, true, faults)

	var late []string
	for _, k := range used {
		if verdicts[k].LateJoiner {
			late = append(late, k)
		}
	}
	fullRows, fullKeep, fullRuns := collect(used)
	keys, rows, keep, runs := used, fullRows, fullKeep, fullRuns
	var joining []string
	joiningMinutes := 0
	if !enough(fullRows, fullRuns) && len(late) > 0 {
		joining, joiningMinutes = late, len(fullRows)
		if carry {
			c := carried(prevR)
			c.Joining, c.JoiningMinutes = joining, joiningMinutes
			return c
		}
		keys = slices.DeleteFunc(slices.Clone(used), func(k string) bool { return slices.Contains(late, k) })
		rows, keep, runs = collect(keys)
	}
	if !enough(rows, runs) {
		if carry {
			return carried(prevR)
		}
		base.Minutes, base.Runs = len(rows), runs
		base.Reason = thinBandReason(len(rows), runs)
		return base
	}

	opts := normalFitOptions{Ridge: normalRidge, Reweight: normalReweight, MinSD: make([]float64, len(keys)), Keep: keep}
	for j, k := range keys {
		opts.MinSD[j] = minSD[k]
	}
	stats, err := fitRegime(rows, opts)
	if err != nil {
		base.Minutes, base.Runs = len(rows), runs
		base.Reason = "could not be fitted: " + err.Error()
		return base
	}
	base.Judged, base.Minutes, base.Runs = true, len(rows), runs
	base.Features, base.normalRegimeStats = keys, stats
	base.Joining, base.JoiningMinutes = joining, joiningMinutes
	if hadPrev && prevR.Judged && slices.Equal(prevR.Features, keys) {
		if d, ok := regimeShift(prevR.normalRegimeStats, stats.Mean); ok {
			base.Shift = &normalShift{FromLearnedAt: prevR.LearnedAt, MeanDistance: math.Round(d*100) / 100}
		}
	}
	return base
}

func thinBandReason(minutes, runs int) string {
	if minutes < normalMinMinutes {
		return fmt.Sprintf("%s minutes learned, needs %d", commaInt(minutes), normalMinMinutes)
	}
	return fmt.Sprintf("%s minutes from %s, needs %d runs", commaInt(minutes), plural(runs, "run", "runs"), normalMinRuns)
}

// normalSwitchedOffProblem is why an engine cannot be judged when a reading its
// gate needs is on the ignored list. Training and scoring say it the same way.
func normalSwitchedOffProblem(key, path string) string {
	return fmt.Sprintf("%s is switched off, so this engine cannot be judged", normalFeatureName(normalFeatureVerdict{Key: key, Path: path}))
}

// normalFeatureName is a reading's plain name for the operator, "oil pressure".
func normalFeatureName(v normalFeatureVerdict) string {
	if name := sensorQuantity(v.Path); name != "" {
		return name
	}
	return v.Key
}
