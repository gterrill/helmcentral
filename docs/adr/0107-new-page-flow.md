# ADR 0107: New Page Flow

## Status

Accepted.

## Context

A user test of creating a dashboard page from scratch turned up eight
problems in one sitting: the new page is named `Page N` and never asks for a
real name; an empty page shows nothing, so there's no clue what to do next;
the layout controls sat below the grid, behind a "Layout Mode, Drag to
rearrange" pill that told you nothing useful; the Add Widget list was 21
items in one flat, alphabetical column and got hidden under the alarm
banner; every built-in widget landed at the same 4x6 footprint regardless of
what it actually shows, which cut Battery & Power's Shore line off before
anyone touched a resize handle; and the manual never explained how to make a
page at all.

Two decisions were made before any of this was built. First, each widget
gets a fixed default size rather than measuring the tile after it lands.
Resizing after the fact is what the bug report was actually about, and a
size chosen up front from the tile's own content is strictly better than one
chosen empty-handed by the operator. Second, a page is created straight away
and named in place, not behind a "name it first" dialog. Dialog-then-page is
two decisions in a row for what is, in practice, one action.

## Decision

### The toolbar moves to the top, in one fixed order

`components/layout-toolbar.tsx` replaces both the old pill above the grid
and the separate control row that used to sit below it. One row: page name
field, **Add Widget**, **Ribbon**, **Skin**, **Hero**, **Kiosk**. Kiosk stays
last deliberately. It's the one control that grows extra fields (a seconds
input and a condition select) the moment it's ticked, so it never pushes
anything after it around. The chip styling carried over unchanged from the
old control row; only its position and the pill it replaced changed.

### A page is named in place, and that's now the only way to rename one

`onCreate` in App.tsx creates the page as `"Untitled page"` and records its
id in `namingPageId`. `components/page-title-field.tsx` is the toolbar's
first item: for the page in `namingPageId` it starts empty with the
placeholder "Page name" and takes focus; for every other page it shows the
current name. Enter or blur saves a non-empty, changed name through
`updatePage`; an empty name is left alone, so the backend's 400 on a blank
name (`backend/dashboard_pages.go`) is never reached from the client at all.
Escape reverts. A read-only session never sees this as a field at all:
`LayoutToolbar` shows the page's name as plain text when `canWrite` is
false, the same rule Delete page already enforced — otherwise this was the
one write surface in the toolbar a read-only user could still type into,
for the backend to reject.

`onSave`'s promise matters here too. `updatePage`
(`hooks/use-dashboard-pages.ts`) resolves the saved page on success or
`null` on a rejected PATCH (which also raises the toast), and App.tsx's
`onSaveName` passes that outcome through as a plain boolean.
`PageTitleField` only records a name into `lastSavedRef`, the guard that
stops a stray second commit from
re-sending what was just saved, once `onSave` reports success — recording
it before the response came back meant a name the backend rejected could
never be retried unchanged, since the retry matched `lastSavedRef` and was
swallowed before it ever reached `onSave` again. A separate `pendingSaveRef`
does the job `lastSavedRef` used to: set synchronously the moment a save
starts, so a real second blur landing right behind an Enter is still caught
before either promise has settled, success or not.

`namingPageId` is also cleared from App.tsx itself, not only from
`PageTitleField`'s own `onDone`. Switching the active page away from the
one being named, or leaving layout mode altogether (dropping below `lg`
included), clears it too. Without that, creating a page and switching away
before touching its name field left `namingPageId` pointed at a page no
longer on screen — reopening layout mode on it later landed on a blank,
focus-stealing name field for a page that already had a name.

The page switcher's pencil-and-inline-input rename is gone.
`components/dashboard-page-switcher.tsx` lost its `editing` state, its
`onRename` prop, and the button entirely. There is now exactly one rename
surface, and it only exists in layout mode, the same as every other layout
edit (drag, resize, Add Widget). Below the `lg` breakpoint there is no
toolbar and so no way to rename a page there either, consistent with the
rest of layout mode being desktop-only rather than a new restriction
invented for naming specifically. The switcher's own **New Page** button is
gated the same way, by the same `useMinWidth(BREAKPOINTS.lg)` check
App.tsx's `canEditLayout` already uses: a page created below `lg` would
land named `"Untitled page"` with no toolbar reachable afterward to rename
or delete it. Selecting a page and reordering the list need no toolbar, so
neither is gated — both stay available at every width.

### Deleting a page moves to the toolbar too, for the same reason

A user test of this branch found that with the page switcher popover open,
clicking a page's trash icon opened the delete confirmation behind the
popover. Two separate problems produced that one symptom.

