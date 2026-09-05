import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { PageHeroSelect } from '@/components/page-hero-select'
import type { DashboardLayoutItem } from '@/lib/dashboard-widgets'

/**
 * The hero picker sits beside PageSkinSelect in layout mode (ADR 0072),
 * following the same pattern: a page-level property edited where the
 * operator already is when deciding what the page looks like.
 */
describe('PageHeroSelect', () => {
  const widgets: DashboardLayoutItem[] = [
    { id: 'wind', x: 0, y: 0, w: 4, h: 6 },
    { id: 'tanks', x: 4, y: 0, w: 4, h: 4 },
  ]

  test('shows the page it is editing and the hero that page has', () => {
    render(<PageHeroSelect page={{ id: 'p1', name: 'Underway', hero: 'wind', widgets }} onSetHero={vi.fn()} />)
    expect(screen.getByLabelText('Hero widget for Underway')).toHaveValue('wind')
  })

  test('treats a page with no hero as "No hero"', () => {
    render(<PageHeroSelect page={{ id: 'p1', name: 'Anchored', widgets }} onSetHero={vi.fn()} />)
    expect(screen.getByLabelText('Hero widget for Anchored')).toHaveValue('')
  })

  test('lists every widget on the page by its display name', () => {
    render(<PageHeroSelect page={{ id: 'p1', name: 'Anchored', widgets }} onSetHero={vi.fn()} />)
    expect(screen.getByRole('option', { name: 'Apparent Wind' })).toBeInTheDocument()
    expect(screen.getByRole('option', { name: 'Tanks' })).toBeInTheDocument()
  })

  test("reports the chosen hero against the page it belongs to", () => {
    const onSetHero = vi.fn()
    render(<PageHeroSelect page={{ id: 'p2', name: 'Underway', widgets }} onSetHero={onSetHero} />)

    fireEvent.change(screen.getByLabelText('Hero widget for Underway'), { target: { value: 'tanks' } })
    expect(onSetHero).toHaveBeenCalledWith('p2', 'tanks')
  })

  test('can be set back to no hero', () => {
    const onSetHero = vi.fn()
    render(<PageHeroSelect page={{ id: 'p2', name: 'Underway', hero: 'wind', widgets }} onSetHero={onSetHero} />)

    fireEvent.change(screen.getByLabelText('Hero widget for Underway'), { target: { value: '' } })
    expect(onSetHero).toHaveBeenCalledWith('p2', '')
  })

  // Nothing to promote, nothing to show — rather than a control wired to no page.
  test('renders nothing without a page', () => {
    const { container } = render(<PageHeroSelect page={null} onSetHero={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })
})
