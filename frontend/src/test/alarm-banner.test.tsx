import { render, screen, fireEvent, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmBanner } from '@/components/alarm-banner'
import type { ActiveAlarm } from '@/hooks/use-alarms'
import type { ForecastWarnings } from '@/hooks/use-forecast-warnings'
import { FORECAST_WIND_WARNING_PATH } from '@/lib/alarm-display'

function makeAlarm(overrides: Partial<ActiveAlarm> = {}): ActiveAlarm {
  return {
    rule_id: 'helmcentral:anchor-drag',
    label: 'Anchor dragging',
    path: 'notifications.navigation.anchor',
    phase: 'active',
    state: 'alarm',
    value: 62,
    message: 'Anchor dragging: 62m from where it was set',
    silenced: false,
    can_silence: false,
    can_acknowledge: true,
    ...overrides,
  }
}

function makeForecastWarnings(detailsUrl: string): ForecastWarnings {
  return {
    provider: 'bom',
    region: 'Capricornia Coast',
    bulletins: [
      {
        id: 'IDQ20085',
        title: 'Marine Wind Warning Summary for Queensland',
        issuedAt: null,
        detailsUrl,
        category: 'wind',
        sections: [{ day: 'Sunday 5 July', warningType: 'Gale Warning' }],
      },
    ],
  }
}

function renderBanner(overrides: Partial<Parameters<typeof AlarmBanner>[0]> = {}) {
  return render(
    <AlarmBanner
      alarms={[]}
      onOpen={vi.fn()}
      onAcknowledge={vi.fn().mockResolvedValue(undefined)}
      forecastWarnings={null}
      {...overrides}
    />,
  )
}

describe('AlarmBanner', () => {
  it('renders nothing when there are no alarms at all', () => {
    renderBanner({ alarms: [] })

    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('renders the loud variant for an unacknowledged alarm', () => {
    renderBanner({ alarms: [makeAlarm()] })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Anchor dragging')
    expect(banner.className).toMatch(/destructive/)
  })

  // P0 regression: the board must never go back to looking calm while a
  // drag condition holds, even after the operator silences it. Filtering
  // acknowledged alarms out entirely used to do exactly that.
  it('keeps rendering a muted-but-present banner once the alarm is acknowledged, rather than disappearing', () => {
    renderBanner({ alarms: [makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })] })

    const banner = screen.getByRole('alert')
    expect(banner).toBeInTheDocument()
    expect(banner).toHaveTextContent('Anchor dragging')
    // Muted, not the loud alert styling reserved for an unacknowledged alarm.
    expect(banner.className).not.toMatch(/destructive/)
  })

  // ADR 0038 draws a line between silenced (stopped sounding) and
  // acknowledged (also stopped the visual alert); the banner used to blur
  // that by calling every muted alarm "silenced".
  it('says all acknowledged, still live rather than silenced once every shown alarm is acknowledged', () => {
    renderBanner({ alarms: [makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })] })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('all acknowledged, still live')
    expect(banner).not.toHaveTextContent(/silenced/i)
  })

  it('renders the operator condition sentence, not the raw SI message, for a rule alarm', () => {
    renderBanner({
      alarms: [
        makeAlarm({
          label: 'Barometer falling',
          op: 'below',
          clear_value: -0.02,
          value: -0.0305,
          unit: 'Pa/s',
          message: 'Barometer falling: -0.03 Pa/s, clears above -0.02 Pa/s',
        }),
      ],
    })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Falling 1.1 mb/hr. Clears once the fall eases to 0.7 mb/hr.')
  })

  it('prefers the unacknowledged alarm as the headline when both kinds are live', () => {
    renderBanner({
      alarms: [
        makeAlarm({ rule_id: 'a', label: 'Silenced one', phase: 'acknowledged', silenced: true, can_acknowledge: false }),
        makeAlarm({ rule_id: 'b', label: 'Loud one', phase: 'active' }),
      ],
    })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Loud one')
    expect(banner.className).toMatch(/destructive/)
  })

  it('opens the alarms panel from the View button in either variant', () => {
    const onOpen = vi.fn()
    renderBanner({ alarms: [makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })], onOpen })

    fireEvent.click(screen.getByRole('button', { name: 'View' }))

    expect(onOpen).toHaveBeenCalledTimes(1)
  })
})

