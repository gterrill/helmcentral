import type { AppLocation, PanelId } from '@/lib/app-location'
import type { SettingsSectionId } from '@/components/settings/settings-nav'

// ADR 0095: the in-app manual. Pure and React-free, like app-location.ts and
// mate-screen.ts - it turns app state into "where in the manual does this
// screen live" without importing App.tsx (which would create a cycle) and
// without pulling react-markdown or any DOM API into a module that's tested
// as plain data.

/** A page in the embedded manual, and optionally a heading on it to land on. */
export interface ManualTarget {
  page: string
  heading?: string
}

/** The hand-written contents page - what the sidebar's Manual item opens. */
export const MANUAL_INDEX: ManualTarget = { page: 'index' }

/**
 * Every dashboard/panel screen's manual target, keyed by PanelId. This is a
 * `Record`, not a lookup function with a default case, so adding a PanelId in
 * app-location.ts without adding a row here is a compile error rather than a
 * silent fallback to the index.
 *
 * `settings` and `display` are never actually read through this table -
 * manualTargetFor branches settings off to SETTINGS_MANUAL_TARGETS before
 * consulting this one, and the wall display route (ADR 0110) mounts no
 * Manual affordance at all (App.tsx's kiosk-route early return precedes the
 * header, sidebar and every sheet). Both rows exist purely so the Record
 * stays total.
 */
export const PANEL_MANUAL_TARGETS: Record<PanelId, ManualTarget> = {
  forecast: { page: 'features/forecast' },
  routes: { page: 'features/dashboard', heading: 'Route planning' },
  radar: { page: 'features/dashboard', heading: 'Radar targets' },
  'anchor-watch': { page: 'features/anchor-watch' },
  alarms: { page: 'features/alarms' },
  assistant: { page: 'features/assistant' },
  documents: { page: 'features/documents' },
  // Reachable, unlike the two below: the management surface (ADR 0112)
  // mounts the ordinary header, so its Manual affordance resolves here.
  'wall-displays': { page: 'features/dashboard', heading: 'Wall displays' },
  // Unreachable (see above) - set to General's target for totality.
  settings: { page: 'index' },
  // Unreachable (see above) - the manual index, for totality.
  display: MANUAL_INDEX,
}

/** Every Settings section's manual target, keyed by SettingsSectionId. Same
 * exhaustive-Record reasoning as PANEL_MANUAL_TARGETS above. */
export const SETTINGS_MANUAL_TARGETS: Record<SettingsSectionId, ManualTarget> = {
  // Units is documented nowhere yet - stated explicitly rather than pointed
  // at a page that doesn't cover it.
  general: MANUAL_INDEX,
  'boat-ui': { page: 'features/dashboard', heading: 'Battery & Power' },
  signalk: { page: 'reference/configuration', heading: 'SignalK' },
  influxdb: { page: 'reference/configuration', heading: 'Environment variables' },
  'anchor-watch': { page: 'features/anchor-watch', heading: 'The rode planner' },
  mayara: { page: 'features/dashboard', heading: 'Radar targets' },
  alarms: { page: 'features/alarms', heading: 'Getting told' },
  assistant: { page: 'how-to/set-up-the-assistant', heading: '2. Configure it in Helmcentral' },
  equipment: { page: 'features/inventory-tracking', heading: 'Settings' },
  tiles: { page: 'reference/plugins' },
  security: { page: 'reference/configuration', heading: 'Security' },
  logs: MANUAL_INDEX,
}

/** Where the dashboard grid itself (panel === null) opens in the manual. */
export const DASHBOARD_MANUAL_TARGET: ManualTarget = { page: 'features/dashboard' }

/**
 * The empty-page prompt's own link (ADR 0107) — a page with nothing on it
 * yet points straight at the how-to for building one, rather than the
 * general dashboard feature page DASHBOARD_MANUAL_TARGET above sends the
 * header `?` to.
 */
export const CREATE_PAGE_MANUAL_TARGET: ManualTarget = { page: 'how-to/create-a-dashboard-page' }

/** The contextual `?`'s target for the screen `location` currently shows. */
export function manualTargetFor(location: AppLocation): ManualTarget {
  if (location.panel === null) return DASHBOARD_MANUAL_TARGET
  if (location.panel === 'settings') return SETTINGS_MANUAL_TARGETS[location.section ?? 'general']
  return PANEL_MANUAL_TARGETS[location.panel]
}

