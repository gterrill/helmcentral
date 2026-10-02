import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { useState } from 'react'
import { fireEvent, render, screen, within } from '@testing-library/react'
import DeckPlanEditorImpl from '@/components/inventory/deck-plan-editor-impl'
import { layoutFromZones, type LayoutDraft } from '@/lib/deck-layout'
import type { InventoryDeck, InventoryZone } from '@/hooks/use-inventory'
import { stubImageSize } from './stub-image'

const deck: InventoryDeck = { id: 'd1', name: 'Main deck', sort_index: 0, plan_document_id: 'doc1' }
const tri: Array<[number, number]> = [[0.1, 0.1], [0.5, 0.1], [0.3, 0.5]]

const zones: InventoryZone[] = [
  {
    id: 'z1', name: 'Engine room', sort_index: 0, deck_id: 'd1', polygon: tri,
    bins: [
      { id: 'b1', zone_id: 'z1', code: 'E1', name: '', sort_index: 0, pin: { x: 0.3, y: 0.2 } },
      { id: 'b2', zone_id: 'z1', code: 'E2', name: '', sort_index: 1, pin: null },
    ],
  },
  { id: 'z2', name: 'Salon', sort_index: 1, deck_id: 'd2', polygon: tri, bins: [] },
  { id: 'z3', name: 'Lazarette', sort_index: 2, deck_id: null, polygon: null, bins: [
    { id: 'b3', zone_id: 'z3', code: 'L1', name: '', sort_index: 0, pin: null },
  ] },
]

let layoutNow: LayoutDraft
function Harness({ initial }: { initial?: LayoutDraft }) {
  const [layout, setLayout] = useState<LayoutDraft>(initial ?? layoutFromZones(zones, 'd1'))
  layoutNow = layout
  return (
    <DeckPlanEditorImpl
      deck={deck}
      zones={zones}
      deckNames={{ d1: 'Main deck', d2: 'Lower deck' }}
      layout={layout}
      onChange={setLayout}
    />
  )
}

// 1000 x 500 screen pixels: clientX / 1000 and clientY / 500 are the fractions.
const rect = { left: 0, top: 0, width: 1000, height: 500, right: 1000, bottom: 500, x: 0, y: 0, toJSON: () => ({}) }

beforeEach(() => {
  stubImageSize(2000, 1000)
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue(rect as DOMRect)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function plan() {
  return screen.findByTestId('deck-plan-editor-svg')
}
const press = (el: Element, x: number, y: number) => fireEvent.pointerDown(el, { clientX: x, clientY: y, pointerId: 1, button: 0 })

describe('DeckPlanEditor selection list', () => {
  it('lists every location and marks one that is on another deck', async () => {
    render(<Harness />)
    await plan()
    expect(screen.getByRole('button', { name: /Engine room/ })).toBeInTheDocument()
    expect(screen.getByText('On Lower deck')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /Lazarette/ })).toBeInTheDocument()
  })

  it('shows the bins of the selected location', async () => {
    render(<Harness />)
    await plan()
    expect(screen.queryByRole('button', { name: /E1/ })).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    expect(screen.getByRole('button', { name: /E1/ })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /E2/ })).toBeInTheDocument()
  })
})

describe('drawing an outline', () => {
  it('adds a point for each tap and closes with Finish once there are three', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    const finish = screen.getByRole('button', { name: 'Finish outline' })
    expect(finish).toBeDisabled()
    press(svg, 100, 50)
    press(svg, 500, 50)
    expect(finish).toBeDisabled()
    press(svg, 300, 250)
    expect(finish).toBeEnabled()
    fireEvent.click(finish)
    expect(layoutNow.polygons.z3).toEqual([[0.1, 0.1], [0.5, 0.1], [0.3, 0.5]])
  })

  it('closes by tapping the first point', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    press(svg, 100, 50)
    press(svg, 500, 50)
    press(svg, 300, 250)
    press(screen.getByLabelText('First point, tap to close'), 100, 50)
    expect(layoutNow.polygons.z3).toHaveLength(3)
  })

  it('does not close on the first point with fewer than three points', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    press(svg, 100, 50)
    press(svg, 500, 50)
    press(screen.getByLabelText('First point, tap to close'), 100, 50)
    expect(layoutNow.polygons.z3).toBeUndefined()
  })

  it('Escape throws the half-drawn outline away', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    press(svg, 100, 50)
    press(svg, 500, 50)
    fireEvent.keyDown(svg, { key: 'Escape' })
    expect(screen.queryByLabelText('First point, tap to close')).not.toBeInTheDocument()
    expect(layoutNow.polygons.z3).toBeUndefined()
  })

  it('Backspace removes the last point while drawing', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    press(svg, 100, 50)
    press(svg, 500, 50)
    press(svg, 300, 250)
    fireEvent.keyDown(svg, { key: 'Backspace' })
    expect(screen.getByRole('button', { name: 'Finish outline' })).toBeDisabled()
  })

  it('keeps a half-drawn outline out of the layout until it is finished', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    press(svg, 100, 50)
    expect(layoutNow.polygons.z3).toBeUndefined()
  })
})

