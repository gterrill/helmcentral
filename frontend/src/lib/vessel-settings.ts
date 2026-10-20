/**
 * Types for the roles block (Settings -> SignalK -> Roles) and the discovery
 * endpoint it sits beside. Mirrors backend/vessel_roles.go and
 * backend/vessel_handlers.go field-for-field, snake_case as the API sends it.
 *
 * An equipment item owns one SignalK source (`propulsion.port`,
 * `electrical.batteries.1`, ...); a role is the choice of which item plays a
 * single part, such as the house bank. Roles hold item ids, never paths.
 */

/**
 * The stored answer "this boat has none". Distinct from '' (not answered yet):
 * it completes setup, and everything that reads a role treats it like unset
 * for data. Only the optional roles accept it (see OPTIONAL_ROLE_FIELDS).
 * Mirrors roleNone in backend/vessel_roles.go.
 */
export const ROLE_NONE = 'none'

/** The roles a boat can lack. The house bank and the voltage readout battery are not on the list. */
export const OPTIONAL_ROLE_FIELDS: readonly (keyof VesselRoles)[] = [
  'port_alternator',
  'starboard_alternator',
  'shore_charger',
  'inverter',
  'generator',
  'generator_ac_instance',
]

/** Every role is an equipment item id except the generator's AC input, which is an instance id. '' is not set; ROLE_NONE is "this boat has none". */
export interface VesselRoles {
  house_bank: string
  port_alternator: string
  starboard_alternator: string
  voltage_readout_battery: string
  shore_charger: string
  inverter: string
  generator: string
  /** The <id> in electrical.ac.<id>: an AC meter, which no item owns. */
  generator_ac_instance: string
}

export const emptyVesselRoles: VesselRoles = {
  house_bank: '',
  port_alternator: '',
  starboard_alternator: '',
  voltage_readout_battery: '',
  shore_charger: '',
  inverter: '',
  generator: '',
  generator_ac_instance: '',
}

/** The kinds of source an item can own; also the `source_kind` an item reports. */
export type SourceKind = 'engine' | 'battery' | 'alternator' | 'solar' | 'tank' | 'generator' | 'charger' | 'inverter'

export const SOURCE_KINDS: readonly SourceKind[] = ['engine', 'battery', 'alternator', 'solar', 'tank', 'generator', 'charger', 'inverter']

export const SOURCE_KIND_LABELS: Record<SourceKind, string> = {
  engine: 'Engines',
  battery: 'Batteries',
  alternator: 'Alternators',
  solar: 'Solar controllers',
  tank: 'Tanks',
  generator: 'Generators',
  charger: 'Chargers',
  inverter: 'Inverters',
}

/** The singular, for a button or a sentence: "Create engine item". */
export const SOURCE_KIND_NOUNS: Record<SourceKind, string> = {
  engine: 'engine',
  battery: 'battery',
  alternator: 'alternator',
  solar: 'solar controller',
  tank: 'tank',
  generator: 'generator',
  charger: 'charger',
  inverter: 'inverter',
}

/** The inventory system a new item for a kind starts in; mirrors sourceSystem in the backend. */
export function systemForKind(kind: SourceKind): 'propulsion' | 'fuel' | 'electrical' {
  if (kind === 'engine') return 'propulsion'
  if (kind === 'tank') return 'fuel'
  return 'electrical'
}

/** The live hour meter a source carries, or '' (mirrors sourceHourMeterPath in the backend). */
export function hourMeterSourceFor(source: string): string {
  if (/^propulsion\.[A-Za-z0-9_-]+$/.test(source)) return `${source}.runTime`
  if (/^electrical\.generator\.[A-Za-z0-9_-]+$/.test(source)) return `${source}.runtime`
  return ''
}

/** One live figure beside a source, already formatted for the operator. */
export interface DiscoveryMetric {
  label: string
  text: string
}

export interface DiscoveryItemRef {
  id: string
  name: string
}

