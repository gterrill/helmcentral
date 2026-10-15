# ADR 0185: Equipment items own their SignalK source

## Status

Accepted (2026-10-16). Supersedes
[ADR 0181](0181-engines-and-house-bank-come-from-vessel-settings.md) and
[ADR 0182](0182-electrical-instances-and-solar-names-are-vessel-settings.md).
Builds on [ADR 0180](0180-alternator-instances-are-a-vessel-setting.md), whose
alternator instances are now a role. Phase 1 is built; phases 2 and 3 are
decided and not yet built.

## Context

ADRs 0180 to 0182 moved every "which instance is which" choice out of guesses
and into Settings → Vessel. It worked, and it left one boat described in four
places: the vessel block (engines, house bank, battery names, solar names,
alternators, electrical instances), `ui.tank_labels` in Settings → SignalK, the
equipment item (profile, hour meter path) and the tile's own configuration. The
same physical engine had a row name, an equipment link, an item name and a hand
typed hour meter path, and nothing kept them together. Naming a battery in one
place did not name it in another. The hour meter path was a free-text pick of
any `runtime` leaf on the bus.

## Decision

An equipment item owns its SignalK source. Everything else is derived from
items.

1. **One source per item.** An item has an optional `signalk_source`, the path
   of one instance: `propulsion.port`, `electrical.batteries.1`,
   `electrical.alternator.12`, `electrical.solar.2`, `tanks.fuel.0`,
   `electrical.generator.0`, `electrical.chargers.276`,
   `electrical.inverters.0`. Only these eight multi-instance groups have one.
   The group is the item's kind, so there is no separate kind column to
   disagree with it. A source belongs to at most one item (a partial unique
   index, with a clear error naming the owner). The server checks the shape;
   the UI offers only what the boat reports, plus the source the item already
   owns, so a source cannot be typed. The server does not check that the source
   is live, because an item whose source has gone quiet must stay editable and
   a save must not depend on the boat being up.
2. **Names come only from items.** Battery names, solar names, engine row names
   and tank labels are gone. The sensor namer, the Solar, Tanks and Battery &
   Power tiles, Mate and the anomaly alarm titles use the mapped item's name,
   and fall back to the instance id for a source with no item. A tank is listed
   only when an item owns it, as an unlabelled tank always was.
3. **An engine is an item mapped to a `propulsion.*` source.** The engine tick
   and `vessel.engines` are gone. Anchor auto-raise, the GNSS jump check, the
   frozen-sensor and engine differential checks, anomaly seeding, Mate's passage
   estimate and the residual paths all read mapped engine items. Readers still
   see a resolved `vesselView`, now computed from items and roles on every
   read, so none of them changed shape.
4. **The hour meter is derived.** An engine's is `<source>.runTime`, a
   generator's is `<source>.runtime` (the names those two publish, in seconds).
   The `hour_meter_path` field and picker are removed; the item shows the
   derived meter read-only, and "Not reporting" when the source has none. The
   column stays in the table as a dead column, because SQLite cannot drop it
   without a rebuild and the single-boat conversion reads it once.
5. **Roles** are single-choice parts, each the id of an item of the right kind:
   house bank, port alternator, starboard alternator (they must differ), voltage
   readout battery, shore charger, inverter and generator. They live in a
   `roles:` block in settings.yaml and save with the Save bar like any other
   setting. The generator is a role, not derived, because the Generator tile
   and the overnight model need exactly one and a second generator would
   otherwise change which one they read. The generator's AC input is the one
   role that is not an item: an `electrical.ac.<id>` circuit is a meter, not
   equipment, so it stays the bare instance id chosen from Discovery. A role is
   checked only when it changes, so an item that lost its source cannot stop an
   unrelated setting saving; the figure shows no data and the log says why.
6. **House bank figures move to the item.** Capacity, cell count and the warn
   and high voltage overrides describe that bank, and sit on the battery item
   (the server refuses them on any other item). The profile does not carry them,
   because it does not know the pack size.
7. **State-of-charge bands bind to the role.** An alarm rule can carry
   `path_role: house_bank_soc`; the server keeps its path equal to the house
   bank's state of charge, rewriting it when the role or the bank item's source
   changes. With no house bank the rule has no path and watches nothing, rather
   than a stale bank. Bands themselves behave as before.
8. **Settings → Vessel keeps only identity and particulars.** Settings →
   SignalK becomes: connection, credentials, **Discovery** (what the boat
   reports by kind, with live readings, the item that owns each source, and
   Assign to item or Create item, where create takes a name and a kind only),
   then **Roles**. `GET /api/vessel/candidates` became `GET /api/vessel/discovery`
   and also lists sources an item owns that are no longer reported. Apply gauge
   zones moved to the engine item's page.
9. **One-off conversion at startup.** An existing `vessel:` block becomes items
   and roles: an engine's equipment link gets `propulsion.<instance>`, the house
   bank item gets its path and figures and the house bank role, alternator and
   electrical instances become roles where an item already owns the source,
   hand-typed engine and generator hour meter paths become sources, and alarm
   rules on the old bank's state of charge start following the role. Anything
   that cannot be mapped is logged once, plainly, and its role is left unset.
   Names of sources that have no item are not carried over (an item is the only
   place a name lives) and each is logged so the operator can create the item.
   A source already owned by another item is never taken. The old keys are
   removed from settings.yaml, which ends the conversion; a block that does not
   parse stops startup.

## Later phases

2. **Tiles reference items.** A tile stores the id of the item it shows instead
   of an instance string, and reads its source from the item. Gauge zones come
   live from the item's profile, so Apply gauge zones goes away. A one-shot
   conversion maps existing tiles by instance; a tile that cannot be matched
   shows "Pick equipment".
3. **Dependants are visible.** Unassigning a source or deleting an item shows a
   "used by" list (roles, tiles, alarm rules, maintenance rules), and anything
   that depended on it shows "Not mapped" instead of going quietly blank.

## Consequences

- Breaking for the one boat: after upgrading, the conversion moves what it can,
  and the operator creates items for batteries, solar controllers and tanks
  that were only named before, and chooses the alternator, charger, inverter,
  generator and readout roles that had no item to map to.
- The equipment PUT replaces the whole record, so every client must send the
  source and bank figures back. The editor and the stocktake do; Mate's
  changesets carry them through untouched and cannot set them.
- Items are read on every tick (cached for a second, dropped on a write), which
  is one small query.
- Discovery and the source picker both depend on a live connection to know
  what exists; with none, saved items still show and say Not reporting.
