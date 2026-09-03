package main

import (
	"fmt"
	"sort"
	"time"
)

/*
Upper-air (500mb) outlook (ADR 0071).

Surviving the Storm treats the 500mb chart as the most valuable forecasting
tool aboard (p61), because the upper trough is what lets a surface low develop
at all: "If there is no trough in the upper atmosphere, the surface low will be
anemic, or won't develop at all" (p62). Its instruction is to watch the rhythm
for ten days to two weeks before departure, which is roughly the window a
global model now gives.

What the book does not give is a number. Its method is reading successive
charts, so every threshold here would have been invented and then falsely
attributed. Instead each day is judged against the rest of the forecast window
at this position: where it sits in that window, and which way it is moving.
That is a relative claim, which is what the data actually supports, and it
needs no retuning as the boat moves between latitudes where 500mb heights are
completely different.
*/

// upperAirDayInput is one day's upper-air figures, already reduced from the
// hourly series. Present is false when the provider carried no upper-air data
// for that day, which is the normal case for a provider whose upstream model
// has no pressure levels.
type upperAirDayInput struct {
	DayKey        string
	Height500M    float64
	ThicknessM    float64
	PeakWind500MS float64
	Present       bool
}

// upperAirDayOutlook is what reaches the wire.
type upperAirDayOutlook struct {
	Present bool `json:"present"`

	Height500M     float64 `json:"height_500_m"`
	ThicknessM     float64 `json:"thickness_m"`
	PeakWind500Kts float64 `json:"peak_wind_500_kts"`

	// HeightPercentile is where this day's 500mb height sits among the days of
	// this forecast window, 0 being the lowest. Tendency24hM is the change
	// from the previous day, negative when heights are falling.
	HeightPercentile float64 `json:"height_percentile"`
	Tendency24hM     float64 `json:"tendency_24h_m"`

	// TroughSupport is the flag the forecast strip marks: this day sits in the
	// lowest part of the window and is still falling, so conditions aloft
	// support a surface low developing.
	TroughSupport bool `json:"trough_support"`
}

const (
	// upperAirLowQuintile is the share of the window a day has to fall within
	// to count as "the low end of the coming fortnight".
	upperAirLowQuintile = 0.2

	// upperAirMinSpreadM stops a flat window from flagging its own lowest day.
	// Some day is always the lowest; that is arithmetic, not an upper trough,
	// and a marker that appears every week is one nobody reads. Twenty metres
	// across a fortnight is a window with no real pattern in it.
	upperAirMinSpreadM = 20.0

	// upperAirRunUpDays is how far back the fall into a day is measured, and
	// upperAirRunUpFraction how much of the window's own range that fall has
	// to cover.
	//
	// Measured across two days rather than one because of a knife-edge found
	// against live data: the bottom of a real trough had a 24-hour tendency of
	// -0.167m, indistinguishable from flat, and a rule keyed on the sign of
	// that number would have marked or missed the day on a coin toss. What is
	// meaningful is the fall INTO the day, and the trough bottom sits at the
	// end of a large one.
	//
	// A fraction of the window's range rather than a fixed metre value, so
	// this stays self-calibrating along with everything else here.
	upperAirRunUpDays     = 2
	upperAirRunUpFraction = 0.2
)

// upperAirOutlook works out where each day sits in its own window.
//
// Days with no upper-air data are carried through as absent rather than
// dropped, so the caller can line the result up with its own day list, and are
// excluded from the window statistics so a gap cannot drag the percentiles
// about.
func upperAirOutlook(days []upperAirDayInput) []upperAirDayOutlook {
	if len(days) == 0 {
		return nil
	}

	sorted := sortedPresentHeights(days)

	spread := 0.0
	if len(sorted) > 1 {
		spread = sorted[len(sorted)-1] - sorted[0]
	}

	minimumRunUp := spread * upperAirRunUpFraction

	out := make([]upperAirDayOutlook, 0, len(days))
	var previous *upperAirDayInput

	for i := range days {
		day := days[i]
		if !day.Present {
			out = append(out, upperAirDayOutlook{})
			// Deliberately not advancing previous: a tendency measured across
			// a gap is not a 24-hour change.
			previous = nil
			continue
		}

		entry := upperAirDayOutlook{
			Present:          true,
			Height500M:       day.Height500M,
			ThicknessM:       day.ThicknessM,
			PeakWind500Kts:   day.PeakWind500MS * metersPerSecondToKnots,
			HeightPercentile: percentileOf(sorted, day.Height500M),
		}
		if previous != nil {
			entry.Tendency24hM = day.Height500M - previous.Height500M
		}

		runUp, haveRunUp := heightRunUp(days, i)
		entry.TroughSupport = spread >= upperAirMinSpreadM &&
			entry.HeightPercentile <= upperAirLowQuintile &&
			haveRunUp && -runUp >= minimumRunUp

		out = append(out, entry)
		previous = &days[i]
	}

	return out
}

