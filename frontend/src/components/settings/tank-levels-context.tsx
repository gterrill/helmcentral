import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'

import { saveTankLevelDefaults, useTankLevelDefaults } from '@/hooks/use-tank-levels'
import {
  TANK_DEFAULT_KINDS,
  parseLevelText,
  tankDefaultsError,
  type TankKind,
  type TankLevelDefaults,
} from '@/lib/tank-levels'

/** One kind's pair as typed. Text, so a half-typed number is not rewritten under the cursor. */
export type TankLevelsDraft = Record<TankKind, { warn: string; alarm: string }>

export interface TankLevelsFormValue {
  /** null until the defaults have been read from the server. */
  draft: TankLevelsDraft | null
  setLevel: (kind: TankKind, field: 'warn' | 'alarm', text: string) => void
  /** Why each kind's pair cannot be saved yet, by kind. */
  errors: Partial<Record<TankKind, string>>
  loadError: string | null
  dirty: boolean
  save: () => Promise<void>
  reset: () => void
}

const TankLevelsContext = createContext<TankLevelsFormValue | null>(null)

function toDraft(defaults: TankLevelDefaults): TankLevelsDraft {
  const text = (v: number) => String(v)
  return {
    fuel: { warn: text(defaults.fuel.warn), alarm: text(defaults.fuel.alarm) },
    water: { warn: text(defaults.water.warn), alarm: text(defaults.water.alarm) },
    waste: { warn: text(defaults.waste.warn), alarm: text(defaults.waste.alarm) },
  }
}

function draftsEqual(a: TankLevelsDraft | null, b: TankLevelsDraft | null): boolean {
  if (a === null || b === null) return a === b
  return TANK_DEFAULT_KINDS.every(({ kind }) => a[kind].warn === b[kind].warn && a[kind].alarm === b[kind].alarm)
}

export function tankLevelErrors(draft: TankLevelsDraft | null): Partial<Record<TankKind, string>> {
  const errors: Partial<Record<TankKind, string>> = {}
  if (draft === null) return errors
  for (const { kind } of TANK_DEFAULT_KINDS) {
    const message = tankDefaultsError(kind, draft[kind].warn, draft[kind].alarm)
    if (message !== null) errors[kind] = message
  }
  return errors
}

/**
 * Owns the per-kind tank levels draft for the settings page, in the same shape
 * as VesselParticularsProvider beside it: mounted for the whole page so an edit
 * counts toward the page's one dirty signal and one Save bar. The levels live
 * behind their own endpoint, so this is a further request inside one save.
 */
export function TankLevelsProvider({ children }: { children: ReactNode }) {
  const { defaults, error } = useTankLevelDefaults()
  const [draft, setDraft] = useState<TankLevelsDraft | null>(null)
  const [savedSnapshot, setSavedSnapshot] = useState<TankLevelsDraft | null>(null)

  useEffect(() => {
    if (defaults === null) return
    const next = toDraft(defaults)
    setDraft(next)
    setSavedSnapshot(next)
  }, [defaults])

  const setLevel = useCallback((kind: TankKind, field: 'warn' | 'alarm', text: string) => {
    setDraft((previous) => (previous === null ? previous : { ...previous, [kind]: { ...previous[kind], [field]: text } }))
  }, [])

  const reset = useCallback(() => setDraft(savedSnapshot), [savedSnapshot])

  const dirty = !draftsEqual(draft, savedSnapshot)
  const errors = useMemo(() => tankLevelErrors(draft), [draft])

  // Untouched means no request, and a draft that never loaded is never written.
  // A pair the server would refuse is refused here first, so the Save bar can
  // say which kind.
  const persist = useCallback(async () => {
    if (!dirty || draft === null) return
    const first = TANK_DEFAULT_KINDS.find(({ kind }) => errors[kind] !== undefined)
    if (first) throw new Error(`${first.label} tanks: ${errors[first.kind]}`)
    const toNumbers = (kind: TankKind) => ({ warn: parseLevelText(draft[kind].warn) as number, alarm: parseLevelText(draft[kind].alarm) as number })
    await saveTankLevelDefaults({ fuel: toNumbers('fuel'), water: toNumbers('water'), waste: toNumbers('waste') })
    setSavedSnapshot(draft)
  }, [dirty, draft, errors])

  const value = useMemo<TankLevelsFormValue>(
    () => ({ draft, setLevel, errors, loadError: error, dirty, save: persist, reset }),
    [draft, setLevel, errors, error, dirty, persist, reset],
  )

  return <TankLevelsContext.Provider value={value}>{children}</TankLevelsContext.Provider>
}

export function useTankLevelsFormContext(): TankLevelsFormValue {
  const context = useContext(TankLevelsContext)
  if (!context) {
    throw new Error('useTankLevelsFormContext must be used within a TankLevelsProvider')
  }
  return context
}
