import { describe, expect, it } from 'vitest'

import { buildReplay, signId } from '@/export-player/plan'
import { prepareExport, type ExportInput } from '@/lib/passage-export'
import { EXPORT_PHOTO_PREFIX } from '@/lib/passage-export-signs'
import type { PassageDetail, PassageStats } from '@/lib/passages'
import { MIN_PASSAGE_SHARE_S, type CameraShot } from '@/lib/replay-clock'
import { planReplay } from '@/lib/replay-plan'
import type { ReplaySign } from '@/lib/replay-signs'
import { MOMENT_TOTAL_S, STAY_S } from '@/lib/replay-time'

// The card's replay is the app's: the same planner, time map and stays, worked
// from nothing but what the card carries.

const STATS: PassageStats = {
  distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null,
  tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}
const T0 = Date.UTC(2026, 8, 14, 0) / 1000
const DAY = 86400

function passage(id: string, t0: number, over: Partial<PassageDetail> & { seconds?: number } = {}): PassageDetail {
  const seconds = over.seconds ?? 3600
  const times = Array.from({ length: 11 }, (_, i) => t0 + (i * seconds) / 10)
  const pts = times.map((_, i) => [-20 + i * 0.01, 148 + i * 0.01] as [number, number])
  return {
    id, start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + seconds) * 1000).toISOString(),
    from_name: `From ${id}`, to_name: `To ${id}`, title: `Leg ${id}`, notes: '', hidden: false, edited: false,
    cruise_id: null, created_at: '', stats: STATS, timezone: 'Australia/Brisbane',
    track: [pts], track_times: [times], replay_track: [pts], replay_times: [times], ...over,
  }
}

const sign = (id: string, atS: number, over: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: atS * 1000, title: `Sign ${id}`, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...over,
})
const URI = `${EXPORT_PHOTO_PREFIX}QUJD`
const photoSign = (id: string, atS: number, over: Partial<ReplaySign> = {}) =>
  sign(id, atS, { document_id: `doc-${id}`, photo_url: `/api/documents/doc-${id}/content`, ...over })

function input(over: Partial<ExportInput> = {}): ExportInput {
  return {
    kind: 'combined', title: 'Out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: false,
    playSeconds: 30, cardId: 'hcp-p', passages: [passage('a', T0), passage('b', T0 + 2 * DAY), passage('c', T0 + 5 * DAY)], ...over,
  }
}

const replay = (over: Partial<ExportInput> = {}) => buildReplay(prepareExport(input(over)))

