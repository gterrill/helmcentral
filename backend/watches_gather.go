package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Reading the boat for a watch report (ADR 0227). Everything here is I/O and
// reuse: get_vessel_status' own builders for the figures, the alarm log for
// what was raised, the previous run for what to compare with, and the weather
// provider for the planned stay. buildWatchReport (watches_report.go) decides
// what it all means.

// watchFirstRunWindow is how far back a watch's first report looks.
const watchFirstRunWindow = 24 * time.Hour

// watchUnusualBudget is how long a watch report waits for the learned normal's
// scoring. The report goes out on time without it.
var watchUnusualBudget = 90 * time.Second

func gatherWatchReportInput(ctx context.Context, d assistantToolDeps, store *documentStore, w savedWatch, now time.Time, loc *time.Location) (watchReportInput, error) {
	in := watchReportInput{Now: now, Loc: loc}

	var prev watchRun
	var havePrev bool
	if err := store.Read(func(q sqlQueryer) error {
		var err error
		prev, havePrev, err = lastRanRunQ(q, w.ID)
		return err
	}); err != nil {
		return in, fmt.Errorf("read the previous run: %w", err)
	}
	switch {
	case havePrev:
		in.Since, in.PreviousAt = prev.StartedAt, prev.StartedAt
		if prev.ReportKind == watchReportKindSections && len(prev.Report) > 0 {
			var report watchReport
			if err := json.Unmarshal(prev.Report, &report); err != nil {
				return in, fmt.Errorf("the previous report is unreadable: %w", err)
			}
			in.Previous = &report
		}
	default:
		in.FirstRun = true
		in.Since = now.Add(-watchFirstRunWindow)
	}

	wants := map[string]bool{}
	for _, s := range w.Sections {
		wants[s] = true
	}

	if wants[watchSectionPower] || wants[watchSectionAnchor] || wants[watchSectionTanks] || wants[watchSectionWeather] || wants[watchSectionInstruments] || wants[watchSectionAlarms] {
		if missing := d.vesselStatusMissingDeps(); len(missing) > 0 {
			in.StatusProblem = "the boat's readings are not wired (" + strings.Join(missing, ", ") + ")"
		} else if status, err := d.buildVesselStatus(context.Background()); err != nil {
			in.StatusProblem = firstErrorLine(err)
		} else {
			in.Status = &status
		}
	}

	if wants[watchSectionAlarms] {
		in.Raised, in.RaisedProblem = watchRaisedAlarms(d, in.Since, now)
	}
	if wants[watchSectionWeather] && in.Status != nil {
		src := resolveWatchForecast(d)
		in.Stay = watchStayForecastFor(src, in.Status.PlannedStay, now)
		in.WindChanges = watchWindChangesFor(src, in.Status, now)
	}
	// The learned normal's suggestions belong with the instruments. A watch that
	// does not check them does not pay for the Influx reads.
	if wants[watchSectionInstruments] && d.unusualReadings != nil {
		// Slow InfluxDB must not hold the report: scoring gets a budget, and when
		// it runs out the block says so and the rest of the report goes on.
		// The budget is taken from the run's own context, so a shutdown ends it too.
		budget, cancel := context.WithTimeout(ctx, watchUnusualBudget)
		budget = withUnusualBudget(budget, watchUnusualBudget)
		res := scoreWithAnswers(d, budget, in.Since, now)
		// A result that came back whole is kept, even if the deadline passed as it
		// did. Only one that failed for want of time is replaced.
		// Each subsystem words its own cut-short failure (scoreUnusualAll).
		cancel()
		// A boat with no InfluxDB has nothing to say here: the block stays out of
		// the report, and Settings says why. A configured InfluxDB that fails
		// still shows its error.
		if res.ProblemKind != unusualNoInflux {
			in.Unusual = &res
		}
	}
	return in, nil
}

// scoreWithAnswers scores the window with the operator's answers. Answers that
// are not wired or cannot be read are the reason nothing was scored, so a
// stretch the operator already called normal is never shown again.
func scoreWithAnswers(d assistantToolDeps, ctx context.Context, since, until time.Time) unusualResult {
	if d.unusualAnswers == nil {
		return unusualResult{Since: since, Until: until, ProblemKind: unusualError, Problem: "the operator's answers are not wired"}
	}
	answers, err := d.unusualAnswers()
	if err != nil {
		return unusualResult{Since: since, Until: until, ProblemKind: unusualError, Problem: "the answers about unusual stretches could not be read: " + firstErrorLine(err)}
	}
	return d.unusualReadings(ctx, since, until, unusualSubject{}, answers)
}

// watchRaisedAlarms counts the alarms raised in [since, now] by title from the
// alarm log. A log that is not open is a reason, never an empty list.
func watchRaisedAlarms(d assistantToolDeps, since, now time.Time) ([]alarmRaisedGroup, string) {
	if d.alarmLog == nil || d.alarmLog() == nil {
		return nil, "the alarm log is not open"
	}
	groups, err := d.alarmLog().RaisedSummary(since, now)
	if err != nil {
		return nil, firstErrorLine(err)
	}
	return groups, ""
}

