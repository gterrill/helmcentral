package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The engines as the first subsystem of the learned normal (ADR 0276): one
// subject per configured engine, regimes of 200 rpm, and the single-engine form
// of the twin gate, so a boat with one engine is judged too.

const normalSubsystemEngines = "engines"

// normalEngineSteadyMinutes is how many minutes rpm must hold within the twin
// gate's own 50 rpm band, and coolant stay warm, before a minute is judged. It
// is the five steady minutes the twin baseline already asks of a learning minute.
const normalEngineSteadyMinutes = engineBaselineMinRunMinutes

// normalEngineCandidates are the readings every engine is offered. Whether one
// is used is decided from the data (selectFeatures), never from a name: a dead
// sender drops out as constant and a repaired one joins on its own.
//
// MinChange is the smallest change worth a look, in SI units, with a reason each.
// None is a boat's value. SIUnit comes from sensorLimitUnit and Path is set per
// engine.
var normalEngineCandidates = []normalFeature{
	// Revolutions say where on its range the minute sits. They are never named as
	// the reason a minute is unusual.
	{Key: "revolutions", Required: true, MinChange: 20.0 / 60},         // Hz: 20 rpm, a throttle nudge
	{Key: "temperature", Explain: true, Required: true, MinChange: 1},  // K: a whole degree, the usual sender step
	{Key: "oilPressure", Explain: true, MinChange: 10000},              // Pa: 10 kPa, about 1.5 psi
	{Key: "boostPressure", Explain: true, MinChange: 5000},             // Pa: 5 kPa, about 0.7 psi
	{Key: "engineLoad", Explain: true, MinChange: 0.02},                // ratio: two percentage points
	{Key: "fuel.rate", Explain: true, MinChange: 1.0 / 3600000},        // m3/s: 1 L/h
	{Key: "transmission.oilPressure", Explain: true, MinChange: 10000}, // Pa: as engine oil
	{Key: "transmission.oilTemperature", Explain: true, MinChange: 1},  // K: as coolant
	{Key: "exhaustTemperature", Explain: true, MinChange: 10},          // K: exhaust swings with every touch of load
}

// normalEngineWording is how the engines are worded in the core's sentences.
var normalEngineWording = normalWording{
	BandPrefix: "at ",
	Steady:     "steady running minutes",
	While:      "while running",
	Unsteady:   "not running steadily",
	This:       "this engine",
	Gone:       "This engine is no longer set up. It may have been renumbered or removed.",
}

func normalEnginesSubsystem() normalSubsystem {
	return normalSubsystem{
		Name:         normalSubsystemEngines,
		Title:        "Engines",
		NoSubjects:   "No engines are set up",
		Wording:      normalEngineWording,
		Subjects:     normalEngineSubjects,
		Regime:       normalEngineRegime,
		Gate:         normalEngineGate,
		Run:          normalEngineRuns,
		Aliases:      normalEngineAliases,
		AlarmReading: normalEngineAlarmReading,
		PathMatters:  func(_ vesselView, path string) bool { return normalEnginePathMatters(path) },
		Active:       func(m normalMinute) bool { return m.Values["revolutions"] > twinIdleRPMHz },
	}
}

// normalEngineSubjects is every configured engine that has an instance. There is
// no two-engine minimum: the learned normal compares an engine with itself.
func normalEngineSubjects(vessel vesselView) []normalSubject {
	namer := newSensorNamer(nil, vessel, nil)
	// The name is the one the alarm cards use (sensorNamer.engineName). When
	// two engines come out alike, as two items of one model do, the position
	// goes in front: "Port Cummins QSB 6.7 engine".
	taken := map[string]int{}
	for _, e := range vessel.Engines {
		if e.Instance != "" {
			taken[namer.engineName(e.Instance)]++
		}
	}
	var out []normalSubject
	for _, e := range vessel.Engines {
		if e.Instance == "" {
			continue
		}
		name := namer.engineName(e.Instance)
		if taken[name] > 1 {
			name = enginePositionPrefix(e.Instance) + " " + name
		}
		s := normalSubject{ID: e.Instance, Name: name, EquipmentIDs: equipmentIDs(e.EquipmentID)}
		for _, re := range normalEngineAlarmRoots {
			s.AlarmPaths = append(s.AlarmPaths, re+e.Instance)
		}
		for _, c := range normalEngineCandidates {
			c.Path, c.SIUnit = "propulsion."+e.Instance+"."+c.Key, sensorLimitUnit[c.Key]
			s.Features = append(s.Features, c)
		}
		out = append(out, s)
	}
	return out
}

// normalEnginePathMatters says a path is one of an engine's candidate readings,
// propulsion.<instance>.<reading>, and nothing else under propulsion.
func normalEnginePathMatters(path string) bool {
	rest, ok := strings.CutPrefix(path, "propulsion.")
	if !ok {
		return false
	}
	for _, c := range normalEngineCandidates {
		if id, ok := strings.CutSuffix(rest, "."+c.Key); ok && id != "" && !strings.Contains(id, ".") {
			return true
		}
	}
	return false
}

