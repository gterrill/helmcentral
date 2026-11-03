import { labelHasRoom, mapLabelStyle, type MapLabelBox } from '@/lib/map-label'

export interface MapLabelProps {
  text: string
  /** Where it goes and how it looks (lib/map-label), in the map's own pixels. */
  box: MapLabelBox
  testId: string
}

/** A small label over the map: the stay's as the boat lies in port, the
 * passage's as it sets sail. It draws what the controller says and decides
 * nothing about time or place; lib/map-label gives where it sits and how it
 * looks, so a video can draw the same label on a canvas. It lets touches through. */
export function MapLabel({ text, box, testId }: MapLabelProps) {
  // No room left between what it keeps clear of: draw nothing, not an empty pill.
  if (!labelHasRoom(box)) return null
  return (
    <div className="pointer-events-none absolute inset-0 z-20 overflow-hidden" data-testid={testId}>
      <span
        className="absolute truncate bg-card/80 text-foreground shadow-sm backdrop-blur-sm"
        style={mapLabelStyle(box)}
      >
        {text}
      </span>
    </div>
  )
}
