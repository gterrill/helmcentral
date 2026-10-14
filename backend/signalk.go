package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// signalKSelfAPIPath is the v1 REST prefix every read and write of the
	// self vessel's data model goes through. Reads default to it through
	// SIGNALK_VESSEL_PATH; writes (czone switches, generatorPut, alarm
	// notifications) hard-code the same prefix, because a PUT to a path
	// outside it reaches no endpoint the server serves.
	signalKSelfAPIPath = "/signalk/v1/api/vessels/self"

	// defaultSignalKReadTimeoutMS bounds every read of SignalK state (vessel,
	// electrical, tanks, nearby vessels, self name, connection probe).
	// Overridable via SIGNALK_READ_TIMEOUT_MS - the boat's server is on the
	// LAN, but a deployment reaching it over a slow link may need more, and
	// tests probing a deliberately unroutable address want far less than a
	// 3s wall-clock wait per case.
	defaultSignalKReadTimeoutMS = 3000
)

// signalKReadTimeout resolves the SignalK read timeout from
// SIGNALK_READ_TIMEOUT_MS, falling back to defaultSignalKReadTimeoutMS on an
// unset or invalid value (logged loudly rather than refusing to talk to the
// vessel over a malformed env var). Mirrors wasmPluginTimeoutMS.
func signalKReadTimeout() time.Duration {
	raw := getEnv("SIGNALK_READ_TIMEOUT_MS", strconv.Itoa(defaultSignalKReadTimeoutMS))
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		log.Printf("signalk: invalid SIGNALK_READ_TIMEOUT_MS %q, falling back to %dms", raw, defaultSignalKReadTimeoutMS)
		return defaultSignalKReadTimeoutMS * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

// signalKReadClient is the HTTP client for every SignalK state read. Callers
// share one timeout so a single knob covers the whole read path; the client is
// built per call because the timeout is resolved at call time.
func signalKReadClient() *http.Client {
	return &http.Client{Timeout: signalKReadTimeout()}
}

// criticalVesselState marks state's position/GNSS fields as critical when
// there's no SignalK payload to validate at all — e.g. the connection itself
// failed. It freezes position at the last trusted fix (via resolveGNSSPosition)
// rather than leaving it at a meaningless sentinel, and reports
// gnss_critical_alert correctly so downstream consumers like the anchor watch
// alarm don't mistake "can't reach SignalK" for "vessel has moved."
func criticalVesselState(state vesselStateData, reason string) vesselStateData {
	validation := criticalGNSSValidation(reason, time.Now().UTC())
	state.Latitude, state.Longitude = resolveGNSSPosition(0, 0, false, validation)
	state.GNSSQualityIndicator = validation.QualityIndicator
	state.GNSSHDOP = validation.HDOP
	state.GNSSSatellites = validation.Satellites
	state.GNSSValidationState = validation.Status
	state.GNSSValidationReason = validation.Reason
	state.GNSSCriticalAlert = validation.Critical
	return state
}

func fetchSignalKVesselState() (vesselStateData, error) {
	state := vesselStateData{
		Status: "Unknown", Datetime: time.Now().UTC(), Depth: -1, LengthOverallM: -1, DraftM: -1, Latitude: -1, Longitude: -1,
		HeadingTrue: -1, SpeedOverGroundKts: -1, WindSpeedApparentKts: -1, WindAngleApparentDeg: -1, WindAngleRelativeDeg: -1,
		// True wind (ADR 0129 / ADR 0130) carries the same -1/"" absent
		// sentinels as its apparent counterpart above, and is never derived
		// from it — a boat with no true-wind source (no boat speed/heading
		// input to the wind instrument) must show '—', not the apparent
		// reading, in Current Conditions or in the Wind tile's True mode.
		WindSpeedTrueKts: -1, WindAngleTrueDeg: -1, WindAngleTrueRelativeDeg: -1, WindDirectionTrueDeg: -1,
		// Every last_update_age_s field defaults to -1 (unknown), not 0: a
		// return before the payload is even parsed (the stream having no
		// self tree yet) must never read as "just measured".
		DepthLastUpdateAge: -1, PositionLastUpdateAge: -1, WindLastUpdateAge: -1,
	}

	// A stream outage lands here the same way an unreachable REST server does,
	// so the position freezes at the last trusted fix rather than reading as a
	// jump to null island and tripping the anchor alarm.
	t := globalSignalKSnapshot.selfTelemetry()
	if !t.HasSelf() {
		err := errSignalKNoSelf
		return criticalVesselState(state, err.Error()), err
	}
	return vesselStateFromTelemetry(t, state), nil
}

// vesselStateFromTelemetry fills state from our own boat's telemetry. The
// -1 and "" sentinels vesselStateData still carries to the browser are
// produced here, at the payload edge, from ok and AgeKnown (ADR 0178).
func vesselStateFromTelemetry(t telemetryReader, state vesselStateData) vesselStateData {
	state.Name = strings.TrimSpace(firstString(t, "name", "design.name"))

	// ADR 0047: the Rode Planner's swing radius needs the boat's LOA. The
	// full tree nests it inside design.length's value; some sources publish
	// the bare path instead - the reader resolves both.
	if v, ok := t.Number("design.length.overall"); ok {
		state.LengthOverallM = v.Value
	}

	// ADR 0135: the Anchor Watch low-water clearance warning needs the
	// boat's draft. Only design.draft.maximum is read - the live server
	// this feature was verified against never publishes
	// current/minimum/canoe, and maximum is the only figure that is always
	// the worst case, so there is no fallback to those.
	if v, ok := t.Number("design.draft.maximum"); ok {
		state.DraftM = v.Value
	}

	state.Status = firstString(t, "navigation.state")
	if state.Status == "" {
		state.Status = "Unknown"
	}

	hasGNSSDatetime := false
	datetimeString := firstString(t, "navigation.datetime", "timestamp")
	if datetimeString != "" {
		parsed, err := time.Parse(time.RFC3339, datetimeString)
		if err == nil {
			state.Datetime = parsed.UTC()
			hasGNSSDatetime = true
		}
	}

	if v, ok := t.Number("environment.depth.belowTransducer"); ok {
		state.Depth = v.Value
	}
	// Scoped to belowTransducer alone, not the whole environment.depth
	// branch: a live belowSurface/belowKeel sensor must never paper over a
	// dead belowTransducer reading, which is the value actually rendered.
	state.DepthLastUpdateAge = subtreeAgeSeconds(t, "environment.depth.belowTransducer", state.Datetime)

	currentDriftKts, currentSetDeg, currentDriftImpactKts := parseSignalKCurrent(t)
	state.CurrentDriftKts = currentDriftKts
	state.CurrentSetDeg = currentSetDeg
	state.CurrentDriftImpactKts = currentDriftImpactKts

	position, hasPosition := t.Position()

	state.Engine0RPM = readEngineRPM(t, []string{"0", "port", "main", "engine0", "engine-0"})
	state.Engine1RPM = readEngineRPM(t, []string{"1", "starboard", "secondary", "engine1", "engine-1"})
	validation := parseGNSSPositionValidation(t)
	validation = applyGNSSHeuristics(validation, gnssObservedSample{
		HasPosition:    hasPosition,
		Latitude:       position.Latitude,
		Longitude:      position.Longitude,
		DepthMeters:    state.Depth,
		Navigation:     state.Status,
		EnginesRunning: (state.Engine0RPM > 0 && !math.IsInf(state.Engine0RPM, 0)) || (state.Engine1RPM > 0 && !math.IsInf(state.Engine1RPM, 0)),
		ObservedAt:     state.Datetime,
		HasObservedAt:  hasGNSSDatetime,
	}, time.Now().UTC())
	state.GNSSQualityIndicator = validation.QualityIndicator
	state.GNSSHDOP = validation.HDOP
	state.GNSSSatellites = validation.Satellites
	state.GNSSValidationState = validation.Status
	state.GNSSValidationReason = validation.Reason
	state.GNSSCriticalAlert = validation.Critical

	// -1,-1 reaches the wire only from the gate, when no fix has ever been
	// trusted.
	state.Latitude, state.Longitude = resolveGNSSPosition(position.Latitude, position.Longitude, hasPosition, validation)

	// The fix (navigation.position) and its quality figures
	// (navigation.gnss) are published under two different parents; either
	// one still ticking means the fix itself is current, so this is the
	// freshest of the two rather than a single subtree walk.
	state.PositionLastUpdateAge = freshestAge(
		subtreeAgeSeconds(t, "navigation.position", state.Datetime),
		subtreeAgeSeconds(t, "navigation.gnss", state.Datetime),
	)

	if v, ok := t.Number("navigation.headingTrue"); ok {
		state.HeadingTrue = v.Value
	}
	if state.HeadingTrue >= 0 {
		if state.HeadingTrue <= 2*math.Pi {
			state.HeadingTrue = state.HeadingTrue * 180 / math.Pi
		}
		state.HeadingTrue = normalizeDegrees(state.HeadingTrue)
	}

	windSpeedApparent, windSpeedApparentOK := t.Number("environment.wind.speedApparent")
	windTimestamp := firstNonEmptyString(
		declaredTimestamp(t, "environment.wind.speedApparent"),
		declaredTimestamp(t, "environment.wind.angleApparent"),
		declaredTimestamp(t, "environment.wind"),
	)

	// Scoped to environment.wind only: environment.current is a different
	// sensor with its own health and must not be folded into wind's own
	// freshness. Whole-subtree walk (not windTimestamp's first-found
	// priority list above) so the tile's age tracks whichever of
	// speedApparent/angleApparent actually updated most recently.
	state.WindLastUpdateAge = subtreeAgeSeconds(t, "environment.wind", state.Datetime)

	windDataRecent := isRecentTimestamp(windTimestamp, defaultWindMaxAge)
	if !windDataRecent {
		windDataRecent = state.Datetime.After(time.Now().UTC().Add(-defaultWindMaxAge))
	}

	if windSpeedApparentOK && windSpeedApparent.Value >= 0 && windDataRecent {
		state.WindSpeedApparentKts = windSpeedApparent.Value * metersPerSecondToKnots
	} else {
		state.WindSpeedApparentKts = 0
	}

	if windAngle, ok := t.Number("environment.wind.angleApparent"); ok && windDataRecent {
		windAngleApparent := windAngle.Value
		if windAngleApparent >= -2*math.Pi && windAngleApparent <= 2*math.Pi {
			windAngleApparent = windAngleApparent * 180 / math.Pi
		}

		signedAngle := normalizeSignedDegrees(windAngleApparent)
		state.WindAngleApparentDeg = normalizeDegrees(signedAngle)
		state.WindAngleRelativeDeg = math.Abs(signedAngle)
		if signedAngle < 0 {
			state.WindSide = "port"
		} else {
			state.WindSide = "starboard"
		}
	} else {
		state.WindAngleApparentDeg = 0
		state.WindAngleRelativeDeg = 0
		state.WindSide = "starboard"
	}

	// True wind: environment.wind.speedTrue/angleTrueWater/directionTrue,
	// parsed the same way as apparent above (radian conversion, side from
	// sign) but kept entirely separate, including its own recency check per
	// leaf — a stale or absent true-wind source must read as unknown
	// (-1/"") rather than silently reusing apparent's numbers, which is a
	// different sensor reading a different quantity. Feeds the Current
	// Conditions tile (ADR 0129: speedTrue/directionTrue) and the Wind
	// tile's True mode (ADR 0130: adds angleTrueWater).
	//
	// Each of the three leaves is gated on its OWN timestamp
	// (speedTrue/angleTrueWater/directionTrue independently), not a
	// combined one and not the generic environment.wind.timestamp
	// apparent's own gate also consults — that timestamp, or one sibling
	// leaf alone updating, can look "recent" while a different leaf's own
	// source has actually gone quiet (e.g. angleTrueWater still ticking
	// while speedTrue's derived-data calculation has stalled), which would
	// wrongly revive the stalled leaf. There is deliberately no
	// state.Datetime (GNSS fix time) fallback either, unlike apparent's
	// gate: a live GPS fix says nothing about whether the true-wind
	// instrument itself is still reporting, so a stale or entirely absent
	// timestamp on a given leaf leaves only that leaf's own field(s) at
	// their sentinel.
	windSpeedTrueRecent := isRecentTimestamp(declaredTimestamp(t, "environment.wind.speedTrue"), defaultWindMaxAge)
	if v, ok := t.Number("environment.wind.speedTrue"); ok && v.Value >= 0 && windSpeedTrueRecent {
		state.WindSpeedTrueKts = v.Value * metersPerSecondToKnots
	} else {
		state.WindSpeedTrueKts = -1
	}

	windAngleTrueRecent := isRecentTimestamp(declaredTimestamp(t, "environment.wind.angleTrueWater"), defaultWindMaxAge)
	if v, ok := t.Number("environment.wind.angleTrueWater"); ok && windAngleTrueRecent {
		windAngleTrue := v.Value
		if windAngleTrue >= -2*math.Pi && windAngleTrue <= 2*math.Pi {
			windAngleTrue = windAngleTrue * 180 / math.Pi
		}

		signedAngleTrue := normalizeSignedDegrees(windAngleTrue)
		state.WindAngleTrueDeg = normalizeDegrees(signedAngleTrue)
		state.WindAngleTrueRelativeDeg = math.Abs(signedAngleTrue)
		if signedAngleTrue < 0 {
			state.WindSideTrue = "port"
		} else {
			state.WindSideTrue = "starboard"
		}
	}
	// No else: WindAngleTrueDeg/WindAngleTrueRelativeDeg stay at their -1
	// sentinel and WindSideTrue stays "" (absent), rather than apparent's
	// arbitrary starboard/0 default - true wind angle needs boat speed and
	// heading behind it, so "the instrument has nothing yet" is common and
	// must read as unknown, not as "dead ahead."

	// environment.wind.directionTrue is the compass bearing the wind comes
	// FROM (0..2π already relative to true north, not bow-relative), so it
	// normalizes straight to 0-360 with no signed/side step.
	windDirectionTrueRecent := isRecentTimestamp(declaredTimestamp(t, "environment.wind.directionTrue"), defaultWindMaxAge)
	if v, ok := t.Number("environment.wind.directionTrue"); ok && windDirectionTrueRecent {
		windDirectionTrue := v.Value
		if windDirectionTrue >= -2*math.Pi && windDirectionTrue <= 2*math.Pi {
			windDirectionTrue = windDirectionTrue * 180 / math.Pi
		}
		state.WindDirectionTrueDeg = normalizeDegrees(windDirectionTrue)
	} else {
		state.WindDirectionTrueDeg = -1
	}

	if v, ok := t.Number("navigation.speedOverGround"); ok && v.Value >= 0 {
		state.SpeedOverGroundKts = math.Round(v.Value*metersPerSecondToKnots*10) / 10
	}

	// Generator state (com.victronenergy.generator.0 via signalk-venus-plugin)
	state.GeneratorState = firstString(t, "electrical.generator.0.state")

	if v, ok := t.Bool("electrical.generator.0.manualStart"); ok {
		state.GeneratorManualStart = v
	}

	if v, ok := t.Number("electrical.generator.0.manualStartTimer"); ok && v.Value >= 0 {
		state.GeneratorManualStartTimer = v.Value
	}

	state.GeneratorRunningByCondition = firstString(t, "electrical.generator.0.runningByCondition")

	if v, ok := t.Number("electrical.generator.0.runtime"); ok && v.Value >= 0 {
		state.GeneratorRuntime = v.Value
	}

	return state
}

func readEngineRPM(t telemetryReader, aliases []string) float64 {
	for _, alias := range aliases {
		prefix := "propulsion." + alias
		ts := firstNonEmptyString(
			declaredTimestamp(t, prefix+".revolutions"),
			declaredTimestamp(t, prefix+".rpm"),
			declaredTimestamp(t, prefix),
		)
		if ts != "" && !isRecentTimestamp(ts, defaultRPMMaxAge) {
			return -1
		}

		if v, ok := t.Number(prefix + ".revolutions"); ok && v.Value >= 0 {
			return roundTo1(v.Value * 60)
		}

		if v, ok := t.Number(prefix + ".rpm"); ok && v.Value >= 0 {
			return roundTo1(v.Value)
		}
	}

	return -1
}

func parseSignalKCurrent(t telemetryReader) (float64, float64, *float64) {
	const current = "environment.current"

	drift := -1.0
	if v, ok := firstNumber(t, current+".drift", current+".drift.speed"); ok {
		drift = v
		if drift >= 0 && drift <= 20 {
			drift = math.Round((drift*metersPerSecondToKnots)*10) / 10
		} else if drift < 0 {
			drift = -1
		}
	}

	// setTrue is the official SignalK path (environment.current has no plain
	// "set" in the spec). setMagnetic is deliberately NOT read as a stand-in
	// for setTrue: converting it would need a magnetic variation reading
	// this codebase doesn't have, and displaying an unconverted magnetic
	// bearing as true would silently misreport direction.
	// A drift published as an object leaf ({"speed", "direction"}) carries
	// the set as its direction, read the same way as a separate leaf.
	setDeg := -1.0
	if v, ok := firstNumber(t, current+".setTrue", current+".drift.direction", current+".drift.angle"); ok {
		if v >= -2*math.Pi && v <= 2*math.Pi {
			setDeg = normalizeDegrees(v * 180 / math.Pi)
		} else {
			setDeg = normalizeDegrees(v)
		}
	}

	var driftImpactKts *float64
	if sample := t.Sample(current + ".driftImpact"); sample.Present {
		if value, ok := sample.Value.(float64); ok {
			knots := roundTo1(value * metersPerSecondToKnots)
			driftImpactKts = &knots
		}
	}

	return drift, setDeg, driftImpactKts
}

func fetchSignalKElectricalState() (electricalStateData, error) {
	state := electricalStateData{Datetime: time.Now().UTC(), LastUpdateAge: -1, BatterySocPercent: -1, BatteryCapacityAh: -1, SolarOutputW: -1, ACOutputW: -1, DC12VPowerW: -1, DC12VCurrentA: -1, DC24VVoltageV: -1, ACLoadsW: -1, Charger0: chargerInstanceData{CurrentA: -1, ACIn1CurrentA: -1}}

	t := globalSignalKSnapshot.selfTelemetry()
	if !t.HasSelf() {
		return state, errSignalKNoSelf
	}
	return electricalStateFromTelemetry(t, state), nil
}

// electricalStateFromTelemetry fills the Battery & Power tile's state. Its
// unsigned fields keep -1 for unknown on the wire; the signed bank current
// and power are nil (ADR 0178).
func electricalStateFromTelemetry(t telemetryReader, state electricalStateData) electricalStateData {
	datetimeString := firstString(t, "timestamp", "navigation.datetime")
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			state.Datetime = parsed.UTC()
		}
	}

	// The tile's values come from paths scattered across the electrical
	// subtree, so its freshness is that of the most recent of them. A feed that
	// has stopped freezes every one of these at once, which is what makes a
	// single subtree-wide age meaningful here.
	state.LastUpdateAge = subtreeAgeSeconds(t, "electrical", state.Datetime)

	// Identify the main house battery before reading any values from it.
	mainBattery, haveMain := lookupMainBattery(t)

	soc, socOK := 0.0, false
	if haveMain {
		soc, socOK = leafNumber(t, mainBattery+".capacity.stateOfCharge")
	}
	if !socOK {
		soc, socOK = firstNumber(t,
			"electrical.batteries.house.capacity.stateOfCharge",
			"electrical.batteries.service.capacity.stateOfCharge",
		)
	}
	if !socOK {
		soc, socOK = leafNumber(t, "electrical.chargers.276.capacity.stateOfCharge")
	}
	if !socOK {
		soc, socOK = leafNumberFromAnyChild(t, "electrical.chargers", "capacity.stateOfCharge")
	}
	if socOK && soc >= 0 {
		if soc <= 1 {
			soc *= 100
		}
		state.BatterySocPercent = math.Max(0, math.Min(100, roundTo1(soc)))
	}

	batteryCapacityAh, capacityOK := firstNumber(t,
		"electrical.batteries.house.capacity.nominal",
		"electrical.batteries.service.capacity.nominal",
		"electrical.batteries.house.capacity.total",
		"electrical.batteries.service.capacity.total",
	)
	if !capacityOK {
		batteryCapacityAh, capacityOK = leafNumberFromAnyChild(t, "electrical.batteries", "capacity.nominal")
	}
	if !capacityOK {
		batteryCapacityAh, capacityOK = leafNumberFromAnyChild(t, "electrical.batteries", "capacity.total")
	}
	if !capacityOK {
		batteryCapacityAh = loadHouseBatteryCapacityAh(getEnv("SETTINGS_FILE", "../settings.yaml"))
	}
	if batteryCapacityAh > 0 {
		state.BatteryCapacityAh = roundTo1(batteryCapacityAh)
	}

	// Identify the main house battery: the one with valid SOC and highest absolute current.
	batteryVoltage, voltageOK := 0.0, false
	if haveMain {
		batteryVoltage, voltageOK = leafNumber(t, mainBattery+".voltage")
	}
	if !voltageOK {
		batteryVoltage, voltageOK = firstNumber(t,
			"electrical.venus.batteryVoltage",
			"electrical.batteries.house.voltage",
			"electrical.batteries.service.voltage",
		)
	}
	if !voltageOK {
		batteryVoltage, voltageOK = leafNumberFromAnyChild(t, "electrical.batteries", "voltage")
	}
	if !voltageOK {
		batteryVoltage = -1
	}

	// Bank current and power are signed, so no number can stand for "unknown":
	// they are left nil until a path actually reports one.
	var current, power *float64
	if haveMain {
		if v, ok := leafNumber(t, mainBattery+".current"); ok {
			current = &v
		}
	}
	if current == nil {
		if v, ok := firstNumber(t, "electrical.batteries.house.current", "electrical.batteries.service.current"); ok {
			current = &v
		}
	}
	if current != nil {
		v := roundTo1(*current)
		state.ChargingCurrentA = &v
	}

	if haveMain {
		if v, ok := leafNumber(t, mainBattery+".power"); ok {
			power = &v
		}
	}
	if power == nil {
		if v, ok := firstNumber(t, "electrical.batteries.house.power", "electrical.batteries.service.power"); ok {
			power = &v
		}
	}
	if power != nil {
		v := roundTo1(*power)
		state.ChargingPowerW = &v
	} else if state.ChargingCurrentA != nil && batteryVoltage > 0 {
		v := roundTo1(*state.ChargingCurrentA * batteryVoltage)
		state.ChargingPowerW = &v
	}
	if state.ChargingCurrentA == nil && state.ChargingPowerW != nil && batteryVoltage > 0 {
		v := roundTo1(*state.ChargingPowerW / batteryVoltage)
		state.ChargingCurrentA = &v
	}

	// venus.totalPanelPower is the Victron system aggregate; sum individual chargers as fallback.
	solar, solarOK := t.Number("electrical.venus.totalPanelPower")
	if solarOK && solar.Value >= 0 {
		state.SolarOutputW = roundTo1(solar.Value)
	} else if !solarOK {
		if sum, ok := sumLeafNumberOverChildren(t, "electrical.solar", "panelPower"); ok && sum >= 0 {
			state.SolarOutputW = roundTo1(sum)
		}
	}

	acOutput, acOutputOK := firstNumber(t,
		"electrical.inverters.0.ac.output.power",
		"electrical.inverters.0.acout.power",
		"electrical.inverters.0.acOutputPower",
		"electrical.alternators.0.ac.output.power",
	)
	if !acOutputOK {
		acOutput, acOutputOK = leafNumberFromAnyChild(t, "electrical.inverters", "acout.power")
	}
	if acOutputOK && acOutput >= 0 {
		state.ACOutputW = roundTo1(acOutput)
	}

	if dc12Power, ok := firstNumber(t, "electrical.venus.dcPower", "electrical.dc.12v.power", "electrical.loads.12v.power"); ok && dc12Power >= 0 {
		state.DC12VPowerW = roundTo1(dc12Power)
	}

	if dc12Current, ok := firstNumber(t, "electrical.venus.dcCurrent", "electrical.dc.12v.current", "electrical.loads.12v.current"); ok && dc12Current >= 0 {
		state.DC12VCurrentA = roundTo1(dc12Current)
	} else if state.DC12VPowerW >= 0 && batteryVoltage > 0 {
		state.DC12VCurrentA = roundTo1(state.DC12VPowerW / batteryVoltage)
	}

	if dc24Voltage, ok := firstNumber(t, "electrical.dc.24v.voltage", "electrical.batteries.starter.voltage"); ok && dc24Voltage >= 0 {
		state.DC24VVoltageV = roundTo1(dc24Voltage)
	} else if batteryVoltage >= 0 {
		state.DC24VVoltageV = roundTo1(batteryVoltage)
	}

	if acLoads, ok := firstNumber(t, "electrical.ac.loads.total.power", "electrical.ac.loads.power"); ok && acLoads >= 0 {
		state.ACLoadsW = roundTo1(acLoads)
	} else if state.ACOutputW >= 0 {
		state.ACLoadsW = state.ACOutputW
	}

	if generatorPower, ok := firstLeafNumber(t,
		"electrical.ac.1.phase.A.realPower",
		"electrical.ac.1.phaseA.realPower",
		"electrical.ac.1.realPower",
	); ok && generatorPower >= 0 {
		state.GeneratorRealPowerW = roundTo1(generatorPower)
	}

	// Alternators — read port (index 0) and starboard (index 1) separately.
	state.Alternator0 = readAlternatorInstance(t, "0")
	state.Alternator1 = readAlternatorInstance(t, "1")
	state.Charger0 = readChargerInstance(t, "0")

	return state
}

