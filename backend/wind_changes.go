package main

import (
	"math"
	"time"
)

// A wind change is one settled wind giving way to another (ADR 0287). A settled
// wind holds for windChangeHoldHours within windSplitDeg of its own mean. Each
// settled wind is measured against the anchor, the wind the last change ended
// on (the first settled wind to begin with), and one that blows windChangeDeg or
// more away from the anchor is a change. Up to windChangeGapHours of light,
// variable or unsettled air may sit between the settled wind just before it
// (whatever its direction) and the new wind and count as part of the change,
// which is dated from when the new wind sets in. "From" names the anchor, the
// wind that set the pattern. A wind that stays within 90 degrees of the anchor
// is a drift, however many small steps it took, and is not a new settled wind.
//
// The detector runs the day summary's segmentation (splitWindSegs and
// markVariableHours, ADR 0266) over the whole forecast horizon in one pass, so
// a change at 2 PM on day six is found the same way as one this evening, and
// one that straddles midnight is not cut in two. It names the change by compass
// points and time only. It does not say "front": the weather providers carry no
// hourly pressure, and without pressure a trough and rise cannot be shown.
const (
	windChangeHoldHours = 6
	// windChangeGapHours is how much air that is not a settled wind may lie
	// before the new wind, counted from the end of the settled wind just before
	// it.
	windChangeGapHours = 12
	// windChangeSpeedHours is how long into the new wind its speeds are read.
	windChangeSpeedHours = 12
	// windChangeLowerConfidenceAfter is how far ahead a change is still
	// firm. Beyond it the label says so.
	windChangeLowerConfidenceAfter = 72 * time.Hour
)

// windChangePoint is one forecast hour. SpeedKts and DirDeg are -1 when the
// provider gave nothing.
type windChangePoint struct {
	Time     time.Time
	SpeedKts float64
	DirDeg   float64
}

// windChange is one change in the forecast.
type windChange struct {
	At              time.Time
	DayKey          string // the boat-local date of At
	Label           string // "2 PM", the boat clock
	DayLabel        string // "today", "tomorrow" or "Tue 17", as of the run that made it: the watch report's wording
	From, To        string // compass points
	SpeedMinKn      int    // the wind after the change, over the hours it holds
	SpeedMaxKn      int
	LowerConfidence bool // more than three days out
	Prominent       bool // the wind after reaches the operator's lowest marginal wind
}

// windChangeDayLabel names the day of a change on the boat's calendar: "today",
// "tomorrow", or the weekday and date as "Tue 17", as of now. The watch report
// uses it, since a report is written once, at its run. The forecast drawer is
// held by the client for up to an hour, so it words the day itself.
func windChangeDayLabel(at, now time.Time, loc *time.Location) string {
	day := func(t time.Time) time.Time {
		y, m, d := t.In(loc).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	}
	switch int(day(at).Sub(day(now)).Hours() / 24) {
	case 0:
		return "today"
	case 1:
		return "tomorrow"
	}
	return at.In(loc).Format("Mon 2")
}

// Why the hourly series stopped before the forecast did.
const (
	windChangeCutSkipsHour    = "the forecast skips an hour here"
	windChangeCutNotHourly    = "the forecast is no longer hourly"
	windChangeSkipMaxGapHours = 2
)

