# Helmcentral

A dashboard and alarm system for [SignalK](https://signalk.org/). It puts anchor
watch, tides, weather, routes, tanks and electrical monitoring on one screen at
the helm, and sends alarm notifications to your phone when you are off the boat.

Helmcentral runs on the boat as a single binary with the web UI compiled into
it. It needs no database server or cloud account. If you already have a SignalK
server, you can install it with one command.

![The Helmcentral dashboard at anchor](docs/images/dashboard.png)

## Why this exists

I spent my working life building software. I now live aboard full time, which
has shaped how I build software for use on a boat.

A boat has intermittent power and unreliable internet, and the person
responsible for it is often asleep. Alarms need to keep running when the browser
is closed, stale readings need to be visible, and failed notifications need to
be retried.

Helmcentral runs anchor watch on the server. Alarm rules have mandatory dwell
and hysteresis, a watchdog monitors the data stream, and failed notifications
are queued for retry.

The dashboard is designed to be readable in daylight, with a layout you can
rearrange without editing a config file.

---

- **One binary.** The React UI is compiled into the Go binary with `//go:embed`.
  Linux (x86-64, arm64, armv7, which covers every Raspberry Pi), macOS and
  Windows. No runtime dependencies.
- **Alarms on any SignalK path.** Battery voltage, tank level, depth, bilge,
  engine temperature, wind. Rules name paths from the live delta stream, so
  alarming on something new needs no code change. Five SignalK severities, with
  dwell and hysteresis on every rule.
- **SignalK notifications.** Helmcentral reads and writes SignalK's
  `notifications.*` tree. Alarms raised by a Victron GX, an N2K device or
  another plugin turn up in its list with no per-source integration, and its own
  rules reach your MFD by the same route.
- **Notification retries.** ntfy, SMTP, webhook, web push, or
  SignalK itself. A failed send is queued and retried with backoff from 30
  seconds out to 30 minutes for up to 24 hours, with every attempt logged.
- **Connection monitoring.** Losing the SignalK connection raises an alarm.
  An external service can also monitor a periodic heartbeat sent off the boat.
- **Configurable layouts.** 17 built-in widgets, custom gauges bound to
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

Docker, manual binaries and upgrade instructions:
[docs/how-to/install.md](docs/how-to/install.md).

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
- [Configuration](#configuration)
- [What you get](#what-you-get)
- [Provider plugins](#provider-plugins)
- [Documentation](#documentation)
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
  | [`mayara-server-signalk-plugin`](https://github.com/MarineYachtRadar/mayara-server-signalk-plugin) | Optional, ARPA radar targets. Needs a [mayara-server](https://github.com/MarineYachtRadar/mayara-server) reaching your radar |

- **A browser on Baseline 2024 or newer**: Chrome/Edge 111+, Firefox 111+,
  Safari 16.4+, which means iPadOS/iOS 16.4+ on a helm tablet. Older devices are
  out of support, since the shipped CSS is not downlevelled past that floor.

Telemetry history is in-memory by default. InfluxDB is optional for longer
retention. Radar is also optional and remains off unless its dependencies are
available.

## Configuration

Helmcentral starts with no configuration file, and **secrets are never set in
files or environment variables**. Start with none configured, then paste SignalK
credentials, the InfluxDB token and any WeatherKit keys into
Settings → Secrets in the running app, where they are encrypted at rest with
AES-256-GCM.

> Back up `data/secrets.key`. Lose it and every stored credential is
> unrecoverable.

### Authentication

`auth.mode` defaults to `none`, so upgrading an existing install never locks
anyone out of a running boat. Setting `auth.mode: signalk` requires login before
the API and dashboard respond to anything except `/api/health` and the login
screen.

Helmcentral has no user database of its own. It forwards submitted credentials
to your SignalK server, mapping SignalK's `readonly`,
`readwrite` and `admin` levels onto matching Helmcentral permissions. An
unrecognised role fails closed rather than falling back to something permissive.

Enable SignalK's security first, then turn on Helmcentral's login from
Settings → Security. Helmcentral refuses to save this setting unless SignalK
security is already on, to prevent a lockout after reboot. Disabling login does
not require SignalK to be reachable.

Every environment variable, state path and startup behaviour:
[docs/reference/configuration.md](docs/reference/configuration.md).

## What you get

**[The dashboard](docs/features/dashboard.md).** Seventeen built-in widgets,
plus gauge widgets that bind to any path your server publishes and embed tiles
that put any URL in the grid. Arrange them yourself into named pages you switch
between, persisted server-side. A gauge's coloured band set into an alarm
severity defines an alarm rule.

**[Anchor watch](docs/features/anchor-watch.md).** Trail sampling and drag
detection run on the server, even when the browser is closed. A
lost GNSS fix does not raise a drag alarm. The rode planner does pay-out and swing-radius
planning against tide-corrected depth before the anchor is down, on a map that
takes shared pins for hazards.

**[Alarms](docs/features/alarms.md).** Rules on any SignalK path, with mandatory
dwell and hysteresis, using SignalK's own severity vocabulary in both
directions. Five transports, none needing a paid subscription. Failed deliveries
are queued and retried, not dropped.

Other features include route planning that pushes an active route to SignalK for your
autopilot, satellite charts from your own MBTiles, autopilot control on
SignalK's v2 API, ARPA radar targets, and an embedded weather radar.

Out of scope: hazard avoidance, weather routing, live
navigation, or anything requiring a chart licence.

## Provider plugins

Tides, weather, waves and forecast warnings come from sandboxed WASM plugins
loaded from disk. Install a provider's `.wasm` file in its plugin directory,
then restart Helmcentral to make it available in Settings. There is no need to
rebuild Helmcentral.

| Category | Bundled reference plugins |
| --- | --- |
| Tides | `bom` (Australia), `noaa` (US) |
| Weather | `open-meteo` (worldwide, keyless, default), `weatherkit` (Apple, needs keys) |
| Waves | `open-meteo-marine` (default) |
| Forecast warnings | `bom` (Australia, default), `nws` (US) |

Plugins run under [Extism](https://extism.org/)/[wazero](https://wazero.io/)
with no filesystem access, no process access, and network default-deny: a plugin
reaches only the hosts named in its `allowed_hosts.json` sidecar. The host owns
all derived data, so a plugin only ever returns raw provider numbers. You can
write one in any language with an Extism PDK.

**Tides have no default provider.** The available providers use regional station
networks rather than a global model. Pick `ui.tide_provider` in
Settings to match your region. Until you do, `/api/tide-today` returns an error
naming the missing configuration.

Contracts, the sandbox model, and how to build a plugin:
[docs/reference/plugins.md](docs/reference/plugins.md).

## Documentation

Start with [docs/index.md](docs/index.md). These pages also ship inside
Helmcentral itself: the `?` button in the app's header opens the page for
whatever screen you're on.

| | |
| --- | --- |
| [docs/features/](docs/features/) | What each part does and where it stops |
| [docs/how-to/](docs/how-to/) | Install, upgrade, develop |
| [docs/reference/](docs/reference/) | Configuration, plugin contracts, engine profiles |
| [docs/adr/](docs/adr/) | Architecture decisions and their rationale |

## Development

Requires Go 1.22 and Node.js 20 or newer. From the repository root, in two
terminals:

```sh
cd backend && go run .                      # API on :8080
```

```sh
cd frontend && npm install && npm run dev   # Vite dev server on :5173
```

```sh
cd backend && go test -short ./...
cd frontend && npm test && npm run lint
```

Docker workflows, the isolated E2E stack and release builds:
[docs/how-to/development.md](docs/how-to/development.md).

## Architecture

```
helmcentral/
├── backend/          # Go REST API; embeds the built frontend
├── frontend/         # React + TypeScript + Vite dashboard
├── plugins/          # WASM providers, by category
├── packaging/        # systemd unit, plugin build script
├── docs/             # Operator docs and architecture decision records
├── install.sh        # One-line installer
└── docker-compose.yml
```

- **Backend.** Go with the Echo framework, serving the API and the SPA from one
  port. Endpoint reference: [backend/README.md](backend/README.md).
- **Frontend.** React and TypeScript, built with Vite, designed for a helm
  touchscreen.
- **Ingestion.** One WebSocket subscription to SignalK's delta stream,
  reassembled into a snapshot tree. It is the only ingestion path. There is no
  REST fallback or toggle, because a fallback could mask upstream failures
  monitored by the watchdog. REST is used only for probing
  during discovery and connection tests.
- **Storage.** SQLite for secrets, alarm history and tile caches; JSON files for
  routes and dashboard pages. Telemetry history is an in-memory ring buffer by
  default, with InfluxDB optional for longer retention.
- **Startup is fail-fast.** If the secrets store or a plugin-override database
  cannot be opened, the process exits rather than running degraded.

### Why Helmcentral isn't a SignalK plugin

SignalK normalizes NMEA 2000 data into a single tree with documented paths and
standard SI units. Helmcentral uses that translation rather than implementing
its own NMEA 2000 decoder.

SignalK runs plugins in-process inside the Node
runtime, where any plugin can register arbitrary HTTP routes, inject spoofed
delta updates, read server configuration or crash the event loop. Helmcentral
runs separately to isolate its alarm processing from those plugins.

NMEA 2000 has no device
authentication, so any node on the CAN bus can claim an address and broadcast
any PGN. Application-level security cannot authenticate that source data.
Commands that can engage an autopilot or switch a CZone breaker need access
controls. The network boundary between the vessel's bus and the internet also
needs protection.

Helmcentral therefore treats SignalK strictly as a translation layer and owns
those boundaries itself: a single read-only delta subscription into an isolated
state snapshot, role resolution downstream of SignalK that fails closed on an
unrecognised role, and third-party provider code confined to WASM with linear
memory isolation, host allowlists and hard execution timeouts.

## Roadmap

- **Inventory tracking**, with stocktakes done by walking the boat with an RFID
  reader rather than opening lockers. Designed and documented, not yet built:
  [docs/features/inventory-tracking.md](docs/features/inventory-tracking.md).
- **Maintenance and service history**, tied to live engine hours so a service
  falling due raises an ordinary alarm and logging the work clears it.
- **`auth.mode: signalk` as the default.** Delegated login is implemented and
  opt-in, but stays off by default for this release so upgrading an existing
  install cannot lock anyone out of a running boat. A later major version can
  flip the default once operators have had a release to opt in deliberately.
- **mDNS-based SignalK discovery**, now viable for native installs since the
  container networking that ruled it out no longer applies.

## Contributing

Issues and pull requests are welcome. CI runs `go vet`, the Go and frontend test
suites, lint, and a full cross-platform release build on every PR, so run those
locally first (see [Development](#development)).

If a change turns on a non-obvious trade-off, add an ADR alongside it in
[docs/adr/](docs/adr/). If it changes what an operator sees, update the affected
page under [docs/](docs/) too.

## License

[MIT](LICENSE)
