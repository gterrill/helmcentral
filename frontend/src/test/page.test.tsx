import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Page } from '@/components/patterns/page'

describe('Page', () => {
  it('renders the title and children', () => {
    render(<Page title="Generator"><p>Body content</p></Page>)
    expect(screen.getByText('Generator')).toBeInTheDocument()
    expect(screen.getByText('Body content')).toBeInTheDocument()
  })

  it('renders a Back control that calls onBack', () => {
    const onBack = vi.fn()
    render(<Page title="Generator" onBack={onBack}>content</Page>)

    fireEvent.click(screen.getByRole('button', { name: 'Back' }))
    expect(onBack).toHaveBeenCalled()
  })

  it('omits the Back control when onBack is not given', () => {
    render(<Page title="Generator">content</Page>)
    expect(screen.queryByRole('button', { name: 'Back' })).not.toBeInTheDocument()
  })

  it('renders a primary action', () => {
    const onClick = vi.fn()
    render(<Page title="Equipment" primaryAction={{ label: 'New item', onClick }}>content</Page>)

    fireEvent.click(screen.getByRole('button', { name: 'New item' }))
    expect(onClick).toHaveBeenCalled()
  })

  it('renders secondary actions in an overflow menu', () => {
    const onDelete = vi.fn()
    render(
      <Page
        title="Generator"
        secondaryActions={[{ label: 'Delete', onClick: onDelete, destructive: true }]}
      >
        content
      </Page>,
    )

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }))
    expect(onDelete).toHaveBeenCalled()
  })

  it('omits the overflow menu when there are no secondary actions', () => {
    render(<Page title="Generator">content</Page>)
    expect(screen.queryByRole('button', { name: 'More actions' })).not.toBeInTheDocument()
  })

  // Documents' "New note" carries a kind submenu (Auto, Procedure, ...).
  it('a secondary action with a submenu opens it and runs the chosen entry', async () => {
    const onAuto = vi.fn()
    const onIntent = vi.fn()
    render(
      <Page
        title="Documents"
        secondaryActions={[
          { label: 'New folder', onClick: vi.fn() },
          { label: 'New note', onClick: vi.fn(), onIntent, submenu: [{ label: 'Auto', onClick: onAuto }, { label: 'Plain note', onClick: vi.fn() }] },
        ]}
      >
        content
      </Page>,
    )
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }))
    const trigger = await screen.findByRole('menuitem', { name: 'New note' })
    fireEvent.pointerEnter(trigger)
    expect(onIntent).toHaveBeenCalled()
    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Auto' }))
    expect(onAuto).toHaveBeenCalled()
  })
})
