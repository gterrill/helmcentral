# ADR 0139: Popovers draw above the sheet and dialog layers

## Status

Accepted. Amends [ADR 0107](0107-new-page-flow.md), which put `Popover` and
`DropdownMenu` at `z-60` so they would clear the live alarm banner at `z-55`.
That reasoning still holds against the banner, and dropdown menus stay at
`z-60`. What it got wrong is the sheet: ADR 0107 argued a popover opened
inside the Mate sheet was safe because it portals out of the sheet's stacking
context, so `z-60` is compared against the banner directly. That is true of
the banner and irrelevant to the sheet's own panel, which is `z-70`. A
popover portalled to the body at `z-60` loses to it.

## Context

An operator editing a note could not attach a link or a photo. The note
editor's Link and Image toolbar buttons opened their popovers behind the
documents viewer sheet, leaving a sliver visible past the sheet's left edge
and every click landing on the sheet instead.

The note editor (ADR 0117) renders inside that sheet, which is where the two
layers finally met: before it, no popover in the app had a trigger inside a
sheet or a dialog, so `z-60` had never been tested against `z-70`. The same
defect was sitting in the Select component and is covered below.

## Decision

`components/ui/popover.tsx` moves its `Positioner` and `Popup` from `z-60` to
`z-85`, above the sheet panel (`z-70`) and dialog content (`z-80`) and below
tooltips (`z-90`).

The ladder this leaves:

| Layer | z |
| --- | --- |
| Sheet backdrop | 50 |
| Live alarm banner | 55 |
| Header, page selector, dropdown menus | 60 |
| Sheet panel, dialog and alert-dialog backdrop | 70 |
| Dialog and alert-dialog content | 80 |
| Popover, select popup | 85 |
| Tooltip | 90 |

The rule underneath it: a surface a trigger can live *inside* has to draw
above the surface that holds the trigger. Tooltips are already at the top for
exactly this reason, and a popover anchored to a button inside a sheet is the
same argument one rung down.

## Rejected

**Raising the sheet and dialog instead.** Pushing the popover down the ladder
by moving sheets below `z-60` puts the sheet panel under the alarm banner and
the header, which is worse: a sheet is a full working surface and the banner
is a strip.

**A per-call-site `className` override on the popovers inside the sheet.**
This is the fix that looks smallest and ages worst. The next popover that
ends up inside a sheet has the same bug, and nothing about the call site says
it needs a z-index.

**Fixing Select in the same change.** `components/ui/select.tsx` portals at
`z-50`, which loses to the sheet, both dialog layers and the alarm banner.
The Item picker in the maintenance rule dialog (ADR 0138) is a live instance:
a Select inside a `DialogContent`. It is the same defect and it needs the
same treatment, but it is a different component with a different call site to
verify, so it is its own change rather than a rider on this one.

*Amended 2026-09-28:* that change followed immediately. `select.tsx` moves to
`z-85` alongside the popover, with its own stacking test, so the Item picker
opens over the dialog that holds it. The table above stands with Select read
as `z-85`, and the rule underneath it now has two components obeying it.

## Consequences

- A dialog or alert dialog opened from *inside* an open popover would now
  render under that popover. No call site does this today: the one that used
  to, the page switcher's Delete page confirmation, moved its trigger out of
  the popover in ADR 0107 and is a sibling of the toolbar row. If a case ever
  appears, the alert dialog moves above `z-85`, not the popover back down.
- Dropdown menus stay at `z-60` and keep ADR 0107's behaviour intact. No
  `DropdownMenu` in the app currently renders inside a sheet or a dialog. The
  day one does, it has this same bug.
- `src/test/popover-stacking.test.tsx` pins the ordering the way
  `tooltip-stacking.test.tsx` already pins tooltips: the popup and its
  positioner both above sheet and dialog, below tooltip. The ladder is now
  asserted in two places rather than described in comments alone.
  `src/test/select-stacking.test.tsx` (the amendment above) pins the same
  ordering for Select. Both read the sheet, dialog and tooltip figures off
  the real rendered components through `src/test/helpers/z-ladder.tsx` rather
  than a copied literal, so a future change to any one surface's z-index
  fails the other two instead of leaving them green against a stale number.

## Related

- [ADR 0107](0107-new-page-flow.md): set the `z-60` popover/menu layer and the
  banner reasoning this amends.
- [ADR 0117](0117-note-editor.md): the note editor whose Link and Image
  buttons exposed the defect.
- [ADR 0138](0138-maintenance-rules-list-and-service-log.md): the maintenance
  rule dialog holding the Select instance of the same bug.
