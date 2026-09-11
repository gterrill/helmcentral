package main

import (
	"testing"
	"time"
)

func tp(sec int64, value float64) telemetryPoint {
	return telemetryPoint{Timestamp: time.Unix(sec, 0).UTC(), Value: value}
}

// ── join and unit conversion ────────────────────────────────────────────

func TestBuildPerformanceTable_UnitConversionSingleInstance(t *testing.T) {
	// 5.0 m/s = ~9.7 kts (band 9). 0.000013 m3/s ~= 46.8 L/h. 30.25 Hz = 1815 rpm.
	sog := []telemetryPoint{tp(0, 5.0), tp(600, 5.0), tp(1200, 5.0)}
	fuel := [][]telemetryPoint{{tp(0, 0.000013), tp(600, 0.000013), tp(1200, 0.000013)}}
	rpm := []telemetryPoint{tp(0, 30.25), tp(600, 30.25), tp(1200, 30.25)}

	table := buildPerformanceTable(sog, fuel, rpm, 4.0)
	if len(table) != 1 {
		t.Fatalf("expected exactly 1 band, got %d: %+v", len(table), table)
	}
	band := table[0]
	if band.SOGKtsMin != 9 {
		t.Errorf("expected band 9 (5.0 m/s ~= 9.7kts), got %v", band.SOGKtsMin)
	}
	if band.LPerH != 46.8 {
		t.Errorf("expected l_per_h=46.8 from 0.000013 m3/s, got %v", band.LPerH)
	}
	if band.RPM != 1815 {
		t.Errorf("expected rpm=1815 from 30.25 Hz, got %v", band.RPM)
	}
	if band.Samples != 3 {
		t.Errorf("expected 3 samples, got %d", band.Samples)
	}
}

func TestBuildPerformanceTable_SumsFuelRateAcrossInstances(t *testing.T) {
	sog := []telemetryPoint{tp(0, 5.0), tp(600, 5.0), tp(1200, 5.0)}
	fuel := [][]telemetryPoint{
		{tp(0, 0.000013), tp(600, 0.000013), tp(1200, 0.000013)},
		{tp(0, 0.000013), tp(600, 0.000013), tp(1200, 0.000013)},
	}
	rpm := []telemetryPoint{tp(0, 30.25), tp(600, 30.25), tp(1200, 30.25)}

	table := buildPerformanceTable(sog, fuel, rpm, 4.0)
	if len(table) != 1 {
		t.Fatalf("expected exactly 1 band, got %d: %+v", len(table), table)
	}
	if got, want := table[0].LPerH, 93.6; got != want {
		t.Errorf("expected l_per_h=%v (two instances summed), got %v", want, got)
	}
}

func TestBuildPerformanceTable_MisalignedTimestampsDoNotJoin(t *testing.T) {
	// Fuel and rpm samples exist at completely different timestamps from
	// sog, so nothing should ever join.
	sog := []telemetryPoint{tp(0, 5.0), tp(600, 5.0), tp(1200, 5.0)}
	fuel := [][]telemetryPoint{{tp(1, 0.000013), tp(601, 0.000013), tp(1201, 0.000013)}}
	rpm := []telemetryPoint{tp(2, 30.25), tp(602, 30.25), tp(1202, 30.25)}

	table := buildPerformanceTable(sog, fuel, rpm, 4.0)
	if len(table) != 0 {
		t.Fatalf("expected no bands when timestamps never align, got %+v", table)
	}
}

func TestBuildPerformanceTable_OneMissingInstanceSkipsTheWholeTimestamp(t *testing.T) {
	// Three timestamps' worth of sog and rpm, but the second fuel instance
	// only reports at two of them - the third timestamp must be dropped
	// entirely, not summed from the one instance that did report.
	sog := []telemetryPoint{tp(0, 5.0), tp(600, 5.0), tp(1200, 5.0)}
	fuel := [][]telemetryPoint{
		{tp(0, 0.000013), tp(600, 0.000013), tp(1200, 0.000013)},
		{tp(0, 0.000013), tp(600, 0.000013)}, // missing at t=1200
	}
	rpm := []telemetryPoint{tp(0, 30.25), tp(600, 30.25), tp(1200, 30.25)}

	table := buildPerformanceTable(sog, fuel, rpm, 4.0)
	// Only 2 samples now join (below the 3-sample floor), so the band is
	// dropped entirely - which itself proves the third timestamp did not
	// silently count on the one instance that reported.
	if len(table) != 0 {
		t.Fatalf("expected the band to be dropped (only 2 complete samples), got %+v", table)
	}
}

// ── minSOGKts floor ─────────────────────────────────────────────────────

