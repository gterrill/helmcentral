package main

import (
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"
)

var fillBase = time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)

// fillAt is clock hh.mm on the fixture day, the way the operator read the
// capture (10.53 is 10:53).
func fillAt(hour, minute int) time.Time {
	return fillBase.Add(time.Duration(hour)*time.Hour + time.Duration(minute)*time.Minute)
}

type fillPoint struct {
	at    time.Time
	level float64 // percent
}

// freshWater3 is tanks.freshWater.3.currentLevel as captured on the boat,
// 1-minute means, as a percent.
func freshWater3() []fillPoint {
	var out []fillPoint
	add := func(hour, minute int, ratio float64) {
		out = append(out, fillPoint{fillAt(hour, minute), ratio * 100})
	}
	for _, p := range []struct {
		m int
		r float64
	}{
		{53, .42}, {54, .42}, {55, .43}, {56, .44}, {57, .46}, {58, .46}, {59, .48},
	} {
		add(10, p.m, p.r)
	}
	for i, r := range []float64{.49, .50, .52, .53, .54, .55, .56, .58, .59, .61, .62, .63, .65, .66, .68, .69, .70, .72, .72, .72, .74, .74, .76, .77} {
		add(11, i, r)
	}
	// fill paused: flat .77 from 11.24 through 11.46
	for m := 24; m <= 46; m++ {
		add(11, m, .77)
	}
	for i, r := range []float64{.78, .79, .80, .82, .82, .84, .85} {
		add(11, 47+i, r)
	}
	return out
}

const fillTankID = "freshWater.3"

func TestTankFillingFollowsTheFixtureThroughPauseAndResume(t *testing.T) {
	tr := newTankFillTracker()
	filling := map[time.Time]bool{}
	var toFull *float64
	for _, p := range freshWater3() {
		tr.observe(fillTankID, "water", p.level, 0, p.at)
		on, secs := tr.status(fillTankID, p.at)
		filling[p.at] = on
		toFull = secs
	}
	check := func(hour, minute int, want bool) {
		t.Helper()
		if got := filling[fillAt(hour, minute)]; got != want {
			t.Errorf("filling at %02d.%02d = %v, want %v", hour, minute, got, want)
		}
	}
	check(10, 54, false)
	check(10, 55, false)
	check(10, 57, true) // inside four minutes of the first rise
	check(11, 10, true)
	check(11, 25, true)
	check(11, 28, true)
	check(11, 30, false) // six minutes flat after the last rise
	check(11, 46, false)
	// The pause is not held by the slow rule: hose rises are a minute or two
	// apart. Filling comes back on the second rise after the resume.
	check(11, 47, false)
	check(11, 49, true)
	check(11, 53, true)

	if toFull == nil {
		t.Fatalf("seconds_to_full at 11.53 = nil, want an estimate")
	}
	// 15% to go at the fixture's late rate of 1 to 1.5 %/min.
	if *toFull < 8*60 || *toFull > 16*60 {
		t.Errorf("seconds_to_full at 11.53 = %.0f s, want between 8 and 16 minutes", *toFull)
	}
}

func TestTankFillingGivesATimeThroughoutAHoseFillWithSlowSteps(t *testing.T) {
	tr := newTankFillTracker()
	from, to := fillAt(11, 3), fillAt(11, 23)
	checked := 0
	for _, p := range freshWater3() {
		tr.observe(fillTankID, "water", p.level, 0, p.at)
		if p.at.Before(from) || p.at.After(to) {
			continue
		}
		// The tile polls every few seconds, not only when a level changes.
		for s := 0; s < 60; s += 10 {
			at := p.at.Add(time.Duration(s) * time.Second)
			on, secs := tr.status(fillTankID, at)
			if !on {
				continue
			}
			checked++
			if secs == nil {
				t.Errorf("filling at %s with no seconds_to_full", at.Format("15.04.05"))
			}
		}
	}
	if checked < 15 {
		t.Errorf("only %d of the minutes from 11.03 to 11.23 were filling, want most", checked)
	}
}

