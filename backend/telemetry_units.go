package main

import (
	"fmt"
	"strings"
)

// Charting units. A logged series is in SignalK's SI unit; the chart card and
// Mate's summary show the operator's display unit. The SI unit of a path is
// what the boat declares (unitForAlarmPath: SignalK's meta.units, or a derived
// path's own unit), never guessed from the path's name. A path with no
// declared unit, or one whose declared unit has no display mapping here, is
// shown in its stored unit and is not converted.

// chartSIQuantity maps a declared SI unit to the quantities.go quantity it
// measures.
var chartSIQuantity = map[string]string{
	"ratio": "ratio", "Hz": "frequency", "K": "temperature", "Pa": "pressure",
	"m3/s": "volumetricFlow", "m/s": "speed", "m": "length",
	"A": "current", "V": "potential", "W": "power",
}

// chartUnitLabels are the display labels that differ from a unit's id.
var chartUnitLabels = map[string]string{
	"percent": "%", "C": "°C", "F": "°F", "Lph": "L/h", "gph": "gal/h",
}

func chartUnitLabel(id string) string {
	if l, ok := chartUnitLabels[id]; ok {
		return l
	}
	return id
}

// chartDisplayUnitID is the unit the operator reads a quantity in. system is
// the Settings "units" choice, "metric" or "imperial"; anything else reads as
// metric, as on the frontend.
func chartDisplayUnitID(quantity, system string) string {
	imperial := system == "imperial"
	switch quantity {
	case "ratio":
		return "percent"
	case "frequency":
		return "rpm"
	case "temperature":
		if imperial {
			return "F"
		}
		return "C"
	case "pressure":
		if imperial {
			return "psi"
		}
		return "kPa"
	case "volumetricFlow":
		if imperial {
			return "gph"
		}
		return "Lph"
	case "speed":
		return "kn"
	case "length":
		if imperial {
			return "ft"
		}
		return "m"
	case "power":
		return "W"
	case "current":
		return "A"
	case "potential":
		return "V"
	}
	return ""
}

// chartDisplayFor resolves the unit a series is shown in, from the SI unit the
// boat declares for it. It returns the display label and the conversion from
// SI. An undeclared unit ("") or one with no display mapping keeps the
// declared unit, unconverted.
func chartDisplayFor(path, siUnit, system string) (string, func(float64) float64, error) {
	identity := func(v float64) float64 { return v }
	// Hz is the SI unit of every frequency, but only an engine's revolutions
	// are read in rpm; a mains frequency stays in hertz.
	if siUnit == "Hz" && !strings.HasSuffix(path, ".revolutions") {
		return siUnit, identity, nil
	}
	q, ok := chartSIQuantity[siUnit]
	if !ok {
		return siUnit, identity, nil
	}
	label := chartUnitLabel(chartDisplayUnitID(q, system))
	conv, err := chartConvertFromSI(siUnit, "", label)
	if err != nil {
		return "", nil, err
	}
	return label, conv, nil
}

// chartConvertFromSI converts an SI value of path, declared as siUnit, into the
// display unit label (or id) unit. An empty unit means no conversion. Asking to
// convert a path whose unit the boat does not declare is an error.
func chartConvertFromSI(siUnit, path, unit string) (func(float64) float64, error) {
	if unit == "" {
		return func(v float64) float64 { return v }, nil
	}
	if siUnit == "" {
		return nil, fmt.Errorf("the boat does not report a unit for %s", path)
	}
	// Asking for the unit the path is already in is no conversion, whatever
	// the unit: a declared unit with no display mapping (rad, s, J) keeps it.
	if unit == siUnit || unit == chartUnitLabel(siUnit) {
		return func(v float64) float64 { return v }, nil
	}
	q, ok := chartSIQuantity[siUnit]
	if !ok {
		return nil, fmt.Errorf("cannot convert %s to %q: it is reported in %s, which has no display unit", path, unit, siUnit)
	}
	for _, sq := range siQuantities {
		if sq.ID != q {
			continue
		}
		for _, u := range sq.Units {
			if u.ID == unit || chartUnitLabel(u.ID) == unit {
				return u.FromSI, nil
			}
		}
	}
	return nil, fmt.Errorf("cannot convert %s to %q: not a unit of %s", path, unit, q)
}
