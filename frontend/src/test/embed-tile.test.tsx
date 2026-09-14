import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { EmbedTile, MOUNT_IDLE_TIMEOUT_MS } from '@/components/embed-tile'

const config = { title: 'Windrose', url: 'http://boat.local:3000/d-solo/abc?panelId=2' }

function getIframe(): HTMLIFrameElement {
  const frame = document.querySelector('iframe')
  if (!frame) throw new Error('expected an iframe to be rendered')
  return frame
}

// happy-dom's own IntersectionObserver is an inert stub — every method is a
// documented TODO (node_modules/happy-dom/lib/intersection-observer/
// IntersectionObserver.js) — so it never actually reports an intersection.
// This fake stands in for it and exposes a way to fire the callback by hand,
// the same way FakeWebSocket in use-radar-echo-stream.test.ts stands in for
// a socket that never really connects.
class FakeIntersectionObserver {
  static instances: FakeIntersectionObserver[] = []

  readonly callback: IntersectionObserverCallback
  readonly options: IntersectionObserverInit | undefined
  observedElements: Element[] = []
  disconnected = false

  constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
    this.callback = callback
    this.options = options
    FakeIntersectionObserver.instances.push(this)
  }

  observe(target: Element) {
    this.observedElements.push(target)
  }

  unobserve(target: Element) {
    this.observedElements = this.observedElements.filter((el) => el !== target)
  }

  disconnect() {
    this.disconnected = true
  }

  takeRecords(): IntersectionObserverEntry[] {
    return []
  }

  intersect(isIntersecting = true) {
    const entries = this.observedElements.map(
      (target) => ({ isIntersecting, target }) as IntersectionObserverEntry,
    )
    this.callback(entries, this as unknown as IntersectionObserver)
  }
}

