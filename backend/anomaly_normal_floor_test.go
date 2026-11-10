package main

import (
	"math/rand"
	"testing"
	"time"
)

func TestNormalMinSD_TheFloorIsTheLargerOfTheObservedStepAndTheReadingsMinChange(t *testing.T) {
	if got := normalMinSD(1, 0.01); got != 1 {
		t.Errorf("a small observed step is floored at the minimum change: %v", got)
	}
	if got := normalMinSD(1, 3); got != 3 {
		t.Errorf("a larger observed step stands: %v", got)
	}
	if got := normalMinSD(0, 0.25); got != 0.25 {
		t.Errorf("no minimum change keeps the observed step: %v", got)
	}
}

// The engines say what a change worth a look is for each of their readings. The
// generic core knows none of them.
func TestEngineCandidates_CarryTheSmallestChangeWorthALook(t *testing.T) {
	want := map[string]float64{
		"temperature":                 1,
		"transmission.oilTemperature": 1,
		"exhaustTemperature":          10,
		"oilPressure":                 10000,
		"transmission.oilPressure":    10000,
		"boostPressure":               5000,
		"engineLoad":                  0.02,
		"fuel.rate":                   1.0 / 3600000,
		"revolutions":                 20.0 / 60,
	}
	subs := normalEngineSubjects(vesselView{Engines: []vesselEngineSetting{{Instance: "port"}}})
	if len(subs) != 1 || len(subs[0].Features) != len(want) {
		t.Fatalf("subjects = %+v", subs)
	}
	for _, f := range subs[0].Features {
		w, ok := want[f.Key]
		if !ok || f.MinChange < w*0.999999 || f.MinChange > w*1.000001 {
			t.Errorf("%s: MinChange %v, want %v", f.Key, f.MinChange, w)
		}
		if f.SIUnit == "" || f.SIUnit != sensorLimitUnit[f.Key] {
			t.Errorf("%s: SI unit %q is not the one the sensor limits name", f.Key, f.SIUnit)
		}
	}
}

// A coolant sender that steps in whole degrees and sits on one value 97% of the
// time averages to fractional minutes, so its observed step is tiny. A one-degree
// dip is not worth a look. A five-degree dip is.
func TestScore_AOneStepDipOnASteppedSenderIsNotUnusualButAFiveStepDipIs(t *testing.T) {
	runs := append(bandRuns(1810, 1990), bandRuns(2010, 2190)...)
	for i := range runs {
		runs[i].Start = runs[i].Start.Add(time.Duration(i) * 7 * time.Hour)
	}
	trainFeed := synthFeed("port", 61, runs)
	rng := rand.New(rand.NewSource(7))
	step := func(rows []normalMinuteRow, dipEvery int) {
		for i := range rows {
			v := 353.15 + 0.01*rng.Float64()
			if dipEvery > 0 && i%dipEvery == 0 {
				v -= 1
			}
			rows[i].Value = v
		}
	}
	step(trainFeed["propulsion.port.temperature"], 33)
	model := normalModelFile{Subsystems: map[string]normalSubsystemModel{
		normalSubsystemEngines: newTrainHarness(trainFeed, "port").train(t, normalSubsystemModel{})}}
	if !model.Subsystems[normalSubsystemEngines].Regimes["port/rpm1800"].Judged {
		t.Fatal("setup: band not judged")
	}

	score := func(dip float64) unusualResult {
		feed := steadyScoreFeed(62)
		step(feed["propulsion.port.temperature"], 0)
		bend(feed, "propulsion.port.temperature", 40, 60, func(v float64) float64 { return v - dip })
		return newScoreCase(model, feed).score(24 * time.Hour)
	}
	if res := score(1); len(res.Stretches) != 0 {
		t.Fatalf("a one-degree dip flagged: %+v", res.Stretches)
	}
	res := score(5)
	if len(res.Stretches) != 1 || res.Stretches[0].Top.Key != "temperature" || res.Stretches[0].Top.Direction != "low" {
		t.Fatalf("a five-degree dip must be found: %+v", res.Stretches)
	}
}
