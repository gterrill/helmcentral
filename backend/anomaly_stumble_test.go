package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var stumbleT0 = time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)

// feedRPM observes one engine at 1 Hz starting at start, one sample per
// element, and returns the time after the last sample. Values are rpm.
func feedRPM(tr *engineStumbleTracker, id string, start time.Time, rpms ...float64) time.Time {
	at := start
	for _, r := range rpms {
		tr.observe(id, at, r/60)
		at = at.Add(time.Second)
	}
	return at
}

func steady(n int, rpm float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = rpm
	}
	return out
}

func join(parts ...[]float64) []float64 {
	var out []float64
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func countFor(tr *engineStumbleTracker, id string, now time.Time) int {
	return tr.summary(now)[id].Count
}

func TestStumbleCountsADipThatRecovers(t *testing.T) {
	tr := newEngineStumbleTracker()
	end := feedRPM(tr, "port", stumbleT0, join(steady(12, 1800), []float64{1700, 1690}, steady(12, 1800))...)
	// A single engine needs no suppression window beyond settling.
	got := tr.summary(end.Add(20 * time.Second))["port"]
	if got.Count != 1 {
		t.Fatalf("count: got %d, want 1", got.Count)
	}
	if got.DeepestRPM < 109 || got.DeepestRPM > 111 || got.SteadyRPM != 1800 {
		t.Fatalf("evidence figures: deepest %v steady %v, want about 110 below 1800", got.DeepestRPM, got.SteadyRPM)
	}
}

func TestStumbleThrottleStepWithoutRecoveryIsNotCounted(t *testing.T) {
	tr := newEngineStumbleTracker()
	end := feedRPM(tr, "port", stumbleT0, join(steady(12, 1800), steady(20, 1500))...)
	if n := countFor(tr, "port", end.Add(20*time.Second)); n != 0 {
		t.Fatalf("a throttle step with no recovery counted %d stumbles", n)
	}
}

func TestStumbleDipBelowThresholdIsNotCounted(t *testing.T) {
	tr := newEngineStumbleTracker()
	// 30 rpm is under both the 40 rpm floor and 3% of 1800 (54 rpm).
	end := feedRPM(tr, "port", stumbleT0, join(steady(12, 1800), []float64{1770}, steady(12, 1800))...)
	if n := countFor(tr, "port", end.Add(20*time.Second)); n != 0 {
		t.Fatalf("shallow dip counted %d", n)
	}
}

func TestStumbleRecoveryMustBeWithinSixSeconds(t *testing.T) {
	tr := newEngineStumbleTracker()
	// Seven samples down, then back: too slow to be a stumble.
	end := feedRPM(tr, "port", stumbleT0, join(steady(12, 1800), steady(7, 1700), steady(12, 1800))...)
	if n := countFor(tr, "port", end.Add(20*time.Second)); n != 0 {
		t.Fatalf("slow recovery counted %d", n)
	}
}

func TestStumbleSimultaneousDipOnBothEnginesIsSuppressed(t *testing.T) {
	tr := newEngineStumbleTracker()
	series := join(steady(12, 1800), []float64{1700, 1690}, steady(12, 1800))
	feedRPM(tr, "port", stumbleT0, series...)
	end := feedRPM(tr, "starboard", stumbleT0.Add(2*time.Second), series...)
	now := end.Add(20 * time.Second)
	if p, s := countFor(tr, "port", now), countFor(tr, "starboard", now); p != 0 || s != 0 {
		t.Fatalf("sea-state dips counted: port %d starboard %d", p, s)
	}
}

func TestStumbleDipOnOneEngineOfTwoIsCounted(t *testing.T) {
	tr := newEngineStumbleTracker()
	dip := join(steady(12, 1800), []float64{1700, 1690}, steady(12, 1800))
	feedRPM(tr, "port", stumbleT0, dip...)
	end := feedRPM(tr, "starboard", stumbleT0, steady(len(dip), 1800)...)
	now := end.Add(20 * time.Second)
	if p, s := countFor(tr, "port", now), countFor(tr, "starboard", now); p != 1 || s != 0 {
		t.Fatalf("port %d starboard %d, want 1 and 0", p, s)
	}
}

func TestStumbleDipsMoreThanThreeSecondsApartAreNotSuppressed(t *testing.T) {
	tr := newEngineStumbleTracker()
	series := join(steady(12, 1800), []float64{1700, 1690}, steady(12, 1800))
	feedRPM(tr, "port", stumbleT0, series...)
	end := feedRPM(tr, "starboard", stumbleT0.Add(5*time.Second), series...)
	now := end.Add(20 * time.Second)
	if p, s := countFor(tr, "port", now), countFor(tr, "starboard", now); p != 1 || s != 1 {
		t.Fatalf("port %d starboard %d, want 1 and 1", p, s)
	}
}

func TestStumbleSingleEngineBoatIsNotSuppressed(t *testing.T) {
	tr := newEngineStumbleTracker()
	end := feedRPM(tr, "main", stumbleT0, join(steady(12, 2400), []float64{2250, 2240}, steady(12, 2400))...)
	if n := countFor(tr, "main", end.Add(20*time.Second)); n != 1 {
		t.Fatalf("single engine count %d, want 1", n)
	}
}

func TestStumbleGapOverThreeSecondsResetsTheWindow(t *testing.T) {
	tr := newEngineStumbleTracker()
	at := feedRPM(tr, "port", stumbleT0, steady(12, 1800)...)
	// Five seconds of silence, then only four steady samples: fewer than the
	// six needed, so the dip that follows has no steady baseline to fall from.
	at = feedRPM(tr, "port", at.Add(5*time.Second), join(steady(4, 1800), []float64{1700, 1690}, steady(12, 1800))...)
	if n := countFor(tr, "port", at.Add(20*time.Second)); n != 0 {
		t.Fatalf("dip after a gap counted %d", n)
	}
}

func TestStumbleBelowEightHundredRPMNeverCounts(t *testing.T) {
	tr := newEngineStumbleTracker()
	end := feedRPM(tr, "port", stumbleT0, join(steady(12, 700), []float64{600, 590}, steady(12, 700))...)
	if n := countFor(tr, "port", end.Add(20*time.Second)); n != 0 {
		t.Fatalf("idle dip counted %d", n)
	}
}

func TestStumbleUnsteadyWindowDoesNotCount(t *testing.T) {
	tr := newEngineStumbleTracker()
	// Ramping 20 rpm a second is a throttle move, not a steady run.
	ramp := make([]float64, 12)
	for i := range ramp {
		ramp[i] = 1500 + float64(i)*20
	}
	end := feedRPM(tr, "port", stumbleT0, join(ramp, []float64{1600, 1590}, steady(12, 1720))...)
	if n := countFor(tr, "port", end.Add(20*time.Second)); n != 0 {
		t.Fatalf("dip during a ramp counted %d", n)
	}
}

func TestStumbleCountIsTrailingTenMinutes(t *testing.T) {
	tr := newEngineStumbleTracker()
	dip := join(steady(12, 1800), []float64{1700, 1690}, steady(12, 1800))
	end := feedRPM(tr, "port", stumbleT0, dip...)
	if n := countFor(tr, "port", end.Add(20*time.Second)); n != 1 {
		t.Fatalf("fresh count %d, want 1", n)
	}
	if n := countFor(tr, "port", end.Add(11*time.Minute)); n != 0 {
		t.Fatalf("count after 11 minutes %d, want 0", n)
	}
}

func TestStumbleEvidenceWording(t *testing.T) {
	got := formatStumbleEvidence(engineStumbleSummary{Count: 6, DeepestRPM: 140, SteadyRPM: 1170})
	want := "6 RPM dips in 10 min · up to 140 rpm below 1170 rpm"
	if got != want {
		t.Fatalf("evidence: got %q, want %q", got, want)
	}
	if strings.ContainsAny(got, "!?") {
		t.Fatalf("evidence carries severity punctuation: %q", got)
	}
}

func TestStumbleRuleTitle(t *testing.T) {
	cases := []struct{ name, instance, want string }{
		{"Port", "port", "Port Engine Stumbling"},
		{"Port Engine", "port", "Port Engine Stumbling"},
		{"", "port", "Port Engine Stumbling"},
		{"", "0", "Engine 0 Stumbling"},
	}
	for _, c := range cases {
		if got := stumbleRuleTitle(c.name, c.instance); got != c.want {
			t.Errorf("title(%q, %q): got %q, want %q", c.name, c.instance, got, c.want)
		}
	}
}

// --- Seeding ---------------------------------------------------------------

func TestSeedEngineStumbleRuleIsEnabledWarnAndIdempotent(t *testing.T) {
	withTempAlarmRules(t)

	for i := 0; i < 2; i++ {
		if err := seedEngineStumbleRule("port", "Port"); err != nil {
			t.Fatalf("seed %d: %v", i, err)
		}
	}
	rules := listAlarmRules()
	if len(rules) != 1 {
		t.Fatalf("expected one rule after seeding twice, got %d: %+v", len(rules), rules)
	}
	r := rules[0]
	if !r.Enabled || r.State != alarmStateWarn || r.Op != alarmOpAbove {
		t.Fatalf("rule shape: %+v", r)
	}
	if r.Path != anomalyEngineStumbleCountPath("port") {
		t.Fatalf("path %q", r.Path)
	}
	if r.Label != "Port Engine Stumbling" {
		t.Fatalf("label %q", r.Label)
	}
	// Fires at 3, clears at 0.
	if !(r.Value >= 2 && r.Value < 3) {
		t.Fatalf("threshold %v must fire at a count of 3 and not 2", r.Value)
	}
	clear := alarmClearValueFor(r)
	if clear == nil || !(*clear >= 0 && *clear < 1) {
		t.Fatalf("clear value %v must be reached at 0 and not at 1", clear)
	}
	if err := seedEngineStumbleRule("starboard", "Starboard"); err != nil {
		t.Fatal(err)
	}
	if n := len(listAlarmRules()); n != 2 {
		t.Fatalf("a second engine must get its own rule, got %d rules", n)
	}
}

func TestSeedEngineStumbleMarkerFollowsInstanceNotName(t *testing.T) {
	if anomalyEngineStumbleSeedMarker("Port") != anomalyEngineStumbleSeedMarker("port") {
		t.Fatal("marker must be keyed on the lower-cased instance id")
	}
	if anomalyEngineStumbleSeedMarker("port") == anomalyEngineSeedMarker("port") {
		t.Fatal("marker must not collide with the twin residual marker")
	}
}

// --- Tick wiring: discovery without vessel setup ----------------------------

func TestComputeAnomalyReadingPublishesStumbleCountWithoutVesselSetup(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	settingsPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}

	series := join(steady(12, 1800), []float64{1700, 1690}, steady(12, 1800))
	var reading anomalyReading
	for i, rpm := range series {
		at := stumbleT0.Add(time.Duration(i) * time.Second)
		applyNumeric(snapshot, "vessels.self", "propulsion.port.revolutions", rpm/60, at)
		reading = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}
	reading = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, stumbleT0.Add(time.Duration(len(series)+8)*time.Second))
	path := anomalyEngineStumbleCountPath("port")
	got, ok := reading.Values[path]
	if !ok || got != 1 {
		t.Fatalf("stumble count: got %v present=%v, want 1", got, ok)
	}
	if !strings.Contains(reading.Evidence[path], "1 RPM dip") {
		t.Fatalf("evidence: %q", reading.Evidence[path])
	}
}

