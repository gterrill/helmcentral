import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gunzipSync } from 'node:zlib'

import { describe, expect, it } from 'vitest'

import { decodeRadarMessage } from '@/lib/radar-echo/spoke-message'

// Phase 0's capture: two real WebSocket frames off mayara's spoke stream,
// message-delimited JSONL (one record per line, `b64` payload), gzipped.
// See backend/testdata/mayara/spokes-fur6424A.md -- this is the fixture
// that pinned the two findings the plan's original draft got wrong: only
// 4096 of the advertised 8192 angles are used, and every spoke is a full
// 1024 bytes despite `hasSparseSpokes: true`.
const fixtureDir = resolve(dirname(fileURLToPath(import.meta.url)), '../../../backend/testdata/mayara')

function loadFrames(): Uint8Array[] {
  const gz = readFileSync(resolve(fixtureDir, 'spokes-fur6424A.jsonl.gz'))
  const jsonl = gunzipSync(gz).toString('utf-8')
  return jsonl
    .split('\n')
    .filter((line) => line.trim() !== '')
    .map((line) => {
      const record = JSON.parse(line) as { t: string; b64: string }
      return new Uint8Array(Buffer.from(record.b64, 'base64'))
    })
}

function gcd(a: number, b: number): number {
  return b === 0 ? a : gcd(b, a % b)
}

describe('decodeRadarMessage against the real captured frames', () => {
  const frames = loadFrames()

  it('captured exactly the two frames the fixture documents', () => {
    expect(frames.length).toBe(2)
  })

  it('decodes a non-empty spoke list per frame, matching the fixture (258 spokes/frame)', () => {
    for (const frame of frames) {
      expect(decodeRadarMessage(frame).length).toBe(258)
    }
  })

  it('every spoke has a non-null bearing, non-null lat/lon, and a full 1024-byte data array', () => {
    for (const frame of frames) {
      for (const spoke of decodeRadarMessage(frame)) {
        expect(spoke.bearing).not.toBeNull()
        expect(spoke.lat).not.toBeNull()
        expect(spoke.lon).not.toBeNull()
        expect(spoke.data.length).toBe(1024)
      }
    }
  })

  it('pins the 4096-not-8192 finding: every angle is even, and the distinct angle step is 2', () => {
    const angles = new Set<number>()
    for (const frame of frames) {
      for (const spoke of decodeRadarMessage(frame)) {
        expect(spoke.angle % 2).toBe(0)
        angles.add(spoke.angle)
      }
    }
    const sorted = Array.from(angles).sort((a, b) => a - b)
    // The GCD of every gap between consecutively-observed angles is the
    // finest step the wire actually uses. If the wire only ever used
    // multiples of 4 (i.e. genuinely 8192/2 aliased to look "always even"
    // by coincidence), this would come out to 4 or more, not 2.
    let step = 0
    for (let i = 1; i < sorted.length; i++) step = gcd(step, sorted[i] - sorted[i - 1])
    expect(step).toBe(2)
    // And directly: some angle is ≡ 2 (mod 4), which a true step-4 wire
    // could never produce.
    expect(sorted.some((a) => a % 4 === 2)).toBe(true)
  })

  it('an angleMask of 1 keeps everything (every angle is already even)', () => {
    const frame = frames[0]
    const all = decodeRadarMessage(frame)
    const masked = decodeRadarMessage(frame, 1)
    expect(masked.length).toBe(all.length)
  })

  it('an angleMask of 3 keeps only angles divisible by 4, roughly halving the count', () => {
    const frame = frames[0]
    const all = decodeRadarMessage(frame)
    const masked = decodeRadarMessage(frame, 3)
    expect(all.length).toBe(258)
    expect(masked.length).toBe(128)
    for (const spoke of masked) expect(spoke.angle % 4).toBe(0)
  })

  it('copies the spoke data rather than handing back a view into the input buffer', () => {
    const frame = frames[0]
    const spokes = decodeRadarMessage(frame)
    const first = spokes[0]
    const originalByte = first.data[0]

    expect(first.data.buffer).not.toBe(frame.buffer)

    frame.fill(0xff) // mutate the source frame after decoding
    expect(first.data[0]).toBe(originalByte)
  })
})
