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