// ADR 0082: the ribbon never reorders, so triage moves to the banner instead —
// it ranks every shown alarm worst first (ALARM_STATES order) and rolls the
// headline up into a count per state present rather than naming only one.
describe('AlarmBanner triage', () => {
  it('ranks the worst alarm first regardless of input order, for both the headline and the condition line', () => {
    renderBanner({
      alarms: [
        makeAlarm({ rule_id: 'warn-1', label: 'High bilge', state: 'warn', message: 'High bilge running.' }),
        makeAlarm({ rule_id: 'alarm-1', label: 'Anchor dragging', state: 'alarm' }),
        makeAlarm({ rule_id: 'warn-2', label: 'Battery low', state: 'warn', message: 'Battery low.' }),
      ],
    })

    const banner = screen.getByRole('alert')
    // Labels appear worst-first: the alarm-severity alarm before either warn.
    expect(banner).toHaveTextContent('Anchor dragging, High bilge, Battery low')
    // The second line is the worst alarm's own condition ("Anchor dragging"
    // from makeAlarm's default message), not the first one that happened to
    // be first in the input array ("High bilge running.").
    expect(banner).toHaveTextContent('Anchor dragging: 62m from where it was set')
    expect(banner).not.toHaveTextContent('High bilge running.')
  })

  it('rolls the headline up into one count per state present, worst first', () => {
    renderBanner({
      alarms: [
        makeAlarm({ rule_id: 'warn-1', label: 'High bilge', state: 'warn' }),
        makeAlarm({ rule_id: 'alarm-1', label: 'Anchor dragging', state: 'alarm' }),
        makeAlarm({ rule_id: 'warn-2', label: 'Battery low', state: 'warn' }),
      ],
    })

    // The state words are the tracked *uppercase idiom* visually (a CSS class,
    // as jsdom does not apply text-transform to textContent), so the raw DOM
    // text is the lowercase AlarmState string, same as ALARM_STATES itself.
    expect(screen.getByRole('alert')).toHaveTextContent('1 alarm · 2 warn')
  })

  it('keeps same-severity alarms in their original relative order', () => {
    renderBanner({
      alarms: [
        makeAlarm({ rule_id: 'warn-1', label: 'First warn', state: 'warn' }),
        makeAlarm({ rule_id: 'warn-2', label: 'Second warn', state: 'warn' }),
      ],
    })

    expect(screen.getByRole('alert')).toHaveTextContent('First warn, Second warn')
  })

  it('shows a single-state count for one alarm, unchanged from a one-alarm headline', () => {
    renderBanner({ alarms: [makeAlarm({ state: 'emergency', label: 'Fire' })] })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('1 emergency')
    expect(banner).toHaveTextContent('Fire')
  })
})

describe('AlarmBanner identifier legibility', () => {
  // The severity word is a short status token and stays in the tracked
  // uppercase idiom. The alarm's own label is a SignalK path
  // (`radar.fur6424a.guardzone.1`), and shouting 44 characters of dotted
  // machine identifier at an operator reading the board at arm's length
  // costs legibility for nothing.
  it('does not uppercase the alarm identifier', () => {
    renderBanner({
      alarms: [
        {
          id: 'a1',
          label: 'radar.fur6424a.guardzone.1',
          state: 'alert',
          message: 'Radar fur6424A guard zone 1: target 100000294 acquired',
          phase: 'raised',
        } as never,
      ],
    })

    const label = screen.getByText(/radar\.fur6424a\.guardzone\.1/i)
    expect(label.className).not.toMatch(/\buppercase\b/)
  })
})

