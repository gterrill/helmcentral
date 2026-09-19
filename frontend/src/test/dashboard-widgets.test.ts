import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { afterEach, describe, expect, test, vi } from 'vitest'

import {
  DASHBOARD_WIDGET_CATEGORY,
  DASHBOARD_WIDGET_DEFAULT_SIZE,
  DASHBOARD_WIDGET_IDS,
  DASHBOARD_WIDGET_LABELS,
  EMBED_WIDGET_ID_PREFIX,
  POI_MAP_RANGE_NM_MAX,
  POI_MAP_RANGE_NM_MIN,
  POI_MAP_WIDGET_ID_PREFIX,
  WIDGET_CATEGORIES,
  duplicateWidget,
  isEmbedWidgetId,
  isGaugeGroupWidgetId,
  isGaugeWidgetId,
  isMultiInstanceWidgetId,
  isPoiMapWidgetId,
  isValidEmbedUrl,
  isValidPoiMapConfig,
  mergeLayoutGeometry,
  newEmbedWidgetId,
  newGaugeGroupWidgetId,
  newPoiMapWidgetId,
  rewriteGaugePaths,
  widgetDisplayName,
  type DashboardLayoutItem,
  type PoiMapWidgetConfig,
} from '@/lib/dashboard-widgets'
import { WIDGET_CONSTRAINTS } from '@/components/dashboard-bento-grid'

describe('isEmbedWidgetId', () => {
  test('recognises embed instance ids', () => {
    expect(isEmbedWidgetId('embed:m1x8abcd')).toBe(true)
  })

  test('rejects every builtin widget id', () => {
    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(isEmbedWidgetId(id)).toBe(false)
    }
  })

  test('rejects an id that merely contains the prefix', () => {
    expect(isEmbedWidgetId('not-an-embed:m1x8abcd')).toBe(false)
  })
})

describe('isValidEmbedUrl', () => {
  test.each([
    'http://boat.local:3000/d-solo/abc/windrose?panelId=2',
    'https://grafana.example.com/d-solo/abc?panelId=2&kiosk&theme=dark',
    'http://192.168.1.20:3000/',
  ])('accepts %s', (url) => {
    expect(isValidEmbedUrl(url)).toBe(true)
  })

  // Mirrors the Go-side rejections in validateEmbedWidget.
  test.each([
    ['blank', ''],
    ['whitespace only', '   '],
    ['javascript scheme', 'javascript:alert(1)'],
    ['data scheme', 'data:text/html,<script>alert(1)</script>'],
    ['file scheme', 'file:///etc/passwd'],
    ['scheme-relative', '//grafana.local/d-solo/a'],
    ['bare host', 'grafana.local/d-solo/a'],
    ['http with no host', 'http:///d-solo/a'],
  ])('rejects %s', (_label, url) => {
    expect(isValidEmbedUrl(url)).toBe(false)
  })

  test('tolerates surrounding whitespace so a pasted URL still validates', () => {
    expect(isValidEmbedUrl('  https://grafana.local/d-solo/a  ')).toBe(true)
  })

  test('rejects a URL past the length the backend accepts', () => {
    expect(isValidEmbedUrl(`https://grafana.local/?q=${'x'.repeat(2048)}`)).toBe(false)
  })
})

/**
 * F-1 (security audit, phase 1): an embed URL whose origin is this app's own
 * used to pass every check here, which mattered because embed-tile.tsx's
 * iframe sandbox grants allow-same-origin — fine for a genuinely third-party
 * embed keeping its own session, but for a same-origin frame that grant
 * instead un-sandboxes it against OUR window (window.top.document, app
 * state, same-origin fetches carrying the SignalK session cookie). See the
 * updated comment on isValidEmbedUrl and on the sandbox attribute in
 * embed-tile.tsx.
 */
