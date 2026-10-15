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

/** The operator's own name for one electrical.batteries.<id> instance. */
export interface VesselBatterySetting {
  instance: string
  name: string
}

/** Which electrical.alternator.<id> instances are port and starboard. '' is not set. */
export interface VesselAlternatorSettings {
  port_instance: string
  starboard_instance: string
}

export interface VesselSettings {
  engines: VesselEngineSetting[]
  house_bank: VesselHouseBankSetting | null
  batteries: VesselBatterySetting[]
  alternators: VesselAlternatorSettings
}

export const emptyVesselSettings: VesselSettings = {
  engines: [],
  house_bank: null,
  batteries: [],
  alternators: { port_instance: '', starboard_instance: '' },
}

export interface VesselEngineCandidate {
  instance: string
  rpm: number | null
  coolant_c: number | null
}

export interface VesselBatteryCandidate {
  path: string
  /** The <id> in electrical.batteries.<id>, the key an operator name is saved under. */
  instance: string
  /** What the bus itself calls this instance; '' when it says nothing. */
  bus_name: string
  /** A charger's own input (a solar array's voltage), not a battery. */
  charger_input: boolean
  voltage: number | null
  current: number | null
  soc: number | null
}

export interface VesselAlternatorCandidate {
  /** The <id> in electrical.alternator.<id>. */
  instance: string
  /** Live output current in amps; null when the instance reports nothing. */
  current: number | null
}

export interface VesselDetectorStatus {
  ready: boolean
  missing?: string
}

export interface VesselCandidatesResponse {
  engines: VesselEngineCandidate[]
  batteries: VesselBatteryCandidate[]
  alternators: VesselAlternatorCandidate[]
  detectors: Record<string, VesselDetectorStatus>
}

export const emptyVesselCandidates: VesselCandidatesResponse = { engines: [], batteries: [], alternators: [], detectors: {} }

/** "propulsion.port" style display, or a raw instance id if that's all there is: "port" -> "Port". */
export function titleCaseInstance(instance: string): string {
  const trimmed = instance.trim()
  if (trimmed === '') return ''
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1)
}
