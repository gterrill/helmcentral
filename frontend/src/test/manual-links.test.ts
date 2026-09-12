import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, resolve } from 'node:path'

import { describe, it, expect } from 'vitest'

import { PANEL_IDS } from '@/lib/app-location'
import { SETTINGS_SECTIONS } from '@/components/settings/settings-nav'
import {
  DASHBOARD_MANUAL_TARGET,
  MANUAL_INDEX,
  PANEL_MANUAL_TARGETS,
  SETTINGS_MANUAL_TARGETS,
  manualBodyWithoutTitle,
  manualTargetFor,
  resolveManualHref,
  slugifyHeading,
} from '@/lib/manual-links'

describe('manualTargetFor', () => {
  it('sends the dashboard (panel null) to the dashboard manual page', () => {
    expect(manualTargetFor({ panel: null })).toEqual(DASHBOARD_MANUAL_TARGET)
  })

  it('sends every non-settings panel to its own table entry', () => {
    for (const id of PANEL_IDS) {
      if (id === 'settings') continue
      expect(manualTargetFor({ panel: id })).toEqual(PANEL_MANUAL_TARGETS[id])
    }
  })

  it('sends settings to the section table, defaulting to general when no section is set', () => {
    expect(manualTargetFor({ panel: 'settings' })).toEqual(SETTINGS_MANUAL_TARGETS.general)
    for (const section of SETTINGS_SECTIONS) {
      expect(manualTargetFor({ panel: 'settings', section: section.id })).toEqual(
        SETTINGS_MANUAL_TARGETS[section.id],
      )
    }
  })

  it('gives the settings panel-table entry the same target as the general section, for totality', () => {
    expect(PANEL_MANUAL_TARGETS.settings).toEqual(SETTINGS_MANUAL_TARGETS.general)
  })

  it('gives the kiosk panel-table entry the manual index, unreachable but total', () => {
    expect(PANEL_MANUAL_TARGETS.kiosk).toEqual(MANUAL_INDEX)
  })
})

// The lookup table is verified against the docs' actual headings so a docs
// rename fails the frontend build instead of the Manual sheet silently
// scrolling nowhere. Resolved the same way manual-links.test.ts's own module
// resolves - relative to the frontend package dir, one level up to the repo
// root, then into docs/.
function readDocsFile(pageId: string): string {
  const testDir = dirname(fileURLToPath(import.meta.url))
  const frontendDir = resolve(testDir, '..', '..')
  const docsRoot = resolve(frontendDir, '..', 'docs')
  const relativePath = pageId === 'index' ? 'index.md' : `${pageId}.md`
  return readFileSync(resolve(docsRoot, relativePath), 'utf8')
}

function allManualTargets(): ManualTargetLike[] {
  const targets: ManualTargetLike[] = [DASHBOARD_MANUAL_TARGET, MANUAL_INDEX]
  for (const id of PANEL_IDS) targets.push(PANEL_MANUAL_TARGETS[id])
  for (const section of SETTINGS_SECTIONS) targets.push(SETTINGS_MANUAL_TARGETS[section.id])
  return targets
}

interface ManualTargetLike {
  page: string
  heading?: string
}