// watchForecastSource is what the weather section reads a forecast with: the
// operator's limits and the configured provider, resolved once per report.
// problem says why one is missing, and is empty when both were found.
type watchForecastSource struct {
	limits     passageLimits
	provider   weatherProvider
	providerID string
	problem    string
	// limitsErr is why the passage limits could not be read. The provider is
	// still resolved: the forecast does not need limits to be read, only to be
	// judged.
	limitsErr error
}

// stayProblem is why the stay's forecast cannot be judged: it needs both the
// forecast and the limits.
func (s watchForecastSource) stayProblem() string {
	if s.problem != "" {
		return s.problem
	}
	if s.limitsErr != nil {
		return firstErrorLine(s.limitsErr)
	}
	return ""
}

func resolveWatchForecast(d assistantToolDeps) watchForecastSource {
	var src watchForecastSource
	if d.passageLimits == nil || d.weather == nil {
		src.problem = "the forecast is not wired"
		return src
	}
	var err error
	src.limits, src.limitsErr = d.passageLimits()
	if src.provider, src.providerID, err = d.weather(); err != nil {
		src.problem = firstErrorLine(err)
	}
	return src
}

// fetch reads the forecast at a place. whose names the place's owner in the
// time-zone problem: "the stay's" or "the boat's".
func (s watchForecastSource) fetch(lat, lon float64, whose string) (bundle weatherForecastBundle, zone placeZone, problem string) {
	zone, err := zoneAt(lat, lon)
	if err != nil {
		return bundle, zone, "the " + whose + " time zone is not known: " + firstErrorLine(err)
	}
	bundle, err = s.provider.FetchForecast(lat, lon, assistantMaxForecastDays, zone.Name)
	if err != nil {
		return bundle, zone, fmt.Sprintf("weather provider %q: %s", s.providerID, firstErrorLine(err))
	}
	return bundle, zone, ""
}

// watchStayForecastFor reads the forecast at the planned stay for its dates.
func watchStayForecastFor(src watchForecastSource, stay vesselStatusStay, now time.Time) watchStayForecast {
	if stay.Missing != "" || stay.Name == "" {
		return watchStayForecast{}
	}
	out := watchStayForecast{Name: stay.Name, NoGoKn: lowestNoGoWind(src.limits)}
	if p := src.stayProblem(); p != "" {
		out.Problem = p
		return out
	}
	// Dates first: a stay with unreadable dates is said without a provider call.
	_, err1 := time.Parse("2006-01-02", stay.Arrive)
	_, err2 := time.Parse("2006-01-02", stay.Depart)
	if err1 != nil || err2 != nil {
		out.Problem = "the stay's dates are unreadable"
		return out
	}
	bundle, stayZone, problem := src.fetch(stay.Lat, stay.Lon, "stay's")
	if problem != "" {
		out.Problem = problem
		return out
	}
	loc := stayZone.Loc
	arrive, _ := time.ParseInLocation("2006-01-02", stay.Arrive, loc)
	depart, _ := time.ParseInLocation("2006-01-02", stay.Depart, loc)
	end := depart.AddDate(0, 0, 1)
	found := false
	for _, hp := range bundle.Hourly {
		if hp.Time.IsZero() || hp.Time.Before(arrive) || !hp.Time.Before(end) || hp.Time.Before(now.Add(-time.Hour)) {
			continue
		}
		kn := hp.WindSpeedMS * metersPerSecondToKnots
		if !found || kn > out.MaxWindKn {
			out.MaxWindKn, out.MaxWindDay, found = kn, hp.Time.In(loc).Format("Mon 2 Jan"), true
		}
	}
	if !found {
		out.Problem = "the forecast does not reach the stay's dates yet"
	}
	return out
}

// watchWindChangesFor reads the forecast at the boat's own position, taken
// from the status the report already built, and finds the wind changes in it
// (ADR 0287). One fetch at the boat: a planned stay is a different place.
func watchWindChangesFor(src watchForecastSource, status *vesselStatusResult, now time.Time) watchWindChanges {
	if src.problem != "" {
		return watchWindChanges{Problem: src.problem}
	}
	if status.Position.Missing != "" {
		return watchWindChanges{Problem: status.Position.Missing}
	}
	bundle, zone, problem := src.fetch(status.Position.Lat, status.Position.Lon, "boat's")
	if problem != "" {
		return watchWindChanges{Problem: problem}
	}
	scan := forecastWindChanges(bundle.Hourly, lastDayStart(bundle.Days), now, zone.Loc, src.limits, src.limitsErr)
	return watchWindChanges{Changes: scan.Changes, LimitsProblem: scan.Problem, CheckedTo: scan.CheckedTo, CheckedReason: scan.CheckedReason}
}
