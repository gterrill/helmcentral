// Package main: WASM POI provider adapter.
//
// NOTE on this file's name: like wasm_wave_provider.go, it is deliberately
// NOT named anything ending in "_wasm.go" - see wasm_tide_provider.go's
// header comment for the full explanation of why (implicit GOARCH=wasm
// build constraint).
//
// This is the thin POI-specific layer on top of the shared WASM host
// machinery in wasm_plugin.go: wasmPOIProvider embeds *wasmPluginBase
// (ID/Name/TTLSeconds/ttlDuration/call all come from there) and adds a
// wasmPluginCache[poiFetchResult] plus the poiProvider interface method. See
// poi_providers.go's top doc comment for the guest contract this adapter
// maps JSON against.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"time"

	extism "github.com/extism/go-sdk"
)

// wasmPOIProvider is a poiProvider backed by a sandboxed WASM guest module,
// loaded via extism/go-sdk - same pattern as wasmWaveProvider.
type wasmPOIProvider struct {
	*wasmPluginBase
	cache *wasmPluginCache[poiFetchResult]
}

// pluginsPOIDir follows the codebase's cacheFilePath(envKey, fallback)
// env-override-with-fallback idiom, mirroring pluginsWavesDir.
func pluginsPOIDir() string {
	return cacheFilePath("PLUGINS_POI_DIR", "plugins/poi")
}

// newWasmPOIProvider compiles the .wasm file at path and validates its
// contract at discovery time (id()/name() must resolve and be callable). It
// deliberately does NOT call fetch_poi here - that needs live network
// access, which has no place at container boot.
func newWasmPOIProvider(path string) (*wasmPOIProvider, error) {
	manifest, err := manifestForWasmPlugin(path)
	if err != nil {
		return nil, err
	}
	return newWasmPOIProviderWithManifest(manifest)
}

// newWasmPOIProviderWithManifest is split out from newWasmPOIProvider so
// tests can construct a provider against a fully custom manifest, mirroring
// newWasmWaveProviderWithManifest.
func newWasmPOIProviderWithManifest(manifest extism.Manifest) (*wasmPOIProvider, error) {
	base, err := newWasmPluginBase(manifest, "plugins/poi")
	if err != nil {
		return nil, err
	}
	return newWasmPOIProviderFromBase(base), nil
}

// newWasmPOIProviderFromBase wraps an already-validated base with a
// POI-specific disk-backed cache (loaded from disk immediately), mirroring
// newWasmWaveProviderFromBase.
func newWasmPOIProviderFromBase(base *wasmPluginBase) *wasmPOIProvider {
	cacheFile := cacheFilePath(
		"POI_WASM_CACHE_FILE_"+strings.ToUpper(strings.ReplaceAll(base.ID(), "-", "_")),
		fmt.Sprintf("cache/poi_wasm_%s_cache.json", base.ID()),
	)
	cache := newWasmPluginCache[poiFetchResult](cacheFile)
	cache.loadFromDisk()

	return &wasmPOIProvider{wasmPluginBase: base, cache: cache}
}

// wasmFetchPOIInput mirrors the guest's fetch_poi input contract.
type wasmFetchPOIInput struct {
	Lat        float64  `json:"lat"`
	Lon        float64  `json:"lon"`
	RadiusM    int      `json:"radius_m"`
	Categories []string `json:"categories"`
	Limit      int      `json:"limit"`
}

