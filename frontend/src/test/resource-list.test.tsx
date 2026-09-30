import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { ResourceList } from '@/components/patterns/resource-list'
import { ResourceItem } from '@/components/patterns/resource-item'

describe('ResourceList', () => {
  it('is a labelled list of its items', () => {
    render(
      <ResourceList label="Documents">
        <ResourceItem item="a" title="Manual" />
        <ResourceItem item="b" title="Wiring" />
      </ResourceList>,
    )
    const list = screen.getByRole('list', { name: 'Documents' })
    expect(within(list).getAllByRole('listitem')).toHaveLength(2)
  })

  it('shows a header with the count', () => {
    render(
      <ResourceList label="Documents" header="Linked documents" count={2}>
        <ResourceItem item="a" title="Manual" />
      </ResourceList>,
    )
    expect(screen.getByText('Linked documents')).toBeInTheDocument()
    expect(screen.getByText('2')).toBeInTheDocument()
  })

  it('renders the empty slot when there are no items', () => {
    render(<ResourceList label="Documents" empty={<p>No documents yet</p>}>{[]}</ResourceList>)
    expect(screen.getByText('No documents yet')).toBeInTheDocument()
    expect(screen.queryByRole('list')).not.toBeInTheDocument()
  })

  it('shows skeleton rows while loading, and no items', () => {
    render(
      <ResourceList label="Documents" loading>
        <ResourceItem item="a" title="Manual" />
      </ResourceList>,
    )
    expect(screen.getAllByTestId('resource-list-skeleton-row').length).toBeGreaterThan(0)
    expect(screen.queryByText('Manual')).not.toBeInTheDocument()
  })

  it('shows an error with a Retry, and no items', () => {
    const onRetry = vi.fn()
    render(
      <ResourceList label="Documents" error="Could not load" onRetry={onRetry}>
        <ResourceItem item="a" title="Manual" />
      </ResourceList>,
    )
    expect(screen.getByRole('alert')).toHaveTextContent('Could not load')
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(onRetry).toHaveBeenCalled()
    expect(screen.queryByText('Manual')).not.toBeInTheDocument()
  })
})

describe('ResourceItem', () => {
  it('lays out media, title, meta and badge', () => {
    render(
      <ResourceList label="L">
        <ResourceItem item="a" media={<span data-testid="m" />} title="Manual" meta="12 Jan 2026" badge={<span>PDF</span>} />
      </ResourceList>,
    )
    expect(screen.getByTestId('m')).toBeInTheDocument()
    expect(screen.getByText('Manual')).toBeInTheDocument()
    expect(screen.getByText('12 Jan 2026')).toBeInTheDocument()
    expect(screen.getByText('PDF')).toBeInTheDocument()
  })

  it('runs the primary action on click', () => {
    const onOpen = vi.fn()
    render(<ResourceList label="L"><ResourceItem item="a" title="Manual" onOpen={onOpen} /></ResourceList>)
    fireEvent.click(screen.getByText('Manual'))
    expect(onOpen).toHaveBeenCalledWith('a')
  })

  it('runs the primary action on Enter on the item itself', () => {
    const onOpen = vi.fn()
    render(<ResourceList label="L"><ResourceItem item="a" title="Manual" onOpen={onOpen} /></ResourceList>)
    const li = screen.getByRole('listitem')
    expect(li).toHaveAttribute('tabindex', '0')
    fireEvent.keyDown(li, { key: 'Enter' })
    expect(onOpen).toHaveBeenCalledWith('a')
  })

  it('is not focusable without a primary action', () => {
    render(<ResourceList label="L"><ResourceItem item="a" title="Manual" /></ResourceList>)
    expect(screen.getByRole('listitem')).not.toHaveAttribute('tabindex')
  })

  it('a row action runs alone: no click or Enter reaches the primary action', async () => {
    const onOpen = vi.fn()
    const onRemove = vi.fn()
    render(
      <ResourceList label="L">
        <ResourceItem
          item="a"
          title="Manual"
          onOpen={onOpen}
          actionsLabel="Actions for Manual"
          actions={[{ label: 'Remove from item', onSelect: onRemove, destructive: true }]}
        />
      </ResourceList>,
    )
    const trigger = screen.getByRole('button', { name: 'Actions for Manual' })
    fireEvent.keyDown(trigger, { key: 'Enter' })
    expect(onOpen).not.toHaveBeenCalled()
    fireEvent.click(trigger)
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Remove from item' }))
    expect(onRemove).toHaveBeenCalledWith('a')
    expect(onOpen).not.toHaveBeenCalled()
  })
})
