package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

/*
anomaly_detector.go ties the three anomaly detectors (sensor health,
full-bank charging, engine differentials) into one derived-path producer,
following the same pattern forecast_warnings_fetcher.go's globalForecastWarningsSlot
already established: a single goroutine (startAnomalyDetector) runs the
checks on its own cadence and writes a mutex-guarded slot, which
derived_paths.go's addAnomalyValues reads on every computeDerivedPaths tick.

This is the file that has to hold state across ticks -- 15-minute frozen
windows, engine runtime -- because computeDerivedPathsFromTree itself is
recomputed concurrently by several readers and cannot (see its own doc
comment). Running the detectors from exactly one goroutine is what makes
that state safe to hold at all: nothing else ever touches anomalyTrackers.
*/

// --- Fixed derived paths (sensor health + battery) --------------------------

const (
	anomalySensorFrozenCountPath       = derivedPathPrefix + "anomaly.sensor.frozenCount"
	anomalySensorOutOfRangeCountPath   = derivedPathPrefix + "anomaly.sensor.outOfRangeCount"
	anomalySensorSilentSourceCountPath = derivedPathPrefix + "anomaly.sensor.silentSourceCount"
	anomalyBatteryFullBankChargingPath = derivedPathPrefix + "anomaly.battery.fullBankCharging"
)

// anomalyFixedPathIDs and anomalyFixedPathUnits are merged into
// derived_paths.go's own derivedPathIDs/derivedPathUnits, the same way
// every other detector's paths are -- see derived_paths.go's init below.
var anomalyFixedPathIDs = []string{
	anomalySensorFrozenCountPath,
	anomalySensorOutOfRangeCountPath,
	anomalySensorSilentSourceCountPath,
	anomalyBatteryFullBankChargingPath,
}

// Unitless counts and levels, the same convention squashZoneIndex and the
// forecast wind ladder already use: a rule binds them with "above 0.5" or
// "above 1.5", not a physical unit.
var anomalyFixedPathUnits = map[string]string{
	anomalySensorFrozenCountPath:       "",
	anomalySensorOutOfRangeCountPath:   "",
	anomalySensorSilentSourceCountPath: "",
	anomalyBatteryFullBankChargingPath: "",
}

// --- Dynamic per-engine residual paths ---------------------------------

// anomalyEngineResidualQuantities are the six quantities the twin
// differential detector compares, each with the operator unit its residual
// path carries (alarm_units.go/quantities.go's deltaK/deltaPa, or the
// existing ratio unit for engine load).
var anomalyEngineResidualQuantities = []struct {
	Suffix string
	// Unit is the residual path's own unit -- a difference, never an
	// absolute reading's unit (see alarm_units.go's deltaK/deltaPa). Used
	// only for the derived path itself (anomalyEngineResidualUnit), a
	// standalone reading with no absolute figure alongside it to stay
	// consistent with.
	Unit string
	// AbsoluteUnit is the raw quantity's own SI unit, used for formatting an
	// evidence sentence's "port 84 degC" half.
	AbsoluteUnit string
	// EvidenceDeltaUnit is the unit formatEngineResidualEvidence renders the
	// offset/residual halves through -- must share AbsoluteUnit's own
	// DISPLAY LABEL (operatorUnitTable, alarm_units.go), or one evidence
	// sentence ends up mixing two units for the same quantity (code review
	// finding 2). Temperature's deltaK already renders through the same
	// "degC" label K does (a temperature difference in kelvin equals the
	// same difference in Celsius, so deltaK only scales, never offsets like
	// K's own -273.15). Pressure's own Pa->mb conversion is likewise a pure
	// scale with no offset, so it is exactly as valid for a difference as
	// for an absolute reading -- EvidenceDeltaUnit is "Pa" here, not
	// "deltaPa": deltaPa's own kPa label is for the residual PATH (Unit,
	// above), read as a standalone gauge value with no absolute figure
	// alongside it, where kPa is the more natural pressure-gap unit.
	EvidenceDeltaUnit string
}{
	{"temperature", "deltaK", "K", "deltaK"},
	{"oilPressure", "deltaPa", "Pa", "Pa"},
	{"boostPressure", "deltaPa", "Pa", "Pa"},
	{"engineLoad", "ratio", "ratio", "ratio"},
	{"transmission.oilPressure", "deltaPa", "Pa", "Pa"},
	{"transmission.oilTemperature", "deltaK", "K", "deltaK"},
}

// anomalyEngineResidualPath builds helmcentral.anomaly.engines.<instance>.<quantity>Residual.
// instance is the Signal K propulsion.<instance> segment (e.g. "port"),
// never the operator's own editable vessel.engines[].Name -- that display
// name can be changed at any time from Settings -> Vessel, and a path built
// from it would move out from under every alarm rule and derived-path
// subscriber bound to the old one the moment the operator renamed an
// engine, for no reason connected to the engine itself. The display name
// belongs in evidence text and rule labels only, never in a path.
func anomalyEngineResidualPath(instance, quantitySuffix string) string {
	return derivedPathPrefix + "anomaly.engines." + instance + "." + quantitySuffix + "Residual"
}

// anomalyEngineResidualUnit reports the operator unit for one of the six
// known quantity suffixes, or "" (not-applicable) for anything else.
func anomalyEngineResidualUnit(quantitySuffix string) string {
	for _, q := range anomalyEngineResidualQuantities {
		if q.Suffix == quantitySuffix {
			return q.Unit
		}
	}
	return ""
}

// anomalyEngineResidualPathIDs lists every residual path the currently
// configured engines could produce (one per engine per quantity), for
// signalk_paths.go's picker to list alongside the fixed derived paths --
// there is no static list of these the way every other derived path has
// one, since their number depends on vessel.engines. Off entirely for 0 or
// 1 configured engines, matching computeAnomalyEngineDifferentials' own
// gate.
func anomalyEngineResidualPathIDs(engines []vesselEngineSetting) []string {
	if len(engines) < 2 {
		return nil
	}
	var out []string
	for _, e := range engines {
		for _, q := range anomalyEngineResidualQuantities {
			out = append(out, anomalyEngineResidualPath(e.Instance, q.Suffix))
		}
	}
	return out
}

