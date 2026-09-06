# ADR 0061: A Fuel Rail on the Engine Cluster, and the Tank Map It Corrected

## Status
Accepted

Extends ADR 0054 (engine cluster tile) with a sixth kind of slot, and inherits
ADR 0050's rule that a zone is the alarm.

## Context

The vessel carries four fuel tanks, two a side. Until now the dashboard showed
them only as percentages in the Tanks tile, so working out how much fuel was
actually aboard meant knowing four capacities and doing the arithmetic by hand.

The first attempt was a plain radial gauge bound to `tanks.fuel.2.currentLevel`.
It could not be made to look like the reference MFD for two reasons that are
worth recording, because neither is fixed here: `GaugeWidgetConfig` has no icon
field at all, and `ringStyle`, `readout` and `labelDivisor` have no UI in
`gauge-fields.tsx` despite being valid on the type and validated server side. A
plain gauge therefore cannot be given an icon or the instrument ring from the
dialog. That gauge has been removed in favour of the rail.

### Reading the vessel first changed the design, again

As in ADR 0054, inspecting the live vessel data established the available inputs.

- **Capacity is on the delta stream.** The backend has no REST read path
  (`signalk_payload.go`), so a value that never arrives as a delta is invisible
  to it whatever the SignalK REST model shows. `tanks.fuel.N.capacity` does
  arrive, which is what made the whole feature a config change rather than a new
  backend pipeline. Had it not, capacity would have had to come from settings.
- **Not one tank path carries `units` metadata**, exactly as ADR 0054 found for
  the engine paths. The path picker infers the quantity from `meta.units`, so
  for a tank it infers nothing and preselects Unitless. See §3.
- **The saved tank labels were wrong.** See §1.

## Decision

### 1. The physical tank map, and the labels it corrects

| Tank | Side | Capacity | Sender |
|---|---|---|---|
| `tanks.fuel.5` | Port forward | 1.2 m3 | YachtDevices.6 |
| `tanks.fuel.4` | Port aft | 1.3 m3 | YachtDevices.6 |
| `tanks.fuel.2` | Starboard forward | 1.2 m3 | YachtDevices.7 |
| `tanks.fuel.7` | Starboard aft | 1.3 m3 | YachtDevices.7 |

`ui.tank_labels` in settings.yaml had `fuel.2` as PORT FWD and `fuel.5` as STBD
FWD, which is this table with the forward pair swapped. The `$source` grouping
settles it: tanks 4 and 5 are both published by one sender and tanks 2 and 7 by
the other. One sender per side is the only physically plausible wiring; a sender
reading one tank on each side is not.

**This table is the single source of truth, and it has two consumers that cannot
be derived from one another.** The Tanks tile reads `ui.tank_labels`; the rail
reads paths in each cluster's widget config. Correcting the labels does not stop
the next drift, and merging the two would be a larger change than this feature.
Anyone touching either should know the other exists.

settings.yaml is gitignored, so no test in this repo can guard the mapping. This
table is the only durable copy.

### 2. A rail on the cluster, not a widget of its own

The rail is an optional field on `EngineClusterConfig`, rendered on whichever
edge of the tile it is configured for. Each engine then sits beside the fuel it
burns, and a facing pair of clusters puts its rails outboard.

`side` is `left | right`, not `port | starboard`. Which edge the rail sits on and
which side of the boat its tanks are on are independent facts: a starboard
cluster laid out on the left of a page wants a left-hand rail. The tanks' side
is already carried by their paths and by the tile's own title.

**The cluster canvas keeps its 520 width.** The rail is a sibling column and the
body keeps its own coordinate space, because the corner masks are computed at
module scope against that 520 and the cards are positioned from its edges.
Growing the canvas would re-centre the ring and move all four cards on every
cluster, including ones with no rail. Only the design width `useFitScale`
measures against grows.

The height does not change, so the tile's derived `minH` is untouched. The width
does, so a cluster carrying a rail gets a floor of 5 columns rather than 4.

### 3. Both fuel slots must declare their quantity, and the save is refused otherwise

A tank's level is a 0..1 ratio and its capacity is m3, and litres is the product.
Because no tank path publishes `meta.units`, `GaugeFields` preselects Unitless
whenever a path is picked. Left that way `convertFromSI` is the identity and the
rail reads 1.2 where it should read 890.

To prevent incorrect fuel readings, `validateClusterFuelRail` rejects a level
that is not `ratio` and a capacity that is not `volume`. It does not guess the
unit, consistent with the fallback policy.

