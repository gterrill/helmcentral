import { Field, FieldDescription, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'

interface AssistantSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

// ADR 0093: onboard assistant over OpenRouter (BYOK). Off by default,
// operator-triggered, key read from the encrypted secrets store, never
// via LoadIntoEnv, and standing notes injected verbatim into every system
// prompt.
export function AssistantSection({ draft, onChange }: AssistantSectionProps) {
  return (
    <div className="mx-auto max-w-3xl space-y-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Assistant</FieldLegend>
        <div className="space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantEnabled}
              onCheckedChange={(checked) => onChange({ assistantEnabled: checked })}
              aria-label="Enable assistant"
            />
            <FieldLabel>Enable the onboard assistant</FieldLabel>
          </Field>

          <Field>
            <FieldLabel htmlFor="assistant-model">Model</FieldLabel>
            <Input
              id="assistant-model"
              value={draft.assistantModel}
              onChange={(e) => onChange({ assistantModel: e.target.value })}
              aria-label="Assistant model"
            />
            <FieldDescription>Any OpenRouter model id that supports tool calling</FieldDescription>
          </Field>
        </div>
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">OpenRouter API key</FieldLegend>
        <SecretFieldGroup fields={[{ key: 'OPENROUTER_API_KEY', label: 'OpenRouter API key' }]} />
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Standing notes</FieldLegend>
        <Field>
          <Textarea
            id="assistant-notes"
            value={draft.assistantNotes}
            onChange={(e) => onChange({ assistantNotes: e.target.value })}
            aria-label="Assistant standing notes"
            rows={8}
            maxLength={8000}
          />
          <FieldDescription>
            Sent with every question. Put your own tide, fishing and anchorage rules here.
          </FieldDescription>
        </Field>
      </FieldSet>

      <FieldDescription>
        Every question sends your position, the question itself, and forecast and tide excerpts to
        OpenRouter and whichever model provider you choose. Every reply shows its cost.
      </FieldDescription>
    </div>
  )
}