// --- The slot ---------------------------------------------------------

// anomalySlotMaxAge is the plan's "Values are absent if the slot is more
// than 5 s old" -- the detector tick stopped, or is running behind, and a
// stale anomaly reading must not keep looking live.
const anomalySlotMaxAge = 5 * time.Second

// anomalyReading is one tick's complete result: every path's value (fixed
// and dynamic together -- a plain map costs nothing extra for the dynamic
// per-engine paths, unlike derivedPathIDs' static slice), the evidence
// sentence behind each alarm-worthy path, which inputs were trusted this
// tick, and when it was computed.
type anomalyReading struct {
	Values     map[string]float64
	Evidence   map[string]string
	Validity   map[string]bool
	ComputedAt time.Time
}

type anomalySlot struct {
	mu      sync.RWMutex
	reading anomalyReading
	ok      bool
}

func (s *anomalySlot) set(r anomalyReading) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reading = r
	s.ok = true
}

func (s *anomalySlot) get() (anomalyReading, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reading, s.ok
}

// globalAnomalySlot is the production slot startAnomalyDetector writes and
// addAnomalyValues (derived_paths.go) reads, the same "tests build their
// own, production has no thread to pass one through" split
// globalForecastWarningsSlot documents.
var globalAnomalySlot = &anomalySlot{}

// --- Sensor health: frozen-window tracking ----------------------------

// frozenTrackerState is one engine-correlated path's rolling 15-minute
// window, owned entirely by the detector goroutine (see the file doc
// comment for why that is safe). Tumbling, not sliding: once a window
// completes (frozenWindowSpan elapses) it is judged and reset, rather than
// continuously sliding -- simpler to reason about, and a false-flat window
// that just missed the gate ages out within one window span either way.
type frozenTrackerState struct {
	windowStart        time.Time
	updateCount        int
	haveValue          bool
	firstValue         float64
	changed            bool
	minEngineHz        float64
	maxEngineHz        float64
	haveEngineHz       bool
	lastValueTimestamp time.Time
	lastObservedAt     time.Time
}

// observe folds one tick's reading of path's value (and every configured
// engine's current rpm) into the tracker. valueSeen is the path's own last
// update time (snapshot.lastSeen); a valueSeen that has not advanced since
// the previous tick is not a new update. now is the tick's own wall time,
// used only to detect a window boundary.
func (fs *frozenTrackerState) observe(value float64, valueSeen time.Time, valuePresent bool, engineHz []float64, now time.Time) {
	if fs.windowStart.IsZero() {
		fs.windowStart = now
	}

	if valuePresent && valueSeen.After(fs.lastValueTimestamp) {
		fs.lastValueTimestamp = valueSeen
		fs.updateCount++
		if !fs.haveValue {
			fs.haveValue = true
			fs.firstValue = value
		} else if value != fs.firstValue {
			fs.changed = true
		}
	}

	for _, hz := range engineHz {
		if !fs.haveEngineHz {
			fs.minEngineHz, fs.maxEngineHz = hz, hz
			fs.haveEngineHz = true
			continue
		}
		if hz < fs.minEngineHz {
			fs.minEngineHz = hz
		}
		if hz > fs.maxEngineHz {
			fs.maxEngineHz = hz
		}
	}

	fs.lastObservedAt = now
}

// windowComplete reports whether fs's window has run its full span as of
// now, along with the frozenWindow summary frozenVerdict judges. Calling
// this does not reset the tracker -- the caller resets explicitly once it
// has used the result, so a caller that only wants to peek is free to.
func (fs *frozenTrackerState) windowComplete(now time.Time) (frozenWindow, bool) {
	if fs.windowStart.IsZero() || now.Sub(fs.windowStart) < frozenWindowSpan {
		return frozenWindow{}, false
	}
	return frozenWindow{
		UpdateCount:   fs.updateCount,
		ValueChanged:  fs.changed,
		StillUpdating: now.Sub(fs.lastValueTimestamp) <= inputValidityMaxAge,
		MinEngineHz:   fs.minEngineHz,
		MaxEngineHz:   fs.maxEngineHz,
	}, true
}

func (fs *frozenTrackerState) reset(now time.Time) {
	*fs = frozenTrackerState{windowStart: now}
}

// anomalyTrackers holds every piece of state startAnomalyDetector carries
// across ticks, owned by that one goroutine alone.
type anomalyTrackers struct {
	frozen map[string]*frozenTrackerState
	// frozenPaths is which paths were flagged frozen by the most recently
	// completed window for each -- persists between window completions so
	// frozenCount reads the latest verdict every tick, not only on the
	// tick a window happens to close.
	frozenPaths map[string]bool
	// voltageHistory is the house bank's own trailing 60s of voltage
	// samples, feeding fullBankChargingInputs.DVdtPerMinute (the rising-
	// pack half of level 2's OR condition). One tracker, not a map: there
	// is exactly one house bank.
	voltageHistory voltageHistoryTracker
}

func newAnomalyTrackers() *anomalyTrackers {
	return &anomalyTrackers{
		frozen:      map[string]*frozenTrackerState{},
		frozenPaths: map[string]bool{},
	}
}

// engineCorrelatedPaths lists every propulsion.<instance>.<suffix> path the
// frozen check watches for the given configured engine instances, across
// every quantity the twin differential detector also compares plus rpm
// itself -- the frozen check's own gate needs every configured engine's
// rpm regardless of which single path is being judged.
func engineCorrelatedPaths(engines []vesselEngineSetting) []string {
	var paths []string
	for _, e := range engines {
		for _, q := range anomalyEngineResidualQuantities {
			paths = append(paths, "propulsion."+e.Instance+"."+q.Suffix)
		}
	}
	sort.Strings(paths)
	return paths
}

// --- Twin gate steadiness tracking --------------------------------------

// conditionTracker tracks how long a boolean condition has held true
// continuously, resetting the instant it reports false -- the twin gate's
// own steadiness sub-clauses (rpm steady 60s, coolant steady 300s) and the
// per-engine running-for-10-minutes clause all reduce to this same shape.
type conditionTracker struct {
	trueSince time.Time
}

