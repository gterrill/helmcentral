import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { StayLabel } from '@/components/passages/stay-label'
import { CARD_FADE_S } from '@/lib/replay-signs'
import { stayLabelBox, stayView, type ReplayStay, type StayView } from '@/lib/replay-stay'
import { STAY_S } from '@/lib/replay-time'

const stayOf = (label: string): ReplayStay => ({ id: 'stay:0', index: 0, clock: 0, at: 0, fromId: 'a', toId: 'b', arrival: 0, departure: 100, label })
const view = (progress: number, label = '2 nights · Blue Pearl Bay'): StayView => stayView(stayOf(label), progress * STAY_S)
const size = { w: 1000, h: 600 }

describe('StayLabel', () => {
  it('says the stay in words, where the pure layout puts it', () => {
    render(<StayLabel view={view(0.5)} container={size} />)
    const text = screen.getByText('2 nights · Blue Pearl Bay')
    const box = stayLabelBox(size, 0.5 * STAY_S)
    expect(text).toHaveStyle({ left: `${box.cx}px`, top: `${box.y}px`, fontSize: `${box.fontPx}px`, opacity: '1' })
  })

  it('lets touches through to the map', () => {
    render(<StayLabel view={view(0.5)} container={size} />)
    expect(screen.getByTestId('stay-label').className).toContain('pointer-events-none')
  })

  it('fades in at the start of the stay and out at its end', () => {
    const { rerender } = render(<StayLabel view={view(0)} container={size} />)
    expect(screen.getByText(/Blue Pearl Bay/)).toHaveStyle({ opacity: '0' })
    rerender(<StayLabel view={view(CARD_FADE_S / 2 / STAY_S)} container={size} />)
    expect(Number(screen.getByText(/Blue Pearl Bay/).style.opacity)).toBeCloseTo(0.5)
    rerender(<StayLabel view={view(1)} container={size} />)
    expect(screen.getByText(/Blue Pearl Bay/)).toHaveStyle({ opacity: '0' })
  })

  it('shows the time alone for a stay with no place', () => {
    render(<StayLabel view={view(0.5, '4 h')} container={size} />)
    expect(screen.getByText('4 h')).toBeInTheDocument()
  })

  it('draws nothing while the map cannot say how big it is', () => {
    render(<StayLabel view={view(0.5)} container={null} />)
    expect(screen.queryByTestId('stay-label')).toBeNull()
  })

  it('keeps clear of the overview map, and is placed again when the map is resized', () => {
    const narrow = { w: 400, h: 600 }
    const { rerender } = render(<StayLabel view={view(0.5)} container={narrow} insetCorner="bottom-left" />)
    const label = screen.getByText(/Blue Pearl Bay/)
    const box = stayLabelBox(narrow, 0.5 * STAY_S, 'bottom-left')
    expect(label).toHaveStyle({ left: `${box.cx}px`, maxWidth: `${box.maxW}px` })
    expect(box.cx - box.maxW / 2).toBeGreaterThanOrEqual(184)
    // The map is resized while the replay is paused in the stay: the label goes where the new size says.
    rerender(<StayLabel view={view(0.5)} container={{ w: 900, h: 500 }} insetCorner="bottom-left" />)
    const wide = stayLabelBox({ w: 900, h: 500 }, 0.5 * STAY_S, 'bottom-left')
    expect(screen.getByText(/Blue Pearl Bay/)).toHaveStyle({ left: `${wide.cx}px`, top: `${wide.y}px` })
  })
})
