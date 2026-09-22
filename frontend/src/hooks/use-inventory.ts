import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'

// ADR 0123: the Inventory panel's data layer - the equipment registry and
// its zones/bins. Same idiom as use-documents.ts/use-manuals.ts throughout:
// no react-query, no shared store, one instance per caller, a plain
// fetch-and-throw-the-server's-own-message write helper (AGENTS.md fallback
// policy). Split into three hooks rather than one, mirroring how
// use-documents.ts splits useDocuments(folderId) (a listing, parametrised by
// the caller-owned filter) from useDocument(id) (one record, parametrised by
// the caller-owned selection) - EquipmentIndex only ever needs the filtered
// list, EquipmentEditor only ever needs one record, and the two are never
// mounted at the same time (App.tsx swaps one for the other, the same
// index/editor split ADR 0112's wall-displays panel and ADR 0115's Documents
// Details page already use). useInventoryZones is its own hook rather than
// folded into either of the other two because BOTH of them need it at once
// (the index's zone filter, the editor's zone/bin selects) and it never
// changes shape with either one's own parameter.

export type EquipmentCategory = 'mechanical' | 'general'
export type EquipmentStatus = 'deployed' | 'stored'

// Enum order, not alphabetical - matches the plan's grouping order exactly
// (backend/inventory_store.go's own CHECK constraint lists them the same
// way), because EquipmentIndex groups its rows by system in this order, not
// alphabetically.
export const EQUIPMENT_SYSTEMS = [
  'propulsion', 'electrical', 'water', 'fuel', 'bilge', 'anchoring',
  'safety', 'hvac', 'navigation', 'appliances', 'structure', 'other',
] as const
export type EquipmentSystem = (typeof EQUIPMENT_SYSTEMS)[number]

export const EQUIPMENT_SYSTEM_LABELS: Record<EquipmentSystem, string> = {
  propulsion: 'Propulsion',
  electrical: 'Electrical',
  water: 'Water',
  fuel: 'Fuel',
  bilge: 'Bilge',
  anchoring: 'Anchoring',
  safety: 'Safety',
  hvac: 'HVAC',
  navigation: 'Navigation',
  appliances: 'Appliances',
  structure: 'Structure',
  other: 'Other',
}

/** equipmentDocument, backend/inventory_store.go - a document linked to an
 * equipment record, joined with the document's own title/filename/kind/
 * note_type at read time (GET /api/inventory/equipment/:id). `source` mirrors
 * document_tags.source ('operator' | 'suggested') - this cycle only ever
 * writes 'operator' rows (links are edited from the equipment side, ADR
 * 0123 decisions), but the field is read here as a plain string rather than
 * a narrowed union so a future 'suggested' row (the enrichment cycle) shows
 * up as data rather than failing to parse. */
export interface EquipmentDocument {
  document_id: string
  title: string
  filename: string
  kind: string
  note_type: string
  source: string
}

/** inventoryBin, backend/inventory_store.go - a numbered container within
 * one zone, always read nested under its zone (GET /api/inventory/zones). */
export interface InventoryBin {
  id: string
  zone_id: string
  code: string
  name: string
  sort_index: number
}

/** inventoryZone, backend/inventory_store.go. */
export interface InventoryZone {
  id: string
  name: string
  sort_index: number
  bins: InventoryBin[]
}

/** equipmentItem, backend/inventory_store.go. zone_id/bin_id are the raw
 * foreign keys (null when unset); zone_name/bin_code are joined in for
 * display only (blank, never null, when unset) - the same "joined for
 * display, not authoritative" shape DocumentRecord's own folder fields
 * don't need but EquipmentIndex's location column does, to avoid every
 * caller re-deriving a zone's name from its id against a separately-fetched
 * zone list. */
export interface EquipmentItem {
  id: string
  name: string
  category: EquipmentCategory
  system: EquipmentSystem
  manufacturer: string
  model: string
  serial: string
  quantity: number
  status: EquipmentStatus
  zone_id: string | null
  bin_id: string | null
  zone_name: string
  bin_code: string
  location_detail: string
  install_date: string
  hour_meter_path: string
  profile_id: string
  aliases: string[]
  verified_aboard: boolean
  notes: string
  link_count: number
  created_at: string
  updated_at: string
}