// hourlyStretch maps a provider's hours to detector points, up to the first
// break in the hourly series: a gap of more than an hour, or an entry with no
// time. cut says why it stopped, and is empty when the series ran to its end. A
// gap of up to two hours, and an entry with no time, read as a skipped hour; a
// longer gap reads as the forecast no longer being hourly.
func hourlyStretch(hourly []weatherHourPoint) (kept []windChangePoint, cut string) {
	kept = make([]windChangePoint, 0, len(hourly))
	for _, hp := range hourly {
		if hp.Time.IsZero() {
			if len(kept) == 0 {
				continue // nothing read yet, so nothing is hidden
			}
			return kept, windChangeCutSkipsHour
		}
		if n := len(kept); n > 0 {
			gap := hp.Time.Sub(kept[n-1].Time)
			if gap > windChangeSkipMaxGapHours*time.Hour {
				return kept, windChangeCutNotHourly
			}
			if gap > time.Hour {
				return kept, windChangeCutSkipsHour
			}
		}
		_, deg := sentinelDirection(hp.WindDirectionDeg)
		kept = append(kept, windChangePoint{Time: hp.Time, SpeedKts: sentinelSpeedKts(hp.WindSpeedMS), DirDeg: deg})
	}
	return kept, ""
}

// lastDayStart is the start of the forecast's last day, zero when it has none.
func lastDayStart(days []weatherDayPoint) time.Time {
	var last time.Time
	for _, d := range days {
		if d.Start.After(last) {
			last = d.Start
		}
	}
	return last
}

// lowestMarginalWind is the smallest marginal wind across the sectors the
// operator has a no-go for, 0 when none is set. Marginal is derived from no-go
// (marginalOf), so the lowest marginal comes from the lowest no-go.
func lowestMarginalWind(l passageLimits) float64 {
	noGo := lowestNoGoWind(l)
	if noGo == 0 {
		return 0
	}
	return marginalOf(noGo)
}

// windChangeScan is what reading a forecast for changes gave.
type windChangeScan struct {
	Changes []windChange
	// Problem says why the changes were not judged against the operator's limits;
	// empty when they were.
	Problem string
	// CheckedTo is the last hour read, on the boat's clock ("Thu 19 3 AM"), and
	// CheckedReason says why the reading stopped there. Both are empty when the
	// whole forecast was read hourly. They are set when the series stops being
	// hourly before the forecast ends, since the detector counts hours and a
	// coarse tail cannot be read as hours.
	CheckedTo     string
	CheckedReason string
}

// forecastWindChanges is the one place the changes in an hourly forecast are
// judged against the operator's limits. When the limits could not be read
// nothing is marked prominent, and Problem says why. Only the hourly stretch of
// the forecast is read: where it stops being hourly the scan stops, and
// CheckedTo and CheckedReason say where and why. A series that simply ends is
// reported when it ends on an earlier day than lastDaily, the start of the
// forecast's last day (zero when unknown).
func forecastWindChanges(hourly []weatherHourPoint, lastDaily time.Time, now time.Time, loc *time.Location, limits passageLimits, limitsErr error) windChangeScan {
	var scan windChangeScan
	prominentKn := 0.0
	if limitsErr != nil {
		scan.Problem = firstErrorLine(limitsErr)
	} else {
		prominentKn = lowestMarginalWind(limits)
	}
	kept, cut := hourlyStretch(hourly)
	if len(kept) == 0 {
		return scan
	}
	scan.Changes = detectWindChanges(kept, now, loc, prominentKn)
	lastRead := kept[len(kept)-1].Time.In(loc)
	if cut == "" && !lastDaily.IsZero() && lastRead.Format("2006-01-02") < lastDaily.In(loc).Format("2006-01-02") {
		cut = windChangeCutNotHourly
	}
	if cut != "" {
		scan.CheckedTo = lastRead.Format("Mon 2") + " " + boatAxisClock(lastRead, loc)
		scan.CheckedReason = cut
	}
	return scan
}

// settledWind is a wind that held: a period of the day summary's segmentation
// that covers windChangeHoldHours at a real speed. first and last index the
// hourly series.
type settledWind struct {
	first, last int
	mean        float64
}

