import { useState } from 'react'
import { FormSection, SettingsLayout } from '@/components/patterns'
import { useSetupFocus } from '@/hooks/use-setup-focus'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import {
  defaultWebSearchModel,
  passageLimitErrors,
  type PassageLimitsDraft,
  type RegularSettingsDraft,
} from '@/components/settings/settings-draft'
import { Button } from '@/components/ui/button'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { ModelCatalogDialog, ModelPicker } from '@/components/settings/sections/model-picker'
import { Switch } from '@/components/ui/switch'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'

const isAssistantSetupControl = (control: string) => control === 'passage-limits'

interface AssistantSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

const noCostTierValue = '__none__'
const recentModelStorageKey = 'assistant.recent-models'
const recentDocumentModelStorageKey = 'assistant.recent-document-models'
const recentWebSearchModelStorageKey = 'assistant.recent-web-search-models'
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

type SectorKey = 'ahead' | 'abeam' | 'astern'
type MeasureKey = 'wind' | 'wave'

const passageSectors: Array<{ key: SectorKey; label: string; angles: string }> = [
  { key: 'ahead', label: 'Ahead', angles: '0–60°' },
  { key: 'abeam', label: 'Abeam', angles: '60–120°' },
  { key: 'astern', label: 'Astern', angles: '120–180°' },
]

const passageMeasures: Array<{ key: MeasureKey; column: string; name: string; unit: string }> = [
  { key: 'wind', column: 'No-go wind', name: 'no-go wind', unit: 'kn' },
  { key: 'wave', column: 'No-go wave height', name: 'no-go wave height', unit: 'm' },
]

function LimitInput({
  label,
  value,
  invalid,
  onChange,
}: {
  label: string
  value: string
  invalid: boolean
  onChange: (value: string) => void
}) {
  return (
    <Input
      type="text"
      inputMode="decimal"
      value={value}
      aria-label={label}
      aria-invalid={invalid || undefined}
      onChange={(e) => onChange(e.target.value)}
      className="h-9 min-w-0 px-2 text-sm tabular-nums"
    />
  )
}

