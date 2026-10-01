# ADR 0142: A CRUD pattern library, piloted on Equipment

## Status

Accepted

## Context

Helmcentral has grown a handful of record-keeping surfaces that are not
telemetry tiles at all - they are ordinary create/read/update/delete screens
over the boat's own records: Equipment, Locations, Profiles, Maintenance,
Wall displays, Documents. Each one was built by copying whichever sibling
looked closest at the time. The Equipment index's own header comment says as
much: it "follows wall-displays-panel.tsx's shell... since that is the only
other plain index surface in the app." That copy is now three generations
removed from a shared source, and each generation drifted a little further -
its own toolbar spacing, its own empty-state wording, its own way of turning
a flat list into a table.

Documents is the far end of that drift: `documents-panel.tsx` is 2045 lines,
carrying its own index, its own filters, its own row action menus, its own
folder tree, and its own bulk-action bar, none of it sharing so much as a
type with Equipment's much smaller version of the same ideas. Nothing wrong
with any one of these files in isolation - `AGENTS.md`'s own "Do not
introduce new primitives" rule for dashboard tiles has kept `tile.tsx`
itself from sprouting a shared `MetricTile` nobody asked for - but a CRUD
page is a different kind of surface than a tile. A tile is one glanceable
instrument; an index or a details page is structure - a list, a filter bar,
sortable columns, a form's sections, a save/delete affordance - and that
structure is exactly what four different implementations of "an index with
a search box and a table" ought to share, and currently don't.

The result an operator can actually see: the Equipment index's search box,
category select and "New item" button sit in a slightly different toolbar
shape than Documents', which sits in a slightly different shape again from
Wall displays'. None of it is broken. All of it reads as unrelated software
built by three different teams.

## Decision

### 1. Three layers, not two

`frontend/src/components/ui` (shadcn primitives, `base-nova` style, Base UI
not Radix) stays the bottom layer and does not grow CRUD-specific concepts -
`ui/table` still means "an HTML table," nothing more.

A new middle layer, `frontend/src/components/patterns`, holds the
CRUD-specific composables built only from `ui` primitives: `Page`,
`IndexTable`, `IndexFilters`, `DetailsLayout`, `FormSection`, `SaveBar`,
`EmptyState`, `ConfirmDelete`, `ResourceList`, `ResourceItem`. These are Shopify Polaris' own vocabulary for
its "resource index" and "resource details" templates, adopted as names
because they already say what each piece is for. None of them fetch data or
know about `EquipmentItem` or any other domain type - `IndexTable<T>` is
generic over the row type, `IndexFilters` takes plain `{value, onChange}`
filter descriptors, `SaveBar` takes a `dirty` boolean. A future third layer
("templates" - a fully wired Equipment-shaped index or details page, ready
to point at a different hook) is left for whenever a second or third
migration shows what actually varies between pages; built now, with one
real caller, it would just be `EquipmentIndex` wearing a second name.

`Page` carries no help action of its own - a page built from this library
gets its contextual help the same way every other panel already does, the
header's `?` (`helpTargetFor`, `lib/help-links.ts`), which follows the
active screen down to its section. Equipment's own migration is what
surfaced (and fixed) the reason a page ever grew a second Help button in
the first place: the header `?`'s own `AppLocation` didn't carry
`inventorySection`, so on Inventory it silently always opened Equipment's
help page regardless of which section was open - see this ADR's own
Consequences.

### 2. TanStack Table as the headless engine

`@tanstack/react-table` (pinned to the `8.x` line - see the Consequences
section) is `IndexTable`'s sorting and column-definition engine, the same
library shadcn's own Data Table recipe is built on. It owns column defs,
`getSortedRowModel`, and nothing else - `IndexTable` still renders through
this project's own `ui/table`, not a component TanStack ships.

### 3. Card list below the mobile breakpoint

`IndexTable` renders the same column cell renderers twice: as `<table>` rows
at `md` and above, and as a stacked list of cards below it, gated by the
existing `useIsMobile()` (`lib/breakpoints.ts`'s `matchMedia`-based hook,
already used elsewhere in this app - no new breakpoint mechanism). A value
is defined once, in one column's `cell`, and both layouts read it - a table
row and a phone card can never show two different numbers for the same
field.

