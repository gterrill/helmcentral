// Shared map-marker label helpers, moved out of anchor-watch-map.tsx so the
// poi-map widget (ADR 0091 phase 3b) can reuse the same declutter and
// zoom-scaling logic without importing a 1800-line map component for two
// pure functions. anchor-watch-map.tsx re-exports these so its own tests
// (and anything importing from it) keep working unchanged.

// ── AIS label collision avoidance ───────────────────────────────────────────
// Design critique item 4 kept AIS labels at the 9px floor and solved overlap
// by keeping them apart. They have since grown to 12px because the floor was
// too small to read at arm's length, so they take a wider radius. Two independent
// collisions matter: a label sitting under the metric overlay or the control
// stack (both draw at a much higher z-index and simply blot the text out),
// and two vessel labels landing close enough on screen to overlap each
// other. Both are resolved from already-projected screen pixels so the logic
// stays a pure, deterministic function that needs no live map instance to
// test.
export interface ScreenRect {
  left: number
  top: number
  right: number
  bottom: number
}

export interface MarkerLabelPoint {
  id: string
  x: number
  y: number
  // Lower wins a collision with a higher value -- the vessel's distance from
  // own ship, so the closer (more operationally relevant) vessel keeps its
  // label and the farther one yields.
  priority: number
}

// How close two projected marker centres can land before their labels
// visibly overlap. Tuned by eye against the label block under the icon: 50
// for the POI tile's one-line labels, 65 for the anchor watch's two-line
// 12px AIS labels.
export const LABEL_COLLISION_RADIUS_PX = 50
export const AIS_LABEL_COLLISION_RADIUS_PX = 65

export function resolveMarkerLabelSuppression(
  points: MarkerLabelPoint[],
  avoidZones: ScreenRect[],
  radiusPx = LABEL_COLLISION_RADIUS_PX,
): Set<string> {
  const suppressed = new Set<string>()

  for (const point of points) {
    for (const zone of avoidZones) {
      if (point.x >= zone.left && point.x <= zone.right && point.y >= zone.top && point.y <= zone.bottom) {
        suppressed.add(point.id)
        break
      }
    }
  }

  // Closest (highest-priority) vessel first, so a three-way pile-up keeps
  // the single most relevant label rather than an arbitrary survivor.
  const byPriority = [...points].sort((a, b) => a.priority - b.priority)
  for (let i = 0; i < byPriority.length; i++) {
    const a = byPriority[i]
    if (suppressed.has(a.id)) continue
    for (let j = i + 1; j < byPriority.length; j++) {
      const b = byPriority[j]
      if (suppressed.has(b.id)) continue
      const dx = a.x - b.x
      const dy = a.y - b.y
      if (Math.sqrt(dx * dx + dy * dy) < radiusPx) {
        // b comes after a in priority order, so it's the one that yields.
        suppressed.add(b.id)
      }
    }
  }

  return suppressed
}

/**
 * Scale a marker by zoom: full size at zoom 14+, shrinking linearly down to
 * 0.45x at zoom 10 and below. Extracted from anchor-watch-map.tsx's inline
 * markerScale expression so the poi-map widget's markers shrink on the same
 * curve rather than reinventing the constants.
 */
export function markerScaleForZoom(zoom: number): number {
  return Math.max(0.45, Math.min(1, (zoom - 10) / (14 - 10)))
}

/**
 * Dark halo that keeps white marker labels legible on any map tile. The map is
 * not themed, so this is a fixed colour on purpose.
 */
// eslint-disable-next-line helm/no-arbitrary-color -- label halo on the non-themed map, black in day and night
export const MAP_LABEL_HALO = 'drop-shadow-[0_1px_2px_rgba(0,0,0,0.8)]'
