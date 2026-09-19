import { BookOpen } from 'lucide-react'
import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
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
import { EquipmentSection } from '@/components/settings/sections/equipment-section'
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
import { SETTINGS_HELP_TARGETS, type HelpTarget } from '@/lib/help-links'

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
  /**
   * ADR 0095: one Help button above the active section, rather than each
   * of the ten sections growing its own. Optional for the same reason
   * `activeSectionId` is - omitted, no button renders, so every existing
   * caller and test is untouched.
   */
  onOpenHelp?: (target: HelpTarget) => void
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
    onOpenHelp,
    onAskMate,
  },
  ref,
) {
  const { settings, loading, error, save } = useSettingsFormContext()
  const { touched, saveTouchedKeys } = useSecretsStatusContext()
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
  const [draft, setDraft] = useState<RegularSettingsDraft>(initialRegularSettingsDraft)
  const [savedDraftSnapshot, setSavedDraftSnapshot] = useState<RegularSettingsDraft>(initialRegularSettingsDraft)
  const [saveSuccess, setSaveSuccess] = useState<string | null>(null)
  // `error` (above) is owned by useSettingsFormContext's useSettingsForm
  // hook and only ever set inside its own save()'s catch block — it can't
  // be set externally without changing that hook's API, which is out of
  // scope here. saveTouchedKeys (a sibling call hitting an independent
  // endpoint) needs its own error slot rendered in the same visual style.
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
  // closed, and those must not be silently dropped by the pinned "Save
  // Settings" button. Throws/rejects on failure (does not swallow) so
  // callers — the page's own button handler, and App.tsx's "Save and
  // Continue" navigation-guard action — can each decide how to react.
  const performSave = useCallback(async () => {
    await Promise.all([
      save(buildRegularSettingsPatch(draft)),
      saveTouchedKeys(SECRET_KEYS).catch((err: unknown) => {
        setSecretsSaveError(err instanceof Error ? err.message : 'Unable to save secrets')
        throw err
      }),
      transports.save().catch((err: unknown) => {
        setTransportsSaveError(err instanceof Error ? err.message : 'Unable to save notifications')
        throw err
      }),
    ])
    setSavedDraftSnapshot(draft)

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
    setSaveSuccess(null)
    setSecretsSaveError(null)
    setTransportsSaveError(null)
    setIsSavingSettings(true)
    try {
      await performSave()
      setSaveSuccess('Settings saved')
    } catch {
      // save()'s own failure is surfaced via the shared `error` state
      // below; saveTouchedKeys's failure is captured above into
      // secretsSaveError. Both requests were attempted regardless of which
      // one rejected first.
    } finally {
      setIsSavingSettings(false)
    }
  }

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
      case 'equipment':
        return <EquipmentSection />
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
      case 'logs':
        return <LogsSection onAskMate={(question, options) => onAskMate?.(question, options)} />
      default:
        return null
    }
  })()

  return (
    <div className="flex flex-col gap-4 md:flex-row">
      <SettingsNav activeSectionId={activeSectionId} onSelect={handleSectionSelect} />

      <div className="min-w-0 flex-1 space-y-4">
        {onOpenHelp && (
          <div className="mx-auto flex max-w-3xl justify-end">
            <Button
              variant="ghost"
              className="h-10 gap-2 text-primary"
              aria-label="Open help for this section"
              onClick={() => onOpenHelp(SETTINGS_HELP_TARGETS[activeSectionId])}
            >
              <BookOpen className="h-4 w-4" />
              Help
            </Button>
          </div>
        )}
        {activeSection}

        <div className="mx-auto flex max-w-3xl items-center justify-end">
          <Button
            variant="outline"
            className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
            onClick={handleSaveSettings}
            disabled={isSavingSettings}
          >
            {isSavingSettings ? 'Saving' : 'Save Settings'}
          </Button>
        </div>

        {error && (
          <div className="mx-auto max-w-3xl rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
            {error}
          </div>
        )}
        {secretsSaveError && (
          <div className="mx-auto max-w-3xl rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
            {secretsSaveError}
          </div>
        )}
        {transportsSaveError && (
          <div className="mx-auto max-w-3xl rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
            {transportsSaveError}
          </div>
        )}
        {saveSuccess && (
          <div className="mx-auto max-w-3xl rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
            {saveSuccess}
          </div>
        )}
      </div>
    </div>
  )
})
