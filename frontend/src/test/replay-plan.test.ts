import { describe, expect, it } from 'vitest'

import { prepareVideoReplay, type PassageVideoInput } from '@/lib/passage-video/setup'
import { passageLegs } from '@/lib/passage-legs'
import { buildTimeline, type PassageDetail } from '@/lib/passages'
import { legPassages, labelledSigns, piecesOf, planReplay, playableShots, replayItems, wantsHeading } from '@/lib/replay-plan'
import type { CameraShot } from '@/lib/replay-clock'
import { resolveSigns, type ReplaySign } from '@/lib/replay-signs'

const t0 = Date.UTC(2026, 9, 24, 0, 0) / 1000
const passage = (id: string, at: number, withTrack = true): PassageDetail => ({
  id, start: new Date((t0 + at) * 1000).toISOString(), end: new Date((t0 + at + 1000) * 1000).toISOString(),
  from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '', stats: {}, timezone: 'Australia/Brisbane',
  track: withTrack ? [[[-20, 149], [-20, 149.1]]] : [], track_times: withTrack ? [[t0 + at, t0 + at + 1000]] : [],
  replay_track: withTrack ? [[[-20, 149], [-20, 149.05], [-20, 149.1]]] : [], replay_times: withTrack ? [[t0 + at, t0 + at + 500, t0 + at + 1000]] : [],
} as unknown as PassageDetail)
const sign = (id: string, at: number, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: (t0 + at) * 1000, title: id, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})
const shot = (id: string, at: number): CameraShot => ({ id, t_ms: (t0 + at) * 1000, mode: 'follow', zoom: 14, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1 })

const passages = [passage('a', 0), passage('b', 2000)]
const signs = [sign('named', 300), sign('waiting', 400, { title: '' }), sign('join', 2500, { starts_leg: true })]
const shots = [shot('in', 100), shot('out', 90_000)]

describe('planReplay', () => {
  // The replay panel builds the same things a step at a time, each in its own memo.
  const items = replayItems(passages)
  const timeline = buildTimeline(items)
  const legs = passageLegs(legPassages(items), signs)

  it('is what the panel gets from its steps', () => {
    const plan = planReplay(passages, shots, signs, null, true)
    expect(plan.timeline).toEqual(timeline)
    expect(plan.pieces).toEqual(piecesOf(items, legs))
    expect(plan.resolved).toEqual(playableShots(shots, timeline))
    expect(plan.placed).toEqual(resolveSigns(signs, timeline))
    expect(plan.labelled).toEqual(labelledSigns(resolveSigns(signs, timeline)))
    expect(plan.wantHeading).toBe(wantsHeading(null, true, plan.resolved))
  })

  it('plays the shots inside the passages, and draws only the signs that have a title', () => {
    const plan = planReplay(passages, shots, signs, null, true)
    expect(plan.resolved.map((r) => r.shot.id)).toEqual(['in'])
    expect(plan.placed.map((r) => r.sign.id)).toContain('waiting')
    expect(plan.labelled.map((r) => r.sign.id)).not.toContain('waiting')
  })

  it('cuts a join into legs, which the pieces follow', () => {
    expect(planReplay(passages, [], signs, null, true).pieces.length).toBeGreaterThan(2)
    expect(planReplay(passages, [], [], null, true).pieces.map((p) => p.key)).toEqual(['a', 'b'])
  })

  it('steers by the heading with a model on 3D, or with shots to follow', () => {
    const hull = { type: 'sail_mono', loaM: 12, beamM: 4 } as never
    expect(wantsHeading(null, true, [])).toBe(false)
    expect(wantsHeading(hull, true, [])).toBe(true)
    expect(wantsHeading(hull, false, [])).toBe(false)
    expect(planReplay(passages, [shot('in', 100)], [], null, false).wantHeading).toBe(true)
  })

  it('frames the drawn track, and has no frame for passages with none', () => {
    expect(planReplay(passages, [], [], null, true).bounds?.[0]).toBeCloseTo(149)
    expect(planReplay([passage('x', 0, false)], [], [], null, true).bounds).toBeNull()
  })
})

describe('the video is made from the same plan', () => {
  it('hands the controller what the plan says', () => {
    const input = { orientation: 'landscape', passages, shots, signs, hull: null, threeD: true, satellite: false, isDarkTheme: false, lengthS: 15, summary: {} } as unknown as PassageVideoInput
    const plan = planReplay(passages, shots, signs, null, true)
    const { config, wantHeading, bounds } = prepareVideoReplay(input)
    expect(config.timeline).toEqual(plan.timeline)
    expect(config.pieces).toEqual(plan.pieces)
    expect(config.resolved).toEqual(plan.resolved)
    expect(config.signs).toEqual(plan.labelled)
    expect(wantHeading).toBe(plan.wantHeading)
    expect(bounds).toEqual(plan.bounds)
  })
})
