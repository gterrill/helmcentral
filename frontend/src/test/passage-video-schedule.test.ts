import { describe, expect, it } from 'vitest'

import { CLOSING_S, FPS, VIDEO_SIZES, frameSchedule, frameTimestampUs, videoFileName } from '@/lib/passage-video/schedule'
import { INTRO_MS } from '@/lib/replay-intro'
import { MOMENT_TOTAL_S, buildReplayTime } from '@/lib/replay-time'

const time = (lengthS: number, moments: { clock: number; id: string }[] = []) => buildReplayTime({ total: 1000, lengthS, spans: [], moments })

describe('the sizes', () => {
  it('are 1920 by 1080 and 1080 by 1920', () => {
    expect(VIDEO_SIZES.landscape).toMatchObject({ width: 1920, height: 1080 })
    expect(VIDEO_SIZES.portrait).toMatchObject({ width: 1080, height: 1920 })
  })
})

describe('frameSchedule', () => {
  it('is the fly-in, the sailing and the closing card, at 30 frames a second', () => {
    expect(FPS).toBe(30)
    const s = frameSchedule(time(15))
    expect(s.intro).toBe(Math.ceil((INTRO_MS / 1000) * FPS))
    // 15 s of sailing, and the frame that lands on its end.
    expect(s.sailing).toBe(15 * FPS + 1)
    expect(s.closing).toBe(CLOSING_S * FPS)
    expect(s.total).toBe(s.intro + s.sailing + s.closing)
  })

  it.each([8, 15, 30, 60])('for a %i second replay is that many seconds of sailing', (len) => {
    const s = frameSchedule(time(len))
    expect(s.sailing).toBe(len * FPS + 1)
    expect(s.total).toBeGreaterThan(len * FPS)
  })

  it('adds each photo moment\'s hold to the sailing', () => {
    const none = frameSchedule(time(15))
    const two = frameSchedule(time(15, [{ clock: 300, id: 'a' }, { clock: 700, id: 'b' }]))
    expect(two.sailing - none.sailing).toBe(Math.ceil((15 + 2 * MOMENT_TOTAL_S) * FPS - 1e-6) - 15 * FPS)
    expect(two.intro).toBe(none.intro)
    expect(two.closing).toBe(none.closing)
  })

  it('holds the closing card for about three seconds', () => {
    expect(CLOSING_S).toBe(3)
    expect(frameSchedule(time(8)).closing / FPS).toBe(3)
  })
})

describe('frameTimestampUs', () => {
  it('puts frame i at i over the frame rate, in whole microseconds', () => {
    expect(frameTimestampUs(0)).toBe(0)
    expect(frameTimestampUs(30)).toBe(1_000_000)
    expect(frameTimestampUs(1)).toBe(33_333)
  })

  it('never goes backwards and never repeats', () => {
    let last = -1
    for (let i = 0; i < 2000; i++) {
      const t = frameTimestampUs(i)
      expect(t).toBeGreaterThan(last)
      last = t
    }
  })

  it('leaves no gap longer than a frame and no overlap', () => {
    for (let i = 0; i < 300; i++) {
      const d = frameTimestampUs(i + 1) - frameTimestampUs(i)
      expect(d).toBeGreaterThanOrEqual(33_333)
      expect(d).toBeLessThanOrEqual(33_334)
    }
  })
})

describe('videoFileName', () => {
  it('comes from the passage title and the day it started, as the other exports', () => {
    // 2026-10-24 21:00 UTC is 25 Oct in a zone ten hours east.
    const start = Date.UTC(2026, 9, 24, 3, 0) / 1000
    expect(videoFileName({ title: 'Fairey Reef to Blue Pearl Bay', start, zone: { o: 10, l: 'UTC+10' } })).toBe('fairey-reef-to-blue-pearl-bay-2026-10-24.mp4')
  })

  it('uses the boat\'s own day, and a plain name when there is no title', () => {
    const start = Date.UTC(2026, 9, 24, 20, 0) / 1000
    expect(videoFileName({ title: 'Reef', start, zone: { o: 10, l: 'UTC+10' } })).toBe('reef-2026-10-25.mp4')
    expect(videoFileName({ title: '', start, zone: { o: 0, l: 'UTC' } })).toBe('passage-2026-10-24.mp4')
  })
})