describe('the card\'s replay', () => {
  it('is the replay\'s plan: the same stays and passage labels the app builds for those passages', () => {
    const passages = [passage('a', T0), passage('b', T0 + 2 * DAY), passage('c', T0 + 5 * DAY)]
    const mine = replay({ passages })
    const theirs = planReplay(passages, [], [], null, false)
    // Named by position in the card, by id in the app: the words, places and times are the same.
    expect(mine.plan.stays.map((s) => [s.label, s.arrival, s.departure])).toEqual(theirs.stays.map((s) => [s.label, s.arrival, s.departure]))
    expect(mine.plan.passageLabels.map((l) => l.text)).toEqual(theirs.passageLabels.map((l) => l.text))
    expect(mine.plan.stays.map((s) => s.label)).toEqual(['2 nights · To a', '3 nights · To b'])
    expect(mine.plan.passageLabels.map((l) => l.text)).toEqual(['Day 1 · From a → To a', 'Day 3 · From b → To b', 'Day 6 · From c → To c'])
  })

  it('holds the replay for each stay on top of the length chosen, and has no stays for one passage', () => {
    for (const playSeconds of [15, 30, 60] as const) {
      const r = replay({ playSeconds })
      expect(r.config.time.sailingWallAt(r.config.time.total)).toBeCloseTo(playSeconds, 6)
      expect(r.config.time.totalWall).toBeCloseTo(playSeconds + 2 * STAY_S, 6)
    }
    const single = replay({ kind: 'passage', passages: [passage('a', T0)] })
    expect(single.plan.stays).toEqual([])
    expect(single.plan.passageLabels).toEqual([])
    expect(single.config.time.totalWall).toBeCloseTo(30, 6)
  })

  it('gives each passage of a cruise at least the minimum share of the length', () => {
    // 100 s against two of 5000 s: by its sailing alone the short one would get a tenth of a second.
    const r = replay({ playSeconds: 15, passages: [passage('a', T0, { seconds: 5000 }), passage('b', T0 + DAY, { seconds: 100 }), passage('c', T0 + 2 * DAY, { seconds: 5000 })] })
    const { timeline, time } = { timeline: r.plan.timeline, time: r.config.time }
    const short = timeline.entries[1]
    const shortWall = time.sailingWallAt(short.offset + (short.end - short.start)) - time.sailingWallAt(short.offset)
    expect(shortWall).toBeCloseTo(MIN_PASSAGE_SHARE_S, 3)
    expect(time.sailingWallAt(time.total)).toBeCloseTo(15, 6)
  })

  it('holds the replay for a photo moment, only where the card says the photo grows', () => {
    const signs = [photoSign('on', T0 + 600), photoSign('off', T0 + 1200, { photo_moment: false }), sign('note', T0 + 1800)]
    const r = replay({
      kind: 'passage', passages: [passage('a', T0)], signs,
      photos: new Map([['on', URI], ['off', URI]]),
    })
    expect(r.config.time.holds.filter((h) => h.kind === 'moment')).toHaveLength(1)
    expect(r.config.time.totalWall).toBeCloseTo(30 + MOMENT_TOTAL_S, 6)
    expect(r.config.signs.map((s) => s.sign.title)).toEqual(['Sign on', 'Sign off', 'Sign note'])
  })

  it('gives each sign its one photo, by its place in the card', () => {
    const data = prepareExport(input({
      kind: 'passage', passages: [passage('a', T0)],
      signs: [sign('note', T0 + 100), photoSign('p', T0 + 600)], photos: new Map([['p', URI]]),
    }))
    const signs = buildReplay(data).config.signs
    expect(signs.map((s) => s.sign.id)).toEqual([signId(0), signId(1)])
    expect(signs[1].sign.photo_url).toBe(URI)
  })

  it('cuts a join into legs and runs the colour across them, as the replay does', () => {
    const r = replay({ kind: 'passage', passages: [passage('a', T0)], joins: [{ t_ms: (T0 + 1800) * 1000, starts_leg: true }] })
    expect(r.plan.pieces.map((p) => p.colourIndex)).toEqual([0, 1])
    expect(r.plan.pieces.map((p) => p.passageId)).toEqual(['0', '0'])
    const three = replay()
    expect(three.plan.pieces.map((p) => p.colourIndex)).toEqual([0, 1, 2])
  })

  it('frames each passage on its own in a cruise, and steers by the heading with shots to follow', () => {
    const r = replay({ shots: [{ id: 's', t_ms: (T0 + 600) * 1000, mode: 'follow', zoom: 14, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1 }] })
    expect(r.plan.passageBounds.map((p) => p.id)).toEqual(['0', '1', '2'])
    expect(r.plan.wantHeading).toBe(true)
    expect(r.plan.resolved).toHaveLength(1)
    expect(replay().plan.wantHeading).toBe(false)
  })

  describe('with camera shots', () => {
    const shot = (id: string, atS: number, over: Partial<CameraShot> = {}): CameraShot => ({
      id, t_ms: atS * 1000, mode: 'follow', zoom: 14, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...over,
    })
    const one = (shots: CameraShot[], playSeconds: 15 | 30 | 60 = 30) => replay({ kind: 'passage', passages: [passage('a', T0)], shots, playSeconds })

    it('is the length chosen without shots', () => {
      const r = one([])
      expect(r.config.time.totalWall).toBeCloseTo(30, 9)
      expect(r.config.time.at(15).clock).toBeCloseTo(1800, 6)
    })

    it('stretches the chosen length by a shot\'s pace for as long as it lasts', () => {
      // 3600 s in 30 s is 120 timeline seconds per watched second. Half pace from
      // the start doubles the first half; quarter pace from the middle takes four.
      const r = one([shot('a', T0, { pace: 0.5 }), shot('b', T0 + 1800, { pace: 0.25 })])
      expect(r.config.time.totalWall).toBeCloseTo(30 + 15 + 3 * 15, 6)
      expect(r.config.time.at(30).clock).toBeCloseTo(1800, 6)
      expect(r.config.time.at(60).clock).toBeCloseTo(2700, 6)
    })

    it('lays the shots on the replay clock, and stretches each play length the same way', () => {
      expect(one([shot('a', T0 + 1200)]).plan.resolved[0].clock).toBeCloseTo(1200, 9)
      for (const play of [15, 30, 60] as const) expect(one([shot('a', T0, { pace: 0.5 })], play).config.time.totalWall).toBeCloseTo(play * 2, 6)
    })

    it('keeps stays out of the paces', () => {
      const r = replay({ passages: [passage('a', T0), passage('b', T0 + 2 * DAY)], shots: [shot('a', T0, { pace: 0.5 })] })
      expect(r.config.time.sailingWallAt(r.config.time.total)).toBeCloseTo(60, 6)
      expect(r.config.time.totalWall).toBeCloseTo(60 + STAY_S, 6)
    })
  })

  it('gives no moment to a sign whose photo the card may not show, as the app and the video do for a photo that will not load', () => {
    const data = prepareExport(input({
      kind: 'passage', passages: [passage('a', T0)], signs: [photoSign('p', T0 + 600), photoSign('q', T0 + 1200)],
      photos: new Map([['p', URI], ['q', URI]]),
    }))
    expect(buildReplay(data).config.time.holds.filter((h) => h.kind === 'moment')).toHaveLength(2)
    data.signs[0].ph = 'https://evil.example/x.jpg'
    const r = buildReplay(data)
    expect(r.config.time.holds.filter((h) => h.kind === 'moment').map((h) => h.id)).toEqual([signId(1)])
    expect(r.config.time.totalWall).toBeCloseTo(30 + MOMENT_TOTAL_S, 6)
  })
})
