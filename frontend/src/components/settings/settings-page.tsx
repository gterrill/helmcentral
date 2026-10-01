import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'

import { SaveBar } from '@/components/patterns'
import { SettingsFormProvider, useSettingsFormContext } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider, useSecretsStatusContext } from '@/components/settings/secrets-status-context'
import { AlarmTransportsProvider, useAlarmTransportsFormContext } from '@/components/settings/alarm-transports-context'
import { refreshAuthState } from '@/hooks/use-auth'
import { SettingsNav, SETTINGS_SECTIONS, type SettingsSectionId } from '@/components/settings/settings-nav'
import { SECRET_KEYS } from '@/hooks/use-secrets-status'
import { AlarmsSection } from '@/components/settings/sections/alarms-section'
import { AnchorWatchOptionsSection } from '@/components/settings/sections/anchor-watch-options-section'
import { AssistantSection } from '@/components/settings/sections/assistant-section'
import { BoatUiSection } from '@/components/settings/sections/boat-ui-section'
import { ImportSection } from '@/components/settings/sections/import-section'
import { ImportWizard } from '@/components/import/import-wizard'
import { GeneralSection } from '@/components/settings/sections/general-section'
import { SecuritySection } from '@/components/settings/sections/security-section'
import { InfluxdbSection } from '@/components/settings/sections/influxdb-section'
import { LogsSection } from '@/components/settings/sections/logs-section'
import { MayaraSection } from '@/components/settings/sections/mayara-section'
import { SignalKConnectionSection } from '@/components/settings/sections/signalk-connection-section'
import { TilesSection } from '@/components/settings/sections/tiles-section'
import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'
interface SettingsPageProps {
  onDirtyChange?: (dirty: boolean) => void
  /**
   * Controlled active section (ADR 0074): App.tsx owns this so it can mirror
   * it to `/settings/<id>`. Both props are optional and only meaningful
   * together — omitted (as `settings-page.test.tsx` and
   * `settings-alarms-section.test.tsx` do), the page falls back to its own
   * local `useState`, which is what keeps those tests unchanged.
   */
  activeSectionId?: SettingsSectionId
  onSectionChange?: (id: SettingsSectionId) => void
  /** The Import wizard's run ("new" for its upload page), mirrored to the URL by App.tsx. */
  importRunId?: string | null
  onImportRunChange?: (runId: string | null) => void
  onAskMate?: (question: string, options?: { newConversation?: boolean }) => void
}

export interface SettingsPageHandle {
  save: () => Promise<void>
}

export const SettingsPage = forwardRef<SettingsPageHandle, SettingsPageProps>(function SettingsPage(props, ref) {
  return (
    <SettingsFormProvider>
      <SecretsStatusProvider>
        <AlarmTransportsProvider>
          <SettingsPageContent {...props} ref={ref} />
        </AlarmTransportsProvider>
      </SecretsStatusProvider>
    </SettingsFormProvider>
  )
})

