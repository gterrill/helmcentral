import { createHash } from 'node:crypto'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { describe, expect, it } from 'vitest'

import {
  dateRangeLabel,
  combinedTitle,
  escapeHtml,
  exportFileName,
  generateEmbed,
  generateStandalonePage,
  nightsBetween,
  playerScript,
  POINT_BUDGET,
  prepareExport,
  safeJson,
  simplifyTracks,
  type ExportInput,
} from '@/lib/passage-export'
import {
  MAPLIBRE_CSS_SRI,
  MAPLIBRE_JS_SRI,
  MAPLIBRE_VERSION,
} from '@/lib/passage-export-style'
import type { CruiseStats, PassageDetail, PassageStats, TimedSegment } from '@/lib/passages'

const STATS: PassageStats = {
  distance_nm: 42.3,
  underway_seconds: 7 * 3600 + 5 * 60,
  mean_sog_kts: 6,
  max_sog_kts: 8,
  fuel_burned_l: 120,
  tws_mean_kts: 12,
  max_gust_kts: 24.4,
  soc_start_pct: null,
  soc_end_pct: null,
  soc_min_pct: null,
}

/** A straight run of `n` fixes, 60 s apart, from (lat, lon) heading east. */
function run(t0: number, n: number, lat = -20.3, lon = 148.9, step = 60): [number, number, number][] {
  return Array.from({ length: n }, (_, i) => [t0 + i * step, lat + Math.sin(i / 7) * 0.01, lon + i * 0.001])
}

function passage(over: Partial<PassageDetail> & { id: string; t0: number; points?: number }): PassageDetail {
  const pts = run(over.t0, over.points ?? 50)
  const base: PassageDetail = {
    id: over.id,
    start: new Date(over.t0 * 1000).toISOString(),
    end: new Date(pts[pts.length - 1][0] * 1000).toISOString(),
    from_name: 'Hamilton Island',
    to_name: 'Airlie Beach',
    title: '',
    notes: '',
    hidden: false,
    edited: false,
    cruise_id: null,
    created_at: '2026-09-14T00:00:00Z',
    stats: STATS,
    start_lon: 148.9,
    track: [pts.map(([, lon, lat]) => [lat, lon] as [number, number])],
    track_times: [pts.map(([t]) => t)],
    replay_track: [pts.map(([, lon, lat]) => [lat, lon] as [number, number])],
    replay_times: [pts.map(([t]) => t)],
  }
  return { ...base, ...over }
}

const T0 = Date.UTC(2026, 8, 14, 0, 0, 0) / 1000 // 14 Sep 2026 00:00Z, 10:00 at UTC+10
const ZONE = { offsetHours: 10, label: 'UTC+10' }

function input(over: Partial<ExportInput> = {}): ExportInput {
  return {
    kind: 'passage',
    title: 'Hamilton to Airlie',
    passages: [passage({ id: 'p1', t0: T0 })],
    units: 'metric',
    zone: ZONE,
    inset: true,
    playSeconds: 30,
    cardId: 'hcp-test',
    ...over,
  }
}

describe('safeJson', () => {
  it('escapes everything that could close or break a script element', () => {
    const out = safeJson({ t: '</script><img src=x onerror=alert(1)> & <!-- \u2028\u2029' })
    expect(out).not.toMatch(/[<>&\u2028\u2029]/)
    expect(JSON.parse(out).t).toBe('</script><img src=x onerror=alert(1)> & <!-- \u2028\u2029')
  })
})

describe('escapeHtml', () => {
  it('escapes markup characters and quotes', () => {
    expect(escapeHtml(`<a href="x">'&'</a>`)).toBe('&lt;a href=&quot;x&quot;&gt;&#39;&amp;&#39;&lt;/a&gt;')
  })
})

