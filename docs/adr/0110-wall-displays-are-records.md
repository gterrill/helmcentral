# ADR 0110: Wall Displays Are Records

## Status

Accepted. Section 8's management surface (the dialog, and the sidebar group
that nested each display's pages) is superseded by ADR 0112, which moves both
onto routed pages at /wall-displays. The rest of section 8 stands: wall pages
still leave the Dashboard page list, and the reorder and switcher-label
consequences it records are unchanged.

## Context

ADR 0089 put the boat's 1920x360 flybridge strip on Helmcentral's own page
model: a `kiosk` flag, a duration and a condition on an ordinary dashboard
page, with the screen's own properties (rotation, and later the viewport
height the ODROID's browser misreports) carried on the URL as query
parameters. That design had exactly one screen in mind, and said so: section
3 justified keeping rotation off the server precisely because it is a
property of one mounting rather than of the dashboard, "which breaks the
moment there is a second wall display".

That moment has arrived. A 55" LG C5 OLED is going into the saloon. It shares
nothing with the strip except the app: a different aspect ratio, a different
pixel count, a different viewing distance, a different panel technology with
a burn-in problem the strip does not have, and a remote control where the
strip has no input device at all. Under the current design there is no way to
express it. `kiosk: true` is a single global set, so every flagged page would
appear on both screens, and a board authored for a 1920x360 band is not a
board anyone wants filling a 55" television.

The same design causes a second, unrelated irritation the operator raised
alongside it. ADR 0089 section 7 deliberately kept flagged pages in the one
sidebar page list, distinguished only by a glyph, on the reasoning that a
wall page is still an ordinary page. That reasoning holds for what a page
*is* and fails for what a list is *for*: the sidebar's page list is the
navigation surface someone reaches for at the helm, and four pages that only
ever appear on a wall sit between the operator and Anchor or Engines every
time. One wall display made that a minor cost. Two makes it the majority of
the list.

Facts that shaped the design:

- The pages file (`data/dashboard-pages.json`) is a single JSON document
  holding a `pages` array and, since ADR 0082, a sibling `ribbon` key, both
  guarded by one mutex and written by one atomic writer. Three call sites
  write that file, and two of them build the file struct by hand and carry
  the ribbon forward because someone remembered to; the reorder handler
  carries a comment saying exactly how fragile that is.
- `validateKioskFields` validates the merged result of a PATCH rather than
  only the fields present in it, so unticking and re-ticking the kiosk box
  remembers the duration that was there before. That carry-forward is worth
  preserving through any rename.
- The hero (ADR 0072) renders its widget in a full-width row above the grid,
  enlarged by `HERO_SCALE`, and freezes that widget's grid slot. On a page
  whose entire point is fitting a measured vertical budget, it consumes
  budget the fold guide is measuring and shows one tile twice.
- `react-grid-layout`'s `WidthProvider` measures its container's layout
  width. A CSS `transform` leaves layout width alone; CSS `zoom` does not.
- The grid is 12 columns, 32px rows, and 16px margins narrowed to 8px on the
  strip (`WALL_ROW_MARGIN`) because a 352px budget cannot spare the gutters.
  Columns are proportional to container width, so the column count means the
  same thing on any width; only the vertical budget differs between screens.
- The reorder endpoint rejects any list that is not every current page id
  exactly once, which is a correctness guard worth keeping and an obstacle
  to any UI that shows a subset of the pages.
- What an LG Magic Remote emits in the webOS browser is not knowable from a
  development machine, and the webOS browser's ability to run this app's
  bundle at all is unproven. A probe page
  (`frontend/public/display-probe.html`, ADR 0089 section 11) already exists
  for answering exactly that class of question on the device itself.

## Decision

### 1. A display is a record; a page belongs to one

