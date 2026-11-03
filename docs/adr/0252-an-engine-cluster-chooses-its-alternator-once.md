# ADR 0252: An Engine Cluster chooses its alternator once

## Status

Accepted (2026-11-03). Amends [ADR 0188](0188-engine-cluster-v2.md), which gave
every footer row its own equipment item.

## Context

The Engine Cluster footer holds eight rows. Five of them read one alternator:
current, output power, temperature, charge stage and charge limit. ADR 0188 let
each row name its own item, so the operator picked the same alternator five
times in the tile's settings. Nothing stopped two rows pointing at different
alternators, and nobody wants that card. The five pickers also made the dialog
clumsy. Changing the alternator meant five visits, and "Fill slots from profile"
had to seed the item into each row to keep them together.

The battery and exhaust rows are different. Each reads one thing from one item,
so a picker per row is the right size for them.

## Decision

**The footer holds one `alternatorId`.** The five alternator rows hold only
their readings, a suffix each, and the backend and frontend resolve them against
the footer's alternator. The battery, exhaust and transmission rows are as they
were.

**Absent and empty are different answers.** A footer with no `alternatorId` has
never had one chosen. An `alternatorId` of `""` means the operator cleared it.
The backend stores the field as a pointer so the empty string survives a save,
and the dialog keeps it even when no rows remain. Both read nothing. A later
Roles default may fill the first. It never overrides the second.

**An alternator row never falls back to the engine.** A row with no equipment of
its own used to read the tile's engine. That is right for the transmission and
wrong here. With no alternator, or one that is gone or unmapped, an alternator
row has no path, raises no alarm and streams nothing. Validation, the alarm
walk and the path walk all read the footer through `readSlots`, which pins each
alternator row to the footer's alternator and drops it when there is none.
Slot order and `clusterFooterSlotCount` are unchanged, so derived alarm ids for
zones a row still holds stay where they were.

**The alternator starts from Roles.** With no `alternatorId` and an engine
chosen, the picker takes the starboard alternator for a starboard engine and
the port alternator otherwise. A role of "none", or no role, leaves it empty.
This runs when a card opens, when an engine is picked and when the Engine side
changes. The picker follows the side only while the operator hasn't chosen it
in the dialog session. A saved alternator counts as chosen. A Starboard card
may hold the Port alternator, and the dialog doesn't warn.

**Readings follow what the alternator sends.** Picking an alternator gives each
row its expected reading when live SignalK paths show it being sent, and leaves
the row blank otherwise. A blank row is left off the card and doesn't block
Save. The charge limit has no expected reading and stays blank until chosen.
When the paths haven't loaded, or can't be read, the rows keep their readings,
and the switch to the new alternator runs once the paths have loaded. Opening
a saved card never refills its blank rows, because a blank may be deliberate.
Switching alternators keeps a row's reading if the new alternator sends it, live
or on its profile, and otherwise falls back to the expected reading or blank.
`switchAlternatorRows` is the one pure function for that rule, and the tests
call it directly.

**Typed readings are checked.** With paths ready, Save is blocked on an
alternator row whose reading was typed in this session, isn't live, isn't on the
profile and isn't the row's expected reading. A reading already on the card at
opening doesn't block, and nothing blocks while the paths are unknown. The old
"needs a reading" block no longer applies to alternator rows, which have no
equipment of their own. The battery and exhaust rows keep it.

**An alternator that sends nothing is said.** When the paths are ready and the
chosen alternator has a source but no live readings, an amber note says so under
the picker. Save stays available, because the boat may be quiet for a good
reason.

**The tab is called "Alternator & more".** The picker sits at the top, the five
reading rows follow, then the battery, transmission and exhaust rows.

## Alternatives rejected

- **Keep per-row storage and make one picker copy its choice into five rows.**
  The dialog would look right and the saved shape would still allow five
  different alternators. The copy would also run only in the dialog, so any
  other writer of the config, or a hand edit, could split the rows again. One
  field in the saved shape removes the case.
- **Convert saved cards.** The cards in use hold a per-row id, and the first row
  could seed `alternatorId`. The conversion would have to guess which of five
  ids the operator meant, and the operator would still need to check the result.
  With one operator and a handful of tiles, asking them to open each tile and
  save is cheaper and honest. The change goes under **Breaking** in the release
  notes.
- **Fall back to the engine when no alternator is set.** It is the old rule for
  rows without equipment. It would draw the engine's `current` as the
  alternator's, which is a wrong number on the card.

## What it cannot know

- Whether the alternator is quiet because it is off or because its source is
  wrong. The note says what Helmcentral sees and where to look.
- Which alternator an old card meant. The old per-row id is ignored when
  resolving and is dropped on the next save.
- The instance renumbering problem. If an alternator's SignalK instance changes
  number, its item's source still has to be corrected in Equipment. This change
  neither causes that nor fixes it, and it is out of scope here.

## Consequences

- A saved card shows no alternator until the operator opens its settings and
  saves. The alternator fills from Roles on opening, so the usual case is open,
  check, save. Until then the alternator rows read nothing, so the alarms
  raised from their zones don't run either. The release notes tell the operator
  to re-save each tile straight after upgrading. The old readings stay on the
  rows in the dialog, with their zones, and the first save keeps them if the
  new alternator sends them.
- `dashboardClusterFooter` gains a trailing `alternatorId` pointer. The slot
  fixture test skips it and checks that it stays last.
- `clusterFromProfile` no longer takes the Roles setting. The dialog applies the
  Roles default itself, and "Fill slots from profile" fills blank alternator
  rows from the picker's alternator.
- Equipment's list of tiles that use an item counts the footer's alternator, so
  deleting an alternator names the tiles that show it. The battery and exhaust
  rows still aren't counted, as before.
