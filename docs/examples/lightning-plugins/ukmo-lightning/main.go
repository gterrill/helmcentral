//go:build tinygo
// +build tinygo

// ukmo-lightning is Helmcentral's free, keyless lightning-probability WASM
// plugin. It reads the Met Office's Blended Probabilistic Forecast from a
// public S3 bucket (CC BY-SA 4.0) and returns, for the grid cell holding a
// position, the chance of at least one flash in each 3-hour window.
//
// Like every Helmcentral plugin it returns raw values only: no bucketing,
// smoothing or summary. The host decides which spans to ask for and when
// (backend/lightning_providers.go).
//
// This file holds only the //go:wasmexport wrapper layer and is gated
// `//go:build tinygo` so the plain host toolchain can still build and test
// ukmo-lightning.go and hdf5.go. See the wave plugin's main.go for the full
// explanation.
package main

import (
	"fmt"
	"time"

	"github.com/extism/go-pdk"
)

//go:wasmexport id
func id() int32 {
	pdk.OutputString("ukmo-lightning")
	return 0
}

//go:wasmexport name
func name() int32 {
	pdk.OutputString("Met Office Lightning")
	return 0
}

//go:wasmexport ttl_seconds
func ttlSeconds() int32 {
	// 6 hours: the Met Office issues a new run every six hours.
	pdk.OutputString("21600")
	return 0
}

//go:wasmexport description
func description() int32 {
	pdk.OutputString("Free, keyless lightning probability from the Met Office blended forecast, 3-hour windows out to 14.5 days. Lightning probability: © Crown copyright, Met Office, CC BY-SA 4.0.")
	return 0
}

// pdkGet is the getFunc the logic in ukmo-lightning.go runs on. It reports a
// non-2xx status to the caller rather than failing, since a 404 on a window
// file is an expected answer.
func pdkGet(url string) (int, []byte, error) {
	res := pdk.NewHTTPRequest(pdk.MethodGet, url).Send()
	return int(res.Status()), res.Body(), nil
}

//go:wasmexport fetch_lightning
func exportFetchLightning() int32 {
	var input fetchLightningInput
	if err := pdk.InputJSON(&input); err != nil {
		pdk.SetError(err)
		return -1
	}

	out, err := fetchLightning(pdkGet, time.Now().UTC(), input)
	if err != nil {
		pdk.SetErrorString(fmt.Sprintf("ukmo-lightning: %v", err))
		return -1
	}

	if err := pdk.OutputJSON(out); err != nil {
		pdk.SetError(err)
		return -1
	}
	return 0
}

func main() {}
