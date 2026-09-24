import { SETTINGS_SECTIONS, type SettingsSectionId } from '@/components/settings/settings-nav'
import type { InventorySectionId } from '@/components/inventory/inventory-nav'

// ADR 0074: path URLs, hand-rolled (no router library). This module is the
// only place that knows how a URL string maps to app state — App.tsx's sync
// effect and popstate handler both go through parseAppLocation and
// formatAppLocation rather than building or reading path strings themselves.
// Pure and React-free so it can be unit tested without mounting anything,
// and so PANEL_IDS can be validated here without importing App.tsx (which
// would create a cycle: App needs the parser, the parser must not need App).
export const PANEL_IDS = ['forecast', 'routes', 'radar', 'anchor-watch', 'alarms', 'assistant', 'settings', 'display', 'documents', 'inventory', 'wall-displays'] as const
export type PanelId = (typeof PANEL_IDS)[number]

export interface AppLocation {
  panel: PanelId | null
  pageId?: string | null
  section?: SettingsSectionId
  conversationId?: string | null
  /** ADR 0106 F1: the Documents panel's current folder, null for the root.
   * Absent (undefined) on every other panel/location. */
  documentFolderId?: string | null
  /** ADR 0106 F1: a document to open in the viewer on mount - how a Mate
   * attachment chip's link opens straight to that document. formatAppLocation
   * serializes it like any other field (round-tripping a deep link exactly);
   * App.tsx's own sync effect is what chooses to read it only once, from
   * initialLocation, rather than feeding it back in on every render the way
   * it does documentFolderId - see that effect's own comment. */
  documentId?: string | null
  /** ADR 0115 §2: the Details page's `/documents/<documentId>`
   * segment - a full-panel page for one document's metadata (title, notes,
   * tags, the read-only indexing facts), reached from the listing rather
   * than the viewer's ?document= dialog above. Absent on the ordinary browse
   * shape (that shape's own three keys - panel/documentFolderId/documentId -
   * are unchanged so existing `toEqual` tests keep passing), present and the
   * others null'd out whenever the Details page is what the route names.
   * documentFolderId still carries the current folder on this shape too
   * (see formatAppLocation's own comment) - without it, Back out of the
   * Details page would land the listing at the root rather than the folder
   * it was opened from. */
  documentEditId?: string | null
  /** ADR 0110: the wall display route's `/display/<slug>` segment. null on
   * a bare `/display` (and absent everywhere else). Never validated against
   * the configured displays here — an unknown or absent slug is a distinct,
   * explicit shape the wall shell renders a diagnostic for; this module has
   * no fallback to "the first display", which would silently put one
   * display's geometry on another screen. */
  displaySlug?: string | null
  /** Revision "one panel, not three" (2026-09-20): which section (a document
   * node in documentFolderId's manual tree, when that folder carries
   * document_folders.role='manual') is open in the reading pane rendered by
   * documents-panel.tsx's ManualFolderView, `/documents?folder=<id>&section=<id>`.
   * Meaningless without documentFolderId also set - formatAppLocation only
   * ever emits it alongside `folder=`, the same "meaningless on its own"
   * rule the deleted /manuals route's own manualSectionId used to follow.
   * A note (unfiled or filed) opens through the existing documentId above
   * instead of a field of its own - it is a document with kind='note', and
   * `/documents?document=<id>` already addresses any document by id. */
  documentSectionId?: string | null
  /** ADR 0112: the wall-displays management editor's `/wall-displays/<slug>`
   * segment (the "editor" name, not "displaySlug", keeps it from being
   * confused with the wall route's field above). null on a bare
   * `/wall-displays`, absent everywhere else.
   *
   * `/display/<slug>` (above) and `/wall-displays/<slug>` are deliberately
   * NOT one character apart from each other. `/display/<slug>` is the wall
   * itself - what gets typed into a kiosk browser and rendered at kiosk
   * scale on a screen as narrow as 1920x360. `/wall-displays/<slug>` is the
   * admin editor for that display's configuration, meant for a normal
   * browser. Resist the urge to "tidy" these into `/displays/<slug>` next to
   * `/display/<slug>` - a one-character typo (or a stale bookmark from
   * before a rename) would then silently swap one for the other, and the
   * failure mode is a 1920x360 strip showing the management UI, or the
   * editor rendering nothing because it hit the wall shell instead. */
  displayEditSlug?: string | null
  /** ADR 0123: the Inventory panel's internal section (the InventoryNav
   * shape, same contract as `section` above) - `/inventory`, `/inventory/profiles`,
   * `/inventory/locations`. Defaults to 'equipment' the same way `section`
   * defaults to 'general'. */
  inventorySection?: InventorySectionId
  /** ADR 0123: the Equipment editor's `/inventory/equipment/<id>` segment -
   * only meaningful alongside inventorySection 'equipment' (or its default),
   * same "meaningless on its own" rule documentSectionId already follows.
   * null on the bare `/inventory` index; absent everywhere else. A brand
   * new, not-yet-saved draft (the "New item" button) has no id and so no
   * URL of its own - see App.tsx's inventoryCreatingEquipment, which is
   * local UI state, never serialized here. */
  equipmentEditId?: string | null
  /** ADR 0127: the Locations section's `/inventory/bins/<code>` segment -
   * the bin page a tag tap or a Locations-section click opens. Only
   * meaningful alongside inventorySection 'locations', the same
   * "meaningless on its own" rule documentSectionId already follows, and
   * present only when it names a real, non-empty code (decoded with
   * decodeSegment). Absent everywhere else, INCLUDING a bare
   * `/inventory/bins` with no third segment - that collapses to the
   * ordinary Locations shape rather than carrying an explicit null, since
   * nothing in this app ever deliberately links to a codeless bin route
   * and a plain `/inventory/locations` is the correct place to land on one
   * anyway (isCanonicalAppPath then just replaceState's it there). */
  binCode?: string
}

