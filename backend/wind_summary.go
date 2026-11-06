package main

import (
	"fmt"
	"math"
	"strings"
)

// The wind summary describes a day in periods, not as a first-to-last
// comparison. A period is a run of hours whose direction stays within
// windSplitDeg of the period's own mean. Hours under windCalmKts are calm and
// their direction means nothing; a calm always ends a group. A change is a
// turn of windChangeDeg or more that lasts more than one hour (ADR 0266).
const (
	windCalmKts           = 2.0
	windSplitDeg          = 45.0
	windChangeDeg         = 90.0
	windChangeWithinHours = 3
	windLightKts          = 5.0 // a variable stretch is light only if every hour is under this
)

// windDir is one hour's direction with its index into the hourly slice.
type windDir struct {
	idx int
	deg float64
}

// meanOfDegs is the circular mean of some directions in degrees. ok is false
// when they cancel, as 90 and 270 do, so no direction can be named.
func meanOfDegs(degs ...float64) (deg float64, ok bool) {
	var s, c float64
	for _, d := range degs {
		rad := d * math.Pi / 180
		s += math.Sin(rad)
		c += math.Cos(rad)
	}
	n := float64(len(degs))
	rad, _, ok := circularMean(s/n, c/n)
	return rad * 180 / math.Pi, ok
}

// windSpanDegs gives the direction at the start and at the end of a run of
// hours. A run of four or more hours uses the mean of its first two and of its
// last two; a shorter run uses its first and last hour. ok is false for an
// empty run or one whose ends cancel.
func windSpanDegs(dirs []windDir) (start, end float64, ok bool) {
	n := len(dirs)
	if n == 0 {
		return 0, 0, false
	}
	if n < 4 {
		return dirs[0].deg, dirs[n-1].deg, true
	}
	start, okStart := meanOfDegs(dirs[0].deg, dirs[1].deg)
	end, okEnd := meanOfDegs(dirs[n-2].deg, dirs[n-1].deg)
	return start, end, okStart && okEnd
}

// windPeriod is a run of non-calm hours sharing a direction. hours holds
// indexes into the hourly slice. mean and usable cache the circular mean of
// dirs, and a period whose directions cancel has no usable direction.
type windPeriod struct {
	hours  []int
	dirs   []windDir
	sumSin float64
	sumCos float64
	mean   float64
	usable bool
}

func (p *windPeriod) hasDir() bool { return p.usable }
func (p *windPeriod) first() int   { return p.hours[0] }
func (p *windPeriod) last() int    { return p.hours[len(p.hours)-1] }

func (p *windPeriod) add(idx int, deg float64, hasDir bool) {
	p.hours = append(p.hours, idx)
	if !hasDir {
		return
	}
	p.dirs = append(p.dirs, windDir{idx: idx, deg: deg})
	rad := deg * math.Pi / 180
	p.sumSin += math.Sin(rad)
	p.sumCos += math.Cos(rad)
	n := float64(len(p.dirs))
	mean, _, ok := circularMean(p.sumSin/n, p.sumCos/n)
	p.mean, p.usable = mean*180/math.Pi, ok
}

// windDay is the hourly series with its spacing. Hours between entries come
// from HourOfDay, so a provider that sends entries three hours apart is not
// read as hourly.
type windDay struct {
	hourly []weatherHourlyWindData
	step   int // the smallest gap between entries, in hours
}

func newWindDay(hourly []weatherHourlyWindData) windDay {
	d := windDay{hourly: hourly, step: 1}
	prev, step := -1, 0
	for i, e := range hourly {
		if e.WindSpeedKts < 0 {
			continue
		}
		if prev >= 0 {
			if gap := d.hoursBetween(prev, i); gap > 0 && (step == 0 || gap < step) {
				step = gap
			}
		}
		prev = i
	}
	if step > 0 {
		d.step = step
	}
	return d
}

// hoursBetween is the clock gap between two entries, across midnight too.
func (d windDay) hoursBetween(from, to int) int {
	gap := d.hourly[to].HourOfDay - d.hourly[from].HourOfDay
	if gap < 0 {
		gap += 24
	}
	return gap
}

// oneHour reports whether a period covers a single hour of the day.
func (d windDay) oneHour(p *windPeriod) bool {
	return len(p.hours) == 1 && d.step <= 1
}

type windSegKind int

const (
	windPeriodSeg windSegKind = iota
	windCalmSeg
	windVariableSeg
)

