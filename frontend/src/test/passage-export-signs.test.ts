import { describe, expect, it, vi } from 'vitest'

import { estimateEmbed, generateEmbed, prepareExport, safeJson, type ExportInput } from '@/lib/passage-export'
import {
  CARD_LIMIT_BYTES,
  ESTIMATED_PHOTO_BYTES,
  EXPORT_PHOTO_PREFIX,
  cleanJpeg,
  exportSigns,
  fitWithin,
  isExportPhoto,
  jpegMetadataMarkers,
  signsInRange,
} from '@/lib/passage-export-signs'
import { prepareSignPhotos } from '@/lib/passage-export-photos'
import type { PassageDetail, PassageStats } from '@/lib/passages'
import type { ReplaySign } from '@/lib/replay-signs'

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

const sign = (id: string, atS: number, over: Partial<ReplaySign> = {}): ReplaySign => ({
  id, t_ms: atS * 1000, title: `Sign ${id}`, note: '', document_id: null, photo_url: null, visibility: 'passing', show_s: 4, ...over,
})
const withPhoto = (id: string, atS: number, over: Partial<ReplaySign> = {}) =>
  sign(id, atS, { document_id: `doc-${id}`, photo_url: `/api/documents/doc-${id}/content`, ...over })

const URI = `${EXPORT_PHOTO_PREFIX}QUJD`

function input(over: Partial<ExportInput> = {}): ExportInput {
  return {
    kind: 'passage', title: 'Out', zone: { offsetHours: 10, label: 'UTC+10' }, units: 'metric', inset: false,
    playSeconds: 30, cardId: 'hcp-c', passages: [passage('1', T0)], ...over,
  }
}

describe('what the card carries for signs', () => {
  it('has no signs unless it is given some', () => {
    expect(prepareExport(input()).signs).toEqual([])
  })

  it('carries the signs inside an exported passage, oldest first, without ids', () => {
    const d = prepareExport(input({
      signs: [sign('b', T0 + 2000, { title: 'Reef', note: 'Anchor here', visibility: 'after', show_s: 6 }), sign('out', T0 - 5), sign('a', T0 + 100, { visibility: 'always' }), sign('late', T0 + 9000)],
    }))
    expect(d.signs).toEqual([
      { t: (T0 + 100) * 1000, ti: 'Sign a', no: '', v: 'always', s: 4 },
      { t: (T0 + 2000) * 1000, ti: 'Reef', no: 'Anchor here', v: 'after', s: 6 },
    ])
    expect(JSON.stringify(d.signs)).not.toMatch(/id|document|doc-|photo_url/i)
  })

  it('puts a prepared photo on its sign', () => {
    const d = prepareExport(input({ signs: [withPhoto('a', T0 + 100)], photos: new Map([['a', URI]]) }))
    expect(d.signs[0].ph).toBe(URI)
  })

  it('sends a sign whose photo document is gone as a note, as the app shows it', () => {
    const gone = sign('a', T0 + 100, { document_id: 'doc-a', photo_url: null })
    const d = prepareExport(input({ signs: [gone] }))
    expect(d.signs[0].ph).toBeUndefined()
  })

  it('refuses a sign whose photo was not prepared, rather than send it as a note', () => {
    expect(() => prepareExport(input({ signs: [withPhoto('a', T0 + 100, { title: 'Whale' })] }))).toThrow(/"Whale".*not prepared/)
  })

  it('refuses a photo that is not a JPEG data URI the export made', () => {
    for (const bad of ['https://example.com/a.jpg', 'data:image/svg+xml;base64,QUJD', `${EXPORT_PHOTO_PREFIX}QU"onerror=`, 'javascript:alert(1)']) {
      expect(() => prepareExport(input({ signs: [withPhoto('a', T0 + 100)], photos: new Map([['a', bad]]) }))).toThrow(/photo/)
    }
  })

  it('counts the signs and photos in the exported range', () => {
    const passages = [{ a: T0, b: T0 + 3600 }]
    const found = signsInRange([sign('a', T0 + 1), withPhoto('b', T0 + 2), sign('c', T0 + 99999)], passages)
    expect(found.map((s) => s.id)).toEqual(['a', 'b'])
    expect(exportSigns([], passages)).toEqual([])
  })

  it('knows which strings may be an image source', () => {
    expect(isExportPhoto(URI)).toBe(true)
    expect(isExportPhoto('data:image/png;base64,QUJD')).toBe(false)
    expect(isExportPhoto(`${EXPORT_PHOTO_PREFIX}QU JD`)).toBe(false)
    expect(isExportPhoto(42)).toBe(false)
  })
})

