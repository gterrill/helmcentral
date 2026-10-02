import { vi } from 'vitest'

/**
 * Replaces the global Image so any image given a src "loads" a moment later at
 * the stated natural size, as a real plan image would. jsdom never loads
 * images, so the deck plan components would otherwise never leave their
 * waiting state. A src containing "broken" fires onerror instead.
 */
export function stubImageSize(width: number, height: number) {
  class FakeImage {
    onload: (() => void) | null = null
    onerror: (() => void) | null = null
    naturalWidth = width
    naturalHeight = height
    set src(value: string) {
      queueMicrotask(() => {
        if (value.includes('broken')) this.onerror?.()
        else this.onload?.()
      })
    }
  }
  vi.stubGlobal('Image', FakeImage)
}

/** Makes every element report this rendered width, so label scaling can be tested. */
export function stubClientWidth(width: number) {
  Object.defineProperty(Element.prototype, 'clientWidth', { configurable: true, get: () => width })
}
