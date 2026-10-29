import { render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'

import { AssistantCard } from '@/components/assistant-card'
import { AssistantTileCard } from '@/components/assistant-tile-card'
import { AssistantTileCardFallback } from '@/components/assistant-tile-card-fallback'
import { TILE_CARD_HEADER_PX } from '@/lib/mate-tile-size'

import { tileCard } from './helpers/tile-card-fixture'

vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: 'metric' } }) }))

// The live tile reads the app-wide stores; these stand in for the boat now.
// The tile itself is the real one.
vi.mock('@/hooks/use-vessel-state', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/hooks/use-vessel-state')>()),
  useVesselFields: () => ({ depth: 7.7, depthLastUpdateAgeS: 0, navigationState: 'underway' }),
}))
vi.mock('@/hooks/use-depth-trend', () => ({ useDepthTrend: () => ({ points: [], since: 'window' }) }))
vi.mock('@/hooks/use-tide-today', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/hooks/use-tide-today')>()
  return { ...original, useTideToday: () => ({ tide: original.defaultTide, loading: false }) }
})

const clock = (iso: string) => new Date(iso).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })

describe('AssistantTileCard', () => {
  it('draws the live tile with a Live marker while its reply is the newest', async () => {
    render(<AssistantTileCard card={tileCard()} live />)
    expect(screen.getByText('Live')).toBeInTheDocument()
    // The live stores say 7.7 m; the snapshot's 3.3 m is not what a live card shows.
    await screen.findByText('Depth & Tide')
    expect(screen.getByTestId('assistant-tile-card').querySelector('p.font-display')?.textContent).toMatch(/^7\.7/)
  })

  it('draws the same tile on the stored figures, labelled with when they were read, once a newer reply exists', () => {
    render(<AssistantTileCard card={tileCard()} live={false} />)
    expect(screen.queryByText('Live')).toBeNull()
    expect(screen.getByText(clock('2026-10-28T04:03:00Z'))).toBeInTheDocument()
    expect(screen.getByText('Depth & Tide')).toBeInTheDocument()
    // The depth readout opens the first big figure on the tile.
    expect(screen.getByTestId('assistant-tile-card').querySelector('p.font-display')?.textContent).toMatch(/^3\.3/)
    expect(screen.getByText(/Cape Upstart/)).toBeInTheDocument()
    expect(screen.getByText('Rising')).toBeInTheDocument()
    expect(screen.getByText('2.00')).toBeInTheDocument()
    expect(screen.queryByText('7.7')).toBeNull()
  })

  it('shows dashes for a reading that was missing, as the dashboard tile does', () => {
    render(<AssistantTileCard card={tileCard({}, { depth_m: null, tide: null })} live={false} />)
    const tile = screen.getByTestId('assistant-tile-card')
    expect(within(tile).getByText('unavailable')).toBeInTheDocument()
    expect(within(tile).getAllByText('—').length).toBeGreaterThanOrEqual(2)
    expect(tile.textContent).not.toContain('NaN')
  })

  it('draws a card with no depth trend yet, with dashes where the readings are missing', () => {
    const card = tileCard({}, { depth_m: null, tide: null, depth_trend: { points: [], since: 'window' } })
    render(<AssistantTileCard card={card} live={false} />)
    expect(screen.getByTestId('assistant-tile-card').textContent).not.toContain('NaN')
    expect(screen.getByText('unavailable')).toBeInTheDocument()
  })

  it('holds the place of the lazy card at the card\'s own height, so the thread does not jump', () => {
    const { getByTestId } = render(<AssistantTileCardFallback tile="depth-tide" />)
    expect(getByTestId('assistant-tile-fallback')).toHaveStyle({ height: `${320 + TILE_CARD_HEADER_PX}px` })
    render(<AssistantTileCard card={tileCard()} live={false} />)
    const header = screen.getByTestId('assistant-tile-header')
    expect(header).toHaveStyle({ height: `${TILE_CARD_HEADER_PX}px` })
  })

  it('is read-only: no button to open anything', () => {
    render(<AssistantTileCard card={tileCard()} live={false} />)
    expect(screen.queryByRole('button')).toBeNull()
  })

  it('gives the tile the height it has on the dashboard by default', () => {
    render(<AssistantTileCard card={tileCard()} live={false} />)
    // 7 rows of 32px and 6 gaps of 16px.
    expect(screen.getByTestId('assistant-tile-box')).toHaveStyle({ height: '320px' })
  })

  it('is what AssistantCard draws for a tile card, passing the live flag on', async () => {
    const { rerender } = render(<AssistantCard card={tileCard()} liveTile />)
    expect(await screen.findByText('Live')).toBeInTheDocument()
    rerender(<AssistantCard card={tileCard()} liveTile={false} />)
    expect(await screen.findByText(clock('2026-10-28T04:03:00Z'))).toBeInTheDocument()
  })
})
