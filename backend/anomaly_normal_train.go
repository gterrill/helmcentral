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
	Alarms   func(from, to time.Time) ([]alarmLogEntry, error)
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
	now := map[string]bool{}
	for _, e := range vessel.Engines {
		if e.Instance != "" {
			now[e.Instance] = true
		}
	}
	changed := len(now) != len(learned)
	for id := range now {
		if _, ok := learned[id]; !ok {
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
	var ignored map[string]bool
	if deps.Ignored != nil {
		ignored = deps.Ignored()
	}

	model := normalSubsystemModel{
		TrainedAt: stamp, Window: window, Source: src,
		Subjects: map[string]normalSubjectModel{}, Regimes: map[string]normalRegime{}, Excluded: []normalExcluded{},
	}
	for _, subject := range subjects {
		if err := ctx.Err(); err != nil {
			return normalSubsystemModel{}, err
		}
		if err := trainSubject(ctx, sub, subject, deps, trainContext{stamp: stamp, window: window, src: src, alarms: alarms, ignored: ignored, prev: prev}, &model); err != nil {
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
	ignored map[string]bool
	prev    normalSubsystemModel
}

type bandMinutes struct {
	label    string
	idx      []int // indexes into the joined minutes, usable ones
	excluded int   // gated minutes in the band that an alarm took out
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
	excludedAt := make([]bool, len(minutes))
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
		model.Excluded = append(model.Excluded, normalExcluded{Subject: subject.ID, From: from, To: to, Reason: "alarm", Ref: a.ID, Title: a.Label})
		lo := sort.Search(len(minutes), func(i int) bool { return !minutes[i].At.Before(from) })
		for i := lo; i < len(minutes) && !minutes[i].At.After(to); i++ {
			excludedAt[i] = true
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

	sm := normalSubjectModel{Name: subject.Name, GatedMinutes: len(usable)}
	haveVerdicts := len(usable) > 0
	var used []string // keys in subject order
	switch {
	case gateCount == 0:
		sm.Problem = fmt.Sprintf("no steady running minutes in the last %d days", int(normalTrainWindow/(24*time.Hour)))
	case len(usable) == 0:
		sm.Problem = fmt.Sprintf("all %s steady running minutes were within an alarm", commaInt(gateCount))
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
			model.Regimes[key] = fitBand(subject.ID, key, b, minutes, runIDs, used, byKey, minSD, tc)
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
		if usableCarry(r, tc.stamp, used, haveVerdicts) {
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
func usableCarry(r normalRegime, now time.Time, used []string, usedKnown bool) bool {
	if !r.Judged || now.Sub(r.LearnedAt) > normalRegimeExpiry {
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
func fitBand(subjectID, key string, b *bandMinutes, minutes []normalMinute, runIDs []int, used []string, verdicts map[string]normalFeatureVerdict, minSD map[string]float64, tc trainContext) normalRegime {
	collect := func(keys []string) (rows [][]float64, runs int) {
		seenRun := map[int]bool{}
		for _, i := range b.idx {
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
				seenRun[runIDs[i]] = true
			}
		}
		return rows, len(seenRun)
	}
	enough := func(rows [][]float64, runs int) bool { return len(rows) >= normalMinMinutes && runs >= normalMinRuns }

	base := normalRegime{Subject: subjectID, Label: b.label, LearnedAt: tc.stamp, Window: tc.window, ExcludedMinutes: b.excluded}
	prevR, hadPrev := tc.prev.Regimes[key]
	carry := hadPrev && usableCarry(prevR, tc.stamp, used, true)

	var late []string
	for _, k := range used {
		if verdicts[k].LateJoiner {
			late = append(late, k)
		}
	}
	fullRows, fullRuns := collect(used)
	keys, rows, runs := used, fullRows, fullRuns
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
		rows, runs = collect(keys)
	}
	if !enough(rows, runs) {
		if carry {
			return carried(prevR)
		}
		base.Minutes, base.Runs = len(rows), runs
		base.Reason = thinBandReason(len(rows), runs)
		return base
	}

	opts := normalFitOptions{Ridge: normalRidge, Reweight: normalReweight, MinSD: make([]float64, len(keys))}
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
