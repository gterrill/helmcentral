package main

import (
	"sort"
	"strings"
	"sync"
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
provider numbers and the host computes what is derived from them
(docs/developers/plugins.md).
*/

const derivedPathPrefix = "helmcentral."

// vesselFuelEconomyPath is distance per unit volume for the whole boat, in the
// same metres per cubic metre SignalK states per-engine economy in, so the
// existing fuelEconomy quantity converts it unchanged.
const vesselFuelEconomyPath = derivedPathPrefix + "propulsion.fuelEconomy"

/*
Fuel volume, time to empty and range at current burn (ADR 0084).

N2KView's main screen showed five tank quantities and a burn rate and left
range to the operator to work out in their head. These three paths do that
arithmetic host-side, the same way ADR 0055 derives fuel economy.

fuelVolumePath sums currentLevel × capacity over every tanks.fuel.<id> node
that publishes both; a tank with only a level (no known capacity) contributes
nothing, because multiplying a ratio by an unknown capacity is not a volume.
fuelTimeToEmptyPath and fuelRangeAtCurrentBurnPath both need volume and the
same total burn vesselFuelEconomy already sums, so they are absent under
exactly the same conditions that already make economy or volume absent:
stopped, not burning, or no tank contributing.
*/
const (
	fuelVolumePath             = derivedPathPrefix + "fuel.volume"
	fuelTimeToEmptyPath        = derivedPathPrefix + "fuel.timeToEmpty"
	fuelRangeAtCurrentBurnPath = derivedPathPrefix + "fuel.rangeAtCurrentBurn"
)

// derivedInputMaxAge is how old a derived fuel figure's oldest contributing
// input can be before the figure itself is reported as absent rather than
// merely aged (the frozen-burn-rate addendum to ADR 0084's plan). This is a
// stronger guarantee than the gauge-values staleness treatment (ADR 0083),
// which still shows the number with a badge: an alarm rule bound to one of
// these paths reads a derived value's Present field directly
// (derivedAwareAlarmReader), never seeing the UI's stale marker, so the value
// itself has to go absent or a rule could fire on arithmetic done against a
// source that stopped reporting a day ago. Must agree with the frontend's
// STALE_AFTER_SECONDS in lib/staleness.ts -- both exist to say "a SignalK
// source publishing every few seconds has been silent long enough that its
// last value is not a measurement of the present."
const derivedInputMaxAge = 120 * time.Second

// freshEnoughToPublish decides whether a derived figure's oldest
// contributing input is recent enough to publish a value at all. -1
// (unknown) counts as fresh: a source that has never carried a timestamp
// gives no evidence of staleness (ADR 0068), and treating "no evidence" as
// "too old" would blank a figure forever the first time it met an input with
// no timestamp of its own.
func freshEnoughToPublish(age float64) bool {
	return age < 0 || age <= derivedInputMaxAge.Seconds()
}

/*
Heavy-weather trends (ADR 0070, extended by ADR 0095).

These exist because both sources' advice is almost entirely about rates and
relationships, and an alarm rule compares one path against one number.
Deriving the figures here means the rule engine, its dwell and its
hysteresis all work unchanged.

pressureRate is the barometer's slope (Surviving the Storm's figure).
squashZoneIndex is the pattern that book says onboard instruments are worst
at spotting and that causes the most trouble: the wind climbing while the
barometer sits still. pressureChange12h, pressureChange24h, stormIndex and
severeThunderstormIndex are the Law of Storms ladder (R. J. Ellis,
worldstormcentral.co, rules-for-storms-and-gales page): the barometer's
tendency over a stated window, and two composite signatures built from it and
an absolute-pressure gate.
*/
const (
	pressureRatePath            = derivedPathPrefix + "environment.pressureRate"
	pressureChange3hPath        = derivedPathPrefix + "environment.pressureChange3h"
	squashZoneIndexPath         = derivedPathPrefix + "environment.squashZoneIndex"
	pressureChange12hPath       = derivedPathPrefix + "environment.pressureChange12h"
	pressureChange24hPath       = derivedPathPrefix + "environment.pressureChange24h"
	stormIndexPath              = derivedPathPrefix + "environment.stormIndex"
	severeThunderstormIndexPath = derivedPathPrefix + "environment.severeThunderstormIndex"
)

// SignalK paths these are derived from. Read through the snapshot rather than
// added to the vessel-state struct, since nothing else needs them there.
const (
	outsidePressurePath   = "environment.outside.pressure"
	windSpeedTruePath     = "environment.wind.speedTrue"
	windDirectionTruePath = "environment.wind.directionTrue"
)

/*
Forecast wind and surf warning level (plan: fold the forecast warning into
the alarm system; ADR 0087).

The forecast-warnings WASM plugin (ADR 0019) already tells the host which
official met-service bulletins are active for the vessel's own zone; nothing
publishes that onto a path an alarm rule can bind, which is the same gap ADR
0055 and ADR 0070 exist to close for fuel economy and the barometer. These
two paths are that: a ranked wind level (0 none, 1 strong wind / small
craft / watch, 2 gale, 3 storm / hurricane) and a surf flag, fed by a
background fetcher (forecast_warnings_fetcher.go) that polls independently of
the SignalK stream, on its own ten-minute cadence, since a marine warning
bulletin changes far slower than the boat's live data.

Never 0/false by default, the same way squashZoneIndex above is absent
rather than 0 with no barometer history: these two are absent (nil) until a
fetch has actually landed, and absent again once the last one is older than
forecastWarningsMaxAge, even though the age itself is still reported past
that point so a staleness rule has something to watch climb. 0 (no warning
currently in force) is a real fetched result and reads as present, exactly
like squashZoneIndex's own 0.
*/
const (
	forecastWindWarningLevelPath = derivedPathPrefix + "environment.forecastWindWarningLevel"
	forecastSurfWarningPath      = derivedPathPrefix + "environment.forecastSurfWarning"
)

var derivedPathIDs = []string{
	vesselFuelEconomyPath,
	pressureRatePath,
	pressureChange3hPath,
	squashZoneIndexPath,
	pressureChange12hPath,
	pressureChange24hPath,
	stormIndexPath,
	severeThunderstormIndexPath,
	fuelVolumePath,
	fuelTimeToEmptyPath,
	fuelRangeAtCurrentBurnPath,
	forecastWindWarningLevelPath,
	forecastSurfWarningPath,
}

// Units each derived path reports in, so the path picker can preselect a
// quantity the same way it does from SignalK's own meta.
//
// squashZoneIndex is unitless: it is 1 or 0, and a rule binds it with
// "above 0.5". An empty unit is the honest answer rather than inventing one.
var derivedPathUnits = map[string]string{
	vesselFuelEconomyPath:        "m/m3",
	pressureRatePath:             "Pa/s",
	pressureChange3hPath:         "Pa",
	squashZoneIndexPath:          "",
	pressureChange12hPath:        "Pa",
	pressureChange24hPath:        "Pa",
	stormIndexPath:               "",
	severeThunderstormIndexPath:  "",
	fuelVolumePath:               "m3",
	fuelTimeToEmptyPath:          "s",
	fuelRangeAtCurrentBurnPath:   "m",
	forecastWindWarningLevelPath: "",
	forecastSurfWarningPath:      "",
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
	value, _, ok := vesselFuelEconomyWithAge(globalSignalKSnapshot, read, ratePaths, time.Now().UTC())
	return value, ok
}

// vesselFuelEconomyWithAge is vesselFuelEconomy plus the oldest age among the
// inputs that actually contributed: the SOG sample and every burning
// engine's rate path (ADR 0083), each read through the same pathAge
// function buildGaugeValuesPayload uses, so this figure and a widget bound
// straight to one of its inputs can never disagree about that input's age. A
// figure computed from a frozen engine must carry that engine's age, however
// current SOG happens to be -- the case a 20-hour-old fuel rate produced a
// 0.02 nm/L Economy reading exists for.
//
// snapshot is taken explicitly, the same snapshot read builds its samples
// from, rather than assumed to be globalSignalKSnapshot: a test exercising
// this against a local snapshot must have pathAge resolve node timestamps
// from that same snapshot.
//
// Only computed alongside a real figure: an age for an undefined economy
// would say nothing an operator could act on.
func vesselFuelEconomyWithAge(snapshot *signalKSnapshot, read alarmReader, ratePaths []string, now time.Time) (value float64, age float64, ok bool) {
	const speedOverGroundPath = "navigation.speedOverGround"
	speed := read(speedOverGroundPath)
	if !speed.Present || speed.Value <= 0 {
		return 0, -1, false
	}

	total, burnAge, burning := totalFuelBurnWithAge(snapshot, read, ratePaths, now)
	if !burning {
		return 0, -1, false
	}

	speedAge := pathAge(snapshot, speed, speedOverGroundPath, now)
	return speed.Value / total, derivedInputAge(speedAge, burnAge), true
}

// totalFuelBurnWithAge sums every rate path that is currently reporting a
// burn above zero, and reports the oldest age among only the engines that
// actually contributed. Shared between vesselFuelEconomyWithAge and the fuel
// time-to-empty and range paths (ADR 0084), all three of which need "the
// boat's total current burn" as an input. An engine that is off contributes
// nothing to the total; it does not make the total unknowable.
func totalFuelBurnWithAge(snapshot *signalKSnapshot, read alarmReader, ratePaths []string, now time.Time) (total float64, age float64, ok bool) {
	var ages []float64
	for _, path := range ratePaths {
		rate := read(path)
		if !rate.Present || rate.Value <= 0 {
			continue
		}
		total += rate.Value
		ok = true
		ages = append(ages, pathAge(snapshot, rate, path, now))
	}
	if !ok || total <= 0 {
		return 0, -1, false
	}
	return total, derivedInputAge(ages...), true
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

// fuelTankVolumePathPair is the currentLevel and capacity path for one fuel
// tank, the two inputs fuelVolumeWithAge needs to turn that tank's ratio into
// a volume.
type fuelTankVolumePathPair struct {
	currentLevelPath string
	capacityPath     string
}

// collectFuelPaths finds every engine's burn path and every tanks.fuel.<id>
// node that publishes both currentLevel and capacity, in one pass over an
// already-collected path list -- a tank carrying only a level (tanks.fuel.0
// and .1 on the reference vessel report a ratio with no known size) is
// excluded rather than silently treated as a zero-sized tank.
//
// fuelRatePaths and fuelTankVolumePaths used to each call
// collectSignalKPaths(tree) themselves, so computeDerivedPaths walked the
// same already-copied tree twice for the same pass (backend-perf-audit.md
// Tier 1 #2). Both now delegate here over one collectSignalKPaths call.
func collectFuelPaths(entries []signalKPath) (ratePaths []string, tankPaths []fuelTankVolumePathPair) {
	levels := map[string]bool{}
	capacities := map[string]bool{}
	const tankPrefix = "tanks.fuel."

	for _, entry := range entries {
		switch {
		case strings.HasPrefix(entry.Path, "propulsion.") && strings.HasSuffix(entry.Path, ".fuel.rate"):
			ratePaths = append(ratePaths, entry.Path)
		case strings.HasPrefix(entry.Path, tankPrefix) && strings.HasSuffix(entry.Path, ".currentLevel"):
			levels[strings.TrimSuffix(strings.TrimPrefix(entry.Path, tankPrefix), ".currentLevel")] = true
		case strings.HasPrefix(entry.Path, tankPrefix) && strings.HasSuffix(entry.Path, ".capacity"):
			capacities[strings.TrimSuffix(strings.TrimPrefix(entry.Path, tankPrefix), ".capacity")] = true
		}
	}
	sort.Strings(ratePaths)

	var ids []string
	for id := range levels {
		if capacities[id] {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)

	tankPaths = make([]fuelTankVolumePathPair, 0, len(ids))
	for _, id := range ids {
		tankPaths = append(tankPaths, fuelTankVolumePathPair{
			currentLevelPath: tankPrefix + id + ".currentLevel",
			capacityPath:     tankPrefix + id + ".capacity",
		})
	}
	return ratePaths, tankPaths
}

// fuelRatePaths finds every engine's burn path in the snapshot, so a boat with
// one engine or three needs no configuration here.
func fuelRatePaths(tree map[string]any) []string {
	if tree == nil {
		return nil
	}
	rates, _ := collectFuelPaths(collectSignalKPaths(tree))
	return rates
}

// fuelTankVolumePaths finds every tanks.fuel.<id> node that publishes both
// currentLevel and capacity, so a tank carrying only a level -- tanks.fuel.0
// and .1 on the reference vessel, which report a ratio with no known size --
// is excluded rather than silently treated as a zero-sized tank.
func fuelTankVolumePaths(tree map[string]any) []fuelTankVolumePathPair {
	if tree == nil {
		return nil
	}
	_, tanks := collectFuelPaths(collectSignalKPaths(tree))
	return tanks
}

/*
fuelVolumeWithAge sums currentLevel × capacity over every tank pair that is
currently reporting both, so it survives a tank the snapshot has never seen a
level for as cleanly as it does one this boat never fitted. A tank at 0% with
a known capacity is a real "no fuel in this tank" reading and still
contributes -- 0 volume -- because that is what a tank with a known size
reports, not an absence.

Absent, not zero, whenever no tank contributes at all, per the same reasoning
ADR 0055 already applies to fuel economy.
*/
func fuelVolumeWithAge(snapshot *signalKSnapshot, read alarmReader, tankPaths []fuelTankVolumePathPair, now time.Time) (volume float64, age float64, ok bool) {
	var ages []float64
	for _, pair := range tankPaths {
		level := read(pair.currentLevelPath)
		capacity := read(pair.capacityPath)
		if !level.Present || !capacity.Present {
			continue
		}
		volume += level.Value * capacity.Value
		ok = true
		ages = append(ages, pathAge(snapshot, level, pair.currentLevelPath, now), pathAge(snapshot, capacity, pair.capacityPath, now))
	}
	if !ok {
		return 0, -1, false
	}
	return volume, derivedInputAge(ages...), true
}

// fuelTimeToEmptyWithAge is the volume aboard over the boat's total current
// burn. Absent whenever either input is: no tank contributing a volume, or
// nothing burning.
func fuelTimeToEmptyWithAge(volume, volumeAge float64, volumeOK bool, burn, burnAge float64, burnOK bool) (float64, float64, bool) {
	if !volumeOK || !burnOK || burn <= 0 {
		return 0, -1, false
	}
	return volume / burn, derivedInputAge(volumeAge, burnAge), true
}

// fuelRangeAtCurrentBurnWithAge is the volume aboard times the vessel's fuel
// economy (distance per unit volume), so it is absent under exactly the
// conditions that already make economy absent: stopped, not burning, or no
// volume to plan a range from.
func fuelRangeAtCurrentBurnWithAge(volume, volumeAge float64, volumeOK bool, economy, economyAge float64, economyOK bool) (float64, float64, bool) {
	if !volumeOK || !economyOK {
		return 0, -1, false
	}
	return volume * economy, derivedInputAge(volumeAge, economyAge), true
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
// buffers and the snapshot twice for the same numbers. It always resolves
// against globalSignalKSnapshot; computeDerivedPathsFromTree is the same
// pass over an already-fetched tree, for a caller that also needs that tree
// for something else.
func computeDerivedPaths(now time.Time) (map[string]*float64, map[string]float64) {
	return computeDerivedPathsFromTree(globalSignalKSnapshot, globalSignalKSnapshot.selfContext(), globalSignalKSnapshot.selfTree(), now)
}

// computeDerivedPathsFromTree does computeDerivedPaths' work against a tree
// and context the caller already has, rather than calling selfTree() a
// second time.
//
// Before this, computeDerivedPaths cost two whole-tree copies of its own on
// every call -- one to fetch tree, a second inside snapshotAlarmReader to
// build read -- and, called once per helmcentral.* alarm rule per tick
// (derivedAwareAlarmReader used to recompute this from scratch on every
// derived-path read) or once per request inside the old
// signalKPathsHandler's per-derived-path loop, that multiplied straight
// through (backend-perf-audit.md Tier 1 #2). derivedAwareAlarmReader now
// memoizes one call per reader instance, signalKPathsHandler calls this
// once per request, and this function itself now takes one tree and builds
// its reader from that same tree instead of fetching a second copy.
//
// fuelRatePaths and fuelTankVolumePaths also used to each walk the tree via
// their own collectSignalKPaths call; collectFuelPaths does both in one
// walk.
func computeDerivedPathsFromTree(snapshot *signalKSnapshot, context string, tree map[string]any, now time.Time) (map[string]*float64, map[string]float64) {
	values := map[string]*float64{
		vesselFuelEconomyPath:        nil,
		pressureRatePath:             nil,
		pressureChange3hPath:         nil,
		squashZoneIndexPath:          nil,
		pressureChange12hPath:        nil,
		pressureChange24hPath:        nil,
		stormIndexPath:               nil,
		severeThunderstormIndexPath:  nil,
		fuelVolumePath:               nil,
		fuelTimeToEmptyPath:          nil,
		fuelRangeAtCurrentBurnPath:   nil,
		forecastWindWarningLevelPath: nil,
		forecastSurfWarningPath:      nil,
	}
	ages := map[string]float64{
		vesselFuelEconomyPath:        -1,
		pressureRatePath:             -1,
		pressureChange3hPath:         -1,
		squashZoneIndexPath:          -1,
		pressureChange12hPath:        -1,
		pressureChange24hPath:        -1,
		stormIndexPath:               -1,
		severeThunderstormIndexPath:  -1,
		fuelVolumePath:               -1,
		fuelTimeToEmptyPath:          -1,
		fuelRangeAtCurrentBurnPath:   -1,
		forecastWindWarningLevelPath: -1,
		forecastSurfWarningPath:      -1,
	}

	// The weather trends come from ring buffers rather than the snapshot, so
	// they are computed whether or not the vessel tree has arrived yet.
	addWeatherTrendValues(values, ages, now)

	// Forecast warnings come from the background fetcher's own slot
	// (forecast_warnings_fetcher.go), not the SignalK tree either -- same
	// reasoning as the weather trends above, so it runs before the tree==nil
	// guard below.
	addForecastWarningValues(values, ages, now)

	if tree == nil {
		return values, ages
	}

	read := alarmReaderFromTree(snapshot, context, tree)
	ratePaths, tankPaths := collectFuelPaths(collectSignalKPaths(tree))

	// Every fuel figure below reports its true age in ages[] regardless of
	// freshness -- "the age still reports the oldest input so the reason is
	// visible" -- and is only promoted into values[] (published as a real
	// number) when that age clears derivedInputMaxAge. Below the guard the
	// figure stays absent rather than a rule silently evaluating arithmetic
	// done against a source that stopped reporting hours or days ago.
	economy, economyAge, economyOK := vesselFuelEconomyWithAge(snapshot, read, ratePaths, now)
	if economyOK {
		ages[vesselFuelEconomyPath] = economyAge
		if freshEnoughToPublish(economyAge) {
			values[vesselFuelEconomyPath] = &economy
		}
	}

	volume, volumeAge, volumeOK := fuelVolumeWithAge(snapshot, read, tankPaths, now)
	if volumeOK {
		ages[fuelVolumePath] = volumeAge
		if freshEnoughToPublish(volumeAge) {
			values[fuelVolumePath] = &volume
		}
	}

	burn, burnAge, burnOK := totalFuelBurnWithAge(snapshot, read, ratePaths, now)

	if timeToEmpty, ttAge, ttOK := fuelTimeToEmptyWithAge(volume, volumeAge, volumeOK, burn, burnAge, burnOK); ttOK {
		ages[fuelTimeToEmptyPath] = ttAge
		if freshEnoughToPublish(ttAge) {
			values[fuelTimeToEmptyPath] = &timeToEmpty
		}
	}

	if rangeM, rangeAge, rangeOK := fuelRangeAtCurrentBurnWithAge(volume, volumeAge, volumeOK, economy, economyAge, economyOK); rangeOK {
		ages[fuelRangeAtCurrentBurnPath] = rangeAge
		if freshEnoughToPublish(rangeAge) {
			values[fuelRangeAtCurrentBurnPath] = &rangeM
		}
	}

	return values, ages
}

// pointsAfter slices an already-chronological points scan down to the
// entries strictly after cutoff -- telemetryRingBuffer.since's own "after,
// not after-or-equal" contract -- via a binary search rather than another
// linear scan of the ring buffer itself. Used to turn one 24h
// barometerHistory scan into the 12h and 3h windows the Law of Storms ladder
// also needs (ADR 0095), instead of scanning the buffer three times for
// three different cutoffs.
func pointsAfter(points []telemetryPoint, cutoff time.Time) []telemetryPoint {
	i := sort.Search(len(points), func(i int) bool {
		return points[i].Timestamp.After(cutoff)
	})
	return points[i:]
}

// addWeatherTrendValues fills in the barometric and squash-zone paths, and
// their ages, from the recorded history.
//
// One barometerHistory.since scan covers the full 24h Law of Storms window;
// pointsAfter then slices that same scan down to the 12h and 3h windows
// rather than walking the ring buffer three separate times for three
// separate cutoffs.
//
// The squash-zone index is only reported once there is enough barometer
// history to say the barometer is genuinely steady. Without that, "the
// pressure is not moving" and "we have not been watching the pressure" are
// indistinguishable, and the second must not read as the first.
//
// Each value's age is the newest sample in its own ring-buffer window
// (newestSampleAge): a figure covers the whole window, but it is only as
// fresh as the newest point still arriving into it. squashZoneIndex depends
// on three buffers, so its age is the oldest of the three; the storm and
// severe-thunderstorm indices likewise take the oldest of the windows that
// feed them.
func addWeatherTrendValues(values map[string]*float64, ages map[string]float64, now time.Time) {
	cutoff3h := now.Add(-pressureTrendWindow)
	cutoff12h := now.Add(-pressureTendency12hWindow)

	history := barometerHistory.since(now.Add(-pressureTendency24hWindow))
	pressure := pointsAfter(history, cutoff3h)
	twelveHour := pointsAfter(history, cutoff12h)

	pressureAge := newestSampleAge(pressure, now)
	twelveHourAge := newestSampleAge(twelveHour, now)
	twentyFourHourAge := newestSampleAge(history, now)

	rate, rateOK := linearSlopePerSecond(pressure)
	if rateOK {
		values[pressureRatePath] = &rate
		ages[pressureRatePath] = pressureAge
	}

	// Gated on rateOK -- pressureRate's own trendMinimumSpan -- rather than
	// tendencyOverWindow's much stricter window-minus-slack gate below: three
	// hours has nothing shorter to fall back on while the buffer fills, and a
	// short-span change under-reports a monotonic fall, which is the safe
	// direction to be wrong in.
	change3h, change3hOK := changeOverWindow(pressure)
	change3hOK = change3hOK && rateOK
	if change3hOK {
		values[pressureChange3hPath] = &change3h
		ages[pressureChange3hPath] = pressureAge
	}

	change12h, change12hOK := tendencyOverWindow(twelveHour, pressureTendency12hWindow)
	if change12hOK {
		values[pressureChange12hPath] = &change12h
		ages[pressureChange12hPath] = twelveHourAge
	}

	if change24h, ok := tendencyOverWindow(history, pressureTendency24hWindow); ok {
		values[pressureChange24hPath] = &change24h
		ages[pressureChange24hPath] = twentyFourHourAge
	}

	if change3hOK {
		currentPressure := pressure[len(pressure)-1].Value

		stormIndex := stormSignature(change3h, currentPressure)
		values[stormIndexPath] = &stormIndex
		ages[stormIndexPath] = pressureAge

		if change12hOK {
			severeIndex := severeThunderstormSignature(change3h, change12h, currentPressure)
			values[severeThunderstormIndexPath] = &severeIndex
			ages[severeThunderstormIndexPath] = derivedInputAge(pressureAge, twelveHourAge)
		}
	}

	if !rateOK {
		return
	}
	windSpeed := trueWindSpeedHistory.since(cutoff3h)
	if _, ok := linearSlopePerSecond(windSpeed); !ok {
		return
	}
	windDirection := trueWindDirectionHistory.since(cutoff3h)

	squashIndex := squashZoneSignature(pressure, windSpeed, windDirection)
	values[squashZoneIndexPath] = &squashIndex
	ages[squashZoneIndexPath] = derivedInputAge(pressureAge, newestSampleAge(windSpeed, now), newestSampleAge(windDirection, now))
}

// addForecastWarningValues fills in the forecast-warnings derived paths from
// globalForecastWarningsSlot (forecast_warnings_fetcher.go), the background
// fetcher's own cross-goroutine state. It needs no SignalK tree at all -- the
// provider is polled independently of the delta stream -- so, like
// addWeatherTrendValues above, it runs unconditionally rather than waiting
// on the vessel tree to exist.
//
// Age is reported the moment a fetch has ever landed, and stays reported
// past forecastWarningsMaxAge even once the value itself goes absent: the
// same freshEnoughToPublish shape the fuel paths use. That is what gives the
// "Forecast warnings unavailable" rule (alarm_seed_forecast_warnings.go)
// something concrete to watch climb, rather than the path just vanishing
// with no trace of why it went quiet.
func addForecastWarningValues(values map[string]*float64, ages map[string]float64, now time.Time) {
	reading, ok := globalForecastWarningsSlot.get()
	if !ok {
		return
	}

	age := now.Sub(reading.FetchedAt).Seconds()
	ages[forecastWindWarningLevelPath] = age
	ages[forecastSurfWarningPath] = age

	if age > forecastWarningsMaxAge.Seconds() {
		return
	}

	windLevel := float64(reading.WindLevel)
	values[forecastWindWarningLevelPath] = &windLevel

	surf := 0.0
	if reading.Surf {
		surf = 1.0
	}
	values[forecastSurfWarningPath] = &surf
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

LastSeen used to be the SignalK stream's own last-message time for every
derived path, regardless of what the path was actually derived from. That
was close enough for the barometer paths, which genuinely do depend on data
riding that same stream, but it stopped being true the moment a path could be
computed from something else entirely: forecastWindWarningLevel is fed by a
background fetcher on its own ten-minute cadence
(forecast_warnings_fetcher.go), completely independent of whether SignalK
itself is talking. Stamping it with the stream's arrival time would let a
"Forecast warnings unavailable" rule read as fresh for as long as the boat's
other instruments kept reporting -- exactly the failure that rule exists to
catch. LastSeen is now each path's own reported age (ages[path] from
computeDerivedPaths, the same figure ADR 0083's gauge-values stream already
carries): the stream's last-message time is only a fallback for a path whose
age is genuinely unknown (-1), the same "no evidence of staleness" case
freshEnoughToPublish already treats as fresh.

computeDerivedPaths used to run again on every single derived-path call this
reader's closure received: an alarm tick with several helmcentral.* rules
enabled reran the whole pass -- two self-tree copies, the fuel-path tree
walks, the barometer ring scan -- once per rule (backend-perf-audit.md
Tier 1 #2). now is fixed once, at the moment this reader is built, rather
than re-read on every call; sync.Once then means the whole pass runs at most
once per reader instance, however many derived-path rules that instance's
caller reads through it, and never at all when it reads none (tracks.go's
own use of this reader, for three ordinary SignalK paths, costs nothing
extra from this).
*/
func derivedAwareAlarmReader(snapshot *signalKSnapshot) alarmReader {
	published := snapshotAlarmReader(snapshot)
	now := time.Now().UTC()

	var (
		once   sync.Once
		values map[string]*float64
		ages   map[string]float64
	)

	return func(path string) alarmSample {
		if !isDerivedPath(path) {
			return published(path)
		}

		once.Do(func() {
			values, ages = computeDerivedPaths(now)
		})

		value, ok := values[path]
		if !ok || value == nil {
			return alarmSample{}
		}

		_, lastSeen := snapshot.status()
		if age := ages[path]; age >= 0 {
			lastSeen = now.Add(-time.Duration(age * float64(time.Second)))
		}

		return alarmSample{Value: *value, Present: true, LastSeen: lastSeen}
	}
}