The dialog then pins those quantities on every change, so the rule is one the
operator never meets. That pinning is not a nicety: without it the form produces
a config the backend refuses, and the error names a field the form never showed.

### 4. Capacity is a second gauge slot, not a field on the first

`GaugeWidgetConfig` binds one widget to one path, and litres needs two readings.
Both slots are that type verbatim, which is the decision that made gauge groups
and cluster corners cheap: zones, units, `validateGaugeConfig` and
`gaugeZoneRule` all work unchanged.

The cost is that the capacity slot leaves `min`, `max`, `zones`, `window` and
`ringStyle` meaningless. That is paid in the editor, which renders a cut-down
form for it, rather than in a parallel type with a second Go struct, a second
validator and a second bound-path branch.

### 5. Empty and absent must not look alike

A tank reading zero and a sender that has stopped talking both draw a
zero-length bar. On a fuel gauge that is a safety problem, not a cosmetic one.

Each bar carries `data-bar-state` of `reading`, `empty` or `absent`. An absent
level draws its track dashed and at reduced opacity and no bar at all, and the
readout shows the structural dash. An empty tank draws a solid track and reads
`0`.

**The total is `--` unless every tank contributed both a level and a capacity.**
A sum missing one tank is a plausible number that is wrong.

The total is computed from unrounded readings, so it is the volume actually
aboard rather than the displayed figures added up. Those can differ by a litre:
890 and 889 are each rounded up while the true 1778.4 rounds down.

### 6. The scale is vertical, and identical on both tiles

Built first as a curve, because the reference MFD draws one: its fan is polar,
with the 0..100 scale on the angular axis, ticks as radial segments and each
tank a thick arc at its own radius. That is `dial-ring.tsx`'s maths at a
different aspect ratio, and it looked close to the reference.

**Both the curve and the mirroring were then removed.** Two reasons, and neither
showed up until it was on a board next to a real dial:

- A bowed bar is harder to read against a straight tick than a straight bar is.
  The curve is decorative here in a way it is not on a round dial, where the arc
  *is* the scale.
- Mirroring put the port rail's scale on its right and the starboard rail's on
  its left, so the two engines' fuel gauges were handed differently. Reading the
  second one required switching scale orientation. A consistent orientation was
  preferred over a symmetrical layout.

So the bars run straight up their own columns, ticks and numbers sit to their
right on both tiles, and the `%` marks the right-hand side of the scale in both.
`side` now says only which edge of the tile the rail attaches to, which is the
cluster's business rather than the rail's, and the rail renders identically
whichever it is given.

Two viewBox units to the design pixel, for the reason ADR 0054 gives: whole
units at this size are far too coarse to place ticks in.

**Labels every 25 percent, ticks every 5.** The reference labels every 10, but
the panel is 112px wide before the canvas scales down, and eleven numbers at the
legibility floor do not survive that. Tick density matches the reference; label
density does not.

The hairline outside the bars was removed with the curve. It clarified the
curved scale's outline but was unnecessary for the straight scale.

### 7. Zones alarm, and at a fixed index

A low-fuel band is a zone like any other, so it raises an alarm (ADR 0050)
rather than only colouring, which is what a cluster telltale does.

Fuel bars are collected at a **fixed base index** past every corner row a cluster
can hold, not appended after the corners actually present. Appending would keep
existing ids stable but renumber the fuel bars whenever a corner row was added,
so an unrelated edit would orphan a low-fuel alarm's history and its
acknowledgement.

Level slots only. A capacity is a constant, and an alarm derived from one would
never clear.

## Consequences

- A railed cluster's design is 646 wide rather than 520, so at a given column it
  scales to about four fifths of what it did. Both clusters were widened to 6
  columns to absorb it, which moved Vessel Economy to its own row.
- The geometry constants stay in the component rather than becoming tokens. The
  tick and label columns are placed against the bar columns in the same file, so
  a skin free to move one and not the other could put a bar through its
  neighbour. Only colour and the glow are tokens.
- `--fuel-bar-glow` holds a whole `filter` value rather than one drop-shadow's
  arguments, which is how `--dial-needle-glow` is written. One argument list can
  only make one falloff, and the reference's bars have a tight emissive core and
  a wide bloom. That is two stacked `drop-shadow()` functions, and it is why the
  dial's glow never matched.
- `reading`, `zoneFor` and `zoneTextClass` moved from `engine-cluster-tile.tsx`
  to `lib/cluster-readings.ts`, since a component the tile renders cannot import
  from the tile. `zoneStroke` in `dial-ring.tsx` was exported as `zoneColor` and
  the cluster's duplicate `zoneFill` deleted, so the alarm palette has one home.
