import { describe, expect, it } from 'vitest'

import { parseOvernightProjection, projectSocAtDawn, type OvernightProjection } from '@/lib/soc-projection'

const SOC_PATH = 'electrical.batteries.0.capacity.stateOfCharge'

const validPayload = {
  soc_path: SOC_PATH,
  sunset: '2026-09-07T07:56:45Z',
  sunrise: '2026-09-07T20:07:50Z',
  basis: 'history',
  night_rate_percent_per_hour: -2.4866,
  nights_used: 4,
  nights_considered: 7,
  reason: null,
  computed_at: '2026-09-06T22:40:25Z',
}

describe('parseOvernightProjection', () => {
  it('parses a well-formed history payload', () => {
    const result = parseOvernightProjection(validPayload)
    expect(result).not.toBeNull()
    expect(result?.socPath).toBe(SOC_PATH)
    expect(result?.sunset).toBeInstanceOf(Date)
    expect(result?.sunset.toISOString()).toBe('2026-09-07T07:56:45.000Z')
    expect(result?.sunrise.toISOString()).toBe('2026-09-07T20:07:50.000Z')
    expect(result?.basis).toBe('history')
    expect(result?.nightRatePercentPerHour).toBeCloseTo(-2.4866, 4)
    expect(result?.nightsUsed).toBe(4)
    expect(result?.nightsConsidered).toBe(7)
    expect(result?.reason).toBeNull()
  })

  it('parses a linear payload with a null night rate and a reason string', () => {
    const result = parseOvernightProjection({
      ...validPayload,
      basis: 'linear',
      night_rate_percent_per_hour: null,
      nights_used: 0,
      reason: 'influxdb not configured',
    })
    expect(result?.basis).toBe('linear')
    expect(result?.nightRatePercentPerHour).toBeNull()
    expect(result?.reason).toBe('influxdb not configured')
  })

  it('parses a none payload', () => {
    const result = parseOvernightProjection({
      ...validPayload,
      basis: 'none',
      night_rate_percent_per_hour: null,
      nights_used: 1,
      reason: 'need 2 usable nights, have 1',
    })
    expect(result?.basis).toBe('none')
  })

  it('rejects a non-object payload', () => {
    expect(parseOvernightProjection(null)).toBeNull()
    expect(parseOvernightProjection(undefined)).toBeNull()
    expect(parseOvernightProjection('history')).toBeNull()
    expect(parseOvernightProjection([1, 2, 3])).toBeNull()
  })

  it('rejects a non-string soc_path', () => {
    expect(parseOvernightProjection({ ...validPayload, soc_path: 42 })).toBeNull()
  })

  it('rejects an unparsable sunset', () => {
    expect(parseOvernightProjection({ ...validPayload, sunset: 'not-a-date' })).toBeNull()
  })

  it('rejects an unparsable sunrise', () => {
    expect(parseOvernightProjection({ ...validPayload, sunrise: 'not-a-date' })).toBeNull()
  })

  it('rejects a basis outside the three known values', () => {
    expect(parseOvernightProjection({ ...validPayload, basis: 'forecast' })).toBeNull()
  })

  it('rejects a night rate that is neither a number nor null', () => {
    expect(parseOvernightProjection({ ...validPayload, night_rate_percent_per_hour: '-2.4' })).toBeNull()
  })
})

function baseProjection(overrides: Partial<OvernightProjection> = {}): OvernightProjection {
  return {
    socPath: SOC_PATH,
    sunset: new Date('2026-09-07T07:56:00Z'),
    sunrise: new Date('2026-09-07T20:07:00Z'),
    basis: 'history',
    nightRatePercentPerHour: -2.5,
    nightsUsed: 4,
    nightsConsidered: 7,
    reason: null,
    ...overrides,
  }
}

// Daytime fixture: now sits before the next sunset, which is itself before
// the next sunrise, so projection.sunset < projection.sunrise.
const DAYTIME_NOW = new Date('2026-09-07T02:00:00Z')