// observe folds one tick's reading of holds into the tracker and returns
// how long it has now held continuously (zero if holds is false).
func (c *conditionTracker) observe(holds bool, now time.Time) time.Duration {
	if !holds {
		c.trueSince = time.Time{}
		return 0
	}
	if c.trueSince.IsZero() {
		c.trueSince = now
	}
	return now.Sub(c.trueSince)
}

// dVdtWindow is how far back voltageHistoryTracker looks to compute the
// house bank's rate of climb -- 5 minutes, not the plan's original 60s.
// The plan's own two-point slope over 60s at its original 5 mV/min
// threshold was finer than a typical battery monitor's own ~0.01V reading
// resolution: a single quantization step landing on the oldest or newest
// sample alone was enough, by itself, to read as "rising" (code review
// finding 10). A longer window fit by least squares across every sample in
// it, rather than just the two endpoints, averages that reading noise out
// instead of amplifying it.
const dVdtWindow = 5 * time.Minute

// dVdtMinSamples is the fewest samples observe will fit a slope to. At the
// live detector's 1s tick a full window holds roughly 300 samples; this
// floor is generous enough to tolerate real gaps (a brief invalid reading,
// a tick that ran long) without accepting what is really still only a
// handful of points spread across 5 minutes -- which would reintroduce the
// same quantization-noise sensitivity a bare two-point slope had.
const dVdtMinSamples = 60

// dVdtMinSpan is the least wall-clock spread observe requires between its
// oldest and newest kept sample before trusting a slope -- without this, a
// tracker that has only just started (or that lost most of its history to
// a gap) could satisfy dVdtMinSamples from a burst of ticks spanning far
// less time than dVdtWindow implies, which is the same "too few real
// samples per unit time" problem in different clothes.
const dVdtMinSpan = 4 * time.Minute

type voltageHistorySample struct {
	at    time.Time
	value float64
}

// voltageHistoryTracker holds a trailing dVdtWindow of one path's voltage
// samples -- goroutine-owned state, the same reason frozenTrackerState and
// conditionTracker are -- and reports the rate of change in volts per
// minute, fullBankChargingInputs.DVdtPerMinute's own input.
type voltageHistoryTracker struct {
	samples []voltageHistorySample
	// bankPath is the house bank path the current run of samples belongs
	// to. resetIfBankChanged drops the whole run the moment this changes,
	// so a different bank chosen in Settings -> Vessel -> Power (or the
	// same bank unset then reconfigured) never blends a trailing run of one
	// pack's voltage into a completely different pack's dV/dt fit (code
	// review finding 5).
	bankPath string
}

// resetIfBankChanged clears the tracker's samples when bankPath differs from
// the path the current run belongs to -- called once per tick, before
// observe, so a bank switch takes effect on the very next reading rather
// than after dVdtWindow ages the old bank's samples out on its own.
func (t *voltageHistoryTracker) resetIfBankChanged(bankPath string) {
	if t.bankPath != bankPath {
		t.samples = nil
		t.bankPath = bankPath
	}
}

// observe appends a new sample when valid is true, prunes the window down
// to dVdtWindow, and returns a least-squares fit of the rate of change
// across every sample still in the window -- not a two-point slope between
// the oldest and newest alone, which a single noisy or quantized reading at
// either end could swing on its own (code review finding 10). 0 -- not an
// error, and not treated as "flat" by the caller -- until both
// dVdtMinSamples and dVdtMinSpan are satisfied, matching
// fullBankChargingInputs.DVdtPerMinute's own "zero means unknown, only ever
// promotes to level 2, never blocks it" contract.
//
// valid false (a stale or glitched reading -- a 0 V dropout -- that the
// caller's own validity check already rejected this tick) skips recording
// the sample entirely, rather than letting it sit in the trailing window for
// up to dVdtWindow the way every other sample does (code review finding 5):
// the window is still pruned and re-fit from whatever genuine samples
// remain, so a single bad tick does not also blank an already-established
// rate to 0.
func (t *voltageHistoryTracker) observe(voltage float64, now time.Time, valid bool) float64 {
	if valid {
		t.samples = append(t.samples, voltageHistorySample{at: now, value: voltage})
	}

	cutoff := now.Add(-dVdtWindow)
	// anchorFloor is how far before cutoff the single retained anchor
	// sample (see below) may be and still count as a genuine continuation
	// of the window, rather than a stale point left over from a gap (the
	// detector paused, or the house bank was briefly unconfigured). Without
	// this floor, that anchor survives no matter how old it is, and a burst
	// of fresh samples right after a gap can satisfy dVdtMinSamples and
	// dVdtMinSpan from what is really just two points spanning the whole
	// gap, not a genuine window's worth of history (code review finding 4).
	// One tick's tolerance either side of the cutoff is enough for the
	// anchor's own purpose -- keeping the window's effective span close to
	// dVdtWindow across ordinary ticking -- without also trusting a gap.
	anchorFloor := cutoff.Add(-anomalyDetectorInterval)

	keepFrom := 0
	for i, s := range t.samples {
		if s.at.After(cutoff) {
			break
		}
		if s.at.Before(anchorFloor) {
			// Too old to serve as the window's anchor -- drop it, and
			// everything before it, rather than keep stretching the
			// window back across a gap.
			keepFrom = i + 1
			continue
		}
		keepFrom = i
	}
	t.samples = t.samples[keepFrom:]

	if len(t.samples) < dVdtMinSamples {
		return 0
	}
	oldest := t.samples[0]
	if now.Sub(oldest.at) < dVdtMinSpan {
		return 0
	}
	return leastSquaresSlopePerMinute(t.samples)
}

// leastSquaresSlopePerMinute fits an ordinary least-squares line to
// samples (elapsed minutes since the earliest sample, vs. voltage) and
// returns its slope in volts per minute. Fitting every sample rather than
// just the two endpoints is what lets a longer dVdtWindow actually average
// out reading noise instead of just diluting a two-point slope over a
// longer, equally noise-sensitive baseline.
func leastSquaresSlopePerMinute(samples []voltageHistorySample) float64 {
	oldest := samples[0].at
	n := float64(len(samples))

	var sumX, sumY, sumXY, sumXX float64
	for _, s := range samples {
		x := s.at.Sub(oldest).Minutes()
		sumX += x
		sumY += s.value
		sumXY += x * s.value
		sumXX += x * x
	}

	denominator := n*sumXX - sumX*sumX
	if denominator == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denominator
}

