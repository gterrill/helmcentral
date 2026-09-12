# Development

Requires **Go 1.22** and **Node.js 20 or newer**.

The Go toolchain is deliberately pinned at 1.22, and several dependencies are
held back to match, so do not let `go get -u` bump the `go` directive. CI and
release builds run Node 24; the dev containers run Node 20.

## Running it

In two terminals, from the repository root:

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

## Tests

```sh
cd backend && go test -short ./...   # -short skips live BOM FTP round-trips
cd frontend && npm test && npm run lint
```

## Docker workflows

```sh
make dev     # backend-dev (air hot-reload) + frontend-dev (Vite), :8080 / :5173
make logs    # tail both
make down    # stop

make e2e-up  # isolated stack on :5174 for anything that mutates state
```

**Use the E2E stack for any script that clicks Save.** The dev stack
bind-mounts your live `settings.yaml` and can take a real dashboard offline.

A production-like build is:

```sh
docker compose -f docker-compose.dev.yml --profile prod up --build -d backend frontend
```

## Release builds

```sh
goreleaser build --snapshot --clean   # cross-compiles every published target
```

Build hooks build the frontend and stage it into `backend/dist` for the
`//go:embed`, so a snapshot binary includes the frontend just as a release does;
run `make manual-stage` first if you want a local `go build`/`go run` to embed
the operator manual, which feeds both Mate's `read_manual` tool and the
in-app Manual sheet behind the header's `?` button.

Tagging `vX.Y.Z` and pushing runs
[.github/workflows/release.yml](../../.github/workflows/release.yml), which
publishes the archives, the WASM plugin bundle and the multi-arch image.

Use SemVer tags only.
