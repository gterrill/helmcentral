import { describe, expect, test } from 'vitest'

import {
  DASHBOARD_WIDGET_IDS,
  DASHBOARD_WIDGET_LABELS,
  EMBED_WIDGET_ID_PREFIX,
  isEmbedWidgetId,
  isValidEmbedUrl,
  newEmbedWidgetId,
  widgetDisplayName,
  type DashboardLayoutItem,
} from '@/lib/dashboard-widgets'

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
})
