# Configuration reference

Helmcentral needs no configuration file to start. A missing `settings.yaml` is
tolerated, and on first run the dashboard sweeps your network for a SignalK
server and offers what it finds. Everything below is for tuning an install
that is already running.

## Where things live

A native install (via `install.sh`) uses:

| Path | Contents |
| --- | --- |
| `/usr/local/bin/helmcentral` | The binary. Self-contained — the web UI is embedded in it. |
| `/var/lib/helmcentral/settings.yaml` | Operator settings, rewritten by the Settings UI on save. |
| `/var/lib/helmcentral/data/` | SQLite stores, routes, dashboard pages, uploaded charts. |
| `/var/lib/helmcentral/cache/` | Anchor-watch state and plugin forecast caches. |
| `/var/lib/helmcentral/plugins/` | WASM providers, by category (`tides/`, `weather/`, `waves/`, `forecast-warnings/`). |
| `/etc/systemd/system/helmcentral.service` | The service unit. |

Under Docker the same tree lives in the `./backend-data` bind mount, with
plugins mounted separately at `/app/plugins`.

### Back this up

`data/secrets.key` decrypts the secrets store. **Lose it and every stored
credential is unrecoverable** — SignalK login, InfluxDB token, WeatherKit
keys all have to be re-entered. Back up the whole state directory; at minimum
back up that file. See
[ADR 0023](adr/0023-encrypted-secrets-store.md).

## Environment variables

Non-secret knobs are real environment variables. There is no `.env` file and
nothing loads one — the backend reads these through `os.Getenv` only. Set them
in the systemd unit's `Environment=` lines, the compose `environment:` block,
or your shell.

### Common

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `HELMCENTRAL_STATE_DIR` | *(unset)* | Roots all relative state paths here instead of the working directory. Set by the systemd unit; strongly recommended for any manual run. |
| `SETTINGS_FILE` | `../settings.yaml` | Path to the settings file. The default assumes running from inside `backend/` during development. |
| `VESSEL_STATUS` | `At Anchor` | Fallback status when SignalK reports none |
| `HELMCENTRAL_MASTER_KEY` | *(auto-generated)* | Base64, exactly 32 bytes. Overrides the generated `data/secrets.key`. |
| `CORS_ALLOWED_ORIGINS` | *(unset)* | Comma-separated extra origins allowed to call the API with credentials, on top of the server's own origin (always allowed). Only needed for a frontend hosted somewhere other than this binary. |

### Authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `SESSIONS_DB_PATH` | `data/sessions.sqlite` | Session store — see State paths below. |

`auth.mode` is set in `settings.yaml` (or from Settings → Security in the
running app) and has no environment-variable override — `settings.yaml` is its
only source. Changes take effect on the next request, without a restart.

See [ADR 0040](adr/0040-signalk-delegated-authentication.md) for the full design.

### SignalK

| Variable | Default | Purpose |
| --- | --- | --- |
| `SIGNALK_VESSEL_PATH` | `/signalk/v1/api/vessels/self` | Self-vessel path |
| `SIGNALK_VESSELS_PATH` | `/signalk/v1/api/vessels` | All-vessels path, for AIS targets |
| `SIGNALK_READ_TIMEOUT_MS` | `3000` | Per-request read timeout |
| `HELMCENTRAL_DISCOVERY_SUBNET` | *(derived)* | `/24` to sweep when the browser hostname and configured address give no usable hint |
| `HELMCENTRAL_DISCOVERY_DIAL_TIMEOUT_MS` | `600` | Per-host dial timeout during discovery |

The server address and port themselves come from `settings.yaml`'s `signalk:`
section, set from the Settings UI.

### State paths

Each of these overrides one file or directory and takes precedence over
`HELMCENTRAL_STATE_DIR`. Defaults are relative, resolved against the state dir
when one is set (see `cacheFilePath` in `backend/weather_tide.go`).

