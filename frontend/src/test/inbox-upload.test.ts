import { describe, expect, it, vi } from 'vitest'

import type { InboxUploadMeta, InboxUploadResult } from '@/lib/inbox-api'
import { INBOX_MAX_DIMENSION, prepareFile, summariseTally, uploadFiles, type PreparedFile } from '@/lib/inbox-upload'
import { dms, gpsJpeg } from './helpers/exif-gps-jpeg'

const fileOf = (bytes: Uint8Array, name: string, type = 'image/jpeg') => new File([bytes as BlobPart], name, { type })

const fakeFingerprint = async () => ({ fingerprint: '00ff00ff00ff00ff', sharpness: 42 })
const downscaled = new Blob(['small'], { type: 'image/jpeg' })
const SHA = 'ab'.repeat(32)
const fakeSha = async () => SHA

describe('prepareFile', () => {
  it('reads the time, position and bearing from the original, then downscales to 2560 px', async () => {
    const order: string[] = []
    const jpg = gpsJpeg({
      dt: '2026:10:22 11:30:15', offset: '+10:00',
      latRef: 'S', lat: dms(20, 15, 0), lonRef: 'E', lon: dms(149, 30, 0), dirRef: 'T', dir: [90, 1],
    })
    const file = fileOf(jpg, 'IMG_0001.HEIC.jpg')
    const downscale = vi.fn(async (blob: Blob, max: number) => {
      order.push(`downscale:${max}`)
      expect(blob).toBe(file)
      return downscaled
    })
    // The score is worked out from the shrunk copy, not a second decode of the original.
    const fingerprint = vi.fn(async (blob: Blob) => {
      order.push('fingerprint')
      expect(blob).toBe(downscaled)
      return fakeFingerprint()
    })
    const sha256 = vi.fn(async (blob: Blob) => {
      order.push('sha256')
      expect(blob).toBe(file)
      return fakeSha()
    })
    const got = await prepareFile(file, { boatTimeZone: 'Australia/Brisbane', downscale, fingerprint, sha256 })
    expect(order).toEqual(['sha256', 'downscale:2560', 'fingerprint'])
    expect(INBOX_MAX_DIMENSION).toBe(2560)
    expect(got.blob).toBe(downscaled)
    expect(got.filename).toBe('IMG_0001.HEIC.jpg')
    expect(got.warnings).toEqual([])
    expect(got.meta).toEqual<InboxUploadMeta>({
      captureMs: Date.UTC(2026, 9, 22, 1, 30, 15),
      captureOffsetMin: 600,
      bearingDeg: 90,
      phoneLat: -20.25,
      phoneLon: 149.5,
      fingerprint: '00ff00ff00ff00ff',
      sharpness: 42,
      sourceSha256: SHA,
    })
  })

  it('hashes the original, so the same photo is recognised however it was shrunk', async () => {
    const got = await prepareFile(fileOf(gpsJpeg({}), 'a.jpg'), { downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.meta.sourceSha256).toBe(SHA)
  })

  it('hashes without Web Crypto, as on a plain http page', async () => {
    const bytes = gpsJpeg({})
    vi.stubGlobal('crypto', undefined)
    try {
      const got = await prepareFile(fileOf(bytes, 'a.jpg'), { downscale: async () => downscaled, fingerprint: fakeFingerprint })
      expect(got.meta.sourceSha256).toBe((await import('node:crypto')).createHash('sha256').update(bytes).digest('hex'))
      expect(got.warnings).toEqual([])
    } finally {
      vi.unstubAllGlobals()
    }
  })

  it('works out the boat-zone offset on the right side of a daylight saving change', async () => {
    // Sydney moves its clocks forward at 2 am on 4 Oct 2026, so 1 am that day is still +10.
    const before = await prepareFile(fileOf(gpsJpeg({ dt: '2026:10:04 01:00:00' }), 'a.jpg'), { boatTimeZone: 'Australia/Sydney', downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(before.meta.captureMs).toBe(Date.UTC(2026, 9, 3, 15, 0, 0))
    const after = await prepareFile(fileOf(gpsJpeg({ dt: '2026:10:04 03:30:00' }), 'a.jpg'), { boatTimeZone: 'Australia/Sydney', downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(after.meta.captureMs).toBe(Date.UTC(2026, 9, 3, 16, 30, 0))
  })

  it('treats the zeroed GPS block of a hidden location as missing, never due north', async () => {
    const jpg = gpsJpeg({ dt: '2026:10:22 11:30:15', offset: '+10:00', latRef: '', lat: dms(0, 0, 0), lonRef: '', lon: dms(0, 0, 0), dirRef: '', dir: [0, 1] })
    const got = await prepareFile(fileOf(jpg, 'a.jpg'), { downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.meta.phoneLat).toBeNull()
    expect(got.meta.phoneLon).toBeNull()
    expect(got.meta.bearingDeg).toBeNull()
    expect(got.meta.captureMs).toBe(Date.UTC(2026, 9, 22, 1, 30, 15))
  })

  it('renames a re-encoded photo to .jpg', async () => {
    const got = await prepareFile(fileOf(gpsJpeg({}), 'IMG_7.heic', 'image/heic'), { downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.filename).toBe('IMG_7.jpg')
  })

  it('reads a photo with no zone of its own in the boat zone', async () => {
    const jpg = gpsJpeg({ dt: '2026:10:22 11:30:15' })
    const got = await prepareFile(fileOf(jpg, 'a.jpg'), { boatTimeZone: 'Australia/Brisbane', downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.meta.captureMs).toBe(Date.UTC(2026, 9, 22, 1, 30, 15))
    expect(got.meta.captureOffsetMin).toBeNull()
    expect(got.warnings).toEqual([])
  })

  it('does not guess a zone: no zone on the photo and none for the boat leaves it with no time, and says so', async () => {
    const jpg = gpsJpeg({ dt: '2026:10:22 11:30:15' })
    const got = await prepareFile(fileOf(jpg, 'a.jpg'), { boatTimeZone: undefined, downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.meta.captureMs).toBeNull()
    expect(got.warnings).toHaveLength(1)
    expect(got.warnings[0]).toMatch(/time zone/i)
  })

  it('a photo with no capture time is uploaded without one', async () => {
    const got = await prepareFile(fileOf(gpsJpeg({}), 'a.jpg'), { boatTimeZone: 'Australia/Brisbane', downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.meta.captureMs).toBeNull()
    expect(got.warnings).toEqual([])
  })

  it('an unreadable EXIF block is reported, and the photo still goes with no time', async () => {
    const broken = gpsJpeg({ dt: '2026:10:22 11:30:15', offset: '+10:00' }).slice(0, 40)
    const got = await prepareFile(fileOf(broken, 'a.jpg'), { downscale: async () => downscaled, fingerprint: fakeFingerprint, sha256: fakeSha })
    expect(got.meta.captureMs).toBeNull()
    expect(got.warnings[0]).toMatch(/damaged/i)
  })

  it('sends a PDF as it is, with nothing worked out from it', async () => {
    const pdf = fileOf(new Uint8Array([0x25, 0x50, 0x44, 0x46]), 'receipt.pdf', 'application/pdf')
    const downscale = vi.fn()
    const fingerprint = vi.fn()
    const got = await prepareFile(pdf, { downscale, fingerprint })
    expect(got.blob).toBe(pdf)
    expect(got.filename).toBe('receipt.pdf')
    expect(got.meta).toEqual<InboxUploadMeta>({ captureMs: null, captureOffsetMin: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' })
    expect(downscale).not.toHaveBeenCalled()
    expect(fingerprint).not.toHaveBeenCalled()
  })

  it('refuses a file that is neither a photo nor a PDF', async () => {
    await expect(prepareFile(fileOf(new Uint8Array([1]), 'notes.txt', 'text/plain'), {})).rejects.toThrow(/photo or a PDF/)
  })

  it('lets a failed downscale through as the failure it is', async () => {
    await expect(
      prepareFile(fileOf(gpsJpeg({}), 'a.jpg'), { downscale: async () => { throw new Error('no canvas') }, fingerprint: fakeFingerprint, sha256: fakeSha }),
    ).rejects.toThrow('no canvas')
  })
})

describe('uploadFiles', () => {
  const prepared = (name: string): PreparedFile => ({
    blob: new Blob([name]),
    filename: name,
    meta: { captureMs: name.startsWith('x') ? null : 1000, captureOffsetMin: null, bearingDeg: null, phoneLat: null, phoneLon: null, fingerprint: '', sharpness: 0, sourceSha256: '' },
    warnings: [],
  })
  const files = (...names: string[]) => names.map((n) => new File([n], n, { type: 'image/jpeg' }))

  it('uploads one at a time, in order, counting stored and skipped', async () => {
    let inFlight = 0
    let peak = 0
    const calls: string[] = []
    const upload = async (p: PreparedFile): Promise<InboxUploadResult> => {
      inFlight++
      peak = Math.max(peak, inFlight)
      await Promise.resolve()
      calls.push(p.filename)
      inFlight--
      return p.filename === 'b.jpg' || p.filename === 'd.jpg' ? { status: 'already_uploaded', where: 'inbox' } : { status: 'stored', item: {} as never }
    }
    const progress: number[] = []
    const tally = await uploadFiles(files('a.jpg', 'b.jpg', 'c.jpg', 'd.jpg'), {
      prepare: async (f) => prepared(f.name),
      upload,
      onProgress: (t) => progress.push(t.done),
    })
    expect(calls).toEqual(['a.jpg', 'b.jpg', 'c.jpg', 'd.jpg'])
    expect(peak).toBe(1)
    expect(progress).toEqual([0, 1, 2, 3, 4])
    expect(tally).toMatchObject({ total: 4, done: 4, uploaded: 2, skipped: 2, failed: [] })
    expect(summariseTally(tally)).toBe('2 uploaded, 2 already in Helmcentral')
  })

  it('goes on after a file fails and reports which and why', async () => {
    const tally = await uploadFiles(files('a.jpg', 'bad.jpg', 'c.jpg'), {
      prepare: async (f) => {
        if (f.name === 'bad.jpg') throw new Error('could not decode')
        return prepared(f.name)
      },
      upload: async (p) => {
        if (p.filename === 'c.jpg') throw new Error('HTTP 500')
        return { status: 'stored', item: {} as never }
      },
    })
    expect(tally.uploaded).toBe(1)
    expect(tally.failed).toEqual([{ name: 'bad.jpg', error: 'could not decode' }, { name: 'c.jpg', error: 'HTTP 500' }])
    expect(tally.done).toBe(3)
    expect(summariseTally(tally)).toBe('1 uploaded; 2 failed')
    expect(summariseTally({ uploaded: 6, skipped: 0, failed: [] })).toBe('6 uploaded')
    expect(summariseTally({ uploaded: 0, skipped: 3, failed: [] })).toBe('0 uploaded, 3 already in Helmcentral')
  })

  it('counts stored photos with no capture time and carries the warnings', async () => {
    const tally = await uploadFiles(files('x1.jpg', 'a.jpg'), {
      prepare: async (f) => ({ ...prepared(f.name), warnings: f.name === 'x1.jpg' ? ['Its EXIF block is damaged'] : [] }),
      upload: async () => ({ status: 'stored', item: {} as never }),
    })
    expect(tally.noTime).toBe(1)
    expect(tally.warnings).toEqual([{ name: 'x1.jpg', warning: 'Its EXIF block is damaged' }])
  })

  it('stops starting new files once told to', async () => {
    const stop = new AbortController()
    const calls: string[] = []
    const tally = await uploadFiles(files('a.jpg', 'b.jpg', 'c.jpg'), {
      prepare: async (f) => prepared(f.name),
      upload: async (p) => {
        calls.push(p.filename)
        stop.abort()
        return { status: 'stored', item: {} as never }
      },
      signal: stop.signal,
    })
    expect(calls).toEqual(['a.jpg'])
    expect(tally.done).toBe(1)
    expect(tally.total).toBe(3)
  })
})
