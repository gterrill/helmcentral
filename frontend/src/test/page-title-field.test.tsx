/**
 * The page name field (ADR 0107). `lastSavedRef` exists so a real second
 * blur landing right behind an Enter doesn't send the same name twice
 * before `page.name` has caught up — but it was recorded before `onSave`'s
 * promise had actually resolved. A save the backend rejected still counted
 * as "sent", so retyping the exact same name and pressing Enter again was
 * silently swallowed: the second attempt matched `lastSavedRef` and never
 * reached `onSave` at all.
 */
import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react'
import { PageTitleField, type NameablePage } from '@/components/page-title-field'

describe('PageTitleField', () => {
  const page: NameablePage = { id: 'p1', name: 'Untitled page' }

  it('retries a rejected save with the same name instead of swallowing it', async () => {
    const onSave = vi.fn()
      .mockResolvedValueOnce(false) // the backend rejects the rename
      .mockResolvedValueOnce(true)
    render(<PageTitleField page={page} naming={false} onSave={onSave} onDone={vi.fn()} />)
    const field = screen.getByLabelText('Page name') as HTMLInputElement

    // handleKeyDown's Enter path commits by calling inputRef.current.blur(),
    // which only fires a real blur event if the field is actually focused -
    // `naming` only auto-focuses a brand-new page's field, so an existing
    // page's field (this test) needs a real focus() first.
    field.focus()
    fireEvent.change(field, { target: { value: 'Anchored' } })
    fireEvent.keyDown(field, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(1))
    expect(onSave).toHaveBeenNthCalledWith(1, 'p1', 'Anchored')

    // Same field, same typed name, tried again after the rejection.
    field.focus()
    fireEvent.change(field, { target: { value: 'Anchored' } })
    fireEvent.keyDown(field, { key: 'Enter', code: 'Enter' })

    await waitFor(() => expect(onSave).toHaveBeenCalledTimes(2))
    expect(onSave).toHaveBeenNthCalledWith(2, 'p1', 'Anchored')
  })

  it('still sends only one PATCH for a blur landing right behind an Enter', async () => {
    let resolveSave!: (ok: boolean) => void
    const onSave = vi.fn(() => new Promise<boolean>((resolve) => { resolveSave = resolve }))
    render(<PageTitleField page={page} naming={false} onSave={onSave} onDone={vi.fn()} />)
    const field = screen.getByLabelText('Page name') as HTMLInputElement

    field.focus()
    fireEvent.change(field, { target: { value: 'Anchored' } })
    fireEvent.keyDown(field, { key: 'Enter', code: 'Enter' }) // blurs; commit runs from onBlur
    fireEvent.blur(field) // the extra blur that used to double-commit

    expect(onSave).toHaveBeenCalledTimes(1)

    await act(async () => {
      resolveSave(true)
      await Promise.resolve()
    })

    expect(onSave).toHaveBeenCalledTimes(1)
  })
})