/** A source the boat reports now, or one an item still owns that has gone quiet. */
export interface DiscoveryRow {
  source: string
  /** The <id> segment: "port", "1", "fuel.2". */
  instance: string
  /** What the bus itself calls a battery; '' when it says nothing. */
  bus_name: string
  /** A charger's own input (a solar array's voltage), not a battery. */
  charger_input: boolean
  /** False for a source an item owns that the boat has stopped reporting. */
  reporting: boolean
  metrics: DiscoveryMetric[]
  item: DiscoveryItemRef | null
  /** When the boat last logged this source's signature reading (an ISO time); null when it has not within the logged history. */
  last_logged: string | null
  /**
   * Whether the source carries what its kind needs (RPM for an engine).
   * 'missing': history was read and holds none. 'unknown': history could not
   * be read. An unowned source is only listed when it carries it.
   */
  signature: 'carried' | 'missing' | 'unknown'
}

export interface DiscoveryGroup {
  /** A SourceKind, or 'ac' for the AC inputs, which no item owns. */
  kind: string
  mappable: boolean
  /** What a source of this kind must report, in the operator's words ("RPM"). */
  signature_label: string
  rows: DiscoveryRow[]
}

export interface VesselDetectorStatus {
  ready: boolean
  missing?: string
}

export interface VesselDiscoveryResponse {
  /** False while Helmcentral has no vessel data from SignalK. */
  connected: boolean
  groups: DiscoveryGroup[]
  detectors: Record<string, VesselDetectorStatus>
  /** Why logged history could not be read; '' when it was. While set, only sources reporting now are listed. */
  history_error: string
}

export const emptyVesselDiscovery: VesselDiscoveryResponse = { connected: false, groups: [], detectors: {}, history_error: '' }

/** "16 Oct": the day a source's signature reading was last logged. */
export function loggedDay(iso: string): string {
  return new Date(iso).toLocaleDateString('en-GB', { day: 'numeric', month: 'short' })
}

/** The row for an exact source, or undefined. */
export function discoveryRowFor(discovery: VesselDiscoveryResponse, source: string): { row: DiscoveryRow; label: string } | undefined {
  for (const g of discovery.groups) {
    const row = g.rows.find((r) => r.source === source)
    if (row) return { row, label: g.signature_label }
  }
  return undefined
}

/**
 * The warning for a source an item owns that has never reported what its kind
 * needs, or '' when there is nothing to say. It is the operator's cue to pick a
 * different source for the item.
 */
export function signatureWarning(found: { row: DiscoveryRow; label: string } | undefined): string {
  if (!found || found.row.item === null || found.row.signature !== 'missing') return ''
  return `${found.row.source} has not reported ${found.label} in the logged history, so nothing reads it. Choose the source that does.`
}

/** "not reporting now · last logged 16 Oct", or "not reporting now" when never logged. */
export function notReportingText(row: DiscoveryRow): string {
  return row.last_logged ? `not reporting now · last logged ${loggedDay(row.last_logged)}` : 'not reporting now'
}

/** The rows of one kind, or none. */
export function discoveryRows(discovery: VesselDiscoveryResponse, kind: string): DiscoveryRow[] {
  return discovery.groups.find((g) => g.kind === kind)?.rows ?? []
}

const SOURCE_SHAPES: { kind: SourceKind; re: RegExp }[] = [
  { kind: 'engine', re: /^propulsion\.[A-Za-z0-9_-]+$/ },
  { kind: 'battery', re: /^electrical\.batteries\.[A-Za-z0-9_-]+$/ },
  { kind: 'alternator', re: /^electrical\.alternator\.[A-Za-z0-9_-]+$/ },
  { kind: 'solar', re: /^electrical\.solar\.[A-Za-z0-9_-]+$/ },
  { kind: 'tank', re: /^tanks\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$/ },
  { kind: 'generator', re: /^electrical\.generator\.[A-Za-z0-9_-]+$/ },
  { kind: 'charger', re: /^electrical\.chargers\.[A-Za-z0-9_-]+$/ },
  { kind: 'inverter', re: /^electrical\.inverters\.[A-Za-z0-9_-]+$/ },
]

/** The kind of item a source belongs to, or null for '' or anything that is not one of the eight shapes. */
export function sourceKindOf(source: string): SourceKind | null {
  return SOURCE_SHAPES.find((shape) => shape.re.test(source))?.kind ?? null
}