func fetchSignalKSolarState() (solarStateData, error) {
	state := solarStateData{
		Datetime:      time.Now().UTC(),
		CurrentW:      -1,
		TodayKWh:      -1,
		YesterdayKWh:  -1,
		PeakTodayW:    -1,
		LastUpdateAge: -1,
		Controllers:   []solarControllerData{},
		Trend24hTotal: []solarTrendPoint{},
	}

	t := globalSignalKSnapshot.selfTelemetry()
	if !t.HasSelf() {
		return state, errSignalKNoSelf
	}
	return solarStateFromTelemetry(t, state), nil
}

func solarStateFromTelemetry(t telemetryReader, state solarStateData) solarStateData {
	datetimeString := firstString(t, "timestamp", "navigation.datetime")
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			state.Datetime = parsed.UTC()
		}
	}

	if ids := t.Children("electrical.solar"); len(ids) > 0 {
		sumCurrentW := 0.0
		foundCurrent := false
		sumTodayKWh := 0.0
		foundToday := false
		sumYesterdayKWh := 0.0
		foundYesterday := false

		for i, id := range ids {
			controller := readSolarController(t, "electrical.solar."+id, id, i, state.Datetime)
			state.Controllers = append(state.Controllers, controller)

			if controller.CurrentW >= 0 {
				sumCurrentW += controller.CurrentW
				foundCurrent = true
			}
			if controller.TodayKWh >= 0 {
				sumTodayKWh += controller.TodayKWh
				foundToday = true
			}
			if controller.YesterdayKWh >= 0 {
				sumYesterdayKWh += controller.YesterdayKWh
				foundYesterday = true
			}
			// A source is only as stale as its most recent update, so the
			// state-level age tracks the freshest controller rather than the
			// oldest. Controllers reporting no timestamp (-1) cannot vouch for
			// freshness and are skipped.
			if controller.LastUpdateAge >= 0 {
				if state.LastUpdateAge < 0 || controller.LastUpdateAge < state.LastUpdateAge {
					state.LastUpdateAge = controller.LastUpdateAge
				}
			}
		}

		if foundCurrent {
			state.CurrentW = roundTo1(sumCurrentW)
		}
		if foundToday {
			state.TodayKWh = roundTo3(sumTodayKWh)
		}
		if foundYesterday {
			state.YesterdayKWh = roundTo3(sumYesterdayKWh)
		}
	}

	if v, ok := t.Number("electrical.venus.totalPanelPower"); ok && v.Value >= 0 {
		state.CurrentW = roundTo1(v.Value)
	}

	if v, ok := t.Number("electrical.venus.yieldToday"); ok {
		if kwh := joulesToKWh(v.Value); kwh >= 0 {
			state.TodayKWh = roundTo3(kwh)
		}
	}

	if v, ok := t.Number("electrical.venus.yieldYesterday"); ok {
		if kwh := joulesToKWh(v.Value); kwh >= 0 {
			state.YesterdayKWh = roundTo3(kwh)
		}
	}

	if v, ok := t.Number("electrical.venus.maxPanelPowerToday"); ok && v.Value >= 0 {
		state.PeakTodayW = roundTo1(v.Value)
	}

	if state.CurrentW > 0 {
		for i := range state.Controllers {
			if state.Controllers[i].CurrentW >= 0 {
				state.Controllers[i].Contribution = roundTo1((state.Controllers[i].CurrentW / state.CurrentW) * 100)
			}
		}
	}

	return state
}

