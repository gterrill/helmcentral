/**
 * Pure parse/format tests for the deep-link URL scheme (ADR 0074). No React
 * — app-location.ts has none, so these run directly against the module.
 */
import { describe, it, expect } from 'vitest'
import {
  parseAppLocation,
  formatAppLocation,
  isCanonicalAppPath,
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

  it.each(['forecast', 'routes', 'charts', 'radar', 'anchor-watch', 'alarms', 'kiosk'] as const)(
    'parses /%s as that panel',
    (panel) => {
      expect(parseAppLocation(`/${panel}`)).toEqual({ panel })
    },
  )

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
    expect(parseAppLocation('/settings/equipment')).toEqual({ panel: 'settings', section: 'equipment' })
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

  it.each(['forecast', 'routes', 'charts', 'radar', 'anchor-watch', 'alarms', 'kiosk'] as const)(
    'formats a panel as /%s',
    (panel) => {
      expect(formatAppLocation({ panel }, ctx)).toBe(`/${panel}`)
    },
  )

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
    expect(formatAppLocation({ panel: 'settings', section: 'equipment' }, ctx)).toBe('/settings/equipment')
  })

  it('formats settings, Assistant section as /settings/mate', () => {
    expect(formatAppLocation({ panel: 'settings', section: 'assistant' }, ctx)).toBe('/settings/mate')
  })
})

describe('parse/format fixed point', () => {
  const paths = [
    '/', '/dashboard/p2', '/dashboard/a%20b', '/forecast', '/routes', '/charts',
    '/radar', '/anchor-watch', '/alarms', '/mate', '/mate/12345', '/settings', '/settings/signalk', '/kiosk',
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
    expect(isCanonicalAppPath('/kiosk', baseCtx)).toBe(true)
  })

  it('is false for the legacy /assistant alias because canonical is /mate', () => {
    expect(isCanonicalAppPath('/assistant', baseCtx)).toBe(false)
  })

  it('accepts an unknown page id when the page list has not loaded (knownPageIds: null)', () => {
    expect(isCanonicalAppPath('/dashboard/zzz', { ...baseCtx, knownPageIds: null })).toBe(true)
  })
})