describe('isValidEmbedUrl same-origin rejection (F-1)', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  test('rejects a URL whose origin is this app\'s own, even though it passes every other check', () => {
    vi.stubGlobal('location', { ...window.location, origin: 'http://192.168.50.240:9091' })
    expect(isValidEmbedUrl('http://192.168.50.240:9091/anything')).toBe(false)
  })

  test('still accepts a different host reachable on the same LAN', () => {
    vi.stubGlobal('location', { ...window.location, origin: 'http://192.168.50.240:9091' })
    expect(isValidEmbedUrl('http://192.168.50.240:3030/d-solo/abc')).toBe(true)
  })

  test('still accepts the same host on a different port -- a distinct origin', () => {
    vi.stubGlobal('location', { ...window.location, origin: 'http://192.168.50.240:9091' })
    expect(isValidEmbedUrl('http://192.168.50.240:3000/')).toBe(true)
  })

  test('same host and port but a different scheme is still a distinct origin', () => {
    vi.stubGlobal('location', { ...window.location, origin: 'https://192.168.50.240:9091' })
    expect(isValidEmbedUrl('http://192.168.50.240:9091/anything')).toBe(true)
  })
})

describe('newEmbedWidgetId', () => {
  test('mints an id the backend token pattern accepts', () => {
    const id = newEmbedWidgetId([])
    expect(id.startsWith(EMBED_WIDGET_ID_PREFIX)).toBe(true)
    expect(id.slice(EMBED_WIDGET_ID_PREFIX.length)).toMatch(/^[A-Za-z0-9_-]{8,64}$/)
  })

  test('does not collide with an id already on the page', () => {
    const existing: DashboardLayoutItem[] = []
    const seen = new Set<string>()
    for (let i = 0; i < 200; i += 1) {
      const id = newEmbedWidgetId(existing)
      expect(seen.has(id)).toBe(false)
      seen.add(id)
      existing.push({ id, x: 0, y: 0, w: 6, h: 8, embed: { title: '', url: '' } })
    }
  })
})

describe('widgetDisplayName', () => {
  test('uses the catalog label for a builtin widget', () => {
    expect(widgetDisplayName({ id: 'wind', x: 0, y: 0, w: 4, h: 8 }))
      .toBe(DASHBOARD_WIDGET_LABELS.wind)
  })

  test('uses the configured title for an embed', () => {
    expect(widgetDisplayName({
      id: 'embed:m1x8abcd', x: 0, y: 0, w: 6, h: 8,
      embed: { title: 'Windrose', url: 'https://grafana.local/a' },
    })).toBe('Windrose')
  })

  test('falls back to a generic name for an untitled embed', () => {
    expect(widgetDisplayName({
      id: 'embed:m1x8abcd', x: 0, y: 0, w: 6, h: 8,
      embed: { title: '   ', url: 'https://grafana.local/a' },
    })).toBe('Embed')
  })

  test('uses the configured title for a poi map, falling back to "Nearby"', () => {
    expect(widgetDisplayName({
      id: 'poi-map:m1x8abcd', x: 0, y: 0, w: 12, h: 7,
      poiMap: { title: 'Anchorages', rangeNm: 5, categories: ['anchorage'], layout: 'map' },
    })).toBe('Anchorages')
    expect(widgetDisplayName({
      id: 'poi-map:m1x8abcd', x: 0, y: 0, w: 12, h: 7,
      poiMap: { title: '  ', rangeNm: 5, categories: ['anchorage'], layout: 'map' },
    })).toBe('Nearby')
  })
})

