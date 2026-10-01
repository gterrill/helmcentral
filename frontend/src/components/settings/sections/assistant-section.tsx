import { useEffect, useMemo, useState } from 'react'
import { FormSection, SettingsLayout } from '@/components/patterns'
import { SecretFieldGroup } from '@/components/settings/secret-field-group'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'

interface AssistantSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
}

const noCostTierValue = '__none__'
const chooseNewModelValue = '__choose_new_model__'
const recentModelStorageKey = 'assistant.recent-models'
const modelSearchDebounceMs = 300
const costTierLabels: Record<string, string> = {
  low: 'Low',
  medium: 'Medium',
  high: 'High',
  xhigh: 'XHigh',
  max: 'Max',
}
const customModelIDPattern = /^[a-z0-9][a-z0-9._-]*\/[a-z0-9][a-z0-9._:-]*$/i

type AssistantModelsSortKey = 'popular' | 'newest' | 'throughput' | 'latency' | 'price'

type AssistantModelOption = {
  id: string
  name: string
  price: number
  created_at: string
}

function normalizeModelSearchText(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, ' ')
    .trim()
}

function modelMatchesSearch(model: AssistantModelOption, query: string): boolean {
  const trimmedQuery = query.trim().toLowerCase()
  if (!trimmedQuery) return true

  const idLower = model.id.toLowerCase()
  const nameLower = model.name.toLowerCase()
  if (idLower.includes(trimmedQuery) || nameLower.includes(trimmedQuery)) {
    return true
  }

  const normalizedHaystack = normalizeModelSearchText(`${model.id} ${model.name}`)
  const tokens = normalizeModelSearchText(trimmedQuery).split(' ').filter(Boolean)
  return tokens.length > 0 && tokens.every((token) => normalizedHaystack.includes(token))
}

function parseSortSelection(value: string): { sort: AssistantModelsSortKey; order: 'asc' | 'desc' } {
  if (value === 'price_asc') return { sort: 'price', order: 'asc' }
  if (value === 'price_desc') return { sort: 'price', order: 'desc' }
  if (value === 'newest') return { sort: 'newest', order: 'desc' }
  if (value === 'throughput') return { sort: 'throughput', order: 'desc' }
  if (value === 'latency') return { sort: 'latency', order: 'asc' }
  return { sort: 'popular', order: 'desc' }
}

function formatSortSelection(sort: AssistantModelsSortKey, order: 'asc' | 'desc'): string {
  if (sort === 'price') return order === 'desc' ? 'price_desc' : 'price_asc'
  return sort
}

function formatModelPrice(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '--'
  return `$${value.toFixed(6)}`
}

