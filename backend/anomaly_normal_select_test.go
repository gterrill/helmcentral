package main

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

var selectBase = time.Date(2026, 10, 20, 8, 0, 0, 0, time.UTC)

func selectMinutes(n int) []time.Time {
	out := make([]time.Time, n)
	for i := range out {
		out[i] = selectBase.Add(time.Duration(i) * time.Minute)
	}
	return out
}

// evidenceFor builds a feature's evidence from a value function over the gated
// minutes; a NaN-free nil return leaves the minute without a reading.
func featureEvidenceFor(key, path string, gated []time.Time, value func(i int) (float64, bool)) featureEvidence {
	values := map[time.Time]float64{}
	for i, at := range gated {
		if v, ok := value(i); ok {
			values[at] = v
		}
	}
	return featureEvidence{Feature: normalFeature{Key: key, Path: path, SIUnit: "Pa", Explain: true}, Source: "can0.1", Values: values, Wording: normalEngineWording}
}

func noLimits(string, float64) rangeVerdict { return rangeNotApplicable }

func verdictOf(t *testing.T, v []normalFeatureVerdict, key string) normalFeatureVerdict {
	t.Helper()
	for _, x := range v {
		if x.Key == key {
			return x
		}
	}
	t.Fatalf("no verdict for %s in %+v", key, v)
	return normalFeatureVerdict{}
}

func TestSelectFeatures_AVaryingReadingIsUsed(t *testing.T) {
	gated := selectMinutes(300)
	rng := rand.New(rand.NewSource(1))
	ev := featureEvidenceFor("oilPressure", "propulsion.port.oilPressure", gated, func(int) (float64, bool) { return 300000 + 5000*rng.NormFloat64(), true })
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "oilPressure")
	if !v.Used || v.Reason != "" {
		t.Fatalf("verdict = %+v, want used", v)
	}
}

// A placeholder that reads 310.8 with the float noise an average leaves is one
// value: nothing about the sender's path is written into the rule.
func TestSelectFeatures_ConstantWithFloatNoiseIsLeftOutByTheData(t *testing.T) {
	gated := selectMinutes(300)
	ev := featureEvidenceFor("transmission.oilTemperature", "propulsion.port.transmission.oilTemperature", gated, func(i int) (float64, bool) {
		return []float64{310.79999999999967, 310.80000000000007, 310.8, 310.7999999999997}[i%4], true
	})
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "transmission.oilTemperature")
	if v.Used || !strings.Contains(v.Reason, "never changed while running") {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestSelectFeatures_TwoDistinctValuesIsAConstant(t *testing.T) {
	gated := selectMinutes(300)
	ev := featureEvidenceFor("boostPressure", "p", gated, func(i int) (float64, bool) { return float64(i % 2), true })
	if v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "boostPressure"); v.Used {
		t.Fatalf("two values is not a reading that varies: %+v", v)
	}
}

func TestSelectFeatures_OneValueInNearlyEveryMinuteIsAConstant(t *testing.T) {
	gated := selectMinutes(1000)
	ev := featureEvidenceFor("engineLoad", "p", gated, func(i int) (float64, bool) {
		switch i {
		case 10:
			return 0.5, true
		case 20:
			return 0.6, true
		case 30:
			return 0.7, true
		}
		return 0.32, true
	})
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "engineLoad")
	if v.Used {
		t.Fatalf("a reading stuck at one value for 99.7%% of minutes is a placeholder: %+v", v)
	}
}