describe('mergeLayoutGeometry', () => {
  // REGRESSION TEST for the embed-drag silent-save-failure bug: rebuilding
  // widgets from RGL's LayoutItem alone drops `embed`, which the backend
  // then rejects, so no drag/resize on the page persists. This must keep
  // `embed` intact while still picking up the new geometry.
  test('preserves an embed widget config while applying new geometry', () => {
    const widgets: DashboardLayoutItem[] = [
      { id: 'embed:m1x8abcd', x: 0, y: 0, w: 6, h: 8, embed: { title: 'Windrose', url: 'https://grafana.local/a' } },
    ]
    const geometry = [{ i: 'embed:m1x8abcd', x: 2, y: 4, w: 5, h: 7 }]

    const result = mergeLayoutGeometry(widgets, geometry)

    expect(result).toEqual([
      { id: 'embed:m1x8abcd', x: 2, y: 4, w: 5, h: 7, embed: { title: 'Windrose', url: 'https://grafana.local/a' } },
    ])
  })

  test('applies new geometry to a builtin widget', () => {
    const widgets: DashboardLayoutItem[] = [{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }]
    const geometry = [{ i: 'wind', x: 8, y: 3, w: 4, h: 6 }]

    const result = mergeLayoutGeometry(widgets, geometry)

    expect(result).toEqual([{ id: 'wind', x: 8, y: 3, w: 4, h: 6 }])
  })

  test('leaves a widget untouched when geometry has no entry for its id', () => {
    const widgets: DashboardLayoutItem[] = [{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }]

    const result = mergeLayoutGeometry(widgets, [])

    expect(result).toEqual(widgets)
  })

  test('preserves the order of widgets, not the order of geometry', () => {
    const widgets: DashboardLayoutItem[] = [
      { id: 'wind', x: 0, y: 0, w: 4, h: 6 },
      { id: 'solar', x: 4, y: 0, w: 4, h: 6 },
    ]
    // Geometry supplied in the opposite order.
    const geometry = [
      { i: 'solar', x: 4, y: 2, w: 4, h: 6 },
      { i: 'wind', x: 0, y: 2, w: 4, h: 6 },
    ]

    const result = mergeLayoutGeometry(widgets, geometry)

    expect(result.map((w) => w.id)).toEqual(['wind', 'solar'])
  })

  test('does not invent widgets from geometry entries that match no widget', () => {
    const widgets: DashboardLayoutItem[] = [{ id: 'wind', x: 0, y: 0, w: 4, h: 6 }]
    const geometry = [
      { i: 'wind', x: 1, y: 1, w: 4, h: 6 },
      { i: 'solar', x: 4, y: 0, w: 4, h: 6 },
    ]

    const result = mergeLayoutGeometry(widgets, geometry)

    expect(result).toEqual([{ id: 'wind', x: 1, y: 1, w: 4, h: 6 }])
  })
})

describe('gauge group ids (ADR 0049)', () => {
  test('a gauge group id is not mistaken for a gauge id', () => {
    expect(isGaugeGroupWidgetId('gauge-group:m1x8abcd')).toBe(true)
    expect(isGaugeWidgetId('gauge-group:m1x8abcd')).toBe(false)
    expect(isGaugeGroupWidgetId('gauge:m1x8abcd')).toBe(false)
  })

  test('rejects every builtin widget id', () => {
    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(isGaugeGroupWidgetId(id)).toBe(false)
    }
  })

  test('mints ids unique within the page', () => {
    const existing: DashboardLayoutItem[] = []
    for (let i = 0; i < 20; i += 1) {
      existing.push({ id: newGaugeGroupWidgetId(existing), x: 0, y: 0, w: 6, h: 8 })
    }
    expect(new Set(existing.map((w) => w.id)).size).toBe(20)
  })

  test('names a group by its title, falling back to a structural label', () => {
    const widget: DashboardLayoutItem = {
      id: 'gauge-group:m1x8abcd', x: 0, y: 0, w: 6, h: 8,
      gaugeGroup: { title: 'Port', gauges: [] },
    }
    expect(widgetDisplayName(widget)).toBe('Port')
    expect(widgetDisplayName({ ...widget, gaugeGroup: { title: '  ', gauges: [] } })).toBe('Gauges')
  })
})

