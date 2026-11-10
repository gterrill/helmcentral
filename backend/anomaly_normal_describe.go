package main

import (
	"fmt"
	"strconv"
	"time"
)

// unusualStretchView is a stretch as the watch report and the Mate tool say it:
// boat-time labels with the UTC instants beside them, and the figures in the
// operator's units.
type unusualStretchView struct {
	ID        string   `json:"id"`
	Engine    string   `json:"engine"`
	From      string   `json:"from"`
	To        string   `json:"to"`
	FromISO   string   `json:"from_iso"`
	ToISO     string   `json:"to_iso"`
	Minutes   int      `json:"minutes"`
	Reading   string   `json:"reading,omitempty"`
	Value     *float64 `json:"value,omitempty"`
	Usual     *float64 `json:"usual,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Band      string   `json:"band"`
	Summary   string   `json:"summary"`
	Direction string   `json:"direction,omitempty"`
}

// describeDisplayFor is how a reading is converted for the operator, a variable
// so a test can make the conversion fail.
var describeDisplayFor = chartDisplayFor

// describeUnusualStretch renders a stretch. The line names its times the way
// the Alarms section does (watchDayTime: no day for today, "yesterday", the
// weekday), and the end only when it falls on another day than the start. The
// labels always carry the day. A reading that cannot be converted to the
// operator's units is an error, never shown in SI.
func describeUnusualStretch(st unusualStretch, system string, loc *time.Location, now time.Time) (unusualStretchView, error) {
	if loc == nil {
		loc = time.UTC
	}
	v := unusualStretchView{
		ID: st.ID, Engine: st.SubjectName, Minutes: st.Minutes, Band: st.Band,
		From: boatDayClock(st.Start, loc), To: boatDayClock(st.End, loc),
		FromISO: st.Start.UTC().Format(time.RFC3339), ToISO: st.End.UTC().Format(time.RFC3339),
	}
	from := watchDayTime(st.Start, now, loc)
	to := watchDayTime(st.End, now, loc)
	if st.Start.In(loc).Format("2006-01-02") == st.End.In(loc).Format("2006-01-02") {
		to = boatClock(st.End, loc)
	}

	top := st.Top
	if top.Key == "" {
		v.Summary = fmt.Sprintf("%s's readings were outside their usual pattern from %s to %s at %s.", st.SubjectName, from, to, st.Band)
		return v, nil
	}
	reading := sensorQuantity(top.Path)
	if reading == "" {
		reading = top.Key
	}
	unit, conv, err := describeDisplayFor(top.Path, top.SIUnit, system)
	if err != nil {
		return unusualStretchView{}, fmt.Errorf("%s cannot be shown in the operator's units: %w", reading, err)
	}
	shown := func(x float64) float64 { return roundTo(conv(x), statusDecimals(unit)) }
	value, usual := shown(top.StretchMean), shown(top.Usual)
	v.Reading, v.Direction, v.Unit, v.Value, v.Usual = reading, top.Direction, unit, &value, &usual
	v.Summary = fmt.Sprintf("%s ran %s from %s to %s: %s %s against a usual %s %s at %s.",
		sensorLabel(st.SubjectName, reading), top.Direction, from, to,
		strconv.FormatFloat(value, 'f', -1, 64), unit, strconv.FormatFloat(usual, 'f', -1, 64), unit, st.Band)
	return v, nil
}
