import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'

import { ManualMarkdown } from '@/components/manual-markdown'

// ADR 0095: the Manual sheet renders a page's markdown through the same
// visual language as assistant-markdown.tsx (ADR 0093), plus two things a
// chat reply never needs - heading ids the sheet can scroll to, and links
// resolved against the manual tree instead of always opening a new tab.

// ManualMarkdown now renders through a React.lazy-loaded impl chunk (kiosk
// bundle-split), so the markdown output isn't there on the first synchronous
// render - only the Suspense fallback is. The first content-bearing
// assertion in each test below awaits it with findBy*; whatever follows on
// the same rendered tree can stay a plain getBy*/queryBy* once that first
// await has resolved.

describe('ManualMarkdown heading ids', () => {
  it('gives an h2 (## in the markdown) the GitHub slug id', async () => {
    render(<ManualMarkdown content="## The kiosk feed" pageId="features/dashboard" onNavigate={vi.fn()} />)

    const heading = await screen.findByText('The kiosk feed')
    expect(heading).toHaveAttribute('id', 'the-kiosk-feed')
  })

  // routes/charts/radar targets in the lookup table are all ### headings, so
  // h3 (not just h2) has to gain an id too, or the sheet's scroll-to-heading
  // silently lands nowhere for exactly those three panels.
  it('gives an h3 (### in the markdown) the GitHub slug id too', async () => {
    render(<ManualMarkdown content="### Route planning" pageId="features/dashboard" onNavigate={vi.fn()} />)

    const heading = await screen.findByText('Route planning')
    expect(heading).toHaveAttribute('id', 'route-planning')
  })
})

describe('ManualMarkdown links', () => {
  it('prevents the default navigation and calls onNavigate for an in-tree page link', async () => {
    const onNavigate = vi.fn()
    render(
      <ManualMarkdown content="[Alarms](alarms.md)" pageId="features/dashboard" onNavigate={onNavigate} />,
    )

    const link = await screen.findByRole('link', { name: 'Alarms' })
    const notCancelled = fireClickReturningWhetherDefaultRan(link)

    expect(notCancelled).toBe(false)
    expect(onNavigate).toHaveBeenCalledWith({ kind: 'page', page: 'features/alarms' })
  })

  it('treats a #hash link as an anchor and calls onNavigate with the anchor kind', async () => {
    const onNavigate = vi.fn()
    render(<ManualMarkdown content="[Tiles](#tiles)" pageId="features/dashboard" onNavigate={onNavigate} />)

    const link = await screen.findByRole('link', { name: 'Tiles' })
    fireClickReturningWhetherDefaultRan(link)

    expect(onNavigate).toHaveBeenCalledWith({ kind: 'anchor', hash: 'tiles' })
  })

  it('keeps target=_blank and rel=noreferrer, and does not call onNavigate, for an external link', async () => {
    const onNavigate = vi.fn()
    render(
      <ManualMarkdown
        content="[wazero](https://wazero.io/)"
        pageId="reference/plugins"
        onNavigate={onNavigate}
      />,
    )

    const link = await screen.findByRole('link', { name: 'wazero' })
    expect(link).toHaveAttribute('href', 'https://wazero.io/')
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noreferrer')
    expect(onNavigate).not.toHaveBeenCalled()
  })

  // fireEvent.click returns the result of the DOM's dispatchEvent - false
  // when a cancelable event (a click always is) had preventDefault() called
  // on it. That is the one observable difference between "handled in the
  // sheet" and "let the browser navigate", without actually letting jsdom
  // attempt a real navigation.
  function fireClickReturningWhetherDefaultRan(element: HTMLElement): boolean {
    const event = new MouseEvent('click', { bubbles: true, cancelable: true })
    return element.dispatchEvent(event)
  }
})

describe('ManualMarkdown img handling', () => {
  it('drops an <img> tag from the rendered output', async () => {
    const { container } = render(
      <ManualMarkdown
        content="![alt](https://example.com/x.png)\n\nSafe text"
        pageId="features/dashboard"
        onNavigate={vi.fn()}
      />,
    )

    await screen.findByText(/Safe text/)
    expect(container.querySelector('img')).toBeNull()
  })
})
