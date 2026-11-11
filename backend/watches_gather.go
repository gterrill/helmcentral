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
		in.Stay = watchStayForecastFor(d, in.Status.PlannedStay, now)
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

// watchLowestNoGoWind is the smallest no-go wind the operator has set in any
// sector, in knots, 0 when none is set. A planned stay has no course, so the
// lowest limit is the cautious reading.
func watchLowestNoGoWind(l passageLimits) float64 {
	lowest := 0.0
	for _, s := range []passageSectorLimits{l.Ahead, l.Abeam, l.Astern} {
		if s.WindKnNoGo != nil && (lowest == 0 || *s.WindKnNoGo < lowest) {
			lowest = *s.WindKnNoGo
		}
	}
	return lowest
}

// watchStayForecastFor reads the forecast at the planned stay for its dates.
func watchStayForecastFor(d assistantToolDeps, stay vesselStatusStay, now time.Time) watchStayForecast {
	if stay.Missing != "" || stay.Name == "" {
		return watchStayForecast{}
	}
	out := watchStayForecast{Name: stay.Name}
	if d.passageLimits == nil || d.weather == nil {
		out.Problem = "the forecast is not wired"
		return out
	}
	limits, err := d.passageLimits()
	if err != nil {
		out.Problem = firstErrorLine(err)
		return out
	}
	out.NoGoKn = watchLowestNoGoWind(limits)
	provider, providerID, err := d.weather()
	if err != nil {
		out.Problem = firstErrorLine(err)
		return out
	}
	stayZone, err := zoneAt(stay.Lat, stay.Lon)
	if err != nil {
		out.Problem = "the stay's time zone is not known: " + firstErrorLine(err)
		return out
	}
	loc := stayZone.Loc
	arrive, err1 := time.ParseInLocation("2006-01-02", stay.Arrive, loc)
	depart, err2 := time.ParseInLocation("2006-01-02", stay.Depart, loc)
	if err1 != nil || err2 != nil {
		out.Problem = "the stay's dates are unreadable"
		return out
	}
	end := depart.AddDate(0, 0, 1)
	bundle, err := provider.FetchForecast(stay.Lat, stay.Lon, assistantMaxForecastDays, stayZone.Name)
	if err != nil {
		out.Problem = fmt.Sprintf("weather provider %q: %s", providerID, firstErrorLine(err))
		return out
	}
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