describe('projectSocAtDawn', () => {
  it('returns null when there is no projection', () => {
    expect(
      projectSocAtDawn({ socPercent: 63, liveRatePercentPerHour: 2.8, projection: null, now: DAYTIME_NOW }),
    ).toBeNull()
  })

  it('returns null when the basis is none', () => {
    const projection = baseProjection({ basis: 'none', nightRatePercentPerHour: null })
    expect(
      projectSocAtDawn({ socPercent: 63, liveRatePercentPerHour: 2.8, projection, now: DAYTIME_NOW }),
    ).toBeNull()
  })

  it('returns null when soc is unknown', () => {
    const projection = baseProjection()
    expect(
      projectSocAtDawn({ socPercent: null, liveRatePercentPerHour: 2.8, projection, now: DAYTIME_NOW }),
    ).toBeNull()
  })

  it('before sunset with basis history and a known night rate: live rate to sunset, then the night rate to sunrise', () => {
    const projection = baseProjection({ basis: 'history', nightRatePercentPerHour: -2.5 })
    const result = projectSocAtDawn({
      socPercent: 63,
      liveRatePercentPerHour: 2.8,
      projection,
      now: DAYTIME_NOW,
    })

    // hoursToSunset = 07:56 - 02:00 = 5h56m; hoursSunsetToSunrise = 20:07 - 07:56 = 12h11m.
    const hoursToSunset = (projection.sunset.getTime() - DAYTIME_NOW.getTime()) / 3_600_000
    const hoursSunsetToSunrise = (projection.sunrise.getTime() - projection.sunset.getTime()) / 3_600_000
    const socAtSunset = Math.min(100, Math.max(0, 63 + 2.8 * hoursToSunset))
    const expected = Math.min(100, Math.max(0, socAtSunset + -2.5 * hoursSunsetToSunrise))

    expect(result).not.toBeNull()
    expect(result?.basis).toBe('history')
    expect(result?.socPercent).toBeCloseTo(expected, 6)
    expect(Math.round(result!.socPercent)).toBe(49)
  })

  it('treats a null live rate as a flat bank (0) in the history branch', () => {
    const projection = baseProjection({ basis: 'history', nightRatePercentPerHour: -2.5 })
    const result = projectSocAtDawn({
      socPercent: 63,
      liveRatePercentPerHour: null,
      projection,
      now: DAYTIME_NOW,
    })

    const hoursSunsetToSunrise = (projection.sunrise.getTime() - projection.sunset.getTime()) / 3_600_000
    const expected = Math.min(100, Math.max(0, 63 + -2.5 * hoursSunsetToSunrise))
    expect(result?.socPercent).toBeCloseTo(expected, 6)
  })

  it('before sunset with basis linear: live rate held to sunrise', () => {
    const projection = baseProjection({ basis: 'linear', nightRatePercentPerHour: null })
    const result = projectSocAtDawn({
      socPercent: 40,
      liveRatePercentPerHour: 3.0,
      projection,
      now: DAYTIME_NOW,
    })

    const hoursToSunrise = (projection.sunrise.getTime() - DAYTIME_NOW.getTime()) / 3_600_000
    const expected = Math.min(100, Math.max(0, 40 + 3.0 * hoursToSunrise))

    expect(result?.basis).toBe('linear')
    expect(result?.socPercent).toBeCloseTo(expected, 6)
  })

  it('a history basis with a null night rate falls back to a live-rate projection to sunrise rather than crashing', () => {
    const projection = baseProjection({ basis: 'history', nightRatePercentPerHour: null })
    const result = projectSocAtDawn({
      socPercent: 50,
      liveRatePercentPerHour: 2.0,
      projection,
      now: DAYTIME_NOW,
    })

    const hoursToSunrise = (projection.sunrise.getTime() - DAYTIME_NOW.getTime()) / 3_600_000
    const expected = Math.min(100, Math.max(0, 50 + 2.0 * hoursToSunrise))

    expect(result?.basis).toBe('linear')
    expect(result?.socPercent).toBeCloseTo(expected, 6)
  })

  it('after sunset and before sunrise uses the live rate to sunrise regardless of the projection basis, and labels it linear', () => {
    // Nighttime fixture: the next sunrise comes before the next sunset.
    const projection = baseProjection({
      basis: 'history',
      nightRatePercentPerHour: -2.5,
      sunrise: new Date('2026-09-07T20:07:00Z'),
      sunset: new Date('2026-09-08T07:56:00Z'),
    })
    const now = new Date('2026-09-07T10:00:00Z')
    const result = projectSocAtDawn({
      socPercent: 50,
      liveRatePercentPerHour: -3,
      projection,
      now,
    })

    const hoursToSunrise = (projection.sunrise.getTime() - now.getTime()) / 3_600_000
    const expected = Math.min(100, Math.max(0, 50 + -3 * hoursToSunrise))

    expect(result?.basis).toBe('linear')
    expect(result?.socPercent).toBeCloseTo(expected, 6)
  })

  it('clamps the dawn estimate at 100 when the projection would overshoot full', () => {
    const projection = baseProjection({ basis: 'linear', nightRatePercentPerHour: null })
    const result = projectSocAtDawn({
      socPercent: 95,
      liveRatePercentPerHour: 10,
      projection,
      now: DAYTIME_NOW,
    })

    expect(result?.socPercent).toBe(100)
  })

  it('clamps the dawn estimate at 0 when the projection would overshoot empty', () => {
    const projection = baseProjection({ basis: 'linear', nightRatePercentPerHour: null })
    const result = projectSocAtDawn({
      socPercent: 5,
      liveRatePercentPerHour: -10,
      projection,
      now: DAYTIME_NOW,
    })

    expect(result?.socPercent).toBe(0)
  })

  it('clamps the intermediate socAtSunset before applying the night rate', () => {
    // A very high live rate would push the afternoon figure past 100 on its
    // own; the clamp there must happen before the night rate is applied, not
    // only at the end, or a small negative night rate could still read under
    // 100 despite the bank already being full at sunset.
    const projection = baseProjection({ basis: 'history', nightRatePercentPerHour: -0.1 })
    const result = projectSocAtDawn({
      socPercent: 90,
      liveRatePercentPerHour: 50,
      projection,
      now: DAYTIME_NOW,
    })

    const hoursSunsetToSunrise = (projection.sunrise.getTime() - projection.sunset.getTime()) / 3_600_000
    const expected = 100 + -0.1 * hoursSunsetToSunrise
    expect(result?.socPercent).toBeCloseTo(expected, 6)
  })
})
