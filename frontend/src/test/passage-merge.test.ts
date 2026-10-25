import { describe, expect, it } from 'vitest'

import { mergeBlocker } from '@/lib/passage-merge'

const p = (id: string, h0: number, h1: number, cruise_id: string | null = null, hidden = false) => ({
  id, start: new Date(Date.UTC(2026, 9, 24, h0)).toISOString(), end: new Date(Date.UTC(2026, 9, 24, h1)).toISOString(), cruise_id, hidden,
})

describe('mergeBlocker', () => {
  const a = p('a', 0, 2)
  const b = p('b', 3, 5)
  const c = p('c', 6, 8)
  it('allows neighbours', () => {
    expect(mergeBlocker([a, b], [a, b, c])).toBeNull()
    expect(mergeBlocker([c, a, b], [a, b, c])).toBeNull()
  })
  it('needs two', () => {
    expect(mergeBlocker([a], [a, b])).toMatch(/two or more/)
  })
  it('refuses when another passage sits between', () => {
    expect(mergeBlocker([a, c], [a, b, c])).toMatch(/not next to each other/)
  })
  it('refuses different cruises, allows one unfiled', () => {
    expect(mergeBlocker([p('a', 0, 2, 'x'), p('b', 3, 5, 'y')], [])).toMatch(/different cruises/)
    expect(mergeBlocker([p('a', 0, 2, null), p('b', 3, 5, 'y')], [])).toBeNull()
  })
  it('refuses a hidden passage', () => {
    expect(mergeBlocker([a, p('b', 3, 5, null, true)], [])).toMatch(/hidden/)
  })
})
