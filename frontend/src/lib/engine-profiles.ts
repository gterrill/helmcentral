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
  hero?: boolean
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

/** A battery profile's per-cell/pack number (full_soc, charge_warn, charge_high). A null value is a slot the manufacturer's datasheet doesn't give - same idea as EngineProfileZone's null threshold. */
export interface BatteryProfileThreshold {
  value: number | null
  source?: string
  note?: string
}

export interface EngineProfile {
  schema_version?: number
  kind?: 'engine' | 'alternator' | 'generator' | 'battery'
  id: string
  name: string
  manufacturer?: string
  model?: string
  rating_hp?: number
  source?: string
  notes?: string
  // Absent for a battery profile (kind: 'battery'), which carries no gauges
  // at all - every gauge-tile consumer of this type (EngineProfileDialog,
  // EngineClusterConfigDialog) filters those out of its own profile picker
  // before this field is ever read, per AGENTS.md: a battery profile is
  // for Settings -> Vessel -> Power, never for a dashboard tile.
  gauges?: EngineProfileGauge[]
  service?: EngineProfileService[]
  // Battery-only fields (kind: 'battery').
  chemistry?: string
  full_soc?: BatteryProfileThreshold
  charge_warn?: BatteryProfileThreshold
  charge_high?: BatteryProfileThreshold
}

export interface EngineProfileProblem {
  file: string
  error: string
}

export interface EquipmentProfileValidationError {
  path?: string
  message: string
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
  return (profile.gauges ?? []).map((gauge) => ({
    path: joinPath(instancePrefix, gauge.path_suffix),
    label: gauge.label,
    ...gaugeSettingsFor(gauge),
  }))
}

/** The profile member marked as hero, if any. */
export function profileHeroGaugeIndex(profile: EngineProfile): number | undefined {
  const index = (profile.gauges ?? []).findIndex((gauge) => gauge.hero === true)
  return index === -1 ? undefined : index
}

/** The first gauge whose path matches the given profile suffix, if any. */
export function gaugeIndexByProfileSuffix(
  gauges: readonly GaugeWidgetConfig[],
  suffix: string,
): number | undefined {
  const index = gauges.findIndex((gauge) => matchBySuffix(gauge.path, [suffix]) === suffix)
  return index === -1 ? undefined : index
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
  const profileGauges = profile.gauges ?? []
  return gauges.map((gauge) => {
    const suffix = matchBySuffix(gauge.path, profileGauges.map((g) => g.path_suffix))
    const match = suffix === null ? undefined : profileGauges.find((c) => c.path_suffix === suffix)
    if (!match) return gauge
    return { ...gauge, ...gaugeSettingsFor(match) }
  })
}

/**
 * The declared suffix `path` ends with, longest first.
 *
 * Splitting on the last dotted segment is not good enough: this vessel's only
 * oil temperature is `transmission.oilTemperature`, and a last-segment match
 * would happily bind it to a bare `oilTemperature` gauge — the wrong sensor,
 * silently.
 */
function matchBySuffix(path: string, suffixes: readonly string[]): string | null {
  for (const suffix of [...suffixes].sort((a, b) => b.length - a.length)) {
    if (path === suffix || path.endsWith(`.${suffix}`)) return suffix
  }
  return null
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
  /**
   * The suffixes the profile declared, positionally matching `incoming`.
   *
   * Required rather than derived: nothing in a composed path says where the
   * instance ends and the suffix begins, so guessing the last segment binds
   * the gearbox sensor to a bare `oilTemperature` gauge without complaining.
   */
  suffixes: readonly string[],
): MergedGauges {
  const bySuffix = new Map<string, GaugeWidgetConfig>()
  incoming.forEach((gauge, index) => bySuffix.set(suffixes[index] ?? '', gauge))

  const covered = new Set<string>()
  let updated = 0

  const merged = existing.map((gauge) => {
    const suffix = matchBySuffix(gauge.path, suffixes)
    const match = suffix === null ? undefined : bySuffix.get(suffix)
    if (!match) return gauge
    covered.add(suffix!)
    updated += 1
    return { ...gauge, ...settingsOnly(match) }
  })

  const additions = incoming.filter((_, index) => !covered.has(suffixes[index] ?? ''))
  return { gauges: [...merged, ...additions.map((gauge) => ({ ...gauge }))], updated, added: additions.length }
}

/**
 * The instance prefix a tile's gauges share, or null if they do not agree.
 *
 * Suffix-aware, because it has to be: stripping the last dotted segment turns
 * `propulsion.port.transmission.oilTemperature` into
 * `propulsion.port.transmission`, which disagrees with every other slot and
 * makes the whole thing give up and fall back to a guess.
 */
export function commonInstancePrefix(
  gauges: readonly GaugeWidgetConfig[],
  suffixes: readonly string[],
): string | null {
  const prefixes = new Set<string>()

  for (const gauge of gauges) {
    const path = gauge.path.trim()
    if (path === '') continue
    const suffix = matchBySuffix(path, suffixes)
    if (suffix === null || path.length <= suffix.length) return null
    prefixes.add(path.slice(0, path.length - suffix.length - 1))
  }

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
  return (profile.gauges ?? []).reduce(
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
  const suffixes = (profile.gauges ?? []).map((g) => g.path_suffix)
  const hits = new Map<string, number>()

  for (const { path } of paths) {
    const suffix = matchBySuffix(path, suffixes)
    if (suffix === null || path.length <= suffix.length) continue
    const prefix = path.slice(0, path.length - suffix.length - 1)
    hits.set(prefix, (hits.get(prefix) ?? 0) + 1)
  }

  // Ranked by how much of the profile each instance actually satisfies, not
  // alphabetically. `temperature` on its own is published by alternators,
  // batteries, chargers and the outside air, so sorting by name handed the
  // engine slot to whichever sorted first — an alternator, on this vessel.
  return [...hits.entries()]
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([prefix]) => prefix)
}
