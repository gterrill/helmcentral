import { describe, expect, it } from 'vitest'

import { decodeMateCards } from '@/lib/mate-cards'

import { marinaCard, marinaPayload } from './helpers/marina-fixture'

type Loose = ReturnType<typeof JSON.parse>

const reasonOf = (payload: unknown) => {
  const c = decodeMateCards([{ kind: 'marina', payload }])[0]
  expect(c.kind).toBe('error')
  return c.kind === 'error' ? c.reason : ''
}
const mutate = (fn: (p: Loose) => void) => {
  const p = JSON.parse(JSON.stringify(marinaPayload()))
  fn(p)
  return p
}

describe('decodeMateCards: marina', () => {
  it('reads the card the Go side writes for Coral Sea Marina', () => {
    const card = marinaCard()
    expect(card.name).toBe('Coral Sea Marina Resort')
    expect(card.berths).toBe(520)
    expect(card.contact.website).toBe('https://www.coralseamarina.com/berthing/')
    expect(card.pontoonArms).toContain('B&C Arm')
    expect(card.facilities.find((f) => f.kind === 'haul_out')?.facts).toEqual({ 'crane:type': 'travel_lift' })
    expect(card.vesselLimitCheck.map((c) => [c.particular, c.status])).toEqual([
      ['draft', 'not charted'],
      ['length', 'not charted'],
    ])
    expect(card.dataAsOf).toBe('2026-10-29T21:20:09Z')
    expect(card.geometry.outline).toHaveLength(1)
    expect(card.geometry.pontoons.length).toBeGreaterThan(0)
    expect(card.geometry.pontoons[0].shape.points[0]).toHaveLength(2)
  })

  it('accepts an unknown data date as empty', () => {
    expect(marinaCard({ data_as_of: '' }).dataAsOf).toBe('')
  })

  it('reads the card whether or not the payload carries a source', () => {
    expect(decodeMateCards([{ kind: 'marina', payload: mutate((p) => (p.source = 'x')) }])[0].kind).toBe('marina')
    expect(marinaCard()).not.toHaveProperty('source')
  })

  it('ignores a field it does not know, so a newer server can add one', () => {
    const p = mutate((p) => {
      p.surprise = 1
      p.contact.fax = '1'
      p.geometry.extra = []
      p.facilities[0].note = 'x'
    })
    expect(decodeMateCards([{ kind: 'marina', payload: p }])[0].kind).toBe('marina')
  })

  it('rejects a missing field', () => {
    expect(reasonOf(mutate((p) => delete p.facilities))).toMatch(/facilities/)
    expect(reasonOf(mutate((p) => delete p.data_as_of))).toMatch(/data_as_of/)
  })

  it('rejects a malformed field', () => {
    expect(reasonOf(mutate((p) => (p.berths = '520')))).toMatch(/berths/)
    expect(reasonOf(mutate((p) => (p.berths = -1)))).toMatch(/berths/)
    expect(reasonOf(mutate((p) => (p.data_as_of = 7)))).toMatch(/data_as_of/)
    expect(reasonOf(mutate((p) => (p.lat = 91)))).toMatch(/lat/)
    expect(reasonOf(mutate((p) => (p.facilities[0].count = -1)))).toMatch(/facilities\[0\]\.count/)
    expect(reasonOf(mutate((p) => (p.facilities[0].facts = { a: 1 })))).toMatch(/facts/)
    expect(reasonOf(mutate((p) => (p.truncated = 'no')))).toMatch(/truncated/)
    expect(reasonOf(mutate((p) => (p.geometry_truncated = 'no')))).toMatch(/geometry_truncated/)
    expect(reasonOf(mutate((p) => delete p.geometry_truncated))).toMatch(/geometry_truncated/)
  })

  it('rejects a limit check it cannot read', () => {
    expect(reasonOf(mutate((p) => (p.vessel_limit_check[0].status = 'maybe')))).toMatch(/status/)
    expect(reasonOf(mutate((p) => (p.vessel_limit_check[0].particular = 'beam')))).toMatch(/particular/)
    expect(reasonOf(mutate((p) => (p.vessel_limit_check[0].boat_m = 'x')))).toMatch(/boat_m/)
  })

  it('rejects shapes that cannot be drawn', () => {
    expect(reasonOf(mutate((p) => (p.geometry.outline[0].kind = 'circle')))).toMatch(/kind/)
    expect(reasonOf(mutate((p) => (p.geometry.pontoons[0].shape.points = [[-20, 148]])))).toMatch(/points/)
    expect(reasonOf(mutate((p) => (p.geometry.pontoons[0].shape.points[0] = [-20, 200])))).toMatch(/points/)
    expect(reasonOf(mutate((p) => (p.geometry.pontoons[0].private = 'no')))).toMatch(/private/)
    expect(reasonOf(mutate((p) => (p.geometry.ashore[0].lat = null)))).toMatch(/lat/)
  })

  it('turns a non-object payload into an error entry', () => {
    expect(reasonOf('marina')).toMatch(/object/)
  })
})
