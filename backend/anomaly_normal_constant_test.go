package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

// coolantRuns builds three starts of an engine: the coolant climbs from ambient
// over warm minutes, then the judged minutes follow. hold gives the judged
// minute's value. It returns the running minutes (warm-up included), the judged
// ones, and the series.
func coolantRuns(warm, judged int, hold func(i int) float64) (running, gated []time.Time, values map[time.Time]float64) {
	values = map[time.Time]float64{}
	for s := 0; s < 3; s++ {
		base := selectBase.Add(time.Duration(s) * 4 * time.Hour)
		for i := 0; i < warm+judged; i++ {
			at := base.Add(time.Duration(i) * time.Minute)
			running = append(running, at)
			if i < warm {
				values[at] = 298.15 + (347.15-298.15)*float64(i)/float64(warm-1) // 25 to 74 degC
				continue
			}
			gated = append(gated, at)
			values[at] = hold(i - warm)
		}
	}
	return running, gated, values
}

func coolantEvidence(values map[time.Time]float64) featureEvidence {
	return featureEvidence{
		Feature: normalFeature{Key: "temperature", Path: "propulsion.port.temperature", SIUnit: "K", Explain: true, Required: true},
		Source:  "can0.1", Values: values,
	}
}

// Coolant on a thermostat holds one value for nearly every judged minute. It is
// not a dead sender: it climbs from ambient after each start, and that is in the
// running minutes the constant rule looks at.
func TestSelectFeatures_AThermostatHeldCoolantThatWarmsUpAfterEachStartIsUsed(t *testing.T) {
	running, gated, values := coolantRuns(15, 100, func(i int) float64 {
		if i%50 == 7 { // 2 of 100 judged minutes a dip of one degree
			return 346.15
		}
		return 347.15
	})
	v := verdictOf(t, selectFeatures([]featureEvidence{coolantEvidence(values)}, gated, running, nil, noLimits), "temperature")
	if !v.Used {
		t.Fatalf("a required reading held at one value by a thermostat is not constant: %+v", v)
	}
}

// A sender that reads 37.6 degC from the first running minute to the last,
// warm-up included, never changed at all.
func TestSelectFeatures_AReadingThatNeverChangedEvenWhileTheEngineWarmedUpIsConstant(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	running, gated, _ := coolantRuns(15, 100, nil2)
	values := map[time.Time]float64{}
	for _, at := range running {
		values[at] = 310.75 + 1e-13*rng.Float64()
	}
	ev := coolantEvidence(values)
	ev.Feature.Required = false
	ev.Feature.Key, ev.Feature.Path = "transmission.oilTemperature", "propulsion.port.transmission.oilTemperature"
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, running, nil, noLimits), "transmission.oilTemperature")
	if v.Used || !strings.Contains(v.Reason, "never changed while running") {
		t.Fatalf("verdict = %+v", v)
	}
}

func nil2(int) float64 { return 0 }

// A trainer run on an engine whose coolant is held by a thermostat keeps the
// coolant and learns bands.
func TestTrain_AThermostatHeldCoolantDoesNotCostTheEngineItsBands(t *testing.T) {
	feed := synthFeed("port", 70, goodRuns())
	rows := feed["propulsion.port.temperature"]
	for i := range rows {
		off := rows[i].At.Sub(rows[0].At)
		_ = off
	}
	// Each run's first ten minutes climb from ambient. The rest hold one value
	// with a one-degree dip in 2% of minutes.
	runStart := map[time.Time]bool{}
	for _, r := range goodRuns() {
		runStart[r.Start.Truncate(time.Minute)] = true
	}
	var start time.Time
	n := 0
	for i := range rows {
		if runStart[rows[i].At] {
			start, n = rows[i].At, 0
		}
		minute := int(rows[i].At.Sub(start) / time.Minute)
		switch {
		case minute < 10:
			rows[i].Value = 298.15 + 4.9*float64(minute)
		case n%50 == 7:
			rows[i].Value = 346.15
		default:
			rows[i].Value = 347.15
		}
		n++
	}
	m := newTrainHarness(feed, "port").train(t, normalSubsystemModel{})
	if p := m.Subjects["port"].Problem; p != "" {
		t.Fatalf("subject problem: %q", p)
	}
	if r := regimeNamed(t, m, "port/rpm1800"); !r.Judged {
		t.Fatalf("regime = %+v", r)
	}
}
