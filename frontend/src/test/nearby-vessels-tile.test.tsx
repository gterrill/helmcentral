import { render, screen, fireEvent, act } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import { formatCoordinate } from '@/lib/format'

describe('NearbyVesselsTile', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-12T12:00:00Z'))
  })

  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
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

  it('fetches and shows sighting history rows when the popover is opened', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        sightings: [
          { seen_at: '2026-07-10T09:14:00Z', lat: -21.59, lon: 149.79, geoname: 'Airlie Beach', nav_context: 'anchored' },
        ],
      }),
    })
    vi.stubGlobal('fetch', fetchMock)

    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        vessels={[
          {
            name: 'TAKU X',
            mmsi: '316042555',
            range_ft: 5536,
            age_seconds: 39,
            seen_count: 3,
            last_seen_at: '2026-07-10T12:00:00Z',
          },
        ]}
      />,
    )

    const trigger = screen.getByLabelText('View sighting history for TAKU X')
    fireEvent.click(trigger)

    await act(async () => {
      await Promise.resolve()
      await Promise.resolve()
    })

    expect(fetchMock).toHaveBeenCalledWith('/api/nearby-vessels/316042555/sightings')
    expect(screen.getByText('Airlie Beach')).toBeInTheDocument()
    expect(screen.getByText('At Anchor')).toBeInTheDocument()
    expect(screen.getByText(formatCoordinate(-21.59, true), { exact: false })).toBeInTheDocument()
    expect(screen.getByText(formatCoordinate(149.79, false), { exact: false })).toBeInTheDocument()
  })

  it('does not fetch sighting history until the popover is opened', () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        vessels={[
          {
            name: 'TAKU X',
            mmsi: '316042555',
            range_ft: 5536,
            age_seconds: 39,
            seen_count: 3,
            last_seen_at: '2026-07-10T12:00:00Z',
          },
        ]}
      />,
    )

    expect(fetchMock).not.toHaveBeenCalled()
  })
})
