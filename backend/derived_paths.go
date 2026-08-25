package main

import (
	"sort"
	"strings"
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
provider numbers and the host computes what is derived from them (docs/plugins.md).
*/

const derivedPathPrefix = "helmcentral."

// vesselFuelEconomyPath is distance per unit volume for the whole boat, in the
// same metres per cubic metre SignalK states per-engine economy in, so the
// existing fuelEconomy quantity converts it unchanged.
const vesselFuelEconomyPath = derivedPathPrefix + "propulsion.fuelEconomy"

var derivedPathIDs = []string{vesselFuelEconomyPath}

// Units each derived path reports in, so the path picker can preselect a
// quantity the same way it does from SignalK's own meta.
var derivedPathUnits = map[string]string{vesselFuelEconomyPath: "m/m3"}

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
	out := map[string]*float64{vesselFuelEconomyPath: nil}

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
