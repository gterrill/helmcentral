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
  // outputs — SignalK's meta.supportsPut says so. The user explicitly wants
  // their state shown anyway, just not offered as a control.
  it('a non-writable switch is disabled but still shows its state', () => {
    const readOnlySwitches: CZoneSwitch[] = [
      { id: 'bank.0.2', display_name: 'Bank 0 Circuit 2', state: 0, writable: false },
    ]
    render(<CZoneSwitchesTile switches={readOnlySwitches} loading={false} pending={new Set()} onToggle={vi.fn()} />)

    const button = screen.getByRole('button', { name: /bank 0 circuit 2/i })
    expect(button).toBeDisabled()
    expect(button).toHaveAccessibleName(/read-only/i)
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

  it('readOnly disables every switch regardless of writable', () => {
    const mixedSwitches: CZoneSwitch[] = [
      { id: 'venus-0', display_name: 'Venus 0', state: 1, writable: true },
      { id: 'bank.0.2', display_name: 'Bank 0 Circuit 2', state: 0, writable: false },
    ]
    render(
      <CZoneSwitchesTile switches={mixedSwitches} loading={false} pending={new Set()} onToggle={vi.fn()} readOnly />,
    )

    for (const button of screen.getAllByRole('button')) {
      expect(button).toBeDisabled()
    }
  })
})