func readSolarController(t telemetryReader, prefix string, id string, index int, sampleTime time.Time) solarControllerData {
	controller := solarControllerData{
		ID:            id,
		Label:         defaultSolarControllerLabel(index, id),
		CurrentW:      -1,
		TodayKWh:      -1,
		YesterdayKWh:  -1,
		LastUpdateAge: -1,
		Contribution:  -1,
	}

	if powerW, ok := firstNumber(t, prefix+".panelPower", prefix+".power"); ok && powerW >= 0 {
		controller.CurrentW = roundTo1(powerW)
	}

	if v, ok := t.Number(prefix + ".yieldToday"); ok {
		if kwh := joulesToKWh(v.Value); kwh >= 0 {
			controller.TodayKWh = roundTo3(kwh)
		}
	}

	if v, ok := t.Number(prefix + ".yieldYesterday"); ok {
		if kwh := joulesToKWh(v.Value); kwh >= 0 {
			controller.YesterdayKWh = roundTo3(kwh)
		}
	}

	controller.Mode = strings.TrimSpace(firstString(t, prefix+".chargingMode", prefix+".state", prefix+".mode"))

	controller.Error = strings.TrimSpace(firstString(t, prefix+".error", prefix+".alarm"))

	timestamp := firstNonEmptyString(
		declaredTimestamp(t, prefix+".panelPower"),
		declaredTimestamp(t, prefix+".yieldToday"),
		declaredTimestamp(t, prefix),
	)
	if timestamp != "" {
		parsed, err := time.Parse(time.RFC3339, timestamp)
		if err == nil {
			age := sampleTime.Sub(parsed.UTC()).Seconds()
			if age >= 0 {
				controller.LastUpdateAge = roundTo1(age)
			}
		}
	}

	return controller
}

