package main

import "testing"

// --- 1. Level edges: SoC / voltage / current boundaries, and C/50 scaling ---

// baseFullBankSettings is a plain, round-number pack configuration shared by
// the edge-boundary tests below: 400 Ah, full at 95% SoC, warn at 28.0 V,
// high at 28.8 V. Individual tests vary exactly one input around its
// threshold.
func baseFullBankSettings() fullBankChargingSettings {
	return fullBankChargingSettings{
		CapacityAh:  400,
		FullSOC:     0.95,
		WarnVoltage: 28.0,
		HighVoltage: 28.8,
	}
}

func TestFullBankChargingLevel_SoCBoundary(t *testing.T) {
	settings := baseFullBankSettings()
	below := fullBankChargingInputs{SoC: 0.94, Voltage: 28.0, Current: 10}
	at := fullBankChargingInputs{SoC: 0.95, Voltage: 28.0, Current: 10}
	above := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10}

	if got := fullBankChargingLevel(below, settings, nil).Level; got != 0 {
		t.Fatalf("SoC 0.94 (below FullSOC 0.95): level = %d, want 0", got)
	}
	if got := fullBankChargingLevel(at, settings, nil).Level; got != 1 {
		t.Fatalf("SoC 0.95 (at FullSOC): level = %d, want 1", got)
	}
	if got := fullBankChargingLevel(above, settings, nil).Level; got != 1 {
		t.Fatalf("SoC 0.96 (above FullSOC): level = %d, want 1", got)
	}
}

func TestFullBankChargingLevel_VoltageBoundary(t *testing.T) {
	settings := baseFullBankSettings()
	below := fullBankChargingInputs{SoC: 0.96, Voltage: 27.99, Current: 10}
	at := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10}
	above := fullBankChargingInputs{SoC: 0.96, Voltage: 28.01, Current: 10}

	if got := fullBankChargingLevel(below, settings, nil).Level; got != 0 {
		t.Fatalf("V 27.99 (below WarnVoltage 28.0): level = %d, want 0", got)
	}
	if got := fullBankChargingLevel(at, settings, nil).Level; got != 1 {
		t.Fatalf("V 28.0 (at WarnVoltage): level = %d, want 1", got)
	}
	if got := fullBankChargingLevel(above, settings, nil).Level; got != 1 {
		t.Fatalf("V 28.01 (above WarnVoltage): level = %d, want 1", got)
	}
}

// TestFullBankChargingLevel_CurrentScalesWithCapacity is the C/50 test: the
// charge-current threshold is 2% of bank capacity, not a fixed amp number,
// so it must move when capacity moves. 400 Ah -> 8 A; 1000 Ah -> 20 A.
func TestFullBankChargingLevel_CurrentScalesWithCapacity(t *testing.T) {
	cases := []struct {
		name       string
		capacityAh float64
		wantThresh float64
	}{
		{"400Ah bank", 400, 8},
		{"1000Ah bank", 1000, 20},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := fullBankChargingSettings{
				CapacityAh:  tc.capacityAh,
				FullSOC:     0.95,
				WarnVoltage: 28.0,
				HighVoltage: 28.8,
			}
			below := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: tc.wantThresh - 0.01}
			at := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: tc.wantThresh}

			if got := fullBankChargingLevel(below, settings, nil).Level; got != 0 {
				t.Fatalf("current just under C/50 (%.2f A of %.0f Ah): level = %d, want 0", below.Current, tc.capacityAh, got)
			}
			if got := fullBankChargingLevel(at, settings, nil).Level; got != 1 {
				t.Fatalf("current at C/50 (%.2f A of %.0f Ah): level = %d, want 1", at.Current, tc.capacityAh, got)
			}
		})
	}
}

// TestFullBankChargingLevel_Level2ViaHighVoltage covers the first half of
// level 2's OR condition: level 1's three conditions plus pack voltage at or
// above HighVoltage.
func TestFullBankChargingLevel_Level2ViaHighVoltage(t *testing.T) {
	settings := baseFullBankSettings() // HighVoltage 28.8

	justUnder := fullBankChargingInputs{SoC: 0.96, Voltage: 28.79, Current: 10}
	at := fullBankChargingInputs{SoC: 0.96, Voltage: 28.8, Current: 10}

	if got := fullBankChargingLevel(justUnder, settings, nil).Level; got != 1 {
		t.Fatalf("V 28.79 (just under HighVoltage 28.8): level = %d, want 1", got)
	}
	if got := fullBankChargingLevel(at, settings, nil).Level; got != 2 {
		t.Fatalf("V 28.8 (at HighVoltage): level = %d, want 2", got)
	}
}