describe('editing an outline', () => {
  it('drags a point to a new place', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    press(screen.getByLabelText('Point 1 of Engine room'), 100, 50)
    fireEvent.pointerMove(svg, { clientX: 700, clientY: 100, pointerId: 1 })
    fireEvent.pointerUp(svg, { pointerId: 1 })
    expect(layoutNow.polygons.z1[0]).toEqual([0.7, 0.2])
    // Released: further movement does nothing.
    fireEvent.pointerMove(svg, { clientX: 900, clientY: 400, pointerId: 1 })
    expect(layoutNow.polygons.z1[0]).toEqual([0.7, 0.2])
  })

  it('keeps a dragged point on the plan', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    press(screen.getByLabelText('Point 1 of Engine room'), 100, 50)
    fireEvent.pointerMove(svg, { clientX: 1400, clientY: -80, pointerId: 1 })
    expect(layoutNow.polygons.z1[0]).toEqual([1, 0])
  })

  it('adds a point from a midpoint handle', async () => {
    render(<Harness />)
    await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    press(screen.getByLabelText('Add a point after point 1 of Engine room'), 300, 50)
    expect(layoutNow.polygons.z1).toHaveLength(4)
    expect(layoutNow.polygons.z1[1]).toEqual([0.3, 0.1])
  })

  it('removes the selected point with Delete, never below three', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    press(screen.getByLabelText('Add a point after point 1 of Engine room'), 300, 50)
    fireEvent.pointerUp(svg, { pointerId: 1 })
    expect(layoutNow.polygons.z1).toHaveLength(4)
    press(screen.getByLabelText('Point 2 of Engine room'), 300, 50)
    fireEvent.pointerUp(svg, { pointerId: 1 })
    fireEvent.keyDown(svg, { key: 'Delete' })
    expect(layoutNow.polygons.z1).toHaveLength(3)

    press(screen.getByLabelText('Point 1 of Engine room'), 100, 50)
    fireEvent.pointerUp(svg, { pointerId: 1 })
    fireEvent.keyDown(svg, { key: 'Delete' })
    expect(layoutNow.polygons.z1).toHaveLength(3)
  })

  it('nudges the selected point with the arrow keys', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    press(screen.getByLabelText('Point 1 of Engine room'), 100, 50)
    fireEvent.pointerUp(svg, { pointerId: 1 })
    fireEvent.keyDown(svg, { key: 'ArrowRight' })
    expect(layoutNow.polygons.z1[0][0]).toBeCloseTo(0.105)
  })

  it('removes the location from the plan along with its pins', async () => {
    render(<Harness />)
    await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove from plan' }))
    expect(layoutNow.polygons.z1).toBeUndefined()
    expect(layoutNow.pins.b1).toBeUndefined()
  })

  it('selects another outlined location by tapping its outline', async () => {
    const withTwo: LayoutDraft = { polygons: { z1: tri, z3: [[0.6, 0.6], [0.9, 0.6], [0.8, 0.9]] }, pins: {} }
    render(<Harness initial={withTwo} />)
    await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    press(screen.getByLabelText('Select Lazarette'), 800, 400)
    const list = within(screen.getByRole('list', { name: 'Locations' }))
    expect(list.getByRole('button', { name: /Lazarette/ })).toHaveAttribute('aria-pressed', 'true')
  })
})

describe('pinning bins', () => {
  it('drops a pin where the plan is tapped, and moves it on the next tap', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    fireEvent.click(screen.getByRole('button', { name: /E2/ }))
    press(svg, 400, 100)
    expect(layoutNow.pins.b2).toEqual({ x: 0.4, y: 0.2 })
    press(svg, 200, 300)
    expect(layoutNow.pins.b2).toEqual({ x: 0.2, y: 0.6 })
  })

  it('removes a pin', async () => {
    render(<Harness />)
    await plan()
    fireEvent.click(screen.getByRole('button', { name: /Engine room/ }))
    fireEvent.click(screen.getByRole('button', { name: /E1/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Remove pin' }))
    expect(layoutNow.pins.b1).toBeUndefined()
  })

  it('will not pin a bin whose location is not outlined, and says why', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    fireEvent.click(screen.getByRole('button', { name: /L1/ }))
    expect(screen.getByText(/Outline Lazarette on the plan before pinning its bins/)).toBeInTheDocument()
    press(svg, 400, 100)
    expect(layoutNow.pins.b3).toBeUndefined()
  })

  it('does not draw a point when a bin is being pinned', async () => {
    render(<Harness />)
    const svg = await plan()
    fireEvent.click(screen.getByRole('button', { name: /Lazarette/ }))
    fireEvent.click(screen.getByRole('button', { name: /L1/ }))
    press(svg, 400, 100)
    expect(screen.queryByLabelText('First point, tap to close')).not.toBeInTheDocument()
  })
})
