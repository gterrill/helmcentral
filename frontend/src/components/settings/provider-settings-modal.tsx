import { useEffect, useMemo, useState } from 'react'

import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { SecretFieldGroup, type SecretFieldSpec } from '@/components/settings/secret-field-group'
import { useSecretsStatusContext } from '@/components/settings/secrets-status-context'
import { useFollowingDraft } from '@/hooks/use-following-draft'
import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'
import { sameSet } from '@/lib/same-value'

export type ProviderDomain = 'tide' | 'weather' | 'wave' | 'poi' | 'forecast-warnings'

// PluginConfigField mirrors backend/plugin_overrides_handlers.go's
// pluginConfigFieldResponse: one of a plugin's own operator-editable config
// fields (its <name>.config_fields.json sidecar), plus its currently stored
// Value. A plugin with none declared has an empty config_fields array.
interface PluginConfigField {
  key: string
  label: string
  type: 'url' | 'text' | 'boolean'
  help: string
  placeholder: string
  value: string
}

interface PluginInfoResponse {
  type: string
  id: string
  name: string
  description: string
  sandboxed: boolean
  allowed_hosts: string[]
  allowed_hosts_overridden: boolean
  allowed_secrets: string[]
  allowed_secrets_overridden: boolean
  config_fields: PluginConfigField[]
}

// Frontend-only table: which secret fields a given provider's Settings
// modal should expose, keyed by domain then provider id. There is no
// backend equivalent of this table — it must be kept in sync by hand
// whenever a new provider requiring secrets is added. See ADR 0025.
const PROVIDER_SECRET_FIELDS: Partial<Record<ProviderDomain, Record<string, SecretFieldSpec[]>>> = {
  weather: {
    weatherkit: [
      { key: 'WEATHERKIT_KEY_ID', label: 'WeatherKit Key ID' },
      { key: 'WEATHERKIT_TEAM_ID', label: 'WeatherKit Team ID' },
      { key: 'WEATHERKIT_SERVICE_ID', label: 'WeatherKit Service ID' },
      { key: 'WEATHERKIT_PRIVATE_KEY', label: 'WeatherKit Private Key', multiline: true },
    ],
  },
  poi: {
    'google-places': [{ key: 'GOOGLE_PLACES_API_KEY', label: 'Google Places API key' }],
  },
}

