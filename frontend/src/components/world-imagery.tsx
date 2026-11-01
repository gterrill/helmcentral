import { Satellite } from 'lucide-react'
import { Layer, Source } from 'react-map-gl/maplibre'

import { MapToggleButton } from '@/components/map-toggle-button'
import { WORLD_IMAGERY_LAYER_ID, worldImageryPaint, worldImagerySource } from '@/lib/world-imagery'

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
      <Layer id={WORLD_IMAGERY_LAYER_ID} type="raster" beforeId={beforeId} minzoom={minzoom} paint={worldImageryPaint(opacity)} />
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
  return <MapToggleButton icon={Satellite} label="Toggle satellite imagery" on={on} onToggle={onToggle} className={className} />
}
