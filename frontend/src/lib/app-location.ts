import { SETTINGS_SECTIONS, type SettingsSectionId } from '@/components/settings/settings-nav'

// ADR 0074: path URLs, hand-rolled (no router library). This module is the
// only place that knows how a URL string maps to app state — App.tsx's sync
// effect and popstate handler both go through parseAppLocation and
// formatAppLocation rather than building or reading path strings themselves.
// Pure and React-free so it can be unit tested without mounting anything,
// and so PANEL_IDS can be validated here without importing App.tsx (which
// would create a cycle: App needs the parser, the parser must not need App).
export const PANEL_IDS = ['forecast', 'routes', 'radar', 'anchor-watch', 'alarms', 'assistant', 'settings', 'display', 'documents', 'wall-displays'] as const
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
    return {
      panel: 'documents',
      documentFolderId,
      documentId: params.get('document'),
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

  if (PANEL_ID_SET.has(first)) {
    return { panel: first as PanelId }
  }

  return { panel: null, pageId: null }
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
    const qs = params.toString()
    return qs ? `/documents?${qs}` : '/documents'
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
