package main

import (
	"fmt"
	"sort"
	"strings"
)

// anomaly_battery.go implements the "full-bank charging" anomaly detector:
// alternators/chargers driving an already-full house bank higher, the
// failure mode behind the 2026-09-21 dead-ship event (BMS high-cell trip,
// remote-disconnect switch opened, Quattro dropped out). See
// TestFullBankChargingLevel_20260921Regression in anomaly_battery_test.go
// for that incident reconstructed as a regression scenario, and the
// anomaly-detection plan for full cycle context.
//
// This file owns only fullBankChargingLevel and its evidence formatting.
// The persisted vessel.house_bank settings block, the battery
// equipment-profile schema, and input-validity checking (frozen/stale/
// out-of-range/silent-source) are being built in parallel elsewhere in this
// worktree (vessel_settings.go, anomaly_sensor_health.go). To avoid a
// compile-time dependency on that concurrent, still-changing work, this file
// defines its own small input/settings structs below. A later integration
// phase maps the real persisted settings and a live SignalK read onto these
// fields - each field's doc comment states its unit and SignalK convention
// so that mapping is unambiguous.

// fullBankChargingInputs is a point-in-time snapshot of the chosen house
// bank's electrical state, plus whatever charge sources the boat happens to
// be publishing. It carries no SignalK paths or timestamps of its own -
// fullBankChargingLevel does no windowing; DVdtPerMinute is a value the
// caller already computed over a trailing window.
type fullBankChargingInputs struct {
	// SoC is the bank's state of charge as a ratio 0..1, matching
	// SignalK's own electrical.batteries.<id>.capacity.stateOfCharge
	// convention (1.0 is 100%, not 100).
	SoC float64

	// Voltage is the bank's pack voltage in volts
	// (electrical.batteries.<id>.voltage).
	Voltage float64

	// Current is the bank's own shunt current in amps, positive when the
	// bank is charging (electrical.batteries.<id>.current sign
	// convention). This is the "charge current" the detector uses -
	// derived from the bank itself, never summed from a list of sources.
	Current float64

	// DVdtPerMinute is the bank voltage's rate of change in volts per
	// minute, already computed by the caller over the trailing 60s window
	// (see the doc comment on fullBankChargingLevel - this file does no
	// voltage-history accumulation of its own). Zero means "unknown / not
	// yet computed", not "flat": a genuinely flat pack also reads zero, so
	// this field only ever contributes to level 2, it never blocks it.
	DVdtPerMinute float64

	// ChargeSources is discovered charge-producing readings, keyed by a
	// human label ("port alternator", "starboard alternator", "solar") to
	// their present current in amps. It is evidence-text enrichment only -
	// the level arithmetic below never reads it - typically populated by
	// walking whatever electrical.alternator.*.current,
	// electrical.chargers.*.current and solar paths the boat publishes,
	// not a configured per-boat list. Nil or empty is fine.
	ChargeSources map[string]float64
}

// fullBankChargingSettings is the operator-set/profile-derived pack
// thresholds fullBankChargingLevel evaluates against. All voltage fields
// are per-pack, not per-cell - a later phase multiplies a battery profile's
// per-cell thresholds by the cell count before calling in here.
type fullBankChargingSettings struct {
	// CapacityAh is the house bank's rated capacity in amp-hours. <= 0
	// means "house bank not configured" (fullBankChargingLevel returns
	// Ok = false).
	CapacityAh float64

	// FullSOC is the state-of-charge ratio (0..1, same convention as
	// fullBankChargingInputs.SoC) at/above which the bank is considered
	// "full" per the battery profile. <= 0 means "house bank not
	// configured".
	FullSOC float64

	// WarnVoltage is the pack voltage (volts) at/above which the bank is
	// running warm for a "should be finished charging" state - level 1's
	// voltage threshold, and, like FullSOC, one of the battery profile's
	// required slots: <= 0 means "house bank not configured" rather than
	// "no voltage threshold", since a real pack's voltage is always > 0
	// and a zero here can only mean the slot was never filled.
	WarnVoltage float64

	// HighVoltage is the pack voltage (volts) at/above which the bank is
	// unambiguously being driven too high - one of level 2's two
	// escalation conditions. Unlike WarnVoltage, HighVoltage is optional:
	// <= 0 disables the voltage half of level 2's OR condition (that slot
	// not yet filled) without affecting level 1 or the overall Ok gate;
	// only dV/dt can still promote to level 2 in that case.
	HighVoltage float64

	// BankPath is the SignalK path for the chosen house bank, e.g.
	// "electrical.batteries.512" (mirrors vessel.house_bank.path). It is
	// used only to build sub-paths for the optional validity check in
	// fullBankChargingLevel - never read for any electrical value, which
	// always comes from Inputs. Left empty, the validity check is skipped
	// even when a validity func is supplied.
	BankPath string
}

// fullBankChargingResult is fullBankChargingLevel's return value.
type fullBankChargingResult struct {
	// Level is 0 (no condition asserted), 1 (full bank still taking
	// meaningful charge) or 2 (level 1 plus overvoltage or a still-rising
	// pack under charge).
	Level int

	// Evidence is a human-readable sentence for an alarm card or
	// notification, e.g. "House bank 96% SoC, 28.90 V, charging 42 A
	// (port alternator 21 A, starboard alternator 19 A)". Populated
	// whenever Ok is true and the inputs passed the validity check,
	// regardless of Level - a level-0 evaluation still has a sentence to
	// show if a caller wants it, though only Level >= 1 is alarm-worthy.
	// Empty when Ok is false, or when a validity check rejected an input.
	Evidence string

	// Ok is false when settings describes an incomplete/unconfigured
	// house bank (see fullBankChargingLevel's doc comment). Level and
	// Evidence are not meaningful when Ok is false. This is the flag a
	// later phase uses to decide whether to seed this detector's alarm
	// rules at all - "not set up" is a persistent, settings-driven state,
	// not a per-tick one, which is why an invalid *input* (see
	// fullBankChargingValidityFunc) does not also clear Ok: the bank is
	// still configured, only this tick's reading is untrusted.
	Ok bool
}

