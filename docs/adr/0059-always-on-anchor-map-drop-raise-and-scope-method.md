# ADR 0059: Always-on anchor map, a labelled Drop/Raise control, and a scope method setting

## Status
Accepted

Extends ADR 0047 (rode planner replaces the Rode & Scope tile).

## Context

The Anchor Watch tile and the fullscreen panel both rendered their map only after a watch
was active. Before the hook went down there was no chart at all: no look at the seamarks,
the AIS traffic, or the swing room around the spot you were about to commit to. The one
moment the chart matters most is the moment it wasn't there.

Raising the anchor had the opposite problem. Dropping was a labelled button, but raising
was a small unlabelled `CircleStop` icon inside the map's overlay control stack, hidden
whenever an edit mode was active, and it deleted the watch, the trail, and the session's
placemarks in a single tap with no confirmation.

And the "how much chain do I pay out" answer lived only in the Rode Planner sidebar of the
fullscreen panel. ADR 0047 put it there when it retired the Rode & Scope tile, but what
that ADR actually retired was the backward-looking pass/fail badge that graded deployed
rode against a number nobody entered. The forward-looking recommendation is a different
thing, and it is exactly what you want at a glance on the dashboard while picking a spot.

## Decision

### 1. The map renders in every state

Both the tile and the fullscreen panel now mount `AnchorWatchMap` whether or not a watch
is active. Without an anchor the map centres on the vessel and shows the basemap,
seamarks, imagery, and AIS as usual; there is no alarm circle, no anchor marker, no
reposition or radius edit mode, and no placemark pin (the backend refuses placemarks
without an active watch, so the UI never offers one).

`anchorLat`/`anchorLon` became nullable props and the map derives `hasAnchor` once.
There is no separate mode prop; a mode flag would restate what the props already say and
the two could disagree.

The load-bearing detail is layer ordering. The seamark and imagery rasters pin themselves
below the alarm circle with `beforeId="alarm-circle-fill"`, and react-map-gl defers adding
a layer until its `beforeId` target exists. Unmounting the circle layers when there is no
anchor would leave the rasters detached in no-watch mode and scramble the stack when a
watch started mid-session. So the circle source and layers stay mounted unconditionally
and the source's GeoJSON collapses to an empty `FeatureCollection` when there is no
anchor. The layers draw nothing, and the rasters always have their anchor point.

The single exception to "always a map": no GPS fix and no anchor. There is nothing
truthful to centre on, and the previous code's `?? 0` fallback would have rendered Null
Island as if it were a position. That state shows a "No GPS fix" placeholder instead,
with Drop disabled. A fabricated centre is the masking pattern the fallback policy
exists to prevent.

### 2. One labelled button: Drop, or Raise behind a confirmation

A shared `AnchorDropRaiseButton` sits below the map in both hosts. With no watch it is
the existing teal Drop button. With a watch it reads Raise and opens a confirmation
dialog stating what raising actually does: stops the watch, clears the vessel trail,
clears the session's placemarks. That is precisely what `DELETE /api/anchor-watch` does
server-side, so the dialog describes the deletion rather than euphemising it.

The map's `CircleStop` icon is gone, along with its `onClearAnchor` prop. Two affordances
for the same destructive action, one confirmed and one not, is worse than either alone.

Audio priming moved into the shared button. The tile's Drop handler had always primed the
audio context so a later alarm could sound; the panel's Drop handler never did. The
shared button closes that gap for both.

### 3. The map overlay shows the recommendation, not a grade

The recommended rode, from tide-corrected depth and planning wind (seeded the same way the
Rode Planner seeds it, 1-hour max gust falling back to apparent), renders as a Scope row
inside the map's own metric overlay, directly under Current (Distance, Bearing, Radius,
Depth, Current, Scope). It does not live in a separate box below the map. Because the tile
and the fullscreen drawer already share one `AnchorWatchMap` component, putting the row
there gets it into both hosts for the price of one prop, `scopeRecommendation`, rather than
a readout each host has to render for itself.

The row shows only the rode figure and its unit, `55 m`, matching the Distance, Radius,
and Depth rows above it, not a merged suffix. The scope ratio and, when it binds, the
`3:1 minimum` floor marker were briefly rendered as a trailing tag on the row (`55 m ·
4.6:1 · 3:1 min`), but that duplicated what the row's own tooltip already said and crowded
an overlay with no room to spare. Both figures are reachable only through the tooltip now
(the full `note`, below), the same place the depth source and wind figure already lived.

The composition that used to sit inline in the tile, seed the wind, build the
`RodePlanInput`, and pick `catenaryMethod` versus `ratioMethod` off `anchor.scope_method`,
is now `computeScopeRecommendation` in `lib/rode-plan.ts`. The tile and the drawer both
call it in a `useMemo` and hand the result straight to the map; neither duplicates the
~15 lines of composition, and both pick up a change to the seeding or method-selection
rule the moment it lands in one place.

