package main

import (
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// anomalyDetectorTestNow is a fixed instant every test below applies deltas
// against, chosen well clear of any zero-time edge case.
var anomalyDetectorTestNow = time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)

func applyNumeric(snapshot *signalKSnapshot, context, path string, value float64, at time.Time) {
	snapshot.applyDelta(signalKDelta{
		Context: context,
		Updates: []signalKUpdate{{
			SourceRef: "test",
			Timestamp: at.Format(time.RFC3339),
			Values:    []signalKValue{{Path: path, Value: value}},
		}},
	}, at)
}

func newAnomalyTestSnapshot() *signalKSnapshot {
	snapshot := newSignalKSnapshot()
	snapshot.setSelfContext("vessels.self")
	return snapshot
}

// --- No vessel setup: nothing is evaluated or published --------------------

func TestComputeAnomalyReadingFreshInstallPublishesNoBatteryOrEngineValues(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	settingsPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	if _, ok := reading.Values[anomalyBatteryFullBankChargingPath]; ok {
		t.Fatalf("expected no full-bank-charging value with no vessel.house_bank configured")
	}
	for path := range reading.Values {
		if path != anomalySensorFrozenCountPath && path != anomalySensorOutOfRangeCountPath && path != anomalySensorSilentSourceCountPath {
			t.Fatalf("expected no engine residual values with no engines configured, got %s", path)
		}
	}
	// Impossible/silent-source need no setup: they still report a count
	// (0, "checked and found nothing"), not absence.
	if reading.Values[anomalySensorOutOfRangeCountPath] != 0 {
		t.Fatalf("expected out-of-range count 0 on an empty snapshot, got %v", reading.Values[anomalySensorOutOfRangeCountPath])
	}
	if reading.Values[anomalySensorFrozenCountPath] != 0 {
		t.Fatalf("expected frozen count 0 with no engines configured, got %v", reading.Values[anomalySensorFrozenCountPath])
	}
}

// --- Full-bank charging wiring ---------------------------------------------

func setupBatteryTestStore(t *testing.T) (equipmentID string) {
	t.Helper()
	store, err := newDocumentStore(filepath.Join(t.TempDir(), "documents.sqlite"))
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	prev := globalDocumentStore
	globalDocumentStore = store
	t.Cleanup(func() { globalDocumentStore = prev })

	item, err := store.CreateEquipment(equipmentItem{Name: "House bank", Category: "mechanical", ProfileID: "test-battery-wiring"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}
	return item.ID
}

func setupBatteryProfileFixture(t *testing.T) {
	t.Helper()
	setupEngineProfiles(t, map[string]string{"battery.json": `{
		"schema_version": 1,
		"kind": "battery",
		"id": "test-battery-wiring",
		"name": "Test LiFePO4",
		"chemistry": "LiFePO4",
		"full_soc": {"value": 0.95, "source": "test"},
		"charge_warn": {"value": 3.5, "source": "test"},
		"charge_high": {"value": 3.6, "source": "test"}
	}`})
}

func TestComputeAnomalyReadingBatteryWiringPublishesLevel(t *testing.T) {
	equipmentID := setupBatteryTestStore(t)
	setupBatteryProfileFixture(t)

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(settingsPath, vesselSettings{
		HouseBank: &vesselHouseBankSetting{
			Path: "electrical.batteries.0", EquipmentID: equipmentID, CapacityAh: 400, Cells: 8,
		},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}
	// Pack thresholds: 3.5V x 8 = 28.0V warn. FullSOC 0.95.

	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.capacity.stateOfCharge", 0.96, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.voltage", 28.2, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.current", 10, anomalyDetectorTestNow) // > C/50 (8A)

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	level, ok := reading.Values[anomalyBatteryFullBankChargingPath]
	if !ok {
		t.Fatalf("expected a full-bank-charging value, got none: %+v", reading.Values)
	}
	if level != 1 {
		t.Fatalf("level: got %v, want 1", level)
	}
	if reading.Evidence[anomalyBatteryFullBankChargingPath] == "" {
		t.Fatalf("expected evidence text for the full-bank-charging reading")
	}
}

func TestComputeAnomalyReadingBatteryNotSetUpWithoutLinkedProfile(t *testing.T) {
	store, err := newDocumentStore(filepath.Join(t.TempDir(), "documents.sqlite"))
	if err != nil {
		t.Fatalf("newDocumentStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	prev := globalDocumentStore
	globalDocumentStore = store
	t.Cleanup(func() { globalDocumentStore = prev })

	// No ProfileID linked at all.
	item, err := store.CreateEquipment(equipmentItem{Name: "House bank", Category: "mechanical"})
	if err != nil {
		t.Fatalf("CreateEquipment: %v", err)
	}

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(settingsPath, vesselSettings{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", EquipmentID: item.ID, CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.capacity.stateOfCharge", 0.99, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.voltage", 29, anomalyDetectorTestNow)
	applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.current", 50, anomalyDetectorTestNow)

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, anomalyDetectorTestNow)
	if _, ok := reading.Values[anomalyBatteryFullBankChargingPath]; ok {
		t.Fatalf("expected no value with no linked battery profile, got %+v", reading.Values[anomalyBatteryFullBankChargingPath])
	}
}

// --- Engine differential wiring ---------------------------------------------

func setUpTwinEngineVessel(t *testing.T) string {
	t.Helper()
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(settingsPath, vesselSettings{
		Engines: []vesselEngineSetting{
			{Instance: "port", Name: "Port"},
			{Instance: "starboard", Name: "Starboard"},
		},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}
	return settingsPath
}

func applyWarmedUpTwinEngines(snapshot *signalKSnapshot, portOilPa, stbdOilPa float64, at time.Time) {
	for _, engine := range []string{"port", "starboard"} {
		applyNumeric(snapshot, "vessels.self", "propulsion."+engine+".revolutions", 1800.0/60, at)
		applyNumeric(snapshot, "vessels.self", "propulsion."+engine+".temperature", 350, at)
	}
	applyNumeric(snapshot, "vessels.self", "propulsion.port.oilPressure", portOilPa, at)
	applyNumeric(snapshot, "vessels.self", "propulsion.starboard.oilPressure", stbdOilPa, at)
}

// tickTwinGateSteady advances a fresh steadiness tracker through n ticks a
// second apart, so RunningFor/steady windows have genuinely elapsed by the
// last one -- computeAnomalyReading (and the twin gate under it) reads real
// elapsed wall time, not a mocked clock.
func tickTwinGateSteady(t *testing.T, settingsPath string, portOilPa, stbdOilPa float64, n int, start time.Time) anomalyReading {
	t.Helper()
	snapshot := newAnomalyTestSnapshot()
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}

	var last anomalyReading
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		applyWarmedUpTwinEngines(snapshot, portOilPa, stbdOilPa, at)
		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}
	return last
}

func TestComputeAnomalyReadingOffForFewerThanTwoEngines(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(settingsPath, vesselSettings{
		Engines: []vesselEngineSetting{{Instance: "port", Name: "Port"}},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	reading := tickTwinGateSteady(t, settingsPath, 388100, 385600, 5, anomalyDetectorTestNow)
	for path := range reading.Values {
		if path == anomalySensorFrozenCountPath || path == anomalySensorOutOfRangeCountPath || path == anomalySensorSilentSourceCountPath {
			continue
		}
		t.Fatalf("expected no engine residual values for a single configured engine, got %s", path)
	}
}

func TestComputeAnomalyReadingEngineDifferentialGateNotYetHeld(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)

	// Only 5 seconds of data: the gate's own runFor/steady windows (minutes)
	// have not elapsed yet, so no residual should publish.
	reading := tickTwinGateSteady(t, settingsPath, 388100, 385600, 5, anomalyDetectorTestNow)
	path := anomalyEngineResidualPath("port", "oilPressure")
	if _, ok := reading.Values[path]; ok {
		t.Fatalf("expected no residual before the gate's steadiness windows have elapsed, got %v", reading.Values[path])
	}
}

// TestComputeAnomalyReadingEngineDifferentialNeedsALearnedBaseline asserts
// a residual stays absent with no learned baseline at all -- a fresh
// install, or one still within its first 14 days -- even once the gate
// itself holds: residualQuantity requires a learned offset (ok=false with
// none), the same "the residual is absent if the bucket has under 30
// qualifying minutes" rule extended to "no bucket learned yet at all".
func TestComputeAnomalyReadingEngineDifferentialNeedsALearnedBaseline(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)

	ticks := int(twinGateMinRunFor.Seconds()) + 5
	reading := tickTwinGateSteady(t, settingsPath, 388100, 385600, ticks, anomalyDetectorTestNow)

	path := anomalyEngineResidualPath("port", "oilPressure")
	if _, ok := reading.Values[path]; ok {
		t.Fatalf("expected no residual with no baseline learned yet, got %v", reading.Values[path])
	}
}

func TestComputeAnomalyReadingEngineDifferentialPublishesOnceGateHolds(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)

	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 0, Minutes: 40}}},
		},
	})

	// twinGateMinRunFor (10 min) is the longest window the gate needs;
	// tick one full second at a time past it so RunningFor and the 60s/300s
	// steadiness windows have all genuinely elapsed.
	ticks := int(twinGateMinRunFor.Seconds()) + 5
	reading := tickTwinGateSteady(t, settingsPath, 388100, 385600, ticks, anomalyDetectorTestNow)

	path := anomalyEngineResidualPath("port", "oilPressure")
	residual, ok := reading.Values[path]
	if !ok {
		t.Fatalf("expected a port oilPressure residual once the gate holds, got none: %+v", reading.Values)
	}
	// Learned offset 0 -> residual is exactly the raw port-vs-starboard
	// difference: 388100 - 385600 = 2500 Pa.
	if residual != 2500 {
		t.Fatalf("residual: got %v, want 2500", residual)
	}
	if reading.Evidence[path] == "" {
		t.Fatalf("expected evidence text for the residual")
	}

	// The mirrored starboard residual should be the exact negative (no
	// learned offset for starboard's own bucket, so it falls back to
	// absent -- only assert on port above, and separately confirm
	// starboard genuinely publishes nothing without its own baseline).
	stbdPath := anomalyEngineResidualPath("starboard", "oilPressure")
	if _, ok := reading.Values[stbdPath]; ok {
		t.Fatalf("expected starboard's residual to stay absent with no baseline learned for it, got %v", reading.Values[stbdPath])
	}
}

