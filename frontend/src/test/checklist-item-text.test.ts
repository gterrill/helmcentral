import { describe, expect, it } from 'vitest'

import { normalizeChecklistItemText } from '@/lib/checklist-item-text'

// Plan "Notes and the Boat's Manual" §3: "The key is the item's plain text,
// not its raw Markdown ... Without this, opening a checklist in the editor
// and saving it could silently invalidate every tick in a live run." Phase 4
// (the backend checklist-run store) computes its own sha256 of a
// server-side equivalent of this same normalisation; this frontend copy
// exists so the EDITOR layer can be tested against the identical contract
// without depending on a Go package - see
// note-editor.test.tsx's ItemKeyIsStableAcrossAnEmphasisChange test, the one
// that actually proves the editor doesn't silently invalidate a tick.

describe('normalizeChecklistItemText', () => {
  it('strips a leading unchecked GFM marker', () => {
    expect(normalizeChecklistItemText('- [ ] Seacocks open')).toBe('Seacocks open')
  })

  it('strips a leading checked GFM marker', () => {
    expect(normalizeChecklistItemText('- [x] Seacocks open')).toBe('Seacocks open')
  })

  it('is unaffected by bolding a word - the whole point of this function', () => {
    const before = normalizeChecklistItemText('- [ ] Seacocks open')
    const after = normalizeChecklistItemText('- [ ] **Seacocks** open')
    expect(after).toBe(before)
  })

  it('is unaffected by italicising a word', () => {
    const before = normalizeChecklistItemText('- [ ] Seacocks open')
    const after = normalizeChecklistItemText('- [ ] Seacocks *open*')
    expect(after).toBe(before)
  })

  it('is unaffected by inline code marks', () => {
    const before = normalizeChecklistItemText('- [ ] Check the raw water strainer')
    const after = normalizeChecklistItemText('- [ ] Check the `raw water` strainer')
    expect(after).toBe(before)
  })

  it('collapses internal whitespace', () => {
    expect(normalizeChecklistItemText('- [ ] Seacocks   open')).toBe('Seacocks open')
  })

  it('trims leading and trailing whitespace', () => {
    expect(normalizeChecklistItemText('- [ ]   Seacocks open  ')).toBe('Seacocks open')
  })

  it('leaves plain text with no checkbox marker alone (aside from trimming)', () => {
    expect(normalizeChecklistItemText('Seacocks open')).toBe('Seacocks open')
  })
})
