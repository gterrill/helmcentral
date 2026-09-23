/**
 * Pure parse/format tests for the deep-link URL scheme (ADR 0074). No React
 * — app-location.ts has none, so these run directly against the module.
 */
import { describe, it, expect } from 'vitest'
import {
  parseAppLocation,
  formatAppLocation,
  isCanonicalAppPath,
  inventoryEditorClosedBy,
  type LocationContext,
} from '@/lib/app-location'

const ctx: Pick<LocationContext, 'firstPageId'> = { firstPageId: 'p1' }

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
    expect(parseAppLocation('/display')).toEqual({ panel: 'display', displaySlug: null })
  })

  it('parses /display/<slug> as that display', () => {
    expect(parseAppLocation('/display/flybridge')).toEqual({ panel: 'display', displaySlug: 'flybridge' })
  })

  it('decodes a percent-encoded display slug', () => {
    expect(parseAppLocation('/display/saloon%20tv')).toEqual({ panel: 'display', displaySlug: 'saloon tv' })
  })

  it('treats a malformed percent-escape in the display slug as no slug rather than throwing', () => {
    expect(parseAppLocation('/display/%E0%A4%A')).toEqual({ panel: 'display', displaySlug: null })
  })

  // The wall-displays management editor: /display/<slug> is the wall itself
  // (typed into a kiosk browser), /wall-displays/<slug> is the admin editor
  // for that display's config. Same parse/format shape as /display, on a
  // deliberately dissimilar path so a typo in either can never land on the
  // other's route.
  it('parses /wall-displays with no slug as the index', () => {
    expect(parseAppLocation('/wall-displays')).toEqual({ panel: 'wall-displays', displayEditSlug: null })
  })

  it('parses /wall-displays/<slug> as that display\'s editor', () => {
    expect(parseAppLocation('/wall-displays/flybridge')).toEqual({ panel: 'wall-displays', displayEditSlug: 'flybridge' })
  })

  it('decodes a percent-encoded wall-displays edit slug', () => {
    expect(parseAppLocation('/wall-displays/saloon%20tv')).toEqual({ panel: 'wall-displays', displayEditSlug: 'saloon tv' })
  })

  it('treats a malformed percent-escape in the wall-displays edit slug as no slug rather than throwing', () => {
    expect(parseAppLocation('/wall-displays/%E0%A4%A')).toEqual({ panel: 'wall-displays', displayEditSlug: null })
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
    expect(wall).toEqual({ panel: 'display', displaySlug: 'x' })
    expect(editor).toEqual({ panel: 'wall-displays', displayEditSlug: 'x' })
  })

  // ADR 0106 F1: the Documents panel carries its current folder (and, on a
  // deep link from a Mate attachment chip, which document to open in the
  // viewer) as query params rather than path segments, since either can be
  // absent independently and neither has a natural path position the way
  // /mate/<conversationId> does.
  it('parses /documents with no query as the Documents panel at the root', () => {
    expect(parseAppLocation('/documents')).toEqual({ panel: 'documents', documentFolderId: null, documentId: null })
  })

  it('parses /documents?folder=<id> as that folder', () => {
    expect(parseAppLocation('/documents?folder=f1')).toEqual({
      panel: 'documents', documentFolderId: 'f1', documentId: null,
    })
  })

  it('parses /documents?document=<id> as a viewer deep link, root folder', () => {
    expect(parseAppLocation('/documents?document=d1')).toEqual({
      panel: 'documents', documentFolderId: null, documentId: 'd1',
    })
  })

  it('parses /documents?folder=<id>&document=<id> as both together', () => {
    expect(parseAppLocation('/documents?folder=f1&document=d1')).toEqual({
      panel: 'documents', documentFolderId: 'f1', documentId: 'd1',
    })
  })

  // ADR 0115 §2: the Details page's own route. Carries the folder
  // on purpose (unlike the viewer's ?document=, which never needs one - the
  // viewer is a dialog over whatever folder is already open) - without it,
  // Back out of the Details page would land the listing at the root instead
  // of the folder it was opened from.
  it('parses /documents/<documentId> as the Details page, root folder', () => {
    expect(parseAppLocation('/documents/doc-1')).toEqual({
      panel: 'documents', documentEditId: 'doc-1', documentFolderId: null, documentId: null,
    })
  })

  it('parses /documents/<documentId>?folder=<id> as the Details page, that folder', () => {
    expect(parseAppLocation('/documents/doc-1?folder=f1')).toEqual({
      panel: 'documents', documentEditId: 'doc-1', documentFolderId: 'f1', documentId: null,
    })
  })

  it('decodes a percent-encoded Details page document id', () => {
    expect(parseAppLocation('/documents/a%20b')).toEqual({
      panel: 'documents', documentEditId: 'a b', documentFolderId: null, documentId: null,
    })
  })

  // decodeSegment's own tolerance (see its doc comment): a malformed escape
  // degrades to the ordinary browse shape rather than a half-parsed Details
  // route, the same way every other caller of decodeSegment falls back to
  // its own "nothing named" state instead of surfacing a parse error.
  it('treats a malformed percent-escape in the Details page id as the ordinary browse shape', () => {
    expect(parseAppLocation('/documents/%E0%A4%A')).toEqual({
      panel: 'documents', documentFolderId: null, documentId: null,
    })
  })

  // Revision "one panel, not three" (2026-09-20): /notes and /manuals are
  // gone - a note opens through the existing ?document= (it's a document
  // with kind='note'), and a manual is addressed by ?folder=<id> like any
  // other folder, since a manual IS a folder. ?section=<id> is the only new
  // piece: which node in that folder's manual tree is open in the reading
  // pane, meaningless without a folder the same way manualSectionId used to
  // be meaningless without a manual.
  it('parses /documents?folder=<id>&section=<id> as that folder with a section open', () => {
    expect(parseAppLocation('/documents?folder=f1&section=s1')).toEqual({
      panel: 'documents', documentFolderId: 'f1', documentId: null, documentSectionId: 's1',
    })
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
    expect(parseAppLocation('/settings')).toEqual({ panel: 'settings', section: 'general' })
  })

  it('parses /settings/<sectionId> as that section', () => {
    expect(parseAppLocation('/settings/signalk')).toEqual({ panel: 'settings', section: 'signalk' })
    expect(parseAppLocation('/settings/security')).toEqual({ panel: 'settings', section: 'security' })
  })

  // ADR 0123: Equipment profiles moved out of Settings into Inventory -
  // 'equipment' is no longer a SettingsSectionId at all, so a stale
  // /settings/equipment bookmark degrades the same way any other unknown
  // section id does (falls back to General), rather than resolving.
  it('falls back to General for the retired /settings/equipment section id', () => {
    expect(parseAppLocation('/settings/equipment')).toEqual({ panel: 'settings', section: 'general' })
  })

  // ADR 0093: the onboard assistant's settings section.
  it('parses /settings/mate as the Assistant section', () => {
    expect(parseAppLocation('/settings/mate')).toEqual({ panel: 'settings', section: 'assistant' })
  })

  it('treats /settings/assistant as a legacy alias of /settings/mate', () => {
    expect(parseAppLocation('/settings/assistant')).toEqual({ panel: 'settings', section: 'assistant' })
  })

  it('falls back to General for an unknown settings section id', () => {
    expect(parseAppLocation('/settings/bogus')).toEqual({ panel: 'settings', section: 'general' })
  })

  it('drops a trailing slash', () => {
    expect(parseAppLocation('/settings/')).toEqual({ panel: 'settings', section: 'general' })
  })

  it('treats an unknown top-level path as the dashboard', () => {
    expect(parseAppLocation('/nonsense')).toEqual({ panel: null, pageId: null })
  })

  // ADR 0123: the Inventory panel. Same collapse-to-default shape as
  // /settings - the bare path is the Equipment section (the canonical
  // index), rather than needing its own /inventory/equipment.
  it('parses /inventory as the Equipment index', () => {
    expect(parseAppLocation('/inventory')).toEqual({
      panel: 'inventory', inventorySection: 'equipment', equipmentEditId: null,
    })
  })

  it('parses /inventory/equipment/<id> as the Equipment editor', () => {
    expect(parseAppLocation('/inventory/equipment/eq-1')).toEqual({
      panel: 'inventory', inventorySection: 'equipment', equipmentEditId: 'eq-1',
    })
  })

  it('decodes a percent-encoded equipment edit id', () => {
    expect(parseAppLocation('/inventory/equipment/a%20b')).toEqual({
      panel: 'inventory', inventorySection: 'equipment', equipmentEditId: 'a b',
    })
  })

  it('parses /inventory/profiles as the Profiles section', () => {
    expect(parseAppLocation('/inventory/profiles')).toEqual({ panel: 'inventory', inventorySection: 'profiles' })
  })

  it('parses /inventory/locations as the Locations section', () => {
    expect(parseAppLocation('/inventory/locations')).toEqual({ panel: 'inventory', inventorySection: 'locations' })
  })

  // ADR 0127: the bin page.
  it('parses /inventory/bins/<code> as the Locations section with a binCode', () => {
    expect(parseAppLocation('/inventory/bins/LAZ-02')).toEqual({
      panel: 'inventory', inventorySection: 'locations', binCode: 'LAZ-02',
    })
  })

  it('decodes a percent-encoded bin code', () => {
    expect(parseAppLocation('/inventory/bins/LAZ%2002')).toEqual({
      panel: 'inventory', inventorySection: 'locations', binCode: 'LAZ 02',
    })
  })

  it('collapses a bare /inventory/bins (no code) to the ordinary Locations shape', () => {
    expect(parseAppLocation('/inventory/bins')).toEqual({ panel: 'inventory', inventorySection: 'locations' })
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
    expect(formatAppLocation({ panel: 'display' }, ctx)).toBe('/display')
    expect(formatAppLocation({ panel: 'display', displaySlug: null }, ctx)).toBe('/display')
  })

  it('formats a display slug as /display/<slug>', () => {
    expect(formatAppLocation({ panel: 'display', displaySlug: 'flybridge' }, ctx)).toBe('/display/flybridge')
  })

  it('encodes the display slug', () => {
    expect(formatAppLocation({ panel: 'display', displaySlug: 'saloon tv' }, ctx)).toBe('/display/saloon%20tv')
  })

  it('formats the wall-displays panel with no slug as /wall-displays', () => {
    expect(formatAppLocation({ panel: 'wall-displays' }, ctx)).toBe('/wall-displays')
    expect(formatAppLocation({ panel: 'wall-displays', displayEditSlug: null }, ctx)).toBe('/wall-displays')
  })

  it('formats a wall-displays edit slug as /wall-displays/<slug>', () => {
    expect(formatAppLocation({ panel: 'wall-displays', displayEditSlug: 'flybridge' }, ctx)).toBe('/wall-displays/flybridge')
  })

  it('encodes the wall-displays edit slug', () => {
    expect(formatAppLocation({ panel: 'wall-displays', displayEditSlug: 'saloon tv' }, ctx)).toBe('/wall-displays/saloon%20tv')
  })

  it('formats the Mate panel as /mate', () => {
    expect(formatAppLocation({ panel: 'assistant' }, ctx)).toBe('/mate')
  })

  it('formats a Mate thread deeplink as /mate/<conversationId>', () => {
    expect(formatAppLocation({ panel: 'assistant', conversationId: '12345' }, ctx)).toBe('/mate/12345')
  })

  it('formats settings, General section as /settings', () => {
    expect(formatAppLocation({ panel: 'settings', section: 'general' }, ctx)).toBe('/settings')
    expect(formatAppLocation({ panel: 'settings' }, ctx)).toBe('/settings')
  })

  it('formats settings, another section as /settings/<sectionId>', () => {
    expect(formatAppLocation({ panel: 'settings', section: 'signalk' }, ctx)).toBe('/settings/signalk')
    expect(formatAppLocation({ panel: 'settings', section: 'security' }, ctx)).toBe('/settings/security')
  })

  it('formats settings, Assistant section as /settings/mate', () => {
    expect(formatAppLocation({ panel: 'settings', section: 'assistant' }, ctx)).toBe('/settings/mate')
  })

  it('formats the Documents panel with no folder/document as /documents', () => {
    expect(formatAppLocation({ panel: 'documents' }, ctx)).toBe('/documents')
    expect(formatAppLocation({ panel: 'documents', documentFolderId: null, documentId: null }, ctx)).toBe('/documents')
  })

  it('formats a Documents folder as /documents?folder=<id>', () => {
    expect(formatAppLocation({ panel: 'documents', documentFolderId: 'f1' }, ctx)).toBe('/documents?folder=f1')
  })

  it('formats a Documents viewer deep link as /documents?document=<id>', () => {
    expect(formatAppLocation({ panel: 'documents', documentId: 'd1' }, ctx)).toBe('/documents?document=d1')
  })

  it('formats a Documents folder plus viewer deep link together', () => {
    expect(formatAppLocation({ panel: 'documents', documentFolderId: 'f1', documentId: 'd1' }, ctx)).toBe(
      '/documents?folder=f1&document=d1',
    )
  })

  it('formats the Details page as /documents/<documentId>', () => {
    expect(formatAppLocation({ panel: 'documents', documentEditId: 'doc-1' }, ctx)).toBe('/documents/doc-1')
  })

  it('formats the Details page with a folder as /documents/<documentId>?folder=<id>', () => {
    expect(formatAppLocation({ panel: 'documents', documentEditId: 'doc-1', documentFolderId: 'f1' }, ctx)).toBe(
      '/documents/doc-1?folder=f1',
    )
  })

  it('never serializes documentId alongside a Details page documentEditId', () => {
    expect(formatAppLocation({ panel: 'documents', documentEditId: 'doc-1', documentId: 'd2' }, ctx)).toBe('/documents/doc-1')
  })

  it('encodes the Details page document id', () => {
    expect(formatAppLocation({ panel: 'documents', documentEditId: 'a b' }, ctx)).toBe('/documents/a%20b')
  })

  // Revision "one panel, not three" (2026-09-20): a section within a
  // Manual-kind folder (documents-panel.tsx's own ManualFolderView) is
  // addressed the same way a document deep link is - a query param on the
  // existing /documents route, since a manual IS a folder rather than a
  // panel of its own.
  it('formats a folder with a section open as /documents?folder=<id>&section=<id>', () => {
    expect(formatAppLocation({ panel: 'documents', documentFolderId: 'f1', documentSectionId: 's1' }, ctx)).toBe(
      '/documents?folder=f1&section=s1',
    )
  })

  it('drops a section with no folder open - it is meaningless on its own', () => {
    expect(formatAppLocation({ panel: 'documents', documentFolderId: null, documentSectionId: 's1' }, ctx)).toBe('/documents')
  })

  it('formats the Inventory panel, Equipment section with no id, as /inventory', () => {
    expect(formatAppLocation({ panel: 'inventory' }, ctx)).toBe('/inventory')
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', equipmentEditId: null }, ctx)).toBe('/inventory')
  })

  it('formats an equipment edit id as /inventory/equipment/<id>', () => {
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', equipmentEditId: 'eq-1' }, ctx)).toBe(
      '/inventory/equipment/eq-1',
    )
  })

  it('encodes the equipment edit id', () => {
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', equipmentEditId: 'a b' }, ctx)).toBe(
      '/inventory/equipment/a%20b',
    )
  })

  it('formats the Profiles and Locations sections as /inventory/<section>', () => {
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'profiles' }, ctx)).toBe('/inventory/profiles')
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'locations' }, ctx)).toBe('/inventory/locations')
  })

  // ADR 0127: the bin page.
  it('formats a binCode as /inventory/bins/<code>', () => {
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'locations', binCode: 'LAZ-02' }, ctx)).toBe(
      '/inventory/bins/LAZ-02',
    )
  })

  it('encodes the bin code', () => {
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'locations', binCode: 'LAZ 02' }, ctx)).toBe(
      '/inventory/bins/LAZ%2002',
    )
  })

  it('drops a binCode on the Equipment section - it is meaningless on its own', () => {
    expect(formatAppLocation({ panel: 'inventory', inventorySection: 'equipment', binCode: 'LAZ-02' }, ctx)).toBe('/inventory')
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
    '/inventory/bins/LAZ-02',
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

describe('inventoryEditorClosedBy', () => {
  const at = (path: string) => parseAppLocation(path)

  it('says no when the editor is not showing at all', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: null, creating: false },
      at('/inventory/locations'),
    )).toBe(false)
  })

  it('says no when the target is the same record', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: 'eq-1', creating: false },
      at('/inventory/equipment/eq-1'),
    )).toBe(false)
  })

  it('says yes when the target is a different record', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: 'eq-1', creating: false },
      at('/inventory/equipment/eq-2'),
    )).toBe(true)
  })

  it('says yes when the target is the index', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: 'eq-1', creating: false },
      at('/inventory'),
    )).toBe(true)
  })

  // The hole this function was extracted to close: a section switch leaves the
  // equipment id null on both sides, so comparing ids alone saw no change and
  // let a dirty editor close unguarded.
  it('says yes when only the section changed', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: 'eq-1', creating: false },
      at('/inventory/profiles'),
    )).toBe(true)
  })

  // And the same hole for a New item draft, which has no id to compare with.
  it('says yes for an unsaved New item draft, whatever the inventory target', () => {
    const draft = { section: 'equipment' as const, equipmentEditId: null, creating: true }
    expect(inventoryEditorClosedBy(draft, at('/inventory'))).toBe(true)
    expect(inventoryEditorClosedBy(draft, at('/inventory/profiles'))).toBe(true)
    expect(inventoryEditorClosedBy(draft, at('/inventory/locations'))).toBe(true)
  })

  it('says yes when leaving the panel entirely', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: 'eq-1', creating: false },
      at('/documents'),
    )).toBe(true)
  })

  // ADR 0127: a bin page target is inventorySection 'locations', so it is
  // already covered by the same "any non-equipment section closes it" check
  // as a plain /inventory/locations - no special case needed, and this
  // pins that a target carrying a binCode doesn't slip through.
  it('says yes when the target is a bin page', () => {
    expect(inventoryEditorClosedBy(
      { section: 'equipment', equipmentEditId: 'eq-1', creating: false },
      at('/inventory/bins/LAZ-02'),
    )).toBe(true)
  })
})
