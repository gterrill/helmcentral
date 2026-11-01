import { describe, expect, it } from 'vitest'

import { sailedLines, type TimedSegment } from '@/lib/passages'
import {
  MAX_SPEED_SEGMENTS, SPEED_RAMP_DARK, SPEED_RAMP_LIGHT, extentAt, nextSpeedGradient, rampColour, speedFrameAt, speedProfile, speedTrack,
} from '@/lib/replay-speed'

// Points a minute apart due east along the equator.
const KT = 1852 / 3600
const metresPerDegree = 111_195
function eastward(steps: number[], t0 = 1000): TimedSegment {
  const seg: TimedSegment = [{ t: t0, lat: 0, lon: 150 }]
  for (const s of steps) {
    const last = seg[seg.length - 1]
    seg.push({ t: last.t + 60, lat: 0, lon: last.lon + s })
  }
  return seg
}
const kts = (degPerMin: number) => (degPerMin * metresPerDegree) / 60 / KT
const luma = (hex: string) => { const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16)); return 0.2126 * r + 0.7152 * g + 0.0722 * b }

describe('speedProfile', () => {
  it('reads each point\'s speed from the distance and time around it', () => {
    const p = speedProfile(eastward([0.001, 0.001, 0.001, 0.001, 0.001, 0.001]))
    expect(p.sog).toHaveLength(7)
    for (const v of p.sog) expect(v).toBeCloseTo(kts(0.001), 1)
  })

  it('accumulates distance along the track from zero', () => {
    const p = speedProfile(eastward([0.001, 0.001]))
    expect(p.dist[0]).toBe(0)
    expect(p.dist[2]).toBeCloseTo(2 * 0.001 * metresPerDegree, 0)
    expect(p.totalM).toBe(p.dist[2])
  })

  it('sees a change of pace where it happens', () => {
    const p = speedProfile(eastward([0.002, 0.002, 0.002, 0.002, 0.0005, 0.0005, 0.0005, 0.0005]))
    expect(p.sog[1]).toBeGreaterThan(p.sog[7] * 2)
  })

  it('gives a stopped boat zero, and a repeated time stamp zero rather than infinity', () => {
    const seg = [{ t: 0, lat: 0, lon: 150 }, { t: 0, lat: 0, lon: 150.01 }, { t: 60, lat: 0, lon: 150.01 }] as TimedSegment
    for (const v of speedProfile(seg).sog) expect(Number.isFinite(v)).toBe(true)
  })
})

describe('speedTrack', () => {
  const piece = (segments: TimedSegment[], key = 'p1') => ({ key, passageId: key, segments, from: -Infinity, to: Infinity })

  it('runs the top of the ramp to the speed most of the passage stays under, never less than 5 knots', () => {
    expect(speedTrack([piece([eastward(Array(10).fill(0.0001))])])!.domainKts).toBe(5)
    const fast = speedTrack([piece([eastward(Array(20).fill(0.002))])])!.domainKts
    expect(fast).toBeGreaterThan(5)
    expect(fast).toBeCloseTo(kts(0.002), 0)
  })

  it('measures the metres of track in each passage', () => {
    const t = speedTrack([piece([eastward(Array(100).fill(0.001))])])!
    expect(t.passageM.get('p1')).toBeCloseTo(100 * 0.001 * metresPerDegree, 0)
  })

  it('has nothing for a track cut into more segments than the colours take', () => {
    const segs = Array.from({ length: MAX_SPEED_SEGMENTS + 1 }, (_, i) => eastward([0.001], 1000 + i * 1000))
    expect(speedTrack([piece(segs)])).toBeNull()
    expect(speedTrack([piece(segs.slice(0, MAX_SPEED_SEGMENTS))])).not.toBeNull()
  })
})

