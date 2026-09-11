interface VesselArrowProps {
  /** Heading in compass degrees; unrotated (pointing up the screen) when null. */
  headingDeg: number | null
  /** Marker scale, typically markerScaleForZoom(zoom) from lib/marker-labels.ts. */
  scale: number
}

/**
 * The own-ship arrowhead marker: a white triangle outlined in the map's
 * accent blue, rotated to heading and scaled by zoom.
 *
 * Extracted from anchor-watch-map.tsx's inline vessel marker SVG so the
 * poi-map widget (ADR 0091 phase 3b) can draw the same own-ship icon without
 * importing the whole anchor-watch map component.
 */
export function VesselArrow({ headingDeg, scale }: VesselArrowProps) {
  return (
    <div
      style={{
        transform: `${headingDeg !== null ? `rotate(${headingDeg}deg) ` : ''}scale(${scale})`,
        transformOrigin: 'center',
        transition: 'transform 150ms ease-out',
        filter: 'drop-shadow(0 1px 3px rgba(0,0,0,0.6))',
      }}
    >
      <svg width="22" height="32" viewBox="0 0 22 32" fill="none">
        <path d="M11 2 L20 26 L11 22 L2 26 Z" fill="white" stroke="#0ea5e9" strokeWidth="1.5" />
      </svg>
    </div>
  )
}
