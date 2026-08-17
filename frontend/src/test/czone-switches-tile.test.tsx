import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { CZoneSwitchesTile } from '@/components/czone-switches-tile'
import type { CZoneSwitch } from '@/hooks/use-czone-switches'

const switches: CZoneSwitch[] = [
  { id: 'sw1', display_name: 'Nav Lights', state: 0 },
  { id: 'sw2', display_name: 'Anchor Light', state: 1 },
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
})