beforeEach(() => {
  FakeIntersectionObserver.instances = []
  vi.stubGlobal('IntersectionObserver', FakeIntersectionObserver)
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

// Drives a freshly-rendered EmbedTile past both lazy-mount gates: reports an
// intersection on its own container, then advances past the idle-deferral
// window, matching how a real tile scrolls into view and then gets its
// idle-scheduled mount. happy-dom has no requestIdleCallback (Node doesn't
// either), so the component's fallback setTimeout path is what's under test
// here by default.
function revealAndMount() {
  const observer = FakeIntersectionObserver.instances.at(-1)
  if (!observer) throw new Error('expected an IntersectionObserver to have been created')
  act(() => {
    observer.intersect(true)
  })
  act(() => {
    vi.advanceTimersByTime(MOUNT_IDLE_TIMEOUT_MS)
  })
}

describe('EmbedTile', () => {
  test('renders the configured URL in an iframe titled by the widget', () => {
    render(<EmbedTile config={config} editing={false} />)
    revealAndMount()

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
    revealAndMount()

    const sandbox = getIframe().getAttribute('sandbox') ?? ''
    expect(sandbox).toContain('allow-scripts')
    expect(sandbox).toContain('allow-same-origin')
    expect(sandbox).not.toContain('allow-top-navigation')
  })

  test('sets loading=lazy on the frame', () => {
    render(<EmbedTile config={config} editing={false} />)
    revealAndMount()

    expect(getIframe()).toHaveAttribute('loading', 'lazy')
  })

  // Without this, a drag or resize whose mouse-up lands over the frame is
  // swallowed by the embedded document and the gesture never completes.
  test('makes the frame click-through while the dashboard is in layout mode', () => {
    const { rerender } = render(<EmbedTile config={config} editing={false} />)
    revealAndMount()
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
    revealAndMount()

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
    revealAndMount()

    const src = getIframe().getAttribute('src') ?? ''
    expect(new URL(src).searchParams.get('theme')).toBe('dark')
  })

  test('rewrites theme=dark to theme=light when isDarkTheme is false', () => {
    const darkUrl = grafanaUrl.replace('theme=light', 'theme=dark')
    render(<EmbedTile config={{ title: 'Weather', url: darkUrl }} editing={false} isDarkTheme={false} />)
    revealAndMount()

    const src = getIframe().getAttribute('src') ?? ''
    expect(new URL(src).searchParams.get('theme')).toBe('light')
  })

  test('preserves other query params, including valueless ones like kiosk', () => {
    render(<EmbedTile config={{ title: 'Weather', url: grafanaUrl }} editing={false} isDarkTheme />)
    revealAndMount()

    const src = getIframe().getAttribute('src') ?? ''
    const params = new URL(src).searchParams
    expect(params.get('orgId')).toBe('1')
    expect(params.get('panelId')).toBe('panel-1')
    expect(params.has('kiosk')).toBe(true)
  })

  test('passes a URL with no theme param through byte-for-byte unchanged', () => {
    render(<EmbedTile config={config} editing={false} isDarkTheme />)
    revealAndMount()

    const src = getIframe().getAttribute('src') ?? ''
    expect(src).toBe(config.url)
  })

  test('omitting isDarkTheme behaves as light mode', () => {
    const darkUrl = grafanaUrl.replace('theme=light', 'theme=dark')
    render(<EmbedTile config={{ title: 'Weather', url: darkUrl }} editing={false} />)
    revealAndMount()

    const src = getIframe().getAttribute('src') ?? ''
    expect(new URL(src).searchParams.get('theme')).toBe('light')
  })
})

describe('EmbedTile lazy loading', () => {
  test('does not mount an iframe before the tile intersects the viewport', () => {
    render(<EmbedTile config={config} editing={false} />)

    expect(document.querySelector('iframe')).toBeNull()
  })

  test('does not mount the iframe on intersection alone — it waits out the idle deferral first', () => {
    render(<EmbedTile config={config} editing={false} />)

    act(() => {
      FakeIntersectionObserver.instances.at(-1)!.intersect(true)
    })
    expect(document.querySelector('iframe')).toBeNull()

    act(() => {
      vi.advanceTimersByTime(MOUNT_IDLE_TIMEOUT_MS)
    })
    expect(getIframe()).toHaveAttribute('src', config.url)
  })

  test('sets loading=lazy on the frameless frame too', () => {
    render(<EmbedTile config={{ ...config, frameless: true }} editing={false} />)
    revealAndMount()

    expect(getIframe()).toHaveAttribute('loading', 'lazy')
  })

  test('observes the tile with a 200px rootMargin so loading starts just before it is visible', () => {
    render(<EmbedTile config={config} editing={false} />)

    expect(FakeIntersectionObserver.instances).toHaveLength(1)
    expect(FakeIntersectionObserver.instances[0].options?.rootMargin).toBe('200px')
    expect(FakeIntersectionObserver.instances[0].observedElements).toEqual([
      screen.getByTestId('embed-frame-container'),
    ])
  })

  test('keeps the iframe mounted after the tile scrolls back out of view', () => {
    render(<EmbedTile config={config} editing={false} />)
    revealAndMount()
    const frame = getIframe()

    // Fire a non-intersecting report by hand: useInView disconnects its
    // observer on first sight, but the underlying object can still be
    // invoked directly, the same way a real observer could in principle
    // report `isIntersecting: false` for an element it's about to drop.
    act(() => {
      FakeIntersectionObserver.instances.at(-1)!.intersect(false)
    })

    expect(document.querySelector('iframe')).toBe(frame)
  })

  test('disconnects the intersection observer on unmount before it ever intersects', () => {
    const { unmount } = render(<EmbedTile config={config} editing={false} />)
    const observer = FakeIntersectionObserver.instances.at(-1)!

    unmount()

    expect(observer.disconnected).toBe(true)
  })

  test('shows a placeholder that keeps the tile structure and names the embed before it mounts', () => {
    render(<EmbedTile config={config} editing={false} />)

    // Tile chrome (header, title) is unaffected by the mount gate.
    expect(screen.getByText('Windrose')).toBeInTheDocument()
    expect(document.querySelector('iframe')).toBeNull()

    // The placeholder fills the same slot the iframe will occupy, sized the
    // same way, and names the embed the way the "No URL configured" zero
    // state already names its own condition.
    const container = screen.getByTestId('embed-frame-container')
    expect(container.className).toContain('min-h-[240px]')
    expect(container).toHaveTextContent('Loading Windrose...')
  })

  test('shows the placeholder in frameless mode too, since there is no header to fall back on', () => {
    render(<EmbedTile config={{ ...config, frameless: true }} editing={false} />)

    const container = screen.getByTestId('embed-frame-container')
    expect(container).toHaveTextContent('Loading Windrose...')
  })
})
