import { describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { Page } from '@/components/patterns/page'

function setup(pager?: Parameters<typeof Page>[0]['pager']) {
  return render(
    <Page title="Passage" pager={pager}>
      <input aria-label="Title" />
      <div contentEditable suppressContentEditableWarning data-testid="editable" />
    </Page>,
  )
}

describe('Page pager', () => {
  it('renders no pager buttons without the prop', () => {
    setup()
    expect(screen.queryByRole('button', { name: /^Previous/ })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /^Next/ })).not.toBeInTheDocument()
  })

  it('names each button after its target', () => {
    setup({ previous: { label: 'Alpha', onSelect: vi.fn() }, next: { label: 'Gamma', onSelect: vi.fn() } })
    expect(screen.getByRole('button', { name: 'Previous: Alpha' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Next: Gamma' })).toBeEnabled()
  })

  it('disables an end with no target', () => {
    setup({ previous: null, next: { label: 'Gamma', onSelect: vi.fn() } })
    expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Next: Gamma' })).toBeEnabled()
  })

  it('calls onSelect when clicked', () => {
    const prev = vi.fn()
    const next = vi.fn()
    setup({ previous: { label: 'Alpha', onSelect: prev }, next: { label: 'Gamma', onSelect: next } })
    fireEvent.click(screen.getByRole('button', { name: 'Previous: Alpha' }))
    fireEvent.click(screen.getByRole('button', { name: 'Next: Gamma' }))
    expect(prev).toHaveBeenCalledTimes(1)
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('moves on [ and ]', () => {
    const prev = vi.fn()
    const next = vi.fn()
    setup({ previous: { label: 'Alpha', onSelect: prev }, next: { label: 'Gamma', onSelect: next } })
    fireEvent.keyDown(window, { key: '[' })
    fireEvent.keyDown(window, { key: ']' })
    expect(prev).toHaveBeenCalledTimes(1)
    expect(next).toHaveBeenCalledTimes(1)
  })

  it('does nothing on a key at an end with no target', () => {
    const next = vi.fn()
    setup({ previous: null, next: { label: 'Gamma', onSelect: next } })
    fireEvent.keyDown(window, { key: '[' })
    expect(next).not.toHaveBeenCalled()
  })

  it('ignores keys typed in an input or an editable region', () => {
    const next = vi.fn()
    setup({ next: { label: 'Gamma', onSelect: next } })
    fireEvent.keyDown(screen.getByLabelText('Title'), { key: ']' })
    fireEvent.keyDown(screen.getByTestId('editable'), { key: ']' })
    expect(next).not.toHaveBeenCalled()
  })

  it('ignores keys with a modifier held', () => {
    const next = vi.fn()
    setup({ next: { label: 'Gamma', onSelect: next } })
    fireEvent.keyDown(window, { key: ']', metaKey: true })
    fireEvent.keyDown(window, { key: ']', ctrlKey: true })
    fireEvent.keyDown(window, { key: ']', altKey: true })
    expect(next).not.toHaveBeenCalled()
  })

  it('ignores held, already-handled and in-dialog keys', () => {
    const next = vi.fn()
    setup({ next: { label: 'Gamma', onSelect: next } })
    fireEvent.keyDown(window, { key: ']', repeat: true })
    const handled = new KeyboardEvent('keydown', { key: ']', cancelable: true })
    handled.preventDefault()
    window.dispatchEvent(handled)
    for (const role of ['dialog', 'alertdialog', 'menu', 'listbox']) {
      const overlay = document.createElement('div')
      overlay.setAttribute('role', role)
      const button = document.createElement('button')
      overlay.append(button)
      document.body.append(overlay)
      fireEvent.keyDown(button, { key: ']' })
      overlay.remove()
    }
    expect(next).not.toHaveBeenCalled()
  })

  it('stops listening when the pager goes away', () => {
    const next = vi.fn()
    const { rerender } = setup({ next: { label: 'Gamma', onSelect: next } })
    rerender(<Page title="Passage" />)
    fireEvent.keyDown(window, { key: ']' })
    expect(next).not.toHaveBeenCalled()
  })
})
