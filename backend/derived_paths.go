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

/*
vesselFuelEconomy is speed over ground divided by the total burn of every
engine that is reporting one.

Reports absence rather than a number whenever the figure is not defined:
stopped, not burning, or nothing published. Zero would read as "this boat
covers no distance per litre", which is a measurement rather than the absence
of one, and an infinity would render as a plausible-looking enormous range.
*/
func vesselFuelEconomy(read alarmReader, ratePaths []string) (float64, bool) {
	speed := read("navigation.speedOverGround")
	if !speed.Present || speed.Value <= 0 {
		return 0, false
	}

	total := 0.0
	burning := false
	for _, path := range ratePaths {
		rate := read(path)
		if !rate.Present || rate.Value <= 0 {
			// An engine that is off contributes nothing; it does not make the
			// vessel's economy unknowable.
			continue
		}
		total += rate.Value
		burning = true
	}
	if !burning || total <= 0 {
		return 0, false
	}

	return speed.Value / total, true
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
	out := map[string]*float64{
		vesselFuelEconomyPath: nil,
		pressureRatePath:      nil,
		pressureChange3hPath:  nil,
		squashZoneIndexPath:   nil,
	}

	// The weather trends come from ring buffers rather than the snapshot, so
	// they are computed whether or not the vessel tree has arrived yet.
	addWeatherTrendValues(out, time.Now().UTC())

	tree := globalSignalKSnapshot.selfTree()
	if tree == nil {
		return out
	}

	read := snapshotAlarmReader(globalSignalKSnapshot)
	if economy, ok := vesselFuelEconomy(read, fuelRatePaths(tree)); ok {
		out[vesselFuelEconomyPath] = &economy
	}
	return out
}

// addWeatherTrendValues fills in the barometric and squash-zone paths from
// the recorded history.
//
// The squash-zone index is only reported once there is enough barometer
// history to say the barometer is genuinely steady. Without that, "the
// pressure is not moving" and "we have not been watching the pressure" are
// indistinguishable, and the second must not read as the first.
func addWeatherTrendValues(out map[string]*float64, now time.Time) {
	cutoff := now.Add(-pressureTrendWindow)
	pressure := barometerHistory.since(cutoff)

	if rate, ok := linearSlopePerSecond(pressure); ok {
		out[pressureRatePath] = &rate
	}
	if change, ok := changeOverWindow(pressure); ok {
		out[pressureChange3hPath] = &change
	}

	if _, ok := linearSlopePerSecond(pressure); !ok {
		return
	}
	windSpeed := trueWindSpeedHistory.since(cutoff)
	if _, ok := linearSlopePerSecond(windSpeed); !ok {
		return
	}

	index := squashZoneSignature(pressure, windSpeed, trueWindDirectionHistory.since(cutoff))
	out[squashZoneIndexPath] = &index
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
