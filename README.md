# Helmcentral

A dashboard and alarm system for [SignalK](https://signalk.org/). It puts anchor
watch, tides, weather, routes, tanks, points of interest and electrical
monitoring on one screen at the helm, drives a wall display on its own, and
sends alarm notifications to your phone when you are off the boat.

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
- **Weather warnings from the first boot.** Thirteen rules are created on first
  run and ten of them are already on: a barometer ladder reading the three-,
  twelve- and twenty-four-hour tendency your marine forecast already quotes, and
  four rules bound to the official warnings in force for your own zone. The
  three that need local calibration ship switched off and say why.
- **SignalK notifications.** Helmcentral reads and writes SignalK's
  `notifications.*` tree. Alarms raised by a Victron GX, an N2K device or
  another plugin turn up in its list with no per-source integration, and its own
  rules reach your MFD by the same route.
- **Notification retries.** ntfy, SMTP, webhook, web push, or
  SignalK itself. A failed send is queued and retried with backoff from 30
  seconds out to 30 minutes for up to 24 hours, with every attempt logged.
- **Connection monitoring.** Losing the SignalK connection raises an alarm.
  An external service can also monitor a periodic heartbeat sent off the boat.
- **Configurable layouts.** 21 built-in tiles, custom gauges bound to
  any path your server publishes, Nearby maps of the points of interest around
  the vessel, and embed tiles for anything with a URL. Named pages you switch
  between, persisted server-side.
- **Wall displays, from pages you already have.** Name a screen, give it a size,
  a magnification and an orientation, then assign pages to it; `/display/<name>`
  cycles through them fullscreen with no chrome, on its own. Several screens,
  each with its own pages: a strip at the helm and a television in the saloon
  are both wall displays.
- **Every screen has a URL.** `/forecast`, `/alarms`, `/settings/alarms`,
  `/dashboard/<page id>`. Send someone a link and it opens where you meant.
  Back and Forward work, and a tapped alarm notification lands on the Alarms
  panel.
- **Mate, a chat assistant on your own key.** Ask a passage-planning question
  and it answers from the boat's live position, your forecast providers and
  your tide station, by typing or by voice. You bring an OpenRouter account and
  pick the model; every reply shows what that reply cost.
- **The help is in the binary.** The `?` button in the header opens the page
  for the screen you are on, with no connection needed. Mate reads the same
  pages when you ask it how something works.
- **Forecasts with no API key.** Open-Meteo and Open-Meteo Marine are the
  defaults, so weather, wind and swell work on a fresh install.
- **Sandboxed plugins for regional data.** Tide, forecast, wave, warning and
  points-of-interest providers are WASM modules loaded from a directory. No
  filesystem access, no process access, and no network beyond a per-plugin
  allowlist.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/gterrill/helmcentral/main/install.sh | sh
```

Then open `http://<this-machine>:8080/`. On first run Helmcentral searches your
network for a SignalK server and offers what it finds, so there is nothing to
edit by hand.

Every panel has its own address, so once it is up you can link straight to one:

```
http://<this-machine>:8080/forecast          # the forecast drawer
http://<this-machine>:8080/anchor-watch      # anchor watch
http://<this-machine>:8080/display/flybridge # a wall display, by its name
```

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
  A wall panel should be checked with `/display-probe.html` before you mount it;
  see [Set up a wall display](docs/how-to/set-up-a-wall-display.md).

Telemetry history is in-memory by default. InfluxDB is optional for longer
retention, and Mate uses it for passage and fuel estimates when it is there.
Radar is also optional and remains off unless its dependencies are available.

## Configuration

Helmcentral starts with no configuration file, and **secrets are never set in
files or environment variables**. Start with none configured, then paste SignalK
credentials, the InfluxDB token, any WeatherKit keys and your OpenRouter key
into the matching Settings section in the running app, where they are encrypted
at rest with AES-256-GCM.

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

**[The dashboard](docs/features/dashboard.md).** Twenty-one built-in tiles,
plus gauge tiles that bind to any path your server publishes and embed tiles
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

**[Heavy-weather warning rules](docs/features/alarms.md#the-law-of-storms-barometer-rules),
already built.** A fresh install comes with a barometer ladder wired to the
three-, twelve- and twenty-four-hour tendency: 6 mb in three hours warns, 10 mb
alarms, a 24 mb fall in a day is a weather bomb. Alongside it, four rules bound
to the official marine warnings in force for your zone, polled every ten
minutes, including one that fires when the warning feed itself goes quiet so a
dead provider does not look like a calm sea. Each rule's thresholds, sources and
default on/off state are on the Alarms page, along with what a rate of fall
cannot tell you while you are under way.

**[Mate](docs/features/assistant.md), a chat assistant that knows the boat.**
Ask "should we take Tongue Bay or Blue Pearl Bay over the next two days" and it
resolves both places, pulls wind, wave and tide for each from the providers you
have configured, and answers in your units and time zone. It works out wind and
sea angle against your planned course arithmetically rather than leaving it to
the model, and with InfluxDB configured it estimates passage time and fuel burn
from your own logged speed and fuel-rate history. It reads the shipped help
pages when you ask how Helmcentral itself works. Open it from the sidebar, or from the
header's sparkle button as a sheet over whatever you are looking at. Voice is
push-to-talk on `Alt+M`, with an experimental "Hey Mate" mode that is off by
default and documented with its costs. You bring your own OpenRouter account and
choose the model; each reply's footer shows what that reply cost. It is
read-only: it can look things up and explain, and it cannot start, change or
steer anything.

**[Wall displays](docs/features/dashboard.md#wall-displays).** A display is a
named screen with its own size, magnification and orientation. Assign a page to
one in layout mode, set its dwell (5 to 3600 seconds) and a condition (always,
or a vessel state such as anchored), and `/display/<name>` cycles through that
screen's pages fullscreen with no chrome. A page belongs to one display, since
the layout is the page and a 1920x360 strip is not a 55 inch television;
**Duplicate to** copies one onto another screen to diverge from there. Wall
pages leave the ordinary page list for their own sidebar group, so the list you
scan at the helm stays short. A display always renders dark, drops a
conditional page out of the rotation mid-lap when its condition stops being
true, and shows a compact status pill for a lost connection or a live alarm
instead of the full banners. It can shift its own pixels on a slow cycle to
spare an OLED panel, and a remote or keyboard can step and pause the rotation.
Four of the built-in tiles (clock, current conditions, forecast, sea state)
were sized for the narrowest strip's fold. Setup and probing:
[Set up a wall display](docs/how-to/set-up-a-wall-display.md).

**[Nearby maps](docs/features/dashboard.md#nearby-map).** A tile showing the
points of interest around the vessel across eleven categories: anchorages, bays,
islands, marinas, fuel, boat ramps, moorings, historic landmarks, lookouts, dive
and snorkel spots, and walking trails. Map only, or map with a ranked list of the
five nearest by distance and bearing, badged to match the markers. Each instance
keeps its own range and categories, so one page can watch fuel and ramps while
another watches dive sites. The default provider is OpenStreetMap over Overpass
and needs no key. Setup: [Add a Nearby map](docs/how-to/add-a-nearby-map.md).

**[Links to anywhere in the app](docs/features/dashboard.md#linking-to-a-page).**
Every panel, dashboard page and settings section has its own URL, so you can
send someone `http://boat:8080/forecast` instead of directions. Back and Forward
move between panels, an unsaved Settings page still asks before you leave it,
and a tapped alarm notification opens the Alarms panel rather than the
dashboard.

Other features include route planning that pushes an active route to SignalK for your
autopilot, autopilot control on SignalK's v2 API, ARPA radar targets, and an
embedded weather radar.

Out of scope: hazard avoidance, weather routing, live
navigation, or anything requiring a chart licence.

## Provider plugins

Tides, weather, waves, forecast warnings and points of interest come from
sandboxed WASM plugins loaded from disk. Install a provider's `.wasm` file in
its plugin directory, then restart Helmcentral to make it available in Settings.
There is no need to rebuild Helmcentral.

| Category | Bundled reference plugins |
| --- | --- |
| Tides | `bom` (Australia), `noaa` (US) |
| Weather | `open-meteo` (worldwide, keyless, default), `weatherkit` (Apple, needs keys) |
| Waves | `open-meteo-marine` (default) |
| Forecast warnings | `bom` (Australia, default), `nws` (US) |
| Points of interest | `osm-overpass` (worldwide, keyless, default), `google-places` (needs a key, partial category coverage) |

Plugins run under [Extism](https://extism.org/)/[wazero](https://wazero.io/)
with no filesystem access, no process access, and network default-deny: a plugin
reaches only the hosts named in its `allowed_hosts.json` sidecar. The host owns
all derived data, so a plugin only ever returns raw provider numbers. Distance,
bearing and ranking for points of interest are computed host-side for the same
reason. You can write one in any language with an Extism PDK.

**Tides have no default provider.** The available providers use regional station
networks rather than a global model. Pick `ui.tide_provider` in
Settings to match your region. Until you do, `/api/tide-today` returns an error
naming the missing configuration.

Contracts, the sandbox model, and how to build a plugin:
[docs/reference/plugins.md](docs/reference/plugins.md).

## Documentation

Start with [docs/index.md](docs/index.md).

**These pages also ship inside the binary**, so the help is on the boat whether
or not the boat has a connection. The `?` button in the app's header opens the
page and heading for whatever screen you are on, as a sheet beside it; the
sidebar's **Help** item opens the contents page for browsing; every Settings
section carries its own Help link. Links between help pages stay in the
sheet with working Back; a link to something outside it, an ADR or an example
plugin's source, opens on GitHub and needs a connection. Mate answers questions
about Helmcentral from these same pages.

| | |
| --- | --- |
| [docs/features/](docs/features/) | What each part does and where it stops |
| [docs/how-to/](docs/how-to/) | Install, upgrade, wall displays, Mate, develop |
| [docs/reference/](docs/reference/) | Configuration, plugin contracts, POI categories, engine profiles |
| [docs/adr/](docs/adr/) | Architecture decisions and their rationale |

Editing a page under `docs/` is the only place to edit it. `make help-stage`
copies them into `backend/help/` for the embed, and a build without that step
reports the help as unstaged rather than serving stale pages.

## Development

Requires Go 1.22 and Node.js 20 or newer. From the repository root, in two
terminals:

```sh
make help-stage                             # stage docs/ for the embedded help
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
├── backend/          # Go REST API; embeds the built frontend and the help
├── frontend/         # React + TypeScript + Vite dashboard
├── plugins/          # WASM providers, by category
├── packaging/        # systemd unit, plugin build script
├── docs/             # Operator docs and architecture decision records
├── install.sh        # One-line installer
└── docker-compose.yml
```

- **Backend.** Go with the Echo framework, serving the API and the SPA from one
  port. Any path it does not recognise as a file serves the app shell, which is
  what makes deep links work on a fresh load. Endpoint reference:
  [backend/README.md](backend/README.md).
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