export interface LocationContext {
  /** The first page in server order, or null while the page list hasn't loaded. */
  firstPageId: string | null
  /** All known page ids, or null while the page list hasn't loaded (unknown ids are accepted rather than rejected). */
  knownPageIds: readonly string[] | null
  canAdmin: boolean
}

const PANEL_ID_SET: ReadonlySet<string> = new Set(PANEL_IDS)

function isSettingsSectionId(id: string): id is SettingsSectionId {
  return SETTINGS_SECTIONS.some((section) => section.id === id)
}

// decodeURIComponent throws on a malformed escape (`%E0%A4%A`), and a pasted
// or truncated link is exactly where one turns up. Returning null here means
// "nothing named" rather than a crash: every caller treats a null segment
// the same way it treats an unknown id, landing on its own fallback (the
// first page, no thread, no slug) rather than surfacing a parse error - a
// mistyped or stale link should degrade gracefully, not break the app.
function decodeSegment(segment: string): string | null {
  try {
    return decodeURIComponent(segment)
  } catch {
    return null
  }
}

// Never throws: an unrecognised shape always resolves to the closest valid
// ancestor (the dashboard, first page) rather than surfacing a parse error —
// a mistyped or stale link should degrade gracefully, not break the app.
export function parseAppLocation(pathname: string): AppLocation {
  // Split off the query string before segmenting on '/' - only the
  // Documents panel (below) has one; every other branch never looks at
  // `search` at all, same as before this existed.
  const [path, search = ''] = pathname.split('?', 2)
  const segments = path.split('/').filter(Boolean)
  const [first, second] = segments

  if (!first || first === 'dashboard') {
    return { panel: null, pageId: second !== undefined ? decodeSegment(second) : null }
  }

  if (first === 'settings') {
    let section: SettingsSectionId = 'general'
    if (second === 'mate' || second === 'assistant') {
      section = 'assistant'
    } else if (second !== undefined && isSettingsSectionId(second)) {
      section = second
    }
    return { panel: 'settings', section }
  }

  // ADR 0094 named the assistant "Mate" in the UI. Keep /assistant as a
  // tolerated legacy alias, but canonicalize to /mate via formatAppLocation.
  if (first === 'mate' || first === 'assistant') {
    return { panel: 'assistant', conversationId: second !== undefined ? decodeSegment(second) : null }
  }

  // ADR 0106 F1: the current folder and, on a deep link from a Mate
  // attachment chip, a document to open in the viewer - both optional and
  // independent of each other, so query params rather than a path segment.
  //
  // ADR 0115 §2: a second path segment names the Details page
  // instead - `/documents/<documentId>` - since that page has its own URL
  // (unlike the viewer, which is a dialog over the listing, not a
  // navigation). A segment present but unusable (fails to decode, or decodes
  // to '') falls back to the ordinary browse shape below rather than a
  // half-parsed Details route, the same tolerance decodeSegment's other
  // callers already have for a mistyped or stale link.
  if (first === 'documents') {
    const params = new URLSearchParams(search)
    const documentFolderId = params.get('folder')
    if (second !== undefined) {
      const documentEditId = decodeSegment(second)
      if (documentEditId !== null && documentEditId !== '') {
        return { panel: 'documents', documentEditId, documentFolderId, documentId: null }
      }
    }
    // `section` (revision "one panel, not three", 2026-09-20) is only ever
    // meaningful alongside a folder - a manual IS a folder, so its reading
    // pane's open section has nowhere to attach without one. Omitted
    // entirely (not even a null key) when absent, so every existing
    // toEqual fixture for this branch that predates `section` keeps
    // matching unchanged.
    const documentSectionId = params.get('section')
    return {
      panel: 'documents',
      documentFolderId,
      documentId: params.get('document'),
      ...(documentSectionId !== null ? { documentSectionId } : {}),
    }
  }

  // ADR 0110: the wall display route. Kept ahead of the generic PANEL_ID_SET
  // branch because it carries an extra segment none of those panels do.
  if (first === 'display') {
    return { panel: 'display', displaySlug: second !== undefined ? decodeSegment(second) : null }
  }

  // ADR 0112: the wall-displays management editor. Same shape as /display
  // above (an optional slug segment), kept as its own branch for the same
  // reason: it carries an extra segment the generic PANEL_ID_SET branch
  // below doesn't know how to read. See the displayEditSlug doc comment on
  // AppLocation for why this path is spelled so differently from /display.
  if (first === 'wall-displays') {
    return { panel: 'wall-displays', displayEditSlug: second !== undefined ? decodeSegment(second) : null }
  }

  // ADR 0123: the Inventory panel. `/inventory` (bare) is the canonical
  // Equipment index - the same collapse-to-default `section` gives
  // `/settings`. `/inventory/equipment/<id>` carries a third segment none of
  // the other two sub-routes do, so (like /documents/<id> above) it's read
  // explicitly here rather than falling through to the generic
  // PANEL_ID_SET branch below.
  if (first === 'inventory') {
    // ADR 0127: the bin page. Kept ahead of the plain 'locations' branch
    // below because it carries an extra segment that one doesn't - same
    // reasoning as /documents/<id> and /display/<slug> elsewhere in this
    // function. A missing or empty/undecodable code collapses to the
    // ordinary Locations shape (binCode's own doc comment on AppLocation).
    if (second === 'bins') {
      const codeSegment = segments[2]
      const binCode = codeSegment !== undefined ? decodeSegment(codeSegment) : null
      if (binCode) {
        return { panel: 'inventory', inventorySection: 'locations', binCode }
      }
      return { panel: 'inventory', inventorySection: 'locations' }
    }
    if (second === 'profiles' || second === 'locations' || second === 'stocktake') {
      return { panel: 'inventory', inventorySection: second }
    }
    if (second === 'equipment') {
      const segments2 = segments[2]
      const equipmentEditId = segments2 !== undefined ? decodeSegment(segments2) : null
      return { panel: 'inventory', inventorySection: 'equipment', equipmentEditId: equipmentEditId || null }
    }
    return { panel: 'inventory', inventorySection: 'equipment', equipmentEditId: null }
  }

  if (PANEL_ID_SET.has(first)) {
    return { panel: first as PanelId }
  }

  return { panel: null, pageId: null }
}

