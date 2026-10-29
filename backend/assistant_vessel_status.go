package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// get_vessel_status (ADR 0222) hands Mate one snapshot of the boat, built from
// the operator's roles and items, so a "how's the boat" answer copies figures
// instead of choosing SignalK paths.
//
// A reading that cannot be given is absent with a reason (statusValue.Missing).
// It is never estimated and never filled from an older value: a reading older
// than its freshness limit is reported as having no fresh reading, whatever the
// last value was.

const (
	// vesselStatusFreshAge is how old a live reading may be and still be
	// given. It is the age the Tanks tile and the tank alarms already call
	// stale (tankAlarmStaleAfterSeconds), applied to every reading but the
	// barometer.
	vesselStatusFreshAge = time.Duration(tankAlarmStaleAfterSeconds) * time.Second
	// vesselStatusPressureFreshAge is the barometer's limit. Pressure sensors
	// report slowly and the figure moves slowly, so a minute-old pressure is
	// still the pressure, where a minute-old depth is not.
	vesselStatusPressureFreshAge = barometerMaxAge
	// vesselStatusPositionFreshAge is the limit on the boat's position when
	// measuring to the anchor or the planned stay.
	vesselStatusPositionFreshAge = 5 * time.Minute
	// vesselStatusGustWindow names the gust window to the reader and to the
	// Wind tile's ladder (inMemoryMaxTrueWindGustKtsFor).
	vesselStatusGustWindow = "10m"
	// vesselStatusMaxAlarms caps the active alarms listed by title.
	vesselStatusMaxAlarms = 10
)

// Overnight draw observation (ADR 0222). Conservative on purpose: an
// observation is a question put to the operator, so a false one costs trust.
const (
	// observationMinPriorNights is how many usable earlier nights the typical
	// rate must come from. Two is the least medianNightRate will take, and
	// two nights can both be unusual.
	observationMinPriorNights = 3
	// observationDrawRatio is how many times its typical overnight draw last
	// night's must be.
	observationDrawRatio = 1.5
	// observationMinExtraPercentPerHour is how much more than typical, in
	// percentage points of state of charge an hour, last night must have
	// drained. It stops a ratio on a tiny draw (0.05 to 0.15 percent an hour)
	// from reading as news. 0.25 is 2 points over an eight hour night.
	observationMinExtraPercentPerHour = 0.25
)