// The action is server-side (ADR 0038), so the banner can offer it directly
// instead of sending the operator to the Alarms drawer to silence a live
// alarm from a wall display or a phone with no other screen open (ADR 0114).
describe('AlarmBanner Acknowledge button', () => {
  it('renders an Acknowledge button when a shown alarm allows it, and calls onAcknowledge with its rule id', async () => {
    const onAcknowledge = vi.fn().mockResolvedValue(undefined)
    renderBanner({ alarms: [makeAlarm({ rule_id: 'rule-1' })], onAcknowledge })

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))

    await waitFor(() => expect(onAcknowledge).toHaveBeenCalledTimes(1))
    expect(onAcknowledge).toHaveBeenCalledWith('rule-1')
  })

  it('labels the button Acknowledge all and acknowledges every acknowledgeable shown alarm, sequentially', async () => {
    const order: string[] = []
    const onAcknowledge = vi.fn().mockImplementation(async (ruleId: string) => {
      order.push(ruleId)
    })
    renderBanner({
      alarms: [
        makeAlarm({ rule_id: 'a', label: 'First' }),
        makeAlarm({ rule_id: 'b', label: 'Second' }),
      ],
      onAcknowledge,
    })

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge all' }))

    await waitFor(() => expect(onAcknowledge).toHaveBeenCalledTimes(2))
    expect(order).toEqual(['a', 'b'])
  })

  it('renders no Acknowledge button once every shown alarm has already been acknowledged', () => {
    renderBanner({ alarms: [makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })] })

    expect(screen.queryByRole('button', { name: /Acknowledge/ })).toBeNull()
  })

  it('disables the Acknowledge button while the request is in flight', async () => {
    let resolvePromise: () => void = () => {}
    const onAcknowledge = vi.fn().mockImplementation(
      () => new Promise<void>((resolve) => { resolvePromise = resolve }),
    )
    renderBanner({ alarms: [makeAlarm()], onAcknowledge })

    const button = screen.getByRole('button', { name: 'Acknowledge' })
    fireEvent.click(button)

    await waitFor(() => expect(button).toBeDisabled())
    resolvePromise()
    await waitFor(() => expect(button).not.toBeDisabled())
  })

  it('stops at the first rejection, surfaces its message, and does not acknowledge the remaining shown alarms', async () => {
    const onAcknowledge = vi.fn()
      .mockRejectedValueOnce(new Error('boat says no'))
      .mockResolvedValueOnce(undefined)
    renderBanner({
      alarms: [
        makeAlarm({ rule_id: 'a', label: 'First' }),
        makeAlarm({ rule_id: 'b', label: 'Second' }),
      ],
      onAcknowledge,
    })

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge all' }))

    await waitFor(() => expect(screen.getByText('boat says no')).toBeInTheDocument())
    expect(onAcknowledge).toHaveBeenCalledTimes(1)
    expect(onAcknowledge).toHaveBeenCalledWith('a')
  })

  it('clears a previous error on the next Acknowledge attempt', async () => {
    const onAcknowledge = vi.fn()
      .mockRejectedValueOnce(new Error('boat says no'))
      .mockResolvedValueOnce(undefined)
    renderBanner({ alarms: [makeAlarm({ rule_id: 'a' })], onAcknowledge })

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))
    await waitFor(() => expect(screen.getByText('boat says no')).toBeInTheDocument())

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))
    await waitFor(() => expect(screen.queryByText('boat says no')).toBeNull())
  })

  // A failure message describes the alarm set it was raised against. Once
  // that set has moved on - the alarm cleared itself, or a different one
  // replaced it - the old sentence is stale, and a stale refusal sitting
  // under a live alarm reads as a refusal of that alarm.
  it('drops a previous error once the alarm it failed on is no longer on the board', async () => {
    const onAcknowledge = vi.fn().mockRejectedValue(new Error('boat says no'))
    const { rerender } = render(
      <AlarmBanner alarms={[makeAlarm({ rule_id: 'a' })]} onOpen={vi.fn()} onAcknowledge={onAcknowledge} forecastWarnings={null} />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge' }))
    await waitFor(() => expect(screen.getByText('boat says no')).toBeInTheDocument())

    rerender(
      <AlarmBanner alarms={[makeAlarm({ rule_id: 'b', label: 'Bilge high' })]} onOpen={vi.fn()} onAcknowledge={onAcknowledge} forecastWarnings={null} />,
    )

    await waitFor(() => expect(screen.queryByText('boat says no')).toBeNull())
  })

  // The click closes over the alarms of one render, but the loop awaits
  // between them. An alarm the server has since cleared answers 409 to an
  // acknowledge (alarm_service.go's errNotificationNotLive), which in a
  // fail-fast loop would abandon the alarms that are still sounding.
  it('skips a shown alarm the server has already cleared and still acknowledges the rest', async () => {
    let resolveFirst: () => void = () => {}
    const onAcknowledge = vi.fn().mockImplementation(
      () => new Promise<void>((resolve) => { resolveFirst = resolve }),
    )
    const props = { onOpen: vi.fn(), onAcknowledge, forecastWarnings: null }
    const { rerender } = render(
      <AlarmBanner
        alarms={[makeAlarm({ rule_id: 'a', label: 'First' }), makeAlarm({ rule_id: 'b', label: 'Second' }), makeAlarm({ rule_id: 'c', label: 'Third' })]}
        {...props}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Acknowledge all' }))
    await waitFor(() => expect(onAcknowledge).toHaveBeenCalledWith('a'))

    // 'b' clears on the bus while 'a' is still in flight.
    rerender(
      <AlarmBanner
        alarms={[makeAlarm({ rule_id: 'a', label: 'First' }), makeAlarm({ rule_id: 'c', label: 'Third' })]}
        {...props}
      />,
    )
    resolveFirst()

    await waitFor(() => expect(onAcknowledge).toHaveBeenCalledWith('c'))
    expect(onAcknowledge).not.toHaveBeenCalledWith('b')
  })
})

// The URL is joined client-side from the forecast-warnings payload (ADR
// 0114) rather than carried on the alarm, so the banner needs both the
// alarm and the payload to resolve it.
describe('AlarmBanner forecast bulletin link', () => {
  it('renders a View details link to the bulletin and drops the Forecast-page clause when an active bulletin matches', () => {
    renderBanner({
      alarms: [makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 2, label: 'Forecast gale or storm warning' })],
      forecastWarnings: makeForecastWarnings('https://www.bom.gov.au/qld/warnings/'),
    })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Gale warning in force.')
    expect(banner).not.toHaveTextContent('Details on the Forecast page.')

    const link = screen.getByRole('link', { name: 'View details →' })
    expect(link).toHaveAttribute('href', 'https://www.bom.gov.au/qld/warnings/')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  })

  it('keeps the Forecast-page sentence and renders no link when there is no matching active bulletin', () => {
    renderBanner({
      alarms: [makeAlarm({ path: FORECAST_WIND_WARNING_PATH, op: 'above', unit: '', value: 2, label: 'Forecast gale or storm warning' })],
      forecastWarnings: null,
    })

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Gale warning in force. Details on the Forecast page.')
    expect(screen.queryByRole('link', { name: /View details/ })).toBeNull()
  })
})
