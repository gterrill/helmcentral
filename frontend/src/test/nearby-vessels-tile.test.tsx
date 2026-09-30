import { render, screen, fireEvent, act } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { NearbyVesselsTile } from '@/components/nearby-vessels-tile'
import type { AlarmState } from '@/hooks/use-alarms'
import type { NearbyVessel } from '@/hooks/use-nearby-vessels'
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
        lastUpdateAgeS={null}
        vessels={[
          {
            id: 'urn:mrn:imo:mmsi:316042555',
            name: 'TAKU X',
            range_m: 1687.6,
            age_seconds: 39,
            seen_count: 3,
            last_seen_at: '2026-07-10T12:00:00Z',
          },
        ]}
      />,
    )

    expect(screen.getByText('Seen 3x before, last 2d ago')).toBeInTheDocument()
  })

  // The nearby-vessels feed carries no top-level "how old is this list"
  // field today (see buildNearbyVesselsPayload in backend/main.go) — only
  // each vessel's own age_seconds, a different thing (how long since that
  // contact was last seen, not whether the AIS/radar feed itself died).
  // lastUpdateAgeS is null until a source actually publishes a feed age;
  // these only prove the Tile wiring is correct once one exists.

  it('flags the tile stale once a feed age passes the threshold', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={5940}
        vessels={[]}
      />,
    )

    const badge = screen.getByTestId('tile-stale-badge')
    expect(badge).toHaveTextContent('1h 39m')
  })

  it('does not flag the tile stale when no update age is known', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={[]}
      />,
    )

    expect(screen.queryByTestId('tile-stale-badge')).not.toBeInTheDocument()
  })

  it('hides prior-contact text when no history is available', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={[
          {
            id: 'urn:mrn:imo:mmsi:316042555',
            name: 'TAKU X',
            range_m: 1687.6,
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
        lastUpdateAgeS={null}
        vessels={[
          {
            id: 'urn:mrn:imo:mmsi:316042555',
            name: 'TAKU X',
            mmsi: '316042555',
            range_m: 1687.6,
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
        lastUpdateAgeS={null}
        vessels={[
          {
            id: 'urn:mrn:imo:mmsi:316042555',
            name: 'TAKU X',
            mmsi: '316042555',
            range_m: 1687.6,
            age_seconds: 39,
            seen_count: 3,
            last_seen_at: '2026-07-10T12:00:00Z',
          },
        ]}
      />,
    )

    expect(fetchMock).not.toHaveBeenCalled()
  })

  // Regression test for keying the vessel list by name: names are not
  // unique (two boats can share one, and unnamed vessels all fall back to
  // the same compactVesselID shape), so React needs the backend's stable id
  // for reconciliation. Rendering two same-named vessels with distinct ids
  // must produce two rows and no duplicate-key warning.
  it('renders two vessels sharing a name without a duplicate-key warning', () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)

    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={[
          { id: 'urn:mrn:imo:mmsi:111111111', name: 'SAME NAME', range_m: 100, age_seconds: 10 },
          { id: 'urn:mrn:imo:mmsi:222222222', name: 'SAME NAME', range_m: 200, age_seconds: 20 },
        ]}
      />,
    )

    expect(screen.getAllByText('SAME NAME')).toHaveLength(2)
    const keyWarning = consoleError.mock.calls.some((call) => String(call[0]).includes('same key'))
    expect(keyWarning).toBe(false)

    consoleError.mockRestore()
  })
})

