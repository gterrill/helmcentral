import { render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { ClockTile } from '@/components/clock-tile'
import { formatClock } from '@/hooks/use-vessel-identity'

// formatClock renders in whatever timezone the test runner's Node process is
// in (Intl.DateTimeFormat with no explicit timeZone), which fake-timing
// Date.now() does not change - so every expectation below is derived from
// formatClock itself rather than a hand-computed wall-clock string, and the
// suite is timezone-independent by construction.
function hhmmFor(date: Date) {
  const [hh = '--', mm = '--'] = formatClock(date).timePart.split(':')
  return `${hh}:${mm}`
}

const NOW = new Date('2026-06-14T14:32:07Z')

// use-vessel-identity ticks a local clock every second and fetches
// /api/vessel-state and /api/settings on mount - stub both so the tile
// renders a deterministic time with no network noise.
beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, json: async () => ({}) }))
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('ClockTile', () => {
  test('renders the current time and date from the vessel clock', () => {
    render(
      <ClockTile
        sunriseTime="6:02AM"
        sunsetTime="7:41PM"
        moonPhase="waxingGibbous"
        placeName="Airlie Beach"
        nextWaypoint={null}
      />,
    )

    // formatClock is 12-hour with seconds; the tile drops the seconds for
    // its hero readout, so only hh:mm and the meridiem are asserted.
    expect(screen.getByText(hhmmFor(NOW))).toBeInTheDocument()
    expect(screen.getByText(formatClock(NOW).meridiem)).toBeInTheDocument()
  })

  test('shows sunrise, sunset and moon phase when present', () => {
    render(
      <ClockTile
        sunriseTime="6:02AM"
        sunsetTime="7:41PM"
        moonPhase="waxingGibbous"
        placeName="Airlie Beach"
        nextWaypoint={null}
      />,
    )

    expect(screen.getByText('6:02AM')).toBeInTheDocument()
    expect(screen.getByText('7:41PM')).toBeInTheDocument()
    expect(screen.getByText(/Waxing Gibbous/)).toBeInTheDocument()
  })

  test('shows structural dashes for sunrise, sunset and moon when absent', () => {
    render(<ClockTile sunriseTime={null} sunsetTime={null} moonPhase={null} placeName={null} nextWaypoint={null} />)

    // One dash for sunrise, one for sunset, one for moon, one for place, one for ETA.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(5)
  })

  test('shows the place name in a chip', () => {
    render(<ClockTile sunriseTime={null} sunsetTime={null} moonPhase={null} placeName="Airlie Beach" nextWaypoint={null} />)
    expect(screen.getByText('Airlie Beach')).toBeInTheDocument()
  })

  test('shows an ETA line with the waypoint label and time when a route is active', () => {
    const etaAt = new Date('2026-06-14T18:05:00Z')
    render(
      <ClockTile
        sunriseTime={null}
        sunsetTime={null}
        moonPhase={null}
        placeName={null}
        nextWaypoint={{ label: 'WP 3', etaAt, basis: 'sog' }}
      />,
    )

    const eta = screen.getByTestId('clock-eta')
    expect(eta).toHaveTextContent('ETA')
    expect(eta).toHaveTextContent('WP 3')
    expect(eta).toHaveTextContent(hhmmFor(etaAt))
    expect(eta).not.toHaveTextContent(/plan/i)
  })

  test('marks a planning-speed ETA with a plan suffix', () => {
    render(
      <ClockTile
        sunriseTime={null}
        sunsetTime={null}
        moonPhase={null}
        placeName={null}
        nextWaypoint={{ label: 'Mooloolaba', etaAt: new Date('2026-06-14T18:05:00Z'), basis: 'plan' }}
      />,
    )

    expect(screen.getByTestId('clock-eta')).toHaveTextContent(/plan/i)
  })

  test('shows the waypoint label with a dash for the time when no honest ETA exists', () => {
    render(
      <ClockTile
        sunriseTime={null}
        sunsetTime={null}
        moonPhase={null}
        placeName={null}
        nextWaypoint={{ label: 'WP 1', etaAt: null, basis: 'plan' }}
      />,
    )

    expect(screen.getByText(/WP 1/)).toBeInTheDocument()
    expect(screen.getByTestId('clock-eta')).toHaveTextContent('—')
  })

  test('shows a structural dash for the ETA line when no route is active', () => {
    render(<ClockTile sunriseTime={null} sunsetTime={null} moonPhase={null} placeName={null} nextWaypoint={null} />)
    expect(screen.getByTestId('clock-eta')).toHaveTextContent('—')
  })

  test('renders the date in compact three-letter form so a long weekday holds one line', () => {
    // Local-time constructor (not a 'Z' string) so the calendar date is fixed
    // to Sep 12, 2026 - a Saturday - regardless of the test runner's own
    // timezone. formatDate's long form ("SATURDAY, SEP 12, 2026") wraps at
    // the tile's ~300px minW; the tile-local compact form must not.
    vi.setSystemTime(new Date(2026, 8, 12, 14, 32, 7))
    render(
      <ClockTile
        sunriseTime="6:02AM"
        sunsetTime="7:41PM"
        moonPhase="waxingGibbous"
        placeName="Airlie Beach"
        nextWaypoint={null}
      />,
    )

    expect(screen.getByTestId('clock-date')).toHaveTextContent('SAT, SEP 12, 2026')
  })

  test('marks the date line non-wrapping as a last line of defence against clipping the place chip', () => {
    render(
      <ClockTile
        sunriseTime="6:02AM"
        sunsetTime="7:41PM"
        moonPhase="waxingGibbous"
        placeName="Airlie Beach"
        nextWaypoint={null}
      />,
    )

    expect(screen.getByTestId('clock-date').className).toContain('whitespace-nowrap')
  })

  test('still renders the place name when a long weekday and a route ETA are both present', () => {
    // Wednesday, Sep 16 2026 - one of the longest weekday names - paired with
    // an active route, the combination most likely to starve the place chip
    // of vertical room if the date line were ever allowed to wrap again.
    vi.setSystemTime(new Date(2026, 8, 16, 14, 32, 7))
    render(
      <ClockTile
        sunriseTime="6:02AM"
        sunsetTime="7:41PM"
        moonPhase="waxingGibbous"
        placeName="Airlie Beach"
        nextWaypoint={{ label: 'Mooloolaba Marina', etaAt: new Date('2026-09-16T18:05:00Z'), basis: 'plan' }}
      />,
    )

    expect(screen.getByText('Airlie Beach')).toBeInTheDocument()
    expect(screen.getByTestId('clock-eta')).toHaveTextContent('Mooloolaba Marina')
  })
})
