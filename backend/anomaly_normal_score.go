package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scoring a past window against the learned normal (ADR 0276). It is done on
// demand, from the same one-minute means training uses, always read from the
// main bucket because the minute bucket can lag by an hour. A minute that fails
// the gate, has no learned band or lacks a reading of its band is not judged and
// is counted by reason. Nothing is imputed.

const (
	// normalScoreLeadIn is read before the window so the gate's ten running
	// minutes and five steady ones are known for the window's first minute.
	normalScoreLeadIn = 15 * time.Minute
	// normalScoreMaxSince is the longest window a score covers. A longer one is
	// cut to it and the result says so (Clipped).
	normalScoreMaxSince = 7 * 24 * time.Hour
)

// Problem kinds, the cases the report and the tool word differently.
const (
	unusualNoInflux     = "no_influx"
	unusualUnreadable   = "unreadable"
	unusualNeverTrained = "never_trained"
	unusualNoModel      = "no_model" // no model, and the last run failed
	unusualError        = "error"
	unusualMixed        = "mixed" // several subsystems, each with its own reason
)

type normalScoreDeps struct {
	Now     func() time.Time
	Source  func() (normalSource, error)
	Minutes func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error)
	Vessel  func() (vesselView, error)
	Model   func() (normalModelFile, error)
	// Ignored is the operator's ignored-sensor list, read when scoring: a reading
	// switched off since the last training run is never judged or named.
	Ignored func() map[string]bool
	// Wake asks for a training run, for a model that no longer matches the boat.
	Wake func()
}

func productionNormalScoreDeps(settingsPath string) normalScoreDeps {
	return normalScoreDeps{
		Now:    func() time.Time { return time.Now().UTC() },
		Source: func() (normalSource, error) { return resolveNormalSource(settingsPath, true) },
		Minutes: func(ctx context.Context, src normalSource, paths []string, start, stop time.Time) (map[string][]normalMinuteRow, error) {
			return queryNormalMinutes(ctx, settingsPath, src, paths, start, stop)
		},
		Vessel:  func() (vesselView, error) { return loadVesselView(settingsPath) },
		Model:   func() (normalModelFile, error) { return loadNormalModel(anomalyNormalPath()) },
		Ignored: ignoredSensorSet,
		Wake:    requestNormalTraining,
	}
}

// unusualReading is the reading that explains a stretch, in SI units.
type unusualReading struct {
	Key  string
	Path string
	// Name is the whole reading's name when the feature carries one ("House bank
	// voltage"), and empty when it is named from the subject and the path.
	Name        string
	SIUnit      string
	Direction   string // "high" or "low"
	StretchMean float64
	Usual       float64
}

type unusualStretch struct {
	ID          string
	Subsystem   string
	Subject     string
	SubjectName string
	Start       time.Time
	// End is the instant after the last unusual minute.
	End       time.Time
	Minutes   int
	PeakRatio float64 // highest D2 over its band's threshold
	Band      string
	Top       unusualReading
}

// unusualNotJudged is a count of minutes that were not judged, with the reason.
type unusualNotJudged struct {
	Kind        string // not_steady, thin_band, missing_reading
	Subject     string
	SubjectName string
	Label       string // the band, for thin_band
	Reason      string
	// Short is a thin band said for the report: "1,400 to 1,600 rpm has 22
	// minutes of history, needs 60".
	Short   string
	Minutes int
}

type unusualModelInfo struct {
	TrainedAt time.Time
	Source    normalSource
	// Bands counts the judged bands of each subject, by the subject's name.
	Bands map[string]int
}

type unusualResult struct {
	Since, Until time.Time
	// Clipped says Since was cut back to normalScoreMaxSince before Until.
	Clipped bool
	// ProblemKind and Problem say why no judgement could be made at all.
	ProblemKind string
	Problem     string
	// TrainingProblem is the latest training run's problem, set even when an
	// older model could still be used.
	TrainingProblem string
	// cutShort says the problem is an error the clock or a shutdown caused.
	cutShort      bool
	Model         *unusualModelInfo
	JudgedMinutes int
	Stretches     []unusualStretch
	NotJudged     []unusualNotJudged
}