// anomalyTrackers (continued): the twin gate's own steadiness state, added
// to the struct declared above it in this file.
type twinSteadinessTrackers struct {
	rpmGroup conditionTracker
	coolant  map[string]*conditionTracker
	running  map[string]*conditionTracker
	// invalidSince tracks, per engine instance, when that engine's own
	// rpm/coolant inputs most recently became invalid -- absent while
	// currently valid. See invalidTooLong.
	invalidSince map[string]time.Time
}

// invalidTooLong folds one tick's validity for engine into invalidSince and
// reports whether that engine's inputs have now been invalid for longer than
// inputValidityMaxAge -- long enough that the gap is not just a momentary
// blip (a dropped N2K frame, one bad tick) but plausibly covers the engine
// actually stopping and restarting, which runningFor/coolantFor must not
// silently ride through (code review finding 8): those trackers are only
// ever observe()'d on a tick where the engine is currently valid, so without
// this, an arbitrarily long invalid stretch left them frozen at whatever
// duration they last saw, and the instant good data returned, that stale
// duration satisfied the twin gate's own running/steadiness thresholds
// immediately.
func (t *twinSteadinessTrackers) invalidTooLong(engine string, validNow bool, now time.Time) bool {
	if validNow {
		delete(t.invalidSince, engine)
		return false
	}
	if t.invalidSince == nil {
		t.invalidSince = map[string]time.Time{}
	}
	since, seen := t.invalidSince[engine]
	if !seen {
		t.invalidSince[engine] = now
		return false
	}
	return now.Sub(since) > inputValidityMaxAge
}

func (t *twinSteadinessTrackers) coolantFor(engine string) *conditionTracker {
	if t.coolant == nil {
		t.coolant = map[string]*conditionTracker{}
	}
	tr, ok := t.coolant[engine]
	if !ok {
		tr = &conditionTracker{}
		t.coolant[engine] = tr
	}
	return tr
}

func (t *twinSteadinessTrackers) runningFor(engine string) *conditionTracker {
	if t.running == nil {
		t.running = map[string]*conditionTracker{}
	}
	tr, ok := t.running[engine]
	if !ok {
		tr = &conditionTracker{}
		t.running[engine] = tr
	}
	return tr
}

// --- Baseline cache: loaded at startup, refreshed every 24h ---------------

type engineBaselineCache struct {
	mu       sync.RWMutex
	baseline engineBaseline
	loaded   bool
}

func (c *engineBaselineCache) set(b engineBaseline) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.baseline = b
	c.loaded = true
}

func (c *engineBaselineCache) get() (engineBaseline, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseline, c.loaded
}

var globalEngineBaselineCache = &engineBaselineCache{}

// --- Helpers shared by the battery and engine wiring below ----------------

func numericFromPath(snapshot *signalKSnapshot, path string) (float64, bool) {
	node := snapshot.nodeAt(path)
	if node == nil {
		return 0, false
	}
	return numericNodeValue(node)
}

// discoveredEngineBatteryPaths lists every currently-published path whose
// quantity physicalLimits knows (propulsion.*/electrical.batteries.* leaf
// suffixes) -- the impossible-reading check's own input set, which needs no
// vessel setup: it watches whatever the boat happens to publish, not only
// configured engines/the configured house bank.
func discoveredEngineBatteryPaths(snapshot *signalKSnapshot) []string {
	tree := snapshot.selfTree()
	if tree == nil {
		return nil
	}
	var out []string
	for _, p := range collectSignalKPaths(tree) {
		if _, ok := engineOrBatterySuffix(p.Path); ok {
			out = append(out, p.Path)
		}
	}
	return out
}

// discoverChargeSources walks the currently-published tree for
// electrical.alternator.*.current, electrical.chargers.*.current and any
// solar current, keyed by a human label -- evidence enrichment only, per
// the plan's "discovered rather than configured".
func discoverChargeSources(snapshot *signalKSnapshot) map[string]float64 {
	tree := snapshot.selfTree()
	if tree == nil {
		return nil
	}
	sources := map[string]float64{}
	for _, p := range collectSignalKPaths(tree) {
		var label string
		switch {
		case strings.HasPrefix(p.Path, "electrical.alternator.") && strings.HasSuffix(p.Path, ".current"):
			id := strings.TrimSuffix(strings.TrimPrefix(p.Path, "electrical.alternator."), ".current")
			label = "alternator " + id
		case strings.HasPrefix(p.Path, "electrical.chargers.") && strings.HasSuffix(p.Path, ".current"):
			id := strings.TrimSuffix(strings.TrimPrefix(p.Path, "electrical.chargers."), ".current")
			label = "charger " + id
		case strings.HasPrefix(p.Path, "electrical.solar.") && strings.HasSuffix(p.Path, ".current"):
			id := strings.TrimSuffix(strings.TrimPrefix(p.Path, "electrical.solar."), ".current")
			label = "solar " + id
		default:
			continue
		}
		if v, ok := numericFromPath(snapshot, p.Path); ok {
			sources[label] = v
		}
	}
	if len(sources) == 0 {
		return nil
	}
	return sources
}

// equipmentBatteryProfile resolves a vessel.house_bank.equipment_id (or a
// vessel.engines[].equipment_id) to its linked battery/engine profile, via
// the inventory registry item's own profile_id -- the plan's "one home for
// the profile": Vessel and Inventory read/write the same field, never two
// copies.
func equipmentProfile(equipmentID string) (engineProfile, bool) {
	if equipmentID == "" || globalDocumentStore == nil {
		return engineProfile{}, false
	}
	item, err := globalDocumentStore.GetEquipment(equipmentID)
	if err != nil || item.ProfileID == "" {
		return engineProfile{}, false
	}
	profiles, _ := engineProfiles()
	for _, p := range profiles {
		if p.ID == item.ProfileID {
			return p, true
		}
	}
	return engineProfile{}, false
}

