---
name: run-dashboard
description: Launch and screenshot the Helmcentral dashboard (frontend + backend) to visually verify a UI change. Use when asked to run the app, start the dev server, take a screenshot of the dashboard, or confirm a frontend change looks right in the browser.
---

Helmcentral is a Vite/React frontend proxying `/api` to a Go backend
that talks to a SignalK server. Both run via Docker Compose.

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

There is no `--clip` flag for cropping to one element. Crop the full
screenshot afterward instead:

```bash
sips -c <height> <width> --cropOffset <y> <x> /tmp/shot.png --out /tmp/shot-crop.png
```

## Gotchas

- **Live vs. placeholder data**: if the SignalK source configured in
  `settings.yaml` / `backend/.env` is unreachable, vessel-state hooks
  return `null` and tiles render `—` placeholders instead of numbers.
  Components still render structurally fine for layout/style checks —
  don't mistake this for a broken page.
- **`npm run dev` on the bare host** (no Docker) also works standalone
  for frontend-only checks, but then `/api` calls fail unless
  `VITE_API_PROXY_TARGET` points at a reachable backend — expect all
  tiles to show placeholders in that mode.
