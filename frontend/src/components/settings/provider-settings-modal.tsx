import { useEffect, useRef, useState } from 'react'

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
import { apiRequest, setQueryData, useApiQuery } from '@/lib/api-query'

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

// buildConfigValues turns a provider's declared config fields into the
// {key: value} shape the initial POST /config request body wants, seeded
// from each field's currently-stored value. Tolerates a missing
// config_fields (older/mocked responses that predate this field) as "none
// declared" rather than throwing.
const buildConfigValues = (fields: PluginConfigField[] | undefined) =>
  Object.fromEntries((fields ?? []).map((field) => [field.key, field.value]))

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
  const [hostsInput, setHostsInput] = useState('')
  const [secretsInput, setSecretsInput] = useState('')
  const [configValues, setConfigValues] = useState<Record<string, string>>({})
  const [isSaving, setIsSaving] = useState(false)
  const [isResetting, setIsResetting] = useState(false)

  const seedDrafts = (from: PluginInfoResponse) => {
    setHostsInput(from.allowed_hosts.join(', '))
    setSecretsInput(from.allowed_secrets.join(', '))
    setConfigValues(buildConfigValues(from.config_fields))
  }

  // The drafts are seeded once per opening, from the first answer to arrive
  // (cached or fresh), and again after each save or reset. A background
  // refetch landing later must not overwrite what the operator is typing.
  const seededFor = useRef<string | null>(null)
  useEffect(() => {
    if (!open) {
      seededFor.current = null
      return
    }
    if (!info || seededFor.current === infoPath) return
    seededFor.current = infoPath
    seedDrafts(info)
  }, [info, open, infoPath])

  useEffect(() => {
    if (open) setSaveError(null)
  }, [open, providerId, type])

  const applyResponse = (data: PluginInfoResponse) => {
    setQueryData<PluginInfoResponse>([infoPath], data)
    seedDrafts(data)
  }

  const secretFields = providerId ? PROVIDER_SECRET_FIELDS[type]?.[providerId] : undefined
  const configFields = info?.config_fields ?? []

  // Single Save button for the whole modal: saves the plugin's own config
  // fields (if any), allowlist overrides, AND any touched secret field(s)
  // (only relevant for providers listed in PROVIDER_SECRET_FIELDS) in the
  // same click. Config is saved FIRST and awaited on its own — a 400 there
  // (an unknown key, or a bad URL) stops before touching the allowlist or
  // secrets, so a rejected config edit can never look like the OTHER two
  // succeeded. The overrides branch is skipped only while `info` hasn't
  // loaded yet (every provider is sandboxed once loaded, see
  // backend/plugin_overrides_handlers.go).
  const handleSave = async () => {
    if (!providerId) return
    setIsSaving(true)
    setSaveError(null)
    try {
      if (configFields.length > 0) {
        applyResponse(
          await apiRequest<PluginInfoResponse>(`${infoPath}/config`, {
            method: 'POST',
            json: { values: configValues },
          }),
        )
      }

      const overridesPromise =
        info?.sandboxed === true
          ? apiRequest<PluginInfoResponse>(`${infoPath}/overrides`, {
              method: 'POST',
              json: {
                allowed_hosts: parseList(hostsInput),
                allowed_secrets: parseList(secretsInput),
              },
            })
          : null

      const secretsPromise =
        secretFields && secretFields.length > 0
          ? saveTouchedKeys(secretFields.map((field) => field.key))
          : null

      const [overridesData] = await Promise.all([overridesPromise, secretsPromise])

      if (overridesData) {
        applyResponse(overridesData)
      }
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'Unable to save settings')
    } finally {
      setIsSaving(false)
    }
  }

  const handleResetOverrides = async () => {
    if (!providerId) return
    setIsResetting(true)
    setSaveError(null)
    try {
      const data = await apiRequest<PluginInfoResponse>(`${infoPath}/overrides`, { method: 'DELETE' })
      applyResponse(data)
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
    <Dialog open={open} onOpenChange={onOpenChange}>
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

        {hasSecretFields && <SecretFieldGroup fields={secretFields!} />}

        {hasConfigFields && (
          <div className="space-y-2">
            {configFields.map((field) => (
              <div key={field.key} className="space-y-1">
                {field.type === 'boolean' ? (
                  <Field orientation="horizontal">
                    <Switch
                      id={`config-field-${field.key}`}
                      checked={configValues[field.key] === 'true'}
                      onCheckedChange={(checked) =>
                        setConfigValues((previous) => ({ ...previous, [field.key]: checked ? 'true' : 'false' }))
                      }
                    />
                    <FieldLabel htmlFor={`config-field-${field.key}`}>{field.label}</FieldLabel>
                  </Field>
                ) : (
                  <Field>
                    <FieldLabel htmlFor={`config-field-${field.key}`}>{field.label}</FieldLabel>
                    <Input
                      id={`config-field-${field.key}`}
                      value={configValues[field.key] ?? ''}
                      onChange={(e) => setConfigValues((previous) => ({ ...previous, [field.key]: e.target.value }))}
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
                onChange={(e) => setHostsInput(e.target.value)}
                placeholder="example.com, api.example.org"
                aria-label="Allowed hosts"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor="allowed-secrets">Allowed Secrets</FieldLabel>
              <Input
                id="allowed-secrets"
                value={secretsInput}
                onChange={(e) => setSecretsInput(e.target.value)}
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
                disabled={isResetting}
              >
                {isResetting ? 'Resetting' : 'Reset to defaults'}
              </Button>
            </div>
          </div>
        )}

        {showSaveButton && (
          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              className="h-8 whitespace-nowrap px-3 text-[10px] uppercase tracking-[0.1em]"
              onClick={handleSave}
              disabled={isSaving}
            >
              {isSaving ? 'Saving' : 'Save'}
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
