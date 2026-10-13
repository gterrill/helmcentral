/**
 * The three panel route hooks (ADR 0171): each returns the panel's slice of
 * the location, goes to a view by replacing the whole slice, and closes by
 * going up one level.
 */
import { describe, it, expect } from 'vitest'
import { renderHook, act } from '@testing-library/react'
import { useAppLocation } from '@/hooks/use-app-location'
import { useDocumentsRoute } from '@/hooks/use-documents-route'
import { useInventoryRoute } from '@/hooks/use-inventory-route'
import { useSettingsRoute } from '@/hooks/use-settings-route'
import { testRouter } from './helpers/router'

describe('useSettingsRoute', () => {
  it('reads the route and goes to a section', () => {
    const { wrapper, history } = testRouter('/settings/signalk')
    const { result } = renderHook(() => useSettingsRoute(), { wrapper })
    expect(result.current.route).toEqual({ view: 'section', section: 'signalk' })

    act(() => { result.current.go({ view: 'section', section: 'security' }) })
    expect(result.current.route).toEqual({ view: 'section', section: 'security' })
    expect(history.path()).toBe('/settings/security')
  })

  it('drops the import run when going to another section', () => {
    const { wrapper } = testRouter('/settings/import/run-1')
    const { result } = renderHook(() => useSettingsRoute(), { wrapper })
    expect(result.current.route).toEqual({ view: 'import-run', runId: 'run-1' })
    act(() => { result.current.go({ view: 'section', section: 'general' }) })
    expect(result.current.route).toEqual({ view: 'section', section: 'general' })
  })

  it('close returns an import run to the import list', () => {
    const { wrapper, history } = testRouter('/settings/import/run-1')
    const { result } = renderHook(() => useSettingsRoute(), { wrapper })
    act(() => { result.current.close() })
    expect(result.current.route).toEqual({ view: 'section', section: 'import' })
    expect(history.path()).toBe('/settings/import')
  })

  it('is the panel as last left when the panel is not showing', () => {
    const { wrapper, router } = testRouter('/settings/security')
    router.navigate({ panel: 'forecast' })
    const { result } = renderHook(() => useSettingsRoute(), { wrapper })
    expect(result.current.route).toEqual({ view: 'section', section: 'security' })
  })
})

describe('useDocumentsRoute', () => {
  it('opens a folder, and drops the manual section that belonged to the old one', () => {
    const { wrapper, history } = testRouter('/documents?folder=f1&section=s1')
    const { result } = renderHook(() => useDocumentsRoute(), { wrapper })

    act(() => { result.current.openFolder('f2') })
    expect(result.current.route).toEqual({ view: 'browse', folderId: 'f2', sectionId: null, documentId: null })
    expect(history.path()).toBe('/documents?folder=f2')
  })

  it('keeps the section when the folder has not changed', () => {
    const { wrapper } = testRouter('/documents?folder=f1&section=s1')
    const { result } = renderHook(() => useDocumentsRoute(), { wrapper })
    act(() => { result.current.openFolder('f1') })
    expect(result.current.route).toMatchObject({ folderId: 'f1', sectionId: 's1' })
  })

  it('opens and closes a section of the current folder', () => {
    const { wrapper, history } = testRouter('/documents?folder=f1')
    const { result } = renderHook(() => useDocumentsRoute(), { wrapper })
    act(() => { result.current.openSection('s9') })
    expect(history.path()).toBe('/documents?folder=f1&section=s9')
    act(() => { result.current.openSection(null) })
    expect(history.path()).toBe('/documents?folder=f1')
  })

  it('opens Details from a listing and Back returns to the same folder and section', () => {
    const { wrapper, history } = testRouter('/documents?folder=f1&section=s1')
    const { result } = renderHook(() => useDocumentsRoute(), { wrapper })

    act(() => { result.current.openDetails('doc-1') })
    expect(result.current.route).toEqual({ view: 'details', id: 'doc-1', folderId: 'f1', sectionId: 's1' })
    expect(history.path()).toBe('/documents/doc-1?folder=f1')

    act(() => { result.current.close() })
    expect(result.current.route).toEqual({ view: 'browse', folderId: 'f1', sectionId: 's1', documentId: null })
    expect(history.path()).toBe('/documents?folder=f1&section=s1')
  })

  it('follows a Move on the Details page by replacing the entry', () => {
    const { wrapper, history } = testRouter('/documents')
    const { result } = renderHook(() => useDocumentsRoute(), { wrapper })
    act(() => { result.current.openDetails('doc-1') })
    expect(history.entries).toEqual(['/documents', '/documents/doc-1'])

    act(() => { result.current.followFolder('f9') })
    expect(history.entries).toEqual(['/documents', '/documents/doc-1?folder=f9'])
    expect(result.current.route).toMatchObject({ view: 'details', id: 'doc-1', folderId: 'f9' })
  })

  it('hands out callbacks that keep one identity across moves', () => {
    const { wrapper } = testRouter('/documents')
    const { result } = renderHook(() => useDocumentsRoute(), { wrapper })
    const { openFolder, openSection, openDetails } = result.current
    act(() => { result.current.openFolder('f1') })
    expect(result.current.openFolder).toBe(openFolder)
    expect(result.current.openSection).toBe(openSection)
    expect(result.current.openDetails).toBe(openDetails)
  })
})

