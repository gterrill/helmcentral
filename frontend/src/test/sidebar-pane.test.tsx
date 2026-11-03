import { fireEvent, render, screen } from '@testing-library/react'
import { Link } from 'lucide-react'
import { describe, expect, it, vi } from 'vitest'

import { SidebarPaneHeader, SidebarPaneRail } from '@/components/patterns/sidebar-pane'

describe('SidebarPane controls', () => {
  it('a left pane hides with a left chevron and its rail opens with a right one', () => {
    render(
      <>
        <SidebarPaneHeader side="left" icon={Link} label="Folders" hideLabel="Hide folders" controlsId="c" onHide={vi.fn()} />
        <SidebarPaneRail side="left" icon={Link} label="Show folders" onShow={vi.fn()} />
      </>,
    )
    expect(screen.getByRole('button', { name: 'Hide folders' }).querySelector('svg.lucide-chevron-left')).not.toBeNull()
    expect(screen.getByRole('button', { name: 'Show folders' }).querySelector('svg.lucide-chevron-right')).not.toBeNull()
  })

  it('a right pane is the mirror image', () => {
    const onShow = vi.fn()
    render(
      <>
        <SidebarPaneHeader side="right" icon={Link} label="Rode Planner" hideLabel="Collapse rode planner" controlsId="c" onHide={vi.fn()} />
        <SidebarPaneRail side="right" icon={Link} label="Expand Rode Planner" onShow={onShow} />
      </>,
    )
    expect(screen.getByRole('button', { name: 'Collapse rode planner' }).querySelector('svg.lucide-chevron-right')).not.toBeNull()
    const rail = screen.getByRole('button', { name: 'Expand Rode Planner' })
    expect(rail.querySelector('svg.lucide-chevron-left')).not.toBeNull()
    expect(rail).toHaveAttribute('aria-expanded', 'false')
    expect(rail).not.toHaveAttribute('aria-controls')
    fireEvent.click(rail)
    expect(onShow).toHaveBeenCalledTimes(1)
  })
})