describe('the lookup table against the docs on disk', () => {
  it('resolves every mapped page to a file that exists', () => {
    for (const target of allManualTargets()) {
      expect(() => readDocsFile(target.page), `docs page for "${target.page}" is missing`).not.toThrow()
    }
  })

  it('finds every mapped heading as a ## or ### line on its page', () => {
    for (const target of allManualTargets()) {
      if (!target.heading) continue
      const body = readDocsFile(target.page)
      const headingLines = body
        .split('\n')
        .filter((line) => line.startsWith('## ') || line.startsWith('### '))
        .map((line) => line.replace(/^#{2,3}\s+/, '').trim())
      expect(
        headingLines,
        `"${target.heading}" not found as a ##/### heading on ${target.page}.md`,
      ).toContain(target.heading)
    }
  })
})

describe('slugifyHeading', () => {
  it('lowercases and hyphenates a plain heading', () => {
    expect(slugifyHeading('The kiosk feed')).toBe('the-kiosk-feed')
  })

  it('hyphenates every word in a multi-word heading', () => {
    expect(slugifyHeading('Web push over Tailscale')).toBe('web-push-over-tailscale')
  })

  it('strips punctuation but keeps the surrounding spaces, producing a doubled hyphen', () => {
    expect(slugifyHeading('Battery & Power')).toBe('battery--power')
  })

  it('strips a leading numbered-step period', () => {
    expect(slugifyHeading('2. Configure it in Helmcentral')).toBe('2-configure-it-in-helmcentral')
  })
})

describe('resolveManualHref', () => {
  it('resolves a bare #hash to an anchor, regardless of the current page', () => {
    expect(resolveManualHref('#nearby-map', 'features/dashboard')).toEqual({
      kind: 'anchor',
      hash: 'nearby-map',
    })
  })

  it('resolves a same-directory .md link to a page', () => {
    expect(resolveManualHref('alarms.md', 'features/dashboard')).toEqual({
      kind: 'page',
      page: 'features/alarms',
    })
  })

  it('resolves a same-directory .md link with a hash to a page and hash', () => {
    expect(resolveManualHref('dashboard.md#widgets', 'features/forecast')).toEqual({
      kind: 'page',
      page: 'features/dashboard',
      hash: 'widgets',
    })
  })

  it('resolves a ../ link across directories', () => {
    expect(resolveManualHref('../how-to/talk-to-mate.md', 'features/assistant')).toEqual({
      kind: 'page',
      page: 'how-to/talk-to-mate',
    })
  })

  it('resolves a ../ link with a hash across directories', () => {
    expect(resolveManualHref('../reference/configuration.md#web-push-over-tailscale', 'features/alarms')).toEqual({
      kind: 'page',
      page: 'reference/configuration',
      hash: 'web-push-over-tailscale',
    })
  })

  it('resolves a link from the root index page with no directory to strip', () => {
    expect(resolveManualHref('features/dashboard.md', 'index')).toEqual({
      kind: 'page',
      page: 'features/dashboard',
    })
  })

  it('sends a directory link outside the manual tree to the GitHub tree view', () => {
    expect(resolveManualHref('adr/', 'index')).toEqual({
      kind: 'external',
      url: 'https://github.com/gterrill/helmcentral/tree/main/docs/adr',
    })
  })

  it('sends a file link outside the manual tree, with a hash, to the GitHub blob view', () => {
    expect(
      resolveManualHref('../examples/poi-plugins/osm-overpass/README.md#usage', 'reference/plugins'),
    ).toEqual({
      kind: 'external',
      url: 'https://github.com/gterrill/helmcentral/blob/main/docs/examples/poi-plugins/osm-overpass/README.md#usage',
    })
  })

  it('resolves a link that climbs above docs/ entirely, with a hash, to the GitHub blob view', () => {
    expect(resolveManualHref('../../README.md#configuration', 'how-to/upgrading')).toEqual({
      kind: 'external',
      url: 'https://github.com/gterrill/helmcentral/blob/main/README.md#configuration',
    })
  })

  it('resolves a link into a dotfile directory outside docs/ to the GitHub blob view', () => {
    expect(resolveManualHref('../../.github/workflows/release.yml', 'how-to/development')).toEqual({
      kind: 'external',
      url: 'https://github.com/gterrill/helmcentral/blob/main/.github/workflows/release.yml',
    })
  })

  it('treats an absolute http(s) URL as external, unchanged', () => {
    expect(resolveManualHref('https://wazero.io/', 'reference/plugins')).toEqual({
      kind: 'external',
      url: 'https://wazero.io/',
    })
  })

  it('treats a mailto: URL as external, unchanged', () => {
    expect(resolveManualHref('mailto:skipper@example.com', 'features/assistant')).toEqual({
      kind: 'external',
      url: 'mailto:skipper@example.com',
    })
  })
})

describe('manualBodyWithoutTitle', () => {
  it('strips the leading # heading line and the blank line after it', () => {
    expect(manualBodyWithoutTitle('# Mate\n\nBody text.')).toBe('Body text.')
  })

  it('leaves the body alone when it has no leading # heading', () => {
    expect(manualBodyWithoutTitle('Body text with no title.')).toBe('Body text with no title.')
  })

  it('returns an empty string for a page that is only a title', () => {
    expect(manualBodyWithoutTitle('# Mate')).toBe('')
  })
})