// --- Full-bank charging wiring ------------------------------------------

// computeAnomalyBattery evaluates the full-bank-charging detector against
// vessel.house_bank and folds its result into reading. A house bank that is
// not fully set up (no bank chosen, no linked profile, or the profile's
// full_soc/charge_warn slots not yet filled) publishes nothing at all --
// the plan's "nothing is seeded or evaluated for an unconfigured detector".
func computeAnomalyBattery(snapshot *signalKSnapshot, vessel vesselSettings, valid fullBankChargingValidityFunc, voltageHistory *voltageHistoryTracker, now time.Time, reading *anomalyReading) {
	hb := vessel.HouseBank
	if hb == nil || strings.TrimSpace(hb.Path) == "" || hb.CapacityAh <= 0 || hb.Cells <= 0 {
		return
	}

	profile, ok := equipmentProfile(hb.EquipmentID)
	if !ok || profile.Kind != profileKindBattery {
		return
	}

	warnV, highV, ok := batteryPackThresholds(profile, hb.Cells)
	if !ok {
		return
	}
	if hb.WarnVoltage > 0 {
		warnV = hb.WarnVoltage
	}
	if hb.HighVoltage > 0 {
		highV = hb.HighVoltage
	}

	if profile.FullSOC == nil || profile.FullSOC.Value == nil || *profile.FullSOC.Value <= 0 {
		return
	}

	settings := fullBankChargingSettings{
		CapacityAh:  hb.CapacityAh,
		FullSOC:     *profile.FullSOC.Value,
		WarnVoltage: warnV,
		HighVoltage: highV,
		BankPath:    hb.Path,
	}

	soc, socOK := numericFromPath(snapshot, hb.Path+".capacity.stateOfCharge")
	voltage, vOK := numericFromPath(snapshot, hb.Path+".voltage")
	current, cOK := numericFromPath(snapshot, hb.Path+".current")
	if !socOK || !vOK || !cOK {
		return
	}

	// A different bank than the one this tracker's samples belong to (a
	// fresh choice in Settings -> Vessel -> Power, or the same bank unset
	// then reconfigured) must not blend its trailing voltage into this
	// bank's dV/dt fit (code review finding 5).
	voltageHistory.resetIfBankChanged(hb.Path)

	// Only record a voltage reading the caller's own validity check already
	// trusts -- a stale or glitched reading (a 0 V dropout) must not sit in
	// the trailing window for up to dVdtWindow corrupting the fit the whole
	// time (code review finding 5). A nil valid trusts every input, the
	// same "nil means trust everything" contract fullBankChargingLevel's
	// own valid param already documents.
	voltageValid := valid == nil || valid(hb.Path+".voltage")

	inputs := fullBankChargingInputs{
		SoC: soc, Voltage: voltage, Current: current,
		// The rising-pack half of level 2's OR condition (+5 mV/min over
		// 60s): a trailing voltage history the caller's own goroutine holds
		// across ticks (voltageHistoryTracker), not recomputed here.
		DVdtPerMinute: voltageHistory.observe(voltage, now, voltageValid),
		ChargeSources: discoverChargeSources(snapshot),
	}

	result := fullBankChargingLevel(inputs, settings, valid)
	if !result.Ok {
		return
	}
	reading.Values[anomalyBatteryFullBankChargingPath] = float64(result.Level)
	if result.Evidence != "" {
		reading.Evidence[anomalyBatteryFullBankChargingPath] = result.Evidence
	}
}

// --- Engine differentials wiring ------------------------------------------

// formatEngineResidualEvidence renders the plan's evidence style: "At 2,400
// rpm port 84.0 degC, peers 79.0 degC; usually 1.2 degC off (38m learned);
// now 3.8 degC beyond that."
//
// ownValue and peerMedian are absolute readings and render through
// absoluteUnit (e.g. "K", which subtracts 273.15); offset and residual are
// DIFFERENCES between two readings and must render through deltaUnit (e.g.
// "deltaK", scale only) instead -- formatting a difference through the
// absolute unit's own offset turns a genuine 1 K learned offset into
// "-272.2 degC" (code review finding 2). See alarm_units.go's deltaK/deltaPa
// entries.
func formatEngineResidualEvidence(engineName string, absoluteUnit, deltaUnit string, ownValue, peerMedian, offset, residual float64, minutes int, rpmHz float64) string {
	rpm := int(math.Round(rpmHz * 60))
	return fmt.Sprintf(
		"At %d rpm %s %s, peers %s; usually %s off (%dm learned); now %s beyond that.",
		rpm, engineName, formatAlarmReading(ownValue, absoluteUnit), formatAlarmReading(peerMedian, absoluteUnit),
		formatAlarmReading(offset, deltaUnit), minutes, formatAlarmReading(residual, deltaUnit),
	)
}