First, delete had stayed behind in the switcher's per-row trash icon while
rename had already moved out. That left two edit surfaces open behind the
same trigger at once, the popover and the confirmation it could spawn,
instead of the one surface every other layout edit already used. The fix
follows the reasoning that moved rename: `components/dashboard-page-switcher.tsx`
loses its `pendingDelete` state, its `AlertDialog`, its per-row trash button
and the `onDelete` prop entirely. It is now select, reorder and New Page,
nothing else. `components/layout-toolbar.tsx` gains a "Delete page" control
as the last item, after Kiosk and set apart from the rest of the row by a
divider, since it is the one destructive control there. It acts on the page
the toolbar is already showing, hidden rather than disabled with only one
page left, the same rule the switcher enforced (`canWrite && pageCount >
1`). The confirmation dialog moves with it unchanged: same title, same body,
same Cancel/Delete buttons, calling the same `deletePage` handler in
App.tsx that already resolves a new active page when the one it deleted was
active.

Second, and independently of where the trigger lived,
`components/ui/alert-dialog.tsx` was still `z-50` for both its backdrop and
its content, while `components/ui/dialog.tsx` already used `z-70` and `z-80`
(the header's own layer, one above the popovers/menus/dropdown-menu at
`z-60` this same branch introduced above). An `AlertDialog` opened from
inside, or above, anything at `z-60` rendered underneath it regardless of
which component owned the trigger. Moving the trigger out of the popover
fixed this one case, but any future alert dialog opened from a popover or
menu would have hit the same layering bug. `alert-dialog.tsx` now matches
`dialog.tsx`: `z-70` backdrop, `z-80` content.

### An empty page prompts instead of sitting blank

`components/empty-page-prompt.tsx` renders in place of `DashboardBentoGrid`
when `effectiveWidgets.length === 0` and the page has no hero (a page whose
only content is its hero widget still shows the grid: the hero row is
content in its own right), and pages have actually finished loading.
`pagesLoading` gates the whole thing in App.tsx: before the initial GET
`/api/dashboard-pages` resolves there is no active page yet either, which
otherwise satisfied the exact same "nothing here" condition and flashed the
prompt on every load, not just on a genuinely empty page. Three variants
once loading is done: in layout mode it names Add Widget and links the new
how-to; outside layout mode at `lg` and above it says to press Edit; below
`lg`, where layout mode isn't reachable at all, it says widgets need a
screen at least 1024px wide. It never renders at `/kiosk`. There is nothing
to click on an unattended wall display, and telling an operator who isn't
there to press Edit would be pointless.

### Every built-in widget gets a category and a fixed default size

`lib/dashboard-widgets.ts` gains `WIDGET_CATEGORIES` (an ordered list:
Navigation, Weather, Situational, Power, Engine, Systems, At a glance,
Custom), `DASHBOARD_WIDGET_CATEGORY` and `DASHBOARD_WIDGET_DEFAULT_SIZE`,
both `Record<BuiltinWidgetId, …>` so a widget id added to
`DASHBOARD_WIDGET_IDS` without a matching entry in either is a compile
error, the same protection `DASHBOARD_WIDGET_LABELS` already had.

Fifteen of the twenty-one default sizes are not invented for this change.
They're read straight off `defaultDashboardLayout` in
`backend/dashboard_pages.go`, the hand-tuned arrangement that already ships
as a genuinely fresh install's first page. That layout's anchor-watch entry
even carries its own comment explaining why H:10, not 8, is what the
always-on map plus the rode readout and Drop/Raise button need. Reusing it
here means the picker and the fresh-install page agree about what a widget
needs, instead of two numbers that can drift apart the first time only one
of them gets tuned. `clock`, `current-conditions`, `forecast-days` and
`sea-state` instead default to their own `WIDGET_CONSTRAINTS` minimum
(`components/dashboard-bento-grid.tsx`): that minimum was already the
wall-display's actual content need at the kiosk's 7-row fold, not a
density-scale floor, so there was no taller "ordinary desktop" number to
prefer over it. `radar-targets` and `autopilot` have no backend precedent,
since both shipped after that layout was written, so their sizes are
estimated from their own tile content (radar-targets mirrors nearby-vessels'
row-list shape; autopilot from its heading/target readout, mode strip, three
button grids, and the hold-to-confirm engage bar) and flagged for a browser
check rather than claimed as measured.

`handleAddWidget` (App.tsx) looks the size up in
`DASHBOARD_WIDGET_DEFAULT_SIZE` instead of placing every widget at a
hard-coded `{w: 4, h: 6}`. Battery & Power's own default is `{w: 4, h: 12}`,
double the old hard-coded height and the specific bug this change fixes.

### The grouped picker

