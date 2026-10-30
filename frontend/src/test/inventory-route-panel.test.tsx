/**
 * Inventory's route wiring (ADR 0171), with InventoryPanel stubbed. The
 * guards belong to the owners of the unsaved state (the editors, Stocktake,
 * the bin's quick-add) and are covered in their own tests; this covers the
 * wiring, and that a create/delete/discard that already settled goes through
 * a guard that is still registered.
 */
import { useState } from 'react'
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { InventoryRoutePanel } from '@/components/inventory/inventory-route-panel'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import { testRouter } from './helpers/router'

const toastError = vi.fn()
vi.mock('sonner', () => ({ toast: { error: (message: string) => toastError(message) } }))

type StubProps = {
  activeSectionId: string
  equipmentEditId: string | null
  creatingEquipment: boolean
  binCode: string | null
  locationEditId: string | null
  decksOpen: boolean
  deckEditId: string | null
  locationsView: string
  planDeckId: string | null
  newEquipmentPreset: unknown
  onSectionChange: (id: string) => void
  onOpenEquipment: (id: string) => void
  onNewEquipment: (preset?: { zoneId?: string; binId?: string }) => void
  onCloseEditor: () => void
  onEquipmentCreated: (id: string) => void
  onEquipmentDeleted: (message?: string) => void
  onEquipmentDiscarded: () => void
  onOpenBin: (code: string) => void
  onCloseBin: (zoneId?: string) => void
  onOpenLocation: (id: string) => void
  onLocationsViewChange: (view: 'table' | 'plan') => void
  onPlanDeckChange: (id: string) => void
  onOpenDecks: () => void
  onOpenDeck: (id: string) => void
  onCloseDecks: () => void
  onCloseDeck: () => void
  onLocationDeleted: () => void
  onDeckDeleted: () => void
  list: unknown
  onListChange: (list: { q?: string; sort?: { id: string; desc: boolean } }) => void
}

vi.mock('@/components/inventory/inventory-panel', () => ({
  InventoryPanel: function InventoryPanelStub(props: StubProps) {
    // Stands in for an editor with unsaved edits.
    const [dirty, setDirty] = useState(false)
    useNavigationGuard(dirty ? { owner: 'inventory', label: 'Inventory', blocks: () => ({ kind: 'dirty' }) } : null)
    return (
      <div>
        <p data-testid="shape">
          {[props.activeSectionId, props.equipmentEditId ?? '-', props.creatingEquipment ? 'creating' : '-', props.binCode ?? '-',
            props.locationEditId ?? '-', props.decksOpen ? 'decks' : '-', props.deckEditId ?? '-', props.locationsView, props.planDeckId ?? '-'].join(' ')}
        </p>
        <p data-testid="list">{JSON.stringify(props.list)}</p>
        <button type="button" onClick={() => props.onListChange({ q: 'pump', sort: { id: 'name', desc: true } })}>set-list</button>
        <button type="button" onClick={() => props.onListChange({})}>clear-list</button>
        <button type="button" onClick={() => props.onOpenLocation('z2')}>open-location-z2</button>
        <button type="button" onClick={() => setDirty(true)}>dirty</button>
        <button type="button" onClick={() => props.onOpenEquipment('eq-2')}>open-eq-2</button>
        <button type="button" onClick={() => props.onNewEquipment({ zoneId: 'z1', binId: 'b1' })}>new-item</button>
        <button type="button" onClick={props.onCloseEditor}>back</button>
        <button type="button" onClick={() => props.onEquipmentCreated('eq-new')}>created</button>
        <button type="button" onClick={() => props.onEquipmentDeleted('photo file stayed')}>deleted</button>
        <button type="button" onClick={props.onEquipmentDiscarded}>discarded</button>
        <button type="button" onClick={() => props.onSectionChange('profiles')}>nav-profiles</button>
        <button type="button" onClick={() => props.onSectionChange('locations')}>nav-locations</button>
        <button type="button" onClick={() => props.onOpenBin('LAZ-02')}>open-bin</button>
        <button type="button" onClick={() => props.onCloseBin('z9')}>close-bin-z9</button>
        <button type="button" onClick={() => props.onCloseBin()}>close-bin</button>
        <button type="button" onClick={() => props.onLocationsViewChange('plan')}>view-plan</button>
        <button type="button" onClick={() => props.onPlanDeckChange('d4')}>deck-d4</button>
        <button type="button" onClick={props.onOpenDecks}>open-decks</button>
        <button type="button" onClick={() => props.onOpenDeck('d1')}>open-deck-d1</button>
        <button type="button" onClick={props.onCloseDecks}>close-decks</button>
        <button type="button" onClick={props.onCloseDeck}>close-deck</button>
        <button type="button" onClick={props.onLocationDeleted}>location-deleted</button>
        <button type="button" onClick={props.onDeckDeleted}>deck-deleted</button>
      </div>
    )
  },
}))

