import { describe, expect, it, vi } from 'vitest'

import { recordPager } from '@/lib/record-pager'

const list = [{ id: 'a', name: 'Anchor' }, { id: 'b', name: 'Bilge' }, { id: 'c', name: 'Cabin' }]
const pager = (id: string, open = vi.fn()) => ({ open, pager: recordPager(list, id, (r) => r.id, (r) => r.name, open) })

describe('recordPager', () => {
  it('names each neighbour and opens it by id', () => {
    const { open, pager: p } = pager('b')
    expect(p.previous?.label).toBe('Anchor')
    expect(p.next?.label).toBe('Cabin')
    p.next?.onSelect()
    expect(open).toHaveBeenLastCalledWith('c')
    p.previous?.onSelect()
    expect(open).toHaveBeenLastCalledWith('a')
  })

  it('has nothing before the first or after the last', () => {
    expect(pager('a').pager.previous).toBeNull()
    expect(pager('c').pager.next).toBeNull()
  })

  it('has nothing either way for a record that is not in the list', () => {
    expect(pager('z').pager).toEqual({ previous: null, next: null })
  })
})