// A vessel with a live AIS collision alarm (ADR 0088) is marked and moved to
// the top of this list the same way it is marked on the anchor-watch map,
// except the row has room for a text label the map's marker doesn't: the
// two-tier collapse (collisionTier in nearby-vessels-tile.tsx) groups
// alert/warn as "Collision warning" (amber) and alarm/emergency as
// "Collision alarm" (red), matching the map's own alarm-tier ring cutoff.
describe('NearbyVesselsTile collision alarms', () => {
  const vessels: NearbyVessel[] = [
    { id: 'urn:mrn:imo:mmsi:100000001', name: 'NEAR BOAT', range_m: 50, age_seconds: 5 },
    { id: 'urn:mrn:imo:mmsi:100000002', name: 'ALARM BOAT', range_m: 200, age_seconds: 5 },
    { id: 'urn:mrn:imo:mmsi:100000003', name: 'EMERGENCY BOAT', range_m: 300, age_seconds: 5 },
  ]

  function vesselNames(): (string | null)[] {
    return screen.getAllByText(/BOAT$/).map((el) => el.textContent)
  }

  it('marks an alarm-tier vessel with the collision alarm label and red styling', () => {
    const aisCollisionAlarms = new Map<string, AlarmState>([['urn:mrn:imo:mmsi:100000002', 'alarm']])

    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={vessels}
        aisCollisionAlarms={aisCollisionAlarms}
      />,
    )

    expect(screen.getByText('Collision alarm')).toBeInTheDocument()
    const row = screen.getByRole('group', { name: 'ALARM BOAT, collision alarm' })
    expect(row.className).toContain('border-red-500')
    expect(row.className).toContain('bg-red-50')
  })

  it('marks a warn-tier vessel with the collision warning label and amber styling', () => {
    const aisCollisionAlarms = new Map<string, AlarmState>([['urn:mrn:imo:mmsi:100000002', 'warn']])

    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={vessels}
        aisCollisionAlarms={aisCollisionAlarms}
      />,
    )

    expect(screen.getByText('Collision warning')).toBeInTheDocument()
    const row = screen.getByRole('group', { name: 'ALARM BOAT, collision warning' })
    expect(row.className).toContain('border-amber-500')
    expect(row.className).toContain('bg-amber-50')
  })

  it('sorts alarmed vessels above a nearer unalarmed one, worst state first', () => {
    const aisCollisionAlarms = new Map<string, AlarmState>([
      ['urn:mrn:imo:mmsi:100000002', 'alarm'],
      ['urn:mrn:imo:mmsi:100000003', 'emergency'],
    ])

    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={vessels}
        aisCollisionAlarms={aisCollisionAlarms}
      />,
    )

    expect(vesselNames()).toEqual(['EMERGENCY BOAT', 'ALARM BOAT', 'NEAR BOAT'])
  })

  it('renders exactly as before when no alarm map is passed', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={vessels}
      />,
    )

    expect(screen.queryByText(/Collision/)).not.toBeInTheDocument()
    expect(screen.queryByRole('group')).not.toBeInTheDocument()
    expect(vesselNames()).toEqual(['NEAR BOAT', 'ALARM BOAT', 'EMERGENCY BOAT'])
  })

  it('renders exactly as before when the alarm map is empty', () => {
    render(
      <NearbyVesselsTile
        loading={false}
        distanceUnits="metric"
        lastUpdateAgeS={null}
        vessels={vessels}
        aisCollisionAlarms={new Map()}
      />,
    )

    expect(screen.queryByText(/Collision/)).not.toBeInTheDocument()
    expect(vesselNames()).toEqual(['NEAR BOAT', 'ALARM BOAT', 'EMERGENCY BOAT'])
  })

  describe('empty state', () => {
    const empty = (props: Partial<React.ComponentProps<typeof NearbyVesselsTile>>) =>
      render(<NearbyVesselsTile loading={false} distanceUnits="metric" lastUpdateAgeS={null} vessels={[]} {...props} />)

    it('names the range the backend searched, in metric', () => {
      empty({ maxRangeM: 5000 })
      expect(screen.getByText('No vessels within 5 km')).toBeInTheDocument()
    })

    it('names it in nautical miles for imperial operators', () => {
      empty({ maxRangeM: 5000, distanceUnits: 'imperial' })
      expect(screen.getByText('No vessels within 2.7 nm')).toBeInTheDocument()
    })

    it('keeps a fractional range and uses metres under a kilometre', () => {
      const { unmount } = empty({ maxRangeM: 2500 })
      expect(screen.getByText('No vessels within 2.5 km')).toBeInTheDocument()
      unmount()
      empty({ maxRangeM: 800 })
      expect(screen.getByText('No vessels within 800 m')).toBeInTheDocument()
    })

    it('does not invent a range when the backend sent none', () => {
      empty({ maxRangeM: null })
      expect(screen.getByText('No vessels in range')).toBeInTheDocument()
      expect(screen.queryByText(/5 km/)).toBeNull()
    })
  })
})
