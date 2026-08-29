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
  emptyTransportConfig,
  transportConfigsEqual,
  useAlarmTransports,
  type AlarmTransportConfig,
  type TransportSecretKey,
} from '@/hooks/use-alarm-transports'

export interface AlarmTransportsFormValue {
  draft: AlarmTransportConfig
  setSection: <K extends keyof AlarmTransportConfig>(key: K, value: Partial<AlarmTransportConfig[K]>) => void
  secrets: Partial<Record<TransportSecretKey, string>>
  setSecret: (key: TransportSecretKey, value: string) => void
  secretsPresent: Record<string, boolean>
  loading: boolean
  error: string | null
  dirty: boolean
  save: () => Promise<void>
  test: () => Promise<void>
  testResults: Record<string, string> | null
  testing: boolean
}

const AlarmTransportsFormContext = createContext<AlarmTransportsFormValue | null>(null)

/**
 * Owns the alarm-transports draft for the settings page, in the same shape
 * as SettingsFormProvider and SecretsStatusProvider beside it.
 *
 * The draft lives up here rather than inside the Notifications panel so the
 * page can fold it into its single dirty signal and its single Save button.
 * The panel used to carry its own Save, which put two near-identical buttons
 * on screen once it moved onto the settings page. The cost is that opening
 * Settings fetches the transport config even when the Alarms section is
 * never selected — two small GETs, and the price of the page knowing whether
 * it has unsaved work before you navigate away from it.
 *
 * The config still goes to /api/alarm-transports rather than the settings
 * endpoint (ADR 0038 §2), so this is a second request inside one save, not a
 * merge into the settings patch.
 */
export function AlarmTransportsProvider({ children }: { children: ReactNode }) {
  const { config, secretsPresent, loading, loaded, error, save, test, testResults, testing } = useAlarmTransports()
  const [draft, setDraft] = useState<AlarmTransportConfig>(emptyTransportConfig)
  const [savedSnapshot, setSavedSnapshot] = useState<AlarmTransportConfig>(emptyTransportConfig)
  const [secrets, setSecrets] = useState<Partial<Record<TransportSecretKey, string>>>({})

  // Re-seed only when the server's copy changes, so typing is never
  // clobbered. Draft and snapshot move together in one commit, so a fresh
  // fetch can't flash "dirty" at the page between the two.
  useEffect(() => {
    setDraft(config)
    setSavedSnapshot(config)
  }, [config])

  const setSection = useCallback(
    <K extends keyof AlarmTransportConfig>(key: K, value: Partial<AlarmTransportConfig[K]>) =>
      setDraft((current) => ({ ...current, [key]: { ...current[key], ...value } })),
    [],
  )

  const setSecret = useCallback(
    (key: TransportSecretKey, value: string) => setSecrets((current) => ({ ...current, [key]: value })),
    [],
  )

  const hasEnteredSecret = Object.values(secrets).some((value) => (value ?? '').trim() !== '')
  const dirty = !transportConfigsEqual(draft, savedSnapshot) || hasEnteredSecret

  // Called on every page save, from whichever section is on screen, so it
  // has to answer for a panel nobody opened:
  //
  //  - Untouched means no request. The old per-panel Save could only fire
  //    while someone was looking at the panel; this one fires regardless,
  //    and a POST per settings save is both pointless and a way to write a
  //    draft the operator never edited.
  //  - A draft that was never seeded from the server is refused. It would be
  //    emptyTransportConfig(), and one toggle on top of that would wipe the
  //    ntfy and SMTP config the server holds. This is keyed on `loaded`
  //    rather than on `error`: a refresh that fails after a good load leaves
  //    a real config underneath, which is still safe to save against.
  //
  // Snapshots what was just saved rather than waiting for save()'s own
  // refetch to come back and re-seed it: the page's dirty indicator gates
  // its navigation guard, so it must settle on the save succeeding, not on
  // a follow-up GET that may not. An edit made while the save was in flight
  // stays dirty, because `draft` here is the copy that actually went up.
  //
  // Entered secrets are cleared only on success — a failed save must not
  // swallow a token the operator would then have to retype.
  const persist = useCallback(async () => {
    if (!dirty) return
    if (!loaded) {
      throw new Error(
        `Notifications did not load${error ? ` (${error})` : ''}, so the stored transport config was left alone`,
      )
    }
    await save(draft, secrets)
    setSavedSnapshot(draft)
    setSecrets({})
  }, [dirty, loaded, error, save, draft, secrets])

  const value = useMemo<AlarmTransportsFormValue>(
    () => ({
      draft, setSection, secrets, setSecret, secretsPresent,
      loading, error, dirty, save: persist, test, testResults, testing,
    }),
    [draft, setSection, secrets, setSecret, secretsPresent, loading, error, dirty, persist, test, testResults, testing],
  )

  return <AlarmTransportsFormContext.Provider value={value}>{children}</AlarmTransportsFormContext.Provider>
}

export function useAlarmTransportsFormContext(): AlarmTransportsFormValue {
  const context = useContext(AlarmTransportsFormContext)
  if (!context) {
    throw new Error('useAlarmTransportsFormContext must be used within an AlarmTransportsProvider')
  }
  return context
}
