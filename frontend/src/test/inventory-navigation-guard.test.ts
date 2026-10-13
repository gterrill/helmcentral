/**
 * The Inventory panel's navigation guard as pure functions (ADR 0171). The
 * closedBy cases are the ones that lived in app-location.test.ts when App's
 * popstate handler asked them.
 */
import { describe, it, expect } from 'vitest'
import { parseAppLocation, type AppLocation, type InventoryRoute } from '@/lib/app-location'
import {
  inventoryEditorClosedBy,
  inventoryWorkClosedBy,
} from '@/lib/inventory-navigation-guard'

const at = (path: string): AppLocation => parseAppLocation(path)

const equipmentIndex: InventoryRoute = { view: 'index', section: 'equipment' }
const editing = (id: string): InventoryRoute => ({ view: 'equipment', id })
const draft: InventoryRoute = { view: 'equipment-new', preset: null }

describe('inventoryEditorClosedBy', () => {
  it('says no when the editor is not showing at all', () => {
    expect(inventoryEditorClosedBy(equipmentIndex, at('/inventory/locations'))).toBe(false)
  })

  it('says no when the target is the same record', () => {
    expect(inventoryEditorClosedBy(editing('eq-1'), at('/inventory/equipment/eq-1'))).toBe(false)
  })

  it('says yes when the target is a different record', () => {
    expect(inventoryEditorClosedBy(editing('eq-1'), at('/inventory/equipment/eq-2'))).toBe(true)
  })

  it('says yes when the target is the index', () => {
    expect(inventoryEditorClosedBy(editing('eq-1'), at('/inventory'))).toBe(true)
  })

  // The hole this function was extracted to close: a section switch leaves the
  // equipment id null on both sides, so comparing ids alone saw no change and
  // let a dirty editor close unguarded.
  it('says yes when only the section changed', () => {
    expect(inventoryEditorClosedBy(editing('eq-1'), at('/inventory/profiles'))).toBe(true)
  })

  // And the same hole for a New item draft, which has no id to compare with.
  it('says yes for an unsaved New item draft, whatever the inventory target', () => {
    expect(inventoryEditorClosedBy(draft, at('/inventory'))).toBe(true)
    expect(inventoryEditorClosedBy(draft, at('/inventory/profiles'))).toBe(true)
    expect(inventoryEditorClosedBy(draft, at('/inventory/locations'))).toBe(true)
  })

  it('says yes when leaving the panel entirely', () => {
    expect(inventoryEditorClosedBy(editing('eq-1'), at('/documents'))).toBe(true)
  })

  // ADR 0127: a bin page is under Locations, so a target carrying a bin code
  // must not slip through as "still the same editor".
  it('says yes when the target is a bin page', () => {
    expect(inventoryEditorClosedBy(editing('eq-1'), at('/inventory/bins/LAZ-02'))).toBe(true)
  })
})

describe('inventoryEditorClosedBy for a location page', () => {
  const open: InventoryRoute = { view: 'location', id: 'z1' }

  it('says no when the target is the same location', () => {
    expect(inventoryEditorClosedBy(open, at('/inventory/locations/z1'))).toBe(false)
  })

  it('says yes for the index, another location, a bin page or another section', () => {
    expect(inventoryEditorClosedBy(open, at('/inventory/locations'))).toBe(true)
    expect(inventoryEditorClosedBy(open, at('/inventory/locations/z2'))).toBe(true)
    expect(inventoryEditorClosedBy(open, at('/inventory/bins/LAZ-02'))).toBe(true)
    expect(inventoryEditorClosedBy(open, at('/inventory'))).toBe(true)
    expect(inventoryEditorClosedBy(open, at('/documents'))).toBe(true)
  })
})

describe('inventoryEditorClosedBy for a deck page (ADR 0156)', () => {
  const current: InventoryRoute = { view: 'deck', id: 'd1' }

  it('is an editor that a move to anywhere else closes', () => {
    expect(inventoryEditorClosedBy(current, at('/inventory/decks'))).toBe(true)
    expect(inventoryEditorClosedBy(current, at('/inventory/locations'))).toBe(true)
    expect(inventoryEditorClosedBy(current, at('/inventory/decks/d2'))).toBe(true)
    expect(inventoryEditorClosedBy(current, at('/inventory/decks/d1'))).toBe(false)
  })
})

// Back off Stocktake or a bin page's quick-add draft: the other two things
// the guard protects, which have nothing to save.
describe('inventoryWorkClosedBy', () => {
  const bin: InventoryRoute = { view: 'bin', code: 'LAZ-02' }
  const stocktake: InventoryRoute = { view: 'index', section: 'stocktake' }

  it('says no when neither Stocktake nor a bin page is showing', () => {
    expect(inventoryWorkClosedBy(equipmentIndex, at('/inventory/locations'))).toBe(false)
  })

  it('says no when a bin page is showing but the target is the same bin', () => {
    expect(inventoryWorkClosedBy(bin, at('/inventory/bins/LAZ-02'))).toBe(false)
  })

  it('says yes when the target is a different bin', () => {
    expect(inventoryWorkClosedBy(bin, at('/inventory/bins/LAZ-03'))).toBe(true)
  })

  it('says yes when the target is the Locations index (no bin)', () => {
    expect(inventoryWorkClosedBy(bin, at('/inventory/locations'))).toBe(true)
  })

  it('says yes when only the section changed', () => {
    expect(inventoryWorkClosedBy(bin, at('/inventory/equipment'))).toBe(true)
  })

  it('says no when Stocktake is showing and the target is still Stocktake', () => {
    expect(inventoryWorkClosedBy(stocktake, at('/inventory/stocktake'))).toBe(false)
  })

  it('says yes when Stocktake is showing and the target is a different section', () => {
    expect(inventoryWorkClosedBy(stocktake, at('/inventory/locations'))).toBe(true)
  })

  it('says yes when leaving the panel entirely', () => {
    expect(inventoryWorkClosedBy(stocktake, at('/documents'))).toBe(true)
  })
})
