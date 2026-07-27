/**
 * Phase 1 of the phone-responsive work: the header must survive a 320px viewport.
 *
 * These assert on the two leaf components that carry real conditional behaviour —
 * the clock drops content below `sm`, and the page-switcher trigger goes icon-only.
 * The App-level header structure (`min-w-0` on both halves, the breadcrumb
 * truncating, LayoutModeToggle gated to `lg`) is not covered here: jsdom has no
 * layout engine, so an overflow assertion would be theatre. That is verified by
 * screenshot at the Phase 4 viewport matrix instead.
 */
import { render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { DashboardPageSwitcher } from '@/components/dashboard-page-switcher'
import { VesselStatusBar } from '@/components/vessel-status-bar'
import { setViewportWidth } from './viewport'

vi.mock('@/hooks/use-vessel-identity', () => ({
  useVesselIdentity: () => ({
    currentDate: 'Monday, Jul 27, 2026',
    clock: { timePart: '09:41:07', meridiem: 'AM' },
    signalkConnected: true,
  }),
}))

const pages = [
  { id: 'p1', name: 'Helm', widgets: [], created_at: '', updated_at: '' },
  { id: 'p2', name: 'Engine Room', widgets: [], created_at: '', updated_at: '' },
]

function renderSwitcher() {
  return render(
    <DashboardPageSwitcher
      pages={pages}
      activePageId="p1"
      onSelect={vi.fn()}
      onCreate={vi.fn()}
      onRename={vi.fn()}
      onDelete={vi.fn()}
    />,
  )
}

describe('header at phone width', () => {
  it('drops the date line and seconds from the clock below sm', () => {
    setViewportWidth(375)
    render(<VesselStatusBar />)

    expect(screen.getByText(/09:41/)).toBeInTheDocument()
    expect(screen.queryByText(/:07/)).not.toBeInTheDocument()
    expect(screen.queryByText('Monday, Jul 27, 2026')).not.toBeInTheDocument()
  })

  it('keeps the page-switcher trigger reachable by accessible name when icon-only', () => {
    setViewportWidth(375)
    renderSwitcher()

    // Below `sm` the label is hidden in CSS, so the aria-label is the only
    // accessible name left — losing it would strand create/rename/delete, which
    // live nowhere else. That is what this asserts.
    expect(screen.getByRole('button', { name: 'Switch dashboard page' })).toBeInTheDocument()

    // jsdom applies no CSS, so the label is still in the DOM and still "visible"
    // to Testing Library. Asserting the class is the honest limit of what a unit
    // test can check here; that it actually disappears is a Phase 4 screenshot.
    expect(screen.getByText('Helm')).toHaveClass('hidden', 'sm:inline')
  })
})

describe('header at helm width', () => {
  it('shows the full clock including seconds and the date', () => {
    setViewportWidth(1280)
    render(<VesselStatusBar />)

    expect(screen.getByText(/09:41/)).toBeInTheDocument()
    expect(screen.getByText(/:07/)).toBeInTheDocument()
    expect(screen.getByText('Monday, Jul 27, 2026')).toBeInTheDocument()
  })

  it('shows the page name on the switcher trigger', () => {
    setViewportWidth(1280)
    renderSwitcher()

    expect(screen.getByRole('button', { name: 'Switch dashboard page' })).toBeInTheDocument()
    expect(screen.getByText('Helm')).toBeInTheDocument()
  })
})