func TestTankFillingGivesATimeOnAHoseWithStepsTwoAndAHalfMinutesApart(t *testing.T) {
	tr := newTankFillTracker()
	start := fillAt(9, 0)
	var checked int
	// 2.5% every 150 s from 30%, polled every 10 s for 25 minutes.
	for sec := 0; sec <= 25*60; sec += 10 {
		at := start.Add(time.Duration(sec) * time.Second)
		level := 30 + 2.5*float64(sec/150)
		tr.observe(fillTankID, "water", level, 0, at)
		on, secs := tr.status(fillTankID, at)
		if !on || sec < 12*60 {
			continue
		}
		checked++
		if secs == nil {
			t.Fatalf("filling at %d s with no seconds_to_full", sec)
		}
	}
	if checked < 50 {
		t.Errorf("only %d polls were filling after 12 minutes, want most", checked)
	}
}

func TestTankFillingOnTheRawTwoSecondCapture(t *testing.T) {
	file, err := os.Open("testdata/tank-fill-fw3-raw.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	// The capture runs from 85% to full at 02:06:50 and sits at 100% until 02:19.
	tr := newTankFillTracker()
	var early, midFill, afterFull bool
	var secs *float64
	for _, row := range rows[1:] {
		at, err := time.Parse(time.RFC3339Nano, row[0])
		if err != nil {
			t.Fatal(err)
		}
		ratio, err := strconv.ParseFloat(row[1], 64)
		if err != nil {
			t.Fatal(err)
		}
		tr.observe(fillTankID, "water", ratio*100, 0, at)
		on, s := tr.status(fillTankID, at)
		if at.Before(time.Date(2026, 11, 12, 1, 57, 0, 0, time.UTC)) && on {
			early = true // before the second rise at 01:57:13
		}
		if !midFill && !at.Before(time.Date(2026, 11, 12, 2, 0, 0, 0, time.UTC)) {
			midFill, secs = on, s // 92.4% at 02:00, full 6.9 minutes later
		}
		if at.After(time.Date(2026, 11, 12, 2, 14, 0, 0, time.UTC)) && on {
			afterFull = true
		}
	}
	if early {
		t.Errorf("filling before the second rising step")
	}
	if !midFill {
		t.Fatalf("not filling at 02:00")
	}
	if secs == nil || *secs < 4*60 || *secs > 12*60 {
		t.Errorf("seconds_to_full at 02:00 = %v, want 4 to 12 minutes (the tank was full 6.9 minutes later)", secs)
	}
	if afterFull {
		t.Errorf("still filling more than 6 minutes after the tank stopped rising at 100%%")
	}
}

func TestTankFillingIgnoresAFlatTankJitteringOneStep(t *testing.T) {
	tr := newTankFillTracker()
	for i := 0; i < 120; i++ {
		level := 50.0
		if i%2 == 1 {
			level = 52.5
		}
		at := fillAt(8, 0).Add(time.Duration(i) * time.Minute)
		tr.observe(fillTankID, "water", level, 0, at)
		if on, _ := tr.status(fillTankID, at); on {
			t.Fatalf("flat jittering tank shows filling at minute %d", i)
		}
	}
}

func TestTankFillingIgnoresNoiseBelowTheFloor(t *testing.T) {
	tr := newTankFillTracker()
	for i := 0; i < 60; i++ {
		at := fillAt(8, 0).Add(time.Duration(i) * time.Minute)
		// Under 2 points in any half hour: creep, not a fill.
		tr.observe(fillTankID, "water", 50+0.05*float64(i), 0, at)
		if on, _ := tr.status(fillTankID, at); on {
			t.Fatalf("0.05%% steps show filling at minute %d", i)
		}
	}
}

func TestTankFillingNeverOnAFallingTank(t *testing.T) {
	tr := newTankFillTracker()
	for i := 0; i < 60; i++ {
		at := fillAt(8, 0).Add(time.Duration(i) * time.Minute)
		tr.observe(fillTankID, "water", 80-2.5*float64(i)/2, 0, at)
		if on, _ := tr.status(fillTankID, at); on {
			t.Fatalf("falling tank shows filling at minute %d", i)
		}
	}
}

func TestTankFillingNeedsTheBoatStoppedAndSOGKnown(t *testing.T) {
	for _, tc := range []struct {
		name string
		sog  float64
		want bool
	}{
		{"stopped", 0.2, true},
		{"just under a knot", 0.99, true},
		{"one knot", 1.0, false},
		{"under way", 6, false},
		{"unknown", -1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTankFillTracker()
			var on bool
			for i := 0; i < 8; i++ {
				at := fillAt(9, 0).Add(time.Duration(i) * time.Minute)
				tr.observe(fillTankID, "water", 40+2.5*float64(i), tc.sog, at)
				on, _ = tr.status(fillTankID, at)
			}
			if on != tc.want {
				t.Errorf("filling = %v, want %v", on, tc.want)
			}
		})
	}
}