describe('notes that try to break out of the page', () => {
  const hostile = ['</script><img src=x onerror=alert(1)>', '<!-- not a comment', 'a\u2028b\u2029c & <b>']
  it('stay inside the data script', () => {
    for (const note of hostile) {
      const html = generateEmbed(input({ signs: [sign('a', T0 + 100, { title: note, note })] }))
      const json = html.match(/<script type="application\/json"[^>]*>([\s\S]*?)<\/script>/) as RegExpMatchArray
      expect(json[1]).not.toMatch(/[<>&\u2028\u2029]/)
      expect(JSON.parse(json[1]).signs[0].no).toBe(note)
      expect(html.split('</script>').length).toBe(4)
      expect(html).not.toContain('<!-- not a comment')
    }
  })

  it('escape a data URI the same way as any other string', () => {
    expect(safeJson({ ph: URI })).toContain(URI)
  })
})

describe('the size of the card', () => {
  it('stops the export when the card is over the limit and names the largest photos', () => {
    const big = `${EXPORT_PHOTO_PREFIX}${'A'.repeat(1_500_000)}`
    const small = `${EXPORT_PHOTO_PREFIX}${'A'.repeat(300_000)}`
    const signs = [withPhoto('a', T0 + 100, { title: 'Whale' }), withPhoto('b', T0 + 200, { title: 'Reef' }), withPhoto('c', T0 + 300, { title: 'Quay' }), withPhoto('d', T0 + 400, { title: 'Tiny' })]
    const photos = new Map([['a', big], ['b', big], ['c', small], ['d', URI]])
    let message = ''
    try { generateEmbed(input({ signs, photos })) } catch (err) { message = (err as Error).message }
    expect(message).toMatch(/over the 3 MB limit/)
    expect(message).toMatch(/Whale.*1\.4 MB/)
    expect(message.indexOf('Whale')).toBeLessThan(message.indexOf('Quay'))
    expect(message).not.toContain('Tiny')
    expect(message).toMatch(/Include signs/)
  })

  it('lets a card under the limit through', () => {
    const html = generateEmbed(input({ signs: [withPhoto('a', T0 + 100)], photos: new Map([['a', URI]]) }))
    expect(new TextEncoder().encode(html).length).toBeLessThan(CARD_LIMIT_BYTES)
    expect(html).toContain(URI)
  })
})

describe('estimating the card before the photos are ready', () => {
  const wait = (n: number, over: Partial<ReplaySign> = {}) => Array.from({ length: n }, (_, i) => withPhoto(`p${i}`, T0 + 100 + i, { title: `Photo ${i}`, ...over }))

  it('is exact when there is nothing to wait for', () => {
    const e = estimateEmbed(input({ signs: [sign('a', T0 + 100)] }))
    expect(e.exact).toBe(true)
    expect(e.bytes).toBe(new TextEncoder().encode(generateEmbed(input({ signs: [sign('a', T0 + 100)] }))).length)
  })

  it('counts a conservative figure for each photo not yet prepared', () => {
    const none = estimateEmbed(input()).bytes
    const e = estimateEmbed(input({ signs: wait(3) }))
    expect(e.exact).toBe(false)
    expect(e.bytes - none).toBeGreaterThanOrEqual(3 * ESTIMATED_PHOTO_BYTES)
    expect(ESTIMATED_PHOTO_BYTES).toBeGreaterThanOrEqual(300_000)
  })

  it('counts the titles and notes of the signs that have photos', () => {
    const short = estimateEmbed(input({ signs: wait(2) })).bytes
    const long = estimateEmbed(input({ signs: wait(2, { note: 'x'.repeat(280) }) })).bytes
    expect(long - short).toBeGreaterThanOrEqual(2 * 280)
  })

  it('uses a prepared photo\'s real size and guesses only the rest', () => {
    const signs = wait(2)
    const real = `${EXPORT_PHOTO_PREFIX}${'A'.repeat(1000)}`
    const e = estimateEmbed(input({ signs, photos: new Map([['p0', real]]) }))
    const allGuess = estimateEmbed(input({ signs })).bytes
    expect(e.exact).toBe(false)
    expect(allGuess - e.bytes).toBeGreaterThan(ESTIMATED_PHOTO_BYTES - 2000)
    expect(estimateEmbed(input({ signs, photos: new Map([['p0', real], ['p1', real]]) })).exact).toBe(true)
  })
})

// ── the photos ─────────────────────────────────────────────────────────────