### 4. Lint enforcement, not a style guide

`eslint.config.js` gets a `no-restricted-imports` rule: `@/components/ui/table`
can only be imported from `components/patterns` (`IndexTable` itself) and
from a short, commented allowlist of surfaces this cycle didn't migrate.
Removing a page from that allowlist as it migrates is how progress on this
ADR is measured - unlike a style-guide document, a forgotten import fails
the build the moment someone reaches for `ui/table` directly in a new index
page.

### 5. A dev-only gallery, not Storybook

`/patterns` (`components/patterns/gallery.tsx`) renders every pattern
against fixture data - no hook, no network call. It exists only when
`import.meta.env.DEV` and loads through the same `React.lazy()` mechanism
every other panel in `App.tsx` already uses, so it costs the production
bundle nothing (verified by `scripts/check-entry-chunk.mjs`'s own eager-chunk
scan, and by grepping the built entry chunk for the gallery's own dynamic
`import()` call rather than its code). It is not routed through
`parseAppLocation`/`formatAppLocation` (ADR 0074) at all - it reads
`window.location.pathname` directly and returns before the dashboard shell
mounts, the same "reuse nothing from the ordinary panel machinery" shape the
wall display's own early return already uses for the opposite reason. A
`/patterns` visit still passes this app's ordinary sign-in gate first; it
only skips the sidebar, header and panel routing below it.

### 6. Equipment as the pilot, URL state skipped

The Equipment index and its editor (`components/inventory/equipment-index.tsx`,
`equipment-editor.tsx`) are rebuilt on the three layers above, unchanged in
what they fetch (`useEquipment(filter)` is still server-side filtering) and
in the grouping rule (system order, not alphabetical). `IndexTable` adds
client-side column sort and a per-row action menu (Open/Delete, backed by a
new `deleteEquipment()` call against the DELETE route the editor's own
"Delete equipment" flow already used - no new backend endpoint).

Search/filter/sort state was deliberately **not** pushed into the URL.
`lib/app-location.ts` (ADR 0074) treats every field of `AppLocation` as
significant to canonical-path equality and to the dirty-navigation guards
(`inventoryEditorClosedBy` and friends) - adding a query-string shape that
changes on every keystroke would either spam `pushState` on every character
typed into the search box, or need a second, ad hoc "replace, don't push"
carve-out that the rest of that module doesn't have. That is a real feature
(a shareable filtered-list link) for a later cycle to design properly
against `app-location.ts`'s own contract, not a two-line addition to fold in
here.

### 7. The Details template: main column vs. aside, and a SaveBar that takes over the header