// statusValue is one reading, or the reason there is not one. Value is SI as
// SignalK stores it (a state of charge and a tank level are percent); Display
// is the same figure in the operator's units.
type statusValue struct {
	Value       *float64 `json:"value,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Display     *float64 `json:"display,omitempty"`
	DisplayUnit string   `json:"display_unit,omitempty"`
	AgeSeconds  *float64 `json:"age_s,omitempty"`
	Missing     string   `json:"missing,omitempty"`
}

func missingValue(reason string) statusValue { return statusValue{Missing: reason} }

type vesselStatusHouseBank struct {
	SocPercent statusValue `json:"soc_percent"`
	Voltage    statusValue `json:"voltage"`
	// Current is signed: negative is the bank discharging.
	Current statusValue `json:"current"`
}

type vesselStatusPower struct {
	HouseBank vesselStatusHouseBank `json:"house_bank"`
	SolarW    statusValue           `json:"solar_w"`
	DCLoadW   statusValue           `json:"dc_load_w"`
}

type vesselStatusTank struct {
	Name  string      `json:"name"`
	Kind  string      `json:"kind"`
	Level statusValue `json:"level_percent"`
	// levelPath is the SignalK path the level was read from; not on the wire.
	levelPath    string
	WarnPercent  float64 `json:"warn_percent"`
	AlarmPercent float64 `json:"alarm_percent"`
	// BadWhen says which end is the bad one: "low" for fuel and water, "high"
	// for waste.
	BadWhen string `json:"bad_when"`
	// AlarmsNotRunning is set when the warn and alarm levels do not make a
	// valid pair, so the tank raises no alarm.
	AlarmsNotRunning string `json:"alarms_not_running,omitempty"`
	AlarmsOff        bool   `json:"alarms_off,omitempty"`
	AlarmState       string `json:"alarm_state,omitempty"`
}

type vesselStatusTanks struct {
	Tanks   []vesselStatusTank `json:"tanks"`
	Missing string             `json:"missing,omitempty"`
}

type vesselStatusSurroundings struct {
	Depth         statusValue `json:"depth"`
	TrueWindSpeed statusValue `json:"true_wind_speed"`
	// TrueWindGust is the strongest true wind recorded in the last ten
	// minutes, and TrueWindGustNote says so.
	TrueWindGust     statusValue `json:"true_wind_gust"`
	TrueWindGustNote string      `json:"true_wind_gust_note,omitempty"`
	Pressure         statusValue `json:"pressure"`
	// PressureChange3h is the barometer's change over three hours, signed:
	// negative is falling. It is the figure the Law of Storms alarms use.
	PressureChange3h statusValue `json:"pressure_change_3h"`
}

type vesselStatusEngine struct {
	Name    string      `json:"name"`
	State   string      `json:"state,omitempty"`
	RPM     statusValue `json:"rpm"`
	Missing string      `json:"missing,omitempty"`
}

type vesselStatusAnchor struct {
	Down                bool     `json:"down"`
	Lat                 float64  `json:"lat,omitempty"`
	Lon                 float64  `json:"lon,omitempty"`
	RadiusM             float64  `json:"radius_m,omitempty"`
	SetAt               string   `json:"set_at,omitempty"`
	DistanceFromAnchorM *float64 `json:"distance_from_anchor_m,omitempty"`
	DistanceMissing     string   `json:"distance_missing,omitempty"`
	Missing             string   `json:"missing,omitempty"`
}

type vesselStatusBoat struct {
	Engines        []vesselStatusEngine `json:"engines"`
	EnginesMissing string               `json:"engines_missing,omitempty"`
	Anchor         vesselStatusAnchor   `json:"anchor"`
}

type vesselStatusAlarm struct {
	Title    string `json:"title"`
	Message  string `json:"message,omitempty"`
	State    string `json:"state"`
	Phase    string `json:"phase"`
	Silenced bool   `json:"silenced,omitempty"`
}

type vesselStatusAlarms struct {
	ActiveCount  int                 `json:"active_count"`
	Active       []vesselStatusAlarm `json:"active"`
	IgnoredCount int                 `json:"ignored_count"`
	Ignored      []string            `json:"ignored"`
}

type vesselStatusAnomalies struct {
	Frozen            int      `json:"frozen_count"`
	OutOfRange        int      `json:"out_of_range_count"`
	SilentSources     int      `json:"silent_source_count"`
	FrozenSensors     []string `json:"frozen_sensors,omitempty"`
	OutOfRangeSensors []string `json:"out_of_range_sensors,omitempty"`
	SilentSourceNames []string `json:"silent_sources,omitempty"`
	Missing           string   `json:"missing,omitempty"`
}

type vesselStatusStay struct {
	Name   string  `json:"name,omitempty"`
	Lat    float64 `json:"lat,omitempty"`
	Lon    float64 `json:"lon,omitempty"`
	Arrive string  `json:"arrive,omitempty"`
	Depart string  `json:"depart,omitempty"`
	// The distance is from the boat's own position, in a straight line. It is
	// a fact in the result so a stay on the wrong side of the coast is
	// visible without Mate having to notice.
	DistanceFromBoatM  *float64 `json:"distance_from_boat_m,omitempty"`
	DistanceFromBoatNM *float64 `json:"distance_from_boat_nm,omitempty"`
	BearingTrueDeg     *float64 `json:"bearing_from_boat_true_deg,omitempty"`
	DistanceMissing    string   `json:"distance_missing,omitempty"`
	Missing            string   `json:"missing,omitempty"`
}

// vesselObservation is something below an alarm that a person would call odd.
// It is never an alarm and is never pushed: it appears here for Mate to put to
// the operator as a question.
type vesselObservation struct {
	Kind                    string  `json:"kind"`
	Summary                 string  `json:"summary"`
	LastNightPercentPerHour float64 `json:"last_night_percent_per_hour"`
	TypicalPercentPerHour   float64 `json:"typical_percent_per_hour"`
	Ratio                   float64 `json:"ratio"`
	NightsCompared          int     `json:"nights_compared"`
	NightStart              string  `json:"night_start,omitempty"`
	// The night window and the bank, for a watch's chart. Not on the wire.
	nightStart, nightEnd time.Time
	bankPath             string
}

type vesselStatusResult struct {
	GeneratedAt  string                   `json:"generated_at"`
	Units        string                   `json:"units"`
	Power        vesselStatusPower        `json:"power"`
	Tanks        vesselStatusTanks        `json:"tanks"`
	Surroundings vesselStatusSurroundings `json:"surroundings"`
	Boat         vesselStatusBoat         `json:"boat"`
	Alarms       vesselStatusAlarms       `json:"alarms"`
	Anomalies    vesselStatusAnomalies    `json:"anomalies"`
	PlannedStay  vesselStatusStay         `json:"planned_stay"`
	// Observations are the deterministic "worth a second look" findings.
	// ObservationsUnchecked says which could not be looked at and why, so an
	// empty observations list is never read as an all-clear for them.
	Observations          []vesselObservation `json:"observations"`
	ObservationsUnchecked []string            `json:"observations_unchecked,omitempty"`
}

// ── readings ─────────────────────────────────────────────────────────────

func roundTo(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func describeAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d days", int(d.Hours()/24))
}

// staleReason is the reason a reading of this age is not given.
func staleReason(age time.Duration, known bool) string {
	if !known {
		return "no fresh reading (the source gives no timestamp, so its age is unknown)"
	}
	return fmt.Sprintf("no fresh reading (the last one arrived %s ago)", describeAge(age))
}

// freshNumber reads path and says why not when it is absent or too old.
func freshNumber(t telemetryReader, path string, maxAge time.Duration, what string) (telemetryNumber, string) {
	n, ok := t.Number(path)
	if !ok {
		return telemetryNumber{}, "no " + what + " reading"
	}
	if !n.AgeKnown || n.Age > maxAge {
		return telemetryNumber{}, staleReason(n.Age, n.AgeKnown)
	}
	return n, ""
}

// statusDecimals is how many decimals a displayed figure in this unit carries,
// the precision an instrument would show: whole watts, whole rpm and degrees,
// hundredths of a volt, tenths of everything else.
func statusDecimals(unit string) int {
	switch unit {
	case "W", "rpm", "°":
		return 0
	case "V":
		return 2
	}
	return 1
}

// statusReading shapes si (declared as siUnit) for display in system. A
// percentage has no SI unit and is given as is. The barometer is given in
// hectopascals on both systems, the unit the barometer alarms are written in.
func statusReading(path string, si float64, siUnit, system string, age time.Duration) (statusValue, error) {
	out := statusValue{Value: floatPtr(roundTo(si, 4)), Unit: siUnit}
	secs := roundTo(age.Seconds(), 1)
	out.AgeSeconds = &secs
	switch siUnit {
	case "%", "rpm":
		out.Display, out.DisplayUnit = floatPtr(roundTo(si, statusDecimals(siUnit))), siUnit
	case "Pa":
		out.Display, out.DisplayUnit = floatPtr(roundTo(si/100, 1)), "hPa"
	default:
		label, conv, err := chartDisplayFor(path, siUnit, system)
		if err != nil {
			return statusValue{}, fmt.Errorf("get_vessel_status: %s: %w", path, err)
		}
		out.Display, out.DisplayUnit = floatPtr(roundTo(conv(si), statusDecimals(label))), label
	}
	return out, nil
}

// statusFreshReading is freshNumber and statusReading together.
func statusFreshReading(t telemetryReader, path string, siUnit, system string, maxAge time.Duration, what string) (statusValue, error) {
	n, reason := freshNumber(t, path, maxAge, what)
	if reason != "" {
		return missingValue(reason), nil
	}
	return statusReading(path, n.Value, siUnit, system, n.Age)
}

// secondsAge turns a last_update_age_s field (-1 for unknown) into a duration.
func secondsAge(s float64) (time.Duration, bool) {
	if s < 0 {
		return 0, false
	}
	return time.Duration(s * float64(time.Second)), true
}

// ── the tool ─────────────────────────────────────────────────────────────

// vesselStatusMissingDeps names what is not wired, so a half-built dependency
// set fails plainly instead of reading as an empty boat.
func (d assistantToolDeps) vesselStatusMissingDeps() []string {
	var missing []string
	check := func(name string, wired bool) {
		if !wired {
			missing = append(missing, name)
		}
	}
	check("telemetry", d.telemetry != nil)
	check("vesselView", d.vesselView != nil)
	check("displayUnits", d.displayUnits != nil)
	check("vesselState", d.vesselState != nil)
	check("trueWindGustKts", d.trueWindGustKts != nil)
	check("sourceItems", d.sourceItems != nil)
	check("tankDefaults", d.tankDefaults != nil)
	check("derivedPaths", d.derivedPaths != nil)
	check("barometerWindow", d.barometerWindow != nil)
	check("engineStates", d.engineStates != nil)
	check("activeAlarms", d.activeAlarms != nil)
	check("ignoredAlarms", d.ignoredAlarms != nil)
	check("anomalyReading", d.anomalyReading != nil)
	check("anchorWatch", d.anchorWatch != nil)
	check("overnight", d.overnight != nil)
	check("now", d.now != nil)
	return missing
}

func (d assistantToolDeps) executeGetVesselStatus(ctx context.Context, _ json.RawMessage) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if missing := d.vesselStatusMissingDeps(); len(missing) > 0 {
		return "", fmt.Errorf("get_vessel_status: not available (%s not wired)", strings.Join(missing, ", "))
	}
	res, err := d.buildVesselStatus()
	if err != nil {
		return "", err
	}
	return capToolResultJSON(&res, nil)
}

func (d assistantToolDeps) buildVesselStatus() (vesselStatusResult, error) {
	now := d.now().UTC()
	t, err := d.telemetry(now)
	if err != nil {
		return vesselStatusResult{}, fmt.Errorf("get_vessel_status: no live instrument data: %w", err)
	}
	vessel, err := d.vesselView()
	if err != nil {
		return vesselStatusResult{}, fmt.Errorf("get_vessel_status: the roles are unreadable: %w", err)
	}
	system, err := d.displayUnits()
	if err != nil {
		return vesselStatusResult{}, fmt.Errorf("get_vessel_status: the units setting is unreadable: %w", err)
	}
	state, err := d.vesselState()
	if err != nil {
		return vesselStatusResult{}, fmt.Errorf("get_vessel_status: the boat's state is unavailable: %w", err)
	}

	res := vesselStatusResult{GeneratedAt: now.Format(time.RFC3339), Units: system}
	alarms := d.activeAlarms()

	if res.Power, err = vesselStatusPowerFor(t, vessel, system); err != nil {
		return vesselStatusResult{}, err
	}
	if res.Tanks, err = d.vesselStatusTanksFor(t, system, alarms); err != nil {
		return vesselStatusResult{}, err
	}
	if res.Surroundings, err = d.vesselStatusSurroundingsFor(t, state, system, now); err != nil {
		return vesselStatusResult{}, err
	}

	lat, lon, noPosition := boatPosition(state)
	if res.Boat, err = d.vesselStatusBoatFor(t, vessel, system, lat, lon, noPosition, now); err != nil {
		return vesselStatusResult{}, err
	}
	res.Alarms = d.vesselStatusAlarmsFor(alarms)
	res.Anomalies = d.vesselStatusAnomaliesFor(now)
	res.PlannedStay = d.vesselStatusStayFor(lat, lon, noPosition, now)
	res.Observations, res.ObservationsUnchecked = d.vesselObservationsFor(now)
	return res, nil
}

// ── power ────────────────────────────────────────────────────────────────

func vesselStatusPowerFor(t telemetryReader, vessel vesselView, system string) (vesselStatusPower, error) {
	var p vesselStatusPower
	var err error

	bank := vessel.houseBankPath()
	switch {
	case bank != "":
		soc, reason := freshNumber(t, bank+".capacity.stateOfCharge", vesselStatusFreshAge, "house bank state of charge")
		if reason != "" {
			p.HouseBank.SocPercent = missingValue(reason)
		} else {
			// SignalK carries a ratio; the tile scales a value of 1 or less.
			percent := soc.Value
			if percent <= 1 {
				percent *= 100
			}
			if percent < 0 || percent > 100 {
				p.HouseBank.SocPercent = missingValue("impossible reading")
			} else if p.HouseBank.SocPercent, err = statusReading(bank, percent, "%", system, soc.Age); err != nil {
				return p, err
			}
		}
		if p.HouseBank.Voltage, err = statusFreshReading(t, bank+".voltage", "V", system, vesselStatusFreshAge, "house bank voltage"); err != nil {
			return p, err
		}
		if p.HouseBank.Current, err = statusFreshReading(t, bank+".current", "A", system, vesselStatusFreshAge, "house bank current"); err != nil {
			return p, err
		}
	case vessel.HouseBankUnresolved:
		reason := "the house bank role names an item that has no SignalK source"
		p.HouseBank = vesselStatusHouseBank{SocPercent: missingValue(reason), Voltage: missingValue(reason), Current: missingValue(reason)}
	default:
		reason := "no house bank role set"
		p.HouseBank = vesselStatusHouseBank{SocPercent: missingValue(reason), Voltage: missingValue(reason), Current: missingValue(reason)}
	}

	if p.SolarW, err = solarWatts(t, system); err != nil {
		return p, err
	}
	if p.DCLoadW, err = firstFreshReading(t, "W", system, "DC load",
		"electrical.venus.dcPower", "electrical.dc.12v.power", "electrical.loads.12v.power"); err != nil {
		return p, err
	}
	return p, nil
}

// solarWatts is the Victron system total when the boat publishes one, else the
// sum of the controllers, the order the Battery & Power tile reads them in. A
// controller that has gone quiet makes the sum a guess, so the total is
// withheld rather than summed from the rest.
func solarWatts(t telemetryReader, system string) (statusValue, error) {
	const total = "electrical.venus.totalPanelPower"
	if _, ok := t.Number(total); ok {
		return nonNegativeReading(t, total, "W", system, "solar")
	}
	ids := t.Children("electrical.solar")
	if len(ids) == 0 {
		return missingValue("no solar controllers are reporting"), nil
	}
	sum, oldest := 0.0, time.Duration(0)
	for _, id := range ids {
		path := "electrical.solar." + id + ".panelPower"
		n, reason := freshNumber(t, path, vesselStatusFreshAge, "solar controller "+id)
		if reason != "" {
			return missingValue(fmt.Sprintf("%s (controller %s), so the total would be a guess", reason, id)), nil
		}
		if n.Value < 0 {
			return missingValue(fmt.Sprintf("reading is negative (controller %s), so the total would be a guess", id)), nil
		}
		sum += n.Value
		if n.Age > oldest {
			oldest = n.Age
		}
	}
	return statusReading(total, sum, "W", system, oldest)
}

// firstFreshReading reads the first of paths the boat publishes at all. If that
// one is stale it says so; it does not fall through to the next, which would
// hide a source gone quiet behind a different one.
func firstFreshReading(t telemetryReader, siUnit, system, what string, paths ...string) (statusValue, error) {
	for _, path := range paths {
		if _, ok := t.Number(path); ok {
			return nonNegativeReading(t, path, siUnit, system, what)
		}
	}
	return missingValue("no " + what + " reading"), nil
}

// nonNegativeReading is a fresh reading of a quantity that cannot be below
// zero. A negative one is a faulty sensor or a sign convention nobody has
// checked, so it is reported as such and not clamped or passed on.
func nonNegativeReading(t telemetryReader, path, siUnit, system, what string) (statusValue, error) {
	n, reason := freshNumber(t, path, vesselStatusFreshAge, what)
	if reason != "" {
		return missingValue(reason), nil
	}
	if n.Value < 0 {
		return missingValue("reading is negative"), nil
	}
	return statusReading(path, n.Value, siUnit, system, n.Age)
}

// ── tanks ────────────────────────────────────────────────────────────────

func (d assistantToolDeps) vesselStatusTanksFor(t telemetryReader, system string, alarms []alarmStatus) (vesselStatusTanks, error) {
	items, err := d.sourceItems()
	if err != nil {
		return vesselStatusTanks{}, fmt.Errorf("get_vessel_status: the equipment items are unreadable: %w", err)
	}
	defaults := d.tankDefaults()
	out := vesselStatusTanks{Tanks: []vesselStatusTank{}}
	for _, item := range items {
		if item.Group != sourceTank {
			continue
		}
		kind := tankKindOfInstance(item.Instance)
		levels, off := resolveTankLevels(kind, defaults, item)
		tank := vesselStatusTank{
			Name: item.Name, Kind: kind, WarnPercent: levels.Warn, AlarmPercent: levels.Alarm,
			BadWhen: "low", AlarmsOff: off,
		}
		if !tankLowIsBad(kind) {
			tank.BadWhen = "high"
		}
		if perr := validateTankLevels(kind, levels); perr != nil {
			tank.AlarmsNotRunning = perr.Error()
		}
		for _, a := range alarms {
			if a.RuleID == tankAlarmRuleIDPrefix+item.ID {
				tank.AlarmState = a.State
			}
		}
		n, reason := freshNumber(t, item.Source+".currentLevel", vesselStatusFreshAge, "tank level")
		if reason != "" {
			tank.Level = missingValue(reason)
		} else if percent, ok := tankLevelPercent(n.Value); !ok {
			tank.Level = missingValue("the tank sender reports a level that cannot be read")
		} else if tank.Level, err = statusReading(item.Source, percent, "%", system, n.Age); err != nil {
			return vesselStatusTanks{}, err
		}
		tank.levelPath = item.Source + ".currentLevel"
		out.Tanks = append(out.Tanks, tank)
	}
	if len(out.Tanks) == 0 {
		out.Missing = "no tanks are set up on the Equipment page"
	}
	return out, nil
}

// ── surroundings ─────────────────────────────────────────────────────────

func (d assistantToolDeps) vesselStatusSurroundingsFor(t telemetryReader, state vesselStateData, system string, now time.Time) (vesselStatusSurroundings, error) {
	var s vesselStatusSurroundings
	var err error

	if state.Depth < 0 {
		s.Depth = missingValue("no depth reading")
	} else if age, known := secondsAge(state.DepthLastUpdateAge); !known || age > vesselStatusFreshAge {
		s.Depth = missingValue(staleReason(age, known))
	} else if s.Depth, err = statusReading("environment.depth.belowTransducer", state.Depth, "m", system, age); err != nil {
		return s, err
	}

	// WindSpeedTrueKts already carries its own recency gate and reads -1 when
	// the true-wind source is absent or quiet, never the apparent figure.
	windAge, windKnown := secondsAge(state.WindLastUpdateAge)
	switch {
	case state.WindSpeedTrueKts < 0:
		reason := "no true wind reading (the boat has no true wind source, or it has gone quiet)"
		s.TrueWindSpeed, s.TrueWindGust = missingValue(reason), missingValue(reason)
	case !windKnown || windAge > vesselStatusFreshAge:
		reason := staleReason(windAge, windKnown)
		s.TrueWindSpeed, s.TrueWindGust = missingValue(reason), missingValue(reason)
	default:
		if s.TrueWindSpeed, err = statusReading("environment.wind.speedTrue", state.WindSpeedTrueKts/metersPerSecondToKnots, "m/s", system, windAge); err != nil {
			return s, err
		}
		if gust, ok := d.trueWindGustKts(); ok {
			if s.TrueWindGust, err = statusReading("environment.wind.speedTrue", gust/metersPerSecondToKnots, "m/s", system, windAge); err != nil {
				return s, err
			}
			s.TrueWindGust.AgeSeconds = nil
			s.TrueWindGustNote = "The strongest true wind recorded in the last 10 minutes. It starts empty when Helmcentral restarts."
		} else {
			s.TrueWindGust = missingValue("no true wind has been recorded in the last 10 minutes")
		}
	}

	if s.Pressure, err = statusFreshReading(t, outsidePressurePath, "Pa", system, vesselStatusPressureFreshAge, "barometric pressure"); err != nil {
		return s, err
	}

	// The change is judged from the barometer's own history: it needs a fresh
	// pressure behind it and about three hours of record (the 2.5 hours the
	// alarm side's tendencyOverWindow accepts), so a half hour of readings is
	// not called the three hour change. The value is the alarm's own figure.
	switch {
	case s.Pressure.Value == nil:
		s.PressureChange3h = missingValue("no 3 hour change: there is no fresh pressure reading")
	case !barometerCoversWindow(d.barometerWindow(now)):
		s.PressureChange3h = missingValue(fmt.Sprintf("pressure history still filling (%d min of 3 h)", barometerSpanMinutes(d.barometerWindow(now))))
	default:
		values, ages := d.derivedPaths(now)
		change := values[pressureChange3hPath]
		if change == nil {
			s.PressureChange3h = missingValue("no 3 hour change yet: the barometer history is still filling, and it starts empty when Helmcentral restarts")
		} else if age, known := secondsAge(ages[pressureChange3hPath]); !known || age > vesselStatusPressureFreshAge {
			s.PressureChange3h = missingValue(staleReason(age, known))
		} else if s.PressureChange3h, err = statusReading(pressureChange3hPath, *change, "Pa", system, age); err != nil {
			return s, err
		}
	}
	return s, nil
}

func barometerSpanMinutes(points []telemetryPoint) int {
	if len(points) < 2 {
		return 0
	}
	return int(points[len(points)-1].Timestamp.Sub(points[0].Timestamp).Minutes())
}

func barometerCoversWindow(points []telemetryPoint) bool {
	if len(points) < 2 {
		return false
	}
	return points[len(points)-1].Timestamp.Sub(points[0].Timestamp) >= pressureTrendWindow-tendencySpanSlack
}

// ── boat ─────────────────────────────────────────────────────────────────

// boatPosition is the boat's fix, or the reason there is none. The vessel
// state already holds a position back when the GNSS gate distrusts it.
func boatPosition(state vesselStateData) (lat, lon float64, missing string) {
	if state.GNSSCriticalAlert || !hasUsableVesselPosition(state.Latitude, state.Longitude) {
		return 0, 0, "no usable boat position"
	}
	age, known := secondsAge(state.PositionLastUpdateAge)
	if !known || age > vesselStatusPositionFreshAge {
		return 0, 0, "no fresh boat position"
	}
	return state.Latitude, state.Longitude, ""
}

func (d assistantToolDeps) vesselStatusBoatFor(t telemetryReader, vessel vesselView, system string, lat, lon float64, noPosition string, now time.Time) (vesselStatusBoat, error) {
	out := vesselStatusBoat{Engines: []vesselStatusEngine{}}

	if len(vessel.Engines) == 0 {
		out.EnginesMissing = "no engine items are set up"
	} else {
		states := d.engineStates(now)
		for _, e := range vessel.Engines {
			out.Engines = append(out.Engines, engineStatus(t, e, states[e.Instance]))
		}
	}

	watch, active, loadErr := d.anchorWatch()
	switch {
	case loadErr != "":
		out.Anchor.Missing = loadErr
	case active:
		out.Anchor = vesselStatusAnchor{
			Down: true, Lat: watch.Lat, Lon: watch.Lon, RadiusM: watch.RadiusMeters,
			SetAt: watch.SetAt.UTC().Format(time.RFC3339),
		}
		if noPosition == "" {
			out.Anchor.DistanceFromAnchorM = floatPtr(roundTo(haversineMeters(lat, lon, watch.Lat, watch.Lon), 0))
		} else {
			out.Anchor.DistanceMissing = noPosition + ", so the distance from the anchor is unknown"
		}
	}
	return out, nil
}

// engineStatus judges one engine. An engine whose readings have stopped is off
// only when the engine state says so (the key is off and the connection is
// alive, ADR 0163); silence on its own is unknown, not off.
func engineStatus(t telemetryReader, e vesselEngineSetting, info engineStateInfo) vesselStatusEngine {
	out := vesselStatusEngine{Name: e.Name}
	prefix := "propulsion." + e.Instance
	if info.State == engineStateOff {
		out.State = "off"
		return out
	}
	// Only .revolutions, which is what the Engine Cluster reads.
	hz, reason := freshNumber(t, prefix+".revolutions", vesselStatusFreshAge, "engine speed")
	if reason == "" {
		hz.Value *= 60
	}
	if reason != "" {
		out.Missing = reason
		if info.State == engineStateLost {
			out.Missing = "the engine's readings and its connection have both stopped"
		}
		return out
	}
	rpm := roundTo(hz.Value, 0)
	age := roundTo(hz.Age.Seconds(), 1)
	out.RPM = statusValue{Value: &rpm, Unit: "rpm", Display: &rpm, DisplayUnit: "rpm", AgeSeconds: &age}
	out.State = "off"
	if rpm > 0 {
		out.State = "running"
	}
	return out
}

// ── alarms and anomalies ─────────────────────────────────────────────────

func (d assistantToolDeps) vesselStatusAlarmsFor(alarms []alarmStatus) vesselStatusAlarms {
	out := vesselStatusAlarms{ActiveCount: len(alarms), Active: []vesselStatusAlarm{}, Ignored: d.ignoredAlarms()}
	if out.Ignored == nil {
		out.Ignored = []string{}
	}
	out.IgnoredCount = len(out.Ignored)
	for i, a := range alarms {
		if i == vesselStatusMaxAlarms {
			break
		}
		out.Active = append(out.Active, vesselStatusAlarm{Title: a.Label, Message: a.Message, State: a.State, Phase: string(a.Phase), Silenced: a.Silenced})
	}
	return out
}

func (d assistantToolDeps) vesselStatusAnomaliesFor(now time.Time) vesselStatusAnomalies {
	reading, ok := d.anomalyReading()
	if !ok {
		return vesselStatusAnomalies{Missing: "the sensor checks have not run yet"}
	}
	if age := now.Sub(reading.ComputedAt); age > anomalySlotMaxAge {
		return vesselStatusAnomalies{Missing: fmt.Sprintf("the sensor checks have stopped (the last ran %s ago)", describeAge(age))}
	}
	counts := []string{anomalySensorFrozenCountPath, anomalySensorOutOfRangeCountPath, anomalySensorSilentSourceCountPath}
	reported := false
	for _, p := range counts {
		if _, has := reading.Values[p]; has {
			reported = true
		}
	}
	if !reported {
		return vesselStatusAnomalies{Missing: "the sensor checks have not reported a count"}
	}
	texts := func(path string) []string {
		var out []string
		for _, e := range reading.Sensors[path] {
			if e.Text != "" {
				out = append(out, e.Text)
			}
		}
		return out
	}
	var silent []string
	for _, e := range reading.Sensors[anomalySensorSilentSourceCountPath] {
		silent = append(silent, e.Name)
	}
	return vesselStatusAnomalies{
		Frozen:            int(reading.Values[anomalySensorFrozenCountPath]),
		OutOfRange:        int(reading.Values[anomalySensorOutOfRangeCountPath]),
		SilentSources:     int(reading.Values[anomalySensorSilentSourceCountPath]),
		FrozenSensors:     texts(anomalySensorFrozenCountPath),
		OutOfRangeSensors: texts(anomalySensorOutOfRangeCountPath),
		SilentSourceNames: silent,
	}
}

// ── planned stay ─────────────────────────────────────────────────────────

func (d assistantToolDeps) vesselStatusStayFor(lat, lon float64, noPosition string, now time.Time) vesselStatusStay {
	store, err := d.recordStore("get_vessel_status")
	if err != nil {
		return vesselStatusStay{Missing: "the records are not available"}
	}
	stay, ok, err := store.GetPlannedStay()
	if err != nil {
		return vesselStatusStay{Missing: "the planned stay could not be read: " + err.Error()}
	}
	switch {
	case !ok:
		return vesselStatusStay{Missing: "no stay is planned"}
	case !plannedStayActive(stay, now):
		return vesselStatusStay{Missing: "the last planned stay ended before today"}
	}
	out := vesselStatusStay{Name: stay.Name, Lat: stay.Lat, Lon: stay.Lon, Arrive: stay.Arrive, Depart: stay.Depart}
	if noPosition != "" {
		out.DistanceMissing = noPosition + ", so the distance to the stay is unknown"
		return out
	}
	metres := haversineMeters(lat, lon, stay.Lat, stay.Lon)
	out.DistanceFromBoatM = floatPtr(roundTo(metres, 0))
	out.DistanceFromBoatNM = floatPtr(roundTo(metres/metersPerNauticalMile, 1))
	out.BearingTrueDeg = floatPtr(math.Round(bearingDeg(lat, lon, stay.Lat, stay.Lon)))
	return out
}

// ── observations ─────────────────────────────────────────────────────────

func (d assistantToolDeps) vesselObservationsFor(now time.Time) ([]vesselObservation, []string) {
	observations := []vesselObservation{}
	var unchecked []string

	night, reason := d.overnight(now)
	switch {
	case reason != "":
		unchecked = append(unchecked, "overnight house bank draw: "+reason)
	case night.Basis != "history":
		why := "there is no overnight history to compare against"
		if night.Reason != nil {
			why = *night.Reason
		}
		unchecked = append(unchecked, "overnight house bank draw: "+why)
	case night.LastNightRatePercentPerHour == nil:
		unchecked = append(unchecked, "overnight house bank draw: the last completed night has no usable record (a gap in the data, shore power, the generator, or the bank charging)")
	case len(night.PriorNightRatesPercentPerHour) < observationMinPriorNights:
		unchecked = append(unchecked, fmt.Sprintf("overnight house bank draw: only %d earlier usable nights, need %d",
			len(night.PriorNightRatesPercentPerHour), observationMinPriorNights))
	default:
		if o := overnightDrawObservation(night); o != nil {
			observations = append(observations, *o)
		}
	}
	return observations, unchecked
}

// overnightDrawObservation compares the last completed night's house bank
// drain with the median of the usable nights before it. It returns nil unless
// there are at least observationMinPriorNights earlier nights and last night
// drained at least observationDrawRatio times as fast and
// observationMinExtraPercentPerHour faster than typical. The summary states the
// figures and offers no cause.
func overnightDrawObservation(r overnightResult) *vesselObservation {
	if r.LastNightRatePercentPerHour == nil || len(r.PriorNightRatesPercentPerHour) < observationMinPriorNights {
		return nil
	}
	drains := make([]float64, len(r.PriorNightRatesPercentPerHour))
	for i, s := range r.PriorNightRatesPercentPerHour {
		drains[i] = -s
	}
	typical, ok := medianNightRate(drains)
	if !ok || typical <= 0 {
		return nil
	}
	last := -*r.LastNightRatePercentPerHour
	if last < typical*observationDrawRatio || last-typical < observationMinExtraPercentPerHour {
		return nil
	}
	o := &vesselObservation{
		Kind:                    "overnight_house_draw",
		LastNightPercentPerHour: roundTo(last, 2),
		TypicalPercentPerHour:   roundTo(typical, 2),
		Ratio:                   roundTo(last/typical, 2),
		NightsCompared:          len(drains),
	}
	if !r.LastNightStart.IsZero() {
		o.NightStart = r.LastNightStart.UTC().Format(time.RFC3339)
	}
	o.nightStart, o.nightEnd = r.LastNightStart, r.LastNightEnd
	o.bankPath = strings.TrimSuffix(r.SocPath, ".capacity.stateOfCharge")
	o.Summary = fmt.Sprintf("On the last completed night the house bank drained at %.2f %% of charge an hour, %.1f times the %.2f %% an hour it usually drains over the %d nights before.",
		o.LastNightPercentPerHour, o.Ratio, o.TypicalPercentPerHour, o.NightsCompared)
	return o
}

// ── registry record ──────────────────────────────────────────────────────

var mateToolGetVesselStatus = mateTool{
	name: "get_vessel_status",
	function: openRouterFunctionDef{
		Description: "Read one snapshot of the boat as it is now, built from the operator's own roles and equipment: " +
			"power (house bank charge, voltage and current, solar watts, DC load), every tank with its level and " +
			"alarm levels, the surroundings (depth, true wind and gust, barometric pressure and its three hour " +
			"change), the engines and the anchor, the active and ignored alarms, the sensor checks (frozen, " +
			"impossible and silent sensors), the planned stay with its distance from the boat, and " +
			"observations: odd things below an alarm, such as the house bank draining faster overnight than usual. " +
			"Use it for \"status\", \"how's the boat\", \"quick update\" and the like, and copy its figures: values are " +
			"SI with a display figure in the operator's units. A reading that cannot be given has a missing reason " +
			"and no value, and an empty observations list does not cover anything listed in observations_unchecked.",
		Parameters: json.RawMessage(`{"type": "object", "properties": {}}`),
	},
	execute: assistantToolDeps.executeGetVesselStatus,
	describe: func(args json.RawMessage) string {
		return "Reading the boat's status…"
	},
}

// ── production wiring ────────────────────────────────────────────────────

// liveOwnShipTelemetry is the own-ship reader over the delta-stream snapshot.
func liveOwnShipTelemetry(now time.Time) (telemetryReader, error) {
	t := globalSignalKSnapshot.selfTelemetryAt(now)
	if !t.HasSelf() {
		return nil, errSignalKNoSelf
	}
	return t, nil
}

// recentTrueWindGustKts is the Wind tile's true-wind gust for the last ten
// minutes, from the in-memory buffer (ADR 0130).
func recentTrueWindGustKts() (float64, bool) {
	kts, ok := computeMaxGustTrueKtsFor([]string{vesselStatusGustWindow})[vesselStatusGustWindow]
	return kts, ok && kts >= 0
}

// currentAnchorWatch copies the anchor watch out from under its lock.
func currentAnchorWatch() (anchorWatchData, bool, string) {
	anchorWatchMu.RLock()
	defer anchorWatchMu.RUnlock()
	if anchorWatchState == nil {
		return anchorWatchData{}, false, anchorWatchLoadErr
	}
	return *anchorWatchState, true, ""
}

// overnightForStatus is the dawn projection's own result, from the same cache
// the Battery & Power tile fills, so the two never disagree about the nights.
// The reason is set when the nights cannot be looked at at all.
func overnightForStatus(now time.Time) (overnightResult, string) {
	paths, err := overnightMeasurementPaths()
	if err != nil {
		return overnightResult{}, "the roles are unreadable: " + err.Error()
	}
	if paths.Soc == "" {
		return overnightResult{}, "no house bank role set"
	}
	state, err := fetchSignalKVesselState()
	lat, lon, ok := positionForSun(state)
	if err != nil || !ok {
		return overnightResult{}, "no usable boat position, which the night windows need"
	}
	result := globalOvernightCache.get(now, lat, lon, paths.cacheKey(), func() overnightResult {
		return computeOvernight(now, lat, lon, paths, dawnLinearFallbackEnabled(), influxTelemetryConfigured(), queryInfluxPathRange)
	})
	return result, ""
}
