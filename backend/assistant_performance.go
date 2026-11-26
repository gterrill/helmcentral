package main

import (
	"math"
	"sort"
)

// This file holds the pure maths behind the estimate_passage tool
// (assistant_tools.go, ADR 0093 section 12): turning the boat's own logged
// speed-over-ground, fuel-rate and rpm history into a speed-to-burn table,
// and reading a burn rate and rpm off that table at a chosen speed. Nothing
// here touches InfluxDB, SignalK or JSON - the tool call site gathers the
// three raw series and hands them in, so this logic can be driven entirely
// by synthetic []telemetryPoint slices in tests.

// assistantPerformanceQueryEvery is the InfluxDB aggregation window
// estimate_passage joins its series at. A 10-minute mean is coarse enough
// that a boat holding a steady cruising speed for even a short leg
// contributes several samples to one band, and fine enough that two
// genuinely different cruising speeds (a fast passage versus a slow one)
// don't get smeared into the same band.
const assistantPerformanceQueryEvery = "10m"

// assistantPerformanceMinSOGKts is the speed-over-ground floor a 10-minute
// mean must clear before it counts toward the boat's own speed-to-burn
// table. Below this the boat is manoeuvring, holding station, or idling
// alongside, none of which describes passage-making performance.
const assistantPerformanceMinSOGKts = 4.0

// assistantPerformanceMinBandSamples is the fewest joined 10-minute means a
// whole-knot band needs before it is reported. Fewer than this and the band
// is one or two short legs' worth of data pretending to be a pattern, not
// something an estimate should be built on.
const assistantPerformanceMinBandSamples = 3

// cubicMetersPerSecondToLitresPerHour converts a SignalK fuel.rate value
// (cubic metres per second) to litres per hour: 1 m3 = 1000 L, and there are
// 3600 seconds in an hour, so the factor is 1000 * 3600.
const cubicMetersPerSecondToLitresPerHour = 3600000.0

// hertzToRPM converts a SignalK propulsion.<id>.revolutions value (Hz, i.e.
// revolutions per second) to revolutions per minute.
const hertzToRPM = 60.0

// performanceBand is one whole-knot slice of the boat's own speed-to-burn
// table: band 9 covers every joined sample whose speed over ground fell in
// [9, 10) knots. SOGKtsMin is the band's lower edge (9, not the smallest
// sample actually observed in it); SOGKtsMean is the mean of the samples
// that landed in it, which is what estimateAtSpeed interpolates between.
type performanceBand struct {
	SOGKtsMin  float64 `json:"sog_kts_min"`
	SOGKtsMean float64 `json:"sog_kts_mean"`
	LPerH      float64 `json:"l_per_h"`
	RPM        float64 `json:"rpm"`
	Samples    int     `json:"samples"`
}

// indexTelemetryByUnixSecond keys a telemetry series by its timestamp's Unix
// second, the join key buildPerformanceTable uses to line up sog, fuelRates
// and rpm samples that share an identical timestamp (the same InfluxDB
// aggregateWindow boundary, since every series is queried with the same
// `every`).
func indexTelemetryByUnixSecond(points []telemetryPoint) map[int64]float64 {
	index := make(map[int64]float64, len(points))
	for _, p := range points {
		index[p.Timestamp.UTC().Unix()] = p.Value
	}
	return index
}

// performanceBandAccumulator sums one whole-knot band's contributing samples
// so buildPerformanceTable can report each band's mean rather than its
// running total.
type performanceBandAccumulator struct {
	sumSOGKts float64
	sumLPerH  float64
	sumRPM    float64
	count     int
}

// buildPerformanceTable joins sog, every series in fuelRates and rpm on
// identical timestamps, converts units (m/s to knots, m3/s summed across
// every fuelRates instance to L/h, Hz to rpm), drops any sample below
// minSOGKts, and buckets what remains into whole-knot bands.
//
// A timestamp only contributes when every series has a point at it: sog,
// every instance in fuelRates, and rpm. This is deliberate, not an
// oversight - an instance that has gone quiet (an engine off, a sensor
// dropout) means the total burn at that instant is unknown, not "whatever
// the other instances reported", so the whole timestamp is skipped rather
// than silently undercounted.
//
// A band with fewer than assistantPerformanceMinBandSamples joined samples
// is dropped entirely rather than returned on thin evidence. The surviving
// bands are sorted by SOGKtsMin ascending.
func buildPerformanceTable(sog []telemetryPoint, fuelRates [][]telemetryPoint, rpm []telemetryPoint, minSOGKts float64) []performanceBand {
	rpmByTime := indexTelemetryByUnixSecond(rpm)
	fuelByTime := make([]map[int64]float64, len(fuelRates))
	for i, series := range fuelRates {
		fuelByTime[i] = indexTelemetryByUnixSecond(series)
	}

	bands := map[int]*performanceBandAccumulator{}

	for _, point := range sog {
		sogKts := point.Value * metersPerSecondToKnots
		if sogKts < minSOGKts {
			continue
		}

		ts := point.Timestamp.UTC().Unix()

		totalLPerH := 0.0
		everyInstancePresent := true
		for _, fuelSeries := range fuelByTime {
			rate, ok := fuelSeries[ts]
			if !ok {
				everyInstancePresent = false
				break
			}
			totalLPerH += rate * cubicMetersPerSecondToLitresPerHour
		}
		if !everyInstancePresent {
			continue
		}

		rpmHz, ok := rpmByTime[ts]
		if !ok {
			continue
		}

		band := int(math.Floor(sogKts))
		acc, exists := bands[band]
		if !exists {
			acc = &performanceBandAccumulator{}
			bands[band] = acc
		}
		acc.sumSOGKts += sogKts
		acc.sumLPerH += totalLPerH
		acc.sumRPM += rpmHz * hertzToRPM
		acc.count++
	}

	table := make([]performanceBand, 0, len(bands))
	for band, acc := range bands {
		if acc.count < assistantPerformanceMinBandSamples {
			continue
		}
		table = append(table, performanceBand{
			SOGKtsMin:  float64(band),
			SOGKtsMean: roundTo1(acc.sumSOGKts / float64(acc.count)),
			LPerH:      roundTo1(acc.sumLPerH / float64(acc.count)),
			RPM:        math.Round(acc.sumRPM / float64(acc.count)),
			Samples:    acc.count,
		})
	}
	sort.Slice(table, func(i, j int) bool { return table[i].SOGKtsMin < table[j].SOGKtsMin })
	return table
}