// TestFullBankChargingLevel_Level2ViaDVdt covers the second half of level
// 2's OR condition: dV/dt strictly greater than +20 mV/min (0.02 V/min),
// with pack voltage held below HighVoltage so only the dV/dt path can fire.
// 20 mV/min, not the plan's original 5 mV/min: a typical battery monitor's
// own ~0.01V reading resolution means 5 mV/min was finer than the
// instrument itself can actually resolve, so a single quantization step
// was enough to read as "rising" (code review finding 10).
func TestFullBankChargingLevel_Level2ViaDVdt(t *testing.T) {
	settings := baseFullBankSettings() // HighVoltage 28.8

	atThreshold := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10, DVdtPerMinute: 0.02}
	aboveThreshold := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10, DVdtPerMinute: 0.0201}

	if got := fullBankChargingLevel(atThreshold, settings, nil).Level; got != 1 {
		t.Fatalf("dV/dt exactly +20 mV/min (not '>'): level = %d, want 1", got)
	}
	if got := fullBankChargingLevel(aboveThreshold, settings, nil).Level; got != 2 {
		t.Fatalf("dV/dt +20.1 mV/min (> threshold): level = %d, want 2", got)
	}
}

// --- 2. Golden evidence text, with and without discovered charge sources ---

func TestFormatFullBankChargingEvidence_WithSources(t *testing.T) {
	inputs := fullBankChargingInputs{
		SoC:     0.96,
		Voltage: 28.9,
		Current: 42.4,
		ChargeSources: map[string]float64{
			"port alternator":      21.3,
			"starboard alternator": 19.4,
		},
	}
	want := "House bank 96% SoC, 28.90 V, charging 42 A (port alternator 21 A, starboard alternator 19 A)"
	if got := formatFullBankChargingEvidence(inputs); got != want {
		t.Fatalf("evidence:\n got  %q\n want %q", got, want)
	}
}

func TestFormatFullBankChargingEvidence_WithoutSources(t *testing.T) {
	inputs := fullBankChargingInputs{
		SoC:     0.96,
		Voltage: 28.9,
		Current: 42.4,
	}
	want := "House bank 96% SoC, 28.90 V, charging 42 A"
	if got := formatFullBankChargingEvidence(inputs); got != want {
		t.Fatalf("evidence:\n got  %q\n want %q", got, want)
	}
}

// TestFormatFullBankChargingEvidence_SourcesAreSorted pins the ordering rule
// (alphabetical by label) so the evidence sentence is deterministic - map
// iteration order in Go is not, and a flaky evidence string would make the
// golden-text tests above flaky too.
func TestFormatFullBankChargingEvidence_SourcesAreSorted(t *testing.T) {
	inputs := fullBankChargingInputs{
		SoC:     0.9,
		Voltage: 28.5,
		Current: 30,
		ChargeSources: map[string]float64{
			"starboard alternator": 10,
			"solar":                5,
			"port alternator":      15,
		},
	}
	want := "House bank 90% SoC, 28.50 V, charging 30 A (port alternator 15 A, solar 5 A, starboard alternator 10 A)"
	if got := formatFullBankChargingEvidence(inputs); got != want {
		t.Fatalf("evidence:\n got  %q\n want %q", got, want)
	}
}

// fullBankChargingLevel's Result.Evidence must use the exact same formatter,
// not a second, drifting copy of the formatting logic.
func TestFullBankChargingLevel_ResultCarriesEvidence(t *testing.T) {
	settings := baseFullBankSettings()
	inputs := fullBankChargingInputs{
		SoC:     0.96,
		Voltage: 28.9,
		Current: 42.4,
		ChargeSources: map[string]float64{
			"port alternator":      21.3,
			"starboard alternator": 19.4,
		},
	}
	want := "House bank 96% SoC, 28.90 V, charging 42 A (port alternator 21 A, starboard alternator 19 A)"
	result := fullBankChargingLevel(inputs, settings, nil)
	if !result.Ok {
		t.Fatalf("expected Ok = true for a configured house bank")
	}
	if result.Evidence != want {
		t.Fatalf("Result.Evidence:\n got  %q\n want %q", result.Evidence, want)
	}
}

// --- 3. Incomplete settings: house bank not configured ---

