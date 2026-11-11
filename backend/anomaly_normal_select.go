package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Which readings the learned normal uses, decided from the data with a stated
// reason for each one left out. Nothing here knows a boat: a dead sender drops
// out because it never changes, whatever its path.

// featureEvidence is everything the data says about one reading over the
// training window.
type featureEvidence struct {
	Feature normalFeature
	// Source is the one source the values come from.
	Source string
	// TwoSources says another source reports the same minutes (chooseSender).
	TwoSources bool
	// SenderChanged says the reading had more than one sender in the window and
	// the latest has under normalMinMinutes of minutes.
	SenderChanged bool
	// Values is the reading's mean for each minute it has one.
	Values map[time.Time]float64
	// Wording is the subsystem's, for the sentences about its minutes.
	Wording normalWording
}

// normalFeatureVerdict is the saved and shown verdict on one reading.
type normalFeatureVerdict struct {
	Key  string `json:"key"`
	Path string `json:"path"`
	// Name is the reading as the operator reads it, where the path does not say.
	Name string `json:"name,omitempty"`
	// Placing says the reading only places the minute, so it is always read, from
	// any sender, and has no switch.
	Placing bool `json:"placing,omitempty"`
	// Component says the reading is only read to be summed into another.
	Component bool `json:"component,omitempty"`
	// Paths are the paths of the readings this one is the sum of: the switch for it
	// puts them all on or off the ignored list.
	Paths []string `json:"paths,omitempty"`
	// Group is set for a reading that is one of several paths for the same thing.
	Group  string `json:"group,omitempty"`
	SIUnit string `json:"si_unit,omitempty"`
	Source string `json:"source,omitempty"`
	// Required says the gate needs the reading, so it cannot be switched off.
	Required bool   `json:"required,omitempty"`
	Used     bool   `json:"used"`
	Reason   string `json:"reason,omitempty"`
	// SwitchedOff says the reading was left out because it is on the
	// ignored-sensor list, whatever the data says.
	SwitchedOff bool `json:"switched_off,omitempty"`
	// Joining says the reading started to report part-way through the window and
	// has fewer than normalMinMinutes of running minutes so far.
	Joining        bool `json:"joining,omitempty"`
	JoiningMinutes int  `json:"joining_minutes,omitempty"`
	// LateJoiner says the reading first appeared after the subject's first
	// judged minute, so bands that cannot yet fit it are carried forward.
	LateJoiner bool `json:"late_joiner,omitempty"`
	// SenderChanged says the reading's latest sender has too little history to
	// learn from, so it is left out until it qualifies.
	SenderChanged bool `json:"sender_changed,omitempty"`
}

// Status is the verdict as the settings page and the tool say it.
func (v normalFeatureVerdict) Status() string {
	switch {
	case !v.Used:
		return v.Reason
	case v.Joining:
		return fmt.Sprintf("joins after %d minutes, %d so far", normalMinMinutes, v.JoiningMinutes)
	}
	return "used"
}

// normalSenderChanged is the verdict on a reading whose latest sender is too new
// to learn from.
const normalSenderChanged = "sender changed, relearning"

// senderChoice is the one sender a reading is taken from, chosen at training.
type senderChoice struct {
	Source string
	Values map[time.Time]float64
	// Two says another sender reports more than normalTwoSourceOverlap of the
	// same minutes, so there is no one reading to learn from.
	Two bool
	// Changed says the reading has had more than one sender in the window and the
	// chosen one has under normalMinMinutes of minutes.
	Changed bool
}

