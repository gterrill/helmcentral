# ADR 0188: Engine Cluster v2

## Status

Accepted. Builds on [ADR 0187](0187-equipment-items-own-their-signalk-source.md),
whose equipment model the tile's slots use. Supersedes the layout in ADR 0054
section 5a (the telltale strip) and ADR 0061 (the straight fuel rail). The rest
of ADR 0054 stands: one tile is one engine, every slot is a `GaugeWidgetConfig`,
zones are the alarm source (ADR 0050), skin tokens (ADR 0060), colour is state
only (ADR 0080), engine off (ADR 0163).

## Context

A design for a full engine card (800 x 616: a 380 px tachometer, four readout
cards cut round it, a fuel fan, a footer) replaced the 520-wide ring-and-corners
layout. Paths were captured from the live SignalK server rather than assumed.

Under ADR 0187 a cluster stores an engine item and, per slot, a suffix under that
item's source; the path is worked out on every read and the zones come from the
item's profile. The new card shows more than one piece of equipment: the starter
battery, the alternator and the exhaust are not the engine.

## Decision

1. **Layout.** One tile, one engine. `side` (`port` | `stbd`, absent is port)
   mirrors the card: fuel and the status chip on the outer edge, readouts and
   hours on the inner. The geometry is pure functions of the side
   (`lib/engine-cluster-layout.ts`). The card scales with `FitCanvas`; it needs 6
   of 12 columns and 14 rows, and is 592 design px tall, ending 12 px under the
   footer.
2. **Slots are the equipment model's.** The ring, centre, corners, the two dial
   arcs and the transmission pressure are suffixes under the cluster's engine item
   and take the engine profile's zones, quantity, unit and scale
   (`resolveSlot`), exactly like the slots they join. The footer's starter
   battery, alternator (amps, watts, temperature, stage, charge limit) and
   exhaust name their own item with a per-slot `equipmentId` and read a suffix of
   its source, with zones from *that item's* profile. A slot whose item is gone
   or unmapped reads as no data and never falls back to the engine. No slot holds
   a path; the backend refuses one. A slot that is not set is absent, draws
   nothing, raises nothing. A corner holds one row and a rail two tanks, since
   that is what is drawn; the backend caps match.
3. **Which alternator.** There is no alternator instance handling of its own
   (ADR 0180). **Fill slots from profile** reads the port and starboard
   alternator roles (Settings, SignalK, Roles) and seeds the alternator slots
   with the item the engine's side names. With no role set they stay unset; the
   operator picks one. The starter battery has no role, so it is picked. The
   exhaust is an item mapped to its own source (`propulsion.0`), because it is
   not part of either engine.
4. **Alarm ids.** Zones on a profile alarm as one rule per item and reading
   (`zone:item:<item>:<suffix>:<n>`), whichever tile shows them, so a footer
   slot's zones alarm under its own item. A footer slot whose own item is gone or
   unmapped reads nothing and raises nothing; it never falls back to the
   engine's source. Zones a slot still holds (a converted tile with no profile)
   keep the index ids of ADR 0187. The old telltales shipped at index 18 and up,
   and a telltale moved onto an arc keeps the index of its own old position
   (`zoneIndex` on the slot), so no issued id changes, whichever order the
   telltales were in. The coolant arc is 18 by default. The footer and the right
   arc are new and follow without gaps: footer slot i is 24 + i in the order of
   the frontend's `CLUSTER_FOOTER_SLOTS`, the right arc 32. A fixture read by both
   the Go and the frontend tests pins that order. Append only from here.
5. **Telltales move.** `telltales` is a legacy field only the conversion and a
   load-time step read: the engine temperature goes to the left arc and the
   gearbox temperature to the right arc; anything else (the exhaust read
   from another node) is dropped and logged. A save that still carries one is
   refused. A cluster saved under the old caps (a corner of several rows, a rail
   of up to four tanks) is trimmed to the new ones at load and logged, so its
   page stays editable; one narrower than the card is drawn at the minimum width.