// windSeriesOf turns detector points into the hourly series the day summary
// reads. The hour of day is the boat's clock, so a gap across midnight reads
// as one hour.
func windSeriesOf(points []windChangePoint, loc *time.Location) []weatherHourlyWindData {
	out := make([]weatherHourlyWindData, len(points))
	for i, p := range points {
		e := weatherHourlyWindData{
			Label:            boatAxisClock(p.Time, loc),
			HourOfDay:        p.Time.In(loc).Hour(),
			WindSpeedKts:     p.SpeedKts,
			WindGustKts:      p.SpeedKts,
			WindDirection:    "—",
			WindDirectionDeg: p.DirDeg,
		}
		if p.DirDeg >= 0 {
			e.WindDirection = degreesToDirection(p.DirDeg)
		}
		out[i] = e
	}
	return out
}

// settledWinds lists the periods that held, in time order. A period holds when
// its hours at or above windLightKts cover windChangeHoldHours; light hours
// inside it neither break it nor count towards it. step is the spacing of the
// series in hours.
func settledWinds(segs []windSeg, series []weatherHourlyWindData, step int) []settledWind {
	var out []settledWind
	for _, s := range segs {
		if s.kind != windPeriodSeg || s.period == nil || !s.period.hasDir() {
			continue
		}
		firm := 0
		for _, i := range s.period.hours {
			if series[i].WindSpeedKts >= windLightKts {
				firm++
			}
		}
		if firm*step < windChangeHoldHours {
			continue
		}
		out = append(out, settledWind{first: s.period.first(), last: s.period.last(), mean: s.period.mean})
	}
	return out
}

// windOnset is the hour the wind after a change set in. It starts at the first
// hour of the settled wind and reaches back over the firm hours just before it
// that were already turning towards it (within windChangeDeg of it), but never
// past the hour the old wind ended, so "turning south and settling south-east"
// is dated from the south.
func windOnset(series []weatherHourlyWindData, from, to settledWind) int {
	onset := to.first
	for k := to.first - 1; k > from.last; k-- {
		e := series[k]
		if e.WindSpeedKts < windLightKts || !windDirectionKnown(e) || angleBetween(to.mean, e.WindDirectionDeg) >= windChangeDeg {
			break
		}
		onset = k
	}
	return onset
}

// lastHourOfWind is the last hour before onset that still blew from a settled
// wind's direction, at any speed above a calm, or the wind's own last hour. It
// carries the end of a settled wind across the light-air wobbles that follow it.
func lastHourOfWind(series []weatherHourlyWindData, from settledWind, onset int) int {
	for k := onset - 1; k > from.last; k-- {
		e := series[k]
		if e.WindSpeedKts >= windCalmKts && windDirectionKnown(e) && angleBetween(from.mean, e.WindDirectionDeg) < windSplitDeg {
			return k
		}
	}
	return from.last
}

