import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { DeckPlan } from '@/components/inventory/deck-plan'
import type { InventoryDeck, InventoryZone } from '@/hooks/use-inventory'
import { stubClientWidth, stubImageSize } from './stub-image'

const deck: InventoryDeck = { id: 'd1', name: 'Main deck', sort_index: 0, plan_document_id: 'doc1' }

const zones: InventoryZone[] = [
  {
    id: 'z1', name: 'Engine room', sort_index: 0, deck_id: 'd1',
    polygon: [[0.1, 0.2], [0.5, 0.2], [0.5, 0.6]],
    bins: [
      { id: 'b1', zone_id: 'z1', code: 'ENG-01', name: '', sort_index: 0, pin: { x: 0.3, y: 0.3 } },
      { id: 'b2', zone_id: 'z1', code: 'ENG-02', name: '', sort_index: 1, pin: null },
    ],
  },
  { id: 'z2', name: 'Salon', sort_index: 1, deck_id: 'd2', polygon: [[0, 0], [1, 0], [1, 1]], bins: [] },
  { id: 'z3', name: 'Lazarette', sort_index: 2, deck_id: null, polygon: null, bins: [] },
]

beforeEach(() => {
  stubImageSize(2000, 1000)
  stubClientWidth(0)
})
afterEach(() => {
  vi.unstubAllGlobals()
  delete (Element.prototype as unknown as { clientWidth?: number }).clientWidth
})

describe('DeckPlan', () => {
  it('draws nothing until the plan image has loaded', () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} />)
    expect(container.querySelector('svg')).toBeNull()
  })

  it('sizes the drawing to the image once it loads', async () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} />)
    await screen.findByTestId('deck-plan-svg')
    const svg = container.querySelector('svg')!
    expect(svg).toHaveAttribute('viewBox', '0 0 2000 1000')
    expect(container.querySelector('image')).toHaveAttribute('href', expect.stringContaining('/api/documents/doc1/content'))
  })

  it('says so when the image will not load', async () => {
    render(<DeckPlan deck={{ ...deck, plan_document_id: 'broken' }} zones={zones} />)
    expect(await screen.findByText(/plan image could not be loaded/i)).toBeInTheDocument()
  })

  it('draws only the zones outlined on this deck, in image pixels', async () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} />)
    await screen.findByTestId('deck-plan-svg')
    const polygons = container.querySelectorAll('polygon')
    expect(polygons).toHaveLength(1)
    expect(polygons[0]).toHaveAttribute('points', '200,200 1000,200 1000,600')
  })

  it('pins only the bins that have a pin', async () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} />)
    await screen.findByTestId('deck-plan-svg')
    const circles = container.querySelectorAll('circle')
    expect(circles).toHaveLength(1)
    expect(circles[0]).toHaveAttribute('cx', '600')
    expect(circles[0]).toHaveAttribute('cy', '300')
  })

  it('makes zones and pins links with accessible names when handlers are given', async () => {
    const onOpenZone = vi.fn()
    const onOpenBin = vi.fn()
    render(<DeckPlan deck={deck} zones={zones} onOpenZone={onOpenZone} onOpenBin={onOpenBin} />)
    const zone = await screen.findByRole('link', { name: 'Engine room, 2 bins' })
    expect(zone).toHaveAttribute('href', '/inventory/locations/z1')
    fireEvent.click(zone)
    expect(onOpenZone).toHaveBeenCalledWith('z1')

    const pin = screen.getByRole('link', { name: 'Bin ENG-01' })
    expect(pin).toHaveAttribute('href', '/inventory/bins/ENG-01')
    fireEvent.click(pin)
    expect(onOpenBin).toHaveBeenCalledWith('ENG-01')
  })

  it('uses the singular for one bin', async () => {
    const one = [{ ...zones[0], bins: [zones[0].bins[0]] }]
    render(<DeckPlan deck={deck} zones={one} onOpenZone={vi.fn()} />)
    expect(await screen.findByRole('link', { name: 'Engine room, 1 bin' })).toBeInTheDocument()
  })

  it('has no links at all without handlers', async () => {
    render(<DeckPlan deck={deck} zones={zones} />)
    await screen.findByTestId('deck-plan-svg')
    expect(screen.queryAllByRole('link')).toHaveLength(0)
  })

  it('marks the highlighted zone and bin', async () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} highlightZoneId="z1" highlightBinId="b1" />)
    await screen.findByTestId('deck-plan-svg')
    expect(container.querySelector('polygon')).toHaveAttribute('data-highlighted', 'true')
    expect(container.querySelector('circle')).toHaveAttribute('data-highlighted', 'true')
  })

  it('colours from theme tokens, never raw colours', async () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} />)
    await screen.findByTestId('deck-plan-svg')
    expect(container.querySelector('polygon')!.getAttribute('stroke')).toContain('var(--primary)')
    expect(container.querySelector('polygon')!.getAttribute('fill')).toContain('var(--primary)')
    for (const t of container.querySelectorAll('text')) expect(t.getAttribute('fill')).toContain('var(--')
  })

  it('holds labels at a constant on-screen size at any plan width', async () => {
    stubClientWidth(500)
    const { container } = render(<DeckPlan deck={deck} zones={zones} />)
    await screen.findByTestId('deck-plan-svg')
    // 2000 plan pixels shown in 500 screen pixels: 4 plan pixels per screen pixel.
    const zoneLabel = [...container.querySelectorAll('text')].find((t) => t.textContent === 'Engine room')!
    expect(zoneLabel).toHaveAttribute('font-size', '48')
    const pinLabel = [...container.querySelectorAll('text')].find((t) => t.textContent === 'ENG-01')!
    expect(pinLabel).toHaveAttribute('font-size', '40')
  })

  it('labels only the highlighted pin when pin labels are off', async () => {
    const { container } = render(<DeckPlan deck={deck} zones={zones} showPinLabels={false} highlightBinId="b1" />)
    await screen.findByTestId('deck-plan-svg')
    expect([...container.querySelectorAll('text')].some((t) => t.textContent === 'ENG-01')).toBe(true)
    const none = render(<DeckPlan deck={deck} zones={zones} showPinLabels={false} />)
    await screen.findAllByTestId('deck-plan-svg')
    expect(none.container.querySelectorAll('text').length).toBe(1)
  })
})