// rankStretches orders stretches by how far past its threshold each peaked,
// then by length, then by start.
func rankStretches(s []unusualStretch) {
	sort.SliceStable(s, func(i, j int) bool {
		if s[i].PeakRatio != s[j].PeakRatio {
			return s[i].PeakRatio > s[j].PeakRatio
		}
		if s[i].Minutes != s[j].Minutes {
			return s[i].Minutes > s[j].Minutes
		}
		return s[i].Start.Before(s[j].Start)
	})
}

// unusualSubject names one subject of one subsystem. The zero value is every
// subject of every subsystem.
type unusualSubject struct {
	Subsystem, ID string
}

// unusualInputs is what a scoring reads once and every subsystem shares: where
// the minutes come from, the saved model and the vessel.
type unusualInputs struct {
	src    normalSource
	file   normalModelFile
	vessel vesselView
	// leadIn is how far before the window the minutes are read, so the gate and
	// the classification of its first minutes are known as in training.
	leadIn time.Duration
}

// loadUnusualInputs reads them, or says which kind of problem stops the scoring.
func loadUnusualInputs(deps normalScoreDeps) (unusualInputs, string, error) {
	src, err := deps.Source()
	if err != nil {
		if errors.Is(err, errNormalNoInflux) {
			return unusualInputs{}, unusualNoInflux, err
		}
		return unusualInputs{}, unusualError, err
	}
	file, err := deps.Model()
	if err != nil {
		return unusualInputs{}, unusualUnreadable, err
	}
	vessel, err := deps.Vessel()
	if err != nil {
		return unusualInputs{}, unusualError, fmt.Errorf("vessel settings: %w", err)
	}
	return unusualInputs{src: src, file: file, vessel: vessel, leadIn: normalScoreLeadIn}, "", nil
}

// newUnusualResult is the empty result for [since, until). Minutes are labelled
// by their first instant, so the minute in progress is left out by ending the
// window on the last whole minute. A window longer than normalScoreMaxSince is
// cut to its newest part and the result says so.
func newUnusualResult(since, until time.Time) unusualResult {
	until = until.Truncate(time.Minute)
	res := unusualResult{Since: since, Until: until}
	if floor := until.Add(-normalScoreMaxSince); since.Before(floor) {
		res.Since, res.Clipped = floor, true
	}
	return res
}

// scoreUnusualAll judges [since, until) for every subsystem the boat has
// something set up for, or only the subject named, and merges the results: the
// stretches ranked together, the minutes and the notes added up. A subsystem
// that cannot be judged (no model yet) is one note and does not hide the others;
// only when none can be is there a problem for the whole result.
func scoreUnusualAll(ctx context.Context, deps normalScoreDeps, since, until time.Time, only unusualSubject, answers []normalAnswer) unusualResult {
	res := newUnusualResult(since, until)
	in, kind, err := loadUnusualInputs(deps)
	if err != nil {
		res.ProblemKind, res.Problem = kind, firstErrorLine(err)
		return res
	}
	subs := normalSubsystems()
	if only.Subsystem != "" {
		sub, ok := normalSubsystemNamed(only.Subsystem)
		if !ok {
			res.ProblemKind, res.Problem = unusualError, fmt.Sprintf("%q is not something Helmcentral learns", only.Subsystem)
			return res
		}
		subs = []normalSubsystem{sub}
	} else {
		// A boat with nothing of a kind set up is not told about it. When nothing at
		// all is set up the first subsystem says why.
		present := slices.DeleteFunc(slices.Clone(subs), func(sub normalSubsystem) bool { return len(sub.Subjects(in.vessel)) == 0 })
		if len(present) == 0 {
			present = subs[:1]
		}
		subs = present
	}
	// Every subsystem reads from the same instant, the earliest any of them needs, so
	// what they share can be read once.
	for _, sub := range subs {
		in.leadIn = max(in.leadIn, sub.LeadIn)
	}
	deps.Minutes = shareMinutes(deps.Minutes, sharedNormalPaths(in.vessel))
	// The subsystems are scored together, under the one budget.
	results := make([]unusualResult, len(subs))
	var wg sync.WaitGroup
	for i, sub := range subs {
		id := ""
		if only.Subsystem == sub.Name {
			id = only.ID
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = scoreSubsystem(ctx, sub, deps, in, res, id, answers)
		}()
	}
	wg.Wait()
	// A subsystem cut short by the clock or a shutdown says so in the budget's own
	// sentence, not as a raw error.
	for i := range results {
		if results[i].ProblemKind == unusualError && results[i].cutShort {
			if why, cut := unusualCutShort(ctx); cut {
				results[i].Problem = why
			}
		}
	}
	return mergeUnusual(res, subs, results)
}