// computeAnomalyEngineDifferentials evaluates the twin/N-way differential
// detector for every configured quantity, folding each engine's residual
// into reading. Off entirely for 0 or 1 configured engines. The gate is
// judged once per tick, shared across every quantity (they are all read at
// the same instant); a tick where the gate does not hold publishes no
// residuals at all, leaving the derived paths absent rather than stale.
//
// valid gates every path this function reads through the same age/range
// check computeAnomalyBattery applies to the house bank's own paths
// (inputValidity, via computeAnomalyReading's own closure). Without it, a
// dead gateway's last-known rpm/coolant/quantity readings would sit in the
// snapshot forever: numericFromPath alone has no notion of age, so the twin
// gate's steadiness windows would happily accumulate off a value the boat
// stopped sending minutes ago and the detector would keep comparing stale
// numbers indefinitely instead of going quiet. A nil valid trusts every
// input, matching fullBankChargingLevel's own "nil means trust everything"
// contract for tests that have no reason to exercise staleness.
func computeAnomalyEngineDifferentials(snapshot *signalKSnapshot, vessel vesselSettings, valid func(path string) bool, steadiness *twinSteadinessTrackers, now time.Time, reading *anomalyReading) {
	engines := vessel.Engines
	if len(engines) < 2 {
		return
	}
	if valid == nil {
		valid = func(string) bool { return true }
	}

	type liveEngine struct {
		setting        vesselEngineSetting
		rpmHz, coolant float64
		ok             bool
	}
	live := make([]liveEngine, len(engines))
	for i, e := range engines {
		rpmPath := "propulsion." + e.Instance + ".revolutions"
		coolantPath := "propulsion." + e.Instance + ".temperature"
		rpm, rpmOK := numericFromPath(snapshot, rpmPath)
		coolant, coolantOK := numericFromPath(snapshot, coolantPath)
		ok := rpmOK && coolantOK && valid(rpmPath) && valid(coolantPath)
		live[i] = liveEngine{setting: e, rpmHz: rpm, coolant: coolant, ok: ok}
	}

	minHz, maxHz := math.Inf(1), math.Inf(-1)
	everyReading := true
	for _, e := range live {
		if !e.ok {
			everyReading = false
			continue
		}
		if e.rpmHz < minHz {
			minHz = e.rpmHz
		}
		if e.rpmHz > maxHz {
			maxHz = e.rpmHz
		}
	}
	rpmInstantOK := everyReading && minHz >= twinGateMinRPMHz && (maxHz-minHz) <= twinGateMaxRPMSpreadHz
	rpmSteadyDur := steadiness.rpmGroup.observe(rpmInstantOK, now)

	states := make([]engineTwinState, 0, len(live))
	for _, e := range live {
		if !e.ok {
			// Invalid for longer than a momentary blip: the engine's own
			// running/coolant-steady trackers must not silently carry
			// whatever duration they last saw across a gap that could just
			// as well have covered the engine actually stopping (code
			// review finding 8).
			if steadiness.invalidTooLong(e.setting.Instance, false, now) {
				steadiness.runningFor(e.setting.Instance).observe(false, now)
				steadiness.coolantFor(e.setting.Instance).observe(false, now)
			}
			continue
		}
		steadiness.invalidTooLong(e.setting.Instance, true, now)
		runFor := steadiness.runningFor(e.setting.Instance).observe(e.rpmHz > twinIdleRPMHz, now)
		coolantSteadyDur := steadiness.coolantFor(e.setting.Instance).observe(e.coolant >= twinGateMinCoolantK, now)
		states = append(states, engineTwinState{
			Name: e.setting.Instance, RPMHz: e.rpmHz, CoolantK: e.coolant, RunningFor: runFor,
			RPMSteady:     rpmSteadyDur >= twinGateRPMSteadyFor,
			CoolantSteady: coolantSteadyDur >= twinGateCoolantSteadyFor,
		})
	}

	if !twinGateHolds(states) {
		return
	}

	baseline, haveBaseline := globalEngineBaselineCache.get()
	rpmByEngine := map[string]float64{}
	for _, e := range live {
		if e.ok {
			rpmByEngine[e.setting.Instance] = e.rpmHz
		}
	}

	for _, q := range anomalyEngineResidualQuantities {
		values := map[string]float64{}
		for _, e := range engines {
			path := "propulsion." + e.Instance + "." + q.Suffix
			if !valid(path) {
				continue
			}
			if v, ok := numericFromPath(snapshot, path); ok {
				values[e.Instance] = v
			}
		}
		if len(values) != len(engines) {
			continue
		}

		for _, e := range engines {
			var peers []float64
			for instance, v := range values {
				if instance != e.Instance {
					peers = append(peers, v)
				}
			}

			var offset float64
			var haveOffset bool
			var minutes int
			if haveBaseline {
				if buckets, ok := baseline.Engines[e.Instance][q.Suffix]; ok {
					offset, haveOffset = engineBaselineOffsetFor(buckets, rpmByEngine[e.Instance])
					if haveOffset {
						want := rpmBucket(rpmByEngine[e.Instance])
						for _, b := range buckets {
							if b.RPMBucket == want {
								minutes = b.Minutes
							}
						}
					}
				}
			}

			residual, ok := residualQuantity(values[e.Instance], peers, offset, haveOffset)
			if !ok {
				continue
			}

			// The path is keyed on the stable Signal K instance, never the
			// operator's own editable display name (see
			// anomalyEngineResidualPath's doc comment) -- the display name
			// is used below only in the evidence sentence, where an
			// operator reading an alarm wants "Port Main", not "port".
			name := e.Name
			if strings.TrimSpace(name) == "" {
				name = e.Instance
			}
			path := anomalyEngineResidualPath(e.Instance, q.Suffix)
			reading.Values[path] = residual
			reading.Evidence[path] = formatEngineResidualEvidence(
				name, q.AbsoluteUnit, q.EvidenceDeltaUnit, values[e.Instance], median(peers), offset, residual, minutes, rpmByEngine[e.Instance],
			)
		}
	}
}

// --- The tick: computeAnomalyReading --------------------------------------

