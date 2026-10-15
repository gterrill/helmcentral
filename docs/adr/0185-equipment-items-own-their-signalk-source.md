# ADR 0185: Equipment items own their SignalK source

## Status

Accepted (2026-10-16). Supersedes
[ADR 0181](0181-engines-and-house-bank-come-from-vessel-settings.md) and
[ADR 0182](0182-electrical-instances-and-solar-names-are-vessel-settings.md).
Builds on [ADR 0180](0180-alternator-instances-are-a-vessel-setting.md), whose
alternator instances are now a role. Phases 1 and 2 are built; phase 3 is
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

## Phase 2: tiles reference items (built)

Tiles that show a piece of equipment store the item, not paths.

1. **The record.** An engine cluster has `equipmentId`; a gauge group has
   `equipmentKind` (the source group) and `equipmentId`; a lamp for
   multi-instance equipment has `equipmentId` and a `suffix`; a cluster fuel bar
   has a `tankId`. Each slot stores a `suffix` (`oilPressure`, `fuel.rate`)
   plus the operator's display choices (label, display, units, scale, ring
   options), and never a path or zones. The title is an optional override and
   defaults to the item's name. A gauge group with no `equipmentKind` stays a
   plain list of paths, and a lamp with a `path` stays a direct lamp, for
   single-instance signals such as a bilge float. Single-instance gauges
   (depth, wind) are unchanged. The server refuses a slot that holds a path or
   zones on an equipment tile, and a bar that still holds level and capacity
   paths. A tile with no item picked yet may hold empty suffixes, so it can be
   moved and resized while it waits.
2. **Resolution.** A path is the item's source plus the suffix, worked out on
   every read: in the browser for drawing (`lib/equipment-tiles.ts`) and in the
   backend for the paths it streams and for alarms (`dashboard_equipment.go`).
   The two walk the same rules. A fuel bar's level and capacity are the tank
   item's source plus `currentLevel` and `capacity`, read as a ratio and a
   volume, which removes the old rule that refused a tank saved without units.
3. **Zones live on the profile.** A slot's bands are the zones of the gauge on
   the item's profile with the same suffix, anchored to the slot's scale, and a
   zone with no threshold draws nothing. `gauge_zone_alarms.go` builds an item's
   alarms from the item and the profile gauge alone, so a tile and its alarm
   cannot differ and the alarm does not depend on which tile shows it. Alarm ids for these are
   `zone:item:<item id>:<suffix>:<n>`: one alarm per item and reading however
   many tiles show it, named for the item ("Port engine Oil Press"). There is
   no per-tile override, and the unit of a reading on the profile is the
   profile's (a slot's own quantity and unit apply only to a reading the profile
   lacks), because the thresholds are in that unit. The scale a zone is anchored
   to is the slot's, else the gauge's, else 0 to 100, in one helper per language.
   The exception is a fuel bar: a tank has no profile, so a bar keeps its own
   zones, as bands of the level in percent, and they raise a low-fuel alarm
   keyed `zone:tank:<tank item id>:<n>` and named "<tank> level". Editing a zone in a tile's form edits the profile (the
   tile's save writes the profile first and stops if the server refuses it), and
   the form names the profile and the items that follow it. A reading the
   profile has no gauge for has no zones; the form says so.
4. **The picker.** The tile dialogs list mapped items of the right kind. An item
   with no source is listed separately and, when chosen, offers a list of
   discovered reporting sources of that kind that no item owns; mapping writes
   the item's `signalk_source`, the field on the item page, and the notice says
   it changes the item for every tile and feature.
5. **Statuses.** A tile with no item shows "Pick equipment", one whose item was
   deleted "Equipment removed", an item with no source "Not mapped", and an item
   whose source Discovery does not list "Not reporting", each with a link to the
   item. Discovery not having answered is not "Not reporting", and an engine the
   server knows is switched off still draws its tile with OFF. There is no
   fallback to a path the tile once held. A lamp whose item is unmapped reads as
   no data, with the strip still drawn.
6. **Removed.** Apply gauge zones, the equipment profile dialog (and its Add Tile
   entry), the gauge group's Apply profile merge and its retarget helper, the
   free-text engine instance and the path-pattern ribbon suggestions. The ribbon
   now suggests one lamp per mapped engine, generator, charger, inverter and
   alternator, labelled with the item's name.
7. **One-shot conversion** (`dashboard_equipment_convert.go`), after the vessel
   conversion and after profiles load, recorded by `equipment_converted` in the
   pages file so a plain group of paths made later is never converted. A tile
   whose paths sit under a mapped item's source becomes a reference and keeps
   its display choices. A slot reading outside the tile's source is dropped and
   logged. A tile that matches no item keeps its readings as suffixes with no
   item and asks for one. A tile's zones move onto the profile gauge when the
   profile has none for that reading; when it has its own they are dropped and
   logged once; a band that floats mid-scale, a reading the profile has no gauge
   for, or an item with no profile are dropped and logged; zones in another unit
   than the profile's are converted. A fuel bar's zones are kept on the bar. Lamps with a
   mapped source become references; an unowned lamp keeps its path and is
   logged. A profile or pages file that cannot be written stops startup.

Mate has no tool that builds or edits these tiles, and the record registry
does not carry dashboard records, so nothing there changed.

## Phase 3 (not built)

1. **Dependants are visible.** Unassigning a source or deleting an item shows a
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
