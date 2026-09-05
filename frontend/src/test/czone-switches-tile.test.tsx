import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CZoneSwitchesTile } from '@/components/czone-switches-tile'
import type { CZoneSwitch } from '@/hooks/use-czone-switches'

const switches: CZoneSwitch[] = [
  { id: 'sw1', display_name: 'Nav Lights', state: 0, writable: true },
  { id: 'sw2', display_name: 'Anchor Light', state: 1, writable: true },
]

describe('CZoneSwitchesTile', () => {
  it('toggles are enabled by default', () => {
    render(<CZoneSwitchesTile switches={switches} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    for (const sw of screen.getAllByRole('button')) {
      expect(sw).not.toBeDisabled()
    }
  })

  // Role-gating (ADR 0040 §frontend): a readonly SignalK user can see switch
  // state but must not be offered a control that would only 403 on the
  // server. This is cosmetic only — the server is the actual enforcement
  // point — but a button that always fails is worse than no button.
  it('disables every switch when readOnly, without hiding them', () => {
    render(<CZoneSwitchesTile switches={switches} loading={false} pending={new Set()} onToggle={vi.fn()} readOnly />)

    const buttons = screen.getAllByRole('button')
    expect(buttons).toHaveLength(2)
    for (const sw of buttons) {
      expect(sw).toBeDisabled()
    }
    expect(screen.getByText('Nav Lights')).toBeInTheDocument()
    expect(screen.getByText('Anchor Light')).toBeInTheDocument()
  })

  it('a pending switch stays disabled even when not readOnly', () => {
    render(
      <CZoneSwitchesTile
        switches={switches}
        loading={false}
        pending={new Set(['sw1'])}
        onToggle={vi.fn()}
      />,
    )

    expect(screen.getByRole('button', { name: /nav lights/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: /anchor light/i })).not.toBeDisabled()
  })

  // The whole point of plumbing the error through: "no switches" must read
  // differently from "could not read the switch panel". Rendering the old
  // empty-state copy here would mask the fetch failure exactly like the
  // hook used to before this fix.
  it('renders the error and not "No CZone switches found" when the list is empty', () => {
    render(
      <CZoneSwitchesTile
        switches={[]}
        loading={false}
        pending={new Set()}
        onToggle={vi.fn()}
        error='electrical/switches payload has no "bank" object (keys present: gx, venus-0)'
      />,
    )

    expect(screen.getByRole('alert')).toHaveTextContent(
      'electrical/switches payload has no "bank" object (keys present: gx, venus-0)',
    )
    expect(screen.queryByText('No CZone switches found')).not.toBeInTheDocument()
  })

  it('renders the switches plus a staleness notice when an error accompanies a non-empty list', () => {
    render(
      <CZoneSwitchesTile
        switches={switches}
        loading={false}
        pending={new Set()}
        onToggle={vi.fn()}
        error="signalk returned status 500"
      />,
    )

    expect(screen.getByText('Nav Lights')).toBeInTheDocument()
    expect(screen.getByText('Anchor Light')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toBeInTheDocument()
  })

  it('still renders "No CZone switches found" when there is no error and no data', () => {
    render(<CZoneSwitchesTile switches={[]} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    expect(screen.getByText('No CZone switches found')).toBeInTheDocument()
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  // Most bank circuits are status indicators (PGN 127501), not controllable
  // outputs — SignalK's meta.supportsPut says so. Finding 4: these used to
  // render as a disabled toggle, indistinguishable from a real control until
  // tapped. They now render with no toggle affordance at all, but their
  // state is still shown and still announced as read-only.
  it('a non-writable switch renders as a read-only indicator, not a toggle', () => {
    const readOnlySwitches: CZoneSwitch[] = [
      { id: 'bank.0.2', display_name: 'Bank 0 Circuit 2', state: 0, writable: false },
    ]
    render(<CZoneSwitchesTile switches={readOnlySwitches} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    expect(screen.queryByRole('button', { name: /bank 0 circuit 2/i })).not.toBeInTheDocument()
    expect(screen.getByRole('group', { name: /bank 0 circuit 2.*read-only/i })).toBeInTheDocument()
    expect(screen.getByText('OFF')).toBeInTheDocument()
  })

  it('a writable switch is interactive when not readOnly', () => {
    const writableSwitches: CZoneSwitch[] = [
      { id: 'venus-0', display_name: 'Venus 0', state: 1, writable: true },
    ]
    render(<CZoneSwitchesTile switches={writableSwitches} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    const button = screen.getByRole('button', { name: /venus 0/i })
    expect(button).not.toBeDisabled()
    expect(button).toHaveAccessibleName(/tap to toggle/i)
  })

  it('readOnly disables the writable switch\'s control, and leaves the indicator a static chip either way', () => {
    const mixedSwitches: CZoneSwitch[] = [
      { id: 'venus-0', display_name: 'Venus 0', state: 1, writable: true },
      { id: 'bank.0.2', display_name: 'Bank 0 Circuit 2', state: 0, writable: false },
    ]
    render(
      <CZoneSwitchesTile switches={mixedSwitches} loading={false} pending={new Set()} onToggle={vi.fn()} readOnly />,
    )

    const buttons = screen.getAllByRole('button')
    expect(buttons).toHaveLength(1)
    expect(buttons[0]).toBeDisabled()
    expect(screen.queryByRole('button', { name: /bank 0 circuit 2/i })).not.toBeInTheDocument()
  })

  // Finding 1: "on" was a raw emerald (an alert-semantics colour), not the
  // primary token DESIGN.md's Switches spec calls for.
  it('renders the "on" control on the primary token, not an alert-semantics emerald', () => {
    const on: CZoneSwitch[] = [{ id: 'sw2', display_name: 'Anchor Light', state: 1, writable: true }]
    render(<CZoneSwitchesTile switches={on} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    const button = screen.getByRole('button', { name: /anchor light/i })
    expect(button).toHaveClass('bg-primary')
    expect(button).not.toHaveClass('bg-emerald-600')
  })

  it('renders the "off" control on the input token', () => {
    const off: CZoneSwitch[] = [{ id: 'sw1', display_name: 'Nav Lights', state: 0, writable: true }]
    render(<CZoneSwitchesTile switches={off} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    expect(screen.getByRole('button', { name: /nav lights/i })).toHaveClass('bg-input')
  })

  // Finding 4: fourteen identical toggles made a writable breaker
  // indistinguishable from a read-only status light until tapped.
  it('splits writable rows under Controls and read-only rows under Indicators', () => {
    const mixed: CZoneSwitch[] = [
      { id: 'w1', display_name: 'Windlass', state: 1, writable: true },
      { id: 'r1', display_name: 'Bilge Pump', state: 0, writable: false },
    ]
    render(<CZoneSwitchesTile switches={mixed} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    expect(screen.getByText('Controls')).toBeInTheDocument()
    expect(screen.getByText('Indicators')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /windlass/i })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /bilge pump/i })).not.toBeInTheDocument()
  })

  it('omits the Controls header when every switch is read-only', () => {
    const allIndicators: CZoneSwitch[] = [
      { id: 'r1', display_name: 'Bilge Pump', state: 0, writable: false },
    ]
    render(<CZoneSwitchesTile switches={allIndicators} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    expect(screen.queryByText('Controls')).not.toBeInTheDocument()
    expect(screen.getByText('Indicators')).toBeInTheDocument()
  })

  it('omits the Indicators header when every switch is writable', () => {
    render(<CZoneSwitchesTile switches={switches} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    expect(screen.getByText('Controls')).toBeInTheDocument()
    expect(screen.queryByText('Indicators')).not.toBeInTheDocument()
  })
})
