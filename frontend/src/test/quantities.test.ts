import { describe, expect, it } from 'vitest'

import {
  convertFromSI,
  formatQuantity,
  quantityForSIUnit,
  unitOption,
} from '@/lib/quantities'

describe('quantity conversions', () => {
  // SignalK is SI-canonical, so every one of these is a conversion the old
  // two-function units.ts could not do.
  it('converts oil pressure out of pascals', () => {
    // 35 psi, a plausible warm idle.
    expect(convertFromSI(241_325, 'pressure', 'psi')).toBeCloseTo(35, 1)
    expect(convertFromSI(241_325, 'pressure', 'kPa')).toBeCloseTo(241.3, 1)
    expect(convertFromSI(241_325, 'pressure', 'bar')).toBeCloseTo(2.41, 2)
  })

  it('converts coolant temperature out of kelvin', () => {
    expect(convertFromSI(361.15, 'temperature', 'C')).toBeCloseTo(88, 2)
    expect(convertFromSI(361.15, 'temperature', 'F')).toBeCloseTo(190.4, 1)
  })

  it('converts fuel rate out of cubic metres per second', () => {
    // 20 L/h.
    const litresPerHourInSI = 20 / 1000 / 3600
    expect(convertFromSI(litresPerHourInSI, 'volumetricFlow', 'Lph')).toBeCloseTo(20, 3)
    expect(convertFromSI(litresPerHourInSI, 'volumetricFlow', 'gph')).toBeCloseTo(5.28, 2)
  })

  it('converts engine hours out of seconds', () => {
    expect(convertFromSI(3600 * 1234, 'duration', 'h')).toBeCloseTo(1234, 6)
  })

  it('converts revolutions out of hertz, because tachometers show RPM', () => {
    expect(convertFromSI(30, 'frequency', 'rpm')).toBeCloseTo(1800, 6)
  })

  it('converts a tank level ratio to a percentage', () => {
    expect(convertFromSI(0.8106, 'ratio', 'percent')).toBeCloseTo(81.06, 4)
  })

  it('converts tank capacity out of cubic metres', () => {
    expect(convertFromSI(1.2, 'volume', 'L')).toBeCloseTo(1200, 6)
  })
})

describe('quantity inference', () => {
  it('infers the quantity from SignalK meta units', () => {
    expect(quantityForSIUnit('Pa').id).toBe('pressure')
    expect(quantityForSIUnit('K').id).toBe('temperature')
    expect(quantityForSIUnit('m3/s').id).toBe('volumetricFlow')
    expect(quantityForSIUnit('Hz').id).toBe('frequency')
  })

  it('falls back to unitless for an unknown or missing unit', () => {
    expect(quantityForSIUnit('parsecs').id).toBe('raw')
    expect(quantityForSIUnit(undefined).id).toBe('raw')
  })

  it('falls back to the first unit when the configured one is unknown', () => {
    // A config written against a unit that later disappears must still render.
    expect(unitOption('pressure', 'furlongs').id).toBe('kPa')
  })
})

describe('formatting', () => {
  it('uses the unit default decimals unless overridden', () => {
    expect(formatQuantity(241_325, 'pressure', 'psi')).toBe('35.0')
    expect(formatQuantity(241_325, 'pressure', 'psi', 2)).toBe('35.00')
  })

  // A gauge showing 0 when it means "no data" is the dangerous failure, so
  // absent values return null and callers render the structural dash.
  it('returns null rather than zero for absent data', () => {
    expect(formatQuantity(null, 'pressure', 'psi')).toBeNull()
    expect(formatQuantity(undefined, 'pressure', 'psi')).toBeNull()
    expect(formatQuantity(Number.NaN, 'pressure', 'psi')).toBeNull()
  })

  it('formats a real zero as zero, not as absent', () => {
    expect(formatQuantity(0, 'volumetricFlow', 'Lph')).toBe('0.0')
  })
})

// SignalK publishes electrical readings in W, V and A. Without these a
// generator's output rendered as a bare number with no unit at all.
describe('electrical quantities', () => {
  it('formats power in kilowatts', () => {
    expect(formatQuantity(7564, 'power', 'kW')).toBe('7.56')
    expect(formatQuantity(13500, 'power', 'kW', 1)).toBe('13.5')
    expect(formatQuantity(7564, 'power', 'W')).toBe('7564')
  })

  it('passes volts and amps through', () => {
    expect(formatQuantity(232, 'potential', 'V', 0)).toBe('232')
    expect(formatQuantity(39, 'current', 'A', 0)).toBe('39')
  })

  it('infers each from SignalK meta units', () => {
    expect(quantityForSIUnit('W').id).toBe('power')
    expect(quantityForSIUnit('V').id).toBe('potential')
    expect(quantityForSIUnit('A').id).toBe('current')
  })

  // Absent stays absent, the same rule every other quantity follows.
  it('formats an absent electrical reading to null', () => {
    expect(formatQuantity(null, 'power', 'kW')).toBeNull()
  })
})

describe('fuel economy', () => {
  // 792950.7 m/m³ is what the vessel published at 10.21 kn burning 23.9 L/h on
  // that engine, which works out to 0.427 nm/L the long way round.
  it('converts metres per cubic metre to nautical miles per litre', () => {
    expect(formatQuantity(792950.7, 'fuelEconomy', 'nmpl')).toBe('0.43')
  })

  it('converts to nautical miles per US gallon', () => {
    expect(formatQuantity(792950.7, 'fuelEconomy', 'nmpg')).toBe('1.62')
  })

  it('infers the quantity from SignalK meta units', () => {
    expect(quantityForSIUnit('m/m3').id).toBe('fuelEconomy')
  })
})
