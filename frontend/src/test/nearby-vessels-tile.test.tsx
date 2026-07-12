import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'

describe('NearbyVesselsTile', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-12T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows prior contacts and last-seen age when available', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        vessels={[
          {
            name: 'TAKU X',
            range_ft: 5536,
            age_seconds: 39,
            seen_count: 3,
            last_seen_at: '2026-07-10T12:00:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByText('Seen 3x before, last 2d ago')).toBeInTheDocument()
  })

  it('hides prior-contact text when no history is available', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        vessels={[
          {
            name: 'TAKU X',
            range_ft: 5536,
            age_seconds: 39,
            seen_count: 0,
          },
        ]}
      />,
    )

    expect(screen.queryByText(/Seen .* before/)).not.toBeInTheDocument()
  })
})