describe('rampColour', () => {
  it('runs from the first colour to the last in each theme', () => {
    expect(rampColour(SPEED_RAMP_LIGHT, 0)).toBe(SPEED_RAMP_LIGHT[0])
    expect(rampColour(SPEED_RAMP_LIGHT, 1)).toBe(SPEED_RAMP_LIGHT[SPEED_RAMP_LIGHT.length - 1])
    expect(rampColour(SPEED_RAMP_DARK, 5)).toBe(SPEED_RAMP_DARK[SPEED_RAMP_DARK.length - 1])
    expect(rampColour(SPEED_RAMP_DARK, -1)).toBe(SPEED_RAMP_DARK[0])
  })

  it('keeps the slow end away from the pale map in light mode and the dark map in dark mode', () => {
    for (const c of SPEED_RAMP_LIGHT) expect(luma(c)).toBeLessThan(200)
    for (const c of SPEED_RAMP_DARK) expect(luma(c)).toBeGreaterThan(50)
  })
})

describe('nextSpeedGradient', () => {
  const seg = eastward([0.002, 0.002, 0.002, 0.002, 0.0005, 0.0005, 0.0005, 0.0005])
  const profile = speedProfile(seg)

  it('runs from 0 to 1 along the sailed length, strictly rising', () => {
    const { stops } = nextSpeedGradient(profile, profile.totalM, null, 10, SPEED_RAMP_LIGHT)
    expect(stops[0][0]).toBe(0)
    expect(stops[stops.length - 1][0]).toBe(1)
    for (let i = 1; i < stops.length; i++) expect(stops[i][0]).toBeGreaterThan(stops[i - 1][0])
  })

  it('puts the fast colour where the boat was fast and the slow colour where she was slow', () => {
    const { stops } = nextSpeedGradient(profile, profile.totalM, null, kts(0.002), SPEED_RAMP_LIGHT)
    expect(stops[0][1]).toBe(rampColour(SPEED_RAMP_LIGHT, profile.sog[0] / kts(0.002)))
    expect(stops[0][1]).toBe(SPEED_RAMP_LIGHT[SPEED_RAMP_LIGHT.length - 1])
    expect(stops[stops.length - 1][1]).toBe(rampColour(SPEED_RAMP_LIGHT, profile.sog[8] / kts(0.002)))
    expect(stops[stops.length - 1][1]).not.toBe(stops[0][1])
  })

  it('stops short of the end of the track when only part of it is sailed', () => {
    const { stops } = nextSpeedGradient(profile, profile.dist[4], null, 10, SPEED_RAMP_LIGHT)
    expect(stops[stops.length - 1][0]).toBe(1)
    // The sailed length ends exactly on the fifth fix: the last colour is that fix's.
    expect(stops[stops.length - 1][1]).toBe(rampColour(SPEED_RAMP_LIGHT, profile.sog[4] / 10))
  })

  it('never has more stops than a gradient texture can carry', () => {
    const long = speedProfile(eastward(Array(5000).fill(0.001)))
    expect(nextSpeedGradient(long, long.totalM, null, 10, SPEED_RAMP_LIGHT).stops.length).toBeLessThanOrEqual(256)
  })

  it('carries the paint expression for its stops, made once', () => {
    const g = nextSpeedGradient(profile, profile.totalM, null, 10, SPEED_RAMP_LIGHT)
    expect(g.expression.slice(0, 3)).toEqual(['interpolate', ['linear'], ['line-progress']])
    expect(g.expression.slice(3)).toEqual(g.stops.flat())
  })

  describe('over a long track', () => {
    const long = speedProfile(eastward(Array(100).fill(0.001)))
    const build = (extent: number, prev: ReturnType<typeof nextSpeedGradient> | null) => nextSpeedGradient(long, extent, prev, 10, SPEED_RAMP_LIGHT)

    it('keeps the gradient it has while the sailed length grows by a little', () => {
      const first = build(long.totalM * 0.5, null)
      expect(build(long.totalM * 0.5 * 1.02, first)).toBe(first)
    })

    it('builds a new one once the sailed length has grown enough to show', () => {
      const first = build(long.totalM * 0.5, null)
      const next = build(long.totalM * 0.5 * 1.05, first)
      expect(next).not.toBe(first)
      expect(next.extentM).toBeCloseTo(long.totalM * 0.5 * 1.05, 5)
    })

    it('builds a new one when the replay goes back', () => {
      const first = build(long.totalM * 0.5, null)
      expect(build(long.totalM * 0.4, first)).not.toBe(first)
    })
  })
})