A new `displayData` record carries the name an operator gives a screen, the
slug that addresses it, the logical canvas a page is authored against
(`width` and `height` in CSS pixels, both zero meaning "whatever this browser
reports"), an on-screen magnification (`scale`), a rotation, and two
behaviour flags covered in sections 5 and 6. `dashboardPageData`'s `Kiosk
bool` becomes `DisplayID string`, naming the one display a page appears on,
or empty for an ordinary dashboard page. `KioskSeconds` and `KioskWhen`
become `DwellSeconds` and `ShowWhen`, keeping their meaning and their
merged-PATCH carry-forward exactly as ADR 0089 section 1 established them.

One display per page, rather than a page appearing on several. The layout a
page carries is a set of twelve-column coordinates authored against one
vertical budget, and a board that fills a 1920x360 band is not a board that
fills a 55" television; a page shared between the two would be wrong on at
least one of them by construction. "Duplicate this page onto another
display" (section 8) is the honest operation, because it produces a second
layout that can then diverge, which is what actually happens.

Rejected: a separate displays file. Deleting a display has to clear the
reference on every page that named it, and that has to be one write or a
crash between two writes leaves pages pointing at a screen that no longer
exists. The ribbon already established that a second concept can live as a
sibling key under the same lock.

Rejected: a display owning an ordered list of its page ids. That is ADR 0089
section 1's own argument, unchanged and still correct: it would be two things
to keep in sync, with nothing enforcing that they matched. Feed order remains
page order, and a page's membership remains one field on the page.

### 2. Geometry moves from the URL onto the record

ADR 0089 section 3 kept `rotate` and `height` on the URL on the reasoning
that they describe one mounting rather than the dashboard. They do. What that
reasoning missed is that a mounting is itself a durable thing worth naming
once: the flybridge strip is upside down every day, not upside down in the
opinion of whoever last typed a URL. Carrying it on the URL meant the facts
about a screen lived only in a snap configuration file on the ODROID, where
nothing else could read them, which is why nothing else could: the fold guide
an operator authors against had a hardcoded 360 in it because there was
nowhere else for the number to come from.

Rotation, canvas and scale are now fields on the display. The route becomes
`/display/<slug>`, and `PANEL_IDS` renames `'kiosk'` to `'display'`. The only
query parameter that survives is `?page=<id>`, which pins one page and
disables the rotation timer, because that genuinely is a property of one
browser tab doing one temporary thing.

The fold guide and the row-margin choice now derive from the display a page
belongs to, so authoring a television page shows a television-height fold
line and television gutters, and the helm browser and the wall can no longer
disagree about where the bottom of the screen is.

Rejected: back-compatibility for `/kiosk?rotate=180&height=360`. There is one
operator and one ODROID; re-pointing a snap's URL is a smaller cost than a
parallel code path that exists forever to serve a configuration nobody will
have after the deploy window.

An unrecognised or missing slug renders an explicit diagnostic naming the
configured displays and their URLs. It never falls back to the first display:
that would silently put 1920x360 geometry on a television, which is precisely
the masking fallback the project's own policy forbids, and the failure would
present as "the wall looks wrong" rather than as an error anyone can act on.

Rejected: 90 and 270 degree rotation. The shell rotates its box about that
box's own centre, which is correct at 180 and wrong at 90 without also
swapping the axes and re-originating the transform. A value that half-works
is worse than one that is refused, so validation accepts 0 and 180 only and
this paragraph records why.

### 3. Scale is magnification of an authored canvas, not browser zoom

A strip read at arm's length and a television read from across a saloon need
different type sizes from the same pixel count. The display therefore carries
a logical canvas and a separate `scale`: the saloon television is authored as
1280x720 at 1.5x, not as 1920x1080 at 1x. Every tile minimum size, the fold
budget and the twelve-column geometry then continue to mean on the television
exactly what they mean on the strip, and the operator does not have to
compensate by authoring enormous tiles.

The shell renders two nested boxes to achieve it. The outer box occupies the
physical footprint and carries the rotation about its own centre, exactly as
`KioskShell` already did. The inner box is the logical canvas, with
`transform-origin: top left` and a `transform` carrying the scale and the
pixel shift. Two boxes rather than one composed transform because rotation
about a centre and scaling from a corner do not compose into a single
sensible origin.

Rejected: CSS `zoom`. It changes layout width, which is the thing
`WidthProvider` measures to decide how wide a grid column is; the grid would
lay out against the scaled width and then be scaled again. `transform` leaves
layout untouched, which is what keeps every existing grid constraint valid.

Validation rejects a scale other than 1 on a display with a zero canvas:
magnifying a viewport that was never measured produces a board whose extent
nobody can predict, and the operator would discover it by looking at the
screen rather than by being told.

### 4. Assigning a page to a display clears its hero

A page on a wall display has no hero widget. The hero's row is additional
height above the grid and its promoted tile also keeps its grid slot, so on a
page authored to a measured budget it spends that budget showing one tile
twice. Assigning a display therefore clears `Hero` and says so in the
response, and the display picker states it before the operator commits.

The reverse direction is refused rather than absorbed: setting a hero on a
page that already has a display is rejected, because the operator has to take
the page off the wall first and a silent no-op would leave the control
showing a hero that never renders. Releasing a page from a display does not
restore a hero, because the clear discarded it and there is nothing to
restore it from.

### 5. A display can shift its own pixels

An OLED showing a substantially static board for a season will retain it. The
always-dark wall rendering (ADR 0089 section 10) is already doing most of the
work, and the page rotation itself moves a good deal of what is on screen,
but the chrome that survives every page (tile borders, labels, the status
pills) sits in the same pixels all afternoon. A `pixel_shift` flag moves the
whole inner box through four positions eight pixels apart on a sixteen-minute
loop.

The period is deliberately far longer than any dwell. The shift must never
read as movement to someone watching the board; it only has to ensure the
image was not in one place all day. The fold budget subtracts the shift
amplitude when the flag is on, so the board has room to move into rather than
clipping its own bottom edge, which keeps the authoring guide honest about
what will actually be visible.

The field is named for what it does and labelled "OLED panel" in the
interface, which is why an operator would want it. LG's own screen shift and
pixel refresher still apply and are better at this than a web page can be;
they belong in the how-to as television settings, not here.

### 6. Remote keys drive the rotation, and are not focus navigation

A television has a remote and the strip has nothing. Arrow keys and space
currently do nothing on a wall, so the handlers are unconditional rather than
a per-display flag: left and right step the rotation and restart the dwell,
enter or space pauses and resumes it, and the webOS back key resumes. A brief
overlay names the page and its position in the feed. `useKioskRotation`
becomes `useDisplayRotation` and grows the surface this needs, which it has
never had: stepping in both directions, pausing without the top-of-lap
refetch firing behind the pause, and resuming on a full dwell rather than the
remainder of an interrupted one.

These are global key handlers, deliberately not focus navigation. Focus
navigation would mean a tab order, visible focus rings sized for a television,
and interactive tiles, on a screen whose entire design is that nothing on it
is interactive. The rotation is the only thing there is to control, and four
keys control it.

Rejected: the Gamepad API. A Magic Remote is not a gamepad, a real gamepad
would need pairing, and the keyboard path already covers a remote, a USB
keyboard and an air mouse with one implementation.

The cursor now hides after a few seconds of stillness rather than
unconditionally. A Magic Remote shows a pointer when it is waved, and a
screen that swallows it reads as frozen; the ODROID has no pointer to show,
so idle-hiding is correct on both and unconditional hiding is not.

### 7. Wake lock is opt-in and says when it is not working

A `wake_lock` flag requests `navigator.wakeLock` on mount and re-requests it
when the tab becomes visible again, since the lock is dropped when a tab
hides. Any outcome other than held is surfaced in the status badge rather
than swallowed: a best-effort feature that fails quietly is one the operator
discovers from a dark screen, and the project's fallback policy asks for the
opposite.

Its limits belong in the how-to rather than in the code. A wake lock cannot
defeat a television's own four-hour auto-power-off or its energy-saving
dimming; those are menu settings on the set.

### 8. Wall pages leave the page list for their own sidebar group

This reverses ADR 0089 section 7. The Dashboard sub-list and the header page
switcher now show only pages with no display. A new sidebar group lists each
display with its pages nested beneath it and a link that opens that display's
URL in a new tab. A wall page is still an ordinary dashboard page and is
still edited at `/dashboard/<id>` by clicking it there, so nothing about
authoring moves; only the list an operator scans at the helm gets shorter.

Two consequences of showing a subset had to be handled rather than
discovered. The reorder endpoint requires every page id exactly once, so the
switcher's reordering is merged back into the full order before it is sent,
which also means wall pages keep their absolute positions and feed order
survives an unrelated dashboard reorder. And the switcher's own label reads
from the page list it renders, so it takes the active page's name separately
or it would read "Dashboard" while an operator is authoring a wall page.

Displays are managed in a dialog opened from that sidebar group, following
ADR 0074's rule that dialogs carry no URL. (Superseded by ADR 0112: the
dialog could not hold the per-display page ordering this ADR deferred, and
that ordering is the thing the surface exists to do.)

Rejected: a `/settings/displays` section. Settings is gated on admin and page
editing is gated on write, which would leave an operator able to author wall
pages but not to create the screen to put them on.

Duplicating a page onto another display is a client-side create through the
existing endpoint, which already accepts every field. A dedicated duplicate
endpoint was rejected on the grounds that it would be a second creation path
to keep in step with validation forever.

### 9. Deleting a display releases its pages

Deleting a display clears the reference on every page that named it, leaves
their dwell and condition intact, and returns the released ids. The pages
reappear in the Dashboard list, which is visible and reversible.

Rejected: refusing to delete a display while pages reference it. It makes
removing a screen a chore of unassigning pages one at a time, for no safety
gain, since the pages survive either way.

Rejected: cascading the delete to the pages. A page is a board of tiles with
no undo. The screen it happened to be shown on going away is not a reason to
destroy it.

### 10. The vocabulary is "display" throughout

ADR 0109 established that an operator-facing object gets one word. The
operator's word for this is "wall display"; the code said "kiosk" in nine
places, including two JSON keys. Because the load-time conversion rewrites
the data file anyway, the rename costs nothing at this moment and would cost
a second migration at any later one. `lib/kiosk.ts` becomes `lib/displays.ts`,
`KioskShell` becomes `DisplayShell`, `kioskFeed` becomes `displayFeed`, and
`kiosk_seconds` and `kiosk_when` become `dwell_seconds` and `show_when`.

### 11. The conversion is one-shot and repairs on every load

On the first load of a file written before this ADR, the dwell and condition
keys are copied to their new names for every page that has them, whether or
not it was flagged, so that the carry-forward a past unticking left behind is
not lost. If any page carried `kiosk: true`, a display named Flybridge is
synthesized at 1920x360, rotated 180, and those pages are assigned to it with
their heroes cleared. That geometry is hardcoded because nothing in the file
ever recorded it; it existed only in the ODROID's URL, which is the problem
this ADR is fixing.

A flagged page with no dwell is left unassigned and logged rather than given
an invented one. Validation forbade that state, so a file containing it was
edited by hand or corrupted, and guessing a duration would hide that.

Independently of the conversion, and on every load, a `display_id` naming no
display is cleared and a hero on a page with a display is cleared, following
the repair-on-load stance the retired-widget strip and the orphaned-hero rule
already take. That is what makes a hand-edited file safe.

### 12. The television's runtime is probed before it is trusted

Whether the C5's own webOS browser can run this app's bundle is unproven, and
what its remote emits as key events is unknowable from here. The probe page
gains a live keydown readout, a wake-lock attempt, a scaled swatch and a
reported-viewport line, and is run on the television before any of section 6
is written against a guess.

If it fails, an HDMI box drives the television the way the ODROID drives the
strip. Nothing in this ADR changes under that outcome: the record, the scale,
the pixel shift, the navigation split and the remote handling all still
apply, and only the webOS key codes and the wake lock become moot.

## Consequences

- A second wall display is now a record an operator creates in a dialog, and
  a page reaches it by picking it from a dropdown. Adding a third is the same
  work again.
- Every screen-specific fact about a mounting now lives in one place that the
  authoring view can read, which is what lets the fold guide measure the
  screen a page is actually for. The cost is that the facts have to be
  entered once, from the probe's viewport line, rather than typed into a URL.
- The old `/kiosk` URL stops working. Re-pointing the ODROID's snap URL to
  `/display/flybridge` is part of the same deploy, and the pages file is
  rewritten in place on first boot, so it is worth copying first.
- The sidebar's page list is now the navigation surface it was meant to be,
  and wall pages are grouped under the screen they run on, which also makes
  "what is the saloon television showing" answerable by looking.
- A page can no longer be both a wall page and a hero page. Pages that were
  both have lost their hero, silently in the conversion and loudly in the
  interface from then on.
- `useDisplayRotation` is meaningfully larger than the hook it replaces,
  because pausing and stepping are real state it never carried. It remains
  the only place rotation logic lives.
- The pixel shift, the wake lock and the remote are all inert on the
  flybridge strip, which has an LCD, a browser that never sleeps and no input
  device. They cost it nothing and are not flagged off for it.

## Related

- ADR 0089 (kiosk feed is a page flag), whose sections 1, 3 and 7 this ADR
  supersedes and whose sections 2, 4, 5, 8, 9 and 10 it leaves standing.
- ADR 0072 (page hero), which a page on a display no longer has.
- ADR 0074 (hand-rolled path routing), which `/display/<slug>` extends with
  one parsing branch, and whose no-URL-for-dialogs rule the displays dialog
  follows.
- ADR 0082 (pinned ribbon), the sibling key in the pages file that set the
  precedent for the displays key, and which still does not render on a wall.
- ADR 0092 (wall display tiles), whose tile constraints were sized against
  the strip's 352px budget, now named `NARROW_STRIP_FOLD_PX` and kept as the
  floor those constraints are still checked against.
- ADR 0107 (new page flow), whose create-and-name flow the duplicate action
  reuses.
- ADR 0109 (tiles, not widgets), the vocabulary rule section 10 applies.