// GitHub's own heading-slug algorithm (what every ##/### heading's anchor id
// is on github.com, and so what every relative doc link's #hash already
// assumes): lowercase, drop anything that isn't a letter, digit, space or
// hyphen, then turn each space into a hyphen. Consecutive hyphens are NOT
// collapsed - "Battery & Power" loses only the "&", leaving the space on
// each side, which is why its slug is "battery--power" with two hyphens.
//
// No duplicate heading text exists anywhere in docs/ today, so the
// "-1", "-2", ... suffix GitHub (and github-slugger) appends to a repeated
// heading is never exercised and deliberately not implemented here - adding
// a second heading with the same text anywhere in the manual needs this
// revisited, not silently mismatched.
export function slugifyHeading(text: string): string {
  return text
    .toLowerCase()
    .replace(/[^a-z0-9 -]/g, '')
    .replace(/ /g, '-')
}

/** Where a link found inside a rendered manual page's markdown should go. */
export type ManualLink =
  | { kind: 'anchor'; hash: string }
  | { kind: 'page'; page: string; hash?: string }
  | { kind: 'external'; url: string }

// Matches the backend's own hardcoded repo reference (openrouter_client.go's
// HTTP-Referer, webpush_notify.go's webPushSubscriber) - one canonical
// spelling of "this project on GitHub" rather than a second copy that can
// drift from it.
const REPO_URL = 'https://github.com/gterrill/helmcentral'

function splitHash(href: string): [string, string | undefined] {
  const index = href.indexOf('#')
  return index === -1 ? [href, undefined] : [href.slice(0, index), href.slice(index + 1)]
}

// A manual page id's "directory": the part before its last slash, or '' for
// a root-level id like "index" (mirroring how loadManual derives the id
// itself from a file path under manual/).
function pageDirectory(pageId: string): string {
  const index = pageId.lastIndexOf('/')
  return index === -1 ? '' : pageId.slice(0, index)
}

// A tiny posix path normaliser (no node:path - this module stays
// browser-safe): resolves "." and ".." segments against the segments before
// them. Never throws on a ".." that runs off the front; it just stops
// popping, which is exactly what leaving docs/ entirely from a shallow page
// needs to do.
function normalizePath(path: string): string {
  const segments: string[] = []
  for (const part of path.split('/')) {
    if (part === '' || part === '.') continue
    if (part === '..') segments.pop()
    else segments.push(part)
  }
  return segments.join('/')
}

const MANUAL_PAGE_PATH = /^docs\/(features|how-to|reference)\/(.+)\.md$/

// Resolves a markdown link's `href`, found on the page named `currentPageId`,
// to where the Manual sheet should send the operator: an anchor on the same
// page, another in-tree manual page, or an external GitHub URL for anything
// the manual doesn't stage (adr/, examples/, the repo root, dotfiles).
export function resolveManualHref(href: string, currentPageId: string): ManualLink {
  if (href.startsWith('#')) return { kind: 'anchor', hash: href.slice(1) }
  if (/^(https?:|mailto:)/.test(href)) return { kind: 'external', url: href }

  const [hrefPath, hash] = splitHash(href)
  const directory = pageDirectory(currentPageId)
  const joined = directory ? `docs/${directory}/${hrefPath}` : `docs/${hrefPath}`
  const normalized = normalizePath(joined)

  const manualMatch = MANUAL_PAGE_PATH.exec(normalized)
  const pageId = normalized === 'docs/index.md' ? 'index' : manualMatch ? `${manualMatch[1]}/${manualMatch[2]}` : null

  if (pageId !== null) {
    return hash ? { kind: 'page', page: pageId, hash } : { kind: 'page', page: pageId }
  }

  const isDirectoryLink = href.endsWith('/')
  const url = `${REPO_URL}/${isDirectoryLink ? 'tree' : 'blob'}/main/${normalized}`
  return { kind: 'external', url: hash ? `${url}#${hash}` : url }
}

// Strips body's leading "# Title" line (and the blank line after it) - the
// Manual sheet's own header already shows the title, so the body would
// otherwise repeat it.
export function manualBodyWithoutTitle(body: string): string {
  const newlineIndex = body.indexOf('\n')
  const firstLine = newlineIndex === -1 ? body : body.slice(0, newlineIndex)
  if (!firstLine.startsWith('# ')) return body
  const rest = newlineIndex === -1 ? '' : body.slice(newlineIndex + 1)
  return rest.replace(/^\n+/, '')
}
