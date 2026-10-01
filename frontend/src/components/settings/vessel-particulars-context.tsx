import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

import {
  ParticularsSaveError,
  saveVesselParticulars,
  useVesselParticulars,
  type VesselParticulars,
} from '@/hooks/use-vessel-particulars'

export interface VesselParticularsFormValue {
  /** null until the record has been read from the server. */
  draft: VesselParticulars | null
  update: (patch: Partial<VesselParticulars>) => void
  /** Why the server refused the last save, when it named a field of the record. */
  fieldError: { field: string; message: string } | null
  /** The load failure, if the record could not be read. */
  loadError: string | null
  dirty: boolean
  save: () => Promise<void>
  /** Puts the draft back to the last saved record (Discard). */
  reset: () => void
}

const VesselParticularsContext = createContext<VesselParticularsFormValue | null>(null)

// updated_at is the server's stamp, not something the operator edits, so it
// never counts as a change.
export function particularsEqual(a: VesselParticulars | null, b: VesselParticulars | null): boolean {
  if (a === null || b === null) return a === b
  const keys = Object.keys(a) as Array<keyof VesselParticulars>
  return keys.every((key) => key === 'updated_at' || a[key] === b[key])
}

/**
 * Owns the vessel particulars draft for the settings page, in the same shape
 * as AlarmTransportsProvider beside it: mounted for the whole page so an edit
 * counts toward the page's dirty signal and one Save bar, whichever section is
 * on screen. The record stays in its own table behind its own endpoint (the
 * import wizard writes it too), so this is a further request inside one save,
 * not part of the settings patch.
 */
export function VesselParticularsProvider({ children }: { children: ReactNode }) {
  const { particulars, error } = useVesselParticulars()
  const [draft, setDraft] = useState<VesselParticulars | null>(null)
  const [savedSnapshot, setSavedSnapshot] = useState<VesselParticulars | null>(null)
  const [fieldError, setFieldError] = useState<{ field: string; message: string } | null>(null)

  // Draft and snapshot move together when the server's copy arrives, so a
  // fresh fetch can't flash "dirty" at the page between the two.
  useEffect(() => {
    if (particulars === null) return
    setDraft(particulars)
    setSavedSnapshot(particulars)
  }, [particulars])

  const update = useCallback((patch: Partial<VesselParticulars>) => {
    setFieldError((current) => (current !== null && current.field in patch ? null : current))
    setDraft((previous) => (previous === null ? previous : { ...previous, ...patch }))
  }, [])

  const reset = useCallback(() => {
    setDraft(savedSnapshot)
    setFieldError(null)
  }, [savedSnapshot])

  const dirty = !particularsEqual(draft, savedSnapshot)

  // Untouched means no request, and a record that never loaded is never
  // written (it could only be blank, and would replace the stored one).
  // Snapshots what was sent as soon as the PUT succeeds, so Discard after a
  // sibling failure does not roll back behind the server; an edit made while
  // the save was in flight stays dirty.
  const persist = useCallback(async () => {
    if (!dirty || draft === null) return
    setFieldError(null)
    try {
      const saved = await saveVesselParticulars(draft)
      setSavedSnapshot({ ...draft, updated_at: saved.updated_at })
    } catch (err) {
      if (err instanceof ParticularsSaveError && err.field !== null) {
        setFieldError({ field: err.field, message: err.message })
      }
      throw err
    }
  }, [dirty, draft])

  const value = useMemo<VesselParticularsFormValue>(
    () => ({ draft, update, fieldError, loadError: error, dirty, save: persist, reset }),
    [draft, update, fieldError, error, dirty, persist, reset],
  )

  return <VesselParticularsContext.Provider value={value}>{children}</VesselParticularsContext.Provider>
}

export function useVesselParticularsFormContext(): VesselParticularsFormValue {
  const context = useContext(VesselParticularsContext)
  if (!context) {
    throw new Error('useVesselParticularsFormContext must be used within a VesselParticularsProvider')
  }
  return context
}
