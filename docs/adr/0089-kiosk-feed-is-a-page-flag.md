# ADR 0089: Kiosk Feed Is a Page Flag

## Status
Accepted

## Context

HelmCast is a separate Go and vanilla-JS app that drives the boat's 1920x360
ultra-wide wall display, rotating through full-screen pages: a clock and
weather page, a camera page, an engines page. Its data layer is a thinner,
less robust copy of Helmcentral's own: hardcoded WeatherKit credentials, a
browser WebSocket straight to SignalK with a hardcoded IP, and a GeoNames
place-name lookup carrying the 56 km cache-cell bug Helmcentral already fixed
for its own place-name feature (ADR 0056). Running the same boat's data
through two independent, differently-correct pipelines is the wrong shape:
Helmcentral's dashboard is the one place this boat's telemetry, forecast,
alarms and page model already live, and HelmCast reimplements a worse version
of the same thing on a separate box. The decision this ADR records is Phase 1
of retiring HelmCast: Helmcentral serves the wall display itself, following
the precedent ADR set when HelmLocker folded into Helmcentral rather than
staying a second app.

The operator's framing for the feature: a checkbox in dashboard page edit
mode marks a page as part of the wall display's rotation, and checking it
exposes how long that page shows before the rotation advances. Later phases
(not part of this decision) add purpose-built wall-display tiles and a
points-of-interest map; this phase only has to get an existing dashboard page
onto the wall and cycling.

Facts that shaped the design:

- Pages are already server-persisted (`backend/dashboard_pages.go`,
  `data/dashboard-pages.json`) with per-page `skin` (ADR 0060) and `hero`
  (ADR 0072) fields added by exactly the pattern a third page-level flag
  needs. Go's JSON decoder drops unknown keys silently, so any new field has
  to exist in the struct or it is discarded on save without complaint.
- Nothing in Helmcentral's frontend has a chromeless, fullscreen or embedded
  mode. The shell (`SidebarProvider`, `Sidebar`, the header) is unconditional
  in `App.tsx`, and `renderWidget` closes over roughly 58 hooks declared
  inside `App()` itself, so the wall display has to reuse `App()`'s own grid
  rather than extract the grid into a separate component tree.
- The router is hand-rolled path parsing (`frontend/src/lib/app-location.ts`,
  ADR 0074): an unrecognised first path segment falls back to the dashboard
  and the URL-sync effect rewrites the address bar to match, so a new route
  has to round-trip through that parser cleanly rather than needing a special
  case.
- The grid is 12 columns, 32px rows, 16px margins (`GRID_ROW_HEIGHT`,
  `GRID_MARGIN` in `dashboard-bento-grid.tsx`). Seven rows use 320px; eight
  rows use 368px. The 360px strip, minus the kiosk root's own 8px of padding
  on top and bottom, leaves a 344px budget, which fits seven rows with 24px
  to spare and not an eighth.
- The sidebar already lists every dashboard page as a sub-item under
  Dashboard, for navigation only. The header's page switcher
  (`dashboard-page-switcher.tsx`) owns create, rename, delete and reorder.
  Feed order is page order, and there is no reason for a wall-display rotation
  to keep an ordering independent of the one an operator already curates.

## Decision

### 1. Kiosk pages are ordinary pages plus a flag, a duration and a condition

`dashboardPageData` gains `Kiosk bool`, `KioskSeconds int` and `KioskWhen
string` (empty or `"always"` for no condition, `"anchored"` to only show
while the anchor watch is active), all `omitempty` so a file with no
kiosk-flagged pages stays byte-identical to one written before this ADR.
`validateKioskFields` holds the three rules together (an unknown condition,
an out-of-range duration, or the flag on with a zero duration are all
rejected), and both `createDashboardPageHandler` and
`patchDashboardPageHandler` run it, the same shape ADR 0060 established for
skin and ADR 0072 for hero. A PATCH validates the *merged* result rather than
only the fields present in that one request: `{"kiosk": true}` alone succeeds
when a duration is already on record, and `{"kiosk": false}` alone leaves the
stored duration and condition untouched, so unticking and re-ticking the
box remembers what was there before.

Rejected: a separate list of "kiosk pages" alongside the ordinary page list.
That would mean two things to keep in sync: an operator adding, reordering
or deleting a dashboard page would also have to remember to mirror the
change into a second list, and nothing would enforce that they matched. A
page a wall display shows is still, in every other respect, exactly the page
it was: same widgets, same skin, same editing surface. The flag is metadata
on that one record, not a fork of it.

