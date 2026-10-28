import { useCallback, useRef } from 'react'
import { apiBaseUrl } from '@/config/api'
import { ApiError, apiRequest, declareQuery, fetchQuery, getQueryData, invalidateQueries, setQueryData, useApiQuery } from '@/lib/api-query'
import type { EquipmentDependants } from '@/lib/equipment-dependants'
import type { SourceKind } from '@/lib/vessel-settings'

// ADR 0123: the Inventory panel's data layer - the equipment registry and
// its zones/bins. Reads go through useApiQuery and writes through apiRequest
// (ADR 0173), which throws the server's own message (AGENTS.md fallback
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
  /** The document's MIME type and upload time, for the Documents list's type
   * badge, image thumbnail and date. */
  mime: string
  created_at: string
}

/** inventoryBin, backend/inventory_store.go - a numbered container within
 * one zone, always read nested under its zone (GET /api/inventory/zones). */
export interface InventoryBin {
  id: string
  zone_id: string
  code: string
  name: string
  sort_index: number
  /** Where the bin sits on its location's deck plan, as fractions (0..1) of
   * the plan image; null or absent when it has not been placed. */
  pin?: { x: number; y: number } | null
}

/** inventoryZone, backend/inventory_store.go. */
export interface InventoryZone {
  id: string
  name: string
  sort_index: number
  bins: InventoryBin[]
  /** The deck plan this location is outlined on; null or absent when it is
   * not on a plan. Set together with polygon. */
  deck_id?: string | null
  /** The outline on the plan: 3 to 64 [x, y] points, fractions (0..1) of the
   * plan image. */
  polygon?: Array<[number, number]> | null
}

/** inventoryDeck, backend/inventory_decks_store.go - a deck plan image that
 * locations are outlined on. */
export interface InventoryDeck {
  id: string
  name: string
  sort_index: number
  plan_document_id: string | null
}

/** One deck's whole layout as the editor saves it: the outlines of the
 * locations on the plan and the pins of their bins. */
export interface DeckLayout {
  zones: Array<{ id: string; polygon: Array<[number, number]> }>
  bins: Array<{ id: string; x: number; y: number }>
}

