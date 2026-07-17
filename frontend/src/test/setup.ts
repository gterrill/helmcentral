import '@testing-library/jest-dom'

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

// jsdom doesn't implement window.matchMedia; stub it so components that use
// it (e.g. useIsMobile, used by the shadcn Sidebar) can mount in tests.
if (typeof window.matchMedia === 'undefined') {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  }) as unknown as MediaQueryList
}
