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
  'solar',
  'alternator',
  'generator',
  'czone-switches',
  'hot-water',
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
  'solar': 'Solar',
  'alternator': 'Alternator',
  'generator': 'Generator',
  'czone-switches': 'Switches',
  'hot-water': 'Hot Water',
}

export interface DashboardLayoutItem {
  id: DashboardWidgetId
  x: number
  y: number
  w: number
  h: number
}
