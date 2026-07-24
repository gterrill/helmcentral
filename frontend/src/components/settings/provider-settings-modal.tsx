import { useEffect, useState } from 'react'

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
import { SecretFieldGroup, type SecretFieldSpec } from '@/components/settings/secret-field-group'
import { useSecretsStatusContext } from '@/components/settings/secrets-status-context'

export type ProviderDomain = 'tide' | 'weather' | 'wave' | 'forecast-warnings'

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
}

// Frontend-only table: which secret fields a given provider's Settings
// modal should expose, keyed by domain then provider id. There is no
// backend equivalent of this table — it must be kept in sync by hand
// whenever a new provider requiring secrets is added. See ADR 0025.
const PROVIDER_SECRET_FIELDS: Partial<Record<ProviderDomain, Record<string, SecretFieldSpec[]>>> = {
  tide: {
    stormglass: [{ key: 'STORMGLASS_API_KEY', label: 'Storm Glass API Key' }],
  },
  weather: {
    weatherkit: [
      { key: 'WEATHERKIT_KEY_ID', label: 'WeatherKit Key ID' },
      { key: 'WEATHERKIT_TEAM_ID', label: 'WeatherKit Team ID' },
      { key: 'WEATHERKIT_SERVICE_ID', label: 'WeatherKit Service ID' },
      { key: 'WEATHERKIT_PRIVATE_KEY', label: 'WeatherKit Private Key', multiline: true },
    ],
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

/**
 * Per-provider settings modal: fetches `GET /api/plugins/:type/:id` only
 * while open (not on mount for every card in the grid), shows the
 * provider's description, any secret fields the static
 * `PROVIDER_SECRET_FIELDS` table says this provider needs, and — only for
 * sandboxed (WASM) providers — an allowed-hosts/allowed-secrets override
 * editor. The native, non-sandboxed "stormglass" tide provider omits the
 * allowlist block entirely.
 */
export function ProviderSettingsModal({ type, providerId, open, onOpenChange }: ProviderSettingsModalProps) {
  const { saveTouchedKeys } = useSecretsStatusContext()
  const [info, setInfo] = useState<PluginInfoResponse | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [hostsInput, setHostsInput] = useState('')
  const [secretsInput, setSecretsInput] = useState('')
  const [isSaving, setIsSaving] = useState(false)
  const [isResetting, setIsResetting] = useState(false)

  useEffect(() => {
    if (!open || !providerId) return

    setInfo(null)
    setError(null)
    setLoading(true)

    const fetchInfo = async () => {
      try {
        const response = await fetch(`/api/plugins/${type}/${providerId}`)
        if (!response.ok) {
          throw new Error(`HTTP error! status: ${response.status}`)
        }
        const data = (await response.json()) as PluginInfoResponse
        setInfo(data)
        setHostsInput(data.allowed_hosts.join(', '))
        setSecretsInput(data.allowed_secrets.join(', '))
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Unable to load plugin info')
      } finally {
        setLoading(false)
      }
    }

    void fetchInfo()
  }, [open, providerId, type])

  const applyResponse = (data: PluginInfoResponse) => {
    setInfo(data)
    setHostsInput(data.allowed_hosts.join(', '))
    setSecretsInput(data.allowed_secrets.join(', '))
  }

  const secretFields = providerId ? PROVIDER_SECRET_FIELDS[type]?.[providerId] : undefined

  // Single Save button for the whole modal: saves allowlist overrides (only
  // relevant for sandboxed/WASM providers) AND any touched secret field(s)
  // (only relevant for providers listed in PROVIDER_SECRET_FIELDS) in the
  // same click. Either branch is skipped when not applicable to this
  // provider — e.g. the native, non-sandboxed "stormglass" provider only
  // has secret fields and no allowlist, so only the secrets branch runs.
  const handleSave = async () => {
    if (!providerId) return
    setIsSaving(true)
    setError(null)
    try {
      const overridesPromise =
        info?.sandboxed === true
          ? fetch(`/api/plugins/${type}/${providerId}/overrides`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({
                allowed_hosts: parseList(hostsInput),
                allowed_secrets: parseList(secretsInput),
              }),
            }).then(async (response) => {
              if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`)
              }
              return (await response.json()) as PluginInfoResponse
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
      setError(err instanceof Error ? err.message : 'Unable to save settings')
    } finally {
      setIsSaving(false)
    }
  }

  const handleResetOverrides = async () => {
    if (!providerId) return
    setIsResetting(true)
    setError(null)
    try {
      const response = await fetch(`/api/plugins/${type}/${providerId}/overrides`, {
        method: 'DELETE',
      })
      if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`)
      }
      const data = (await response.json()) as PluginInfoResponse
      applyResponse(data)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Unable to reset allowlist overrides')
    } finally {
      setIsResetting(false)
    }
  }

  const hasSecretFields = Boolean(secretFields && secretFields.length > 0)
  const isSandboxed = info?.sandboxed === true
  // A single Save button is shown whenever there's anything for it to save
  // — either allowlist overrides (sandboxed providers) or secret fields
  // (providers listed in PROVIDER_SECRET_FIELDS), or both. Providers with
  // neither (most) show no Save button at all — no behavior change there.
  const showSaveButton = isSandboxed || hasSecretFields

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
                placeholder="STORMGLASS_API_KEY"
                aria-label="Allowed secrets"
              />
            </Field>
            <p className="text-[10px] text-muted-foreground">
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
