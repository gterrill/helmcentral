package main

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// The wind changes on a passage plan's rows (ADR 0287). The assessment works
// them out, so the page only draws them: the instants are exact here, and the
// judging against the passage limits happens in one place, forecastWindChanges.
//
// A leg reads the hourly forecast the assessment already fetched for it, at the
// sample nearest the middle of its route (the point the page's mid-route forecast
// was read at), and keeps the changes whose onset lies between its departure and
// its ETA. A lay-day night reads the forecast at the stop, from 18:00 on the lay
// day to 06:00 the day after, on the stop's clock.

const (
	// planNightStartHour and planNightEndHour bound a lay-day night on the clock
	// of the stop it is spent at.
	planNightStartHour = 18
	planNightEndHour   = 6
)

// planWindChange is one change on a row. Label is the boat clock of the place
// the row is at, "2 AM", with the weekday in front when the change falls on a
// day after the one the row starts on.
type planWindChange struct {
	Label string `json:"label"`
	windChangeFigures
	// FromSector and Sector say where the wind before and after the change blows
	// from against the leg's course: ahead, abeam or astern. Both are "" on a
	// night at a stop, which has no course.
	FromSector string `json:"from_sector"`
	Sector     string `json:"sector"`
}

// planWindChanges is a row's changes, earliest first, and what the server could
// not say about them. NotChecked holds the reason in words when the row's changes
// were not looked for or not looked for all the way: the forecast read for the
// row stops before the window ends, or the limits that judge a change are not set.
type planWindChanges struct {
	Changes    []planWindChange `json:"changes"`
	NotChecked string           `json:"not_checked"`
}

// limitsNotChecked is what a row says when the passage limits cannot judge its
// changes, so none were looked for. The reason is the limits' own: unset, or
// unreadable and why.
func (e planEnv) limitsNotChecked() string {
	if e.limitsErr != nil {
		return "Wind changes weren't checked because your passage limits couldn't be read: " + firstErrorLine(e.limitsErr)
	}
	return "Wind changes weren't checked because your passage limits aren't set (Settings → Mate → Passage limits)."
}

func noPlanWindChanges() planWindChanges { return planWindChanges{Changes: []planWindChange{}} }

// planNightWinds is the changes on one lay-day night at a stop.
type planNightWinds struct {
	Stop int    `json:"stop"`
	Date string `json:"date"`
	planWindChanges
}

// changeWindow is the span a row's changes are kept in. closed says whether a
// change at the very end instant is in (a leg's ETA) or not (the end of a day or
// of a night, which is the start of the next). fromOpen says a change at the very
// start instant is out: the arrival night starts at the ETA, which the leg owns.
type changeWindow struct {
	from, to time.Time
	closed   bool
	fromOpen bool
}

// legChangeWindow is the span a leg's changes are kept in: its departure to its
// ETA, both exact. With no ETA it is the departure to the end of the departure
// day, and with no departure the whole of that day. day is the start of the
// departure day at the departure stop; loc is that stop's zone.
func legChangeWindow(day, depart, arrive time.Time, loc *time.Location) changeWindow {
	y, m, d := day.In(loc).Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	end := time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	switch {
	case depart.IsZero():
		return changeWindow{from: start, to: end}
	case arrive.IsZero():
		return changeWindow{from: depart, to: end}
	}
	return changeWindow{from: depart, to: arrive, closed: true}
}

// nightChangeWindow is a lay-day night: 18:00 on the day to 06:00 the next, on
// loc's clock.
func nightChangeWindow(day time.Time, loc *time.Location) changeWindow {
	y, m, d := day.In(loc).Date()
	return changeWindow{
		from: time.Date(y, m, d, planNightStartHour, 0, 0, 0, loc),
		to:   time.Date(y, m, d+1, planNightEndHour, 0, 0, 0, loc),
	}
}