// TestComputeAnomalyReadingEngineDifferentialAbsentWhenInputsAreStale covers
// a dead gateway: propulsion.*.revolutions/temperature/oilPressure are
// applied once and never updated again, but the detector keeps ticking on
// real wall time. numericFromPath alone has no notion of age -- it would
// keep returning that first reading forever, letting the gate's own
// steadiness windows accumulate off a value the boat stopped sending
// minutes ago and eventually publish a residual computed from stale data.
// Every input the comparison reads must be run through the same
// inputValidity age check computeAnomalyBattery already applies to the
// house bank's own paths.
func TestComputeAnomalyReadingEngineDifferentialAbsentWhenInputsAreStale(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)

	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 0, Minutes: 40}}},
		},
	})

	snapshot := newAnomalyTestSnapshot()
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}

	start := anomalyDetectorTestNow
	applyWarmedUpTwinEngines(snapshot, 388100, 385600, start) // applied once, then the gateway goes dark

	ticks := int(twinGateMinRunFor.Seconds()) + 5
	var last anomalyReading
	for i := 0; i < ticks; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}

	path := anomalyEngineResidualPath("port", "oilPressure")
	if _, ok := last.Values[path]; ok {
		t.Fatalf("expected no residual once the readings are older than inputValidityMaxAge (a dead gateway holding its last value forever), got %v", last.Values[path])
	}
}

