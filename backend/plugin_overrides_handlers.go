package main

import (
	"fmt"
	"net/http"

	"github.com/labstack/echo/v4"
)

// namedProvider is the subset of tideProvider/weatherProvider/waveProvider/
// forecastWarningsProvider's methods these handlers need generically. Every
// concrete provider type across all four domains already implements
// ID()/Name()/Description() (see wasm_plugin.go and each *_providers.go
// interface), so no per-type switch is needed to read those three fields -
// only providerByTypeAndID's registry dispatch is type-specific.
type namedProvider interface {
	ID() string
	Name() string
	Description() string
}

// pluginPathProvider is implemented by every WASM-backed provider -
// wasmPluginBase's Path() accessor (Part 1), inherited by every
// wasmTideProvider/wasmWeatherProvider/wasmWaveProvider/
// wasmForecastWarningsProvider via embedding. Every provider registered in
// every domain's registry (tide/weather/wave/forecast-warnings) is
// WASM-backed, so this assertion holding is an invariant, not a runtime
// possibility to branch on: a provider that fails it is a programming error
// (something registered a non-WASM provider), and the call sites below
// treat that as an internal error rather than a supported "native provider"
// configuration.
type pluginPathProvider interface{ Path() string }

// pluginInfoResponse is the exact, fixed HTTP contract for
// GET/POST/DELETE /api/plugins/:type/:id(/overrides) - a separate frontend
// team builds against this shape, do not change it without updating them.
type pluginInfoResponse struct {
	Type                     string   `json:"type"`
	ID                       string   `json:"id"`
	Name                     string   `json:"name"`
	Description              string   `json:"description"`
	Sandboxed                bool     `json:"sandboxed"`
	AllowedHosts             []string `json:"allowed_hosts"`
	AllowedHostsOverridden   bool     `json:"allowed_hosts_overridden"`
	AllowedSecrets           []string `json:"allowed_secrets"`
	AllowedSecretsOverridden bool     `json:"allowed_secrets_overridden"`
}

// providerByTypeAndID dispatches providerType to the matching registry and
// looks up id. validType=false means providerType itself isn't one of
// tide/weather/wave/forecast-warnings; found=false (with validType=true)
// means the type is valid but no such id is registered under it.
func providerByTypeAndID(providerType, id string) (provider namedProvider, validType, found bool) {
	switch providerType {
	case "tide":
		p, ok := getTideProvider(id)
		if !ok {
			return nil, true, false
		}
		return p, true, true
	case "weather":
		p, ok := getWeatherProvider(id)
		if !ok {
			return nil, true, false
		}
		return p, true, true
	case "wave":
		p, ok := getWaveProvider(id)
		if !ok {
			return nil, true, false
		}
		return p, true, true
	case "forecast-warnings":
		p, ok := getForecastWarningsProvider(id)
		if !ok {
			return nil, true, false
		}
		return p, true, true
	default:
		return nil, false, false
	}
}

// buildPluginInfoResponse builds the pluginInfoResponse for an already
// resolved provider. Every registered provider is WASM-backed (implements
// pluginPathProvider): allowed hosts/secrets are resolved via
// allowedHostsForWasmPlugin/allowedSecretsForWasmPlugin (already
// override-aware, Part 2) and the two *Overridden flags come from a direct
// globalPluginOverridesStore.Get - both flags share the same "is there a
// saved override row for this path at all" answer, since Set always saves
// both arrays together in one row. If provider does not implement
// pluginPathProvider, that is an invariant violation (see pluginPathProvider
// doc comment) and is reported as an error rather than a degraded response.
func buildPluginInfoResponse(providerType string, provider namedProvider) (pluginInfoResponse, error) {
	resp := pluginInfoResponse{
		Type:           providerType,
		ID:             provider.ID(),
		Name:           provider.Name(),
		Description:    provider.Description(),
		AllowedHosts:   []string{},
		AllowedSecrets: []string{},
	}

	pp, ok := provider.(pluginPathProvider)
	if !ok {
		return pluginInfoResponse{}, fmt.Errorf("provider %q (type %s) does not implement pluginPathProvider; every registered provider must be WASM-backed", provider.ID(), providerType)
	}
	resp.Sandboxed = true

	hosts, err := allowedHostsForWasmPlugin(pp.Path())
	if err != nil {
		return pluginInfoResponse{}, err
	}
	if hosts != nil {
		resp.AllowedHosts = hosts
	}

	secrets, err := allowedSecretsForWasmPlugin(pp.Path())
	if err != nil {
		return pluginInfoResponse{}, err
	}
	if secrets != nil {
		resp.AllowedSecrets = secrets
	}

	if globalPluginOverridesStore != nil {
		_, _, overridden, err := globalPluginOverridesStore.Get(pp.Path())
		if err != nil {
			return pluginInfoResponse{}, err
		}
		resp.AllowedHostsOverridden = overridden
		resp.AllowedSecretsOverridden = overridden
	}

	return resp, nil
}

