import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { buildPlan } from '@/export-player/plan'
import { cspHint, warnCsp } from '@/export-player/csp'
import { terrainLoader, TERRAIN_PROTOCOL, tileOfUrl, upstreamUrl, type PixelIO, type Pixels } from '@/export-player/terrain-protocol'
import { loadThree } from '@/export-player/three-loader'
import { generateEmbed, PLAYER_SOURCE, prepareExport, type ExportInput } from '@/lib/passage-export'
import { THREE_SRI, THREE_URL, THREE_VERSION } from '@/lib/passage-export-style'
import type { PassageDetail, PassageStats } from '@/lib/passages'
import type { CameraShot } from '@/lib/replay-clock'

const STATS: PassageStats = {
  distance_nm: 10, underway_seconds: 3600, mean_sog_kts: 5, max_sog_kts: 6, fuel_burned_l: null,
  tws_mean_kts: null, max_gust_kts: null, soc_start_pct: null, soc_end_pct: null, soc_min_pct: null,
}
const T0 = Date.UTC(2026, 8, 14, 0) / 1000

function passage(id: string, t0: number): PassageDetail {
  const times = Array.from({ length: 11 }, (_, i) => t0 + i * 360)
  const pts = times.map((_, i) => [-20 + i * 0.01, 148 + i * 0.01] as [number, number])
  return {
    id, start: new Date(t0 * 1000).toISOString(), end: new Date((t0 + 3600) * 1000).toISOString(),
    from_name: 'A', to_name: 'B', title: `Leg ${id}`, notes: '', hidden: false, edited: false,
    cruise_id: null, created_at: '', stats: STATS, start_lon: 148,
    track: [pts], track_times: [times], replay_track: [pts], replay_times: [times],
  }
}

const shot = (id: string, atS: number, over: Partial<CameraShot> = {}): CameraShot => ({
  id, t_ms: atS * 1000, mode: 'follow', zoom: 15, pitch: 60, bearing_offset_deg: 0, orbit_deg_per_s: 0, pace: 1, ...over,
})

function input(over: Partial<ExportInput> = {}): ExportInput {
  return {
    kind: 'passage', title: 'Out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: false,
    playSeconds: 30, cardId: 'hcp-c', passages: [passage('1', T0)], ...over,
  }
}