describe('duplicateWidget', () => {
  const group: DashboardLayoutItem = {
    id: 'gauge-group:m1x8abcd', x: 2, y: 4, w: 6, h: 8,
    gaugeGroup: {
      title: 'Port',
      gauges: [
        { path: 'propulsion.port.revolutions', label: 'RPM', display: 'radial', quantity: 'frequency', unit: 'rpm', zones: [{ from: 3000, to: 4000, state: 'alarm' }] },
      ],
    },
  }

  test('mints a fresh id and keeps the config', () => {
    const copy = duplicateWidget(group, [group])
    expect(copy).not.toBeNull()
    expect(copy!.id).not.toBe(group.id)
    expect(isGaugeGroupWidgetId(copy!.id)).toBe(true)
    expect(copy!.gaugeGroup?.title).toBe('Port')
    expect(copy!.gaugeGroup?.gauges).toHaveLength(1)
  })

  test('deep-copies, so editing the copy never edits the original', () => {
    const copy = duplicateWidget(group, [group])!
    copy.gaugeGroup!.gauges[0].path = 'propulsion.starboard.revolutions'
    copy.gaugeGroup!.gauges[0].zones![0].state = 'warn'
    copy.gaugeGroup!.gauges.push({ path: 'a.b', label: '', display: 'numeric', quantity: 'raw', unit: 'raw' })

    expect(group.gaugeGroup!.gauges).toHaveLength(1)
    expect(group.gaugeGroup!.gauges[0].path).toBe('propulsion.port.revolutions')
    expect(group.gaugeGroup!.gauges[0].zones![0].state).toBe('alarm')
  })

  test('duplicates gauges and embeds too', () => {
    const gauge: DashboardLayoutItem = {
      id: 'gauge:m1x8abcd', x: 0, y: 0, w: 3, h: 6,
      gauge: { path: 'a.b', label: 'A', display: 'numeric', quantity: 'raw', unit: 'raw' },
    }
    const embed: DashboardLayoutItem = {
      id: 'embed:m1x8abcd', x: 0, y: 0, w: 6, h: 8,
      embed: { title: 'Grafana', url: 'https://grafana.local/a' },
    }
    expect(duplicateWidget(gauge, [gauge])?.gauge?.path).toBe('a.b')
    expect(duplicateWidget(embed, [embed])?.embed?.url).toBe('https://grafana.local/a')
  })

  test('duplicates a poi map, deep-copying its categories array', () => {
    const poiMap: DashboardLayoutItem = {
      id: 'poi-map:m1x8abcd', x: 0, y: 0, w: 12, h: 7,
      poiMap: { title: 'Nearby', rangeNm: 5, categories: ['anchorage', 'fuel'], layout: 'split' },
    }
    const copy = duplicateWidget(poiMap, [poiMap])
    expect(copy).not.toBeNull()
    expect(isPoiMapWidgetId(copy!.id)).toBe(true)
    expect(copy!.id).not.toBe(poiMap.id)
    expect(copy!.poiMap?.categories).toEqual(['anchorage', 'fuel'])

    copy!.poiMap!.categories.push('marina')
    expect(poiMap.poiMap!.categories).toEqual(['anchorage', 'fuel'])
  })

  test('refuses a builtin, which is one-per-page', () => {
    expect(duplicateWidget({ id: 'wind', x: 0, y: 0, w: 4, h: 8 }, [])).toBeNull()
  })
})

describe('rewriteGaugePaths', () => {
  const gauges = [
    { path: 'propulsion.port.revolutions', label: 'Port RPM', display: 'radial' as const, quantity: 'frequency', unit: 'rpm' },
    { path: 'propulsion.port.oilPressure', label: 'Port oil', display: 'bar' as const, quantity: 'pressure', unit: 'psi' },
    { path: 'environment.depth.belowTransducer', label: 'Depth', display: 'numeric' as const, quantity: 'length', unit: 'ft' },
  ]

  test('rewrites matching paths and leaves labels alone', () => {
    const next = rewriteGaugePaths(gauges, 'port', 'starboard')
    expect(next[0].path).toBe('propulsion.starboard.revolutions')
    expect(next[1].path).toBe('propulsion.starboard.oilPressure')
    expect(next[2].path).toBe('environment.depth.belowTransducer')
    expect(next[0].label).toBe('Port RPM')
  })

  test('replaces every occurrence, not just the first', () => {
    const next = rewriteGaugePaths([{ ...gauges[0], path: 'a.port.b.port' }], 'port', 'stbd')
    expect(next[0].path).toBe('a.stbd.b.stbd')
  })

  test('is a no-op when the search text is blank or absent', () => {
    expect(rewriteGaugePaths(gauges, '', 'starboard')).toEqual(gauges)
    expect(rewriteGaugePaths(gauges, 'nothing-matches', 'x')).toEqual(gauges)
  })

  test('does not mutate the input', () => {
    rewriteGaugePaths(gauges, 'port', 'starboard')
    expect(gauges[0].path).toBe('propulsion.port.revolutions')
  })
})

