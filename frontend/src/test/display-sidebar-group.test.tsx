import { describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, within } from '@testing-library/react'
import type { ReactElement } from 'react'

import { DisplaySidebarGroup, WallPageGlyph } from '@/components/display-sidebar-group'
import { Sidebar, SidebarContent, SidebarMenu, SidebarProvider } from '@/components/ui/sidebar'
import type { Display } from '@/lib/displays'

// SidebarMenuButton/SidebarMenuSubButton read sidebar context — the same
// wrapper anchor-rode-planner.test.tsx uses for its own sidebar-hosted content.
function renderGroup(ui: ReactElement) {
  return render(
    <SidebarProvider>
      <Sidebar>
        <SidebarContent>
          <SidebarMenu>{ui}</SidebarMenu>
        </SidebarContent>
      </Sidebar>
    </SidebarProvider>,
  )
}

function makeDisplay(overrides: Partial<Display> = {}): Display {
  return {
    id: 'd1',
    name: 'Flybridge',
    slug: 'flybridge',
    width: 1920,
    height: 360,
    scale: 1,
    rotate: 180,
    pixel_shift: false,
    wake_lock: false,
    created_at: '',
    updated_at: '',
    ...overrides,
  }
}

describe('WallPageGlyph', () => {
  it('renders nothing for a page with no display_id', () => {
    const { container } = render(<WallPageGlyph page={{}} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('renders nothing for a page with no dwell_seconds', () => {
    const { container } = render(<WallPageGlyph page={{ display_id: 'd1' }} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('shows the dwell duration', () => {
    render(<WallPageGlyph page={{ display_id: 'd1', dwell_seconds: 30 }} />)
    expect(screen.getByText('30s')).toBeInTheDocument()
  })

  it('adds an anchor glyph and tooltip note for an anchored condition', () => {
    render(<WallPageGlyph page={{ display_id: 'd1', dwell_seconds: 30, show_when: 'anchored' }} />)
    expect(screen.getByTitle(/while anchored/)).toBeInTheDocument()
  })

  it('carries no anchor glyph, and no "while" note, for the default always condition', () => {
    render(<WallPageGlyph page={{ display_id: 'd1', dwell_seconds: 30 }} />)
    expect(screen.queryByTitle(/while/)).not.toBeInTheDocument()
  })
})

describe('DisplaySidebarGroup', () => {
  it('always renders the header, even with zero displays, so the dialog stays reachable', () => {
    const onOpenDisplaysDialog = vi.fn()
    renderGroup(<DisplaySidebarGroup displays={[]} pages={[]} activePageId={null} dashboardActive onSelectPage={vi.fn()} onOpenDisplaysDialog={onOpenDisplaysDialog} />)
    fireEvent.click(screen.getByRole('button', { name: 'Wall displays' }))
    expect(onOpenDisplaysDialog).toHaveBeenCalled()
  })

  it('lists each display as an external link to /display/<slug>', () => {
    const displays = [makeDisplay(), makeDisplay({ id: 'd2', name: 'Saloon TV', slug: 'saloon-tv' })]
    renderGroup(<DisplaySidebarGroup displays={displays} pages={[]} activePageId={null} dashboardActive onSelectPage={vi.fn()} onOpenDisplaysDialog={vi.fn()} />)
    const flybridgeLink = screen.getByRole('link', { name: /Flybridge/ })
    expect(flybridgeLink).toHaveAttribute('href', '/display/flybridge')
    expect(flybridgeLink).toHaveAttribute('target', '_blank')
    expect(screen.getByRole('link', { name: /Saloon TV/ })).toHaveAttribute('href', '/display/saloon-tv')
  })

  it('nests each display\'s own pages under it, by display_id', () => {
    const displays = [makeDisplay(), makeDisplay({ id: 'd2', name: 'Saloon TV', slug: 'saloon-tv' })]
    const pages = [
      { id: 'p1', name: 'Wall: Engines', display_id: 'd1', dwell_seconds: 30 },
      { id: 'p2', name: 'Wall: Anchor', display_id: 'd1', dwell_seconds: 45, show_when: 'anchored' as const },
      { id: 'p3', name: 'TV: Overview', display_id: 'd2', dwell_seconds: 60 },
      { id: 'p4', name: 'Not on a wall', widgets: [] },
    ]
    renderGroup(<DisplaySidebarGroup displays={displays} pages={pages} activePageId={null} dashboardActive onSelectPage={vi.fn()} onOpenDisplaysDialog={vi.fn()} />)

    const flybridgeRow = screen.getByRole('link', { name: /Flybridge/ }).closest('li') as HTMLElement
    expect(within(flybridgeRow).getByText('Wall: Engines')).toBeInTheDocument()
    expect(within(flybridgeRow).getByText('Wall: Anchor')).toBeInTheDocument()
    expect(within(flybridgeRow).queryByText('TV: Overview')).not.toBeInTheDocument()

    const saloonRow = screen.getByRole('link', { name: /Saloon TV/ }).closest('li') as HTMLElement
    expect(within(saloonRow).getByText('TV: Overview')).toBeInTheDocument()

    expect(screen.queryByText('Not on a wall')).not.toBeInTheDocument()
  })

  it('navigates the ordinary way when a nested page row is clicked', () => {
    const onSelectPage = vi.fn()
    const displays = [makeDisplay()]
    const pages = [{ id: 'p1', name: 'Wall: Engines', display_id: 'd1', dwell_seconds: 30 }]
    renderGroup(<DisplaySidebarGroup displays={displays} pages={pages} activePageId={null} dashboardActive onSelectPage={onSelectPage} onOpenDisplaysDialog={vi.fn()} />)
    fireEvent.click(screen.getByRole('button', { name: /Wall: Engines/ }))
    expect(onSelectPage).toHaveBeenCalledWith('p1')
  })

  it('marks the active page only while the dashboard panel is showing', () => {
    const displays = [makeDisplay()]
    const pages = [{ id: 'p1', name: 'Wall: Engines', display_id: 'd1', dwell_seconds: 30 }]
    const { rerender } = renderGroup(<DisplaySidebarGroup displays={displays} pages={pages} activePageId="p1" dashboardActive onSelectPage={vi.fn()} onOpenDisplaysDialog={vi.fn()} />)
    expect(screen.getByRole('button', { name: /Wall: Engines/ })).toHaveAttribute('data-active', 'true')

    rerender(
      <SidebarProvider>
        <Sidebar>
          <SidebarContent>
            <SidebarMenu>
              <DisplaySidebarGroup displays={displays} pages={pages} activePageId="p1" dashboardActive={false} onSelectPage={vi.fn()} onOpenDisplaysDialog={vi.fn()} />
            </SidebarMenu>
          </SidebarContent>
        </Sidebar>
      </SidebarProvider>,
    )
    expect(screen.getByRole('button', { name: /Wall: Engines/ })).toHaveAttribute('data-active', 'false')
  })

  it('renders a display with no pages yet with nothing nested under it', () => {
    const displays = [makeDisplay()]
    renderGroup(<DisplaySidebarGroup displays={displays} pages={[]} activePageId={null} dashboardActive onSelectPage={vi.fn()} onOpenDisplaysDialog={vi.fn()} />)
    expect(screen.getByRole('link', { name: /Flybridge/ })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /Wall:/ })).not.toBeInTheDocument()
  })
})
