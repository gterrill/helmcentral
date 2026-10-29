package main

import (
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

func gatherWatchReportInput(d assistantToolDeps, store *documentStore, w savedWatch, now time.Time, loc *time.Location) (watchReportInput, error) {
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
		} else if status, err := d.buildVesselStatus(); err != nil {
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
	return in, nil
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
	loc := vesselLocalLocation(stay.Lon)
	arrive, err1 := time.ParseInLocation("2006-01-02", stay.Arrive, loc)
	depart, err2 := time.ParseInLocation("2006-01-02", stay.Depart, loc)
	if err1 != nil || err2 != nil {
		out.Problem = "the stay's dates are unreadable"
		return out
	}
	end := depart.AddDate(0, 0, 1)
	bundle, err := provider.FetchForecast(stay.Lat, stay.Lon, assistantMaxForecastDays, vesselLocalTimezoneName(stay.Lon))
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
