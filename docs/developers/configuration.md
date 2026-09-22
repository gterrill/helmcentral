# Configuration internals

Implementation detail behind [Configuration](../reference/configuration.md),
split out because it is only useful to someone building or debugging
Helmcentral, not running it.

## Profiling

| Variable | Default | Purpose |
| --- | --- | --- |
| `HELMCENTRAL_PPROF` | *(unset)* | Set to `1` to register `net/http/pprof`'s handlers under `/debug/pprof/`, for pulling a CPU, heap or goroutine profile from a running instance. |

Off by default: `auth.mode` is often `none` on a boat LAN, and profiling
endpoints are not something to expose to anyone who can reach the port.
Helmcentral logs a warning at startup when this is on.

## How the in-app help gets embedded

`ASSISTANT_DB_PATH` holds Mate's conversations, not the in-app help Mate
reads from when a question is about Helmcentral itself. That help is staged
into `backend/help` from `docs/features`, `docs/how-to` and `docs/reference`,
the same way the built frontend is staged into `backend/dist` for the
`//go:embed` that ships both inside the binary. `make help-stage` does this
for a local `go build`/`go run`; both a container build and a release build
stage it themselves as part of their own build steps. A binary built without
that step still runs; Mate just reports that the help isn't embedded rather
than answering from an empty one.

`docs/developers/` and `docs/adr/` are never staged: only `docs/index.md`
plus `docs/features`, `docs/how-to` and `docs/reference` feed the help tree,
which is why a link from an operator-facing page into this tree resolves to
GitHub instead of staying inside the Help sheet.

## Upgrade notes

API changes that break scripts or external displays calling Helmcentral
directly.

- **Marine warnings became forecast warnings.** `GET /api/marine-warnings` has
  been removed. Warnings are served at `GET /api/forecast-warnings`.
