# ADR 0082: Pinned Indicator Ribbon

## Status
Accepted

Promotes the per-page widget ADR 0052 introduced, using the placement
precedent ADR 0072 set for the hero row and depending on ADR 0060's
page-level skin container. Shipped alongside ADR 0080 and ADR 0081, from the
same evaluation of MV Dirona's N2KView screen.

## Context

ADR 0052 built the lamp strip as a per-page widget and named the reason
plainly: pinning it across every page would have been the first cross-page
setting in the dashboard, needing its own storage, its own editing surface,
and a decision about how it interacts with per-page layout. Duplicating a
strip across pages was the cheaper thing to ship at the time, at a cost that
ADR 0052 wrote down as it shipped: copies do not stay in step when one is
edited. That ADR named pinning as the obvious follow-up once that cost
started to bite.

The N2KView evaluation is what makes it bite now. Hamilton's three-level scan
(glance at the always-identical ribbon, glance at whichever cluster looks
wrong, read the number) works only because the first level never changes
shape from screen to screen. It is the single strongest decision in that
whole design, and a ribbon that has to be copied by hand onto every page, and
re-copied by hand every time it changes, is exactly the decision most likely
to drift out of that property one edited copy at a time, with nothing to
notice the drift.

## Decision

### 1. One vessel-level config, not a copy per page

`dashboardPagesFile` gains an optional `ribbon` field, a
`dashboardLampStripConfig` exactly like a page's `lamps` widget, nullable when
nothing is pinned. It is kept in memory as `dashboardRibbonState`, guarded by
the same mutex that guards the pages map, and it round-trips through the same
file every page already lives in rather than a second file: `GET`/`PUT
/api/dashboard-ribbon`, `tierRead` and `tierWrite` respectively, mirroring the
existing dashboard-pages routes.

Every code path that already rewrites that file from state has to carry the
ribbon forward or lose it silently on the next restart. This is the same trap
ADR 0060 section 7 documented for a page's skin field: `saveDashboardPagesLocked`
picks it up automatically since it now reads the package-level ribbon state
whenever it writes, but `reorderDashboardPagesHandler` writes the file
directly rather than going through that helper, and had to be updated by
hand. One test pins a ribbon, reorders pages through that handler, reloads
from disk, and asserts the ribbon survived; a second does the same through an
ordinary widgets-only PATCH to one page, the exact shape a layout drag sends.

### 2. Validation is shared with the widget, not forked

`validateLampStripWidget`'s rules (title length, at least one lamp or the CHK
indicator, the lamp cap, a path required and capped per lamp, a label capped)
move into a new `validateLampStripConfig(cfg, where)`, called by both the
widget's own validator and the ribbon's PUT handler. A strip that would be
rejected as a page widget is rejected as a ribbon for the identical reason,
and the reverse. Forking the two would let them drift the first time a rule
changed, the same failure ADR 0049 already found and fixed once for a gauge's
per-field rules.

### 3. The ribbon renders inside the skin, above the grid

It renders in the same slot ADR 0072 gave the hero row: inside the page's
`data-skin` container, after the layout-mode chip, before the bento grid, on
every dashboard page and at every width. Not among the app-theme banners next
to the alarm banner and the connection banner, which sit outside the skin on
purpose. An instrument-skinned page is meant to look like a dark MFD from
edge to edge; a ribbon rendered in the app theme regardless of the page's
skin would put a bright strip across the middle of that board.

It is not one of the widgets `react-grid-layout` manages. Adding it there
would mean giving a vessel-level setting page-specific `x`/`y`/`w`/`h`
coordinates it does not have and cannot sensibly have, since the same ribbon
appears on every page. It renders directly in the flex column that already
holds the layout-mode chip and the grid, a full-width block the column
stretches to fill on its own.

It does not appear on the forecast, routes, charts, radar, anchor watch,
alarms or settings panels, because none of those render the dashboard grid's
skin container at all; the alarm banner already covers every one of those
screens.