`components/add-widget-picker.tsx` is built on the shadcn/Base UI dropdown
menu (`components/ui/dropdown-menu.tsx`, added via `npx shadcn add
dropdown-menu` against this project's `base-nova` style) rather than the
plain `Popover` the old flat list used, for its built-in
`DropdownMenuGroup`/`DropdownMenuLabel` grouping and keyboard navigation.
Every built-in widget shows, always. A widget already on the page renders
`DropdownMenuItem disabled` with a quiet "On page" hint, rather than
disappearing from the list the way the old picker's `unplacedWidgetIds`
filter made it. Disappearing looked like the widget didn't exist; disabled
with a reason says why nothing happens when you'd otherwise click it.
Multi-instance entries (Gauge, Gauge Group, Engine Cluster, an equipment
profile, Indicators, Embed, Nearby map) are always enabled, since more than
one of each can exist per page. They reach the picker as a plain
`{label, category, onSelect}` list rather than as entries in
`DASHBOARD_WIDGET_IDS`, since App.tsx opens a config dialog or draft for
each instead of placing a widget straight away. `unplacedWidgetIds` no
longer exists in App.tsx at all.

Engine and Custom are categories with no built-in widget in them at all.
They exist only to give Engine Cluster/equipment-profile and
Gauge/Gauge-Group/Indicators/Embed somewhere to live in the grouping. The
picker skips rendering a category heading with nothing under it, so an empty
category is never visible.

### Popovers and menus draw above the alarm banner

The live alarm banner sits at `z-55` (no ADR of its own, "impeccable
critique 2026-09-12, P1"), deliberately above the Mate sheet's backdrop
(`z-50`) and deliberately below the header, which sits at `z-60`. Every
`Popover` and the new `DropdownMenu` were at `z-50`, which put every one of
them, including the page switcher's own popover, underneath a live banner.
Both `components/ui/popover.tsx` and `components/ui/dropdown-menu.tsx` move
to `z-60`, the same layer the header itself already occupies, so a long Add
Widget list, or any other popover, now draws above the banner instead of a
live alarm silently blocking half of it. A popover opened from inside the
Mate sheet is not an exception: `PopoverContent` renders through its own
portal rather than the sheet's stacking context, so its `z-60` is compared
against the banner directly and clears it regardless of the sheet. The
sheet's own panel (`components/ui/sheet.tsx`) is `z-70` — only its backdrop
is `z-50` — above the banner and ordinary popovers/menus alike, and the
confirmation and settings dialogs (`components/ui/dialog.tsx`,
`alert-dialog.tsx`) sit at `z-70`/`z-80`, that same layer again or higher.
Tooltips were left at `z-50` here and moved to `z-90` on 2026-09-19, once a
collapsed sidebar's nav tooltip was seen drawing under the header: a tooltip
can be anchored to a trigger inside a popover, a sheet or a dialog, so it has
to clear all of them. `z-90` is the ceiling of this table, not `z-80`.

`DropdownMenuContent` ships `max-h-(--available-height) overflow-y-auto` by
default from the shadcn registry, so the grouped Add Widget list scrolls
within the viewport instead of flipping upward over the header the way an
unconstrained popup would once it's taller than the space below its trigger.

## Consequences

- `DashboardPageSwitcherProps` dropped `onRename`, and later `onDelete` too;
  any other caller constructing that component directly (there was exactly
  one, `app-header-responsive.test.tsx`) had to drop the prop too.
- `LayoutToolbarProps` gained `onDeletePage`, `pageCount` and `canWrite`, the
  last two existing only to reproduce the switcher's old "hidden with one
  page, write access required" rule for the Delete page control. `canWrite`
  also decides whether `PageTitleField` renders at all, same as Delete
  page. `onSaveName` (and `page-title-field.tsx`'s own `onSave`) resolves to
  a boolean now, not `void`, so a rejected PATCH can be told apart from a
  successful one.
- `DashboardPageSwitcher` calls `useMinWidth(BREAKPOINTS.lg)` itself for
  the New Page gate rather than taking it as a prop from App.tsx — it's a
  pure function of viewport width, the same reason App.tsx computes
  `canEditLayout` the same way instead of storing it.
- The old inline Add Widget popover's seven multi-instance buttons
  (`Gauge…` through `Nearby map…`) still call the exact same App.tsx
  handlers they always did. Only their presentation moved, into
  `AddWidgetMultiInstanceEntry` values passed to the picker.
- Existing App-level tests that queried the old flat Add Widget list by
  `role="button"` (`app-gauge-group-duplicate.test.tsx`) needed updating to
  `role="menuitem"`, since Base UI's `Menu.Item` renders a
  `div[role=menuitem]`, not a `button`.
- Default sizes are fixed, not measured live: an operator's own tile
  content (a longer vessel name, more tanks, a wider indicator strip) can
  still need a manual resize afterward the same as it always could. This
  trades a small amount of per-boat precision for a size that is right far
  more often than the old blanket 4x6 was.

## Related

- ADR 0060 (page skin), ADR 0072 (hero), ADR 0082 (indicator ribbon), ADR
  0089 (kiosk/wall display): the three page-level controls and the kiosk
  fields the toolbar now carries in a fixed order. None of their own
  behaviour changed here, only where their controls live.
- ADR 0031 (embeds), ADR 0049 (gauge groups), ADR 0052 (indicator strips),
  ADR 0054 (engine clusters), ADR 0091 (Nearby map): the multi-instance
  widgets the Add Widget picker now offers alongside the built-ins, unchanged
  in how they're created.