function formatCreatedDate(value: string): string {
  if (!value) return '--'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '--'
  return date.toLocaleDateString()
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
  const [recentModels, setRecentModels] = useState<string[]>([])
  const [dialogOpen, setDialogOpen] = useState(false)
  const [tableModels, setTableModels] = useState<AssistantModelOption[]>([])
  const [tableLoading, setTableLoading] = useState(false)
  const [tableError, setTableError] = useState<string | null>(null)
  const [tablePage, setTablePage] = useState(1)
  const [tableTotalPages, setTableTotalPages] = useState(1)
  const [tableSort, setTableSort] = useState<AssistantModelsSortKey>('popular')
  const [tableOrder, setTableOrder] = useState<'asc' | 'desc'>('desc')
  const [tableQueryInput, setTableQueryInput] = useState('')
  const [tableQuery, setTableQuery] = useState('')
  const [customModelID, setCustomModelID] = useState('')
  const [customModelError, setCustomModelError] = useState(false)

  useEffect(() => {
    const timeoutID = window.setTimeout(() => {
      const normalized = tableQueryInput.trim()
      setTableQuery((previous) => (previous === normalized ? previous : normalized))
    }, modelSearchDebounceMs)
    return () => window.clearTimeout(timeoutID)
  }, [tableQueryInput])

  useEffect(() => {
    setTablePage(1)
  }, [tableQuery])

  const visibleTableModels = useMemo(
    () => tableModels.filter((model) => modelMatchesSearch(model, tableQuery)),
    [tableModels, tableQuery],
  )

  const isAutoModel = draft.assistantModel.trim().toLowerCase() === 'openrouter/auto'
  const modelOptions = useMemo(() => {
    const current = draft.assistantModel.trim()
    const options: Array<{ id: string; label: string }> = []
    if (current !== '') {
      options.push({ id: current, label: current })
    }
    for (const model of recentModels) {
      if (model !== current) {
        options.push({ id: model, label: `${model} (recent)` })
      }
    }
    return options
  }, [draft.assistantModel, recentModels])

  const persistRecentModels = (values: string[]) => {
    setRecentModels(values)
    try {
      window.localStorage.setItem(recentModelStorageKey, JSON.stringify(values))
    } catch {
      // Ignore localStorage write failures and keep in-memory recents.
    }
  }

  const pushRecentModel = (modelID: string) => {
    const trimmed = modelID.trim()
    if (!trimmed) return
    const updated = [trimmed, ...recentModels.filter((value) => value !== trimmed)].slice(0, 6)
    persistRecentModels(updated)
  }

  const submitCustomModel = () => {
    const id = customModelID.trim()
    if (!customModelIDPattern.test(id)) {
      setCustomModelError(true)
      return
    }
    onChange({ assistantModel: id })
    pushRecentModel(id)
    setCustomModelID('')
    setCustomModelError(false)
    setDialogOpen(false)
  }

  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(recentModelStorageKey)
      if (!raw) return
      const parsed = JSON.parse(raw) as string[]
      if (!Array.isArray(parsed)) return
      setRecentModels(parsed.map((value) => value.trim()).filter((value) => value.length > 0))
    } catch {
      // Ignore unreadable localStorage values.
    }
  }, [])

  useEffect(() => {
    if (!dialogOpen) return

    let cancelled = false
    const params = new URLSearchParams({
      sort: tableSort,
      order: tableOrder,
      page: String(tablePage),
      page_size: '10',
    })
    const trimmedQuery = tableQuery.trim()
    if (trimmedQuery !== '') {
      params.set('q', trimmedQuery)
    }

    async function loadModelsPage() {
      setTableLoading(true)
      setTableError(null)
      try {
        const response = await fetch(`/api/assistant/models?${params.toString()}`)
        if (!response.ok) {
          setTableError('Unable to load model catalog.')
          return
        }
        const payload = (await response.json()) as {
          models?: AssistantModelOption[]
          page?: { total_pages?: number }
        }
        if (cancelled) return
        setTableModels(Array.isArray(payload.models) ? payload.models : [])
        setTableTotalPages(Math.max(1, payload.page?.total_pages ?? 1))
      } catch {
        if (!cancelled) {
          setTableError('Unable to load model catalog.')
        }
      } finally {
        if (!cancelled) {
          setTableLoading(false)
        }
      }
    }

    void loadModelsPage()

    return () => {
      cancelled = true
    }
  }, [dialogOpen, tableSort, tableOrder, tablePage, tableQuery])

  return (
    <SettingsLayout
      title="Mate"
      description={
        <>
          Every question sends your position, the question itself, and forecast and tide excerpts to OpenRouter and whichever
          model provider you choose. Mate also reads your documents and notes to answer questions, and once it&apos;s on it
          summarises and indexes them for search on its own, no separate prompt per item. All of that text goes to OpenRouter
          too, billed to your key. Every reply shows its cost.
        </>
      }
    >
      <FormSection title="Model">
        <FieldGroup>
          <Field orientation="horizontal">
            <Switch
              checked={draft.assistantEnabled}
              onCheckedChange={(checked) => onChange({ assistantEnabled: checked })}
              aria-label="Enable Mate"
            />
            <FieldLabel>Enable Mate</FieldLabel>
          </Field>

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
            <Field>
              <FieldLabel htmlFor="assistant-model">Model</FieldLabel>
              <Select
                value={draft.assistantModel}
                onValueChange={(value) => {
                  if (!value) return
                  if (value === chooseNewModelValue) {
                    setTablePage(1)
                    setDialogOpen(true)
                    return
                  }
                  onChange({ assistantModel: value })
                  pushRecentModel(value)
                }}
              >
                <SelectTrigger id="assistant-model" aria-label="Mate model">
                  <SelectValue />
                </SelectTrigger>
                <SelectPopup>
                  {modelOptions.map((model) => (
                    <SelectItem key={model.id} value={model.id}>
                      {model.label}
                    </SelectItem>
                  ))}
                  <SelectItem value={chooseNewModelValue}>Choose a new model...</SelectItem>
                </SelectPopup>
              </Select>
              <FieldDescription>
                Select a recent model, or open the catalog to choose a new tool-capable model.
              </FieldDescription>
            </Field>
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
                onClick={() => {
                  setTablePage(1)
                  setDialogOpen(true)
                }}
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
          <Field>
            <FieldLabel htmlFor="assistant-document-model">Model</FieldLabel>
            <Input
              id="assistant-document-model"
              aria-label="Document indexing model"
              placeholder="google/gemini-2.5-flash"
              value={draft.assistantDocumentModel}
              onChange={(e) => onChange({ assistantDocumentModel: e.target.value })}
            />
            {draft.assistantDocumentModel.trim() !== '' && !customModelIDPattern.test(draft.assistantDocumentModel.trim()) ? (
              <p className="text-xs text-destructive">Enter an OpenRouter model ID like provider/model.</p>
            ) : null}
            <FieldDescription>
              Reads every document and photo you add, including text in scans, and suggests its title, summary and
              tags. It runs on its own over the whole library, so pick a cheap model that can read images. It doesn't
              need to be the model Mate answers with.
            </FieldDescription>
          </Field>
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


      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="w-[calc(100vw-2rem)] max-w-5xl">
          <DialogHeader>
            <DialogTitle>{isAutoModel ? 'Manage Auto model filters' : 'Choose a model'}</DialogTitle>
            <DialogDescription>
              {isAutoModel
                ? 'Include or exclude tool-capable models for OpenRouter Auto.'
                : 'Tool-capable models with server-side sorting and pagination.'}
            </DialogDescription>
          </DialogHeader>

          {!isAutoModel ? (
            <form
              className="flex flex-col gap-1"
              onSubmit={(e) => {
                e.preventDefault()
                submitCustomModel()
              }}
            >
              <FieldLabel htmlFor="assistant-custom-model-id">Use a model ID</FieldLabel>
              <div className="flex items-center gap-2">
                <Input
                  id="assistant-custom-model-id"
                  aria-label="Model ID"
                  placeholder="provider/model, e.g. typesafe/jev-router"
                  value={customModelID}
                  onChange={(e) => {
                    setCustomModelID(e.target.value)
                    setCustomModelError(false)
                  }}
                />
                <Button type="submit" variant="outline" size="sm" disabled={customModelID.trim() === ''}>
                  Use
                </Button>
              </div>
              {customModelError ? (
                <p className="text-xs text-destructive">Enter an OpenRouter model ID like provider/model.</p>
              ) : null}
            </form>
          ) : null}

          <FieldGroup className="grid grid-cols-4 items-end gap-3">
            <Field className="col-span-3 min-w-0">
              <FieldLabel htmlFor="assistant-model-search">Search</FieldLabel>
              <div className="flex w-full items-center gap-2">
                <Input
                  id="assistant-model-search"
                  aria-label="Search model catalog"
                  placeholder="Search by model name or id"
                  value={tableQueryInput}
                  onChange={(e) => {
                    setTableQueryInput(e.target.value)
                  }}
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label="Clear model catalog search"
                  disabled={tableQueryInput.trim() === ''}
                  onClick={() => {
                    setTableQueryInput('')
                    setTableQuery('')
                  }}
                >
                  Clear
                </Button>
              </div>
            </Field>

            <Field className="col-span-1 min-w-0">
              <FieldLabel htmlFor="assistant-model-sort">Sort</FieldLabel>
              <Select
                value={formatSortSelection(tableSort, tableOrder)}
                onValueChange={(value) => {
                  const sortSelection = parseSortSelection(value ?? 'popular')
                  setTableSort(sortSelection.sort)
                  setTableOrder(sortSelection.order)
                  setTablePage(1)
                }}
              >
                <SelectTrigger id="assistant-model-sort" aria-label="Model catalog sort" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectPopup>
                  <SelectItem value="popular">Popular</SelectItem>
                  <SelectItem value="newest">Newest</SelectItem>
                  <SelectItem value="throughput">Throughput</SelectItem>
                  <SelectItem value="latency">Latency</SelectItem>
                  <SelectItem value="price_asc">Price low to high</SelectItem>
                  <SelectItem value="price_desc">Price high to low</SelectItem>
                </SelectPopup>
              </Select>
            </Field>
          </FieldGroup>

          <div className="h-[50vh] overflow-auto rounded-md border">
            <table className="w-full border-collapse text-sm">
              <thead className="bg-muted/50 text-left text-muted-foreground">
                <tr>
                  <th className="px-3 py-2">Name</th>
                  <th className="px-3 py-2">Price</th>
                  <th className="px-3 py-2">Date created</th>
                  <th className="px-3 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {tableLoading ? (
                  <tr>
                    <td className="px-3 py-3 text-muted-foreground" colSpan={4}>
                      Loading models...
                    </td>
                  </tr>
                ) : null}
                {!tableLoading && tableError ? (
                  <tr>
                    <td className="px-3 py-3 text-destructive" colSpan={4}>
                      {tableError}
                    </td>
                  </tr>
                ) : null}
                {!tableLoading && !tableError && visibleTableModels.length === 0 ? (
                  <tr>
                    <td className="px-3 py-3 text-muted-foreground" colSpan={4}>
                      No models found.
                    </td>
                  </tr>
                ) : null}
                {!tableLoading && !tableError
                  ? visibleTableModels.map((model) => (
                      <tr key={model.id} className="border-t">
                        <td className="px-3 py-2">
                          <div className="font-medium">{model.name}</div>
                          <div className="text-xs text-muted-foreground">{model.id}</div>
                          {isAutoModel && draft.assistantAllowedModels.includes(model.id) ? (
                            <span className="mt-1 inline-flex rounded-xs bg-secondary px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-secondary-foreground">
                              Allowed
                            </span>
                          ) : null}
                          {isAutoModel && draft.assistantExcludedModels.includes(model.id) ? (
                            <span className="mt-1 inline-flex rounded-xs bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
                              Excluded
                            </span>
                          ) : null}
                        </td>
                        <td className="px-3 py-2 tabular-nums">{formatModelPrice(model.price)}</td>
                        <td className="px-3 py-2">{formatCreatedDate(model.created_at)}</td>
                        <td className="px-3 py-2 text-right">
                          {isAutoModel ? (
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
                          ) : (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => {
                                onChange({ assistantModel: model.id })
                                pushRecentModel(model.id)
                                setDialogOpen(false)
                              }}
                            >
                              Select
                            </Button>
                          )}
                        </td>
                      </tr>
                    ))
                  : null}
              </tbody>
            </table>
          </div>

          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">Page {tablePage} of {tableTotalPages}</p>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                disabled={tablePage <= 1 || tableLoading}
                onClick={() => setTablePage((current) => Math.max(1, current - 1))}
              >
                Previous
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={tablePage >= tableTotalPages || tableLoading}
                onClick={() => setTablePage((current) => Math.min(tableTotalPages, current + 1))}
              >
                Next
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </SettingsLayout>
  )
}
