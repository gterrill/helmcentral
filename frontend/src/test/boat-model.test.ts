import { describe, expect, it } from 'vitest'

import {
  boatMarkerKind,
  boatScaleFactor,
  circularMeanDeg,
  HEADING_SMOOTH_S,
  HeadingSmoother,
  nextHeading,
  MIN_BOAT_PX,
  resolveBoatDimensions,
  trackHeading,
  trackIndex,
} from '@/lib/boat-model'
import { metersPerPixel } from '@/lib/anchor-adjust'
import { positionAt, type TimedSegment } from '@/lib/passages'

describe('resolveBoatDimensions', () => {
  it('uses the vessel LOA and beam when both are known', () => {
    expect(resolveBoatDimensions('sail_mono', 14.2, 4.3)).toEqual({ loaM: 14.2, beamM: 4.3, missing: [] })
  })
  it('derives the beam from a typical length-to-beam ratio when only the length is known', () => {
    const mono = resolveBoatDimensions('sail_mono', 12, null)
    expect(mono.loaM).toBe(12)
    expect(mono.beamM).toBeGreaterThan(3)
    expect(mono.beamM).toBeLessThan(5)
    expect(mono.missing).toEqual(['beam'])
    const cat = resolveBoatDimensions('power_cat', 12, null)
    expect(cat.beamM).toBeGreaterThan(mono.beamM)
  })
  it('has no length at all when none is known: a unit-length model, not a made-up boat', () => {
    const d = resolveBoatDimensions('sail_cat', null, 6)
    expect(d.loaM).toBeNull()
    expect(d.missing).toEqual(['length'])
    // The shape is built one metre long, so its beam is the ratio, below 1 for a monohull.
    expect(resolveBoatDimensions('sail_mono', null, null).beamM).toBeLessThan(1)
  })
  it('treats zero and negative figures as unknown', () => {
    expect(resolveBoatDimensions('power_mono', 0, -1).loaM).toBeNull()
  })
})

describe('boatScaleFactor', () => {
  it('is 1 once the boat is at least the minimum size on screen', () => {
    expect(boatScaleFactor(19, 45, 12)).toBe(1)
  })
  it('enlarges the boat so its length is never under the minimum', () => {
    for (const zoom of [8, 12, 14, 16, 17]) {
      const f = boatScaleFactor(zoom, -20, 12)
      const px = (12 * f) / metersPerPixel(-20, zoom)
      expect(px).toBeGreaterThanOrEqual(MIN_BOAT_PX - 1e-6)
    }
  })
  it('grows as the map zooms out and is the same either side of the equator', () => {
    expect(boatScaleFactor(10, 30, 12)).toBeGreaterThan(boatScaleFactor(14, 30, 12))
    expect(boatScaleFactor(10, 30, 12)).toBeCloseTo(boatScaleFactor(10, -30, 12))
  })
  it('needs less enlarging for a longer boat', () => {
    expect(boatScaleFactor(12, 0, 24)).toBeLessThan(boatScaleFactor(12, 0, 12))
  })
  it('draws a boat of unknown length at the minimum size at every zoom', () => {
    for (const zoom of [6, 12, 16, 20]) {
      // The unit-length model times the factor is its length in metres.
      const px = boatScaleFactor(zoom, 40, null) / metersPerPixel(40, zoom)
      expect(px).toBeCloseTo(MIN_BOAT_PX, 6)
    }
  })
})

describe('circularMeanDeg', () => {
  it('averages across north', () => {
    expect(circularMeanDeg([359, 1])).toBeCloseTo(0, 5)
    expect(circularMeanDeg([350, 20])).toBeCloseTo(5, 5)
  })
  it('stays in 0..360', () => {
    expect(circularMeanDeg([270, 280])).toBeCloseTo(275, 5)
  })
})

// A boat steaming east at 6 kn, one fix every 30 s.
const KN = 0.514444
function run(bearingEast = true, speedKts = 6, n = 20): TimedSegment {
  const mPerDegLon = 111_320
  return Array.from({ length: n }, (_, i) => ({
    t: i * 30,
    lat: bearingEast ? 0 : i * ((speedKts * KN * 30) / 110_574),
    lon: bearingEast ? i * ((speedKts * KN * 30) / mPerDegLon) : 0,
  }))
}

