# Configuration reference

Helmcentral doesn't need a configuration file to start. It copes with a missing
`settings.yaml`, and on first run the dashboard searches your network for a
SignalK server and offers what it finds. This page is for tuning an existing
installation.

## Where things live

A native install (via `install.sh`) uses:

| Path | Contents |
| --- | --- |
| `/usr/local/bin/helmcentral` | The binary. Self-contained: the web UI is embedded in it. |
| `/var/lib/helmcentral/settings.yaml` | Operator settings, rewritten by the Settings UI on save. |
| `/var/lib/helmcentral/data/` | Databases, dashboard pages, uploaded charts, the anchor watch and its placemarks. |
| `/var/lib/helmcentral/cache/` | Plugin forecast caches. |
| `/var/lib/helmcentral/plugins/` | Provider plugins, by category (`tides/`, `weather/`, `waves/`, `forecast-warnings/`). |
| `/etc/systemd/system/helmcentral.service` | The service unit. |

Under Docker the same tree lives in the `./backend-data` bind mount, with
plugins mounted separately at `/app/plugins`.

### Back this up

`data/secrets.key` decrypts the secrets store. **If you lose it, every stored
credential is unrecoverable.** You'd have to re-enter the SignalK login, the
InfluxDB token and the WeatherKit keys. Back up the whole state directory, or at
minimum that file. There is no key recovery.

