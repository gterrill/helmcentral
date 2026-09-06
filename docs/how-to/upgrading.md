# Upgrading

Re-running `install.sh` upgrades in place and preserves your settings and data.
The notes below cover the releases that required manual steps.

If you are installing fresh, none of this applies.

## Checking what you are running

The release version sits in the footer of the menu sidebar, below the
navigation items. Use it to confirm an upgrade: if the footer still reads the
old tag after a restart, the container is running
the previous image.

The version comes from the running backend rather than the browser. Hovering it gives the full build
stamp including the git revision, which is what to quote when reporting a
problem. A build made outside the release workflow reports `dev` or `(devel)`
instead of a tag.

The same values are served by `GET /api/health` if you would rather read it
from the shell:

```sh
curl -s http://localhost:8080/api/health
```

## SignalK delegated authentication (opt-in)

This is not a breaking change: no action is required. `auth.mode` defaults to
`none` (no authentication, matching every prior release's behaviour), so an
upgrade does not introduce a login requirement.

**Recommended:** Once your SignalK server has security enabled, set
`auth.mode: signalk` in `settings.yaml` to require login before the API and
dashboard respond to anything but the login screen. Helmcentral checks at
startup and will not start in `auth.mode: signalk` against a server with
security switched off, so turn on SignalK's own security first. See the
[README's Configuration section](../../README.md#configuration).

## Weather and waves moved to WASM plugins

This applies if you previously ran a version with the hardcoded WeatherKit /
Open-Meteo-marine integration.

- Delete the orphaned cache files, as weather and wave caching moved to
  per-plugin files:

  ```sh
  rm -f cache/weather_today_cache.json cache/weather_forecast_cache.json
  ```

  The replacements are `cache/weather_wasm_*_cache.json` and
  `cache/wave_wasm_*_cache.json`.

- **If you relied on WeatherKit:** The default provider on upgrade is
  Open-Meteo (keyless). Paste the four WeatherKit credentials into
  Settings → Secrets and select "Apple WeatherKit" under Settings → Weather.

Both providers remain keyless by default, so an install that never touches
Settings continues working.

## Marine warnings became forecast warnings

- `GET /api/marine-warnings` has been removed. Warnings are now served at
  `GET /api/forecast-warnings`.
- `cache/bom_marine_warnings_cache.json` is orphaned and can be deleted.
  Warnings are now cached per-plugin under
  `cache/forecast_warnings_wasm_*_cache.json`.
- No environment variables are needed for the default BOM plugin: it is
  keyless, reading BOM's public anonymous FTP mirror.

The rename reflects the underlying change: warnings became a plugin category
like the rest.

## Tides became plugin-only

The last built-in tide provider (Storm Glass) was removed. There is no built-in
tide provider and no fallback: install a tide plugin for your region and set
`ui.tide_provider` in Settings. Until you do, `/api/tide-today` returns an
error naming what is missing.

Storm Glass was a worldwide position-based provider. A global model
substituting for a missing local station network gives tide times that appear
authoritative but are inaccurate for local waters, so it was removed rather
than left as a fallback. See
[docs/reference/plugins.md](../reference/plugins.md).