// wasmPOIFeatureOutput/wasmFetchPOIOutput mirror the guest's fetch_poi
// output contract exactly (snake_case field names) - see poi_providers.go's
// top doc comment for the full JSON shape. Unlike the wave/weather
// contracts, there is no per-item timestamp to parse - a POI feature has no
// notion of "when", so mapping this output can never fail on malformed
// per-item data the way mapWasmFetchWavesOutput can.
type wasmPOIFeatureOutput struct {
	ID        string  `json:"id"`
	Category  string  `json:"category"`
	Name      string  `json:"name"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Detail    string  `json:"detail"`
	SourceURL string  `json:"source_url"`
}

type wasmFetchPOIOutput struct {
	Features    []wasmPOIFeatureOutput `json:"features"`
	Truncated   []string               `json:"truncated"`
	Unsupported []string               `json:"unsupported"`
}

func mapWasmFetchPOIOutput(out wasmFetchPOIOutput) poiFetchResult {
	features := make([]poiFeatureRaw, 0, len(out.Features))
	for _, f := range out.Features {
		features = append(features, poiFeatureRaw{
			ID:        f.ID,
			Category:  f.Category,
			Name:      f.Name,
			Lat:       f.Lat,
			Lon:       f.Lon,
			Detail:    f.Detail,
			SourceURL: f.SourceURL,
		})
	}

	return poiFetchResult{
		Features:    features,
		Truncated:   out.Truncated,
		Unsupported: out.Unsupported,
	}
}

// poiWasmCacheCellDegrees is the position-rounding granularity for the POI
// cache key - about 2km. Waves/weather round to 1dp (about 11km) because a
// wave/weather model's resolution is coarse to begin with; local features
// (an anchorage, a fuel dock) need a tighter cell so the cache does not
// paper over genuinely different Overpass/Places answers a couple of
// kilometres apart, while still collapsing normal vessel drift/anchor swing
// onto one cached entry within the 6h TTL.
const poiWasmCacheCellDegrees = 0.02

// poiWasmCacheKey rounds lat/lon to the 0.02 degree cell, folds in radiusM,
// limit, and the sorted category list (order-independent - the same set of
// categories in a different order must hit the same entry), so a request for
// a different radius, limit, or category set at the same rounded position
// doesn't clobber a different combination's cache entry within the TTL
// window - same reasoning as weatherWasmCacheKey/waveWasmCacheKey.
func poiWasmCacheKey(lat, lon float64, radiusM int, categories []string, limit int) string {
	roundedLat := math.Round(lat/poiWasmCacheCellDegrees) * poiWasmCacheCellDegrees
	roundedLon := math.Round(lon/poiWasmCacheCellDegrees) * poiWasmCacheCellDegrees

	sorted := append([]string(nil), categories...)
	sort.Strings(sorted)

	return fmt.Sprintf("%.2f,%.2f,%d,%d,%s", roundedLat, roundedLon, radiusM, limit, strings.Join(sorted, "|"))
}

// FetchPOI calls the guest's fetch_poi, unmarshals+maps the raw JSON into a
// typed poiFetchResult, and applies the same TTL cache + stale-on-error
// fallback pattern as wasmWaveProvider.FetchWaves.
func (p *wasmPOIProvider) FetchPOI(lat, lon float64, radiusM int, categories []string, limit int) (result poiFetchResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in fetch_poi: %v", p.id, r)
			result = poiFetchResult{}
		}
	}()

	key := poiWasmCacheKey(lat, lon, radiusM, categories, limit)

	if cached, ok := p.cache.get(key, p.ttlDuration()); ok {
		cached.Cached = true
		return cached, nil
	}

	fetched, ferr := p.cache.singleflightFetch(key, func() (poiFetchResult, error) {
		return p.fetchFromPlugin(lat, lon, radiusM, categories, limit)
	})
	if ferr != nil {
		if stale, ok := p.cache.getStale(key); ok {
			stale.Cached = true
			return stale, nil
		}
		return poiFetchResult{}, ferr
	}

	fetched.CachedAt = time.Now().UTC()
	fetched.Cached = false
	p.cache.set(key, fetched)

	return fetched, nil
}

func (p *wasmPOIProvider) fetchFromPlugin(lat, lon float64, radiusM int, categories []string, limit int) (poiFetchResult, error) {
	input, err := json.Marshal(wasmFetchPOIInput{Lat: lat, Lon: lon, RadiusM: radiusM, Categories: categories, Limit: limit})
	if err != nil {
		return poiFetchResult{}, fmt.Errorf("plugin %q: failed to marshal fetch_poi input: %w", p.id, err)
	}

	out, err := p.call("fetch_poi", input)
	if err != nil {
		return poiFetchResult{}, err
	}

	var parsed wasmFetchPOIOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return poiFetchResult{}, fmt.Errorf("plugin %q: unparseable fetch_poi JSON: %w", p.id, err)
	}

	return mapWasmFetchPOIOutput(parsed), nil
}

// ── place_name_at / search_places (ADR 0101, optional exports) ────────────
//
// A wasmPOIProvider is only ever handed out as a placeNameProvider
// (place_name_provider.go's asPlaceNameProvider) once SupportsPlaceNames()
// is true, but PlaceNameAt/SearchPlaces below still guard themselves the
// same way FetchPOI would guard a required export - defence in depth against
// any future caller that skips that check.

// wasmPlaceNameAtInput mirrors the guest's place_name_at input contract.
type wasmPlaceNameAtInput struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM int     `json:"radius_m"`
}

// wasmPlaceNameAtOutput mirrors the guest's place_name_at output contract:
// {"name": string, "kind": string, "lat": number, "lon": number} for a
// winner, or {"name": ""} for "nothing named here". Lat/Lon are pointers so
// a winner sitting at exactly 0 latitude/longitude still round-trips as a
// real coordinate.
type wasmPlaceNameAtOutput struct {
	Name string   `json:"name"`
	Kind string   `json:"kind"`
	Lat  *float64 `json:"lat"`
	Lon  *float64 `json:"lon"`
}

// PlaceNameAt calls the guest's place_name_at export through p.call, so the
// plugin's own stored config values (e.g. osm-overpass's overpass_url)
// apply exactly as they do for fetch_poi. Decoding is strict: a winner
// (non-empty Name) MUST carry both Lat and Lon, or the response is rejected
// as malformed rather than silently returned as a zero coordinate.
func (p *wasmPOIProvider) PlaceNameAt(lat, lon float64, radiusM int) (result placeNameResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in place_name_at: %v", p.id, r)
			result = placeNameResult{}
		}
	}()

	if !p.SupportsPlaceNames() {
		return placeNameResult{}, fmt.Errorf("plugin %q does not support place names (missing place_name_at/search_places export)", p.id)
	}

	input, err := json.Marshal(wasmPlaceNameAtInput{Lat: lat, Lon: lon, RadiusM: radiusM})
	if err != nil {
		return placeNameResult{}, fmt.Errorf("plugin %q: failed to marshal place_name_at input: %w", p.id, err)
	}

	out, err := p.call("place_name_at", input)
	if err != nil {
		return placeNameResult{}, err
	}

	var parsed wasmPlaceNameAtOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return placeNameResult{}, fmt.Errorf("plugin %q: unparseable place_name_at JSON: %w", p.id, err)
	}
	if strings.TrimSpace(parsed.Name) == "" {
		return placeNameResult{}, nil
	}
	if parsed.Lat == nil || parsed.Lon == nil {
		return placeNameResult{}, fmt.Errorf("plugin %q: place_name_at returned name %q with no lat/lon", p.id, parsed.Name)
	}
	return placeNameResult{Name: parsed.Name, Kind: parsed.Kind, Lat: *parsed.Lat, Lon: *parsed.Lon}, nil
}

// wasmSearchPlacesInput mirrors the guest's search_places input contract.
type wasmSearchPlacesInput struct {
	Query      string  `json:"query"`
	Lat        float64 `json:"lat"`
	Lon        float64 `json:"lon"`
	MaxResults int     `json:"max_results"`
	Broad      bool    `json:"broad"`
}

// wasmSearchPlaceMatch/wasmSearchPlacesCentre/wasmSearchPlacesOutput mirror
// the guest's search_places output contract exactly - see
// docs/examples/poi-plugins/osm-overpass's package doc comment for the
// two-rung ladder this executes on the plugin side.
type wasmSearchPlaceMatch struct {
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type wasmSearchPlacesCentre struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

type wasmSearchPlacesOutput struct {
	Search    string                  `json:"search"`
	RadiusNm  float64                 `json:"radius_nm"`
	CentredOn *wasmSearchPlacesCentre `json:"centred_on"`
	Results   []wasmSearchPlaceMatch  `json:"results"`
	Note      string                  `json:"note"`
}

// validSearchPlacesValues is search_places' closed set of "search" values -
// decoding is strict, so anything else is a malformed response, not a
// fourth silently-accepted state.
var validSearchPlacesValues = map[string]bool{"exact": true, "regex": true, "none": true}

// SearchPlaces calls the guest's search_places export through p.call, same
// config-overlay and panic-recovery treatment as PlaceNameAt.
func (p *wasmPOIProvider) SearchPlaces(input placeSearchInput) (result placeSearchResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin %q panicked in search_places: %v", p.id, r)
			result = placeSearchResult{}
		}
	}()

	if !p.SupportsPlaceNames() {
		return placeSearchResult{}, fmt.Errorf("plugin %q does not support place names (missing place_name_at/search_places export)", p.id)
	}

	marshalled, err := json.Marshal(wasmSearchPlacesInput{
		Query:      input.Query,
		Lat:        input.Lat,
		Lon:        input.Lon,
		MaxResults: input.MaxResults,
		Broad:      input.Broad,
	})
	if err != nil {
		return placeSearchResult{}, fmt.Errorf("plugin %q: failed to marshal search_places input: %w", p.id, err)
	}

	out, err := p.call("search_places", marshalled)
	if err != nil {
		return placeSearchResult{}, err
	}

	var parsed wasmSearchPlacesOutput
	if err := json.Unmarshal(out, &parsed); err != nil {
		return placeSearchResult{}, fmt.Errorf("plugin %q: unparseable search_places JSON: %w", p.id, err)
	}
	if !validSearchPlacesValues[parsed.Search] {
		return placeSearchResult{}, fmt.Errorf("plugin %q: search_places returned unrecognised search value %q", p.id, parsed.Search)
	}

	matches := make([]placeSearchMatch, 0, len(parsed.Results))
	for _, m := range parsed.Results {
		matches = append(matches, placeSearchMatch{Name: m.Name, Kind: m.Kind, Lat: m.Lat, Lon: m.Lon})
	}

	var centredOn *placeSearchCentre
	if parsed.CentredOn != nil {
		centredOn = &placeSearchCentre{Name: parsed.CentredOn.Name, Lat: parsed.CentredOn.Lat, Lon: parsed.CentredOn.Lon}
	}

	return placeSearchResult{
		Search:    parsed.Search,
		RadiusNm:  parsed.RadiusNm,
		CentredOn: centredOn,
		Results:   matches,
		Note:      parsed.Note,
	}, nil
}

// loadWasmPOIProviders scans dir once at startup for .wasm plugins, via the
// shared loadWasmPluginsFromDir. Mirrors loadWasmWaveProviders - a file that
// fails to load as a valid plugin is logged and skipped, discovery
// continues for the rest.
func loadWasmPOIProviders(dir string) {
	loadWasmPluginsFromDir(dir, "plugins/poi",
		func(id string) bool {
			_, ok := getPOIProvider(id)
			return ok
		},
		func(base *wasmPluginBase, path string) error {
			provider := newWasmPOIProviderFromBase(base)
			registerPOIProvider(provider)
			log.Printf("plugins/poi: registered plugin %q (%s) from %s", provider.ID(), provider.Name(), filepath.Base(path))
			return nil
		},
	)
}
