import '@testing-library/jest-dom'
import { beforeEach } from 'vitest'
import { setViewportWidth } from './viewport'

// jsdom doesn't implement the Popover API. The real @oddbird/popover-polyfill
// (used in production) depends on document.adoptedStyleSheets, which jsdom
// doesn't support either, so stub the two methods components actually call.
if (typeof HTMLElement.prototype.showPopover !== 'function') {
  HTMLElement.prototype.showPopover = function () {}
  HTMLElement.prototype.hidePopover = function () {}
}

// jsdom doesn't implement ResizeObserver; stub it so components that use it
// (e.g. for responsive scaling) can mount in tests.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

// jsdom doesn't implement Element.scrollIntoView; stub it so components that
// use it (e.g. to scroll a panel back into view on selection change) can
// mount in tests.
if (typeof Element.prototype.scrollIntoView === 'undefined') {
  Element.prototype.scrollIntoView = () => {}
}

// Neither jsdom nor happy-dom implements window.confirm/alert (both are
// `undefined`, not e.g. a no-op returning false), so any test that spies on
// them with vi.spyOn needs a real function there to spy on first.
if (typeof window.confirm === 'undefined') {
  window.confirm = () => false
}
if (typeof window.alert === 'undefined') {
  window.alert = () => {}
}

// Unlike jsdom, happy-dom implements Element.getAnimations() (returning an
// empty array rather than throwing or being absent). Base UI's exit-animation
// completion hook (useAnimationsFinished, used by Dialog/AlertDialog/Tabs to
// keep the outgoing element mounted until its close transition finishes)
// explicitly treats a missing getAnimations as "finish synchronously" and its
// presence as "wait a requestAnimationFrame plus a Promise.all([]).then(...)
// microtask before unmounting". Under jsdom every Base UI close/tab-switch is
// synchronous, which is what this suite's fireEvent-based tests assume; under
// happy-dom the outgoing panel/dialog is still in the DOM (mid `data-ending-
// style`) when the assertion runs immediately after. Force the API away so
// both environments take the synchronous fallback Base UI already ships for
// browsers that lack it, rather than rewriting every such test to await a
// frame it doesn't otherwise care about.
if (typeof Element.prototype.getAnimations === 'function') {
  // @ts-expect-error - deliberately removing the API, not adding a stub
  delete Element.prototype.getAnimations
}

// jsdom doesn't implement EventSource, which the shared telemetry stream
// (ADR 0037) opens as soon as any telemetry hook mounts — so without this every
// test that renders App throws ReferenceError. The stub records listeners and
// never connects; tests that need to drive events construct their own.
if (typeof globalThis.EventSource === 'undefined') {
  globalThis.EventSource = class {
    static readonly CONNECTING = 0
    static readonly OPEN = 1
    static readonly CLOSED = 2

    readyState = 0
    onerror: ((this: EventSource, ev: Event) => unknown) | null = null

    addEventListener() {}
    removeEventListener() {}
    dispatchEvent() { return false }
    close() { this.readyState = 2 }
  } as unknown as typeof EventSource
}

// Unlike jsdom's WebSocket, which never actually opens a socket in this
// setup, happy-dom's WebSocket dials a real connection. use-radar-echo-
// stream.ts opens one as soon as a radar tile mounts, so any test that
// mounts one incidentally (a full dashboard render, say) made a real,
// slow, non-deterministic outbound connection attempt to a host nothing in
// the test run is listening on. Force it off (not guarded on `undefined`,
// since happy-dom's is a real function) to an inert stub that never
// connects. use-radar-echo-stream.test.ts installs its own fake via
// vi.stubGlobal('WebSocket', ...) before each of its tests, which replaces
// this one.
if (typeof globalThis.WebSocket === 'function') {
  globalThis.WebSocket = class {
    static readonly CONNECTING = 0
    static readonly OPEN = 1
    static readonly CLOSING = 2
    static readonly CLOSED = 3

    readyState = 0
    binaryType: 'blob' | 'arraybuffer' = 'blob'

    constructor(public url: string) {}

    addEventListener() {}
    removeEventListener() {}
    dispatchEvent() { return false }
    send() {}
    close() { this.readyState = 3 }
  } as unknown as typeof WebSocket
}

// jsdom doesn't implement window.matchMedia or window.innerWidth changes; stub both via
// setViewportWidth so components that key off viewport width (useIsMobile, the RGL vs.
// stacked-list split in DashboardBentoGrid) can mount and react in tests. 1280 matches
// react-grid-layout's own WidthProvider fallback width and the product's primary target
// (a helm touchscreen) — comfortably above the `lg` (1024) grid breakpoint and the `md`
// (768) mobile-sidebar breakpoint, so the desktop grid and desktop sidebar are the default
// rendered in every test unless a test opts into a narrower viewport itself.
beforeEach(() => {
  setViewportWidth(1280)
})

// jsdom implements <canvas> but never a rendering context unless the
// `canvas` npm package is installed, so getContext('webgl2') returns null
// here by default. lib/webgl.ts's hasWebGL2() probes exactly that before a
// map-bearing tile mounts a map (ADR 0089 §11) — without this stub every
// existing map test would silently fall onto the no-WebGL2 fallback panel
// instead of the map it means to test. Stub a truthy context so the suite
// defaults to "this browser can render maps", matching every real browser
// these tiles ship to; a test exercising the fallback itself overrides
// hasWebGL2 (webgl.test.ts stubs getContext directly; tile tests mock the
// '@/lib/webgl' module). Only 'webgl2' is intercepted — any other context
// id (e.g. echo-canvas.ts's '2d') falls through to jsdom's own behaviour.
if (typeof HTMLCanvasElement.prototype.getContext === 'function') {
  const originalGetContext = HTMLCanvasElement.prototype.getContext
  HTMLCanvasElement.prototype.getContext = function (
    this: HTMLCanvasElement,
    contextId: string,
    ...rest: unknown[]
  ) {
    if (contextId === 'webgl2') return {} as unknown as WebGL2RenderingContext
    return (originalGetContext as (...args: unknown[]) => unknown).call(this, contextId, ...rest)
  } as typeof HTMLCanvasElement.prototype.getContext
}

// jsdom keeps the URL across tests within a file, and App now reads it on
// mount to seed a deep link (ADR 0074) — without this, a Forecast click in
// one test (which pushes /forecast onto the shared jsdom location) would
// mount the next test's <App> already on the Forecast panel instead of the
// dashboard.
beforeEach(() => {
  window.history.replaceState({}, '', '/')
})
