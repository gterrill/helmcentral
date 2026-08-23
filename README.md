# Helmcentral

A dashboard and alarm system for [SignalK](https://signalk.org/). It puts anchor
watch, tides, weather, routes, tanks and electrical monitoring on one screen at
the helm, and when something goes wrong it tells you on your phone, off the
boat.

Everything runs on the boat. It is a single binary with the web UI compiled into
it, so there is no database server to run and nothing phoning home to a service
that can change its pricing. If you already have a SignalK server, installing it
is one command.

![The Helmcentral dashboard at anchor](docs/images/dashboard.png)

## Why this exists

I spent my working life building software. I now live aboard full time, which
has been a useful education in what "reliable" actually means when you are the
only person on watch.

A boat runs on intermittent power and worse internet, and the person responsible
for it is frequently asleep. That rules out a lot of otherwise sensible designs.
An alarm that only exists in a browser tab is not an alarm. A dashboard that
quietly freezes looks exactly like a calm night. A notification that failed to
send because the LTE dropped out is not a notification.

So the parts of Helmcentral that look over-engineered for a hobby project are
the deliberate ones. Anchor watch runs on the server rather than in the browser.
Alarm rules have mandatory dwell and hysteresis. There is a watchdog on the data
stream itself. Failed notifications are queued and retried instead of dropped.
Those are the parts I would not go to sea without.

The rest is ordinary work done carefully: instruments on a screen you can read
in daylight, in a layout you can rearrange without editing a config file.

---

- **One binary.** The React UI is compiled into the Go binary with `//go:embed`.
  Linux (x86-64, arm64, armv7, which covers every Raspberry Pi), macOS and
  Windows. No runtime dependencies.
- **Alarms on any SignalK path.** Battery voltage, tank level, depth, bilge,
  engine temperature, wind. Rules name paths from the live delta stream, so
  alarming on something new needs no code change. Five SignalK severities, with
  dwell and hysteresis on every rule.
- **Two-way on the notification bus.** Helmcentral reads *and* writes SignalK's
  `notifications.*` tree. Alarms raised by a Victron GX, an N2K device or
  another plugin turn up in its list with no per-source integration, and its own
  rules reach your MFD by the same route.
- **Delivery that survives bad internet.** ntfy, SMTP, webhook, web push, or
  SignalK itself. A failed send is queued and retried with backoff from 30
  seconds out to 30 minutes for up to 24 hours, with every attempt logged.
- **A watchdog on the stream.** If the SignalK connection dies, that is itself
  an alarm. A periodic heartbeat sent off the boat makes its absence one too.
- **Layouts you arrange yourself.** 16 built-in widgets, custom gauges bound to
  any path your server publishes, and embed tiles for anything with a URL.
  Named pages you switch between, persisted server-side.
- **Forecasts with no API key.** Open-Meteo and Open-Meteo Marine are the
  defaults, so weather, wind and swell work on a fresh install.
- **Sandboxed plugins for regional data.** Tide and forecast providers are WASM
  modules loaded from a directory. No filesystem access, no process access, and
  no network beyond a per-plugin allowlist.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
```

Then open `http://<this-machine>:8080/`. On first run Helmcentral searches your
network for a SignalK server and offers what it finds, so there is nothing to
edit by hand.

[Other ways to install](#other-ways-to-install), including Docker, are below.

> [!WARNING]
> **Authentication is off by default** (`auth.mode: none`). A fresh install's
> API can control connected equipment, including generator start/stop and CZone
> switching, with no login. Run it on a trusted boat LAN and do not port-forward
> it to the internet. Setting `auth.mode: signalk` requires login against your
> SignalK server's own accounts; see [Configuration](#configuration). For remote
> access, put it behind a VPN or an authenticating reverse proxy either way.

---

## Contents

- [Requirements](#requirements)
- [Other ways to install](#other-ways-to-install)
- [Configuration](#configuration)
- [What's on the dashboard](#whats-on-the-dashboard)
- [Alarms](#alarms)
- [Provider plugins](#provider-plugins)
- [Development](#development)
- [Architecture](#architecture)
- [Roadmap](#roadmap)
- [Contributing](#contributing)

## Requirements

- **Linux, macOS or Windows.** Linux gets a systemd service. The armv7 and arm64
  builds cover every Raspberry Pi.
- **A SignalK server** reachable on your network, with these plugins:

  | Plugin | Provides |
  | --- | --- |
  | [`signalk-derived-data`](https://github.com/SignalK/signalk-derived-data) | True wind and other derived navigation data |
  | [`tracks`](https://github.com/SignalK/tracks) | Vessel trails and historical path data |
  | [`signalk-venus-plugin`](https://github.com/sbender9/signalk-venus-plugin) | Generator and advanced electrical state (Victron GX devices) |
  | [`signalk-to-influxdb-v2`](https://github.com/tkurki/signalk-to-influxdb-v2) | Optional, only for InfluxDB-backed history |

- **A browser on Baseline 2024 or newer**: Chrome/Edge 111+, Firefox 111+,
  Safari 16.4+, which means iPadOS/iOS 16.4+ on a helm tablet. Older devices are
  out of support, since the shipped CSS is not downlevelled past that floor
  ([ADR 0046](docs/adr/0046-frontend-build-toolchain-and-css-browser-floor.md)).

That is the whole list. Telemetry history is in-memory by default. InfluxDB buys
you longer retention if you want it, and is not otherwise needed.

## Other ways to install

### Install script (recommended)

```sh
curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
```

It detects your platform, verifies the download against the published
checksums, installs to `/usr/local/bin`, creates `/var/lib/helmcentral` for
state, installs the reference plugin bundle, and enables a systemd service on
Linux. **Re-run it any time to upgrade**; your settings and data are left alone.

Pin a version or change locations with `HELMCENTRAL_VERSION`,
`HELMCENTRAL_PREFIX` and `HELMCENTRAL_STATE_DIR`.

Useful afterwards:

```sh
systemctl status helmcentral
journalctl -u helmcentral -f
```

On macOS the script installs the binary and prints how to run it, with no
launchd service. On Windows, download the `.zip` from the
[releases page](https://github.com/gterrill/helmcentral/releases).

### Manual binary download

Grab the archive for your platform from the
[releases page](https://github.com/gterrill/helmcentral/releases), then:

```sh
tar -xzf helmcentral_<version>_linux_arm64.tar.gz
sudo install -m0755 helmcentral /usr/local/bin/helmcentral

# State needs an explicit home, or it lands in the working directory.
sudo mkdir -p /var/lib/helmcentral
HELMCENTRAL_STATE_DIR=/var/lib/helmcentral \
  SETTINGS_FILE=/var/lib/helmcentral/settings.yaml \
  helmcentral
```

The archive also contains `packaging/helmcentral.service` if you want the
systemd unit, and `settings.example.yaml` as a starting config.

### Docker

The image is multi-arch (amd64, arm64, armv7), so it runs on a Raspberry Pi.
Take `docker-compose.yml` from this repo, then, from the directory containing
it:

```sh
# 1. Add the reference plugins first. Compose bind-mounts ./plugins, and without
#    them there are no tide, weather, wave or warning providers. They are
#    deliberately not baked into the image, so you can add or update one without
#    repulling.
mkdir -p plugins && curl -fsSL \
  https://github.com/gterrill/helmcentral/releases/latest/download/helmcentral-plugins-<version>.tar.gz \
  | tar -xz -C plugins

# 2. Start it.
docker compose pull
docker compose up -d --force-recreate
```

Dashboard and API: <http://localhost:9091>. Change the left-hand side of the
port mapping to serve it elsewhere. State lands in `./backend-data`, created on
first run, so nothing needs to exist beforehand.

Stop with `docker compose down`.

## Configuration

Helmcentral starts with no configuration file, and **secrets are never set in
files or environment variables**. Start with none configured, then paste SignalK
credentials, the InfluxDB token and any GeoNames/WeatherKit keys into
Settings → Secrets in the running app, where they are encrypted at rest with
AES-256-GCM ([ADR 0023](docs/adr/0023-encrypted-secrets-store.md)).

> Back up `data/secrets.key`. Lose it and every stored credential is
> unrecoverable.

### Authentication

`auth.mode` defaults to `none`, so upgrading an existing install never locks
anyone out of a running boat. Setting `auth.mode: signalk` in `settings.yaml`
requires login before the API and dashboard respond to anything except
`/api/health` and the login screen.

Helmcentral has no user database of its own. It forwards submitted credentials
to your SignalK server's `/signalk/v1/auth/login` and trusts the answer, mapping
SignalK's `readonly`, `readwrite` and `admin` levels onto matching Helmcentral
permissions. That means SignalK's own security has to be enabled first.
Helmcentral checks at startup and refuses to boot into `auth.mode: signalk`
against a server with security switched off, because "login required" against a
server with no login to require cannot be satisfied.

Turn it on from Settings → Security, which refuses to save unless SignalK's
security is already enabled, so the lockout is prevented at save time rather
than discovered at the next reboot. Turning it back off is never gated on
SignalK being reachable, so that way out always works. Full design:
[ADR 0040](docs/adr/0040-signalk-delegated-authentication.md).

### Reference

- **Full operator reference:** [docs/configuration.md](docs/configuration.md),
  covering every environment variable, state path and startup behaviour.
- **Upgrading across a breaking release:**
  [docs/upgrading.md](docs/upgrading.md).

## What's on the dashboard

Sixteen built-in widgets: Vessel, Apparent Wind, Depth & Tide, Position,
Today & Now, Anchor Watch, Tanks, Route, Nearby Vessels, Battery & Power, Solar,
Alternator, Generator, Switches, Hot Water and Autopilot.

Two of them are not fixed. **Gauge widgets** bind to any path your SignalK
server publishes, with a numeric, radial, bar or lamp display and coloured bands
that reuse the alarm severities, so oil pressure or engine hours do not have to
wait for someone to write a widget for them
([ADR 0039](docs/adr/0039-bindable-gauge-widgets.md)). **Embed tiles** put any
URL in the grid, such as a Grafana panel or a camera feed; the windrose in the
screenshot above is one.

Arrange them yourself: toggle layout mode in the header, then drag, resize or
remove widgets and add them back from a picker. Layouts are named **pages** you
switch between, because "Anchored" and "Underway" want different screens.
Everything is persisted server-side and restored next session
([ADR 0012](docs/adr/0012-configurable-bento-dashboard.md),
[ADR 0013](docs/adr/0013-multi-page-dashboard.md)). The layout stays usable down
to a phone, with three structurally different arrangements across the range
([ADR 0032](docs/adr/0032-responsive-dashboard-below-grid-breakpoint.md)).

Beyond the grid:

- **Anchor watch.** Trail sampling and drag detection both run on the server,
  for your vessel and for nearby AIS targets. Closing the browser cannot silence
  a drag: it is a normal alarm, so it is logged, acknowledgeable, and delivered
  off the boat by whatever transports you have configured. Silencing is a
  server-side acknowledgement, so a second browser is not left ringing. A lost
  GNSS fix never raises a drag, because position freezes at its last trusted
  value during an outage and the stream watchdog reports that separately
  ([ADR 0001](docs/adr/0001-server-owned-trail-sampling.md),
  [ADR 0002](docs/adr/0002-separate-motoring-and-anchor-trails.md),
  [ADR 0038](docs/adr/0038-alarms.md)).
- **Rode planner.** A sidebar on Anchor Watch for pay-out and swing-radius
  planning against tide-corrected depth and gust-seeded wind, usable before the
  anchor is down
  ([ADR 0047](docs/adr/0047-rode-planner-replaces-rode-scope-tile.md)). Its map
  takes pins: click open water to mark a bombie or the nearest shoreline and
  watch the range close as you swing. Pins are shared across every device
  watching the anchorage and expire with the anchoring
  ([ADR 0048](docs/adr/0048-session-bound-anchor-placemarks.md)).
- **Route planning.** Multi-leg waypoint sequences with per-leg distance,
  bearing and ETA. A saved route can be activated, which pushes it to SignalK as
  the vessel's active route for autopilots and MFDs to follow. This is manual
  waypoint planning and nothing more: no hazard avoidance, no weather routing,
  no chart licensing dependency, and Helmcentral does no live navigation itself
  ([ADR 0006](docs/adr/0006-manual-route-planning.md),
  [ADR 0007](docs/adr/0007-signalk-route-activation.md)).
- **Satellite charts.** Upload your own MBTiles and Helmcentral serves them. It
  never fetches or bulk-caches tiles from a live provider, so it takes on no
  licensing exposure
  ([ADR 0011](docs/adr/0011-mbtiles-satellite-chart-upload.md)).
- **Autopilot.** Engage and disengage, mode, heading nudges, tack and gybe
  either way, and dodge, against SignalK's v2 Autopilot API only, with no legacy
  `steering.autopilot.*` write fallback. The tile shows what the pilot last
  reported on the delta stream, never what a command asked for. It greys itself
  if steering data goes quiet, and disables rather than hides any action the
  connected pilot is not currently advertising. Anything that changes who is
  steering takes a deliberate press-and-hold
  ([ADR 0041](docs/adr/0041-autopilot-widget.md)).
- **Weather radar** via an embedded Windy map, centred on the vessel.
- **Max wind gust** over a window you pick per readout: 10m, 30m, 1h or 24h
  ([ADR 0030](docs/adr/0030-selectable-max-gust-windows.md)).

## Alarms

Rules can name **any path the SignalK server publishes**, because ingestion is a
delta-stream subscription rather than a hardcoded path list
([ADR 0037](docs/adr/0037-signalk-delta-stream-ingestion.md)). Alarming on
something new needs no code change.

Helmcentral uses SignalK's own notification vocabulary, with severities
`normal | alert | warn | alarm | emergency` verbatim. That is what makes it work
in both directions: alarms raised elsewhere on the bus show up here
untranslated, and Helmcentral's own rule hits are written back to
`notifications.*`, where a buzzer plugin or an MFD can react to them without
knowing Helmcentral exists.

Three decisions worth spelling out, because they are the ones people ask about:

- **Dwell and hysteresis are mandatory.** A rule has to hold for its dwell
  before it fires, and the value has to travel back past a deadband before it
  clears. Alarm storms are the most common reason people switch marine alarms
  off, and a switched-off alarm is worse than no alarm at all, because you still
  think something is watching.
- **Absence is not a value.** A missing path does not satisfy a threshold, so a
  freshly booted boat does not fire every rule at once. The same rule applies in
  reverse: a live alarm does not clear when its path goes stale, so a dying
  sensor cannot silence its own alarm by going quiet.
- **Failed deliveries are queued, not dropped.** Retried from 30 seconds out to
  30 minutes for 24 hours, then discarded, because a day-old alarm delivered as
  though it were current is its own kind of wrong. The heartbeat is the
  exception and is never queued, since a burst of stale "still alive" messages
  is worse than useless.

Anchor drag and the stream watchdog are built in and travel the same path as
your own rules. See [Anchor watch](#whats-on-the-dashboard) above.

Five transports, none of which needs a paid subscription: **ntfy**
(self-hostable, or the free public server, no account), **SMTP**, **webhook**,
**SignalK `notifications.*`**, which needs no internet at all, and **web push**,
which puts alarms on your phone's lock screen with no app to install and needs
Helmcentral served over https (see
[ADR 0045](docs/adr/0045-web-push-secure-context-and-pwa-shell.md) for a
`tailscale serve` recipe).

`POST /api/alarm-transports/test` probes every enabled transport. Discovering at
3am that the ntfy topic was mistyped is the failure that justifies one button.

Full design and its trade-offs: [ADR 0038](docs/adr/0038-alarms.md).

## Provider plugins

Tides, weather, waves and forecast warnings are **not built in**. Each is a
sandboxed WASM plugin loaded from disk, so adding another region's government
API means dropping a `.wasm` file into a directory. No fork, no Go, no rebuild,
and the new provider appears in the existing Settings dropdown on restart.

| Category | Bundled reference plugins |
| --- | --- |
| Tides | `bom` (Australia), `noaa` (US) |
| Weather | `open-meteo` (worldwide, keyless, default), `weatherkit` (Apple, needs keys) |
| Waves | `open-meteo-marine` (default) |
| Forecast warnings | `bom` (Australia, default), `nws` (US) |

Plugins run under [Extism](https://extism.org/)/[wazero](https://wazero.io/)
with no filesystem access, no process access, and network default-deny: a plugin
reaches only the hosts named in its `allowed_hosts.json` sidecar. The host owns
all derived data (units, interpolation, caching, timezone bucketing), so a
plugin only ever returns raw provider numbers. You can write one in any language
with an Extism PDK.

**Tides are the one thing with no default.** Tide data is tied to physical
station networks rather than a global model, so there is no keyless worldwide
API to hardcode and nothing sensible to fall back to. Pick `ui.tide_provider` in
Settings to match your region. Until you do, `/api/tide-today` returns an error
naming what is missing rather than guessing.

Contracts, the sandbox model, and how to build a plugin:
**[docs/plugins.md](docs/plugins.md)**.

## Development

Requires **Go 1.22** and **Node.js 20+**. The Go toolchain is deliberately
pinned at 1.22, and several dependencies are held back to match, so do not let
`go get -u` bump the `go` directive
([ADR 0037](docs/adr/0037-signalk-delta-stream-ingestion.md)). CI and release
builds use Node 24; the dev containers run Node 20.

In two terminals, from the repo root:

```sh
cd backend && go run .                      # API on :8080
```

```sh
cd frontend && npm install && npm run dev   # Vite dev server on :5173
```

Open <http://localhost:5173>.

The Vite dev server proxies `/api` to `localhost:8080`, so the frontend always
talks to the backend on the same origin, exactly as in a release build where the
SPA is embedded in the binary.

### Tests

```sh
cd backend && go test -short ./...   # -short skips live BOM FTP round-trips
cd frontend && npm test && npm run lint
```

### Docker workflows

```sh
make dev     # backend-dev (air hot-reload) + frontend-dev (Vite), :8080 / :5173
make logs    # tail both
make down    # stop

make e2e-up  # isolated stack on :5174 for anything that mutates state
```

Use the E2E stack for any script that clicks Save. The dev stack bind-mounts
your live `settings.yaml` and can take a real dashboard offline
([ADR 0026](docs/adr/0026-e2e-stack-isolation.md)).

A production-like build is
`docker compose -f docker-compose.dev.yml --profile prod up --build -d backend frontend`.

### Release builds

```sh
goreleaser build --snapshot --clean   # cross-compiles every published target
```

Build hooks build the frontend and stage it into `backend/dist` for the
`//go:embed`, so a snapshot binary is the real thing. Tagging `vX.Y.Z` and
pushing runs [.github/workflows/release.yml](.github/workflows/release.yml),
which publishes the archives, the WASM plugin bundle and the multi-arch image.

## Architecture

```
helmcentral/
├── backend/          # Go REST API; embeds the built frontend
├── frontend/         # React + TypeScript + Vite dashboard
├── plugins/          # WASM providers, by category
├── packaging/        # systemd unit, plugin build script
├── docs/adr/         # Architecture decision records
├── install.sh        # One-line installer
└── docker-compose.yml
```

- **Backend.** Go with the Echo framework, serving the API and the SPA from one
  port. Endpoint reference: [backend/README.md](backend/README.md).
- **Frontend.** React and TypeScript, built with Vite, designed for a helm
  touchscreen.
- **Ingestion.** One WebSocket subscription to SignalK's delta stream,
  reassembled into a snapshot tree. It is the only ingestion path. There is no
  REST fallback and no toggle, because a fallback would mask exactly the
  upstream failures the watchdog exists to catch. REST survives only for probing
  during discovery and connection tests
  ([ADR 0037](docs/adr/0037-signalk-delta-stream-ingestion.md)).
- **Storage.** SQLite for secrets, alarm history and tile caches; JSON files for
  routes and dashboard pages. Telemetry history is an in-memory ring buffer by
  default, with InfluxDB optional for longer retention
  ([ADR 0020](docs/adr/0020-in-memory-telemetry-history-optional-influxdb.md)).
- **Startup is fail-fast.** If the secrets store or a plugin-override database
  cannot be opened, the process exits rather than running degraded.

Durable design decisions live in [docs/adr/](docs/adr/), 48 of them, covering
why each non-obvious trade-off went the way it did.

## Roadmap

- **`auth.mode: signalk` as the default.** Delegated login against your SignalK
  server's own accounts is implemented and opt-in
  ([ADR 0040](docs/adr/0040-signalk-delegated-authentication.md)), but stays off
  by default for this release so upgrading an existing install cannot lock
  anyone out of a running boat. A later major version can flip the default once
  operators have had a release to opt in deliberately.
- **Collapse the client-side `dragging` state onto the server's answer.**
  `use-anchor-watch.ts` still derives its own for map and tile styling. The
  alarm no longer depends on it, but the two can disagree inside the hysteresis
  band ([ADR 0038](docs/adr/0038-alarms.md)).
- **mDNS-based SignalK discovery**, now viable for native installs, since the
  container networking that ruled it out no longer applies
  ([ADR 0029](docs/adr/0029-signalk-discovery.md)).

## Contributing

Issues and pull requests are welcome. CI runs `go vet`, the Go and frontend test
suites, lint, and a full cross-platform release build on every PR, so run those
locally first (see [Development](#development)).

If a change turns on a non-obvious trade-off, add an ADR alongside it in
[docs/adr/](docs/adr/).

## License

[MIT](LICENSE)
