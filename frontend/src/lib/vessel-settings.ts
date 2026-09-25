/**
 * Types for the vessel.* settings block (anomaly detection: sensor health,
 * full-bank charging, engine differentials) and its candidates endpoint.
 * Mirrors backend/vessel_settings.go and backend/vessel_handlers.go
 * field-for-field, snake_case as the API sends it.
 */

export interface VesselEngineSetting {
  instance: string
  name: string
  equipment_id: string
}

export interface VesselHouseBankSetting {
  path: string
  equipment_id: string
  capacity_ah: number
  cells: number
  /** 0 means "use the linked battery profile's own computed threshold". */
  warn_voltage?: number
  high_voltage?: number
}

export interface VesselSettings {
  engines: VesselEngineSetting[]
  house_bank: VesselHouseBankSetting | null
}

export const emptyVesselSettings: VesselSettings = { engines: [], house_bank: null }

export interface VesselEngineCandidate {
  instance: string
  rpm: number | null
  coolant_c: number | null
}

export interface VesselBatteryCandidate {
  path: string
  voltage: number | null
  current: number | null
  soc: number | null
}

export interface VesselDetectorStatus {
  ready: boolean
  missing?: string
}

export interface VesselCandidatesResponse {
  engines: VesselEngineCandidate[]
  batteries: VesselBatteryCandidate[]
  detectors: Record<string, VesselDetectorStatus>
}

export const emptyVesselCandidates: VesselCandidatesResponse = { engines: [], batteries: [], detectors: {} }

/** "propulsion.port" style display, or a raw instance id if that's all there is: "port" -> "Port". */
export function titleCaseInstance(instance: string): string {
  const trimmed = instance.trim()
  if (trimmed === '') return ''
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1)
}
