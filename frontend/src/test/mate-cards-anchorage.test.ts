import { describe, expect, it } from 'vitest'

import { decodeMateCards } from '@/lib/mate-cards'

import { anchorageCard, anchoragePayload } from './helpers/anchorage-fixture'

type Loose = ReturnType<typeof JSON.parse>

const reasonOf = (payload: unknown) => {
  const c = decodeMateCards([{ kind: 'anchorage', payload }])[0]
  expect(c.kind).toBe('error')
  return c.kind === 'error' ? c.reason : ''
}
const mutate = (fn: (p: Loose) => void) => {
  const p = JSON.parse(JSON.stringify(anchoragePayload()))
  fn(p)
  return p
}

describe('decodeMateCards: anchorage', () => {
  it('reads the card the Go side writes for West Butterfly Bay', () => {
    const card = anchorageCard()
    expect(card.name).toBe('West Butterfly Bay')
    expect(card.timezone).toBe('Australia/Lindeman')
    expect(card.arrive).toBe('2026-11-02T14:00:00+10:00')
    expect(card.leave).toBe('2026-11-03T08:00:00+10:00')
    expect(card.shelter.bearings).toHaveLength(36)
    expect(card.shelter.bearings[9]).toMatchObject({ bearingDeg: 90 })
    expect(card.shelter.arcs.length).toBeGreaterThan(0)
    expect(card.forecast.rows).toHaveLength(7)
    expect(card.forecast.rows[0]).toMatchObject({ windKts: 15, gustKts: 19, dir: 'N', waveM: 0.8, waveDir: 'SE' })
    expect(card.forecast.days).toHaveLength(2)
    expect(card.tide.station).toBe('Shute Harbour')
    expect(card.tide.heightAtArrivalM).toBe(1.5)
    expect(card.tide.rangeM).toBe(2.4)
    expect(card.features.moorings).toHaveLength(23)
    expect(card.features.moorings[0]).toMatchObject({ vesselSizeM: 20, class: 'B', detailsUntagged: false })
    expect(card.features.bay?.name).toBe('Butterfly Bay')
    expect(card.features.restrictedAreas.every((r) => r.source === 'protected_area' && r.distanceM === null)).toBe(true)
    expect(card.features.moorings[0]).toMatchObject({ lat: -20.076476, lon: 148.925222 })
    expect(card.dataAsOf).toBe('2026-10-31T21:22:14Z')
  })

  it('ignores a field it does not know, so a newer server can add one', () => {
    const p = mutate((p) => {
      p.surprise = 1
      p.shelter.note = 'x'
      p.forecast.rows[0].extra = []
      p.features.moorings[0].colour = 'green'
      p.features.extra = 1
    })
    expect(decodeMateCards([{ kind: 'anchorage', payload: p }])[0].kind).toBe('anchorage')
  })

  it('accepts empty sections that carry an error', () => {
    const p = mutate((p) => {
      p.tide = { ...p.tide, error: 'Tide unavailable: x', extremes: [], height_at_arrival_m: null, range_m: null }
    })
    const c = decodeMateCards([{ kind: 'anchorage', payload: p }])[0]
    expect(c.kind === 'anchorage' && c.tide.error).toBe('Tide unavailable: x')
  })

  it('accepts a missing forecast time and data date as empty', () => {
    const card = anchorageCard({ data_as_of: '', forecast: { ...(anchoragePayload().forecast as object), fetched_at: '' } })
    expect(card.dataAsOf).toBe('')
    expect(card.forecast.fetchedAt).toBe('')
  })

  it('rejects a missing field', () => {
    for (const key of ['shelter', 'forecast', 'tide', 'features', 'arrive', 'leave', 'timezone', 'data_as_of']) {
      expect(reasonOf(mutate((p) => delete p[key]))).toMatch(new RegExp(key))
    }
    expect(reasonOf(mutate((p) => delete p.forecast.error))).toMatch(/forecast\.error/)
    expect(reasonOf(mutate((p) => delete p.features.truncated))).toMatch(/features\.truncated/)
  })

  it('rejects a malformed field', () => {
    expect(reasonOf(mutate((p) => (p.lat = 91)))).toMatch(/lat/)
    expect(reasonOf(mutate((p) => (p.arrive = 'tomorrow')))).toMatch(/arrive/)
    expect(reasonOf(mutate((p) => (p.timezone = 'Mars/Olympus')))).toMatch(/timezone/)
    expect(reasonOf(mutate((p) => (p.data_as_of = 'yesterday')))).toMatch(/data_as_of/)
    expect(reasonOf(mutate((p) => (p.forecast.rows[0].wind_kts = '15')))).toMatch(/wind_kts/)
    expect(reasonOf(mutate((p) => (p.forecast.rows[0].wind_kts = -1)))).toMatch(/wind_kts/)
    expect(reasonOf(mutate((p) => (p.forecast.rows[0].shelter = 'safe')))).toMatch(/shelter/)
    expect(reasonOf(mutate((p) => (p.forecast.days[0].date = '2 Nov')))).toMatch(/date/)
    expect(reasonOf(mutate((p) => (p.tide.extremes[0].type = 'middle')))).toMatch(/type/)
    expect(reasonOf(mutate((p) => (p.tide.extremes[0].in_window = 1)))).toMatch(/in_window/)
    expect(reasonOf(mutate((p) => (p.features.moorings[0].vessel_size_m = 'big')))).toMatch(/vessel_size_m/)
    expect(reasonOf(mutate((p) => (p.features.moorings[0].details_untagged = 'no')))).toMatch(/details_untagged/)
    expect(reasonOf(mutate((p) => (p.features.restricted_areas[0].source = 'guess')))).toMatch(/source/)
    expect(reasonOf(mutate((p) => (p.features.bay = { name: 'x' })))).toMatch(/bay/)
  })

  it('rejects a shelter that is not 36 bearings in order', () => {
    expect(reasonOf(mutate((p) => p.shelter.bearings.pop()))).toMatch(/36 bearings/)
    expect(reasonOf(mutate((p) => (p.shelter.bearings[3].bearing_deg = 35)))).toMatch(/bearing_deg/)
    expect(reasonOf(mutate((p) => (p.shelter.bearings[0].grade = 'calm')))).toMatch(/grade/)
    expect(reasonOf(mutate((p) => (p.shelter.bearings[0].open = 'yes')))).toMatch(/open/)
  })

  it('rejects shapes and points the map could not draw', () => {
    const area = { osm: 'way/1', name: 'x', category: '', distance_m: 5 }
    expect(reasonOf(mutate((p) => p.features.anchorage_areas.push({ ...area, shapes: [{ kind: 'circle', points: [] }] })))).toMatch(/kind/)
    expect(reasonOf(mutate((p) => (p.features.moorings[0].lat = null)))).toMatch(/lat/)
    expect(reasonOf(mutate((p) => (p.features.moorings[0].lon = 200)))).toMatch(/lon/)
    expect(reasonOf(mutate((p) => delete p.features.restricted_areas[0].shapes))).toMatch(/shapes/)
  })

  it('turns a non-object payload into an error entry', () => {
    expect(reasonOf('anchorage')).toMatch(/object/)
  })
})