// windSeg is one stretch of the day in time order: a period (then, after
// grouping, a group), a calm spell or a variable hour. idx is the first hour
// of a calm spell or the hour of a variable one.
type windSeg struct {
	kind   windSegKind
	idx    int
	period *windPeriod
	group  *windGroup
	// continues marks a variable hour that its group carries on after.
	continues bool
}

// windBreak says why a group began.
type windBreak int

const (
	windOpening windBreak = iota // the first group of the day
	windResumed                  // the wind came back after a calm
	windChanged                  // it turned 90 degrees or more within three hours
	windLater                    // it turned 90 degrees or more over a longer time
)

// windGroup is a run of periods with no calm and no quick turn of 90 degrees
// or more from where the group started. extra holds variable hours that count
// for its speeds.
type windGroup struct {
	subs     []*windPeriod
	extra    []int
	how      windBreak
	start    float64 // the direction the group started in
	hasStart bool
}

func newWindGroup(p *windPeriod, how windBreak) *windGroup {
	g := &windGroup{subs: []*windPeriod{p}, how: how}
	g.start, _, g.hasStart = windSpanDegs(p.dirs)
	return g
}

func (g *windGroup) dirs() []windDir {
	var dirs []windDir
	for _, s := range g.subs {
		dirs = append(dirs, s.dirs...)
	}
	return dirs
}

// lastHourNearStart is the last hour still within 45 degrees of where the
// group started, so the length of a turn is measured from when it began.
func (g *windGroup) lastHourNearStart(fallback int) int {
	for i := len(g.subs) - 1; i >= 0; i-- {
		dirs := g.subs[i].dirs
		for k := len(dirs) - 1; k >= 0; k-- {
			if angleBetween(g.start, dirs[k].deg) < windSplitDeg {
				return dirs[k].idx
			}
		}
	}
	return fallback
}

func windDirectionKnown(e weatherHourlyWindData) bool {
	return e.WindDirectionDeg >= 0 && e.WindDirection != "" && e.WindDirection != "—"
}

// windSpeedPhrase gives the group's speeds as "3 to 5 kts". When the rounded
// ends match it gives "6 kts" inside a clause and "around 6 kts" for the
// steady one-period form.
func windSpeedPhrase(hourly []weatherHourlyWindData, g *windGroup, steady bool) string {
	lo, hi := math.MaxFloat64, -1.0
	note := func(i int) {
		lo = math.Min(lo, hourly[i].WindSpeedKts)
		hi = math.Max(hi, hourly[i].WindSpeedKts)
	}
	for _, s := range g.subs {
		for _, i := range s.hours {
			note(i)
		}
	}
	for _, i := range g.extra {
		note(i)
	}
	loR, hiR := int(math.Round(lo)), int(math.Round(hi))
	switch {
	case loR != hiR:
		return fmt.Sprintf("%d to %d kts", loR, hiR)
	case steady:
		return fmt.Sprintf("around %d kts", hiR)
	}
	return fmt.Sprintf("%d kts", hiR)
}

// windWords joins the words that are not empty.
func windWords(words ...string) string {
	var kept []string
	for _, w := range words {
		if w != "" {
			kept = append(kept, w)
		}
	}
	return strings.Join(kept, " ")
}

// timed returns "around 5 PM" for a label, or nothing when the label is empty.
func timed(preposition, label string) string {
	if label == "" {
		return ""
	}
	return preposition + " " + label
}

// splitWindSegs turns the hourly series into periods and calm spells.
func splitWindSegs(hourly []weatherHourlyWindData) []windSeg {
	var segs []windSeg
	var cur *windPeriod
	for i, e := range hourly {
		if e.WindSpeedKts < 0 {
			continue
		}
		if e.WindSpeedKts < windCalmKts {
			if len(segs) == 0 || segs[len(segs)-1].kind != windCalmSeg {
				segs = append(segs, windSeg{kind: windCalmSeg, idx: i})
			}
			cur = nil
			continue
		}
		known := windDirectionKnown(e)
		if cur != nil && known && cur.hasDir() && angleBetween(cur.mean, e.WindDirectionDeg) > windSplitDeg {
			cur = nil
		}
		if cur == nil {
			cur = &windPeriod{}
			segs = append(segs, windSeg{kind: windPeriodSeg, period: cur})
		}
		cur.add(i, e.WindDirectionDeg, known)
	}
	return segs
}