// TestComputeAnomalyReadingEngineDifferentialSuppressedBySilentSourceNotJustInputValidity
// is code review finding 7, reopened by finding 1's fix: inputValidity
// alone judges a path's own ARRIVAL time (snapshot.lastSeen/pathSeen)
// fresh the instant any update for that exact path arrives -- including a
// SignalK reconnect replaying a $source's whole retained state, which
// resets every one of its paths' arrival times to "now" no matter how old
// the values themselves are. Before finding 1's fix, silentSources judged
// staleness from arrival time too, so a path could never be BOTH
// inputValidity-fresh and silentSourceNow-silent at once, and the
// silent-source half of valid()'s closure (computeAnomalyReading) never
// actually changed the result -- dead code, per finding 7's own question.
// Now that silentSources judges a source's own declared SignalK timestamp
// instead (signalk_snapshot.go's applyDelta), a path whose $source has gone
// properly silent -- old declared timestamps, replayed with a fresh arrival
// time every tick -- is exactly this case: inputValidity alone reports it
// fresh, and only the silentSourceNow check in valid() catches it. The
// check is kept, not removed.
func TestComputeAnomalyReadingEngineDifferentialSuppressedBySilentSourceNotJustInputValidity(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)
	snapshot := newAnomalyTestSnapshot()
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}

	// A learned baseline is what makes ANY residual publish at all
	// (residualQuantity requires learnedOk). Both quantities get one --
	// oilPressure's is what proves its own absence below is really the
	// silent-source gate at work, not merely "no baseline to publish
	// against" (which would hide the residual regardless of validity and
	// make that assertion pass for the wrong reason).
	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {
				"temperature": {{RPMBucket: 1800, Median: 0, Minutes: 40}},
				"oilPressure": {{RPMBucket: 1800, Median: 0, Minutes: 40}},
			},
		},
	})

	// The gateway that reports port's oilPressure died on 2026-09-21 -- its
	// last retained value is replayed, carrying that same original
	// timestamp, on every tick of this test's own simulated restart days
	// later (the exact shape a SignalK reconnect replay takes).
	const deadSource = "yachtdevices.dead"
	const deadTimestamp = "2026-09-21T10:34:00Z"
	const oilPressurePath = "propulsion.port.oilPressure"

	ticks := int(twinGateMinRunFor.Seconds()) + 5
	var last anomalyReading
	var finalAt time.Time
	for i := 0; i < ticks; i++ {
		at := anomalyDetectorTestNow.Add(time.Duration(i) * time.Second)
		finalAt = at
		for _, engine := range []string{"port", "starboard"} {
			applyNumeric(snapshot, "vessels.self", "propulsion."+engine+".revolutions", 1800.0/60, at)
			applyNumeric(snapshot, "vessels.self", "propulsion."+engine+".temperature", 350, at)
		}
		applyNumeric(snapshot, "vessels.self", "propulsion.starboard.oilPressure", 385600, at)
		// port's oilPressure comes only from the dead source: fresh arrival
		// (at) every tick, but the same long-dead declared timestamp every
		// single time -- exactly what a reconnect replay looks like.
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{
				SourceRef: deadSource,
				Timestamp: deadTimestamp,
				Values:    []signalKValue{{Path: oilPressurePath, Value: 388100.0}},
			}},
		}, at)

		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}

	if !inputValidity(snapshot, oilPressurePath, finalAt) {
		t.Fatalf("expected inputValidity ALONE to still read this path as fresh (arrival-based) -- that is exactly the gap valid() has to close")
	}

	oilPath := anomalyEngineResidualPath("port", "oilPressure")
	if _, ok := last.Values[oilPath]; ok {
		t.Fatalf("expected no oilPressure residual once its source is silent by declared timestamp, got %v", last.Values[oilPath])
	}

	// A quantity that does not depend on the dead source still publishes --
	// the gate is scoped to the one path it actually invalidates, not a
	// blanket "this tick is untrustworthy".
	tempPath := anomalyEngineResidualPath("port", "temperature")
	if _, ok := last.Values[tempPath]; !ok {
		t.Fatalf("expected the temperature residual (unaffected by the dead source) to still publish, got %+v", last.Values)
	}
}

// TestComputeAnomalyReadingEngineDifferentialRunningForRestartsAfterALongGap
// is the direct regression case for code review finding 8: the per-engine
// runningFor/coolant conditionTrackers were only ever observe()'d on a tick
// where that engine's own inputs were valid -- the "if !e.ok { continue }"
// skip above -- so a long stretch of invalid data (an instrument dropout
// outlasting a real engine stop/restart) never told the tracker the engine
// had actually stopped. trueSince stayed frozen at whatever it was before
// the gap, so the instant good data returned, now.Sub(trueSince) already
// covered the whole gap and satisfied twinGateMinRunFor immediately,
// however long -- or however thoroughly -- the engine had actually been off
// in between.
//
// Runs steady for most of twinGateMinRunFor (short of it), an invalid gap
// well past inputValidityMaxAge, then resumes: the residual must stay
// absent until a genuinely fresh twinGateMinRunFor has elapsed since the
// resume, not the moment rpmSteady/coolantSteady's own (already-correctly-
// resetting) windows catch back up.
func TestComputeAnomalyReadingEngineDifferentialRunningForRestartsAfterALongGap(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)

	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 0, Minutes: 40}}},
		},
	})

	snapshot := newAnomalyTestSnapshot()
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}
	path := anomalyEngineResidualPath("port", "oilPressure")

	// Steady for most of the 10-minute requirement, but short of it.
	preGap := int(twinGateMinRunFor.Seconds()) - 30
	t0 := anomalyDetectorTestNow
	for i := 0; i < preGap; i++ {
		at := t0.Add(time.Duration(i) * time.Second)
		applyWarmedUpTwinEngines(snapshot, 388100, 385600, at)
		computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}

	// A long dropout -- comfortably past inputValidityMaxAge -- with nothing
	// updating at all, but the detector keeps ticking on real wall time.
	gapStart := t0.Add(time.Duration(preGap) * time.Second)
	gapTicks := int(inputValidityMaxAge.Seconds())*2 + 30
	for i := 0; i < gapTicks; i++ {
		computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, gapStart.Add(time.Duration(i)*time.Second))
	}

	// Good data resumes. If the pre-gap runtime survived, 400 more seconds
	// (comfortably past rpmSteady's 60s and coolantSteady's 300s own fresh
	// windows) would already total pre-gap(570s)+400s >> 600s and the gate
	// would hold; it must not, since the genuine post-resume runtime is only
	// 400s.
	resumeStart := gapStart.Add(time.Duration(gapTicks) * time.Second)
	var mid anomalyReading
	for i := 0; i < 400; i++ {
		at := resumeStart.Add(time.Duration(i) * time.Second)
		applyWarmedUpTwinEngines(snapshot, 388100, 385600, at)
		mid = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}
	if _, ok := mid.Values[path]; ok {
		t.Fatalf("expected no residual 400s after the gap (runningFor must have restarted from zero, not carried the pre-gap 570s over), got %v", mid.Values[path])
	}

	// Continuing on to a genuinely fresh twinGateMinRunFor since the resume
	// proves the reset did not simply break running-time tracking outright.
	var last anomalyReading
	for i := 400; i < int(twinGateMinRunFor.Seconds())+10; i++ {
		at := resumeStart.Add(time.Duration(i) * time.Second)
		applyWarmedUpTwinEngines(snapshot, 388100, 385600, at)
		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}
	if _, ok := last.Values[path]; !ok {
		t.Fatalf("expected a residual once a genuinely fresh twinGateMinRunFor elapsed after the resume, got none: %+v", last.Values)
	}
}

