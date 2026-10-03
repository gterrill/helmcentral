# ADR 0158: Mate Changes Records Through Changesets

## Status

Accepted

## Context

ADR 0146 let Mate change the maintenance schedule: Mate proposes, the proposal
shows as a card, and the operator's Apply makes every change in one
transaction. The model was built for that one feature. The operation struct is
the maintenance operation; apply, the freshness check, stale handling, the
history wording and the card's link are all hard-wired to maintenance.

The operator wants Mate to create, read, update and delete any Helmcentral
record: set up a deck from a drawing in the documents, file equipment into a
bin, rename locations, tag documents, adjust an alarm rule. Extending ADR 0146
one feature at a time means a new apply path, freshness check, stale
classifier and card for every record type, which is the maintenance special
case copied once per type.

Helmcentral's write surface has two different things in it. Most routes edit
**records**: rows the operator keeps, such as equipment, locations, documents
and rules. Some routes are **vessel controls**: engaging the autopilot,
starting the generator, switching a CZone circuit, raising the anchor watch.

## Decision

1. **The unit is an operation on a record, and a proposal is a changeset.** An
   operation is `{type, action, id, fields, base_version}` with `action` one of
   `create`, `update`, `delete`. A changeset is an ordered list of operations,
   at most 50, applied together in one transaction or not at all.
2. **Record types register once.** Each type supplies:
   - its field schema, with which fields are writable;
   - list and get functions;
   - create, update and delete commands that take a transaction;
   - its version stamp (`updated_at`);
   - an operator-worded description of what an operation does, including side
     effects ("removes the outlines of 3 locations");
   - the page that shows a record.

   The type's REST handlers call the same commands, so a rule is checked in
   one place whether the operator or Mate's changeset makes the change.
3. **Optimistic concurrency by default.** An update or delete carries the
   `base_version` Mate read. If the record has changed or gone by Apply, the
   changeset goes stale with a reason naming the record, and nothing is
   written. ADR 0146 wrote this check by hand for maintenance rules; here
   every type gets it.
4. **Operations can refer to records created earlier in the same changeset**,
   by a local reference (`"$1"`) in place of an id, so "create the Salon deck
   and move these three locations onto it" is one proposal.
5. **Mate's tools.**
   - `list_records(type, filter)` and `get_record(type, id)` read any
     registered type. The existing tuned read tools (`find_equipment`,
     `list_maintenance` and the rest) stay where they answer better.
   - `describe_record_type(type)` returns a type's fields when Mate needs them,
     so every schema does not ride along on every turn.
   - `propose_changes(operations)` validates and dry-runs the changeset in a
     transaction that always rolls back, then returns the proposal with each
     operation's description. Like every Mate tool it writes nothing.
6. **Apply stays the operator's.** The card and the write-tier Apply route are
   ADR 0146's, unchanged in principle. Mate still never says a change is done
   until the card shows it applied.
7. **The card renders any changeset.** Each operation shows its description and
   a field-by-field before and after; a delete shows what goes with it. Once
   applied, each record links to its page. A type may supply its own view where
   a diff is not enough to judge the change. The first is a plan image, shown
   as a thumbnail, because Mate cannot see pictures and the operator has to.
8. **What Mate can never reach.**
   - Vessel controls are not records and are never registered: autopilot,
     generator, CZone switches, anchor watch, alarm acknowledgement.
   - Secrets, API keys, sessions and login are never registered, for reading
     or writing.
   - Imports, map imagery caches and Mate's own conversations are not
     registered.
9. **Atomicity bounds which types can register.** A changeset is one SQLite
   transaction, so only types stored in `helmcentral.sqlite` register. Types
   still kept in JSON files (alarm rules, dashboard pages, routes) register
   once their store moves into the database, in their own change.
10. **Maintenance proposals become changesets.** Maintenance rules and log
    entries are registered types, and `propose_maintenance_changes` is
    replaced by `propose_changes`. Stored maintenance proposals are converted
    to the new operation shape when the table is upgraded; with one operator
    there is no compatibility path for the old shape.

## Rollout

- **Cycle 1:** registry, changeset, read tools, `propose_changes`, generic
  card. Types: maintenance rules and log entries (proving the conversion),
  equipment, locations, bins and decks (proving create, local references and
  a custom view).
- **Cycle 2:** documents (details, tags, folders, notes, manuals), equipment
  profiles, vessel particulars, wall displays, anchor placemarks.
- **Later, each after its move into the database:** alarm rules, dashboard
  pages, routes.
- Settings need a separate decision: some are operator data, others are
  configuration that can take Helmcentral offline when wrong.

## Consequences

- Adding a record type to Mate is a registration and a transaction-taking
  split of its commands, not a new tool, apply path and card.
- Each type's commands move from opening their own transaction to taking one,
  the refactor ADR 0146 made for maintenance.
- Reads through `list_records` return every registered field, so a type
  registers only fields that are safe to show in a chat transcript.
- Supersedes the maintenance-specific parts of ADR 0146: its operation shape,
  its hand-written freshness check and its single-feature card. The
  propose-then-Apply principle stands.