// ADR 0127: turns scanned text (a keyboard-wedge or pasted URL, or a bare
// bin code) into an AppLocation naming the bin or item it scanned, or null
// when nothing recognisable comes out of it. Stocktake's own scan handler
// (stocktake-section.tsx's handleScan) is the only caller; moved here,
// next to parseAppLocation, so the string-matching stays in the one module
// that already owns "how a path string maps to app state" rather than
// living inline in a component.
//
// `new URL(text)` only succeeds for an ABSOLUTE url (a scheme included),
// which a scan typed into a boat's own tailnet address bar without
// "https://" is not (e.g. "boat.tailnet.ts.net/inventory/bins/LAZ-02").
// Treating that whole string as a bare bin code (the naive approach this
// replaced) resolved to the wrong bin - or, worse, silently to none at all.
// So a scheme-less scan is read three ways, in order: an /inventory/ path
// pulled out of wherever it starts in the string; failing that, a bare bin
// code ONLY if there's no slash in it at all (a real bin code never has
// one); anything else is unrecognised. Whatever pathname results is handed
// to parseAppLocation, but only a bin-page or equipment-editor shape counts
// as "recognised" here - a URL that parses fine but names some other panel
// (or nothing) is just as unrecognised as text that never parsed as a path
// at all.
export function resolveScannedText(text: string): AppLocation | null {
  const trimmed = text.trim()
  if (trimmed === '') return null

  let pathname: string
  try {
    pathname = new URL(trimmed).pathname
  } catch {
    const inventoryIndex = trimmed.indexOf('/inventory/')
    if (inventoryIndex !== -1) {
      pathname = trimmed.slice(inventoryIndex)
    } else if (!trimmed.includes('/')) {
      pathname = `/inventory/bins/${trimmed}`
    } else {
      return null
    }
  }

  const parsed = parseAppLocation(pathname)
  if (parsed.panel === 'inventory' && parsed.inventorySection === 'locations' && parsed.binCode) {
    return parsed
  }
  if (parsed.panel === 'inventory' && parsed.inventorySection === 'equipment' && parsed.equipmentEditId) {
    return parsed
  }
  return null
}