describe('where the stops sit', () => {
  it('follows the line the map draws: by projected length, not by distance over the ground', () => {
    // 30 degrees of latitude twice over, an hour apart: equal on the ground, but the
    // projected line is shorter from 0 to 30 than from 30 to 60.
    const seg = [{ t: 0, lat: 0, lon: 150 }, { t: 3600, lat: 30, lon: 150 }, { t: 7200, lat: 60, lon: 150 }] as TimedSegment
    const profile = speedProfile(seg)
    expect(profile.dist[1] / profile.totalM).toBeCloseTo(0.5, 2)
    const { stops } = nextSpeedGradient(profile, profile.totalM, null, 30, SPEED_RAMP_LIGHT)
    const y = (lat: number) => 0.5 - Math.log(Math.tan(Math.PI / 4 + (lat * Math.PI) / 360)) / (2 * Math.PI)
    const expected = (y(0) - y(30)) / (y(0) - y(60))
    expect(stops).toHaveLength(3)
    expect(stops[1][0]).toBeCloseTo(expected, 3)
    expect(expected).toBeLessThan(0.45)
  })

  it('puts the end of a part-sailed track at the end of the line it draws', () => {
    const seg = [{ t: 0, lat: 0, lon: 150 }, { t: 3600, lat: 30, lon: 150 }, { t: 7200, lat: 60, lon: 150 }] as TimedSegment
    const profile = speedProfile(seg)
    const { stops } = nextSpeedGradient(profile, profile.dist[1] * 1.5, null, 30, SPEED_RAMP_LIGHT)
    expect(stops.at(-1)![0]).toBe(1)
    for (let i = 1; i < stops.length; i++) expect(stops[i][0]).toBeGreaterThan(stops[i - 1][0])
  })
})

describe('extentAt', () => {
  const seg = eastward([0.001, 0.001, 0.001, 0.001])
  const profile = speedProfile(seg)

  it('is the distance sailed by a time, interpolated between fixes', () => {
    expect(extentAt(profile, seg[0].t - 100)).toBe(0)
    expect(extentAt(profile, seg[0].t + 30)).toBeCloseTo(profile.dist[1] / 2, 3)
    expect(extentAt(profile, seg[4].t + 500)).toBe(profile.totalM)
  })
})

describe('sailedLines', () => {
  const a = eastward([0.001, 0.001, 0.001], 1000)
  const b = eastward([0.001, 0.001, 0.001], 5000)

  it('keeps a line for each segment, in step with them, and null where there is none', () => {
    const lines = sailedLines([a, b], 1090)
    expect(lines).toHaveLength(2)
    expect(lines[0]).toHaveLength(3)
    expect(lines[1]).toBeNull()
  })

  it('ends the last line on the boat', () => {
    const lines = sailedLines([a, b], 5100, { lat: 1, lon: 2 })
    expect(lines[0]![lines[0]!.length - 1]).not.toEqual([2, 1])
    expect(lines[1]![lines[1]!.length - 1]).toEqual([2, 1])
  })
})

