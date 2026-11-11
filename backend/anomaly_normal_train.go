package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"math"
	"slices"
	"sort"
	"strings"
	"sync"
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
		Now:    func() time.Time { return time.Now().UTC() },
		Source: func() (normalSource, error) { return resolveNormalSource(settingsPath, false) },
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
	return []normalSubsystem{normalEnginesSubsystem(), normalElectricalSubsystem()}
}

// normalSubsystemNamed is the subsystem a saved name stands for. A name this
// build does not train, such as one from a newer build, is not one.
func normalSubsystemNamed(name string) (normalSubsystem, bool) {
	for _, sub := range normalSubsystems() {
		if sub.Name == name {
			return sub, true
		}
	}
	return normalSubsystem{}, false
}

// trainNormal runs every subsystem and saves one record for each: trained with
// its model, skipped with its reason, or failed with its problem. A subsystem that
// fails, or that learns no band where its saved model has some, keeps that model,
// so one subsystem's failure is never another's. The run is a failure when any of
// them failed. A file that cannot be read is returned and left untouched:
// overwriting it would hide the corruption.
func trainNormal(ctx context.Context, path string, deps normalTrainDeps) error {
	normalModelMu.Lock()
	defer normalModelMu.Unlock()

	file, err := loadNormalModel(path)
	if err != nil {
		return err
	}
	now := deps.Now().UTC()
	// Every subsystem trains on the run's own clock, so they share one window.
	deps.Now = func() time.Time { return now }
	// The source is resolved once for the whole run, so the settings cannot change
	// between the record and the read. An error here fails every subsystem.
	src, srcErr := deps.Source()
	deps.Source = func() (normalSource, error) { return src, srcErr }
	// The answers and the maintenance log are read once for the whole run too, and
	// so are the vessel, the ignored list and the alarms over a window, which the
	// subsystems share.
	answers, answersErr := deps.Answers()
	deps.Answers = func() ([]normalAnswer, error) { return answers, answersErr }
	repairs, repairsErr := deps.Repairs()
	deps.Repairs = func() ([]maintenanceRepair, error) { return repairs, repairsErr }
	deps.Vessel = sync.OnceValues(deps.Vessel)
	if deps.Ignored != nil {
		deps.Ignored = sync.OnceValue(deps.Ignored)
	}
	alarmsFor := deps.Alarms
	type window struct{ from, to time.Time }
	type alarmRead struct {
		entries []alarmLogEntry
		err     error
	}
	alarmReads := map[window]alarmRead{}
	deps.Alarms = func(from, to time.Time) ([]alarmLogEntry, error) {
		if r, ok := alarmReads[window{from, to}]; ok {
			return r.entries, r.err
		}
		entries, err := alarmsFor(from, to)
		alarmReads[window{from, to}] = alarmRead{entries, err}
		return entries, err
	}
	vessel, vesselErr := deps.Vessel()
	if vesselErr == nil {
		deps.Minutes = shareMinutes(deps.Minutes, sharedNormalPaths(vessel))
	}
	record := func(runErr error) error {
		// A shutdown is not a failed attempt: the records stay as they were, so the
		// operator is not shown "context canceled" and the next start does not
		// retrain for it.
		if runErr != nil && ctx.Err() != nil {
			return runErr
		}
		if saveErr := saveNormalModel(path, file); saveErr != nil {
			log.Printf("anomaly normal: could not save %s: %v", path, saveErr)
			if runErr == nil {
				return saveErr
			}
		}
		return runErr
	}
	// failedRecord is the earlier record with this attempt's failure on it.
	failedRecord := func(previous normalSubsystemModel, problem string, inputs []normalSubjectInputs) normalSubsystemModel {
		previous.State, previous.AttemptedAt, previous.Problem, previous.Inputs = normalStateFailed, now, problem, inputs
		return previous
	}
	// A subsystem that fails keeps its own saved model and the others are still
	// trained: a house bank that cannot be read must not cost the engines their
	// run. The first failure is the run's.
	var failed error
	var skipped []string
	for _, sub := range normalSubsystems() {
		previous := file.Subsystems[sub.Name]
		// A vessel that cannot be read leaves the inputs as they were, so the next
		// equipment save does not see a change nobody made.
		inputs := previous.Inputs
		if vesselErr == nil {
			inputs = normalInputsOf(sub.Subjects(vessel))
		}
		model, err := trainSubsystem(ctx, sub, deps, previous)
		switch {
		case errors.Is(err, errNormalNoSubjects):
			// Nothing of this kind is set up. The earlier model stays in the record, so a
			// brief glitch in a role or an instance does not lose the history, but it is
			// not used while it is not the model of what is set up (usableFor).
			kept := previous
			kept.State, kept.AttemptedAt, kept.Problem, kept.Inputs = normalStateSkipped, now, sub.NoSubjects, nil
			file.Subsystems[sub.Name] = kept
			skipped = append(skipped, sub.NoSubjects)
		case err != nil:
			file.Subsystems[sub.Name] = failedRecord(previous, firstErrorLine(err), inputs)
			if failed == nil {
				failed = err
			}
		case model.judgedRegimes() == 0 && previous.judgedRegimes() > 0:
			noBand := fmt.Errorf("this run learned no band for the %s, so the model from %s is kept", sub.Name, previous.TrainedAt.Format(time.RFC3339))
			file.Subsystems[sub.Name] = failedRecord(previous, noBand.Error(), inputs)
			if failed == nil {
				failed = noBand
			}
		default:
			model.State, model.AttemptedAt, model.Inputs = normalStateTrained, now, inputs
			file.Subsystems[sub.Name] = model
		}
	}
	switch {
	case failed != nil:
		return record(failed)
	case len(skipped) == len(normalSubsystems()):
		// A boat with nothing set up has nothing to learn, and says so.
		return record(errors.New(lowerFirst(skipped[0])))
	}
	return record(nil)
}

