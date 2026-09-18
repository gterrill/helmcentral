/**
 * The empty-page prompt (ADR 0107): before this, a page with nothing on it
 * showed a blank grid with no clue what to do next. Three variants depending
 * on whether layout mode is even reachable here, plus never shown on the
 * wall display, which has no interaction of its own to act on the prompt.
 */
import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { EmptyPagePrompt } from '@/components/empty-page-prompt'
import { CREATE_PAGE_MANUAL_TARGET } from '@/lib/manual-links'

describe('EmptyPagePrompt', () => {
  it('in layout mode, prompts to use Add Widget and links to the how-to', () => {
    const onOpenManual = vi.fn()
    render(<EmptyPagePrompt editing canEditLayout onOpenManual={onOpenManual} />)

    expect(screen.getByText(/add widget/i)).toBeInTheDocument()
    expect(screen.getByText(/drag widgets to rearrange/i)).toBeInTheDocument()

    fireEvent.click(screen.getByRole('button', { name: /how/i }))
    expect(onOpenManual).toHaveBeenCalledWith(CREATE_PAGE_MANUAL_TARGET)
  })

  it('outside layout mode at lg and above, points at Edit', () => {
    render(<EmptyPagePrompt editing={false} canEditLayout onOpenManual={vi.fn()} />)
    expect(screen.getByText(/press edit to add widgets/i)).toBeInTheDocument()
  })

  it('below lg, explains layout mode needs a wider screen', () => {
    render(<EmptyPagePrompt editing={false} canEditLayout={false} onOpenManual={vi.fn()} />)
    expect(screen.getByText(/at least 1024px wide/i)).toBeInTheDocument()
  })

  it('never renders on the wall display', () => {
    const { container } = render(<EmptyPagePrompt editing canEditLayout onOpenManual={vi.fn()} isKiosk />)
    expect(container).toBeEmptyDOMElement()
  })
})