func defaultSolarControllerLabel(index int, id string) string {
	switch index {
	case 0:
		return "Port"
	case 1:
		return "Starboard"
	case 2:
		return "Salon"
	default:
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			return fmt.Sprintf("Array %d", index+1)
		}
		return "Array " + strings.ToUpper(trimmed)
	}
}

// joulesToKWh converts a raw electrical.solar.<id>.yieldToday/yieldYesterday
// (and the equivalent electrical.venus.yieldToday/yieldYesterday) value from
// joules to kWh (1 kWh = 3,600,000 J). Confirmed live against this fleet's
// SignalK server, 2026-09-28: meta units "J", e.g. yieldToday 35999.99J and
// yieldYesterday 9648000.24J.
//
// Replaces the old magnitude-guessing heuristic (raw > 200 meant Wh, so
// divide by 1000), which was wrong for every value this fleet actually
// reports: 35999.99J read as if it were 35999.99Wh (36 kWh, not the true
// ~0.01 kWh), and 9648000.24J read as ~9648 kWh (not the true ~2.68 kWh).
func joulesToKWh(raw float64) float64 {
	if raw < 0 {
		return -1
	}
	return raw / 3_600_000
}

func roundTo3(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func readAlternatorInstance(t telemetryReader, index string) alternatorInstanceData {
	inst := alternatorInstanceData{CurrentA: -1, VoltageV: -1, PowerW: -1, TempC: -1}
	prefix := "electrical.alternator." + index

	if v, ok := t.Number(prefix + ".current"); ok && v.Value >= 0 {
		inst.CurrentA = roundTo1(v.Value)
	}

	if v, ok := t.Number(prefix + ".voltage"); ok && v.Value >= 0 {
		inst.VoltageV = roundTo1(v.Value)
	}

	power, powerOK := 0.0, false
	if v, ok := t.Number(prefix + ".power"); ok {
		power, powerOK = v.Value, true
	} else if inst.CurrentA >= 0 && inst.VoltageV > 0 {
		power, powerOK = roundTo1(inst.CurrentA*inst.VoltageV), true
	}
	if powerOK && power >= 0 {
		inst.PowerW = roundTo1(power)
	}

	if v, ok := t.Number(prefix + ".temperature"); ok && v.Value >= 0 {
		inst.TempC = roundTo1(v.Value - 273.15)
	}

	return inst
}

func readChargerInstance(t telemetryReader, index string) chargerInstanceData {
	inst := chargerInstanceData{CurrentA: -1, ACIn1CurrentA: -1}
	prefix := "electrical.chargers." + index

	if v, ok := t.Number(prefix + ".current"); ok && v.Value >= 0 {
		inst.CurrentA = roundTo1(v.Value)
	}

	if v, ok := t.Number(prefix + ".acin.1.current"); ok && v.Value >= 0 {
		inst.ACIn1CurrentA = roundTo1(v.Value)
	}

	if chargingMode := strings.TrimSpace(firstString(t, prefix+".chargingMode")); chargingMode != "" {
		inst.ChargingMode = chargingMode
	}

	if errorValue := strings.TrimSpace(firstString(t, prefix+".error")); errorValue != "" {
		inst.Error = errorValue
	}

	return inst
}

// firstNumber reads the first of paths that holds a number.
func firstNumber(t telemetryReader, paths ...string) (float64, bool) {
	for _, path := range paths {
		if v, ok := t.Number(path); ok {
			return v.Value, true
		}
	}
	return 0, false
}

// firstString returns the first non-empty string among paths, "" if none.
func firstString(t telemetryReader, paths ...string) string {
	for _, path := range paths {
		if s, ok := t.String(path); ok && s != "" {
			return s
		}
	}
	return ""
}

// leafNumber reads the number held by the leaf at exactly path - Sample's
// form, for the readers that never accepted a bare value there.
func leafNumber(t telemetryReader, path string) (float64, bool) {
	sample := t.Sample(path)
	if !sample.Present {
		return 0, false
	}
	return numericTelemetryValue(sample.Value)
}

func firstLeafNumber(t telemetryReader, paths ...string) (float64, bool) {
	for _, path := range paths {
		if v, ok := leafNumber(t, path); ok {
			return v, true
		}
	}
	return 0, false
}

// leafNumberFromAnyChild reads suffix under the first child of parent, in
// sorted order, that has it.
func leafNumberFromAnyChild(t telemetryReader, parent, suffix string) (float64, bool) {
	for _, child := range t.Children(parent) {
		if v, ok := leafNumber(t, parent+"."+child+"."+suffix); ok {
			return v, true
		}
	}
	return 0, false
}

// sumLeafNumberOverChildren sums suffix across every child of parent that
// has it; ok is false when none does.
func sumLeafNumberOverChildren(t telemetryReader, parent, suffix string) (float64, bool) {
	sum, found := 0.0, false
	for _, child := range t.Children(parent) {
		if v, ok := leafNumber(t, parent+"."+child+"."+suffix); ok {
			sum += v
			found = true
		}
	}
	return sum, found
}

// lookupMainBattery names the battery with valid SOC data and the highest
// absolute current — i.e. the actively monitored house bank — as its path,
// "electrical.batteries.<id>".
func lookupMainBattery(t telemetryReader) (string, bool) {
	best, found := "", false
	bestAbsCurrent := -1.0

	for _, id := range t.Children("electrical.batteries") {
		path := "electrical.batteries." + id

		soc, ok := leafNumber(t, path+".capacity.stateOfCharge")
		if !ok || soc < 0 || soc > 1.01 {
			continue
		}

		current, ok := leafNumber(t, path+".current")
		if !ok {
			continue
		}

		if absCurrent := math.Abs(current); absCurrent > bestAbsCurrent {
			bestAbsCurrent = absCurrent
			best, found = path, true
		}
	}

	return best, found
}

// declaredTimestamp is the source timestamp the node at path declares on
// itself, "" when it declares none. The wind, engine and solar readers judge
// recency by one named leaf's own clock, in a set order, rather than by the
// newest anywhere beneath it (Timestamp).
func declaredTimestamp(t telemetryReader, path string) string {
	s, _ := t.String(path + ".timestamp")
	return s
}

// subtreeAgeSeconds is how long before ref the newest source timestamp at or
// under path was written, to one decimal, or -1 when there is none - the
// last_update_age_s the tiles still read.
func subtreeAgeSeconds(t telemetryReader, path string, ref time.Time) float64 {
	stamp, ok := t.Timestamp(path)
	if !ok {
		return -1
	}
	return roundTo1(clampAge(ref.Sub(stamp)).Seconds())
}

// nearbyVesselMaxAge drops AIS contacts whose position has not been refreshed
// recently. The snapshot never evicts contexts, so without this a vessel that
// transmitted once and left stays frozen at its last position forever - and
// because the list sorts by range and caps at 10, a nearby ghost crowds out
// live targets. Class A and Class B both transmit at least every 3 minutes
// when stationary, so 10 minutes is ~3 missed reports.
const nearbyVesselMaxAge = 10 * time.Minute

// collisionTargetMaxAge is the cutoff for holding a CPA alarm up. Tighter than
// nearbyVesselMaxAge on purpose: the tile lists stationary neighbours that
// report every ~3 minutes, but a target worth alarming over is moving, and a
// moving target reports every 5 to 30 seconds. Five minutes is a dozen missed
// reports for the case that matters, not one. It costs nothing on the
// stationary case either, because ADR 0058 already stops a stationary target
// tripping this alarm in the first place.
const collisionTargetMaxAge = 5 * time.Minute

// aisTargetPositionFresh reports when a target's navigation.position last
// arrived and whether that is recent enough to trust the rest of its tree.
//
// One predicate, two cutoffs. The tile aged its targets and the CPA alarm did
// not, so a target that left range dropped off the tile while its collision
// alarm stayed lit in the banner until the process restarted.
//
// Position is the reference, never the notification's own path. The
// prioritizer writes notifications.navigation.closestApproach on a state
// change, not on a timer, so ageing that path against itself would silently
// clear an alarm that is still entirely valid — the target could be closing
// steadily for half an hour on one write and this would drop it a maxAge
// after that single write, which is a masking failure dressed up as a fix.
func aisTargetPositionFresh(snapshot *signalKSnapshot, vesselID string, maxAge time.Duration, now time.Time) (time.Time, bool) {
	seen := snapshot.lastSeen(vesselContextPrefix+vesselID, "navigation.position")
	if seen.IsZero() {
		return time.Time{}, false
	}
	return seen, now.Sub(seen) <= maxAge
}

const (
	// nearbyMinRangeMeters drops this vessel itself, which SignalK may publish
	// under a non-self context as well.
	nearbyMinRangeMeters = 9.144 // the 30 ft threshold this replaced
	nearbyMaxRangeMeters = 5000.0
)

// nearbyVesselsDefaultLimit is the map tile's own cap: fetchSignalKNearbyVessels
// (the /api/nearby-vessels handler and the contact poller, tracks.go) has
// always kept only the 10 closest vessels. fetchSignalKNearbyVesselsLimit
// (ADR 0128) generalizes that cap for a caller - Mate's get_nearby_vessels
// tool - that needs more than the tile itself ever shows.
const nearbyVesselsDefaultLimit = 10

// nearbyVesselsUnlimited is fetchSignalKNearbyVesselsLimit's sentinel for
// "keep every vessel currently in range, no cap at all" - get_nearby_vessels'
// (ADR 0128) own use when a name/MMSI filter names one specific vessel: a
// second, merely-larger-but-still-finite cap could still miss a vessel in a
// crowded anchorage, where an outright unlimited search cannot.
const nearbyVesselsUnlimited = -1

func fetchSignalKNearbyVessels(selfLatitude float64, selfLongitude float64, now time.Time, excludedNames []string) ([]nearbyVessel, error) {
	return fetchSignalKNearbyVesselsLimit(selfLatitude, selfLongitude, now, excludedNames, nearbyVesselsDefaultLimit)
}

// fetchSignalKNearbyVesselsLimit is fetchSignalKNearbyVessels with a
// caller-chosen cap on how many of the sorted-by-range results to keep,
// instead of the tile's hard-coded 10 (ADR 0128) - a behaviour-preserving
// split, not a change to fetchSignalKNearbyVessels' own contract.
func fetchSignalKNearbyVesselsLimit(selfLatitude float64, selfLongitude float64, now time.Time, excludedNames []string, limit int) ([]nearbyVessel, error) {
	payload, err := signalKVesselsPayload()
	if err != nil {
		return nil, err
	}

	vessels := make([]nearbyVessel, 0, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		if vesselID == "self" {
			continue
		}

		latitude := lookupNumber(vesselMap, "navigation", "position", "value", "latitude")
		if latitude == -1 {
			latitude = lookupNumber(vesselMap, "navigation", "position", "latitude")
		}

		longitude := lookupNumber(vesselMap, "navigation", "position", "value", "longitude")
		if longitude == -1 {
			longitude = lookupNumber(vesselMap, "navigation", "position", "longitude")
		}

		if latitude < -90 || latitude > 90 || longitude < -180 || longitude > 180 {
			continue
		}

		name := firstNonEmptyString(lookupString(vesselMap, "name"), lookupString(vesselMap, "design", "name"))
		if name == "" {
			name = compactVesselID(vesselID)
		}
		if matchesExcludedName(name, excludedNames) {
			continue
		}

		// SignalK servers are inconsistent about whether "mmsi" is a JSON
		// string or a bare JSON number - this app's actual live server
		// sends it as a string (confirmed live), so try that first and
		// fall back to numeric for servers that don't.
		mmsi := strings.TrimSpace(lookupString(vesselMap, "mmsi"))
		if mmsi == "" {
			if rawMmsi := lookupNumber(vesselMap, "mmsi"); rawMmsi >= 0 {
				mmsi = fmt.Sprintf("%.0f", rawMmsi)
			}
		}

		rangeMeters := math.Round(haversineMeters(selfLatitude, selfLongitude, latitude, longitude)*10) / 10
		if rangeMeters < nearbyMinRangeMeters || rangeMeters > nearbyMaxRangeMeters {
			continue
		}

		// Age comes from delta receive time (pathSeen), not the AIS-reported
		// navigation.position.timestamp: receive time is always present for
		// any vessel that exists in the snapshot at all, is immune to
		// transmitter clock skew, and resets cleanly with the process. The
		// old timestamp-parse approach silently left ageSeconds at 0 whenever
		// the field was missing or failed to parse - a masking fallback that
		// reported a dead target as "0s ago" forever, which is exactly the
		// case a staleness filter most needs to catch (see ADR 0042).
		//
		// aisTargetPositionFresh folds two failures into this one continue: no
		// position delta ever received for this context at all (nothing to age
		// against, so the position in the tree cannot be trusted), and a
		// position that arrived but has since gone stale. The collision-alarm
		// gate in collision_ais.go shares this same predicate against a
		// tighter cutoff (ADR 0057).
		positionSeen, fresh := aisTargetPositionFresh(globalSignalKSnapshot, vesselID, nearbyVesselMaxAge, now)
		if !fresh {
			continue
		}
		age := now.Sub(positionSeen)
		ageSeconds := int(age.Seconds())
		if ageSeconds < 0 {
			ageSeconds = 0
		}

		var sogKnots *float64
		sog := lookupNumber(vesselMap, "navigation", "speedOverGround", "value")
		if sog >= 0 {
			knots := math.Round((sog*1.943844)*10) / 10
			sogKnots = &knots
		}

		figures := collisionFiguresFor(vesselMap)

		vessels = append(vessels, nearbyVessel{
			ID: vesselID, Name: strings.ToUpper(name), Mmsi: mmsi,
			RangeM: rangeMeters, AgeSeconds: ageSeconds, SogKnots: sogKnots,
			Lat: latitude, Lon: longitude, PositionSeen: positionSeen,
			CpaM: figures.CpaM, TcpaSeconds: figures.TcpaSeconds, BearingRad: figures.BearingRad,
			CollisionAlarmType: figures.AlarmType, CollisionAlarmState: figures.AlarmState,
		})
	}

	sort.Slice(vessels, func(i int, j int) bool { return vessels[i].RangeM < vessels[j].RangeM })
	switch {
	case limit == nearbyVesselsUnlimited:
		// no trim - every in-range vessel is kept.
	case limit <= 0:
		limit = nearbyVesselsDefaultLimit
		if len(vessels) > limit {
			vessels = vessels[:limit]
		}
	case len(vessels) > limit:
		vessels = vessels[:limit]
	}

	return vessels, nil
}

// ── SignalK History API (ADR 0128) ──────────────────────────────────────

// signalKHistoryContextPrefix is what the SignalK History API expects a
// non-self vessel's context to be namespaced under. This app only ever has a
// bare MMSI on hand (nearbyVessel.Mmsi / vesselContactKey's own identity),
// so vesselHistoryContext builds the same "vessels.urn:mrn:imo:mmsi:<mmsi>"
// form an AIS MMSI's SignalK identity actually takes - confirmed against a
// live server (2026-09-25): /signalk/v2/api/history/contexts lists this
// vessel's own context in exactly this form.
const signalKHistoryContextPrefix = "vessels.urn:mrn:imo:mmsi:"

func vesselHistoryContext(mmsi string) string {
	return signalKHistoryContextPrefix + mmsi
}

// signalKHistoryValuesAPIPath is the v2 History API endpoint that answers
// "where was this context over this time range" - distinct from every other
// SignalK path in this file, which reads the live data-model tree rather
// than a time series. Confirmed against a live server (2026-09-25): it needs
// context, paths, from, to and resolution query parameters, and returns
// {"data":[[iso-time, [lon,lat]], ...]}.
const signalKHistoryValuesAPIPath = "/signalk/v2/api/history/values"

// signalKHistoryPoint is one point of a SignalK History API position series
// - get_nearby_vessels' stationary-since figure (ADR 0128): when the fix was
// recorded, and where.
type signalKHistoryPoint struct {
	Time time.Time
	Lat  float64
	Lon  float64
}

// parseSignalKHistoryValues decodes a /signalk/v2/api/history/values response
// body into a slice of points, in whatever order the server returned them
// (ascending by time on the live server this was confirmed against, but the
// caller sorts explicitly rather than assuming that). Each data row is
// [iso-timestamp, [lon, lat]] - a resolution bucket with nothing sampled in
// it comes back with a null second element rather than being omitted, which
// is skipped rather than failing the whole decode over one empty bucket. An
// empty data array (confirmed live, 2026-09-25, for a vessel this boat's own
// InfluxDB writer has never recorded - see ADR 0128) decodes to an empty,
// nil-error slice: "no history for this vessel" is a normal outcome, not a
// parse failure.
func parseSignalKHistoryValues(body []byte) ([]signalKHistoryPoint, error) {
	var parsed struct {
		Data [][]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode signalk history response: %w", err)
	}

	points := make([]signalKHistoryPoint, 0, len(parsed.Data))
	for _, row := range parsed.Data {
		if len(row) != 2 {
			continue
		}
		var ts string
		if err := json.Unmarshal(row[0], &ts); err != nil {
			continue
		}
		// An empty resolution bucket comes back with a literal JSON null
		// here rather than a [lon,lat] pair. Unmarshaling null into a fixed
		// [2]float64 array is a silent no-op in encoding/json (null only
		// resets interface/map/pointer/slice targets), so this must be
		// checked explicitly - without it, an empty bucket would decode as
		// a bogus fix at (0, 0) instead of being skipped.
		if string(row[1]) == "null" {
			continue
		}
		// Decoded as a slice, not straight into a fixed [2]float64: Go's
		// encoding/json silently zero-fills a JSON array shorter than the Go
		// array it targets, and silently discards extra elements of a longer
		// one - neither is an error, so a malformed one-element [lon] row
		// would otherwise decode into a bogus (lon, 0) point instead of
		// being skipped (code-review finding, 2026-09-25). The explicit
		// length check below is what actually enforces "must be [lon,lat]".
		var lonLat []float64
		if err := json.Unmarshal(row[1], &lonLat); err != nil || len(lonLat) != 2 {
			// Any other shape that isn't exactly a [lon,lat] pair - skip
			// this one row rather than failing the whole response over it.
			continue
		}
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue
		}
		points = append(points, signalKHistoryPoint{Time: t, Lon: lonLat[0], Lat: lonLat[1]})
	}

	return points, nil
}

