import { render } from '@testing-library/react'
import { describe, expect, it, vi, beforeEach } from 'vitest'
import {
  MapPlaceLabels,
  BASE_VECTOR_SOURCE_ID,
  PLACE_LABEL_LAYER_IDS,
  warnIfBaseVectorSourceMissing,
} from '@/components/map-place-labels'

interface RecordedLayerProps {
  id: string
  type?: string
  source?: string
  'source-layer'?: string
  minzoom?: number
  maxzoom?: number
  beforeId?: string
  filter?: unknown
  layout?: Record<string, unknown>
  paint?: Record<string, unknown>
}
let recordedLayers: RecordedLayerProps[] = []

vi.mock('react-map-gl/maplibre', () => ({
  Layer: (props: RecordedLayerProps) => {
    recordedLayers.push(props)
    return null
  },
}))

beforeEach(() => {
  recordedLayers = []
})

function byId(id: string) {
  return recordedLayers.find((l) => l.id === id)
}

describe('MapPlaceLabels', () => {
  it('mounts all five layer ids', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    const mountedIds = recordedLayers.map((l) => l.id)
    for (const id of PLACE_LABEL_LAYER_IDS) {
      expect(mountedIds).toContain(id)
    }
    expect(mountedIds).toHaveLength(5)
  })

  it('attaches every layer to the carto source with the right source-layer', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    expect(byId('place-names-water')).toMatchObject({ source: BASE_VECTOR_SOURCE_ID, 'source-layer': 'water_name' })
    expect(byId('place-names-island')).toMatchObject({ source: BASE_VECTOR_SOURCE_ID, 'source-layer': 'place' })
    expect(byId('place-names-harbor')).toMatchObject({ source: BASE_VECTOR_SOURCE_ID, 'source-layer': 'poi' })
    expect(byId('place-names-peak')).toMatchObject({ source: BASE_VECTOR_SOURCE_ID, 'source-layer': 'mountain_peak' })
    expect(byId('place-names-town-topup')).toMatchObject({ source: BASE_VECTOR_SOURCE_ID, 'source-layer': 'place' })
  })

  it('gives every layer the coalesce(name, name_en) text field and symbol type', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    for (const id of PLACE_LABEL_LAYER_IDS) {
      const layer = byId(id)
      expect(layer?.type).toBe('symbol')
      expect(layer?.layout?.['text-field']).toEqual(['coalesce', ['get', 'name'], ['get', 'name_en']])
    }
  })

  it('the water filter admits both bay and strait classes', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    const filter = byId('place-names-water')?.filter
    expect(filter).toEqual([
      'all',
      ['has', 'name'],
      ['==', ['geometry-type'], 'Point'],
      ['in', ['get', 'class'], ['literal', ['bay', 'strait']]],
    ])
  })

  it('the island filter is class == island', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    expect(byId('place-names-island')?.filter).toEqual(['all', ['has', 'name'], ['==', ['get', 'class'], 'island']])
  })

  it('sets the documented minzoom per layer', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    expect(byId('place-names-water')?.minzoom).toBe(9)
    expect(byId('place-names-island')?.minzoom).toBe(9)
    expect(byId('place-names-harbor')?.minzoom).toBe(14)
    expect(byId('place-names-peak')?.minzoom).toBe(12)
    expect(byId('place-names-town-topup')?.minzoom).toBe(14)
  })

  it('paints white text on a black halo when overImagery is true, regardless of theme', () => {
    render(<MapPlaceLabels isDarkTheme={true} overImagery={true} />)

    for (const id of PLACE_LABEL_LAYER_IDS) {
      expect(byId(id)?.paint).toMatchObject({
        'text-color': '#ffffff',
        'text-halo-color': '#000000',
        'text-halo-width': 1.5,
      })
    }
  })

  it('paints the light-theme colours when overImagery is false and isDarkTheme is false', () => {
    render(<MapPlaceLabels isDarkTheme={false} overImagery={false} />)

    expect(byId('place-names-water')?.paint).toMatchObject({ 'text-color': '#7a96a0', 'text-halo-width': 1 })
    expect(byId('place-names-island')?.paint).toMatchObject({ 'text-color': '#697b89', 'text-halo-width': 1 })
  })

  it('paints the dark-theme colours when isDarkTheme is true and overImagery is false', () => {
    render(<MapPlaceLabels isDarkTheme={true} overImagery={false} />)

    expect(byId('place-names-water')?.paint).toMatchObject({ 'text-color': 'rgba(155,155,155,1)', 'text-halo-color': '#181818' })
    expect(byId('place-names-island')?.paint).toMatchObject({ 'text-color': 'rgba(204,208,228,1)', 'text-halo-color': '#181818' })
  })
})

describe('warnIfBaseVectorSourceMissing', () => {
  it('logs an explicit, prefixed error when the carto source is absent', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    warnIfBaseVectorSourceMissing({ getSource: () => undefined })

    expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining('[map-place-labels]'))
    expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining(BASE_VECTOR_SOURCE_ID))
    errorSpy.mockRestore()
  })

  it('does not log when the carto source is present', () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    warnIfBaseVectorSourceMissing({ getSource: () => ({}) })

    expect(errorSpy).not.toHaveBeenCalled()
    errorSpy.mockRestore()
  })
})