// detectWindChanges finds the changes in an hourly series that start at or
// after now. The series must be hourly with no gaps (hourlyStretch). It reads
// the series in one pass across midnight and finds the winds that settled. Each
// settled wind is measured against the anchor: the first settled wind, then the
// wind each change ended on. One that blows windChangeDeg or more from the
// anchor is a change, provided no more than windChangeGapHours of other air lies
// between the end of the settled wind just before it and it. A wind that does not hold, such as a
// burst that comes straight back, is not settled and so is not a change, and a
// new wind needs the forecast to reach windChangeHoldHours past where it set
// in. prominentKn is the operator's lowest marginal wind, 0 when none is set,
// in which case nothing is prominent.
func detectWindChanges(points []windChangePoint, now time.Time, loc *time.Location, prominentKn float64) []windChange {
	series := windSeriesOf(points, loc)
	day := newWindDay(series)
	segs := splitWindSegs(series)
	markVariableHours(segs, day)
	settled := settledWinds(segs, series, day.step)

	// The hour that has begun is not past: now cut to the start of the boat's hour.
	local := now.In(loc)
	hourStart := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, loc)

	var out []windChange
	if len(settled) == 0 {
		return out
	}
	anchor := settled[0]
	for k := 1; k < len(settled); k++ {
		to := settled[k]
		if angleBetween(anchor.mean, to.mean) < windChangeDeg {
			continue
		}
		before := settled[k-1]
		// A wind that leads on from the settled wind just before it, within the
		// split angle, is that wind carrying on after a lull. The turn came
		// earlier, by steps that were each a drift, and it is not a change now.
		if angleBetween(before.mean, to.mean) < windSplitDeg {
			continue
		}
		from := anchor
		anchor = to // whether or not it is reported, this is the wind now
		// The onset never reaches back into the settled wind just before.
		onset := windOnset(series, before, to)
		// The gap is measured from where the settled wind just before this one
		// ended, whatever its direction: a drift under 90 degrees is not a new
		// settled wind, but it is not a gap in the air either.
		seen := lastHourOfWind(series, before, onset)
		if points[onset].Time.Sub(points[seen].Time) > (windChangeGapHours+1)*time.Hour {
			continue // too long between them for one wind to give way to the other
		}
		at := points[onset].Time
		if at.Before(hourStart) {
			continue
		}
		// The wind after is read over its first windChangeSpeedHours: the
		// direction it blew from, and the speeds from lowest to highest.
		lo, hi := math.MaxFloat64, 0.0
		var degs []float64
		for i := onset; i <= to.last && points[i].Time.Sub(at) < windChangeSpeedHours*time.Hour; i++ {
			e := series[i]
			if e.WindSpeedKts < 0 {
				continue
			}
			lo, hi = math.Min(lo, e.WindSpeedKts), math.Max(hi, e.WindSpeedKts)
			if e.WindSpeedKts >= windCalmKts && windDirectionKnown(e) {
				degs = append(degs, e.WindDirectionDeg)
			}
		}
		after, ok := meanOfDegs(degs...)
		if !ok {
			after = to.mean
		}
		out = append(out, windChange{
			At:              at,
			DayKey:          at.In(loc).Format("2006-01-02"),
			Label:           boatAxisClock(at, loc),
			DayLabel:        windChangeDayLabel(at, now, loc),
			From:            degreesToDirection(from.mean),
			To:              degreesToDirection(after),
			SpeedMinKn:      int(math.Round(lo)),
			SpeedMaxKn:      int(math.Round(hi)),
			LowerConfidence: at.Sub(now) > windChangeLowerConfidenceAfter,
			// Judged on the speed shown, as the plan table does: 19.5 kn reads as
			// 20 kn and is at a 20 kn marginal wind.
			Prominent: prominentKn > 0 && math.Round(hi) >= prominentKn,
		})
	}
	return out
}

// windChangesByDay groups changes by boat-local day key.
func windChangesByDay(changes []windChange) map[string][]windChange {
	out := map[string][]windChange{}
	for _, c := range changes {
		out[c.DayKey] = append(out[c.DayKey], c)
	}
	return out
}

// windChangeResponse is a change on the wire. It names no day: it sits under
// its day in the response, and the client words today and tomorrow when it
// draws, because it may hold the response for an hour.
type windChangeResponse struct {
	Label           string `json:"label"`
	From            string `json:"from"`
	To              string `json:"to"`
	SpeedMinKn      int    `json:"speed_min_kn"`
	SpeedMaxKn      int    `json:"speed_max_kn"`
	LowerConfidence bool   `json:"lower_confidence"`
	Prominent       bool   `json:"prominent"`
}

// mapWindChangeResponses is never nil, so the wire carries [] and not null.
func mapWindChangeResponses(changes []windChange) []windChangeResponse {
	out := make([]windChangeResponse, 0, len(changes))
	for _, c := range changes {
		out = append(out, windChangeResponse{
			Label: c.Label,
			From:  c.From, To: c.To, SpeedMinKn: c.SpeedMinKn, SpeedMaxKn: c.SpeedMaxKn,
			LowerConfidence: c.LowerConfidence, Prominent: c.Prominent,
		})
	}
	return out
}
