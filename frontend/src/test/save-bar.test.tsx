import { afterEach, describe, expect, it, vi } from 'vitest'
import { render, screen, fireEvent, within } from '@testing-library/react'
import { SaveBar } from '@/components/patterns/save-bar'
import { SaveBarSlot } from '@/components/patterns/save-bar-slot'

// ADR 0142: the bar takes over the app's header (Polaris' contextual save
// bar) by portalling into a slot the shell provides inside it - so every
// render here carries a stand-in header with the same SaveBarSlot App.tsx
// mounts.
function renderInHeader(bar: React.ReactElement) {
  return render(
    <div>
      <header data-testid="header" className="relative">
        <span>Breadcrumb</span>
        <SaveBarSlot />
      </header>
      <main data-testid="body">{bar}</main>
    </div>,
  )
}

afterEach(() => { vi.restoreAllMocks() })

describe('SaveBar', () => {
  it('renders nothing when not dirty', () => {
    renderInHeader(<SaveBar dirty={false} onSave={vi.fn()} onDiscard={vi.fn()} />)
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
    expect(screen.queryByText('Unsaved changes')).not.toBeInTheDocument()
  })

  it('renders into the header slot, not the page body', () => {
    renderInHeader(<SaveBar dirty onSave={vi.fn()} onDiscard={vi.fn()} />)
    const header = screen.getByTestId('header')
    expect(within(header).getByRole('button', { name: 'Save' })).toBeInTheDocument()
    expect(within(header).getByRole('button', { name: 'Discard' })).toBeInTheDocument()
    expect(within(header).getByText('Unsaved changes')).toBeInTheDocument()
    expect(within(screen.getByTestId('body')).queryByRole('button', { name: 'Save' })).not.toBeInTheDocument()
  })

  it('covers the whole header rather than floating: the slot is absolute inset-0 and the bar fills it', () => {
    renderInHeader(<SaveBar dirty onSave={vi.fn()} onDiscard={vi.fn()} />)
    const slot = screen.getByTestId('save-bar-slot')
    expect(slot).toHaveClass('absolute', 'inset-0')
    const bar = screen.getByText('Unsaved changes').closest('[data-slot="save-bar"]')!
    expect(slot).toContainElement(bar as HTMLElement)
    expect(bar).toHaveClass('h-full', 'w-full')
    // The old bottom-pinned bar is gone.
    expect(bar).not.toHaveClass('fixed')
    expect(bar.className).not.toContain('safe-area-inset-bottom')
  })

  it('an empty slot is hidden, so it never covers the header when nothing is dirty', () => {
    renderInHeader(<SaveBar dirty={false} onSave={vi.fn()} onDiscard={vi.fn()} />)
    expect(screen.getByTestId('save-bar-slot')).toHaveClass('empty:hidden')
    expect(screen.getByTestId('save-bar-slot')).toBeEmptyDOMElement()
  })

  it('calls onSave and onDiscard', () => {
    const onSave = vi.fn()
    const onDiscard = vi.fn()
    renderInHeader(<SaveBar dirty onSave={onSave} onDiscard={onDiscard} />)

    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Discard' }))
    expect(onDiscard).toHaveBeenCalled()
  })

  it('disables both actions and shows a busy label while saving', () => {
    renderInHeader(<SaveBar dirty saving onSave={vi.fn()} onDiscard={vi.fn()} />)

    expect(screen.getByRole('button', { name: /saving/i })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Discard' })).toBeDisabled()
  })

  it('surfaces a save error inline in the bar', () => {
    renderInHeader(<SaveBar dirty onSave={vi.fn()} onDiscard={vi.fn()} error="The server rejected that name." />)
    const alert = screen.getByRole('alert')
    expect(alert).toHaveTextContent('The server rejected that name.')
    expect(within(screen.getByTestId('header')).getByRole('alert')).toBe(alert)
  })

  // Fail fast: a shell that forgot the slot must not quietly lose its Save
  // button (or fall back to floating it inline) - that is a programming
  // error, and a form the operator cannot save is worse than a loud crash.
  it('throws when dirty and the shell provides no header slot', () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    expect(() => render(<SaveBar dirty onSave={vi.fn()} onDiscard={vi.fn()} />)).toThrow(/save-bar-slot/)
  })

  it('does not require a slot while nothing is dirty', () => {
    expect(() => render(<SaveBar dirty={false} onSave={vi.fn()} onDiscard={vi.fn()} />)).not.toThrow()
  })
})
