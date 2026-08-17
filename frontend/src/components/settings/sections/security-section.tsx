import { Field, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface SecuritySectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

/**
 * Authentication is delegated to the SignalK server (ADR 0040), so there is
 * one switch here and no credential fields — accounts live on SignalK.
 *
 * Turning it on is validated server-side before the save lands: the backend
 * refuses if SignalK's own security is switched off, which is the combination
 * that would otherwise lock the operator out of this very page.
 */
export function SecuritySection({ draft, onChange }: SecuritySectionProps) {
  const enabled = draft.authMode === 'signalk'

  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Authentication</FieldLegend>
        <div className="mt-3 space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={enabled}
              onCheckedChange={(checked) => onChange({ authMode: checked ? 'signalk' : 'none' })}
              aria-label="Require login"
            />
            <FieldLabel>Require login, using your SignalK server's accounts</FieldLabel>
          </Field>

          <FieldDescription>
            {enabled
              ? 'Anyone reaching this server must sign in with a SignalK account. Helmcentral keeps no accounts or passwords of its own.'
              : 'Anyone on the network this server is reachable from can read and control everything, including starting the generator and switching circuits. Fine on a trusted boat LAN; never expose it further.'}
          </FieldDescription>

          <FieldDescription>
            SignalK's own security must already be enabled (Server → Security). Saving is refused if it is not,
            since requiring a login against a server that has none would lock you out of this page.
          </FieldDescription>
        </div>
      </FieldSet>
    </div>
  )
}
