// Package main: the place-names provider layer (ADR 0101).
//
// Place-name resolution (place_name.go: the position tile, the anchor
// watch's pinned name) and the assistant's find_places tool
// (assistant_tools.go) both need a place-names data source, and both get one
// the same way: any installed POI plugin (poi_providers.go) that ALSO
// exports place_name_at and search_places (wasmPluginBase.SupportsPlaceNames,
// wasm_plugin.go) can serve as one. There is no separate plugin kind or
// directory for this - a POI plugin either declares the extra pair of
// exports or it doesn't, and the operator picks which installed, supporting
// plugin answers place-name questions via ui.place_name_provider
// (Settings -> Widgets -> Place names), independently of ui.poi_provider
// (Settings -> Widgets -> Nearby). The two settings commonly name the same
// plugin (osm-overpass, by default) but need not.
package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/labstack/echo/v4"
)

// placeNameResult is one place_name_at call's answer: either a named
// feature (Name non-empty, Kind/Lat/Lon meaningful) or the legitimate
// "nothing named here" negative (Name == "", everything else zero) - never
// an error. Mirrors the plugin contract's {"name": string, "kind": string,
// "lat": number, "lon": number} | {"name": ""}.
type placeNameResult struct {
	Name string
	Kind string
	Lat  float64
	Lon  float64
}

// placeSearchCentre names and locates the qualifier feature a provider
// re-centred its broadened search on (search_places' "centred_on": {...} |
// null) - e.g. searching "Bona Bay, Gloucester Island" from far away centres
// on the resolved Gloucester Island rather than the vessel's own position.
type placeSearchCentre struct {
	Name string
	Lat  float64
	Lon  float64
}

// placeSearchMatch is one raw search_places result: a named feature with no
// distance, bearing or source label yet - the host (executeFindPlaces,
// assistant_tools.go) computes those itself once a provider's raw matches
// come back, exactly as it always has, so every provider agrees on units
// and behaviour the same way poi_providers.go's decorateAndRankPOIFeatures
// already does for Nearby.
type placeSearchMatch struct {
	Name string
	Kind string
	Lat  float64
	Lon  float64
}

// placeSearchInput is one search_places call's arguments. Broad tells the
// provider whether to run its more expensive, broadened search (e.g. a
// partial-name regex rung) - the host, not the provider, knows whether a
// saved route waypoint already answered the query, so it alone decides
// whether that's worth the extra cost.
type placeSearchInput struct {
	Query      string
	Lat        float64
	Lon        float64
	MaxResults int
	Broad      bool
}

// placeSearchResult is search_places' answer. Search is "exact", "regex" or
// "none".
type placeSearchResult struct {
	Search    string
	RadiusNm  float64
	CentredOn *placeSearchCentre
	Results   []placeSearchMatch
	Note      string
}

// placeNameProvider is implemented by any installed POI plugin that also
// supports place-name resolution - see placeNameCapable and
// asPlaceNameProvider below for how a poiProvider is checked against this
// before being handed out. Defined as its own small interface (rather than
// folding these two methods into poiProvider itself) so tests can supply a
// fake with no WASM runtime involved at all.
type placeNameProvider interface {
	ID() string
	Name() string
	PlaceNameAt(lat, lon float64, radiusM int) (placeNameResult, error)
	SearchPlaces(input placeSearchInput) (placeSearchResult, error)
}

// placeNameCapable is implemented by every wasmPOIProvider (via the embedded
// wasmPluginBase) regardless of whether the underlying plugin actually
// exports place_name_at/search_places - PlaceNameAt/SearchPlaces are always
// callable Go methods on that type, so a plain type assertion to
// placeNameProvider would always succeed and tell us nothing. This is the
// capability check that actually gates it.
type placeNameCapable interface {
	SupportsPlaceNames() bool
}

// asPlaceNameProvider returns p as a placeNameProvider only when it both
// declares place-name support (placeNameCapable) and satisfies the calling
// interface - the ordinary path for any real, WASM-backed provider. A
// provider that doesn't implement placeNameCapable at all (there is no such
// registered provider type today, but a future non-WASM one could exist) is
// treated as not supporting place names, not as an error.
func asPlaceNameProvider(p poiProvider) (placeNameProvider, bool) {
	capable, ok := p.(placeNameCapable)
	if !ok || !capable.SupportsPlaceNames() {
		return nil, false
	}
	pn, ok := p.(placeNameProvider)
	return pn, ok
}

// defaultPlaceNameProviderID is used when ui.place_name_provider is unset in
// settings.yaml - the same plugin poi_providers.go's defaultPOIProviderID
// defaults to, so a fresh install resolves place names with zero
// configuration.
const defaultPlaceNameProviderID = "osm-overpass"

// resolvePlaceNameProvider reads ui.place_name_provider from settingsPath
// (defaulting to defaultPlaceNameProviderID) and resolves it against the
// shared POI provider registry, mirroring resolvePOIProvider's idiom
// exactly. An unconfigured id, or one that names a plugin not installed, or
// one that names a plugin that doesn't support place names, all return a
// clear, actionable error naming the id at fault - never a silent fallback
// to a different provider (AGENTS.md's fail-fast / no-masking-fallback
// policy).
func resolvePlaceNameProvider(settingsPath string) (placeNameProvider, string, error) {
	settings, err := readSettings(settingsPath)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read settings: %w", err)
	}

	uiMap, _ := settings["ui"].(map[string]any)
	configuredProvider := strings.TrimSpace(coerceString(uiMap["place_name_provider"]))
	if configuredProvider == "" {
		configuredProvider = defaultPlaceNameProviderID
	}

	provider, ok := getPOIProvider(configuredProvider)
	if !ok {
		return nil, configuredProvider, fmt.Errorf("unknown place-names provider configured: %q (is the plugin installed in plugins/poi?)", configuredProvider)
	}

	pn, ok := asPlaceNameProvider(provider)
	if !ok {
		return nil, configuredProvider, fmt.Errorf("POI plugin %q does not support place names (it does not export both place_name_at and search_places)", configuredProvider)
	}

	return pn, configuredProvider, nil
}

// placeNameProviderResolver resolves the currently configured place-names
// provider, read fresh on every call (mirroring resolvePlaceNameProvider's
// own "read settings fresh" contract) so a Settings save takes effect on
// the very next lookup with no restart. Both place_name.go's tick/anchor
// resolution and assistant_tools.go's find_places take one of these rather
// than a concrete provider, so tests can inject a fake with no settings
// file or WASM runtime at all.
type placeNameProviderResolver func() (placeNameProvider, string, error)

// placeNameProvidersHandler serves GET /api/place-name-providers: every
// registered POI plugin that supports place names, in registration order -
// mirroring poiProvidersHandler (poi_providers.go) but filtered to the
// supporting subset, since not every installed POI plugin necessarily
// exports place_name_at/search_places (google-places does not, per ADR
// 0056).
func placeNameProvidersHandler(c echo.Context) error {
	result := make([]poiProviderInfo, 0, len(poiProviderOrder))
	for _, id := range poiProviderOrder {
		provider, ok := poiProviderRegistry[id]
		if !ok {
			continue
		}
		if _, ok := asPlaceNameProvider(provider); !ok {
			continue
		}
		result = append(result, poiProviderInfo{ID: provider.ID(), Name: provider.Name(), Description: provider.Description()})
	}
	return c.JSON(http.StatusOK, result)
}