| Variable | Default |
| --- | --- |
| `ANCHOR_WATCH_FILE` | `cache/anchor_watch.json` |
| `ROUTES_FILE` | `data/routes.json` |
| `DASHBOARD_PAGES_FILE` | `data/dashboard-pages.json` |
| `SECRETS_DB_PATH` | `data/secrets.sqlite` |
| `SECRETS_KEY_PATH` | `data/secrets.key` |
| `SESSIONS_DB_PATH` | `data/sessions.sqlite` |
| `PLUGIN_OVERRIDES_DB_PATH` | `data/plugin_overrides.sqlite` |
| `NEARBY_CONTACTS_DB_PATH` | `data/nearby-contacts.sqlite` |
| `TILE_CACHE_PATH` | `data/tile-cache.sqlite` |
| `SAT_CHARTS_DIR` | `data/sat-charts` |
| `PLUGINS_TIDES_DIR` | `plugins/tides` |
| `PLUGINS_WEATHER_DIR` | `plugins/weather` |
| `PLUGINS_WAVES_DIR` | `plugins/waves` |
| `PLUGINS_FORECAST_WARNINGS_DIR` | `plugins/forecast-warnings` |
| `WASM_PLUGIN_TIMEOUT_MS` | `5000` |

## Secrets

SignalK credentials, `INFLUXDB_TOKEN`, `GEONAMES_USERNAME` and the
`WEATHERKIT_*` keys are **not** environment variables in normal use. They live
in an AES-256-GCM encrypted SQLite store and are managed from the Settings
UI's Secrets panel. See [ADR 0023](adr/0023-encrypted-secrets-store.md).

## Plugins

Tide, weather, wave and forecast-warning data all come from WASM plugins
rather than being built in, so a provider can be added or swapped without
rebuilding (see [ADR 0017](adr/0017-wasm-plugin-tide-providers.md), and
[plugins.md](plugins.md) for the contracts and how to build one). The release
bundle ships:

| Category | Plugins |
| --- | --- |
| `tides/` | `bom` (Australia), `noaa` (US) |
| `weather/` | `open-meteo` (worldwide, no key), `weatherkit` (Apple, needs keys) |
| `waves/` | `open-meteo-marine` |
| `forecast-warnings/` | `bom` (Australia), `nws` (US) |

Select which is active per category in Settings. Each plugin carries an
`allowed_hosts.json` next to its `.wasm`; the runtime refuses any outbound host
not listed there, so keep the sidecars alongside the binaries.

To install or update the bundle by hand:

```sh
curl -fsSL https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | sudo tar -xz -C /var/lib/helmcentral/plugins
sudo systemctl restart helmcentral
```

## Startup behaviour

Startup is deliberately fail-fast. If the secrets store, session store,
plugin-override store, tile cache or nearby-contacts store cannot be opened,
the process exits rather than running degraded — usually a permissions
problem on the state directory, or a `secrets.key` that no longer matches the
store. Check `journalctl -u helmcentral -n 50`.

`auth.mode: signalk` adds one more fail-fast check: Helmcentral probes your
SignalK server's security status once at startup and refuses to boot if
SignalK's own security is disabled — "login required" against a server with
no login to require can't be satisfied. Enable security on the SignalK server
first, or set `auth.mode: none` in `settings.yaml` to boot without it.

You should not normally reach that state: Settings → Security refuses to save
`signalk` in the first place unless SignalK reports security is already on, so
the unsatisfiable combination is prevented rather than discovered on the next
reboot. Reaching it means `settings.yaml` was edited by hand, or SignalK's
security was turned off after Helmcentral was configured. Either way the fix
is the same — turn SignalK's security back on, or set `auth.mode: none` in
`settings.yaml` and restart. Turning authentication *off* is never gated on
SignalK being reachable, so that route out always works.

## Security

By default (`auth.mode: none`), Helmcentral has **no authentication** and its
API can control connected equipment (generator start/stop, CZone switching).
It is designed for a trusted boat LAN in this mode.

- Do not port-forward it to the internet.
- For remote access, use a VPN (Tailscale, WireGuard) or put it behind a
  reverse proxy that enforces authentication.
- Anyone who can reach the port can change settings and operate equipment.

Set `auth.mode: signalk` to require login via your SignalK server's own
accounts before Helmcentral serves anything but the login screen — see
[ADR 0040](adr/0040-signalk-delegated-authentication.md). This still assumes a
trusted-enough network to reach the login screen itself: it does not replace
a VPN or reverse proxy for genuine internet exposure, and every startup with
`auth.mode: none` logs a warning naming the risk so it isn't easy to run this
way by accident.

CORS is an explicit allowlist (the server's own origin, plus optional
`CORS_ALLOWED_ORIGINS`) rather than the wildcard earlier releases sent, so a
credentialed cross-origin request only succeeds against an origin you named.