func TestTankFillingNeverForWaste(t *testing.T) {
	tr := newTankFillTracker()
	for i := 0; i < 8; i++ {
		tr.observe("blackWater.1", "waste", 10+2.5*float64(i), 0, fillAt(9, 0).Add(time.Duration(i)*time.Minute))
	}
	if on, _ := tr.status("blackWater.1", fillAt(9, 7)); on {
		t.Errorf("waste tank shows filling")
	}
}

func TestTankFillingSeesASlowWatermakerAfterTheThirdStepAndStaysOnBetweenSteps(t *testing.T) {
	tr := newTankFillTracker()
	filling := map[int]bool{}
	// A 2.5% step every ten minutes, the last at minute 50, then nothing.
	for minute := 0; minute <= 70; minute++ {
		level := 30 + 2.5*float64(min(minute, 50)/10)
		at := fillAt(9, 0).Add(time.Duration(minute) * time.Minute)
		tr.observe(fillTankID, "water", level, 0, at)
		filling[minute], _ = tr.status(fillTankID, at)
	}
	for _, minute := range []int{0, 10, 20, 29} {
		if filling[minute] {
			t.Errorf("filling at minute %d, before the third step", minute)
		}
	}
	for _, minute := range []int{30, 35, 39, 40, 45, 49, 50, 55} {
		if !filling[minute] {
			t.Errorf("slow watermaker not filling at minute %d", minute)
		}
	}
	// Off within 15 minutes of the last step, at minute 65.
	if filling[66] {
		t.Errorf("still filling 16 minutes after the last step")
	}
}

func TestTankFillingWatermakerEndsAtADipAndComesBackOnlyWithNewHighs(t *testing.T) {
	tr := newTankFillTracker()
	levelAt := map[int]float64{0: 30, 10: 32.5, 20: 35, 30: 37.5, 40: 40, 45: 37.5, 50: 40, 60: 42.5, 70: 45, 80: 47.5}
	level := 30.0
	filling := map[int]bool{}
	for minute := 0; minute <= 85; minute++ {
		if v, ok := levelAt[minute]; ok {
			level = v
		}
		at := fillAt(9, 0).Add(time.Duration(minute) * time.Minute)
		tr.observe(fillTankID, "water", level, 0, at)
		filling[minute], _ = tr.status(fillTankID, at)
	}
	if !filling[44] {
		t.Fatalf("not filling at minute 44, before the dip")
	}
	for minute := 45; minute < 50; minute++ {
		if filling[minute] {
			t.Errorf("filling at minute %d, with the level below its high after a step down", minute)
		}
	}
	// Recovering to the old high at minute 50 is not a rise: only a level above
	// every earlier one in the climb counts. Filling returns with the next
	// rises, at minute 80.
	if filling[50] {
		t.Errorf("filling at minute 50, on a recovery to the old high")
	}
	if !filling[80] {
		t.Errorf("not filling at minute 80, three new highs since the dip")
	}
}

