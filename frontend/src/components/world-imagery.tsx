import { Satellite } from 'lucide-react'
import { Layer, Source } from 'react-map-gl/maplibre'

import { cn } from '@/lib/utils'
import { worldImageryPaint, worldImagerySource } from '@/lib/world-imagery'

export interface WorldImageryLayerProps {
  /** A fixed opacity, or a style expression that follows the zoom. */
  opacity: number | readonly unknown[]
  /** The layer this one goes under. */
  beforeId?: string
  /** Layer minzoom: below it the layer is hidden and loads no tiles. */
  minzoom?: number
}

/** The imagery source and its raster layer. The caller decides when to mount it. */
export function WorldImageryLayer({ opacity, beforeId, minzoom }: WorldImageryLayerProps) {
  return (
    <Source id="world-imagery" {...worldImagerySource({})}>
      <Layer id="world-imagery-layer" type="raster" beforeId={beforeId} minzoom={minzoom} paint={worldImageryPaint(opacity)} />
    </Source>
  )
}

export interface ImageryToggleButtonProps {
  on: boolean
  onToggle: () => void
  className?: string
}

/** The satellite button both maps carry: sky-blue when imagery is on. */
export function ImageryToggleButton({ on, onToggle, className }: ImageryToggleButtonProps) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label="Toggle satellite imagery"
      aria-pressed={on}
      className={cn(
        'flex h-10 w-10 items-center justify-center rounded-lg text-white shadow-sm backdrop-blur-sm active:scale-95',
        on ? 'bg-sky-600/90 hover:bg-sky-500/90' : 'bg-black/65 hover:bg-black/80',
        className,
      )}
      style={{ transition: 'background-color 150ms ease-out' }}
    >
      <Satellite className="h-4 w-4" />
    </button>
  )
}
