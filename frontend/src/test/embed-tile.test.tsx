import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'

import { EmbedTile } from '@/components/embed-tile'

const config = { title: 'Windrose', url: 'http://boat.local:3000/d-solo/abc?panelId=2' }

function getIframe(): HTMLIFrameElement {
  const frame = document.querySelector('iframe')
  if (!frame) throw new Error('expected an iframe to be rendered')
  return frame
}

describe('EmbedTile', () => {
  test('renders the configured URL in an iframe titled by the widget', () => {
    render(<EmbedTile config={config} editing={false} />)

    const frame = getIframe()
    expect(frame).toHaveAttribute('src', config.url)
    expect(frame).toHaveAttribute('title', 'Windrose')
  })

  test('shows the configured title in the tile header', () => {
    render(<EmbedTile config={config} editing={false} />)
    expect(screen.getByText('Windrose')).toBeInTheDocument()
  })

  test('sandboxes the frame without granting it top-level navigation', () => {
    render(<EmbedTile config={config} editing={false} />)

    const sandbox = getIframe().getAttribute('sandbox') ?? ''
    expect(sandbox).toContain('allow-scripts')
    expect(sandbox).toContain('allow-same-origin')
    expect(sandbox).not.toContain('allow-top-navigation')
  })

  // Without this, a drag or resize whose mouse-up lands over the frame is
  // swallowed by the embedded document and the gesture never completes.
  test('makes the frame click-through while the dashboard is in layout mode', () => {
    const { rerender } = render(<EmbedTile config={config} editing={false} />)
    expect(getIframe().className).not.toContain('pointer-events-none')

    rerender(<EmbedTile config={config} editing />)
    expect(getIframe().className).toContain('pointer-events-none')
  })

  test('renders a zero state instead of a frame when no URL is configured', () => {
    render(<EmbedTile config={undefined} editing={false} />)

    expect(document.querySelector('iframe')).toBeNull()
    expect(screen.getByText(/no url configured/i)).toBeInTheDocument()
  })

  test('renders the zero state when the configured URL is not http(s)', () => {
    render(<EmbedTile config={{ title: 'Bad', url: 'javascript:alert(1)' }} editing={false} />)

    expect(document.querySelector('iframe')).toBeNull()
    expect(screen.getByText(/no url configured/i)).toBeInTheDocument()
  })

  test('offers a configure action from the zero state while editing', () => {
    const onConfigure = vi.fn()
    render(<EmbedTile config={undefined} editing onConfigure={onConfigure} />)

    // Exact name: the header gear is also called "Configure embed: …".
    fireEvent.click(screen.getByRole('button', { name: 'Configure' }))
    expect(onConfigure).toHaveBeenCalledOnce()
  })

  test('exposes the gear only while editing', () => {
    const onConfigure = vi.fn()
    const { rerender } = render(<EmbedTile config={config} editing={false} onConfigure={onConfigure} />)
    expect(screen.queryByRole('button', { name: /configure embed/i })).toBeNull()

    rerender(<EmbedTile config={config} editing onConfigure={onConfigure} />)
    fireEvent.click(screen.getByRole('button', { name: /configure embed/i }))
    expect(onConfigure).toHaveBeenCalledOnce()
  })
})
