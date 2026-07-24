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

    fireEvent.click(screen.getByRole('button', { name: 'Widgets' }))

    expect(onSelect).toHaveBeenCalledWith('widgets')
  })

  it('marks only the active section as current', () => {
    const { rerender } = render(<SettingsNav activeSectionId="signalk" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'SignalK' }).getAttribute('aria-current')).toBe('true')
    expect(screen.getByRole('button', { name: 'Widgets' }).getAttribute('aria-current')).toBeNull()

    rerender(<SettingsNav activeSectionId="widgets" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'SignalK' }).getAttribute('aria-current')).toBeNull()
    expect(screen.getByRole('button', { name: 'Widgets' }).getAttribute('aria-current')).toBe('true')
  })
})
