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