// fetchSignalKPositionHistory reads mmsi's navigation.position history from
// the SignalK History API over [from, to], aggregated to resolutionSeconds
// buckets. A non-2xx status or an undecodable body is returned as an
// explicit error (AGENTS.md's fallback policy) - never a fabricated point;
// an empty result set is not an error (see parseSignalKHistoryValues) since
// today that is the expected outcome for every vessel but self (ADR 0128).
func fetchSignalKPositionHistory(settingsPath, mmsi string, from, to time.Time, resolutionSeconds int) ([]signalKHistoryPoint, error) {
	// Unlike buildNearbyVesselsPayload's use of loadSignalKSettings, a
	// settings-read failure here must not silently fall back to
	// defaultSignalKAddress: this result feeds get_nearby_vessels'
	// per-vessel position_history_error field, and reporting a request
	// against a wrong, made-up host as if it were this boat's actual
	// SignalK server would misattribute the real failure entirely
	// (AGENTS.md's fallback policy; code-review finding, 2026-09-25).
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("load signalk settings: %w", err)
	}
	signalkURL := buildSignalKURL(address, port)

	query := url.Values{}
	query.Set("context", vesselHistoryContext(mmsi))
	query.Set("paths", "navigation.position")
	query.Set("from", from.UTC().Format(time.RFC3339))
	query.Set("to", to.UTC().Format(time.RFC3339))
	query.Set("resolution", strconv.Itoa(resolutionSeconds))
	path := signalKHistoryValuesAPIPath + "?" + query.Encode()

	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, path, http.MethodGet, nil)
	if err != nil {
		return nil, fmt.Errorf("signalk history request for %s: %w", mmsi, err)
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("signalk history endpoint returned status %d for %s: %s", status, mmsi, string(body))
	}

	points, err := parseSignalKHistoryValues(body)
	if err != nil {
		return nil, fmt.Errorf("%w (vessel %s)", err, mmsi)
	}
	return points, nil
}