// TestFormatEngineResidualEvidenceUsesDeltaUnitsNotAbsoluteOnes is the direct
// regression case for code review finding 2: formatEngineResidualEvidence
// formatted the learned offset and the residual -- both DIFFERENCES between
// two readings -- with the quantity's own ABSOLUTE unit ("K", "Pa"), which
// for temperature subtracts 273.15 from a number that was never a Kelvin
// reading in the first place. A genuine 1.0 K learned offset and a 3.0 K
// residual then read as "-272.2 degC" and "-270.2 degC". Only ownValue and
// peerMedian are absolute readings; offset and residual must render through
// the quantity's own delta unit (deltaK/deltaPa), which converts by scale
// only, matching alarm_units.go's deltaK/deltaPa entries added alongside
// this cycle's engine-differential detector.
func TestFormatEngineResidualEvidenceUsesDeltaUnitsNotAbsoluteOnes(t *testing.T) {
	// ownValue 357.15 K (84.0 degC), peerMedian 353.15 K (80.0 degC), a 1.0 K
	// learned offset, residual (4.0 - 1.0) = 3.0 K.
	got := formatEngineResidualEvidence("Port", "K", "deltaK", 357.15, 353.15, 1.0, 3.0, 38, 1800.0/60)

	if strings.Contains(got, "-272") || strings.Contains(got, "-270") {
		t.Fatalf("evidence formatted a difference through the absolute K->degC conversion: %q", got)
	}
	want := "At 1800 rpm Port 84.0 °C, peers 80.0 °C; usually 1.0 °C off (38m learned); now 3.0 °C beyond that."
	if got != want {
		t.Fatalf("evidence:\n got  %q\n want %q", got, want)
	}
}

func TestComputeAnomalyReadingEngineDifferentialUsesLearnedBaseline(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)

	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 2000, Minutes: 40}}},
		},
	})

	ticks := int(twinGateMinRunFor.Seconds()) + 5
	reading := tickTwinGateSteady(t, settingsPath, 388100, 385600, ticks, anomalyDetectorTestNow)

	path := anomalyEngineResidualPath("port", "oilPressure")
	// Raw difference 2500 Pa, less the learned 2000 Pa offset -> 500 Pa.
	if reading.Values[path] != 500 {
		t.Fatalf("residual with a learned baseline: got %v, want 500", reading.Values[path])
	}
}

// TestComputeAnomalyReadingEngineResidualPathStaysStableAcrossRename covers
// code review finding 6: the residual path used to be built from the
// operator's own editable vessel.engines[].Name, not the stable Signal K
// propulsion.<instance> id. An operator renaming "Port" to "Port Main" -- an
// entirely ordinary edit, nothing to do with the physical engine changing --
// would silently move every future residual (and, via
// anomalyEngineSeedMarker, every seeded alarm rule) onto a brand new path,
// orphaning whatever was bound to the old one. The path must stay put
// across a rename; only the evidence sentence's wording should follow the
// new display name.
func TestComputeAnomalyReadingEngineResidualPathStaysStableAcrossRename(t *testing.T) {
	before := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(before, vesselSettings{
		Engines: []vesselEngineSetting{
			{Instance: "port", Name: "Port"},
			{Instance: "starboard", Name: "Starboard"},
		},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}
	after := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(after, vesselSettings{
		Engines: []vesselEngineSetting{
			{Instance: "port", Name: "Port Main"}, // renamed, same instance
			{Instance: "starboard", Name: "Starboard"},
		},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 0, Minutes: 40}}},
		},
	})

	ticks := int(twinGateMinRunFor.Seconds()) + 5
	beforeReading := tickTwinGateSteady(t, before, 388100, 385600, ticks, anomalyDetectorTestNow)
	afterReading := tickTwinGateSteady(t, after, 388100, 385600, ticks, anomalyDetectorTestNow)

	path := anomalyEngineResidualPath("port", "oilPressure")
	if _, ok := beforeReading.Values[path]; !ok {
		t.Fatalf("expected the instance-keyed path to carry the residual before the rename, got %+v", beforeReading.Values)
	}
	if _, ok := afterReading.Values[path]; !ok {
		t.Fatalf("expected the SAME instance-keyed path to still carry the residual after a display-name rename, got %+v", afterReading.Values)
	}
	if got := afterReading.Evidence[path]; !strings.Contains(got, "Port Main") {
		t.Fatalf("expected the evidence text to follow the new display name, got %q", got)
	}
}

// --- Frozen verdict: recomputed against the CURRENT engines/ignore list ---
//
// trackers.frozenPaths persists a path's verdict between the tumbling
// 15-minute windows that actually judge it (frozenVerdict only runs when a
// window completes), so frozenCount can read the latest verdict on every
// tick in between. But the second loop that turns frozenPaths into
// frozenCount must still check each path against the vessel settings and
// ignore list AS OF THIS TICK, not just echo back whatever a window last
// decided -- otherwise unticking an engine, or ignoring a sensor from the
// alarm card, would leave a stale "frozen" flag reporting for up to 15
// minutes (or forever, for an engine that was removed and will never get
// another window at all).

