# ADR 0036: Distribution and Packaging for Open Source Release

## Status
Accepted

Establishes the artifacts a `vX.Y.Z` tag produces, and the state-directory
convention every non-Docker install depends on.

## Context

Helmcentral is being published as a public repository. The onboarding target is
that the only prerequisite is an existing SignalK server and an internet
connection — no toolchain, no checkout, no config file to hand-edit.

What existed fell well short of that. The only published artifact was a GHCR
image built for `linux/amd64` alone, which fails with `exec format error` on a
Raspberry Pi — the single most common SignalK host. The documented install
required cloning the repo, editing `settings.yaml` by hand, and running a
TinyGo container to compile seven WASM plugins from source. `docker-compose.yml`
carried hardcoded `dns:` entries pointing at a private LAN resolver, which
would have broken DNS for every other user.

Three properties of the codebase made native binaries the obvious primary
channel rather than a nice-to-have:

- The backend is `CGO_ENABLED=0` throughout — SQLite is `modernc.org/sqlite`
  and the WASM runtime is wazero, both pure Go (a constraint ADR 0023 already
  defends). Cross-compiling to every SignalK-capable platform is free.
- The React frontend is already embedded via `//go:embed all:dist`
  (`backend/static.go`), so one binary serves the UI and the API.
- Build metadata is already injected by ldflags and surfaced on
  `/api/health` (`resolveBuildMetadata`).

Against that, SignalK itself is most often installed natively via npm on Pi OS.
Requiring Docker to run a companion dashboard is a real barrier for that
audience, while the homelab audience already has Compose working.

Two latent defects blocked either channel, both of which had been invisible
because the Docker build happened to paper over them.

**The API base URL pinned port 8080.** Five frontend modules each carried
`import.meta.env.VITE_API_BASE_URL ?? \`${location.protocol}//${location.hostname}:8080\``.
The root Dockerfile set `VITE_API_BASE_URL=""` to force same-origin, so the
container was fine; any binary run on a different `PORT`, or anything behind a
reverse proxy, would have had its settings, discovery, vessel-identity and
secrets calls sent to the wrong origin.

**The frontend inlined `settings.yaml` at build time.** `app-config.ts` read the
repo-root settings file through an eager `import.meta.glob('?raw')`. Grepping a
local `npm run build` bundle confirmed the consequence: the builder's private
InfluxDB URL, LAN addresses and vessel details were embedded verbatim in the
shipped JavaScript. In the Docker build the file was absent so the fallbacks
applied, which is why this had never been noticed — but a maintainer running
`goreleaser` locally would have published their own boat's configuration.

## Decision

### 1. Both channels, binaries first

One `v*.*.*` tag drives `.github/workflows/release.yml`, producing:

- Cross-compiled archives for `linux/{amd64,arm64,armv7}`,
  `darwin/{amd64,arm64}` and `windows/amd64` via GoReleaser. armv7 is included
  because a substantial share of Pis still run 32-bit Pi OS.
- A `linux/amd64,linux/arm64,linux/arm/v7` image, via QEMU + buildx.
- The WASM plugin bundle (below).

Documentation leads with the install script; Docker is presented as an equal
alternative rather than the default. GoReleaser's `before` hooks build the
frontend and stage it into `backend/dist`, so a snapshot binary is the real
artifact, not an API-only build. `gomod.dir: backend` is required because the
module is not at the repo root.

### 2. Plugins ship as one architecture-independent bundle

The `plugins-builder` Compose service needs Docker, TinyGo *and* a checkout —
none of which a binary installer has. WASM output is architecture-independent,
so CI builds the seven reference plugins once per release
(`packaging/build-plugins.sh`, still pinned to `tinygo/tinygo:0.41.1`) and
attaches `helmcentral-plugins-<version>.tar.gz` to the release. The installer
unpacks it into `<state dir>/plugins`.

They are deliberately not embedded in the binary and not baked into the image.
That preserves ADR 0017's central property — drop a plugin in without a
rebuild — and keeps each plugin's `allowed_hosts.json` sidecar travelling with
its `.wasm`, which the default-deny network allowlist depends on.

### 3. State lives at `/var/lib/helmcentral`

`cacheFilePath` (`backend/weather_tide.go`) already resolves every piece of
state through one choke point, rooting relative paths at
`HELMCENTRAL_STATE_DIR`. That variable existed only for E2E isolation
(ADR 0026); it is now the load-bearing mechanism for native installs, set by
the systemd unit alongside `SETTINGS_FILE=/var/lib/helmcentral/settings.yaml`.

