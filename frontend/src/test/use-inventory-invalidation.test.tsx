import { describe, it, expect } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { stubApi } from '@/test/helpers/stub-api'
import {
  createEquipment,
  deleteEquipment,
  useEquipment,
  useInventoryDecks,
  useInventoryZones,
  BLANK_DRAFT,
  type EquipmentItem,
  type InventoryZone,
} from '@/hooks/use-inventory'
import { useApiQuery } from '@/lib/api-query'
import { completeMaintenanceRule, useMaintenanceLogEntries, useMaintenanceRules } from '@/hooks/use-maintenance'

// ADR 0173: a write refreshes the lists it changes inside the hook, so no
// component has to hand a refresh callback to its sibling.

function item(id: string, name: string): EquipmentItem {
  return { id, name } as EquipmentItem
}

function zone(id: string, deckId: string | null): InventoryZone {
  return { id, name: id, sort_index: 0, bins: [], deck_id: deckId }
}

describe('cross-hook invalidation', () => {
  it('createEquipment refreshes a mounted equipment list', async () => {
    let items = [item('a', 'Anchor')]
    stubApi({
      'GET /api/inventory/equipment': () => ({ items }),
      'POST /api/inventory/equipment': () => {
        items = [...items, item('b', 'Bilge pump')]
        return { item: items[1] }
      },
    })
    const { result } = renderHook(() => useEquipment({}))
    await waitFor(() => expect(result.current.items).toHaveLength(1))

    await act(async () => { await createEquipment(BLANK_DRAFT) })

    await waitFor(() => expect(result.current.items).toHaveLength(2))
  })

  it('deleteEquipment re-reads the settings, whose roles the delete may have cleared', async () => {
    let roles = { house_bank: 'a' }
    const api = stubApi({
      'GET /api/inventory/equipment': { items: [] },
      'GET /api/settings': () => ({ roles }),
      'DELETE /api/inventory/equipment/a': () => {
        roles = { house_bank: '' }
        return null
      },
    })
    const { result } = renderHook(() => useApiQuery<{ roles: { house_bank: string } }>({ queryKey: ['/api/settings'], path: '/api/settings' }))
    await waitFor(() => expect(result.current.data?.roles.house_bank).toBe('a'))
    expect(api).toBeDefined()

    await act(async () => { await deleteEquipment('a') })

    await waitFor(() => expect(result.current.data?.roles.house_bank).toBe(''))
  })

  it('saveLayout hands the zones cache the zones the server answered with', async () => {
    const answered = [zone('z1', 'd1')]
    const api = stubApi({
      'GET /api/inventory/zones': { zones: [zone('z1', null)] },
      'GET /api/inventory/decks': { decks: [] },
      'PUT /api/inventory/decks/d1/layout': { zones: answered },
    })
    const zones = renderHook(() => useInventoryZones())
    const decks = renderHook(() => useInventoryDecks())
    await waitFor(() => expect(zones.result.current.zones[0]?.deck_id).toBeNull())
    const zoneReads = api.mock.calls.filter(([url]) => String(url).endsWith('/api/inventory/zones')).length

    await act(async () => { await decks.result.current.saveLayout('d1', { zones: [], bins: [] }) })

    expect(zones.result.current.zones).toEqual(answered)
    // Adopted from the response, not read again.
    expect(api.mock.calls.filter(([url]) => String(url).endsWith('/api/inventory/zones'))).toHaveLength(zoneReads)
  })

  it('deleteDeck refreshes the zones that lost their outline', async () => {
    let zones = [zone('z1', 'd1')]
    stubApi({
      'GET /api/inventory/zones': () => ({ zones }),
      'GET /api/inventory/decks': { decks: [] },
      'DELETE /api/inventory/decks/d1': () => {
        zones = [zone('z1', null)]
        return { zones_cleared: 1 }
      },
    })
    const zoneHook = renderHook(() => useInventoryZones())
    const deckHook = renderHook(() => useInventoryDecks())
    await waitFor(() => expect(zoneHook.result.current.zones[0]?.deck_id).toBe('d1'))

    await act(async () => { await deckHook.result.current.deleteDeck('d1') })

    await waitFor(() => expect(zoneHook.result.current.zones[0]?.deck_id).toBeNull())
  })

  it('completing a rule refreshes both the rule list and the log', async () => {
    let done = false
    const api = stubApi({
      'GET /api/inventory/maintenance/rules': () => ({ rules: [{ id: 'r1', status: done ? 'ok' : 'overdue' }] }),
      'GET /api/inventory/maintenance/log': () => ({ entries: done ? [{ id: 'e1' }] : [] }),
      'POST /api/inventory/maintenance/rules/r1/complete': () => {
        done = true
        return { rule: { id: 'r1' }, entry: { id: 'e1' } }
      },
    })
    const rules = renderHook(() => useMaintenanceRules({ equipment: 'eq-1' }))
    const log = renderHook(() => useMaintenanceLogEntries({ equipment: 'eq-1' }))
    await waitFor(() => expect(rules.result.current.rules[0]?.status).toBe('overdue'))
    await waitFor(() => expect(log.result.current.loading).toBe(false))

    await act(async () => {
      await completeMaintenanceRule('r1', { performed_at: '2026-10-14', hours: null, description: '', who: '', cost: null, currency: '', parts: [] })
    })

    await waitFor(() => expect(rules.result.current.rules[0]?.status).toBe('ok'))
    await waitFor(() => expect(log.result.current.entries).toHaveLength(1))
    expect(api).toHaveBeenCalled()
  })
})