// sharedNormalPaths are the readings more than one subsystem is offered, such as an
// engine's revolutions, which the engines are judged by and the house bank is
// placed by.
func sharedNormalPaths(vessel vesselView) map[string]bool {
	count := map[string]int{}
	for _, sub := range normalSubsystems() {
		seen := map[string]bool{}
		for _, s := range sub.Subjects(vessel) {
			for _, f := range s.Features {
				seen[f.Path] = true
			}
		}
		for path := range seen {
			count[path]++
		}
	}
	shared := map[string]bool{}
	for path, n := range count {
		shared[path] = n > 1
	}
	return shared
}

// shareMinutes reads the shared paths once for a run and gives the rest of the
// run what it read. Only shared paths are kept, so a run does not hold ninety days
// of every reading. A read for one source and window is the same for all.
func shareMinutes(read func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error), shared map[string]bool) func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
	type key struct {
		src         normalSource
		start, stop time.Time
	}
	// A shared reading is read once, by whoever asks first. Whoever asks while it is
	// being read waits for that read alone, and reads that are not shared go on.
	type flight struct {
		done chan struct{}
		rows []normalMinuteRow
		err  error
	}
	var mu sync.Mutex
	kept := map[key]map[string]*flight{}
	return func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
		mu.Lock()
		flights := kept[key{src, start, stop}]
		if flights == nil {
			flights = map[string]*flight{}
			kept[key{src, start, stop}] = flights
		}
		var mine, rest []string
		waiting := map[string]*flight{}
		for _, p := range paths {
			switch f, started := flights[p]; {
			case !shared[p]:
				rest = append(rest, p)
			case started:
				waiting[p] = f
			default:
				flights[p] = &flight{done: make(chan struct{})}
				waiting[p] = flights[p]
				mine = append(mine, p)
			}
		}
		mu.Unlock()

		out := map[string][]normalMinuteRow{}
		// What is mine is read in a query of its own, so another subsystem waiting on it
		// is not failed by a reading only this one asked for.
		if len(mine) > 0 {
			got, err := read(ctx, src, mine, start, stop)
			for _, p := range mine {
				f := flights[p]
				f.rows, f.err = got[p], err
				close(f.done)
			}
			if err != nil {
				return nil, err
			}
		}
		if len(rest) > 0 {
			got, err := read(ctx, src, rest, start, stop)
			if err != nil {
				return nil, err
			}
			maps.Copy(out, got)
		}
		for p, f := range waiting {
			<-f.done
			if f.err != nil {
				return nil, f.err
			}
			out[p] = f.rows
		}
		return out, nil
	}
}

// errNormalNoSubjects is a subsystem with nothing of its kind set up. It is a
// reason to skip the subsystem, not a failed run.
var errNormalNoSubjects = errors.New("nothing of this kind is set up")

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
// learned normal is trained on: the InfluxDB settings or token, the engines and
// house bank that are set up, a reading switched on or off.
func normalInputsChanged() { requestNormalTraining() }

