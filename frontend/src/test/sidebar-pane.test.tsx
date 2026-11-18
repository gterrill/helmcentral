import { useState } from 'react'
import { fireEvent, render, screen } from '@testing-library/react'
import { Link } from 'lucide-react'
import { describe, expect, it, vi } from 'vitest'

import { SidebarPane, SidebarPaneHeader, SidebarPaneRail } from '@/components/patterns/sidebar-pane'

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

function Harness(props: Partial<React.ComponentProps<typeof SidebarPane>>) {
  const [open, setOpen] = useState(true)
  return (
    <SidebarPane
      side="right"
      icon={Link}
      label="Plans"
      open={open}
      onOpenChange={setOpen}
      hideLabel="Hide plans"
      showLabel="Show plans"
      contentId="plans-content"
      {...props}
    >
      <p>body</p>
    </SidebarPane>
  )
}

describe('SidebarPane', () => {
  it('swaps the pane and the rail as the open state changes', () => {
    render(<Harness />)
    const hide = screen.getByRole('button', { name: 'Hide plans' })
    expect(hide).toHaveAttribute('aria-controls', 'plans-content')
    expect(document.getElementById('plans-content')).toContainElement(screen.getByText('body'))
    fireEvent.click(hide)
    expect(screen.queryByText('body')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Show plans' }))
    expect(screen.getByText('body')).toBeInTheDocument()
  })

  it('is a landmark, sized and wrapped, only when asked', () => {
    const { unmount } = render(<Harness />)
    expect(screen.queryByRole('complementary')).toBeNull()
    unmount()
    render(<Harness landmarkLabel="Plans" width="24rem" paneClassName="w-full" />)
    const wrapper = screen.getByRole('complementary', { name: 'Plans' }).parentElement!
    expect(wrapper).toHaveClass('w-full')
    expect(wrapper.style.getPropertyValue('--sidebar-width')).toBe('24rem')
  })

  it('wraps the rail only when given a wrapper class, and renders the footer after the content', () => {
    const { unmount } = render(<Harness footer={<p>foot</p>} />)
    const content = document.getElementById('plans-content')!
    expect(content.compareDocumentPosition(screen.getByText('foot')) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    fireEvent.click(screen.getByRole('button', { name: 'Hide plans' }))
    expect(screen.getByRole('button', { name: 'Show plans' }).parentElement).toHaveClass('bg-sidebar')
    unmount()
    render(<Harness railWrapperClassName="strip" />)
    fireEvent.click(screen.getByRole('button', { name: 'Hide plans' }))
    expect(screen.getByRole('button', { name: 'Show plans' }).parentElement!.parentElement).toHaveClass('strip')
  })
})
