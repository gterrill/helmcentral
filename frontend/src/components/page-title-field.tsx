import { useEffect, useRef, useState } from 'react'

export interface NameablePage {
  id: string
  name: string
}

interface PageTitleFieldProps {
  page: NameablePage
  /**
   * True for the page App.tsx's `namingPageId` names: the field just created
   * (ADR 0107) — starts empty with a placeholder and takes focus, instead of
   * showing the page's current name.
   */
  naming: boolean
  /** Resolves to whether the save actually succeeded (App.tsx's updatePage
   * resolves the saved page, or null on a rejected PATCH — this reports
   * that outcome as a plain boolean) so a rejected name is never recorded
   * as sent. */
  onSave: (id: string, name: string) => Promise<boolean>
  /** Called once editing settles — an Enter/blur that saved, or an Escape —
   * so App.tsx can clear namingPageId. A no-op the rest of the time. */
  onDone: () => void
}

/**
 * The page name field (ADR 0107): the first item in the layout toolbar, and
 * now the *only* way to rename a page — the page switcher's old pencil is
 * gone. A brand-new page starts here empty and focused rather than behind a
 * "name it first" dialog, so creating a page is one action, not two.
 *
 * `key={page.id}` at the call site (layout-toolbar.tsx) is load-bearing:
 * switching the active page has to reset this field's local draft back to
 * that page's own name, and a remount is the plain way to get that without a
 * synchronising effect racing the naming/focus effect below.
 */
export function PageTitleField({ page, naming, onSave, onDone }: PageTitleFieldProps) {
  const [value, setValue] = useState(naming ? '' : page.name)
  const inputRef = useRef<HTMLInputElement>(null)
  // Escape sets `value` back to page.name and then blurs; without this flag
  // that blur's own commit() would re-read `value` from its own (possibly
  // stale) closure and could save what Escape just reverted.
  const skipSaveRef = useRef(false)
  // The last name this field actually got saved. Enter blurs to let onBlur
  // do the one commit, but a second real blur event can still land before
  // the `page` prop comes back with the saved name — comparing against this
  // instead of only `page.name` keeps that second commit from re-sending
  // it. Only ever set once `onSave` reports success: recording it up front
  // (before the response came back) meant a name the backend rejected could
  // never be retried unchanged — the second, identical Enter matched this
  // ref and was silently swallowed instead of trying again.
  const lastSavedRef = useRef(page.name)
  // The name a save is currently in flight for, or null. This is what
  // actually stops the second-blur-right-behind-Enter case from double
  // sending: it's set synchronously before onSave's promise settles, so it
  // catches a duplicate commit that arrives before lastSavedRef could have
  // been updated by a success (or ever will be, on a failure).
  const pendingSaveRef = useRef<string | null>(null)

  useEffect(() => {
    if (naming) inputRef.current?.focus()
  }, [naming])

  const commit = () => {
    const skip = skipSaveRef.current
    skipSaveRef.current = false
    const trimmed = value.trim()
    if (!skip && trimmed && trimmed !== page.name && trimmed !== lastSavedRef.current && trimmed !== pendingSaveRef.current) {
      pendingSaveRef.current = trimmed
      void onSave(page.id, trimmed).then((saved) => {
        if (pendingSaveRef.current === trimmed) pendingSaveRef.current = null
        if (saved) lastSavedRef.current = trimmed
      })
    } else if (!trimmed) {
      // Nothing to save: show the page's real name instead of sitting blank
      // at whatever an aborted naming session left behind.
      setValue(page.name)
    }
    if (naming) onDone()
  }

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      e.preventDefault()
      // Blur so commit runs exactly once, from onBlur below — calling it
      // here too used to double-commit before page.name caught up.
      inputRef.current?.blur()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      skipSaveRef.current = true
      setValue(page.name)
      inputRef.current?.blur()
    }
  }

  return (
    <input
      ref={inputRef}
      type="text"
      value={value}
      placeholder="Page name"
      aria-label="Page name"
      onChange={(e) => setValue(e.target.value)}
      onBlur={commit}
      onKeyDown={handleKeyDown}
      className="min-w-0 w-40 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold text-foreground outline-hidden focus:border-primary/40 focus:ring-1 focus:ring-primary/40 sm:w-48"
    />
  )
}
