package main

import "fmt"

// Unit conversion, mirroring frontend/src/lib/quantities.ts in the toSI
// direction.
//
// The backend needed none of this until gauge zones became the alarm source
// (ADR 0050). Zones are authored in display units — oil pressure in psi,
// temperature in °C — because that is what the operator sets them against on
// the gauge scale. alarmReader reads SI from the SignalK snapshot: pascals,
// kelvin. Comparing one against the other produces an alarm that never fires
// and says nothing about why, so the threshold has to be converted once, here.
//
// Both directions are defined per unit and round-trip-tested against each
// other, and the table is asserted against the same values as the frontend's
// quantities.test.ts, so the two cannot drift apart silently.

type siUnitOption struct {
	ID string
	// FromSI matches the frontend's fromSI exactly; ToSI is its inverse.
	FromSI func(float64) float64
	ToSI   func(float64) float64
}

type siQuantity struct {
	ID    string
	Units []siUnitOption
}

func siIdentity(v float64) float64 { return v }

// linear builds the common case: a unit that is the SI value scaled by a factor.
func linear(id string, perSI float64) siUnitOption {
	return siUnitOption{
		ID:     id,
		FromSI: func(v float64) float64 { return v * perSI },
		ToSI:   func(v float64) float64 { return v / perSI },
	}
}

func identityUnit(id string) siUnitOption {
	return siUnitOption{ID: id, FromSI: siIdentity, ToSI: siIdentity}
}

var siQuantities = []siQuantity{
	{ID: "pressure", Units: []siUnitOption{
		linear("kPa", 1.0/1000),
		linear("psi", 1.0/6894.757),
		linear("bar", 1.0/100000),
		identityUnit("Pa"),
	}},
	{ID: "temperature", Units: []siUnitOption{
		{ID: "C",
			FromSI: func(v float64) float64 { return v - 273.15 },
			ToSI:   func(v float64) float64 { return v + 273.15 }},
		{ID: "F",
			FromSI: func(v float64) float64 { return (v-273.15)*9/5 + 32 },
			ToSI:   func(v float64) float64 { return (v-32)*5/9 + 273.15 }},
		identityUnit("K"),
	}},
	{ID: "volumetricFlow", Units: []siUnitOption{
		linear("Lph", 1000*3600),
		linear("gph", 264.172*3600),
	}},
	{ID: "duration", Units: []siUnitOption{
		linear("h", 1.0/3600),
		linear("min", 1.0/60),
		identityUnit("s"),
	}},
	{ID: "ratio", Units: []siUnitOption{
		linear("percent", 100),
		identityUnit("ratio"),
	}},
	{ID: "volume", Units: []siUnitOption{
		linear("L", 1000),
		linear("gal", 264.172),
	}},
	{ID: "speed", Units: []siUnitOption{
		linear("kn", 1.943844),
		linear("kph", 3.6),
		identityUnit("mps"),
	}},
	{ID: "length", Units: []siUnitOption{
		identityUnit("m"),
		linear("ft", 3.28084),
	}},
	// Distance per unit volume, which is how SignalK states fuel economy.
	{ID: "fuelEconomy", Units: []siUnitOption{
		linear("nmpl", 1.0/1852000),
		linear("nmpg", 3.785411784/1852000),
		identityUnit("m/m3"),
	}},
	{ID: "power", Units: []siUnitOption{
		linear("kW", 1.0/1000),
		identityUnit("W"),
	}},
	{ID: "potential", Units: []siUnitOption{identityUnit("V")}},
	{ID: "current", Units: []siUnitOption{identityUnit("A")}},
	{ID: "frequency", Units: []siUnitOption{
		// SignalK publishes revolutions in Hz; every tachometer shows RPM.
		linear("rpm", 60),
		identityUnit("Hz"),
	}},
	{ID: "raw", Units: []siUnitOption{identityUnit("raw")}},
	// Difference quantities, for a residual between two readings rather than
	// an absolute one (the twin-engine differential detector). A difference
	// in kelvin is numerically the same difference in Celsius, so unlike
	// "temperature" above, deltaK only scales -- it must never carry K's
	// -273.15 offset, or a 4 K gap would render as -269.1 degC. deltaPa
	// mirrors it in kPa, the unit an operator reads an oil or boost pressure
	// gap in, not "pressure"'s mb (which is for an absolute barometric
	// reading).
	{ID: "temperatureDelta", Units: []siUnitOption{identityUnit("deltaC"), identityUnit("deltaK")}},
	{ID: "pressureDelta", Units: []siUnitOption{linear("deltaKPa", 1.0/1000), identityUnit("deltaPa")}},
}

// convertToSI turns a value in a gauge's display unit into the SI value
// SignalK publishes. An unknown quantity or unit is an error rather than a
// pass-through: silently comparing psi against pascals is the failure this
// exists to prevent.
func convertToSI(value float64, quantityID, unitID string) (float64, error) {
	for _, q := range siQuantities {
		if q.ID != quantityID {
			continue
		}
		for _, u := range q.Units {
			if u.ID == unitID {
				return u.ToSI(value), nil
			}
		}
		return 0, fmt.Errorf("unknown unit %q for quantity %q", unitID, quantityID)
	}
	return 0, fmt.Errorf("unknown quantity %q", quantityID)
}
