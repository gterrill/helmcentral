import { describe, expect, it } from 'vitest'

import {
  anchorageFeatures,
  depthLines,
  swingRing,
  anchorageFrame,
  footerLines,
  groupMoorings,
  lengthText,
  restrictedLine,
  roseLabel,
  roseSectors,
  sizeText,
  timeText,
  waveText,
  windText,
} from '@/lib/anchorage-card'

import { destinationPoint } from '@/lib/geo'
import { distanceText } from '@/lib/marina-card'

import { anchorageCard, anchorageCardWithoutDepth, anchoragePayload, unknownDepth } from './helpers/anchorage-fixture'

describe('roseSectors', () => {
  it('has 36 sectors and sizes each spoke by its fetch up to 15 nm', () => {
    const card = anchorageCard()
    const sectors = roseSectors(card)
    expect(sectors).toHaveLength(36)
    const open = sectors.filter((s) => s.open)
    expect(open.length).toBeGreaterThan(0)
    expect(open.every((s) => s.share === 1)).toBe(true)
    const shortest = sectors.reduce((a, b) => (a.fetchNm < b.fetchNm ? a : b))
    expect(shortest.share).toBeCloseTo(Math.min(shortest.fetchNm, 15) / 15)
    expect(sectors[9]).toMatchObject({ bearingDeg: 90, from: 85, to: 95 })
  })

  it('caps a fetch beyond 15 nm', () => {
    const card = anchorageCard()
    card.shelter.bearings[0] = { ...card.shelter.bearings[0], fetchNm: 40, open: false }
    expect(roseSectors(card)[0].share).toBe(1)
  })

  it('labels the rose with the arcs, for assistive tech', () => {
    const card = anchorageCard()
    expect(roseLabel(card)).toContain('Sheltered from')
    expect(roseLabel(card)).toContain('Shelter by the direction the wind comes from')
    const bare = anchorageCard()
    bare.shelter.summary = ''
    expect(roseLabel(bare)).toMatch(/Sheltered .*\(/)
  })
})

describe('units and times', () => {
  it('shows lengths in the operator units', () => {
    expect(lengthText(0.8, 'metric')).toBe('0.8 m')
    expect(lengthText(0.8, 'imperial')).toBe('2.6 ft')
    expect(sizeText(20, 'metric')).toBe('20 m')
    expect(sizeText(20, 'imperial')).toBe('66 ft')
    expect(distanceText(90, 'metric')).toBe('90 m')
    expect(distanceText(2400, 'metric')).toBe('2.4 km')
    expect(distanceText(1852, 'imperial')).toBe('1.0 nm')
  })

  it('reads times in the anchorage zone', () => {
    expect(timeText('2026-11-02T05:00:00Z', 'Australia/Lindeman')).toBe('Mon 15:00')
  })

  it('writes wind, gust and waves, leaving out what the row does not have', () => {
    const [row] = anchorageCard().forecast.rows
    expect(windText(row)).toBe('15 g19')
    expect(waveText(row, 'metric')).toBe('0.8 m 5 s SE')
    expect(waveText({ ...row, waveM: null }, 'metric')).toBe('--')
    expect(waveText({ ...row, waveS: null, waveDir: '' }, 'imperial')).toBe('2.6 ft')
    expect(windText({ ...row, windKts: null })).toBe('--')
  })
})

describe('moorings and restrictions', () => {
  it('groups moorings by what the record says and marks the untagged', () => {
    const card = anchorageCard()
    const groups = groupMoorings(card.features.moorings, 'metric')
    expect(groups.reduce((n, g) => n + g.count, 0)).toBe(23)
    expect(groups.every((g) => !g.untagged && /^(Up to \d+ m|Size limit not tagged) · class \S+$/.test(g.label))).toBe(true)
    expect(groups.map((g) => g.label)).toContain('Size limit not tagged · class D')
    expect(groups.map((g) => g.nearestM)).toEqual([...groups.map((g) => g.nearestM)].sort((a, b) => a - b))
    const untagged = [...card.features.moorings, { ...card.features.moorings[0], vesselSizeM: null, class: '', detailsUntagged: true, distanceM: 5 }]
    const g = groupMoorings(untagged, 'imperial')
    expect(g[0]).toEqual({ label: 'Details not tagged', count: 1, nearestM: 5, untagged: true })
    expect(g[1].label).toMatch(/^(Up to \d+ ft|Size limit not tagged) · class/)
  })

  it('words a restriction without claiming what the map does not say', () => {
    const base = { osm: 'way/1', name: 'Reef zone', source: 'seamark' as const, restriction: 'anchoring_prohibited', category: 'nature_reserve', distanceM: 120, prohibitsAnchoring: true, shapes: [] }
    expect(restrictedLine(base, 'metric')).toEqual({ label: 'Reef zone', detail: 'Anchoring prohibited · Nature reserve · 120 m', tone: 'critical' })
    expect(restrictedLine({ ...base, restriction: 'anchoring_restricted', prohibitsAnchoring: false, distanceM: 0 }, 'metric')).toMatchObject({
      detail: 'Anchoring restricted · Nature reserve · you are inside it',
      tone: 'warning',
    })
    const park = restrictedLine({ ...base, source: 'protected_area', restriction: 'Marine Park', prohibitsAnchoring: false, distanceM: null }, 'metric')
    expect(park.tone).toBe('muted')
    expect(park.detail).toContain('check the zoning')
    expect(park.detail).not.toMatch(/prohibited/)
    // A stored card may still carry the raw protect_class list.
    const coded = restrictedLine({ ...base, source: 'protected_area', restriction: 'Marine Park', category: '1;2;4;6', prohibitsAnchoring: false, distanceM: null }, 'metric')
    expect(coded.detail).not.toContain('1;2;4;6')
    expect(coded.detail).not.toMatch(/\d;\d/)
  })
})

describe('footerLines', () => {
  it('dates the forecast, the coastline and the map', () => {
    expect(footerLines(anchorageCard())).toEqual([
      'Forecast fetched Mon 11:00',
      'Coastline downloaded 3 Oct 2026',
      'OpenStreetMap · data as of 31 Oct 2026',
    ])
  })

  it('names only OpenStreetMap when it gave no date, and leaves out an unknown forecast time', () => {
    const card = anchorageCard({ data_as_of: '', forecast: { ...(anchoragePayload().forecast as object), fetched_at: '' } })
    expect(footerLines(card).at(-1)).toBe('OpenStreetMap')
    expect(footerLines(card).some((l) => l.startsWith('Forecast fetched'))).toBe(false)
  })
})

describe('the map', () => {
  it('casts 36 rays from the anchorage, each as long as its fetch', () => {
    const card = anchorageCard()
    const { rays } = anchorageFeatures(card, 'metric')
    expect(rays.features).toHaveLength(36)
    const east = rays.features[9]
    expect(east.properties).toMatchObject({ bearing: 90, grade: card.shelter.bearings[9].grade })
    const [, end] = (east.geometry as GeoJSON.LineString).coordinates
    const lat = card.lat
    const metres = ((end[0] - card.lon) * Math.PI / 180) * 6371000 * Math.cos((lat * Math.PI) / 180)
    expect(metres / 1852).toBeCloseTo(card.shelter.bearings[9].open ? 15 : card.shelter.bearings[9].fetchNm, 0)
  })

  it('draws moorings with their size limit as the label, and no shoreline', () => {
    const f = anchorageFeatures(anchorageCard(), 'metric')
    expect(f.moorings.features).toHaveLength(23)
    expect(f.moorings.features[0].properties).toEqual({ label: `${anchorageCard().features.moorings[0].vesselSizeM} m` })
    expect(Object.keys(f)).not.toContain('shoreline')
  })

  it('fills restricted polygons and outlines the areas', () => {
    const card = anchorageCard()
    card.features.restrictedAreas.push({
      osm: 'way/1', name: 'Zone', source: 'seamark', restriction: 'anchoring_prohibited', category: '', distanceM: 5, prohibitsAnchoring: true,
      shapes: [{ kind: 'polygon', points: [[-20, 148], [-20, 148.01], [-20.01, 148.01], [-20, 148]] }],
    })
    card.features.anchorageAreas.push({ osm: 'way/2', name: 'A', category: '', distanceM: 9, shapes: [{ kind: 'point', points: [[-20.07, 148.92]] }] })
    const f = anchorageFeatures(card, 'metric')
    expect(f.restrictedAreas.features).toHaveLength(1)
    expect(f.restrictedAreas.features[0].geometry.type).toBe('Polygon')
    expect(f.restrictedAreas.features[0].properties).toMatchObject({ prohibits: true })
    expect(f.anchorageAreas.features[0].geometry.type).toBe('Point')
  })

  it('frames the anchorage and one nautical mile round it', () => {
    const card = anchorageCard()
    const [[w, s], [e, n]] = anchorageFrame(card)
    expect(s).toBeLessThan(card.lat - 0.016)
    expect(n).toBeGreaterThan(card.lat + 0.016)
    expect(w).toBeLessThan(card.lon)
    expect(e).toBeGreaterThan(card.lon)
  })

  it('is not widened by a large area, which is drawn and may run off the map', () => {
    const card = anchorageCard()
    const before = anchorageFrame(card)
    card.features.restrictedAreas.push({
      osm: 'relation/9', name: 'Huge', source: 'seamark', restriction: 'anchoring_prohibited', category: '', distanceM: 0, prohibitsAnchoring: true,
      shapes: [{ kind: 'polygon', points: [[-19, 147], [-19, 150], [-21, 150], [-21, 147], [-19, 147]] }],
    })
    card.features.seabedAreas.push({ osm: 'way/3', name: '', surface: 'sand', distanceM: 400, shapes: [{ kind: 'line', points: [[-20.2, 148.9], [-20.21, 149.2]] }] })
    expect(anchorageFrame(card)).toEqual(before)
  })

  it('frames the moorings it draws', () => {
    const card = anchorageCard()
    const [[w, s], [e, n]] = anchorageFrame(card)
    for (const m of card.features.moorings) {
      expect(m.lat).toBeGreaterThanOrEqual(s)
      expect(m.lat).toBeLessThanOrEqual(n)
      expect(m.lon).toBeGreaterThanOrEqual(w)
      expect(m.lon).toBeLessThanOrEqual(e)
    }
    const [lat, lon] = destinationPoint(-20, 148, 0, 1852 * 60)
    expect(lat).toBeCloseTo(-19, 1)
    expect(lon).toBeCloseTo(148, 3)
  })
})

describe('depthLines', () => {
  it('words each check from the figures, in the operator units', () => {
    const lines = depthLines(anchorageCard(), 'metric')
    expect(lines.map((l) => l.key)).toEqual(['depth', 'clearance', 'rode', 'swing'])
    expect(lines[0]).toMatchObject({ badge: '4 m', text: '4 m at chart datum, from you' })
    expect(lines[1]).toMatchObject({ badge: 'OK', tone: 'good' })
    expect(lines[1].text).toMatch(/4\.8 m of water, 3\.4 m under a 1\.4 m draft\. You want at least 0\.5 m\./)
    expect(lines[2]).toMatchObject({ badge: 'Short by 20.9 m', tone: 'critical' })
    expect(lines[2].text).toMatch(/About 60\.9 m of rode \(7:1, ratio\) against 40 m aboard/)
    expect(lines[2].detail).toBe('Planned at 20 kts, the strongest forecast gust in the stay rounded up to its band.')
    expect(lines[3]).toMatchObject({ badge: '56 m', tone: 'neutral' })
    expect(lines[3].text).toMatch(/^With all 40 m of chain out, short of the 60\.9 m needed, the boat swings within about 56 m of the anchor/)
  })

  it('writes feet when the operator reads imperial', () => {
    const lines = depthLines(anchorageCard(), 'imperial')
    expect(lines[0].text).toBe('13.1 ft at chart datum, from you')
    expect(lines[3].badge).toBe('183.7 ft')
  })

  it('says where a saved place depth came from', () => {
    const base = anchoragePayload().depth as object
    const card = anchorageCard({ depth: { ...base, source: 'saved_place', saved_place: 'Butterfly Bay', saved_source: 'sounded', saved_date: '2026-10-20' } })
    expect(depthLines(card, 'metric')[0].text).toBe('4 m at chart datum, from saved place Butterfly Bay (sounded 2026-10-20)')
  })

  it('calls a too-shallow low water what it is', () => {
    const base = anchoragePayload().depth as { clearance: object }
    const card = anchorageCard({ depth: { ...base, clearance: { ...base.clearance, status: 'too_shallow', clearance_m: 0.4 } } })
    expect(depthLines(card, 'metric')[1]).toMatchObject({ badge: 'Too shallow', tone: 'critical' })
  })

  it('says how far the wind was checked when the forecast ends before the stay', () => {
    const base = anchoragePayload().depth as { rode: object }
    const card = anchorageCard({ depth: { ...base, rode: { ...base.rode, wind_checked_until: '2026-11-02T23:00:00+10:00' } } })
    expect(depthLines(card, 'metric')[2].detail).toMatch(/before the stay does, so the wind was only checked up to then\.$/)
  })

  it('words the swing for the rode needed when the chain is enough', () => {
    const base = anchoragePayload().depth as { swing: object }
    const card = anchorageCard({ depth: { ...base, swing: { ...base.swing, all_chain_out: false, rode_m: 60.9, radius_m: 76.9 } } })
    expect(depthLines(card, 'metric')[3].text).toBe("The boat swings within about 76.9 m of the anchor: 60.9 m of rode, 3 m from bow to GPS and the boat's 13 m length.")
  })

  it('with no depth says so and skips every check with its reason, never as passed', () => {
    const lines = depthLines(anchorageCardWithoutDepth(), 'metric')
    expect(lines[0]).toMatchObject({ badge: 'Unknown', text: 'Depth unknown: check the chart.' })
    for (const l of lines.slice(1)) {
      expect(l).toMatchObject({ badge: 'Skipped', tone: 'muted', text: 'Skipped: the depth is unknown.' })
    }
  })

  it('shows a saved-places error in place of a depth', () => {
    const depth = { ...unknownDepth(), error: 'Depth unavailable: disk full' }
    expect(depthLines(anchorageCard({ depth }), 'metric')[0]).toMatchObject({ badge: 'Unknown', text: 'Depth unavailable: disk full' })
  })
})

describe('the swing circle', () => {
  it('is a closed ring at the computed radius round the anchorage', () => {
    const card = anchorageCard()
    const ring = swingRing(card)!
    expect(ring).toHaveLength(65)
    expect(ring[0]).toEqual(ring[64])
    for (const [lon, lat] of ring.slice(0, 64)) {
      const dLat = ((lat - card.lat) * Math.PI) / 180
      const dLon = ((lon - card.lon) * Math.PI) / 180
      const m = Math.hypot(dLat * 6371000, dLon * 6371000 * Math.cos((card.lat * Math.PI) / 180))
      expect(m).toBeCloseTo(56, 0)
    }
    const swing = anchorageFeatures(card, 'metric').swing
    expect(swing.features).toHaveLength(1)
    expect(swing.features[0].geometry.type).toBe('Polygon')
  })

  it('is not drawn when no radius was computed', () => {
    const card = anchorageCardWithoutDepth()
    expect(swingRing(card)).toBeNull()
    expect(anchorageFeatures(card, 'metric').swing.features).toHaveLength(0)
  })
})
