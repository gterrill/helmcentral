# Configuration reference

Helmcentral does not require a configuration file to start. A missing
`settings.yaml` is tolerated, and on first run the dashboard searches your
network for a SignalK server and offers what it finds. The information below
is for tuning an existing installation.

## Where things live

A native install (via `install.sh`) uses:

| Path | Contents |
| --- | --- |
| `/usr/local/bin/helmcentral` | The binary. Self-contained: the web UI is embedded in it. |
| `/var/lib/helmcentral/settings.yaml` | Operator settings, rewritten by the Settings UI on save. |
| `/var/lib/helmcentral/data/` | SQLite stores, routes, dashboard pages, uploaded charts. |
| `/var/lib/helmcentral/cache/` | Anchor-watch state and plugin forecast caches. |
| `/var/lib/helmcentral/plugins/` | WASM providers, by category (`tides/`, `weather/`, `waves/`, `forecast-warnings/`). |
| `/etc/systemd/system/helmcentral.service` | The service unit. |

Under Docker the same tree lives in the `./backend-data` bind mount, with
plugins mounted separately at `/app/plugins`.

### Back this up

`data/secrets.key` decrypts the secrets store. **Lose it and every stored
credential is unrecoverable**: SignalK login, InfluxDB token, and WeatherKit
keys must all be re-entered. Back up the whole state directory; at minimum
back up that file. There is no key-recovery mechanism.

## Environment variables

Non-secret settings use standard environment variables. There is no `.env`
file and nothing loads one; the backend reads these through `os.Getenv` only.
Set them in the systemd unit's `Environment=` lines, the compose `environment:`
block, or your shell.

### Common

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `HELMCENTRAL_STATE_DIR` | *(unset)* | Roots all relative state paths here instead of the working directory. Set by the systemd unit; strongly recommended for any manual run. |
| `SETTINGS_FILE` | `../settings.yaml` | Path to the settings file. The default assumes running from inside `backend/` during development. |
| `VESSEL_STATUS` | `At Anchor` | Fallback status when SignalK reports none |
| `HELMCENTRAL_MASTER_KEY` | *(auto-generated)* | Base64, exactly 32 bytes. Overrides the generated `data/secrets.key`. |
| `CORS_ALLOWED_ORIGINS` | *(unset)* | Comma-separated extra origins allowed to call the API with credentials, on top of the server's own origin (always allowed). Only needed for a frontend hosted somewhere other than this binary. |
| `INFLUX_SOC_MEASUREMENT` | `electrical.batteries.0.capacity.stateOfCharge` | The state-of-charge measurement the Battery & Power tile's overnight (dawn) projection reads, and the path its state-of-charge bands look for a matching alarm rule on. |
| `DAWN_LINEAR_FALLBACK` | `true` | When overnight state-of-charge history is unavailable (InfluxDB not configured, unreachable, or too few usable nights), extrapolate the live rate to sunrise and label the result as such. Set `false` to show a dash with the reason instead. |
| `INFLUX_SHORE_MEASUREMENT` | `electrical.chargers.0.acin.1.current` | The charger's AC input current. A night with a reading above 0.5 A is excluded from the overnight model as shore-powered. |
| `INFLUX_GENERATOR_MEASUREMENT` | `electrical.generator.0.stateNumber` | The generator's state. A night with a reading above zero is excluded from the overnight model as a generator night. |

### Authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `SESSIONS_DB_PATH` | `data/sessions.sqlite` | Session store (see State paths below). |

`auth.mode` is set in `settings.yaml` (or from Settings → Security in the
running application) and has no environment-variable override; `settings.yaml`
is its only source. Changes take effect on the next request, without a restart.

Helmcentral keeps no user database. It forwards credentials to your SignalK
server and maps the `readonly`, `readwrite` and `admin` levels it answers with
onto matching Helmcentral permissions. An unrecognised role fails closed.

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
| `WEBPUSH_DB_PATH` | `data/webpush-subscriptions.sqlite` |
| `TILE_CACHE_PATH` | `data/tile-cache.sqlite` |
| `SAT_CHARTS_DIR` | `data/sat-charts` |
| `PLUGINS_TIDES_DIR` | `plugins/tides` |
| `PLUGINS_WEATHER_DIR` | `plugins/weather` |
| `PLUGINS_WAVES_DIR` | `plugins/waves` |
| `PLUGINS_FORECAST_WARNINGS_DIR` | `plugins/forecast-warnings` |
| `WASM_PLUGIN_TIMEOUT_MS` | `5000` |

## Secrets

SignalK credentials, `INFLUXDB_TOKEN`, the `WEATHERKIT_*` keys and the
`VAPID_*` web push keys are **not** environment variables in normal use. They
reside in an AES-256-GCM encrypted SQLite store and are managed from the
Secrets panel in the Settings UI. Keeping them out of the process environment
is deliberate: every WASM plugin's `${VAR}` configuration expansion reads
from the environment, so a value placed there is accessible to any plugin.