// fullBankChargingValidityFunc reports whether a bank electrical sub-path
// (e.g. "electrical.batteries.512.voltage") currently holds a trustworthy
// reading - not absent, stale, out-of-range, frozen or a silent source.
// This is exactly what anomaly_sensor_health.go's inputValidity will provide
// once that file exists; fullBankChargingLevel takes it as a plain
// collaborator so this file has no compile-time dependency on that
// concurrent, still-changing work. A nil func means "trust every input",
// which is what this file's own tests use, and what any caller built before
// the sensor-health phase lands gets by default.
type fullBankChargingValidityFunc func(path string) bool

// fullBankChargingLevel implements the "full-bank charging" detector: an
// already-full house bank still taking meaningful charge from its
// alternators/chargers.
//
// ok is false when settings has no usable house bank configured
// (CapacityAh, FullSOC or WarnVoltage <= 0 - see their doc comments above).
// Callers must check Ok before using Level/Evidence; this is what backs the
// plan's "incomplete house_bank gives 'not set up' and nothing seeded"
// requirement.
//
// When settings.BankPath is set and valid is non-nil, the bank's own SoC,
// voltage and current sub-paths are each checked with valid before any
// arithmetic runs. If any is untrusted, Ok stays true (the bank *is*
// configured) but Level is 0 and Evidence is empty - the plan's "a detector
// with an invalid input publishes nothing" rule. Leaving BankPath empty, or
// passing a nil valid func, skips this check entirely.
//
// Levels (all three level-1 conditions must hold; level 2 needs level 1
// plus one of its two OR conditions):
//
//  1. inputs.SoC >= settings.FullSOC, inputs.Voltage >= settings.WarnVoltage,
//     and inputs.Current >= settings.CapacityAh * 0.02 (C/50 - amps, scales
//     with bank size, never a fixed threshold).
//  2. level 1, plus inputs.Voltage >= settings.HighVoltage (when
//     HighVoltage > 0) OR inputs.DVdtPerMinute > 0.02 (+20 mV/min).
//
// dV/dt: inputs.DVdtPerMinute is a precomputed rate the caller supplies
// (voltageHistoryTracker.observe, a least-squares fit over a trailing
// window -- see its own doc comment for why 20 mV/min, not the plan's
// original 5, is the threshold now: that was finer than a typical battery
// monitor's own reading resolution); this function does no voltage-history
// windowing itself - that accumulator belongs with the sensor-health
// frozen check, a later integration phase, not here.
func fullBankChargingLevel(inputs fullBankChargingInputs, settings fullBankChargingSettings, valid fullBankChargingValidityFunc) fullBankChargingResult {
	if settings.CapacityAh <= 0 || settings.FullSOC <= 0 || settings.WarnVoltage <= 0 {
		return fullBankChargingResult{}
	}

	if valid != nil && settings.BankPath != "" {
		for _, suffix := range []string{".capacity.stateOfCharge", ".voltage", ".current"} {
			if !valid(settings.BankPath + suffix) {
				return fullBankChargingResult{Ok: true}
			}
		}
	}

	chargeThreshold := settings.CapacityAh * 0.02 // C/50: 2% of bank capacity, scales with bank size
	level1 := inputs.SoC >= settings.FullSOC &&
		inputs.Voltage >= settings.WarnVoltage &&
		inputs.Current >= chargeThreshold

	level := 0
	if level1 {
		level = 1
		overvoltage := settings.HighVoltage > 0 && inputs.Voltage >= settings.HighVoltage
		rising := inputs.DVdtPerMinute > 0.02
		if overvoltage || rising {
			level = 2
		}
	}

	return fullBankChargingResult{
		Ok:       true,
		Level:    level,
		Evidence: formatFullBankChargingEvidence(inputs),
	}
}

// formatFullBankChargingEvidence renders the bank's state as an operator
// sentence, e.g. "House bank 96% SoC, 28.90 V, charging 42 A (port
// alternator 21 A, starboard alternator 19 A)", or without ChargeSources,
// "House bank 96% SoC, 28.90 V, charging 42 A".
//
// Rounding: SoC to the nearest whole percent, pack voltage to 2 decimals,
// current (bank and each discovered source) to the nearest whole amp - a
// banner reader needs the amp figure at a glance, not tenths. Discovered
// sources are listed alphabetically by label so the sentence is
// deterministic (Go map iteration order is not).
//
// This formatter assumes a charging (positive) bank current, matching the
// sign convention fullBankChargingInputs.Current documents; it is only ever
// exercised by fullBankChargingLevel at level 1 or above, where Current has
// already cleared the C/50 charge threshold.
func formatFullBankChargingEvidence(inputs fullBankChargingInputs) string {
	var b strings.Builder
	fmt.Fprintf(&b, "House bank %.0f%% SoC, %.2f V, charging %.0f A", inputs.SoC*100, inputs.Voltage, inputs.Current)

	if len(inputs.ChargeSources) > 0 {
		labels := make([]string, 0, len(inputs.ChargeSources))
		for label := range inputs.ChargeSources {
			labels = append(labels, label)
		}
		sort.Strings(labels)

		parts := make([]string, len(labels))
		for i, label := range labels {
			parts[i] = fmt.Sprintf("%s %.0f A", label, inputs.ChargeSources[label])
		}
		fmt.Fprintf(&b, " (%s)", strings.Join(parts, ", "))
	}

	return b.String()
}