// chooseSender pins a reading to one sender: the sender of its latest minute,
// where a reading handed from one sender to another has been reported ever
// since. Scoring reads only the sender saved here. Another sender that reports
// the same minutes at the same time is two sources and no choice is made for it.
func chooseSender(rows []normalMinuteRow) senderChoice {
	bySource := map[string]map[time.Time]float64{}
	var latest time.Time
	source := ""
	for _, r := range rows {
		at := r.At.Truncate(time.Minute)
		m := bySource[r.Source]
		if m == nil {
			m = map[time.Time]float64{}
			bySource[r.Source] = m
		}
		m[at] = r.Value
		if at.After(latest) || (at.Equal(latest) && r.Source < source) {
			latest, source = at, r.Source
		}
	}
	if len(bySource) == 0 {
		return senderChoice{Values: map[time.Time]float64{}}
	}
	out := senderChoice{Source: source, Values: bySource[source]}
	for other, vals := range bySource {
		if other == source {
			continue
		}
		shared := 0
		for at := range vals {
			if _, ok := out.Values[at]; ok {
				shared++
			}
		}
		if float64(shared) > normalTwoSourceOverlap*float64(len(out.Values)) {
			out.Two = true
		}
	}
	out.Changed = len(bySource) > 1 && len(out.Values) < normalMinMinutes
	return out
}

// valuesFromAnySender is a placing reading's minutes from whichever sender
// reported them. Where two report one minute the larger counts, so a turning
// engine is not hidden by a sender that says it is not.
func valuesFromAnySender(rows []normalMinuteRow) map[time.Time]float64 {
	out := map[time.Time]float64{}
	for _, r := range rows {
		at := r.At.Truncate(time.Minute)
		if v, ok := out[at]; !ok || r.Value > v {
			out[at] = r.Value
		}
	}
	return out
}

// withoutPlacing is the ignored list without the readings that place a minute,
// which it does not switch off.
func withoutPlacing(features []normalFeature, ignored map[string]bool) map[string]bool {
	out := make(map[string]bool, len(ignored))
	for path := range ignored {
		out[path] = true
	}
	for _, f := range features {
		if f.Placing {
			delete(out, f.Path)
		}
	}
	return out
}

// valuesFromSender is a reading's minutes from the one sender training pinned it
// to. Another sender's minutes are never used in its place.
func valuesFromSender(rows []normalMinuteRow, source string) map[time.Time]float64 {
	out := map[time.Time]float64{}
	for _, r := range rows {
		if r.Source == source {
			out[r.At.Truncate(time.Minute)] = r.Value
		}
	}
	return out
}

// selectFeatures judges each reading over the gated minutes: switched off,
// reported by two sources, no readings, missing too often, outside physical
// limits too often, or constant. classify is the sensor-health range check.
//
// A reading that starts working part-way through the window (a repaired sender)
// is judged over the gated minutes since it first appeared, so it is not held
// out for the length of the window. One that stopped is judged over the whole
// of its life and left out.
func selectFeatures(evidence []featureEvidence, gated, running []time.Time, ignored map[string]bool, classify func(path string, v float64) rangeVerdict) []normalFeatureVerdict {
	sorted := append([]time.Time(nil), gated...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })

	pathOf := map[string]string{}
	for _, ev := range evidence {
		pathOf[ev.Feature.Key] = ev.Feature.Path
	}
	out := make([]normalFeatureVerdict, 0, len(evidence))
	for _, ev := range evidence {
		if ev.Feature.Placing {
			// Read to place other readings, and neither used nor left out.
			out = append(out, normalFeatureVerdict{Key: ev.Feature.Key, Path: ev.Feature.Path, Name: ev.Feature.Name, SIUnit: ev.Feature.SIUnit, Source: ev.Source, Placing: true})
			continue
		}
		if ev.Feature.Component {
			// Read to be summed, and neither used nor left out.
			out = append(out, normalFeatureVerdict{Key: ev.Feature.Key, Path: ev.Feature.Path, Name: ev.Feature.Name, SIUnit: ev.Feature.SIUnit, Source: ev.Source, Component: true})
			continue
		}
		v := normalFeatureVerdict{Key: ev.Feature.Key, Path: ev.Feature.Path, Name: ev.Feature.Name, Group: ev.Feature.Group, SIUnit: ev.Feature.SIUnit, Source: ev.Source, Required: ev.Feature.Required}
		// A sum with any part switched off is not judged: a partial sum would be a
		// different reading.
		partOff := false
		for _, k := range ev.Feature.SumOf {
			v.Paths = append(v.Paths, pathOf[k])
			partOff = partOff || ignored[pathOf[k]]
		}
		reason, late, present := judgeFeature(ev, sorted, running, ignored, classify)
		if partOff {
			reason, late, present = "switched off", false, 0
		}
		if reason != "" {
			v.Reason = reason
			v.SwitchedOff = ignored[ev.Feature.Path] || partOff
			v.SenderChanged = reason == normalSenderChanged
		} else {
			v.Used = true
			v.LateJoiner = late
			if late && present < normalMinMinutes {
				v.Joining, v.JoiningMinutes = true, present
			}
		}
		out = append(out, v)
	}
	return out
}

