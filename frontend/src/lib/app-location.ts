import { SETTINGS_SECTIONS, type SettingsSectionId } from '@/components/settings/settings-nav'
import type { InventorySectionId } from '@/components/inventory/inventory-nav'
import { parseDisplayOptions, type DisplayOptions } from '@/lib/displays'

// ADR 0074: path URLs, hand-rolled (no router library). This module is the
// only place that knows how a URL string maps to an app location. The
// router (app-router.ts) goes through parseAppLocation and formatAppLocation
// rather than building or reading path strings itself.
// Pure and React-free so it can be unit tested without mounting anything,
// and so PANEL_IDS can be validated here without importing App.tsx (which
// would create a cycle: App needs the parser, the parser must not need App).
//
// ADR 0171: an AppLocation is a discriminated union. Each panel with views of
// its own carries a `route`, a single union of that panel's views, so going
// to a view replaces the whole slice and no sibling field is ever left
// behind to clear.
export const PANEL_IDS = ['forecast', 'radar', 'anchor-watch', 'alarms', 'assistant', 'settings', 'display', 'documents', 'inventory', 'wall-displays', 'passages', 'watches'] as const
export type PanelId = (typeof PANEL_IDS)[number]

/** The panels with a single view and nothing to remember beyond being open. */
export type PlainPanelId = 'forecast' | 'radar' | 'anchor-watch' | 'alarms'

/** `/settings/<section>`, or the Import wizard at `/settings/import/<runId>`
 * ("new" is the upload page). The bare `/settings/import` list is the
 * `section` view. */
export type SettingsRoute =
  | { view: 'section'; section: SettingsSectionId }
  | { view: 'import-run'; runId: string }

/**
 * The Documents panel. `browse` is the listing: a folder (null for the root),
 * the section open in a Manual folder's reading pane (`?section=`, only
 * meaningful alongside a folder) and, on a deep link from a Mate attachment
 * chip, a document to open in the viewer (`?document=`). The router drops
 * `documentId` once the location is adopted: the viewer is opened once, from
 * the location the app was loaded with, and is not part of where the app is.
 *
 * `details` is the full-panel metadata page at `/documents/<id>`. It carries
 * the folder it was opened from so Back lands the listing there. `sectionId`
 * rides along for the same reason but has no URL of its own, so a reload of
 * the details page loses it.
 */
export type DocumentsRoute =
  | { view: 'browse'; folderId: string | null; sectionId: string | null; documentId: string | null }
  | { view: 'details'; id: string; folderId: string | null; sectionId: string | null }

/**
 * `/wall-displays` is the management index, `/wall-displays/<slug>` the
 * editor for one display. `/display/<slug>` (the wall itself) is a different
 * panel on a deliberately dissimilar path: a one-character typo between the
 * two would put a 1920x360 strip on the management UI.
 */
export type WallDisplaysRoute = { view: 'index' } | { view: 'editor'; slug: string }

/**
 * The Inventory panel. `index` is a section's own page (`/inventory`,
 * `/inventory/profiles`, ...), Locations' table included. The rest are pages
 * under a section: `equipment` and `equipment-new` under Equipment (a New
 * item draft has no URL of its own and formats to the Equipment index), and
 * `plan`, `bin`, `location`, `decks` and `deck` under Locations.
 */
export type InventoryRoute =
  | { view: 'index'; section: InventorySectionId }
  | { view: 'equipment'; id: string }
  | { view: 'equipment-new'; preset: { zoneId?: string; binId?: string } | null }
  | { view: 'plan'; deckId: string | null }
  | { view: 'bin'; code: string }
  | { view: 'location'; id: string }
  | { view: 'decks' }
  | { view: 'deck'; id: string }

/**
 * Which passages the Passages index lists: all, those in no Cruise, or one
 * Cruise's. Passage and combined views carry the filter they were opened from
 * so Back lands the index there.
 */
export type PassagesFilter = { kind: 'all' } | { kind: 'unfiled' } | { kind: 'cruise'; id: string }

/**
 * `/passages` is the index. `/passages/<id>` is one passage, `/passages/cruises/<id>`
 * one Cruise, and `/passages/combined?ids=a,b` an ad-hoc selection viewed together.
 */
export type PassagesRoute =
  | { view: 'index'; filter: PassagesFilter }
  | { view: 'passage'; id: string; filter: PassagesFilter }
  | { view: 'cruise'; id: string }
  | { view: 'combined'; ids: string[]; filter: PassagesFilter }

/** The starting points the Watches page offers on an empty list. */
export type WatchTemplateId = 'morning' | 'overnight' | 'anchor'

/**
 * `/watches` is the index and `/watches/<id>` one watch with its run history.
 * `new` is the draft of a watch not saved yet, optionally started from a
 * template. A draft has no id and so no URL of its own: it formats to the
 * index, the same as a New equipment item.
 */
