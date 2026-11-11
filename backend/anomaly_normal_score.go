package main

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
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
	Key         string
	Path        string
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
	Model           *unusualModelInfo
	JudgedMinutes   int
	Stretches       []unusualStretch
	NotJudged       []unusualNotJudged
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

// scoreUnusualReadings judges [since, until) for every subject of the
// subsystem, or only subjectFilter when it is set. A minute covered by the
// operator's latest answer being normal is judged but never flagged. A window longer than
// normalScoreMaxSince is cut to its newest part and the result says so.
func scoreUnusualReadings(ctx context.Context, sub normalSubsystem, deps normalScoreDeps, since, until time.Time, subjectFilter string, answers []normalAnswer) unusualResult {
	// Minutes are labelled by their first instant, so the minute in progress is
	// left out by ending the window on the last whole minute.
	until = until.Truncate(time.Minute)
	res := unusualResult{Since: since, Until: until}
	if floor := until.Add(-normalScoreMaxSince); since.Before(floor) {
		res.Since, res.Clipped = floor, true
		since = floor
	}
	fail := func(kind string, err error) unusualResult {
		res.ProblemKind, res.Problem = kind, firstErrorLine(err)
		return res
	}

	src, err := deps.Source()
	if err != nil {
		if errors.Is(err, errNormalNoInflux) {
			return fail(unusualNoInflux, err)
		}
		return fail(unusualError, err)
	}
	file, err := deps.Model()
	if err != nil {
		return fail(unusualUnreadable, err)
	}
	model, ok := file.Subsystems[sub.Name]
	if !ok {
		if file.LastAttempt != nil && !file.LastAttempt.OK && file.LastAttempt.Problem != "" {
			return fail(unusualNoModel, errors.New(file.LastAttempt.Problem))
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
	if file.LastAttempt != nil && !file.LastAttempt.OK {
		res.TrainingProblem = file.LastAttempt.Problem
	}

	vessel, err := deps.Vessel()
	if err != nil {
		return fail(unusualError, fmt.Errorf("vessel settings: %w", err))
	}
	subjects := sub.Subjects(vessel)
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
		if err := scoreSubject(ctx, sub, subject, deps, src, model, since, until, answers, &res); err != nil {
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

func scoreSubject(ctx context.Context, sub normalSubsystem, subject normalSubject, deps normalScoreDeps, src normalSource, model normalSubsystemModel, since, until time.Time, answers []normalAnswer, res *unusualResult) error {
	engineProblem := func(reason string) {
		res.NotJudged = append(res.NotJudged, unusualNotJudged{Kind: "engine_problem", Subject: subject.ID, SubjectName: subject.Name, Reason: reason})
	}
	// What training said about this subject decides whether it is judged at all.
	sm, known := model.Subjects[subject.ID]
	if !known {
		engineProblem(fmt.Sprintf("no learned normal yet for %s", subject.Name))
		return nil
	}
	if sm.Problem != "" {
		engineProblem(sm.Problem)
		return nil
	}
	var ignored map[string]bool
	if deps.Ignored != nil {
		ignored = deps.Ignored()
	}
	featureByKey := map[string]normalFeature{}
	for _, f := range subject.Features {
		featureByKey[f.Key] = f
		if f.Required && ignored[f.Path] {
			engineProblem(normalSwitchedOffProblem(f.Key, f.Path))
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
	switchedOff := func(reg normalRegime) []string {
		var keys []string
		for _, k := range reg.Features {
			if ignored[featureByKey[k].Path] {
				keys = append(keys, k)
			}
		}
		return keys
	}

	// Read the readings some judged band of this subject uses, and the ones the
	// gate needs.
	need := map[string]bool{}
	for _, f := range subject.Features {
		if f.Required {
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
	var paths []string
	for _, f := range subject.Features {
		if need[f.Key] {
			if _, pinned := senders[f.Key]; !pinned {
				return fmt.Errorf("%s: %s has no sender saved with the learned normal", subject.Name, normalFeatureName(normalFeatureVerdict{Key: f.Key, Path: f.Path}))
			}
			paths = append(paths, f.Path)
		}
	}
	rows, err := deps.Minutes(ctx, src, paths, since.Add(-normalScoreLeadIn), until)
	if err != nil {
		return err
	}
	values := map[string]map[time.Time]float64{}
	for _, f := range subject.Features {
		if need[f.Key] {
			values[f.Key] = valuesFromSender(rows[f.Path], senders[f.Key])
		}
	}
	minutes := joinNormalMinutes(subject.Features, values, func(key string) bool { return need[key] })
	gate := sub.Gate(minutes)

	// A sender the model was learned from that has gone quiet while the engine
	// runs, on a reading that other senders still report or on one with no data at
	// all, is said and never left to look like a missing reading. A model that no
	// longer matches the boat asks for training.
	ran := false
	for i, m := range minutes {
		if gate[i] && !m.At.Before(since) && m.At.Before(until) {
			ran = true
			break
		}
	}
	silent := map[string]*unusualNotJudged{}
	for _, f := range subject.Features {
		if !need[f.Key] || len(values[f.Key]) > 0 {
			continue
		}
		if (f.Required && len(rows[f.Path]) > 0) || (!f.Required && ran) {
			sender := senders[f.Key]
			if sender == "" {
				sender = "no sender name"
			}
			t := &unusualNotJudged{Kind: "sender_silent", Subject: subject.ID, SubjectName: subject.Name,
				Reason: fmt.Sprintf("%s: the sender this was learned from (%s) has gone quiet; relearning at the next training run", normalFeatureName(normalFeatureVerdict{Key: f.Key, Path: f.Path}), sender)}
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
		key, label, ok := sub.Regime(subject, m)
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
					t.Reason = fmt.Sprintf("no history yet for %s", label)
					t.Short = fmt.Sprintf("%s has no history yet", label)
				case reg.Minutes >= normalMinMinutes && reg.Runs >= normalMinRuns:
					// Enough history, but the fit itself failed.
					why := strings.TrimPrefix(reg.Reason, "could not be fitted: ")
					t.Reason = fmt.Sprintf("%s could not be fitted: %s", label, why)
					t.Short = fmt.Sprintf("%s could not be fitted: %s", label, why)
				case reg.Minutes < normalMinMinutes:
					t.Reason = fmt.Sprintf("not enough history for %s: %s", label, reg.Reason)
					t.Short = fmt.Sprintf("%s has %s of history, needs %d", label, plural(reg.Minutes, "minute", "minutes"), normalMinMinutes)
				default:
					t.Reason = fmt.Sprintf("not enough history for %s: %s", label, reg.Reason)
					t.Short = fmt.Sprintf("%s has %s of history from %s, needs %d runs", label, plural(reg.Minutes, "minute", "minutes"), plural(reg.Runs, "run", "runs"), normalMinRuns)
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
		res.NotJudged = append(res.NotJudged, unusualNotJudged{Kind: "not_steady", Subject: subject.ID, SubjectName: subject.Name, Reason: "not running steadily", Minutes: notSteady})
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
		out[i] = normalFeatureName(normalFeatureVerdict{Key: k, Path: features[k].Path})
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
	top := unusualReading{Key: best, Path: features[best].Path, SIUnit: features[best].SIUnit}
	top.StretchMean, top.Usual = meanSum/float64(n), usualSum/float64(n)
	top.Direction = "high"
	if top.StretchMean < top.Usual {
		top.Direction = "low"
	}
	st.Top = top
	return st
}