/** The body EquipmentEditor sends on create (POST) and save (PUT) - every
 * equipmentItem field the operator can actually edit, i.e. everything
 * except the id and the read-only joined/derived fields (zone_name,
 * bin_code, link_count, created_at, updated_at). */
export interface EquipmentInput {
  name: string
  category: EquipmentCategory
  system: EquipmentSystem
  manufacturer: string
  model: string
  serial: string
  quantity: number
  status: EquipmentStatus
  zone_id: string | null
  bin_id: string | null
  location_detail: string
  install_date: string
  hour_meter_path: string
  profile_id: string
  aliases: string[]
  verified_aboard: boolean
  notes: string
}

export interface EquipmentFilter {
  category?: EquipmentCategory | ''
  system?: EquipmentSystem | ''
  status?: EquipmentStatus | ''
  zone?: string
  q?: string
}

/** One `{field, message}` entry from a 4xx validation failure
 * (backend/inventory_handlers.go's validateEquipmentInput). */
export interface InventoryFieldError {
  field: string
  message: string
}

// Thrown by submitJSON below when the server responded with a structured
// `{errors: [...]}` validation body rather than a single `{error}` string -
// distinct from a plain Error so a caller that wants to place each message
// next to its own field (EquipmentEditor) can, while a caller that just
// wants one string to show (LocationsSection, a 409) can treat it as an
// ordinary Error and read `.message`. AGENTS.md fallback policy: `.message`
// is built ONLY from the server's own per-field messages, joined, never a
// generic "check the form" standing in for them.
export class InventoryValidationError extends Error {
  fields: InventoryFieldError[]
  constructor(fields: InventoryFieldError[]) {
    super(fields.map((f) => f.message).join('; '))
    this.name = 'InventoryValidationError'
    this.fields = fields
  }
}

// Every write below goes through this: a non-2xx response throws with the
// server's own message (AGENTS.md fallback policy - no invented "something
// went wrong" standing in for a 409's actual "zone is in use by 3 items" or
// a validation failure's actual per-field reasons). 204 (every DELETE) has
// no body to parse. Copied from use-documents.ts's own submitJSON rather
// than imported - see that file's own comment on why a two-line helper is
// cheaper to duplicate a third time than to thread a shared-module
// dependency between features that otherwise don't know about each other -
// with one addition: a field validation failure throws the richer
// InventoryValidationError so the editor can mark the offending input.
//
// Three body shapes reach here, and all three are the server's own words:
//
//   {"field":"install_date","message":"..."}  one rejected field
//       (writeInventoryValidationError, inventory_handlers.go) - the common
//       case, and the one an earlier cut of this function missed entirely by
//       looking only for the array below, turning every validation failure
//       into a bare "HTTP 400" and leaving the per-field error block dead.
//   {"errors":[{field,message}, ...]}         several at once, if this API
//       ever grows a bulk write; cheap to keep recognised.
//   {"error":"zone is in use: ..."}           conflicts and not-founds,
//       which belong to the record as a whole rather than to one input.
//
// A status code is the last resort, only for a body that parses as none of
// them (AGENTS.md fallback policy: never a generic message standing in for
// one the server actually sent).
async function submitJSON<T>(url: string, method: string, body?: unknown): Promise<T> {
  const response = await fetch(url, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : undefined,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as {
      error?: string
      errors?: InventoryFieldError[]
      field?: string
      message?: string
    }
    if (Array.isArray(payload.errors) && payload.errors.length > 0) {
      throw new InventoryValidationError(payload.errors)
    }
    if (typeof payload.field === 'string' && typeof payload.message === 'string') {
      throw new InventoryValidationError([{ field: payload.field, message: payload.message }])
    }
    throw new Error(payload.error ?? `HTTP ${response.status}`)
  }
  if (response.status === 204) return undefined as T
  return (await response.json()) as T
}

