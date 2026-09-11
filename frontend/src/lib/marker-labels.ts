// Shared map-marker label helpers, moved out of anchor-watch-map.tsx so the
// poi-map widget (ADR 0091 phase 3b) can reuse the same declutter and
// zoom-scaling logic without importing a 1800-line map component for two
// pure functions. anchor-watch-map.tsx re-exports these so its own tests
// (and anything importing from it) keep working unchanged.

// ── AIS label collision avoidance ───────────────────────────────────────────
// Design critique item 4: AIS labels are 9px map annotation (DESIGN.md's
// legibility floor) and must stay there rather than growing to "fix"
// legibility -- the fix is keeping them apart, not bigger. Two independent
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

// Half the label block's on-screen footprint (max-w-28 = 112px wide, two
// lines of 9px text under the marker icon): two labels whose projected
// centres land closer than this will visibly overlap.
export const LABEL_COLLISION_RADIUS_PX = 50

export function resolveMarkerLabelSuppression(
  points: MarkerLabelPoint[],
  avoidZones: ScreenRect[],
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
      if (Math.sqrt(dx * dx + dy * dy) < LABEL_COLLISION_RADIUS_PX) {
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
