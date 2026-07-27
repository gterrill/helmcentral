---
name: run-dashboard
description: Launch and screenshot the Helmcentral dashboard (frontend + backend) to visually verify a UI change. Use when asked to run the app, start the dev server, take a screenshot of the dashboard, or confirm a frontend change looks right in the browser.
---

Helmcentral is a Vite/React frontend proxying `/api` to a Go backend
that talks to a SignalK server. Both run via Docker Compose.

## 0. Read-only look, or are you going to click things?

Pick the stack **before** you start, by what the script will do:

| What you're doing | Use | URL |
|---|---|---|
| Screenshot, visual/layout check, read-only browsing | dev stack | http://localhost:5173 |
| Anything that submits a form, clicks Save/Connect/Apply, or otherwise mutates state | **E2E stack** (`make e2e-up`) | http://localhost:5174 |

The dev stack bind-mounts the developer's **live** `settings.yaml` and
writes runtime state into `./backend/data`. It is pointed at a real
boat. A Playwright script that fills `#signalk-address` with a dummy
value to test a dirty-state indicator, then clicks "Save and
Continue", **will persist that value** and take the real dashboard
offline — this has already happened once (see
`docs/adr/0026-e2e-stack-isolation.md`). `settings.yaml` is gitignored,
so there is no history to restore from.

If you are unsure whether your script mutates anything, use the E2E
stack. It costs one `make e2e-up`.

```bash
make e2e-up      # dashboard on :5174, API on :8090
make e2e-reset   # discard everything the run did, re-seed
make e2e-down    # stop it
```

It serves the same UI from the same source (hot-reload included), but
against `e2e/settings.seed.yaml` copied into a container-local state
volume — `HELMCENTRAL_STATE_DIR` redirects *every* backend write, so
nothing it does can reach your working tree. Tear it down and recreate
it freely; unlike the dev stack, nothing there is shared.

Note the seed points SignalK at a black hole, so tiles render `—`
placeholders by design. That makes it right for layout, form and flow
checks, and wrong for anything needing live vessel data — for that,
read-only against :5173.

## 1. Check whether the dev stack is already running

The dev stack is normally left running for days at a time — **do not
blindly start or stop it**.

```bash
docker compose -f docker-compose.dev.yml ps
```

If `backend-dev` and `frontend-dev` both show `Up`, skip straight to
step 3 — the app is already live at http://localhost:5173.

## 2. Start it, only if step 1 shows nothing running

```bash
make dev     # docker compose --profile dev up -d backend-dev frontend-dev
make logs    # tail both services, Ctrl-C to stop tailing (containers keep running)
```

`frontend-dev` is `npm run dev -- --host 0.0.0.0 --port 5173` inside a
`node:20-alpine` container with the repo bind-mounted, so edits under
`frontend/src` hot-reload with no rebuild needed. `backend-dev` runs
under `air` (Go hot-reload) similarly for `backend/`.

Do **not** run a second `npm run dev` on the host if the container is
already bound to :5173 — it'll just fail with `EADDRINUSE` or grab
:5174 and give you a redundant, non-hot-reloading instance. If you
must (e.g. compose is unavailable), kill it yourself when done; never
`make down` the container stack unless the user asks — it's shared,
long-running dev infrastructure.

## 3. Screenshot it

No `chromium-cli` in this environment. Use Playwright's screenshot CLI
via `npx` (auto-downloads Chromium on first run, ~1min):

```bash
npx --yes playwright screenshot \
  --viewport-size=1600,1000 --wait-for-timeout=3000 \
  http://localhost:5173/ /tmp/shot.png
```

Then view it with the Read tool. `--wait-for-timeout` is a blunt
instrument (there's no `wait-for` command in this CLI, unlike
`chromium-cli`) — 3000ms has been enough for the dashboard's client
fetches to settle in practice; bump it if a screenshot looks
mid-fetch.

### Viewport matrix

For any layout change, shoot all four. The dashboard renders three
structurally different layouts across this range (ADR 0032), so one
screenshot proves very little:

| Viewport | What it covers |
|---|---|
| `390,844` | iPhone 14 — the primary phone target; single-column CSS grid |
| `430,932` | Pro Max — the wide end of the phone band |
| `768,1024` | iPad portrait — **the tightest case**: two columns *and* a fixed 256px sidebar, so each tile gets ~240px |
| `1600,1000` | Helm baseline — the react-grid-layout grid; regression guard |

768 is the one that catches overflow. Below it the sidebar is an
overlay sheet and takes no width; at `lg` the RGL grid takes over. It
is the only band where two columns and a fixed sidebar compete, and
it's where large fixed type (`text-3xl` values, `md:` step-ups written
back when `md` meant "more room") spills its card.

Two CLI gotchas:

- There is **no `--device` flag** on `playwright screenshot`, and it
  emulates neither touch nor DPR. `--viewport-size` alone is fine for
  layout and overflow checks, which is all this is for.
- The CLI can't run `document.documentElement.scrollWidth`, so the
  tell for horizontal overflow is a `--full-page` shot at `390` that
  comes back **wider than 390px**.

Note `sips -c` crops from the **centre**, not the top-left, so it's
awkward for grabbing a header. Easier to re-shoot without
`--full-page` to get just the viewport.

There is no `--clip` flag for cropping to one element. Crop the full
screenshot afterward instead:

```bash
sips -c <height> <width> --cropOffset <y> <x> /tmp/shot.png --out /tmp/shot-crop.png
```

## Gotchas

- **Live vs. placeholder data**: if the SignalK server is unreachable,
  vessel-state hooks return `null` and tiles render `—` placeholders
  instead of numbers. Components still render structurally fine for
  layout/style checks — don't mistake this for a broken page. On the
  E2E stack this is the normal state, by design.

  Its address/port come from the `signalk:` stanza of `settings.yaml`.
  Credentials, if the server requires auth, come from the encrypted
  secrets store (`backend/data/secrets.sqlite`, managed via Settings →
  SignalK or `GET`/`POST /api/settings/secrets`) — the backend pushes
  `SIGNALK_USERNAME`/`SIGNALK_PASSWORD` into its own environment at
  startup. There is no `.env` file: `backend/.env` was retired in
  favour of the store, and nothing loads one. Non-secret knobs
  (`PORT`, `SETTINGS_FILE`, `HELMCENTRAL_STATE_DIR`, …) are real
  environment variables — see `backend/README.md`.
- **`npm run dev` on the bare host** (no Docker) also works standalone
  for frontend-only checks, but then `/api` calls fail unless
  `VITE_API_PROXY_TARGET` points at a reachable backend — expect all
  tiles to show placeholders in that mode.