func TestTankFillingNeverHoldsOnForAFlickerAfterAFill(t *testing.T) {
	tr := newTankFillTracker()
	var at time.Time
	for i := 0; i <= 8; i++ { // 80% to 100% in 2.5% steps, 100 s apart
		at = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		tr.observe(fillTankID, "water", 80+2.5*float64(i), 0, at)
	}
	flickerStart := at
	for m := 1; m <= 40; m++ {
		level := 97.5
		if m%2 == 1 {
			level = 100
		}
		at = flickerStart.Add(time.Duration(m) * time.Minute)
		tr.observe(fillTankID, "water", level, 0, at)
		if on, _ := tr.status(fillTankID, at); on && m > 7 {
			t.Fatalf("a 97.5/100 flicker shows filling at minute %d after the fill", m)
		}
	}
}

func TestTankFillingIgnoresOneStepArrivingAsTwoValues(t *testing.T) {
	tr := newTankFillTracker()
	tr.observe(fillTankID, "water", 50, 0, fillAt(9, 0))
	tr.observe(fillTankID, "water", 51.2, 0, fillAt(9, 5))
	tr.observe(fillTankID, "water", 52.4, 0, fillAt(9, 5).Add(10*time.Second))
	for _, d := range []time.Duration{10 * time.Second, time.Minute, 3 * time.Minute} {
		at := fillAt(9, 5).Add(d)
		tr.observe(fillTankID, "water", 52.4, 0, at)
		if on, _ := tr.status(fillTankID, at); on {
			t.Errorf("one step arriving as two values shows filling after %v", d)
		}
	}
}

func TestTankFillingEndsAtOnceOnAStepDown(t *testing.T) {
	tr := newTankFillTracker()
	var at time.Time
	for i := 0; i < 6; i++ {
		at = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		tr.observe(fillTankID, "water", 40+2.5*float64(i), 0, at)
	}
	if on, _ := tr.status(fillTankID, at); !on {
		t.Fatalf("not filling before the step down")
	}
	at = at.Add(5 * time.Second)
	tr.observe(fillTankID, "water", 50, 0, at) // 52.5 down to 50
	if on, secs := tr.status(fillTankID, at); on || secs != nil {
		t.Errorf("after a step down: filling=%v estimate=%v, want neither", on, secs)
	}
}

func TestAnnotateTankFillingFillsThePayloadFields(t *testing.T) {
	saved := tankFill
	tankFill = newTankFillTracker()
	defer func() { tankFill = saved }()
	for i := 0; i < 6; i++ {
		tankFill.observe("freshWater.1", "water", 40+2.5*float64(i), 0, fillAt(9, 0).Add(time.Duration(i)*time.Minute))
	}
	tanks := []tankLevelData{
		{ID: "freshWater.1", Kind: "water", LevelPercent: 52.5},
		{ID: "fuel.1", Kind: "fuel", LevelPercent: 60},
	}
	annotateTankFilling(tanks, fillAt(9, 5))
	if !tanks[0].Filling || tanks[0].SecondsToFull == nil {
		t.Errorf("filling tank = %+v, want filling with an estimate", tanks[0])
	}
	if tanks[1].Filling || tanks[1].SecondsToFull != nil {
		t.Errorf("idle tank = %+v, want not filling and no estimate", tanks[1])
	}
}

// feedTankSnapshot installs a telemetry snapshot holding one tank's
// currentLevel (a 0-1 ratio) as the delta stream would, and samples it.
func feedTankSnapshot(t *testing.T, path string, ratio float64, now time.Time) {
	t.Helper()
	globalSignalKSnapshot = snapshotWithSelfValues(map[string]any{path: ratio, "navigation.speedOverGround": 0.0})
	sampleTankFilling(vesselStateData{SpeedOverGroundKts: 0}, nil, now)
}