describe('trackHeading', () => {
  it('points along the track the boat has just sailed', () => {
    expect(trackHeading([run()], 300, null)).toBeCloseTo(90, 0)
    expect(trackHeading([run(false)], 300, null)).toBeCloseTo(0, 0)
  })
  it('holds the previous heading when the boat is stopped and nothing lies ahead', () => {
    expect(trackHeading([run(true, 0.1)], 300, 123)).toBe(123)
  })
  it('prefers the way she is about to go over a held heading', () => {
    const wait: TimedSegment = Array.from({ length: 6 }, (_, i) => ({ t: i * 30, lat: 0, lon: 0 }))
    const go = run().slice(1).map((p) => ({ ...p, t: p.t + 150 }))
    expect(trackHeading([[...wait, ...go]], 120, 270)).toBeCloseTo(90, 0)
  })
  it('leaves a berth on the new passage heading, not the old one', () => {
    // Passage 1 ends alongside heading 270; passage 2 starts there heading 045.
    const west: TimedSegment = Array.from({ length: 10 }, (_, i) => ({ t: i * 30, lat: 0, lon: -i * 0.0014 }))
    const berthed = [...west, ...[1, 2, 3, 4].map((k) => ({ ...west.at(-1)!, t: west.at(-1)!.t + k * 30 }))]
    const ne: TimedSegment = Array.from({ length: 10 }, (_, i) => ({ t: i * 30, lat: i * 0.001, lon: i * 0.001 }))
    const under = nextHeading(null, { id: 'p1', at: west.at(-1)!.t }, [berthed])
    expect(under.heading).toBeCloseTo(270, 0)
    // Alongside, stopped: the last heading is held.
    const state = nextHeading(under, { id: 'p1', at: berthed.at(-1)!.t }, [berthed])
    expect(state.heading).toBeCloseTo(270, 0)
    const start2 = nextHeading(state, { id: 'p2', at: 0 }, [ne])
    expect(start2.heading).toBeCloseTo(45, 0)
  })
  it('holds through a pause in the middle of a run', () => {
    const seg = run()
    const stopped: TimedSegment = [...seg, ...seg.slice(-1).flatMap((p) => [1, 2, 3, 4].map((k) => ({ ...p, t: p.t + k * 30 })))]
    expect(trackHeading([stopped], stopped.at(-1)!.t, 77)).toBe(77)
  })
  it('looks ahead at the very start when there is nothing to hold yet', () => {
    expect(trackHeading([run()], 0, null)).toBeCloseTo(90, 0)
  })
  it('is null for a boat that has never moved', () => {
    expect(trackHeading([run(true, 0)], 100, null)).toBeNull()
    expect(trackHeading([], 100, null)).toBeNull()
  })
  it('smooths across north without swinging through 180', () => {
    // 359 -> 1 degree: lon wobbles either side of a northbound track.
    const seg: TimedSegment = [
      { t: 0, lat: 0, lon: 0 },
      { t: 30, lat: 0.0005, lon: -0.0000087 },
      { t: 60, lat: 0.001, lon: 0 },
      { t: 90, lat: 0.0015, lon: 0.0000087 },
      { t: 120, lat: 0.002, lon: 0 },
    ]
    const h = trackHeading([seg], 120, null)!
    const off = Math.min(h, 360 - h)
    expect(off).toBeLessThan(2)
  })
})

describe('nextHeading', () => {
  const stopped: TimedSegment = Array.from({ length: 6 }, (_, i) => ({ t: i * 30, lat: 0, lon: 0 }))
  it('holds the last heading only while playback moves forward within the same passage', () => {
    const last = { id: 'p1', at: 100, heading: 123 }
    expect(nextHeading(last, { id: 'p1', at: 110 }, [stopped]).heading).toBe(123)
    // backwards, another passage, or a long jump all recompute: nothing moved, so nothing to say
    expect(nextHeading(last, { id: 'p1', at: 50 }, [stopped]).heading).toBeNull()
    expect(nextHeading(last, { id: 'p2', at: 110 }, [stopped]).heading).toBeNull()
    expect(nextHeading(last, { id: 'p1', at: 100 + 10_000 }, [stopped]).heading).toBeNull()
  })
})

describe('trackIndex', () => {
  it('answers exactly as positionAt does, across gaps and past both ends', () => {
    const segs: TimedSegment[] = [
      [{ t: 10, lat: 0, lon: 0 }, { t: 20, lat: 1, lon: 1 }, { t: 40, lat: 2, lon: 3 }],
      [],
      [{ t: 100, lat: 5, lon: 5 }, { t: 130, lat: 6, lon: 4 }],
      [{ t: 200, lat: 7, lon: 7 }],
    ]
    const index = trackIndex(segs)
    for (let t = 0; t <= 230; t += 3.5) expect(index.positionAt(t)).toEqual(positionAt(segs, t))
    expect(trackIndex([]).positionAt(5)).toBeNull()
  })
})

