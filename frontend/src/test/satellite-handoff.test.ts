import { describe, expect, it } from 'vitest'
import { computeSatelliteBlend } from '@/components/anchor-watch-map'

describe('satellite handoff blend', () => {
  it('uses world imagery at anchor map minimum zoom to avoid no-data Himawari tiles', () => {
    const blend = computeSatelliteBlend(10, true)

    expect(blend.worldBlend).toBe(1)
    expect(blend.himawariBlend).toBe(0)
  })

  it('returns zero blend when satellite imagery is disabled', () => {
    const blend = computeSatelliteBlend(14, false)

    expect(blend.worldBlend).toBe(0)
    expect(blend.himawariBlend).toBe(0)
  })
})
