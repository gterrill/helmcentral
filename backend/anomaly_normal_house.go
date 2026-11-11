package main

import (
	"fmt"
	"strings"
	"time"
)

// The house bank as the second subsystem of the learned normal (ADR 0276): one
// subject, "house", judged on its voltage and current beside what is charging it.
// A regime is the set of things charging (shore power, solar, the generator, the
// engines) and a quarter of the state of charge, so a bank on solar at 70 % is
// compared with itself on solar at 70 %.

const (
	normalSubsystemElectrical = "electrical"
	normalHouseSubject        = "house"
	normalHouseName           = "House bank"

	// normalHouseSteadyMinutes is how long what is charging must have stayed the
	// same, minute after minute, before a minute is judged.
	normalHouseSteadyMinutes = 10
	// normalSolarOffWatts is where solar stops charging once it has started, so cloud
	// near the edge does not split a spell.
	normalSolarOffWatts = 50.0
	// normalSolarOnWatts says the solar controllers are charging. The shore and
	// inverter thresholds are the overnight model's own (shorePowerPresentAmps,
	// inverterChargingPresentWatts), and the engines are charging while turning.
	normalSolarOnWatts = 100.0
)

// normalSolarHold is how long solar stays on in the band between off and on after
// it last read on. Scoring reads the hold and the steady window before its window,
// so the first minutes are classified and gated as they were in training.
const normalSolarHold = 15 * time.Minute

// normalHouseLeadIn is that: the hold, then the minutes the gate looks back over.
const normalHouseLeadIn = normalSolarHold + normalHouseSteadyMinutes*time.Minute

// House sources, in the order a regime names them.
const (
	houseShore = 1 << iota
	houseSolar
	houseInverter
	houseEngines
)

// houseSOCBands are the four charge bands, as a ratio, each from its floor up.
var houseSOCBands = []struct {
	floor float64
	key   string
	label string
}{
	{0, "soc0", "under 40 % charge"},
	{0.4, "soc40", "40 to 60 % charge"},
	{0.6, "soc60", "60 to 80 % charge"},
	{0.8, "soc80", "80 to 100 % charge"},
}

var normalHouseWording = normalWording{
	BandNamePrefix: "the house bank ",
	Steady:         "steady minutes",
	While:          "while the bank was steady",
	Unsteady:       "what was charging kept changing",
	This:           "the house bank",
	Gone:           "The house bank is no longer set up. It may have been changed or removed.",
}

func normalElectricalSubsystem() normalSubsystem {
	return normalSubsystem{
		Name:         normalSubsystemElectrical,
		Title:        normalHouseName,
		NoSubjects:   "No house bank is chosen",
		Wording:      normalHouseWording,
		Subjects:     normalHouseSubjects,
		Regime:       normalHouseRegime,
		Gate:         normalHouseGate,
		Run:          normalHouseRuns,
		PathMatters:  normalHousePathMatters,
		AlarmReading: normalHouseAlarmReading,
		Annotate:     normalHouseAnnotate,
		LeadIn:       normalHouseLeadIn,
		Active:       func(normalMinute) bool { return true },
	}
}

// houseChargingSource is one thing that charges the bank: the root of its
// SignalK node and the readings taken from it.
type houseChargingSource struct {
	root     string
	features []normalFeature
}

