import type { GaugeDisplay, GaugeWidgetConfig, GaugeZone } from '@/lib/dashboard-widgets'

/**
 * Engine profiles (ADR 0053): drop-in reference data describing an engine's
 * gauges, its advisory operating bands, and its service intervals.
 *
 * These types mirror backend/engine_profiles.go. The conversion below is the
 * only place a profile becomes dashboard config, and it is pure so it can be
 * tested directly rather than through the dialog.
 */

export interface EngineProfileZone {
  direction: 'below' | 'above'
  /** null is a slot: the manufacturer defines a threshold, but it is not public. */
  threshold: number | null
  state: GaugeZone['state']
  source?: string
  note?: string
}

export interface EngineProfileGauge {
  path_suffix: string
  label: string
  display: GaugeDisplay
  quantity: string
  unit: string
  min?: number
  max?: number
  decimals?: number
  zones?: EngineProfileZone[]
}

export interface EngineProfileService {
  id: string
  description?: string
  interval_hours?: number | null
  interval_months?: number | null
  first_at_hours?: number | null
  supersedes?: string[]
  source?: string | null
}

export interface EngineProfile {
  id: string
  name: string
  manufacturer?: string
  model?: string
  rating_hp?: number
  source?: string
  notes?: string
  gauges: EngineProfileGauge[]
  service?: EngineProfileService[]
}

export interface EngineProfileProblem {
  file: string
  error: string
}

/**
 * Turns a profile zone into the stored `{from, to}` shape, anchored against the
 * gauge's own scale — the same model the zone editor uses, and the reason a
 * profile cannot describe a mid-range band the backend would reject.
 *
 * Returns null for a slot, which must never become a zone at some default
 * number nobody chose.
 */
function toStoredZone(zone: EngineProfileZone, min: number, max: number): GaugeZone | null {
  if (zone.threshold === null || zone.threshold === undefined) return null
  return zone.direction === 'below'
    ? { from: min, to: zone.threshold, state: zone.state }
    : { from: zone.threshold, to: max, state: zone.state }
}

function storedZonesFor(gauge: EngineProfileGauge): GaugeZone[] | undefined {
  const min = gauge.min ?? 0
  const max = gauge.max ?? 100
  const zones = (gauge.zones ?? [])
    .map((zone) => toStoredZone(zone, min, max))
    .filter((zone): zone is GaugeZone => zone !== null)
  return zones.length > 0 ? zones : undefined
}

/** Config common to both apply modes, minus the path. */
function gaugeSettingsFor(gauge: EngineProfileGauge): Omit<GaugeWidgetConfig, 'path' | 'label'> {
  return {
    display: gauge.display,
    quantity: gauge.quantity,
    unit: gauge.unit,
    ...(gauge.decimals !== undefined ? { decimals: gauge.decimals } : {}),
    ...(gauge.min !== undefined ? { min: gauge.min } : {}),
    ...(gauge.max !== undefined ? { max: gauge.max } : {}),
    ...(storedZonesFor(gauge) ? { zones: storedZonesFor(gauge) } : {}),
  }
}

function joinPath(prefix: string, suffix: string): string {
  return `${prefix.trim().replace(/\.+$/, '')}.${suffix}`
}

/** Builds a full gauge set for one engine instance, e.g. `propulsion.port`. */
export function profileToGauges(profile: EngineProfile, instancePrefix: string): GaugeWidgetConfig[] {
  return profile.gauges.map((gauge) => ({
    path: joinPath(instancePrefix, gauge.path_suffix),
    label: gauge.label,
    ...gaugeSettingsFor(gauge),
  }))
}

/**
 * Applies a profile to gauges that already exist, matching on path suffix.
 *
 * Path and label are the operator's own choices and are left alone; a profile
 * supplies the reference numbers, not the naming.
 */
