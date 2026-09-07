import { render, screen, fireEvent } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AlarmBanner } from '@/components/alarm-banner'
import type { ActiveAlarm } from '@/hooks/use-alarms'

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

describe('AlarmBanner', () => {
  it('renders nothing when there are no alarms at all', () => {
    render(<AlarmBanner alarms={[]} onOpen={vi.fn()} />)

    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('renders the loud variant for an unacknowledged alarm', () => {
    render(<AlarmBanner alarms={[makeAlarm()]} onOpen={vi.fn()} />)

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Anchor dragging')
    expect(banner.className).toMatch(/destructive/)
  })

  // P0 regression: the board must never go back to looking calm while a
  // drag condition holds, even after the operator silences it. Filtering
  // acknowledged alarms out entirely used to do exactly that.
  it('keeps rendering a muted-but-present banner once the alarm is acknowledged, rather than disappearing', () => {
    render(<AlarmBanner alarms={[makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })]} onOpen={vi.fn()} />)

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
    render(<AlarmBanner alarms={[makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })]} onOpen={vi.fn()} />)

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('all acknowledged, still live')
    expect(banner).not.toHaveTextContent(/silenced/i)
  })

  it('renders the operator condition sentence, not the raw SI message, for a rule alarm', () => {
    render(
      <AlarmBanner
        alarms={[
          makeAlarm({
            label: 'Barometer falling',
            op: 'below',
            clear_value: -0.02,
            value: -0.0305,
            unit: 'Pa/s',
            message: 'Barometer falling: -0.03 Pa/s, clears above -0.02 Pa/s',
          }),
        ]}
        onOpen={vi.fn()}
      />,
    )

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Falling 1.1 mb/hr. Clears once the fall eases to 0.7 mb/hr.')
  })

  it('prefers the unacknowledged alarm as the headline when both kinds are live', () => {
    render(
      <AlarmBanner
        alarms={[
          makeAlarm({ rule_id: 'a', label: 'Silenced one', phase: 'acknowledged', silenced: true, can_acknowledge: false }),
          makeAlarm({ rule_id: 'b', label: 'Loud one', phase: 'active' }),
        ]}
        onOpen={vi.fn()}
      />,
    )

    const banner = screen.getByRole('alert')
    expect(banner).toHaveTextContent('Loud one')
    expect(banner.className).toMatch(/destructive/)
  })

  it('opens the alarms panel from the View button in either variant', () => {
    const onOpen = vi.fn()
    render(<AlarmBanner alarms={[makeAlarm({ phase: 'acknowledged', silenced: true, can_acknowledge: false })]} onOpen={onOpen} />)

    fireEvent.click(screen.getByRole('button', { name: 'View' }))

    expect(onOpen).toHaveBeenCalledTimes(1)
  })
})

// ADR 0082: the ribbon never reorders, so triage moves to the banner instead —
// it ranks every shown alarm worst first (ALARM_STATES order) and rolls the
// headline up into a count per state present rather than naming only one.
describe('AlarmBanner triage', () => {
  it('ranks the worst alarm first regardless of input order, for both the headline and the condition line', () => {
    render(
      <AlarmBanner
        alarms={[
          makeAlarm({ rule_id: 'warn-1', label: 'High bilge', state: 'warn', message: 'High bilge running.' }),
          makeAlarm({ rule_id: 'alarm-1', label: 'Anchor dragging', state: 'alarm' }),
          makeAlarm({ rule_id: 'warn-2', label: 'Battery low', state: 'warn', message: 'Battery low.' }),
        ]}
        onOpen={vi.fn()}
      />,
    )

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
    render(
      <AlarmBanner
        alarms={[
          makeAlarm({ rule_id: 'warn-1', label: 'High bilge', state: 'warn' }),
          makeAlarm({ rule_id: 'alarm-1', label: 'Anchor dragging', state: 'alarm' }),
          makeAlarm({ rule_id: 'warn-2', label: 'Battery low', state: 'warn' }),
        ]}
        onOpen={vi.fn()}
      />,
    )

    // The state words are the tracked *uppercase idiom* visually (a CSS class,
    // as jsdom does not apply text-transform to textContent), so the raw DOM
    // text is the lowercase AlarmState string, same as ALARM_STATES itself.
    expect(screen.getByRole('alert')).toHaveTextContent('1 alarm · 2 warn')
  })

  it('keeps same-severity alarms in their original relative order', () => {
    render(
      <AlarmBanner
        alarms={[
          makeAlarm({ rule_id: 'warn-1', label: 'First warn', state: 'warn' }),
          makeAlarm({ rule_id: 'warn-2', label: 'Second warn', state: 'warn' }),
        ]}
        onOpen={vi.fn()}
      />,
    )

    expect(screen.getByRole('alert')).toHaveTextContent('First warn, Second warn')
  })

  it('shows a single-state count for one alarm, unchanged from a one-alarm headline', () => {
    render(<AlarmBanner alarms={[makeAlarm({ state: 'emergency', label: 'Fire' })]} onOpen={vi.fn()} />)

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
    render(
      <AlarmBanner
        alarms={[
          {
            id: 'a1',
            label: 'radar.fur6424a.guardzone.1',
            state: 'alert',
            message: 'Radar fur6424A guard zone 1: target 100000294 acquired',
            phase: 'raised',
          } as never,
        ]}
        onOpen={() => {}}
      />,
    )

    const label = screen.getByText(/radar\.fur6424a\.guardzone\.1/i)
    expect(label.className).not.toMatch(/\buppercase\b/)
  })
})