`settings.yaml` goes in the writable state directory rather than `/etc`
because `POST /api/settings` rewrites it whenever the operator saves from the
Settings UI. The old Compose file mounted it `:ro`, which meant saving settings
could never have worked in Docker; that mount is gone.

No code change was needed here, but the convention is now something installs
depend on, so it is recorded rather than left as an environment variable with a
test-only rationale.

### 4. The frontend is same-origin, always

`apiBaseUrl` is now a single exported constant (`frontend/src/config/api.ts`)
defaulting to `''`. Every shipped artifact serves the SPA and the API from one
Echo instance, so relative paths are the only correct answer, and they survive
any `PORT` and any reverse proxy. `VITE_API_BASE_URL` remains as a build-time
escape hatch for a genuinely split-origin deployment. Development is unaffected:
`vite.config.ts` already proxies `/api` to `localhost:8080`.

The port-8080 fallback was removed rather than kept alongside the new default,
per the repo's policy against retaining superseded paths "just in case".

### 5. No build-time configuration bake

The `import.meta.glob` on `settings.yaml` is gone, guarded by
`src/test/app-config-no-build-time-bake.test.ts`, which asserts against the
inlining mechanisms rather than the filename.

Removing it exposed that `units` and the `anchor` block had no runtime source
at all: they are operator-configurable and stored by the backend, but nothing
read them back, so every shipped artifact used the compiled defaults and an
operator who chose imperial still saw metric. That was already true of every
Docker install — the image never copied `settings.yaml` — so it was a
pre-existing defect rather than one the glob removal introduced.

It is now fixed. `app-config.ts` holds the defaults and pure normalizers;
`useAppConfig` (`src/hooks/use-app-config.ts`) reads `GET /api/settings` at
runtime and hands `App` both blocks. The store is module-level rather than a
context provider, because it is consumed from hooks that do not render beneath
`App`, and single-flighted so a dozen consumers issue one request. Every field
is validated individually — settings.yaml is hand-editable and the endpoint
returns whatever it was given, so one bad key falls back for that field alone
instead of discarding the rest.

A successful `POST /api/settings` publishes its own response to the store, so a
save takes effect immediately rather than after a reload, without a redundant
GET for data the app was just handed.

### 6. CI gates releases

There was no test workflow — the only automation built the image. Publishing
signed archives from an untested tree is not defensible in public, so
`.github/workflows/ci.yml` runs `go vet`, `go test -short`, the frontend suite,
lint, and a full `goreleaser build --snapshot` on every push and PR.

`-short` is deliberate: `wasm_ftp_*_test.go` performs live round-trips against
`ftp.bom.gov.au`, whose files rotate. Those tests were already failing locally
against a file BOM no longer serves. They stay available for anyone touching
the FTP fetch path, but they cannot be allowed to fail a release for reasons
unrelated to the change under review.

### 7. The security posture is documented, not fixed

Helmcentral has no authentication, sets `AllowOrigins: ["*"]`, and exposes
generator start/stop and CZone switching. Publishing the repo means strangers
will follow the README onto their own boat networks.

Adding authentication was considered and deferred — it is a substantial piece
of work and gating the release on it would delay everything else. Instead the
constraint is stated prominently in the README, `docs/configuration.md`, the
installer's closing output and the GoReleaser release notes footer: trusted LAN
only, do not port-forward, use a VPN or an authenticating reverse proxy for
remote access. Authentication is the first item on the roadmap.

This is a conscious trade-off, and the honest framing matters: the software is
no less safe than it was, but its audience is about to grow, and an operator
who is told the constraint can act on it.

## Consequences

- A new user runs one `curl | sh` and reaches a working dashboard with no file
  editing; the first-run SignalK sweep (ADR 0029) covers server configuration.
- Native installs make mDNS discovery viable, which ADR 0029 measured as
  unworkable from a bridge-networked container. The sweep endpoint is the right
  seam to add it behind later.
- Upgrades are `install.sh` re-run, or `docker compose pull`. Neither touches
  `settings.yaml` or the state directory.
- `data/secrets.key` becomes a backup obligation for a much larger group of
  people; ADR 0023's warning is now repeated in the operator docs and the
  installer output.
- Adding a release platform means one line in `.goreleaser.yaml`; adding a
  reference plugin means one line in `packaging/build-plugins.sh`.