const SCAN = [0x12, 0x34, 0xff, 0x00, 0x56]
const jpeg = (...segments: number[][]) => Uint8Array.from([0xff, 0xd8, ...segments.flat(), 0xff, 0xda, 0x00, 0x02, ...SCAN, 0xff, 0xd9])
const ascii = (body: string) => [...body].map((c) => c.charCodeAt(0))
const app = (marker: number, body: string) => [0xff, marker, 0x00, body.length + 2, ...ascii(body)]
const DQT = [0xff, 0xdb, 0x00, 0x04, 0x00, 0x08]
const SOF = [0xff, 0xc0, 0x00, 0x05, 0x08, 0x00, 0x10]
const asText = (b: Uint8Array) => String.fromCharCode(...b)

describe('metadata in a JPEG', () => {
  it('finds the Exif and the other places a position can hide', () => {
    expect(jpegMetadataMarkers(jpeg(app(0xe0, 'JFIF\0')))).toEqual([])
    expect(jpegMetadataMarkers(jpeg(app(0xe0, 'JFIF\0'), app(0xe1, 'Exif\0\0GPS')))).toEqual(['APP1'])
    expect(jpegMetadataMarkers(jpeg(app(0xed, 'Photoshop'), app(0xfe, 'hello')))).toEqual(['APP13', 'COM'])
    expect(jpegMetadataMarkers(jpeg(app(0xe2, 'ICC_PROFILE\0')))).toEqual([])
  })

  it('does not read past the picture data', () => {
    expect(jpegMetadataMarkers(Uint8Array.from([0xff, 0xd8, 0xff, 0xda, 0x00, 0x02, 0xff, 0xe1, 0x00, 0x04, 0x45, 0x78]))).toEqual([])
  })

  it('refuses bytes that are not a JPEG', () => {
    expect(() => jpegMetadataMarkers(Uint8Array.from([1, 2, 3]))).toThrow(/not a JPEG/)
  })

  it('refuses a file that ends before the picture data', () => {
    const cut = Uint8Array.from([0xff, 0xd8, ...app(0xe0, 'JFIF\0'), ...DQT])
    expect(() => jpegMetadataMarkers(cut)).toThrow(/cut short/)
    expect(() => cleanJpeg(cut)).toThrow(/cut short/)
    expect(() => cleanJpeg(Uint8Array.from([0xff, 0xd8, 0xff, 0xe1, 0x01, 0x00, 0x45]))).toThrow(/cut short/)
  })
})

describe('cleaning a JPEG', () => {
  const dirty = jpeg(
    app(0xe1, 'Exif\0\0II*\0GPSLatitude 20.5 S'),
    app(0xe0, 'JFIF\0'),
    app(0xe1, 'http://ns.adobe.com/xap/1.0/\0<x:xmpmeta>'),
    app(0xe2, 'ICC_PROFILE\0'),
    app(0xed, 'Photoshop 3.0\0'),
    app(0xfe, 'taken with a phone'),
    DQT,
    SOF,
  )

  it('drops Exif, XMP, IPTC and comments and keeps what the picture needs', () => {
    const clean = cleanJpeg(dirty)
    const t = asText(clean)
    expect(t).not.toContain('Exif')
    expect(t).not.toContain('GPS')
    expect(t).not.toContain('http://ns.adobe.com/xap')
    expect(t).not.toContain('Photoshop')
    expect(t).not.toContain('phone')
    expect(t).toContain('JFIF')
    expect(t).toContain('ICC_PROFILE')
    expect(jpegMetadataMarkers(clean)).toEqual([])
    expect([...clean.slice(0, 2)]).toEqual([0xff, 0xd8])
    expect([...clean.slice(-2)]).toEqual([0xff, 0xd9])
    expect([...clean].join(',')).toContain([...DQT, ...SOF].join(','))
  })

  it('copies the picture data untouched, and nothing after the end marker', () => {
    const clean = cleanJpeg(Uint8Array.from([...dirty, ...ascii('GPS trailer')]))
    expect([...clean.slice(-(SCAN.length + 2))]).toEqual([...SCAN, 0xff, 0xd9])
    expect(asText(clean)).not.toContain('trailer')
  })

  it('leaves a clean file as it was', () => {
    const plain = jpeg(app(0xe0, 'JFIF\0'), DQT, SOF)
    expect([...cleanJpeg(plain)]).toEqual([...plain])
  })

  it('drops a JFIF extension and an APP2 that is not a colour profile', () => {
    const t = asText(cleanJpeg(jpeg(app(0xe0, 'JFXX\0thumbnail'), app(0xe2, 'MPF\0stuff'), DQT)))
    expect(t).not.toContain('thumbnail')
    expect(t).not.toContain('MPF')
  })
})

