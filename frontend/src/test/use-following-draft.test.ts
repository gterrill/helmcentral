import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'

import { useFollowingDraft } from '@/hooks/use-following-draft'

interface Rec { id: string; title: string; notes: string; tags: string[] }
interface Draft { title: string; notes: string; tags: string[] }
const toDraft = (r: Rec): Draft => ({ title: r.title, notes: r.notes, tags: r.tags })
const rec = (o: Partial<Rec> = {}): Rec => ({ id: 'a', title: 'T', notes: 'N', tags: [], ...o })

function setup(initial: Rec | null, options?: Parameters<typeof useFollowingDraft<Rec, Draft, Draft | null>>[3]) {
  return renderHook(({ record }: { record: Rec | null }) => useFollowingDraft(record, toDraft, null as Draft | null, options), {
    initialProps: { record: initial },
  })
}

describe('useFollowingDraft', () => {
  it('seeds from the first record', () => {
    const { result } = setup(rec())
    expect(result.current[0]).toEqual({ title: 'T', notes: 'N', tags: [] })
  })

  it('is the initial value until a record arrives', () => {
    const { result, rerender } = setup(null)
    expect(result.current[0]).toBeNull()
    rerender({ record: rec() })
    expect(result.current[0]?.title).toBe('T')
  })

  it('takes a changed field from a fresher record of the same id when the operator has not edited it', () => {
    const { result, rerender } = setup(rec())
    rerender({ record: rec({ title: 'Suggested' }) })
    expect(result.current[0]?.title).toBe('Suggested')
  })

  it('keeps the fields the operator edited and follows the others', () => {
    const { result, rerender } = setup(rec())
    act(() => result.current[1]((d) => ({ ...d!, notes: 'mine' })))
    rerender({ record: rec({ title: 'Suggested', notes: 'server notes', tags: ['x'] }) })
    expect(result.current[0]).toEqual({ title: 'Suggested', notes: 'mine', tags: ['x'] })
  })

  it("treats an edited field as the operator's even when a different value arrives", () => {
    const { result, rerender } = setup(rec())
    act(() => result.current[1]((d) => ({ ...d!, tags: ['a'] })))
    rerender({ record: rec({ tags: ['b'] }) })
    expect(result.current[0]?.tags).toEqual(['a'])
  })

  it('reseeds completely for a different record id', () => {
    const { result, rerender } = setup(rec())
    act(() => result.current[1]((d) => ({ ...d!, notes: 'mine' })))
    rerender({ record: rec({ id: 'b', title: 'Other', notes: 'theirs' }) })
    expect(result.current[0]).toEqual({ title: 'Other', notes: 'theirs', tags: [] })
  })

  it('forgets the seed when the record goes away', () => {
    const { result, rerender } = setup(rec())
    rerender({ record: null })
    rerender({ record: rec({ title: 'Back again' }) })
    expect(result.current[0]?.title).toBe('Back again')
  })

  // The operator adds a tag, saves, and the server echoes the set back in its
  // own order. The draft then differs from the new seed only in order.
  function saveWithReorderedEcho(options?: Parameters<typeof setup>[1]) {
    const view = setup(rec({ tags: ['a', 'b'] }), options)
    act(() => view.result.current[1]((d) => ({ ...d!, tags: ['a', 'b', 'c'] })))
    view.rerender({ record: rec({ tags: ['c', 'a', 'b'] }) })
    view.rerender({ record: rec({ tags: ['c', 'a', 'b', 'd'] }) })
    return view.result.current[0]?.tags
  }

  it('compares a list field as a set when it is declared unordered, so a reordered echo is not an edit', () => {
    expect(saveWithReorderedEcho({ unordered: ['tags'] })).toEqual(['c', 'a', 'b', 'd'])
  })

  it('without the declaration a reordered echo leaves the field counted as edited', () => {
    expect(saveWithReorderedEcho()).toEqual(['a', 'b', 'c'])
  })

  it('keeps a declared group together: one edited field keeps its whole group', () => {
    interface Loc { id: string; zone: string | null; bin: string | null; name: string }
    const { result, rerender } = renderHook(
      ({ record }: { record: Loc }) => useFollowingDraft(record, (r: Loc) => ({ zone: r.zone, bin: r.bin, name: r.name }), null, { groups: [['zone', 'bin']] }),
      { initialProps: { record: { id: 'a', zone: 'z1', bin: 'b1', name: 'N' } } },
    )
    act(() => result.current[1]((d) => ({ ...d!, zone: 'z2' })))
    rerender({ record: { id: 'a', zone: 'z3', bin: 'b3', name: 'M' } })
    // zone was edited, so bin keeps the draft's too; the ungrouped name follows.
    expect(result.current[0]).toEqual({ zone: 'z2', bin: 'b1', name: 'M' })
  })

  it('keeps the same draft object when a fresher record changes nothing the draft shows', () => {
    const { result, rerender } = setup(rec({ tags: ['a'] }))
    const before = result.current[0]
    rerender({ record: rec({ tags: ['a'] }) })
    expect(result.current[0]).toBe(before)
  })
})
