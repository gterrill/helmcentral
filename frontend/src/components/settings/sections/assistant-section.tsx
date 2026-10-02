import { useState } from 'react'
import { FormSection, SettingsLayout } from '@/components/patterns'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { Button } from '@/components/ui/button'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ModelCatalogDialog, ModelPicker } from '@/components/settings/sections/model-picker'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

interface AssistantSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

const noCostTierValue = '__none__'
const recentModelStorageKey = 'assistant.recent-models'
const recentDocumentModelStorageKey = 'assistant.recent-document-models'
// Keep in step with defaultDocumentModel in backend/assistant_settings.go.
const defaultDocumentModel = 'google/gemini-2.5-flash'
const costTierLabels: Record<string, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'XHigh',
  max: 'Max',
}
function upsertUnique(values: string[], value: string): string[] {
  const trimmed = value.trim()
  if (!trimmed) return values
  return values.includes(trimmed) ? values : [...values, trimmed]
}

function withoutValue(values: string[], value: string): string[] {
  return values.filter((item) => item !== value)
}

function shortModelName(id: string): string {
  const slash = id.lastIndexOf('/')
  return slash >= 0 && slash < id.length - 1 ? id.slice(slash + 1) : id
}

function ModelChipRow({
  label,
  listLabel,
  removeNoun,
  values,
  emptyText,
  onRemove,
}: {
  label: string
  listLabel: string
  removeNoun: string
  values: string[]
  emptyText: string
  onRemove: (id: string) => void
}) {
  return (
    <div className="flex items-start gap-3">
      <span className="w-16 shrink-0 pt-1 text-xs text-muted-foreground">{label}</span>
      <ul role="list" aria-label={listLabel} className="flex min-w-0 flex-1 flex-wrap gap-1.5">
        {values.length === 0 ? (
          <li className="pt-1 text-xs text-muted-foreground">{emptyText}</li>
        ) : (
          values.map((id) => (
            <li
              key={id}
              className="flex min-w-0 max-w-full items-center gap-1 rounded-md border border-border bg-muted py-0.5 pl-2 pr-1 text-xs text-foreground"
            >
              <span className="min-w-0 truncate" title={id}>
                {shortModelName(id)}
              </span>
              <button
                type="button"
                className="shrink-0 rounded-sm px-1 text-muted-foreground hover:text-foreground"
                aria-label={`Remove ${id} from ${removeNoun} models`}
                onClick={() => onRemove(id)}
              >
                ×
              </button>
            </li>
          ))
        )}
      </ul>
    </div>
  )
}

