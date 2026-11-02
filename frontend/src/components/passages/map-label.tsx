import { MAP_LABEL, type MapLabelBox } from '@/lib/map-label'

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
  return (
    <div className="pointer-events-none absolute inset-0 z-20 overflow-hidden" data-testid={testId}>
      <span
        className="absolute truncate bg-card/80 text-foreground shadow-sm backdrop-blur-sm"
        style={{
          left: box.x,
          top: box.y,
          transform: box.align === 'center' ? 'translateX(-50%)' : undefined,
          fontSize: box.fontPx,
          fontWeight: MAP_LABEL.weight,
          lineHeight: MAP_LABEL.lineHeight,
          borderRadius: MAP_LABEL.radius,
          padding: `${box.padY}px ${box.padX}px`,
          maxWidth: box.maxW,
          opacity: box.alpha,
        }}
      >
        {text}
      </span>
    </div>
  )
}