// feedStampedSnapshot is feedTankSnapshot with each path carrying the source
// timestamp the sender declared, so a reading can be old. A path missing from
// stamps arrives unstamped, which reads as just arrived.
func feedStampedSnapshot(t *testing.T, values map[string]any, stamps map[string]time.Time, sogKts float64, now time.Time) {
	t.Helper()
	snapshot := newSignalKSnapshot()
	var updates []signalKUpdate
	for path, value := range values {
		update := signalKUpdate{Values: []signalKValue{{Path: path, Value: value}}}
		if stamp, ok := stamps[path]; ok {
			update.Timestamp = stamp.UTC().Format(time.RFC3339)
		}
		updates = append(updates, update)
	}
	snapshot.applyDelta(signalKDelta{Context: "vessels.self", Updates: updates}, time.Now().UTC())
	snapshot.setSelfContext("vessels.self")
	globalSignalKSnapshot = snapshot
	sampleTankFilling(vesselStateData{SpeedOverGroundKts: sogKts}, nil, now)
}

func TestSampleTankFillingTreatsAStaleSpeedAsUnknown(t *testing.T) {
	withFreshTankTracker(t)
	old := time.Now().Add(-10 * time.Minute)
	var now time.Time
	for i := 0; i < 8; i++ {
		now = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		feedStampedSnapshot(t, map[string]any{
			"tanks.freshWater.3.currentLevel": (40 + 2.5*float64(i)) / 100,
			"navigation.speedOverGround":      0.0,
		}, map[string]time.Time{"navigation.speedOverGround": old}, 0, now)
		if on, _ := tankFill.status("freshWater.3", now); on {
			t.Fatalf("a ten-minute-old speed of 0 lets the tank show filling at step %d", i)
		}
	}
}

func TestSampleTankFillingTreatsADeadSenderAsNotSeen(t *testing.T) {
	withFreshTankTracker(t)
	var now time.Time
	for i := 0; i < 8; i++ {
		now = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		feedTankSnapshot(t, "tanks.freshWater.3.currentLevel", (40+2.5*float64(i))/100, now)
	}
	if on, _ := tankFill.status("freshWater.3", now); !on {
		t.Fatalf("not filling while the sender is live")
	}
	// The sender dies: the snapshot still holds a level, stamped ten minutes ago.
	now = now.Add(100 * time.Second)
	feedStampedSnapshot(t, map[string]any{
		"tanks.freshWater.3.currentLevel": 0.625,
		"navigation.speedOverGround":      0.0,
	}, map[string]time.Time{"tanks.freshWater.3.currentLevel": time.Now().Add(-10 * time.Minute)}, 0, now)
	if on, secs := tankFill.status("freshWater.3", now); on || secs != nil {
		t.Errorf("stale sender: filling=%v estimate=%v, want neither", on, secs)
	}
}

func withFreshTankTracker(t *testing.T) {
	t.Helper()
	savedTracker, savedSnapshot := tankFill, globalSignalKSnapshot
	tankFill = newTankFillTracker()
	t.Cleanup(func() { tankFill, globalSignalKSnapshot = savedTracker, savedSnapshot })
}

func TestSampleTankFillingFeedsThePayloadFromTheLiveSnapshot(t *testing.T) {
	withFreshTankTracker(t)
	var now time.Time
	for i := 0; i < 8; i++ {
		now = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		feedTankSnapshot(t, "tanks.freshWater.3.currentLevel", (40+2.5*float64(i))/100, now)
	}
	// The payload path: labelled tanks from the snapshot, then annotated.
	tanks, _, err := fetchSignalKTanksState(map[string]string{"freshwater.3": "Fresh water"})
	if err != nil {
		t.Fatal(err)
	}
	if len(tanks) != 1 {
		t.Fatalf("tanks = %+v, want the one labelled tank", tanks)
	}
	annotateTankFilling(tanks, now)
	if !tanks[0].Filling {
		t.Errorf("tank = %+v, want filling after the live sampler saw it rise", tanks[0])
	}
}

