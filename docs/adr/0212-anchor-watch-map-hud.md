# ADR 0212: The Anchor Watch Page Puts Its Readings on the Map

## Status

Accepted (2026-10-25)

## Context

The full-page Anchor Watch view stacked a card above the map (depth, tide, the
Adjust text button, the low-water clearance line) and a full-width row below it
for Drop and Raise. On a phone or a short laptop screen those two bands left
the chart a strip. The page exists to watch the swing on the chart, and the map
already carries a top-left panel for distance, bearing, radius and current.

## Decision

1. **One panel on the map.** The header card is gone when the map is on screen.
   Its content is passed to the map as a `hudHeader` slot and rendered at the top
   of the existing metrics panel: depth (with the stale badge), the tide
   direction and one line with the next turn and estimated depth, then the
   low-water clearance line, then the existing rows. The station name is
   visible text beside the Tide label, truncated. One presenter component
   renders both this panel and the fallback card, so the content lives once. The dashboard tile passes no slot and is unchanged.
2. **Phone width.** Below `sm` the quiet ok/unknown clearance lines are
   hidden; the too-shallow alert never is. The other rows stay.
3. **Drop and Raise float over the map.** Drop is bottom-centre, Raise compact
   at bottom-left (bottom-right is MapLibre's attribution, and Drop keeps a
   right-hand gutter clear of it). Raise stays visible during Adjust, lifted above
   the Cancel/Save row on narrow screens, and raising still closes Adjust.
4. **Adjust has one entry.** The Move icon in the map's control stack. The text
   Adjust button survives only on the fallback card.
5. **Fallback kept.** With no GPS fix or no WebGL2 there is no panel, so the
   card and the Drop/Raise row render as before. Nothing is invented for that
   state.
6. **The map keeps clear of the panel.** The map observes the panel's size.
   The AIS label avoid-zone follows it, and MapLibre's top padding is set to the
   panel's footprint so the anchor, boat and the radius fit are centred in the
   clear part. The padding is zero for the tile (no header) and during Adjust,
   whose crosshair must stay at the true screen centre.

## Rejected

- **Two stacked panels** (depth card plus metrics panel): more of the chart
  covered for the same information.
- **Tap-to-expand on phones.** Leaving out the quiet lines is enough.
- **Hiding Raise during Adjust.** It removed a path that existed.

## Consequences

- The map panel is taller, so it covers more of the top-left corner. AIS labels
  under it are suppressed by the existing avoid-zone logic.
- Two renderings of the depth/tide figures exist, the dark panel and the
  fallback card. They share the formatting helpers.

## Related

- [ADR 0059](0059-always-on-anchor-map-drop-raise-and-scope-method.md): Drop/Raise on the map.
- [ADR 0135](0135-anchor-watch-warns-too-shallow-at-low-water.md): the clearance warning.
- [ADR 0136](0136-anchor-adjust-is-a-crosshair-mode.md): Adjust entry points.