export type WatchesRoute =
  | { view: 'index' }
  | { view: 'details'; id: string }
  | { view: 'new'; template: WatchTemplateId | null }

export type AppLocation =
  | { panel: null; pageId: string | null }
  | { panel: PlainPanelId }
  | { panel: 'assistant'; conversationId: string | null }
  | { panel: 'settings'; route: SettingsRoute }
  | { panel: 'documents'; route: DocumentsRoute }
  | { panel: 'inventory'; route: InventoryRoute }
  | { panel: 'wall-displays'; route: WallDisplaysRoute }
  | { panel: 'passages'; route: PassagesRoute }
  | { panel: 'watches'; route: WatchesRoute }
  /** ADR 0110: the wall display at `/display/<slug>`. `slug` is null on a bare
   * `/display`. Never validated against the configured displays here: an
   * unknown or absent slug is a distinct, explicit shape the wall shell
   * renders a diagnostic for, with no fallback to "the first display" (which
   * would put one display's geometry on another screen). Its query string
   * (`options`) is parsed once and the location is never written to history. */
  | { panel: 'display'; slug: string | null; options: DisplayOptions }

/** The Inventory section a route sits under, for the nav and the breadcrumb. */
export function inventorySectionOf(route: InventoryRoute): InventorySectionId {
  switch (route.view) {
    case 'index': return route.section
    case 'equipment':
    case 'equipment-new': return 'equipment'
    default: return 'locations'
  }
}

export function settingsSectionOf(route: SettingsRoute): SettingsSectionId {
  return route.view === 'import-run' ? 'import' : route.section
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
  // Split off the query string before segmenting on '/'. Documents, the
  // Locations plan view and the wall display read it; every other branch
  // never looks at `search` at all.
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
    if (section === 'import') {
      const runSegment = segments[2]
      const runId = runSegment !== undefined ? decodeSegment(runSegment) : null
      if (runId) return { panel: 'settings', route: { view: 'import-run', runId } }
    }
    return { panel: 'settings', route: { view: 'section', section } }
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
  // ADR 0115 §2: a second path segment names the Details page instead -
  // `/documents/<documentId>` - since that page has its own URL (unlike the
  // viewer, which is a dialog over the listing, not a navigation). A segment
  // present but unusable (fails to decode, or decodes to '') falls back to
  // the ordinary browse shape below rather than a half-parsed Details route,
  // the same tolerance decodeSegment's other callers already have for a
  // mistyped or stale link.
  if (first === 'documents') {
    const params = new URLSearchParams(search)
    const folderId = params.get('folder')
    if (second !== undefined) {
      const id = decodeSegment(second)
      if (id !== null && id !== '') {
        return { panel: 'documents', route: { view: 'details', id, folderId, sectionId: null } }
      }
    }
    return {
      panel: 'documents',
      route: { view: 'browse', folderId, sectionId: params.get('section'), documentId: params.get('document') },
    }
  }

  if (first === 'passages') {
    const params = new URLSearchParams(search)
    const cruiseParam = params.get('cruise')
    const filter: PassagesFilter = cruiseParam
      ? { kind: 'cruise', id: cruiseParam }
      : params.get('filter') === 'unfiled' ? { kind: 'unfiled' } : { kind: 'all' }
    const indexRoute: AppLocation = { panel: 'passages', route: { view: 'index', filter } }
    if (second === undefined) return indexRoute
    if (second === 'cruises') {
      const idSegment = segments[2]
      const id = idSegment !== undefined ? decodeSegment(idSegment) : null
      return id ? { panel: 'passages', route: { view: 'cruise', id } } : indexRoute
    }
    if (second === 'combined') {
      const ids = (params.get('ids') ?? '').split(',').filter(Boolean)
      return ids.length > 0 ? { panel: 'passages', route: { view: 'combined', ids, filter } } : indexRoute
    }
    const id = decodeSegment(second)
    return id ? { panel: 'passages', route: { view: 'passage', id, filter } } : indexRoute
  }

  // ADR 0227: the Watches pages. An unusable id falls back to the index.
  if (first === 'watches') {
    const id = second !== undefined ? decodeSegment(second) : null
    return { panel: 'watches', route: id ? { view: 'details', id } : { view: 'index' } }
  }

  // ADR 0110: the wall display route. Kept ahead of the generic PANEL_ID_SET
  // branch because it carries an extra segment none of those panels do.
  if (first === 'display') {
    return {
      panel: 'display',
      slug: second !== undefined ? decodeSegment(second) : null,
      options: parseDisplayOptions(search),
    }
  }

  // ADR 0112: the wall-displays management editor. Same shape as /display
  // above (an optional slug segment), kept as its own branch for the same
  // reason. See WallDisplaysRoute for why this path is spelled so
  // differently from /display.
  if (first === 'wall-displays') {
    const slug = second !== undefined ? decodeSegment(second) : null
    return { panel: 'wall-displays', route: slug ? { view: 'editor', slug } : { view: 'index' } }
  }

  // ADR 0123: the Inventory panel. `/inventory` (bare) is the canonical
  // Equipment index - the same collapse-to-default `/settings` gives
  // General.
  if (first === 'inventory') {
    // ADR 0127: the bin page. A missing or empty/undecodable code collapses
    // to the ordinary Locations index: nothing in this app deliberately links
    // to a codeless bin route, and a plain `/inventory/locations` is the
    // correct place to land on one anyway (the router then replaces the bar
    // with it).
    if (second === 'bins') {
      const codeSegment = segments[2]
      const code = codeSegment !== undefined ? decodeSegment(codeSegment) : null
      if (code) return { panel: 'inventory', route: { view: 'bin', code } }
      return { panel: 'inventory', route: { view: 'index', section: 'locations' } }
    }
    // ADR 0156: decks sit under Locations in the nav but have a URL of
    // their own, like the bin page.
    if (second === 'decks') {
      const idSegment = segments[2]
      const id = idSegment !== undefined ? decodeSegment(idSegment) : null
      if (id) return { panel: 'inventory', route: { view: 'deck', id } }
      return { panel: 'inventory', route: { view: 'decks' } }
    }
    if (second === 'locations') {
      const idSegment = segments[2]
      const id = idSegment !== undefined ? decodeSegment(idSegment) : null
      if (id) return { panel: 'inventory', route: { view: 'location', id } }
      const params = new URLSearchParams(search)
      if (params.get('view') === 'plan') {
        return { panel: 'inventory', route: { view: 'plan', deckId: params.get('deck') || null } }
      }
      return { panel: 'inventory', route: { view: 'index', section: 'locations' } }
    }
    if (second === 'maintenance' || second === 'profiles' || second === 'stocktake') {
      return { panel: 'inventory', route: { view: 'index', section: second } }
    }
    if (second === 'equipment') {
      const idSegment = segments[2]
      const id = idSegment !== undefined ? decodeSegment(idSegment) : null
      if (id) return { panel: 'inventory', route: { view: 'equipment', id } }
    }
    return { panel: 'inventory', route: { view: 'index', section: 'equipment' } }
  }

  if (PANEL_ID_SET.has(first)) {
    return { panel: first as PlainPanelId }
  }

  return { panel: null, pageId: null }
}

