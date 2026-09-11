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
        <FieldLegend variant="label">Mate</FieldLegend>
        <div className="space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantEnabled}
              onCheckedChange={(checked) => onChange({ assistantEnabled: checked })}
              aria-label="Enable Mate"
            />
            <FieldLabel>Enable Mate</FieldLabel>
          </Field>

          <Field>
            <FieldLabel htmlFor="assistant-model">Model</FieldLabel>
            <Input
              id="assistant-model"
              value={draft.assistantModel}
              onChange={(e) => onChange({ assistantModel: e.target.value })}
              aria-label="Mate model"
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
            aria-label="Mate standing notes"
            rows={8}
            maxLength={8000}
          />
          <FieldDescription>
            Sent with every question you ask Mate. Put your own tide, fishing and anchorage rules here.
          </FieldDescription>
        </Field>
      </FieldSet>

      {/* ADR 0093 voice phase: push-to-talk input, reading the spoken summary
          back aloud, and the always-listening "Hey Mate" wake word - each its
          own switch since each has its own cost (an https requirement, a
          voice interrupting the cabin, battery and a cloud-speech vendor). */}
      <FieldSet>
        <FieldLegend variant="label">Voice</FieldLegend>
        <div className="space-y-3">
          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantVoiceInput}
              onCheckedChange={(checked) => onChange({ assistantVoiceInput: checked })}
              aria-label="Voice input"
            />
            <FieldLabel>Voice input</FieldLabel>
          </Field>
          <FieldDescription>
            A microphone button in the header. Needs the app opened over https.
          </FieldDescription>

          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantReadAloud}
              onCheckedChange={(checked) => onChange({ assistantReadAloud: checked })}
              aria-label="Read replies aloud"
            />
            <FieldLabel>Read replies aloud</FieldLabel>
          </Field>
          <FieldDescription>Mate reads the spoken summary of each reply.</FieldDescription>

          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantWakeWord}
              onCheckedChange={(checked) => onChange({ assistantWakeWord: checked })}
              aria-label="Listen for Hey Mate"
            />
            <FieldLabel>Listen for Hey Mate</FieldLabel>
          </Field>
          <FieldDescription>
            Keeps the microphone open while the app is on screen and answers when you say Hey Mate. Uses
            battery and, in Chrome, sends audio to Google; off by default.
          </FieldDescription>
        </div>
      </FieldSet>

      <FieldDescription>
        Every question sends your position, the question itself, and forecast and tide excerpts to
        OpenRouter and whichever model provider you choose. Every reply shows its cost.
      </FieldDescription>
    </div>
  )
}