// --- Real captured fixtures ---------------------------------------------------

type stumbleFixture struct {
	Columns []string `json:"columns"`
	Data    [][]any  `json:"data"`
}

// replayStumbleFixture feeds a captured 1 s history through a tracker using
// the rows' own timestamps and returns, per engine, the highest trailing
// 10-minute count seen and the final count.
func replayStumbleFixture(t *testing.T, name string) (maxCount, finalCount map[string]int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "anomaly", name))
	if err != nil {
		t.Fatal(err)
	}
	var fx stumbleFixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		t.Fatal(err)
	}
	engines := map[int]string{}
	for i, c := range fx.Columns {
		if strings.HasSuffix(c, ".revolutions") {
			engines[i] = strings.Split(c, ".")[1]
		}
	}
	if len(engines) == 0 || len(fx.Data) == 0 {
		t.Fatalf("fixture %s has no revolutions data", name)
	}

	tr := newEngineStumbleTracker()
	maxCount = map[string]int{}
	finalCount = map[string]int{}
	for _, row := range fx.Data {
		at, err := time.Parse(time.RFC3339, row[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		for col, id := range engines {
			if v, ok := row[col].(float64); ok {
				tr.observe(id, at, v)
			}
		}
		for id, s := range tr.summary(at) {
			if s.Count > maxCount[id] {
				maxCount[id] = s.Count
			}
			finalCount[id] = s.Count
		}
	}
	return maxCount, finalCount
}

func TestStumbleFixtureFaultDay(t *testing.T) {
	maxC, final := replayStumbleFixture(t, "engine-stumble-fault_2026-10-04.json")
	t.Logf("fault 2026-10-04: max %v final %v", maxC, final)
	if maxC["port"] < 3 || final["port"] < 3 {
		t.Fatalf("port must reach and hold 3 or more: max %d final %d", maxC["port"], final["port"])
	}
	if maxC["starboard"] != 0 {
		t.Fatalf("starboard counted %d", maxC["starboard"])
	}
}

func TestStumbleFixtureOnset(t *testing.T) {
	maxC, _ := replayStumbleFixture(t, "engine-stumble-onset_2026-10-02.json")
	t.Logf("onset 2026-10-02: max %v", maxC)
	if maxC["port"] < 3 {
		t.Fatalf("port never reached 3: max %d", maxC["port"])
	}
	if maxC["starboard"] != 0 {
		t.Fatalf("starboard counted %d", maxC["starboard"])
	}
}

func TestStumbleFixtureHealthy(t *testing.T) {
	maxC, _ := replayStumbleFixture(t, "engine-stumble-healthy_2026-09-28.json")
	t.Logf("healthy 2026-09-28: max %v", maxC)
	if maxC["port"] != 0 || maxC["starboard"] != 0 {
		t.Fatalf("healthy engines counted stumbles: %v", maxC)
	}
}
