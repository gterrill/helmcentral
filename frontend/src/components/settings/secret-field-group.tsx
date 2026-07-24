import { Button } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { useSecretsStatusContext } from '@/components/settings/secrets-status-context'
import type { SecretKey } from '@/hooks/use-secrets-status'

export interface SecretFieldSpec {
  key: SecretKey
  label: string
  multiline?: boolean
}

interface SecretFieldGroupProps {
  fields: SecretFieldSpec[]
}

/**
 * Renders a small, purely presentational group of secret fields
 * (password/text inputs, or a monospace textarea for multiline PEM-style
 * secrets), each with its own "touched" tracking (owned by
 * useSecretsStatusContext, shared across every consumer) and a per-field
 * Clear-with-confirm action. Deliberately has NO Save button of its own —
 * saving touched values here is the responsibility of whichever caller
 * embeds this group (the settings page's page-level "Save Settings" button
 * for inline sections, or a provider-settings modal's own Save button),
 * via `saveTouchedKeys` from the shared context.
 */
export function SecretFieldGroup({ fields }: SecretFieldGroupProps) {
  const { status, values, touched, setFieldValue, clearKey } = useSecretsStatusContext()

  return (
    <div className="grid grid-cols-1 gap-2">
      {fields.map((field) => (
        <SecretField
          key={field.key}
          field={field}
          value={values[field.key] ?? ''}
          isSet={status[field.key] ?? false}
          isTouched={touched[field.key] ?? false}
          onChange={(value) => setFieldValue(field.key, value)}
          onClear={() => clearKey(field.key, field.label)}
        />
      ))}
    </div>
  )
}

interface SecretFieldProps {
  field: SecretFieldSpec
  value: string
  isSet: boolean
  isTouched: boolean
  onChange: (value: string) => void
  onClear: () => Promise<Record<SecretKey, boolean> | null>
}

function SecretField({ field, value, isSet, isTouched, onChange, onClear }: SecretFieldProps) {
  const showClear = isSet || isTouched

  const handleClear = () => {
    void (async () => {
      try {
        await onClear()
      } catch (err) {
        // No error banner lives in this component anymore (saving/clearing
        // state moved to the shared secrets-status hook), but a failed
        // clear must still be surfaced rather than silently swallowed —
        // per this repo's fail-fast/no-masking-fallback policy.
        window.alert(err instanceof Error ? err.message : 'Unable to clear secret')
      }
    })()
  }

  if (field.multiline) {
    return (
      <Field>
        <FieldLabel htmlFor={field.key}>{field.label}</FieldLabel>
        <div className="flex gap-2">
          <Textarea
            id={field.key}
            value={value}
            onChange={(e) => onChange(e.target.value)}
            placeholder={isSet ? '-----BEGIN PRIVATE KEY----- (set)' : '-----BEGIN PRIVATE KEY----- (not set)'}
            aria-label={field.label}
            rows={6}
            className="font-mono text-[11px]"
          />
          {showClear && (
            <Button
              type="button"
              variant="ghost"
              className="h-8 px-2 text-[10px] uppercase tracking-wide text-destructive"
              onClick={handleClear}
            >
              Clear
            </Button>
          )}
        </div>
      </Field>
    )
  }

  return (
    <Field>
      <FieldLabel htmlFor={field.key}>{field.label}</FieldLabel>
      <div className="flex gap-2">
        <Input
          id={field.key}
          type="password"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          placeholder={isSet ? '•••• (set)' : 'not set'}
          aria-label={field.label}
        />
        {showClear && (
          <Button
            type="button"
            variant="ghost"
            className="h-8 px-2 text-[10px] uppercase tracking-wide text-destructive"
            onClick={handleClear}
          >
            Clear
          </Button>
        )}
      </div>
    </Field>
  )
}