// Sector rows by no-go columns. Below sm each sector stacks and every
// measure carries its own label, so nothing runs off a phone screen.
function PassageLimitsFields({
  limits,
  errors,
  onChange,
}: {
  limits: PassageLimitsDraft
  errors: Record<string, string>
  onChange: (next: PassageLimitsDraft) => void
}) {
  const setSector = (key: SectorKey, measure: MeasureKey, value: string) =>
    onChange({ ...limits, [key]: { ...limits[key], [measure]: value } })

  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="hidden gap-3 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground sm:grid sm:grid-cols-[7rem_repeat(2,minmax(0,1fr))]">
        <span />
        {passageMeasures.map((m) => (
          <span key={m.key}>
            {m.column} {m.unit}
          </span>
        ))}
      </div>
      {passageSectors.map((sector) => (
        <div
          key={sector.key}
          className="grid min-w-0 gap-2 rounded-md border border-border p-2 sm:grid-cols-[7rem_repeat(2,minmax(0,1fr))] sm:items-center sm:gap-3 sm:border-0 sm:p-0"
        >
          <div className="min-w-0">
            <p className="truncate text-sm font-medium text-foreground">{sector.label}</p>
            <p className="truncate text-xs text-muted-foreground">{sector.angles}</p>
          </div>
          {passageMeasures.map((m) => (
            <div key={m.key} className="flex min-w-0 flex-col gap-1">
              <span className="text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground sm:hidden">
                {m.column} {m.unit}
              </span>
              <LimitInput
                label={`${sector.label} ${m.name} (${m.unit})`}
                value={limits[sector.key][m.key]}
                invalid={`${sector.key}.${m.key}` in errors}
                onChange={(value) => setSector(sector.key, m.key, value)}
              />
            </div>
          ))}
        </div>
      ))}

      <div className="flex min-w-0 flex-col gap-1.5 border-t border-border pt-3">
        <p className="text-sm font-medium text-foreground">Wind against tide</p>
        <p className="text-xs text-muted-foreground">
          While the tidal stream is running against the wind, through the strongest part of the flood or ebb, the
          no-go wind above is lowered by this many knots, and marginal follows at 70% of the lowered figure. Gust
          limits are not changed. Only used for legs with a known flood direction. Leave it blank to skip the check.
        </p>
        <div className="max-w-xs">
          <LimitInput
            label="Reduce wind limits by (kn)"
            value={limits.tideOffset}
            invalid={'tide' in errors}
            onChange={(tideOffset) => onChange({ ...limits, tideOffset })}
          />
        </div>
      </div>

      {Object.keys(errors).length > 0 ? (
        <ul role="alert" className="flex flex-col gap-0.5">
          {Object.entries(errors).map(([key, message]) => (
            <li key={key} className="text-xs text-destructive">
              {message}
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  )
}

// ADR 0093: onboard assistant over OpenRouter (BYOK). Off by default,
// operator-triggered, key read from the encrypted secrets store, never
// via LoadIntoEnv, and standing notes injected verbatim into every system
// prompt.
export function AssistantSection({ draft, onChange }: AssistantSectionProps) {
  // The setup checklist can ask for the passage limits group to be focused.
  useSetupFocus(isAssistantSetupControl)
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
                text goes to OpenRouter too, billed to your key. Every reply shows its cost. Mate also reviews every watch
                report, so each run uses your OpenRouter key.
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

          <FormSection title="Diagnostics">
            <FieldGroup>
              <Field orientation="horizontal">
                <Switch
                  checked={draft.assistantVerboseRunLog}
                  onCheckedChange={(checked) => onChange({ assistantVerboseRunLog: checked })}
                  aria-label="Verbose run log"
                />
                <FieldContent>
                  <FieldLabel>Verbose run log</FieldLabel>
                  <FieldDescription>
                    Writes a line to the server log for every round of every Mate run: how long it took, what it cost, how
                    many lookups it made and how much time was left. Turn it on while you are working out why Mate is slow
                    or stops early. Every run&apos;s details are kept either way, and a run that fails always logs why.
                  </FieldDescription>
                </FieldContent>
              </Field>
            </FieldGroup>
          </FormSection>

          <FormSection title="OpenRouter API key">
            <SecretFieldGroup fields={[{ key: 'OPENROUTER_API_KEY', label: 'OpenRouter API key' }]} />
          </FormSection>

          <FormSection title="Web search">
            <FieldGroup>
              <Field orientation="horizontal">
                <Switch
                  checked={draft.assistantWebSearch}
                  onCheckedChange={(checked) =>
                    onChange({
                      assistantWebSearch: checked,
                      ...(checked && draft.assistantWebSearchModel.trim() === '' ? { assistantWebSearchModel: defaultWebSearchModel } : {}),
                    })
                  }
                  aria-label="Web search"
                />
                <FieldContent>
                  <FieldLabel>Web search</FieldLabel>
                  <FieldDescription>
                    Mate searches the web through OpenRouter when it needs outside information, at a small extra cost per
                    search.
                  </FieldDescription>
                </FieldContent>
              </Field>

              {draft.assistantWebSearch ? (
                <ModelPicker
                  id="assistant-web-search-model"
                  ariaLabel="Search model"
                  value={draft.assistantWebSearchModel}
                  onChange={(assistantWebSearchModel) => onChange({ assistantWebSearchModel })}
                  recentStorageKey={recentWebSearchModelStorageKey}
                  capability="all"
                  description="The model that carries the search. Results come from the same search engine whichever model you choose, so a cheap model is fine."
                  dialogDescription="Any model, with server-side sorting and pagination."
                />
              ) : null}
            </FieldGroup>
          </FormSection>

          {/* tabIndex so the setup checklist can scroll to and focus this group. */}
          <div id="passage-limits" tabIndex={-1} className="min-w-0 outline-hidden">
            <FormSection
              title="Passage limits"
              description="Mate checks passages against these limits. Sectors are the true wind angle off your course. Each sector needs a no-go wind before Mate will assess a leg in it; a blank no-go wave height is shown as not checked. Marginal is 70% of no-go, and gusts are judged against no-go wind plus 10 kn."
            >
              <FieldGroup>
                <PassageLimitsFields
                  limits={draft.assistantPassageLimits}
                  errors={passageLimitErrors(draft)}
                  onChange={(assistantPassageLimits) => onChange({ assistantPassageLimits })}
                />
                <Field orientation="horizontal">
                  <Switch
                    checked={draft.assistantPassageLimits.daylightArrival}
                    onCheckedChange={(daylightArrival) =>
                      onChange({ assistantPassageLimits: { ...draft.assistantPassageLimits, daylightArrival } })
                    }
                    aria-label="Daylight arrival"
                  />
                  <FieldContent>
                    <FieldLabel>Daylight arrival</FieldLabel>
                    <FieldDescription>Flag any leg that would arrive in the dark, before sunrise or after sunset.</FieldDescription>
                  </FieldContent>
                </Field>
              </FieldGroup>
            </FormSection>
          </div>

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