describe('simplifyTracks', () => {
  it('keeps every point when under budget', () => {
    const seg: TimedSegment = run(0, 20).map(([t, lon, lat]) => ({ t, lon, lat }))
    expect(simplifyTracks([[seg]], 1500)[0][0]).toHaveLength(20)
  })

  it('fits the budget across passages and keeps segment ends', () => {
    const mk = (t0: number): TimedSegment => run(t0, 4000, -20, 148, 10).map(([t, lon, lat]) => ({ t, lon, lat: lat + Math.sin(lon * 400) * 0.002 }))
    const a = mk(0)
    const b = mk(1_000_000)
    const out = simplifyTracks([[a], [b]], 1500)
    const total = out.flat().reduce((n, s) => n + s.length, 0)
    expect(total).toBeLessThanOrEqual(1500)
    expect(total).toBeGreaterThan(100)
    expect(out[0][0][0]).toEqual(a[0])
    expect(out[0][0].at(-1)).toEqual(a.at(-1))
    expect(out[1][0].at(-1)).toEqual(b.at(-1))
  })

  it('keeps both points around a long pause so the marker does not glide through it', () => {
    const seg: TimedSegment = [
      { t: 0, lat: 0, lon: 0 }, { t: 60, lat: 0, lon: 0.001 }, { t: 120, lat: 0, lon: 0.002 },
      { t: 3000, lat: 0, lon: 0.002 }, { t: 3060, lat: 0, lon: 0.003 }, { t: 3120, lat: 0, lon: 0.004 },
    ]
    const times = simplifyTracks([[seg]], 3)[0][0].map((p) => p.t)
    expect(times).toContain(120)
    expect(times).toContain(3000)
  })
})

describe('nights and dates', () => {
  it('counts boat-local midnights between passages', () => {
    // Arrive 15 Sep 16:00 UTC+10 (06:00Z), leave 16 Sep 07:00 UTC+10 (21:00Z on 15th)
    expect(nightsBetween(Date.UTC(2026, 8, 15, 6) / 1000, Date.UTC(2026, 8, 15, 21) / 1000, 10)).toBe(1)
    // Same local day: no night.
    expect(nightsBetween(Date.UTC(2026, 8, 15, 0) / 1000, Date.UTC(2026, 8, 15, 5) / 1000, 10)).toBe(0)
    // Three local midnights.
    expect(nightsBetween(Date.UTC(2026, 8, 14, 6) / 1000, Date.UTC(2026, 8, 17, 21) / 1000, 10)).toBe(4)
  })

  it('writes a date range in the boat zone', () => {
    const d = (day: number, h = 0) => Date.UTC(2026, 8, day, h) / 1000
    expect(dateRangeLabel(d(14), d(14, 5), 10)).toBe('14 Sep 2026')
    expect(dateRangeLabel(d(14), d(16), 10)).toBe('14 to 16 Sep 2026')
    expect(dateRangeLabel(Date.UTC(2026, 8, 28) / 1000, Date.UTC(2026, 9, 2) / 1000, 10)).toBe('28 Sep to 2 Oct 2026')
    expect(dateRangeLabel(Date.UTC(2026, 11, 30) / 1000, Date.UTC(2027, 0, 2) / 1000, 10)).toBe('30 Dec 2026 to 2 Jan 2027')
    // 20:00Z on the 14th is already the 15th at UTC+10.
    expect(dateRangeLabel(d(14, 20), d(14, 21), 10)).toBe('15 Sep 2026')
  })

  it('titles a hand-picked set from its ends, or by count', () => {
    const a = passage({ id: 'a', t0: T0, from_name: 'Mackay', to_name: 'Scawfell' })
    const b = passage({ id: 'b', t0: T0 + 86400, from_name: 'Scawfell', to_name: 'Hamilton' })
    expect(combinedTitle([b, a])).toBe('Mackay to Hamilton')
    expect(combinedTitle([passage({ id: 'c', t0: T0, from_name: '' })])).toBe('1 passages')
  })
})

describe('prepareExport', () => {
  it('fails fast when there is no track', () => {
    const empty = passage({ id: 'e', t0: T0, track: [], track_times: [], replay_track: [], replay_times: [] })
    expect(() => prepareExport(input({ passages: [empty] }))).toThrow(/no track/)
  })

  it('numbers the nights across the selection and names them for the arrival', () => {
    const a = passage({ id: 'a', t0: T0, to_name: 'Scawfell' })
    const b = passage({ id: 'b', t0: T0 + 3 * 86400, to_name: 'Hamilton' })
    const data = prepareExport(input({ kind: 'combined', passages: [b, a] }))
    expect(data.passages.map((p) => p.nb)).toEqual([0, 3])
    expect(data.passages[1].n0).toBe(1)
    expect(data.passages[1].pl).toBe('Scawfell')
    expect(data.stats.find((s) => s.l === 'Nights')?.v).toBe('3')
  })

  it('gives each passage its own colour and its heading when untitled', () => {
    const a = passage({ id: 'a', t0: T0, title: 'Leg one' })
    const b = passage({ id: 'b', t0: T0 + 86400 })
    const data = prepareExport(input({ kind: 'combined', passages: [a, b] }))
    expect(data.passages[0].c).not.toBe(data.passages[1].c)
    expect(data.passages.map((p) => p.n)).toEqual(['Leg one', 'Hamilton Island → Airlie Beach'])
  })

  it('stays under the point budget for a long track', () => {
    const long = passage({ id: 'l', t0: T0, points: 6000 })
    const data = prepareExport(input({ passages: [long] }))
    expect(data.passages[0].s.flat().length).toBeLessThanOrEqual(POINT_BUDGET)
  })
})

