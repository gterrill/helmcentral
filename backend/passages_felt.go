package main

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// How a passage felt (ADR 0298). The measure is worked out from the stored
// track points and kept with the passage's stats; the band words are worked out
// on every read from the operator's ratings, so a new rating moves them without
// touching anything stored.

const (
	// feltSpellLength is the under-way time one spell covers.
	feltSpellLength = 15 * time.Minute
	// feltMinSpell is the least measured under-way time a spell needs to count.
	// A shorter tail is left out: a few minutes at one moment says little.
	feltMinSpell = 5 * time.Minute
	// feltStopGap is the stop or hole after which the next under-way leg starts
	// a new spell, so a spell never paints over a stop.
	feltStopGap = 5 * time.Minute
	// feltPercentile is the share of spells at or below the felt figure.
	feltPercentile = 0.75
)

// feltSpell is one stretch of under-way time with its sway in degrees: the root
// of the mean roll RMS squared plus the mean pitch RMS squared. Start and End
// are Unix seconds.
type feltSpell struct {
	Start   int64   `json:"start"`
	End     int64   `json:"end"`
	SwayDeg float64 `json:"sway_deg"`
}

// Why a figure is missing. Each of the three figures has a function of its own
// that reads only that figure's inputs, so a slow attitude feed never explains a
// missing heave, and each always returns a sentence. The first point of a
// passage is left out of every one, as in the maxima: its figures describe the
// interval before the passage.

const (
	figureSidewaysG = "sideways g"
	figureSnap      = "snap"
)

const (
	reasonImported = "This passage was imported, so it has no motion figures."
	reasonNoMotion = "No motion was recorded on this passage: the boat had no attitude sensor, or its motion feed wasn't reaching Helmcentral."

	feltReasonTooLittle     = "Motion was recorded for less than five minutes at a time under way, too little to judge how it felt."
	feltReasonOneAxis       = "Only %s was recorded on this passage; the felt figure needs roll and pitch."
	feltReasonBeforeSway    = "This passage was recorded before sway was kept, so it has roll peaks but no felt figure."
	feltReasonNoPoints      = "This passage has no track points left to work out how it felt."
	snapReasonSlow          = "The attitude feed was below 8 Hz on this passage, too slow to work out %s."
	snapReasonNoAccel       = "Motion was recorded on this passage, but no point has %s."
	snapReasonBeforeCapture = "This passage was recorded before 10 Hz capture, so it has roll peaks but no %s."
	snapReasonHistory       = "Roll and pitch were filled from history on this passage, too coarse to work out %s."
	snapReasonNoPoints      = "This passage has no track points left to work out snap."

	poundingReasonNoHeave       = "No heave was recorded on this passage, so pounding isn't available."
	poundingReasonSlow          = "Heave arrived below 8 Hz on this passage, too slow to work out pounding."
	poundingReasonNoAccel       = "Heave was recorded on this passage, but no point has pounding."
	poundingReasonHistory       = "Heave was filled from history on this passage, too coarse to work out pounding."
	poundingReasonBeforeCapture = "This passage was recorded before 10 Hz capture, so it has heave peaks but no pounding."
	poundingReasonNoPoints      = "This passage has no track points left to work out pounding."
)

// afterFirst is the points a figure is judged from: all but the first.
func afterFirst(pts []recordedTrackPoint) []recordedTrackPoint {
	if len(pts) == 0 {
		return pts
	}
	return pts[1:]
}

// slowMostly says most of the n rates counted were under the 8 Hz floor.
func slowMostly(slow, n int) bool { return slow*2 > n }

// feltMissingReason says why the points hold no felt figure, from their roll and
// pitch RMS alone.
func feltMissingReason(pts []recordedTrackPoint) string {
	var roll, pitch, peaks, imported bool
	for _, p := range afterFirst(pts) {
		imported = imported || p.Source == trackSourceImported
		roll = roll || p.RollRMSDeg != nil
		pitch = pitch || p.PitchRMSDeg != nil
		peaks = peaks || p.RollPeakDeg != nil
	}
	switch {
	case roll && pitch:
		return feltReasonTooLittle
	case roll:
		return fmt.Sprintf(feltReasonOneAxis, "roll")
	case pitch:
		return fmt.Sprintf(feltReasonOneAxis, "pitch")
	case imported:
		return reasonImported
	case peaks:
		return feltReasonBeforeSway
	default:
		return reasonNoMotion
	}
}

