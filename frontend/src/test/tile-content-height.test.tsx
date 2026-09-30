import { render } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { Tile } from '@/components/ui/tile'
import { TileHeightScope } from '@/lib/tile-content-height'

let observerCb: (() => void) | null = null

beforeEach(() => {
  vi.stubGlobal('ResizeObserver', class {
    constructor(cb: () => void) { observerCb = cb }
    observe() {}
    disconnect() {}
  })
})
afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); observerCb = null })

function stubHeights(card: number, area: number, content: number) {
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
    if (this.dataset.slot === 'card') return card
    if (this.dataset.slot === 'card-content') return area
    if (this.classList.contains('flow-root')) return content
    return 0
  })
}

describe('a shrinkToContent tile', () => {
  it('reports the card chrome plus the content at natural height', () => {
    stubHeights(400, 350, 90)
    const report = vi.fn()
    render(
      <TileHeightScope id="t" onReport={report}>
        <Tile title="T" shrinkToContent><p>row</p></Tile>
      </TileHeightScope>,
    )
    // 400 card - 350 stretched area = 50 of chrome, plus 90 of content.
    expect(report).toHaveBeenLastCalledWith('t', 140)
  })

  it('reports the same height however tall the card has been stretched', () => {
    stubHeights(400, 350, 90)
    const report = vi.fn()
    render(
      <TileHeightScope id="t" onReport={report}>
        <Tile title="T" shrinkToContent><p>row</p></Tile>
      </TileHeightScope>,
    )
    stubHeights(700, 650, 90)
    observerCb?.()
    expect(report).toHaveBeenLastCalledWith('t', 140)
  })

  it('withdraws its report on unmount', () => {
    stubHeights(400, 350, 90)
    const report = vi.fn()
    const { unmount } = render(
      <TileHeightScope id="t" onReport={report}>
        <Tile title="T" shrinkToContent><p>row</p></Tile>
      </TileHeightScope>,
    )
    unmount()
    expect(report).toHaveBeenLastCalledWith('t', null)
  })

  it('a tile that did not opt in reports nothing', () => {
    stubHeights(400, 350, 90)
    const report = vi.fn()
    render(
      <TileHeightScope id="t" onReport={report}>
        <Tile title="T"><p>row</p></Tile>
      </TileHeightScope>,
    )
    expect(report).not.toHaveBeenCalled()
  })
})

describe('a shrunk tile whose list outgrows it', () => {
  // Lays out the way the browser does: the card keeps its grid height, and the
  // content area is flex-1, so it is card minus chrome unless its min-height is
  // auto, in which case it is pushed out to the content's own height.
  function stubLayout(cardPx: number, chrome: number, contentPx: number) {
    vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (this: HTMLElement) {
      if (this.dataset.slot === 'card') return cardPx
      if (this.dataset.slot === 'card-content') {
        const stretched = cardPx - chrome
        return this.classList.contains('min-h-0') ? stretched : Math.max(stretched, contentPx)
      }
      if (this.classList.contains('flow-root')) return contentPx
      return 0
    })
  }

  it('reports the taller need so the tile can grow back', () => {
    stubLayout(128, 50, 40)
    const report = vi.fn()
    render(
      <TileHeightScope id="t" onReport={report}>
        <Tile title="T" shrinkToContent><p>row</p></Tile>
      </TileHeightScope>,
    )
    expect(report).toHaveBeenLastCalledWith('t', 90)
    stubLayout(128, 50, 300)
    observerCb?.()
    expect(report).toHaveBeenLastCalledWith('t', 350)
  })
})