// heightRunUp is the change in 500mb height over the days leading into index
// i, negative when heights have been falling, and whether there was enough
// unbroken history to measure it.
//
// A day at the very start of the window, or one whose run-up crosses a gap in
// the data, reports no run-up rather than a partial one: whether heights are
// arriving or leaving is genuinely unknowable there, and saying nothing is
// the honest answer.
func heightRunUp(days []upperAirDayInput, i int) (float64, bool) {
	start := i - upperAirRunUpDays
	if start < 0 || !days[i].Present {
		return 0, false
	}
	for j := start; j <= i; j++ {
		if !days[j].Present {
			return 0, false
		}
	}
	return days[i].Height500M - days[start].Height500M, true
}

// percentileOf is the share of the sorted set that sits below value, so the
// lowest day comes out at 0.
func percentileOf(sorted []float64, value float64) float64 {
	if len(sorted) < 2 {
		return 0
	}
	below := sort.SearchFloat64s(sorted, value)
	return float64(below) / float64(len(sorted)-1)
}

// buildUpperAirInputs reduces a provider's flat hourly series into one entry
// per local calendar day, keyed the same way the weather day cards are.
//
// A day is Present only if at least one of its hours carried a 500mb height.
// The mean is taken across the hours that did, so a partial day still reports
// rather than being thrown away.
func buildUpperAirInputs(hourly []upperAirHourPoint, localLocation *time.Location) map[string]upperAirDayInput {
	type accumulator struct {
		heightTotal    float64
		thicknessTotal float64
		count          int
		peakWindMS     float64
	}

	byDay := map[string]*accumulator{}
	for _, hp := range hourly {
		if hp.Time.IsZero() || hp.GeopotentialHeight500M <= 0 {
			continue
		}
		dayKey := hp.Time.In(localLocation).Format("2006-01-02")
		acc, ok := byDay[dayKey]
		if !ok {
			acc = &accumulator{}
			byDay[dayKey] = acc
		}
		acc.heightTotal += hp.GeopotentialHeight500M
		acc.count++
		if hp.GeopotentialHeight1000M > 0 {
			acc.thicknessTotal += hp.GeopotentialHeight500M - hp.GeopotentialHeight1000M
		}
		if hp.WindSpeed500MS > acc.peakWindMS {
			acc.peakWindMS = hp.WindSpeed500MS
		}
	}

	out := make(map[string]upperAirDayInput, len(byDay))
	for dayKey, acc := range byDay {
		if acc.count == 0 {
			continue
		}
		out[dayKey] = upperAirDayInput{
			DayKey:        dayKey,
			Height500M:    acc.heightTotal / float64(acc.count),
			ThicknessM:    acc.thicknessTotal / float64(acc.count),
			PeakWind500MS: acc.peakWindMS,
			Present:       true,
		}
	}
	return out
}

// sortedPresentHeights is every day's 500mb height that actually exists, in
// ascending order. Both the per-day percentile and the window band the chart
// draws are cut from this same list, so the drawn band and the marked days
// cannot drift apart.
func sortedPresentHeights(days []upperAirDayInput) []float64 {
	heights := make([]float64, 0, len(days))
	for _, day := range days {
		if day.Present {
			heights = append(heights, day.Height500M)
		}
	}
	sort.Float64s(heights)
	return heights
}

// upperAirWindow is the vertical extent of the forecast window, so the chart
// can draw the range each day is being judged against instead of restating a
// percentile in prose.
//
// Surviving the Storm reads a trough off the shape of successive charts, and
// that shape only means anything against the rest of the window. LowQuintileM
// is the height at which a day stops counting as "the low end of the coming
// fortnight", which is the same edge TroughSupport tests.
type upperAirWindow struct {
	Present bool `json:"present"`

	LowM  float64 `json:"low_m"`
	HighM float64 `json:"high_m"`

	// LowQuintileM is the highest 500mb height still inside the lowest
	// quintile of the window. A day at or below it is in the band the flag
	// looks at; a day above it is not.
	LowQuintileM float64 `json:"low_quintile_m"`
}