// computeAnomalyReading runs every check once and returns the tick's full
// result. settingsPath is threaded through explicitly (rather than the
// package-wide getEnv("SETTINGS_FILE", ...) default) so a test can point it
// at a fixture without touching the environment.
func computeAnomalyReading(snapshot *signalKSnapshot, settingsPath string, trackers *anomalyTrackers, steadiness *twinSteadinessTrackers, now time.Time) anomalyReading {
	reading := anomalyReading{
		Values:     map[string]float64{},
		Evidence:   map[string]string{},
		Validity:   map[string]bool{},
		ComputedAt: now,
	}

	vessel, err := loadVesselSettings(settingsPath)
	if err != nil {
		log.Printf("anomaly detector: vessel settings unavailable, skipping this tick: %v", err)
		return reading
	}

	// The operator's own exclusions from the frozen/impossible/silent-source
	// alarm card's "Ignore this sensor" action (alarm_ignored_sensors.go) --
	// Pikorua's dead exhaust senders, on the boat, never shipped config.
	ignored := ignoredSensorSet()

	// Impossible readings: no setup needed, scans whatever the boat
	// publishes under a known engine/battery quantity.
	var outOfRangePaths []string
	for _, path := range discoveredEngineBatteryPaths(snapshot) {
		if ignored[path] {
			continue
		}
		v, ok := numericFromPath(snapshot, path)
		if !ok {
			continue
		}
		ok = classifyRange(path, v) != rangeOutOfRange
		reading.Validity[path] = ok
		if !ok {
			outOfRangePaths = append(outOfRangePaths, path)
		}
	}
	sort.Strings(outOfRangePaths)
	reading.Values[anomalySensorOutOfRangeCountPath] = float64(len(outOfRangePaths))
	// Evidence names the offending paths -- a bare count gives the operator
	// nothing to act on, and the "Ignore this sensor" alarm-card action
	// needs an identifier to submit.
	if len(outOfRangePaths) > 0 {
		reading.Evidence[anomalySensorOutOfRangeCountPath] = strings.Join(outOfRangePaths, ", ")
	}

	// Silent source: no setup needed either.
	self := snapshot.selfContext()
	sources := snapshot.sourcesFor(self)
	health := make([]sourceHealth, 0, len(sources))
	for source, entry := range sources {
		health = append(health, sourceHealth{Source: source, First: entry.First, Last: entry.Last, Count: entry.Count})
	}
	_, lastMessage := snapshot.status()
	silent := silentSources(health, now, now.Sub(lastMessage), ignored)
	reading.Values[anomalySensorSilentSourceCountPath] = float64(len(silent))
	if len(silent) > 0 {
		reading.Evidence[anomalySensorSilentSourceCountPath] = strings.Join(silent, ", ")
	}

	// Frozen: needs at least one configured engine.
	frozenCount := 0
	currentFrozenPaths := map[string]bool{}
	for _, path := range engineCorrelatedPaths(vessel.Engines) {
		currentFrozenPaths[path] = true
		tracker := trackers.frozen[path]
		if tracker == nil {
			tracker = &frozenTrackerState{}
			trackers.frozen[path] = tracker
		}

		value, present := numericFromPath(snapshot, path)
		var engineHz []float64
		for _, e := range vessel.Engines {
			if hz, ok := numericFromPath(snapshot, "propulsion."+e.Instance+".revolutions"); ok {
				engineHz = append(engineHz, hz)
			}
		}
		tracker.observe(value, snapshot.lastSeen(self, path), present, engineHz, now)

		if w, complete := tracker.windowComplete(now); complete {
			trackers.frozenPaths[path] = frozenVerdict(path, w, ignored)
			tracker.reset(now)
		}
	}
	// The verdict a window last recorded can be stale by the time this tick
	// runs: the operator may have unticked the engine since (path no longer
	// current at all) or ignored the sensor from the alarm card (still
	// current, but now excluded). Both must take effect this tick, not wait
	// for the next window boundary up to frozenWindowSpan away -- so recheck
	// every stored verdict against the CURRENT engines and ignore list
	// rather than trusting it outright. Bookkeeping for a path that is no
	// longer configured at all is dropped here too, so it does not
	// accumulate forever as engines are added and removed.
	var frozenPaths []string
	for path, flagged := range trackers.frozenPaths {
		if !currentFrozenPaths[path] {
			delete(trackers.frozenPaths, path)
			delete(trackers.frozen, path)
			continue
		}
		if ignored[path] {
			continue
		}
		if flagged {
			frozenCount++
			frozenPaths = append(frozenPaths, path)
		}
	}
	sort.Strings(frozenPaths)
	reading.Values[anomalySensorFrozenCountPath] = float64(frozenCount)
	if len(frozenPaths) > 0 {
		reading.Evidence[anomalySensorFrozenCountPath] = strings.Join(frozenPaths, ", ")
	}

	// The sensor-health verdicts above gate the battery and engine-
	// differential detectors below, not just their own alarm counts: a
	// stuck or silenced reading is exactly the kind of input those two
	// detectors must not trust either (code review finding 3 -- the commit
	// message and inputValidity's own doc comment both promised this
	// gating; valid used to consult inputValidity alone). Built from
	// frozenPaths/silent, the same currently-active, ignore-aware sets the
	// counts just above published, not trackers.frozenPaths' raw internal
	// state, so a sensor the operator has already ignored still gates
	// nothing.
	frozenNow := make(map[string]bool, len(frozenPaths))
	for _, p := range frozenPaths {
		frozenNow[p] = true
	}
	silentSourceNow := make(map[string]bool, len(silent))
	for _, s := range silent {
		silentSourceNow[s] = true
	}
	valid := func(path string) bool {
		if !inputValidity(snapshot, path, now) {
			return false
		}
		if frozenNow[path] {
			return false
		}
		if node := snapshot.nodeAt(path); node != nil {
			if source, ok := node["$source"].(string); ok && silentSourceNow[source] {
				return false
			}
		}
		return true
	}

	computeAnomalyBattery(snapshot, vessel, valid, &trackers.voltageHistory, now, &reading)
	computeAnomalyEngineDifferentials(snapshot, vessel, valid, steadiness, now, &reading)

	return reading
}

// --- The goroutine ---------------------------------------------------------

// startAnomalyDetector runs computeAnomalyReading on interval, writing each
// result to globalAnomalySlot. All rolling-window state (frozen windows,
// twin gate steadiness) lives in this one goroutine's own local variables --
// see the file doc comment for why that is the only place it is safe to
// hold at all.
// anomalyDetectorInterval is the plan's "startAnomalyDetector(ctx, 1s)".
const anomalyDetectorInterval = 1 * time.Second

// anomalyBaselineRefreshInterval is the plan's "relearned every 24h".
const anomalyBaselineRefreshInterval = 24 * time.Hour

func startAnomalyDetector(ctx context.Context, interval time.Duration) {
	trackers := newAnomalyTrackers()
	steadiness := &twinSteadinessTrackers{}
	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			reading := computeAnomalyReading(globalSignalKSnapshot, settingsPath, trackers, steadiness, now.UTC())
			globalAnomalySlot.set(reading)
		}
	}
}

