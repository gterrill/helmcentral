import { forwardRef, useCallback, useEffect, useImperativeHandle, useRef, useState } from 'react'

import { Button } from '@/components/ui/button'
import { SettingsFormProvider, useSettingsFormContext } from '@/components/settings/settings-form-context'
import { SecretsStatusProvider, useSecretsStatusContext } from '@/components/settings/secrets-status-context'
import { SettingsNav, SETTINGS_SECTIONS, type SettingsSectionId } from '@/components/settings/settings-nav'
import { SECRET_KEYS } from '@/hooks/use-secrets-status'
import { AnchorWatchOptionsSection } from '@/components/settings/sections/anchor-watch-options-section'
import { BoatUiSection } from '@/components/settings/sections/boat-ui-section'
import { GeneralSection } from '@/components/settings/sections/general-section'
import { GeonamesSection } from '@/components/settings/sections/geonames-section'
import { InfluxdbSection } from '@/components/settings/sections/influxdb-section'
import { SignalKConnectionSection } from '@/components/settings/sections/signalk-connection-section'
import { WidgetsSection } from '@/components/settings/sections/widgets-section'
import {
  buildRegularSettingsPatch,
  draftsEqual,
  hydrateDraftFromSettings,
  initialRegularSettingsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'

interface SettingsPageProps {
  autoCloseAnchorWatchEnabled: boolean
  onAutoCloseAnchorWatchToggle?: (enabled: boolean) => void
  onDirtyChange?: (dirty: boolean) => void
}

export interface SettingsPageHandle {
  save: () => Promise<void>
}

export const SettingsPage = forwardRef<SettingsPageHandle, SettingsPageProps>(function SettingsPage(props, ref) {
  return (
    <SettingsFormProvider>
      <SecretsStatusProvider>
        <SettingsPageContent {...props} ref={ref} />
      </SecretsStatusProvider>
    </SettingsFormProvider>
  )
})

const SettingsPageContent = forwardRef<SettingsPageHandle, SettingsPageProps>(function SettingsPageContent(
  { autoCloseAnchorWatchEnabled, onAutoCloseAnchorWatchToggle, onDirtyChange },
  ref,
) {
  const { settings, loading, error, save } = useSettingsFormContext()
  const { touched, saveTouchedKeys } = useSecretsStatusContext()
  const [activeSectionId, setActiveSectionId] = useState<SettingsSectionId>(SETTINGS_SECTIONS[0].id)
  const [draft, setDraft] = useState<RegularSettingsDraft>(initialRegularSettingsDraft)
  const [savedDraftSnapshot, setSavedDraftSnapshot] = useState<RegularSettingsDraft>(initialRegularSettingsDraft)
  const [saveSuccess, setSaveSuccess] = useState<string | null>(null)
  // `error` (above) is owned by useSettingsFormContext's useSettingsForm
  // hook and only ever set inside its own save()'s catch block — it can't
  // be set externally without changing that hook's API, which is out of
  // scope here. saveTouchedKeys (a sibling call hitting an independent
  // endpoint) needs its own error slot rendered in the same visual style.
  const [secretsSaveError, setSecretsSaveError] = useState<string | null>(null)
  const [isSavingSettings, setIsSavingSettings] = useState(false)

  // `touched` covers ALL secret keys tracked by useSecretsStatus — the
  // three inline-section keys (SignalK/InfluxDB/GeoNames) AND the
  // provider-modal-only keys (Storm Glass/WeatherKit), whether or not that
  // modal happens to be open right now. Any of them being touched means
  // there's an in-memory edit that would be lost on navigation, so it must
  // count toward "dirty" too.
  const hasUnsavedSecrets = Object.values(touched).some(Boolean)
  const draftDirty = !draftsEqual(draft, savedDraftSnapshot)
  const dirty = draftDirty || hasUnsavedSecrets

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
    ])
    setSavedDraftSnapshot(draft)
  }, [save, draft, saveTouchedKeys])

  useImperativeHandle(ref, () => ({ save: performSave }), [performSave])

  const handleSaveSettings = async () => {
    setSaveSuccess(null)
    setSecretsSaveError(null)
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
      case 'widgets':
        return <WidgetsSection draft={draft} onChange={handleDraftChange} />
      case 'influxdb':
        return <InfluxdbSection draft={draft} onChange={handleDraftChange} />
      case 'geonames':
        return <GeonamesSection />
      case 'anchor-watch':
        return (
          <AnchorWatchOptionsSection
            draft={draft}
            onChange={handleDraftChange}
            autoCloseAnchorWatchEnabled={autoCloseAnchorWatchEnabled}
            onAutoCloseAnchorWatchToggle={onAutoCloseAnchorWatchToggle}
          />
        )
      default:
        return null
    }
  })()

  return (
    <div className="flex flex-col gap-4 md:flex-row">
      <SettingsNav activeSectionId={activeSectionId} onSelect={setActiveSectionId} />

      <div className="min-w-0 flex-1 space-y-4">
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
        {saveSuccess && (
          <div className="mx-auto max-w-3xl rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
            {saveSuccess}
          </div>
        )}
      </div>
    </div>
  )
})