function mount(path: string) {
  const harness = testRouter(path)
  render(<InventoryRoutePanel canWrite />, { wrapper: harness.wrapper as never })
  return harness
}

const shape = () => screen.getByTestId('shape').textContent

describe('InventoryRoutePanel route wiring', () => {
  it('hands the panel the view the location names', () => {
    mount('/inventory/equipment/eq-1')
    expect(shape()).toBe('equipment eq-1 - - - - - table -')
    mount('/inventory/bins/LAZ-02')
  })

  it.each([
    ['/inventory/bins/LAZ-02', 'locations - - LAZ-02 - - - table -'],
    ['/inventory/locations/z1', 'locations - - - z1 - - table -'],
    ['/inventory/decks', 'locations - - - - decks - table -'],
    ['/inventory/decks/d1', 'locations - - - - decks d1 table -'],
    ['/inventory/locations?view=plan&deck=d2', 'locations - - - - - - plan d2'],
    ['/inventory/stocktake', 'stocktake - - - - - - table -'],
  ])('shows %s as the view it names', (path, expected) => {
    mount(path)
    expect(shape()).toBe(expected)
  })

  it('opens an item, a bin, a location\'s deck and a New item by replacing the slice', () => {
    const { history } = mount('/inventory/bins/LAZ-02')
    fireEvent.click(screen.getByText('open-eq-2'))
    expect(history.path()).toBe('/inventory/equipment/eq-2')
    expect(shape()).toBe('equipment eq-2 - - - - - table -')

    fireEvent.click(screen.getByText('new-item'))
    expect(shape()).toBe('equipment - creating - - - - table -')
    expect(history.path()).toBe('/inventory') // a draft has no address of its own: the Equipment index
  })

  it('Back out of a bin goes to its location, or to Locations when the bin did not resolve', () => {
    const { history } = mount('/inventory/bins/LAZ-02')
    fireEvent.click(screen.getByText('close-bin-z9'))
    expect(history.path()).toBe('/inventory/locations/z9')
    fireEvent.click(screen.getByText('open-bin'))
    fireEvent.click(screen.getByText('close-bin'))
    expect(history.path()).toBe('/inventory/locations')
  })

  it('the Decks and Locations crumbs return to Plan when Plan was where the operator came from', () => {
    const { history } = mount('/inventory/locations?view=plan')
    fireEvent.click(screen.getByText('open-decks'))
    expect(history.path()).toBe('/inventory/decks')
    fireEvent.click(screen.getByText('close-decks'))
    expect(history.path()).toBe('/inventory/locations?view=plan')
    fireEvent.click(screen.getByText('open-deck-d1'))
    fireEvent.click(screen.getByText('close-deck'))
    expect(history.path()).toBe('/inventory/decks')
  })

  it('switching to Plan and choosing a deck is in the address', () => {
    const { history } = mount('/inventory/locations')
    fireEvent.click(screen.getByText('view-plan'))
    expect(history.path()).toBe('/inventory/locations?view=plan')
    fireEvent.click(screen.getByText('deck-d4'))
    expect(history.path()).toBe('/inventory/locations?view=plan&deck=d4')
  })

  it('the nav\'s Locations returns to Plan once Plan has been chosen', () => {
    const { history } = mount('/inventory/locations')
    fireEvent.click(screen.getByText('view-plan'))
    fireEvent.click(screen.getByText('nav-profiles'))
    expect(history.path()).toBe('/inventory/profiles')
    fireEvent.click(screen.getByText('nav-locations'))
    expect(history.path()).toBe('/inventory/locations?view=plan')
  })
})

