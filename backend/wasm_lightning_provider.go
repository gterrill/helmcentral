// Package main: WASM lightning provider adapter.
//
// NOTE on this file's name: like wasm_upper_air_provider.go, it is
// deliberately NOT named anything ending in "_wasm.go" - see
// wasm_tide_provider.go's header comment for why (implicit GOARCH=wasm build
// constraint).
//
// The thin lightning layer on the shared WASM host machinery in
// wasm_plugin.go. Unlike the other adapters it holds no cache of its own: the
// schedule (lightning_schedule.go) decides what to fetch and keeps what it
// holds, so this only makes the one call. See lightning_providers.go's top doc
// comment for the guest contract.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"time"

	extism "github.com/extism/go-sdk"
)

type wasmLightningProvider struct {
	*wasmPluginBase
}

func pluginsLightningDir() string {
	return cacheFilePath("PLUGINS_LIGHTNING_DIR", "plugins/lightning")
}

func newWasmLightningProvider(path string) (*wasmLightningProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmLightningProviderWithManifest(manifest)
}

func newWasmLightningProviderWithManifest(manifest extism.Manifest) (*wasmLightningProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/lightning")
	if err != nil {
		return nil, err
	}
	return &wasmLightningProvider{wasmPluginBase: base}, nil
}

type wasmFetchLightningInput struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	From string  `json:"from"`
	To   string  `json:"to"`
	// Run is the forecast run to read; omitted, the plugin finds the newest.
	Run string `json:"run,omitempty"`
}

// lightningBackgroundCallBudget is the time one fetch_lightning call may take.
// A call downloads up to eight whole-grid windows with no one waiting on it,
// so it gets more than the 15 second default.
const lightningBackgroundCallBudget = 60 * time.Second

// wasmLightningWindowOutput mirrors the guest's fetch_lightning output.
type wasmLightningWindowOutput struct {
	Start       string  `json:"start"`
	End         string  `json:"end"`
	Probability float64 `json:"probability"`
}

type wasmFetchLightningOutput struct {
	Run     string                      `json:"run"`
	Windows []wasmLightningWindowOutput `json:"windows"`
}

func mapWasmFetchLightningOutput(out wasmFetchLightningOutput) (lightningSpan, error) {
	run, err := parseRequiredTime("run", out.Run)
	if err != nil {
		return lightningSpan{}, err
	}
	span := lightningSpan{Run: run, Windows: make([]lightningWindow, 0, len(out.Windows))}
	for i, w := range out.Windows {
		start, err := parseRequiredTime(fmt.Sprintf("windows[%d].start", i), w.Start)
		if err != nil {
			return lightningSpan{}, err
		}
		end, err := parseRequiredTime(fmt.Sprintf("windows[%d].end", i), w.End)
		if err != nil {
			return lightningSpan{}, err
		}
		span.Windows = append(span.Windows, lightningWindow{Start: start, End: end, Probability: w.Probability})
	}
	return span, nil
}

func (p *wasmLightningProvider) FetchLightning(lat, lon float64, from, to, run time.Time) (span lightningSpan, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_lightning: %v", p.id, r)
			span = lightningSpan{}
		}
	}()

	in := wasmFetchLightningInput{
		Lat: lat, Lon: lon,
		From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339),
	}
	if !run.IsZero() {
		in.Run = run.UTC().Format(time.RFC3339)
	}
	input, err := json.Marshal(in)
	if err != nil {
		return lightningSpan{}, fmt.Errorf("plugin %q: failed to marshal fetch_lightning input: %w", p.id, err)
	}

	out, err := p.callWithin("fetch_lightning", input, lightningBackgroundCallBudget)
	if err != nil {
		return lightningSpan{}, err
	}

	var parsed wasmFetchLightningOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return lightningSpan{}, fmt.Errorf("plugin %q: unparseable fetch_lightning JSON: %w", p.id, err)
	}
	return mapWasmFetchLightningOutput(parsed)
}

func loadWasmLightningProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/lightning",
		func(id string) bool {
			_, ok := getLightningProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := &wasmLightningProvider{wasmPluginBase: base}
			registerLightningProvider(provider)
			log.Printf("plugins/lightning: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
