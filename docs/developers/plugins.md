# Writing a provider plugin

This is the plugin authoring reference: the sandbox, the contracts, and the
build. For what a plugin is from an operator's chair (categories, installing
one, the settings it exposes), see [Provider plugins](../reference/plugins.md).

Tide, weather, wave, points-of-interest, forecast-warning and upper-air
providers are sandboxed WASM modules loaded from disk at startup. The six
registries (`backend/tide_providers.go`, `backend/weather_providers.go`,
`backend/wave_providers.go`, `backend/poi_providers.go`,
`backend/forecast_warnings_providers.go`, and `backend/upper_air_providers.go`)
share a single WASM host layer in `backend/wasm_plugin.go`.

## The sandbox

Plugins run via [Extism](https://extism.org/) and [wazero](https://wazero.io/).
This provides WASM linear-memory isolation without filesystem or process
access. Network access is **default-deny**: a plugin can only reach hosts
specified in a companion `<name>.allowed_hosts.json` file located next to the
`.wasm` file. If this file is missing, the plugin cannot access the network.

If a plugin requires operator-supplied secrets (WeatherKit's signing key is
currently the only example in this codebase), it reads them from a companion
`<name>.config.json` file. The host expands `${ENV_VAR}` references in this
file using the backend environment at load time.

A plugin can also declare its own operator-editable settings, changeable from
the Settings UI without an env var or a rebuild - see "Plugin-declared config
fields" below.

**The host owns all derived data.** Unit conversion, interpolation, caching,
day-bucketing into the vessel's local timezone, spring/neap classification,
summary sentences, moon phase calculations, and (for points of interest)
distance, bearing, dedupe, sort and limit are all executed host-side. The
plugin only returns raw, provider-native numbers.

Plugins can be written in any language that provides an Extism PDK, such as
TinyGo, Rust, Zig, C, AssemblyScript, C++, or Haskell.

## The contracts

Every plugin exports `id`, `name`, and `ttl_seconds`, along with the fetch
function for its category:

| Category | Fetch exports | Returns |
| --- | --- | --- |
| Tides | `search_stations`, `fetch_tide_chart` | Raw station and tide-extreme data |
| Weather | `fetch_forecast` | Current + multi-day + hourly, all SI units |
| Waves | `fetch_waves` | Hourly wave/swell series with per-component direction and period, optional sea-surface temperature |
| Points of interest | `fetch_poi(lat, lon, radius_m, categories, limit)` | Raw features per category (id, name, position, optional detail/source URL), plus which categories were truncated or unsupported |
| Forecast warnings | `fetch_warnings(lat, lon)` | Current, relevant bulletins only |
| Upper air | `fetch_upper_air` | Hourly 500mb and 1000mb geopotential height, 500mb wind and temperature |

The reference plugins under `docs/examples/` define the exact JSON shape for
each export. Refer to the example for your category alongside this table
rather than relying on written summaries that might diverge from the code.

### Companion files

A plugin consists of a `.wasm` file and up to four optional companion files
sharing its base name. An omitted companion file means that no hosts,
configs, config fields, or secrets are granted.

| File | Shape | Purpose |
| --- | --- | --- |
| `<name>.allowed_hosts.json` | JSON array of hostnames | The only hosts this plugin may reach, over HTTP or FTP. Absent means no network at all. |
| `<name>.config.json` | JSON object of string values | Plugin configuration, resolved once when the plugin loads. A `${VAR}` value is expanded by the host before the plugin sees it. |
| `<name>.config_fields.json` | JSON array of field declarations | Which `config.json` keys are operator-editable from the Settings UI, resolved fresh on every call. See "Plugin-declared config fields" below. |
| `<name>.allowed_secrets.json` | JSON array of secret names | Which stored secrets this plugin's `${VAR}` references may resolve. A secret not listed here is denied and the denial is logged. |

```jsonc
// noaa.allowed_hosts.json
["api.tidesandcurrents.noaa.gov"]
```

```jsonc
// weatherkit.config.json
{
  "key_id": "${WEATHERKIT_KEY_ID}",
  "team_id": "${WEATHERKIT_TEAM_ID}",
  "service_id": "${WEATHERKIT_SERVICE_ID}",
  "private_key": "${WEATHERKIT_PRIVATE_KEY}"
}
```

```jsonc
// weatherkit.allowed_secrets.json
["WEATHERKIT_KEY_ID", "WEATHERKIT_TEAM_ID", "WEATHERKIT_SERVICE_ID", "WEATHERKIT_PRIVATE_KEY"]
```

```jsonc
// google-places.config.json
{
  "api_key": "${GOOGLE_PLACES_API_KEY}"
}
```

### Plugin-declared config fields

A plugin can expose one or more of its own settings for an operator to edit
from the Settings UI, without an env var, a `.env` file, or a rebuild -
`osm-overpass`'s Overpass mirror is the first example. Declare each field in
a `<name>.config_fields.json` sidecar:

```jsonc
// osm-overpass.config_fields.json
[
  {
    "key": "overpass_url",
    "label": "Overpass server",
    "type": "url",
    "placeholder": "https://overpass-api.de/api/interpreter",
    "help": "Blank uses the public overpass-api.de."
  }
]
```

Each entry needs a unique, non-empty `key` (matching a name your plugin
reads via its PDK's config accessor, e.g. `pdk.GetConfig("overpass_url")`
in TinyGo) and a `type` of `"url"` or `"text"`. A malformed sidecar - bad
JSON, an empty or duplicate key, or an unrecognised type - fails plugin load
outright, the same as a malformed `allowed_hosts.json`.

The Settings UI reads this sidecar via `GET /api/plugins/:type/:id`, which
returns a `config_fields` array with each field's declared metadata plus its
currently stored value (blank if never set), and renders one input per
field in that provider's own settings modal. Saving posts
`POST /api/plugins/:type/:id/config` with `{"values": {"<key>": "<value>"}}`;
the backend rejects an unknown key or a non-blank `"url"`-typed value that
isn't an absolute `http(s)` URL (400), otherwise stores it and returns the
updated info. Every stored value is read fresh on the very next call to that
export (`wasm_plugin.go`'s `applyConfigValues`) - no plugin reload or
backend restart needed, unlike an `allowed_hosts.json` /
`allowed_secrets.json` override (ADR 0024). A blank saved value reverts to
whatever `config.json` provides for that key, or the plugin's own built-in
default if `config.json` has no entry for it either.

This is deliberately separate from the allowlist overrides: a config field's
value is plugin-interpreted data (a URL, a limit, a label), not a security
boundary, so it needs no restart and no separate review step the way
widening network access does.

Both allowlists are enforced by the host rather than the WASM sandbox. They
prevent a plugin from receiving unapproved secrets or contacting unlisted
hosts. If a plugin receives approval for both a secret and an external host,
it can transmit that secret to that host. Review third-party companion files
with this in mind.

### Why warnings are the exception

For tides, weather, and waves, the host performs all derived calculations.
For forecast warnings, it does **none**: it performs no zone matching and no
filtering between active and cancelled bulletins. Each plugin resolves its own
zones for a coordinate and returns only bulletins that are currently active.

BOM's zone taxonomy (named coastal zones derived
from state bounding boxes) and NWS's taxonomy (UGC marine zone codes) use
incompatible namespaces. Determining whether a warning remains active also
differs: BOM requires parsing free-text sections, whereas NWS provides
structured CAP alert status fields. Because these models share no common
structure, this logic cannot be generalised into the host.

The host performs one derivation after the plugin has done that work: it
ranks each active wind section's `warning_type` onto a severity ladder for the
alarm paths (`helmcentral.environment.forecastWindWarningLevel`, ADR 0087). The
ladder is a case-insensitive first-match table: `watch` ranks lowest before
anything else is tested, `hurricane` and `storm` rank highest, `gale` next,
and `strong wind`, `small craft`, `wind advisory` and `brisk wind` lowest. A
wind type the ladder does not recognise ranks lowest and is logged by name, so
the vocabulary can be extended. It is never dropped: the plugin has already
said the warning is in force. The operator-facing result of this ladder is
documented as a rule input in [Alarms](../features/alarms.md#values-helmcentral-works-out-for-itself).

### Why points of interest rank host-side

A `poi` plugin returns raw features only - no distance, no bearing, no
ranking. `backend/poi_providers.go` computes distance and bearing from the
vessel's live position (never a client-supplied one), drops anything outside
the requested radius, dedupes near-identical features (a name plus position
match within 5 decimal places), sorts by distance, and applies the limit.

This is the same "host owns derived data" principle as tides/weather/waves,
applied to a case where every provider needs the exact same ranking logic:
`osm-overpass` and `google-places` would otherwise each have to get
haversine distance and initial bearing right independently, and a future
third provider would have to match whichever of the two it copied from. See
[docs/reference/poi-categories.md](../reference/poi-categories.md) for the category list and
[ADR 0091](../adr/0091-points-of-interest-as-a-plugin-kind.md) for the full
reasoning, including why POI's cache cell (0.02 degrees, about 2km) is much
tighter than weather's.

### Optional: a `poi` plugin can also answer place names

A `poi` plugin can additionally export `place_name_at` and `search_places`
to serve as a place-names provider - the position tile, the anchor pin, and
Mate's `find_places` tool. Both exports are optional and both are required
together: a plugin exporting only one is treated as supporting neither.
There is no separate plugin kind or directory for this; the operator picks
which installed, supporting `poi` plugin answers place-name questions via
`ui.place_name_provider` (Settings → Tiles → Place names),
independently of `ui.poi_provider` (Settings → Tiles → Nearby) - the two
commonly name the same plugin but need not.

| Export | Input | Returns |
| --- | --- | --- |
| `place_name_at` | `{lat, lon, radius_m}` | The single best-named feature within `radius_m`, or the legitimate negative `{"name": ""}` |
| `search_places` | `{query, lat, lon, max_results, broad}` | `{search: "exact"\|"regex"\|"none", radius_nm, centred_on, results, note}` - the plugin owns the search ladder, the host owns distance/bearing/dedupe/sort exactly as it does for `fetch_poi` |

`place_name_at` is called on a widening ladder (400m, 1500m, 5000m,
stopping at the first named answer) by the host, not the plugin - the
plugin only ever answers one radius per call. `search_places`' `broad` flag
tells the plugin whether the host already has a saved-route-waypoint match
for this query: when true, a plugin is free to run a more expensive
broadened search if its cheap exact-name search comes back empty; when
false, the plugin should still run its cheap search (it is the only way the
plugin's own copy of a feature's position reaches the model) but can skip
straight to reporting no broader match. An upstream failure - a bad status,
an unparseable body, a rate limit, or (Overpass specifically) a `remark`
field reporting a runtime error - must be returned as a call error, never
masked as an empty result. See
[docs/examples/poi-plugins/osm-overpass/main.go](../examples/poi-plugins/osm-overpass/main.go)'s
`placeNameAt`/`searchPlaces` exports for a full worked implementation, and
[ADR 0101](../adr/0101-place-names-come-from-a-plugin.md) for the full
reasoning.

### The FTP host function

BOM warnings are only reliably available over anonymous FTP (`ftp.bom.gov.au`),
because the BOM website blocks automated HTTP scraping of the same content.
Because WASM guests cannot open raw network sockets, Helmcentral provides a
generic custom Extism host function, `ftp_fetch` (in `backend/wasm_ftp_fetch.go`).
This function is available to all plugin types and is controlled by the same
`allowed_hosts.json` allowlist used for HTTP. It is the only custom host
function in the codebase.

The host function lets BOM use the same plugin interface as other providers
despite its FTP requirement, instead of requiring a built-in Go provider.

## Building a plugin

Reference plugins are located in `docs/examples/`. The NOAA tide plugin is a
complete TinyGo implementation for the NOAA CO-OPS API, written directly for
this interface rather than ported from BOM.

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/tide-plugins/noaa &&
  go mod tidy &&
  tinygo build -o noaa.wasm -target wasip1 -buildmode c-shared main.go
"
```

The same command structure applies to other examples. **Note the build target:**
the NOAA tide plugin consists of a single file (`main.go`), whereas the weather,
wave, and forecast-warning plugins are multi-file packages that use `.` as the
build target:

```bash
docker run --rm -v $(pwd):/src -w /src tinygo/tinygo:latest sh -c "
  cd docs/examples/weather-plugins/open-meteo &&
  go mod tidy &&
  tinygo build -o open-meteo.wasm -target wasip1 -buildmode c-shared .
"
```

Available example directories:

- `docs/examples/tide-plugins/bom`, `docs/examples/tide-plugins/noaa`
- `docs/examples/weather-plugins/open-meteo`, `docs/examples/weather-plugins/weatherkit`
- `docs/examples/wave-plugins/open-meteo-marine`
- `docs/examples/poi-plugins/osm-overpass`, `docs/examples/poi-plugins/google-places`
- `docs/examples/forecast-warnings-plugins/bom`, `docs/examples/forecast-warnings-plugins/nws`
- `docs/examples/upper-air-plugins/open-meteo-upper`

Each directory contains a README with installation instructions.
[docs/examples/weather-plugins/weatherkit/README.md](../examples/weather-plugins/weatherkit/README.md)
also explains how to obtain WeatherKit credentials, and
[docs/examples/poi-plugins/osm-overpass/README.md](../examples/poi-plugins/osm-overpass/README.md)
and
[docs/examples/poi-plugins/google-places/README.md](../examples/poi-plugins/google-places/README.md)
cover the two points-of-interest examples, including Google's category
mapping and billing.

A coordinate-based tide provider (of the kind Helmcentral used to bundle,
before it was removed as an unsafe global fallback - see
[Provider plugins](../reference/plugins.md#why-tides-have-no-default)) can be
built by having `search_stations` return a single synthetic station for the
requested coordinates.

Once built, install it the same way as any plugin - see
[Provider plugins](../reference/plugins.md#installing-a-plugin).