func fetchSignalKVesselNameMap() (map[string]string, error) {
	payload, err := signalKVesselsPayload()
	if err != nil {
		return nil, err
	}

	names := make(map[string]string, len(payload))
	for vesselID, raw := range payload {
		vesselMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		name := firstNonEmptyString(lookupString(vesselMap, "name"), lookupString(vesselMap, "design", "name"))
		if name == "" {
			name = compactVesselID(vesselID)
		}
		names[vesselID] = strings.ToUpper(strings.TrimSpace(name))
	}

	return names, nil
}

func fetchSignalKTanksState(labelOverrides map[string]string) ([]tankLevelData, time.Time, error) {
	t := globalSignalKSnapshot.selfTelemetry()
	if !t.HasSelf() {
		return nil, time.Now().UTC(), errSignalKNoSelf
	}
	tanks, datetime := tanksFromTelemetry(t, labelOverrides, time.Now().UTC())
	return tanks, datetime, nil
}

func tanksFromTelemetry(t telemetryReader, labelOverrides map[string]string, datetime time.Time) ([]tankLevelData, time.Time) {
	datetimeString := firstString(t, "navigation.datetime", "timestamp")
	if datetimeString != "" {
		parsed, parseErr := time.Parse(time.RFC3339, datetimeString)
		if parseErr == nil {
			datetime = parsed.UTC()
		}
	}

	present := t.Children("tanks")
	if len(present) == 0 {
		return []tankLevelData{}, datetime
	}

	categoryOrder := []string{"freshWater", "fuel", "blackWater", "greyWater", "liveWell", "lubrication", "water", "wasteWater"}
	knownCategory := map[string]struct{}{}
	for _, category := range categoryOrder {
		knownCategory[category] = struct{}{}
	}
	isPresent := map[string]bool{}
	for _, category := range present {
		isPresent[category] = true
	}

	orderedCategories := make([]string, 0, len(present))
	for _, category := range categoryOrder {
		if isPresent[category] {
			orderedCategories = append(orderedCategories, category)
		}
	}
	for _, category := range present {
		if _, ok := knownCategory[category]; ok {
			continue
		}
		orderedCategories = append(orderedCategories, category)
	}

	tanks := make([]tankLevelData, 0)
	for _, category := range orderedCategories {
		for _, entryID := range t.Children("tanks." + category) {
			entry := "tanks." + category + "." + entryID

			reading, ok := t.Number(entry + ".currentLevel")
			if !ok || reading.Value < 0 {
				continue
			}

			level := reading.Value
			if level <= 1 {
				level *= 100
			}
			level = math.Max(0, math.Min(100, roundTo1(level)))

			label := tankLabelOverride(labelOverrides, category, entryID)
			if label == "" {
				continue
			}

			// Mirrors readSolarController (ADR 0068): each tank's age comes
			// from its own subtree, so one dead sender does not condemn
			// every tank on the boat.
			lastUpdateAge := subtreeAgeSeconds(t, entry, datetime)

			tanks = append(tanks, tankLevelData{ID: category + "." + entryID, Label: label, Category: category, Kind: tankKindFromCategory(category), LevelPercent: level, LastUpdateAge: lastUpdateAge})
		}
	}

	return tanks, datetime
}

