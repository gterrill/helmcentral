/**
 * Unit conversions for bindable gauges (ADR 0039).
 *
 * SignalK is SI-canonical: oil pressure arrives in pascals, temperatures in
 * kelvin, fuel rate in cubic metres per second, engine hours in seconds, and
 * revolutions in hertz. None of those are readable on a helm display, and
 * `lib/units.ts` only knew feet and fahrenheit, so every gauge needs this.
 */

export type QuantityId =
  | 'pressure'
  | 'temperature'
  | 'volumetricFlow'
  | 'duration'
  | 'ratio'
  | 'volume'
  | 'speed'
  | 'length'
  | 'frequency'
  | 'raw'

export interface UnitOption {
  /** Stored in the widget config, so it must stay stable. */
  id: string
  label: string
  /** Converts from the SI base unit SignalK publishes. */
  fromSI: (value: number) => number
  decimals: number
}

export interface Quantity {
  id: QuantityId
  label: string
  /** SignalK's unit string for this quantity, used to infer it from meta. */
  siUnit: string
  units: UnitOption[]
}

const identity = (value: number) => value

export const QUANTITIES: Quantity[] = [
  {
    id: 'pressure',
    label: 'Pressure',
    siUnit: 'Pa',
    units: [
      { id: 'kPa', label: 'kPa', fromSI: (v) => v / 1000, decimals: 0 },
      { id: 'psi', label: 'psi', fromSI: (v) => v / 6894.757, decimals: 1 },
      { id: 'bar', label: 'bar', fromSI: (v) => v / 100000, decimals: 2 },
      { id: 'Pa', label: 'Pa', fromSI: identity, decimals: 0 },
    ],
  },
  {
    id: 'temperature',
    label: 'Temperature',
    siUnit: 'K',
    units: [
      { id: 'C', label: '°C', fromSI: (v) => v - 273.15, decimals: 1 },
      { id: 'F', label: '°F', fromSI: (v) => (v - 273.15) * 9 / 5 + 32, decimals: 1 },
      { id: 'K', label: 'K', fromSI: identity, decimals: 1 },
    ],
  },
  {
    id: 'volumetricFlow',
    label: 'Flow',
    siUnit: 'm3/s',
    units: [
      { id: 'Lph', label: 'L/h', fromSI: (v) => v * 1000 * 3600, decimals: 1 },
      { id: 'gph', label: 'gal/h', fromSI: (v) => v * 264.172 * 3600, decimals: 1 },
    ],
  },
  {
    id: 'duration',
    label: 'Duration',
    siUnit: 's',
    units: [
      { id: 'h', label: 'h', fromSI: (v) => v / 3600, decimals: 0 },
      { id: 'min', label: 'min', fromSI: (v) => v / 60, decimals: 0 },
      { id: 's', label: 's', fromSI: identity, decimals: 0 },
    ],
  },
  {
    id: 'ratio',
    label: 'Ratio',
    siUnit: 'ratio',
    units: [
      { id: 'percent', label: '%', fromSI: (v) => v * 100, decimals: 0 },
      { id: 'ratio', label: '', fromSI: identity, decimals: 2 },
    ],
  },
  {
    id: 'volume',
    label: 'Volume',
    siUnit: 'm3',
    units: [
      { id: 'L', label: 'L', fromSI: (v) => v * 1000, decimals: 0 },
      { id: 'gal', label: 'gal', fromSI: (v) => v * 264.172, decimals: 0 },
    ],
  },
  {
    id: 'speed',
    label: 'Speed',
    siUnit: 'm/s',
    units: [
      { id: 'kn', label: 'kts', fromSI: (v) => v * 1.943844, decimals: 1 },
      { id: 'kph', label: 'km/h', fromSI: (v) => v * 3.6, decimals: 1 },
      { id: 'mps', label: 'm/s', fromSI: identity, decimals: 1 },
    ],
  },
  {
    id: 'length',
    label: 'Length',
    siUnit: 'm',
    units: [
      { id: 'm', label: 'm', fromSI: identity, decimals: 1 },
      { id: 'ft', label: 'ft', fromSI: (v) => v * 3.28084, decimals: 1 },
    ],
  },
  {
    id: 'frequency',
    label: 'Frequency',
    siUnit: 'Hz',
    units: [
      // SignalK publishes revolutions in Hz; every tachometer on earth shows RPM.
      { id: 'rpm', label: 'RPM', fromSI: (v) => v * 60, decimals: 0 },
      { id: 'Hz', label: 'Hz', fromSI: identity, decimals: 1 },
    ],
  },
  {
    id: 'raw',
    label: 'Unitless',
    siUnit: '',
    units: [{ id: 'raw', label: '', fromSI: identity, decimals: 2 }],
  },
]

export function quantityById(id: QuantityId | string): Quantity {
  return QUANTITIES.find((quantity) => quantity.id === id) ?? QUANTITIES[QUANTITIES.length - 1]
}

/** Infers the quantity from SignalK's own meta.units, so the picker can preselect. */
export function quantityForSIUnit(siUnit: string | undefined): Quantity {
  const trimmed = (siUnit ?? '').trim()
  if (trimmed === '') return quantityById('raw')
  return QUANTITIES.find((quantity) => quantity.siUnit === trimmed) ?? quantityById('raw')
}

export function unitOption(quantityId: QuantityId | string, unitId: string | undefined): UnitOption {
  const quantity = quantityById(quantityId)
  return quantity.units.find((unit) => unit.id === unitId) ?? quantity.units[0]
}

/**
 * Converts and formats a raw SI value for display. Returns null when there is
 * nothing to show, so callers render the structural dash rather than a zero —
 * a gauge reading 0 when it means "no data" is the dangerous failure.
 */
export function formatQuantity(
  value: number | null | undefined,
  quantityId: QuantityId | string,
  unitId: string | undefined,
  decimals?: number,
): string | null {
  if (value === null || value === undefined || !Number.isFinite(value)) return null

  const unit = unitOption(quantityId, unitId)
  const converted = unit.fromSI(value)
  const places = decimals ?? unit.decimals

  return converted.toFixed(Math.max(0, Math.min(6, places)))
}

export function convertFromSI(value: number, quantityId: QuantityId | string, unitId: string | undefined): number {
  return unitOption(quantityId, unitId).fromSI(value)
}
