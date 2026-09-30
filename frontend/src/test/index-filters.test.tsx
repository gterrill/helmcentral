import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { IndexFilters, type IndexFilter } from '@/components/patterns/index-filters'

function categoryFilter(overrides: Partial<IndexFilter> = {}): IndexFilter {
  return {
    id: 'category',
    label: 'Filter by category',
    value: '',
    options: [
      { value: 'mechanical', label: 'Mechanical' },
      { value: 'general', label: 'General' },
    ],
    onChange: vi.fn(),
    ...overrides,
  }
}

describe('IndexFilters', () => {
  it('calls onSearchChange as the operator types', () => {
    const onSearchChange = vi.fn()
    render(<IndexFilters searchValue="" onSearchChange={onSearchChange} searchLabel="Search equipment" />)

    fireEvent.change(screen.getByLabelText('Search equipment'), { target: { value: 'genset' } })
    expect(onSearchChange).toHaveBeenCalledWith('genset')
  })

  it('calls a filter\'s onChange when an option is picked', async () => {
    const onChange = vi.fn()
    render(
      <IndexFilters
        searchValue=""
        onSearchChange={vi.fn()}
        filters={[categoryFilter({ onChange })]}
      />,
    )

    fireEvent.click(screen.getByRole('combobox', { name: 'Filter by category' }))
    const option = await screen.findByRole('option', { name: 'Mechanical' })
    fireEvent.pointerDown(option)
    fireEvent.pointerUp(option)
    fireEvent.click(option)

    expect(onChange).toHaveBeenCalledWith('mechanical')
  })

  it('shows no active-filter count when every filter is at its All value', () => {
    render(<IndexFilters searchValue="" onSearchChange={vi.fn()} filters={[categoryFilter()]} />)
    expect(screen.queryByText(/clear all/i)).not.toBeInTheDocument()
  })

  it('shows the active-filter count and a Clear all action once a filter is set', () => {
    render(
      <IndexFilters
        searchValue=""
        onSearchChange={vi.fn()}
        filters={[categoryFilter({ value: 'mechanical' })]}
      />,
    )
    expect(screen.getByText('1')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /clear all/i })).toBeInTheDocument()
  })

  it('calls onClearAll from the Clear all button', () => {
    const onClearAll = vi.fn()
    render(
      <IndexFilters
        searchValue=""
        onSearchChange={vi.fn()}
        filters={[categoryFilter({ value: 'mechanical' })]}
        onClearAll={onClearAll}
      />,
    )
    fireEvent.click(screen.getByRole('button', { name: /clear all/i }))
    expect(onClearAll).toHaveBeenCalled()
  })

  // Documents: search is a separate overlay, so the field only announces
  // intent (click, or starting to type) and never holds text of its own.
  describe('as a search trigger', () => {
    it('opens on click and holds no text', () => {
      const onSearchActivate = vi.fn()
      render(<IndexFilters searchValue="" onSearchChange={vi.fn()} onSearchActivate={onSearchActivate} searchLabel="Search" />)
      const field = screen.getByLabelText('Search')
      expect(field).toHaveAttribute('readonly')
      fireEvent.click(field)
      expect(onSearchActivate).toHaveBeenCalledWith(undefined)
    })

    it('opens on Enter, and on a typed character which it hands over as the seed', () => {
      const onSearchActivate = vi.fn()
      render(<IndexFilters searchValue="" onSearchChange={vi.fn()} onSearchActivate={onSearchActivate} searchLabel="Search" />)
      const field = screen.getByLabelText('Search')
      fireEvent.keyDown(field, { key: 'Enter' })
      expect(onSearchActivate).toHaveBeenLastCalledWith(undefined)
      fireEvent.keyDown(field, { key: 'g' })
      expect(onSearchActivate).toHaveBeenLastCalledWith('g')
    })

    it('does not open on focus alone (focus returns here when the overlay closes)', () => {
      const onSearchActivate = vi.fn()
      render(<IndexFilters searchValue="" onSearchChange={vi.fn()} onSearchActivate={onSearchActivate} searchLabel="Search" />)
      fireEvent.focus(screen.getByLabelText('Search'))
      fireEvent.keyDown(screen.getByLabelText('Search'), { key: 'Tab' })
      expect(onSearchActivate).not.toHaveBeenCalled()
    })
  })
})
