import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { InventoryNav } from '@/components/inventory/inventory-nav'

// Same shape as settings-nav.test.tsx: a pure controlled list, no state of
// its own, no scroll-spy - the panel owns which section is active.
describe('InventoryNav', () => {
  it('renders all four sections', () => {
    render(<InventoryNav activeSectionId="equipment" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Equipment' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Profiles' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Locations' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Stocktake' })).toBeInTheDocument()
  })

  it('calls onSelect with the clicked section id', () => {
    const onSelect = vi.fn()
    render(<InventoryNav activeSectionId="equipment" onSelect={onSelect} />)

    fireEvent.click(screen.getByRole('button', { name: 'Locations' }))

    expect(onSelect).toHaveBeenCalledWith('locations')
  })

  it('marks only the active section as current', () => {
    const { rerender } = render(<InventoryNav activeSectionId="equipment" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Equipment' }).getAttribute('aria-current')).toBe('true')
    expect(screen.getByRole('button', { name: 'Profiles' }).getAttribute('aria-current')).toBeNull()

    rerender(<InventoryNav activeSectionId="profiles" onSelect={vi.fn()} />)

    expect(screen.getByRole('button', { name: 'Equipment' }).getAttribute('aria-current')).toBeNull()
    expect(screen.getByRole('button', { name: 'Profiles' }).getAttribute('aria-current')).toBe('true')
  })
})
