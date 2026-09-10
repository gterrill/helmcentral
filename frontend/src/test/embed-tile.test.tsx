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

describe('EmbedTile frameless mode', () => {
  test('renders just the iframe, with no tile chrome, when frameless and not editing', () => {
    render(<EmbedTile config={{ ...config, frameless: true }} editing={false} />)

    expect(screen.queryByText('Windrose')).not.toBeInTheDocument()

    const frame = getIframe()
    expect(frame).toHaveAttribute('src', config.url)
    expect(frame).toHaveAttribute('title', 'Windrose')
    const sandbox = frame.getAttribute('sandbox') ?? ''
    expect(sandbox).toContain('allow-scripts')
    expect(sandbox).toContain('allow-same-origin')
    expect(sandbox).not.toContain('allow-top-navigation')
  })

  test('falls back to the framed tile while editing, even when frameless is set', () => {
    const onConfigure = vi.fn()
    render(
      <EmbedTile config={{ ...config, frameless: true }} editing onConfigure={onConfigure} />,
    )

    expect(screen.getByText('Windrose')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /configure embed/i }))
    expect(onConfigure).toHaveBeenCalledOnce()
  })

  test('renders the framed tile as before when frameless is false', () => {
    render(<EmbedTile config={{ ...config, frameless: false }} editing={false} />)
    expect(screen.getByText('Windrose')).toBeInTheDocument()
  })

  test('renders the framed tile as before when frameless is unset', () => {
    render(<EmbedTile config={config} editing={false} />)
    expect(screen.getByText('Windrose')).toBeInTheDocument()
  })

  test('still shows the "No URL configured" zero state when frameless but no URL is set', () => {
    render(<EmbedTile config={{ title: 'Windrose', url: '', frameless: true }} editing={false} />)

    expect(document.querySelector('iframe')).toBeNull()
    expect(screen.getByText(/no url configured/i)).toBeInTheDocument()
  })

  // A frameless empty tile with no reachable gear or Configure button would be
  // a dead end, so the zero state (and its Configure action) still needs to
  // fall through in edit mode even though the widget is set frameless.
  test('still offers the Configure action from the zero state when frameless and editing', () => {
    const onConfigure = vi.fn()
    render(
      <EmbedTile
        config={{ title: 'Windrose', url: '', frameless: true }}
        editing
        onConfigure={onConfigure}
      />,
    )

    fireEvent.click(screen.getByRole('button', { name: 'Configure' }))
    expect(onConfigure).toHaveBeenCalledOnce()
  })
})

describe('EmbedTile theme sync', () => {
  const grafanaUrl =
    'http://192.168.50.240:3030/d-solo/ad6vblf/weather?orgId=1&panelId=panel-1&kiosk&theme=light'

  test('rewrites theme=light to theme=dark when isDarkTheme is true', () => {
    render(<EmbedTile config={{ title: 'Weather', url: grafanaUrl }} editing={false} isDarkTheme />)

    const src = getIframe().getAttribute('src') ?? ''
    expect(new URL(src).searchParams.get('theme')).toBe('dark')
  })

  test('rewrites theme=dark to theme=light when isDarkTheme is false', () => {
    const darkUrl = grafanaUrl.replace('theme=light', 'theme=dark')
    render(<EmbedTile config={{ title: 'Weather', url: darkUrl }} editing={false} isDarkTheme={false} />)

    const src = getIframe().getAttribute('src') ?? ''
    expect(new URL(src).searchParams.get('theme')).toBe('light')
  })

  test('preserves other query params, including valueless ones like kiosk', () => {
    render(<EmbedTile config={{ title: 'Weather', url: grafanaUrl }} editing={false} isDarkTheme />)

    const src = getIframe().getAttribute('src') ?? ''
    const params = new URL(src).searchParams
    expect(params.get('orgId')).toBe('1')
    expect(params.get('panelId')).toBe('panel-1')
    expect(params.has('kiosk')).toBe(true)
  })

  test('passes a URL with no theme param through byte-for-byte unchanged', () => {
    render(<EmbedTile config={config} editing={false} isDarkTheme />)

    const src = getIframe().getAttribute('src') ?? ''
    expect(src).toBe(config.url)
  })

  test('omitting isDarkTheme behaves as light mode', () => {
    const darkUrl = grafanaUrl.replace('theme=light', 'theme=dark')
    render(<EmbedTile config={{ title: 'Weather', url: darkUrl }} editing={false} />)

    const src = getIframe().getAttribute('src') ?? ''
    expect(new URL(src).searchParams.get('theme')).toBe('light')
  })
})