/**
 * Go's JSON decoder drops any field/id it doesn't recognise, silently: a
 * widget id present in the frontend's picker but missing from the backend's
 * validDashboardWidgetIDs map is a 400 an operator only discovers by trying
 * to save the page. Reads the backend source directly off disk (the same
 * technique forecast-drawer.test.tsx's colour-token guards use) so the two
 * lists can never drift apart without failing the suite immediately.
 */
describe('DASHBOARD_WIDGET_IDS backend parity', () => {
  test('matches backend/dashboard_pages.go validDashboardWidgetIDs exactly', () => {
    const testDir = dirname(fileURLToPath(import.meta.url))
    const source = readFileSync(resolve(testDir, '../../../backend/dashboard_pages.go'), 'utf8')

    const start = source.indexOf('var validDashboardWidgetIDs = map[string]bool{')
    expect(start, 'validDashboardWidgetIDs map not found in backend/dashboard_pages.go').toBeGreaterThan(-1)
    const end = source.indexOf('}', start)
    const block = source.slice(start, end)

    const backendIds = [...block.matchAll(/"([a-z0-9-]+)":\s*true/g)].map((m) => m[1])

    expect(backendIds.length, 'no widget ids parsed out of validDashboardWidgetIDs - regex or map shape changed').toBeGreaterThan(0)
    expect(new Set(backendIds)).toEqual(new Set(DASHBOARD_WIDGET_IDS))
  })
})

/**
 * ADR 0107: every built-in widget gets a category (for the grouped Add
 * Widget picker) and a fixed default footprint sized to its own content,
 * never below the constraints the grid itself enforces — that floor is what
 * used to let Battery & Power land cut off at the old hard-coded 4x6 default.
 */
describe('DASHBOARD_WIDGET_CATEGORY', () => {
  test('every built-in widget has a category', () => {
    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(DASHBOARD_WIDGET_CATEGORY[id], `no category for "${id}"`).toBeDefined()
    }
  })

  test('every category used is one of the ordered WIDGET_CATEGORIES', () => {
    const known = new Set(WIDGET_CATEGORIES.map((c) => c.id))
    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(known.has(DASHBOARD_WIDGET_CATEGORY[id]), `"${DASHBOARD_WIDGET_CATEGORY[id]}" is not in WIDGET_CATEGORIES`).toBe(true)
    }
  })
})

describe('DASHBOARD_WIDGET_DEFAULT_SIZE', () => {
  test('every built-in widget has a default size', () => {
    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(DASHBOARD_WIDGET_DEFAULT_SIZE[id], `no default size for "${id}"`).toBeDefined()
    }
  })

  test('the default is never below the widget\'s own grid constraints', () => {
    for (const id of DASHBOARD_WIDGET_IDS) {
      const size = DASHBOARD_WIDGET_DEFAULT_SIZE[id]
      const constraints = WIDGET_CONSTRAINTS[id]
      if (constraints?.minW !== undefined) {
        expect(size.w, `${id} default w (${size.w}) is below its minW (${constraints.minW})`).toBeGreaterThanOrEqual(constraints.minW)
      }
      if (constraints?.minH !== undefined) {
        expect(size.h, `${id} default h (${size.h}) is below its minH (${constraints.minH})`).toBeGreaterThanOrEqual(constraints.minH)
      }
    }
  })

  // Regression test for the bug this plan fixes: every built-in widget used
  // to land at a hard-coded 4x6, which cut Battery & Power's content off.
  test('Battery & Power is clearly taller than the old hard-coded default of 6', () => {
    expect(DASHBOARD_WIDGET_DEFAULT_SIZE['battery-power'].h).toBeGreaterThan(6)
  })
})

