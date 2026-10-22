import { describe, expect, it } from 'vitest'

import { generateEmbed, generateStandalonePage, prepareExport, type ExportInput } from '@/lib/passage-export'
import { composeImageLayout } from '@/lib/passage-export-layout'
import {
  EXPORT_IMAGE_ATTRIBUTION,
  EXPORT_IMAGE_ATTRIBUTION_SATELLITE,
  addExportImagery,
  exportImageAttribution,
  TRACK_LAYERS,
} from '@/lib/passage-export-style'
import { worldImageryPaint, worldImagerySource, worldImageryVisibleAtZoom } from '@/lib/world-imagery'
import type { PassageDetail } from '@/lib/passages'

const t0 = Date.UTC(2026, 8, 14) / 1000
const passage: PassageDetail = {
  id: 'p', start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + 600) * 1000).toISOString(),
  from_name: 'A', to_name: 'B', title: 'Out', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '',
  stats: {
    distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null, tws_mean_kts: null,
    max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
  },
  start_lon: 148,
  track: [[[-20, 148], [-20.01, 148.01], [-20.02, 148.02]]],
  track_times: [[t0, t0 + 60, t0 + 120]],
  replay_track: [], replay_times: [],
}
const input: ExportInput = {
  kind: 'passage', title: 'Out', passages: [passage], units: 'metric', zone: { offsetHours: 10, label: 'AEST' }, inset: true, playSeconds: 30,
}

describe('satellite imagery in still images', () => {
  it('adds the imagery raster through the boat proxy, fading in from zoom 9 to 10, below the seamarks', () => {
    const sources: { id: string; spec: Record<string, unknown> }[] = []
    const layers: { spec: Record<string, unknown>; before?: string }[] = []
    addExportImagery(
      { addSource: (id, spec) => sources.push({ id, spec: spec as unknown as Record<string, unknown> }), addLayer: (spec, before) => layers.push({ spec: spec as unknown as Record<string, unknown>, before }) },
      'http://boat.local:8080',
      'place-names',
    )
    expect(sources).toHaveLength(1)
    expect(sources[0].spec.tiles).toEqual(['http://boat.local:8080/api/world-imagery/{z}/{x}/{y}?exact=1'])
    expect(sources[0].spec.maxzoom).toBe(18)
    expect(layers).toHaveLength(1)
    expect(layers[0].before).toBe('place-names')
    expect(layers[0].spec.minzoom).toBe(9)
    expect(JSON.stringify((layers[0].spec.paint as Record<string, unknown>)['raster-opacity'])).toBe(
      JSON.stringify(['interpolate', ['linear'], ['zoom'], 9, 0, 10, 1]),
    )
  })

  it('asks the proxy for exact tiles, so a stand-in tile fails the image instead of being drawn', () => {
    expect(worldImagerySource({ origin: 'http://x', exact: true }).tiles[0]).toMatch(/\?exact=1$/)
    expect(worldImagerySource({}).tiles[0]).not.toContain('exact')
  })

  it('shares one source and paint spec between the live map and the images', () => {
    const live = worldImagerySource({})
    expect(live.tileSize).toBe(256)
    expect(live.maxzoom).toBe(18)
    expect(live.attribution).toBe('Source: Esri, Maxar, Earthstar Geographics')
    expect(worldImageryPaint(0.5)['raster-opacity']).toBe(0.5)
  })

  it('counts imagery as visible from zoom 9', () => {
    expect(worldImageryVisibleAtZoom(8.9)).toBe(false)
    expect(worldImageryVisibleAtZoom(9)).toBe(true)
  })

  it('keeps the track casing above the imagery', () => {
    // The images add the track layers after the imagery, casing first.
    expect(String((TRACK_LAYERS[0] as { id: string }).id)).toMatch(/casing$/)
    expect(String((TRACK_LAYERS[1] as { id: string }).id)).toMatch(/line$/)
  })

  it('credits Esri on the image only when imagery is on', () => {
    expect(exportImageAttribution(false)).toBe(EXPORT_IMAGE_ATTRIBUTION)
    expect(exportImageAttribution(true)).toBe(EXPORT_IMAGE_ATTRIBUTION_SATELLITE)
    expect(EXPORT_IMAGE_ATTRIBUTION).not.toContain('Esri')
    expect(EXPORT_IMAGE_ATTRIBUTION_SATELLITE).toContain('Esri, Maxar, Earthstar Geographics')
  })

  it('puts the matching credit in the image layout', () => {
    const measure = (t: string) => t.length * 6
    const spec = { width: 1200, height: 630, title: 'T', dates: 'D', stats: [], inset: false }
    expect(composeImageLayout(spec, measure).attribution.text).toBe(EXPORT_IMAGE_ATTRIBUTION)
    expect(composeImageLayout({ ...spec, satellite: true }, measure).attribution.text).toBe(EXPORT_IMAGE_ATTRIBUTION_SATELLITE)
  })

  it('never puts imagery in the blog embed or the standalone page', () => {
    const data = JSON.stringify(prepareExport(input))
    const html = generateEmbed(input) + generateStandalonePage(input)
    for (const text of [data, html]) {
      expect(text).not.toContain('world-imagery')
      expect(text).not.toContain('Esri')
    }
  })
})
