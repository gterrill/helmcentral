import { act, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { ClockTile, type ClockTileProps } from '@/components/clock-tile'
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

// use-vessel-identity.ts is the shared module-level store (item C): its
// `now`/timezone come off the SSE `vessel-state` event, not a poll of its
// own. Mock the shared telemetry module and drive it directly, the same
// pattern use-gauge-values.test.ts and use-vessel-identity.test.ts use.
const subscribeTelemetryMock = vi.fn()
let capturedListener: ((raw: string) => void) | null = null

vi.mock('@/hooks/use-telemetry-stream', () => ({
  subscribeTelemetry: (event: string, cb: (raw: string) => void) => {
    capturedListener = cb
    subscribeTelemetryMock(event, cb)
    return () => {}
  },
}))

vi.mock('@/hooks/use-app-config', () => ({
  useAppConfig: () => ({ boatModel: null }),
}))

function emitVesselState(payload: { datetime?: string; timezone?: string; status?: string; name?: string }) {
  act(() => {
    capturedListener?.(JSON.stringify(payload))
  })
}

const defaultProps: ClockTileProps = {
  sunriseTime: null,
  sunsetTime: null,
  moonPhase: null,
  placeName: null,
  tripEta: null,
}

/**
 * Renders ClockTile, then immediately syncs the shared vessel clock to the
 * current (possibly faked) system time — or to an explicit override, for the
 * timezone-label tests below that need a fixed instant under real timers.
 * The store is a module-level singleton (item C): its `now`/timezone persist
 * across tests in this file rather than resetting per mount, so every render
 * has to re-sync it explicitly rather than relying on a fresh per-instance
 * clock the way the old one-hook-per-consumer implementation did.
 */
function renderClockTile(props: Partial<ClockTileProps> = {}, stream: { datetime?: string; timezone?: string } = {}) {
  const result = render(<ClockTile {...defaultProps} {...props} />)
  emitVesselState({ datetime: stream.datetime ?? new Date().toISOString(), timezone: stream.timezone })
  return result
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(NOW)
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  subscribeTelemetryMock.mockClear()
  capturedListener = null
})

