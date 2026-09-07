package main

import (
	"sort"
	"strings"
	"time"
)

/*
Values the host computes because nothing publishes them (ADR 0055).

Vessel fuel economy is the motivating case. SignalK's
propulsion.<id>.fuel.economy is per engine, so on a twin it reads roughly twice
the boat's actual distance per unit volume — range planned off one engine's
figure is out by however many engines are running.

Derived values are namespaced under a prefix of our own so they can never
shadow a path the vessel publishes, and they ride the existing gauge-values
stream, so every widget that can bind a path can bind one of these with no
widget code at all.

Host-side derivation is this repo's standing rule: a plugin returns raw
provider numbers and the host computes what is derived from them (docs/reference/plugins.md).
*/

const derivedPathPrefix = "helmcentral."

// vesselFuelEconomyPath is distance per unit volume for the whole boat, in the
// same metres per cubic metre SignalK states per-engine economy in, so the
// existing fuelEconomy quantity converts it unchanged.
const vesselFuelEconomyPath = derivedPathPrefix + "propulsion.fuelEconomy"

/*
Heavy-weather trends (ADR 0070).

These exist because Surviving the Storm's advice is almost entirely about
rates and relationships, and an alarm rule compares one path against one
number. Deriving the rate here means the rule engine, its dwell and its
hysteresis all work unchanged.

pressureRate is the barometer's slope, the figure the book treats as the real
warning. squashZoneIndex is the pattern it says onboard instruments are worst
at spotting and that causes the most trouble: the wind climbing while the
barometer sits still.
*/
const (
	pressureRatePath     = derivedPathPrefix + "environment.pressureRate"
	pressureChange3hPath = derivedPathPrefix + "environment.pressureChange3h"
	squashZoneIndexPath  = derivedPathPrefix + "environment.squashZoneIndex"
)

// SignalK paths these are derived from. Read through the snapshot rather than
// added to the vessel-state struct, since nothing else needs them there.
const (
	outsidePressurePath   = "environment.outside.pressure"
	windSpeedTruePath     = "environment.wind.speedTrue"
	windDirectionTruePath = "environment.wind.directionTrue"
)

var derivedPathIDs = []string{
	vesselFuelEconomyPath,
	pressureRatePath,
	pressureChange3hPath,
	squashZoneIndexPath,
}

// Units each derived path reports in, so the path picker can preselect a
// quantity the same way it does from SignalK's own meta.
//
// squashZoneIndex is unitless: it is 1 or 0, and a rule binds it with
// "above 0.5". An empty unit is the honest answer rather than inventing one.
var derivedPathUnits = map[string]string{
	vesselFuelEconomyPath: "m/m3",
	pressureRatePath:      "Pa/s",
	pressureChange3hPath:  "Pa",
	squashZoneIndexPath:   "",
}

func isDerivedPath(path string) bool {
	return strings.HasPrefix(path, derivedPathPrefix)
}

// unitForAlarmPath resolves the SI unit an alarm should report for a path,
// derived-aware the same way derivedAwareAlarmReader is: a helmcentral.* path
// reports the unit its own derivation carries (derivedPathUnits), everything
// else defers to whatever the vessel published under meta.units on the real
// data node. An unknown path, or one that has never carried meta, reports ""
// -- absence, not a guessed unit -- and the caller is expected to treat that
// as "omit", not "unitless".
func unitForAlarmPath(snapshot *signalKSnapshot, path string) string {
	if isDerivedPath(path) {
		return derivedPathUnits[path]
	}
	return unitsFor(snapshot.nodeAt(path))
}

/*
vesselFuelEconomy is speed over ground divided by the total burn of every
engine that is reporting one.

Reports absence rather than a number whenever the figure is not defined:
stopped, not burning, or nothing published. Zero would read as "this boat
covers no distance per litre", which is a measurement rather than the absence
of one, and an infinity would render as a plausible-looking enormous range.
*/
func vesselFuelEconomy(read alarmReader, ratePaths []string) (float64, bool) {
	value, _, ok := vesselFuelEconomyWithAge(read, ratePaths, time.Now().UTC())
	return value, ok
}

// vesselFuelEconomyWithAge is vesselFuelEconomy plus the oldest age among the
// inputs that actually contributed: the SOG sample and every burning
// engine's rate path (ADR 0083). A figure computed from a frozen engine must
// carry that engine's age, however current SOG happens to be -- the case a
// 20-hour-old fuel rate produced a 0.02 nm/L Economy reading exists for.
//
// Only computed alongside a real figure: an age for an undefined economy
// would say nothing an operator could act on.
func vesselFuelEconomyWithAge(read alarmReader, ratePaths []string, now time.Time) (value float64, age float64, ok bool) {
	speed := read("navigation.speedOverGround")
	if !speed.Present || speed.Value <= 0 {
		return 0, -1, false
	}

	total := 0.0
	burning := false
	ages := []float64{alarmSampleAge(speed, now)}
	for _, path := range ratePaths {
		rate := read(path)
		if !rate.Present || rate.Value <= 0 {
			// An engine that is off contributes nothing; it does not make the
			// vessel's economy unknowable.
			continue
		}
		total += rate.Value
		burning = true
		ages = append(ages, alarmSampleAge(rate, now))
	}
	if !burning || total <= 0 {
		return 0, -1, false
	}

	return speed.Value / total, derivedInputAge(ages...), true
}

// derivedInputAge reduces several ages (each -1 for unknown) down to the
// oldest of whichever are actually known -- freshestAge's counterpart, but
// picking the stalest input rather than the freshest. A derived value is
// only as fresh as its stalest contributing input. All-unknown inputs report
// -1: an unknown age is neither fresh nor stale.
func derivedInputAge(ages ...float64) float64 {
	result := -1.0
	for _, age := range ages {
		if age < 0 {
			continue
		}
		if age > result {
			result = age
		}
	}
	return result
}