// upperAirWindowFor measures the window a day list is judged in.
//
// A window with fewer than two days of data has no range and reports absent:
// percentileOf already returns 0 for everything there, and a band drawn from a
// zero would sit at sea level.
func upperAirWindowFor(days []upperAirDayInput) upperAirWindow {
	sorted := sortedPresentHeights(days)
	if len(sorted) < 2 {
		return upperAirWindow{}
	}

	// The index whose percentile is the last one at or under the quintile.
	// Derived from the same len-1 denominator percentileOf uses rather than
	// from a metre value, so the two stay in step by construction.
	edge := int(upperAirLowQuintile * float64(len(sorted)-1))

	return upperAirWindow{
		Present:      true,
		LowM:         sorted[0],
		HighM:        sorted[len(sorted)-1],
		LowQuintileM: sorted[edge],
	}
}

// upperAirSeriesPoint is one sample of the sub-daily 500mb trace.
//
// The day cards carry a daily mean, which is the right figure for a badge and
// the wrong one for a chart: a trough rendered at one point per day is a
// sawtooth, and the fall into it is the part the book actually reads. The
// provider already returns hourly data, so the trace costs nothing upstream.
type upperAirSeriesPoint struct {
	Time   string `json:"time"`
	DayKey string `json:"day_key"`

	// LocalHour is the hour of the vessel's local day this sample falls in.
	// It travels with the sample rather than being recovered from Time in the
	// browser, because the browser's timezone is not the vessel's and a label
	// derived there would disagree with DayKey.
	LocalHour int `json:"local_hour"`

	Height500M float64 `json:"height_500_m"`
	ThicknessM float64 `json:"thickness_m"`
	Wind500Kts float64 `json:"wind_500_kts"`
	Temp500C   float64 `json:"temperature_500_c"`
}

// upperAirSeriesBucketHours is how coarsely the hourly series is thinned for
// the chart. Six-hourly matches the synoptic chart times the book works from,
// and holds a 16-day trace to about 64 points, which is a line a 175px chart
// can actually draw.
const upperAirSeriesBucketHours = 6

// buildUpperAirSeries thins a provider's hourly series down to one sample per
// six-hour block of local time, dropping anything before fromDayKey.
//
// Buckets are cut on the hour's position within the local day rather than on
// an exact clock hour. A provider reporting at 01:00/07:00/13:00/19:00 is
// off-phase from a 0/6/12/18 test and still has exactly the resolution asked
// for; selecting on the clock would have silently returned nothing for it.
func buildUpperAirSeries(hourly []upperAirHourPoint, localLocation *time.Location, fromDayKey string) []upperAirSeriesPoint {
	present := make([]upperAirHourPoint, 0, len(hourly))
	for _, hp := range hourly {
		if hp.Time.IsZero() || hp.GeopotentialHeight500M <= 0 {
			continue
		}
		present = append(present, hp)
	}
	sort.Slice(present, func(i, j int) bool { return present[i].Time.Before(present[j].Time) })

	out := make([]upperAirSeriesPoint, 0, len(present)/upperAirSeriesBucketHours+1)
	seen := map[string]bool{}

	for _, hp := range present {
		local := hp.Time.In(localLocation)
		dayKey := local.Format("2006-01-02")
		if fromDayKey != "" && dayKey < fromDayKey {
			continue
		}

		bucket := fmt.Sprintf("%s#%d", dayKey, local.Hour()/upperAirSeriesBucketHours)
		if seen[bucket] {
			continue
		}
		seen[bucket] = true

		point := upperAirSeriesPoint{
			Time:       hp.Time.UTC().Format(time.RFC3339),
			DayKey:     dayKey,
			LocalHour:  local.Hour(),
			Height500M: hp.GeopotentialHeight500M,
			Wind500Kts: hp.WindSpeed500MS * metersPerSecondToKnots,
			Temp500C:   hp.Temperature500C,
		}
		if hp.GeopotentialHeight1000M > 0 {
			point.ThicknessM = hp.GeopotentialHeight500M - hp.GeopotentialHeight1000M
		}
		out = append(out, point)
	}

	return out
}