describe('InventoryRoutePanel: a move that already settled', () => {
  it('does not hold a create, delete or discard, even with a guard still registered', () => {
    const { router, history } = mount('/inventory/equipment/eq-1')
    fireEvent.click(screen.getByText('dirty'))
    fireEvent.click(screen.getByText('created'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(history.path()).toBe('/inventory/equipment/eq-new')

    fireEvent.click(screen.getByText('deleted'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(history.path()).toBe('/inventory')
    expect(toastError).toHaveBeenCalledWith('photo file stayed')

    fireEvent.click(screen.getByText('discarded'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(shape()).toBe('equipment - - - - - - table -')
  })

  it('Discard on a New item draft, whose guard holds every inventory move, leaves without holding anything', () => {
    const { router, history } = mount('/inventory')
    fireEvent.click(screen.getByText('new-item'))
    fireEvent.click(screen.getByText('dirty'))
    expect(router.getSnapshot().guardOwners).toEqual(['inventory'])
    fireEvent.click(screen.getByText('discarded'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(history.path()).toBe('/inventory')
    expect(shape()).toBe('equipment - - - - - - table -')
  })

  it('lets a location page or deck page that settled go without asking', () => {
    const { router } = mount('/inventory/locations/z1')
    fireEvent.click(screen.getByText('dirty'))
    fireEvent.click(screen.getByText('location-deleted'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(shape()).toBe('locations - - - - - - table -')

    fireEvent.click(screen.getByText('open-deck-d1'))
    fireEvent.click(screen.getByText('deck-deleted'))
    expect(router.getSnapshot().pending).toBeNull()
    expect(shape()).toBe('locations - - - - decks - table -')
  })

  describe('the index state (filter, search, sort)', () => {
    it('is in the address of an index, replacing the entry rather than adding one', () => {
      const { history } = mount('/inventory')
      fireEvent.click(screen.getByText('set-list'))
      expect(history.path()).toBe('/inventory?q=pump&sort=-name')
      expect(history.entries).toHaveLength(1)
      fireEvent.click(screen.getByText('clear-list'))
      expect(history.path()).toBe('/inventory')
    })

    it('rides on an Equipment record and comes back with Back', () => {
      const { history } = mount('/inventory?system=bilge&sort=-name')
      expect(screen.getByTestId('list').textContent).toBe('{"system":"bilge","sort":{"id":"name","desc":true}}')
      fireEvent.click(screen.getByText('open-eq-2'))
      expect(history.path()).toBe('/inventory/equipment/eq-2?system=bilge&sort=-name')
      expect(screen.getByTestId('list').textContent).toBe('{"system":"bilge","sort":{"id":"name","desc":true}}')
      fireEvent.click(screen.getByText('back'))
      expect(history.path()).toBe('/inventory?system=bilge&sort=-name')
    })

    it('rides on a location the same way', () => {
      const { history } = mount('/inventory/locations?q=la')
      fireEvent.click(screen.getByText('open-location-z2'))
      expect(history.path()).toBe('/inventory/locations/z2?q=la')
      fireEvent.click(screen.getByText('back'))
      expect(history.path()).toBe('/inventory/locations?q=la')
    })

    it('survives a switch between Table and Plan', () => {
      const { history } = mount('/inventory/locations?q=la&sort=-name')
      fireEvent.click(screen.getByText('view-plan'))
      expect(history.path()).toBe('/inventory/locations?view=plan&q=la&sort=-name')
      expect(screen.getByTestId('list').textContent).toBe('{"q":"la","sort":{"id":"name","desc":true}}')
    })

    it('rides on a deck the same way', () => {
      const { history } = mount('/inventory/decks?sort=-name')
      fireEvent.click(screen.getByText('open-deck-d1'))
      expect(history.path()).toBe('/inventory/decks/d1?sort=-name')
      fireEvent.click(screen.getByText('close-deck'))
      expect(history.path()).toBe('/inventory/decks?sort=-name')
    })

    it('sends Back from an item opened out of a bin to the Equipment index, which its arrows then follow', () => {
      const { history } = mount('/inventory/bins/LAZ-02')
      fireEvent.click(screen.getByText('open-eq-2'))
      expect(history.path()).toBe('/inventory/equipment/eq-2')
      expect(screen.getByTestId('list').textContent).toBe('{}')
      fireEvent.click(screen.getByText('back'))
      expect(history.path()).toBe('/inventory')
    })

    it('is not carried to a record of another kind', () => {
      const { history } = mount('/inventory?q=pump')
      fireEvent.click(screen.getByText('open-bin'))
      expect(history.path()).toBe('/inventory/bins/LAZ-02')
    })
  })

})