### 4. Fixed order; triage moves to the alarm banner

The ribbon's lamps stay in the order the operator saved them, always.
Nothing about an alarm changes that order at render time. This is the
decision the whole design rests on: a scan that means the same thing every
time depends on the layout never rearranging itself out from under the
person reading it.

Triage, which alarm is worst right now and how many of each severity are
active, moves to the alarm banner instead, which already sits above every
panel including the dashboard. It now ranks its shown alarms worst first by
`ALARM_STATES` order, rolls the headline up into a count per severity
present in that same worst-first order ("1 alarm, 2 warn" in the tracked
uppercase idiom), and lists the alarms' own labels after it in that order,
separated by commas and left to the existing truncate rule rather than a
separate "and N more" count now that every shown label is already listed.
The second line still reads `alarmConditionSentence` for whichever alarm is
worst, which after this change is always the actual worst one rather than
whichever happened to be first in the array the alarms feed delivered.

### 5. The walker learns a fourth thing, again

`gaugeBoundPaths()` is the single point where a bound path becomes a value
pushed over the `gauge-values` stream, and it has now had to learn about a
new source of paths four separate times: a standalone gauge, a gauge group, a
cluster, a page lamp strip, and now the ribbon. The ribbon has no widget id
and no page, so it is walked separately from the per-page loop, into the
same dedup map, so a path bound by both a page lamp and the ribbon is pushed
once. The same test shape ADR 0052 established covers it: a ribbon path is
present, and a path shared with a page lamp appears exactly once.

## Consequences

- Pinning a ribbon is now a vessel-level setting stored once, editable from
  any page's layout mode, rather than nineteen widgets in nineteen page
  widget arrays that happened to look alike.
- Copies no longer drift. There is exactly one ribbon to edit, so there is
  nothing left to fall out of step.
- The per-page lamp strip widget from ADR 0052 is unchanged and still
  exists, for a page that wants a second, page-specific strip alongside the
  pinned one. Nothing about the ribbon reads or writes a page's own `lamps`
  widgets, and the reverse.
- Every panel other than the dashboard now depends on the alarm banner alone
  for anything resembling triage, since the ribbon never appears there. This
  was already true before this ADR; it is now the documented reason rather
  than an accident of where the ribbon happened to render.
- The alarm banner's headline is denser than before on a boat with several
  active alarms of mixed severity: what used to name one alarm and "and 2
  more" now reads as a count per severity plus every label. This trades
  brevity for the ranking Hamilton's own screen depends on, the same trade
  the ribbon itself makes by never reordering.

## Verification

Backend: `go test -short ./...`, covering the round trip through PUT and GET,
null clearing the ribbon, validation (an empty strip with no lamps and no CHK
indicator, seventeen lamps), the ribbon surviving a page's widgets-only PATCH
and surviving `reorderDashboardPagesHandler` specifically, the one write path
that does not go through `saveDashboardPagesLocked`, and the walker
collecting a ribbon path deduplicated against a page lamp. 1121 to 1129 tests
passing.

Frontend: `npx vitest run`, `npx tsc --noEmit`, `npm run lint`, covering
`useDashboardRibbon` against a mocked fetch, the config dialog accepting the
ribbon's synthetic `{ id: 'ribbon' }` shape alongside an ordinary page widget
unchanged, a fresh ribbon's own default title, the Remove ribbon button, and
the alarm banner's new ranking and counts. 1794 to 1812 tests passing, no new
lint warnings.

Checked against the E2E stack (`make e2e-up`): a two-lamp ribbon PUT through
the API rendered above the grid at 1600, 768 and 390 pixels wide, staying
within its card's width at every size, and did not appear on the forecast
panel; `make e2e-reset` and `make e2e-down` afterward. Unrelated finding from
the same pass: the CZone switches tile's error text (a raw backend error
string with no `truncate`/wrap) overflows the 390px column on its own,
pre-existing and untouched by this change.
