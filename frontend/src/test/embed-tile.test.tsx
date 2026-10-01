import { act, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'

import { DisplayShell } from '@/components/display-shell'
import { EmbedTile, MOUNT_IDLE_TIMEOUT_MS } from '@/components/embed-tile'
import type { Display } from '@/lib/displays'

// DisplayShell pulls in the shared telemetry stream and the pixel-shift/wake-
// lock hooks for its own status badge; mocked the same way display-
// shell.test.tsx does, so mounting it here doesn't open a real EventSource
// or start real interval/wake-lock timers.
vi.mock('@/hooks/use-telemetry-stream', () => ({
  useTelemetryStatus: () => 'connected' as const,
}))

const { mockPixelShift, mockWakeLock } = vi.hoisted(() => ({
  mockPixelShift: vi.fn(() => ({ dx: 0, dy: 0 })),
  mockWakeLock: vi.fn(() => ({ status: 'off' as const })),
}))
vi.mock('@/hooks/use-pixel-shift', () => ({ usePixelShift: mockPixelShift }))
vi.mock('@/hooks/use-screen-wake-lock', () => ({ useScreenWakeLock: mockWakeLock }))
// The shell also reports its viewport to the backend. Unmocked, that PUT goes
// to a server that isn't there, and its failure logs after the file tears down.
vi.mock('@/hooks/use-display-viewport', () => ({
  useDisplayViewport: () => ({ w: window.innerWidth, h: window.innerHeight }),
}))

function display(overrides: Partial<Display> = {}): Display {
  return {
    id: 'd1',
    name: 'Flybridge',
    slug: 'flybridge',
    width: 1920,
    height: 360,
    scale: 1,
    rotate: 0,
    pixel_shift: false,
    wake_lock: false,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...overrides,
  }
}

const config = { title: 'Windrose', url: 'http://boat.local:3000/d-solo/abc?panelId=2' }

function getIframe(): HTMLIFrameElement {
  const frame = document.querySelector('iframe')
  if (!frame) throw new Error('expected an iframe to be rendered')
  return frame
}

// Mocks just enough of a browser's box model - offsetWidth/offsetHeight and
// getBoundingClientRect, both from the same box - for EmbedFrame's
// positioning/clipping math to have real numbers to work with. happy-dom
// implements no layout at all, so every element's rect and
// offsetWidth/offsetHeight are 0 by default (display-shell.test.tsx's own
// mock for the inner canvas does the same thing for the same reason).
// Deliberately keeps offsetWidth/offsetHeight equal to the rect's own
// width/height (i.e. assumes scale 1 for the box being mocked), so a
// caller's expected left/top collapses to plain box.left/box.top.
function mockBox(el: Element, box: { top: number; left: number; right: number; bottom: number }) {
  const width = box.right - box.left
  const height = box.bottom - box.top
  el.getBoundingClientRect = () =>
    ({
      top: box.top,
      left: box.left,
      right: box.right,
      bottom: box.bottom,
      width,
      height,
      x: box.left,
      y: box.top,
      toJSON() {},
    }) as DOMRect
  Object.defineProperty(el, 'offsetWidth', { configurable: true, value: width })
  Object.defineProperty(el, 'offsetHeight', { configurable: true, value: height })
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

  // F-3 (security audit): "no-referrer-when-downgrade" sent this page's full
  // URL — including a deep-link path like /dashboard/<page id> — as the
  // Referer header on every request an http-served embed makes. Nothing
  // about the embed needs that.
  test('sets referrerPolicy to no-referrer so the dashboard URL is never leaked to the embed', () => {
    render(<EmbedTile config={config} editing={false} />)
    revealAndMount()

    expect(getIframe()).toHaveAttribute('referrerpolicy', 'no-referrer')
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
    expect(frame).toHaveAttribute('referrerpolicy', 'no-referrer')
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

// WPE WebKit 2.44.1 (the flybridge kiosk browser) can't composite a
// multipart MJPEG <img> inside this iframe when it sits under a CSS-
// transformed ancestor - which is exactly what display-shell.tsx's rotated
// outer box / scaled inner box give it. Verified on-device, the workaround
// is to portal the iframe onto document.body (no transformed ancestor) and
// apply the shell's own rotation/scale to the iframe itself instead.
describe('EmbedTile inside a DisplayShell (WPE compositing workaround)', () => {
  test('outside a DisplayShell, the iframe stays inline inside embed-frame-container', () => {
    render(<EmbedTile config={config} editing={false} />)
    revealAndMount()

    const container = screen.getByTestId('embed-frame-container')
    const frame = getIframe()
    expect(container).toContainElement(frame)
    expect(screen.queryByTestId('embed-frame-portal')).toBeNull()
  })

  test('inside a rotated DisplayShell, the iframe is portalled to document.body, rotated and scaled to match', () => {
    render(
      <DisplayShell display={display({ rotate: 180, scale: 1.5 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const outer = screen.getByTestId('display-shell-outer')
    const portalFrame = screen.getByTestId('embed-frame-portal')

    expect(outer).not.toContainElement(portalFrame)
    expect(portalFrame.parentElement).toBe(document.body)
    expect(portalFrame.style.transform).toContain('rotate(180deg)')
    expect(portalFrame.style.transform).toContain('scale(')
  })

  test('inside a DisplayShell with rotate 0, the iframe is still portalled but gets no rotation', () => {
    render(
      <DisplayShell display={display({ rotate: 0, scale: 1.2 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const portalFrame = screen.getByTestId('embed-frame-portal')
    expect(portalFrame.parentElement).toBe(document.body)
    expect(portalFrame.style.transform).not.toContain('rotate(')
    expect(portalFrame.style.transform).toContain('scale(')
  })

  test('removes the portalled iframe from document.body on unmount', () => {
    const { unmount } = render(
      <DisplayShell display={display({ rotate: 180 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()
    expect(screen.getByTestId('embed-frame-portal')).toBeInTheDocument()

    unmount()

    expect(document.querySelector('[data-testid="embed-frame-portal"]')).toBeNull()
  })

  // No z-index (or a z-0) is load-bearing: display-shell.tsx's overlay
  // layer is given an explicit z-10 specifically so it paints above this,
  // and an explicit z-index here would fight that ordering.
  test('sets no z-index on the portalled iframe, so the overlay layer above it wins the stacking order', () => {
    render(
      <DisplayShell display={display({ rotate: 180 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    expect(screen.getByTestId('embed-frame-portal').style.zIndex).toBe('')
  })
})

// react-grid-layout moves a tile by writing `transform`/`top`/`left` (or a
// class toggle mid-drag) onto the grid-item element - never by resizing the
// tile's own container - and a sibling growing (e.g. the hero row) shifts
// everything below it the same way. Neither is caught by a ResizeObserver
// on the container alone, so EmbedFrame also watches every ancestor between
// the container and the shell's own inner box.
describe('EmbedTile portal follows a tile that moves without resizing', () => {
  class FakeResizeObserver {
    static instances: FakeResizeObserver[] = []
    readonly callback: ResizeObserverCallback
    observed: Element[] = []
    constructor(callback: ResizeObserverCallback) {
      this.callback = callback
      FakeResizeObserver.instances.push(this)
    }
    observe(target: Element) {
      this.observed.push(target)
    }
    unobserve(target: Element) {
      this.observed = this.observed.filter((el) => el !== target)
    }
    disconnect() {}
  }

  class FakeMutationObserver {
    static instances: FakeMutationObserver[] = []
    readonly callback: MutationCallback
    observedTargets: Node[] = []
    observedOptions: (MutationObserverInit | undefined)[] = []
    constructor(callback: MutationCallback) {
      this.callback = callback
      FakeMutationObserver.instances.push(this)
    }
    observe(target: Node, options?: MutationObserverInit) {
      this.observedTargets.push(target)
      this.observedOptions.push(options)
    }
    disconnect() {}
    takeRecords(): MutationRecord[] {
      return []
    }
    trigger() {
      this.callback([] as unknown as MutationRecord[], this as unknown as MutationObserver)
    }
  }

  test('registers the container and every ancestor up to (not including) the shell inner box with the ResizeObserver', () => {
    FakeResizeObserver.instances = []
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)

    render(
      <DisplayShell display={display({ rotate: 180 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const container = screen.getByTestId('embed-frame-container')
    const shellInner = screen.getByTestId('display-shell-inner')
    const expectedAncestors: Element[] = []
    for (let node = container.parentElement; node && node !== shellInner; node = node.parentElement) {
      expectedAncestors.push(node)
    }
    // Sanity: the Tile chrome between the container and the shell's inner
    // box really does give this test something to walk, so this isn't
    // passing vacuously on an empty list either way.
    expect(expectedAncestors.length).toBeGreaterThan(0)

    const observer = FakeResizeObserver.instances.at(-1)!
    expect(new Set(observer.observed)).toEqual(new Set([container, ...expectedAncestors]))
    expect(observer.observed).not.toContain(shellInner)
  })

  test('recomputes the portal position when a mutation on an ancestor reports a style/class change', () => {
    FakeMutationObserver.instances = []
    vi.stubGlobal('MutationObserver', FakeMutationObserver)

    render(
      <DisplayShell display={display({ rotate: 0 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const container = screen.getByTestId('embed-frame-container')
    mockBox(container, { top: 40, left: 60, right: 260, bottom: 160 })

    const mutationObserver = FakeMutationObserver.instances.at(-1)!
    expect(mutationObserver.observedOptions[0]).toEqual({
      attributes: true,
      attributeFilter: ['style', 'class'],
    })

    act(() => {
      mutationObserver.trigger()
    })

    const portalFrame = screen.getByTestId('embed-frame-portal')
    expect(portalFrame.style.left).toBe('60px')
    expect(portalFrame.style.top).toBe('40px')
  })

  // The guard that skips a redundant setState when reposition recomputes
  // the exact same style - without it, every ResizeObserver/MutationObserver
  // firing on an otherwise-static tile would still rewrite the portal's
  // style attribute. Watched here at the DOM level (a real MutationObserver
  // on the portal iframe itself) rather than by counting React renders:
  // returning the same style object from the state updater can still leave
  // React re-invoking the component, but React's own prop-diffing then
  // skips writing an unchanged `style` object to the DOM either way - which
  // is the actual, observable "churn" this guard exists to avoid.
  test('does not rewrite the portal iframe style attribute when a reposition trigger recomputes the exact same style', async () => {
    render(
      <DisplayShell display={display({ rotate: 180 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const container = screen.getByTestId('embed-frame-container')
    mockBox(container, { top: 10, left: 10, right: 210, bottom: 110 })
    act(() => {
      window.dispatchEvent(new Event('resize'))
    })

    const portalFrame = screen.getByTestId('embed-frame-portal')
    const mutations: MutationRecord[] = []
    const styleWatcher = new MutationObserver((records) => mutations.push(...records))
    styleWatcher.observe(portalFrame, { attributes: true, attributeFilter: ['style'] })

    // Same geometry again - nothing has actually moved.
    act(() => {
      window.dispatchEvent(new Event('resize'))
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(mutations).toHaveLength(0)

    // A genuine change still gets through the guard.
    mockBox(container, { top: 20, left: 20, right: 220, bottom: 120 })
    act(() => {
      window.dispatchEvent(new Event('resize'))
    })
    await act(async () => {
      await Promise.resolve()
    })
    expect(mutations.length).toBeGreaterThan(0)

    styleWatcher.disconnect()
  })

  // react-grid-layout slides a moved tile over a CSS transition, so the
  // style mutation fires at the start of the move, when the tile is still
  // where it was. The end of the slide has to be caught separately.
  // [P1 finding] Toggling the widget's `frameless` setting while the embed
  // is already mounted swaps EmbedTile's returned branch: the old
  // container div + EmbedFrame unmount and a *new* container div +
  // EmbedFrame mount, together, in the same commit. React attaches a host
  // node's ref only after its child subtree's own layout effects have
  // already run in that same commit, so EmbedFrame's containerRef.current
  // read used to see null forever - no portalStyle, no ResizeObserver
  // attached to notice anything later, and the iframe simply never comes
  // back.
  test('keeps rendering the portalled iframe after toggling frameless while the embed is already mounted', () => {
    const { rerender } = render(
      <DisplayShell display={display({ rotate: 180 })} alarms={[]}>
        <EmbedTile config={{ ...config, frameless: false }} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()
    expect(screen.getByTestId('embed-frame-portal')).toBeInTheDocument()

    act(() => {
      rerender(
        <DisplayShell display={display({ rotate: 180 })} alarms={[]}>
          <EmbedTile config={{ ...config, frameless: true }} editing={false} />
        </DisplayShell>,
      )
    })

    expect(screen.getByTestId('embed-frame-portal')).toBeInTheDocument()
  })

  test('recomputes the portal position when an ancestor finishes a transition', () => {
    render(
      <DisplayShell display={display({ rotate: 0 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const container = screen.getByTestId('embed-frame-container')
    mockBox(container, { top: 70, left: 90, right: 290, bottom: 190 })

    act(() => {
      container.parentElement!.dispatchEvent(new Event('transitionend', { bubbles: true }))
    })

    const portalFrame = screen.getByTestId('embed-frame-portal')
    expect(portalFrame.style.left).toBe('90px')
    expect(portalFrame.style.top).toBe('70px')
  })
})

// The hero row enlarges its tile with a transform of its own, on top of the
// display's scale. The portalled iframe must pick that up too, or a hero
// embed paints smaller than its frame.
describe('EmbedTile portal matches any extra ancestor scale', () => {
  test('scales by the on-screen size over the layout size, not the display scale alone', () => {
    render(
      <DisplayShell display={display({ rotate: 180, scale: 1 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const container = screen.getByTestId('embed-frame-container')
    // Laid out at 200x100, drawn at 230x115: a 1.15 hero enlargement.
    mockBox(container, { top: 0, left: 0, right: 230, bottom: 115 })
    Object.defineProperty(container, 'offsetWidth', { configurable: true, value: 200 })
    Object.defineProperty(container, 'offsetHeight', { configurable: true, value: 100 })

    act(() => {
      window.dispatchEvent(new Event('resize'))
    })

    const portalFrame = screen.getByTestId('embed-frame-portal')
    expect(portalFrame.style.width).toBe('200px')
    expect(portalFrame.style.transform).toBe('rotate(180deg) scale(1.15)')
    expect(portalFrame.style.left).toBe('15px')
  })
})

// The shell's outer box clips its own content with overflow-hidden, but a
// portalled iframe lives at document.body and escapes that clip entirely.
// EmbedFrame re-derives the same crop by hand from the outer box's and the
// container's own bounding rects.
describe('EmbedTile portal is cropped to the display outer box', () => {
  test('clips the sides that overflow the display, converted to local (unscaled) px', () => {
    render(
      <DisplayShell display={display({ rotate: 0, scale: 1 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const outer = screen.getByTestId('display-shell-outer')
    const container = screen.getByTestId('embed-frame-container')
    mockBox(outer, { top: 0, left: 0, right: 400, bottom: 300 })
    mockBox(container, { top: -8, left: -5, right: 420, bottom: 312 })

    act(() => {
      window.dispatchEvent(new Event('resize'))
    })

    expect(screen.getByTestId('embed-frame-portal').style.clipPath).toBe('inset(8px 20px 12px 5px)')
  })

  test('swaps top/bottom and left/right for a rotated display, since local top paints at screen bottom', () => {
    render(
      <DisplayShell display={display({ rotate: 180, scale: 1 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const outer = screen.getByTestId('display-shell-outer')
    const container = screen.getByTestId('embed-frame-container')
    mockBox(outer, { top: 0, left: 0, right: 400, bottom: 300 })
    mockBox(container, { top: -8, left: -5, right: 420, bottom: 312 })

    act(() => {
      window.dispatchEvent(new Event('resize'))
    })

    expect(screen.getByTestId('embed-frame-portal').style.clipPath).toBe('inset(12px 5px 8px 20px)')
  })

  test('omits clipPath entirely when the tile sits fully inside the display', () => {
    render(
      <DisplayShell display={display({ rotate: 0 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const outer = screen.getByTestId('display-shell-outer')
    const container = screen.getByTestId('embed-frame-container')
    mockBox(outer, { top: 0, left: 0, right: 400, bottom: 300 })
    mockBox(container, { top: 50, left: 50, right: 150, bottom: 150 })

    act(() => {
      window.dispatchEvent(new Event('resize'))
    })

    expect(screen.getByTestId('embed-frame-portal').style.clipPath).toBe('')
  })

  test('hides the frame entirely once the tile has scrolled fully outside the display', () => {
    render(
      <DisplayShell display={display({ rotate: 0 })} alarms={[]}>
        <EmbedTile config={config} editing={false} />
      </DisplayShell>,
    )
    revealAndMount()

    const outer = screen.getByTestId('display-shell-outer')
    const container = screen.getByTestId('embed-frame-container')
    mockBox(outer, { top: 0, left: 0, right: 400, bottom: 300 })
    mockBox(container, { top: 400, left: 400, right: 500, bottom: 500 })

    act(() => {
      window.dispatchEvent(new Event('resize'))
    })

    expect(screen.getByTestId('embed-frame-portal').style.visibility).toBe('hidden')
  })
})
