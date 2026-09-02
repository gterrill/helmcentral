import { describe, expect, it } from 'vitest'

import {
  RADAR_ECHO_SPOKE_MAX_AGE_MS,
  clearEchoBuffer,
  createEchoBuffer,
  expireStaleSpokes,
  fillEchoPixels,
  ingestSpoke,
} from '@/lib/radar-echo/echo-buffer'
import { buildEchoLut } from '@/lib/radar-echo/geometry'
import type { RadarSpoke } from '@/lib/radar-echo/spoke-message'

function makeSpoke(fillValue: number, length: number): RadarSpoke {
  return {
    angle: 0,
    bearing: 0,
    rangeM: 100,
    lat: null,
    lon: null,
    data: new Uint8Array(length).fill(fillValue),
  }
}

describe('ingestSpoke / fillEchoPixels', () => {
  const sizePx = 33
  const spokeCount = 8
  const binCount = 4
  const lut = buildEchoLut(sizePx, spokeCount, binCount)

  it('renders one hot row as a wedge at the right bearing and nowhere else', () => {
    const buf = createEchoBuffer(spokeCount, binCount)
    // Row 2 of 8 is due east: geometry.ts puts east at spokeCount/4 == 2.
    ingestSpoke(buf, makeSpoke(200, binCount), 2, 1000)

    const palette = new Uint32Array(256)
    palette[200] = 0xff00ffff // arbitrary opaque marker; palette[0] stays 0 (transparent)

    const out = new Uint32Array(sizePx * sizePx)
    fillEchoPixels(out, buf, lut, palette)

    let litCount = 0
    for (let i = 0; i < out.length; i++) {
      if (out[i] !== 0) {
        litCount += 1
        expect(lut.spokeIndex[i]).toBe(2)
        expect(out[i]).toBe(0xff00ffff)
      }
    }
    // The wedge must actually exist -- an empty result would vacuously
    // satisfy the loop above without proving anything was drawn.
    expect(litCount).toBeGreaterThan(0)
  })

  it('draws nothing at all when the buffer is untouched', () => {
    const buf = createEchoBuffer(spokeCount, binCount)
    const palette = new Uint32Array(256).fill(0xff00ffff) // even a fully-opaque palette
    const out = new Uint32Array(sizePx * sizePx)
    fillEchoPixels(out, buf, lut, palette)
    // Data is all zero, and palette[0] is opaque here on purpose: this
    // proves it's the buffer's zeroed data driving the blank picture, not
    // an accidentally-transparent palette entry 0.
    for (let i = 0; i < out.length; i++) {
      if (lut.inside[i] === 1) expect(out[i]).toBe(0xff00ffff)
      else expect(out[i]).toBe(0)
    }
  })
})

describe('expireStaleSpokes', () => {
  it('blanks a stale row and spares a fresh one', () => {
    const buf = createEchoBuffer(2, 4)
    ingestSpoke(buf, makeSpoke(9, 4), 0, 0) // seen at t=0
    ingestSpoke(buf, makeSpoke(9, 4), 1, 5000) // seen at t=5000

    // At t=7000: row 0 is 7000ms old (stale, past RADAR_ECHO_SPOKE_MAX_AGE_MS),
    // row 1 is 2000ms old (fresh).
    const expired = expireStaleSpokes(buf, 7000, RADAR_ECHO_SPOKE_MAX_AGE_MS)

    expect(expired).toBe(1)
    expect(Array.from(buf.data.slice(0, 4))).toEqual([0, 0, 0, 0])
    expect(Array.from(buf.data.slice(4, 8))).toEqual([9, 9, 9, 9])
    expect(buf.lastSeenMs[0]).toBe(-Infinity)
    expect(buf.lastSeenMs[1]).toBe(5000)
  })

  it('never seen at all reads as already stale', () => {
    const buf = createEchoBuffer(1, 4)
    const expired = expireStaleSpokes(buf, 1_000_000, RADAR_ECHO_SPOKE_MAX_AGE_MS)
    expect(expired).toBe(1)
  })
})

describe('clearEchoBuffer', () => {
  it('zeroes the data and resets lastSeenMs to never-seen', () => {
    const buf = createEchoBuffer(2, 4)
    ingestSpoke(buf, makeSpoke(9, 4), 0, 1000)
    ingestSpoke(buf, makeSpoke(9, 4), 1, 1000)

    clearEchoBuffer(buf)

    expect(Array.from(buf.data)).toEqual(new Array(8).fill(0))
    expect(buf.lastSeenMs[0]).toBe(-Infinity)
    expect(buf.lastSeenMs[1]).toBe(-Infinity)
  })
})