type unusualBudgetKey struct{}

// withUnusualBudget says how long the scoring was allowed, so a subsystem that ran
// out of it can say so.
func withUnusualBudget(ctx context.Context, budget time.Duration) context.Context {
	return context.WithValue(ctx, unusualBudgetKey{}, budget)
}

// unusualCutShort is the sentence for a scoring the clock or a shutdown ended.
func unusualCutShort(ctx context.Context) (string, bool) {
	switch err := ctx.Err(); {
	case err == nil:
		return "", false
	case errors.Is(err, context.Canceled):
		return "Helmcentral was stopping", true
	}
	if budget, ok := ctx.Value(unusualBudgetKey{}).(time.Duration); ok {
		return fmt.Sprintf("it took longer than %d seconds", int(budget/time.Second)), true
	}
	return "it took longer than the time allowed", true
}

// mergeUnusual adds up the results of the subsystems that could be judged, on
// top of base (the window). Those that could not are a note each, and when none
// could the first one's problem stands for the whole.
func mergeUnusual(base unusualResult, subs []normalSubsystem, results []unusualResult) unusualResult {
	out := base
	judged := false
	var problems []string
	for i, r := range results {
		if r.ProblemKind != "" {
			continue
		}
		judged = true
		out.JudgedMinutes += r.JudgedMinutes
		out.Stretches = append(out.Stretches, r.Stretches...)
		out.NotJudged = append(out.NotJudged, r.NotJudged...)
		if r.TrainingProblem != "" {
			// With several subsystems each says its own, by name.
			problem := r.TrainingProblem
			if len(results) > 1 {
				problem = subs[i].Title + ": " + problem
			}
			problems = append(problems, problem)
		}
		if r.Model != nil {
			// The line says when and from where it was learned, so both come from the
			// one record: the oldest.
			if out.Model == nil {
				out.Model = &unusualModelInfo{TrainedAt: r.Model.TrainedAt, Source: r.Model.Source, Bands: map[string]int{}}
			} else if r.Model.TrainedAt.Before(out.Model.TrainedAt) {
				out.Model.TrainedAt, out.Model.Source = r.Model.TrainedAt, r.Model.Source
			}
			maps.Copy(out.Model.Bands, r.Model.Bands)
		}
	}
	if !judged {
		// Nothing could be judged: every subsystem says why, or the one reason they share.
		first := results[0]
		kindsDiffer := slices.ContainsFunc(results, func(r unusualResult) bool { return r.ProblemKind != first.ProblemKind })
		switch {
		case kindsDiffer:
			// Each subsystem is worded in the sentence for its own kind.
			var parts []string
			for i, r := range results {
				parts = append(parts, subs[i].Title+": "+unusualProblemNote(r.ProblemKind, r.Problem))
			}
			first.ProblemKind, first.Problem = unusualMixed, strings.Join(parts, " ")
		case !allSame(results):
			var parts []string
			for i, r := range results {
				parts = append(parts, subs[i].Title+": "+strings.TrimSuffix(r.Problem, "."))
			}
			first.Problem = strings.Join(parts, "; ")
		}
		return first
	}
	out.TrainingProblem = strings.Join(problems, "; ")
	for i, r := range results {
		if r.ProblemKind == "" {
			continue
		}
		reason := r.Problem
		if r.ProblemKind == unusualNeverTrained {
			reason = "no learned normal yet"
		}
		out.NotJudged = append(out.NotJudged, unusualNotJudged{Kind: "subject_problem", SubjectName: subs[i].Title, Reason: reason})
	}
	rankStretches(out.Stretches)
	return out
}

// allSame says every result gives the same problem.
func allSame(results []unusualResult) bool {
	return !slices.ContainsFunc(results, func(r unusualResult) bool { return r.Problem != results[0].Problem })
}