/** The URL of a deck's plan image. */
export function deckPlanUrl(deck: Pick<InventoryDeck, 'plan_document_id'>): string | null {
  return deck.plan_document_id ? `${apiBaseUrl}/api/documents/${encodeURIComponent(deck.plan_document_id)}/content` : null
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
  /** How many are on board. 0 is valid: a spare that has run out. */
  quantity: number
  /** The supplier's part number for a spare; '' when none. */
  part_number: string
  /** How many the boat should carry; null when no target is set. */
  required_quantity: number | null
  status: EquipmentStatus
  zone_id: string | null
  bin_id: string | null
  zone_name: string
  bin_code: string
  location_detail: string
  install_date: string
  /** The one SignalK node this item owns, such as 'propulsion.port'; '' when it owns none. */
  signalk_source: string
  /** A battery item's bank figures; 0 means not entered (warn/high: use the profile's thresholds). */
  capacity_ah: number
  cells: number
  warn_voltage: number
  high_voltage: number
  /** A tank item's own alarm levels in percent of full; 0 means blank, the kind's default applies (ADR 0214). */
  tank_warn_percent: number
  tank_alarm_percent: number
  /** Silences this tank's alarms; the tile still colours by its levels. */
  tank_alarms_off: boolean
  /** Derived from signalk_source on every read: 'engine', 'battery', ... or ''. */
  source_kind: SourceKind | ''
  /** The live hour meter derived from the source, such as 'propulsion.port.runTime'; '' when it has none. */
  hour_meter_source: string
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
  system: EquipmentSystem
  manufacturer: string
  model: string
  serial: string
  quantity: number
  part_number: string
  required_quantity: number | null
  status: EquipmentStatus
  zone_id: string | null
  bin_id: string | null
  location_detail: string
  install_date: string
  signalk_source: string
  /** The id of the item that owns signalk_source and is to give it up (a swap or move); sent on an update only. */
  take_source_from?: string
  /** What the page told the operator would happen; the server refuses the save if it would do the other. */
  take_source_mode?: 'swap' | 'move'
  capacity_ah: number
  cells: number
  warn_voltage: number
  high_voltage: number
  tank_warn_percent: number
  tank_alarm_percent: number
  tank_alarms_off: boolean
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
 * collects (name, quantity, status, zone_id, bin_id) rather than
 * spelling out every field of its own. */
export const BLANK_DRAFT: EquipmentInput = {
  name: '',
  system: 'other',
  manufacturer: '',
  model: '',
  serial: '',
  quantity: 1,
  part_number: '',
  required_quantity: null,
  status: 'deployed',
  zone_id: null,
  bin_id: null,
  location_detail: '',
  install_date: '',
  signalk_source: '',
  capacity_ah: 0,
  cells: 0,
  warn_voltage: 0,
  high_voltage: 0,
  tank_warn_percent: 0,
  tank_alarm_percent: 0,
  tank_alarms_off: false,
  profile_id: '',
  aliases: [],
  verified_aboard: false,
  notes: '',
}

export interface EquipmentFilter {
  system?: EquipmentSystem | ''
  status?: EquipmentStatus | ''
  zone?: string
  /** ADR 0127: the bin page's own filter - `useEquipment({ bin: bin.id })`. */
  bin?: string
  q?: string
}

/** One `{field, message}` entry from a 4xx validation failure
 * (backend/inventory_handlers.go's validateEquipmentInput). A write that the
 * server rejects per field throws ApiError with these in `fieldErrors`. */
export interface InventoryFieldError {
  field: string
  message: string
}

// Cache keys follow lib/api-query.ts: the URL path, then the filter. A list
// key is [EQUIPMENT, filter] and an item key is [EQUIPMENT/<id>], so
// invalidating [EQUIPMENT] reaches every list and no single item.
const EQUIPMENT = '/api/inventory/equipment'
// Lists only: an item's key is the one string EQUIPMENT/<id>.
declareQuery([EQUIPMENT], { structuralSharing: true })
const ZONES = '/api/inventory/zones'
const DECKS = '/api/inventory/decks'
// Owned by use-maintenance.ts. An equipment write can change its schedule
// (profile, stored/deployed), so it invalidates the same prefix.
const MAINTENANCE_RULES = '/api/inventory/maintenance/rules'
const DISCOVERY = '/api/vessel/discovery'
const SETUP_STATUS = '/api/setup/status'

const NO_ITEMS: EquipmentItem[] = []
const NO_DOCUMENTS: EquipmentDocument[] = []
const NO_ZONES: InventoryZone[] = []
const NO_DECKS: InventoryDeck[] = []

function itemPath(id: string): string {
  return `${EQUIPMENT}/${encodeURIComponent(id)}`
}

interface EquipmentItemResponse {
  item: EquipmentItem
  documents?: EquipmentDocument[]
}

// A write that changes a record every list shows. The lists refetch in the
// background; an item that is not mounted is not touched.
function equipmentListsChanged() {
  invalidateQueries([EQUIPMENT])
}

// An item's own fields (profile, status) decide which maintenance jobs it has.
function equipmentChanged() {
  invalidateQueries([EQUIPMENT])
  invalidateQueries([MAINTENANCE_RULES])
  // An item owns a SignalK source, so Discovery's "assigned to" column and the
  // roles' vessel checks read from it.
  invalidateQueries([DISCOVERY])
  // Engines and the house bank are items, so the setup steps follow them.
  invalidateQueries([SETUP_STATUS])
}

// An item is gone. The equipment lists re-read (the promise settles when they
// have, for a caller that closes a dialog on fresh rows), the other items'
// maintenance rules re-read in the background, and the deleted item's own
// rules are left alone: a read of them would only answer 404.
function equipmentDeleted(id: string): Promise<void> {
  void invalidateQueries([DISCOVERY])
  void invalidateQueries([SETUP_STATUS])
  // Deleting an item clears the roles that named it in settings.
  void invalidateQueries(['/api/settings'])
  void invalidateQueries([MAINTENANCE_RULES], (key) => {
    const filter = key[1] as { equipment?: string } | undefined
    return filter?.equipment === id
  })
  return invalidateQueries([EQUIPMENT])
}

/**
 * The Equipment index's filtered listing - `?system=&status=&zone=&bin=&q=`.
 * `filter` is caller-owned (EquipmentIndex's own toolbar state). `null`
 * fetches nothing at all (items stay `[]`, loading false) for a caller like
 * StocktakeSection that only has something to filter BY once the operator
 * has scanned a bin.
 */
export function useEquipment(filter: EquipmentFilter | null) {
  const params = new URLSearchParams()
  if (filter?.system) params.set('system', filter.system)
  if (filter?.status) params.set('status', filter.status)
  if (filter?.zone) params.set('zone', filter.zone)
  if (filter?.bin) params.set('bin', filter.bin)
  if (filter?.q) params.set('q', filter.q)
  const qs = params.toString()

  // The filter is a fresh object on most renders; the store keys on its
  // contents, so an unchanged filter does not refetch.
  const { data, error, isLoading, refetch } = useApiQuery<EquipmentItem[]>({
    queryKey: [EQUIPMENT, { system: filter?.system ?? '', status: filter?.status ?? '', zone: filter?.zone ?? '', bin: filter?.bin ?? '', q: filter?.q ?? '' }],
    path: `${EQUIPMENT}${qs ? `?${qs}` : ''}`,
    enabled: filter !== null,
    select: (raw: { items?: EquipmentItem[] }) => raw.items ?? [],
  })

  return { items: data ?? NO_ITEMS, loading: isLoading, error: error?.message ?? null, refresh: refetch }
}

/**
 * One equipment record plus its linked documents - `id === null` (a brand
 * new, not-yet-saved draft) fetches nothing, the same shape useDocument(null)
 * gives an unresolved Details route.
 */
export function useEquipmentItem(id: string | null) {
  const path = itemPath(id ?? '')
  const { data, error, isLoading, isFetching, refetch } = useApiQuery<EquipmentItemResponse>({
    queryKey: [path],
    path,
    enabled: id !== null,
    // setItem below asks whether a read is in flight, so the hook re-renders
    // when one leaves and lands, keeping fetchingRef current.
    trackFetching: true,
  })

  // A write that lands while a read of the same record is in flight would be
  // overwritten by that read's older answer. setQueryData now drops that read,
  // but the read was also the only carrier of the linked documents (a write
  // response has the item alone), so they would never arrive. Invalidating
  // refetches, and only when there is a read to replace.
  const fetchingRef = useRef(isFetching)
  fetchingRef.current = isFetching

  // The record the server just handed back from a write. The cache is keyed
  // by id, so a response for a record that is no longer open lands under its
  // own key and cannot show A's fields under B.
  const setItem = useCallback((next: EquipmentItem) => {
    const prev = getQueryData<EquipmentItemResponse>([itemPath(next.id)])
    // The item is a whole record, so it is stored as a plain value; only the
    // linked documents are unknown when nothing was cached (a write response
    // carries the item alone).
    setQueryData<EquipmentItemResponse>([itemPath(next.id)], { documents: prev?.documents, item: next })
    // The documents come from the read: ask again when there is none cached,
    // or when a read of the same record is in flight that this write passed.
    if (next.id === id && (prev === undefined || fetchingRef.current)) invalidateQueries([itemPath(next.id)])
  }, [id])

  // Throws on a rejected PUT (a validation failure, a bin/zone that
  // disagree): the caller (EquipmentEditor's Save) shows it and keeps the
  // draft intact.
  const update = useCallback(async (input: EquipmentInput) => {
    if (id === null) throw new Error('useEquipmentItem: no id to update')
    const data = await apiRequest<{ item: EquipmentItem }>(itemPath(id), { method: 'PUT', json: input })
    setItem(data.item)
    equipmentChanged()
    return data.item
  }, [id, setItem])

  // deletePhotos is the operator's own explicit choice, from the delete
  // confirm's "Also delete N photo(s) only this item uses" checkbox. An
  // ordinary delete only unlinks, see equipmentItem's exclusive_photo_ids.
  const remove = useCallback(async (deletePhotos = false) => {
    if (id === null) throw new Error('useEquipmentItem: no id to delete')
    const qs = deletePhotos ? '?delete_photos=true' : ''
    await apiRequest<void>(`${itemPath(id)}${qs}`, { method: 'DELETE' })
    void equipmentDeleted(id)
  }, [id])

  // Applies a DIFF to the link set (PATCH /documents: {add, remove}), so a
  // link this call does not name (a just-uploaded photo) is never unlinked.
  // Re-reads afterward because the join back to each document's title and
  // kind is only the server's to make.
  const patchLinkedDocuments = useCallback(async (add: string[], remove: string[]) => {
    if (id === null) throw new Error('useEquipmentItem: no id to patch documents on')
    await apiRequest<void>(`${itemPath(id)}/documents`, { method: 'PATCH', json: { add, remove } })
    await refetch()
    equipmentListsChanged()
  }, [id, refetch])

  // Re-reads one record after a photo write, whose response carries the item
  // but not the documents the Documents tab shows. Throws the server's own
  // message on failure: the photo write already succeeded, but a list that
  // silently stays stale shows a removed photo as still linked, so the
  // caller reports it.
  const refreshDocuments = useCallback(async (targetId: string) => {
    const fresh = await apiRequest<EquipmentItemResponse>(itemPath(targetId))
    setQueryData<EquipmentItemResponse>([itemPath(targetId)], fresh)
  }, [])

  return {
    item: data?.item ?? null,
    documents: data?.documents ?? NO_DOCUMENTS,
    loading: isLoading,
    error: error?.message ?? null,
    refresh: refetch,
    update,
    remove,
    patchLinkedDocuments,
    setItem,
    refreshDocuments,
  }
}

/** Creates a brand new equipment record - standalone because the "New item"
 * draft has no id yet to parametrise a hook with. EquipmentEditor calls this
 * directly, then hands the returned item's id to App.tsx so the URL becomes
 * `/inventory/equipment/<id>` and a fresh useEquipmentItem(id) takes over. */
export async function createEquipment(input: EquipmentInput): Promise<EquipmentItem> {
  const data = await apiRequest<{ item: EquipmentItem }>(EQUIPMENT, { method: 'POST', json: input })
  equipmentChanged()
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

/** GET /api/inventory/equipment/:id - a one-off read (not useEquipmentItem's
 * cached hook) for a caller that reads an ARBITRARY id it holds no state
 * for - stocktake-section.tsx scanning a different item's tag on every pass. */
export async function fetchEquipment(id: string): Promise<EquipmentItem> {
  try {
    const data = await apiRequest<{ item: EquipmentItem }>(itemPath(id))
    return data.item
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) throw new EquipmentNotFoundError(err.message)
    throw err
  }
}

/** GET /api/inventory/equipment/:id/dependants - the tiles, roles, alarm rules
 * and features that read this item, asked for just before the operator takes
 * its source away or deletes it. Never cached: the answer is only useful live. */
export function fetchEquipmentDependants(id: string): Promise<EquipmentDependants> {
  return apiRequest<EquipmentDependants>(`${itemPath(id)}/dependants`)
}

/** Strips an equipmentItem down to its own editable EquipmentInput shape -
 * draftFromItem's own reasoning (equipment-editor.tsx), pulled out here so
 * a caller with no editor draft of its own (stocktake-section.tsx's
 * verified_aboard/bin_id writes) can still send a PUT's required
 * whole-record body built from a record it only just fetched. */
export function toEquipmentInput(item: EquipmentItem): EquipmentInput {
  return {
    name: item.name,
    system: item.system,
    manufacturer: item.manufacturer,
    model: item.model,
    serial: item.serial,
    quantity: item.quantity,
    part_number: item.part_number,
    required_quantity: item.required_quantity,
    status: item.status,
    zone_id: item.zone_id,
    bin_id: item.bin_id,
    location_detail: item.location_detail,
    install_date: item.install_date,
    signalk_source: item.signalk_source,
    capacity_ah: item.capacity_ah,
    cells: item.cells,
    warn_voltage: item.warn_voltage,
    high_voltage: item.high_voltage,
    tank_warn_percent: item.tank_warn_percent,
    tank_alarm_percent: item.tank_alarm_percent,
    tank_alarms_off: item.tank_alarms_off,
    profile_id: item.profile_id,
    aliases: item.aliases,
    verified_aboard: item.verified_aboard,
    notes: item.notes,
  }
}

/** PUT /api/inventory/equipment/:id - standalone, for the same "no
 * persistent hook instance" reason fetchEquipment exists. */
export async function updateEquipment(id: string, input: EquipmentInput): Promise<EquipmentItem> {
  const data = await apiRequest<{ item: EquipmentItem }>(itemPath(id), { method: 'PUT', json: input })
  invalidateQueries([itemPath(id)])
  equipmentChanged()
  return data.item
}

/** DELETE /api/inventory/equipment/:id - standalone, for a caller with no
 * useEquipmentItem instance open on this id, e.g. the Equipment index's own
 * row action menu. Same route and deletePhotos semantics as
 * useEquipmentItem's own `remove`. */
export async function deleteEquipment(id: string, deletePhotos = false): Promise<void> {
  const qs = deletePhotos ? '?delete_photos=true' : ''
  await apiRequest<void>(`${itemPath(id)}${qs}`, { method: 'DELETE' })
  // Settles once the lists have re-read, so a caller can close its dialog on
  // fresh rows without asking for the list again.
  await equipmentDeleted(id)
}

// ── equipment photos (ADR 0127) ─────────────────────────────────────────
// Three plain functions, not a hook: the photo row works identically for a
// saved item (each action calls its route immediately) and, before Save, for
// a brand new draft held only as local Blobs, neither of which has a hook
// instance to hang these off. A photo changes the item's photo_ids, which the
// lists show; the open item takes the returned record through setItem.

/** POST /api/inventory/equipment/:id/photos - one multipart upload, linked
 * at the end of the item's photo order server-side. No tag is written - the
 * item's photo_ids view is MIME-based. Throws the server's own message on a
 * rejected upload (AGENTS.md fallback policy: HEIC/non-image get a specific
 * reason, never a generic one). A byte-identical upload is not refused with
 * a 409 - it links the existing document and returns 200. */
export async function uploadEquipmentPhoto(equipmentId: string, file: Blob, filename: string): Promise<EquipmentItem> {
  const form = new FormData()
  form.append('file', file, filename)
  const data = await apiRequest<{ item: EquipmentItem }>(`${itemPath(equipmentId)}/photos`, { method: 'POST', body: form })
  equipmentListsChanged()
  return data.item
}

/** PUT /api/inventory/equipment/:id/photos - {document_ids} must name
 * EXACTLY the item's current photo set; "Make cover" is this same call
 * with the chosen id moved to the front. */
export async function setEquipmentPhotoOrder(equipmentId: string, documentIds: string[]): Promise<EquipmentItem> {
  const data = await apiRequest<{ item: EquipmentItem }>(`${itemPath(equipmentId)}/photos`, {
    method: 'PUT',
    json: { document_ids: documentIds },
  })
  equipmentListsChanged()
  return data.item
}

/** DELETE /api/inventory/equipment/:id/photos/:documentId - removes the
 * photo from the item. UNLINK ONLY by default - deletePhotos is the
 * operator's own explicit choice, from the strip's "Remove and delete"
 * option (offered only when the photo is exclusive to this item - see
 * EquipmentItem's own exclusive_photo_ids doc comment), and additionally
 * deletes the document (and, server-side, its file) once nothing else
 * links it. */
export async function deleteEquipmentPhoto(equipmentId: string, documentId: string, deletePhotos = false): Promise<EquipmentItem> {
  const qs = deletePhotos ? '?delete=true' : ''
  const data = await apiRequest<{ item: EquipmentItem }>(
    `${itemPath(equipmentId)}/photos/${encodeURIComponent(documentId)}${qs}`,
    { method: 'DELETE' },
  )
  equipmentListsChanged()
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

// Marks the zone list stale and resolves once the fresh one has landed, so a
// caller that reads `zones` right after a write sees it. The invalidate comes
// first: it supersedes a read already in flight, which would otherwise be the
// one fetchQuery hands back.
async function reloadZones() {
  invalidateQueries([ZONES])
  await fetchQuery([ZONES], ZONES)
}

async function reloadDecks() {
  invalidateQueries([DECKS])
  await fetchQuery([DECKS], DECKS)
}

/** Zones (with their nested bins) plus every zone/bin write - shared by
 * EquipmentEditor's zone/bin selects and LocationsSection's own CRUD. */
export function useInventoryZones() {
  const { data, error, isLoading, refetch } = useApiQuery<InventoryZone[]>({
    queryKey: [ZONES],
    path: ZONES,
    select: (raw: { zones?: InventoryZone[] }) => raw.zones ?? [],
  })

  // Every write re-reads the whole zone list rather than merging its own
  // response: sort_index and a bin's zone membership are server-decided. The
  // 409 "in use" body (deleteZone/deleteBin) names what is still filed in it,
  // and ApiError carries that message verbatim. Create returns the record
  // because the bin page's "Create bin" flow needs the new zone's id.
  const createZone = useCallback(async (name: string) => {
    const data = await apiRequest<{ zone: InventoryZone }>(ZONES, { method: 'POST', json: { name } })
    await reloadZones()
    return data.zone
  }, [])

  // A zone's or bin's name is joined into every equipment row (zone_name,
  // bin_code), so a rename also refreshes the lists.
  const renameZone = useCallback(async (id: string, name: string) => {
    await apiRequest<unknown>(`${ZONES}/${encodeURIComponent(id)}`, { method: 'PUT', json: { name } })
    await reloadZones()
    equipmentListsChanged()
  }, [])

  const deleteZone = useCallback(async (id: string) => {
    await apiRequest<void>(`${ZONES}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await reloadZones()
  }, [])

  const createBin = useCallback(async (zoneId: string, code: string, name: string) => {
    const data = await apiRequest<{ bin: InventoryBin }>('/api/inventory/bins', { method: 'POST', json: { zone_id: zoneId, code, name } })
    await reloadZones()
    return data.bin
  }, [])

  const renameBin = useCallback(async (id: string, patch: { code?: string; name?: string }) => {
    await apiRequest<unknown>(`/api/inventory/bins/${encodeURIComponent(id)}`, { method: 'PUT', json: patch })
    await reloadZones()
    equipmentListsChanged()
  }, [])

  const deleteBin = useCallback(async (id: string) => {
    await apiRequest<void>(`/api/inventory/bins/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await reloadZones()
  }, [])

  return { zones: data ?? NO_ZONES, loading: isLoading, error: error?.message ?? null, refresh: refetch, createZone, renameZone, deleteZone, createBin, renameBin, deleteBin }
}

/**
 * Decks and their plan images (ADR 0156). Same shape as useInventoryZones.
 * saveLayout answers with the refreshed zone list, which it puts straight
 * into the zones cache, and deleteDeck clears outlines, so both keep every
 * zones reader current without a call from the component.
 */
export function useInventoryDecks() {
  const { data, error, isLoading, refetch } = useApiQuery<InventoryDeck[]>({
    queryKey: [DECKS],
    path: DECKS,
    select: (raw: { decks?: InventoryDeck[] }) => raw.decks ?? [],
  })

  const createDeck = useCallback(async (name: string) => {
    const data = await apiRequest<{ deck: InventoryDeck }>(DECKS, { method: 'POST', json: { name } })
    await reloadDecks()
    return data.deck
  }, [])

  const renameDeck = useCallback(async (id: string, name: string) => {
    await apiRequest<unknown>(`${DECKS}/${encodeURIComponent(id)}`, { method: 'PUT', json: { name } })
    await reloadDecks()
  }, [])

  /** Resolves with how many locations lost their outline. */
  const deleteDeck = useCallback(async (id: string) => {
    const data = await apiRequest<{ zones_cleared: number }>(`${DECKS}/${encodeURIComponent(id)}`, { method: 'DELETE' })
    await reloadDecks()
    invalidateQueries([ZONES])
    return data.zones_cleared
  }, [])

  const uploadPlan = useCallback(async (id: string, file: Blob, filename: string) => {
    const form = new FormData()
    form.append('file', file, filename)
    const data = await apiRequest<{ deck: InventoryDeck }>(`${DECKS}/${encodeURIComponent(id)}/plan`, { method: 'POST', body: form })
    await reloadDecks()
    return data.deck
  }, [])

  const saveLayout = useCallback(async (id: string, layout: DeckLayout) => {
    const data = await apiRequest<{ zones: InventoryZone[] }>(`${DECKS}/${encodeURIComponent(id)}/layout`, { method: 'PUT', json: layout })
    setQueryData<{ zones: InventoryZone[] }>([ZONES], { zones: data.zones })
    return data.zones
  }, [])

  return { decks: data ?? NO_DECKS, loading: isLoading, error: error?.message ?? null, refresh: refetch, createDeck, renameDeck, deleteDeck, uploadPlan, saveLayout }
}