describe('fitting a photo', () => {
  it('shrinks the long side to 960 and keeps the shape', () => {
    expect(fitWithin(4000, 3000, 960)).toEqual({ width: 960, height: 720 })
    expect(fitWithin(3000, 4000, 960)).toEqual({ width: 720, height: 960 })
  })
  it('never makes a photo bigger', () => {
    expect(fitWithin(800, 600, 960)).toEqual({ width: 800, height: 600 })
  })
})

describe('preparing the photos', () => {
  const okFetch = () => vi.fn(async () => new Response(new Uint8Array([1, 2, 3]))) as unknown as typeof fetch
  const clean = jpeg(app(0xe0, 'JFIF\0'), DQT, SOF)
  const out = (bytes: Uint8Array, type = 'image/jpeg') => async () => new Blob([bytes as BlobPart], { type })

  it('fetches each photo from the document, downscales it and returns a JPEG data URI', async () => {
    const downscale = vi.fn(out(clean))
    const fetchFn = okFetch()
    const result = await prepareSignPhotos([withPhoto('a', 1), sign('b', 2)], { downscale, fetchFn, cache: new Map() })
    expect(fetchFn).toHaveBeenCalledTimes(1)
    expect(String((fetchFn as unknown as ReturnType<typeof vi.fn>).mock.calls[0][0])).toContain('/api/documents/doc-a/content')
    expect(downscale).toHaveBeenCalledTimes(1)
    expect([...result.keys()]).toEqual(['a'])
    expect(atob((result.get('a') as string).slice(EXPORT_PHOTO_PREFIX.length))).toBe(String.fromCharCode(...clean))
  })

  it('writes the photo without any Exif the encoder left in', async () => {
    const leaky = jpeg(app(0xe1, 'Exif\0\0GPSLatitude'), app(0xe0, 'JFIF\0'), DQT, SOF)
    const result = await prepareSignPhotos([withPhoto('a', 1)], { downscale: out(leaky), fetchFn: okFetch(), cache: new Map() })
    const bytes = atob((result.get('a') as string).slice(EXPORT_PHOTO_PREFIX.length))
    expect(bytes).not.toContain('Exif')
    expect(bytes).not.toContain('GPS')
  })

  it('does not fetch a photo twice', async () => {
    const cache = new Map<string, string>()
    const fetchFn = okFetch()
    await prepareSignPhotos([withPhoto('a', 1)], { downscale: out(clean), fetchFn, cache })
    await prepareSignPhotos([withPhoto('a', 1)], { downscale: out(clean), fetchFn, cache })
    expect(fetchFn).toHaveBeenCalledTimes(1)
  })

  it('reports progress', async () => {
    const seen: [number, number][] = []
    await prepareSignPhotos([withPhoto('a', 1), withPhoto('b', 2)], { downscale: out(clean), fetchFn: okFetch(), cache: new Map(), onProgress: (d, t) => seen.push([d, t]) })
    expect(seen).toEqual([[0, 2], [1, 2], [2, 2]])
  })

  it('fails and names the sign when a photo cannot be fetched', async () => {
    const fetchFn = vi.fn(async () => new Response('no', { status: 404 })) as unknown as typeof fetch
    await expect(prepareSignPhotos([withPhoto('a', 1, { title: 'Whale' })], { downscale: out(clean), fetchFn, cache: new Map() })).rejects.toThrow(/"Whale".*404/)
  })

  it('fails and names the sign when a photo cannot be read as a picture', async () => {
    const bad = async () => { throw new Error('bad image') }
    await expect(prepareSignPhotos([withPhoto('a', 1, { title: 'Whale' })], { downscale: bad, fetchFn: okFetch(), cache: new Map() })).rejects.toThrow(/"Whale".*bad image/)
  })

  it('fails and names the sign when the encoded file is cut short', async () => {
    const cut = Uint8Array.from([0xff, 0xd8, ...app(0xe0, 'JFIF\0')])
    await expect(prepareSignPhotos([withPhoto('a', 1, { title: 'Whale' })], { downscale: out(cut), fetchFn: okFetch(), cache: new Map() })).rejects.toThrow(/"Whale".*cut short/)
  })

  it('refuses output that is not a JPEG', async () => {
    await expect(prepareSignPhotos([withPhoto('a', 1, { title: 'Whale' })], { downscale: out(clean, 'image/png'), fetchFn: okFetch(), cache: new Map() })).rejects.toThrow(/"Whale".*JPEG/)
  })
})
