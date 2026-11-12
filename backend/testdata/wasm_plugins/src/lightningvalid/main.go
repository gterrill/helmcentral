//go:build !std
// +build !std

// lightningvalid is a minimal but complete lightning-provider WASM fixture,
// implementing the plugin contract (id/name/ttl_seconds/fetch_lightning) with
// invented data: every 3-hour window ending in (from, to] on the UTC 3-hour
// grid, with a probability that rises by 0.1 per window across the day so a
// test can tell the windows apart. The run is 2026-11-12T00:00:00Z unless the input names one, which is echoed back.
// Window ends at 09:00 UTC are left out, to show that a missing window
// reaches the host as a gap. See backend/wasm_plugin_test.go for the
// regeneration command.
package main

import (
	"time"

	"github.com/extism/go-pdk"
)

type fetchLightningInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	From string  `json:"from"`
	To   string  `json:"to"`
	// Run, when given, is the run to read; the fixture reports it back.
	Run string `json:"run"`
}

type windowOut struct {
	Start       string  `json:"start"`
	End         string  `json:"end"`
	Probability float64 `json:"probability"`
}

type fetchLightningOutput struct {
	Run     string      `json:"run"`
	Windows []windowOut `json:"windows"`
}

//go:wasmexport id
func id() int32 {
	pdk.OutputString("lightning-valid-fixture")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Lightning Valid Fixture Provider")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	pdk.OutputString("900")
	return 0
}

//go:wasmexport fetch_lightning
func fetchLightning() int32 {
	var input fetchLightningInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}
	from, err := time.Parse(time.RFC3339, input.From)
	if err != nil {
		pdk.SetError(err)
		return -1
	}
	to, err := time.Parse(time.RFC3339, input.To)
	if err != nil {
		pdk.SetError(err)
		return -1
	}

	run := "2026-11-12T00:00:00Z"
	if input.Run != "" {
		run = input.Run
	}
	out := fetchLightningOutput{Run: run, Windows: []windowOut{}}
	end := from.Truncate(3 * time.Hour).Add(3 * time.Hour)
	for ; !end.After(to); end = end.Add(3 * time.Hour) {
		if end.Hour() == 9 {
			continue
		}
		out.Windows = append(out.Windows, windowOut{
			Start:       end.Add(-3 * time.Hour).UTC().Format(time.RFC3339),
			End:         end.UTC().Format(time.RFC3339),
			Probability: float64(end.Hour()/3) / 10,
		})
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
