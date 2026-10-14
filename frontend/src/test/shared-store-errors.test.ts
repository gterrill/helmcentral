import { describe, it, expect } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useDocumentFolderTree } from '@/hooks/use-document-folder-tree'
import { useDocuments } from '@/hooks/use-documents'
import { useEquipment, useInventoryDecks, useInventoryZones } from '@/hooks/use-inventory'
import { useMaintenanceLogEntries, useMaintenanceRules } from '@/hooks/use-maintenance'
import { useNotes } from '@/hooks/use-notes'
import { jsonResponse, stubApi } from '@/test/helpers/stub-api'

// The query store is shared, so a failed revalidation started by any reader
// lands in every reader's error. The hooks report it (AGENTS.md fallback
// policy: no hiding an upstream failure) and keep the cached rows beside it,
// so a panel can show both.

// First read answers with `body`, every later one fails.
function thenFails(body: unknown) {
  let calls = 0
  return () => (++calls === 1 ? body : jsonResponse({ error: 'blip' }, 502))
}

interface ListHook {
  loading: boolean
  error: string | null
  refresh: () => Promise<void>
}

async function expectReportsErrorAndKeepsList<T extends ListHook>(
  render: () => T,
  holdsData: (r: T) => boolean,
) {
  const { result } = renderHook(render)
  await waitFor(() => expect(result.current.loading).toBe(false))
  expect(holdsData(result.current)).toBe(true)
  await act(async () => { await result.current.refresh() })
  expect(holdsData(result.current)).toBe(true)
  expect(result.current.error).toBe('blip')
}

describe('a failed revalidation over cached lists reports the error and keeps the rows', () => {
  it('reports the error with the equipment list', async () => {
    stubApi({ 'GET /api/inventory/equipment': thenFails({ items: [{ id: 'eq-1' }] }) })
    await expectReportsErrorAndKeepsList(() => useEquipment({}), (r) => r.items.length === 1)
  })

  it('reports the error with the zones', async () => {
    stubApi({ 'GET /api/inventory/zones': thenFails({ zones: [{ id: 'z1' }] }) })
    await expectReportsErrorAndKeepsList(() => useInventoryZones(), (r) => r.zones.length === 1)
  })

  it('reports the error with the decks', async () => {
    stubApi({ 'GET /api/inventory/decks': thenFails({ decks: [{ id: 'd1' }] }) })
    await expectReportsErrorAndKeepsList(() => useInventoryDecks(), (r) => r.decks.length === 1)
  })

  it('reports the error with the maintenance rules', async () => {
    stubApi({ 'GET /api/inventory/maintenance/rules': thenFails({ rules: [{ id: 'r1' }] }) })
    await expectReportsErrorAndKeepsList(() => useMaintenanceRules({}), (r) => r.rules.length === 1)
  })

  it('reports the error with the maintenance log', async () => {
    stubApi({ 'GET /api/inventory/maintenance/log': thenFails({ entries: [{ id: 'e1' }] }) })
    await expectReportsErrorAndKeepsList(() => useMaintenanceLogEntries({}), (r) => r.entries.length === 1)
  })

  it('reports the error with the notes', async () => {
    stubApi({ 'GET /api/notes': thenFails({ notes: [{ id: 'n1' }] }) })
    await expectReportsErrorAndKeepsList(() => useNotes(), (r) => r.notes.length === 1)
  })

  it('reports the error with the document folder tree', async () => {
    stubApi({ 'GET /api/document-folders/tree': thenFails({ folders: [{ id: 'f1' }] }) })
    await expectReportsErrorAndKeepsList(() => useDocumentFolderTree(), (r) => r.folders.length === 1)
  })

  it('reports the error with the documents listing', async () => {
    stubApi({
      'GET /api/document-folders': thenFails({ path: [], folders: [], documents: [{ id: 'd1', status: 'ready' }] }),
      'GET /api/documents/tags': [],
      'GET /api/documents/embeddings': { enabled: false, backfill: { running: false }, counts: {} },
    })
    await expectReportsErrorAndKeepsList(() => useDocuments(null), (r) => r.documents.length === 1)
  })
})

describe('tagged documents', () => {
  it('reports a failed tag filter instead of showing an empty list', async () => {
    stubApi({
      'GET /api/document-folders': { path: [], folders: [{ id: 'f1' }], documents: [] },
      'GET /api/documents': () => jsonResponse({ error: 'tag search down' }, 502),
      'GET /api/documents/tags': [],
      'GET /api/documents/embeddings': { enabled: false, backfill: { running: false }, counts: {} },
    })
    const { result } = renderHook(() => useDocuments(null))
    await waitFor(() => expect(result.current.loading).toBe(false))
    act(() => result.current.setSelectedTag('engine'))
    await waitFor(() => expect(result.current.error).toBe('tag search down'))
    expect(result.current.documents).toEqual([])
  })
})

describe('with nothing loaded', () => {
  it('still reports the equipment list error', async () => {
    stubApi({ 'GET /api/inventory/equipment': () => jsonResponse({ error: 'down' }, 502) })
    const { result } = renderHook(() => useEquipment({}))
    await waitFor(() => expect(result.current.error).toBe('down'))
  })
})
