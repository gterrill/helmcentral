import { describe, expect, it } from 'vitest'

import { prepareVideoReplay, type PassageVideoInput } from '@/lib/passage-video/setup'
import { passageLegs } from '@/lib/passage-legs'
import { buildTimeline, type PassageDetail } from '@/lib/passages'
import { legPassages, labelledSigns, piecesOf, planFromItems, planReplay, playableShots, replayConfigOf, replayItems, wantsHeading } from '@/lib/replay-plan'
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

describe('a plan from items the caller has already read (the exported page)', () => {
  const named = passages.map((p) => ({ id: p.id, from_name: p.from_name, to_name: `To ${p.id}`, timezone: null as string | null }))
  const items = replayItems(passages)

  it('is the plan planReplay makes when the offset is the one the passages\' zone gives', () => {
    const mine = planFromItems(items, passages, undefined, [], [], null, false, 10)
    const theirs = planReplay(passages, [], [], null, false)
    expect(mine.stays).toEqual(theirs.stays)
    expect(mine.passageLabels).toEqual(theirs.passageLabels)
  })

  it('counts days at the offset it is given, not by a zone name it does not carry', () => {
    // Without the zone and without an offset the days cannot be counted, and there are no labels.
    expect(planFromItems(items, named, undefined, [], [], null, false).passageLabels).toEqual([])
    // With the boat's offset (+10) they are counted as the cruise page counts them.
    const known = planFromItems(items, named, undefined, [], [], null, false, 10)
    expect(known.passageLabels.map((l) => l.text)).toEqual(['Day 1 \u00b7 to To a', 'Day 1 \u00b7 to To b'])
  })

  it('builds the controller\'s config the way the video does', () => {
    const plan = planReplay(passages, shots, signs, null, true)
    const input = { orientation: 'landscape', passages, shots, signs, hull: null, threeD: true, satellite: false, isDarkTheme: true, lengthS: 30, summary: {} } as unknown as PassageVideoInput
    const mine = replayConfigOf(plan, 30, true)
    const theirs = prepareVideoReplay(input).config
    // The time map holds functions, so it is compared by what it says.
    expect({ ...mine, time: undefined }).toEqual({ ...theirs, time: undefined })
    expect(mine.time.totalWall).toBe(theirs.time.totalWall)
    expect(mine.time.holds).toEqual(theirs.time.holds)
    expect(replayConfigOf(plan, 30, false).isDarkTheme).toBe(false)
  })

  it('leaves out of the time map the moment of a sign whose photo would not load', () => {
    const withPhoto = [sign('p', 300, { photo_url: '/api/documents/p/content' })]
    const plan = planReplay(passages, [], withPhoto, null, false)
    expect(replayConfigOf(plan, 30, false).time.holds.filter((h) => h.kind === 'moment')).toHaveLength(1)
    expect(replayConfigOf(plan, 30, false, new Set(['p'])).time.holds.filter((h) => h.kind === 'moment')).toHaveLength(0)
  })
})
