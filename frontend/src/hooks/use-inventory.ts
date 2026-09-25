import { useCallback, useEffect, useRef, useState } from 'react'
import { apiBaseUrl } from '@/config/api'
import { readErrorMessage } from '@/lib/api-error'

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
  /** ADR 0127: orders a link within its OWN item's photo strip - meaningless
   * for a non-photo link, where the server leaves it at 0. */
  sort_index: number
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
  /** ADR 0127, amended 2026-09-25: a VIEW over the item's own linked
   * documents - whichever ones are image/jpeg or image/png, cover first, no
   * tag involved. Never every linked document (that's link_count/the
   * Documents tab, which now shows photos too) - just the image-MIME
   * subset the bin page's photo strip and the editor's photo row both read. */
  photo_ids: string[]
  /** 2026-09-25 amendment: the subset of photo_ids that reference NOTHING
   * else - no other item's equipment_documents link still references the
   * same document. This is what the delete dialogs offer to also delete:
   * an item delete's "Also delete N photo(s) only this item uses"
   * checkbox, and the strip's per-photo "Remove and delete" choice. Only
   * ever populated by a single-item GET (useEquipmentItem) - a listing
   * (useEquipment) always gets back an empty array here, see the backend's
   * own doc comment on why. */
  exclusive_photo_ids: string[]
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

/** A brand new, unsaved equipment record - every field at its own default,
 * matching backend validateEquipmentInput's own defaults (system 'other',
 * status 'deployed') wherever one applies. Shared by the Equipment editor's
 * own "New item" draft and the bin page's quick-add form (bin-quick-add.tsx),
 * which spreads this and overrides only the handful of fields it actually
 * collects (name, quantity, category, status, zone_id, bin_id) rather than
 * spelling out every field of its own. */
export const BLANK_DRAFT: EquipmentInput = {
  name: '',
  category: 'general',
  system: 'other',
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
}

