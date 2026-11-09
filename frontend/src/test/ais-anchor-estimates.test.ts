import { describe, expect, it } from 'vitest'

import { aisAnchorSwingCircles, type AisAnchorEstimate } from '@/lib/ais-anchor-estimates'
import { haversineMeters } from '@/lib/geo'

const est = (over: Partial<AisAnchorEstimate> = {}): AisAnchorEstimate => ({
  mmsi: '111111111',
  lat: -20.001,
  lon: 149.002,
  swing_radius_m: 40,
  drawn_radius_m: 55,
  swing_arc_deg: 180,
  points: 50,
  overlaps_ours: false,
  ...over,
})

describe('aisAnchorSwingCircles', () => {
  it('is an empty collection with no estimates', () => {
    expect(aisAnchorSwingCircles([])).toEqual({ type: 'FeatureCollection', features: [] })
  })

  it('draws one closed polygon per estimate at its drawn radius, not the bare fit, round its anchor', () => {
    const { features } = aisAnchorSwingCircles([est()])
    expect(features).toHaveLength(1)
    const f = features[0]
    expect(f.geometry.type).toBe('Polygon')
    const ring = f.geometry.coordinates[0]
    expect(ring[0]).toEqual(ring[ring.length - 1])
    for (const [lon, lat] of ring) {
      expect(haversineMeters(-20.001, 149.002, lat, lon)).toBeCloseTo(55, 0)
    }
    expect(f.properties).toEqual({ mmsi: '111111111', overlaps: false })
  })
})