Equipment's editor became the pilot for `DetailsLayout`'s two-column shape,
not just `FormSection`'s card chrome. The split follows Polaris' own
"resource details" template
([shopify.dev/.../templates/details](https://shopify.dev/docs/api/app-home/latest/patterns/templates/details)):
the main column holds what the operator edits and reads at length
(Specifications, Photos, Documents, Maintenance, Notes); the aside holds
what they scan without editing on every visit (Status - deployed or stored,
verified aboard; Location - zone, bin, location detail, and the tag address
(`TagRow`) for a saved item; and Organisation - profile, aliases, install
date, hour-meter path). Nothing in the aside is fetched differently or
validated differently from the main column; the split is purely about which
fields answer "what is this, and where" at a glance versus "edit this
record." Location moved to the aside (not Equipment's first cut, which kept
it in the main column) once the tag address - previously its own row at the
very top of the page, above Specifications - needed somewhere that actually
made sense for it: it names where the record is, the same question the
zone/bin/detail fields already answer, so it sits with them rather than
getting a section of its own. `TagRow` gained a `layout` prop
(`'inline'`, the default - unchanged for `bin-page.tsx`; `'stacked'`, the
Equipment aside) for this: the aside is `DetailsLayout`'s 20rem column, too
narrow for the URL and its Copy/Write-tag buttons on one row without
truncating the URL down to nothing legible.

`DetailsLayout` gained `asidePosition="start"` for this: below `lg`, where
the grid collapses to a single column and DOM order becomes visual order,
Status is what an operator should see first - not something to scroll a
whole form past to reach. Implemented as a CSS `order` toggle (`order-first`
below `lg`, cancelled by `lg:order-none` at `lg` and up, where the aside
already has its own column regardless of DOM order), so it costs nothing at
the breakpoint where the two-column grid is doing the actual layout work.

The persisted `category` field (`mechanical` | `general`) stopped being a
choice. An earlier pass relabelled it "Serviced by" (Running hours |
Calendar) and hid the hour-meter path on Calendar, which surfaced two facts:
maintenance rules are already hours-or-months (an `interval_hours`, an
`interval_months`, or both), so category drove nothing about how a schedule
counts; and the backend accepted and used `hour_meter_path` on any item
whatever its category, so a path hidden on a Calendar item stayed bound and
kept counting. The category was a second, unchecked statement of what the hour
meter already says. It is now derived: on create and update the server sets
`category` to `mechanical` when `hour_meter_path` is non-blank after trimming
and `general` otherwise, and ignores whatever a client sends (bin quick-add and
older clients still send one, without error). The column, its CHECK
constraint, the `?category=` list filter and the JSON field are kept, so
nothing stored or wired changes shape; the frontend simply no longer sends or
shows it. The editor's Maintenance section opens with an optional Hour meter
field, always visible (a new item included), and warns when the item has a
rule with an hours interval and no meter. The Equipment index drops its
Serviced-by column and filter, and choosing a profile fills only blank
manufacturer and model. A follow-up will make schedules reference the
equipment profile rather than copy it.

`SaveBar` replaced Equipment's plain, always-rendered Save button, gated on
`dirty`, and follows Polaris' contextual save bar: while the draft is dirty
it takes over the app's top bar (the shell's `<header>`) rather than
floating at the bottom of the page. The header carries a `SaveBarSlot`
(absolutely covering it at full header height, `empty:hidden` so it covers
nothing otherwise) and `SaveBar` portals into it, showing "Unsaved changes"
on the left, Discard (outline) and Save (primary) on the right, and a save
error inline beneath the label. Why the header and not a bottom bar: it is
the Polaris convention operators of any modern admin UI already recognise;
it can never cover the sidebar or the form being edited (a fixed bottom bar
did both at various widths, and needed bottom padding on the page and a
safe-area inset to stay reachable); and the header is where page-level
actions already live, so it is always in reach without scrolling. The
header's own content is covered, not moved, while it shows. The window scrolls
in this shell, so the header also sticks to the top while (and only while) a
save bar is in it (`has-[[data-slot=save-bar]]:sticky` on the header) -
otherwise editing a field halfway down the page would scroll the bar away
with it; every other page's header behaves as before. A missing slot
is a wiring error and `SaveBar` throws rather than quietly rendering inline
or not at all - a form the operator can edit but cannot save is worse than a
loud failure. Discard's behaviour depends on
whether there's anywhere to discard back to: for a saved record it reverts
the draft (and any staged document-link changes) to the loaded baseline,
leaving the operator on the same record with nothing unsaved; for a brand
new draft (`id === null`) there is no baseline to revert to, so Discard
instead calls the same `onBack` the page's own back arrow uses - abandoning
the draft and returning to the index, rather than resetting the form in
place and leaving the operator sitting on a blank "New item" they didn't
ask to keep.

Making this work meant proving Save could actually hide until the draft is
dirty, which two existing tests stood in the way of - see the Consequences
section for what they used to prove and how they were rewritten.

### 7a. ResourceList for short media-led lists, IndexTable for records

`ResourceList` and `ResourceItem` are Polaris' pair for a list whose entries
read as one line each: media on the left, a title, a metadata line, a
trailing badge and a menu (built on `RowActions`, so the click and Enter
rules match `IndexTable`: Enter counts only when it starts on the item itself,
and the menu never also opens the item). The list carries the same loading,
error-with-Retry and empty states as `IndexTable`.