/**
 * The Equipment index's filtered listing - `?category=&system=&status=&zone=&q=`.
 * `filter` is caller-owned (EquipmentIndex's own toolbar state), the same
 * contract useDocuments(folderId) gives folderId.
 */
export function useEquipment(filter: EquipmentFilter) {
  const [items, setItems] = useState<EquipmentItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  // Ordering guard (same idiom as use-documents.ts's refreshSeqRef): a
  // filter change fires a new request before the previous one lands, and
  // without this the OLDER, now-stale response could still win and show
  // results for a filter the operator has already changed away from.
  const seqRef = useRef(0)

  // filter is a fresh object literal on every render from most callers
  // (EquipmentIndex builds it inline from several useState values) - keying
  // the effect on its serialized contents, not its identity, is what keeps
  // the request from refiring every render for no filter change at all.
  const filterKey = JSON.stringify([filter.category ?? '', filter.system ?? '', filter.status ?? '', filter.zone ?? '', filter.q ?? ''])

  const refresh = useCallback(async () => {
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filter.category) params.set('category', filter.category)
      if (filter.system) params.set('system', filter.system)
      if (filter.status) params.set('status', filter.status)
      if (filter.zone) params.set('zone', filter.zone)
      if (filter.q) params.set('q', filter.q)
      const qs = params.toString()
      const res = await fetch(`${apiBaseUrl}/api/inventory/equipment${qs ? `?${qs}` : ''}`)
      if (!res.ok) {
        const payload = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(payload.error ?? `HTTP ${res.status}`)
      }
      const data = (await res.json()) as { items?: EquipmentItem[] }
      if (seq !== seqRef.current) return
      setItems(data.items ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
    // filterKey stands in for filter's actual fields - see the comment above it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey])

  useEffect(() => { void refresh() }, [refresh])

  return { items, loading, error, refresh }
}

/**
 * One equipment record plus its linked documents - `id === null` (a brand
 * new, not-yet-saved draft; App.tsx's inventoryCreatingEquipment) clears
 * state and fetches nothing, the same shape useDocument(null) gives an
 * unresolved Details route.
 */
export function useEquipmentItem(id: string | null) {
  const [item, setItem] = useState<EquipmentItem | null>(null)
  const [documents, setDocuments] = useState<EquipmentDocument[]>([])
  const [loading, setLoading] = useState(id !== null)
  const [error, setError] = useState<string | null>(null)

  // Same ordering guard as useEquipment's own refresh() above, and for the
  // same reason: a GET for an id the operator has since navigated away from
  // must not have its late reply overwrite whatever a newer call already set.
  const seqRef = useRef(0)

  const refresh = useCallback(async () => {
    if (id === null) {
      seqRef.current += 1
      setItem(null)
      setDocuments([])
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}`)
      if (!res.ok) {
        // AGENTS.md fallback policy: the server's own message (e.g. "item
        // not found"), never an invented "something went wrong".
        const payload = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(payload.error ?? `HTTP ${res.status}`)
      }
      const data = (await res.json()) as { item: EquipmentItem; documents?: EquipmentDocument[] }
      if (seq !== seqRef.current) return
      setItem(data.item)
      setDocuments(data.documents ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setItem(null)
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [id])

  useEffect(() => { void refresh() }, [refresh])

  // Throws on a rejected PUT (a validation failure, a bin/zone that
  // disagree) rather than swallowing it - the caller (EquipmentEditor's
  // Save) is the one that has to show the failure and keep the draft
  // intact, mirroring useDocument(id)'s own patch().
  const update = useCallback(async (input: EquipmentInput) => {
    if (id === null) throw new Error('useEquipmentItem: no id to update')
    const data = await submitJSON<{ item: EquipmentItem }>(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}`, 'PUT', input)
    setItem(data.item)
    return data.item
  }, [id])

  const remove = useCallback(async () => {
    if (id === null) throw new Error('useEquipmentItem: no id to delete')
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}`, 'DELETE')
  }, [id])

  // Replaces the WHOLE link set (ADR 0123: "Links edited from the equipment
  // side this cycle... replace wholesale"). Re-fetches afterward rather than
  // trusting a locally-merged guess at the response shape - the join back to
  // each document's title/filename/kind/note_type only the server can do,
  // and this is the one place that already knows how to ask for it.
  const setLinkedDocuments = useCallback(async (documentIds: string[]) => {
    if (id === null) throw new Error('useEquipmentItem: no id to set documents on')
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}/documents`, 'PUT', { document_ids: documentIds })
    await refresh()
  }, [id, refresh])

  return { item, documents, loading, error, refresh, update, remove, setLinkedDocuments }
}

