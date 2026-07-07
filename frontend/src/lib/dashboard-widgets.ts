export const DASHBOARD_WIDGET_IDS = [
  'vessel',
  'wind',
  'depth-tide',
  'position',
  'today-now',
  'anchor-watch',
  'rode-scope',
  'tanks',
  'route',
  'nearby-vessels',
  'battery-power',
  'alternator',
  'generator',
  'czone-switches',
] as const

export type DashboardWidgetId = typeof DASHBOARD_WIDGET_IDS[number]

export const DASHBOARD_WIDGET_LABELS: Record<DashboardWidgetId, string> = {
  'vessel': 'Vessel',
  'wind': 'Apparent Wind',
  'depth-tide': 'Depth & Tide',
  'position': 'Position',
  'today-now': 'Today & Now',
  'anchor-watch': 'Anchor Watch',
  'rode-scope': 'Rode & Scope',
  'tanks': 'Tanks',
  'route': 'Route',
  'nearby-vessels': 'Nearby Vessels',
  'battery-power': 'Battery & Power',
  'alternator': 'Alternator',
  'generator': 'Generator',
  'czone-switches': 'Switches',
}

export interface DashboardLayoutItem {
  id: DashboardWidgetId
  x: number
  y: number
  w: number
  h: number
}