`VAPID_PUBLIC_KEY` and `VAPID_PRIVATE_KEY` are the exception to being managed
from the UI. The VAPID keypair is self-issued, generated automatically on first
start, and never rotated.
**Losing them is unrecoverable.** Every registered web push device stores the
public key it subscribed with, so a new pair requires every phone to
re-subscribe. Helmcentral discards orphaned registrations at boot and logs
the count, rather than allowing pushes to fail silently. Back up
`backend/data/secrets.sqlite` and `backend/data/secrets.key` together.

## Plugins

Tide, weather, wave and forecast-warning data all come from WASM plugins
rather than being built into the core binary, so providers can be added or
swapped without recompilation. See [plugins.md](plugins.md) for plugin
contracts and build instructions. The release bundle ships:

| Category | Plugins |
| --- | --- |
| `tides/` | `bom` (Australia), `noaa` (US) |
| `weather/` | `open-meteo` (worldwide, no key), `weatherkit` (Apple, needs keys) |
| `waves/` | `open-meteo-marine` |
| `forecast-warnings/` | `bom` (Australia), `nws` (US) |

Select the active plugin per category in Settings. Each plugin carries an
`allowed_hosts.json` file next to its `.wasm` binary; the runtime rejects any
outbound host not listed there, so keep the sidecar files alongside the
binaries.

To install or update the bundle manually:

```sh
curl -fsSL https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | sudo tar -xz -C /var/lib/helmcentral/plugins
sudo systemctl restart helmcentral
```

## Startup behaviour

Startup is fail-fast. If the secrets store, session store,
plugin-override store, tile cache or nearby-contacts store cannot be opened,
the process exits rather than running degraded: this is usually a permissions
problem on the state directory, or a `secrets.key` that no longer matches the
store. Check `journalctl -u helmcentral -n 50`.

`auth.mode: signalk` adds one more fail-fast check: Helmcentral probes the
SignalK server's security status once at startup and refuses to boot if
SignalK's own security is disabled. Delegated login requires authentication on
the upstream server. Enable security on the SignalK
server first, or set `auth.mode: none` in `settings.yaml` to boot without it.

Settings → Security refuses to save
`signalk` unless SignalK reports security is already active, preventing an
invalid configuration. This startup failure means
`settings.yaml` was edited manually, or SignalK security was disabled after
Helmcentral was configured. Either way the fix is the same: turn SignalK
security back on, or set `auth.mode: none` in `settings.yaml` and restart.
Turning authentication *off* is never gated on SignalK being reachable, so that
recovery path always works.

## Web push over Tailscale

Web push delivers alarms to a phone's lock screen without requiring an app
install, but browsers only expose the Push API in a **secure context**.
Helmcentral ships no TLS certificate of its own, so on an unencrypted
`http://<lan-ip>:8080` address the feature cannot function. The alarm
settings detect this and state the requirement rather than displaying an
inactive toggle.

The supported configuration uses Tailscale on the machine running Helmcentral:

```sh
tailscale serve --bg 8080
```

That publishes the dashboard at `https://<machine>.<tailnet>.ts.net` with a
valid Let's Encrypt certificate: no public DNS, no certificates to install on
each phone, and no open ports on the boat. Open Helmcentral at that address,
then enable web push under Alarms → Notifications.

**Do not use `tailscale funnel`.** The push service never calls back into
Helmcentral; the only component that requires the secure origin is the
browser, which is already on the tailnet. Funnel exposes the local network to
the public internet and is not needed for web push.

**On iPhone and iPad**, add Helmcentral to the Home Screen first (Share → Add to
Home Screen) and open it from that icon: iOS grants the Push API only to
installed web apps, and only on iOS 16.4 or later. It must be installed
**from the `https://…ts.net` address**: installing from a LAN `http://`
address produces an app that cannot receive push, with no indication on
screen explaining why.

Registered devices are stored in `backend/data/webpush-subscriptions.sqlite`
(`WEBPUSH_DB_PATH`), separate from the alarm log so that clearing alarm history
never disconnects a phone.

## Security

By default (`auth.mode: none`), Helmcentral has **no authentication** and its
API can control connected equipment (generator start/stop, CZone switching).
It is designed for a trusted boat LAN in this mode.

- Do not port-forward it to the internet.
- For remote access, use a VPN (Tailscale, WireGuard) or place it behind a
  reverse proxy that enforces authentication.
- Anyone who can reach the port can change settings and operate equipment.

Set `auth.mode: signalk` to require login via your SignalK server's own
accounts before Helmcentral serves anything other than the login screen. This
configuration still assumes a trusted network to reach the login screen
itself: it does not replace a VPN or reverse proxy for internet exposure, and
every startup with `auth.mode: none` logs a warning explaining the risk.

CORS uses an explicit allowlist (the server's own origin, plus any optional
`CORS_ALLOWED_ORIGINS`) rather than wildcard matching, so credentialed
cross-origin requests only succeed against explicitly named origins.
