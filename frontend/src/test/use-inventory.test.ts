import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useEquipmentItem, InventoryValidationError, type EquipmentInput } from '@/hooks/use-inventory'

// ADR 0123: the equipment registry's write path. The one thing worth pinning
// here is the shape of a rejected write. inventory_handlers.go answers a
// field validation failure with a single `{"field":…,"message":…}` object,
// NOT the `{errors:[…]}` array the first cut of this hook guessed at, and a
// client that only recognises the array shape falls through to a bare
// "HTTP 400" - throwing away the one thing the server took the trouble to
// say, and leaving the editor's per-field error block permanently empty.
// AGENTS.md's fallback policy is exactly about this: surface the upstream
// message, never a generic stand-in.

afterEach(() => { vi.restoreAllMocks() })

// Every field of EquipmentInput, so a test can vary just the one it is
// about. The hook sends the whole record on a PUT (the API replaces rather
// than patches), so a partial object is not a legal call.
function equipmentInput(overrides: Partial<EquipmentInput> = {}): EquipmentInput {
  return {
    name: 'Generator',
    category: 'mechanical',
    system: 'electrical',
    manufacturer: '',
    model: '',
    serial: '',
    quantity: 1,
    status: 'deployed',
    zone_id: null,
    bin_id: null,
    location_detail: '',
    install_date: '',
    hour_meter_path: '',
    profile_id: '',
    aliases: [],
    verified_aboard: false,
    notes: '',
    ...overrides,
  }
}

function jsonResponse(status: number, body: unknown) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

describe('use-inventory writes', () => {
  it('turns the server\'s own {field, message} validation body into a typed error', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(200, {
        item: { id: 'eq-1', name: 'Generator', aliases: [] },
        documents: [],
      }))
      .mockResolvedValueOnce(jsonResponse(400, {
        field: 'install_date',
        message: 'install_date must be blank or YYYY-MM-DD',
      }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useEquipmentItem('eq-1'))
    await waitFor(() => { expect(result.current.item).not.toBeNull() })

    let caught: unknown
    await act(async () => {
      try {
        await result.current.update(equipmentInput({ install_date: '14/03/2026' }))
      } catch (err) {
        caught = err
      }
    })

    expect(caught).toBeInstanceOf(InventoryValidationError)
    const err = caught as InventoryValidationError
    expect(err.fields).toEqual([
      { field: 'install_date', message: 'install_date must be blank or YYYY-MM-DD' },
    ])
    // The message the editor shows is the server's own sentence, not a
    // status code.
    expect(err.message).toBe('install_date must be blank or YYYY-MM-DD')
  })

  it('still reads a plain {error} body, which is what conflicts and not-founds send', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(jsonResponse(200, {
        item: { id: 'eq-1', name: 'Generator', aliases: [] },
        documents: [],
      }))
      .mockResolvedValueOnce(jsonResponse(409, { error: 'zone is in use: 1 bin(s) still reference it' }))
    vi.stubGlobal('fetch', fetchMock)

    const { result } = renderHook(() => useEquipmentItem('eq-1'))
    await waitFor(() => { expect(result.current.item).not.toBeNull() })

    let caught: unknown
    await act(async () => {
      try {
        await result.current.update(equipmentInput())
      } catch (err) {
        caught = err
      }
    })

    expect(caught).toBeInstanceOf(Error)
    expect(caught).not.toBeInstanceOf(InventoryValidationError)
    expect((caught as Error).message).toBe('zone is in use: 1 bin(s) still reference it')
  })
})