func loadTankLabelOverrides(settingsPath string) map[string]string {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return map[string]string{}
	}

	uiMap, ok := settings["ui"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	rawLabels, ok := uiMap["tank_labels"].(map[string]any)
	if !ok {
		return map[string]string{}
	}

	labels := map[string]string{}
	for key, value := range rawLabels {
		label, ok := value.(string)
		if !ok {
			continue
		}

		normalizedKey := strings.ToLower(strings.TrimSpace(key))
		normalizedLabel := strings.TrimSpace(label)
		if normalizedKey == "" || normalizedLabel == "" {
			continue
		}

		labels[normalizedKey] = normalizedLabel
	}

	return labels
}

func tankLabelOverride(overrides map[string]string, category string, entryID string) string {
	if len(overrides) == 0 {
		return ""
	}

	category = strings.ToLower(strings.TrimSpace(category))
	entryID = strings.TrimSpace(entryID)

	keys := []string{category + "." + strings.ToLower(entryID), category + "/" + strings.ToLower(entryID), strings.ToLower(entryID)}
	for _, key := range keys {
		if value, ok := overrides[key]; ok {
			return value
		}
	}

	return ""
}

func tankKindFromCategory(category string) string {
	normalized := strings.ToLower(strings.TrimSpace(category))
	if strings.Contains(normalized, "fuel") {
		return "fuel"
	}

	if strings.Contains(normalized, "black") || strings.Contains(normalized, "grey") || strings.Contains(normalized, "waste") || strings.Contains(normalized, "sewage") || strings.Contains(normalized, "holding") {
		return "waste"
	}

	return "water"
}

func lookupString(payload map[string]any, keys ...string) string {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}

		next, ok := asMap[key]
		if !ok {
			return ""
		}

		current = next
	}

	value, ok := current.(string)
	if !ok {
		return ""
	}

	return value
}

func lookupNumber(payload map[string]any, keys ...string) float64 {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return -1
		}

		next, ok := asMap[key]
		if !ok {
			return -1
		}

		current = next
	}

	switch v := current.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return -1
	}
}

func lookupBool(payload map[string]any, keys ...string) (bool, bool) {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return false, false
		}
		next, ok := asMap[key]
		if !ok {
			return false, false
		}
		current = next
	}
	switch v := current.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	}
	return false, false
}

// freshestAge reduces several last_update_age_s values (each -1 for
// "unknown", per subtreeAgeSeconds) down to the one a tile should show:
// the freshest (minimum) of whichever inputs are actually known. This is
// the same "one dead controller among four does not condemn the total"
// reduction ADR 0068 established for solarStateData.LastUpdateAge across its
// controllers, generalized here so tanks, nearby vessels, and a GNSS fix
// split across navigation.position/navigation.gnss can reuse it instead of
// each re-deriving the same min-ignoring-unknowns logic.
//
// An empty call (or every input unknown) returns -1, not 0: no known ages is
// an absence of evidence, not evidence the source is fresh.
func freshestAge(ages ...float64) float64 {
	result := -1.0
	for _, age := range ages {
		if age < 0 {
			continue
		}
		if result < 0 || age < result {
			result = age
		}
	}
	return result
}

// tanksFeedAge reduces each tank's own last_update_age_s down to the one age
// the Tanks tile itself goes stale on: the freshest of all reporting tanks
// (ADR 0068's "one dead controller does not condemn the total" reduction,
// same as solarStateData.LastUpdateAge across its controllers). Zero tanks
// configured is absence of evidence, not evidence of a dead feed, so an
// empty slice correctly falls through to freshestAge's -1.
func tanksFeedAge(tanks []tankLevelData) float64 {
	ages := make([]float64, len(tanks))
	for i, tank := range tanks {
		ages[i] = tank.LastUpdateAge
	}
	return freshestAge(ages...)
}

// nearbyVesselsFeedAge reduces each contact's own age_seconds (how long
// since that vessel was last seen) down to the one age the Nearby Vessels
// tile itself goes stale on: the freshest contact in range. Zero vessels in
// range is silence, not evidence the AIS/radar feed died, so an empty slice
// correctly falls through to freshestAge's -1 rather than 0.
func nearbyVesselsFeedAge(vessels []nearbyVessel) float64 {
	ages := make([]float64, len(vessels))
	for i, vessel := range vessels {
		ages[i] = float64(vessel.AgeSeconds)
	}
	return freshestAge(ages...)
}

func lookupAnyMap(payload map[string]any, keys ...string) map[string]any {
	var current any = payload
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			return nil
		}

		next, ok := asMap[key]
		if !ok {
			return nil
		}

		current = next
	}

	result, ok := current.(map[string]any)
	if !ok {
		return nil
	}

	return result
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

// ── SignalK authentication token cache ───────────────────────────────────────

