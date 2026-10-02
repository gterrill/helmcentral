import { describe, expect, it } from 'vitest'

import { MATE_WAITING_PHRASES, pickWaitingPhrase } from '@/lib/mate-waiting-phrases'

describe('pickWaitingPhrase', () => {
  it('has 25 distinct phrases, each ending in an ellipsis', () => {
    expect(MATE_WAITING_PHRASES).toHaveLength(25)
    expect(new Set(MATE_WAITING_PHRASES).size).toBe(25)
    for (const phrase of MATE_WAITING_PHRASES) expect(phrase.endsWith('…')).toBe(true)
  })

  it('maps the injected random onto the list', () => {
    expect(pickWaitingPhrase(null, () => 0)).toBe(MATE_WAITING_PHRASES[0])
    expect(pickWaitingPhrase(null, () => 0.999999)).toBe(MATE_WAITING_PHRASES[MATE_WAITING_PHRASES.length - 1])
  })

  it('never returns the previous phrase, whatever the random value', () => {
    for (const previous of MATE_WAITING_PHRASES) {
      for (const r of [0, 0.1, 0.5, 0.9, 0.999999]) {
        expect(pickWaitingPhrase(previous, () => r)).not.toBe(previous)
      }
    }
  })
})