// normalHouseSubjects is the one house bank, when one is chosen. Its readings are
// the bank's own and those of the charging sources the boat has set up: nothing
// is offered for a part the boat does not have.
//
// Which sources are on is placed from steady readings only: the engines'
// revolutions, the inverter's AC input, the shore AC input and solar. The
// alternators, the charger and the inverter's load are readings the bank is
// explained by, fitted only in the bands where they report.
func normalHouseSubjects(vessel vesselView) []normalSubject {
	bank := vessel.houseBankPath()
	if bank == "" {
		return nil
	}
	// MinChange is the smallest change worth a look, with a reason each: a twentieth
	// of a volt, two amps (a shunt's noise on a quiet bank), a point of charge, and
	// for a charging source's output the size of a small load switching.
	features := []normalFeature{
		{Key: "voltage", Path: bank + ".voltage", SIUnit: "V", Name: "House bank voltage", Explain: true, Required: true, MinChange: 0.05},
		{Key: "current", Path: bank + ".current", SIUnit: "A", Name: "House bank current", Explain: true, Required: true, MinChange: 2},
		// Charge says where on its range the bank sits. It is never named as the
		// reason a minute is unusual.
		{Key: "soc", Path: vessel.houseBankSocPath(), SIUnit: "ratio", Name: "House bank state of charge", Required: true, AllowConstant: true, MinChange: 0.01},
	}
	// Every source reading only places the minute: it is never fitted or judged, and
	// a minute without it is a minute with that source off. An engine publishes
	// nothing while it is off, the charger no shore input while the dock is
	// unplugged, and a solar controller nothing at night. Any engine turning means
	// the engines are charging.
	for _, e := range vessel.Engines {
		if e.Instance != "" {
			features = append(features, normalFeature{
				Key: "rpm." + e.Instance, Path: "propulsion." + e.Instance + ".revolutions", SIUnit: engineRevolutionsUnit,
				Name: "Engine " + e.Instance + " revolutions", Placing: true,
			})
		}
	}
	if len(vessel.Solar) == 0 {
		features = append(features, normalFeature{Key: "solar", Path: solarMeasurement(), SIUnit: "W", Name: "Solar power", Placing: true})
	}
	for _, s := range vessel.Solar {
		features = append(features, normalFeature{
			Key: "solar." + s.Instance, Path: "electrical.solar." + s.Instance + ".panelPower", SIUnit: "W",
			Name: solarFeatureName(vessel, s.Instance), Placing: true,
		})
	}

	var sources []houseChargingSource
	// The alternators are read one by one and judged as one reading, their sum, so
	// motoring on one engine is not a sender gone quiet. A minute where none reports
	// has no reading.
	var alternators []string
	for _, a := range []struct{ instance, name string }{{vessel.Alternators.PortInstance, "Port"}, {vessel.Alternators.StarboardInstance, "Starboard"}} {
		if a.instance != "" {
			root := sourcePath(sourceAlternator, a.instance)
			key := "alternator." + a.instance
			alternators = append(alternators, key)
			sources = append(sources, houseChargingSource{root, []normalFeature{
				{Key: key, Path: root + ".current", SIUnit: "A", Name: a.name + " alternator current", Component: true},
			}})
		}
	}
	if id := vessel.Electrical.ShoreChargerInstance; id != "" {
		root := sourcePath(sourceCharger, id)
		sources = append(sources, houseChargingSource{root, []normalFeature{
			// Shore power is on when the charger's AC input draws current. The charger
			// publishes no AC input while the dock is unplugged, so a missing reading
			// is unplugged, as the overnight model reads it (shorePowerPresentAmps).
			{Key: "shoreIn", Path: root + ".acin.1.current", SIUnit: "A", Name: "Shore power input current", Placing: true},
			{Key: "charger", Path: root + ".current", SIUnit: "A", Name: "Shore charger current", Explain: true, PerRegime: true, MinChange: 2},
		}})
	}
	if id := vessel.Electrical.InverterInstance; id != "" {
		root := sourcePath(sourceInverter, id)
		inverter := []normalFeature{{Key: "inverterIn", Path: root + ".acin.power", SIUnit: "W", Name: "Inverter AC input power", Placing: true}}
		// The inverter's output is published under one of three names; the first
		// with data is the one used.
		for _, alt := range []struct{ key, path string }{
			{"acLoad.output", root + ".ac.output.power"}, {"acLoad.acout", root + ".acout.power"}, {"acLoad.acOutPw", root + ".acOutputPower"},
		} {
			inverter = append(inverter, normalFeature{Key: alt.key, Path: alt.path, SIUnit: "W", Name: "Inverter AC load", Explain: true, PerRegime: true, MinChange: 50, Group: "acLoad"})
		}
		sources = append(sources, houseChargingSource{root, inverter})
	}

	subject := normalSubject{
		ID: normalHouseSubject, Name: normalHouseName, EquipmentIDs: houseEquipmentIDs(vessel),
		AlarmPaths:         []string{bank, anomalyBatteryFullBankChargingPath},
		ACInputIsGenerator: vessel.Electrical.GeneratorInstance != "",
	}
	// A solar controller and the generator can raise alarms that bear on the bank.
	for _, s := range vessel.Solar {
		subject.AlarmPaths = append(subject.AlarmPaths, sourcePath(sourceSolar, s.Instance))
	}
	if id := vessel.Electrical.GeneratorInstance; id != "" {
		subject.AlarmPaths = append(subject.AlarmPaths, sourcePath(sourceGenerator, id))
	}
	subject.Features = features
	for _, src := range sources {
		subject.Features = append(subject.Features, src.features...)
		subject.AlarmPaths = append(subject.AlarmPaths, src.root)
	}
	if len(alternators) > 0 {
		subject.Features = append(subject.Features, normalFeature{
			Key: "alternator", SIUnit: "A", Name: "Alternator current", Explain: true, PerRegime: true, MinChange: 2, SumOf: alternators,
		})
	}
	return []normalSubject{subject}
}

