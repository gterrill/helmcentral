import { describe, expect, it } from 'vitest'

import { replayClockOffsetHours } from '@/lib/dev-flags'

describe('replayClockOffsetHours', () => {
  it('reads the offset from the address in a development build', () => {
    expect(replayClockOffsetHours('?replayClockOffsetH=9', true)).toBe(9)
    expect(replayClockOffsetHours('?a=1&replayClockOffsetH=-6.5', true)).toBe(-6.5)
  })

  it('is nothing in a production build, whatever the address says', () => {
    expect(replayClockOffsetHours('?replayClockOffsetH=9', false)).toBe(0)
  })

  it('is nothing when the address does not have it, or has rubbish', () => {
    expect(replayClockOffsetHours('', true)).toBe(0)
    expect(replayClockOffsetHours('?replayClockOffsetH=', true)).toBe(0)
    expect(replayClockOffsetHours('?replayClockOffsetH=soon', true)).toBe(0)
    expect(replayClockOffsetHours('?replayClockOffsetH=Infinity', true)).toBe(0)
  })

  it('keeps to a day either way', () => {
    expect(replayClockOffsetHours('?replayClockOffsetH=500', true)).toBe(24)
    expect(replayClockOffsetHours('?replayClockOffsetH=-500', true)).toBe(-24)
  })

  it('reads the address of the page by default', () => {
    expect(replayClockOffsetHours()).toBe(0)
  })
})