The rule for choosing: rows that are compared column by column, sorted or
filtered are an `IndexTable`; a short list inside a Details section whose
entries are things to open (documents, photos, linked notes) is a
`ResourceList`. The Equipment editor's Documents section is the first user.
It opens a document in the same right-hand viewer the Documents page uses
(`DocumentViewerSheet`, extracted from `documents-panel.tsx` so there is one
viewer, not two), downloads through the existing content route, and its
Remove only unlinks, staged into the save bar like every other edit. The
thumbnail is the existing content route in an `object-cover` image with
`loading="lazy"`; no thumbnail endpoint was added. The equipment-documents
payload gained the document's `mime` and `created_at` for the badge and the
date; a document picked but not yet saved gets its kind, MIME type and date
from the picker, which reads the document's own record at pick time (the
search list carries none of them) and refuses the pick, with the server's
message, if that read fails.

Discard on a brand new item leaves through its own unguarded exit
(`onDiscarded`, like a create or delete that already succeeded) rather than
`onBack`: the editor is still reporting dirty at that moment, so the leave
guard would ask about the changes the operator had just thrown away.

### 8. Migration order

Equipment → Locations → Profiles → Maintenance → Wall displays → Documents
last. Documents was deliberately last because `documents-panel.tsx`'s folder
tree looked as if it would need a tree variant of `IndexTable`. Equipment,
and then Documents, are done (see "Documents migration" below); the flat
migrations in between have not happened yet and Documents did not wait for
them, because its folder browsing turned out not to be a tree at all.

