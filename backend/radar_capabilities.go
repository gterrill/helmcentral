package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
)

// mayaraCapabilitiesAPIPathTemplate is the mayara SignalK plugin's proxied
// endpoint for one radar's capabilities: range table, spoke geometry and the
// colour legend the radar picture overlay needs to make sense of raw spoke
// bytes. %s is the radar id radarsFromSnapshot reports, e.g. "fur6424A",
// mirroring mayaraTargetsAPIPathTemplate (radar_source.go).
const mayaraCapabilitiesAPIPathTemplate = "/signalk/v2/api/vessels/self/radars/%s/capabilities"

// radarCapabilitiesCacheTTL bounds how long a decoded-and-validated
// capabilities response is served from cache before the next request
// re-fetches it. Capabilities change only on a range change (a different
// spoke length and range table), so this is deliberately generous compared
// to radarPollInterval — there is no push signal for a range change, so a
// short TTL is what keeps the picture's geometry from going stale for long
// without hitting mayara on every request.
const radarCapabilitiesCacheTTL = 30 * time.Second

// radarCapabilitiesCacheEntry holds one radar's last-known-good capabilities
// response, verbatim, plus when that copy expires.
type radarCapabilitiesCacheEntry struct {
	body      []byte
	expiresAt time.Time
}

var (
	radarCapabilitiesMu    sync.Mutex
	radarCapabilitiesCache = map[string]radarCapabilitiesCacheEntry{}
)

// radarCapabilitiesProbe is decoded only far enough to confirm the response
// is actually a capabilities object before it is cached and served — never
// re-modelled into a Go type the browser then has to be kept in sync with.
// The legend in particular (up to 256 pixel entries, Doppler ramps, a
// historyStart index that is not itself a usable boundary — see the plan's
// "Corrections that shape the design") is exactly the kind of shape a second
// Go model would get subtly wrong. legend.pixels and spokesPerRevolution are
// the two fields the frontend cannot function without
// (frontend/src/lib/radar-echo/legend.ts's buildEchoPalette and geometry.ts),
// so their presence is what "this looks like a capabilities response" means
// here.
type radarCapabilitiesProbe struct {
	Legend *struct {
		Pixels []json.RawMessage `json:"pixels"`
	} `json:"legend"`
	SpokesPerRevolution *int `json:"spokesPerRevolution"`
}

// fetchMayaraCapabilities GETs one radar's capabilities through the
// already-configured SignalK connection (signalkRequestJSONWithAuthBody,
// route_activation.go:144), exactly as fetchMayaraTargets does
// (radar_source.go) — mayara has no host/port of its own on this path either,
// only the plugin's proxy, reached at the SignalK address already configured
// for everything else.
//
// A non-2xx status, or a body that does not decode into something
// recognisable as a capabilities object, is returned as an explicit error.
// Per AGENTS.md's fallback policy, this must never come back as an empty or
// partial legend standing in for a real one — a radar picture painted from a
// synthesized legend would misrepresent real returns, which is worse than no
// picture.
func fetchMayaraCapabilities(settingsPath, radarID string) ([]byte, error) {
	address, port, err := loadSignalKSettings(settingsPath)
	if err != nil {
		address = defaultSignalKAddress
		port = defaultSignalKPort
	}
	signalkURL := buildSignalKURL(address, port)
	path := fmt.Sprintf(mayaraCapabilitiesAPIPathTemplate, radarID)

	status, body, err := signalkRequestJSONWithAuthBody(signalkURL, settingsPath, path, http.MethodGet, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("mayara capabilities endpoint %s returned status %d: %s", path, status, string(body))
	}

	var probe radarCapabilitiesProbe
	if err := json.Unmarshal(body, &probe); err != nil {
		return nil, fmt.Errorf("decoding mayara capabilities response for radar %s: %w", radarID, err)
	}
	if probe.Legend == nil || probe.SpokesPerRevolution == nil {
		return nil, fmt.Errorf("mayara capabilities response for radar %s is missing legend or spokesPerRevolution: %s", radarID, string(body))
	}

	return body, nil
}

// radarCapabilities returns radarID's capabilities, verbatim, from cache when
// a still-fresh copy exists and from mayara otherwise. A fetch failure is
// never cached, so an outage is retried on the very next request rather than
// being pinned for the rest of the TTL.
func radarCapabilities(settingsPath, radarID string) ([]byte, error) {
	radarCapabilitiesMu.Lock()
	if entry, ok := radarCapabilitiesCache[radarID]; ok && time.Now().Before(entry.expiresAt) {
		body := entry.body
		radarCapabilitiesMu.Unlock()
		return body, nil
	}
	radarCapabilitiesMu.Unlock()

	body, err := fetchMayaraCapabilities(settingsPath, radarID)
	if err != nil {
		return nil, err
	}

	radarCapabilitiesMu.Lock()
	radarCapabilitiesCache[radarID] = radarCapabilitiesCacheEntry{
		body:      body,
		expiresAt: time.Now().Add(radarCapabilitiesCacheTTL),
	}
	radarCapabilitiesMu.Unlock()

	return body, nil
}

// radarCapabilitiesHandler backs GET /api/radar/capabilities?radar=<id>
// (tierRead). It relays mayara's capabilities response through unmodified —
// the browser needs the whole thing, including a legend this backend
// deliberately does not re-model (see radarCapabilitiesProbe) — and surfaces
// any upstream failure as an explicit error rather than any kind of
// synthesized or partial legend.
func radarCapabilitiesHandler(c echo.Context) error {
	radarID := c.QueryParam("radar")
	if radarID == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "radar query parameter is required"})
	}

	settingsPath := getEnv("SETTINGS_FILE", "../settings.yaml")

	body, err := radarCapabilities(settingsPath, radarID)
	if err != nil {
		return c.JSON(http.StatusBadGateway, map[string]string{"error": err.Error()})
	}

	return c.JSONBlob(http.StatusOK, body)
}