/** Creates a brand new equipment record - standalone (not tied to any
 * useEquipmentItem instance) because the "New item" draft has no id yet to
 * parametrise a hook with. EquipmentEditor calls this directly, then hands
 * the returned item's id to App.tsx so the URL becomes
 * `/inventory/equipment/<id>` and a fresh useEquipmentItem(id) takes over -
 * "Create flow: POST, then navigate to the new id" (the plan). */
export async function createEquipment(input: EquipmentInput): Promise<EquipmentItem> {
  const data = await submitJSON<{ item: EquipmentItem }>(`${apiBaseUrl}/api/inventory/equipment`, 'POST', input)
  return data.item
}

/** Zones (with their nested bins) plus every zone/bin write - shared by
 * EquipmentEditor's zone/bin selects and LocationsSection's own CRUD, so it
 * is its own hook rather than folded into either useEquipment or
 * useEquipmentItem above (see this file's header comment). */
export function useInventoryZones() {
  const [zones, setZones] = useState<InventoryZone[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const seqRef = useRef(0)

  const refresh = useCallback(async () => {
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const res = await fetch(`${apiBaseUrl}/api/inventory/zones`)
      if (!res.ok) {
        const payload = (await res.json().catch(() => ({}))) as { error?: string }
        throw new Error(payload.error ?? `HTTP ${res.status}`)
      }
      const data = (await res.json()) as { zones?: InventoryZone[] }
      if (seq !== seqRef.current) return
      setZones(data.zones ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      setError(err instanceof Error ? err.message : String(err))
    } finally {
      if (seq === seqRef.current) setLoading(false)
    }
  }, [])

  useEffect(() => { void refresh() }, [refresh])

  // Every write below re-fetches the whole zone list afterward rather than
  // merging its own response locally - sort_index and a bin's zone
  // membership are server-decided, and re-fetching the canonical shape costs
  // one small GET against getting either wrong. The 409 "in use" body
  // (deleteZone/deleteBin) is a plain `{error}` naming what's still filed in
  // it (ADR 0123 decisions) - submitJSON's ordinary Error path already
  // surfaces that message verbatim, so no special handling is needed here.
  const createZone = useCallback(async (name: string) => {
    await submitJSON<unknown>(`${apiBaseUrl}/api/inventory/zones`, 'POST', { name })
    await refresh()
  }, [refresh])

  const renameZone = useCallback(async (id: string, name: string) => {
    await submitJSON<unknown>(`${apiBaseUrl}/api/inventory/zones/${encodeURIComponent(id)}`, 'PUT', { name })
    await refresh()
  }, [refresh])

  const deleteZone = useCallback(async (id: string) => {
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/zones/${encodeURIComponent(id)}`, 'DELETE')
    await refresh()
  }, [refresh])

  const createBin = useCallback(async (zoneId: string, code: string, name: string) => {
    await submitJSON<unknown>(`${apiBaseUrl}/api/inventory/bins`, 'POST', { zone_id: zoneId, code, name })
    await refresh()
  }, [refresh])

  const renameBin = useCallback(async (id: string, patch: { code?: string; name?: string }) => {
    await submitJSON<unknown>(`${apiBaseUrl}/api/inventory/bins/${encodeURIComponent(id)}`, 'PUT', patch)
    await refresh()
  }, [refresh])

  const deleteBin = useCallback(async (id: string) => {
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/bins/${encodeURIComponent(id)}`, 'DELETE')
    await refresh()
  }, [refresh])

  return { zones, loading, error, refresh, createZone, renameZone, deleteZone, createBin, renameBin, deleteBin }
}