// houseEquipmentIDs are the bank's item and the items of every role that charges
// it, so a repair to any of them leaves out the bank's fault range. The voltage
// readout battery is only a display choice and is left out.
func houseEquipmentIDs(vessel vesselView) []string {
	var out []string
	for _, spec := range roleSpecs {
		if spec.field == "voltage_readout_battery" {
			continue
		}
		if id := vessel.RoleItems[spec.field]; id != "" {
			out = append(out, id)
		}
	}
	for _, s := range vessel.Solar {
		if s.EquipmentID != "" {
			out = append(out, s.EquipmentID)
		}
	}
	return out
}

// solarFeatureName is a controller's power as the operator reads it: "Roof solar
// power". A controller with no name is called "Solar" and its instance.
func solarFeatureName(vessel vesselView, id string) string {
	name := vessel.solarName(id)
	if name == "" {
		name = "Solar " + id
	}
	if strings.Contains(strings.ToLower(name), "solar") {
		return name + " power"
	}
	return name + " solar power"
}

// normalHousePathMatters says a path is one the bank is judged by on this boat and
// the operator can switch off: not a placing reading, and not another battery.
func normalHousePathMatters(vessel vesselView, path string) bool {
	for _, subject := range normalHouseSubjects(vessel) {
		for _, f := range subject.Features {
			if !f.Placing && f.Path == path {
				return true
			}
		}
	}
	return false
}

// normalHouseAlarmReading says which reading an alarm is about, for the one rule:
// any alarm under the bank, a charging source, solar or the generator takes the
// bank's minutes out, a BMS fault and a charger or solar fault included. The
// exception is the operator's own rule for the bank's voltage or charge going
// below a limit: low is the bank being used, and taking those minutes out would
// keep the low bands from ever being learned. A high alarm there, one the boat's
// own monitor raised, or one whose rule has since gone is a fault.
func normalHouseAlarmReading(s normalSubject, alarm alarmLogEntry) (string, bool) {
	path := strings.TrimPrefix(alarm.Path, "notifications.")
	for _, f := range s.Features {
		if (f.Key == "voltage" || f.Key == "soc") && f.Path == path {
			if rule, ok := getAlarmRule(alarm.RuleID); ok && rule.Op == alarmOpBelow {
				return "", true
			}
			return "", false
		}
	}
	return "", false
}