// ADR 0093: onboard assistant over OpenRouter (BYOK). Off by default,
// operator-triggered, key read from the encrypted secrets store, never
// via LoadIntoEnv, and standing notes injected verbatim into every system
// prompt.
export function AssistantSection({ draft, onChange }: AssistantSectionProps) {
  const [autoDialogOpen, setAutoDialogOpen] = useState(false)
  const isAutoModel = draft.assistantModel.trim().toLowerCase() === 'openrouter/auto'

  return (
    <SettingsLayout title="Mate">
      <FormSection title="Helmcentral AI Assistant">
        <FieldGroup>
          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantEnabled}
              onCheckedChange={(checked) => onChange({ assistantEnabled: checked })}
              aria-label="Enable Mate"
            />
            <FieldContent>
              <FieldLabel>Enable Mate</FieldLabel>
              <FieldDescription>
                Every question sends your position, the question itself, and forecast and tide excerpts to OpenRouter and
                whichever model provider you choose. Mate also reads your documents and notes to answer questions, and once
                it&apos;s on it summarises and indexes them for search on its own, no separate prompt per item. All of that
                text goes to OpenRouter too, billed to your key. Every reply shows its cost.
              </FieldDescription>
            </FieldContent>
          </Field>
        </FieldGroup>
      </FormSection>

      {draft.assistantEnabled ? (
        <>
          <FormSection title="Chat">
            <FieldGroup>
              <Field orientation="horizontal">
                <Switch
                  checked={isAutoModel}
                  onCheckedChange={(checked) =>
                    onChange({ assistantModel: checked ? 'openrouter/auto' : 'anthropic/claude-sonnet-4.5' })
                  }
                  aria-label="Use OpenRouter Auto"
                />
                <FieldLabel>Use OpenRouter Auto</FieldLabel>
              </Field>

              {!isAutoModel ? (
                <ModelPicker
                  id="assistant-model"
                  ariaLabel="Mate model"
                  value={draft.assistantModel}
                  onChange={(assistantModel) => onChange({ assistantModel })}
                  recentStorageKey={recentModelStorageKey}
                  capability="tools"
                  description="Select a recent model, or open the catalog to choose a new tool-capable model."
                  dialogDescription="Tool-capable models with server-side sorting and pagination."
                />
              ) : null}
            </FieldGroup>
          </FormSection>

          {isAutoModel ? (
            <FormSection title="Auto">
              <FieldGroup>
                <div className="flex items-center justify-between gap-2">
                  <FieldLabel htmlFor="assistant-auto-model-manager">Model filters</FieldLabel>
                  <Button
                    id="assistant-auto-model-manager"
                    type="button"
                    variant="outline"
                    size="sm"
                    onClick={() => setAutoDialogOpen(true)}
                    aria-label="Manage Auto model filters"
                  >
                    Manage…
                  </Button>
                </div>

                <ModelChipRow
                  label="Allowed"
                  listLabel="Mate allowed models"
                  removeNoun="allowed"
                  values={draft.assistantAllowedModels}
                  emptyText="Any tool-capable model"
                  onRemove={(id) =>
                    onChange({ assistantAllowedModels: withoutValue(draft.assistantAllowedModels, id) })
                  }
                />

                <ModelChipRow
                  label="Excluded"
                  listLabel="Mate excluded models"
                  removeNoun="excluded"
                  values={draft.assistantExcludedModels}
                  emptyText="None"
                  onRemove={(id) =>
                    onChange({ assistantExcludedModels: withoutValue(draft.assistantExcludedModels, id) })
                  }
                />

                <Field>
                  <FieldLabel htmlFor="assistant-cost-tier">Cost cap</FieldLabel>
                  <Select
                    value={draft.assistantCostTier === '' ? noCostTierValue : draft.assistantCostTier}
                    onValueChange={(value) =>
                      onChange({
                        assistantCostTier: (value === noCostTierValue ? '' : value ?? '') as RegularSettingsDraft['assistantCostTier'],
                      })
                    }
                  >
                    <SelectTrigger id="assistant-cost-tier" aria-label="Mate Auto cost tier">
                      <SelectValue>{(value: string) => (value === noCostTierValue ? 'No cost cap' : (costTierLabels[value] ?? value))}</SelectValue>
                    </SelectTrigger>
                    <SelectPopup>
                      <SelectItem value={noCostTierValue}>No cost cap</SelectItem>
                      <SelectItem value="low">Low</SelectItem>
                      <SelectItem value="medium">Medium</SelectItem>
                      <SelectItem value="high">High</SelectItem>
                      <SelectItem value="xhigh">XHigh</SelectItem>
                      <SelectItem value="max">Max</SelectItem>
                    </SelectPopup>
                  </Select>
                </Field>
              </FieldGroup>
            </FormSection>
          ) : null}


          <FormSection title="Document indexing">
            <FieldGroup>
              <ModelPicker
                id="assistant-document-model"
                ariaLabel="Document indexing model"
                value={draft.assistantDocumentModel}
                onChange={(assistantDocumentModel) => onChange({ assistantDocumentModel })}
                recentStorageKey={recentDocumentModelStorageKey}
                capability="images"
                emptyLabel={`Default (${defaultDocumentModel})`}
                description="Reads every document and photo you add, including text in scans, and suggests its title, summary and tags. It runs on its own over the whole library, so pick a cheap model that can read images. It doesn't need to be the model Mate answers with."
                dialogDescription="Models that can read images, with server-side sorting and pagination."
              />
            </FieldGroup>
          </FormSection>

          <FormSection title="OpenRouter API key">
            <SecretFieldGroup fields={[{ key: 'OPENROUTER_API_KEY', label: 'OpenRouter API key' }]} />
          </FormSection>

          <FormSection title="Standing notes">
            <FieldGroup>
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
            </FieldGroup>
          </FormSection>

          {/* ADR 0093 voice phase: push-to-talk input, reading the spoken summary
              back aloud, and the always-listening "Hey Mate" wake word - each its
              own switch since each has its own cost (an https requirement, a
              voice interrupting the cabin, battery and a cloud-speech vendor). */}
          <FormSection title="Voice">
            <FieldGroup>
              <Field orientation="horizontal">
                <Switch
                  checked={draft.assistantVoiceInput}
                  onCheckedChange={(checked) => onChange({ assistantVoiceInput: checked })}
                  aria-label="Voice input"
                />
                <FieldContent>
                  <FieldLabel>Voice input</FieldLabel>
                  <FieldDescription>
                    A microphone button in the header. Needs the app opened over https.
                  </FieldDescription>
                </FieldContent>
              </Field>

              <Field orientation="horizontal">
                <Switch
                  checked={draft.assistantReadAloud}
                  onCheckedChange={(checked) => onChange({ assistantReadAloud: checked })}
                  aria-label="Read replies aloud"
                />
                <FieldContent>
                  <FieldLabel>Read replies aloud</FieldLabel>
                  <FieldDescription>Mate reads the spoken summary of each reply.</FieldDescription>
                </FieldContent>
              </Field>

              <Field orientation="horizontal">
                <Switch
                  checked={draft.assistantWakeWord}
                  onCheckedChange={(checked) => onChange({ assistantWakeWord: checked })}
                  aria-label="Listen for Hey Mate"
                />
                <FieldContent>
                  <FieldLabel>Listen for Hey Mate</FieldLabel>
                  <FieldDescription>
                    Keeps the microphone open while the app is on screen and answers when you say Hey Mate. Uses battery and,
                    in Chrome, sends audio to Google; off by default.
                  </FieldDescription>
                </FieldContent>
              </Field>
            </FieldGroup>
          </FormSection>
        </>
      ) : null}

      {isAutoModel ? (
        <ModelCatalogDialog
          open={autoDialogOpen}
          onOpenChange={setAutoDialogOpen}
          capability="tools"
          title="Manage Auto model filters"
          description="Include or exclude tool-capable models for OpenRouter Auto."
          renderBadges={(model) => (
            <>
              {draft.assistantAllowedModels.includes(model.id) ? (
                <span className="mt-1 inline-flex rounded-xs bg-secondary px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-secondary-foreground">
                  Allowed
                </span>
              ) : null}
              {draft.assistantExcludedModels.includes(model.id) ? (
                <span className="mt-1 inline-flex rounded-xs bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                  Excluded
                </span>
              ) : null}
            </>
          )}
          renderActions={(model) => (
            <div className="flex items-center justify-end gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={draft.assistantAllowedModels.includes(model.id)}
                onClick={() => {
                  onChange({
                    assistantAllowedModels: upsertUnique(draft.assistantAllowedModels, model.id),
                    assistantExcludedModels: withoutValue(draft.assistantExcludedModels, model.id),
                  })
                }}
              >
                {draft.assistantAllowedModels.includes(model.id) ? 'Included' : 'Include'}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={draft.assistantExcludedModels.includes(model.id)}
                onClick={() => {
                  onChange({
                    assistantExcludedModels: upsertUnique(draft.assistantExcludedModels, model.id),
                    assistantAllowedModels: withoutValue(draft.assistantAllowedModels, model.id),
                  })
                }}
              >
                {draft.assistantExcludedModels.includes(model.id) ? 'Excluded' : 'Exclude'}
              </Button>
            </div>
          )}
        />
      ) : null}
    </SettingsLayout>
  )
}