func TestComputeAnomalyReadingFrozenClearsOnceEngineIsUnticked(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)
	trackers := newAnomalyTrackers()
	// Simulate a previously-completed 15-minute window that flagged port's
	// oilPressure as frozen -- the exact state a real tracker would be in
	// partway between windows, without waiting 15 real minutes to get there.
	trackers.frozenPaths["propulsion.port.oilPressure"] = true

	// The operator unticks port: the vessel settings no longer list it.
	if err := saveVesselSettings(settingsPath, vesselSettings{
		Engines: []vesselEngineSetting{{Instance: "starboard", Name: "Starboard"}},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	snapshot := newAnomalyTestSnapshot()
	reading := computeAnomalyReading(snapshot, settingsPath, trackers, &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	if reading.Values[anomalySensorFrozenCountPath] != 0 {
		t.Fatalf("expected frozen count 0 once the engine is unticked, got %v (evidence: %q)",
			reading.Values[anomalySensorFrozenCountPath], reading.Evidence[anomalySensorFrozenCountPath])
	}
}

func TestComputeAnomalyReadingFrozenClearsOnceSensorIsIgnored(t *testing.T) {
	withTempAlarmRules(t)
	settingsPath := setUpTwinEngineVessel(t)
	trackers := newAnomalyTrackers()
	trackers.frozenPaths["propulsion.port.oilPressure"] = true

	if err := ignoreSensor("propulsion.port.oilPressure"); err != nil {
		t.Fatalf("ignoreSensor: %v", err)
	}

	snapshot := newAnomalyTestSnapshot()
	reading := computeAnomalyReading(snapshot, settingsPath, trackers, &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	if reading.Values[anomalySensorFrozenCountPath] != 0 {
		t.Fatalf("expected frozen count 0 the same tick the sensor is ignored, got %v (evidence: %q)",
			reading.Values[anomalySensorFrozenCountPath], reading.Evidence[anomalySensorFrozenCountPath])
	}
}

// --- Frozen/silent verdicts gate the battery and engine-differential ------
// detectors too (code review finding 3): inputValidity alone (present, not
// stale, in range) is not the whole of "sensor health" -- a frozen sensor
// keeps updating its timestamp with the same value, passing every check
// inputValidity makes, and reading.Validity was filled by the impossible-
// reading loop but never read by anything. The commit message and
// inputValidity's own doc comment both promised the sensor-health verdict
// gates the other two detectors; it did not.

// TestComputeAnomalyReadingEngineDifferentialAbsentWhenAnInputIsFrozen
// mirrors TestComputeAnomalyReadingFrozenClearsOnceEngineIsUnticked's own
// "simulate a previously-completed window" pattern: port's coolant
// (propulsion.port.temperature) is flagged frozen, the same tracker state a
// real 15-minute window would leave behind, without waiting 15 real minutes
// to get there. A frozen coolant reading must not feed a residual -- for any
// quantity, since coolant validity gates whether the engine qualifies for
// the twin gate at all.
func TestComputeAnomalyReadingEngineDifferentialAbsentWhenAnInputIsFrozen(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)
	trackers := newAnomalyTrackers()
	trackers.frozenPaths["propulsion.port.temperature"] = true

	prevBaseline, prevLoaded := globalEngineBaselineCache.get()
	t.Cleanup(func() {
		if prevLoaded {
			globalEngineBaselineCache.set(prevBaseline)
		}
	})
	globalEngineBaselineCache.set(engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 0, Minutes: 40}}},
		},
	})

	snapshot := newAnomalyTestSnapshot()
	steadiness := &twinSteadinessTrackers{}
	ticks := int(twinGateMinRunFor.Seconds()) + 5
	var last anomalyReading
	for i := 0; i < ticks; i++ {
		at := anomalyDetectorTestNow.Add(time.Duration(i) * time.Second)
		applyWarmedUpTwinEngines(snapshot, 388100, 385600, at)
		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}

	path := anomalyEngineResidualPath("port", "oilPressure")
	if _, ok := last.Values[path]; ok {
		t.Fatalf("expected no residual while port's coolant reading is flagged frozen, got %v", last.Values[path])
	}
}

// A "frozen SoC/voltage" counterpart to the engine-differential test above
// is not reachable through computeAnomalyReading as things stand: the
// frozen check's own tracker (engineCorrelatedPaths) only ever watches
// engine-correlated paths in this cycle, and computeAnomalyReading's
// per-tick cleanup loop drops any trackers.frozenPaths entry that is not
// one of those current engine paths -- so a battery path injected directly
// into that map is discarded before the gate below ever sees it. The gate
// itself (valid, below) is written generically on path, not restricted to
// engine paths, so it is already correct the day the frozen check is
// widened to cover the house bank too; that widening is a separate, larger
// change this cycle's frozen tracker was never scoped to make. The battery
// detector's own gating is exercised instead by
// TestComputeAnomalyReadingBatteryWiringPublishesLevel and its neighbours
// via inputValidity, which the same valid closure still calls first.
//
// The silent-source half of the same gate is real and wired the same way,
// but is not independently testable either: silentSources never reports a
// source before it has been quiet for at least silentSourceQuietFor (120s),
// comfortably longer than inputValidityMaxAge (30s) -- by the time a
// source is ever silent, every path it feeds has already failed the
// pre-existing staleness check inputValidity makes on its own, the same
// path TestComputeAnomalyReadingEngineDifferentialAbsentWhenInputsAreStale
// already covers. It stays wired in for defence in depth and because the
// commit message promised it, not because a scenario exists where it alone
// makes the difference.

// --- Sensor health evidence: which identifiers actually tripped -----------
//
// A count alarm ("2 out-of-range readings") gives the operator nothing to
// act on by itself; evidence names the offending paths/sources so the
// alarm card's "Ignore this sensor" action has something to submit.

func TestComputeAnomalyReadingOutOfRangeEvidenceNamesThePaths(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	applyNumeric(snapshot, "vessels.self", "propulsion.port.temperature", 0, anomalyDetectorTestNow) // impossible: 0K
	settingsPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	if reading.Values[anomalySensorOutOfRangeCountPath] != 1 {
		t.Fatalf("expected exactly one out-of-range reading, got %v", reading.Values[anomalySensorOutOfRangeCountPath])
	}
	evidence := reading.Evidence[anomalySensorOutOfRangeCountPath]
	if !strings.Contains(evidence, "propulsion.port.temperature") {
		t.Fatalf("expected the out-of-range evidence to name the path, got %q", evidence)
	}
}

func TestComputeAnomalyReadingOutOfRangeEvidenceEmptyWhenNothingFlagged(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	settingsPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	if _, ok := reading.Evidence[anomalySensorOutOfRangeCountPath]; ok {
		t.Fatalf("expected no evidence entry when nothing is out of range, got %q", reading.Evidence[anomalySensorOutOfRangeCountPath])
	}
}

func TestComputeAnomalyReadingSilentSourceEvidenceNamesTheSource(t *testing.T) {
	snapshot := newAnomalyTestSnapshot()
	settingsPath := filepath.Join(t.TempDir(), "does-not-exist.yaml")

	// A source watched (>=5min, >=30 updates) then quiet for >120s.
	start := anomalyDetectorTestNow.Add(-10 * time.Minute)
	for i := 0; i < 40; i++ {
		at := start.Add(time.Duration(i) * 10 * time.Second) // spans ~6.7min, stops 3.3min before now
		snapshot.applyDelta(signalKDelta{
			Context: "vessels.self",
			Updates: []signalKUpdate{{SourceRef: "venus.battery.512", Timestamp: at.Format(time.RFC3339), Values: []signalKValue{{Path: "electrical.batteries.512.voltage", Value: 27.2}}}},
		}, at)
	}
	// Keep the stream itself fresh so the suppression rule doesn't hide it.
	applyNumeric(snapshot, "vessels.self", "navigation.speedOverGround", 5, anomalyDetectorTestNow)

	reading := computeAnomalyReading(snapshot, settingsPath, newAnomalyTrackers(), &twinSteadinessTrackers{}, anomalyDetectorTestNow)

	if reading.Values[anomalySensorSilentSourceCountPath] != 1 {
		t.Fatalf("expected exactly one silent source, got %v", reading.Values[anomalySensorSilentSourceCountPath])
	}
	evidence := reading.Evidence[anomalySensorSilentSourceCountPath]
	if !strings.Contains(evidence, "venus.battery.512") {
		t.Fatalf("expected the silent-source evidence to name the source, got %q", evidence)
	}
}