// normalHouseAnnotate classifies each minute's sources once, in order. Solar turns
// on at normalSolarOnWatts and stays on in the band down to normalSolarOffWatts,
// for no longer than normalSolarHold after it last read on and not across a gap
// longer than a run's, so a minute is classified the same way in training and in
// scoring, whose lead-in is the hold. A missing solar reading is off.
func normalHouseAnnotate(minutes []normalMinute) {
	var lastHigh, prev time.Time
	for i := range minutes {
		m := minutes[i]
		if !prev.IsZero() && m.At.Sub(prev) > normalRunGap {
			lastHigh = time.Time{}
		}
		prev = m.At
		mask := 0
		solar := 0.0
		for key, v := range m.Values {
			if key == "solar" || strings.HasPrefix(key, "solar.") {
				solar += v
			}
			if strings.HasPrefix(key, "rpm.") && v > 0 {
				mask |= houseEngines
			}
		}
		if m.Values["shoreIn"] > shorePowerPresentAmps {
			mask |= houseShore
		}
		if m.Values["inverterIn"] > inverterChargingPresentWatts {
			mask |= houseInverter
		}
		switch {
		case solar >= normalSolarOnWatts:
			lastHigh = m.At
		case solar < normalSolarOffWatts:
			lastHigh = time.Time{}
		}
		if !lastHigh.IsZero() && m.At.Sub(lastHigh) <= normalSolarHold {
			mask |= houseSolar
		}
		minutes[i].Class, minutes[i].Classified = mask, true
	}
}

// normalHouseRegime places a minute by what is charging and the state of charge:
// "house/solar+gen/soc60" for "on solar and generator, 60 to 80 % charge".
func normalHouseRegime(s normalSubject, m normalMinute) (key, label string, ok bool) {
	soc, has := m.Values["soc"]
	if !has {
		return "", "", false
	}
	band := houseSOCBands[0]
	for i := len(houseSOCBands) - 1; i >= 0; i-- {
		if soc >= houseSOCBands[i].floor {
			band = houseSOCBands[i]
			break
		}
	}
	// The key is the same whether the inverter's input is called the generator or
	// AC input, so choosing a generator relabels a band and does not lose it.
	inverterKey, inverterLabel := "inv", "AC input"
	if s.ACInputIsGenerator {
		inverterLabel = "generator"
	}
	sources := m.Class
	var keys, labels []string
	for _, src := range []struct {
		bit        int
		key, label string
	}{
		{houseShore, "shore", "shore power"}, {houseSolar, "solar", "solar"},
		{houseInverter, inverterKey, inverterLabel}, {houseEngines, "engines", "engines"},
	} {
		if sources&src.bit != 0 {
			keys, labels = append(keys, src.key), append(labels, src.label)
		}
	}
	if len(keys) == 0 {
		return fmt.Sprintf("%s/none/%s", s.ID, band.key), "with nothing charging, " + band.label, true
	}
	return fmt.Sprintf("%s/%s/%s", s.ID, strings.Join(keys, "+"), band.key), "on " + listAnd(labels) + ", " + band.label, true
}

// normalHouseGate passes a minute when the ten minutes ending at it follow one
// another and had the same things charging. Every minute carries the bank's
// readings (joinNormalMinutes), so a missing minute is a hole in the times.
func normalHouseGate(minutes []normalMinute) []bool {
	out := make([]bool, len(minutes))
	for i := normalHouseSteadyMinutes - 1; i < len(minutes); i++ {
		steady := true
		for k := i - normalHouseSteadyMinutes + 1; k < i; k++ {
			if minutes[i].At.Sub(minutes[k].At) != time.Duration(i-k)*time.Minute || minutes[k].Class != minutes[i].Class {
				steady = false
				break
			}
		}
		out[i] = steady
	}
	return out
}

// normalHouseRuns numbers the bank's runs: an unbroken spell of one set of
// sources, where a gap of a few minutes (a gateway reconnecting) does not end it.
func normalHouseRuns(minutes []normalMinute) []int {
	out := make([]int, len(minutes))
	run := 0
	for i, m := range minutes {
		if i == 0 || m.At.Sub(minutes[i-1].At) > normalRunGap || minutes[i].Class != minutes[i-1].Class {
			run++
		}
		out[i] = run
	}
	return out
}
