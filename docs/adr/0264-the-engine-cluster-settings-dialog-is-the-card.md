# ADR 0264: The Engine Cluster Settings Dialog Is The Card

## Status

Accepted (2026-11-06). Replaces the four-tab settings dialog that
[ADR 0188](0188-engine-cluster-v2.md) built and [ADR 0252](0252-an-engine-cluster-chooses-its-alternator-once.md)
extended with the "Alternator & more" tab. The saved tile and every rule about
what a save keeps are unchanged.

## Context

The dialog split the card into four tabs: Dial, Readouts, Alternator & more and
Fuel. Measured in the dev stack on an 812 px viewport, the pane that held the
active tab was 288 px tall. The Engine, Title and Side row, the Fill row and the
tab bar took about 60 percent of the dialog before any slot appeared.

The tabs behind that pane were long. Dial was 1787 px of 25 fields and Readouts
was 2030 px of 34 fields, because every slot rendered the whole gauge form:
Reading, Label, Display, Quantity, Unit, Min and Max. The operator scrolled a
288 px window over two thousand pixels to change one label.

Two more faults sat in the form:

- **Display did nothing.** The tile never reads a cluster slot's `display`. It
  draws the ring as a dial, the arcs as arcs and the readouts as bullets
  whatever the field says. The select was dead on every slot.
- **The four tabs used four layouts.** Dial stacked four sections, Readouts
  stacked cards, Alternator & more used folded rows and Fuel used one box. The
  small drawing of the card at top right lit the open tab but could not be
  clicked.

## Decision

The card is the menu. The dialog drops the tabs and the drawing, and shows a
larger schematic of the real card on the left. Each slot in it is a button, and
the editor for the chosen slot fills the pane on the right.

- **One button per slot.** The ring, the centre hours, the two arcs, each
  readout card, the starter battery, the alternator with its five rows, the
  transmission pressure, the exhaust and the fuel rail. The geometry follows
  the tile: the fuel rail on the outer edge, the dial in the middle, the
  readouts on the inner edge and the footer underneath. A starboard card is
  the port one mirrored, and the buttons are in visual order, so the tab key
  walks the picture.
- **Outline carries state.** A slot that reads something has a plain outline.
  One that Save is waiting on has an amber outline and says what it lacks. One
  that shows nothing has a dashed outline. The rule is exact: an amber outline
  means Save waits on that part, and amber text on its own ("Not sending",
  "Equipment missing" on a row) is a warning that blocks nothing. The attention
  rules are the ones the tabs used (`dialAttention`, `cornersAttention`,
  `slotsNeedingReading`, `alternatorUnsent`, `fuelAttention`,
  `alternatorMissing`), now worked out per slot. The accessible name reads
  "Ring, revolutions" or "Exhaust temperature, Not shown".
- **The alternator is a slot.** Its picker is the editor of an "Alternator"
  button that sits above its five row buttons, so ADR 0252's single choice
  keeps one place to be made.
- **The editor shows the slot and nothing else.** Reading, Label and, for the
  ring, arcs, readouts and gearbox pressure, the scale stay in view. Quantity
  and Unit fold under "Units and quantity", since the profile decides them for
  any reading it covers. `GaugeFields` gains `hideDisplay`, `scaled` and
  `collapseUnits`, so the other callers keep the form they had.
- **Where it opens.** The first slot needing attention, or the ring. The pick
  is made once, so fixing a reading does not move the operator to the next
  slot.
- **Every editor stays mounted.** The ones not showing are hidden, not
  removed. The footer forms fill their expected reading from an effect that
  runs when they mount, and unmounting them would delay that fill until the
  operator opened each slot.
- **A narrow window gets a list.** Below the `sm` breakpoint the same buttons
  stack as rows and the editor opens under the chosen row. It is the same slot
  list and the same state, rendered once in the place the width asks for. Only
  a resized window reaches it: the settings gear exists in Layout mode, which
  needs the `lg` grid, so no phone opens this dialog.
- **Zone notes go to their slot.** A zone note or conflict shows in the editor
  of the slot it concerns, and an unresolved conflict gives that slot the amber
  outline. A note on another slot no longer pushes every editor down.
- **Zone choices follow the readout, not its position.** The dialog names each
  readout for as long as it is open, and `useSlotZoneEditing` keeps the
  operator's keep-the-profile's or use-the-tile's choices against that name.
  Removing a readout no longer moves a choice onto the next one, which would
  have written zones the operator never chose. Fill from profile gives the new
  readouts new names, so no old choice carries over. A readout's editor is keyed
  the same way, so its folds and fields stay with it.
- **The first pick waits for the equipment list to answer**, not for it to have
  items: an empty or failed list settles too, and the dialog opens on the first
  blocker or the ring.
- **Smaller changes.** The exhaust picker says "Pick the
  exhaust sensor…" through a new optional `placeholder` on `EquipmentPicker`
  (the exhaust is an engine-kind item, and "Pick engine…" misled). The Save
  line names the slot, never a tab. The Display select is gone from every
  cluster slot.

## What does not change

The saved tile, `pruneUnbound`, the alternator rules of ADR 0252, the zone
write on save and the Roles default all stand. This is a change of
presentation, so no operator has anything to re-enter.

## Alternatives rejected

- **Keep the tabs and trim the header.** Recovering the header's 60 percent
  still left a 288 px window onto a 2000 px form. It also kept the dead Display
  select and the four layouts.
- **Edit in a popover over the schematic.** A popover covers
  the part being edited and the parts beside it. A fixed pane keeps the picture
  in view while the fields change.
- **Unmount the editors that are not open.** It is cheaper to render, and it
  would break the footer forms' first-mount fill (see above).

## When it cannot help

- A slot the schematic does not draw, such as a fifth readout card, cannot
  exist: the tile draws four and the dialog stops at four.
- With no engine picked, every slot is still a button, but nothing fills until
  the operator picks an engine and the profile is known.
