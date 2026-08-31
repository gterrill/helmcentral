# Upgrading

Re-running `install.sh` upgrades in place and leaves your settings and data
alone. The notes below cover the handful of releases that needed a manual step.

If you are installing fresh, none of this applies.

## SignalK delegated authentication (opt-in)

Not a breaking change — no action required. `auth.mode` defaults to `none`
(no authentication, matching every prior release's behaviour), so an upgrade
never locks you out of a running boat.

**Recommended:** once your SignalK server has security enabled, set
`auth.mode: signalk` in `settings.yaml` to require login before the API and
dashboard respond to anything but the login screen. Helmcentral checks at
startup and refuses to boot into `auth.mode: signalk` against a server with
security switched off, so turn SignalK's own security on first. See the
[README's Configuration section](../../README.md#configuration).

## Weather and waves moved to WASM plugins

Applies if you previously ran a version with the hardcoded WeatherKit /
Open-Meteo-marine integration.

- Delete the now-orphaned cache files — weather and wave caching moved to
  per-plugin files:

  ```sh
  rm -f cache/weather_today_cache.json cache/weather_forecast_cache.json
  ```

  Replacements are `cache/weather_wasm_*_cache.json` and
  `cache/wave_wasm_*_cache.json`.

- **If you relied on WeatherKit:** the default provider on upgrade is
  Open-Meteo (keyless). Paste the four WeatherKit credentials into
  Settings → Secrets and select "Apple WeatherKit" under Settings → Weather.

Both providers stay keyless by default, so an install that never touches
Settings keeps working.

## Marine warnings became forecast warnings

- `GET /api/marine-warnings` is gone. Warnings now live at
  `GET /api/forecast-warnings`.
- `cache/bom_marine_warnings_cache.json` is orphaned and can be deleted.
  Warnings are now cached per-plugin under
  `cache/forecast_warnings_wasm_*_cache.json`.
- No environment variables are needed for the default BOM plugin — it is
  keyless, reading BOM's public anonymous FTP mirror.

The rename is the visible part; the change underneath is that warnings became a
plugin category like the rest.

## Tides became plugin-only

The last built-in tide provider (Storm Glass) was removed. There is no built-in
tide provider and no fallback: install a tide plugin for your region and set
`ui.tide_provider` in Settings. Until you do, `/api/tide-today` returns an
error naming what is missing.

The removed built-in was Storm Glass, a worldwide position-based provider. A
global model quietly standing in for a missing local station network gives tide
times that look authoritative and are wrong for your water, so it was removed
rather than left as a fallback. See
[docs/reference/plugins.md](../reference/plugins.md).
