# ADR 0190: First-run setup status

## Status

Accepted (2026-10-17). Amended by [ADR 0196](0196-equipment-sources-must-carry-their-signature.md): the Engines step flags an engine on a source that never reports RPM. Builds on
[ADR 0187](0187-equipment-items-own-their-signalk-source.md), whose items and
roles the steps read.

## Context

A fresh install shows a dashboard of dashes. The chain that fills it is
SignalK connected, then Discovery (sources become equipment items), then Roles,
then tiles with real data, and nothing told the operator that. Worse, the Roles
dropdowns offered only "Not set" until items existed, so a tile saying "choose a
house bank under Roles" sent the operator to a list with nothing in it. On the
first boat to use it, 6 battery, 3 alternator, 3 charger and 2 inverter sources
were reporting and no item owned any of them.

Readiness was also scattered. The Roles page computed the anomaly checks'
statuses from the vessel view, each tile decided for itself whether to say "not
set up", and nothing tracked the parts a boat can lack. A boat with no
generator could never finish.

## Decision

1. **One status function.** `GET /api/setup/status` returns ordered steps
   (`id`, `title`, `state` of `done`, `todo` or `blocked`, `detail`, and a
   `target` of Settings section and control id) and `complete`. It is built by
   one function from the resolved vessel, the SignalK connection and the
   discovery rows. Four steps, in order: Connect to SignalK, Engines, House
   bank, Charging and power sources. Steps are judged from saved
   configuration whatever the feed is doing, so a finished boat stays complete
   when SignalK drops. The connection step is a todo only while SignalK is not
   connected and nothing is set up yet (no item owns a source, no role is
   answered). A step that is not done and needs what the boat reports to be
   answered is `blocked` while the feed is down, and says so. The Roles page's per-check
   statuses call the same helpers for the two questions they share (is an engine
   assigned, is a house bank chosen), so the two cannot disagree.
2. **Nothing is stored.** Status is worked out from live state on every read.
   There is no dismiss flag: a renumbered instance, a deleted item or a cleared
   role makes its step a todo again, and the dashboard line returns.
3. **"This boat has none" is a role value.** The optional roles (port and
   starboard alternator, shore charger, inverter, generator, generator AC input)
   accept the reserved value `none`, stored in the roles block where an item id
   would be. The server refuses it for the house bank and the voltage readout
   battery. Every reader resolves it exactly like a blank role (no figures, no
   log line, no error); only the setup status tells the two apart, through the
   vessel view's record of which roles were answered none. An item id is a UUID
   and an instance id is never called none, so the value cannot collide.
4. **Choosing a reporting source creates the item.** Each Roles dropdown lists
   the items already assigned to that kind, then a "From the boat" group of
   reporting sources no item owns, each with a short live reading. Choosing one
   creates an item named after the role (House bank, Port alternator) with that
   source, through the same create call Discovery uses, and sets the role to it.
   If the create fails the error is shown and the role does not change. With
   nothing to choose, the list says the boat isn't reporting any of that kind, or
   that SignalK isn't connected.
5. **Where it shows.** Settings, SignalK opens with a Setup section: done steps
   as one muted line, the next to-do with its detail and a button that scrolls
   to and focuses its control, the rest one muted line. The dashboard header
   area carries one line, "Setup: 2 of 4 done · Next: House bank", while setup is
   incomplete; it opens Settings, SignalK at that control. Neither appears on a
   wall display, and the dashboard line is for sessions that can write.

## Alternatives rejected

- **A dismissible checklist.** A dismissed list hides a step that later becomes
  undone, which is the case that matters on a boat where an instrument is
  renumbered. Computing from live state needs no memory to go stale.
- **A wizard.** Setup is not linear for long: an operator adds a generator a
  season later. A modal flow would have to be re-entered; a line that appears
  only when something is undone does not.
- **A separate readiness computation for the checklist.** The Roles checks and
  the checklist would drift, and the operator would see one say ready while the
  other said not.
- **Leaving the house bank's capacity, cell count and profile in the step.** They
  live on the item's page, not on Roles, so a step landing on Roles could not
  finish it. The Roles page still lists them under the full-bank check.

## Consequences

- The Battery & Power and Generator tiles still carry their own "not set up"
  wording, fed by the electrical state; their links now land on the exact
  dropdown. They do not read the status steps.
- Choosing a source in Roles creates its item at once, like Discovery's Create
  item; discarding the unsaved roles leaves that item in place.
- A role holding `none` shows as "This boat has none" in Roles and as unset
  everywhere else. Deleting an item leaves other roles' `none` answers alone.
- The status counts saved state: a role chosen in the Roles form counts once the
  Save bar has saved it.
- A house bank role whose item has lost its source is distinct from no bank
  chosen: the electrical state carries `house_bank_unresolved`, the tile and the
  House bank step tell the operator to choose the item again.
- Setting the generator to none clears the generator AC input in the form, and
  the vessel view ignores a stored AC input while the generator is none, so no
  generator power is read from a boat that has no generator.
