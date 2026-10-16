import type { GaugeDisplay, GaugeZone } from '@/lib/dashboard-widgets'

/**
 * Engine profiles (ADR 0053): drop-in reference data describing an engine's
 * gauges, its advisory operating bands, and its service intervals.
 *
 * These types mirror backend/engine_profiles.go. A tile reads a profile's
 * gauges and zones live through lib/equipment-tiles.ts; nothing copies a
 * profile into a tile any more.
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
  // at all: a battery profile is for the house bank, never for a dashboard tile.
  gauges?: EngineProfileGauge[]
  service?: EngineProfileService[]
  // Battery-only fields (kind: 'battery').
  chemistry?: string
  full_soc?: BatteryProfileThreshold
  charge_warn?: BatteryProfileThreshold
  charge_high?: BatteryProfileThreshold
}

export interface EngineProfileProblem {
  id: string
  error: string
}

export interface EquipmentProfileValidationError {
  path?: string
  message: string
}