// passageHoursAsWeather turns a point's hours back into the series the wind
// change scan reads, earliest first. An hour the forecast has no wind direction
// for is not in the map, so the scan sees the hole and says where it stopped.
func passageHoursAsWeather(hours map[int64]passageHour) []weatherHourPoint {
	keys := make([]int64, 0, len(hours))
	for k := range hours {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	out := make([]weatherHourPoint, 0, len(keys))
	for _, k := range keys {
		h := hours[k]
		out = append(out, weatherHourPoint{Time: time.Unix(k, 0), WindSpeedMS: h.WindMS, WindDirectionDeg: h.WindDirDeg})
	}
	return out
}

// rowWindChanges is the changes in hourly that fall in win, judged against the
// operator's limits. day is the start of the day the row starts on, which a
// label names the weekday of when the change falls on another. course is the
// leg's course in degrees true, nil for a night at a stop, which has no course:
// its changes carry no sector and are prominent as the forecast marks them, at
// the lowest marginal wind. The caller has checked the limits can judge a change
// (planEnv.limitsProblem is empty); a row with no limits never gets here.
func rowWindChanges(hourly []weatherHourPoint, win changeWindow, day, now time.Time, loc *time.Location, limits passageLimits, limitsErr error, course *float64) planWindChanges {
	scan := forecastWindChanges(hourly, time.Time{}, now, loc, limits, limitsErr)
	out := noPlanWindChanges()
	// The last hourly stamp read covers the hour after it, for a leg's ETA as for
	// the end of a day or a night.
	covered := scan.ReadTo.Add(time.Hour)
	switch {
	case scan.ReadTo.IsZero():
		out.NotChecked = "No hourly wind forecast was read here, so its changes weren't checked."
	case win.to.After(covered):
		out.NotChecked = "Wind changes were only checked to " + boatDayClock(scan.ReadTo, loc)
		if scan.CheckedReason != "" {
			out.NotChecked += ": " + scan.CheckedReason
		} else {
			out.NotChecked += ", where the forecast ends"
		}
	}
	dayKey := day.In(loc).Format(planDateLayout)
	for _, c := range scan.Changes {
		if c.At.Before(win.from) || c.At.After(win.to) || (!win.closed && c.At.Equal(win.to)) || (win.fromOpen && c.At.Equal(win.from)) {
			continue
		}
		label := c.Label
		if c.DayKey != dayKey {
			label = c.At.In(loc).Format("Mon") + " " + label
		}
		figures := c.figures()
		row := planWindChange{Label: label, windChangeFigures: figures}
		if course != nil {
			row.FromSector = changeSector(*course, c.FromDeg)
			row.Sector = changeSector(*course, c.ToDeg)
			// Prominent as the verdict would call it: the top of the wind after, as
			// shown, reaches the marginal wind of the sector it blows from. A sector
			// with no wind no-go set has no marginal wind, and is never prominent.
			noGo := limits.sector(row.Sector).WindKnNoGo
			row.Prominent = noGo != nil && float64(c.SpeedMaxKn) >= marginalOf(*noGo)
		}
		out.Changes = append(out.Changes, row)
	}
	return out
}

// changeSector is the sector a wind blowing from windFromDeg has against a
// course, the way the verdicts count it (passageSector). The bearing is taken to
// the whole degree the forecast gives it, so a wind exactly 120 degrees off is
// astern whatever the mean's last decimals.
func changeSector(courseDeg, windFromDeg float64) string {
	return passageSector(angleBetween(courseDeg, math.Round(windFromDeg)))
}

// legWindChanges is a leg's changes, read from the forecast fetched for its
// verdict. It runs after the leg is judged, so a suggested departure is the
// departure and its ETA the arrival. The leg the boat is under way on is read from
// now to its ETA: its departure is behind it.
func legWindChanges(as *passageAssessor, w *planLegWork, env planEnv) planWindChanges {
	out := noPlanWindChanges()
	lat, lon, _ := w.plan.at(0.5)
	point, err := as.hoursAt(w.plan.nearestSample(lat, lon))
	if err != nil {
		out.NotChecked = err.Error()
		return out
	}
	var day, depart, arrive time.Time
	if w.underway {
		depart, arrive = env.now, w.eta
		if !arrive.After(depart) {
			out.NotChecked = "The leg is under way and its ETA has passed, so there is no span left to check."
			return out
		}
		y, m, d := env.now.In(w.fromLoc).Date()
		day = time.Date(y, m, d, 0, 0, 0, 0, w.fromLoc)
	} else {
		if day, err = dayStart(w.from.Depart, w.fromLoc); err != nil {
			out.NotChecked = "The departure date is not readable: " + err.Error()
			return out
		}
		for _, s := range []struct {
			text string
			into *time.Time
		}{{w.res.Depart, &depart}, {w.res.Arrive, &arrive}} {
			if s.text == "" {
				continue
			}
			if *s.into, err = time.Parse(time.RFC3339, s.text); err != nil {
				out.NotChecked = "A leg time is not readable: " + err.Error()
				return out
			}
		}
		if depart.IsZero() {
			arrive = time.Time{} // an ETA with no departure is no span
		}
	}
	course := w.course
	return rowWindChanges(passageHoursAsWeather(point.hours), legChangeWindow(day, depart, arrive, w.fromLoc),
		day, env.now, w.fromLoc, env.limits, env.limitsErr, &course)
}

// planNightWork is one lay-day night to read: the stop it is spent at, the
// stop's clock, the lay day, and why it cannot be read when it cannot.
type planNightWork struct {
	stop  int
	place planStop
	loc   *time.Location
	day   time.Time
	// date is the day the row for this night is keyed by.
	date string
	// win is the span of the night that is read.
	win changeWindow
	// problem is why the night cannot be read: the stop's zone or date is not known.
	problem string
}

// layDayNights lists the nights the plan table has a lay-day row for that are
// still ahead and inside the forecast's reach: from tonight to
// assistantMaxForecastDays days on. The table has a row for each lay day of the
// first stop, and at a later stop for those after the night of arrival, which
// sits under the passage row that arrives. A night whose end has passed is not
// listed, nor one the forecast does not reach yet.
func layDayNights(p plan, now time.Time) []planNightWork {
	var out []planNightWork
	for i, s := range p.Stops {
		firstNight := 1
		if i == 0 {
			firstNight = 0
		}
		if s.Nights <= firstNight {
			continue
		}
		zone, err := stayZoneAt(s.Lat, s.Lon)
		if err != nil {
			out = append(out, planNightWork{stop: i, place: s, date: s.Arrive, problem: "The place's time zone is not known: " + err.Error()})
			continue
		}
		arrive, err := dayStart(s.Arrive, zone.Loc)
		if err != nil {
			out = append(out, planNightWork{stop: i, place: s, date: s.Arrive, problem: "The arrival date at " + s.Name + " could not be read: " + s.Arrive})
			continue
		}
		today := now.In(zone.Loc).Format(planDateLayout)
		for n := firstNight; n < s.Nights; n++ {
			day := arrive.AddDate(0, 0, n)
			win := nightChangeWindow(day, zone.Loc)
			if !win.to.After(now) {
				continue
			}
			if day.AddDate(0, 0, -assistantMaxForecastDays).Format(planDateLayout) > today {
				continue
			}
			out = append(out, planNightWork{stop: i, place: s, loc: zone.Loc, day: day, date: day.Format(planDateLayout), win: win})
		}
	}
	return out
}

// holdsArrivalNight says whether the boat spends the night of its arrival at stop
// i of the plan: it stays there, or it is where the plan ends. A stop it leaves
// the same day has no night of arrival; the next leg's span covers it.
func holdsArrivalNight(p plan, i int) bool {
	return p.Stops[i].Nights >= 1 || i == len(p.Stops)-1
}

// planArrival is a leg's ETA at the stop it lands at.
type planArrival struct {
	stop int
	eta  time.Time
}

// arrivalNights lists the night of arrival at each stop a leg lands at, that is
// still ahead and inside the forecast's reach: from 18:00 on the ETA's day, or the
// ETA when it is later, to 06:00 the day after, on the stop's clock. The plan
// table shows it under the passage row that arrives, so it is keyed by the stop's
// plan arrive date, which no lay-day row uses.
func arrivalNights(p plan, arrivals []planArrival, now time.Time) []planNightWork {
	var out []planNightWork
	for _, a := range arrivals {
		s := p.Stops[a.stop]
		zone, err := stayZoneAt(s.Lat, s.Lon)
		if err != nil {
			out = append(out, planNightWork{stop: a.stop, place: s, date: s.Arrive, problem: "The place's time zone is not known: " + err.Error()})
			continue
		}
		y, m, d := a.eta.In(zone.Loc).Date()
		day := time.Date(y, m, d, 0, 0, 0, 0, zone.Loc)
		win := nightChangeWindow(day, zone.Loc)
		if a.eta.After(win.from) {
			win.from, win.fromOpen = a.eta, true
		}
		if !win.to.After(now) {
			continue
		}
		if day.AddDate(0, 0, -assistantMaxForecastDays).Format(planDateLayout) > now.In(zone.Loc).Format(planDateLayout) {
			continue
		}
		out = append(out, planNightWork{stop: a.stop, place: s, loc: zone.Loc, day: day, date: s.Arrive, win: win})
	}
	return out
}

// nightPoints are the distinct places the nights are read at, with the stops legs
// land at, whose nights of arrival are read too.
func nightPoints(nights []planNightWork, landings []planStop) []passageRoutePoint {
	var out []passageRoutePoint
	seen := map[string]bool{}
	add := func(s planStop) {
		pt := passageRoutePoint{Lat: s.Lat, Lon: s.Lon}
		if k := passagePointKey(pt); !seen[k] {
			seen[k] = true
			out = append(out, pt)
		}
	}
	for _, n := range nights {
		if n.problem == "" {
			add(n.place)
		}
	}
	for _, s := range landings {
		add(s)
	}
	return out
}

// stopForecasts is how the fetch of the stops' own forecasts went. err is why none
// was fetched (the source could not be set up, or the request ended); failed holds
// the stops whose fetch failed, by passagePointKey, while the rest were read.
type stopForecasts struct {
	err    error
	failed map[string]error
}

// nightWinds reads every night's changes. as is nil when the forecast could not
// be set up. A stop's forecast is whatever the assessor holds for it, from a leg's
// end or its own fetch; one that is not there says why on the nights at that stop
// alone. With the limits unset or unreadable nothing was fetched and every night
// says it was not checked.
func nightWinds(as *passageAssessor, stops stopForecasts, nights []planNightWork, env planEnv) []planNightWinds {
	out := make([]planNightWinds, 0, len(nights))
	for _, n := range nights {
		entry := planNightWinds{Stop: n.stop, Date: n.date, planWindChanges: noPlanWindChanges()}
		pt := passageRoutePoint{Lat: n.place.Lat, Lon: n.place.Lon}
		switch {
		case n.problem != "":
			entry.NotChecked = n.problem
		case env.limitsProblem != "":
			entry.NotChecked = env.limitsNotChecked()
		default:
			var point passagePointHours
			var err error
			if as != nil {
				point, err = as.hoursAt(pt)
			}
			switch {
			case as != nil && err == nil:
				entry.planWindChanges = rowWindChanges(passageHoursAsWeather(point.hours), n.win,
					n.day, env.now, n.loc, env.limits, env.limitsErr, nil)
			case stops.err != nil:
				entry.NotChecked = fmt.Sprintf("The forecast at %s could not be read: %v", n.place.Name, stops.err)
			case stops.failed[passagePointKey(pt)] != nil:
				entry.NotChecked = fmt.Sprintf("The forecast at %s could not be read: %v", n.place.Name, stops.failed[passagePointKey(pt)])
			case err != nil:
				entry.NotChecked = err.Error()
			default:
				entry.NotChecked = fmt.Sprintf("The forecast at %s was not set up", n.place.Name)
			}
		}
		out = append(out, entry)
	}
	return out
}

// unassessedWindChanges is what a leg says about its changes when no forecast was
// read for it: its changes were not checked because the limits cannot judge them,
// or because the leg was not assessed. A leg that is past, not routed or beyond the
// forecast has no wind to show, and says nothing, and nor does one the boat has
// done or passed by. underway is true for the leg the boat is on, whose past
// departure is no reason to say nothing when the limits are the problem.
func unassessedWindChanges(l planLegAssessment, env planEnv, underway bool) planWindChanges {
	out := noPlanWindChanges()
	if l.Progress != "" {
		return out
	}
	switch {
	case env.limitsProblem != "" && (l.Note == env.limitsProblem || underway):
		out.NotChecked = env.limitsNotChecked()
	case l.NoteKind == planKindPast, l.NoteKind == planKindNotRouted, l.NoteKind == planKindNoForecast, l.NoteKind == planKindNone:
	default:
		out.NotChecked = "The leg isn't assessed (" + l.Note + "), so its wind changes weren't read."
	}
	return out
}