Rejected: an iframe rotator, the way HelmCast's own front end works (load a
page, wait, swap the `src`). That throws away every hook already wired up in
`App()` (telemetry subscriptions, the anchor-watch poller, alarm state) and
reconnects them from scratch on every single page swap, dozens of times an
hour, for no benefit over just changing which page id is active in the one
app instance that is already running.

Rejected: a route per kiosk page (`/kiosk/engines`, `/kiosk/anchor`). The
rotation is a property of one browser tab looking at the feed, not a
navigable destination in its own right. Nothing should ever deep-link to
"the wall display showing page 3", because by the time that link is opened
the rotation has almost certainly moved on.

### 2. `/kiosk` drives `activePageId`, reusing the shell-less grid

`'kiosk'` joins `PANEL_IDS` in `app-location.ts`, so `/kiosk` parses, formats
and round-trips with no new branches in that module. In `App.tsx`,
`isKiosk = activePanel === 'kiosk'` gates a branch placed immediately after
the login gate and before the ordinary `<SidebarProvider>` return: when true,
`App()` renders `<KioskShell>` around `dashboardGrid` directly. Because
`renderWidget`, `effectiveWidgets` and `dashboardGrid` are all just reading
`activePage`, which reads `activePageId`, the wall display needs no
alternate rendering path for any individual widget: a `depth-tide` tile on
the wall is the exact same component, wired to the exact same 58 hooks, as
the one an operator's phone renders. `useKioskRotation` drives
`activePageId` through `onShow`, the same setter every other navigation path
already uses; the rotation is just one more caller of it. Layout mode is
never requested on `/kiosk`, so `layoutEditing` stays false there without
needing an explicit check, and the kiosk grid always renders in its
non-editing form.

### 3. Rotation is a query parameter, not stored state

`?rotate=180` and `?page=<id>` live on the URL, parsed once by
`parseKioskOptions` and never written back to it. This is a property of one
particular mounting of the app (this browser tab, on this physical screen,
oriented this way), not a property of the dashboard data itself. Storing
rotation server-side would mean every device that ever opens `/kiosk` shares
one orientation, which breaks the moment there is a second wall display or
even a desktop browser previewing the feed right-side up while the ODROID
runs rotated. `?page=<id>` pins one page and disables the rotation timer
entirely, for authoring on the helm browser and for screenshots: a way to
look at exactly one page's kiosk rendering without waiting through however
many others come before it in the feed.

The URL-sync effect and the popstate handler both return immediately when
`isKiosk` is true. The kiosk device's URL is fixed at the OS level (the WPE
snap's own `url` setting); rewriting it on every rotation tick would fight
that fixed configuration for no reader's benefit, since nothing is ever
looking at the address bar on a screen with no chrome and no back button.

A live pass on the boat's own wall display (2026-09-12) turned up a second
mounting-specific property of exactly this shape: the ODROID's WPE-webkit
browser reports a 1920x1080 viewport against a panel that is physically
1920x360, and the panel simply shows the top band of whatever gets laid out
across that oversized framebuffer. That is not a fact about the dashboard or
about any page an operator has authored; it is a fact about this one screen
and the browser sitting in front of it, exactly like `rotate`. `?height=<px>`
(`parseKioskOptions`, an integer from 200 to 4320, otherwise null for the
full viewport, same never-written-back treatment as `rotate` and `page`)
joins them as a third mounting property carried on the URL rather than
stored anywhere: `KioskShell` constrains its root to that height at the top
of the viewport instead of the full `inset-0` box when it is set, and
rotates that same constrained box in place, about its own centre, rather
than rotating the entire oversized framebuffer about a centre the panel
never shows half of.

### 4. The feed is recomputed at every advance, not memoised for a lap

`kioskFeed(pages, { anchored })` (`lib/kiosk.ts`) filters to pages with
`kiosk` on, a positive duration, and at least one widget (an empty flagged
page would show nothing for its whole slot, which is a mistake to skip, not a
blank screen to render), then drops any `kiosk_when:
"anchored"` page while the anchor is up. `useKioskRotation` calls this fresh
every time a page's timer expires, not once per lap: a page whose condition
turns false finishes the slot it is already showing (its timer still runs to
completion) and then simply is not offered on the next advance; a page whose
condition turns true is included starting from the very next advance that
looks. `nextKioskIndex` looks the *current* page up by id in the freshly
computed feed rather than carrying a raw array index across advances, so a
page inserted, removed or reordered between two ticks can never point the
rotation at the wrong page. If the currently-showing id has vanished from
the feed, the rotation restarts at index 0 rather than guessing where it
would have landed.

