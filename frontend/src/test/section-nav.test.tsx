import { describe, it, expect, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { SectionNav } from '@/components/section-nav'

// The shared in-page section nav: a pure controlled list (no state, no
// scroll-spy, no fetches) rendered once for Settings and once for
// Inventory. Covers the group-heading rendering settings-nav.test.tsx and
// inventory-nav.test.tsx don't need to, plus the single unlabelled
// group case, plus the same active/onSelect contract those two already cover
// via their thin wrappers.
describe('SectionNav', () => {
  const groups = [
    { label: 'Boat & app', items: [{ id: 'general', label: 'General' }, { id: 'tiles', label: 'Tiles' }] },
    { label: 'Connections', items: [{ id: 'signalk', label: 'SignalK' }] },
  ]

  it('renders group heading text', () => {
    render(<SectionNav groups={groups} activeId="general" onSelect={vi.fn()} aria-label="Settings sections" />)

    expect(screen.getByText('Boat & app')).toBeInTheDocument()
    expect(screen.getByText('Connections')).toBeInTheDocument()
  })

  it('marks the active item with aria-current and leaves the rest unmarked', () => {
    render(<SectionNav groups={groups} activeId="signalk" onSelect={vi.fn()} aria-label="Settings sections" />)

    expect(screen.getByRole('button', { name: 'SignalK' }).getAttribute('aria-current')).toBe('true')
    expect(screen.getByRole('button', { name: 'General' }).getAttribute('aria-current')).toBeNull()
    expect(screen.getByRole('button', { name: 'Tiles' }).getAttribute('aria-current')).toBeNull()
  })

  it('calls onSelect with the clicked item id', () => {
    const onSelect = vi.fn()
    render(<SectionNav groups={groups} activeId="general" onSelect={onSelect} aria-label="Settings sections" />)

    fireEvent.click(screen.getByRole('button', { name: 'Tiles' }))

    expect(onSelect).toHaveBeenCalledWith('tiles')
  })

  it('wraps the nav in a landmark with the given accessible name', () => {
    render(<SectionNav groups={groups} activeId="general" onSelect={vi.fn()} aria-label="Settings sections" />)

    expect(screen.getByRole('navigation', { name: 'Settings sections' })).toBeInTheDocument()
  })

  it('gives each labelled group a role of group with that label', () => {
    render(<SectionNav groups={groups} activeId="general" onSelect={vi.fn()} aria-label="Settings sections" />)

    expect(screen.getByRole('group', { name: 'Boat & app' })).toBeInTheDocument()
    expect(screen.getByRole('group', { name: 'Connections' })).toBeInTheDocument()
  })

  it('renders a single unlabelled group with no heading and no group role', () => {
    render(
      <SectionNav
        groups={[{ items: [{ id: 'equipment', label: 'Equipment' }, { id: 'profiles', label: 'Profiles' }] }]}
        activeId="equipment"
        onSelect={vi.fn()}
        aria-label="Inventory sections"
      />,
    )

    expect(screen.getByRole('button', { name: 'Equipment' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Profiles' })).toBeInTheDocument()
    expect(screen.queryByRole('group')).not.toBeInTheDocument()
  })

  it('uses sentence-case button styling, not the uppercase micro-label look', () => {
    render(<SectionNav groups={groups} activeId="general" onSelect={vi.fn()} aria-label="Settings sections" />)

    const button = screen.getByRole('button', { name: 'General' })
    expect(button.className).not.toMatch(/uppercase/)
    expect(button.className).toMatch(/text-sm/)
  })

  // Below `md` the nav is a sideways strip, so a deep link to a section near
  // the end (/settings/logs) used to open with its active item scrolled out of
  // sight. The strip scrolls itself, never the page.
  it('leaves a strip that does not overflow alone', () => {
    render(<SectionNav groups={groups} activeId="signalk" onSelect={vi.fn()} aria-label="Settings sections" />)
    expect(screen.getByTestId('section-nav-strip').scrollLeft).toBe(0)
  })

  it('brings the active item into view in an overflowing strip', () => {
    const widths = { scrollWidth: 600, clientWidth: 300 }
    const spy = vi.spyOn(HTMLElement.prototype, 'scrollWidth', 'get').mockImplementation(() => widths.scrollWidth)
    const spy2 = vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => widths.clientWidth)
    const spy3 = vi.spyOn(HTMLElement.prototype, 'offsetLeft', 'get').mockImplementation(function (this: HTMLElement) {
      return this.textContent === 'SignalK' ? 450 : 0
    })
    const spy4 = vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockImplementation(() => 80)
    try {
      const { rerender } = render(<SectionNav groups={groups} activeId="general" onSelect={vi.fn()} aria-label="Settings sections" />)
      rerender(<SectionNav groups={groups} activeId="signalk" onSelect={vi.fn()} aria-label="Settings sections" />)
      const strip = screen.getByTestId('section-nav-strip')
      // Right edge of SignalK (450 + 80) must sit inside the 300px viewport.
      expect(strip.scrollLeft).toBeGreaterThanOrEqual(450 + 80 - 300)
      expect(strip.scrollLeft).toBeLessThanOrEqual(450)
    } finally {
      spy.mockRestore(); spy2.mockRestore(); spy3.mockRestore(); spy4.mockRestore()
    }
  })
})
