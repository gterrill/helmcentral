import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { ConfirmDelete } from '@/components/patterns/confirm-delete'

describe('ConfirmDelete', () => {
  it('renders nothing interactive when closed', () => {
    render(
      <ConfirmDelete open={false} onOpenChange={vi.fn()} title="Delete &quot;Generator&quot;?" onConfirm={vi.fn()} />,
    )
    expect(screen.queryByRole('alertdialog')).not.toBeInTheDocument()
  })

  it('shows the title and description when open', () => {
    render(
      <ConfirmDelete
        open
        onOpenChange={vi.fn()}
        title='Delete "Generator"?'
        description="This can't be undone."
        onConfirm={vi.fn()}
      />,
    )
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(screen.getByText('Delete "Generator"?')).toBeInTheDocument()
    expect(screen.getByText("This can't be undone.")).toBeInTheDocument()
  })

  it('calls onConfirm when the destructive action is clicked', () => {
    const onConfirm = vi.fn()
    render(<ConfirmDelete open onOpenChange={vi.fn()} title="Delete?" onConfirm={onConfirm} />)

    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onConfirm).toHaveBeenCalled()
  })

  it('disables both actions while deleting and shows a busy label', () => {
    render(<ConfirmDelete open onOpenChange={vi.fn()} title="Delete?" onConfirm={vi.fn()} deleting />)

    expect(screen.getByRole('button', { name: /deleting/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Cancel' })).toBeDisabled()
  })

  it('calls onOpenChange(false) from Cancel', () => {
    const onOpenChange = vi.fn()
    render(<ConfirmDelete open onOpenChange={onOpenChange} title="Delete?" onConfirm={vi.fn()} />)

    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('still closes on Escape when not deleting', () => {
    const onOpenChange = vi.fn()
    render(<ConfirmDelete open onOpenChange={onOpenChange} title="Delete?" onConfirm={vi.fn()} />)

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })

  it('ignores Escape while deleting, so a delete in flight cannot be dismissed out from under itself', () => {
    const onOpenChange = vi.fn()
    render(<ConfirmDelete open onOpenChange={onOpenChange} title="Delete?" onConfirm={vi.fn()} deleting />)

    fireEvent.keyDown(document, { key: 'Escape' })

    expect(onOpenChange).not.toHaveBeenCalled()
    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
  })

  it('shows the failure once the caller reports it, having stayed open through an Escape press mid-delete', () => {
    const onOpenChange = vi.fn()
    const { rerender } = render(
      <ConfirmDelete open onOpenChange={onOpenChange} title="Delete?" onConfirm={vi.fn()} deleting />,
    )

    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onOpenChange).not.toHaveBeenCalled()

    // The delete rejects: the caller flips `deleting` off and passes the
    // server's own message, still with `open` true - ConfirmDelete never
    // closed itself, so the failure lands somewhere the operator can see it.
    rerender(
      <ConfirmDelete
        open
        onOpenChange={onOpenChange}
        title="Delete?"
        description="has linked maintenance rules"
        onConfirm={vi.fn()}
        deleting={false}
      />,
    )

    expect(screen.getByRole('alertdialog')).toBeInTheDocument()
    expect(screen.getByText('has linked maintenance rules')).toBeInTheDocument()
  })

  it('renders extra content passed as children', () => {
    render(
      <ConfirmDelete open onOpenChange={vi.fn()} title="Delete?" onConfirm={vi.fn()}>
        <p>Also delete 2 photos only this item uses</p>
      </ConfirmDelete>,
    )
    expect(screen.getByText('Also delete 2 photos only this item uses')).toBeInTheDocument()
  })
})