// scoreSubsystem judges the window res holds for one subsystem, from inputs
// already read.
func scoreSubsystem(ctx context.Context, sub normalSubsystem, deps normalScoreDeps, in unusualInputs, res unusualResult, subjectFilter string, answers []normalAnswer) unusualResult {
	since, until := res.Since, res.Until
	fail := func(kind string, err error) unusualResult {
		res.ProblemKind, res.Problem = kind, firstErrorLine(err)
		// The clock or a shutdown ended it when the budget's context is done, whatever
		// error the client gave: the InfluxDB client's does not always wrap the context's.
		res.cutShort = ctx.Err() != nil
		return res
	}
	file, src := in.file, in.src
	model, ok := file.Subsystems[sub.Name]
	if !ok {
		return fail(unusualNeverTrained, errors.New(normalNoModelYet))
	}
	// What the boat has now decides whether there is anything to score. A record
	// saved as skipped says nothing once a bank is chosen.
	subjects := sub.Subjects(in.vessel)
	if len(subjects) == 0 {
		return fail(unusualNoModel, errors.New(lowerFirst(sub.NoSubjects)))
	}
	// A kept model describes what was set up when it was learned. Against another
	// bank it would judge the wrong bank, so it waits for the next training run.
	if model.hasModel() && !model.usableFor(subjects) {
		if model.State == normalStateFailed && model.Problem != "" {
			return fail(unusualNoModel, errors.New(model.Problem))
		}
		return fail(unusualNeverTrained, errors.New(normalNoModelYet))
	}
	if !model.hasModel() {
		// Its own record says why there is nothing to score against, when it failed.
		if model.State == normalStateFailed && model.Problem != "" {
			return fail(unusualNoModel, errors.New(model.Problem))
		}
		return fail(unusualNeverTrained, errors.New(normalNoModelYet))
	}
	res.Model = &unusualModelInfo{TrainedAt: model.TrainedAt, Source: model.Source, Bands: map[string]int{}}
	for id, n := range model.bandsBySubject() {
		name := model.Subjects[id].Name
		if name == "" {
			name = id
		}
		res.Model.Bands[name] = n
	}
	res.TrainingProblem = file.trainingProblem(sub.Name)

	if subjectFilter != "" {
		var only []normalSubject
		for _, s := range subjects {
			if s.ID == subjectFilter {
				only = append(only, s)
			}
		}
		if len(only) == 0 {
			return fail(unusualError, fmt.Errorf("no %s is set up with the id %q", sub.Name, subjectFilter))
		}
		subjects = only
	}
	for _, subject := range subjects {
		if err := scoreSubject(ctx, sub, subject, deps, src, model, since, until, max(in.leadIn, sub.LeadIn), answers, &res); err != nil {
			return fail(unusualError, err)
		}
	}
	rankStretches(res.Stretches)
	return res
}

type scoredMinute struct {
	at      time.Time
	unusual bool
	ratio   float64
	reg     normalRegime
	x       []float64 // the minute's readings, in reg.Features order
	contrib []float64 // each reading's part of the distance, same order
}

