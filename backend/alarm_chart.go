package main

import (
	"context"
	"log"
	"strings"
	"time"
)

// Charts on alarm notifications (ADR 0235). A raised alarm whose rule watches a
// numeric reading carries one chart of that reading, as data, on the queued
// message. The picture is drawn when a channel sends it, the same as a watch
// report's (ADR 0228).

const alarmChartWindow = 6 * time.Hour

// alarmChartDeadline is how long a chart read may take, counted from when the
// alarm was queued (enqueue fixes the time). A variable only so a test need not
// wait three seconds.
var alarmChartDeadline = 3 * time.Second

// alarmChartChannels are the channels that can show a picture. Web push and
// SignalK do not, so an alarm for them alone never reads history.
var alarmChartChannels = map[string]bool{transportNtfy: true, transportWebhook: true, transportSMTP: true}

// alarmChartSpecFor says what to chart for an alarm event, and false when the
// event gets no chart: a clear or any other repeat, an alarm raised elsewhere
// on the network, one of Helmcentral's own checks (none of them watches a
// recorded reading), or a rule that watches for silence.
func alarmChartSpecFor(event alarmEvent, now time.Time) (*watchChartSpec, bool) {
	r := event.Rule
	// A raise, or a live alarm getting worse (a tank from warn to alarm), which
	// is the more urgent message. Easing back, a clear and a reminder that the
	// alarm is still unacknowledged are repeats of something already seen.
	worsened := event.Kind == alarmEventChanged && alarmStateRank[event.Status.State] > alarmStateRank[r.State]
	if (event.Kind != alarmEventRaised && !worsened) || event.Source == alarmSourceSignalK || isBuiltInAlarmCheck(r.ID) {
		return nil, false
	}
	if r.Path == "" || r.Op == alarmOpStale {
		return nil, false
	}
	spec := &watchChartSpec{Title: r.Label + ", last 6 hours", From: now.Add(-alarmChartWindow), To: now}
	series := watchChartSeriesSpec{Label: r.Label, Path: r.Path, Role: r.PathRole}
	if isTankDerivedAlarmRuleID(r.ID) {
		series = tankPercentSeries(tankNameOf(r.Label), r.Path)
		spec.Threshold = &watchChartThreshold{Label: tankWarnLevelLabel, Value: r.Value * 100}
		if r.AlarmLevelValue != nil {
			spec.AlarmThreshold = &watchChartThreshold{Label: tankAlarmLevelLabel, Value: *r.AlarmLevelValue * 100}
		}
	} else {
		series.Convert = "display"
		d, _ := alarmDisplayFor(r.Path, r.PathRole)
		label := "Limit"
		if word, ok := alarmStateWords[r.State]; ok {
			label = word + " limit"
		}
		spec.Threshold = &watchChartThreshold{Label: label, Value: d.toShown(r.Value)}
	}
	spec.Series = []watchChartSeriesSpec{series}
	return spec, true
}

// chartTooSlow is the reason given when the history is not there by the deadline.
const chartTooSlow = "the history did not answer in time"

// startChart begins reading the chart an alarm carries, at once and alongside
// whatever else the dispatcher is sending, and returns a function that waits
// for it until chartBy, the deadline fixed when the alarm was queued. The wait
// is what makes the deadline hard: the reader gets the context, but a reader
// that ignores it cannot hold the alarm past chartBy. The result is nil for an
// alarm that gets no chart, and one entry with Problem set and no series for
// one whose history could not be read in time.
func (d *alarmDispatcher) startChart(event alarmEvent, chartBy time.Time) func() []watchChart {
	now := d.now()
	spec, ok := alarmChartSpecFor(event, now)
	if !ok {
		return func() []watchChart { return nil }
	}
	leftOut := func(reason string) []watchChart {
		log.Printf("alarm notify: %q is sent without its chart: %s", event.Rule.Label, reason)
		return []watchChart{{Title: spec.Title, Problem: reason}}
	}
	if !time.Now().Before(chartBy) {
		return func() []watchChart { return leftOut(chartTooSlow) }
	}
	done := make(chan []watchChart, 1)
	go func() { done <- d.readChart(spec, now, chartBy, leftOut) }()
	return func() []watchChart {
		timer := time.NewTimer(time.Until(chartBy))
		defer timer.Stop()
		select {
		case c := <-done:
			return c
		case <-timer.C:
			return leftOut(chartTooSlow)
		}
	}
}

// readChart reads the history for spec, giving up at chartBy.
func (d *alarmDispatcher) readChart(spec *watchChartSpec, now, chartBy time.Time, leftOut func(string) []watchChart) []watchChart {
	if d.boatLocation == nil {
		return leftOut("the boat's clock is not available")
	}
	loc, err := d.boatLocation()
	if err != nil {
		return leftOut("the boat's clock zone is not known: " + firstErrorLine(err))
	}
	ctx, cancel := context.WithDeadline(context.Background(), chartBy)
	defer cancel()
	c, problem := resolveWatchChart(ctx, spec, d.chartSources, now, loc)
	switch {
	case ctx.Err() != nil:
		return leftOut(chartTooSlow)
	case problem != "":
		return leftOut(strings.TrimPrefix(problem, watchChartNoHistory))
	}
	return []watchChart{c}
}

// tankNameOf is a tank alarm's tank: its label is the tank's name and "Low" or
// "High" (tankDerivedAlarmRules).
func tankNameOf(label string) string {
	return strings.TrimSuffix(strings.TrimSuffix(label, " Low"), " High")
}