// snapMissingReason says why the points hold no roll acceleration, from the
// attitude feed alone. figure names what the page is missing: snap, or sideways
// g.
func snapMissingReason(pts []recordedTrackPoint, figure string) string {
	var rated, slow int
	var imported, peaks, spread bool
	for _, p := range afterFirst(pts) {
		imported = imported || p.Source == trackSourceImported
		if p.MotionRateHz != nil {
			rated++
			if *p.MotionRateHz < motionMinRateHz {
				slow++
			}
		}
		peaks = peaks || p.RollPeakDeg != nil
		spread = spread || p.RollRMSDeg != nil || p.PitchRMSDeg != nil
	}
	switch {
	case imported:
		return reasonImported
	case rated > 0 && slowMostly(slow, rated):
		return fmt.Sprintf(snapReasonSlow, figure)
	case rated > 0:
		return fmt.Sprintf(snapReasonNoAccel, figure)
	case peaks:
		return fmt.Sprintf(snapReasonBeforeCapture, figure)
	case spread:
		return fmt.Sprintf(snapReasonHistory, figure)
	default:
		return reasonNoMotion
	}
}

// poundingMissingReason says why the points hold no vertical acceleration, from
// the heave alone.
func poundingMissingReason(pts []recordedTrackPoint) string {
	var rated, slow int
	var imported, spread, peaks bool
	for _, p := range afterFirst(pts) {
		imported = imported || p.Source == trackSourceImported
		if p.HeaveRateHz != nil {
			rated++
			if *p.HeaveRateHz < motionMinRateHz {
				slow++
			}
		}
		spread = spread || p.HeaveSDM != nil
		peaks = peaks || p.HeavePeakM != nil
	}
	switch {
	case imported:
		return reasonImported
	case rated > 0 && slowMostly(slow, rated):
		return poundingReasonSlow
	case rated > 0:
		return poundingReasonNoAccel
	case spread:
		return poundingReasonHistory
	case peaks:
		return poundingReasonBeforeCapture
	default:
		return poundingReasonNoHeave
	}
}

// feltOf works out a passage's spells and its felt figure: the third quartile
// of the spells' sway. With no figure the reason says why.
func feltOf(pts []recordedTrackPoint) (spells []feltSpell, felt *float64, reason string) {
	spells = []feltSpell{}
	type spell struct {
		start, end         time.Time
		underway, measured float64 // seconds
		roll2, pitch2      float64 // squared RMS weighted by each leg's seconds
	}
	var cur *spell
	flush := func() {
		if cur != nil && cur.measured >= feltMinSpell.Seconds() {
			sway := math.Sqrt((cur.roll2 + cur.pitch2) / cur.measured)
			spells = append(spells, feltSpell{Start: cur.start.Unix(), End: cur.end.Unix(), SwayDeg: sway})
		}
		cur = nil
	}
	for i := 1; i < len(pts); i++ {
		prev, p := pts[i-1], pts[i]
		if !legBetween(prev, p) {
			flush()
			continue
		}
		dt := p.At.Sub(prev.At).Seconds()
		if !underwayLeg(haversineMeters(prev.Lat, prev.Lon, p.Lat, p.Lon), dt) {
			continue
		}
		if cur != nil && (prev.At.Sub(cur.end) > feltStopGap || cur.underway >= feltSpellLength.Seconds()) {
			flush()
		}
		if cur == nil {
			cur = &spell{start: prev.At}
		}
		cur.end = p.At
		cur.underway += dt
		if p.RollRMSDeg != nil && p.PitchRMSDeg != nil {
			cur.measured += dt
			cur.roll2 += dt * *p.RollRMSDeg * *p.RollRMSDeg
			cur.pitch2 += dt * *p.PitchRMSDeg * *p.PitchRMSDeg
		}
	}
	flush()

	if len(spells) == 0 {
		return spells, nil, feltMissingReason(pts)
	}
	sways := make([]float64, len(spells))
	for i, s := range spells {
		sways[i] = s.SwayDeg
	}
	sort.Float64s(sways)
	v := quantileSorted(sways, feltPercentile)
	return spells, &v, ""
}