Locations has since been migrated: `locations-index.tsx` (IndexTable of name
and bin count, a search box, "New location" in a small name-only dialog that
opens the new location's page) and `location-editor.tsx` (a Details page at
`/inventory/locations/<id>` with the name as a SaveBar draft, bins as
inline-editable ResourceList rows saved on blur, and ConfirmDelete for the
location, which shows the server's 409 verbatim). The unsaved-changes guard
covers the rename through the same `inventoryDirty` flag the Equipment editor
uses.

## Documents migration

`documents-panel.tsx` and `document-details-page.tsx` moved onto the
pattern library. What that took, and what was decided:

**Folders are not a tree in this view.** The panel only ever shows one folder
level at a time (navigating a folder replaces the list; the path is a
breadcrumb), so folders and documents are two groups of one flat
`IndexTable` - `groupBy` with "Folders" then "Documents" - and the tree variant
this section used to predict was never needed. The tree-shaped surfaces
(`manual-folder-view`, the Unfiled notes list, the checklist runner and the
note capture sheet) are reading and writing surfaces, not indexes, and stayed
as they are.

**Bulk actions.** `IndexTable` gained Polaris' bulk selection: `selectable`,
controlled `selectedIds` / `onSelectionChange`, `isRowSelectable` (folders get
no checkbox, only documents do), `rowSelectLabel`, and a `bulkActions` slot.
While anything is selected a bar above the list reads "N selected", shows the
slot, and offers Clear selection; the header checkbox selects every
selectable row on screen and goes indeterminate for some. Counts are read
against the rows actually rendered, so an id a caller left behind after a
filter cannot inflate them. On a phone each card carries its own checkbox and
the same bar sits above the cards. Cards also now leave out a "label: value"
line whose accessor yields nothing, so a folder card does not print an empty
Size.

**Page chrome.** The page title is the current folder (the path above it as a
breadcrumb; the root has none). Upload is the primary action. New folder and
New note - which carries the note kinds as a submenu - sit in the overflow
menu, which meant `Page`'s secondary actions gained an optional `submenu` and
an `onIntent` hook (hover or focus on the trigger, used to warm the note
editor chunk exactly as the old New menu did).

**Search stays one overlay.** The ⌘K overlay is unchanged - its input, "All
folders" switch, recent searches, tag suggestions, keyboard navigation and
semantic-problem notice. `IndexFilters`' search field became its trigger, not
a second search: with `onSearchActivate` it is read-only, opens the overlay on
click or Enter, and on the first printable key opens it seeded with that
character. It deliberately does not open on focus alone: closing the overlay
returns focus to the field, which would reopen it. The overlay's result list
(snippets, a highlighted row moved by the arrow keys, page and folder path)
is a command-palette list rather than a table of records, so it did not move
to `IndexTable`.

**Filters.** The tag toggle group became an `IndexFilters` select. "Unfiled
notes" is a saved view rather than a filter, so it stays a button beside the
filters, with the Mate failure line.

**Deleting.** All three deletes (a folder, a document, the bulk selection) use
`ConfirmDelete`. The dialog stays open and cannot be dismissed while the
delete is in flight, and a refusal - the 409 "folder is not empty" - shows
inside it instead of as a page banner behind the overlay.

**Uploads.** Staged uploads are a `ResourceList` above the table, with the
same progress and failure text as before.

**The Details page** is `Page` + `DetailsLayout`. The editable fields
(title, notes, tags) are the main column; what the file is and how it was
indexed are read-only cards in the aside; Save and Discard are the header
`SaveBar`. The `onDirtyChange` / `save()` contract with the navigation guard
is unchanged. The folder is not among the facts: the record carries only a
folder id and showing its name would mean a second fetch for a line nothing
asked for.

**Left bespoke, and why.** `FolderPicker` (the Move dialog's folder browser
with create-here), `SearchResultsTable`, `manual-folder-view`,
`unfiled-notes-view`, `checklist-runner`, `note-capture-sheet` and the viewer
sheet: each is a purpose-built interaction, not a list of records to open,
filter and act on.

## Rejected

**Storybook.** A separate build, a separate dev server, and a second copy of
every design token to keep in sync with `src/index.css`'s `@theme inline`
block - for a four-page problem. `/patterns` answers "what does `IndexTable`
look like with real data" without any of that, in the same app, the same
theme, the same fonts.

**A style-guide document instead of code.** Markdown describing the intended
shape of an index page is exactly what the Equipment index's own header
comment already was - a description of a pattern living next to one copy of
it, trusted to stay accurate as the next three copies drift. A document
cannot fail a build.

**`/impeccable` as the enforcement mechanism.** Impeccable audits visual
language - spacing, colour, type scale, the things `AGENTS.md`'s own
High-Density Tailwind spec already governs. It has no opinion on whether a
page imports `ui/table` directly or composes `IndexTable`, because that is a
structural question, not a visual one. A page built entirely out of
`components/patterns` and a page that hand-rolls the identical pixels can
look indistinguishable to a visual audit and still be exactly the
duplication problem this ADR exists to close.

## Consequences

- `frontend/package.json` gains `@tanstack/react-table@^8.21.3`. Pinned to
  the `8.x` line on purpose: the package's latest published major (`9.x`) is
  a from-scratch API (`useTable`/`tableFeatures` instead of `useReactTable`/
  `getCoreRowModel`) with no relation to shadcn's own Data Table recipe this
  ADR follows - `npm install @tanstack/react-table` with no version pin
  silently grabs it.
- `eslint.config.js`'s `no-restricted-imports` override list
  (`display-editor-panel.tsx`, `documents-panel.tsx`,
  `inventory/maintenance-section.tsx`, `wall-displays-panel.tsx`) is a
  to-do list as much as a lint config - each entry should disappear as §7's
  migration order reaches it, not grow a fifth reason to stay.
- Equipment's delete confirmation moved from an always-visible "Delete
  equipment" button into `Page`'s overflow "More actions" menu (the Polaris
  details-header convention this library follows for a destructive action).
  That costs the operator one extra click to reach it.
- `frontend/src/test/equipment-index.test.tsx` and `equipment-editor.test.tsx`
  were updated for the markup this ADR changes (a table row is now clickable
  as a whole rather than only its name cell; "Delete equipment" is reached
  through "More actions" first) - no test's actual assertion about behaviour
  changed, only how the test reaches the control being asserted on.