// --- Voltage history: dV/dt for full-bank charging level 2 -----------------

// tickVoltageHistory feeds tr one sample a second, starting at start, using
// voltageAt to compute each sample's value from its index (0-based) --
// matching the detector's own 1s tick cadence, since dVdtMinSamples and
// dVdtMinSpan are both sized against that cadence. Returns the final
// observe() result.
func tickVoltageHistory(tr *voltageHistoryTracker, start time.Time, n int, voltageAt func(i int) float64) float64 {
	var got float64
	for i := 0; i < n; i++ {
		got = tr.observe(voltageAt(i), start.Add(time.Duration(i)*time.Second), true)
	}
	return got
}

func TestVoltageHistoryTrackerZeroWithFewerThanTwoSamples(t *testing.T) {
	var tr voltageHistoryTracker
	if got := tr.observe(28.0, anomalyDetectorTestNow, true); got != 0 {
		t.Fatalf("first sample: got %v, want 0 (nothing to compare against yet)", got)
	}
}

// TestVoltageHistoryTrackerTwoSamples60sApartNoLongerProducesARate is the
// direct regression case for code review finding 10: a two-point slope
// over just 60s -- the tracker's original design -- used to report a rate
// from as few as two samples. A single 0.01V quantization step between
// them (typical battery-monitor reading resolution) was then enough to
// read as "rising" at the plan's original 5 mV/min threshold. The tracker
// now requires dVdtMinSamples spread over at least dVdtMinSpan before it
// will report anything at all, so two samples -- however far apart -- stay
// at 0.
func TestVoltageHistoryTrackerTwoSamples60sApartNoLongerProducesARate(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow
	tr.observe(28.0, start, true)
	got := tr.observe(28.01, start.Add(60*time.Second), true)
	if got != 0 {
		t.Fatalf("two samples 60s apart: got %v, want 0 (not enough samples to trust a fit)", got)
	}
}

// TestVoltageHistoryTrackerGapDoesNotSurviveAsAStaleAnchor is the direct
// regression case for code review finding 4: observe used to keep a single
// anchor sample at or before the cutoff no matter how old it was, so the
// window's effective span stayed close to dVdtWindow rather than shrinking
// every tick. After a real gap (the detector paused, or the house bank was
// briefly unconfigured), that anchor could be far older than one tick
// before the cutoff -- combined with a burst of fresh samples right after
// the gap, dVdtMinSamples/dVdtMinSpan were satisfied from what was really
// just two points spanning the whole gap, not a genuine window's worth of
// history. A stale anchor must be dropped along with everything before it,
// the same as if it had never qualified as the anchor at all.
func TestVoltageHistoryTrackerGapDoesNotSurviveAsAStaleAnchor(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow

	// A full window of steady, flat voltage before the gap.
	flatSeconds := int(dVdtWindow.Seconds())
	for i := 0; i < flatSeconds; i++ {
		tr.observe(28.0, start.Add(time.Duration(i)*time.Second), true)
	}

	// A 30-minute gap, then a single very different reading -- if the last
	// pre-gap sample survives as the window's anchor, it alone (plus this
	// one) would satisfy neither dVdtMinSamples nor dVdtMinSpan yet, so keep
	// ticking fresh samples past the gap until dVdtMinSamples is reached.
	// With the bug, that stale anchor still counts toward the total, so the
	// span from it to "now" clears dVdtMinSpan long before dVdtMinSamples
	// genuine post-gap samples do, and the resulting fit is dominated by the
	// stale anchor rather than the actual recent behaviour.
	gapEnd := start.Add(time.Duration(flatSeconds)*time.Second + 30*time.Minute)
	var last float64
	for i := 0; i < dVdtMinSamples-1; i++ {
		last = tr.observe(30.0, gapEnd.Add(time.Duration(i)*time.Second), true)
	}
	if last != 0 {
		t.Fatalf("expected 0 with only %d genuine post-gap samples spanning %ds (well under dVdtMinSpan), got %v -- the stale pre-gap anchor is still being counted", dVdtMinSamples-1, dVdtMinSamples-2, last)
	}
}

// TestVoltageHistoryTrackerComputesVoltsPerMinute feeds a full window's
// worth of 1s ticks (dVdtWindow, currently 5 minutes) rising in a perfectly
// straight line at 0.06 V/min and checks the least-squares fit recovers
// that rate closely -- proving the regression, not just the window size,
// carries the number.
func TestVoltageHistoryTrackerComputesVoltsPerMinute(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow
	const ratePerMinute = 0.06
	n := int(dVdtWindow.Seconds()) + 1
	got := tickVoltageHistory(&tr, start, n, func(i int) float64 {
		return 28.0 + ratePerMinute*(float64(i)/60.0)
	})
	if math.Abs(got-ratePerMinute) > 0.002 {
		t.Fatalf("dV/dt: got %v, want close to %v", got, ratePerMinute)
	}
}

// TestVoltageHistoryTrackerWindowSlidesAndDropsOldSamples covers the same
// "old samples age out" behaviour the two-point version tested, but now
// meaningfully: 5 minutes flat, then 5 minutes rising. Once the window has
// fully slid past the flat period, the fit must reflect only the rising
// part, not be dragged down by history the window no longer covers.
func TestVoltageHistoryTrackerWindowSlidesAndDropsOldSamples(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow
	flatSeconds := int(dVdtWindow.Seconds())
	const risingRatePerMinute = 0.09

	for i := 0; i < flatSeconds; i++ {
		tr.observe(28.0, start.Add(time.Duration(i)*time.Second), true)
	}
	// Another full window's worth of ticks, now genuinely rising -- by the
	// last one, the flat period has aged all the way out of the window.
	got := tickVoltageHistory(&tr, start.Add(time.Duration(flatSeconds)*time.Second), flatSeconds+1, func(i int) float64 {
		return 28.0 + risingRatePerMinute*(float64(i)/60.0)
	})
	if math.Abs(got-risingRatePerMinute) > 0.002 {
		t.Fatalf("dV/dt after the window slid past the flat period: got %v, want close to %v", got, risingRatePerMinute)
	}
}