describe('stats strip', () => {
  it('shows distance, underway, fuel and gust for one passage, in the operator units', () => {
    const metric = prepareExport(input()).stats
    expect(metric.map((s) => s.l)).toEqual(['Distance', 'Underway', 'Fuel', 'Max gust'])
    expect(metric.find((s) => s.l === 'Fuel')?.v).toBe('120 L')
    expect(metric.find((s) => s.l === 'Distance')?.v).toBe('42.3 NM')
    expect(metric.find((s) => s.l === 'Underway')?.v).toBe('7 h 05 m')
    expect(metric.find((s) => s.l === 'Max gust')?.v).toBe('24.4 kn')
    const imperial = prepareExport(input({ units: 'imperial' })).stats
    expect(imperial.find((s) => s.l === 'Fuel')?.v).toBe('32 gal')
  })

  it('leaves out figures the log did not record', () => {
    const p = passage({ id: 'p', t0: T0, stats: { ...STATS, fuel_burned_l: null, max_gust_kts: null } })
    const strip = prepareExport(input({ passages: [p] })).stats
    expect(strip.map((s) => s.l)).toEqual(['Distance', 'Underway'])
    expect(strip.some((s) => s.v === '--')).toBe(false)
  })

  it('omits fuel for a combined selection when any passage lacks it, rather than undercount', () => {
    const a = passage({ id: 'a', t0: T0 })
    const b = passage({ id: 'b', t0: T0 + 86400, stats: { ...STATS, fuel_burned_l: null } })
    const strip = prepareExport(input({ kind: 'combined', passages: [a, b] })).stats
    expect(strip.some((s) => s.l === 'Fuel')).toBe(false)
    expect(strip.find((s) => s.l === 'Distance')?.v).toBe('84.6 NM')
  })

  it('uses the cruise figures, nights included, for a cruise', () => {
    const cruiseStats = {
      passage_count: 2, distance_nm: 100, underway_seconds: 36000, nights: 5, fuel_burned_l: 300,
      underway_max_gust_kts: 20, anchor_max_gust_kts: 33,
    } as CruiseStats
    const strip = prepareExport(input({ kind: 'cruise', cruiseStats })).stats
    expect(strip.map((s) => [s.l, s.v])).toEqual([
      ['Distance', '100.0 NM'], ['Underway', '10 h 00 m'], ['Nights', '5'], ['Fuel', '300 L'], ['Max gust', '33.0 kn'],
    ])
  })
})

