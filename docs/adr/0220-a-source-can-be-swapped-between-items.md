# ADR 0220: A source can be swapped between two items

## Status

Accepted (2026-10-28). Amends [ADR 0187](0187-equipment-items-own-their-signalk-source.md),
which said a source owned by another item is never taken. Builds on
[ADR 0184](0184-one-transaction-convention-for-the-record-store.md) (store writes go through `RunTx`).

## Context

The operator found two fuel tanks the wrong way round: `tanks.fuel.2` was on the
starboard item and `tanks.fuel.5` on the port one. The Source list showed a
source another item owned as disabled text, and the server refused it with
"unassign it there first". To swap them the operator had to give one item a
bogus source, change the other, then come back and set the first. Three saves
and a state in between where a tank alarm followed the wrong tank.

## Decision

Choosing a source another item owns is allowed, and it is explicit.

1. **The picker lists it, enabled.** The label keeps the live reading and names
   the owner: `tanks.fuel.5 · 62% · on Fuel port`. A new, unsaved item still
   cannot take one, because it has no record to swap with.
2. **The Save bar says what saving will do.** "Swap sources with Fuel port.
   Fuel starboard gets tanks.fuel.5, Fuel port gets tanks.fuel.2." with the
   button **Swap sources**, or "Move tanks.fuel.5 from Fuel port. Fuel port will
   have no source." with **Move source**. The toast afterwards says "Sources
   swapped" or "Source moved".
3. **A swap needs matching kinds.** The owner receives the item's old source
   only when both are the same group (a tank for a tank). Otherwise it is a
   move and the owner ends with none, because a tank must not be handed an
   engine's source. The client and the server apply the same rule, and the
   server's wins.
4. **The request names the owner.** `PUT /api/inventory/equipment/:id` accepts
   `take_source_from`, the id of the item that owns the requested source. In one
   transaction the owner lets go, the item takes the source, and the owner takes
   the item's old source when the kinds match. The request also carries
   `take_source_mode`, `swap` or `move`, which is what the page told the
   operator. The server works the mode out itself and refuses with a 409 when
   it differs, so nobody gets a move where they read a swap. The owner must
   exist and still own exactly that source, or the save is a 409 asking for a
   reload. An item cannot take from itself.
5. **Figures stay with the item.** Bank and tank figures describe the physical
   battery or tank, so in a swap each item keeps its own. A move leaves the owner
   with no source, and the server refuses figures on an item with none, so a move
   clears the owner's figures. The Save bar says so, using a `figures` hint on the
   discovery row's item.
6. **What uses the owner is asked about.** The editor checks the owner's
   dependants and asks for the same confirmation as for the item's own source
   change, before anything is saved.
7. **An ordinary save still refuses.** Without `take_source_from` a taken source
   is a 409, and the message is neutral, because the same refusal reaches create, Roles and Mate, where a swap is not possible: `tanks.fuel.5 is already
   on "Fuel port". To give it to another item, pick it on that item's Source list.` The
   partial unique index stays. Mate changesets, fill-form and record-type edits
   do not send the field, so they keep refusing.

The handler invalidates the source-item cache after the save, as for any item
save, so the tank alarm rules, the vessel view and discovery ownership read both
changes.

## Consequences

- Fixing crossed sources is one save the operator can read before confirming.
- Mate cannot move a source between items. That stays a deliberate act in the UI.
- A swap touches two records. A stale page cannot silently steal a source that
  has since moved, because the owner is checked inside the transaction.

## Alternatives considered

- **A swap action on the Discovery page.** Rejected. The operator is on the item
  when they notice the mistake, and a second place to change ownership would
  have to repeat the kind rule and the confirmation.
- **Silently take the source on an ordinary save.** Rejected. A stale page or a
  Mate edit could strip an item of its source without anyone seeing it, and the
  tank alarms would follow.
- **Leave it refused and document the three-step workaround.** Rejected. The
  intermediate state is the hazard.
