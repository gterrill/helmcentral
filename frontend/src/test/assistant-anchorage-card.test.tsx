import { fireEvent, render, screen, within } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { AssistantAnchorageCard } from '@/components/assistant-anchorage-card'
import { AssistantCard } from '@/components/assistant-card'
import { decodeMateCards, type AnchorageCard } from '@/lib/mate-cards'

import { anchorageCard, anchorageCardWithErrors, anchorageCardWithoutDepth, anchoragePayload } from './helpers/anchorage-fixture'

// The map is MapLibre; its own files are under test elsewhere.
vi.mock('@/components/assistant-anchorage-map', () => ({
  AssistantAnchorageMap: ({ card }: { card: AnchorageCard }) => <div data-testid="anchorage-map-stub">{card.features.moorings.length} moorings</div>,
}))

const config = vi.hoisted(() => ({ distanceUnits: 'metric' }))
vi.mock('@/hooks/use-app-config', () => ({ useAppConfig: () => ({ ui: { distanceUnits: config.distanceUnits } }) }))

beforeEach(() => {
  config.distanceUnits = 'metric'
})

describe('AssistantAnchorageCard', () => {
  it('shows the stay in the anchorage\'s own time', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    expect(screen.getByTestId('anchorage-stay')).toHaveTextContent('Mon 2 Nov 14:00 to Tue 3 Nov 08:00')
    expect(screen.getByText('West Butterfly Bay')).toBeInTheDocument()
  })

  it('draws the shelter rose with 36 coloured sectors and an accessible summary', () => {
    const card = anchorageCard()
    render(<AssistantAnchorageCard card={card} />)
    const rose = screen.getByRole('img', { name: /Shelter by the direction the wind comes from/ })
    expect(rose.querySelectorAll('path[data-grade]')).toHaveLength(36)
    expect(rose.getAttribute('aria-label')).toContain(card.shelter.summary)
    const arcs = within(screen.getByRole('list', { name: 'Shelter arcs' })).getAllByRole('listitem')
    expect(arcs).toHaveLength(card.shelter.arcs.length)
  })

  it('draws a grade band for all 36 bearings, even where the fetch is too short to see', () => {
    const card = anchorageCard()
    card.shelter.bearings = card.shelter.bearings.map((b, i) => (i % 2 === 0 ? { ...b, open: false, fetchNm: 0.1, grade: 'sheltered' } : { ...b, open: true, fetchNm: 15, grade: 'exposed' }))
    render(<AssistantAnchorageCard card={card} />)
    const rose = screen.getByTestId('anchorage-rose')
    const band = rose.querySelectorAll('path[data-band-grade]')
    expect(band).toHaveLength(36)
    const sheltered = rose.querySelector('path[data-band-grade="sheltered"]')
    expect(sheltered?.getAttribute('fill')).toBe('hsl(var(--primary))')
    expect(rose.querySelector('path[data-band-grade="exposed"]')?.getAttribute('fill')).toBe('hsl(var(--destructive))')
    expect(screen.getByText(/band outside the ring/i)).toBeInTheDocument()
  })

  it('keeps the ring distance labels clear of the compass letters', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    const rose = screen.getByTestId('anchorage-rose')
    const at = (el: Element) => ({ x: Number(el.getAttribute('x')), y: Number(el.getAttribute('y')) })
    const compass = ['N', 'E', 'S', 'W'].map((l) => at(Array.from(rose.querySelectorAll('text')).find((t) => t.textContent === l)!))
    const rings = Array.from(rose.querySelectorAll('text[data-ring-label]'))
    expect(rings).toHaveLength(2)
    for (const ring of rings) {
      const r = at(ring)
      for (const c of compass) expect(Math.hypot(r.x - c.x, r.y - c.y)).toBeGreaterThan(25)
    }
  })

  it('draws the ring labels after the spokes, with a halo, so a long spoke does not hide them', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    const rose = screen.getByTestId('anchorage-rose')
    const all = Array.from(rose.querySelectorAll('*'))
    const lastSpoke = Math.max(...Array.from(rose.querySelectorAll('[data-bearing]')).map((el) => all.indexOf(el)))
    const labels = Array.from(rose.querySelectorAll('text[data-ring-label]'))
    expect(labels).toHaveLength(2)
    for (const label of labels) {
      expect(all.indexOf(label)).toBeGreaterThan(lastSpoke)
      expect(label.getAttribute('paint-order')).toBe('stroke')
      expect(label.getAttribute('stroke')).toBe('hsl(var(--card))')
    }
  })

  it('lists the forecast rows with wind, direction, shelter and waves', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    const table = screen.getByRole('table')
    const rows = within(table).getAllByRole('row')
    expect(rows).toHaveLength(8) // header + seven
    expect(rows[1]).toHaveTextContent('Mon 14:00')
    expect(rows[1]).toHaveTextContent('15 g19')
    expect(rows[1]).toHaveTextContent('N 005°')
    expect(rows[1]).toHaveTextContent(/Exposed|Sheltered|Partly sheltered/)
    expect(rows[1]).toHaveTextContent('0.8 m 5 s SE')
    expect(screen.getByRole('list', { name: 'Prevailing shelter by day' })).toHaveTextContent('Mon 2 Nov')
  })

  it('shows waves in feet when the operator reads imperial', () => {
    config.distanceUnits = 'imperial'
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    expect(within(screen.getByRole('table')).getAllByRole('row')[1]).toHaveTextContent('2.6 ft 5 s SE')
    expect(screen.getByTestId('anchorage-tide-arrival')).toHaveTextContent('4.9 ft')
  })

  it('gives the tide at arrival, the extremes and the range', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    expect(screen.getByText(/Shute Harbour, 15.2 nm/)).toBeInTheDocument()
    expect(screen.getByTestId('anchorage-tide-arrival')).toHaveTextContent('At arrival 1.5 m, rising')
    expect(within(screen.getByRole('list', { name: 'High and low waters' })).getAllByRole('listitem')).toHaveLength(3)
    expect(screen.getByTestId('anchorage-tide-range')).toHaveTextContent('Range 2.4 m')
  })

  it('says when the tide prediction ends before the stay does', () => {
    const card = anchorageCard({ tide: { ...(anchoragePayload().tide as object), note: 'The tide prediction ends at Tue 3 Nov 06:00, before the stay does.' } })
    render(<AssistantAnchorageCard card={card} />)
    expect(screen.getByTestId('anchorage-tide-note')).toHaveTextContent('The tide prediction ends at Tue 3 Nov 06:00, before the stay does.')
  })

  it('says outside the stay for the extremes added to give a range, and what a missing height means', () => {
    const base = anchorageCard()
    const card = anchorageCard({
      tide: {
        ...JSON.parse(JSON.stringify({ error: '', station: base.tide.station, distance_nm: 1, time_basis: 'x', height_at_arrival_m: null, direction: '', height_note: 'Arrival is outside the predicted tides, which run from A to B.', note: '', range_m: 2, range_note: 'Fewer than two high or low waters fall inside the stay, so the range includes the one before arrival and the one after leaving.' })),
        extremes: [{ time: '2026-11-02T02:00:00Z', type: 'low', height_m: 1, in_window: false }, { time: '2026-11-02T08:00:00Z', type: 'high', height_m: 3, in_window: false }],
      },
    })
    render(<AssistantAnchorageCard card={card} />)
    expect(screen.getAllByText('outside the stay')).toHaveLength(2)
    expect(screen.getByTestId('anchorage-tide-arrival')).toHaveTextContent('outside the predicted tides')
    expect(screen.getByTestId('anchorage-tide-range')).toHaveTextContent(/Range 2 m\. Fewer than two/)
  })

  it('groups the moorings, with sizes and classes, and names the protected areas without calling them closed', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    const features = screen.getByTestId('anchorage-features')
    expect(features).toHaveTextContent('23 public moorings within 1 nm')
    expect(features).toHaveTextContent('Up to 20 m · class B')
    expect(features).toHaveTextContent('Size limit not tagged · class D')
    expect(features).toHaveTextContent('Bay · Butterfly Bay')
    const restricted = within(screen.getByRole('list', { name: 'Restricted areas' })).getAllByRole('listitem')
    expect(restricted).toHaveLength(2)
    expect(restricted[0]).toHaveTextContent('Great Barrier Reef Coast Marine Park')
    expect(restricted[0]).toHaveTextContent('check the zoning')
    expect(features).not.toHaveTextContent(/prohibited/)
  })

  it('puts a restriction that prohibits anchoring in the alert palette, first', () => {
    const base = anchorageCard()
    const card = anchorageCard({
      features: {
        error: '', truncated: false, bay: null, moorings: [], anchorage_areas: [], seabed_areas: [],
        restricted_areas: [
          { osm: 'way/1', name: 'Reef zone', source: 'seamark', restriction: 'anchoring_prohibited', category: '', distance_m: 100, prohibits_anchoring: true, shapes: [] },
          ...base.features.restrictedAreas.map((r) => ({ osm: r.osm, name: r.name, source: r.source, restriction: r.restriction, category: r.category, distance_m: r.distanceM, prohibits_anchoring: r.prohibitsAnchoring, shapes: r.shapes })),
        ],
      },
    })
    render(<AssistantAnchorageCard card={card} />)
    const items = within(screen.getByRole('list', { name: 'Restricted areas' })).getAllByRole('listitem')
    expect(items[0]).toHaveTextContent('Reef zone')
    expect(items[0]).toHaveTextContent('Anchoring prohibited')
    expect(items[0].className).toMatch(/red/)
    expect(items[1].className).not.toMatch(/red/)
    expect(screen.getByTestId('anchorage-features').querySelector('ul')?.getAttribute('aria-label')).toBe('Restricted areas')
  })

  it('says nothing is mapped without saying nothing is there', () => {
    const card = anchorageCard({
      features: { error: '', truncated: false, bay: null, moorings: [], anchorage_areas: [], seabed_areas: [], restricted_areas: [] },
    })
    render(<AssistantAnchorageCard card={card} />)
    expect(screen.getByTestId('anchorage-features')).toHaveTextContent('Nothing is mapped within 1 nm. That does not mean there is nothing there.')
  })

  it('shows a failed section\'s error instead of its data', () => {
    render(<AssistantAnchorageCard card={anchorageCardWithErrors()} />)
    expect(screen.getByTestId('anchorage-forecast-error')).toHaveTextContent('Forecast unavailable: weather provider "open-meteo": timed out')
    expect(screen.getByTestId('anchorage-tide-error')).toHaveTextContent('Tide unavailable: tide provider "bom" has no stations')
    expect(screen.getByTestId('anchorage-features-error')).toHaveTextContent('does not provide anchorage features')
    expect(screen.queryByRole('table')).toBeNull()
    expect(screen.queryByTestId('anchorage-tide-range')).toBeNull()
    expect(screen.queryByText(/public moorings/)).toBeNull()
    // The shelter is required and is still there.
    expect(screen.getByRole('img', { name: /Shelter by the direction/ })).toBeInTheDocument()
  })

  it('shows the depth and its three checks after the local features and before the map', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    const depth = screen.getByTestId('anchorage-depth')
    expect(within(depth).getByTestId('anchorage-depth-depth')).toHaveTextContent('4 m at chart datum, from you')
    expect(within(depth).getByTestId('anchorage-depth-clearance')).toHaveTextContent('3.4 m under a 1.4 m draft')
    expect(within(depth).getByTestId('anchorage-depth-rode')).toHaveTextContent('Short by 20.9 m')
    expect(within(depth).getByTestId('anchorage-depth-swing')).toHaveTextContent('With all 40 m of chain out, short of the 60.9 m needed, the boat swings within about 56 m')
    const order = screen.getAllByRole('region').map((r) => r.getAttribute('data-testid'))
    expect(order.indexOf('anchorage-features')).toBeLessThan(order.indexOf('anchorage-depth'))
    const featuresEl = screen.getByTestId('anchorage-features')
    const mapEl = screen.getByText('Map')
    expect(depth.compareDocumentPosition(mapEl) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(featuresEl.compareDocumentPosition(depth) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  it('draws no depth section for a card stored before the depth checks', () => {
    const payload = anchoragePayload()
    delete payload.depth
    const [card] = decodeMateCards([{ kind: 'anchorage', payload }])
    if (card.kind !== 'anchorage') throw new Error('did not decode')
    render(<AssistantAnchorageCard card={card} />)
    expect(screen.queryByTestId('anchorage-depth')).toBeNull()
    expect(screen.queryByText(/Depth unknown/)).toBeNull()
  })

  it('says the depth is unknown and lists the checks as skipped, not passed', () => {
    render(<AssistantAnchorageCard card={anchorageCardWithoutDepth()} />)
    const depth = screen.getByTestId('anchorage-depth')
    expect(within(depth).getByTestId('anchorage-depth-depth')).toHaveTextContent('Depth unknown: check the chart.')
    for (const key of ['clearance', 'rode', 'swing']) {
      const line = within(depth).getByTestId(`anchorage-depth-${key}`)
      expect(line).toHaveTextContent('Skipped')
      expect(line).toHaveTextContent('the depth is unknown')
      expect(line).not.toHaveTextContent('OK')
    }
  })

  it('shows depth lengths in feet when the operator reads imperial', () => {
    config.distanceUnits = 'imperial'
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    expect(screen.getByTestId('anchorage-depth-depth')).toHaveTextContent('13.1 ft at chart datum')
  })

  it('keeps the map closed until opened, and loads it only then', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    expect(screen.queryByTestId('anchorage-map-stub')).toBeNull()
    const details = screen.getByText('Map').closest('details') as HTMLDetailsElement
    expect(details.open).toBe(false)
    details.open = true
    fireEvent(details, new Event('toggle'))
    expect(screen.getByTestId('anchorage-map-stub')).toHaveTextContent('23 moorings')
    details.open = false
    fireEvent(details, new Event('toggle'))
    expect(screen.queryByTestId('anchorage-map-stub')).toBeNull()
  })

  it('dates the forecast, the coastline and the map in the footer', () => {
    render(<AssistantAnchorageCard card={anchorageCard()} />)
    const footer = screen.getByTestId('anchorage-footer')
    expect(footer).toHaveTextContent('Forecast fetched Mon 11:00')
    expect(footer).toHaveTextContent('Coastline downloaded 3 Oct 2026')
    expect(footer).toHaveTextContent('OpenStreetMap · data as of 31 Oct 2026')
  })

  it('names only OpenStreetMap when the map server gave no date', () => {
    render(<AssistantAnchorageCard card={anchorageCard({ data_as_of: '' })} />)
    expect(screen.getByTestId('anchorage-footer')).toHaveTextContent(/OpenStreetMap$/)
  })

  it('is what an anchorage card draws in a Mate thread', async () => {
    render(<AssistantCard card={anchorageCard()} />)
    expect(await screen.findByTestId('assistant-anchorage-card')).toBeInTheDocument()
  })
})
