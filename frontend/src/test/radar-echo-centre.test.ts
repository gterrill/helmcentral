import { describe, expect, it } from 'vitest'
import { resolveEchoCentre } from '@/hooks/use-radar-echo-layer'

// Where the radar picture gets centred, and why it is not the radar's own fix.
//
// Phase 0 measured that Spoke.lat/lon are populated on every single spoke and
// concluded the antenna offset was therefore solved for free: centre on the
// radar's reported position and the offset disappears by construction.
//
// Populated is not the same as live. Measured 2026-09-02, sampled three times
// over 36 s while the boat was making way:
//
//   radar -20.25242,148.94615   ship -20.31148,149.06041
//   radar -20.25242,148.94615   ship -20.31207,149.06025
//   radar -20.25242,148.94615   ship -20.31259,149.06005
//
// The radar's fix is byte-identical every time while own ship moves. mayara
// captures it once and never refreshes it. Centring on it put the overlay
// 12.5 km from the boat and off the visible map entirely, which is how this
// was found: the layer, the source, the palette and 26k drawn pixels were all
// correct and nothing appeared on screen.
//
// So own ship is the reference. The antenna offset is metres; trusting a
// frozen fix is kilometres and grows without bound.
describe('resolveEchoCentre', () => {
  it('centres on own ship, not the radar reported fix', () => {
    const centre = resolveEchoCentre(
      { lat: -20.31148, lon: 149.06041 },
      { lat: -20.25242, lon: 148.94615 },
    )
    expect(centre).not.toBeNull()
    expect(centre!.lat).toBeCloseTo(-20.31148, 6)
    expect(centre!.lon).toBeCloseTo(149.06041, 6)
  })

  it('reports the divergence so a stale radar fix is visible rather than silent', () => {
    const centre = resolveEchoCentre(
      { lat: -20.31148, lon: 149.06041 },
      { lat: -20.25242, lon: 148.94615 },
    )
    // ~12.5 km apart on the measured pair.
    expect(centre!.radarFixDivergenceM).toBeGreaterThan(10_000)
  })

  it('reports no divergence when the radar fix tracks own ship', () => {
    const centre = resolveEchoCentre(
      { lat: -20.31148, lon: 149.06041 },
      { lat: -20.31150, lon: 149.06043 },
    )
    expect(centre!.radarFixDivergenceM).toBeLessThan(50)
  })

  it('still centres when the radar reports no fix at all', () => {
    const centre = resolveEchoCentre({ lat: -20.31148, lon: 149.06041 }, null)
    expect(centre!.lat).toBeCloseTo(-20.31148, 6)
    expect(centre!.radarFixDivergenceM).toBeNull()
  })

  it('refuses to place the canvas with no own-ship fix', () => {
    expect(resolveEchoCentre(null, { lat: -20.25242, lon: 148.94615 })).toBeNull()
  })
})
