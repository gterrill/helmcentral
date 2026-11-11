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
	ID string `json:"id"`
	// Subject is the engine or the house bank, as the operator names it.
	Subject   string   `json:"subject"`
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
	// OperatorAnswer is what the operator said about the stretch: real, normal or
	// unknown. Set by the tool, not by describeUnusualStretch.
	OperatorAnswer string `json:"operator_answer,omitempty"`
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
	sub, ok := normalSubsystemNamed(st.Subsystem)
	if !ok {
		return unusualStretchView{}, fmt.Errorf("stretch %s is of %q, which is not learned", st.ID, st.Subsystem)
	}
	band := sub.Wording.BandPrefix + st.Band
	v := unusualStretchView{
		ID: st.ID, Subject: st.SubjectName, Minutes: st.Minutes, Band: st.Band,
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
		v.Summary = fmt.Sprintf("%s's readings were outside their usual pattern from %s to %s %s.", st.SubjectName, from, to, band)
		return v, nil
	}
	// A feature that names itself in full ("House bank voltage") is not worded
	// from the subject and its path.
	label := top.Name
	reading := top.Name
	if reading == "" {
		reading = sensorQuantity(top.Path)
		if reading == "" {
			reading = top.Key
		}
		label = sensorLabel(st.SubjectName, reading)
	}
	unit, conv, err := describeDisplayFor(top.Path, top.SIUnit, system)
	if err != nil {
		return unusualStretchView{}, fmt.Errorf("%s cannot be shown in the operator's units: %w", reading, err)
	}
	shown := func(x float64) float64 { return roundTo(conv(x), statusDecimals(unit)) }
	value, usual := shown(top.StretchMean), shown(top.Usual)
	v.Reading, v.Direction, v.Unit, v.Value, v.Usual = reading, top.Direction, unit, &value, &usual
	v.Summary = fmt.Sprintf("%s ran %s from %s to %s: %s %s against a usual %s %s %s.",
		label, top.Direction, from, to,
		strconv.FormatFloat(value, 'f', -1, 64), unit, strconv.FormatFloat(usual, 'f', -1, 64), unit, band)
	return v, nil
}