func judgeFeature(ev featureEvidence, gated, running []time.Time, ignored map[string]bool, classify func(path string, v float64) rangeVerdict) (reason string, late bool, present int) {
	if ignored[ev.Feature.Path] {
		return "switched off", false, 0
	}
	if ev.TwoSources {
		return "two sources report it", false, 0
	}
	if ev.SenderChanged {
		return normalSenderChanged, false, 0
	}
	var first time.Time
	for at := range ev.Values {
		if first.IsZero() || at.Before(first) {
			first = at
		}
	}
	since := 0
	var vals []float64
	if !first.IsZero() {
		for _, at := range gated {
			if at.Before(first) {
				continue
			}
			since++
			if v, ok := ev.Values[at]; ok {
				vals = append(vals, v)
			}
		}
	}
	if len(vals) == 0 {
		return "no readings", false, 0
	}
	present = len(vals)
	// A reading fitted only where it reports (an alternator) is missing from the
	// minutes where its source is off, so how often it is missing says nothing.
	if share := float64(present) / float64(since); share < normalMinPresence && !ev.Feature.PerRegime {
		return fmt.Sprintf("missing in %d%% of %s", int(math.Round(100*(1-share))), ev.Wording.Steady), false, present
	}
	if classify != nil {
		bad := 0
		for _, v := range vals {
			if classify(ev.Feature.Path, v) == rangeOutOfRange {
				bad++
			}
		}
		if share := float64(bad) / float64(present); share > normalMaxOutOfLimits {
			return fmt.Sprintf("readings outside what the sender can read in %d%% of %s", int(math.Round(100*share)), ev.Wording.Steady), false, present
		}
	}
	// Constant is judged over every minute the subject was running, warm-up
	// included, not over the steady minutes alone. A thermostat holds coolant on
	// one value for nearly every steady minute, but it climbs from ambient after
	// each start. A sender that never changed at all does not.
	runVals := vals
	if len(running) > 0 {
		runVals = nil
		for _, at := range running {
			if v, ok := ev.Values[at]; ok {
				runVals = append(runVals, v)
			}
		}
	}
	// A reading fitted only where it reports is judged constant band by band when
	// the bands are fitted, and a conditioning reading may sit still.
	if !ev.Feature.AllowConstant && !ev.Feature.PerRegime && readingIsConstant(runVals) {
		return "never changed " + ev.Wording.While, false, present
	}
	late = len(gated) > 0 && gated[0].Before(first)
	return "", late, present
}

// readingIsConstant says a reading shows no variation worth learning: fewer than
// three distinct values, one value in nearly every minute, or a spread no larger
// than the smallest step it takes. Values are compared to nine significant
// digits so the float noise an average leaves is not a change.
func readingIsConstant(vals []float64) bool {
	counts := map[float64]int{}
	top := 0
	for _, v := range vals {
		q := significant(v)
		counts[q]++
		if counts[q] > top {
			top = counts[q]
		}
	}
	if len(counts) < normalMinDistinct {
		return true
	}
	if float64(top) >= normalConstantShare*float64(len(vals)) {
		return true
	}
	var mean float64
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	var ss float64
	for _, v := range vals {
		ss += (v - mean) * (v - mean)
	}
	sd := math.Sqrt(ss / float64(len(vals)-1))
	return sd <= smallestStep(vals)
}

// commaInt writes n with thousands separators, 2990 as "2,990".
func commaInt(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}
