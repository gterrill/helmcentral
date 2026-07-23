import { useEffect, useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

type SecretKey =
  | 'SIGNALK_USERNAME'
  | 'SIGNALK_PASSWORD'
  | 'INFLUXDB_TOKEN'
  | 'STORMGLASS_API_KEY'
  | 'GEONAMES_USERNAME'
  | 'WEATHERKIT_KEY_ID'
  | 'WEATHERKIT_TEAM_ID'
  | 'WEATHERKIT_SERVICE_ID'
  | 'WEATHERKIT_PRIVATE_KEY'

const SECRET_KEYS: SecretKey[] = [
  'SIGNALK_USERNAME',
  'SIGNALK_PASSWORD',
  'INFLUXDB_TOKEN',
  'STORMGLASS_API_KEY',
  'GEONAMES_USERNAME',
  'WEATHERKIT_KEY_ID',
  'WEATHERKIT_TEAM_ID',
  'WEATHERKIT_SERVICE_ID',
  'WEATHERKIT_PRIVATE_KEY',
]

const HUMAN_READABLE_NAMES: Record<SecretKey, string> = {
  SIGNALK_USERNAME: 'SignalK Username',
  SIGNALK_PASSWORD: 'SignalK Password',
  INFLUXDB_TOKEN: 'InfluxDB Token',
  STORMGLASS_API_KEY: 'Storm Glass API Key',
  GEONAMES_USERNAME: 'GeoNames Username',
  WEATHERKIT_KEY_ID: 'WeatherKit Key ID',
  WEATHERKIT_TEAM_ID: 'WeatherKit Team ID',
  WEATHERKIT_SERVICE_ID: 'WeatherKit Service ID',
  WEATHERKIT_PRIVATE_KEY: 'WeatherKit Private Key',
}

const FIELDSETS: Array<{ legend: string; keys: SecretKey[] }> = [
  { legend: 'SignalK Credentials', keys: ['SIGNALK_USERNAME', 'SIGNALK_PASSWORD'] },
  { legend: 'InfluxDB', keys: ['INFLUXDB_TOKEN'] },
  { legend: 'Storm Glass', keys: ['STORMGLASS_API_KEY'] },
  { legend: 'GeoNames', keys: ['GEONAMES_USERNAME'] },
  {
    legend: 'WeatherKit',
    keys: ['WEATHERKIT_KEY_ID', 'WEATHERKIT_TEAM_ID', 'WEATHERKIT_SERVICE_ID', 'WEATHERKIT_PRIVATE_KEY'],
  },
]

export function SecretsSettingsPanel() {
  const [secretStatus, setSecretStatus] = useState<Record<SecretKey, boolean>>({
    SIGNALK_USERNAME: false,
    SIGNALK_PASSWORD: false,
    INFLUXDB_TOKEN: false,
    STORMGLASS_API_KEY: false,
    GEONAMES_USERNAME: false,
    WEATHERKIT_KEY_ID: false,
    WEATHERKIT_TEAM_ID: false,
    WEATHERKIT_SERVICE_ID: false,
    WEATHERKIT_PRIVATE_KEY: false,
  })

  const [fieldValues, setFieldValues] = useState<Record<SecretKey, string>>(
    Object.fromEntries(SECRET_KEYS.map((key) => [key, ''])) as Record<SecretKey, string>,
  )

  const [touchedKeys, setTouchedKeys] = useState<Record<SecretKey, boolean>>(
    Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>,
  )

  const [isSaving, setIsSaving] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saveSuccess, setSaveSuccess] = useState<string | null>(null)
  const [importError, setImportError] = useState<string | null>(null)
  const [importResult, setImportResult] = useState<string | null>(null)
  const [initialLoadError, setInitialLoadError] = useState<string | null>(null)

  const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

  // Fetch on mount
  useEffect(() => {
    const fetchSecrets = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings/secrets`)
        if (!response.ok) {
          throw new Error('Failed to fetch secrets')
        }
        const data = (await response.json()) as Record<SecretKey, boolean>
        setSecretStatus(data)
      } catch (error) {
        setInitialLoadError(error instanceof Error ? error.message : 'Unable to load secrets')
      }
    }

    void fetchSecrets()
  }, [apiBaseUrl])

  const handleFieldChange = (key: SecretKey, value: string) => {
    setFieldValues((prev) => ({ ...prev, [key]: value }))
    setTouchedKeys((prev) => ({ ...prev, [key]: true }))
  }

  const clearField = (key: SecretKey) => {
    const humanName = HUMAN_READABLE_NAMES[key]
    if (!window.confirm(`Clear ${humanName}? This cannot be undone.`)) {
      return
    }

    const clearAsync = async () => {
      try {
        const response = await fetch(`${apiBaseUrl}/api/settings/secrets`, {
          method: 'POST',
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ [key]: '' }),
        })

        if (!response.ok) {
          const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
          throw new Error(errorPayload?.error ?? 'Unable to clear secret')
        }

        const data = (await response.json()) as Record<SecretKey, boolean>
        setSecretStatus(data)
        setFieldValues((prev) => ({ ...prev, [key]: '' }))
        setTouchedKeys((prev) => ({ ...prev, [key]: false }))
        setSaveSuccess(`${humanName} cleared`)
        setImportError(null)
        setImportResult(null)
      } catch (error) {
        setSaveError(error instanceof Error ? error.message : 'Unable to clear secret')
        setImportError(null)
        setImportResult(null)
      }
    }

    void clearAsync()
  }

  const saveSecrets = async () => {
    setIsSaving(true)
    setSaveError(null)
    setSaveSuccess(null)
    setImportError(null)
    setImportResult(null)

    const payload: Partial<Record<SecretKey, string>> = {}
    for (const key of SECRET_KEYS) {
      if (touchedKeys[key]) {
        payload[key] = fieldValues[key]
      }
    }

    // If no keys are touched, don't fetch
    if (Object.keys(payload).length === 0) {
      setIsSaving(false)
      return
    }

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings/secrets`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to save secrets')
      }

      const data = (await response.json()) as Record<SecretKey, boolean>
      setSecretStatus(data)
      setFieldValues(Object.fromEntries(SECRET_KEYS.map((key) => [key, ''])) as Record<SecretKey, string>)
      setTouchedKeys(Object.fromEntries(SECRET_KEYS.map((key) => [key, false])) as Record<SecretKey, boolean>)
      setSaveSuccess('Secrets saved')
    } catch (error) {
      setSaveError(error instanceof Error ? error.message : 'Unable to save secrets')
    } finally {
      setIsSaving(false)
    }
  }

  const importFromEnv = async () => {
    setIsImporting(true)
    setImportError(null)
    setImportResult(null)
    setSaveError(null)
    setSaveSuccess(null)

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings/secrets/import-env`, {
        method: 'POST',
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to import from environment')
      }

      const data = (await response.json()) as { imported: string[] }

      if (data.imported.length === 0) {
        setImportResult('Nothing to import')
      } else {
        setImportResult(`Imported: ${data.imported.join(', ')}`)
      }

      // Refetch secrets to sync state
      const secretsResponse = await fetch(`${apiBaseUrl}/api/settings/secrets`)
      if (secretsResponse.ok) {
        const secretsData = (await secretsResponse.json()) as Record<SecretKey, boolean>
        setSecretStatus(secretsData)
      }
    } catch (error) {
      setImportError(error instanceof Error ? error.message : 'Unable to import from environment')
    } finally {
      setIsImporting(false)
    }
  }

  const renderField = (key: SecretKey) => {
    const humanName = HUMAN_READABLE_NAMES[key]
    const isPassword = key !== 'WEATHERKIT_PRIVATE_KEY'
    const isSet = secretStatus[key]
    const value = fieldValues[key]
    const isTouched = touchedKeys[key]
    const showClear = isSet || isTouched

    if (key === 'WEATHERKIT_PRIVATE_KEY') {
      return (
        <Field key={key}>
          <FieldLabel htmlFor={key}>{humanName}</FieldLabel>
          <div className="flex gap-2">
            <Textarea
              id={key}
              value={value}
              onChange={(e) => handleFieldChange(key, e.target.value)}
              placeholder={isSet ? '-----BEGIN PRIVATE KEY----- (set)' : '-----BEGIN PRIVATE KEY----- (not set)'}
              aria-label={humanName}
              rows={6}
              className="font-mono text-[11px]"
            />
            {showClear && (
              <Button
                type="button"
                variant="ghost"
                className="h-8 px-2 text-[10px] uppercase tracking-wide text-destructive"
                onClick={() => clearField(key)}
              >
                Clear
              </Button>
            )}
          </div>
        </Field>
      )
    }

    return (
      <Field key={key}>
        <FieldLabel htmlFor={key}>{humanName}</FieldLabel>
        <div className="flex gap-2">
          <Input
            id={key}
            type={isPassword ? 'password' : 'text'}
            value={value}
            onChange={(e) => handleFieldChange(key, e.target.value)}
            placeholder={isSet ? '•••• (set)' : 'not set'}
            aria-label={humanName}
          />
          {showClear && (
            <Button
              type="button"
              variant="ghost"
              className="h-8 px-2 text-[10px] uppercase tracking-wide text-destructive"
              onClick={() => clearField(key)}
            >
              Clear
            </Button>
          )}
        </div>
      </Field>
    )
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      {initialLoadError && (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
          {initialLoadError}
        </div>
      )}

      {FIELDSETS.map(({ legend, keys }) => (
        <FieldSet key={legend}>
          <FieldLegend variant="label">{legend}</FieldLegend>
          <div className="mt-3 grid grid-cols-1 gap-2">{keys.map(renderField)}</div>
        </FieldSet>
      ))}

      <div className="flex justify-between">
        <Button
          type="button"
          variant="ghost"
          className="text-[11px] text-muted-foreground hover:text-primary"
          onClick={importFromEnv}
          disabled={isImporting}
        >
          {isImporting ? 'Importing...' : 'Import from environment'}
        </Button>
        <Button
          variant="outline"
          className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
          onClick={saveSecrets}
          disabled={isSaving}
        >
          {isSaving ? 'Saving' : 'Save Secrets'}
        </Button>
      </div>

      {saveError && (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
          {saveError}
        </div>
      )}
      {saveSuccess && (
        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
          {saveSuccess}
        </div>
      )}
      {importError && (
        <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive">
          {importError}
        </div>
      )}
      {importResult && (
        <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600">
          {importResult}
        </div>
      )}
    </div>
  )
}