// ── what a list row and a passage page show ──────────────────────────────

// feltBandFor names the band a figure falls in and, for a figure outside
// everything rated, says so.
func feltBandFor(bands comfortBands, deg float64) (band *string, note string) {
	name, outside := bands.classify(deg)
	switch outside {
	case belowRated:
		note = "gentler than any rated passage"
	case aboveRated:
		note = "rougher than any rated passage"
	}
	if name == "" {
		return nil, note
	}
	return &name, note
}

// setFelt gives a list row its band word, the note and the hover text.
func (p *passageSummary) setFelt(bands comfortBands) {
	switch {
	case p.Stats.FeltDeg == nil:
		p.FeltHint = p.Stats.FeltReason
	default:
		p.FeltBand, p.FeltBandNote = feltBandFor(bands, *p.Stats.FeltDeg)
		switch {
		case p.FeltBand == nil:
			p.FeltHint = bands.Reason
		case p.FeltBandNote != "":
			p.FeltHint = p.FeltBandNote
		default:
			p.FeltHint = fmt.Sprintf("%.1f° felt", *p.Stats.FeltDeg)
		}
	}
}

type feltSpellOut struct {
	feltSpell
	// Band is the spell's band word, nil while the bands are not learned.
	Band *string `json:"band"`
}

// feltSnap is the sharpest roll: sideways g at the helm when the helm height is
// set, else the peak roll acceleration. Kind says which, so the page labels it.
type feltSnap struct {
	Kind   string   `json:"kind,omitempty"`
	Value  *float64 `json:"value"`
	At     *int64   `json:"at"`
	Reason string   `json:"reason,omitempty"`
	// Note says what would improve the figure, such as the missing helm height.
	Note string `json:"note,omitempty"`
}

type feltPounding struct {
	ValueG *float64 `json:"value_g"`
	At     *int64   `json:"at"`
	Reason string   `json:"reason,omitempty"`
}

const (
	snapKindSidewaysG = "sideways_g"
	snapKindRollAccel = "roll_accel"
)

// passageFelt is everything the page shows about how a passage felt.
type passageFelt struct {
	Deg      *float64       `json:"deg"`
	Reason   string         `json:"reason,omitempty"`
	Band     *string        `json:"band"`
	BandNote string         `json:"band_note,omitempty"`
	Bands    comfortBands   `json:"bands"`
	Spells   []feltSpellOut `json:"spells"`
	Snap     feltSnap       `json:"snap"`
	Pounding feltPounding   `json:"pounding"`
}

func buildPassageFelt(st passageStats, sideways passageSidewaysG, bands comfortBands) passageFelt {
	f := passageFelt{Deg: st.FeltDeg, Reason: st.FeltReason, Bands: bands, Spells: []feltSpellOut{}}
	if st.FeltDeg != nil {
		f.Band, f.BandNote = feltBandFor(bands, *st.FeltDeg)
	}
	for _, sp := range st.FeltSpells {
		band, _ := feltBandFor(bands, sp.SwayDeg)
		f.Spells = append(f.Spells, feltSpellOut{feltSpell: sp, Band: band})
	}
	switch {
	case sideways.PeakG != nil:
		f.Snap = feltSnap{Kind: snapKindSidewaysG, Value: sideways.PeakG, At: sideways.PeakAt}
	case st.MaxRollAccelDps2 != nil:
		f.Snap = feltSnap{Kind: snapKindRollAccel, Value: st.MaxRollAccelDps2, At: st.MaxRollAccelAt, Note: sideways.Reason}
	default:
		f.Snap = feltSnap{Reason: st.SnapReason}
	}
	f.Pounding = feltPounding{ValueG: st.MaxPoundingG, At: st.MaxPoundingAt}
	if st.MaxPoundingG == nil {
		f.Pounding.Reason = st.PoundingReason
	}
	return f
}