// nearestAnchor finds the closest period with a direction before (step -1) or
// after (step +1) segs[j] that covers more than one hour, looking past calm
// spells, variable hours and other one-hour swings.
func nearestAnchor(segs []windSeg, day windDay, j, step int) *windPeriod {
	for k := j + step; k >= 0 && k < len(segs); k += step {
		if segs[k].kind == windPeriodSeg && segs[k].period.hasDir() && !day.oneHour(segs[k].period) {
			return segs[k].period
		}
	}
	return nil
}

// markVariableHours turns a one-hour swing into a variable hour when it is 90
// degrees or more from the nearest period with a direction on each side, since
// a change has to last. Beside a calm it reads as light and variable, and with
// only one period around it, that one is enough. Back-to-back swings are each
// judged against the periods beyond them, so they form one stretch. A one-hour
// swing on the very first or last hour has nothing on one side and stays a
// change.
func markVariableHours(segs []windSeg, day windDay) {
	for j := 1; j < len(segs)-1; j++ {
		s := segs[j]
		if s.kind != windPeriodSeg || !day.oneHour(s.period) || !s.period.hasDir() {
			continue
		}
		variable, haveRef := true, false
		for _, ref := range []*windPeriod{nearestAnchor(segs, day, j, -1), nearestAnchor(segs, day, j, 1)} {
			if ref == nil {
				continue
			}
			haveRef = true
			if angleBetween(s.period.mean, ref.mean) < windChangeDeg {
				variable = false
			}
		}
		if haveRef && variable {
			segs[j] = windSeg{kind: windVariableSeg, idx: s.period.first()}
		}
	}
}

// groupWindSegs joins periods into groups, leaving calm spells and variable
// hours where they fall.
func groupWindSegs(segs []windSeg, day windDay) []windSeg {
	var out []windSeg
	var cur *windGroup
	var prev *windPeriod
	var pending []int // variable hours seen before any group, for the first
	var openVars []int // variable hours since the last period, as positions in out
	afterCalm := false
	for _, s := range segs {
		switch s.kind {
		case windCalmSeg:
			out = append(out, s)
			afterCalm = true
		case windVariableSeg:
			openVars = append(openVars, len(out))
			out = append(out, s)
			// A variable hour counts for the speeds of the group around it.
			if cur != nil {
				cur.extra = append(cur.extra, s.idx)
			} else {
				pending = append(pending, s.idx)
			}
		case windPeriodSeg:
			p := s.period
			bothDir := prev != nil && prev.hasDir() && p.hasDir()
			turned := bothDir && angleBetween(prev.mean, p.mean) >= windChangeDeg
			turnBreak := func(from int) windBreak {
				if day.hoursBetween(from, p.first()) <= windChangeWithinHours {
					return windChanged
				}
				return windLater
			}
			how, split := windOpening, false
			switch {
			case cur == nil:
				split = true
			case afterCalm || turned:
				split, how = true, windResumed
				if turned {
					how = turnBreak(prev.last())
				} else if start, _, ok := windSpanDegs(p.dirs); bothDir && ok && angleBetween(prev.mean, start) >= windSplitDeg {
					// The wind came back from somewhere else, so it is not "again".
					how = windLater
				}
			case bothDir && cur.hasStart && angleBetween(cur.start, p.mean) >= windChangeDeg:
				// A turn of 90 degrees from where the group started is a change
				// only when it came quickly. A slow drift stays in the group,
				// whose "X to Y" label already says where it went.
				if how = turnBreak(cur.lastHourNearStart(prev.last())); how == windChanged {
					split = true
				}
			}
			if split {
				cur = newWindGroup(p, how)
				cur.extra = pending
				pending = nil
				out = append(out, windSeg{kind: windPeriodSeg, group: cur})
			} else {
				cur.subs = append(cur.subs, p)
				for _, k := range openVars {
					out[k].continues = true
				}
			}
			openVars = nil
			afterCalm = false
			prev = p
		}
	}
	return out
}

// windDirSpan names a group's direction at its start and at its end.
type windDirSpan struct{ start, end string }

func (w windDirSpan) label() string {
	if w.start == w.end {
		return w.start
	}
	return w.start + " to " + w.end
}

func (g *windGroup) span() windDirSpan {
	start, end, ok := windSpanDegs(g.dirs())
	if !ok {
		return windDirSpan{}
	}
	return windDirSpan{degreesToDirection(start), degreesToDirection(end)}
}