func TestSampleTankFillingIgnoresCategoriesThatAreNotWaterOrFuel(t *testing.T) {
	withFreshTankTracker(t)
	var now time.Time
	for i := 0; i < 8; i++ {
		now = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		feedTankSnapshot(t, "tanks.liveWell.1.currentLevel", (40+2.5*float64(i))/100, now)
		if on, _ := tankFill.status("liveWell.1", now); on {
			t.Fatalf("live well shows filling at step %d", i)
		}
	}
}

func TestTankFillingSeesAFillAfterADrawdownBelowThePreviousHigh(t *testing.T) {
	tr := newTankFillTracker()
	// 60% drawn down to 50% over ten minutes in 2.5% steps.
	for i := 0; i <= 4; i++ {
		tr.observe(fillTankID, "water", 60-2.5*float64(i), 0, fillAt(9, 0).Add(time.Duration(i)*150*time.Second))
	}
	hose := fillAt(9, 10)
	// Hose on: 1.5 %/min, a 2.5% step about every 100 s.
	var on bool
	var seenAt time.Duration
	for i := 1; i <= 6; i++ {
		at := hose.Add(time.Duration(i) * 100 * time.Second)
		tr.observe(fillTankID, "water", 50+2.5*float64(i), 0, at)
		if got, _ := tr.status(fillTankID, at); got && !on {
			on, seenAt = true, at.Sub(hose)
		}
	}
	if !on || seenAt > 4*time.Minute {
		t.Errorf("filling seen after %v (on=%v), want within 4 minutes of the hose going on", seenAt, on)
	}
}

func TestTankFillingTimeToFullCountsDownBetweenSteps(t *testing.T) {
	tr := newTankFillTracker()
	var last time.Time
	for i := 0; i < 8; i++ {
		last = fillAt(9, 0).Add(time.Duration(i) * 2 * time.Minute)
		tr.observe(fillTankID, "water", 40+2.5*float64(i), 0, last)
	}
	_, atRise := tr.status(fillTankID, last)
	if atRise == nil {
		t.Fatalf("no estimate at the last rise")
	}
	// A minute on, no new step, still two rises inside the six-minute window.
	on, later := tr.status(fillTankID, last.Add(time.Minute))
	if !on || later == nil {
		t.Fatalf("a minute after the last rise: filling=%v estimate=%v, want both", on, later)
	}
	if got := *atRise - *later; got < 55 || got > 65 {
		t.Errorf("estimate fell by %.0f s over a minute, want about 60", got)
	}
	// The fill has stopped: no word and no estimate.
	if on, secs := tr.status(fillTankID, last.Add(7*time.Minute)); on || secs != nil {
		t.Errorf("seven minutes after the last rise: filling=%v estimate=%v, want neither", on, secs)
	}
}

// drawnDownTo60 teaches the tracker a 2.5% step by drawing a tank down to 60%
// in 2.5% steps, five minutes apart, and returns the time of the last one.
func drawnDownTo60(tr *tankFillTracker) time.Time {
	var at time.Time
	for i := 0; i <= 10; i++ {
		at = fillAt(7, 0).Add(time.Duration(i) * 5 * time.Minute)
		tr.observe(fillTankID, "water", 85-2.5*float64(i), 0, at)
	}
	return at
}

func TestTankFillingNeverCountsRecoveryFromADipAsAFill(t *testing.T) {
	for _, dip := range []float64{55, 0} { // a real dip, and a sender reading 0 for one report
		tr := newTankFillTracker()
		at := drawnDownTo60(tr).Add(10 * time.Minute)
		for i := 1; i <= 4; i++ {
			level := dip
			if i%2 == 0 {
				level = 60
			}
			at = at.Add(time.Minute)
			tr.observe(fillTankID, "water", level, 0, at)
			if on, _ := tr.status(fillTankID, at); on {
				t.Fatalf("dip to %v and back shows filling at report %d", dip, i)
			}
		}
	}
}