type cachedToken struct {
	token     string
	expiresAt time.Time
}

var skTokenMu sync.Mutex
var skTokenCache *cachedToken

// loadSignalKCredentials reads SIGNALK_USERNAME/SIGNALK_PASSWORD from
// globalSecretsStore at point of use (ADR 0023 amendment, 2026-09-19) - the
// boot-time copy into the process environment (LoadIntoEnv) is retired, so
// there is no cache here to go stale when an operator rotates the
// credential from the Secrets panel.
//
// A nil store is treated the same as "no credential configured", not an
// error: it mirrors wasm_plugin.go's configForWasmPlugin, whose own doc
// comment draws this exact distinction for a READ (as opposed to
// clearBoundSecret's WRITE, where a nil store must fail loudly) - a caller
// that cannot reach a secrets store at all simply proceeds unauthenticated,
// which acquireSignalKToken already treats as a supported, legitimate mode.
// A store that IS open but fails to read or decrypt a row is a different,
// worse case: an operator may have a real credential configured, and
// silently discarding it here would degrade SignalK auth to anonymous with
// nothing to show it happened - "a working boat with no alarm writes". That
// case is returned as an error instead, per this repo's fallback policy.
func loadSignalKCredentials(_ string) (username, password string, err error) {
	// A nil store is a programming error, never a deployment state: main()
	// opens it and log.Fatalf's on failure long before anything that reads a
	// credential is started. Reporting it as "no credentials configured"
	// would let SignalK quietly fall back to unauthenticated on a boot
	// ordering mistake - a boat that looks healthy while every notifications.*
	// write is silently refused. Surface it instead.
	if globalSecretsStore == nil {
		return "", "", fmt.Errorf("secrets store unavailable, cannot read SignalK credentials")
	}
	username, _, err = globalSecretsStore.Get("SIGNALK_USERNAME")
	if err != nil {
		return "", "", fmt.Errorf("reading SIGNALK_USERNAME: %w", err)
	}
	password, _, err = globalSecretsStore.Get("SIGNALK_PASSWORD")
	if err != nil {
		return "", "", fmt.Errorf("reading SIGNALK_PASSWORD: %w", err)
	}
	return username, password, nil
}

// acquireSignalKToken returns a cached JWT or fetches a fresh one.
func acquireSignalKToken(signalkURL, username, password string) (string, error) {
	if username == "" || password == "" {
		return "", nil
	}

	skTokenMu.Lock()
	defer skTokenMu.Unlock()

	if skTokenCache != nil && time.Now().Before(skTokenCache.expiresAt) {
		return skTokenCache.token, nil
	}

	url := strings.TrimRight(signalkURL, "/") + "/signalk/v1/auth/login"
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("signalk auth: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("signalk auth returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Token      string  `json:"token"`
		TimeToLive float64 `json:"timeToLive"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil || result.Token == "" {
		return "", fmt.Errorf("signalk auth: could not parse token response")
	}

	ttl := result.TimeToLive
	if ttl <= 0 {
		ttl = 86400 // default 24h
	}
	skTokenCache = &cachedToken{
		token:     result.Token,
		expiresAt: time.Now().Add(time.Duration(ttl-60) * time.Second),
	}
	return result.Token, nil
}

// invalidateSignalKToken clears the cached token so the next call re-authenticates.
func invalidateSignalKToken() {
	skTokenMu.Lock()
	skTokenCache = nil
	skTokenMu.Unlock()
}

func buildSignalKURL(address string, port int) string {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		trimmed = defaultSignalKAddress
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return strings.TrimRight(trimmed, "/")
	}

	if port <= 0 || port > 65535 {
		port = defaultSignalKPort
	}

	return fmt.Sprintf("http://%s:%d", trimmed, port)
}

func loadSignalKSettings(settingsPath string) (string, int, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return "", 0, err
	}

	signalkMap, ok := settings["signalk"].(map[string]any)
	if !ok {
		return defaultSignalKAddress, defaultSignalKPort, nil
	}

	address, _ := signalkMap["address"].(string)
	if strings.TrimSpace(address) == "" {
		address = defaultSignalKAddress
	}

	port := coercePort(signalkMap["port"])
	if port <= 0 {
		port = defaultSignalKPort
	}

	return address, port, nil
}

func normalizeDegrees(value float64) float64 {
	normalized := math.Mod(value, 360)
	if normalized < 0 {
		normalized += 360
	}

	return normalized
}

func normalizeSignedDegrees(value float64) float64 {
	normalized := normalizeDegrees(value)
	if normalized > 180 {
		normalized -= 360
	}

	return normalized
}

func roundTo1(value float64) float64 { return math.Round(value*10) / 10 }

func haversineMeters(lat1 float64, lon1 float64, lat2 float64, lon2 float64) float64 {
	const earthRadiusMeters = 6371000.0
	lat1Rad := lat1 * math.Pi / 180
	lon1Rad := lon1 * math.Pi / 180
	lat2Rad := lat2 * math.Pi / 180
	lon2Rad := lon2 * math.Pi / 180
	deltaLat := lat2Rad - lat1Rad
	deltaLon := lon2Rad - lon1Rad
	a := math.Sin(deltaLat/2)*math.Sin(deltaLat/2) + math.Cos(lat1Rad)*math.Cos(lat2Rad)*math.Sin(deltaLon/2)*math.Sin(deltaLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusMeters * c
}

// destinationPoint projects (lat, lon) forward by distanceM metres along
// bearingRad (radians, 0 = north, clockwise), returning the resulting
// lat/lon in degrees. Spherical model, ported from
// frontend/src/lib/geo.ts's destinationPoint (same R = 6371000, same
// longitude wrap) so the two stay numerically consistent.
func destinationPoint(lat float64, lon float64, bearingRad float64, distanceM float64) (float64, float64) {
	const earthRadiusMeters = 6371000.0
	lat1 := lat * math.Pi / 180
	lon1 := lon * math.Pi / 180

	lat2 := math.Asin(
		math.Sin(lat1)*math.Cos(distanceM/earthRadiusMeters) +
			math.Cos(lat1)*math.Sin(distanceM/earthRadiusMeters)*math.Cos(bearingRad),
	)
	lon2 := lon1 + math.Atan2(
		math.Sin(bearingRad)*math.Sin(distanceM/earthRadiusMeters)*math.Cos(lat1),
		math.Cos(distanceM/earthRadiusMeters)-math.Sin(lat1)*math.Sin(lat2),
	)

	latDeg := lat2 * 180 / math.Pi
	lonDeg := math.Mod(lon2*180/math.Pi+540, 360) - 180

	return latDeg, lonDeg
}

func compactVesselID(vesselID string) string {
	trimmed := strings.TrimSpace(vesselID)
	if trimmed == "" {
		return "UNKNOWN"
	}
	segments := strings.Split(trimmed, ":")
	return segments[len(segments)-1]
}

func loadBoatVesselPrefix(settingsPath string) string {
	return loadSettingString(settingsPath, []string{"boat", "name"})
}

func fetchSignalKSelfName() string {
	return strings.TrimSpace(firstString(globalSignalKSnapshot.liveSelfTelemetry(time.Now().UTC()), "name", "design.name"))
}

func matchesExcludedName(candidate string, excludedNames []string) bool {
	trimmedCandidate := strings.TrimSpace(candidate)
	if trimmedCandidate == "" {
		return false
	}
	for _, excluded := range excludedNames {
		if excluded != "" && strings.EqualFold(trimmedCandidate, strings.TrimSpace(excluded)) {
			return true
		}
	}
	return false
}

// signalkRoutes returns the /api routes for SignalK server settings, discovery and path browsing.
// Each row carries its own auth tier (docs/adr/0177).
func signalkRoutes() []apiRoute {
	return []apiRoute{
		{http.MethodGet, "/api/signalk/paths", tierRead, signalKPathsHandler},
		{http.MethodGet, "/api/settings/signalk", tierAdmin, getSignalKSettingsHandler},

		// Probe only — persisting the address is POST /api/settings' job
		// alone (ADR 0028). There is deliberately no POST /api/settings/signalk.
		{http.MethodPost, "/api/settings/signalk/test", tierAdmin, testSignalKConnectionHandler},

		// Also a pure read: sweeps the local network and reports what it
		// found. Accepting a result saves through POST /api/settings like
		// any other write. Gated admin, alongside the rest of the settings
		// workflow it belongs to.
		{http.MethodPost, "/api/signalk/discover", tierAdmin, discoverSignalKHandler},
	}
}
