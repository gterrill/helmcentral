import { describe, expect, it } from 'vitest'

import { PassageVideoError } from '@/lib/passage-video/errors'
import { frameSchedule } from '@/lib/passage-video/schedule'
import { prepareVideoReplay, type PassageVideoInput } from '@/lib/passage-video/setup'
import type { PassageDetail } from '@/lib/passages'
import type { ReplaySign } from '@/lib/replay-signs'
import { MOMENT_TOTAL_S } from '@/lib/replay-time'

const t0 = Date.UTC(2026, 9, 24, 0, 0) / 1000
const passage = (id = 'p1', start = t0, track = true): PassageDetail => ({
  id, start: new Date(start * 1000).toISOString(), end: new Date((start + 1000) * 1000).toISOString(),
  from_name: '', to_name: '', title: '', notes: '', hidden: false, edited: false, cruise_id: null, created_at: '',
  stats: { distance_nm: 0, underway_seconds: 0, mean_sog_kts: null, max_sog_kts: null, fuel_burned_l: null, tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null },
  timezone: 'Australia/Brisbane',
  track: track ? [[[-20, 149], [-20, 149.1]]] : [],
  track_times: track ? [[start, start + 1000]] : [],
  replay_track: track ? [[[-20, 149], [-20, 149.05], [-20, 149.1]]] : [],
  replay_times: track ? [[start, start + 500, start + 1000]] : [],
} as unknown as PassageDetail)

const sign = (id: string, at: number, extra: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: (t0 + at) * 1000, title: id, note: '', document_id: 'd', photo_url: '/p', visibility: 'passing', show_s: 4, starts_leg: false, photo_moment: null, ...extra,
})

const input = (extra: Partial<PassageVideoInput> = {}): PassageVideoInput => ({
  orientation: 'landscape', passages: [passage()], shots: [], signs: [], hull: null, threeD: true, satellite: false, isDarkTheme: false, lengthS: 15,
  summary: { title: 'T', dates: 'D', stats: [], start: t0, zone: { o: 10, l: 'UTC+10' } }, ...extra,
})

describe('prepareVideoReplay', () => {
  it('lays the passages end to end and frames them', () => {
    const s = prepareVideoReplay(input({ passages: [passage('a'), passage('b', t0 + 5000)] }))
    expect(s.config.timeline.total).toBe(2000)
    expect(s.bounds[0]).toBeCloseTo(149)
    expect(s.config.pieces.map((p) => p.key)).toEqual(['a', 'b'])
  })

  it('times the replay by the length the replay is set to', () => {
    const s = prepareVideoReplay(input({ lengthS: 30 }))
    expect(s.config.time.totalWall).toBeCloseTo(30, 6)
    expect(frameSchedule(s.config.time).sailing).toBe(30 * 30 + 1)
  })

  it('adds a hold for a sign with a photo, and none for one without or switched off', () => {
    const s = prepareVideoReplay(input({ signs: [sign('a', 300), sign('b', 400, { photo_moment: false }), sign('c', 500, { photo_url: null, document_id: null })] }))
    expect(s.config.time.moments.map((m) => m.id)).toEqual(['a'])
    expect(s.config.time.totalWall).toBeCloseTo(15 + MOMENT_TOTAL_S)
  })

  it('leaves out the moment of a sign whose photo would not load', () => {
    const s = prepareVideoReplay(input({ signs: [sign('a', 300)] }), new Set(['a']))
    expect(s.config.time.moments).toEqual([])
    // The sign is still there, to say its photo is not available.
    expect(s.config.signs.map((r) => r.sign.id)).toEqual(['a'])
  })

  it('cuts a join into legs, which the speed colours go on', () => {
    const s = prepareVideoReplay(input({ signs: [sign('join', 500, { starts_leg: true, photo_url: null, document_id: null })] }))
    expect(s.config.pieces).toHaveLength(2)
  })

  it('plays the shots inside the passages, in time order', () => {
    const shot = (id: string, at: number) => ({ id, t_ms: (t0 + at) * 1000, mode: 'follow' as const, zoom: 14, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1 })
    const s = prepareVideoReplay(input({ shots: [shot('late', 800), shot('early', 100), shot('outside', 5000)] }))
    expect(s.config.resolved.map((r) => r.shot.id)).toEqual(['early', 'late'])
    expect(s.wantHeading).toBe(true)
  })

  it('works the heading out only with a model or shots', () => {
    expect(prepareVideoReplay(input()).wantHeading).toBe(false)
    expect(prepareVideoReplay(input({ hull: { type: 'sail_mono', loaM: 12, beamM: 4 } })).wantHeading).toBe(true)
    expect(prepareVideoReplay(input({ hull: { type: 'sail_mono', loaM: 12, beamM: 4 }, threeD: false })).wantHeading).toBe(false)
  })

  it('stops, saying so, when there is no track', () => {
    const err = (() => { try { prepareVideoReplay(input({ passages: [passage('a', t0, false)] })) } catch (e) { return e } })()
    expect(err).toBeInstanceOf(PassageVideoError)
    expect((err as PassageVideoError).message).toContain('No track')
  })
})
