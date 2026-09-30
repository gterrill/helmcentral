import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { EmptyState } from '@/components/patterns/empty-state'

describe('EmptyState', () => {
  it('shows the title and description', () => {
    render(<EmptyState title="No equipment matches yet" description="Every system aboard gets its own record." />)

    expect(screen.getByText('No equipment matches yet')).toBeInTheDocument()
    expect(screen.getByText('Every system aboard gets its own record.')).toBeInTheDocument()
  })

  it('renders an icon and an action when given', () => {
    const onAction = vi.fn()
    render(
      <EmptyState
        title="No equipment matches yet"
        icon={<svg data-testid="empty-icon" />}
        action={<button type="button" onClick={onAction}>New item</button>}
      />,
    )

    expect(screen.getByTestId('empty-icon')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'New item' }))
    expect(onAction).toHaveBeenCalled()
  })

  it('omits the description paragraph when none is given', () => {
    render(<EmptyState title="No equipment matches yet" />)
    expect(screen.queryByText(/every system/i)).not.toBeInTheDocument()
  })
})