// TestVoltageHistoryTrackerSkipsRecordingAnInvalidSample is the direct
// regression case for code review finding 5: observe used to append every
// sample regardless of whether the caller's own validity check trusted it,
// so a single stale or glitched reading (a 0 V dropout) sat in the trailing
// 5-minute window for as long as the window itself, skewing the fit the
// whole time. A tick observed with valid=false must not be recorded at all
// -- the fit afterwards should read as if that tick never happened.
func TestVoltageHistoryTrackerSkipsRecordingAnInvalidSample(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow
	const ratePerMinute = 0.06

	n := int(dVdtWindow.Seconds())
	for i := 0; i < n; i++ {
		tr.observe(28.0+ratePerMinute*(float64(i)/60.0), start.Add(time.Duration(i)*time.Second), true)
	}

	// A single 0 V dropout, explicitly marked invalid -- must not join the
	// window at all.
	glitchAt := start.Add(time.Duration(n) * time.Second)
	tr.observe(0, glitchAt, false)

	// A few more genuine, on-trend samples.
	var last float64
	for i := 1; i <= 5; i++ {
		last = tr.observe(28.0+ratePerMinute*(float64(n+i)/60.0), glitchAt.Add(time.Duration(i)*time.Second), true)
	}

	if math.Abs(last-ratePerMinute) > 0.002 {
		t.Fatalf("dV/dt after an invalid 0V dropout: got %v, want close to %v (the dropout must not have been recorded)", last, ratePerMinute)
	}
}

// TestVoltageHistoryTrackerResetsWhenBankPathChanges is the direct
// regression case for the second half of code review finding 5: the tracker
// used to hold onto its samples forever regardless of which house bank they
// came from, so choosing a different bank in Settings -> Vessel -> Power (or
// unsetting and reconfiguring one) blended a trailing run of the OLD pack's
// voltage into the new one's dV/dt fit.
func TestVoltageHistoryTrackerResetsWhenBankPathChanges(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow

	n := int(dVdtWindow.Seconds())
	for i := 0; i < n; i++ {
		tr.resetIfBankChanged("electrical.batteries.0")
		tr.observe(28.0+0.2*(float64(i)/60.0), start.Add(time.Duration(i)*time.Second), true)
	}

	// The operator repoints the detector at a different bank -- a single
	// fresh sample on the new path must not inherit the old one's history.
	switchAt := start.Add(time.Duration(n) * time.Second)
	tr.resetIfBankChanged("electrical.batteries.1")
	got := tr.observe(12.0, switchAt, true)
	if got != 0 {
		t.Fatalf("expected 0 on the first sample of a newly-chosen bank, got %v -- the old bank's samples survived the switch", got)
	}
}

// TestVoltageHistoryTrackerNoisyFlatVoltageStaysUnderThreshold is the
// "noisy flat series" case code review finding 10 asked for directly: a
// pack that is not actually charging any faster than normal, but whose
// raw readings jitter across a realistic ~0.01V reading resolution (here
// alternating two adjacent values with a little extra scatter) must not
// average out to anything close to the 20 mV/min level-2 threshold, the
// way even one such step could swing the old two-point slope.
func TestVoltageHistoryTrackerNoisyFlatVoltageStaysUnderThreshold(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow
	n := int(dVdtWindow.Seconds()) + 1
	got := tickVoltageHistory(&tr, start, n, func(i int) float64 {
		// Deterministic pseudo-noise: steps through 28.00/28.01/28.00/28.02/...
		// -- never trending, always within one reading step of 28.00.
		switch i % 4 {
		case 0:
			return 28.00
		case 1:
			return 28.01
		case 2:
			return 28.00
		default:
			return 28.02
		}
	})
	if math.Abs(got) >= 0.02 {
		t.Fatalf("noisy flat voltage: got %v V/min, want well under the 0.02 V/min level-2 threshold", got)
	}
}

// TestVoltageHistoryTrackerFlatVoltageIsZeroRate covers the simple case
// underneath the noisy one above: genuinely unchanging readings fit to
// exactly 0, once there is enough history to fit anything at all.
func TestVoltageHistoryTrackerFlatVoltageIsZeroRate(t *testing.T) {
	var tr voltageHistoryTracker
	start := anomalyDetectorTestNow
	n := int(dVdtWindow.Seconds()) + 1
	got := tickVoltageHistory(&tr, start, n, func(int) float64 { return 28.0 })
	if got != 0 {
		t.Fatalf("flat voltage: got %v, want 0", got)
	}
}

// TestComputeAnomalyReadingBatteryLevel2ViaRisingVoltage exercises dV/dt
// wired all the way through computeAnomalyReading: level 1's conditions
// hold, HighVoltage is never crossed, but the pack keeps climbing faster
// than +5 mV/min across ticks -> level 2 via the rising-voltage half of the
// OR, not the overvoltage half.
// TestComputeAnomalyReadingBatteryLevel2ViaRisingVoltage exercises dV/dt
// wired all the way through computeAnomalyReading, ticking once a second
// for a full dVdtWindow (as the live detector goroutine actually would) at
// a genuine +30 mV/min climb -- comfortably above the 20 mV/min threshold,
// and never reaching HighVoltage (28.8V), so only the rising-voltage half
// of level 2's OR condition can fire.
func TestComputeAnomalyReadingBatteryLevel2ViaRisingVoltage(t *testing.T) {
	equipmentID := setupBatteryTestStore(t)
	setupBatteryProfileFixture(t) // full_soc 0.95, charge_warn 3.5, charge_high 3.6 (pack: warn 28.0V, high 28.8V @ 8 cells)

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(settingsPath, vesselSettings{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", EquipmentID: equipmentID, CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	snapshot := newAnomalyTestSnapshot()
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}

	const ratePerMinute = 0.03
	n := int(dVdtWindow.Seconds()) + 1
	var last anomalyReading
	for i := 0; i < n; i++ {
		at := anomalyDetectorTestNow.Add(time.Duration(i) * time.Second)
		applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.capacity.stateOfCharge", 0.96, at)
		applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.current", 10, at)
		applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.voltage", 28.2+ratePerMinute*(float64(i)/60.0), at)
		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}

	if last.Values[anomalyBatteryFullBankChargingPath] != 2 {
		t.Fatalf("expected level 2 via rising voltage after a full window's climb at +30 mV/min, got %v", last.Values[anomalyBatteryFullBankChargingPath])
	}
}

// TestComputeAnomalyReadingBatteryStaysLevel1WithNoisyFlatVoltage is the
// integration half of code review finding 10's "noisy flat series" ask: a
// pack sitting at level 1 with realistic ~0.01V reading jitter, but no
// actual upward trend, must never cross into level 2 via dV/dt.
func TestComputeAnomalyReadingBatteryStaysLevel1WithNoisyFlatVoltage(t *testing.T) {
	equipmentID := setupBatteryTestStore(t)
	setupBatteryProfileFixture(t)

	settingsPath := filepath.Join(t.TempDir(), "settings.yaml")
	if err := saveVesselSettings(settingsPath, vesselSettings{
		HouseBank: &vesselHouseBankSetting{Path: "electrical.batteries.0", EquipmentID: equipmentID, CapacityAh: 400, Cells: 8},
	}); err != nil {
		t.Fatalf("saveVesselSettings: %v", err)
	}

	snapshot := newAnomalyTestSnapshot()
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}

	jitter := []float64{28.20, 28.21, 28.20, 28.22}
	n := int(dVdtWindow.Seconds()) + 1
	var last anomalyReading
	for i := 0; i < n; i++ {
		at := anomalyDetectorTestNow.Add(time.Duration(i) * time.Second)
		applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.capacity.stateOfCharge", 0.96, at)
		applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.current", 10, at)
		applyNumeric(snapshot, "vessels.self", "electrical.batteries.0.voltage", jitter[i%len(jitter)], at)
		last = computeAnomalyReading(snapshot, settingsPath, trackers, steadiness, at)
	}

	if last.Values[anomalyBatteryFullBankChargingPath] != 1 {
		t.Fatalf("expected level 1 only with noisy-but-flat voltage, got %v", last.Values[anomalyBatteryFullBankChargingPath])
	}
}