func scoreSubject(ctx context.Context, sub normalSubsystem, subject normalSubject, deps normalScoreDeps, src normalSource, model normalSubsystemModel, since, until time.Time, leadIn time.Duration, answers []normalAnswer, res *unusualResult) error {
	subjectProblem := func(reason string) {
		res.NotJudged = append(res.NotJudged, unusualNotJudged{Kind: "subject_problem", Subject: subject.ID, SubjectName: subject.Name, Reason: reason})
	}
	// What training said about this subject decides whether it is judged at all.
	sm, known := model.Subjects[subject.ID]
	if !known {
		subjectProblem(fmt.Sprintf("no learned normal yet for %s", subject.Name))
		return nil
	}
	if sm.Problem != "" {
		subjectProblem(sm.Problem)
		return nil
	}
	var ignored map[string]bool
	if deps.Ignored != nil {
		ignored = withGroupsOff(subject.Features, withoutPlacing(subject.Features, deps.Ignored()))
	}
	featureByKey := map[string]normalFeature{}
	for _, f := range subject.Features {
		featureByKey[f.Key] = f
		if f.Required && ignored[f.Path] {
			subjectProblem(normalSwitchedOffProblem(sub, f.displayName()))
			return nil
		}
	}
	// The sender each reading is read from is the one training pinned it to.
	senders := map[string]string{}
	for _, v := range sm.Features {
		senders[v.Key] = v.Source
	}
	// A band that describes a reading now switched off is not judged, whatever
	// it was learned with.
	// A reading is off when it is, or when any reading it is the sum of is: a partial
	// sum is not judged.
	featureOff := func(f normalFeature) bool {
		if ignored[f.Path] {
			return true
		}
		for _, k := range f.SumOf {
			if ignored[featureByKey[k].Path] {
				return true
			}
		}
		return false
	}
	switchedOff := func(reg normalRegime) []string {
		var keys []string
		for _, k := range reg.Features {
			if featureOff(featureByKey[k]) {
				keys = append(keys, k)
			}
		}
		return keys
	}

	// Read the readings some judged band of this subject uses, and the ones the
	// gate needs.
	need := map[string]bool{}
	for _, f := range subject.Features {
		// The readings that place a minute are asked for whatever the bands use, or a
		// minute would land in the wrong band when one was learned without them.
		if f.Required || f.Placing {
			need[f.Key] = true
		}
	}
	for _, r := range model.Regimes {
		if r.Subject == subject.ID && r.Judged && len(switchedOff(r)) == 0 {
			for _, k := range r.Features {
				need[k] = true
			}
		}
	}
	// A reading that is a sum needs the readings it sums, those still switched on.
	for _, f := range subject.Features {
		if need[f.Key] {
			for _, k := range f.SumOf {
				if !ignored[featureByKey[k].Path] {
					need[k] = true
				}
			}
		}
	}
	var paths []string
	for _, f := range subject.Features {
		if need[f.Key] && f.Path != "" {
			if _, pinned := senders[f.Key]; !pinned && !f.Placing {
				return fmt.Errorf("%s: %s has no sender saved with the learned normal", subject.Name, f.displayName())
			}
			paths = append(paths, f.Path)
		}
	}
	rows, err := deps.Minutes(ctx, src, paths, since.Add(-leadIn), until)
	if err != nil {
		return err
	}
	values := map[string]map[time.Time]float64{}
	for _, f := range subject.Features {
		switch {
		case !need[f.Key]:
		case len(f.SumOf) > 0:
			values[f.Key] = sumSeries(values, f.SumOf)
		case f.Placing:
			values[f.Key] = valuesFromAnySender(rows[f.Path])
		default:
			values[f.Key] = valuesFromSender(rows[f.Path], senders[f.Key])
		}
	}
	minutes := joinNormalMinutes(subject.Features, values, func(key string) bool { return need[key] })
	if err := annotateMinutes(sub, subject, minutes); err != nil {
		return err
	}
	gate := sub.Gate(minutes)

	// A sender the model was learned from that has gone quiet while the engine
	// runs, on a reading that other senders still report or on one with no data at
	// all, is said and never left to look like a missing reading. A model that no
	// longer matches the boat asks for training.
	// A reading is missing, in a way nothing explains, from a judged minute when the
	// minute has none and its zero rule (an engine stopped) does not account for it.
	// Each judged minute is placed once.
	type placement struct {
		key, label string
		ok         bool
	}
	placements := make([]placement, len(minutes))
	for i, m := range minutes {
		if gate[i] && !m.At.Before(since) && m.At.Before(until) {
			k, l, ok := sub.Regime(subject, m)
			placements[i] = placement{k, l, ok}
		}
	}
	gapped := func(f normalFeature) bool {
		for i, m := range minutes {
			if _, ok := m.Values[f.Key]; ok || !placements[i].ok {
				continue
			}
			// Only a minute in a band that normally carries the reading counts.
			if reg, has := model.Regimes[placements[i].key]; has && reg.Judged && slices.Contains(reg.Features, f.Key) {
				return true
			}
		}
		return false
	}
	silent := map[string]*unusualNotJudged{}
	for _, f := range subject.Features {
		if !need[f.Key] || len(values[f.Key]) > 0 || f.Placing || f.Component || len(f.SumOf) > 0 {
			continue
		}
		if (f.Required && len(rows[f.Path]) > 0) || (!f.Required && gapped(f)) {
			sender := senders[f.Key]
			if sender == "" {
				sender = "no sender name"
			}
			t := &unusualNotJudged{Kind: "sender_silent", Subject: subject.ID, SubjectName: subject.Name,
				Reason: fmt.Sprintf("%s: the sender this was learned from (%s) has gone quiet; relearning at the next training run", f.displayName(), sender)}
			t.Short = t.Reason
			silent[f.Key] = t
		}
	}
	if len(silent) > 0 && deps.Wake != nil {
		deps.Wake()
	}

	// Only this subject's answers matter below. They never overlap (loadNormalAnswers
	// refuses a file where they do), so sorted once they are walked alongside the
	// minutes, which joinNormalMinutes returns sorted by time: one pass over each.
	mine := slices.DeleteFunc(slices.Clone(answers), func(a normalAnswer) bool { return a.Subsystem != sub.Name || a.Subject != subject.ID })
	slices.SortFunc(mine, func(a, b normalAnswer) int { return a.From.Compare(b.From) })
	next := 0
	scorers := map[string]*normalScorer{}
	notSteady := 0
	thin := map[string]*unusualNotJudged{}
	off := map[string]*unusualNotJudged{}
	missing := map[string]*unusualNotJudged{}
	var scored []scoredMinute

	for i, m := range minutes {
		if m.At.Before(since) || !m.At.Before(until) {
			continue
		}
		if !gate[i] {
			notSteady++
			continue
		}
		key, label, ok := placements[i].key, placements[i].label, placements[i].ok
		if !ok {
			continue
		}
		reg, has := model.Regimes[key]
		if !has || !reg.Judged {
			t := thin[key]
			if t == nil {
				t = &unusualNotJudged{Kind: "thin_band", Subject: subject.ID, SubjectName: subject.Name, Label: label}
				switch {
				case !has:
					t.Reason = fmt.Sprintf("no history yet for %s", sub.Wording.BandNamePrefix+label)
					t.Short = fmt.Sprintf("%s has no history yet", sub.Wording.BandNamePrefix+label)
				case reg.Minutes >= normalMinMinutes && reg.Runs >= normalMinRuns:
					// Enough history, but the fit itself failed.
					why := strings.TrimPrefix(reg.Reason, "could not be fitted: ")
					t.Reason = fmt.Sprintf("%s could not be fitted: %s", sub.Wording.BandNamePrefix+label, why)
					t.Short = t.Reason
				case reg.Minutes < normalMinMinutes:
					t.Reason = fmt.Sprintf("not enough history for %s: %s", sub.Wording.BandNamePrefix+label, reg.Reason)
					t.Short = fmt.Sprintf("%s has %s of history, needs %d", sub.Wording.BandNamePrefix+label, plural(reg.Minutes, "minute", "minutes"), normalMinMinutes)
				default:
					t.Reason = fmt.Sprintf("not enough history for %s: %s", sub.Wording.BandNamePrefix+label, reg.Reason)
					t.Short = fmt.Sprintf("%s has %s of history from %s, needs %d %s", sub.Wording.BandNamePrefix+label, plural(reg.Minutes, "minute", "minutes"), plural(reg.Runs, normalRunOne, normalRunMany), normalMinRuns, normalRunMany)
				}
				thin[key] = t
			}
			t.Minutes++
			continue
		}
		if gone := switchedOff(reg); len(gone) > 0 {
			t := off[key]
			if t == nil {
				t = &unusualNotJudged{Kind: "switched_off", Subject: subject.ID, SubjectName: subject.Name, Label: label,
					Reason: listAnd(featureNames(gone, featureByKey)) + verbAre(len(gone)) + "switched off; relearning at the next training run"}
				t.Short = t.Reason
				off[key] = t
			}
			t.Minutes++
			continue
		}
		quiet := ""
		for _, k := range reg.Features {
			if silent[k] != nil {
				quiet = k
				break
			}
		}
		if quiet != "" {
			silent[quiet].Minutes++
			continue
		}
		x := make([]float64, len(reg.Features))
		var gone []string
		for j, k := range reg.Features {
			v, ok := m.Values[k]
			if !ok {
				gone = append(gone, k)
				continue
			}
			x[j] = v
		}
		if len(gone) > 0 {
			// A minute missing several readings is counted once, under all of them.
			id := strings.Join(gone, ",")
			t := missing[id]
			if t == nil {
				t = &unusualNotJudged{Kind: "missing_reading", Subject: subject.ID, SubjectName: subject.Name,
					Reason: "no " + listAnd(featureNames(gone, featureByKey)) + readingsText(len(gone))}
				missing[id] = t
			}
			t.Minutes++
			continue
		}
		sc := scorers[key]
		if sc == nil {
			var err error
			if sc, err = reg.scorer(); err != nil {
				return fmt.Errorf("%s: %w", reg.Label, err)
			}
			scorers[key] = sc
		}
		d2, contrib, err := sc.score(x)
		if err != nil {
			return fmt.Errorf("%s: %w", reg.Label, err)
		}
		// A minute the operator called normal is judged but never flagged.
		for next < len(mine) && !mine[next].To.After(m.At) {
			next++
		}
		cleared := next < len(mine) && !mine[next].From.After(m.At) && mine[next].Answer == normalAnswerNormal
		scored = append(scored, scoredMinute{at: m.At, unusual: d2 > reg.Threshold && !cleared, ratio: d2 / reg.Threshold, reg: reg, x: x, contrib: contrib})
		res.JudgedMinutes++
	}

	if notSteady > 0 {
		res.NotJudged = append(res.NotJudged, unusualNotJudged{Kind: "not_steady", Subject: subject.ID, SubjectName: subject.Name, Reason: sub.Wording.Unsteady, Minutes: notSteady})
	}
	for _, key := range slices.Sorted(maps.Keys(thin)) {
		res.NotJudged = append(res.NotJudged, *thin[key])
	}
	for _, key := range slices.Sorted(maps.Keys(silent)) {
		res.NotJudged = append(res.NotJudged, *silent[key])
	}
	for _, key := range slices.Sorted(maps.Keys(off)) {
		res.NotJudged = append(res.NotJudged, *off[key])
	}
	for _, id := range slices.Sorted(maps.Keys(missing)) {
		res.NotJudged = append(res.NotJudged, *missing[id])
	}

	// Stretches: unusual minutes one after another.
	var cur []scoredMinute
	flush := func() {
		if len(cur) >= normalStretchMinMinutes {
			res.Stretches = append(res.Stretches, buildStretch(sub, subject, featureByKey, cur))
		}
		cur = nil
	}
	for _, s := range scored {
		switch {
		case !s.unusual:
			flush()
		case len(cur) > 0 && !s.at.Equal(cur[len(cur)-1].at.Add(time.Minute)):
			flush()
			cur = append(cur, s)
		default:
			cur = append(cur, s)
		}
	}
	flush()
	return nil
}