// estimateAtSpeed reads a burn rate and rpm off table at speedKts, linearly
// interpolating between the two bands (by SOGKtsMean) that bracket it and
// clamping to the table's own ends beyond either extreme - the boat's
// logged history says nothing about a speed it never actually made, so the
// nearest band it does have is the honest answer rather than an
// extrapolation past what was ever observed. ok is false only when table is
// empty.
func estimateAtSpeed(table []performanceBand, speedKts float64) (lPerH float64, rpm float64, ok bool) {
	return interpolateBands(table, func(b performanceBand) float64 { return b.SOGKtsMean }, speedKts)
}

// estimateAtRPM reads the burn rate off table at rpm, interpolating between the
// two bands (by their mean rpm) that bracket it and clamping to the table's own
// ends, as estimateAtSpeed does for a speed. ok is false only when table is
// empty.
func estimateAtRPM(table []performanceBand, rpm float64) (lPerH float64, ok bool) {
	lPerH, _, ok = interpolateBands(table, func(b performanceBand) float64 { return b.RPM }, rpm)
	return lPerH, ok
}

// rpmRange is the lowest and highest mean rpm among the table's bands. ok is false
// when no band has an rpm figure.
func rpmRange(table []performanceBand) (lo, hi float64, ok bool) {
	for _, b := range table {
		if math.IsNaN(b.RPM) {
			continue
		}
		if !ok || b.RPM < lo {
			lo = b.RPM
		}
		if !ok || b.RPM > hi {
			hi = b.RPM
		}
		ok = true
	}
	return lo, hi, ok
}

// sogRange is the lowest and highest mean speed over ground among the table's
// bands. ok is false when no band has a speed figure.
func sogRange(table []performanceBand) (lo, hi float64, ok bool) {
	for _, b := range table {
		if math.IsNaN(b.SOGKtsMean) {
			continue
		}
		if !ok || b.SOGKtsMean < lo {
			lo = b.SOGKtsMean
		}
		if !ok || b.SOGKtsMean > hi {
			hi = b.SOGKtsMean
		}
		ok = true
	}
	return lo, hi, ok
}

// interpolateBands reads the burn rate and rpm off table at x, where key is the
// bands' own figure for the axis x is on. It interpolates linearly between the two
// bands that bracket x and clamps to the ends beyond either. ok is false when x is
// not a number or no band has a figure on the axis (an empty table, or bands whose
// key is NaN, which are left out).
func interpolateBands(table []performanceBand, key func(performanceBand) float64, x float64) (lPerH, rpm float64, ok bool) {
	if math.IsNaN(x) {
		return 0, 0, false
	}
	sorted := make([]performanceBand, 0, len(table))
	for _, b := range table {
		if !math.IsNaN(key(b)) {
			sorted = append(sorted, b)
		}
	}
	if len(sorted) == 0 {
		return 0, 0, false
	}
	sort.Slice(sorted, func(i, j int) bool { return key(sorted[i]) < key(sorted[j]) })

	first, last := sorted[0], sorted[len(sorted)-1]
	if x <= key(first) {
		return first.LPerH, first.RPM, true
	}
	if x >= key(last) {
		return last.LPerH, last.RPM, true
	}
	// x is strictly inside the table, so upper is the first band at or past it and
	// lower, the one before, is strictly short of it: the span is never zero.
	i := sort.Search(len(sorted), func(i int) bool { return key(sorted[i]) >= x })
	lower, upper := sorted[i-1], sorted[i]
	frac := (x - key(lower)) / (key(upper) - key(lower))
	return lower.LPerH + frac*(upper.LPerH-lower.LPerH), lower.RPM + frac*(upper.RPM-lower.RPM), true
}

// mostSampledBand returns the band with the most joined samples: the burn rate
// estimate_passage gives when there is no time to read it at. ok is false only
// when table is empty.
func mostSampledBand(table []performanceBand) (performanceBand, bool) {
	if len(table) == 0 {
		return performanceBand{}, false
	}
	best := table[0]
	for _, band := range table[1:] {
		if band.Samples > best.Samples {
			best = band
		}
	}
	return best, true
}