interface ProviderSettingsModalProps {
  type: ProviderDomain
  providerId: string | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

const parseList = (value: string) =>
  value
    .split(',')
    .map((entry) => entry.trim())
    .filter(Boolean)

// The dialog's draft: the hosts and secrets as the text in their inputs, and
// one entry per declared config field under "config:<key>". A boolean field
// with nothing stored reads as off, so switching it on and back off is no
// change.
type Draft = Record<string, string>
const configKey = (key: string) => `config:${key}`

const toDraft = (info: PluginInfoResponse): Draft => {
  const draft: Draft = { hosts: info.allowed_hosts.join(', '), secrets: info.allowed_secrets.join(', ') }
  for (const field of info.config_fields ?? []) {
    draft[configKey(field.key)] = field.type === 'boolean' && field.value === '' ? 'false' : field.value
  }
  return draft
}

/**
 * Per-provider settings modal: fetches `GET /api/plugins/:type/:id` only
 * while open (not on mount for every card in the grid), shows the
 * provider's description, any secret fields the static
 * `PROVIDER_SECRET_FIELDS` table says this provider needs, the plugin's own
 * operator-editable config fields (its `config_fields.json` sidecar - see
 * docs/adr/0100), and — once `info` has loaded — an
 * allowed-hosts/allowed-secrets override editor. Every provider registered
 * anywhere in the app is WASM-backed (see backend/plugin_overrides_handlers.go),
 * so `info.sandboxed` is always true once loaded; the allowlist block is
 * only withheld transiently while `info` is still loading.
 *
 * Config fields and the allowlist have different effective timing: a config
 * field save (`POST .../config`) applies on the very next plugin call, while
 * an allowlist save still needs a backend restart (ADR 0024) - the UI notes
 * both distinctly rather than implying one timing for both.
 */
export function ProviderSettingsModal({ type, providerId, open, onOpenChange }: ProviderSettingsModalProps) {
  const { saveTouchedKeys } = useSecretsStatusContext()
  const infoPath = `/api/plugins/${type}/${providerId ?? ''}`
  const { data: info = null, isLoading: loading, error: loadError } = useApiQuery<PluginInfoResponse>({
    queryKey: [infoPath],
    path: infoPath,
    enabled: open && Boolean(providerId),
  })
  const [saveError, setSaveError] = useState<string | null>(null)
  const error = saveError ?? loadError?.message ?? null
  const [isSaving, setIsSaving] = useState(false)
  const [isResetting, setIsResetting] = useState(false)
  const [savedNote, setSavedNote] = useState<string | null>(null)
  const busy = isSaving || isResetting

  // The draft follows the plugin's record: a refetch that lands while the
  // dialog is open updates every field the operator hasn't edited and keeps
  // the ones they have. With the dialog closed there is no record, so each
  // opening seeds the draft afresh.
  const record = useMemo(() => (open && info ? { ...info, id: infoPath } : null), [open, info, infoPath])
  const [draft, setDraft] = useFollowingDraft<PluginInfoResponse & { id: string }, Draft, Draft>(record, toDraft, {})
  const hostsInput = draft.hosts ?? ''
  const secretsInput = draft.secrets ?? ''
  const setField = (key: string, value: string) => {
    setSavedNote(null)
    setDraft((previous) => ({ ...previous, [key]: value }))
  }

  useEffect(() => {
    if (open) {
      setSaveError(null)
      setSavedNote(null)
    }
  }, [open, providerId, type])

  const cacheInfo = (data: PluginInfoResponse) => setQueryData<PluginInfoResponse>([infoPath], data)

  const secretFields = providerId ? PROVIDER_SECRET_FIELDS[type]?.[providerId] : undefined
  const configFields = info?.config_fields ?? []

  // Single Save button for the whole modal: config fields, allowlist
  // overrides and touched secret fields in one click.
  // - Config goes first and stops the save on an error.
  // - An unchanged allowlist is never posted: that would pin
  //   allowed_hosts_overridden and hide a later plugin version's new defaults.
  // - Hosts and secrets post together, because the backend stores one override.
  // - Overrides and secrets run side by side and both outcomes are read, so a
  //   failure in one never hides that the other went through.
  // - The cache is written once, at the end, from the latest answer, so the
  //   draft is never reseeded under the operator mid-save. The dialog can't be
  //   dismissed mid-save, so a late answer never lands in another provider's.
  // - The dialog closes on success, unless the allowlist was saved: then it
  //   stays open so the restart reminder stays in view.
  const handleSave = async () => {
    if (!providerId || !info) return
    setIsSaving(true)
    setSaveError(null)
    setSavedNote(null)
    try {
      const stored = toDraft(info)
      let latest: PluginInfoResponse | null = null

      const changedConfig = configFields.filter((field) => draft[configKey(field.key)] !== stored[configKey(field.key)])
      let configSaved = false
      if (changedConfig.length > 0) {
        // Only the changed fields go: the backend leaves omitted keys as they
        // are, so an unset option isn't pinned to an explicit value.
        const configData = await apiRequest<PluginInfoResponse>(`${infoPath}/config`, {
          method: 'POST',
          json: { values: Object.fromEntries(changedConfig.map((field) => [field.key, draft[configKey(field.key)] ?? ''])) },
        })
        latest = configData
        configSaved = true
        // Show the values as the plugin stored them.
        const saved = toDraft(configData)
        setDraft((previous) => ({
          ...previous,
          ...Object.fromEntries(Object.entries(saved).filter(([key]) => key.startsWith('config:'))),
        }))
      }

      const hosts = parseList(hostsInput)
      const secrets = parseList(secretsInput)
      const overridesChanged =
        info.sandboxed === true && !(sameSet(hosts, info.allowed_hosts) && sameSet(secrets, info.allowed_secrets))

      const [overridesResult, secretsResult] = await Promise.allSettled([
        overridesChanged
          ? apiRequest<PluginInfoResponse>(`${infoPath}/overrides`, {
              method: 'POST',
              json: { allowed_hosts: hosts, allowed_secrets: secrets },
            })
          : null,
        secretFields && secretFields.length > 0 ? saveTouchedKeys(secretFields.map((field) => field.key)) : null,
      ])

      const overridesSaved = overridesResult.status === 'fulfilled' && overridesChanged
      if (overridesResult.status === 'fulfilled' && overridesResult.value) {
        latest = overridesResult.value
        // Show the lists as the server stored them (no repeats, tidy spacing).
        const saved = toDraft(overridesResult.value)
        setDraft((previous) => ({ ...previous, hosts: saved.hosts, secrets: saved.secrets }))
      }
      if (latest) cacheInfo(latest)

      const failure =
        overridesResult.status === 'rejected'
          ? overridesResult.reason
          : secretsResult.status === 'rejected'
            ? secretsResult.reason
            : null
      if (failure) {
        if (configSaved || overridesSaved) {
          setSavedNote(
            configSaved && overridesSaved ? 'Options and allowlist saved.' : configSaved ? 'Options saved.' : 'Allowlist saved.',
          )
        }
        setSaveError(failure instanceof Error ? failure.message : 'Unable to save settings')
      } else if (overridesSaved) {
        setSavedNote('Saved.')
      } else {
        onOpenChange(false)
      }
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Unable to save settings')
    } finally {
      setIsSaving(false)
    }
  }

  // Reset discards the operator's edits by design, so the whole draft is set
  // from the answer.
  const handleResetOverrides = async () => {
    if (!providerId) return
    setIsResetting(true)
    setSaveError(null)
    setSavedNote(null)
    try {
      const data = await apiRequest<PluginInfoResponse>(`${infoPath}/overrides`, { method: 'DELETE' })
      cacheInfo(data)
      setDraft(toDraft(data))
      setSavedNote('Reset to defaults.')
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Unable to reset allowlist overrides')
    } finally {
      setIsResetting(false)
    }
  }

  const hasSecretFields = Boolean(secretFields && secretFields.length > 0)
  const hasConfigFields = configFields.length > 0
  const isSandboxed = info?.sandboxed === true
  // A single Save button is shown whenever there's anything for it to save
  // — allowlist overrides (sandboxed providers), the plugin's own config
  // fields, or secret fields (providers listed in PROVIDER_SECRET_FIELDS),
  // or any combination. Providers with none of the three (most) show no
  // Save button at all — no behavior change there.
  const showSaveButton = isSandboxed || hasSecretFields || hasConfigFields

  return (
    <Dialog open={open} onOpenChange={(next) => (next || !busy) && onOpenChange(next)}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle>{info?.name ?? providerId ?? 'Provider Settings'}</DialogTitle>
          <DialogDescription>{loading ? 'Loading…' : info?.description}</DialogDescription>
        </DialogHeader>

        {error && (
          <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
            {error}
          </div>
        )}

        {hasSecretFields && (
          <fieldset disabled={busy} className="contents">
            <SecretFieldGroup fields={secretFields!} />
          </fieldset>
        )}

        {hasConfigFields && (
          <div className="space-y-2">
            {configFields.map((field) => (
              <div key={field.key} className="space-y-1">
                {field.type === 'boolean' ? (
                  <Field orientation="horizontal">
                    <Switch
                      id={`config-field-${field.key}`}
                      checked={draft[configKey(field.key)] === 'true'}
                      disabled={busy}
                      onCheckedChange={(checked) => setField(configKey(field.key), checked ? 'true' : 'false')}
                    />
                    <FieldLabel htmlFor={`config-field-${field.key}`}>{field.label}</FieldLabel>
                  </Field>
                ) : (
                  <Field>
                    <FieldLabel htmlFor={`config-field-${field.key}`}>{field.label}</FieldLabel>
                    <Input
                      id={`config-field-${field.key}`}
                      value={draft[configKey(field.key)] ?? ''}
                      disabled={busy}
                      onChange={(e) => setField(configKey(field.key), e.target.value)}
                      placeholder={field.placeholder}
                      aria-label={field.label}
                    />
                  </Field>
                )}
                {field.help && <p className="text-xs text-muted-foreground">{field.help}</p>}
              </div>
            ))}
            <p className="text-xs text-muted-foreground">
              Applies on the next call — no restart needed.
            </p>
          </div>
        )}

        {isSandboxed && (
          <div className="space-y-2">
            <Field>
              <FieldLabel htmlFor="allowed-hosts">Allowed Hosts</FieldLabel>
              <Input
                id="allowed-hosts"
                value={hostsInput}
                disabled={busy}
                onChange={(e) => setField('hosts', e.target.value)}
                placeholder="example.com, api.example.org"
                aria-label="Allowed hosts"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="allowed-secrets">Allowed Secrets</FieldLabel>
              <Input
                id="allowed-secrets"
                value={secretsInput}
                disabled={busy}
                onChange={(e) => setField('secrets', e.target.value)}
                aria-label="Allowed secrets"
              />
            </Field>
            <p className="text-xs text-muted-foreground">
              Allowlist changes require a backend restart to take effect.
            </p>
            <div className="flex justify-end">
              <Button
                type="button"
                variant="ghost"
                className="h-8 whitespace-nowrap px-3 text-[10px] uppercase tracking-[0.1em]"
                onClick={handleResetOverrides}
                disabled={busy}
              >
                {isResetting ? 'Resetting' : 'Reset to defaults'}
              </Button>
            </div>
          </div>
        )}

        {showSaveButton && (
          <div className="flex items-center justify-end gap-2">
            {savedNote && <p role="status" className="mr-auto text-xs text-muted-foreground">{savedNote}</p>}
            <Button
              type="button"
              variant="outline"
              className="h-8 whitespace-nowrap px-3 text-[10px] uppercase tracking-[0.1em]"
              onClick={handleSave}
              disabled={busy}
            >
              {isSaving ? 'Saving' : 'Save'}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
