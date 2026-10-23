import { describe, expect, it } from 'vitest'

import { exifJpeg } from './helpers/exif-jpeg'
import { captureUnix, readExifCapture } from '@/lib/exif-time'

describe('readExifCapture', () => {
  it('reads DateTimeOriginal as the wall-clock time on the camera', () => {
    expect(readExifCapture(exifJpeg({ dt: '2026:10:22 11:30:15' }).buffer as ArrayBuffer)).toEqual({ year: 2026, month: 10, day: 22, hour: 11, minute: 30, second: 15, offsetMinutes: null })
  })

  it('reads OffsetTimeOriginal when present', () => {
    expect(readExifCapture(exifJpeg({ dt: '2026:10:22 11:30:15', offset: '+10:00' }).buffer as ArrayBuffer)?.offsetMinutes).toBe(600)
    expect(readExifCapture(exifJpeg({ dt: '2026:10:22 11:30:15', offset: '-03:30' }).buffer as ArrayBuffer)?.offsetMinutes).toBe(-210)
  })

  it('reads big-endian files', () => {
    expect(readExifCapture(exifJpeg({ dt: '2026:01:02 03:04:05', bigEndian: true }).buffer as ArrayBuffer)?.hour).toBe(3)
  })

  it('is null when the photo carries no capture time', () => {
    expect(readExifCapture(exifJpeg({}).buffer as ArrayBuffer)).toBeNull()
    expect(readExifCapture(exifJpeg({ dt: '    :  :     :  :  ' }).buffer as ArrayBuffer)).toBeNull()
    expect(readExifCapture(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0, 0, 0, 0]).buffer as ArrayBuffer)).toBeNull()
    expect(readExifCapture(new Uint8Array([0xff, 0xd8, 0xff, 0xd9]).buffer as ArrayBuffer)).toBeNull()
  })

  it('says so when the EXIF block is damaged rather than guessing', () => {
    expect(() => readExifCapture(exifJpeg({ dt: '2026:10:22 11:30:15', truncate: true }).buffer as ArrayBuffer)).toThrow(/damaged/)
    expect(() => readExifCapture(exifJpeg({ dt: '2026:10:22 11:30:15', skipExifPointer: true }).buffer as ArrayBuffer)).toThrow(/damaged/)
    expect(() => readExifCapture(exifJpeg({ dt: '2026:13:45 25:61:61' }).buffer as ArrayBuffer)).toThrow(/damaged/)
  })
})

describe('readExifCapture: damaged and impossible input', () => {
  const ok = exifJpeg({ dt: '2026:10:22 11:30:15', offset: '+10:00' })
  const read = (b: Uint8Array) => readExifCapture(b.buffer as ArrayBuffer)

  it('a file cut anywhere gives the plain damaged error or null, never a RangeError', () => {
    for (let cut = 0; cut < ok.length; cut++) {
      try {
        read(ok.slice(0, cut))
      } catch (err) {
        expect(err).not.toBeInstanceOf(RangeError)
        expect((err as Error).message).toMatch(/damaged/)
      }
    }
    // Cut inside the APP1 header specifically.
    for (const cut of [4, 5, 6, 7, 8, 9, 10]) expect(() => read(ok.slice(0, cut))).toThrow(/damaged/)
  })

  it('reads +HH:MM, +HHMM, and values padded with spaces or NULs', () => {
    expect(read(exifJpeg({ dt: '2026:10:22 11:30:15', offset: '+0530' }))?.offsetMinutes).toBe(330)
    expect(read(exifJpeg({ dt: '2026:10:22 11:30:15', offset: '+10:00  ' }))?.offsetMinutes).toBe(600)
    expect(read(exifJpeg({ dt: '2026:10:22 11:30:15', offset: '-09:30\0\0' }))?.offsetMinutes).toBe(-570)
  })

  it('an offset that is there but unreadable is damaged, not boat time', () => {
    for (const offset of ['garbage', '10:00', '+25:00', '+10:75', '+1']) {
      expect(() => read(exifJpeg({ dt: '2026:10:22 11:30:15', offset }))).toThrow(/damaged/)
    }
  })

  it('rejects dates that do not exist and accepts ones that do', () => {
    for (const dt of ['2026:02:30 10:00:00', '2026:04:31 10:00:00', '2026:02:29 10:00:00', '2026:10:22 24:00:00', '2026:10:22 10:60:00', '2026:10:22 10:00:60', '2026:00:10 10:00:00', '2026:10:00 10:00:00']) {
      expect(() => read(exifJpeg({ dt })), dt).toThrow(/damaged/)
    }
    expect(read(exifJpeg({ dt: '2024:02:29 10:00:00' }))?.day).toBe(29)
    expect(read(exifJpeg({ dt: '2026:12:31 23:59:59' }))?.second).toBe(59)
  })
})

describe('captureUnix', () => {
  const cap = { year: 2026, month: 10, day: 22, hour: 11, minute: 30, second: 0 }
  it('uses the photo offset when it has one', () => {
    expect(captureUnix({ ...cap, offsetMinutes: 600 }, 0)).toEqual({ unix: Date.UTC(2026, 9, 22, 1, 30) / 1000, zone: 'UTC+10:00', fromPhoto: true })
    expect(captureUnix({ ...cap, offsetMinutes: -210 }, 10).zone).toBe('UTC-03:30')
  })
  it('otherwise reads it in the boat zone', () => {
    expect(captureUnix({ ...cap, offsetMinutes: null }, 10)).toEqual({ unix: Date.UTC(2026, 9, 22, 1, 30) / 1000, zone: 'UTC+10', fromPhoto: false })
    expect(captureUnix({ ...cap, offsetMinutes: null }, 0).zone).toBe('UTC')
  })
})