- `SaveBar` requires a `SaveBarSlot` in the document while dirty: `App.tsx`
  mounts one inside its header, and the `/patterns` gallery mounts one in a
  stand-in header of its own. Any test that renders a dirty form outside the
  shell must do the same. `SaveBarSlot` lives in its own module rather than
  the patterns barrel so the shell importing it does not pull TanStack Table
  into the entry chunk. (An earlier cut of this cycle fixed the bar to the
  bottom of the viewport with a safe-area inset and `pb-24` on the page; the
  top-bar takeover replaced it, and none of that remains.)
- `FormRow` exists because `ui/field`'s `orientation="responsive"` never did
  what the editor wanted of it: it places a label beside its control and keys
  off an `@container/field-group` ancestor no `FieldGroup` in this app
  provides, so Category+System, Manufacturer+Model and Serial+Quantity always
  stacked. `FormRow` is a container-query grid (two `minmax(0,1fr)` columns
  from `@md`, one below) that brings its own container, so the same pair is
  side by side in a wide main column and single-column in the aside or on a
  phone.
- Equipment's Save button now genuinely does move onto `SaveBar`, reversing
  this ADR's own earlier call. Two existing tests
  (`equipment-editor.test.tsx`) used to Save a just-loaded, undirtied record
  after switching between items, purely to prove a cross-item document-link
  leak doesn't ride along with it - which only worked because Save used to
  be a plain, always-rendered button. Both were rewritten to assert the
  stronger property directly (the save bar is absent right after the
  switch, because the new record genuinely isn't dirty), then dirty one
  field deliberately and Save from there to prove that save still carries
  nothing stale. The weaker, coincidental proof is gone; the property it
  was standing in for is now checked directly.
- The Equipment migration surfaced a real bug in the header `?`
  (`App.tsx`'s `helpTargetFor({ panel: activePanel, section: settingsSection })`
  call never passed `inventorySection`), fixed as part of this change by
  passing it through. That bug is why `inventory-panel.tsx` and
  `settings-page.tsx` had each grown their own in-page "Help" button
  duplicating the header's - both are removed, along with their
  `onOpenHelp` prop and its wiring in `App.tsx`. `wall-displays-panel.tsx`,
  `documents-panel.tsx` and `empty-page-prompt.tsx` keep their own
  `onOpenHelp` - each of those points at a specific how-to, not the section's
  own page, so the header `?` cannot stand in for them.

## Settings template

`SettingsLayout` is a further template, modelled on Polaris' settings
template: a page heading over one narrow, centred column (`max-w-3xl`, the
width the settings sections already used) of `FormSection` blocks, with an
optional `tools` slot rendered as a trailing "Tools" section for secondary and
destructive actions that are not part of the draft (reset, export, clear).
There is no aside and no second column. It is built on `Page`, so its heading
matches every other CRUD page.

Every settings section under `components/settings/sections` now uses it, in
place of the ad-hoc `rounded-lg border bg-background/60 p-4` wrappers with
`FieldSet`/`FieldLegend` headings (`assistant-section.tsx` follows separately).
The Alarms notifications panel and the ignored-sensors list became
`FormSection`s rather than a `Tile` and a bordered box. The Logs section's
Clear button moved to `tools`.

The settings page's pinned "Save Settings" button and its four inline
error/success banners are replaced by `SaveBar`, the same way Equipment uses
it. The bar is dirty when the regular draft, any touched secret or the
alarm-transports draft differs from what was saved, and it shows the settings,
secrets and transports errors joined. Discard restores the regular draft to its
saved snapshot, clears every in-progress secret edit (`resetTouched` on
`useSecretsStatus`, no network write) and puts the transports draft back to its
saved copy (`reset` on `AlarmTransportsProvider`). The "Settings saved" banner
is gone: the bar disappearing is the confirmation. The Vessel section's own
Save buttons (particulars, engines and house bank) stay, because they write to
their own endpoints and are not part of the page draft.

Amended by ADR 0151: the particulars, engines and house bank now save through
this bar as well.