When the calculation cannot answer, the row still prints the reason ("no depth reading",
"no wind data"), never a dash, exactly as the old below-map box did. What moved with it is
where the reason lives on screen: it renders in the row's value slot in place of the
unit suffix, capped and truncated so a long reason ("bow roller height not configured")
cannot stretch the overlay, with the full text on the truncated span's own tooltip. The
full note, the depth source, the wind figure, and the `3:1 minimum` floor marker when it
binds, rides along as a tooltip on the row itself rather than as a second line of text,
since the overlay has no room for a caption the old box could give it.

This does not reverse ADR 0047. The retired badge compared a recommendation against
`rodeDeployedM`, an input this boat cannot measure, and rendered the missing input as a
red alarm. Nothing in the overlay grades anything: there is no OK/Low/Insufficient state,
only the forward-looking number, and the tile and map tests both pin that as a regression
guard.

### 4. A second method: ratio, and it is the default

The ratio method fills the extension seam ADR 0047 left in `lib/rode-plan.ts`:

    recommendedRode = (tide-corrected depth + bow roller height) × ratio
    ratio = 7 when planning wind ≥ 20 kts, else 5

It reads depth, tide, wind, and bow roller height, and nothing else. It computes happily
with chain size, windage, and hull type unconfigured, which the catenary method cannot.
The same unavailable-reason ladder applies, and the tide-corrected versus sounder-only
distinction stays explicit in the caption, as it does for catenary.

`anchor.scope_method` (`catenary` | `ratio`) selects which method feeds the Scope row, in
both hosts, since `computeScopeRecommendation` reads the same `anchorConfig` regardless of
which host called it. The default is `ratio`, an operator decision: it answers from the
instruments the boat actually has, with no dependence on the anchor hardware being
described in settings first. Unknown or absent values normalise to `ratio` through the
same allowlist pattern `hull_type` uses, and the round-trip test goes through the settings
handler deliberately, because that handler rebuilds the anchor map wholesale and a
forgotten key would be silently stripped on every save.

The Rode Planner shows both methods, one group per entry in the `rodeMethods` array, each
with its own figure or its own reason for having none, regardless of the setting. The
setting controls only the Scope row.

Each group also carries its own chain-onboard warning, comparing that group's own
`recommendedRodeM` against `anchor.chain_onboard_m`, a genuinely per-method fact: the two
methods can recommend different rode, so each can separately outgrow what's aboard. LOA is
a different kind of fact: one shared input (bow offset plus hull length) that both methods'
Swing figures use identically, not a property of either method. Its provenance line and its
"not configured" warning are therefore stated once, in the Rode Planner's `SidebarFooter`,
directly above "Apply as alarm radius", the control they gate, rather than repeated inside
every group that would otherwise say the same thing under a different heading. The rule
going forward: a fact that varies by method belongs in that method's group; a fact about a
shared input belongs once, in the footer, next to the control it gates.

Testing the tile against a calm night, 3 kt of wind, turned up a case the catenary method
had no defence against: it recommended "Rode 7 m · 0.6:1". The physics was right for the
modelled load, but a rode paid out that short pulls the anchor upward rather than holding
it down, no matter how little horizontal force the catenary math says it needs to resist.
No scope method should ever recommend below roughly 3:1. The ratio method already respects
this by construction, since its lowest ratio is 5:1, so the floor only ever binds on the
catenary side. It lives in `buildRodePlan` in `lib/rode-plan.ts`, the composition layer,
not in `calculateCatenary` in `catenary.ts`, the physics. `calculateCatenary` keeps
returning whatever the load model says, including numbers below 3:1 in light air, and its
characterization tests keep pinning those raw values. `buildRodePlan` floors the rode and
scope it hands out to `MIN_SCOPE_RATIO` when the raw figure falls short, and marks the
result with a new `scopeFloorApplied` flag rather than substituting the floored number
quietly. The planner caption reads that flag directly: when the floor binds, the catenary
group's caption appends "3:1 minimum" so the substitution is visible rather than presented
as if the model had recommended it. The map's Scope row carries the same marker
indirectly, through the `note` text riding along as its tooltip (§3), rather than reading
`scopeFloorApplied` itself. A dedicated `floorApplied` field briefly existed on
`RodeMethodResult` for exactly that purpose, but the tag it fed was the same one §3 removed
from the row, so the field was removed along with it once nothing else read it.

## Consequences

- "Apply as alarm radius" now follows `anchor.scope_method` instead of always using the
  catenary plan. With both method groups showing their own Swing figure, applying whichever
  one the operator's setting doesn't select would silently produce an alarm radius that
  matches neither number on screen. Apply resolves the configured method's own result from
  `rodeMethods` and applies its `recommendedRodeM + bowOffsetM + loaM`; when that method
  itself can't answer (no depth, no wind, and so on), the button stays disabled and names
  why, rather than substituting the other method's figure.
- The map's no-anchor mode exists in both hosts but placemarks, trails, and edit modes
  remain watch-only, matching the backend's refusals rather than papering over them.
- Raising now takes two taps. That is the point: the one-tap version deleted a night's
  trail without asking.
- The empty-FeatureCollection technique is now the documented way to keep a
  `beforeId`-targeted layer alive while it has nothing to draw. Anyone conditionally
  unmounting `alarm-circle-fill` will rediscover the raster detachment the hard way.
