import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

import { decodeMateCards } from '@/lib/mate-cards'

import { passageWindowPayload } from './helpers/passage-window-fixture'

describe('decodeMateCards', () => {
  it('reads an absent field as no cards', () => {
    expect(decodeMateCards(undefined)).toEqual([])
  })

  it('reads an empty list as no cards', () => {
    expect(decodeMateCards([])).toEqual([])
  })

  it('turns a non-array into one error entry rather than throwing', () => {
    for (const raw of [{ kind: 'x' }, 'cards', null]) {
      const cards = decodeMateCards(raw)
      expect(cards).toHaveLength(1)
      expect(cards[0]).toMatchObject({ kind: 'error' })
      expect(cards[0].kind === 'error' && cards[0].reason).toMatch(/array/)
    }
  })

  it('keeps a kind it does not know as an error entry carrying that kind', () => {
    const cards = decodeMateCards([{ kind: 'mystery', payload: {} }])
    expect(cards).toEqual([{ kind: 'error', reason: expect.stringMatching(/mystery/), rawKind: 'mystery' }])
  })

  it('turns an element without a kind into an error entry, per element', () => {
    const cards = decodeMateCards([42, { payload: {} }])
    expect(cards).toHaveLength(2)
    for (const c of cards) {
      expect(c.kind).toBe('error')
      expect(c.kind === 'error' && c.rawKind).toBeUndefined()
    }
  })
})

// A loosely typed payload the tests poke holes in.
type PayloadShape = ReturnType<typeof JSON.parse>

describe('decodeMateCards: passage_window', () => {
  const wrap = (payload: unknown) => [{ kind: 'passage_window', payload }]
  const reasonOf = (raw: unknown) => {
    const c = decodeMateCards(raw)[0]
    expect(c.kind).toBe('error')
    return c.kind === 'error' ? c.reason : ''
  }
  const mutate = (fn: (p: PayloadShape) => void) => {
    const p = passageWindowPayload()
    fn(p)
    return wrap(p)
  }

  it('accepts the contract example', () => {
    const cards = decodeMateCards(wrap(passageWindowPayload()))
    expect(cards).toHaveLength(1)
    const c = cards[0]
    expect(c.kind).toBe('passage_window')
    if (c.kind !== 'passage_window') return
    expect(c.timezone).toBe('Australia/Brisbane')
    expect(c.legs).toEqual([
      { name: 'Townsville → Cape Upstart', stayNightsAfter: 2 },
      { name: 'Cape Upstart → Bowen', stayNightsAfter: null },
    ])
    expect(c.rows[0].cells[0].limiting).toMatchObject({ factor: 'wind', sector: 'ahead' })
    expect(c.rows[1].cells[1].note).toBe('No forecast this far ahead')
    expect(c.rows[0].cells[1].limiting).toBeNull()
  })

  it('turns a bad state enum into an error entry naming the field', () => {
    expect(reasonOf(mutate((p) => { p.rows[0].cells[0].state = 'fine' }))).toMatch(/state/)
  })

  it('turns a bad verdict into an error entry', () => {
    expect(reasonOf(mutate((p) => { p.rows[0].verdict = 'maybe' }))).toMatch(/verdict/)
  })

  it('turns missing rows into an error entry', () => {
    expect(reasonOf(mutate((p) => { delete p.rows }))).toMatch(/rows/)
  })

  it('turns a non-number leg index into an error entry', () => {
    expect(reasonOf(mutate((p) => { p.rows[0].cells[0].leg = '0' }))).toMatch(/leg/)
  })

  it('rejects a leg index outside the legs', () => {
    expect(reasonOf(mutate((p) => { p.rows[0].cells[0].leg = 5 }))).toMatch(/leg/)
  })

  it('rejects an unknown limiting factor and an unreadable timestamp', () => {
    expect(reasonOf(mutate((p) => { p.rows[0].cells[0].limiting.factor = 'swell' }))).toMatch(/factor/)
    expect(reasonOf(mutate((p) => { p.forecast_fetched_at = 'yesterday' }))).toMatch(/forecast_fetched_at/)
  })

  it('rejects a bad not_checked value and a bad stay', () => {
    expect(reasonOf(mutate((p) => { p.rows[0].cells[0].not_checked = ['tide'] }))).toMatch(/not_checked/)
    expect(reasonOf(mutate((p) => { p.legs[0].stay_nights_after = '2' }))).toMatch(/stay_nights_after/)
  })

  it('accepts wave_direction as a not-checked measure and still reads cards saved without it', () => {
    const withDirection = decodeMateCards(mutate((p) => { p.rows[0].cells[0].not_checked = ['wave', 'wave_direction'] }))
    const card = withDirection[0]
    expect(card.kind).toBe('passage_window')
    if (card.kind !== 'passage_window') return
    expect(card.rows[0].cells[0].notChecked).toEqual(['wave', 'wave_direction'])
    // A card stored before the measure existed has only the old values.
    expect(decodeMateCards(wrap(passageWindowPayload()))[0]).toMatchObject({ kind: 'passage_window' })
  })

  it('rejects a missing payload and keeps the other cards', () => {
    const cards = decodeMateCards([{ kind: 'passage_window' }, ...wrap(passageWindowPayload())])
    expect(cards[0].kind).toBe('error')
    expect(cards[1].kind).toBe('passage_window')
  })
})

