/**
 * Pure parse/format tests for the deep-link URL scheme (ADR 0074). No React
 * — app-location.ts has none, so these run directly against the module.
 *
 * ADR 0171 changed the shape of an AppLocation (a union per panel, each with
 * a `route`), not a single URL: every path asserted here is the one it was
 * before the change.
 */
import { describe, it, expect } from 'vitest'
import {
  parseAppLocation,
  formatAppLocation,
  isCanonicalAppPath,
  resolveScannedText,
  type AppLocation,
  type InventoryRoute,
  type LocationContext,
} from '@/lib/app-location'
import type { SettingsSectionId } from '@/components/settings/settings-nav'
import type { InventorySectionId } from '@/components/inventory/inventory-nav'

const ctx: Pick<LocationContext, 'firstPageId'> = { firstPageId: 'p1' }

// Short names for the shapes the assertions below keep repeating.
const NO_OPTIONS = { pageId: null, preview: false }
const settings = (section: SettingsSectionId): AppLocation => ({ panel: 'settings', route: { view: 'section', section } })
const importRun = (runId: string): AppLocation => ({ panel: 'settings', route: { view: 'import-run', runId } })
const browse = (folderId: string | null = null, documentId: string | null = null, sectionId: string | null = null): AppLocation => ({
  panel: 'documents', route: { view: 'browse', folderId, sectionId, documentId },
})
const details = (id: string, folderId: string | null = null): AppLocation => ({
  panel: 'documents', route: { view: 'details', id, folderId, sectionId: null },
})
const inventory = (route: InventoryRoute): AppLocation => ({ panel: 'inventory', route })
const indexOf = (section: InventorySectionId): AppLocation => inventory({ view: 'index', section })

