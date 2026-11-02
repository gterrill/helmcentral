import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { MapLabel } from '@/components/passages/map-label'
import { MAP_LABEL, mapLabelBox } from '@/lib/map-label'
import { CARD_FADE_S } from '@/lib/replay-signs'
import { PASSAGE_LABEL_S, passageLabelBox } from '@/lib/replay-passage-label'
import { stayLabelBox } from '@/lib/replay-stay'
import { STAY_S } from '@/lib/replay-time'

const size = { w: 1000, h: 600 }
const stayText = '2 nights · Blue Pearl Bay'
const passageText = 'Day 3 · Funnel Bay → Thomas Island'

describe('mapLabelBox', () => {
  it('sits low, small, and inside the map, in the middle or at the left', () => {
    const centre = mapLabelBox(size, { align: 'center', elapsedS: 1, durationS: 2 })
    const left = mapLabelBox(size, { align: 'left', elapsedS: 1, durationS: 2 })
    expect(centre.x).toBe(500)
    expect(left.x).toBe(MAP_LABEL.edge)
    expect(centre.y).toBe(size.h - MAP_LABEL.aboveFoot)
    expect(left.y).toBe(centre.y)
    expect(centre.fontPx).toBeLessThanOrEqual(14)
    expect(centre.maxW).toBe(size.w - 2 * MAP_LABEL.edge)
  })

  it('fades in and out over the time it is up, and is whole between', () => {
    const at = (e: number) => mapLabelBox(size, { align: 'left', elapsedS: e, durationS: 2 }).alpha
    expect(at(0)).toBe(0)
    expect(at(CARD_FADE_S / 2)).toBeCloseTo(0.5)
    expect(at(1)).toBe(1)
    expect(at(2 - CARD_FADE_S / 2)).toBeCloseTo(0.5)
    expect(at(2)).toBe(0)
    expect(at(-1)).toBe(0)
    expect(at(9)).toBe(0)
  })

  it('keeps clear of the overview map in a bottom corner of a narrow map, and not for a top one', () => {
    const narrow = { w: 400, h: 600 }
    const none = mapLabelBox(narrow, { align: 'left', elapsedS: 1, durationS: 2 })
    const bl = mapLabelBox(narrow, { align: 'center', elapsedS: 1, durationS: 2, insetCorner: 'bottom-left' })
    const br = mapLabelBox(narrow, { align: 'center', elapsedS: 1, durationS: 2, insetCorner: 'bottom-right' })
    expect(bl.x - bl.maxW / 2).toBeGreaterThanOrEqual(8 + 176)
    expect(bl.x + bl.maxW / 2).toBeLessThanOrEqual(narrow.w - MAP_LABEL.edge)
    expect(br.x + br.maxW / 2).toBeLessThanOrEqual(narrow.w - 8 - 176)
    expect(mapLabelBox(narrow, { align: 'left', elapsedS: 1, durationS: 2, insetCorner: 'top-left' })).toEqual(none)
    expect(mapLabelBox({ w: 200, h: 400 }, { align: 'center', elapsedS: 1, durationS: 2, insetCorner: 'bottom-left' }).maxW).toBe(0)
  })

  it('is one label for both: the stay\'s centred for its length, the passage\'s at the left for its own, on the one line', () => {
    expect(stayLabelBox(size, 0.5)).toEqual(mapLabelBox(size, { align: 'center', elapsedS: 0.5, durationS: STAY_S }))
    expect(passageLabelBox(size, 0.5)).toEqual(mapLabelBox(size, { align: 'left', elapsedS: 0.5, durationS: PASSAGE_LABEL_S }))
    expect(stayLabelBox(size, 0.5).y).toBe(passageLabelBox(size, 0.5).y)
  })
})

describe('MapLabel', () => {
  it('says the words where the box puts them: centred on its middle', () => {
    const box = stayLabelBox(size, STAY_S / 2)
    render(<MapLabel testId="stay-label" text={stayText} box={box} />)
    const label = screen.getByText(stayText)
    expect(label).toHaveStyle({ left: `${box.x}px`, top: `${box.y}px`, fontSize: `${box.fontPx}px`, opacity: '1', transform: 'translateX(-50%)' })
    expect(label.style.fontWeight).toBe(String(MAP_LABEL.weight))
  })

  it('or from its left edge', () => {
    const box = passageLabelBox(size, 1)
    render(<MapLabel testId="passage-label" text={passageText} box={box} />)
    const label = screen.getByText(/Day 3/)
    expect(label).toHaveStyle({ left: `${box.x}px`, top: `${box.y}px` })
    expect(label.style.transform).toBe('')
  })

  it('lets touches through to the map', () => {
    render(<MapLabel testId="stay-label" text={stayText} box={stayLabelBox(size, 0.75)} />)
    expect(screen.getByTestId('stay-label').className).toContain('pointer-events-none')
  })

  it('shows it faded, and is placed again where a new box says', () => {
    const { rerender } = render(<MapLabel testId="x" text={stayText} box={stayLabelBox(size, 0)} />)
    expect(screen.getByText(/Blue Pearl Bay/)).toHaveStyle({ opacity: '0' })
    const resized = stayLabelBox({ w: 900, h: 500 }, STAY_S / 2, 'bottom-left')
    rerender(<MapLabel testId="x" text={stayText} box={resized} />)
    expect(screen.getByText(/Blue Pearl Bay/)).toHaveStyle({ left: `${resized.x}px`, top: `${resized.y}px`, maxWidth: `${resized.maxW}px`, opacity: '1' })
  })
})
