package main

import (
	"slices"
	"testing"
	"time"
)

// The generic core grows to carry a second subsystem (ADR 0276, house bank): a
// reading that never moves inside one regime leaves that regime's fit.

// An engine's reading that holds one value inside a band stays in the fit there.
// Only a reading fitted where it reports leaves a band where it does not move.
func TestTrain_AnEnginesReadingConstantInOneBandStaysInThatBandsFit(t *testing.T) {
	feed := synthFeed("port", 21, []synthRun{
		gatedRun(daysAgo(10), 70, 1900), gatedRun(daysAgo(6), 70, 1900),
		gatedRun(daysAgo(9), 70, 1500), gatedRun(daysAgo(5), 70, 1500),
	})
	// Boost holds one value through the 1,900 rpm runs.
	rows := feed["propulsion.port.boostPressure"]
	for i := range rows {
		if rows[i].At.After(daysAgo(11)) && rows[i].At.Before(daysAgo(5.5)) && rows[i].At.Hour() >= 0 {
			// 1,900 rpm runs start at ten and six days ago, 1,500 rpm runs at nine and five.
			if inRun(rows[i].At, daysAgo(10), 80) || inRun(rows[i].At, daysAgo(6), 80) {
				rows[i].Value = 40000
			}
		}
	}
	m := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	high := regimeNamed(t, m, "port/rpm1800")
	low := regimeNamed(t, m, "port/rpm1400")
	if !high.Judged || !low.Judged {
		t.Fatalf("regimes = %+v / %+v", high, low)
	}
	if !slices.Contains(high.Features, "boostPressure") {
		t.Fatalf("an engine's reading that sat still in a band stays in its fit, as it always did: %v", high.Features)
	}
	if !slices.Contains(low.Features, "boostPressure") {
		t.Fatalf("boost moves at 1,500 rpm and stays: %v", low.Features)
	}
	if len(high.Mean) != len(high.Features) || len(high.Corr) != len(high.Features) {
		t.Fatalf("the learned figures do not match the readings: %+v", high.normalRegimeStats)
	}
}

func inRun(at, start time.Time, minutes int) bool {
	return !at.Before(start) && at.Before(start.Add(time.Duration(minutes)*time.Minute))
}