describe('generateEmbed', () => {
  it('loads MapLibre 5.24.0 from jsDelivr with integrity hashes', () => {
    const html = generateEmbed(input())
    expect(html).toContain('https://cdn.jsdelivr.net/npm/maplibre-gl@5.24.0/dist/maplibre-gl.js')
    expect(html).toContain('https://cdn.jsdelivr.net/npm/maplibre-gl@5.24.0/dist/maplibre-gl.css')
    expect(html).toContain(`integrity="${MAPLIBRE_JS_SRI}"`)
    expect(html).toContain(`integrity="${MAPLIBRE_CSS_SRI}"`)
    expect(html).toContain('crossorigin="anonymous"')
  })

  it('uses the Voyager style with OpenSeaMap seamarks', () => {
    const html = generateEmbed(input())
    expect(html).toContain('https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json')
    expect(html).toContain('https://tiles.openseamap.org/seamark/{z}/{x}/{y}.png')
    expect(html).toContain('OpenSeaMap contributors')
  })

  it('never points back at the boat', () => {
    const html = generateEmbed(input({ kind: 'combined', passages: [passage({ id: 'a', t0: T0 }), passage({ id: 'b', t0: T0 + 86400 })] }))
    expect(html).not.toMatch(/\/api\//)
    expect(html).not.toMatch(/localhost|127\.0\.0\.1|100\.\d+\.\d+\.\d+|192\.168\.|\.ts\.net|\.local\b|apiBaseUrl/)
    const hosts = new Set([...html.matchAll(/https?:\/\/([^/"'\s\\{]+)/g)].map((m) => m[1]))
    expect([...hosts].sort()).toEqual(['basemaps.cartocdn.com', 'cdn.jsdelivr.net', 'tiles.openseamap.org'])
  })

  it('cannot be broken out of by a malicious title', () => {
    const evil = '</script><img src=x onerror=alert(1)>'
    const p = passage({ id: 'p', t0: T0, title: evil, to_name: evil, from_name: evil })
    const html = generateEmbed(input({ kind: 'combined', title: evil, passages: [p, passage({ id: 'q', t0: T0 + 3 * 86400 })] }))
    expect(html).not.toContain('<img')
    expect(html).not.toContain(evil)
    // Exactly the elements the generator wrote: link, three scripts' worth.
    expect(html.match(/<\/script>/g)).toHaveLength(3)
    expect(html.match(/<script/g)).toHaveLength(3)
    const json = html.match(/<script type="application\/json" id="hcp-test-data">([\s\S]*?)<\/script>/)?.[1] ?? ''
    expect(JSON.parse(json).title).toBe(evil)
  })

  it('sets visible text with textContent only', () => {
    expect(playerScript('x')).not.toMatch(/innerHTML|outerHTML|insertAdjacentHTML|document\.write/)
  })

  it('scopes its styles to the card id and gives each card its own', () => {
    const a = generateEmbed(input({ cardId: 'hcp-one' }))
    const b = generateEmbed(input({ cardId: 'hcp-two' }))
    expect(a).toContain('id="hcp-one"')
    expect(a).not.toContain('hcp-two')
    expect(b).toContain('id="hcp-two"')
    const css = a.match(/<style>([\s\S]*?)<\/style>/)?.[1] ?? ''
    expect(css.length).toBeGreaterThan(1000)
    const selectors = css.split('}').map((r) => r.split('{')[0].trim()).filter((s) => s && !s.startsWith('@'))
    for (const sel of selectors) {
      for (const one of sel.split(',')) expect(one.trim()).toMatch(/^#hcp-one/)
    }
    expect(css).not.toContain('HCP_ID')
  })

  it('generates a random id when none is given and rejects an unsafe one', () => {
    const { cardId: _unused, ...rest } = input()
    void _unused
    const html = generateEmbed(rest)
    expect(html).toMatch(/id="hcp-[a-z0-9]{10}"/)
    expect(() => generateEmbed(input({ cardId: 'x" onload="y' }))).toThrow(/Invalid card id/)
  })

  it('carries the play length chosen at export', () => {
    for (const playSeconds of [15, 30, 60] as const) {
      expect(prepareExport(input({ playSeconds })).play).toBe(playSeconds)
    }
  })

  it('does not start playing by itself', () => {
    const html = generateEmbed(input())
    expect(html).toContain('class="hcp hcp-idle"')
    expect(html).toContain('<button type="button" class="hcp-btn">')
  })
})

describe('generateStandalonePage', () => {
  it('wraps the snippet in a full page with an escaped title', () => {
    const page = generateStandalonePage(input({ title: '<b>Trip & back</b>' }))
    expect(page.startsWith('<!doctype html>')).toBe(true)
    expect(page).toContain('<title>&lt;b&gt;Trip &amp; back&lt;/b&gt;</title>')
    expect(page).toContain('id="hcp-test"')
    expect(page).toContain('name="viewport"')
  })
})

describe('exportFileName', () => {
  it('builds a plain name from the title and the boat-local first date', () => {
    expect(exportFileName('Hamilton → Airlie Beach', T0 + 20 * 3600, 10, 'png')).toBe('hamilton-airlie-beach-2026-09-15.png')
    expect(exportFileName('Café “Déjà vu”!', T0, 10, 'html')).toBe('cafe-deja-vu-2026-09-14.html')
    expect(exportFileName('  ///  ', T0, 10, 'png')).toBe('passage-2026-09-14.png')
  })
})

describe('the pinned MapLibre files', () => {
  const dist = join(__dirname, '../../node_modules/maplibre-gl/dist')
  const sri = (file: string) => `sha384-${createHash('sha384').update(readFileSync(join(dist, file))).digest('base64')}`

  it('match the installed 5.24.0 build, so the integrity hashes cannot go stale', () => {
    const pkg = JSON.parse(readFileSync(join(dist, '../package.json'), 'utf8')) as { version: string }
    expect(pkg.version).toBe(MAPLIBRE_VERSION)
    expect(sri('maplibre-gl.js')).toBe(MAPLIBRE_JS_SRI)
    expect(sri('maplibre-gl.css')).toBe(MAPLIBRE_CSS_SRI)
  })
})