`data/documents/` (every uploaded file's bytes) and `data/helmcentral.sqlite`
are one backup unit. The database holds documents, notes, the equipment
registry, inventory, maintenance, Mate's conversation history, the AIS sighting
log, alarm history, your alarm rules, registered phone alert devices and plugin
settings. That's
everything Helmcentral knows about your boat and this installation.

On its own the database describes files it can no longer produce, and the
folder on its own is just anonymous content hashes. Once a few manuals and a
season's worth of receipts and photos have gone through it, expect
`data/documents/` to be the largest single thing in the state directory.

Alarm history trims itself, so you never need to clear it out by hand. Don't
delete `data/helmcentral.sqlite` to tidy up old alarms, because that would take
your documents, inventory and maintenance records with it.

`data/helmcentral.sqlite` runs in WAL mode. A plain file copy taken while
Helmcentral is running can therefore miss data still sitting in its `-wal`
sidecar. Back it up with the service stopped, or take a live backup with the
`sqlite3` tool instead of copying the file directly:

```sh
sqlite3 data/helmcentral.sqlite ".backup /path/to/backup/helmcentral.sqlite"
```

Never copy `-wal` or `-shm` separately from the main file while Helmcentral is
running.

## Environment variables

Non-secret settings use standard environment variables. There is no `.env` file
and nothing loads one. The backend reads these from the process environment
only. Set them in the systemd unit's `Environment=` lines, the compose
`environment:` block, or your shell.

### Common

| Variable | Default | Purpose |
| --- | --- | --- |
| `PORT` | `8080` | HTTP listen port |
| `HELMCENTRAL_STATE_DIR` | *(unset)* | Roots all relative state paths here instead of the working directory. Set by the systemd unit. Strongly recommended for any manual run. |
| `SETTINGS_FILE` | `../settings.yaml` | Path to the settings file. The default assumes running from inside `backend/` during development. |
| `VESSEL_STATUS` | `At Anchor` | Fallback status when SignalK reports none |
| `HELMCENTRAL_MASTER_KEY` | *(auto-generated)* | Base64, exactly 32 bytes. Overrides the generated `data/secrets.key`. |
| `CORS_ALLOWED_ORIGINS` | *(unset)* | Comma-separated extra origins allowed to call the API with credentials, on top of the server's own origin (always allowed). Only needed for a frontend hosted somewhere other than this binary. |
| `DAWN_LINEAR_FALLBACK` | `true` | When overnight state-of-charge history is unavailable (InfluxDB not configured, unreachable, or too few usable nights), extrapolate the live rate to sunrise and label the result as such. Set `false` to show a dash with the reason instead. |

There's also a profiling switch for diagnosing a running instance's performance.
See [Configuration internals](../developers/configuration.md) if you've been
asked to use it.

### Authentication

| Variable | Default | Purpose |
| --- | --- | --- |
| `SESSIONS_DB_PATH` | `data/sessions.sqlite` | Session store (see State paths below). |

You set `auth.mode` in `settings.yaml` (or from Settings → Security in the
running application). It has no environment-variable override, so
`settings.yaml` is its only source. Changes take effect on the next request,
without a restart.

Helmcentral keeps no user database. It forwards credentials to your SignalK
server. The server answers with a `readonly`, `readwrite` or `admin` level, and
Helmcentral maps that onto matching Helmcentral permissions. An unrecognised
role gets no access.

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

### Overpass

The Overpass server is the `osm-overpass` points-of-interest plugin's own
setting, not an app-level one. The gear icon on osm-overpass's card opens the
same **Overpass server** field, under either **Settings → Plugins → Nearby** or
**Settings → Plugins → Place names**. There is no environment-variable override
and no `settings.yaml` entry. Saving takes effect on the plugin's next call,
with no restart.

- **Blank:** The default. It uses the public
  `https://overpass-api.de/api/interpreter`.
- **What the save accepts:** It rejects anything that isn't an absolute URL.
- **Plain `http://`:** The plugin itself refuses it on every call unless the
  address is on your own network. That means a private or loopback address,
  `localhost`, a name ending `.local`, `.lan`, `.internal`, `.home.arpa` or
  `.ts.net`, a Tailscale address, or a single-word name. Everything else must be
  `https://`.

This one setting serves everything the plugin does. Nearby, place-name
resolution (the position tile, the anchor pin) and Mate's place-name questions
all use it, so there's one Overpass address to keep. Point it at a mirror such
as `https://overpass.openstreetmap.fr/api/interpreter` when your network refuses
`overpass-api.de`. See
[poi-categories.md](poi-categories.md#the-default-provider-openstreetmap-via-overpass)
for what this plugin covers.

A mirror other than those two also needs adding to the plugin's own allowlist
file. See [Provider plugins](plugins.md#plugin-declared-settings) and the
developer documentation for how. To run a server of your own aboard, see [Run
your own Overpass server](../how-to/run-your-own-overpass-server.md).

### Anchor Watch

`anchor.min_clearance_at_low_m` in `settings.yaml` (or **Clearance at Low
Water** under Settings → Anchor Watch) is the water, in metres, that you want
left under the keel at the next low tide. Anchor Watch's low-water clearance
warning appears when it would be less. There is no environment-variable override. It
defaults to `0.5`. Zero is accepted (a deliberate "warn only once the keel would
touch"), but a negative value isn't.

### State paths

Each of these overrides one file or directory and takes precedence over
`HELMCENTRAL_STATE_DIR`. Defaults are relative, and when a state dir is set
they're resolved against it.

| Variable | Default |
| --- | --- |
| `ANCHOR_WATCH_FILE` | `data/anchor_watch.json` |
| `ANCHOR_PLACEMARKS_FILE` | `data/anchor_placemarks.json` |
| `DASHBOARD_PAGES_FILE` | `data/dashboard-pages.json` |
| `ALARM_RULES_FILE` | `data/alarm-rules.json` (see below) |
| `SECRETS_DB_PATH` | `data/secrets.sqlite` |
| `SECRETS_KEY_PATH` | `data/secrets.key` |
| `SESSIONS_DB_PATH` | `data/sessions.sqlite` |
| `HELMCENTRAL_DB_PATH` | `data/helmcentral.sqlite` |
| `DOCUMENTS_DIR` | `data/documents` |
| `TILE_CACHE_PATH` | `data/tile-cache.sqlite` |
| `PLUGINS_TIDES_DIR` | `plugins/tides` |
| `PLUGINS_WEATHER_DIR` | `plugins/weather` |
| `PLUGINS_WAVES_DIR` | `plugins/waves` |
| `PLUGINS_FORECAST_WARNINGS_DIR` | `plugins/forecast-warnings` |
| `WASM_PLUGIN_TIMEOUT_MS` | `15000` |

`ALARM_RULES_FILE` is only read on the first start after upgrading from a
release that kept your alarm rules in a file. If the file is there, Helmcentral
copies the rules into `data/helmcentral.sqlite` and renames the file to
`alarm-rules.json.imported`, in the same folder. After that the file is never
read or written again, and your rules are edited in the Alarms drawer as before.
If the file can't be read, Helmcentral won't start and leaves it as it is. It
also won't start if `alarm-rules.json.imported` is already there, so move the
older copy aside first.

## Secrets

SignalK credentials, `INFLUXDB_TOKEN`, the `WEATHERKIT_*` keys,
`OPENROUTER_API_KEY` and the `VAPID_*` web push keys are **not** environment
variables in normal use. They live in an AES-256-GCM encrypted store. You enter
each one in the Settings section it belongs to (SignalK, InfluxDB, Mate,
Alarms) or in a weather provider's settings.

Keeping them out of the process environment is deliberate. Every plugin's own
`${VAR}` configuration expansion reads from the environment, so a value placed
there is visible to any plugin.

Each credential belongs to the destination it was entered for, so a save that
repoints the destination drops it:

- **SignalK:** Changing the SignalK address (or port) clears the stored SignalK
  username and password.
- **InfluxDB:** Changing the InfluxDB URL clears `INFLUXDB_TOKEN`.
- **Re-entering:** Enter the credential again in the same section.
- **Other edits:** Editing anything else on the Settings page, including the
  rest of the InfluxDB block (org, bucket), never touches either credential.

The `assistant:` block in `settings.yaml` holds Mate's non-secret
configuration:

- `enabled`
- `model`
- `document_model` (separate model the document library's indexer uses for
  OCR and suggested title, summary and tags. Blank uses a default. Also set in
  Settings → Mate → Document indexing, where it's picked from the models that
  can read images)
- `embedding_model` (separate model the document library's indexer uses for
  semantic search, vectorising every chunk of a consented document. Blank turns
  semantic search off entirely and leaves keyword search as the library's only
  retriever)
- `embedding_dimensions` (vector length requested from `embedding_model`. `0`
  or a negative value uses a default, and anything above 4096 is capped there)
- `notes`
- `allowed_models` (list of model patterns for OpenRouter Auto)
- `excluded_models` (list of model ids to block for OpenRouter Auto)
- `cost_tier` (`low`, `medium`, `high`, `xhigh`, `max`)
- `voice_input`
- `read_aloud`
- `wake_word`
- `verbose_run_log` (writes a log line for every round of every Mate run. Off by default. A run that fails always logs why)

`allowed_models`, `excluded_models`, and `cost_tier` only affect routing when
`model` is `openrouter/auto` (or `openrouter/auto-beta`).

Mate's conversation history lives in `helmcentral.sqlite`
(`HELMCENTRAL_DB_PATH`) alongside your other records. The in-app help Mate
reads when a question is about Helmcentral itself is separate. It ships built
into every official release and doesn't depend on your conversation history or
this database.

`VAPID_PUBLIC_KEY` and `VAPID_PRIVATE_KEY` are the exception, because you don't
manage them from the UI. Helmcentral generates the keypair itself on first start
and never rotates it. **Losing it is unrecoverable.** Every registered web push
device stores the public key it subscribed with, so a new pair means every phone
has to re-subscribe. Helmcentral discards orphaned registrations at boot and
logs the count, so pushes don't fail silently. Back up
`backend/data/secrets.sqlite` and `backend/data/secrets.key` together.

## Roles

Which equipment item plays each single-choice part is chosen under **Settings →
SignalK → Roles** and kept in the `roles:` block of `settings.yaml`. Each value
is an equipment item's id, except the generator AC input, which is the AC
circuit's instance id.

| Key | Takes | Optional |
| --- | --- | --- |
| `house_bank` | battery item | no |
| `voltage_readout_battery` | battery item | no |
| `port_alternator`, `starboard_alternator` | alternator item (must differ) | yes |
| `shore_charger` | charger item | yes |
| `inverter` | inverter item | yes |
| `generator` | generator item | yes |
| `generator_ac_instance` | AC circuit instance id | yes |

An empty value means the question hasn't been answered. An optional role also
accepts the value `none`, the **This boat has none** choice. It counts as
answered for the Setup list and shows no figures, exactly like an empty role.
`none` is refused for the house bank and the voltage readout battery. Deleting
an item blanks the roles that named it and leaves `none` answers alone.

## Plugins

Tide, weather, wave, points-of-interest, forecast-warning and upper-air data
all come from provider plugins. They aren't built into Helmcentral itself, so a
provider can be added or swapped without an upgrade. See
[Provider plugins](plugins.md) for what ships, how to install one, and the
settings a plugin can expose.

## Startup behaviour

Startup is fail-fast. If the secrets store, session store, tile cache or
`helmcentral.sqlite` itself can't be opened, the process exits rather than
running degraded. The usual causes are a permissions problem on the state
directory or a `secrets.key` that no longer matches the store. Check
`journalctl -u helmcentral -n 50`.

Upgrading from a release that still wrote `documents.sqlite`,
`assistant.sqlite`, `nearby-contacts.sqlite`, `alarm-log.sqlite`,
`webpush-subscriptions.sqlite` or `plugin_overrides.sqlite` separately needs a
one-time migration before Helmcentral will start. If any of those files are
still in the state directory, startup refuses and names the exact command to
run. The **Breaking** entry for the release you're upgrading to in
`CHANGELOG.md` has that command too.

`auth.mode: signalk` adds one more fail-fast check. Helmcentral probes the
SignalK server's security status once at startup. It refuses to boot if
SignalK's own security is disabled, because delegated login needs
authentication on the upstream server. Enable security on the SignalK server
first, or set `auth.mode: none` in `settings.yaml` to boot without it.

Settings → Security refuses to save `signalk` unless SignalK reports security
is already active. If startup fails on this check, `settings.yaml` was edited
by hand or SignalK security was disabled after Helmcentral was configured. The
fix is the same either way: turn SignalK security back on, or set
`auth.mode: none` in `settings.yaml` and restart. Turning authentication *off*
is never gated on SignalK being reachable, so that way out always works.

## Web push over Tailscale

Web push puts alarms on a phone's lock screen without an app install, but
browsers only offer the Push API in a **secure context**. Helmcentral ships no
TLS certificate of its own, so on an unencrypted `http://<lan-ip>:8080` address
the feature can't work. The alarm settings detect this and state the
requirement instead of showing an inactive toggle.

The supported configuration uses Tailscale on the machine running Helmcentral:

```sh
tailscale serve --bg 8080
```

That publishes the dashboard at `https://<machine>.<tailnet>.ts.net` with a
valid Let's Encrypt certificate. It needs no public DNS, no certificates on
each phone and no open ports on the boat. Open Helmcentral at that address,
then enable web push under Alarms → Notifications.

**Do not use `tailscale funnel`.** The push service never calls back into
Helmcentral. The only thing that needs the secure origin is the browser, which
is already on the tailnet. Funnel exposes the local network to the public
internet and isn't needed for web push.

**On iPhone and iPad**, add Helmcentral to the Home Screen first (Share → Add to
Home Screen) and open it from that icon. iOS grants the Push API only to
installed web apps, and only on iOS 16.4 or later. Install it
**from the `https://…ts.net` address**. An app installed from a LAN `http://`
address can't receive push, and nothing on screen tells you why.

Registered devices are stored in `backend/data/helmcentral.sqlite`
(`HELMCENTRAL_DB_PATH`), alongside your other records.

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
still assumes a trusted network to reach the login screen itself. It doesn't
replace a VPN or reverse proxy for internet exposure. Every startup with
`auth.mode: none` logs a warning explaining the risk.

CORS uses an explicit allowlist (the server's own origin, plus any optional
`CORS_ALLOWED_ORIGINS`) rather than wildcard matching, so credentialed
cross-origin requests only succeed against explicitly named origins.
