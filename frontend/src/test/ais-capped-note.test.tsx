import { act, render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { AisCappedNote } from '@/components/ais-capped-note'
import { cappedBoatsMessage } from '@/lib/nearby-vessel-display'
import { installFakeTelemetryStream } from './helpers/fake-telemetry-stream'

const stream = installFakeTelemetryStream()

describe('cappedBoatsMessage', () => {
  it('is null when nothing was left out', () => {
    expect(cappedBoatsMessage(0, 40)).toBeNull()
    expect(cappedBoatsMessage(undefined, 40)).toBeNull()
  })

  it('names the count and the limit, and agrees with one boat', () => {
    expect(cappedBoatsMessage(16, 40)).toBe("16 more boats near the anchor aren't shown (limit 40).")
    expect(cappedBoatsMessage(1, 40)).toBe("1 more boat near the anchor isn't shown (limit 40).")
  })

  it('names no limit it was not told', () => {
    expect(cappedBoatsMessage(3, null)).toBe("3 more boats near the anchor aren't shown.")
  })
})

describe('AisCappedNote on the anchor-watch map', () => {
  it('appears when the feed left boats out and is absent otherwise', () => {
    render(<AisCappedNote />)
    expect(screen.queryByTestId('ais-capped-note')).toBeNull()
    act(() => { stream.emit('nearby-vessels', { vessels: [], source: 'signalk', capped_count: 12, feed_cap: 40 }) })
    expect(screen.getByTestId('ais-capped-note')).toHaveTextContent("12 more boats near the anchor aren't shown (limit 40).")
    act(() => { stream.emit('nearby-vessels', { vessels: [], source: 'signalk', capped_count: 0, feed_cap: 40 }) })
    expect(screen.queryByTestId('ais-capped-note')).toBeNull()
  })
})
