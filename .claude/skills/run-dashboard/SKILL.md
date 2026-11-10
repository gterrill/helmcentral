---
name: run-dashboard
description: Launch and screenshot the Helmcentral dashboard (frontend + backend) to visually verify a UI change. Use when asked to run the app, start the dev server, take a screenshot of the dashboard, preview a worktree's UI, or confirm a frontend change looks right in the browser.
---

Helmcentral is a Vite/React frontend proxying `/api` to a Go backend
that talks to a SignalK server. Both run via Docker Compose.

## 0. Read-only look, or are you going to click things?

Pick the stack **before** you start, by what the script will do:

| What you're doing | Use | URL |
|---|---|---|
| Screenshot, visual/layout check, read-only browsing | dev stack | http://localhost:5173 |
| Anything that submits a form, clicks Save/Connect/Apply, or otherwise mutates state | **E2E stack** (`make e2e-up`) | http://localhost:5174 |
| Looking at a UI change made in a worktree | The worktree's own frontend (see "Preview from a worktree") | http://localhost:5175 |

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

## Preview from a worktree

Leave the dev stack alone and run the worktree's frontend on the next free port
against the same backend:

```bash
cd frontend && npx vite --port 5175 --strictPort
```

An isolated worktree session cannot `cd`. Give the worktree's absolute path
twice instead, with the same optional `VITE_API_PROXY_TARGET` prefix:

```bash
npm --prefix <worktree>/frontend exec -- vite <worktree>/frontend --port 5175 --strictPort
```

Keep the path after `vite`. Without it, vite serves from the worktree root: the
page never loads, and it leaves a stray `.vite` folder.

Vite proxies `/api` to the running backend on :8080, so the page shows the same
live data as :5173 and the two can be compared side by side. To see the boat's
own data instead, prefix `VITE_API_PROXY_TARGET=http://192.168.50.240:9091`.
That page writes to the boat, so use it only to look.

If the change needs the worktree's own Go code, or a click that saves something,
run its backend too. `NAME` is the suffix you gave `make worktree`:

```bash
make preview-backend NAME=<name>        # builds, copies the dev state, prints VITE_API_PROXY_TARGET=...
make preview-backend-stop NAME=<name>
```

It runs on the first free port from 8091 against a throwaway copy of the dev
state (`HELMCENTRAL_STATE_DIR`, ADR 0026), with push subscriptions and the
notification queue deleted from the copy. Nothing it saves reaches
`backend/data`. Its SignalK points at a black hole, so tiles show `—`
placeholders. `LIVE=1` keeps the boat's address for live data, and then
setting an anchor publishes to the real SignalK: use it only to look.
The start stops when the copied InfluxDB address does not answer, which the
Tailscale one in `settings.yaml` does not from the Mac: add
`INFLUX_URL=http://<reachable-host>:8086` to replace it in the copy, or
`INFLUX_URL=none` to run the preview without InfluxDB.

Ports 5173 and 5174 belong to the dev and e2e stacks; go up from 5175 if another
worktree already holds it. Stop the server and the preview backend once the
change is checked: `make prune-merged` will not remove a worktree while vite runs.

## 3. Screenshot it

Phone width is the case that matters. One command, from the repo root or a
worktree's absolute path; Playwright is a pinned devDependency of `frontend`:

```bash
node <worktree>/frontend/scripts/phone-preview.mjs http://localhost:5175/ <scratchpad>/phone.png
```

It opens a 390x844 context with touch, `isMobile` and 2x scale, waits for the
page to settle (3 s, or `--wait-for`) and prints the page's `scrollWidth`, with a warning when that
is wider than the viewport. `--width`, `--height`, `--wait-for <selector>` and
`--full-page` are optional. From a worktree, use its own port in place of 5173,
or the shot shows the main checkout's code. Then view the PNG with the Read tool.
`Executable doesn't exist at .../chromium-NNNN` means the cached browser does
not match the pinned Playwright: run `npx playwright install chromium` from
`frontend`. `ERR_MODULE_NOT_FOUND` for `playwright` means the shared
`node_modules` predates it: `make check-frontend-deps`, then `npm ci`.

Do not use Chrome's `resize_window` for this. It reports success and leaves the
viewport at desktop width.

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

Pass `--width`/`--height` from the table. The overflow tell is the script's
`scrollWidth` warning at 390.

## Driving a page

To click, type or read a page, import Playwright the same way the script does.
Run the file with `node` from inside `frontend`'s tree (for instance a scratch
`.mjs` that does `import { chromium } from '<worktree>/frontend/node_modules/playwright/index.mjs'`).
Use `devices['iPhone 14']` or the same context options as `phone-preview.mjs`.
Keep the script in the session scratchpad, not the repo.

Gotchas for driving:

- `left_click_drag` in the Chrome tools does not pass `modifiers: "shift"`: the
  pointerdown shows `shiftKey: false`. Shift-drag needs a Playwright script
  (`page.keyboard.down('Shift')`, then `page.mouse`).
- Layout edit mode does not exist below the `lg` breakpoint. `canEditLayout` in
  `App.tsx` gates `LayoutModeToggle`, so a phone-width shot never shows it.

## Gotchas

- **Live vs. placeholder data**: if the SignalK server is unreachable,
  vessel-state hooks return `null` and tiles render `—` placeholders
  instead of numbers. Components still render structurally fine for
  layout/style checks — don't mistake this for a broken page. On the
  E2E stack this is the normal state, by design.

  Its address/port come from the `signalk:` stanza of `settings.yaml`.
  Credentials, if the server requires auth, come from the encrypted
  secrets store (`backend/data/secrets.sqlite`, managed via Settings →
  SignalK or `GET`/`POST /api/settings/secrets`) — the backend reads
  `SIGNALK_USERNAME`/`SIGNALK_PASSWORD` from that store directly each
  time it authenticates, not from the process environment. There is no
  `.env` file: `backend/.env` was retired in favour of the store, and
  nothing loads one. Non-secret knobs (`PORT`, `SETTINGS_FILE`,
  `HELMCENTRAL_STATE_DIR`, …) are real environment variables — see
  `backend/README.md`.
- **`npm run dev` on the bare host** (no Docker) also works standalone
  for frontend-only checks, but then `/api` calls fail unless
  `VITE_API_PROXY_TARGET` points at a reachable backend — expect all
  tiles to show placeholders in that mode.
