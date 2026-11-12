import { describe, expect, it } from 'vitest'

import { readExifGps } from '@/lib/exif-gps'
import { dms, gpsJpeg } from './helpers/exif-gps-jpeg'
import { exifJpeg } from './helpers/exif-jpeg'

const read = (b: Uint8Array) => readExifGps(b.buffer as ArrayBuffer)

describe('readExifGps', () => {
  it('reads a position in the southern and eastern hemispheres', () => {
    const got = read(gpsJpeg({ latRef: 'S', lat: dms(20, 15, 0), lonRef: 'E', lon: dms(149, 30, 36) }))
    expect(got.lat).toBeCloseTo(-20.25, 6)
    expect(got.lon).toBeCloseTo(149.51, 6)
    expect(got.bearingDeg).toBeNull()
  })

  it('reads northern and western positions and big-endian files', () => {
    const got = read(gpsJpeg({ latRef: 'N', lat: dms(48, 30, 0), lonRef: 'W', lon: dms(123, 0, 0), bigEndian: true }))
    expect(got.lat).toBeCloseTo(48.5, 6)
    expect(got.lon).toBeCloseTo(-123, 6)
  })

  it('reads the direction the phone faced', () => {
    const got = read(gpsJpeg({ dirRef: 'T', dir: [27050, 100] }))
    expect(got.bearingDeg).toBeCloseTo(270.5, 6)
    expect(got.lat).toBeNull()
    expect(read(gpsJpeg({ dirRef: 'M', dir: [90, 1] })).bearingDeg).toBe(90)
  })

  it('keeps which north the direction is measured from', () => {
    expect(read(gpsJpeg({ dirRef: 'T', dir: [90, 1] })).bearingRef).toBe('T')
    expect(read(gpsJpeg({ dirRef: 'M', dir: [90, 1] })).bearingRef).toBe('M')
    // No direction, no reference to report.
    expect(read(gpsJpeg({ latRef: 'S', lat: dms(20, 0, 0), lonRef: 'E', lon: dms(149, 0, 0) })).bearingRef).toBeNull()
  })

  it('a bearing of due north is real when the block says which north', () => {
    expect(read(gpsJpeg({ dirRef: 'T', dir: [0, 1] })).bearingDeg).toBe(0)
  })

  it('a zeroed block, the phone hiding its location, is missing and never due north', () => {
    const zeroed = gpsJpeg({ latRef: '', lat: dms(0, 0, 0), lonRef: '', lon: dms(0, 0, 0), dirRef: '', dir: [0, 1] })
    expect(read(zeroed)).toEqual({ lat: null, lon: null, bearingDeg: null, bearingRef: null })
  })

  it('zero over zero is missing, not an error', () => {
    const got = read(gpsJpeg({ latRef: 'N', lat: [[0, 0], [0, 0], [0, 0]], lonRef: 'E', lon: dms(1, 0, 0), dirRef: 'T', dir: [0, 0] }))
    expect(got.lat).toBeNull()
    expect(got.bearingDeg).toBeNull()
  })

  it('a position at 0, 0 is no position', () => {
    expect(read(gpsJpeg({ latRef: 'N', lat: dms(0, 0, 0), lonRef: 'E', lon: dms(0, 0, 0) }))).toEqual({ lat: null, lon: null, bearingDeg: null, bearingRef: null })
  })

  it('a latitude without a longitude is not half used', () => {
    const got = read(gpsJpeg({ latRef: 'N', lat: dms(10, 0, 0) }))
    expect(got.lat).toBeNull()
    expect(got.lon).toBeNull()
  })

  it('refuses a position outside the globe and a bearing outside the compass', () => {
    expect(read(gpsJpeg({ latRef: 'N', lat: dms(95, 0, 0), lonRef: 'E', lon: dms(10, 0, 0) })).lat).toBeNull()
    expect(read(gpsJpeg({ dirRef: 'T', dir: [360, 1] })).bearingDeg).toBeNull()
  })

  it('is empty when the photo has no GPS block, no Exif or is not a JPEG', () => {
    const none = { lat: null, lon: null, bearingDeg: null, bearingRef: null }
    expect(read(gpsJpeg({ noGpsIfd: true }))).toEqual(none)
    expect(read(exifJpeg({ dt: '2026:10:22 11:30:15' }))).toEqual(none)
    expect(read(new Uint8Array([0x89, 0x50, 0x4e, 0x47, 0, 0, 0, 0]))).toEqual(none)
  })

  it('says so when the block is damaged', () => {
    const ok = gpsJpeg({ latRef: 'S', lat: dms(20, 0, 0), lonRef: 'E', lon: dms(149, 0, 0) })
    expect(() => read(ok.slice(0, 40))).toThrow(/damaged/)
  })
})