func TestSelectFeatures_PresenceUnderEightyPercentIsLeftOut(t *testing.T) {
	gated := selectMinutes(1000)
	mk := func(missingEvery int) featureEvidence {
		return featureEvidenceFor("oilPressure", "p", gated, func(i int) (float64, bool) {
			if missingEvery > 0 && i%missingEvery == 0 {
				return 0, false
			}
			return float64(i % 97), true
		})
	}
	// The first minute is always present so the reading's first sighting is the
	// first gated minute and presence is judged over the whole window.
	left := mk(0)
	for i, at := range gated {
		if i%10 < 3 && i > 0 {
			delete(left.Values, at)
		}
	}
	v := verdictOf(t, selectFeatures([]featureEvidence{left}, gated, gated, nil, noLimits), "oilPressure")
	if v.Used || !strings.Contains(v.Reason, "missing in 30% of steady running minutes") {
		t.Fatalf("70%% present: %+v", v)
	}
	ok := mk(0)
	for i, at := range gated {
		if i%10 < 1 && i > 0 {
			delete(ok.Values, at)
		}
	}
	if v := verdictOf(t, selectFeatures([]featureEvidence{ok}, gated, gated, nil, noLimits), "oilPressure"); !v.Used {
		t.Fatalf("90%% present must be used: %+v", v)
	}
}

func TestSelectFeatures_OverFivePercentOutsidePhysicalLimitsIsLeftOut(t *testing.T) {
	gated := selectMinutes(1000)
	limit := func(_ string, v float64) rangeVerdict {
		if v > 1000 {
			return rangeOutOfRange
		}
		return rangeValid
	}
	make5 := func(bad int) featureEvidence {
		return featureEvidenceFor("oilPressure", "p", gated, func(i int) (float64, bool) {
			if i < bad {
				return 5000 + float64(i), true
			}
			return float64(i % 89), true
		})
	}
	v := verdictOf(t, selectFeatures([]featureEvidence{make5(60)}, gated, gated, nil, limit), "oilPressure")
	if v.Used || !strings.Contains(v.Reason, "outside what the sender can read") {
		t.Fatalf("6%% outside: %+v", v)
	}
	if v := verdictOf(t, selectFeatures([]featureEvidence{make5(40)}, gated, gated, nil, limit), "oilPressure"); !v.Used {
		t.Fatalf("4%% outside is within tolerance: %+v", v)
	}
}

func TestSelectFeatures_AnIgnoredSensorIsLeftOutAndSaysSo(t *testing.T) {
	gated := selectMinutes(300)
	ev := featureEvidenceFor("oilPressure", "propulsion.port.oilPressure", gated, func(i int) (float64, bool) { return float64(i % 53), true })
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, map[string]bool{"propulsion.port.oilPressure": true}, noLimits), "oilPressure")
	if v.Used || v.Reason != "switched off" {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestSelectFeatures_TwoSourcesIsLeftOut(t *testing.T) {
	gated := selectMinutes(300)
	ev := featureEvidenceFor("oilPressure", "p", gated, func(i int) (float64, bool) { return float64(i % 53), true })
	ev.TwoSources = true
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "oilPressure")
	if v.Used || v.Reason != "two sources report it" {
		t.Fatalf("verdict = %+v", v)
	}
}

func TestChooseSender_FlagsOnlyASimultaneousOverlap(t *testing.T) {
	at := selectMinutes(100)
	row := func(src string, i int) normalMinuteRow {
		return normalMinuteRow{Source: src, At: at[i], Value: float64(i)}
	}
	var rows []normalMinuteRow
	for i := 0; i < 100; i++ {
		rows = append(rows, row("a", i))
	}
	// b reports 5 of the same minutes: 5% overlap, below the 10% line.
	for i := 0; i < 5; i++ {
		rows = append(rows, row("b", i))
	}
	pick := chooseSender(rows)
	if pick.Source != "a" || len(pick.Values) != 100 || pick.Two {
		t.Fatalf("5%% overlap: source %q, %d minutes, two=%v", pick.Source, len(pick.Values), pick.Two)
	}
	for i := 5; i < 15; i++ { // 15 minutes of overlap in all
		rows = append(rows, row("b", i))
	}
	if !chooseSender(rows).Two {
		t.Fatal("15% of the same minutes from another source is two sources reporting it")
	}
}