describe('boatMarkerKind', () => {
  const hull = { type: 'sail_mono' as const, loaM: null, beamM: null }
  it('is the model only with 3D on, sound terrain, a hull type and the layer ready', () => {
    expect(boatMarkerKind({ threeD: true, terrain: 'ok', hull, layerReady: true })).toBe('model')
  })
  it('is the dot when the hull type is not set', () => {
    expect(boatMarkerKind({ threeD: true, terrain: 'ok', hull: null, layerReady: true })).toBe('dot')
  })
  it('is the dot with 3D off', () => {
    expect(boatMarkerKind({ threeD: false, terrain: 'ok', hull, layerReady: true })).toBe('dot')
  })
  it('is the dot when the terrain failed or is being retried', () => {
    expect(boatMarkerKind({ threeD: true, terrain: 'failed', hull, layerReady: true })).toBe('dot')
    expect(boatMarkerKind({ threeD: true, terrain: 'retrying', hull, layerReady: true })).toBe('dot')
  })
  it('is the dot until the model has loaded', () => {
    expect(boatMarkerKind({ threeD: true, terrain: 'ok', hull, layerReady: false })).toBe('dot')
  })
})

describe('normalizeBoatHull', () => {
  it('reads the chosen hull type and has no stand-in for an empty or unknown one', async () => {
    const { normalizeBoatHull } = await import('@/config/app-config')
    expect(normalizeBoatHull({ anchor: { hull_type: ' Sail_Cat ' } })).toBe('sail_cat')
    expect(normalizeBoatHull({ anchor: { hull_type: '' } })).toBeNull()
    expect(normalizeBoatHull({ anchor: { hull_type: 'trawler' } })).toBeNull()
    expect(normalizeBoatHull(null)).toBeNull()
  })
})

describe('HeadingSmoother', () => {
  const T = 500
  it('holds nothing until a heading arrives, and returns the first one exactly', () => {
    const s = new HeadingSmoother()
    expect(s.step(null, 0)).toBeNull()
    expect(s.step(123, 100)).toBe(123)
  })
  it('keeps the held value when the target is null', () => {
    const s = new HeadingSmoother()
    s.step(90, 0)
    expect(s.step(null, 500)).toBe(90)
  })
  it('snaps to the target when asked', () => {
    const s = new HeadingSmoother()
    s.step(90, 0)
    expect(s.step(200, 16, { snap: true })).toBe(200)
  })
  it('a snap with a null target clears the held value', () => {
    const s = new HeadingSmoother()
    s.step(90, 0)
    expect(s.step(null, 16, { snap: true })).toBeNull()
    expect(s.step(null, 32)).toBeNull()
    expect(s.step(40, 48)).toBe(40)
  })
  it('holds the value when no time has passed', () => {
    const s = new HeadingSmoother()
    s.step(90, 1000)
    expect(s.step(200, 1000)).toBe(90)
    expect(s.step(200, 900)).toBe(90)
  })
  it('turns the short way round north, both directions', () => {
    const k = 1 - Math.exp(-0.5 / HEADING_SMOOTH_S)
    const a = new HeadingSmoother()
    a.step(350, 0)
    // 350 -> 10 is +20 through north: half a second closes that fraction of it
    expect(a.step(10, T)).toBeCloseTo((350 + 20 * k) % 360, 5)
    const b = new HeadingSmoother()
    b.step(10, 0)
    expect(b.step(350, T)).toBeCloseTo(360 + 10 - 20 * k, 5)
  })
  it('normalises output into [0, 360)', () => {
    const s = new HeadingSmoother()
    s.step(355, 0)
    for (let i = 1; i <= 100; i++) {
      const v = s.step(5, i * 50) as number
      expect(v).toBeGreaterThanOrEqual(0)
      expect(v).toBeLessThan(360)
    }
  })
  it('converges on a sustained step within 3 s of 60 fps frames', () => {
    const s = new HeadingSmoother()
    s.step(80, 0)
    let v = 80
    for (let i = 1; i <= 180; i++) v = s.step(170, (i * 1000) / 60) as number
    expect(Math.abs(v - 170)).toBeLessThan(1)
  })
  it('does not wobble on an alternating jittery target', () => {
    const s = new HeadingSmoother()
    let prev = s.step(90, 0) as number
    let worst = 0
    for (let i = 1; i <= 240; i++) {
      const v = s.step(i % 2 === 0 ? 80 : 100, (i * 1000) / 60) as number
      worst = Math.max(worst, Math.abs(v - prev))
      prev = v
    }
    expect(worst).toBeLessThan(0.5)
  })
  it('caps one step at half a second of watched time', () => {
    const s = new HeadingSmoother()
    s.step(0, 0)
    const v = s.step(90, 60_000) as number
    expect(v).toBeCloseTo(90 * (1 - Math.exp(-0.5 / HEADING_SMOOTH_S)), 5)
  })
  it('hold() means the first step after a gap eases by a normal frame, not the cap', () => {
    const s = new HeadingSmoother()
    s.step(0, 0)
    s.hold(10_000)
    const v = s.step(90, 10_016) as number
    expect(v).toBeCloseTo(90 * (1 - Math.exp(-0.016 / HEADING_SMOOTH_S)), 5)
  })
})