describe('what the card carries for the cinematic replay', () => {
  it('is the flat card with no shots when 3D is off, whatever the vessel has', () => {
    const d = prepareExport(input({ hull: { type: 'sail_mono', loaM: 12, beamM: 4 } }))
    expect(d.d3).toBe(false)
    expect(d.hull).toBeNull()
    expect(d.shots).toEqual([])
    expect(d.map.three).toBeNull()
  })

  it('carries the hull type and size and the pinned three.js with 3D on', () => {
    const d = prepareExport(input({ threeD: true, hull: { type: 'sail_cat', loaM: 14, beamM: 7 } }))
    expect(d.d3).toBe(true)
    expect(d.hull).toEqual({ type: 'sail_cat', loa: 14, beam: 7 })
    expect(d.map.three).toEqual({ url: THREE_URL, sri: THREE_SRI })
  })

  it('keeps a missing length as null so the boat is drawn at a fixed size', () => {
    const d = prepareExport(input({ threeD: true, hull: { type: 'power_mono', loaM: null, beamM: null } }))
    expect(d.hull).toEqual({ type: 'power_mono', loa: null, beam: null })
  })

  it('has no boat model to fetch without a hull type', () => {
    const d = prepareExport(input({ threeD: true, hull: null }))
    expect(d.d3).toBe(true)
    expect(d.hull).toBeNull()
    expect(d.map.three).toBeNull()
  })

  it('carries only the shots inside an exported passage, oldest first, without ids', () => {
    const d = prepareExport(input({
      shots: [shot('c', T0 + 2000, { mode: 'orbit', orbit_deg_per_s: 6, pace: 0.5 }), shot('before', T0 - 10), shot('a', T0 + 100), shot('after', T0 + 5000)],
    }))
    expect(d.shots).toEqual([
      { t: (T0 + 100) * 1000, m: 'follow', z: 15, p: 60, b: 0, o: 0, k: 1 },
      { t: (T0 + 2000) * 1000, m: 'orbit', z: 15, p: 60, b: 0, o: 6, k: 0.5 },
    ])
  })

  it('writes the data into the snippet and still points nowhere near the boat', () => {
    const html = generateEmbed(input({ threeD: true, hull: { type: 'sail_mono', loaM: 12, beamM: 4 }, shots: [shot('a', T0 + 100)] }))
    expect(html).toContain(THREE_URL)
    expect(html).toContain('integrity')
    expect(html).not.toMatch(/\/api\//)
    expect(html).not.toMatch(/localhost|127\.0\.0\.1|192\.168\./)
    const hosts = new Set([...html.matchAll(/https?:\/\/([^/"'\s\\{]+)/g)].map((m) => m[1]))
    expect([...hosts].sort()).toEqual(['basemaps.cartocdn.com', 'cdn.jsdelivr.net', 's3.amazonaws.com', 'tiles.openseamap.org'])
  })

  it('reuses the prepared data only for the same shots and hull', () => {
    const passages = [passage('1', T0)]
    const shots = [shot('a', T0 + 100)]
    expect(prepareExport(input({ passages, shots }))).toBe(prepareExport(input({ passages, shots })))
    expect(prepareExport(input({ passages, shots }))).not.toBe(prepareExport(input({ passages, shots: [shot('a', T0 + 200)] })))
  })
})

describe('the built player', () => {
  it('carries no React, no react-map-gl, no MapLibre import and no address of the boat', () => {
    expect(PLAYER_SOURCE).not.toMatch(/react|maplibre-gl|\/api\/|localhost/i)
    expect(PLAYER_SOURCE).not.toMatch(/innerHTML|outerHTML|insertAdjacentHTML|document\.write/)
    expect(PLAYER_SOURCE).not.toContain('__boatDebug')
    // Only the global the card is handed, never imported or referenced bare.
    // (Class names such as maplibregl-ctrl-attrib are the library's own markup.)
    expect(PLAYER_SOURCE).not.toMatch(/(?<![\w.-])maplibregl(?![\w-])/)
  })

  it('defines one entry and runs nothing by itself', () => {
    expect(PLAYER_SOURCE.startsWith('var HcPlayer=')).toBe(true)
    expect(new Function(`${PLAYER_SOURCE}\nreturn typeof HcPlayer.initCard`)()).toBe('function')
  })
})

describe('the plan with shots', () => {
  const run = (shots: CameraShot[], play: 15 | 30 | 60 = 30) => buildPlan(prepareExport(input({ shots, playSeconds: play })))

  it('is today\'s plan without shots', () => {
    const plan = run([])
    expect(plan.total).toBeCloseTo(30, 9)
    expect(plan.stateAt(15).t).toBeCloseTo(T0 + 1800, 6)
  })

  it('stretches the chosen length by a shot\'s pace for as long as it lasts', () => {
    // 3600 s in 30 s is 120 timeline seconds per watched second. Half pace from
    // the start doubles the first half; quarter pace from the middle takes four.
    const plan = run([shot('a', T0, { pace: 0.5 }), shot('b', T0 + 1800, { pace: 0.25 })])
    expect(plan.total).toBeCloseTo(30 + 15 + 3 * 15, 6)
    // After 30 watched seconds at half pace the boat is 1800 s in.
    expect(plan.stateAt(30).t).toBeCloseTo(T0 + 1800, 6)
    // 30 s later, at quarter pace, 30 * 30 = 900 s further.
    expect(plan.stateAt(60).t).toBeCloseTo(T0 + 2700, 6)
    expect(plan.stateAt(plan.total).done).toBe(true)
  })

  it('gives the camera the replay clock the shots were laid on', () => {
    const plan = run([shot('a', T0 + 1200)])
    expect(plan.shots).toHaveLength(1)
    expect(plan.shots[0].clock).toBeCloseTo(1200, 9)
    expect(plan.stateAt(10).clock).toBeCloseTo(1200, 6)
  })

  it('stretches each play length the same way', () => {
    for (const play of [15, 30, 60] as const) {
      expect(run([shot('a', T0, { pace: 0.5 })], play).total).toBeCloseTo(play * 2, 6)
    }
  })

  it('keeps nights out of the paces', () => {
    const plan = buildPlan(prepareExport(input({
      kind: 'combined', passages: [passage('1', T0), passage('2', T0 + 2 * 86400)], shots: [shot('a', T0, { pace: 0.5 })],
    })))
    const nights = plan.steps.filter((s) => s.kind === 'night')
    expect(nights.reduce((n, s) => n + s.dur, 0)).toBeCloseTo(3, 9)
    expect(plan.steps.filter((s) => s.kind === 'run').reduce((n, s) => n + s.dur, 0)).toBeCloseTo(60, 6)
  })
})

describe('terrain tiles in the reader\'s browser', () => {
  const pixels = (...px: number[]): Pixels => ({ width: 2, height: 1, data: new Uint8ClampedArray(px) })
  const io = (decoded: Pixels): PixelIO & { encoded: Pixels[] } => {
    const encoded: Pixels[] = []
    return { encoded, decode: async () => decoded, encode: async (p) => { encoded.push(p); return { close() {} } as unknown as ImageBitmap } }
  }

  it('reads a tile address and asks AWS for it directly', () => {
    expect(tileOfUrl('hcterrain-v1://11/1815/1186')).toEqual({ z: 11, x: 1815, y: 1186 })
    expect(upstreamUrl({ z: 11, x: 1815, y: 1186 })).toBe('https://s3.amazonaws.com/elevation-tiles-prod/terrarium/11/1815/1186.png')
    expect(() => tileOfUrl('https://example.com/1/2/3')).toThrow(/Not a terrain tile/)
  })

  it('flattens the sea in the tile before MapLibre sees it', async () => {
    const decoded = pixels(100, 9, 9, 255, 130, 4, 5, 255)
    const fake = io(decoded)
    const fetchFn = vi.fn(async () => new Response(new Uint8Array([1, 2, 3]))) as unknown as typeof fetch
    await terrainLoader(fake, fetchFn)({ url: 'hcterrain-v1://3/4/5' }, new AbortController())
    expect(fetchFn).toHaveBeenCalledWith('https://s3.amazonaws.com/elevation-tiles-prod/terrarium/3/4/5.png', expect.objectContaining({ signal: expect.anything() }))
    expect([...fake.encoded[0].data]).toEqual([128, 0, 0, 255, 130, 4, 5, 255])
  })

  it('fails the tile when the server does, and puts nothing in its place', async () => {
    const fake = io(pixels(128, 0, 0, 255, 128, 0, 0, 255))
    const fetchFn = vi.fn(async () => new Response('no', { status: 503 })) as unknown as typeof fetch
    await expect(terrainLoader(fake, fetchFn)({ url: 'hcterrain-v1://3/4/5' }, new AbortController())).rejects.toThrow(/answered 503/)
    expect(fake.encoded).toHaveLength(0)
  })
})

describe('three.js for the boat model', () => {
  beforeEach(() => { delete (globalThis as { __hcpThree?: unknown }).__hcpThree })

  it('is fetched with its integrity hash and only then run', async () => {
    const fetchFn = vi.fn(async () => new Response('export const REVISION = "170"')) as unknown as typeof fetch
    const evaluate = vi.fn(async (text: string) => ({ text }) as never)
    await loadThree({ url: THREE_URL, sri: THREE_SRI }, { fetchFn, evaluate })
    expect(fetchFn).toHaveBeenCalledWith(THREE_URL, expect.objectContaining({ integrity: THREE_SRI }))
    expect(evaluate).toHaveBeenCalledWith('export const REVISION = "170"')
  })

  it('does not run anything the CDN refused', async () => {
    const evaluate = vi.fn()
    const fetchFn = vi.fn(async () => new Response('x', { status: 404 })) as unknown as typeof fetch
    await expect(loadThree({ url: THREE_URL, sri: THREE_SRI }, { fetchFn, evaluate })).rejects.toThrow(/404/)
    expect(evaluate).not.toHaveBeenCalled()
  })

  it('passes on the failure of the integrity check itself', async () => {
    const fetchFn = vi.fn(async () => { throw new TypeError('Failed to find a valid digest in the \'integrity\' attribute') }) as unknown as typeof fetch
    await expect(loadThree({ url: THREE_URL, sri: THREE_SRI }, { fetchFn, evaluate: vi.fn() })).rejects.toThrow(/valid digest/)
  })
})

describe('three.js shared by cards on one page', () => {
  beforeEach(() => { delete (globalThis as { __hcpThree?: unknown }).__hcpThree })
  const src = { url: THREE_URL, sri: THREE_SRI }

  it('is fetched and run once however many cards ask', async () => {
    const fetchFn = vi.fn(async () => new Response('export {}')) as unknown as typeof fetch
    const ns = { REVISION: '170' }
    const evaluate = vi.fn(async () => ns as never)
    const [a, b] = await Promise.all([loadThree(src, { fetchFn, evaluate }), loadThree(src, { fetchFn, evaluate })])
    const c = await loadThree(src, { fetchFn, evaluate })
    expect(fetchFn).toHaveBeenCalledTimes(1)
    expect(evaluate).toHaveBeenCalledTimes(1)
    expect([a, b, c]).toEqual([ns, ns, ns])
  })

  it('is loaded again for a different hash, and again after a failure', async () => {
    const evaluate = vi.fn(async () => ({}) as never)
    const bad = vi.fn(async () => { throw new TypeError('Failed to fetch') }) as unknown as typeof fetch
    const ok = vi.fn(async () => new Response('x')) as unknown as typeof fetch
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    await expect(loadThree(src, { fetchFn: bad, evaluate })).rejects.toThrow()
    await loadThree(src, { fetchFn: ok, evaluate })
    await loadThree({ ...src, sri: 'sha384-other' }, { fetchFn: ok, evaluate })
    expect(ok).toHaveBeenCalledTimes(2)
  })
})

describe('a content security policy in the way', () => {
  beforeEach(() => { delete (globalThis as { __hcpCspWarned?: unknown }).__hcpCspWarned; vi.restoreAllMocks() })

  it('names the directive for a security error or a failed fetch, and not for anything else', () => {
    const se = new DOMException('blocked', 'SecurityError')
    expect(cspHint(se, 'script-src blob:')).toContain('script-src blob:')
    expect(cspHint(new TypeError('Failed to fetch'), 'connect-src s3.amazonaws.com')).toContain('connect-src s3.amazonaws.com')
    expect(cspHint(new Error('the elevation server answered 503'), 'connect-src x')).toBeNull()
  })

  it('says it once per page and directive, however many tiles fail', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    for (let i = 0; i < 5; i++) warnCsp(new TypeError('Failed to fetch'), 'connect-src s3.amazonaws.com')
    warnCsp(new TypeError('Failed to fetch'), 'connect-src cdn.jsdelivr.net')
    expect(warn).toHaveBeenCalledTimes(2)
  })

  it('has the terrain loader say so for a failed tile fetch', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fetchFn = vi.fn(async () => { throw new TypeError('Failed to fetch') }) as unknown as typeof fetch
    await expect(terrainLoader(undefined, fetchFn)({ url: `${TERRAIN_PROTOCOL}://3/4/5` }, new AbortController())).rejects.toThrow()
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('connect-src s3.amazonaws.com'))
  })
})

describe('the terrain protocol name', () => {
  it('carries a version, so cards from different releases do not clobber each other', () => {
    expect(TERRAIN_PROTOCOL).toMatch(/^hcterrain-v\d+$/)
  })
})

describe('the pinned three.js file', () => {
  const dist = join(__dirname, '../../node_modules/three')
  it('matches the installed build, so the integrity hash cannot go stale', () => {
    const pkg = JSON.parse(readFileSync(join(dist, 'package.json'), 'utf8')) as { version: string }
    expect(pkg.version).toBe(THREE_VERSION)
    expect(THREE_URL).toBe(`https://cdn.jsdelivr.net/npm/three@${THREE_VERSION}/build/three.module.min.js`)
    const sri = `sha384-${createHash('sha384').update(readFileSync(join(dist, 'build/three.module.min.js'))).digest('base64')}`
    expect(sri).toBe(THREE_SRI)
  })
})