// A hand-over from one gateway to another, with no minute reported by both, is
// one reading and not two sources. The sender of the latest minute is the one
// chosen, and with 40 minutes it is still relearning.
func TestChooseSender_ASequentialHandoverIsNotTwoSourcesAndTakesTheLatest(t *testing.T) {
	at := selectMinutes(100)
	var rows []normalMinuteRow
	for i := 0; i < 60; i++ {
		rows = append(rows, normalMinuteRow{Source: "old", At: at[i], Value: 1})
	}
	for i := 60; i < 100; i++ {
		rows = append(rows, normalMinuteRow{Source: "new", At: at[i], Value: 2})
	}
	pick := chooseSender(rows)
	if pick.Two {
		t.Fatal("no shared minute, so not two sources")
	}
	if pick.Source != "new" || len(pick.Values) != 40 || !pick.Changed {
		t.Fatalf("the latest sender is chosen and is relearning: %+v", pick)
	}
	// With an hour behind it the same hand-over is settled.
	for i := 0; i < 20; i++ {
		rows = append(rows, normalMinuteRow{Source: "new", At: at[0].Add(-time.Duration(i+1) * time.Minute), Value: 2})
	}
	if pick := chooseSender(rows); pick.Source != "new" || pick.Changed {
		t.Fatalf("60 minutes of the latest sender settle it: %+v", pick)
	}
}

// A repaired sender: absent for the first 700 gated minutes, present in every
// one since. It is judged over the minutes since it first appeared and is
// joining while it has under an hour.
func TestSelectFeatures_ASenderThatStartsWorkingIsNotHeldOutForTheWholeWindow(t *testing.T) {
	gated := selectMinutes(1000)
	rng := rand.New(rand.NewSource(2))
	ev := featureEvidenceFor("exhaustTemperature", "p", gated, func(i int) (float64, bool) {
		if i < 700 {
			return 0, false
		}
		return 600 + 20*rng.NormFloat64(), true
	})
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "exhaustTemperature")
	if !v.Used {
		t.Fatalf("a reading present in every minute since it appeared is used: %+v", v)
	}
	if v.Joining {
		t.Fatalf("300 minutes is past the hour it needs: %+v", v)
	}

	gatedLate := selectMinutes(1000)
	late := featureEvidenceFor("exhaustTemperature", "p", gatedLate, func(i int) (float64, bool) {
		if i < 986 {
			return 0, false
		}
		return 600 + 20*rng.NormFloat64(), true
	})
	v = verdictOf(t, selectFeatures([]featureEvidence{late}, gatedLate, gatedLate, nil, noLimits), "exhaustTemperature")
	if !v.Used || !v.Joining || v.JoiningMinutes != 14 {
		t.Fatalf("14 minutes so far: %+v", v)
	}
	if got := v.Status(); got != "joins after 60 minutes, 14 so far" {
		t.Fatalf("status = %q", got)
	}
}

// A reading that vanished is not excused by the late-joiner rule.
func TestSelectFeatures_ASenderThatDiedIsStillLeftOut(t *testing.T) {
	gated := selectMinutes(1000)
	ev := featureEvidenceFor("oilPressure", "p", gated, func(i int) (float64, bool) {
		if i > 300 {
			return 0, false
		}
		return float64(i % 61), true
	})
	if v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "oilPressure"); v.Used {
		t.Fatalf("present for 30%% of the window: %+v", v)
	}
}

func TestSelectFeatures_NoReadingsAtAllSaysSo(t *testing.T) {
	gated := selectMinutes(120)
	ev := featureEvidence{Feature: normalFeature{Key: "fuel.rate", Path: "p"}, Values: map[time.Time]float64{}}
	v := verdictOf(t, selectFeatures([]featureEvidence{ev}, gated, gated, nil, noLimits), "fuel.rate")
	if v.Used || v.Reason != "no readings" {
		t.Fatalf("verdict = %+v", v)
	}
}