describe('poi map ids (ADR 0091 phase 3b)', () => {
  test('recognises poi map instance ids', () => {
    expect(isPoiMapWidgetId('poi-map:m1x8abcd')).toBe(true)
  })

  test('rejects every builtin widget id', () => {
    for (const id of DASHBOARD_WIDGET_IDS) {
      expect(isPoiMapWidgetId(id)).toBe(false)
    }
  })

  test('is not mistaken for any other multi-instance kind', () => {
    expect(isPoiMapWidgetId('gauge:m1x8abcd')).toBe(false)
    expect(isPoiMapWidgetId('embed:m1x8abcd')).toBe(false)
    expect(isGaugeWidgetId('poi-map:m1x8abcd')).toBe(false)
  })

  test('mints an id the backend token pattern accepts and is unique within the page', () => {
    const existing: DashboardLayoutItem[] = []
    const seen = new Set<string>()
    for (let i = 0; i < 50; i += 1) {
      const id = newPoiMapWidgetId(existing)
      expect(id.startsWith(POI_MAP_WIDGET_ID_PREFIX)).toBe(true)
      expect(id.slice(POI_MAP_WIDGET_ID_PREFIX.length)).toMatch(/^[A-Za-z0-9_-]{8,64}$/)
      expect(seen.has(id)).toBe(false)
      seen.add(id)
      existing.push({ id, x: 0, y: 0, w: 12, h: 7 })
    }
  })

  test('counts as a multi-instance widget', () => {
    expect(isMultiInstanceWidgetId('poi-map:m1x8abcd')).toBe(true)
    expect(isMultiInstanceWidgetId('wind')).toBe(false)
  })
})

function validPoiMapConfig(): PoiMapWidgetConfig {
  return { title: 'Nearby', rangeNm: 5, categories: ['anchorage', 'marina', 'fuel'], layout: 'split' }
}

/**
 * Mirrors validatePoiMapWidget in backend/dashboard_pages.go, the same
 * relationship isValidEmbedUrl has to validateEmbedWidget: duplicated rather
 * than shared because the config dialog needs synchronous feedback while the
 * server must not trust the client.
 */
describe('isValidPoiMapConfig', () => {
  test('accepts a well-formed config', () => {
    expect(isValidPoiMapConfig(validPoiMapConfig())).toBe(true)
  })

  test.each([
    ['title too long', { ...validPoiMapConfig(), title: 't'.repeat(49) }],
    ['no categories', { ...validPoiMapConfig(), categories: [] }],
    ['unknown category', { ...validPoiMapConfig(), categories: ['anchorage', 'moon-base'] }],
    ['range below minimum', { ...validPoiMapConfig(), rangeNm: POI_MAP_RANGE_NM_MIN - 0.1 }],
    ['range above maximum', { ...validPoiMapConfig(), rangeNm: POI_MAP_RANGE_NM_MAX + 0.1 }],
    ['unknown layout', { ...validPoiMapConfig(), layout: 'carousel' as PoiMapWidgetConfig['layout'] }],
  ])('rejects %s', (_label, config) => {
    expect(isValidPoiMapConfig(config)).toBe(false)
  })

  test('accepts the range bounds themselves', () => {
    expect(isValidPoiMapConfig({ ...validPoiMapConfig(), rangeNm: POI_MAP_RANGE_NM_MIN })).toBe(true)
    expect(isValidPoiMapConfig({ ...validPoiMapConfig(), rangeNm: POI_MAP_RANGE_NM_MAX })).toBe(true)
  })
})
