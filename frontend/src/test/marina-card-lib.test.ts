import { describe, expect, it } from 'vitest'

import { dataDateText, distanceText, hasMarinaGeometry, kindLabel, marinaFeatures, marinaFrame, phoneHref, safeHref, shapesBounds } from '@/lib/marina-card'

import { marinaCard, marinaCardWithoutGeometry } from './helpers/marina-fixture'

describe('marina card words', () => {
  it('turns kind slugs into the operator\'s words', () => {
    expect(kindLabel('haul_out')).toBe('Haul-out')
    expect(kindLabel('pump_out')).toBe('Pump-out')
    expect(kindLabel('boat_shop')).toBe('Boat shop')
    expect(kindLabel('some_new_kind')).toBe('Some new kind')
  })

  it('writes distances in metres, then kilometres, or in nautical miles for imperial', () => {
    expect(distanceText(580, 'metric')).toBe('580 m')
    expect(distanceText(1000, 'metric')).toBe('1.0 km')
    expect(distanceText(926, 'imperial')).toBe('0.5 nm')
  })

  it('writes the data date as a day, in UTC', () => {
    expect(dataDateText('2026-10-29T21:20:09Z')).toBe('29 Oct 2026')
    expect(dataDateText('')).toBe('')
  })

  it('leaves out a data date it cannot read', () => {
    expect(dataDateText('last tuesday')).toBe('')
  })
})

describe('marina card links', () => {
  it('links only web addresses', () => {
    expect(safeHref('https://example.com/a')).toBe('https://example.com/a')
    expect(safeHref('http://example.com')).toBe('http://example.com/')
    for (const bad of ['javascript:alert(1)', 'data:text/html,x', 'mailto:a@b.c', 'example.com', '', 'ftp://example.com']) {
      expect(safeHref(bad)).toBeNull()
    }
  })

  it('links only things shaped like a phone number', () => {
    expect(phoneHref('+61 (7) 4948-3000')).toBe('tel:+61749483000')
    expect(phoneHref('ask at the office')).toBeNull()
    expect(phoneHref('javascript:1')).toBeNull()
  })
})

describe('marina card map data', () => {
  it('frames the outline and the pontoons', () => {
    const card = marinaCard()
    const bounds = marinaFrame(card)
    expect(bounds).not.toBeNull()
    const [[w, s], [e, n]] = bounds!
    expect(w).toBeLessThan(e)
    expect(s).toBeLessThan(n)
    // Coral Sea Marina is at 20.27 S, 148.71 E.
    expect(s).toBeLessThan(-20.26)
    expect(n).toBeGreaterThan(-20.27)
  })

  it('has nothing to frame without geometry', () => {
    expect(marinaFrame(marinaCardWithoutGeometry())).toBeNull()
    expect(shapesBounds([])).toBeNull()
  })

  it('writes GeoJSON in lon, lat order', () => {
    const card = marinaCard()
    const f = marinaFeatures(card)
    expect(f.pontoons.features).toHaveLength(card.geometry.pontoons.length)
    const first = (f.facilities.features[0].geometry as GeoJSON.Point).coordinates
    expect(first).toEqual([card.geometry.facilities[0].lon, card.geometry.facilities[0].lat])
    expect(f.ashore.features).toHaveLength(card.geometry.ashore.length)
  })

  it('labels only named pontoons that are not private', () => {
    const card = marinaCard()
    const named = card.geometry.pontoons.filter((p) => p.name !== '' && !p.private).length
    expect(marinaFeatures(card).pontoonLabels.features).toHaveLength(named)
    expect(named).toBeGreaterThan(0)
  })

  it('labels a two-node pontoon at the middle of the line, not at an end', () => {
    const base = marinaCard()
    const shape = { kind: 'line', points: [[-20.0, 148.0], [-20.0, 148.1]] }
    const f = marinaFeatures(marinaCard({ geometry: { ...base.geometry, pontoons: [{ name: 'A Arm', private: false, shape }] } }))
    const [lon, lat] = (f.pontoonLabels.features[0].geometry as GeoJSON.Point).coordinates
    expect(lat).toBeCloseTo(-20.0, 6)
    expect(lon).toBeCloseTo(148.05, 6)
  })

  it('has geometry to draw only for lines, polygons and pontoon lines', () => {
    const base = marinaCard()
    const geometry = (over: object) => marinaCard({ geometry: { ...base.geometry, outline: [], pontoons: [], ...over } })
    const point = { kind: 'point', points: [[-20.0, 148.0]] }
    const line = { kind: 'line', points: [[-20.0, 148.0], [-20.1, 148.1]] }
    expect(hasMarinaGeometry(geometry({ outline: [point] }))).toBe(false)
    expect(hasMarinaGeometry(geometry({ pontoons: [{ name: '', private: false, shape: point }] }))).toBe(false)
    expect(hasMarinaGeometry(geometry({ outline: [point, line] }))).toBe(true)
    expect(hasMarinaGeometry(geometry({ pontoons: [{ name: 'A', private: false, shape: line }] }))).toBe(true)
  })

  it('labels a facility point with its name, or its kind', () => {
    const f = marinaFeatures(marinaCard({ geometry: { outline: [], pontoons: [], ashore: [], facilities: [
      { kind: 'ramp', name: 'Boatramp', lat: -20.1, lon: 148.1 },
      { kind: 'haul_out', name: '', lat: -20.2, lon: 148.2 },
    ] } }))
    expect(f.facilities.features.map((x) => x.properties?.label)).toEqual(['Boatramp', 'Haul-out'])
  })
})
