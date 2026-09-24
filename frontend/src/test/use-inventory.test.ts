import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

import { useEquipmentItem, InventoryValidationError, type EquipmentInput, type EquipmentItem } from '@/hooks/use-inventory'

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

function equipmentItem(overrides: Partial<EquipmentItem> = {}): EquipmentItem {
  return {
    id: 'eq-new',
    name: 'Spare impeller',
    category: 'general',
    system: 'other',
    manufacturer: '',
    model: '',
    serial: '',
    quantity: 1,
    status: 'stored',
    zone_id: null,
    bin_id: null,
    zone_name: '',
    bin_code: '',
    location_detail: '',
    install_date: '',
    hour_meter_path: '',
    profile_id: '',
    aliases: [],
    verified_aboard: false,
    notes: '',
    link_count: 0,
    created_at: '',
    updated_at: '',
    photo_ids: [],
    ...overrides,
  }
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

// ADR 0127 review finding: the create-then-upload race. equipment-editor.
// tsx's performSave POSTs a new draft, hands the created id to App.tsx
// (which flips the `id` prop this hook is keyed on - the id-change GET
// below), and then uploads each picked photo, applying every response
// directly via this hook's own setItem rather than waiting on that GET.
// setItem used to leave seqRef - the SAME ordering guard refresh() already
// uses to discard a stale reply - completely untouched, so a slow id-change
// GET that starts before the uploads but resolves after them was free to
// overwrite the newer, photo-bearing item with whatever it fetched.
describe('useEquipmentItem: the create-then-upload race', () => {
  it('discards a slow id-change GET that resolves after setItem has already applied a newer item', async () => {
    let resolveGet!: (value: Response) => void
    const fetchMock = vi.fn().mockImplementation(() => new Promise<Response>((resolve) => { resolveGet = resolve }))
    vi.stubGlobal('fetch', fetchMock)

    const { result, rerender } = renderHook(({ id }) => useEquipmentItem(id), { initialProps: { id: null as string | null } })
    expect(result.current.item).toBeNull()

    // App.tsx flips the id prop the instant create's POST resolves - this
    // hook's own effect fires the id-change GET (still pending, per the
    // fetchMock above) before any photo upload has even started.
    rerender({ id: 'eq-new' })
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1))

    // A photo upload lands and applies its own response - exactly what
    // equipment-editor.tsx's uploadPhotosToSavedItem/performSave do with
    // each upload's returned item.
    const newerItem = equipmentItem({ photo_ids: ['photo-1'] })
    act(() => { result.current.setItem(newerItem) })
    expect(result.current.item).toEqual(newerItem)

    // The id-change GET FINALLY resolves - with a STALE item (no photos, as
    // of just after create) fetched before the upload above ever happened.
    await act(async () => {
      resolveGet(jsonResponse(200, { item: equipmentItem({ photo_ids: [] }), documents: [] }))
      await Promise.resolve()
    })

    expect(result.current.item).toEqual(newerItem)
  })
})
