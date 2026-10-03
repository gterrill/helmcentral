import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { SettingsNav } from '@/components/settings/settings-nav'

// Section switching is a pure local-state swap (no navigation/URL involved,
// same idiom as App.tsx's own top-level panel switching) — clicking a
// section calls the onSelect callback with that section's id, and the
// caller re-renders with the new active id.
describe('SettingsNav', () => {
  it('calls onSelect with the clicked section id', () => {
    const onSelect = vi.fn()
    render(<SettingsNav activeSectionId="signalk" onSelect={onSelect} />)

    fireEvent.click(screen.getByRole('button', { name: 'Plugins' }))

    expect(onSelect).toHaveBeenCalledWith('plugins')
  })

  // ADR 0093: the onboard assistant gets its own settings section, labelled
  // "Mate" for the operator even though the section id stays 'assistant'.
  it('renders a Mate nav button', () => {
    render(<SettingsNav activeSectionId="signalk" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Mate' })).toBeInTheDocument()
  })

  // ADR 0123: Equipment profiles moved to the Inventory panel's own nav
  // (inventory-nav.test.tsx covers it there) - SettingsNav no longer offers it.
  it('does not render an Equipment nav button', () => {
    render(<SettingsNav activeSectionId="signalk" onSelect={vi.fn()} />)

    expect(screen.queryByRole('button', { name: 'Equipment' })).not.toBeInTheDocument()
  })

  it('renders an Import nav button under System', () => {
    const onSelect = vi.fn()
    render(<SettingsNav activeSectionId="signalk" onSelect={onSelect} />)

    fireEvent.click(screen.getByRole('button', { name: 'Import' }))

    expect(onSelect).toHaveBeenCalledWith('import')
  })

  it('marks only the active section as current', () => {
    const { rerender } = render(<SettingsNav activeSectionId="signalk" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'SignalK' }).getAttribute('aria-current')).toBe('true')
    expect(screen.getByRole('button', { name: 'Plugins' }).getAttribute('aria-current')).toBeNull()

    rerender(<SettingsNav activeSectionId="plugins" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'SignalK' }).getAttribute('aria-current')).toBeNull()
    expect(screen.getByRole('button', { name: 'Plugins' }).getAttribute('aria-current')).toBe('true')
  })
})