describe('ClockTile', () => {
  test('renders the current time and date from the vessel clock', () => {
    renderClockTile({
      sunriseTime: '6:02AM',
      sunsetTime: '7:41PM',
      moonPhase: 'waxingGibbous',
      placeName: 'Airlie Beach',
    })

    // formatClock is 12-hour with seconds; the tile drops the seconds for
    // its hero readout, so only hh:mm and the meridiem are asserted.
    expect(screen.getByText(hhmmFor(NOW))).toBeInTheDocument()
    expect(screen.getByText(formatClock(NOW).meridiem)).toBeInTheDocument()
  })

  test('shows sunrise, sunset and moon phase when present', () => {
    renderClockTile({
      sunriseTime: '6:02AM',
      sunsetTime: '7:41PM',
      moonPhase: 'waxingGibbous',
      placeName: 'Airlie Beach',
    })

    expect(screen.getByText('6:02AM')).toBeInTheDocument()
    expect(screen.getByText('7:41PM')).toBeInTheDocument()
    expect(screen.getByText(/Waxing Gibbous/)).toBeInTheDocument()
  })

  test('shows structural dashes for sunrise, sunset and moon when absent', () => {
    renderClockTile()

    // One dash for sunrise, one for sunset, one for moon, one for place, one for ETA.
    expect(screen.getAllByText('—').length).toBeGreaterThanOrEqual(5)
  })

  test('shows the place name in a chip', () => {
    renderClockTile({ placeName: 'Airlie Beach' })
    expect(screen.getByText('Airlie Beach')).toBeInTheDocument()
  })

  test('shows an ETA line with the waypoint label and time when a route is active', () => {
    const etaAt = new Date('2026-06-14T18:05:00Z')
    renderClockTile({
      tripEta: { label: 'WP 3', etaAt, basis: 'sog' },
    })

    const eta = screen.getByTestId('clock-eta')
    expect(eta).toHaveTextContent('ETA')
    expect(eta).toHaveTextContent('WP 3')
    expect(eta).toHaveTextContent(hhmmFor(etaAt))
    expect(eta).not.toHaveTextContent(/plan/i)
  })

  // A bare "09:40" can't be told from 9:40 PM - the ETA line must carry the
  // same AM/PM meridiem the hero clock shows beside its own hh:mm.
  test('includes the AM/PM meridiem in the ETA line, not just hh:mm', () => {
    const etaAt = new Date('2026-06-14T18:05:00Z')
    renderClockTile({
      tripEta: { label: 'WP 3', etaAt, basis: 'sog' },
    })

    const eta = screen.getByTestId('clock-eta')
    expect(eta).toHaveTextContent(hhmmFor(etaAt))
    expect(eta).toHaveTextContent(formatClock(etaAt).meridiem)
  })

  test('marks a planning-speed ETA with a plan suffix', () => {
    renderClockTile({
      tripEta: { label: 'Mooloolaba', etaAt: new Date('2026-06-14T18:05:00Z'), basis: 'plan' },
    })

    expect(screen.getByTestId('clock-eta')).toHaveTextContent(/plan/i)
  })

  test('shows the waypoint label with a dash for the time when no honest ETA exists', () => {
    renderClockTile({
      tripEta: { label: 'WP 1', etaAt: null, basis: 'plan' },
    })

    expect(screen.getByText(/WP 1/)).toBeInTheDocument()
    expect(screen.getByTestId('clock-eta')).toHaveTextContent('—')
  })

  test('shows a structural dash for the ETA line when no route is active', () => {
    renderClockTile()
    expect(screen.getByTestId('clock-eta')).toHaveTextContent('—')
  })

  // ADR 0125: a chartplotter go-to has no route behind it, so the backend may
  // not have a name for it yet (or ever) - the ETA line shows just the time
  // rather than inventing a label.
  test('shows the ETA time with no label when the destination has none', () => {
    const etaAt = new Date('2026-06-14T15:05:00Z')
    renderClockTile({ tripEta: { label: null, etaAt, basis: 'sog' } })

    const eta = screen.getByTestId('clock-eta')
    expect(eta).toHaveTextContent('ETA')
    expect(eta).toHaveTextContent(hhmmFor(etaAt))
    expect(eta).not.toHaveTextContent('WP')
  })

  // ADR 0125: HelmCast prefixed a cross-day ETA with the weekday; carried
  // forward here now the ETA can span a multi-day passage.
  test('prefixes the ETA with a 3-letter weekday when it falls on a different vessel-local day than today', () => {
    // Vessel-local (UTC) "now" is Jun 14; the ETA below is two days later.
    renderClockTile(
      { tripEta: { label: 'Hook Island', etaAt: new Date('2026-06-16T09:40:00Z'), basis: 'plan' } },
      { datetime: NOW.toISOString(), timezone: 'UTC' },
    )

    expect(screen.getByTestId('clock-eta')).toHaveTextContent(/\bTue\b/)
  })

  test('carries no weekday prefix when the ETA falls on the same vessel-local day as today', () => {
    renderClockTile(
      { tripEta: { label: 'Hook Island', etaAt: new Date('2026-06-14T18:05:00Z'), basis: 'plan' } },
      { datetime: NOW.toISOString(), timezone: 'UTC' },
    )

    const eta = screen.getByTestId('clock-eta')
    for (const weekday of ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']) {
      expect(eta).not.toHaveTextContent(new RegExp(`\\b${weekday}\\b`))
    }
  })

  test('renders the date in compact three-letter form so a long weekday holds one line', () => {
    // Local-time constructor (not a 'Z' string) so the calendar date is fixed
    // to Sep 12, 2026 - a Saturday - regardless of the test runner's own
    // timezone. formatDate's long form ("SATURDAY, SEP 12, 2026") wraps at
    // the tile's ~300px minW; the tile-local compact form must not.
    vi.setSystemTime(new Date(2026, 8, 12, 14, 32, 7))
    renderClockTile({
      sunriseTime: '6:02AM',
      sunsetTime: '7:41PM',
      moonPhase: 'waxingGibbous',
      placeName: 'Airlie Beach',
    })

    expect(screen.getByTestId('clock-date')).toHaveTextContent('SAT, SEP 12, 2026')
  })

  test('marks the date line non-wrapping as a last line of defence against clipping the place chip', () => {
    renderClockTile({
      sunriseTime: '6:02AM',
      sunsetTime: '7:41PM',
      moonPhase: 'waxingGibbous',
      placeName: 'Airlie Beach',
    })

    expect(screen.getByTestId('clock-date').className).toContain('whitespace-nowrap')
  })

  test('still renders the place name when a long weekday and a route ETA are both present', () => {
    // Wednesday, Sep 16 2026 - one of the longest weekday names - paired with
    // an active route, the combination most likely to starve the place chip
    // of vertical room if the date line were ever allowed to wrap again.
    vi.setSystemTime(new Date(2026, 8, 16, 14, 32, 7))
    renderClockTile({
      sunriseTime: '6:02AM',
      sunsetTime: '7:41PM',
      moonPhase: 'waxingGibbous',
      placeName: 'Airlie Beach',
      tripEta: { label: 'Mooloolaba Marina', etaAt: new Date('2026-09-16T18:05:00Z'), basis: 'plan' },
    })

    expect(screen.getByText('Airlie Beach')).toBeInTheDocument()
    expect(screen.getByTestId('clock-eta')).toHaveTextContent('Mooloolaba Marina')
  })

  // The place chip and the ETA line share one box (clock-tile.tsx's own doc
  // comment) rather than the ETA sitting in a separate margined paragraph
  // below it - that merge is what buys back the vertical room the tile
  // needs to hold both lines at h6/h7. Assert the structure directly rather
  // than only its visual effect, which jsdom cannot measure.
  test('renders the ETA line inside the same block as the place name, not as a separate element below it', () => {
    renderClockTile({
      placeName: 'Airlie Beach',
      tripEta: { label: 'Mooloolaba Marina', etaAt: new Date('2026-06-14T18:05:00Z'), basis: 'plan' },
    })

    const place = screen.getByText('Airlie Beach')
    const eta = screen.getByTestId('clock-eta')
    expect(eta.parentElement).toBe(place.parentElement)
  })

  // ── vessel-local timezone label (live finding: the wall display's Ubuntu
  // box stays on UTC while the boat sits at UTC+10, so its clock read 08:31
  // PM at 06:31 AM boat time) ────────────────────────────────────────────

  test('shows a small UTC label beside the meridiem when the vessel zone is UTC because no position is known', () => {
    renderClockTile({}, { datetime: NOW.toISOString(), timezone: 'UTC' })

    expect(screen.getByTestId('clock-timezone-label')).toHaveTextContent('UTC')
  })

  test('shows no zone label once a real vessel-local offset is known', () => {
    renderClockTile({}, { datetime: NOW.toISOString(), timezone: 'Etc/GMT-10' })

    // The date reflects the vessel-local offset (UTC+10) rather than UTC:
    // 2026-06-14T14:32:07Z is already Jun 15 local.
    expect(screen.getByTestId('clock-date')).toHaveTextContent('MON, JUN 15, 2026')
    expect(screen.queryByTestId('clock-timezone-label')).not.toBeInTheDocument()
  })
})