describe('parseAppLocation', () => {
  it('parses the root path as the dashboard, first page', () => {
    expect(parseAppLocation('/')).toEqual({ panel: null, pageId: null })
    expect(parseAppLocation('')).toEqual({ panel: null, pageId: null })
    expect(parseAppLocation('/dashboard')).toEqual({ panel: null, pageId: null })
  })

  it('parses /dashboard/<pageId> as that dashboard page', () => {
    expect(parseAppLocation('/dashboard/p2')).toEqual({ panel: null, pageId: 'p2' })
  })

  it('decodes a percent-encoded page id', () => {
    expect(parseAppLocation('/dashboard/a%20b')).toEqual({ panel: null, pageId: 'a b' })
  })

  it('treats a malformed percent-escape as no page rather than throwing', () => {
    expect(parseAppLocation('/dashboard/%E0%A4%A')).toEqual({ panel: null, pageId: null })
  })

  it.each(['forecast', 'routes', 'radar', 'anchor-watch', 'alarms'] as const)(
    'parses /%s as that panel',
    (panel) => {
      expect(parseAppLocation(`/${panel}`)).toEqual({ panel })
    },
  )

  // ADR 0110: the wall display route. No fallback to "the first display" —
  // an absent or unrecognised slug is a distinct, explicit shape
  // (displaySlug: null) that the caller renders a diagnostic for, never a
  // silently-chosen display.
  it('parses /display with no slug', () => {
    expect(parseAppLocation('/display')).toEqual({ panel: 'display', slug: null, options: NO_OPTIONS })
  })

  it('parses /display/<slug> as that display', () => {
    expect(parseAppLocation('/display/flybridge')).toEqual({ panel: 'display', slug: 'flybridge', options: NO_OPTIONS })
  })

  it('decodes a percent-encoded display slug', () => {
    expect(parseAppLocation('/display/saloon%20tv')).toEqual({ panel: 'display', slug: 'saloon tv', options: NO_OPTIONS })
  })

  it('treats a malformed percent-escape in the display slug as no slug rather than throwing', () => {
    expect(parseAppLocation('/display/%E0%A4%A')).toEqual({ panel: 'display', slug: null, options: NO_OPTIONS })
  })

  // The wall-displays management editor: /display/<slug> is the wall itself
  // (typed into a kiosk browser), /wall-displays/<slug> is the admin editor
  // for that display's config. Same parse/format shape as /display, on a
  // deliberately dissimilar path so a typo in either can never land on the
  // other's route.
  it('parses /wall-displays with no slug as the index', () => {
    expect(parseAppLocation('/wall-displays')).toEqual({ panel: 'wall-displays', route: { view: 'index' } })
  })

  it('parses /wall-displays/<slug> as that display\'s editor', () => {
    expect(parseAppLocation('/wall-displays/flybridge')).toEqual({ panel: 'wall-displays', route: { view: 'editor', slug: 'flybridge' } })
  })

  it('decodes a percent-encoded wall-displays edit slug', () => {
    expect(parseAppLocation('/wall-displays/saloon%20tv')).toEqual({ panel: 'wall-displays', route: { view: 'editor', slug: 'saloon tv' } })
  })

  it('treats a malformed percent-escape in the wall-displays edit slug as no slug rather than throwing', () => {
    expect(parseAppLocation('/wall-displays/%E0%A4%A')).toEqual({ panel: 'wall-displays', route: { view: 'index' } })
  })

  // ADR 0171: the wall's own query string rides on the location, parsed
  // once, and is never part of what formatAppLocation writes.
  it('parses the wall display\'s query string into its options', () => {
    expect(parseAppLocation('/display/flybridge?page=p2&preview=1')).toEqual({
      panel: 'display', slug: 'flybridge', options: { pageId: 'p2', preview: true },
    })
  })

  it('does not write the wall display\'s options back into its path', () => {
    const loc = parseAppLocation('/display/flybridge?page=p2&preview=1')
    expect(formatAppLocation(loc, ctx)).toBe('/display/flybridge')
  })

  // Regression guard: /display and /wall-displays are one word apart, not
  // one character apart, specifically so a typo can't silently swap the
  // kiosk wall route for the management editor (or vice versa). Assert they
  // parse to different panels with different slug fields, not just
  // "different strings".
  it('parses /display/x and /wall-displays/x to different panels', () => {
    const wall = parseAppLocation('/display/x')
    const editor = parseAppLocation('/wall-displays/x')
    expect(wall.panel).toBe('display')
    expect(editor.panel).toBe('wall-displays')
    expect(wall).toEqual({ panel: 'display', slug: 'x', options: NO_OPTIONS })
    expect(editor).toEqual({ panel: 'wall-displays', route: { view: 'editor', slug: 'x' } })
  })

  // ADR 0106 F1: the Documents panel carries its current folder (and, on a
  // deep link from a Mate attachment chip, which document to open in the
  // viewer) as query params rather than path segments, since either can be
  // absent independently and neither has a natural path position the way
  // /mate/<conversationId> does.
  it('parses /documents with no query as the Documents panel at the root', () => {
    expect(parseAppLocation('/documents')).toEqual(browse())
  })

  it('parses /documents?folder=<id> as that folder', () => {
    expect(parseAppLocation('/documents?folder=f1')).toEqual(browse('f1'))
  })

  it('parses /documents?document=<id> as a viewer deep link, root folder', () => {
    expect(parseAppLocation('/documents?document=d1')).toEqual(browse(null, 'd1'))
  })

  it('parses /documents?folder=<id>&document=<id> as both together', () => {
    expect(parseAppLocation('/documents?folder=f1&document=d1')).toEqual(browse('f1', 'd1'))
  })

  // ADR 0115 §2: the Details page's own route. Carries the folder
  // on purpose (unlike the viewer's ?document=, which never needs one - the
  // viewer is a dialog over whatever folder is already open) - without it,
  // Back out of the Details page would land the listing at the root instead
  // of the folder it was opened from.
  it('parses /documents/<documentId> as the Details page, root folder', () => {
    expect(parseAppLocation('/documents/doc-1')).toEqual(details('doc-1'))
  })

  it('parses /documents/<documentId>?folder=<id> as the Details page, that folder', () => {
    expect(parseAppLocation('/documents/doc-1?folder=f1')).toEqual(details('doc-1', 'f1'))
  })

  it('decodes a percent-encoded Details page document id', () => {
    expect(parseAppLocation('/documents/a%20b')).toEqual(details('a b'))
  })

  // decodeSegment's own tolerance (see its doc comment): a malformed escape
  // degrades to the ordinary browse shape rather than a half-parsed Details
  // route, the same way every other caller of decodeSegment falls back to
  // its own "nothing named" state instead of surfacing a parse error.
  it('treats a malformed percent-escape in the Details page id as the ordinary browse shape', () => {
    expect(parseAppLocation('/documents/%E0%A4%A')).toEqual(browse())
  })

  // Revision "one panel, not three" (2026-09-20): /notes and /manuals are
  // gone - a note opens through the existing ?document= (it's a document
  // with kind='note'), and a manual is addressed by ?folder=<id> like any
  // other folder, since a manual IS a folder. ?section=<id> is the only new
  // piece: which node in that folder's manual tree is open in the reading
  // pane, meaningless without a folder the same way manualSectionId used to
  // be meaningless without a manual.
  it('parses /documents?folder=<id>&section=<id> as that folder with a section open', () => {
    expect(parseAppLocation('/documents?folder=f1&section=s1')).toEqual(browse('f1', null, 's1'))
  })

  it('parses /mate as the Mate panel', () => {
    expect(parseAppLocation('/mate')).toEqual({ panel: 'assistant', conversationId: null })
  })

  it('parses /mate/<conversationId> as the Mate panel with a thread deeplink', () => {
    expect(parseAppLocation('/mate/12345')).toEqual({ panel: 'assistant', conversationId: '12345' })
  })

  it('treats /assistant as a legacy alias of /mate', () => {
    expect(parseAppLocation('/assistant')).toEqual({ panel: 'assistant', conversationId: null })
  })

  it('drops extra path segments after a panel id', () => {
    expect(parseAppLocation('/forecast/extra')).toEqual({ panel: 'forecast' })
  })

  it('parses /settings as settings, General section', () => {
    expect(parseAppLocation('/settings')).toEqual(settings('general'))
  })

  it('parses /settings/<sectionId> as that section', () => {
    expect(parseAppLocation('/settings/signalk')).toEqual(settings('signalk'))
    expect(parseAppLocation('/settings/security')).toEqual(settings('security'))
  })

  // ADR 0123: Equipment profiles moved out of Settings into Inventory -
  // 'equipment' is no longer a SettingsSectionId at all, so a stale
  // /settings/equipment bookmark degrades the same way any other unknown
  // section id does (falls back to General), rather than resolving.
  it('parses the Import wizard route with its run id, and the list without one', () => {
    expect(parseAppLocation('/settings/import')).toEqual(settings('import'))
    expect(parseAppLocation('/settings/import/new')).toEqual(importRun('new'))
    expect(parseAppLocation('/settings/import/9d3e-uuid')).toEqual(importRun('9d3e-uuid'))
  })

  it('formats the Import wizard route back to the same path', () => {
    const ctx = { firstPageId: null }
    expect(formatAppLocation(settings('import'), ctx)).toBe('/settings/import')
    expect(formatAppLocation(importRun('run 1'), ctx)).toBe('/settings/import/run%201')
  })

  it('falls back to General for the retired /settings/equipment section id', () => {
    expect(parseAppLocation('/settings/equipment')).toEqual(settings('general'))
  })

  // ADR 0093: the onboard assistant's settings section.
  it('parses /settings/mate as the Assistant section', () => {
    expect(parseAppLocation('/settings/mate')).toEqual(settings('assistant'))
  })

  it('treats /settings/assistant as a legacy alias of /settings/mate', () => {
    expect(parseAppLocation('/settings/assistant')).toEqual(settings('assistant'))
  })

  it('falls back to General for an unknown settings section id', () => {
    expect(parseAppLocation('/settings/bogus')).toEqual(settings('general'))
  })

  it('drops a trailing slash', () => {
    expect(parseAppLocation('/settings/')).toEqual(settings('general'))
  })

  it('treats an unknown top-level path as the dashboard', () => {
    expect(parseAppLocation('/nonsense')).toEqual({ panel: null, pageId: null })
  })

  // ADR 0123: the Inventory panel. Same collapse-to-default shape as
  // /settings - the bare path is the Equipment section (the canonical
  // index), rather than needing its own /inventory/equipment.
  it('parses /inventory as the Equipment index', () => {
    expect(parseAppLocation('/inventory')).toEqual(indexOf('equipment'))
  })

  it('parses /inventory/equipment/<id> as the Equipment editor', () => {
    expect(parseAppLocation('/inventory/equipment/eq-1')).toEqual(inventory({ view: 'equipment', id: 'eq-1' }))
  })

  it('decodes a percent-encoded equipment edit id', () => {
    expect(parseAppLocation('/inventory/equipment/a%20b')).toEqual(inventory({ view: 'equipment', id: 'a b' }))
  })

  it('parses /inventory/profiles as the Profiles section', () => {
    expect(parseAppLocation('/inventory/profiles')).toEqual(indexOf('profiles'))
  })

  it('parses /inventory/locations as the Locations section', () => {
    expect(parseAppLocation('/inventory/locations')).toEqual(indexOf('locations'))
  })

  // ADR 0127: the bin page.
  it('parses /inventory/bins/<code> as the bin page', () => {
    expect(parseAppLocation('/inventory/bins/LAZ-02')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('decodes a percent-encoded bin code', () => {
    expect(parseAppLocation('/inventory/bins/LAZ%2002')).toEqual(inventory({ view: 'bin', code: 'LAZ 02' }))
  })

  it('collapses a bare /inventory/bins (no code) to the ordinary Locations shape', () => {
    expect(parseAppLocation('/inventory/bins')).toEqual(indexOf('locations'))
  })

  it('parses /inventory/locations/<id> as that location\'s page', () => {
    expect(parseAppLocation('/inventory/locations/z1')).toEqual(inventory({ view: 'location', id: 'z1' }))
  })

  it('decodes a percent-encoded location id', () => {
    expect(parseAppLocation('/inventory/locations/a%20b')).toEqual(inventory({ view: 'location', id: 'a b' }))
  })

  it('keeps /inventory/bins/<code> a bin page, not a location page', () => {
    expect(parseAppLocation('/inventory/bins/LAZ-02')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('parses /inventory/stocktake as the Stocktake section', () => {
    expect(parseAppLocation('/inventory/stocktake')).toEqual(indexOf('stocktake'))
  })
})

describe('formatAppLocation', () => {
  it('formats the dashboard with no page id, or the first page id, as /', () => {
    expect(formatAppLocation({ panel: null, pageId: null }, ctx)).toBe('/')
    expect(formatAppLocation({ panel: null, pageId: 'p1' }, ctx)).toBe('/')
  })

  it('formats a non-first dashboard page as /dashboard/<pageId>', () => {
    expect(formatAppLocation({ panel: null, pageId: 'p2' }, ctx)).toBe('/dashboard/p2')
  })

  it('encodes the page id', () => {
    expect(formatAppLocation({ panel: null, pageId: 'a b' }, ctx)).toBe('/dashboard/a%20b')
  })

  it.each(['forecast', 'routes', 'radar', 'anchor-watch', 'alarms'] as const)(
    'formats a panel as /%s',
    (panel) => {
      expect(formatAppLocation({ panel }, ctx)).toBe(`/${panel}`)
    },
  )

  it('formats the display panel with no slug as /display', () => {
    expect(formatAppLocation({ panel: 'display', slug: null, options: NO_OPTIONS }, ctx)).toBe('/display')
  })

  it('formats a display slug as /display/<slug>', () => {
    expect(formatAppLocation({ panel: 'display', slug: 'flybridge', options: NO_OPTIONS }, ctx)).toBe('/display/flybridge')
  })

  it('encodes the display slug', () => {
    expect(formatAppLocation({ panel: 'display', slug: 'saloon tv', options: NO_OPTIONS }, ctx)).toBe('/display/saloon%20tv')
  })

  it('formats the wall-displays panel with no slug as /wall-displays', () => {
    expect(formatAppLocation({ panel: 'wall-displays', route: { view: 'index' } }, ctx)).toBe('/wall-displays')
  })

  it('formats a wall-displays edit slug as /wall-displays/<slug>', () => {
    expect(formatAppLocation({ panel: 'wall-displays', route: { view: 'editor', slug: 'flybridge' } }, ctx)).toBe('/wall-displays/flybridge')
  })

  it('encodes the wall-displays edit slug', () => {
    expect(formatAppLocation({ panel: 'wall-displays', route: { view: 'editor', slug: 'saloon tv' } }, ctx)).toBe('/wall-displays/saloon%20tv')
  })

  it('formats the Mate panel as /mate', () => {
    expect(formatAppLocation({ panel: 'assistant', conversationId: null }, ctx)).toBe('/mate')
  })

  it('formats a Mate thread deeplink as /mate/<conversationId>', () => {
    expect(formatAppLocation({ panel: 'assistant', conversationId: '12345' }, ctx)).toBe('/mate/12345')
  })

  it('formats settings, General section as /settings', () => {
    expect(formatAppLocation(settings('general'), ctx)).toBe('/settings')
  })

  it('formats settings, another section as /settings/<sectionId>', () => {
    expect(formatAppLocation(settings('signalk'), ctx)).toBe('/settings/signalk')
    expect(formatAppLocation(settings('security'), ctx)).toBe('/settings/security')
  })

  it('formats settings, Assistant section as /settings/mate', () => {
    expect(formatAppLocation(settings('assistant'), ctx)).toBe('/settings/mate')
  })

  it('formats the Documents panel with no folder/document as /documents', () => {
    expect(formatAppLocation(browse(), ctx)).toBe('/documents')
  })

  it('formats a Documents folder as /documents?folder=<id>', () => {
    expect(formatAppLocation(browse('f1'), ctx)).toBe('/documents?folder=f1')
  })

  it('formats a Documents viewer deep link as /documents?document=<id>', () => {
    expect(formatAppLocation(browse(null, 'd1'), ctx)).toBe('/documents?document=d1')
  })

  it('formats a Documents folder plus viewer deep link together', () => {
    expect(formatAppLocation(browse('f1', 'd1'), ctx)).toBe(
      '/documents?folder=f1&document=d1',
    )
  })

  it('formats the Details page as /documents/<documentId>', () => {
    expect(formatAppLocation(details('doc-1'), ctx)).toBe('/documents/doc-1')
  })

  it('formats the Details page with a folder as /documents/<documentId>?folder=<id>', () => {
    expect(formatAppLocation(details('doc-1', 'f1'), ctx)).toBe(
      '/documents/doc-1?folder=f1',
    )
  })

  it('encodes the Details page document id', () => {
    expect(formatAppLocation(details('a b'), ctx)).toBe('/documents/a%20b')
  })

  // Revision "one panel, not three" (2026-09-20): a section within a
  // Manual-kind folder (documents-panel.tsx's own ManualFolderView) is
  // addressed the same way a document deep link is - a query param on the
  // existing /documents route, since a manual IS a folder rather than a
  // panel of its own.
  it('formats a folder with a section open as /documents?folder=<id>&section=<id>', () => {
    expect(formatAppLocation(browse('f1', null, 's1'), ctx)).toBe(
      '/documents?folder=f1&section=s1',
    )
  })

  it('drops a section with no folder open - it is meaningless on its own', () => {
    expect(formatAppLocation(browse(null, null, 's1'), ctx)).toBe('/documents')
  })

  it('formats the Inventory panel, Equipment section with no id, as /inventory', () => {
    expect(formatAppLocation(indexOf('equipment'), ctx)).toBe('/inventory')
  })

  it('formats an equipment edit id as /inventory/equipment/<id>', () => {
    expect(formatAppLocation(inventory({ view: 'equipment', id: 'eq-1' }), ctx)).toBe(
      '/inventory/equipment/eq-1',
    )
  })

  it('encodes the equipment edit id', () => {
    expect(formatAppLocation(inventory({ view: 'equipment', id: 'a b' }), ctx)).toBe(
      '/inventory/equipment/a%20b',
    )
  })

  it('formats the Profiles and Locations sections as /inventory/<section>', () => {
    expect(formatAppLocation(indexOf('profiles'), ctx)).toBe('/inventory/profiles')
    expect(formatAppLocation(indexOf('locations'), ctx)).toBe('/inventory/locations')
  })

  // ADR 0127: the bin page.
  it('formats a bin page as /inventory/bins/<code>', () => {
    expect(formatAppLocation(inventory({ view: 'bin', code: 'LAZ-02' }), ctx)).toBe(
      '/inventory/bins/LAZ-02',
    )
  })

  it('encodes the bin code', () => {
    expect(formatAppLocation(inventory({ view: 'bin', code: 'LAZ 02' }), ctx)).toBe(
      '/inventory/bins/LAZ%2002',
    )
  })

  // A New item draft has no id and so no URL of its own: the bar shows the
  // Equipment index until the operator actually saves.
  it('formats a New item draft as the Equipment index', () => {
    expect(formatAppLocation(inventory({ view: 'equipment-new', preset: null }), ctx)).toBe('/inventory')
    expect(formatAppLocation(inventory({ view: 'equipment-new', preset: { zoneId: 'z1', binId: 'b1' } }), ctx)).toBe('/inventory')
  })

  it('formats a location page as /inventory/locations/<id>', () => {
    expect(formatAppLocation(inventory({ view: 'location', id: 'z1' }), ctx)).toBe(
      '/inventory/locations/z1',
    )
    expect(formatAppLocation(inventory({ view: 'location', id: 'a b' }), ctx)).toBe(
      '/inventory/locations/a%20b',
    )
  })

  it('formats the Stocktake section as /inventory/stocktake', () => {
    expect(formatAppLocation(indexOf('stocktake'), ctx)).toBe('/inventory/stocktake')
  })
})

describe('parse/format fixed point', () => {
  const paths = [
    '/', '/dashboard/p2', '/dashboard/a%20b', '/forecast', '/routes',
    '/radar', '/anchor-watch', '/alarms', '/mate', '/mate/12345', '/settings', '/settings/signalk',
    '/display', '/display/flybridge',
    '/wall-displays', '/wall-displays/flybridge',
    '/documents', '/documents?folder=f1', '/documents?document=d1', '/documents?folder=f1&document=d1',
    '/documents?folder=f1&section=s1',
    '/documents/doc-1', '/documents/doc-1?folder=f1',
    '/inventory', '/inventory/equipment/eq-1', '/inventory/profiles', '/inventory/locations',
    '/inventory/bins/LAZ-02', '/inventory/locations/z1', '/inventory/stocktake',
  ]

  it.each(paths)('format(parse(%s)) === %s', (path) => {
    expect(formatAppLocation(parseAppLocation(path), ctx)).toBe(path)
  })
})

describe('isCanonicalAppPath', () => {
  const baseCtx: LocationContext = { firstPageId: 'p1', knownPageIds: ['p1', 'p2'], canAdmin: true }

  it('is false for a non-canonical settings path', () => {
    expect(isCanonicalAppPath('/settings/general', baseCtx)).toBe(false)
  })

  it('is false for an unknown top-level path', () => {
    expect(isCanonicalAppPath('/nonsense', baseCtx)).toBe(false)
  })

  it('is false for /settings when the caller cannot admin', () => {
    expect(isCanonicalAppPath('/settings', { ...baseCtx, canAdmin: false })).toBe(false)
  })

  it('is false for a dashboard page id not in the known page list', () => {
    expect(isCanonicalAppPath('/dashboard/zzz', { ...baseCtx, knownPageIds: ['p1'] })).toBe(false)
  })

  it('is false for /dashboard/<id> when that id is the first page (non-canonical form)', () => {
    expect(isCanonicalAppPath('/dashboard/p1', baseCtx)).toBe(false)
  })

  it('is true for the canonical forms', () => {
    expect(isCanonicalAppPath('/', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/dashboard/p2', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/settings', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/settings/signalk', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/forecast', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/mate', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/mate/12345', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/display', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/display/flybridge', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/documents', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/documents?folder=f1', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/documents?folder=f1&section=s1', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/inventory', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/inventory/equipment/eq-1', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/inventory/profiles', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/inventory/bins/LAZ-02', baseCtx)).toBe(true)
    expect(isCanonicalAppPath('/inventory/stocktake', baseCtx)).toBe(true)
  })

  it('is false for a bare /inventory/bins with no code', () => {
    expect(isCanonicalAppPath('/inventory/bins', baseCtx)).toBe(false)
  })

  it('is false for the legacy /assistant alias because canonical is /mate', () => {
    expect(isCanonicalAppPath('/assistant', baseCtx)).toBe(false)
  })

  it('accepts an unknown page id when the page list has not loaded (knownPageIds: null)', () => {
    expect(isCanonicalAppPath('/dashboard/zzz', { ...baseCtx, knownPageIds: null })).toBe(true)
  })
})

describe('resolveScannedText', () => {
  it('resolves a full https URL to a bin', () => {
    expect(resolveScannedText('https://boat.tailnet.ts.net/inventory/bins/LAZ-02')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('resolves a full https URL to an item', () => {
    expect(resolveScannedText('https://boat.tailnet.ts.net/inventory/equipment/eq-1')).toEqual(inventory({ view: 'equipment', id: 'eq-1' }))
  })

  it('resolves a scheme-less host + path the same way', () => {
    expect(resolveScannedText('boat.tailnet.ts.net/inventory/bins/LAZ-02')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('resolves a bare bin code with no slash', () => {
    expect(resolveScannedText('LAZ-02')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('rejects a bare code that contains a slash', () => {
    expect(resolveScannedText('LAZ/02')).toBeNull()
  })

  it('trims surrounding whitespace before resolving', () => {
    expect(resolveScannedText('  LAZ-02  ')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('returns null for a URL that parses but names no bin or item', () => {
    expect(resolveScannedText('https://example.com/inventory/locations')).toBeNull()
    expect(resolveScannedText('https://example.com/documents')).toBeNull()
  })

  it('strips a ?query and #hash from a scheme-less scan before resolving', () => {
    expect(resolveScannedText('boat.tailnet.ts.net/inventory/bins/LAZ-02?foo=bar')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
    expect(resolveScannedText('boat.tailnet.ts.net/inventory/bins/LAZ-02#section')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
    expect(resolveScannedText('LAZ-02?foo=bar')).toEqual(inventory({ view: 'bin', code: 'LAZ-02' }))
  })

  it('percent-encodes a bare bin code before building the path', () => {
    expect(resolveScannedText('LAZ 02')).toEqual(inventory({ view: 'bin', code: 'LAZ 02' }))
  })

  it('does not treat a bare code containing a colon as an absolute URL', () => {
    expect(resolveScannedText('LAZ:02')).toEqual(inventory({ view: 'bin', code: 'LAZ:02' }))
  })
})

describe('deck plans in the Locations section (ADR 0156)', () => {
  const ctx = { firstPageId: null }

  it('parses the Plan view with and without a deck', () => {
    expect(parseAppLocation('/inventory/locations?view=plan')).toEqual(inventory({ view: 'plan', deckId: null }))
    expect(parseAppLocation('/inventory/locations?view=plan&deck=d1')).toEqual(inventory({ view: 'plan', deckId: 'd1' }))
  })

  it('ignores an unknown view', () => {
    expect(parseAppLocation('/inventory/locations?view=grid')).toEqual(indexOf('locations'))
  })

  it('parses the Decks list and a deck page', () => {
    expect(parseAppLocation('/inventory/decks')).toEqual(inventory({ view: 'decks' }))
    expect(parseAppLocation('/inventory/decks/d%201')).toEqual(inventory({ view: 'deck', id: 'd 1' }))
  })

  it('formats them back to the same paths', () => {
    expect(formatAppLocation(inventory({ view: 'plan', deckId: null }), ctx)).toBe('/inventory/locations?view=plan')
    expect(formatAppLocation(inventory({ view: 'plan', deckId: 'd1' }), ctx)).toBe('/inventory/locations?view=plan&deck=d1')
    expect(formatAppLocation(inventory({ view: 'decks' }), ctx)).toBe('/inventory/decks')
    expect(formatAppLocation(inventory({ view: 'deck', id: 'd 1' }), ctx)).toBe('/inventory/decks/d%201')
  })

  it('treats the new paths as canonical', () => {
    const c = { firstPageId: null, knownPageIds: null, canAdmin: true }
    for (const path of ['/inventory/locations?view=plan', '/inventory/locations?view=plan&deck=d1', '/inventory/decks', '/inventory/decks/d1']) {
      expect(isCanonicalAppPath(path, c)).toBe(true)
    }
  })
})

describe('Passages panel (ADR 0171 panel route)', () => {
  const index = (filter: import('@/lib/app-location').PassagesFilter = { kind: 'all' }): AppLocation => ({
    panel: 'passages', route: { view: 'index', filter },
  })

  it('parses bare /passages as the index showing every passage', () => {
    expect(parseAppLocation('/passages')).toEqual(index())
  })

  it('parses the unfiled and cruise filters', () => {
    expect(parseAppLocation('/passages?filter=unfiled')).toEqual(index({ kind: 'unfiled' }))
    expect(parseAppLocation('/passages?cruise=c1')).toEqual(index({ kind: 'cruise', id: 'c1' }))
  })

  it('parses a passage, a cruise and a combined selection', () => {
    expect(parseAppLocation('/passages/p1')).toEqual({
      panel: 'passages', route: { view: 'passage', id: 'p1', filter: { kind: 'all' } },
    })
    expect(parseAppLocation('/passages/p1?cruise=c1')).toEqual({
      panel: 'passages', route: { view: 'passage', id: 'p1', filter: { kind: 'cruise', id: 'c1' } },
    })
    expect(parseAppLocation('/passages/cruises/c1')).toEqual({ panel: 'passages', route: { view: 'cruise', id: 'c1' } })
    expect(parseAppLocation('/passages/combined?ids=a,b,c')).toEqual({
      panel: 'passages', route: { view: 'combined', ids: ['a', 'b', 'c'], filter: { kind: 'all' } },
    })
  })

  it('falls back to the index for a combined link with no ids or a cruise link with no id', () => {
    expect(parseAppLocation('/passages/combined')).toEqual(index())
    expect(parseAppLocation('/passages/cruises')).toEqual(index())
  })

  it('treats a malformed escape as no id', () => {
    expect(parseAppLocation('/passages/%E0%A4%A')).toEqual(index())
  })

  it('formats every shape and round-trips it', () => {
    const shapes: AppLocation[] = [
      index(),
      index({ kind: 'unfiled' }),
      index({ kind: 'cruise', id: 'c 1' }),
      { panel: 'passages', route: { view: 'passage', id: 'p1', filter: { kind: 'unfiled' } } },
      { panel: 'passages', route: { view: 'cruise', id: 'c1' } },
      { panel: 'passages', route: { view: 'combined', ids: ['a', 'b'], filter: { kind: 'cruise', id: 'c1' } } },
    ]
    for (const loc of shapes) {
      expect(parseAppLocation(formatAppLocation(loc, ctx))).toEqual(loc)
    }
    expect(formatAppLocation(index(), ctx)).toBe('/passages')
    expect(formatAppLocation(shapes[3], ctx)).toBe('/passages/p1?filter=unfiled')
  })

  it('is a canonical path once formatted', () => {
    const c = { firstPageId: 'p1', knownPageIds: null, canAdmin: true }
    expect(isCanonicalAppPath('/passages', c)).toBe(true)
    expect(isCanonicalAppPath('/passages/cruises/c1', c)).toBe(true)
  })
})