// getPluginInfoHandler is GET /api/plugins/:type/:id.
func getPluginInfoHandler(c echo.Context) error {
	providerType := c.Param("type")
	id := c.Param("id")

	provider, validType, found := providerByTypeAndID(providerType, id)
	if !validType {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown provider type: " + providerType})
	}
	if !found {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "provider not found: " + id})
	}

	resp, err := buildPluginInfoResponse(providerType, provider)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read plugin allowlist state"})
	}
	return c.JSON(http.StatusOK, resp)
}

// pluginOverridesRequest is the POST /api/plugins/:type/:id/overrides
// request body. A missing/null field is treated as an empty array, not an
// error - mirroring pluginOverridesStore.Set's own nil-to-empty-array
// normalization.
type pluginOverridesRequest struct {
	AllowedHosts   []string `json:"allowed_hosts"`
	AllowedSecrets []string `json:"allowed_secrets"`
}

// postPluginOverridesHandler is POST /api/plugins/:type/:id/overrides. A
// resolved provider that isn't WASM-backed is an invariant violation (see
// pluginPathProvider doc comment), not a client-triggerable condition -
// providerType/id only decide which registry entry is resolved, and every
// entry in every registry is WASM-backed, so this reports a 500 like any
// other server-side invariant failure rather than blaming the request with
// a 400.
func postPluginOverridesHandler(c echo.Context) error {
	providerType := c.Param("type")
	id := c.Param("id")

	provider, validType, found := providerByTypeAndID(providerType, id)
	if !validType {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown provider type: " + providerType})
	}
	if !found {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "provider not found: " + id})
	}

	pp, ok := provider.(pluginPathProvider)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("provider %q (type %s) does not implement pluginPathProvider; every registered provider must be WASM-backed", id, providerType)})
	}

	var req pluginOverridesRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request payload"})
	}
	if req.AllowedHosts == nil {
		req.AllowedHosts = []string{}
	}
	if req.AllowedSecrets == nil {
		req.AllowedSecrets = []string{}
	}

	if err := globalPluginOverridesStore.Set(pp.Path(), req.AllowedHosts, req.AllowedSecrets); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to save plugin overrides"})
	}

	resp, err := buildPluginInfoResponse(providerType, provider)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read plugin allowlist state"})
	}
	return c.JSON(http.StatusOK, resp)
}

// deletePluginOverridesHandler is DELETE /api/plugins/:type/:id/overrides.
// Clears any saved override, reverting the response to file-based state.
// Deleting when nothing was overridden is not an error (mirrors
// pluginOverridesStore.Delete's own no-op-on-missing-row contract).
func deletePluginOverridesHandler(c echo.Context) error {
	providerType := c.Param("type")
	id := c.Param("id")

	provider, validType, found := providerByTypeAndID(providerType, id)
	if !validType {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "unknown provider type: " + providerType})
	}
	if !found {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "provider not found: " + id})
	}

	pp, ok := provider.(pluginPathProvider)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": fmt.Sprintf("provider %q (type %s) does not implement pluginPathProvider; every registered provider must be WASM-backed", id, providerType)})
	}

	if err := globalPluginOverridesStore.Delete(pp.Path()); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to clear plugin overrides"})
	}

	resp, err := buildPluginInfoResponse(providerType, provider)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to read plugin allowlist state"})
	}
	return c.JSON(http.StatusOK, resp)
}