// fuelRatePaths finds every engine's burn path in the snapshot, so a boat with
// one engine or three needs no configuration here.
func fuelRatePaths(tree map[string]any) []string {
	if tree == nil {
		return nil
	}

	var paths []string
	for _, entry := range collectSignalKPaths(tree) {
		if strings.HasPrefix(entry.Path, "propulsion.") && strings.HasSuffix(entry.Path, ".fuel.rate") {
			paths = append(paths, entry.Path)
		}
	}
	sort.Strings(paths)
	return paths
}

// derivedPathValues computes every derived path, absent ones included, so the
// stream can carry a null rather than dropping the key.
func derivedPathValues() map[string]*float64 {
	values, _ := computeDerivedPaths(time.Now().UTC())
	return values
}

// derivedPathAges reports how stale each derived path's inputs are (ADR
// 0083), so a figure computed from a frozen source -- the fuel-rate SignalK
// last updated twenty hours ago that produced a 0.02 nm/L Economy reading --
// carries that source's age rather than reading as current. -1 when the path
// has no defined value right now, or when nothing about its inputs is
// knowable.
func derivedPathAges(now time.Time) map[string]float64 {
	_, ages := computeDerivedPaths(now)
	return ages
}

// computeDerivedPaths is derivedPathValues and derivedPathAges in one pass,
// so a single build of the gauge-values payload does not walk the ring
// buffers and the snapshot twice for the same numbers.
func computeDerivedPaths(now time.Time) (map[string]*float64, map[string]float64) {
	values := map[string]*float64{
		vesselFuelEconomyPath: nil,
		pressureRatePath:      nil,
		pressureChange3hPath:  nil,
		squashZoneIndexPath:   nil,
	}
	ages := map[string]float64{
		vesselFuelEconomyPath: -1,
		pressureRatePath:      -1,
		pressureChange3hPath:  -1,
		squashZoneIndexPath:   -1,
	}

	// The weather trends come from ring buffers rather than the snapshot, so
	// they are computed whether or not the vessel tree has arrived yet.
	addWeatherTrendValues(values, ages, now)

	tree := globalSignalKSnapshot.selfTree()
	if tree == nil {
		return values, ages
	}

	read := snapshotAlarmReader(globalSignalKSnapshot)
	if economy, age, ok := vesselFuelEconomyWithAge(read, fuelRatePaths(tree), now); ok {
		values[vesselFuelEconomyPath] = &economy
		ages[vesselFuelEconomyPath] = age
	}
	return values, ages
}

// addWeatherTrendValues fills in the barometric and squash-zone paths, and
// their ages, from the recorded history.
//
// The squash-zone index is only reported once there is enough barometer
// history to say the barometer is genuinely steady. Without that, "the
// pressure is not moving" and "we have not been watching the pressure" are
// indistinguishable, and the second must not read as the first.
//
// Each value's age is the newest sample in its own ring-buffer window
// (newestSampleAge): the slope covers the whole window, but it is only as
// fresh as the newest point still arriving into it. squashZoneIndex depends
// on three buffers, so its age is the oldest of the three.
func addWeatherTrendValues(values map[string]*float64, ages map[string]float64, now time.Time) {
	cutoff := now.Add(-pressureTrendWindow)
	pressure := barometerHistory.since(cutoff)
	pressureAge := newestSampleAge(pressure, now)

	if rate, ok := linearSlopePerSecond(pressure); ok {
		values[pressureRatePath] = &rate
		ages[pressureRatePath] = pressureAge
	}
	if change, ok := changeOverWindow(pressure); ok {
		values[pressureChange3hPath] = &change
		ages[pressureChange3hPath] = pressureAge
	}

	if _, ok := linearSlopePerSecond(pressure); !ok {
		return
	}
	windSpeed := trueWindSpeedHistory.since(cutoff)
	if _, ok := linearSlopePerSecond(windSpeed); !ok {
		return
	}
	windDirection := trueWindDirectionHistory.since(cutoff)

	index := squashZoneSignature(pressure, windSpeed, windDirection)
	values[squashZoneIndexPath] = &index
	ages[squashZoneIndexPath] = derivedInputAge(pressureAge, newestSampleAge(windSpeed, now), newestSampleAge(windDirection, now))
}

/*
derivedAwareAlarmReader reads derived paths as well as published ones.

ADR 0055 said a derived value could be bound anywhere a path could and named
alarm rules among them, but the alarm loop read through snapshotAlarmReader,
which walks the SignalK tree alone. A rule naming helmcentral.* therefore saw
absence, and since absence never satisfies a threshold the rule silently never
fired. A rule that cannot fire is worse than one that does not exist, because
the operator believes something is watching.

Derived values keep their absence: a path that is computed but undefined right
now reports not-present, exactly as a missing SignalK path does, so a "below"
rule cannot fire on a number that does not exist.
*/
func derivedAwareAlarmReader(snapshot *signalKSnapshot) alarmReader {
	published := snapshotAlarmReader(snapshot)

	return func(path string) alarmSample {
		if !isDerivedPath(path) {
			return published(path)
		}

		value, ok := derivedPathValues()[path]
		if !ok || value == nil {
			return alarmSample{}
		}

		// Derived values are recomputed every tick from the current snapshot,
		// so they are exactly as fresh as the stream itself. Reporting the
		// snapshot's last message time lets a staleness rule on a derived path
		// mean what it does on a published one.
		_, lastMessage := snapshot.status()
		return alarmSample{Value: *value, Present: true, LastSeen: lastMessage}
	}
}