const SettingsPageContent = forwardRef<SettingsPageHandle, SettingsPageProps>(function SettingsPageContent(
  {
    onDirtyChange,
    activeSectionId: controlledSectionId,
    onSectionChange,
    importRunId: controlledImportRunId,
    onImportRunChange,
    onAskMate,
  },
  ref,
) {
  const { settings, loading, save } = useSettingsFormContext()
  const { touched, saveTouchedKeys, resetTouched } = useSecretsStatusContext()
  const transports = useAlarmTransportsFormContext()
  // Uncontrolled fallback for callers that don't pass activeSectionId (e.g.
  // settings-page.test.tsx, settings-alarms-section.test.tsx) — App.tsx
  // passes both props and this local state just goes along for the ride,
  // updated by the same handler so an uncontrolled render never falls behind
  // a controlled one if a caller starts passing the props later.
  const [localSectionId, setLocalSectionId] = useState<SettingsSectionId>(SETTINGS_SECTIONS[0].id)
  const activeSectionId = controlledSectionId ?? localSectionId
  const handleSectionSelect = useCallback((id: SettingsSectionId) => {
    setLocalSectionId(id)
    onSectionChange?.(id)
  }, [onSectionChange])
  // Same controlled-or-local pairing for the Import wizard's run.
  const [localImportRunId, setLocalImportRunId] = useState<string | null>(null)
  const importRunId = controlledImportRunId !== undefined ? controlledImportRunId : localImportRunId
  const handleImportRunChange = useCallback((runId: string | null) => {
    setLocalImportRunId(runId)
    onImportRunChange?.(runId)
  }, [onImportRunChange])
  const [draft, setDraft] = useState<RegularSettingsDraft>(initialRegularSettingsDraft)
  const [savedDraftSnapshot, setSavedDraftSnapshot] = useState<RegularSettingsDraft>(initialRegularSettingsDraft)
  // Each request inside one save gets its own error slot, all shown in the
  // Save bar. The settings one is the page's own copy, not the form hook's
  // `error`: that is also set by a Tiles provider change (which saves on its
  // own and reports its own failure), and Discard cannot clear it, so showing
  // it here would bring back a stale refusal. saveTouchedKeys hits an
  // independent endpoint, so it needs its own slot too.
  const [settingsSaveError, setSettingsSaveError] = useState<string | null>(null)
  const [secretsSaveError, setSecretsSaveError] = useState<string | null>(null)
  // Same reasoning for the transports POST: it is a third independent
  // endpoint inside one save (ADR 0038 §2 keeps transport config out of
  // settings.yaml), so its failure needs its own slot rather than being
  // folded into either of the other two.
  const [transportsSaveError, setTransportsSaveError] = useState<string | null>(null)
  const [isSavingSettings, setIsSavingSettings] = useState(false)

  // `touched` covers ALL secret keys tracked by useSecretsStatus — the
  // two inline-section keys (SignalK/InfluxDB) AND the provider-modal-only
  // keys (WeatherKit), whether or not that modal happens to be open right
  // now. Any of them being touched means
  // there's an in-memory edit that would be lost on navigation, so it must
  // count toward "dirty" too.
  const hasUnsavedSecrets = Object.values(touched).some(Boolean)
  const draftDirty = !draftsEqual(draft, savedDraftSnapshot)
  // The Alarms section's provider is mounted for the whole page, not just
  // while that section is on screen, so an edit there counts as dirty from
  // whichever section you happen to be looking at when you navigate away.
  const dirty = draftDirty || hasUnsavedSecrets || transports.dirty

  useEffect(() => {
    onDirtyChange?.(dirty)
  }, [dirty, onDirtyChange])

  // Regular-section fields are hydrated from the fetched settings exactly
  // once (on initial load), not on every subsequent context.settings
  // change — otherwise a Widgets-tab provider activation (which also
  // updates context.settings via the same shared `save()`) would clobber
  // any in-progress, not-yet-saved edits in a regular section.
  const hydratedRef = useRef(false)
  useEffect(() => {
    if (!loading && !hydratedRef.current) {
      const hydrated = hydrateDraftFromSettings(settings)
      setDraft(hydrated)
      setSavedDraftSnapshot(hydrated)
      hydratedRef.current = true
    }
  }, [loading, settings])

  const handleDraftChange = useCallback((patch: Partial<RegularSettingsDraft>) => {
    setDraft((previous) => ({ ...previous, ...patch }))
  }, [])

  // Saves the regular-settings draft AND every touched secret across the
  // whole page (not just the three inline-section keys) — a provider modal
  // (e.g. WeatherKit) may have touched-but-unsaved fields even while
  // closed, and those must not be silently dropped by the Save bar. Throws/rejects on failure (does not swallow) so
  // callers — the page's own button handler, and App.tsx's "Save and
  // Continue" navigation-guard action — can each decide how to react.
  const performSave = useCallback(async () => {
    await Promise.all([
      // The snapshot moves as soon as THIS request lands, not once all three
      // have: if a sibling fails, Discard must not roll the form back behind
      // what the server already holds (the next save would then write it).
      save(buildRegularSettingsPatch(draft)).then(() => setSavedDraftSnapshot(draft), (err: unknown) => {
        setSettingsSaveError(err instanceof Error ? err.message : 'Unable to save settings')
        throw err
      }),
      saveTouchedKeys(SECRET_KEYS).catch((err: unknown) => {
        setSecretsSaveError(err instanceof Error ? err.message : 'Unable to save secrets')
        throw err
      }),
      transports.save().catch((err: unknown) => {
        setTransportsSaveError(err instanceof Error ? err.message : 'Unable to save notifications')
        throw err
      }),
    ])

    // Re-read auth after every save. If this save turned authentication on,
    // App.tsx's gate drops this tab to the login screen, rather than leaving it
    // looking signed in while every request is unauthenticated. Unconditional
    // because it is two small GETs with no visible effect when the mode did not
    // change.
    //
    // Deliberately not awaited: it is a side effect of saving, not part of it.
    // Awaiting would make "Save and Continue" navigation wait on an unrelated
    // request, and a slow or failed auth check would stall a save that already
    // succeeded. The module publishes to its listeners when it resolves, so the
    // gate applies either way.
    void refreshAuthState()
  }, [save, draft, saveTouchedKeys, transports])

  useImperativeHandle(ref, () => ({ save: performSave }), [performSave])

  const handleSaveSettings = async () => {
    setSettingsSaveError(null)
    setSecretsSaveError(null)
    setTransportsSaveError(null)
    setIsSavingSettings(true)
    try {
      await performSave()
    } catch {
      // Each request's failure is already captured into its own slot by
      // performSave. All were attempted regardless of which rejected first.
    } finally {
      setIsSavingSettings(false)
    }
  }

  // Discard puts all three stores back to what was last saved: the regular
  // draft, every in-progress secret edit, and the alarm-transports draft.
  // Nothing is written, and any failed-save message goes with the edits.
  const transportsReset = transports.reset
  const handleDiscard = useCallback(() => {
    setDraft(savedDraftSnapshot)
    resetTouched()
    transportsReset()
    setSettingsSaveError(null)
    setSecretsSaveError(null)
    setTransportsSaveError(null)
  }, [savedDraftSnapshot, resetTouched, transportsReset])

  const saveBarError = [settingsSaveError, secretsSaveError, transportsSaveError].filter(Boolean).join('. ') || null

  const activeSection = (() => {
    switch (activeSectionId) {
      case 'general':
        return <GeneralSection draft={draft} onChange={handleDraftChange} />
      case 'signalk':
        return <SignalKConnectionSection draft={draft} onChange={handleDraftChange} />
      case 'boat-ui':
        return <BoatUiSection draft={draft} onChange={handleDraftChange} />
      case 'tiles':
        return <TilesSection draft={draft} onChange={handleDraftChange} />
      case 'alarms':
        return <AlarmsSection />
      case 'assistant':
        return <AssistantSection draft={draft} onChange={handleDraftChange} />
      case 'security':
        return <SecuritySection draft={draft} onChange={handleDraftChange} />
      case 'influxdb':
        return <InfluxdbSection draft={draft} onChange={handleDraftChange} />
      case 'mayara':
        return <MayaraSection draft={draft} onChange={handleDraftChange} />
      case 'anchor-watch':
        return (
          <AnchorWatchOptionsSection
            draft={draft}
            onChange={handleDraftChange}
          />
        )
      case 'import':
        return <ImportSection onOpenImport={handleImportRunChange} />
      case 'logs':
        return <LogsSection onAskMate={(question, options) => onAskMate?.(question, options)} />
      default:
        return null
    }
  })()

  // The wizard takes the whole page: no section nav, no Save bar. It
  // keeps its own draft on the server, so nothing here is left unsaved.
  if (activeSectionId === 'import' && importRunId !== null) {
    return (
      <ImportWizard
        runId={importRunId}
        onRunChange={handleImportRunChange}
        onExit={() => handleImportRunChange(null)}
      />
    )
  }

  return (
    <div className="flex flex-col gap-4 md:flex-row">
      <SettingsNav activeSectionId={activeSectionId} onSelect={handleSectionSelect} />

      {/* No Help button of its own (removed - it duplicated the header's `?`,
          and only existed because App.tsx's own `?` handler didn't pass
          inventorySection through helpTargetFor for the Inventory panel; it
          passed settingsSection correctly for this page all along. See
          inventory-panel.tsx's identical note and ADR 0142's CRUD Pattern
          Library note in AGENTS.md). */}
      <div className="min-w-0 flex-1 space-y-4">
        {activeSection}

        <SaveBar
          dirty={dirty}
          saving={isSavingSettings}
          error={saveBarError}
          onSave={() => void handleSaveSettings()}
          onDiscard={handleDiscard}
        />
      </div>
    </div>
  )
})