describe('useInventoryRoute', () => {
  it('goes to a view by replacing the whole slice', () => {
    const { wrapper, history } = testRouter('/inventory/bins/LAZ-02')
    const { result } = renderHook(() => ({ inventory: useInventoryRoute(), location: useAppLocation() }), { wrapper })
    expect(result.current.inventory.route).toEqual({ view: 'bin', code: 'LAZ-02' })

    act(() => { result.current.inventory.go({ view: 'equipment', id: 'eq-1' }) })
    // Nothing of the bin page is left behind to clear.
    expect(result.current.location).toEqual({ panel: 'inventory', route: { view: 'equipment', id: 'eq-1' } })
    expect(history.path()).toBe('/inventory/equipment/eq-1')
  })

  it('goes up one level with close', () => {
    const { wrapper } = testRouter('/inventory/equipment/eq-1')
    const { result } = renderHook(() => useInventoryRoute(), { wrapper })
    act(() => { result.current.close() })
    expect(result.current.route).toEqual({ view: 'index', section: 'equipment' })

    act(() => { result.current.go({ view: 'deck', id: 'd1' }) })
    act(() => { result.current.close() })
    expect(result.current.route).toEqual({ view: 'decks' })
    act(() => { result.current.close() })
    expect(result.current.route).toEqual({ view: 'index', section: 'locations' })
  })

  it('has nothing above an index', () => {
    const { wrapper, history } = testRouter('/inventory/profiles')
    const { result } = renderHook(() => useInventoryRoute(), { wrapper })
    act(() => { result.current.close() })
    expect(history.entries).toEqual(['/inventory/profiles'])
  })

  it('a New item draft is a view of its own, with no address of its own', () => {
    const { wrapper, history } = testRouter('/inventory')
    const { result } = renderHook(() => useInventoryRoute(), { wrapper })
    act(() => { result.current.go({ view: 'equipment-new', preset: { zoneId: 'z1' } }) })
    expect(result.current.route).toEqual({ view: 'equipment-new', preset: { zoneId: 'z1' } })
    expect(history.path()).toBe('/inventory')
  })

  it('returns to Plan, with its deck, after a trip through a location or the Decks list', () => {
    const { wrapper, history } = testRouter('/inventory/locations?view=plan&deck=d1')
    const { result } = renderHook(() => useInventoryRoute(), { wrapper })

    act(() => { result.current.go({ view: 'decks' }) })
    act(() => { result.current.close() })
    expect(history.path()).toBe('/inventory/locations?view=plan&deck=d1')

    act(() => { result.current.go({ view: 'location', id: 'z1' }) })
    act(() => { result.current.close() })
    expect(history.path()).toBe('/inventory/locations?view=plan&deck=d1')
  })

  it('returns to the Table once Table was chosen', () => {
    const { wrapper, history } = testRouter('/inventory/locations?view=plan')
    const { result } = renderHook(() => useInventoryRoute(), { wrapper })
    act(() => { result.current.go(result.current.locationsHome(false)) })
    act(() => { result.current.go({ view: 'location', id: 'z1' }) })
    act(() => { result.current.close() })
    expect(history.path()).toBe('/inventory/locations')
  })

  it('the nav goes to a section index, and Locations to wherever Locations last was', () => {
    const { wrapper } = testRouter('/inventory/locations?view=plan')
    const { result } = renderHook(() => useInventoryRoute(), { wrapper })
    act(() => { result.current.goSection('profiles') })
    expect(result.current.route).toEqual({ view: 'index', section: 'profiles' })
    act(() => { result.current.goSection('locations') })
    expect(result.current.route).toEqual({ view: 'plan', deckId: null })
    act(() => { result.current.goSection('equipment') })
    expect(result.current.route).toEqual({ view: 'index', section: 'equipment' })
  })
})