On the transition back to index 0 (a genuine wrap past the last page, or a
restart because the showing page vanished), the hook awaits a `refetch()`
before showing anything, mirroring HelmCast's own top-of-lap re-read: a page
someone just flagged, edited or reordered from another device should not
have to wait out an entire lap to appear correctly. The very first page
shown after mounting skips this refetch, since `useDashboardPages` already
fetched fresh data on mount. An empty feed polls `refetch()` every 15
seconds until something appears, rather than sitting on a blank screen
forever with no route back to life short of a manual reload.

### 5. The SPA shell is `no-cache`

`backend/static.go` now sets `Cache-Control: no-cache` on the app shell
(root and every deep-link fallback) and leaves hashed `/assets/*` files
alone. A kiosk left running for days reloads its own tab far more often than
a phone bookmark does (the whole point of a wall display is that it is
never explicitly closed), and a cached shell from before the most recent
deploy would keep naming asset files a later build has already deleted: a
blank screen with nothing in any browser console anyone is watching to say
why. Hashed assets already change their own filename on every build, so the
browser's default caching for them is correct and untouched.

### 6. The frameless embed option is deferred

HelmCast's camera page is a chromeless embed of helmcam's own kiosk URL.
Helmcentral's existing embed widget always wraps its iframe in a titled
`Tile`, which is fine on a dashboard and wrong on a 344px strip that has no
room to spare on a title bar. Adding a frameless variant is real work
(`dashboardEmbedConfig`, `embed-tile.tsx`, `embed-config-dialog.tsx`) that
does not block getting any page onto the wall today; the existing "Cluster
preview" page (12x7, instrument skin) already demonstrates the whole pipeline
end to end with zero frontend changes beyond the three fields this ADR adds.
The frameless embed lands in a later phase.

### 7. The sidebar keeps kiosk pages in the one page list

A flagged page stays exactly where it already was in the sidebar's page
sub-list and in the header's page switcher, gaining a small `MonitorPlay`
glyph with its duration (and an `Anchor` glyph when its condition is
"anchored") rather than moving to a separate section. One additional
top-level sidebar item, "Wall display", opens `/kiosk` in a new tab: a
plain link, not a navigation target inside the running session, because the
wall display is a different physical screen an operator is setting up, not
somewhere this browser tab is going to switch to and back from.

### 8. The compact status pills, not the full banners