// --- Finding 1: a failed/empty refresh must never overwrite a good baseline

// TestLearnAndSaveEngineBaselineNeverOverwritesWithEmptyResult is the
// code-review finding: a refresh that learns nothing (every quantity's
// learnTwinBaseline call failed, or none had enough qualifying data) must
// not call saveEngineBaseline at all, so the previously-persisted, good
// baseline on disk survives untouched.
func TestLearnAndSaveEngineBaselineNeverOverwritesWithEmptyResult(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)
	t.Setenv("SETTINGS_FILE", settingsPath)

	baselinePath := filepath.Join(t.TempDir(), "baseline.json")
	good := engineBaseline{
		Engines: map[string]map[string][]engineBaselineBucket{
			"port": {"oilPressure": {{RPMBucket: 1800, Median: 5000, Minutes: 40}}},
		},
		ComputedAt:   anomalyDetectorTestNow,
		LookbackDays: engineBaselineLookbackDays,
	}
	if err := saveEngineBaseline(baselinePath, good); err != nil {
		t.Fatalf("seeding the existing baseline: %v", err)
	}

	// The fetcher returns real telemetryPoint values, but far too little of
	// it (3 minutes) to ever clear engineBaselineMinRunMinutes/
	// engineBaselineMinMinutes -- every quantity's learn call will come back
	// with zero buckets.
	prevFetch := fetchInfluxRangeChunked
	t.Cleanup(func() { fetchInfluxRangeChunked = prevFetch })
	fetchInfluxRangeChunked = func(path string, start, stop time.Time) ([]telemetryPoint, error) {
		var out []telemetryPoint
		for i := 0; i < 3; i++ {
			at := start.Add(time.Duration(i) * time.Minute)
			value := 1800.0 / 60
			if strings.HasSuffix(path, ".temperature") {
				value = 350
			} else if strings.HasSuffix(path, ".oilPressure") {
				value = 388100
			}
			out = append(out, telemetryPoint{Value: value, Timestamp: at})
		}
		return out, nil
	}

	if err := learnAndSaveEngineBaseline(baselinePath); err == nil {
		t.Fatalf("expected an error when nothing could be learned, got nil")
	}

	got, err := loadEngineBaseline(baselinePath)
	if err != nil {
		t.Fatalf("loading the baseline after the failed refresh: %v", err)
	}
	if len(got.Engines["port"]["oilPressure"]) != 1 || got.Engines["port"]["oilPressure"][0].Median != 5000 {
		t.Fatalf("expected the previous good baseline to survive untouched, got %+v", got)
	}
}

// TestLearnAndSaveEngineBaselineSavesOnceEnoughIsLearned is the mirror
// case: real gappy, duplicate-carrying data (chunk-boundary duplicate rows,
// a few missing minutes) that still adds up to enough qualifying minutes
// must save successfully via the minute-bucketed join, not error out the
// way a strict index/timestamp-aligned join would.
func TestLearnAndSaveEngineBaselineSavesOnceEnoughIsLearned(t *testing.T) {
	settingsPath := setUpTwinEngineVessel(t)
	t.Setenv("SETTINGS_FILE", settingsPath)
	baselinePath := filepath.Join(t.TempDir(), "baseline.json")

	prevFetch := fetchInfluxRangeChunked
	t.Cleanup(func() { fetchInfluxRangeChunked = prevFetch })
	fetchInfluxRangeChunked = func(path string, start, stop time.Time) ([]telemetryPoint, error) {
		var out []telemetryPoint
		// 100 minutes of good data, but each path's own independent fetch
		// has its own gap and its own duplicated row -- a real boat's rpm,
		// coolant and oilPressure sends do not share one cadence, so a
		// join keyed on array index (rather than wall-clock minute) would
		// either error out or silently compare the wrong minutes against
		// each other. Gap/duplicate position is derived from the path
		// itself so port and starboard, and rpm/coolant/oilPressure, each
		// drift independently rather than all four moving in lockstep. The
		// gap sits past minute 70 so the run of minutes before either gap
		// (0..~70) alone clears twinGateMinRunFor's 10-minute warmup plus
		// engineBaselineMinRunMinutes' own 5-minute steady run before
		// engineBaselineMinMinutes' 30-qualifying-minute floor even starts
		// counting -- a short window here would join fine but still fall
		// short of that floor, which is a different failure than the one
		// this test is for.
		gapAt := 70 + len(path)%15
		dupAt := 5 + (len(path)*7)%10
		for i := 0; i < 100; i++ {
			if i == gapAt || i == gapAt+1 {
				continue
			}
			at := start.Add(time.Duration(i) * time.Minute)
			value := 1800.0 / 60
			if strings.HasSuffix(path, ".temperature") {
				value = 350
			} else if strings.HasSuffix(path, ".oilPressure") {
				if strings.Contains(path, "port") {
					value = 388100
				} else {
					value = 385600
				}
			}
			out = append(out, telemetryPoint{Value: value, Timestamp: at})
			if i == dupAt {
				out = append(out, telemetryPoint{Value: value, Timestamp: at})
			}
		}
		return out, nil
	}

	if err := learnAndSaveEngineBaseline(baselinePath); err != nil {
		t.Fatalf("expected the gappy/duplicate data to still learn a baseline, got error: %v", err)
	}

	got, err := loadEngineBaseline(baselinePath)
	if err != nil {
		t.Fatalf("loading the saved baseline: %v", err)
	}
	buckets := got.Engines["port"]["oilPressure"]
	if len(buckets) != 1 || buckets[0].Median != 2500 {
		t.Fatalf("expected a learned port/oilPressure bucket with median 2500 (388100-385600), got %+v", buckets)
	}
}