// startTwinBaselineRefresher relearns data/anomaly-twin-baseline.json every
// interval (the plan's 24h) from the last engineBaselineLookbackDays of
// Influx history, fetched in 7-day chunks (queryInfluxPathRange's own 6s
// timeout budget). A failed refresh -- Influx unreachable, a chunk query
// erroring, too little qualifying data -- keeps the previous file and logs
// the failure; it never overwrites a working baseline with an empty one.
func startTwinBaselineRefresher(ctx context.Context, interval time.Duration) {
	refreshEngineBaseline()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshEngineBaseline()
		}
	}
}

// refreshEngineBaseline is startTwinBaselineRefresher's one pass: relearn
// from Influx if configured, else fall through to whatever was last
// persisted to disk. A boat with no InfluxDB configured (influx.go's
// influxTelemetryConfigured) simply keeps running on its last saved
// baseline (absent, on a fresh install) -- there is nothing to learn from,
// which is not a failure.
func refreshEngineBaseline() {
	path := anomalyTwinBaselinePath()

	if influxTelemetryConfigured() {
		if err := learnAndSaveEngineBaseline(path); err != nil {
			log.Printf("anomaly engine baseline: refresh failed, keeping the previous baseline: %v", err)
		}
	}

	b, err := loadEngineBaseline(path)
	if err != nil {
		log.Printf("anomaly engine baseline: could not load %s, twin residuals stay absent: %v", path, err)
		return
	}
	if len(b.Engines) > 0 {
		globalEngineBaselineCache.set(b)
	}
}

// learnAndSaveEngineBaseline fetches engineBaselineLookbackDays of 1m
// Influx history for every configured engine's rpm, coolant and the six
// residual quantities, in 7-day chunks, and persists what learnTwinBaseline
// makes of it. Pulled out of refreshEngineBaseline so the "no Influx
// configured" branch above never has to build a settingsPath/vessel lookup
// it will not use.
func learnAndSaveEngineBaseline(path string) error {
	vessel, err := loadVesselSettings(getEnv("SETTINGS_FILE", "../settings.yaml"))
	if err != nil {
		return fmt.Errorf("vessel settings: %w", err)
	}
	if len(vessel.Engines) < 2 {
		return nil // nothing to learn for 0 or 1 configured engines
	}

	stop := time.Now().UTC()
	start := stop.AddDate(0, 0, -engineBaselineLookbackDays)

	engines := make([]engineSeries, 0, len(vessel.Engines))
	for _, e := range vessel.Engines {
		rpm, err := fetchInfluxRangeChunked("propulsion."+e.Instance+".revolutions", start, stop)
		if err != nil {
			return fmt.Errorf("%s rpm: %w", e.Instance, err)
		}
		coolant, err := fetchInfluxRangeChunked("propulsion."+e.Instance+".temperature", start, stop)
		if err != nil {
			return fmt.Errorf("%s coolant: %w", e.Instance, err)
		}
		engines = append(engines, engineSeries{Name: e.Instance, RPM: rpm, Coolant: coolant})
	}

	result := engineBaseline{
		Engines:      map[string]map[string][]engineBaselineBucket{},
		ComputedAt:   stop,
		LookbackDays: engineBaselineLookbackDays,
	}

	for _, q := range anomalyEngineResidualQuantities {
		perEngine := make([]engineSeries, len(engines))
		for i, base := range engines {
			e := vessel.Engines[i]
			values, err := fetchInfluxRangeChunked("propulsion."+e.Instance+"."+q.Suffix, start, stop)
			if err != nil {
				return fmt.Errorf("%s %s: %w", e.Instance, q.Suffix, err)
			}
			perEngine[i] = engineSeries{Name: base.Name, RPM: base.RPM, Coolant: base.Coolant, Quantity: values}
		}

		learned, err := learnTwinBaseline(perEngine)
		if err != nil {
			// Not fatal to the whole refresh: a misaligned quantity (a gap
			// only this path has) should not blank out every other
			// quantity's learning too.
			log.Printf("anomaly engine baseline: %s: %v", q.Suffix, err)
			continue
		}
		for engine, buckets := range learned {
			if len(buckets) == 0 {
				continue
			}
			if result.Engines[engine] == nil {
				result.Engines[engine] = map[string][]engineBaselineBucket{}
			}
			result.Engines[engine][q.Suffix] = buckets
		}
	}

	// Never overwrite a previously-good baseline file with an empty one:
	// every quantity above either errored or came back with zero buckets
	// (not enough qualifying minutes in this window), so this refresh
	// learned nothing at all. Returning an error here, rather than saving
	// the empty result, is what makes refreshEngineBaseline's own "log and
	// keep the previous baseline" branch actually keep it.
	learnedAnything := false
	for _, quantities := range result.Engines {
		if len(quantities) > 0 {
			learnedAnything = true
			break
		}
	}
	if !learnedAnything {
		return fmt.Errorf("no baseline could be learned from this refresh (no quantity had enough qualifying data)")
	}

	return saveEngineBaseline(path, result)
}

// fetchInfluxRangeChunked fetches [start, stop) in 7-day chunks (Influx's
// own 6s query timeout, queryInfluxPathRange's doc comment) and
// concatenates the result in order. A package-level var (not a plain func)
// so a test can substitute a fake fetcher rather than needing a real
// InfluxDB connection -- the same test-seam idiom this codebase already
// uses (assistant_handlers.go's newAssistantRunner, place_name.go's
// placeNameProviderResolve).
//
// Chunk boundaries can duplicate a row (the same minute returned by both
// the chunk it closes and the chunk it opens, depending on the upstream
// query's own inclusivity), and a real feed has gaps -- neither is this
// function's problem to solve; learnTwinBaseline's own minute-bucketed
// join absorbs both.
var fetchInfluxRangeChunked = func(path string, start, stop time.Time) ([]telemetryPoint, error) {
	const chunk = 7 * 24 * time.Hour
	var out []telemetryPoint
	for chunkStart := start; chunkStart.Before(stop); chunkStart = chunkStart.Add(chunk) {
		chunkStop := chunkStart.Add(chunk)
		if chunkStop.After(stop) {
			chunkStop = stop
		}
		points, err := queryInfluxPathRange(path, chunkStart, chunkStop, "1m")
		if err != nil {
			return nil, err
		}
		out = append(out, points...)
	}
	return out, nil
}