6. **Bars from zones.** A bullet graph draws only the bands the slot has,
   brightens the one the reading is in, ticks only real end-anchored
   thresholds, and keeps fill and value neutral until the reading is in an
   amber or red band. Bundled profiles ship null thresholds, so the usual case
   is one normal band and no ticks, and a reading outside it is amber, never red
   (and does not light the tile edge, as before).
7. **The right arc is the gearbox oil temperature.** This vessel has no engine
   oil temperature sensor; its only oil temperature is the gearbox's
   (`transmission.oilTemperature`). The first cut left the right arc empty and
   kept the gearbox temperature as the transmission card's header value, so as
   not to pass the gearbox off as engine oil. The operator then asked for it
   under the rpm, which reverses that decision: the right arc is bound from the
   engine item like the coolant arc, labelled and iconed as the gearbox, and
   Fill slots binds it. The transmission card shows only
   `transmission.oilPressure` (value and bullet). The footer's gearbox-temperature
   slot is gone; its place in the footer's id order is held as a gap so the
   exhaust keeps its index. A v1 telltale for the gearbox lands on the right arc.
8. **Exhaust is wet.** Scale 0 to 100 C with an above band for raw-water loss,
   set by the operator on the exhaust item's profile. No threshold of the
   vessel's is shipped.
9. **Paths captured** (live SignalK): starter battery
   `electrical.batteries.292.voltage` ("PORT START BATT"); alternator
   `electrical.alternator.<n>.current`, `.power`, `.temperature`,
   `.chargingModeNumber`; exhaust `propulsion.0` and `.1` `.exhaustTemperature`.
   Nothing publishes a Batrium charge limit, or a Wakespeed stage as text, so the
   charge limit slot ships unset and the stage is read as the regulator's state
   number and named in the tile (Victron state table; an unknown number is
   shown as itself).
10. **History.** The one-hour amps trace reuses `/api/telemetry/history`
    (`window=1h`, one point a minute); no endpoint was added. An error is shown
    in place of a flat line.
11. **Tokens.** `--cluster-*` (band opacities, sweep opacities, surface, stroke,
    track, dial face, rim, hub, needle glow) exist in `:root` and the skin; the
    coverage test checks them.
12. **The tachometer is its own component, not a `DialRing` variant.** The
    reference face (lit radial gradient, inset rim and glow, sweep band rising to
    the reading, short glowing needle, a 190 px hub carrying the readout) shares
    only rules with `DialRing`, not drawing. A variant prop would be the mode
    flag growing, so `lib/dial-geometry.ts` holds what both need (zone marking,
    severity colour, sweep and tick geometry) and `DialRing` uses it with
    unchanged behaviour. `components/ui/cluster-dial.tsx` builds on the same
    helpers. Red ticks and numbers come from the rpm slot's zones, never a stored
    redline: with null thresholds there is none.
13. **The hub and the scale agree.** The hub shows revs as plain rpm rounded to
    the nearest ten, as the reference does, and the scale (divided by 100) is
    captioned "scale x100".
14. **The chip is for a running engine.** RUN, WARN and ALARM are drawn only
    when the engine is known to be turning on live data: not when it is off, its
    feed is lost, its rpm is stale, missing or zero, or the tile is stale, so a
    stale border never has RUN beside it.
15. **Off is said once.** An engine that is off shows `--`, a neutral border and
    the muted OFF pill in the title (ADR 0163). The card's status chip is for a
    running engine and is not drawn when it is off, so OFF does not appear twice.
    No history request is made for an off engine.

## Consequences

Existing clusters are rebuilt on upgrade as far as the conversion can: the
coolant and gearbox move; the operator chooses the engine side, the footer
equipment and fills the slots again (CHANGELOG, Breaking). The straight fuel rail
component and the fuel rail's caption and edge choice are removed.
