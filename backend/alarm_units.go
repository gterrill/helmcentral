package main

import "strconv"

// operatorUnit and formatAlarmReading mirror the frontend's own conversion
// table -- frontend/src/lib/alarm-display.ts (ALARM_UNIT_OVERRIDES,
// formatAlarmReading) and frontend/src/lib/quantities.ts (QUANTITIES) -- so
// an alarm message reads the same whether it lands on the dashboard card, an
// ntfy push, an alarm email, a webhook payload, or the SignalK notification
// an MFD shows. The rule itself evaluates in SI (a barometer rule fires at
// -0.03 Pa/s), which is meaningless to a sailor who thinks in mb/hr, knots
// and degrees C. These two tables must move together: adding or changing a
// unit on one side without the other means the card and the alarm text stop
// agreeing with each other.
type operatorUnitEntry struct {
	label    string
	convert  func(float64) float64
	decimals int
}

var operatorUnitTable = map[string]operatorUnitEntry{
	"Pa/s":  {"mb/hr", func(v float64) float64 { return v * 36 }, 1},
	"Pa":    {"mb", func(v float64) float64 { return v / 100 }, 1},
	"K":     {"°C", func(v float64) float64 { return v - 273.15 }, 1},
	"m/s":   {"kts", func(v float64) float64 { return v * 1.943844 }, 1},
	"ratio": {"%", func(v float64) float64 { return v * 100 }, 0},
	"Hz":    {"RPM", func(v float64) float64 { return v * 60 }, 0},
	"m3":    {"L", func(v float64) float64 { return v * 1000 }, 0},
	"m3/s":  {"L/h", func(v float64) float64 { return v * 3600000 }, 1},
	"s":     {"h", func(v float64) float64 { return v / 3600 }, 0},
	// deltaK and deltaPa are difference units, for a residual between two
	// readings rather than an absolute one (the twin-engine differential
	// detector). "K" above converts absolutely (subtracts 273.15), which is
	// correct for a temperature but wrong for a temperature gap: a 4 K
	// residual would print as -269.1 degC. A difference in kelvin equals the
	// same difference in Celsius, so deltaK only scales, never offsets.
	// deltaPa mirrors it in kPa rather than "Pa"'s mb, because that is the
	// unit an operator reads an oil or boost pressure gap in, not a
	// barometric one.
	"deltaK":  {"°C", func(v float64) float64 { return v }, 1},
	"deltaPa": {"kPa", func(v float64) float64 { return v / 1000 }, 1},
	"V":       {"V", func(v float64) float64 { return v }, 1},
	"A":       {"A", func(v float64) float64 { return v }, 1},
	"m":       {"m", func(v float64) float64 { return v }, 1},
	"W":       {"W", func(v float64) float64 { return v }, 0},
	"m/m3":    {"nm/L", func(v float64) float64 { return v / 1852000 }, 2},
}

// operatorUnit looks up the display unit for an SI unit string. ok is false
// for an SI unit this table does not know (including empty), in which case
// the caller falls back to a bare number rather than guessing a conversion.
func operatorUnit(siUnit string) (label string, convert func(float64) float64, decimals int, ok bool) {
	entry, found := operatorUnitTable[siUnit]
	if !found {
		return "", nil, 0, false
	}
	return entry.label, entry.convert, entry.decimals, true
}

// formatAlarmReading renders an SI value the way an operator reads it --
// "-1.1 mb/hr" instead of "-0.03 Pa/s", "27.0 °C" instead of "300.15 K". An
// SI unit this table does not recognise (including no unit at all) falls
// back to formatAlarmValue's plain rounding, the same bare-number formatting
// alarm messages used before this conversion existed.
func formatAlarmReading(value float64, siUnit string) string {
	label, convert, decimals, ok := operatorUnit(siUnit)
	if !ok {
		return formatAlarmValue(value)
	}
	return strconv.FormatFloat(convert(value), 'f', decimals, 64) + " " + label
}
