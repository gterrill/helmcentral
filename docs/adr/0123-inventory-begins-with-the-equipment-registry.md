# ADR 0123: Inventory Begins With The Equipment Registry

## Status

Accepted (2026-09-23). Builds the part of
[ADR 0065](0065-inventory-records-in-the-binary.md) that was never built,
starting from the equipment end rather than the stock end, and supersedes
its §7 choice of a separate SQLite file. Relocates the equipment profiles
UI that [ADR 0102](0102-equipment-profiles-and-the-duplicate-path.md)
put in Settings; the profile format, files and API are untouched.

## Context

Helmcentral knows a great deal about equipment types and nothing about the
equipment actually aboard. ADR 0053 and ADR 0102 built equipment profiles:
drop-in JSON describing a make and model, its gauges, its alarm zones and
its service intervals. A profile is true of every Onan 13.5 kW ever built.
Nothing anywhere records that this boat has one, that it sits in the
starboard engine room, that its runtime is published under
`electrical.generator.0`, or that the five notes in the Operations manual
mentioning a generator are its documents.

That gap shows up in three places. The profile-to-instance binding exists
only inside the tile dialog's React state: `commonInstancePrefix` re-derives
it from the gauge paths every time the dialog opens, and nothing persists
it. The service block on every profile is parsed, validated, served and
consumed by nothing, because "20 hours since the last oil change on *this*
engine" needs an instance to hang the hours on. And Mate, asked how to start
the generator, gets five loose search hits and has to work out from them
which generator the boat has.

ADR 0065 designed inventory as stock: identifiers, bins, scans, expiry,
quantity items in lockers. That design stands and is worth building. But
the registry is what the rest of it hangs from, and it is what the other
parts of the binary are already waiting on, so it is built first. The model
follows Yachtwave's Equipment Registry, which the operator uses and which
this cycle is deliberately shaped to match: every system aboard, from a
bilge pump to a main engine, gets one record that accumulates its
specifications, its documents, its service schedule, its maintenance
history and its costs over time.

## Decision

### 1. The registry is a table, not a note type

An equipment item is a record with fields that are queried: filter by
system, group by category, find everything in the lazarette, find every
item whose hour meter is bound to a path. A note is prose with a title.
Modelling items as notes was considered and rejected on that: every listing
would be a scan and a parse, sorting would happen in the browser, and a
typed field would live in a sidecar table anyway.

The prose that does belong to an item already exists as notes - the
Generator quirk, the starting procedure, the specification tables - and is
attached by linking rather than by duplication. A record and a note are
different things and both are kept.

### 2. Inventory tables live in `documents.sqlite`

ADR 0065 §7 said "its own SQLite file in the state directory, following the
one-store-one-file convention". That convention comes from
[ADR 0024](0024-plugin-descriptions-and-allowlist-overrides.md) §3, and
reading its three reasons back shows what it was actually about: a store
whose data needs no encryption should not live inside the encrypted secrets
file, and a store read once at boot should not share a schema with one read
on every plugin config expansion. It is a rule about lifecycle and coupling,
not a rule that every feature gets a file.

Equipment references documents on nearly every read: the editor lists them,
the index counts them, and the enrichment cycle to come proposes them. Two
files means no foreign key, no join, a synchronous delete hook in the
document handler to keep links from dangling, and a class of orphan bug
that the database was perfectly capable of preventing. One file gives
`ON DELETE CASCADE` and a join, and costs nothing that ADR 0024's reasoning
was protecting. The tables are added to `documentStoreSchema`; the Go
methods live in `inventory_store.go` on the same store, the way
`notes_store.go` and `manuals_store.go` already split by topic rather than
by file.

This means `PRAGMA foreign_keys` must be on for the store's connection.
That is a behaviour change for the existing document tables, which have
carried `REFERENCES` clauses that SQLite has been ignoring; it is the
correct behaviour and is now enforced and tested.

### 3. A link is a link; tags say what a document is

The link table is `equipment_documents(equipment_id, document_id, source)`
and carries no role column. Yachtwave's Documents tab distinguishes
manuals, parts catalogues, schematics, warranty certificates and invoices.
Helmcentral already has a vocabulary for that: document tags, operator-set
or Mate-suggested. A role column would be a second, narrower tagging system
applying only to documents that happen to be linked to equipment, and the
two would disagree the first time an invoice was tagged one way and linked
the other.

`source` is `operator` or `suggested`, mirroring `document_tags.source`
exactly. Nothing writes `suggested` yet. It is there because the enrichment
cycle that proposes links is next, and adding the column now costs one
word.