// ADR 0127: turns scanned text (a keyboard-wedge or pasted URL, or a bare
// bin code) into an AppLocation naming the bin or item it scanned, or null
// when nothing recognisable comes out of it. Stocktake's own scan handler
// (stocktake-section.tsx's handleScan) is the only caller; kept here, next
// to parseAppLocation, so the string-matching stays in the one module that
// already owns "how a path string maps to an app location".
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

  // Only an explicit http(s):// scheme counts as absolute. `new URL(...)`
  // alone used to accept ANY "scheme:rest" shape - including a bare bin
  // code that happens to contain a colon (e.g. "LAZ:02"), which `new URL`
  // parses as a valid (if useless) custom-scheme URL rather than throwing,
  // so it never reached the bare-code branch below at all.
  let pathname: string
  if (/^https?:\/\//i.test(trimmed)) {
    try {
      pathname = new URL(trimmed).pathname
    } catch {
      return null
    }
  } else {
    // A scheme-less scan is never run through `new URL` (URL has no base to
    // resolve a relative string against), so its own ?query/#hash has to be
    // stripped by hand before it's read as a path or a bare code - left in,
    // either one corrupts a bin code or an inventory path beyond recognition.
    const bare = trimmed.split(/[?#]/)[0]
    const inventoryIndex = bare.indexOf('/inventory/')
    if (inventoryIndex !== -1) {
      pathname = bare.slice(inventoryIndex)
    } else if (bare !== '' && !bare.includes('/')) {
      // A bare bin code is never percent-decoded on the way in (it's typed
      // or scanned as plain text) - encoding it here, the same way every
      // other segment this module builds is encoded, is what keeps a code
      // containing '%' or '#'-adjacent characters intact through
      // parseAppLocation's own decodeSegment on the way back out.
      pathname = `/inventory/bins/${encodeURIComponent(bare)}`
    } else {
      return null
    }
  }

  const parsed = parseAppLocation(pathname)
  if (parsed.panel === 'inventory' && (parsed.route.view === 'bin' || parsed.route.view === 'equipment')) {
    return parsed
  }
  return null
}

// The inverse of parseAppLocation, and the only place that builds a path
// string. Every AppLocation has exactly one canonical string here (the
// one-to-one mapping isCanonicalAppPath and the router's URL writes both
// rely on) — the first page collapses to `/`, General collapses to
// `/settings`.
export function formatAppLocation(loc: AppLocation, ctx: Pick<LocationContext, 'firstPageId'>): string {
  switch (loc.panel) {
    case null: {
      if (!loc.pageId || loc.pageId === ctx.firstPageId) return '/'
      return `/dashboard/${encodeURIComponent(loc.pageId)}`
    }

    case 'settings': {
      const { route } = loc
      if (route.view === 'import-run') return `/settings/import/${encodeURIComponent(route.runId)}`
      if (route.section === 'assistant') return '/settings/mate'
      return route.section === 'general' ? '/settings' : `/settings/${route.section}`
    }

    case 'assistant': {
      if (!loc.conversationId) return '/mate'
      return `/mate/${encodeURIComponent(loc.conversationId)}`
    }

    case 'documents': {
      const { route } = loc
      const params = new URLSearchParams()
      if (route.view === 'details') {
        // The Details page's folder is carried as a query param on the SAME
        // /documents/<id> path segment shape the ordinary browse form uses -
        // deliberately, so Back out of the page lands the listing on the
        // folder the operator actually came from rather than the root. The
        // viewer deep link is never serialized alongside a Details page:
        // the two are mutually exclusive destinations.
        if (route.folderId) params.set('folder', route.folderId)
        const qs = params.toString()
        const base = `/documents/${encodeURIComponent(route.id)}`
        return qs ? `${base}?${qs}` : base
      }
      if (route.folderId) params.set('folder', route.folderId)
      if (route.documentId) params.set('document', route.documentId)
      // A section is meaningless without its folder (a manual IS a folder),
      // so it is never emitted on its own.
      if (route.folderId && route.sectionId) params.set('section', route.sectionId)
      const qs = params.toString()
      return qs ? `/documents?${qs}` : '/documents'
    }

    case 'inventory': {
      const { route } = loc
      switch (route.view) {
        case 'index': return route.section === 'equipment' ? '/inventory' : `/inventory/${route.section}`
        case 'equipment': return `/inventory/equipment/${encodeURIComponent(route.id)}`
        // A New item draft has no id and so no URL of its own.
        case 'equipment-new': return '/inventory'
        case 'plan': {
          const params = new URLSearchParams({ view: 'plan' })
          if (route.deckId) params.set('deck', route.deckId)
          return `/inventory/locations?${params.toString()}`
        }
        case 'bin': return `/inventory/bins/${encodeURIComponent(route.code)}`
        case 'location': return `/inventory/locations/${encodeURIComponent(route.id)}`
        case 'decks': return '/inventory/decks'
        case 'deck': return `/inventory/decks/${encodeURIComponent(route.id)}`
      }
      break
    }

    case 'display': {
      // The wall's query string is its own and is never written back.
      if (!loc.slug) return '/display'
      return `/display/${encodeURIComponent(loc.slug)}`
    }

    case 'passages': {
      const { route } = loc
      const params = new URLSearchParams()
      if (route.view !== 'cruise') {
        if (route.filter.kind === 'unfiled') params.set('filter', 'unfiled')
        if (route.filter.kind === 'cruise') params.set('cruise', route.filter.id)
      }
      if (route.view === 'combined') params.set('ids', route.ids.join(','))
      const qs = params.toString()
      let base = '/passages'
      if (route.view === 'passage') base = `/passages/${encodeURIComponent(route.id)}`
      if (route.view === 'cruise') base = `/passages/cruises/${encodeURIComponent(route.id)}`
      if (route.view === 'combined') base = '/passages/combined'
      return qs ? `${base}?${qs}` : base
    }

    case 'watches': {
      if (loc.route.view === 'details') return `/watches/${encodeURIComponent(loc.route.id)}`
      return '/watches'
    }

    case 'wall-displays': {
      if (loc.route.view === 'index') return '/wall-displays'
      return `/wall-displays/${encodeURIComponent(loc.route.slug)}`
    }
  }

  return `/${loc.panel}`
}

// A path is canonical when re-formatting what it parses to reproduces it
// exactly, AND the state it names is actually reachable right now (a settings
// path when signed in without admin, or a page id that's since been deleted,
// parse fine but aren't states this app will land on). Both checks matter:
// the router uses this to decide replace vs. push when it writes the bar, and
// to decide whether a popstate needs normalising before it is applied.
export function isCanonicalAppPath(path: string, ctx: LocationContext): boolean {
  const loc = parseAppLocation(path)
  if (formatAppLocation(loc, ctx) !== path) return false
  if (loc.panel === 'settings' && !ctx.canAdmin) return false
  if (loc.panel === null && loc.pageId != null && ctx.knownPageIds !== null && !ctx.knownPageIds.includes(loc.pageId)) {
    return false
  }
  return true
}