func TestBuildPerformanceTable_BelowMinSOGIsExcluded(t *testing.T) {
	// 1.0 m/s is well under a 4kt floor.
	sog := []telemetryPoint{tp(0, 1.0), tp(600, 1.0), tp(1200, 1.0)}
	fuel := [][]telemetryPoint{{tp(0, 0.000001), tp(600, 0.000001), tp(1200, 0.000001)}}
	rpm := []telemetryPoint{tp(0, 5), tp(600, 5), tp(1200, 5)}

	table := buildPerformanceTable(sog, fuel, rpm, 4.0)
	if len(table) != 0 {
		t.Fatalf("expected no bands below the minSOGKts floor, got %+v", table)
	}
}

// ── banding and the 3-sample floor ──────────────────────────────────────

func TestBuildPerformanceTable_BandsByWholeKnotAndDropsThinBands(t *testing.T) {
	// Band 9: three samples at slightly different speeds, all in [9,10).
	// Band 12: only two samples - must be dropped.
	sog := []telemetryPoint{
		tp(0, 9.2/metersPerSecondToKnots),
		tp(600, 9.4/metersPerSecondToKnots),
		tp(1200, 9.6/metersPerSecondToKnots),
		tp(1800, 12.1/metersPerSecondToKnots),
		tp(2400, 12.3/metersPerSecondToKnots),
	}
	fuel := [][]telemetryPoint{{
		tp(0, 0.000013), tp(600, 0.000013), tp(1200, 0.000013),
		tp(1800, 0.00002), tp(2400, 0.00002),
	}}
	rpm := []telemetryPoint{
		tp(0, 30.25), tp(600, 30.25), tp(1200, 30.25),
		tp(1800, 35), tp(2400, 35),
	}

	table := buildPerformanceTable(sog, fuel, rpm, 4.0)
	if len(table) != 1 {
		t.Fatalf("expected exactly 1 surviving band (band 12 has only 2 samples), got %d: %+v", len(table), table)
	}
	if table[0].SOGKtsMin != 9 {
		t.Errorf("expected the surviving band to be band 9, got %v", table[0].SOGKtsMin)
	}
	if table[0].Samples != 3 {
		t.Errorf("expected 3 samples in band 9, got %d", table[0].Samples)
	}
}

// ── estimateAtSpeed ─────────────────────────────────────────────────────

func performanceTableFixture() []performanceBand {
	return []performanceBand{
		{SOGKtsMin: 8, SOGKtsMean: 8.5, LPerH: 27, RPM: 1600, Samples: 10},
		{SOGKtsMin: 9, SOGKtsMean: 9.5, LPerH: 47, RPM: 1815, Samples: 20},
	}
}

func TestEstimateAtSpeed_InterpolatesBetweenTwoBands(t *testing.T) {
	lPerH, rpm, ok := estimateAtSpeed(performanceTableFixture(), 9.0)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got, want := lPerH, 37.0; got != want {
		t.Errorf("expected l_per_h=%v (midpoint of 27 and 47), got %v", want, got)
	}
	if got, want := rpm, 1707.5; got != want {
		t.Errorf("expected rpm=%v (midpoint of 1600 and 1815), got %v", want, got)
	}
}

func TestEstimateAtSpeed_ClampsBelowTheLowestBand(t *testing.T) {
	lPerH, rpm, ok := estimateAtSpeed(performanceTableFixture(), 3.0)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if lPerH != 27 || rpm != 1600 {
		t.Errorf("expected clamping to the lowest band's values, got l_per_h=%v rpm=%v", lPerH, rpm)
	}
}

func TestEstimateAtSpeed_ClampsAboveTheHighestBand(t *testing.T) {
	lPerH, rpm, ok := estimateAtSpeed(performanceTableFixture(), 20.0)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if lPerH != 47 || rpm != 1815 {
		t.Errorf("expected clamping to the highest band's values, got l_per_h=%v rpm=%v", lPerH, rpm)
	}
}

func TestEstimateAtSpeed_EmptyTableIsNotOK(t *testing.T) {
	_, _, ok := estimateAtSpeed(nil, 9.0)
	if ok {
		t.Fatalf("expected ok=false for an empty table")
	}
}

// ── mostSampledBand ─────────────────────────────────────────────────────

func TestMostSampledBand_PicksTheHighestSampleCount(t *testing.T) {
	table := []performanceBand{
		{SOGKtsMin: 6, SOGKtsMean: 6.6, LPerH: 25, RPM: 1400, Samples: 5},
		{SOGKtsMin: 7, SOGKtsMean: 7.4, LPerH: 27, RPM: 1500, Samples: 40},
		{SOGKtsMin: 9, SOGKtsMean: 9.5, LPerH: 47, RPM: 1815, Samples: 12},
	}
	got, ok := mostSampledBand(table)
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got.SOGKtsMin != 7 {
		t.Errorf("expected the band with 40 samples (band 7), got %+v", got)
	}
}

func TestMostSampledBand_EmptyTableIsNotOK(t *testing.T) {
	_, ok := mostSampledBand(nil)
	if ok {
		t.Fatalf("expected ok=false for an empty table")
	}
}
