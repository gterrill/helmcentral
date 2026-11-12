package main

import (
	"strings"
	"testing"
	"time"
)

const lightningValidFixtureWasm = "testdata/wasm_plugins/lightningvalid.wasm"

func TestWasmLightningProvider_FetchLightning_RoundTripsThroughTheRealHost(t *testing.T) {
	provider, err := newWasmLightningProvider(lightningValidFixtureWasm)
	if err != nil {
		t.Fatalf("newWasmLightningProvider: %v", err)
	}
	if provider.ID() != "lightning-valid-fixture" {
		t.Fatalf("id = %q", provider.ID())
	}

	from := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	to := time.Date(2026, 11, 12, 15, 0, 0, 0, time.UTC)
	span, err := provider.FetchLightning(-27.4, 153.0, from, to, time.Time{})
	if err != nil {
		t.Fatalf("FetchLightning: %v", err)
	}
	if !span.Run.Equal(time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("run = %s", span.Run)
	}
	// Ends 06, 09, 12, 15 are in (03:00, 15:00]; the fixture omits 09:00.
	var ends []string
	for _, w := range span.Windows {
		ends = append(ends, w.End.Format("15:04"))
		if got := w.End.Sub(w.Start); got != 3*time.Hour {
			t.Errorf("window %s..%s is %s long", w.Start, w.End, got)
		}
	}
	if strings.Join(ends, ",") != "06:00,12:00,15:00" {
		t.Fatalf("window ends = %v, want 06:00,12:00,15:00 (09:00 is a gap, not interpolated)", ends)
	}
	if span.Windows[0].Probability != 0.2 || span.Windows[2].Probability != 0.5 {
		t.Errorf("probabilities = %v, %v", span.Windows[0].Probability, span.Windows[2].Probability)
	}
}

func TestWasmLightningProvider_FetchLightning_PassesTheRunToThePlugin(t *testing.T) {
	provider, err := newWasmLightningProvider(lightningValidFixtureWasm)
	if err != nil {
		t.Fatalf("newWasmLightningProvider: %v", err)
	}
	from := time.Date(2026, 11, 12, 3, 0, 0, 0, time.UTC)
	run := time.Date(2026, 11, 11, 18, 0, 0, 0, time.UTC)
	span, err := provider.FetchLightning(-27.4, 153.0, from, from.Add(12*time.Hour), run)
	if err != nil {
		t.Fatalf("FetchLightning: %v", err)
	}
	// The fixture reports whichever run it was told to read.
	if !span.Run.Equal(run) {
		t.Errorf("run = %s, want the one passed in (%s)", span.Run, run)
	}
}

func TestMapWasmFetchLightningOutput_RejectsBadContract(t *testing.T) {
	good := wasmLightningWindowOutput{Start: "2026-11-12T00:00:00Z", End: "2026-11-12T03:00:00Z", Probability: 0.5}
	for _, tc := range []struct {
		name string
		out  wasmFetchLightningOutput
		want string
	}{
		{"no run", wasmFetchLightningOutput{Windows: []wasmLightningWindowOutput{good}}, "run"},
		{"bad window time", wasmFetchLightningOutput{Run: "2026-11-12T00:00:00Z", Windows: []wasmLightningWindowOutput{{Start: "soon", End: good.End, Probability: 0.5}}}, "windows[0].start"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := mapWasmFetchLightningOutput(tc.out)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tc.want)
			}
		})
	}

	span, err := mapWasmFetchLightningOutput(wasmFetchLightningOutput{Run: "2026-11-12T00:00:00Z"})
	if err != nil || span.Windows == nil || len(span.Windows) != 0 {
		t.Fatalf("an empty window list is valid: %v %#v", err, span.Windows)
	}
}