// wakeNormalIfInputsChanged wakes the trainer when the subjects of a subsystem are
// not what its last attempt was made from: an engine added, removed or given a new
// instance, or the house bank, a charging item or a solar controller replaced. Each
// subsystem is compared with its own last attempt, trained, skipped or failed, so
// one that keeps failing is not trained again for every unrelated equipment save.
// It is asked after an equipment item is saved or deleted. A model file that cannot
// be read is left for the trainer to report.
func wakeNormalIfInputsChanged(vessel vesselView) {
	file, err := loadNormalModel(anomalyNormalPath())
	if err != nil {
		log.Printf("anomaly normal: not checking the vessel against the saved model: %v", err)
		return
	}
	for _, sub := range normalSubsystems() {
		if !sameNormalInputs(normalInputsOf(sub.Subjects(vessel)), file.Subsystems[sub.Name].Inputs) {
			normalInputsChanged()
			return
		}
	}
}

// sameNormalInputs says two attempts were made from the same subjects, items and
// readings.
func sameNormalInputs(a, b []normalSubjectInputs) bool {
	return slices.EqualFunc(a, b, func(x, y normalSubjectInputs) bool {
		return x.Subject == y.Subject && slices.Equal(x.EquipmentIDs, y.EquipmentIDs) && slices.Equal(x.Paths, y.Paths)
	})
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
func normalPathMatters(path string) bool { return normalPathsMatter(path) }

// normalPathsMatter is normalPathMatters for several paths, checking the boat once.
// A vessel that cannot be read is logged. An engine reading is recognised by its
// path alone, so it still counts; a path the bank might use wakes the trainer
// rather than hide the change.
func normalPathsMatter(paths ...string) bool {
	vessel, err := normalPathVessel()
	if err != nil {
		log.Printf("anomaly normal: could not read the vessel to check an ignored sensor: %v", err)
	}
	for _, path := range paths {
		if err != nil && strings.HasPrefix(path, "electrical.") {
			return true
		}
		for _, sub := range normalSubsystems() {
			if sub.PathMatters != nil && sub.PathMatters(vessel, path) {
				return true
			}
		}
	}
	return false
}

// normalPathVessel is the vessel as it is now, a variable so a test can say what
// the boat is.
var normalPathVessel = func() (vesselView, error) {
	return loadVesselView(getEnv("SETTINGS_FILE", "../settings.yaml"))
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

// normalStartDelay is how long a starting trainer waits before its first run: the
// rest of the interval when every subsystem was attempted within it, from the same
// source, and did not fail, and no wait otherwise. One schedule trains them all, so
// one that is due, or failed, trains every subsystem. A restart does not read
// ninety days again for a boat that was trained within the interval.
func normalStartDelay(file normalModelFile, now time.Time, src normalSource, interval time.Duration) time.Duration {
	wait := interval
	for _, sub := range normalSubsystems() {
		rec, recorded := file.Subsystems[sub.Name]
		at := rec.AttemptedAt
		if at.IsZero() {
			at = rec.TrainedAt
		}
		age := now.Sub(at)
		switch {
		case !recorded, rec.State == normalStateFailed, age < 0, age >= interval:
			return 0
		case rec.State != normalStateSkipped && rec.Source != src:
			// Read from somewhere else last time (a skipped subsystem read nothing).
			return 0
		}
		wait = min(wait, interval-age)
	}
	return wait
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
		return normalSubsystemModel{}, fmt.Errorf("%s: %w", lowerFirst(sub.NoSubjects), errNormalNoSubjects)
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
		for _, id := range sj.EquipmentIDs {
			items[id] = true
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
		ModelInputs: normalInputsOf(subjects),
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

// normalAlarmBelongs says an alarm's path is about the subject: it is one of the
// subject's alarm paths or under one, with or without the notifications. prefix a
// bus alarm carries.
func normalAlarmBelongs(subject normalSubject, alarmPath string) bool {
	path := strings.TrimPrefix(alarmPath, "notifications.")
	return slices.ContainsFunc(subject.AlarmPaths, func(root string) bool {
		return path == root || strings.HasPrefix(path, root+".")
	})
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

// annotateMinutes lets the subsystem classify a subject's minutes once, and refuses
// minutes it left unclassified: the gate, the runs and the regime read the class.
func annotateMinutes(sub normalSubsystem, subject normalSubject, minutes []normalMinute) error {
	if sub.Annotate == nil {
		return nil
	}
	sub.Annotate(minutes)
	for _, m := range minutes {
		if !m.Classified {
			return fmt.Errorf("%s: a minute was not annotated by the %s subsystem", subject.Name, sub.Name)
		}
	}
	return nil
}

// withGroupsOff is the ignored list with every path of a group added when any one
// of them is on it, so switching off a reading that is published under several
// names switches off all of them and the next run does not pick another.
func withGroupsOff(features []normalFeature, ignored map[string]bool) map[string]bool {
	out := maps.Clone(ignored)
	if out == nil {
		out = map[string]bool{}
	}
	for _, f := range features {
		if f.Group == "" || !ignored[f.Path] {
			continue
		}
		for _, g := range features {
			if g.Group == f.Group {
				out[g.Path] = true
			}
		}
	}
	return out
}

// sumSeries is, for each minute, the sum of the series that report in it. A minute
// where none does has no value, and a zero is never put in their place.
func sumSeries(values map[string]map[time.Time]float64, keys []string) map[time.Time]float64 {
	out := map[time.Time]float64{}
	for _, k := range keys {
		for at, v := range values[k] {
			out[at] += v
		}
	}
	return out
}

// firstOfEachGroup keeps, of the readings that share a Group, only the one with the
// most rows, and on a tie the one that reported last: they are alternative paths
// for one reading. The others read as having no data, so selectFeatures leaves
// them out and scoring never asks for them. rows is not changed.
func firstOfEachGroup(features []normalFeature, rows map[string][]normalMinuteRow) map[string][]normalMinuteRow {
	latest := func(path string) time.Time {
		var at time.Time
		for _, r := range rows[path] {
			if r.At.After(at) {
				at = r.At
			}
		}
		return at
	}
	best := map[string]normalFeature{}
	for _, f := range features {
		if f.Group == "" || len(rows[f.Path]) == 0 {
			continue
		}
		b, has := best[f.Group]
		if !has || len(rows[f.Path]) > len(rows[b.Path]) || (len(rows[f.Path]) == len(rows[b.Path]) && latest(f.Path).After(latest(b.Path))) {
			best[f.Group] = f
		}
	}
	var out map[string][]normalMinuteRow
	for _, f := range features {
		if f.Group == "" || len(rows[f.Path]) == 0 || best[f.Group].Path == f.Path {
			continue
		}
		if out == nil {
			out = maps.Clone(rows)
		}
		delete(out, f.Path)
	}
	if out == nil {
		return rows
	}
	return out
}

type bandMinutes struct {
	label string
	idx   []int // indexes into the joined minutes, usable ones
	// keep is parallel to idx: the operator said the minute is normal.
	keep     []bool
	excluded int // gated minutes in the band that an alarm, an answer or a repair took out
}

func trainSubject(ctx context.Context, sub normalSubsystem, subject normalSubject, deps normalTrainDeps, tc trainContext, model *normalSubsystemModel) error {
	// A reading that places a minute is not on the ignored list: it cannot be
	// switched off.
	tc.ignored = withGroupsOff(subject.Features, withoutPlacing(subject.Features, tc.ignored))
	// 1. Every reading's minutes, from the one source that carries it, in one
	// query. A reading switched off is not read, unless the gate needs it.
	var paths []string
	for _, f := range subject.Features {
		if f.Path != "" && (f.Required || !tc.ignored[f.Path]) {
			paths = append(paths, f.Path)
		}
	}
	rows, err := deps.Minutes(ctx, tc.src, paths, tc.window.From, tc.window.To)
	if err != nil {
		return err
	}
	rows = firstOfEachGroup(subject.Features, rows)
	evidence := make([]featureEvidence, len(subject.Features))
	values := map[string]map[time.Time]float64{}
	for i, f := range subject.Features {
		pick := chooseSender(rows[f.Path])
		switch {
		case len(f.SumOf) > 0:
			pick = senderChoice{Values: sumSeries(values, f.SumOf)}
		case f.Placing:
			pick = senderChoice{Values: valuesFromAnySender(rows[f.Path])}
		}
		evidence[i] = featureEvidence{Feature: f, Source: pick.Source, TwoSources: pick.Two, SenderChanged: pick.Changed, Values: pick.Values, Wording: sub.Wording}
		values[f.Key] = pick.Values
	}

	// 2. Join: a minute is a minute of the subject when it has every reading the
	// gate needs. A minute that lacks one is not imputed and is not a minute.
	minutes := joinNormalMinutes(subject.Features, values, func(string) bool { return true })
	if err := annotateMinutes(sub, subject, minutes); err != nil {
		return err
	}

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
		if !normalAlarmBelongs(subject, a.Path) {
			continue
		}
		if sub.AlarmReading != nil {
			if key, isReading := sub.AlarmReading(subject, a); isReading && !relevant[key] {
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
		if slices.Contains(subject.EquipmentIDs, r.EquipmentID) {
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

	sm := normalSubjectModel{Name: subject.Name, EquipmentIDs: subject.EquipmentIDs, GatedMinutes: len(usable)}
	haveVerdicts := len(usable) > 0
	var used []string // keys in subject order
	// A reading the gate needs that has no rows at all is named, when the subject
	// has data for the others: it is what stops the subject being judged.
	var absent []string
	required := 0
	for i, f := range subject.Features {
		if f.Required {
			required++
			if len(evidence[i].Values) == 0 {
				absent = append(absent, f.displayName())
			}
		}
	}
	switch {
	case len(absent) > 0 && len(absent) < required:
		sm.Problem = fmt.Sprintf("%s: no readings in the last %d days", absent[0], int(normalTrainWindow/(24*time.Hour)))
	case gateCount == 0:
		sm.Problem = fmt.Sprintf("no %s in the last %d days", sub.Wording.Steady, int(normalTrainWindow/(24*time.Hour)))
	case len(usable) == 0:
		sm.Problem = fmt.Sprintf("all %s %s were left out (alarms, answers marked real or repairs)", commaInt(gateCount), sub.Wording.Steady)
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
					sm.Problem = normalSwitchedOffProblem(sub, normalFeatureName(v))
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
		perRegime := map[string]bool{}
		for _, f := range subject.Features {
			perRegime[f.Key] = f.PerRegime
		}
		for key, b := range bands {
			model.Regimes[key] = fitBand(subject.ID, key, b, minutes, runIDs, used, byKey, minSD, perRegime, faults, tc)
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
func fitBand(subjectID, key string, b *bandMinutes, minutes []normalMinute, runIDs []int, used []string, verdicts map[string]normalFeatureVerdict, minSD map[string]float64, perRegime map[string]bool, faults []normalExcluded, tc trainContext) normalRegime {
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

	// A reading that is fitted only where it reports is left out of a band whose
	// minutes mostly lack it, and the band is judged on the readings it has.
	used = slices.DeleteFunc(slices.Clone(used), func(k string) bool {
		if !perRegime[k] {
			return false
		}
		have := 0
		for _, i := range b.idx {
			if _, ok := minutes[i].Values[k]; ok {
				have++
			}
		}
		return float64(have) < normalMinPresence*float64(len(b.idx))
	})
	// A band is carried forward only if the readings it describes are the readings
	// this run would fit it with.
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

	// A reading that holds one value through the whole band says nothing about it
	// and leaves the fit; it is judged where it moves.
	keys, rows = dropStillColumns(keys, rows, perRegime)
	if len(keys) == 0 {
		base.Minutes, base.Runs = len(rows), runs
		base.Reason = "could not be fitted: no reading changed in this band"
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

// dropStillColumns removes the readings that hold one value in every row, from
// the keys and from each row. Values are compared to nine significant digits so
// float noise from an average is not a change. keys is not changed.
func dropStillColumns(keys []string, rows [][]float64, perRegime map[string]bool) ([]string, [][]float64) {
	var moving []int
	for j := range keys {
		if !perRegime[keys[j]] {
			moving = append(moving, j)
			continue
		}
		first := significant(rows[0][j])
		for _, r := range rows[1:] {
			if significant(r[j]) != first {
				moving = append(moving, j)
				break
			}
		}
	}
	if len(moving) == len(keys) {
		return keys, rows
	}
	kept := make([]string, len(moving))
	for n, j := range moving {
		kept[n] = keys[j]
	}
	slim := make([][]float64, len(rows))
	for i, r := range rows {
		slim[i] = make([]float64, len(moving))
		for n, j := range moving {
			slim[i][n] = r[j]
		}
	}
	return kept, slim
}

func thinBandReason(minutes, runs int) string {
	if minutes < normalMinMinutes {
		return fmt.Sprintf("%s minutes learned, needs %d", commaInt(minutes), normalMinMinutes)
	}
	return fmt.Sprintf("%s minutes from %s, needs %d %s", commaInt(minutes), plural(runs, normalRunOne, normalRunMany), normalMinRuns, normalRunMany)
}

// normalSwitchedOffProblem is why a subject cannot be judged when a reading its
// gate needs is on the ignored list. Training and scoring say it the same way.
func normalSwitchedOffProblem(sub normalSubsystem, reading string) string {
	return fmt.Sprintf("%s is switched off, so %s cannot be judged", reading, sub.Wording.This)
}

// normalFeatureName is a reading's plain name for the operator, "oil pressure".
func normalFeatureName(v normalFeatureVerdict) string {
	if v.Name != "" {
		return v.Name
	}
	if name := sensorQuantity(v.Path); name != "" {
		return name
	}
	return v.Key
}

// displayName is the feature's plain name for the operator.
func (f normalFeature) displayName() string {
	return normalFeatureName(normalFeatureVerdict{Key: f.Key, Path: f.Path, Name: f.Name})
}