// normalEngineAlarmReading reads the engine reading an alarm is about from a
// propulsion.<id>.<reading> path, with or without the notifications. prefix. A
// propulsion path that is not one of the candidate readings is a reading the
// model does not use (key ""). The anomaly detectors' paths are not one reading.
func normalEngineAlarmReading(_ normalSubject, alarm alarmLogEntry) (string, bool) {
	alarmPath := alarm.Path
	rest, ok := strings.CutPrefix(strings.TrimPrefix(alarmPath, "notifications."), "propulsion.")
	if !ok {
		return "", false
	}
	for _, c := range normalEngineCandidates {
		if id, ok := strings.CutSuffix(rest, "."+c.Key); ok && id != "" && !strings.Contains(id, ".") {
			return c.Key, true
		}
	}
	return "", true
}

// normalRunGap is the longest step between two minutes of an engine above idle
// that is still one run: a few minutes of no data (a gateway reconnecting) are
// not a stop and a start. The twin detector keeps its one minute.
const normalRunGap = 5 * time.Minute

// enginePositionPrefix is an instance as a word to put before an engine's name:
// "Port" for port, "Engine 0" for a numbered instance.
func enginePositionPrefix(instance string) string {
	if _, err := strconv.Atoi(instance); err == nil {
		return "Engine " + instance
	}
	return strings.ToUpper(instance[:1]) + instance[1:]
}

// normalEngineRegime places a minute in its 200 rpm band, "port/rpm1800" for
// "1,800 to 2,000 rpm".
func normalEngineRegime(s normalSubject, m normalMinute) (key, label string, ok bool) {
	hz, has := m.Values["revolutions"]
	if !has {
		return "", "", false
	}
	lo := rpmBucket(hz)
	return fmt.Sprintf("%s/rpm%d", s.ID, lo), fmt.Sprintf("%s to %s rpm", commaInt(lo), commaInt(lo+engineBaselineBucketRPM)), true
}

// normalEngineGate is engineGateHolds over a run of one engine's minutes
// (sorted ascending): at or above 900 rpm, coolant at or above 65 degC, running
// ten minutes, and for the last five minutes rpm within 50 rpm and coolant warm.
// The joined minutes always carry rpm and coolant (joinNormalMinutes), so a
// missing value reads as zero here and fails the gate. A missing minute restarts
// the running clock: a stretch with no data is not evidence the engine kept
// running through it.
func normalEngineGate(minutes []normalMinute) []bool {
	running := normalEngineRunning(minutes)
	out := make([]bool, len(minutes))
	for i, m := range minutes {
		rpmSteady, coolantSteady := normalEngineSteady(minutes, i)
		out[i] = engineGateHolds(engineTwinState{
			RPMHz: m.Values["revolutions"], CoolantK: m.Values["temperature"], RunningFor: running[i],
			RPMSteady: rpmSteady, CoolantSteady: coolantSteady,
		})
	}
	return out
}

// normalEngineRunning is how long the engine has run at each minute, from the
// twin baseline's own clock (runningDurationsAt).
func normalEngineRunning(minutes []normalMinute) []time.Duration {
	times := make([]time.Time, len(minutes))
	rpm := make(map[time.Time]float64, len(minutes))
	for i, m := range minutes {
		times[i], rpm[m.At] = m.At, m.Values["revolutions"]
	}
	return runningDurationsWithin(times, rpm, twinIdleRPMHz, normalRunGap)
}

// normalEngineRuns numbers an engine's runs: a new one starts where the running
// clock is at zero, which is when rpm comes up from idle or after a gap.
func normalEngineRuns(minutes []normalMinute) []int {
	running := normalEngineRunning(minutes)
	out := make([]int, len(minutes))
	run := 0
	for i, d := range running {
		if d == 0 {
			run++
		}
		out[i] = run
	}
	return out
}

// normalEngineSteady reports whether the normalEngineSteadyMinutes minutes
// ending at index i are consecutive, all carry rpm and coolant, hold rpm within
// the gate's spread and keep coolant at or above the gate's floor.
func normalEngineSteady(minutes []normalMinute, i int) (rpmSteady, coolantSteady bool) {
	if i < normalEngineSteadyMinutes-1 {
		return false, false
	}
	lo, hi := minutes[i].Values["revolutions"], minutes[i].Values["revolutions"]
	coolantSteady = true
	for k := i - normalEngineSteadyMinutes + 1; k <= i; k++ {
		if minutes[k].At.Sub(minutes[i].At) != time.Duration(k-i)*time.Minute {
			return false, false
		}
		hz, okRPM := minutes[k].Values["revolutions"]
		coolant, okCoolant := minutes[k].Values["temperature"]
		if !okRPM || !okCoolant {
			return false, false
		}
		if hz < lo {
			lo = hz
		}
		if hz > hi {
			hi = hz
		}
		if coolant < twinGateMinCoolantK {
			coolantSteady = false
		}
	}
	return hi-lo <= twinGateMaxRPMSpreadHz+1e-9, coolantSteady
}

// normalEngineAlarmRoots go before an instance to make the subtrees whose alarms
// belong to the engine: its own readings and the anomaly detectors' paths for it.
var normalEngineAlarmRoots = []string{"propulsion.", "helmcentral.anomaly.engines.", "helmcentral.anomaly.propulsion."}

// normalEngineAliases lets the operator say "port" for "Port engine".
func normalEngineAliases(s normalSubject) []string {
	if short, ok := strings.CutSuffix(s.Name, " engine"); ok {
		return []string{short}
	}
	return nil
}

// equipmentIDs is the one item an id names, or none for a blank.
func equipmentIDs(id string) []string {
	if id == "" {
		return nil
	}
	return []string{id}
}