// featureNames is the plain names of readings, in the order given.
func featureNames(keys []string, features map[string]normalFeature) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = features[k].displayName()
	}
	return out
}

func readingsText(n int) string {
	if n == 1 {
		return " reading"
	}
	return " readings"
}

// buildStretch explains a run of unusual minutes: the reading with the largest
// summed contribution among those that may be named, how its mean in the
// stretch compares with its usual value, and the band most of it was in.
func buildStretch(sub normalSubsystem, subject normalSubject, features map[string]normalFeature, minutes []scoredMinute) unusualStretch {
	first, last := minutes[0], minutes[len(minutes)-1]
	st := unusualStretch{
		ID:        stretchIDFor(sub.Name, subject.ID, first.at),
		Subsystem: sub.Name, Subject: subject.ID, SubjectName: subject.Name,
		Start: first.at, End: last.at.Add(time.Minute), Minutes: len(minutes),
	}
	sums := map[string]float64{}
	inBand := map[string]int{}
	for _, m := range minutes {
		st.PeakRatio = max(st.PeakRatio, m.ratio)
		inBand[m.reg.Label]++
		for j, k := range m.reg.Features {
			if features[k].Explain {
				sums[k] += m.contrib[j]
			}
		}
	}
	for label, n := range inBand {
		if st.Band == "" || n > inBand[st.Band] || (n == inBand[st.Band] && label < st.Band) {
			st.Band = label
		}
	}
	// Subject order breaks a tie between readings.
	best, bestSum := "", 0.0
	for _, f := range subject.Features {
		if s, ok := sums[f.Key]; ok && s > bestSum {
			best, bestSum = f.Key, s
		}
	}
	if best == "" {
		return st
	}
	var meanSum, usualSum float64
	n := 0
	for _, m := range minutes {
		if j := slices.Index(m.reg.Features, best); j >= 0 {
			meanSum += m.x[j]
			usualSum += m.reg.Mean[j]
			n++
		}
	}
	top := unusualReading{Key: best, Path: features[best].Path, Name: features[best].Name, SIUnit: features[best].SIUnit}
	top.StretchMean, top.Usual = meanSum/float64(n), usualSum/float64(n)
	top.Direction = "high"
	if top.StretchMean < top.Usual {
		top.Direction = "low"
	}
	st.Top = top
	return st
}
