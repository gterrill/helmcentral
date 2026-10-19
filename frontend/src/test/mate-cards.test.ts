import { describe, expect, it } from 'vitest'

import { decodeMateCards } from '@/lib/mate-cards'

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