export function applyProfileToGauges(
  gauges: readonly GaugeWidgetConfig[],
  profile: EngineProfile,
): GaugeWidgetConfig[] {
  return gauges.map((gauge) => {
    const match = profile.gauges.find((candidate) => gauge.path.endsWith(`.${candidate.path_suffix}`))
    if (!match) return gauge
    return { ...gauge, ...gaugeSettingsFor(match) }
  })
}

function suffixOf(path: string): string {
  return path.split('.').slice(-1)[0]
}

/** Everything about a gauge except the operator's own path and label. */
function settingsOnly(gauge: GaugeWidgetConfig): Omit<GaugeWidgetConfig, 'path' | 'label'> {
  const { path, label, ...settings } = gauge
  void path
  void label
  return settings
}

export interface MergedGauges {
  gauges: GaugeWidgetConfig[]
  /** Gauges the tile already had, whose settings the profile filled in. */
  updated: number
  /** Gauges the profile has that the tile was missing. */
  added: number
}

/**
 * Applies a profile's gauges to a tile that already exists, matching on path
 * suffix. Existing members keep their path and label and take the profile's
 * settings; members the profile has and the tile does not are **appended**.
 *
 * Appending matters: applying a profile to a half-built tile should finish it.
 * Only updating what is already there leaves the gauges you did not think to
 * add still missing, which is the opposite of what a profile is for.
 */
export function mergeGaugeSettingsBySuffix(
  existing: readonly GaugeWidgetConfig[],
  incoming: readonly GaugeWidgetConfig[],
): MergedGauges {
  const bySuffix = new Map(incoming.map((gauge) => [suffixOf(gauge.path), gauge]))
  const covered = new Set(existing.map((gauge) => suffixOf(gauge.path)))

  let updated = 0
  const merged = existing.map((gauge) => {
    const match = bySuffix.get(suffixOf(gauge.path))
    if (!match) return gauge
    updated += 1
    return { ...gauge, ...settingsOnly(match) }
  })

  const additions = incoming.filter((gauge) => !covered.has(suffixOf(gauge.path)))
  return { gauges: [...merged, ...additions.map((gauge) => ({ ...gauge }))], updated, added: additions.length }
}

/**
 * The instance prefix a tile's gauges share, or null if they do not agree.
 *
 * When applying a profile to an existing tile this is where the prefix comes
 * from — not from whatever the server happens to publish first, which would
 * append starboard gauges to the Port tile.
 */
export function commonInstancePrefix(gauges: readonly GaugeWidgetConfig[]): string | null {
  const prefixes = new Set(
    gauges
      .map((gauge) => gauge.path.trim())
      .filter((path) => path.includes('.'))
      .map((path) => path.slice(0, path.lastIndexOf('.'))),
  )
  return prefixes.size === 1 ? [...prefixes][0] : null
}

/**
 * How many of a profile's zones will actually raise an alarm once applied.
 *
 * The apply dialog shows this before anything is saved. A bundled profile
 * reads zero, because published data gives advisory ranges rather than factory
 * setpoints and nothing shipped should alarm until an operator says so.
 */
export function alarmZoneCount(profile: EngineProfile): number {
  return profile.gauges.reduce(
    (total, gauge) =>
      total + (gauge.zones ?? []).filter((z) => z.state !== 'normal' && z.threshold !== null && z.threshold !== undefined).length,
    0,
  )
}

/**
 * Instance prefixes the server is currently publishing for this profile.
 *
 * Only ever a suggestion list: the snapshot holds paths seen since the stream
 * connected, so with the engines off `propulsion.*` is absent — which is
 * exactly when someone sets engine gauges up (ADR 0039 §5).
 */
export function instancePrefixCandidates(
  profile: EngineProfile,
  paths: readonly { path: string }[],
): string[] {
  const suffixes = profile.gauges.map((g) => g.path_suffix)
  const found = new Set<string>()

  for (const { path } of paths) {
    for (const suffix of suffixes) {
      if (path.endsWith(`.${suffix}`)) {
        found.add(path.slice(0, path.length - suffix.length - 1))
      }
    }
  }
  return [...found].sort()
}