describe('speedFrameAt', () => {
  // 100 fixes a minute apart along the equator, 0.001 degrees (111 m) apiece: 11 km in all.
  const seg = eastward(Array(100).fill(0.001), 1000)
  const pieces = [{ key: 'p1', passageId: 'p1', segments: [seg], from: -Infinity, to: Infinity }]
  const track = speedTrack(pieces)!
  const ramp = SPEED_RAMP_LIGHT
  const VIEW = { zoom: 12 }
  const at = (t: number) => new Map([['p1', { at: t, end: { lat: 0, lon: seg[Math.round((t - 1000) / 60)].lon } }]])
  const frame = (t: number, prev: ReturnType<typeof speedFrameAt> | null = null, view = VIEW) => speedFrameAt(pieces, track, at(t), ramp, prev, view)
  const coordinates = (line: { data: GeoJSON.Feature<GeoJSON.LineString> }) => line.data.geometry.coordinates
  const wakeM = (f: ReturnType<typeof speedFrameAt>) => {
    const c = f.wake!.data.geometry.coordinates
    return (c[c.length - 1][0] - c[0][0]) * metresPerDegree
  }

  it('draws the sailed line with a gradient along it', () => {
    const line = frame(1000 + 60 * 50).lines.get('p1')![0]!
    expect(coordinates(line)).toHaveLength(51)
    expect(line.gradient.stops[0][0]).toBe(0)
    expect(line.extentM).toBeCloseTo(50 * 0.001 * metresPerDegree, 0)
  })

  it('has no line and no wake for a passage the replay has not reached', () => {
    const f = speedFrameAt(pieces, track, new Map(), ramp, null, VIEW)
    expect(f.lines.size).toBe(0)
    expect(f.wake).toBeNull()
  })

  it('keeps the gradient from the last frame while the line has grown by a little', () => {
    const first = frame(1000 + 60 * 50)
    const next = frame(1000 + 60 * 51, first)
    expect(next.lines.get('p1')![0]!.gradient).toBe(first.lines.get('p1')![0]!.gradient)
    expect(coordinates(next.lines.get('p1')![0]!)).toHaveLength(52)
  })

  it('draws the same line and wake for the same inputs, whatever was drawn before', () => {
    const a = frame(1000 + 60 * 70, frame(1000 + 60 * 10))
    const b = frame(1000 + 60 * 70)
    expect(coordinates(a.lines.get('p1')![0]!)).toEqual(coordinates(b.lines.get('p1')![0]!))
    expect(a.wake!.data).toEqual(b.wake!.data)
  })

  it('hands back the very same line for a passage the replay has left behind', () => {
    const a = frame(1000 + 60 * 70)
    const b = frame(1000 + 60 * 70, a)
    expect(b.lines.get('p1')![0]).toBe(a.lines.get('p1')![0])
    expect(b.lines.get('p1')).toBe(a.lines.get('p1'))
    const c = frame(1000 + 60 * 71, b)
    expect(c.lines.get('p1')![0]).not.toBe(a.lines.get('p1')![0])
  })

  it('draws the dark ramp when the theme changes and the clock has not moved', () => {
    const t = 1000 + 60 * 70
    const light = speedFrameAt(pieces, track, at(t), SPEED_RAMP_LIGHT, null, VIEW)
    const dark = speedFrameAt(pieces, track, at(t), SPEED_RAMP_DARK, light, VIEW)
    const was = light.lines.get('p1')![0]!
    const now = dark.lines.get('p1')![0]!
    expect(now).not.toBe(was)
    expect(now.gradient.stops[0][1]).toBe(rampColour(SPEED_RAMP_DARK, track.profiles.get('p1')![0].sog[0] / track.domainKts))
    expect(now.gradient.stops[0][1]).not.toBe(was.gradient.stops[0][1])
    expect(now.gradient.expression).not.toBe(was.gradient.expression)
  })

  it('draws the new track when the passage is loaded again, with the clock where it was', () => {
    const t = 1000 + 60 * 70
    const first = frame(t)
    const reloaded = [{ ...pieces[0], segments: [eastward(Array(100).fill(0.002), 1000)] }]
    const again = speedFrameAt(reloaded, speedTrack(reloaded)!, at(t), ramp, first, VIEW)
    expect(again.lines.get('p1')).not.toBe(first.lines.get('p1'))
    expect(again.lines.get('p1')![0]!.data.geometry.coordinates).not.toEqual(first.lines.get('p1')![0]!.data.geometry.coordinates)
  })

  it('draws a new gradient when the top of the ramp has moved', () => {
    const profile = speedProfile(eastward(Array(100).fill(0.001)))
    const g = nextSpeedGradient(profile, profile.totalM * 0.5, null, 10, SPEED_RAMP_LIGHT)
    expect(nextSpeedGradient(profile, profile.totalM * 0.5, g, 10, SPEED_RAMP_LIGHT)).toBe(g)
    expect(nextSpeedGradient(profile, profile.totalM * 0.5, g, 20, SPEED_RAMP_LIGHT)).not.toBe(g)
    expect(nextSpeedGradient(profile, profile.totalM * 0.5, g, 10, SPEED_RAMP_DARK)).not.toBe(g)
    expect(nextSpeedGradient(speedProfile(eastward(Array(100).fill(0.001))), profile.totalM * 0.5, g, 10, SPEED_RAMP_LIGHT)).not.toBe(g)
  })

  describe('the wake', () => {
    it('ends on the boat', () => {
      const f = frame(1000 + 60 * 50)
      const c = f.wake!.data.geometry.coordinates
      expect(c[c.length - 1]).toEqual(coordinates(f.lines.get('p1')![0]!).at(-1))
    })

    it('is four per cent of the passage when that is long enough to see', () => {
      // At zoom 16 a pixel is about 2.4 m at the equator: 60 px is 143 m, under 4 percent of 11 km (445 m).
      expect(wakeM(frame(1000 + 60 * 50, null, { zoom: 16 }))).toBeCloseTo(0.04 * track.passageM.get('p1')!, -1)
    })

    it('is 60 pixels on the screen when four per cent of the passage would be shorter', () => {
      // At zoom 12 a pixel is about 19 m: 60 px is 1.15 km, over 4 percent of 11 km (445 m).
      expect(wakeM(frame(1000 + 60 * 90, null, { zoom: 12 }))).toBeCloseTo(60 * 19.1, -2)
    })

    it('grows as the map zooms out, and is never longer than what has been sailed', () => {
      expect(wakeM(frame(1000 + 60 * 90, null, { zoom: 10 }))).toBeGreaterThan(wakeM(frame(1000 + 60 * 90, null, { zoom: 12 })))
      expect(wakeM(frame(1000 + 60 * 10, null, { zoom: 4 }))).toBeCloseTo(10 * 0.001 * metresPerDegree, 0)
    })

    it('shrinks with the boat\'s latitude, since the same pixels cover less ground', () => {
      const t = 1000 + 60 * 90
      const equator = wakeM(frame(t))
      const sailed = new Map([['p1', { at: t, end: { lat: 60, lon: seg[90].lon } }]])
      const north = speedFrameAt(pieces, track, sailed, ramp, null, VIEW)
      expect(wakeM(north)).toBeCloseTo(equator / 2, -2)
    })

    it('is a light tint of the colour the boat is sailing in, fading to nothing at its tail', () => {
      const f = frame(1000 + 60 * 50)
      const boatColour = f.lines.get('p1')![0]!.gradient.stops.at(-1)![1]
      expect(f.wake!.colour).toMatch(/^#[0-9a-f]{6}$/)
      expect(luma(f.wake!.colour)).toBeGreaterThan(luma(boatColour))
      expect(luma(f.wake!.colour)).toBeGreaterThan(200)
      const g = f.wake!.gradient as unknown[]
      expect(String(g[4])).toMatch(/^rgba\(\d+, \d+, \d+, 0\)$/)
      expect(String(g[6])).toMatch(/, 0\.95\)$/)
    })

    it('is nothing before the boat has moved', () => {
      expect(speedFrameAt(pieces, track, new Map([['p1', { at: seg[0].t, end: { lat: 0, lon: seg[0].lon } }]]), ramp, null, VIEW).wake).toBeNull()
    })
  })
})