describe('decodeMateCards against the backend golden fixture', () => {
  it('decodes the passage_window and telemetry_chart cards the Go side writes into message.sse', () => {
    const sse = readFileSync(join(__dirname, '../../../backend/testdata/mate_events/message.sse'), 'utf8')
    const data = sse.split('\n').find((l) => l.startsWith('data: '))!.slice('data: '.length)
    const cards = decodeMateCards(JSON.parse(data).message.cards)
    expect(cards.map((c) => c.kind)).toEqual(['passage_window', 'telemetry_chart'])
  })

  describe('passage decision', () => {
    const wire = { id: 5, state: 'went', row_date: '2026-10-24', inferred: true, passage_id: 'p9' }
    const entry = (extra: Record<string, unknown>) => [{ kind: 'passage_window', payload: passageWindowPayload(), ...extra }]

    it('reads the card id and the decision beside the payload', () => {
      const [card] = decodeMateCards(entry({ id: 12, decision: wire }))
      expect(card).toMatchObject({
        kind: 'passage_window',
        cardId: 12,
        decision: { id: 5, state: 'went', rowDate: '2026-10-24', inferred: true, passageId: 'p9' },
      })
    })

    it('reads a blank decision with null date and passage', () => {
      const [card] = decodeMateCards(entry({ id: 12, decision: { id: 5, state: '', row_date: null, inferred: false, passage_id: null } }))
      expect(card).toMatchObject({ decision: { state: '', rowDate: null, passageId: null } })
    })

    it('accepts an old card with neither id nor decision', () => {
      const [card] = decodeMateCards(entry({}))
      expect(card.kind).toBe('passage_window')
      expect(card.kind === 'passage_window' && card.decision).toBeUndefined()
      expect(card.kind === 'passage_window' && card.cardId).toBeUndefined()
    })

    it('turns a decision on a card with no id into the card error entry', () => {
      const [card] = decodeMateCards(entry({ decision: wire }))
      expect(card).toMatchObject({ kind: 'error', rawKind: 'passage_window' })
    })

    it('turns a malformed decision into the card error entry', () => {
      for (const decision of [
        'went',
        { ...wire, state: 'sailed' },
        { ...wire, row_date: 'Thursday' },
        { ...wire, inferred: 'yes' },
        { ...wire, id: 'x' },
        { ...wire, state: 'went', row_date: null },
        { ...wire, state: 'went', row_date: '2026-11-01' },
      ]) {
        const [card] = decodeMateCards(entry({ id: 12, decision }))
        expect(card).toMatchObject({ kind: 'error', rawKind: 'passage_window' })
        expect(card.kind === 'error' && card.reason).toMatch(/decision/)
      }
    })
  })
})