func TestTankFillingSeesAFillWhoseSenderDithersAtEachStepBoundary(t *testing.T) {
	tr := newTankFillTracker()
	var at time.Time
	// Three boundaries two minutes apart. At each the sender flips between the
	// old and the new step every 12 s, and it flips right through to the next.
	for i := 0; i < 30; i++ {
		lower := 50 + 2.5*float64(i/10)
		level := lower + 2.5*float64(i%2)
		at = fillAt(9, 0).Add(time.Duration(i) * 12 * time.Second)
		tr.observe(fillTankID, "water", level, 0, at)
	}
	if on, _ := tr.status(fillTankID, at); !on {
		t.Errorf("a fill with three dithering boundaries two minutes apart shows no filling")
	}
}

func TestTankFillingHoldsOnAHoseWithStepsThreeAndAHalfMinutesApart(t *testing.T) {
	tr := newTankFillTracker()
	start := fillAt(9, 0)
	step := 210 // seconds
	lastRise := 7 * step
	for sec := 0; sec <= lastRise; sec += 10 {
		at := start.Add(time.Duration(sec) * time.Second)
		tr.observe(fillTankID, "water", 30+2.5*float64(sec/step), 0, at)
		on, _ := tr.status(fillTankID, at)
		if sec >= 2*step && !on {
			t.Fatalf("not filling at %d s, after the second rise", sec)
		}
	}
}

func TestTankFillingIgnoresNoiseOnAFineResolutionSender(t *testing.T) {
	noise := []float64{0, .6, .1, .7, 1.1, .5, 0, .6, .2, .8}
	tr := newTankFillTracker()
	for i := 0; i < 50; i++ { // a reading every 36 s for 30 minutes
		at := fillAt(9, 0).Add(time.Duration(i) * 36 * time.Second)
		tr.observe(fillTankID, "water", 60+noise[i%len(noise)], 0, at)
		if on, _ := tr.status(fillTankID, at); on {
			t.Fatalf("0.1%% noise shows filling at reading %d", i)
		}
	}
}

func TestTankFillingSeesAFillOnAFineResolutionSender(t *testing.T) {
	tr := newTankFillTracker()
	var at time.Time
	var on bool
	for i := 0; i <= 20; i++ { // +0.3% every 30 s for 10 minutes
		at = fillAt(9, 0).Add(time.Duration(i) * 30 * time.Second)
		tr.observe(fillTankID, "water", 30+0.3*float64(i), 0, at)
		on, _ = tr.status(fillTankID, at)
	}
	if !on {
		t.Errorf("a steady +0.3%% per 30 s fill for 10 minutes shows no filling")
	}
}

func TestTankFillingTimeToFullIsWholeMinutes(t *testing.T) {
	tr := newTankFillTracker()
	var seen int
	for i := 0; i < 12; i++ {
		at := fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		tr.observe(fillTankID, "water", 40+2.5*float64(i), 0, at)
		for s := 0; s < 100; s += 10 {
			_, secs := tr.status(fillTankID, at.Add(time.Duration(s)*time.Second))
			if secs == nil {
				continue
			}
			seen++
			if int(*secs)%60 != 0 || *secs != float64(int(*secs)) || *secs < 60 {
				t.Fatalf("seconds_to_full = %v, want a whole number of minutes of at least one", *secs)
			}
		}
	}
	if seen == 0 {
		t.Fatalf("never saw an estimate")
	}
}

func TestTankFillingExpiresWhenTheSamplerStopsFeedingATank(t *testing.T) {
	tr := newTankFillTracker()
	var last time.Time
	for i := 0; i < 6; i++ {
		last = fillAt(9, 0).Add(time.Duration(i) * 100 * time.Second)
		tr.observe(fillTankID, "water", 40+2.5*float64(i), 0, last)
	}
	if on, _ := tr.status(fillTankID, last); !on {
		t.Fatalf("not filling while fed")
	}
	if on, secs := tr.status(fillTankID, last.Add(time.Hour)); on || secs != nil {
		t.Errorf("an hour with no readings: filling=%v estimate=%v, want neither", on, secs)
	}
}