// buildWindSummary describes a day's wind in periods, naming any change of
// direction with its time, derived from the hourly series so it stays
// consistent with the wind graph.
func buildWindSummary(hourly []weatherHourlyWindData) string {
	maxSpeed := -1.0
	maxGust := -1.0
	found := false
	for _, e := range hourly {
		if e.WindSpeedKts < 0 {
			continue
		}
		found = true
		maxSpeed = math.Max(maxSpeed, e.WindSpeedKts)
		maxGust = math.Max(maxGust, e.WindGustKts)
	}
	if !found {
		return ""
	}
	gustRounded := int(math.Round(maxGust))

	// The first hour whose rounded gust reaches the rounded peak names the
	// time, so a raw 24.4 later in the day does not take it from a 24.2.
	gustLabel := ""
	for _, e := range hourly {
		if e.WindSpeedKts >= 0 && int(math.Round(e.WindGustKts)) == gustRounded {
			gustLabel = e.Label
			break
		}
	}
	peak := windWords(fmt.Sprintf("%d kts", gustRounded), timed("around", gustLabel))
	gusty := gustRounded > int(math.Round(maxSpeed))

	day := newWindDay(hourly)
	segs := splitWindSegs(hourly)
	markVariableHours(segs, day)
	segs = groupWindSegs(segs, day)

	hasGroup := false
	for _, s := range segs {
		hasGroup = hasGroup || s.kind == windPeriodSeg
	}
	gustSentence := ""
	if gusty {
		gustSentence = " Gusts to " + peak + "."
	}
	if !hasGroup {
		return "Light winds all day." + gustSentence
	}

	if len(segs) == 1 {
		g := segs[0].group
		phrase := windSpeedPhrase(hourly, g, true)
		if dir := g.span().label(); dir != "" {
			phrase += " from the " + dir
		}
		if gusty {
			return fmt.Sprintf("Winds %s, gusting to %s.", phrase, peak)
		}
		return fmt.Sprintf("Winds %s.", phrase)
	}

	var clauses []string
	for i := 0; i < len(segs); {
		s := segs[i]
		if s.kind != windPeriodSeg {
			// One clause for a stretch of calm and variable hours. It is light
			// only when a calm is in it and every variable hour is under
			// windLightKts, and brief when its group carries on afterwards.
			hasCalm, firstVariable := false, -1
			light, briefly := true, false
			j := i
			for ; j < len(segs) && segs[j].kind != windPeriodSeg; j++ {
				switch segs[j].kind {
				case windCalmSeg:
					hasCalm = true
				case windVariableSeg:
					if firstVariable < 0 {
						firstVariable = segs[j].idx
					}
					light = light && hourly[segs[j].idx].WindSpeedKts < windLightKts
					briefly = briefly || segs[j].continues
				}
			}
			hasVariable := firstVariable >= 0
			light = light && hasCalm
			at := timed("around", hourly[s.idx].Label)
			if hasVariable && !light {
				at = timed("around", hourly[firstVariable].Label)
			}
			switch {
			case len(clauses) == 0 && hasVariable && light:
				clauses = append(clauses, "Light and variable early")
			case len(clauses) == 0 && hasVariable:
				clauses = append(clauses, "Variable early")
			case len(clauses) == 0:
				clauses = append(clauses, "Light winds early")
			case hasVariable && light:
				clauses = append(clauses, windWords("light and variable", at))
			case hasVariable && briefly:
				clauses = append(clauses, windWords("briefly variable", at))
			case hasVariable:
				clauses = append(clauses, windWords("variable", at))
			default:
				clauses = append(clauses, windWords("going light", at))
			}
			i = j
			continue
		}
		g := s.group
		speed := windSpeedPhrase(hourly, g, false)
		from := timed("from", hourly[g.subs[0].first()].Label)
		span := g.span()
		switch {
		case g.how == windOpening && len(clauses) == 0 && span.start == "":
			clauses = append(clauses, windWords("Winds", speed))
		case g.how == windOpening && len(clauses) == 0:
			clauses = append(clauses, windWords(span.label(), speed))
		case g.how == windOpening:
			clauses = append(clauses, windWords("then", span.label(), speed, from))
		case g.how == windResumed:
			clauses = append(clauses, windWords("then", span.label(), speed, "again", from))
		case g.how == windLater:
			clauses = append(clauses, windWords("then", span.label(), speed, from))
		case span.start != span.end:
			clauses = append(clauses, windWords("then turning", span.start, timed("around", hourly[g.subs[0].first()].Label), "and settling", span.end, speed))
		default:
			clauses = append(clauses, windWords("then turning", span.start, speed, timed("around", hourly[g.subs[0].first()].Label)))
		}
		i++
	}

	return strings.Join(clauses, ", ") + "." + gustSentence
}