// The inverse of parseAppLocation, and the only place that builds a path
// string. Every AppLocation has exactly one canonical string here (the
// one-to-one mapping isCanonicalAppPath and the URL sync effect both rely
// on) — the first page collapses to `/`, General collapses to `/settings`.
export function formatAppLocation(loc: AppLocation, ctx: Pick<LocationContext, 'firstPageId'>): string {
  if (loc.panel === null) {
    if (!loc.pageId || loc.pageId === ctx.firstPageId) return '/'
    return `/dashboard/${encodeURIComponent(loc.pageId)}`
  }

  if (loc.panel === 'settings') {
    const section = loc.section ?? 'general'
    if (section === 'assistant') return '/settings/mate'
    return section === 'general' ? '/settings' : `/settings/${section}`
  }

  if (loc.panel === 'assistant') {
    if (!loc.conversationId) return '/mate'
    return `/mate/${encodeURIComponent(loc.conversationId)}`
  }

  if (loc.panel === 'documents') {
    // The Details page's folder is carried as a query param on the SAME
    // /documents/<id> path segment shape the ordinary browse form uses -
    // deliberately, so Back out of the page (which restores documentFolderId
    // but clears documentEditId - see App.tsx's applyAppLocation) lands on
    // the folder the operator actually came from rather than the root.
    // documentId (the viewer deep link) is never serialized alongside a
    // Details page: the two are mutually exclusive destinations, and
    // App.tsx's own URL sync effect only ever sets one or the other.
    if (loc.documentEditId) {
      const params = new URLSearchParams()
      if (loc.documentFolderId) params.set('folder', loc.documentFolderId)
      const qs = params.toString()
      return qs ? `/documents/${encodeURIComponent(loc.documentEditId)}?${qs}` : `/documents/${encodeURIComponent(loc.documentEditId)}`
    }
    const params = new URLSearchParams()
    if (loc.documentFolderId) params.set('folder', loc.documentFolderId)
    if (loc.documentId) params.set('document', loc.documentId)
    // section is meaningless without folder - see the doc comment on
    // documentSectionId above - so it is never emitted on its own.
    if (loc.documentFolderId && loc.documentSectionId) params.set('section', loc.documentSectionId)
    const qs = params.toString()
    return qs ? `/documents?${qs}` : '/documents'
  }

  if (loc.panel === 'inventory') {
    const section = loc.inventorySection ?? 'equipment'
    if (section === 'equipment') {
      if (loc.equipmentEditId) return `/inventory/equipment/${encodeURIComponent(loc.equipmentEditId)}`
      return '/inventory'
    }
    // ADR 0127: binCode is only ever set alongside 'locations' - see its
    // own doc comment on AppLocation for why a falsy (absent) value here
    // collapses to the ordinary Locations path rather than a bare
    // /inventory/bins.
    if (section === 'locations' && loc.binCode) {
      return `/inventory/bins/${encodeURIComponent(loc.binCode)}`
    }
    return `/inventory/${section}`
  }

  if (loc.panel === 'display') {
    if (!loc.displaySlug) return '/display'
    return `/display/${encodeURIComponent(loc.displaySlug)}`
  }

  if (loc.panel === 'wall-displays') {
    if (!loc.displayEditSlug) return '/wall-displays'
    return `/wall-displays/${encodeURIComponent(loc.displayEditSlug)}`
  }

  return `/${loc.panel}`
}