export interface EquipmentFilter {
  category?: EquipmentCategory | ''
  system?: EquipmentSystem | ''
  status?: EquipmentStatus | ''
  zone?: string
  /** ADR 0127: the bin page's own filter - `useEquipment({ bin: bin.id })`. */
  bin?: string
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
 * contract useDocuments(folderId) gives folderId. `null` fetches nothing at
 * all (items stay `[]`, loading false) - the same "meaningless without a
 * scope" shape useEquipmentItem(null) gives a brand new draft - for a
 * caller like StocktakeSection that only has something to filter BY once
 * the operator has scanned a bin, rather than that caller having to invent
 * a filter value that can never match a real record.
 */
export function useEquipment(filter: EquipmentFilter | null) {
  const [items, setItems] = useState<EquipmentItem[]>([])
  const [loading, setLoading] = useState(filter !== null)
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
  const filterKey = filter === null
    ? null
    : JSON.stringify([filter.category ?? '', filter.system ?? '', filter.status ?? '', filter.zone ?? '', filter.bin ?? '', filter.q ?? ''])

  const refresh = useCallback(async () => {
    if (filter === null) {
      seqRef.current += 1
      setItems([])
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    setLoading(true)
    try {
      const params = new URLSearchParams()
      if (filter.category) params.set('category', filter.category)
      if (filter.system) params.set('system', filter.system)
      if (filter.status) params.set('status', filter.status)
      if (filter.zone) params.set('zone', filter.zone)
      if (filter.bin) params.set('bin', filter.bin)
      if (filter.q) params.set('q', filter.q)
      const qs = params.toString()
      const res = await fetch(`${apiBaseUrl}/api/inventory/equipment${qs ? `?${qs}` : ''}`)
      if (!res.ok) throw new Error(await readErrorMessage(res))
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
  const [item, setItemState] = useState<EquipmentItem | null>(null)
  const [documents, setDocuments] = useState<EquipmentDocument[]>([])
  const [loading, setLoading] = useState(id !== null)
  const [error, setError] = useState<string | null>(null)

  // Same ordering guard as useEquipment's own refresh() above, and for the
  // same reason: a GET for an id the operator has since navigated away from
  // must not have its late reply overwrite whatever a newer call already set.
  const seqRef = useRef(0)

  // Review finding: seqRef alone conflated two different races. setItem
  // bumping it (below) correctly stops a stale GET's `item` from winning
  // against a newer write (the create-then-upload race this hook's own
  // history comment describes) - but a GET in flight checks the SAME
  // seqRef for its documents/error/loading too, so that write collaterally
  // discarded them as well, and left `loading` stuck true forever once
  // nothing else was left to flip it back off (the `finally` block's own
  // `seq === seqRef.current` check fails right along with everything else).
  // update() had the opposite gap: it never touched seqRef at all, so a
  // slower GET for the same id already in flight could still resolve AFTER
  // it and clobber the just-written item with stale data.
  //
  // itemSeqRef is a SEPARATE counter, bumped only by a write applying an
  // item directly (setItem, update()) - a GET captures it when it starts,
  // and only skips re-applying `item` from its own response if a write has
  // landed since; documents/error/loading are never gated by it, so a GET's
  // own results outside the item race still land normally.
  const itemSeqRef = useRef(0)

  const refresh = useCallback(async () => {
    if (id === null) {
      seqRef.current += 1
      setItemState(null)
      setDocuments([])
      setError(null)
      setLoading(false)
      return
    }
    const seq = (seqRef.current += 1)
    const itemSeqAtStart = itemSeqRef.current
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
      if (itemSeqRef.current === itemSeqAtStart) setItemState(data.item)
      setDocuments(data.documents ?? [])
      setError(null)
    } catch (err) {
      if (seq !== seqRef.current) return
      if (itemSeqRef.current === itemSeqAtStart) setItemState(null)
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
    // Review finding: this never used to bump itemSeqRef, so a slower GET
    // for the same id already in flight when this PUT lands could still
    // resolve afterward and win, overwriting the just-saved item with
    // whatever stale copy it fetched before the PUT ever happened.
    itemSeqRef.current += 1
    setItemState(data.item)
    return data.item
  }, [id])

  // deletePhotos (2026-09-25 amendment) is the operator's own explicit
  // choice, surfaced by the delete confirm dialog's "Also delete N
  // photo(s) only this item uses" checkbox (off by default) - an ordinary
  // delete only ever unlinks, never destroys a document, see equipmentItem's
  // own exclusive_photo_ids doc comment.
  const remove = useCallback(async (deletePhotos = false) => {
    if (id === null) throw new Error('useEquipmentItem: no id to delete')
    const qs = deletePhotos ? '?delete_photos=true' : ''
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}${qs}`, 'DELETE')
  }, [id])

  // Applies a DIFF to the link set (backend PATCH /documents: {add, remove}
  // - replaces the old whole-set PUT, ADR 0123's "Links edited from the
  // equipment side this cycle" is still true, only the wire shape changed).
  // The caller only ever has to say what changed, not restate the ids it
  // isn't touching - a link this call doesn't name (a just-uploaded photo,
  // say) is never at risk of being silently unlinked. Re-fetches afterward
  // rather than trusting a locally-merged guess at the response shape - the
  // join back to each document's title/filename/kind/note_type only the
  // server can do, and this is the one place that already knows how to ask
  // for it.
  const patchLinkedDocuments = useCallback(async (add: string[], remove: string[]) => {
    if (id === null) throw new Error('useEquipmentItem: no id to patch documents on')
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}/documents`, 'PATCH', { add, remove })
    await refresh()
  }, [id, refresh])

  // Exposed (rather than making every photo write below call refresh()) so
  // a caller holding an EquipmentItem the server just handed back directly
  // - uploadEquipmentPhoto/setEquipmentPhotoOrder/deleteEquipmentPhoto below
  // all return the updated item the same way update() does - can apply it
  // straight to this hook's own state instead of firing a second, redundant
  // GET. ADR 0127 review: equipment-editor.tsx's create-with-photos flow
  // used to do neither (no refresh, no setItem) after each upload, so the
  // photo strip stayed empty until something else happened to re-fetch -
  // which for a brand new draft was only the ONE GET useEquipmentItem's own
  // effect fires the instant `id` turns from null into the created id, a
  // request that typically lands before the photo uploads that follow it
  // even start.
  //
  // Review finding: that GET is not guaranteed to land first, only to fire
  // first. A caller's own setItem, applied while it's still in flight, used
  // to leave seqRef untouched, so the GET's own ordering guard (seq !==
  // seqRef.current, above) never saw anything to disagree with and its
  // late, stale reply was free to overwrite a newer write - the create-
  // then-upload race this hook's id-change GET can lose against
  // equipment-editor.tsx's own per-upload setItem(updated) calls, silently
  // dropping photos back off the strip once that GET finally landed.
  // Bumping itemSeqRef here invalidates any GET already in flight's own
  // `item` the moment a caller hands this a fresher one - see itemSeqRef's
  // own doc comment (above, by seqRef) for why this is a separate counter
  // from seqRef rather than reusing it: reusing it also discarded that
  // GET's documents/error and left loading stuck true.
  // The editor is not remounted between records, so a write's response can
  // arrive after Back/Forward has already moved this hook to another id - a
  // photo upload for item A landing while item B is open. Applying it would
  // show A's fields under B's id (and a Save would then write them onto B),
  // and bumping itemSeqRef would discard B's own GET. A record that isn't
  // the one open is therefore dropped (final pre-release review finding).
  //
  // `adopt` is the create-then-upload case: Save's create hands the new id
  // to App.tsx, but the first photo upload can resolve before the re-render
  // that brings that id back in as this hook's `id`. An adopting write may
  // claim the id only while no record is open yet (still the draft's null).
  const idRef = useRef(id)
  idRef.current = id

  const setItem = useCallback((next: EquipmentItem, options?: { adopt?: boolean }) => {
    if (options?.adopt && idRef.current === null) idRef.current = next.id
    if (next.id !== idRef.current) return
    itemSeqRef.current += 1
    setItemState(next)
  }, [])

  // Review finding: deleteEquipmentPhoto's own DELETE returns only the
  // updated item (photo_ids with the id gone) - `documents`, fetched once at
  // mount/refresh, still carries that same id's link until something
  // re-fetches it. equipment-editor.tsx's Documents tab excludes photo-
  // tagged links by checking id membership in item.photo_ids, so the moment
  // photo_ids stops naming it, the STILL-STALE documents array makes the
  // just-removed photo look like an ordinary linked document again - visibly
  // reappearing in the tab, and eligible to be sent right back on the next
  // link-set PUT. Pruning it here, at the one write that can make it stale,
  // keeps `documents` correct without a second GET (refresh() would also
  // fix it, but costs a redundant round trip for a response this hook
  // already has everything it needs from).
  const pruneDocument = useCallback((documentId: string) => {
    setDocuments((prev) => prev.filter((d) => d.document_id !== documentId))
  }, [])

  return { item, documents, loading, error, refresh, update, remove, patchLinkedDocuments, setItem, pruneDocument }
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

/** Thrown by fetchEquipment specifically for a 404 - the one failure
 * stocktake-section.tsx's own scan handler treats as "no such item, report
 * it as an unrecognised scan". Every other failure (a genuine server error,
 * a network drop) is a distinct Error instead, so it surfaces as an actual
 * error rather than folding into the same "not an inventory tag" bucket a
 * bad connection would otherwise share with a mistyped id (AGENTS.md
 * fallback policy). */
export class EquipmentNotFoundError extends Error {}

/** GET /api/inventory/equipment/:id - a standalone, one-off fetch (not
 * useEquipmentItem's own persistent hook instance) for a caller that reads
 * an ARBITRARY id it doesn't already hold state for - stocktake-section.tsx
 * scanning a different item's tag on every pass, one after another. */
export async function fetchEquipment(id: string): Promise<EquipmentItem> {
  const res = await fetch(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}`)
  if (!res.ok) {
    const message = await readErrorMessage(res)
    if (res.status === 404) throw new EquipmentNotFoundError(message)
    throw new Error(message)
  }
  const data = (await res.json()) as { item: EquipmentItem }
  return data.item
}

/** Strips an equipmentItem down to its own editable EquipmentInput shape -
 * draftFromItem's own reasoning (equipment-editor.tsx), pulled out here so
 * a caller with no editor draft of its own (stocktake-section.tsx's
 * verified_aboard/bin_id writes) can still send a PUT's required
 * whole-record body built from a record it only just fetched. */
export function toEquipmentInput(item: EquipmentItem): EquipmentInput {
  return {
    name: item.name,
    category: item.category,
    system: item.system,
    manufacturer: item.manufacturer,
    model: item.model,
    serial: item.serial,
    quantity: item.quantity,
    status: item.status,
    zone_id: item.zone_id,
    bin_id: item.bin_id,
    location_detail: item.location_detail,
    install_date: item.install_date,
    hour_meter_path: item.hour_meter_path,
    profile_id: item.profile_id,
    aliases: item.aliases,
    verified_aboard: item.verified_aboard,
    notes: item.notes,
  }
}

/** PUT /api/inventory/equipment/:id - standalone, for the same "no
 * persistent hook instance" reason fetchEquipment exists. */
export async function updateEquipment(id: string, input: EquipmentInput): Promise<EquipmentItem> {
  const data = await submitJSON<{ item: EquipmentItem }>(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(id)}`, 'PUT', input)
  return data.item
}

// ── equipment photos (ADR 0127) ─────────────────────────────────────────
// Three plain functions, not a hook - the same "standalone, not tied to a
// useEquipmentItem instance" reasoning createEquipment's own doc comment
// gives: the photo row works identically for a saved item (each action
// calls its route immediately) and, before Save, for a brand new draft
// held only as local Blobs (photo-strip-editor.tsx/bin-quick-add.tsx),
// neither of which has a persistent hook instance to hang these off.

/** POST /api/inventory/equipment/:id/photos - one multipart upload, linked
 * at the end of the item's photo order server-side. No tag is written
 * (2026-09-25 amendment) - the item's photo_ids view is MIME-based. Throws
 * the server's own message on a rejected upload (AGENTS.md fallback
 * policy: HEIC/non-image get a specific reason, never a generic one). A
 * byte-identical upload is no longer refused with a 409 - it links the
 * existing document and returns 200, same as any other success here. */
export async function uploadEquipmentPhoto(equipmentId: string, file: Blob, filename: string): Promise<EquipmentItem> {
  const form = new FormData()
  form.append('file', file, filename)
  const response = await fetch(`${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(equipmentId)}/photos`, {
    method: 'POST',
    body: form,
  })
  if (!response.ok) throw new Error(await readErrorMessage(response))
  const data = (await response.json()) as { item: EquipmentItem }
  return data.item
}

/** PUT /api/inventory/equipment/:id/photos - {document_ids} must name
 * EXACTLY the item's current photo set; "Make cover" is this same call
 * with the chosen id moved to the front. */
export async function setEquipmentPhotoOrder(equipmentId: string, documentIds: string[]): Promise<EquipmentItem> {
  const data = await submitJSON<{ item: EquipmentItem }>(
    `${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(equipmentId)}/photos`,
    'PUT',
    { document_ids: documentIds },
  )
  return data.item
}

/** DELETE /api/inventory/equipment/:id/photos/:documentId - removes the
 * photo from the item. UNLINK ONLY by default (2026-09-25 amendment,
 * superseding "a photo has no life outside its item") - deletePhotos is
 * the operator's own explicit choice, from the strip's "Remove and delete"
 * option (offered only when the photo is exclusive to this item - see
 * EquipmentItem's own exclusive_photo_ids doc comment), and additionally
 * deletes the document (and, server-side, its file) once nothing else
 * links it. */
export async function deleteEquipmentPhoto(equipmentId: string, documentId: string, deletePhotos = false): Promise<EquipmentItem> {
  const qs = deletePhotos ? '?delete=true' : ''
  const data = await submitJSON<{ item: EquipmentItem }>(
    `${apiBaseUrl}/api/inventory/equipment/${encodeURIComponent(equipmentId)}/photos/${encodeURIComponent(documentId)}${qs}`,
    'DELETE',
  )
  return data.item
}

/** Case-insensitive bin-code lookup against a zone tree - a bin code is
 * "short and human-chosen" (ADR 0127 §2), so this is never a server round
 * trip, just a scan over the already-fetched zone list. Shared by the bin
 * page (resolving `/inventory/bins/<code>`, bin-page.tsx) and Stocktake
 * (resolving a scanned bin code, stocktake-section.tsx), which each
 * duplicated this exact loop before. */
export function findBinByCode(zones: InventoryZone[], code: string): { zone: InventoryZone; bin: InventoryBin } | null {
  const lower = code.toLowerCase()
  for (const zone of zones) {
    const bin = zone.bins.find((b) => b.code.toLowerCase() === lower)
    if (bin) return { zone, bin }
  }
  return null
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
  // Returns the created record (ADR 0127: the bin page's own "Create bin"
  // flow needs the new zone's id to then create a bin under it, and the
  // caller-side `zones` state isn't updated synchronously by refresh()'s own
  // setZones - a caller reading it right after this resolves would see
  // whatever this render still holds, not the new zone). Every existing
  // caller (locations-section.tsx) already discards the return value, so
  // this is additive.
  const createZone = useCallback(async (name: string) => {
    const data = await submitJSON<{ zone: InventoryZone }>(`${apiBaseUrl}/api/inventory/zones`, 'POST', { name })
    await refresh()
    return data.zone
  }, [refresh])

  const renameZone = useCallback(async (id: string, name: string) => {
    await submitJSON<unknown>(`${apiBaseUrl}/api/inventory/zones/${encodeURIComponent(id)}`, 'PUT', { name })
    await refresh()
  }, [refresh])

  const deleteZone = useCallback(async (id: string) => {
    await submitJSON<void>(`${apiBaseUrl}/api/inventory/zones/${encodeURIComponent(id)}`, 'DELETE')
    await refresh()
  }, [refresh])

  // Returns the created record - same reasoning as createZone's own comment
  // just above.
  const createBin = useCallback(async (zoneId: string, code: string, name: string) => {
    const data = await submitJSON<{ bin: InventoryBin }>(`${apiBaseUrl}/api/inventory/bins`, 'POST', { zone_id: zoneId, code, name })
    await refresh()
    return data.bin
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
