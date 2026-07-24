import { useState } from 'react'

import { Button } from '@/components/ui/button'
import { Field, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

const apiBaseUrl = import.meta.env.VITE_API_BASE_URL ?? `${window.location.protocol}//${window.location.hostname}:8080`

interface SignalKConnectionSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

export function SignalKConnectionSection({ draft, onChange }: SignalKConnectionSectionProps) {
  const [isTesting, setIsTesting] = useState(false)
  const [testError, setTestError] = useState<string | null>(null)
  const [testSuccess, setTestSuccess] = useState<string | null>(null)

  // A pure diagnostic against `/api/settings/signalk/test`: it probes the
  // address currently in the draft and reports what answered, without saving
  // anything. Persisting is the pinned "Save Settings" button's job alone
  // (which re-validates a changed address server-side), so there is exactly
  // one write path for this field — see ADR 0028. Deliberately does NOT call
  // onChange: the operator's typed value is the input to the test, not
  // something the test gets to overwrite.
  const testConnection = async () => {
    setIsTesting(true)
    setTestError(null)
    setTestSuccess(null)

    const normalizedPort = Number.parseInt(draft.signalkPort, 10)
    const payload = {
      address: draft.signalkAddress.trim(),
      port: Number.isFinite(normalizedPort) ? normalizedPort : 3000,
    }

    try {
      const response = await fetch(`${apiBaseUrl}/api/settings/signalk/test`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      })

      if (!response.ok) {
        const errorPayload = (await response.json().catch(() => null)) as { error?: string } | null
        throw new Error(errorPayload?.error ?? 'Unable to connect to SignalK')
      }

      const data = (await response.json()) as { vessel_name?: string }
      const vesselName = data.vessel_name?.trim()
      // Naming the vessel that answered is the point: "connected" alone
      // can't tell your boat apart from another server on the same network.
      setTestSuccess(
        vesselName
          ? `Connected — ${vesselName} responded. Save Settings to apply.`
          : 'Connected. Save Settings to apply.',
      )
    } catch (error) {
      setTestError(error instanceof Error ? error.message : 'Unable to connect to SignalK')
    } finally {
      setIsTesting(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">SignalK Connection</FieldLegend>

        <div className="mt-3 grid grid-cols-1 gap-2 md:grid-cols-[minmax(0,1fr)_minmax(0,180px)_auto]">
          <Field>
            <FieldLabel htmlFor="signalk-address">Address</FieldLabel>
            <Input
              id="signalk-address"
              value={draft.signalkAddress}
              onChange={(e) => onChange({ signalkAddress: e.target.value })}
              aria-label="SignalK address"
            />
          </Field>

          <Field>
            <FieldLabel htmlFor="signalk-port">Port</FieldLabel>
            <Input
              id="signalk-port"
              value={draft.signalkPort}
              onChange={(e) => onChange({ signalkPort: e.target.value })}
              aria-label="SignalK port"
            />
          </Field>

          <Button
            variant="outline"
            className="h-10 whitespace-nowrap border-primary/55 px-4 font-display text-xs tracking-[0.14em] text-primary"
            onClick={testConnection}
            disabled={isTesting}
          >
            {isTesting ? 'Testing' : 'Test Connection'}
          </Button>

          {/* Directly under the button that produces them. The section runs
              long (credentials, then a tank label per sensor), so a banner at
              the bottom of the container is off-screen from the control the
              operator just clicked — feedback they have to go looking for is
              feedback they won't see. */}
          {testError && (
            <div className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs uppercase tracking-[0.08em] text-destructive md:col-span-3">
              {testError}
            </div>
          )}
          {testSuccess && (
            <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs uppercase tracking-[0.08em] text-emerald-600 md:col-span-3">
              {testSuccess}
            </div>
          )}

          <Field className="md:col-span-3">
            <FieldLabel htmlFor="signalk-refresh-seconds">Refresh Seconds</FieldLabel>
            <Input
              id="signalk-refresh-seconds"
              value={draft.vesselStateRefreshSeconds}
              onChange={(e) => onChange({ vesselStateRefreshSeconds: e.target.value })}
              aria-label="Vessel state refresh seconds"
            />
          </Field>

          <Field className="md:col-span-3">
            <FieldLabel htmlFor="forecast-refresh-seconds">Forecast Refresh Seconds</FieldLabel>
            <Input
              id="forecast-refresh-seconds"
              value={draft.forecastRefreshSeconds}
              onChange={(e) => onChange({ forecastRefreshSeconds: e.target.value })}
              aria-label="Forecast refresh seconds"
            />
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">SignalK Credentials</FieldLegend>
        <div className="mt-3">
          <SecretFieldGroup
            fields={[
              { key: 'SIGNALK_USERNAME', label: 'SignalK Username' },
              { key: 'SIGNALK_PASSWORD', label: 'SignalK Password' },
            ]}
          />
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Labels</FieldLegend>
        <div className="mt-3 grid grid-cols-1 gap-2">
          {Object.keys(draft.tankLabels).sort((a, b) => a.localeCompare(b, undefined, { numeric: true })).map((id) => (
            <Field key={id}>
              <FieldLabel htmlFor={`tank-label-${id}`}>{`Tank Label ${id}`}</FieldLabel>
              <Input
                id={`tank-label-${id}`}
                value={draft.tankLabels[id] ?? ''}
                onChange={(e) => onChange({ tankLabels: { ...draft.tankLabels, [id]: e.target.value } })}
                aria-label={`Tank label ${id}`}
              />
            </Field>
          ))}
        </div>
      </FieldSet>

    </div>
  )
}