// A path is canonical when re-formatting what it parses to reproduces it
// exactly, AND the state it names is actually reachable right now (a settings
// path when signed in without admin, or a page id that's since been deleted,
// parse fine but aren't states this app will land on). Both checks matter:
// the sync effect uses this to decide replaceState vs. pushState, and the
// popstate handler uses it to decide whether to normalise before navigating.
export function isCanonicalAppPath(path: string, ctx: LocationContext): boolean {
  const loc = parseAppLocation(path)
  if (formatAppLocation(loc, ctx) !== path) return false
  if (loc.panel === 'settings' && !ctx.canAdmin) return false
  if (loc.panel === null && loc.pageId != null && ctx.knownPageIds !== null && !ctx.knownPageIds.includes(loc.pageId)) {
    return false
  }
  return true
}

/**
 * What the Equipment editor is showing right now, as App.tsx holds it.
 * `creating` is the "New item" draft, which deliberately has no URL of its
 * own (see InventoryPanelProps' own note), so it cannot be read back out of
 * an AppLocation - which is exactly why this comparison needs it passed in.
 */
export interface InventoryEditorState {
  section: InventorySectionId
  equipmentEditId: string | null
  creating: boolean
}

/**
 * Would navigating to `target` take the Equipment editor off screen?
 *
 * The popstate handler asks this to decide whether a Back press needs the
 * unsaved-changes guard. Comparing `equipmentEditId` alone is not enough, and
 * got this wrong twice over: a "New item" draft has a null id, so Back out of
 * one to another inventory section compared null to null, saw no change, and
 * discarded the draft with no prompt at all. A saved record open in the
 * editor had the same hole whenever the section changed without the id doing
 * so.
 *
 * So the question is asked the other way round - is the editor showing, and
 * does `target` still show that same editor? - which needs no special case
 * for the draft and none for a section switch.
 */
export function inventoryEditorClosedBy(current: InventoryEditorState, target: AppLocation): boolean {
  const showing = current.section === 'equipment'
    && (current.equipmentEditId !== null || current.creating)
  if (!showing) return false
  if (target.panel !== 'inventory') return true
  if ((target.inventorySection ?? 'equipment') !== 'equipment') return true
  // A draft has no URL that can represent it, so any inventory location at
  // all closes it. A saved record survives only a target naming that same id.
  if (current.creating) return true
  return (target.equipmentEditId ?? null) !== current.equipmentEditId
}

/**
 * What Stocktake or the bin page's quick-add draft is showing right now, as
 * App.tsx holds it - inventoryEditorClosedBy's own state shape, for
 * inventoryHasWork rather than inventoryDirty.
 */
export interface InventoryWorkState {
  section: InventorySectionId
  binCode: string | null
}

/**
 * Would navigating to `target` take Stocktake or the bin page (whichever is
 * reporting inventoryHasWork) off screen?
 *
 * Release-fixes code-review finding: the popstate handler only ever asked
 * inventoryEditorClosedBy this question, so a Back press that left Stocktake
 * or a bin page's quick-add draft behind - the other two things
 * inventoryHasWork guards - went straight through with no prompt. Same
 * "is it showing, and does target still show it" shape as
 * inventoryEditorClosedBy, generalized to the two ways inventoryHasWork's own
 * clearing effect (App.tsx) treats as "no longer showing": leaving the
 * 'inventory' panel, leaving Stocktake, or the bin page's own binCode going
 * back to null.
 */
export function inventoryWorkClosedBy(current: InventoryWorkState, target: AppLocation): boolean {
  const showing = current.section === 'stocktake' || (current.section === 'locations' && current.binCode !== null)
  if (!showing) return false
  if (target.panel !== 'inventory') return true
  const targetSection = target.inventorySection ?? 'equipment'
  if (current.section === 'stocktake') return targetSection !== 'stocktake'
  // current.section === 'locations' with a bin open.
  if (targetSection !== 'locations') return true
  return (target.binCode ?? null) !== current.binCode
}