func TestFullBankChargingLevel_NotConfigured(t *testing.T) {
	inputs := fullBankChargingInputs{SoC: 0.99, Voltage: 29.0, Current: 50}

	cases := []struct {
		name     string
		settings fullBankChargingSettings
	}{
		{"zero value settings", fullBankChargingSettings{}},
		{"CapacityAh unset", fullBankChargingSettings{CapacityAh: 0, FullSOC: 0.95, WarnVoltage: 28.0, HighVoltage: 28.8}},
		{"CapacityAh negative", fullBankChargingSettings{CapacityAh: -10, FullSOC: 0.95, WarnVoltage: 28.0, HighVoltage: 28.8}},
		{"FullSOC unset", fullBankChargingSettings{CapacityAh: 400, FullSOC: 0, WarnVoltage: 28.0, HighVoltage: 28.8}},
		{"WarnVoltage unset (a slot not yet filled)", fullBankChargingSettings{CapacityAh: 400, FullSOC: 0.95, WarnVoltage: 0, HighVoltage: 28.8}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := fullBankChargingLevel(inputs, tc.settings, nil)
			if result.Ok {
				t.Fatalf("expected Ok = false for %s", tc.name)
			}
		})
	}
}

// TestFullBankChargingLevel_HighVoltageOptional confirms HighVoltage is not
// part of the "not configured" gate the way CapacityAh/FullSOC/WarnVoltage
// are: a profile can ship level 1 (warn) before its high-voltage slot is
// filled. With HighVoltage unset, level can still reach 1, and can only
// reach 2 via dV/dt (never via the disabled voltage half of the OR).
func TestFullBankChargingLevel_HighVoltageOptional(t *testing.T) {
	settings := fullBankChargingSettings{CapacityAh: 400, FullSOC: 0.95, WarnVoltage: 28.0, HighVoltage: 0}
	inputs := fullBankChargingInputs{SoC: 0.99, Voltage: 40, Current: 50} // absurdly high V - would trip a real HighVoltage

	result := fullBankChargingLevel(inputs, settings, nil)
	if !result.Ok {
		t.Fatalf("expected Ok = true (HighVoltage alone should not block configuration)")
	}
	if result.Level != 1 {
		t.Fatalf("expected level 1 with HighVoltage unset (voltage half of the OR disabled): got %d", result.Level)
	}
}

// --- validity gating (anomaly_sensor_health.go's future inputValidity) ---

func TestFullBankChargingLevel_NilValidityTrustsInputs(t *testing.T) {
	settings := baseFullBankSettings()
	inputs := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10}
	if got := fullBankChargingLevel(inputs, settings, nil).Level; got != 1 {
		t.Fatalf("nil validity func: level = %d, want 1 (nil must mean 'trust every input')", got)
	}
}

// TestFullBankChargingLevel_InvalidInputPublishesNothing exercises the
// "a detector with an invalid input publishes nothing" rule from the plan:
// Ok stays true (the bank *is* configured), but level and evidence are not
// asserted when the caller's validity check rejects one of the bank's own
// sub-paths.
func TestFullBankChargingLevel_InvalidInputPublishesNothing(t *testing.T) {
	settings := baseFullBankSettings()
	settings.BankPath = "electrical.batteries.512"
	inputs := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10}

	rejectVoltage := func(path string) bool {
		return path != "electrical.batteries.512.voltage"
	}

	result := fullBankChargingLevel(inputs, settings, rejectVoltage)
	if !result.Ok {
		t.Fatalf("expected Ok = true (bank is configured; only the reading is untrusted)")
	}
	if result.Level != 0 {
		t.Fatalf("expected level 0 when an input is invalid, got %d", result.Level)
	}
	if result.Evidence != "" {
		t.Fatalf("expected no evidence when an input is invalid, got %q", result.Evidence)
	}
}

func TestFullBankChargingLevel_ValidityIgnoredWithoutBankPath(t *testing.T) {
	settings := baseFullBankSettings() // BankPath left empty
	inputs := fullBankChargingInputs{SoC: 0.96, Voltage: 28.0, Current: 10}

	alwaysReject := func(path string) bool { return false }

	if got := fullBankChargingLevel(inputs, settings, alwaysReject).Level; got != 1 {
		t.Fatalf("empty BankPath should skip the validity check entirely: level = %d, want 1", got)
	}
}

// --- 4. Regression: the 2026-09-21 dead-ship incident (PENDING) ---
//
// The plan calls for a regression test that replays the 2026-09-21 docking
// incident from real InfluxDB data and confirms fullBankChargingLevel
// reaches level >= 1 ahead of the BMS trip. This worktree has no InfluxDB
// token, and AGENTS.md's Test-First policy plus the plan's explicit
// instruction rule out reconstructing or estimating the combined alternator
// charge current to fake it - a captured fixture is required, not invented
// numbers. Left out deliberately; see the anomaly-detection plan's TDD order
// step 7 and the final report's open items.
