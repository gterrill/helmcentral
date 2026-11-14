import { describe, expect, it } from 'vitest'
import { matchesAllWords } from '@/lib/word-match'

describe('matchesAllWords', () => {
  it('needs every word, in any order, ignoring case', () => {
    expect(matchesAllWords('Raw water impeller Jabsco', 'jabsco IMPELLER')).toBe(true)
    expect(matchesAllWords('Raw water impeller', 'jabsco impeller')).toBe(false)
  })
  it('matches everything for an empty query', () => {
    expect(matchesAllWords('anything', '  ')).toBe(true)
  })
})