### 4. Two categories, because hours are the difference

`category` is `mechanical` or `general`. Mechanical items are hour-metered:
engines, generators, transmissions, thrusters, stabilisers, watermakers.
They carry `hour_meter_path`, the SignalK path their runtime is published
on, and their service schedules will be driven by hours. General items -
navigation electronics, safety gear, plumbing, galley appliances, deck
hardware - are driven by the calendar and by where they physically are.

`system` is a separate axis and is about grouping for a human reading the
list: propulsion, electrical, water, fuel, bilge, anchoring, safety, hvac,
navigation, appliances, structure, other. A watermaker is mechanical and
belongs to water; the two axes answer different questions and collapsing
them would lose one.

`status` is `deployed` or `stored`. A spare impeller in the lazarette is a
real item with a real record, and marking it stored is what will keep it
out of maintenance reminders once those exist.

### 5. Zones and bins now, scanning later

ADR 0065 §4 established that location comes from bin tags rather than from
signal strength, because a keyboard-wedge reader delivers an EPC with no
RSSI and no antenna identity, so the host can never infer which bin held a
tag. That decision stands, and the schema it implies - zones are areas,
bins are numbered containers with short printable codes - is built now,
ahead of any scanning, because an equipment record needs somewhere to be.

A bin belongs to exactly one zone, so naming a bin names the zone. Supplying
a bin and a zone that disagree is a validation error rather than a silent
correction: the two values came from somewhere, and quietly preferring one
hides whichever was wrong. Zones and bins in use cannot be deleted; the
refusal names how many items are in the way.

Free-text `location_detail` sits beside them for the half of the truth a bin
code cannot carry ("outboard side, behind the raw water strainer").

### 6. Profiles move to Inventory, unchanged

Equipment profiles were reachable only through Settings, which is where
things that configure Helmcentral live. A profile is not configuration; it
is reference data about equipment, and it is now one sub-menu away from the
items that use it. `POST/PUT/DELETE /api/equipment-profiles` and the files
in `plugins/engine-profiles/` are untouched, and the section component moves
with its tests intact.

An item references a profile by id, one direction only. Profiles know
nothing about items, stay drop-in JSON, and remain shareable between boats.
Choosing a profile prefills a blank manufacturer, model and category on the
item, because retyping "Cummins" under a picker that already knows it is
how two spellings of one manufacturer get into a database.

### 7. Links are edited from the equipment side, for now

One write path, `PUT /api/inventory/equipment/:id/documents`, replacing the
set wholesale the way operator tags are replaced. The document Details page
gains nothing this cycle. When enrichment starts proposing links, the
document side gets the Keep gesture it already uses for suggested tags, and
that is the natural moment to build it - not before there is anything to
keep.

## Consequences

- Deleting a document silently removes its equipment links. That is the
  intent, and it is why the foreign key is worth having, but it means a
  link is not a reason a document survives deletion. An operator who
  deletes a manual loses the association without being warned.
- `PRAGMA foreign_keys=ON` now applies to the whole document store.
  Existing `REFERENCES` clauses that were decorative are now enforced.
  Anything that was quietly inserting a row with a dangling folder id will
  now fail loudly, which is the correct outcome and may surface an old bug.
- The registry starts empty and is filled by hand. The nine "Equipment
  list" spec notes in the Operations manual hold well over a hundred rows
  between them, and extracting them automatically would flood a register
  the operator has not reviewed. Extraction with review is the next cycle.
- Mate cannot yet see the register. Asked about the generator it still
  searches documents. The resolver and the equipment card are designed and
  deferred; `aliases` is in the schema for them, because "the genset" has
  to resolve to the item and the name field alone will not do it.
- Service intervals remain parsed, served and unconsumed. The instance
  record they were waiting for now exists, which is what makes the
  maintenance cycle buildable.

## Alternatives considered

**An `equipment` note type with a sidecar table.** Explored in some detail.
It inherits folders, tags, chunking, search, `read_document` and the
Details page for free, which is a real saving. It was rejected on two
counts: `documents.note_type` carries a CHECK constraint baked into
existing databases by `ALTER TABLE ADD COLUMN`, so adding a value needs a
guarded table rebuild; and, more importantly, sorting and filtering a
register is a table's job. The saving was in the surfaces, and the cost was
in the data model, which is the wrong way round.

**A separate `inventory.sqlite`.** See §2.

**A role column on the link table.** See §3.
