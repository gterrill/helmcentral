import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

import { buildEchoPalette, parseCapabilities, type MayaraLegend } from '@/lib/radar-echo/legend'

// Phase 0 fixture: the real /capabilities response captured from the boat's
// mayara instance (backend/testdata/mayara/README.md). Read fresh each time
// rather than shared, so one test mutating its copy can't leak into another.
const fixtureDir = resolve(dirname(fileURLToPath(import.meta.url)), '../../../backend/testdata/mayara')

function loadCapabilities(): Record<string, unknown> {
  const raw = readFileSync(resolve(fixtureDir, 'capabilities-fur6424A.json'), 'utf-8')
  return JSON.parse(raw) as Record<string, unknown>
}

// Ground truth for the endianness-sensitive pack: write the four channel
// bytes straight into a buffer shaped like ImageData.data (a
// Uint8ClampedArray) and read it back through a Uint32Array view over the
// SAME buffer -- exactly what a canvas 2D context does with the array
// buildEchoPalette hands it. This proves the packed value round-trips to
// the right bytes on this platform rather than re-deriving buildEchoPalette's
// own arithmetic inside the test, which would just duplicate the bug it
// might contain.
function packedBytesRoundTrip(packed: number): [number, number, number, number] {
  const bytes = new Uint8ClampedArray(4)
  new Uint32Array(bytes.buffer)[0] = packed
  return [bytes[0], bytes[1], bytes[2], bytes[3]]
}

describe('parseCapabilities', () => {
  it('parses the real captured capabilities', () => {
    const parsed = parseCapabilities(loadCapabilities())
    expect(parsed).not.toBeNull()
    expect(parsed?.spokesPerRevolution).toBe(8192)
    expect(parsed?.maxSpokeLength).toBe(1024)
    // The legend has 254 entries, not the 252 `pixelValues` implies and not
    // a round number either -- Phase 0's correction, see spokes-fur6424A.md.
    expect(parsed?.legend.pixels.length).toBe(254)
  })

  it('returns null when the legend is missing', () => {
    const raw = loadCapabilities()
    delete raw.legend
    expect(parseCapabilities(raw)).toBeNull()
  })

  it('returns null when spokesPerRevolution is missing', () => {
    const raw = loadCapabilities()
    delete raw.spokesPerRevolution
    expect(parseCapabilities(raw)).toBeNull()
  })

  it('returns null when pixels is not an array', () => {
    const raw = loadCapabilities()
    const legend = raw.legend as Record<string, unknown>
    legend.pixels = 'not-an-array'
    expect(parseCapabilities(raw)).toBeNull()
  })

  it('returns null for a non-object input rather than throwing', () => {
    expect(parseCapabilities(null)).toBeNull()
    expect(parseCapabilities(undefined)).toBeNull()
    expect(parseCapabilities('nope')).toBeNull()
  })
})

describe('buildEchoPalette', () => {
  const capabilities = parseCapabilities(loadCapabilities())
  if (capabilities === null) throw new Error('fixture capabilities failed to parse; test setup is broken')
  const palette = buildEchoPalette(capabilities.legend)

  it('is always exactly 256 entries, regardless of the 254-entry source legend', () => {
    expect(palette.length).toBe(256)
  })

  it('packs index 0, which the legend marks fully transparent, to 0', () => {
    expect(capabilities.legend.pixels[0].color).toBe('#00000000')
    expect(palette[0]).toBe(0)
  })

  it('packs a known opaque colour mid-ramp to the right channel bytes', () => {
    expect(capabilities.legend.pixels[58].color).toBe('#0004faff')
    const [r, g, b, a] = packedBytesRoundTrip(palette[58])
    expect([r, g, b, a]).toEqual([0x00, 0x04, 0xfa, 0xff])
  })

  it('leaves indices 254 and 255 transparent -- one and two past the 254-entry legend', () => {
    expect(palette[254]).toBe(0)
    expect(palette[255]).toBe(0)
  })

  it('round-trips a full, distinct RGBA colour through an ImageData-shaped buffer', () => {
    const legend: MayaraLegend = { pixels: [{ color: '#1a2b3cd4' }] }
    const p = buildEchoPalette(legend)
    const [r, g, b, a] = packedBytesRoundTrip(p[0])
    expect([r, g, b, a]).toEqual([0x1a, 0x2b, 0x3c, 0xd4])
  })

  it('defaults alpha to opaque for a 6-digit #rrggbb colour', () => {
    const legend: MayaraLegend = { pixels: [{ color: '#112233' }] }
    const p = buildEchoPalette(legend)
    const [r, g, b, a] = packedBytesRoundTrip(p[0])
    expect([r, g, b, a]).toEqual([0x11, 0x22, 0x33, 0xff])
  })
})