`ConnectionBanner` and `AlarmBanner` are both too tall for a 344px budget:
`ConnectionBanner` alone costs roughly 40px of it. Hiding connection and
alarm state outright on a wall display was rejected outright: this is an
alarm product, and a screen that goes silent about a dropped connection or a
live alarm because there was no room for the existing banner is a screen
that has quietly stopped doing its job. `KioskStatusBadge` renders two
compact pills, bottom-right, inside the rotated root so they read upright
under `?rotate=180` along with everything else: a "No signal" or
"Reconnecting" pill in amber (matching `ConnectionBanner`'s own vocabulary)
when the telemetry stream is not connected, and an alarm-coloured "N alarms"
pill (ADR 0080's colour rules) when anything is active: loud red while
anything showing is unacknowledged, muted once everything has been but is
still live, the same distinction `AlarmBanner` already draws. Both pills are
absent entirely while the feed is connected and quiet.

### 9. The pinned ribbon does not render at `/kiosk`

A 1920x360 screenshot of the wall display showed the pinned indicator ribbon
(ADR 0082) eating about a third of the strip's height on its own, pushing a
seven-row page below the fold before a single tile had a chance to render.
The ribbon buys its keep on a phone or a tablet screen, where 320-368px of
grid still leaves plenty of vertical room underneath it; on a 344px budget it
is by far the most expensive thing on the page for the least specific
information, since it is deliberately the same vessel-wide strip everywhere.

`isKiosk` now gates the ribbon block directly in `dashboardGrid`, alongside
the ribbon's own null check, so it renders nothing at `/kiosk` regardless of
whether one is configured. A page that wants status lamps on the wall adds
its own lamp-strip widget in layout mode instead, the same widget type the
ribbon itself wraps, sized and placed like any other tile on that one page,
rather than the one vessel-wide strip every page shares. This does not
change the ribbon anywhere else: it still pins above the grid on the
ordinary dashboard, unmodified, for every page an operator navigates to by
hand.

`KioskFoldGuide` moved with it. It used to sit inside the same `relative`
container as the ribbon, so its `topPx` measured 344px down from the
ribbon's own top edge: correct while the ribbon counted against the
budget, wrong now that it doesn't reach the wall at all. The guide's
`relative` container now wraps only `DashboardBentoGrid`, so the dashed line
an operator sees while authoring a kiosk page measures the fold from the top
of the grid, matching what `/kiosk` will actually show.

### 10. The wall display always renders dark

The operator's decision for the wall display is that it always runs in the
dark theme, regardless of what any given browser profile has stored; a
freshly provisioned kiosk browser defaults to light, and a live screenshot at
1920x360 on the "Cluster preview" page showed exactly the mismatch that
produces: a light basemap inside the instrument skin's dark tokens, because
`AnchorWatchMap` picks its style off the same `isDarkTheme` value the skin
ignores. `App()` derives `isDarkTheme` as `isKiosk || storedIsDarkTheme`
immediately next to the `useDarkMode()` call, ahead of every consumer that
reads it, and a small effect there keeps `document.documentElement`'s `dark`
class in sync with that same derived value. Every tile, map and the toaster
already took `isDarkTheme` from this one variable (ADR 0060's skin work put
it there first), so the override needed no per-tile special-casing: it is
one boolean, computed once, at the same place it was always read from.
`toggleDarkMode` itself is untouched and keeps reading and writing the real
stored preference; the override never calls it and never persists anything,
so leaving `/kiosk` resumes exactly what this browser had chosen before.

### 11. Phase 0 findings

Before any of the above was worth building, a static probe page
(`frontend/public/kiosk-probe.html`) checked whether the ODROID's WPE-webkit
kiosk snap can actually run this: a WebGL2 context (and whether the renderer
string names a software rasteriser, which would fail the later map-bearing
pages), `:has()` selector support, `structuredClone`, and
`EventSource`/`ResizeObserver`/`matchMedia`/`Intl` timezone resolution. The
probe's rotated mode also embeds helmcam's own kiosk URL to judge whether an
MJPEG stream survives an interruption without a page reload. This is
recorded here rather than left to whoever next asks "does this even run on
the ODROID" to rediscover from scratch; the device-specific pass/fail result
itself belongs in the wall-display how-to, not in this decision record, since
it can change with a firmware update in a way this ADR's reasoning does not.

## Consequences

- A wall display is now three fields and a checkbox away from any existing
  dashboard page, with zero new backend endpoints and zero new widget types.
  "Cluster preview" (the existing engine cluster page) and a helmcam embed
  page both work today.
- The rotation logic lives entirely in one pure module (`lib/kiosk.ts`) and
  one hook (`use-kiosk-rotation.ts`), independent of any particular tile:
  later wall-display-specific tiles (a clock, a sea-state chart, a
  points-of-interest map) plug into the same `dashboardGrid` this ADR wires
  up, with no change to how the rotation itself works.
- A page can now be flagged for the wall display and separately reused as an
  ordinary dashboard page an operator navigates to by hand; the two uses do
  not interfere, because the flag changes nothing about how the page renders
  outside of `/kiosk`.
- The pinned ribbon is the one exception to that last point: it renders on
  every ordinary dashboard page and never renders on `/kiosk`, regardless of
  whether that page is flagged. A page authored with the ribbon showing above
  it will show one row fewer of grid once it reaches the wall; the fold guide
  reflects that by measuring from the grid, not the ribbon. A wall-specific
  status row is a lamp-strip widget placed on that one page, not the ribbon.
- The frameless embed, the points-of-interest map and the purpose-built
  wall-display tiles (clock, current conditions, forecast days, sea state)
  remain open work, tracked separately from this decision.

## Related

- ADR 0056 (place-name cache cell), the bug this phase avoids reintroducing.
- ADR 0060 (page skin) and ADR 0072 (page hero), the pattern this ADR's
  kiosk fields follow exactly.
- ADR 0074 (hand-rolled path routing), which `/kiosk` extends with no new
  parsing branches.
- ADR 0080 (alarm colour reserved for state), which
  `KioskStatusBadge`'s alarm pill follows.
- The HelmLocker fold-in, the precedent for retiring a separate app in favour
  of Helmcentral serving its function directly.
