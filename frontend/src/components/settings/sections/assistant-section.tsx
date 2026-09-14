import { useEffect, useMemo, useState } from 'react'
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
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel, FieldLegend, FieldSet } from '@/components/ui/field'
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
    <div className="mx-auto flex max-w-3xl flex-col gap-4 rounded-lg border bg-background/60 p-4">
      <FieldSet>
        <FieldLegend variant="label">Mate</FieldLegend>
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
      </FieldSet>

      {isAutoModel ? (
        <FieldSet>
          <FieldLegend variant="label">Auto</FieldLegend>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="assistant-auto-model-manager">Model filters</FieldLabel>
              <Button
                id="assistant-auto-model-manager"
                variant="outline"
                onClick={() => {
                  setTablePage(1)
                  setDialogOpen(true)
                }}
                aria-label="Manage Auto model filters"
              >
                Manage allowed and excluded models
              </Button>
              <FieldDescription>Use the model catalog to include or exclude models without typing IDs.</FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="assistant-cost-tier">Auto cost tier</FieldLabel>
              <Select
                value={draft.assistantCostTier === '' ? noCostTierValue : draft.assistantCostTier}
                onValueChange={(value) =>
                  onChange({
                    assistantCostTier: (value === noCostTierValue ? '' : value ?? '') as RegularSettingsDraft['assistantCostTier'],
                  })
                }
              >
                <SelectTrigger id="assistant-cost-tier" aria-label="Mate Auto cost tier">
                  <SelectValue />
                </SelectTrigger>
                <SelectPopup>
                  <SelectItem value={noCostTierValue}>No cost cap</SelectItem>
                  <SelectItem value="low">low</SelectItem>
                  <SelectItem value="medium">medium</SelectItem>
                  <SelectItem value="high">high</SelectItem>
                  <SelectItem value="xhigh">xhigh</SelectItem>
                  <SelectItem value="max">max</SelectItem>
                </SelectPopup>
              </Select>
            </Field>

            <Field>
              <FieldLabel htmlFor="assistant-allowed-models">Allowed models</FieldLabel>
              <div className="flex items-center gap-2">
                <Input
                  id="assistant-allowed-models"
                  value={draft.assistantAllowedModels.join(', ')}
                  aria-label="Mate allowed models"
                  readOnly
                  aria-readonly="true"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label="Clear allowed models"
                  disabled={draft.assistantAllowedModels.length === 0}
                  onClick={() => onChange({ assistantAllowedModels: [] })}
                >
                  Clear
                </Button>
              </div>
              <FieldDescription>Managed from the model catalog below (read-only).</FieldDescription>
            </Field>

            <Field>
              <FieldLabel htmlFor="assistant-excluded-models">Excluded models</FieldLabel>
              <div className="flex items-center gap-2">
                <Input
                  id="assistant-excluded-models"
                  value={draft.assistantExcludedModels.join(', ')}
                  aria-label="Mate excluded models"
                  readOnly
                  aria-readonly="true"
                />
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  aria-label="Clear excluded models"
                  disabled={draft.assistantExcludedModels.length === 0}
                  onClick={() => onChange({ assistantExcludedModels: [] })}
                >
                  Clear
                </Button>
              </div>
              <FieldDescription>Managed from the model catalog below (read-only).</FieldDescription>
            </Field>
          </FieldGroup>
        </FieldSet>
      ) : null}

      <FieldSet>
        <FieldLegend variant="label">OpenRouter API key</FieldLegend>
        <SecretFieldGroup fields={[{ key: 'OPENROUTER_API_KEY', label: 'OpenRouter API key' }]} />
      </FieldSet>

      <FieldSet>
        <FieldLegend variant="label">Standing notes</FieldLegend>
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
      </FieldSet>

      {/* ADR 0093 voice phase: push-to-talk input, reading the spoken summary
          back aloud, and the always-listening "Hey Mate" wake word - each its
          own switch since each has its own cost (an https requirement, a
          voice interrupting the cabin, battery and a cloud-speech vendor). */}
      <FieldSet>
        <FieldLegend variant="label">Voice</FieldLegend>
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
      </FieldSet>

      <FieldDescription>
        Every question sends your position, the question itself, and forecast and tide excerpts to OpenRouter and whichever
        model provider you choose. Every reply shows its cost.
      </FieldDescription>

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
                            <span className="mt-1 inline-flex rounded-sm bg-secondary px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-secondary-foreground">
                              Allowed
                            </span>
                          ) : null}
                          {isAutoModel && draft.assistantExcludedModels.includes(model.id) ? (
                            <span className="mt-1 inline-flex rounded-sm bg-muted px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-[0.14em] text-muted-foreground">
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
    </div>
  )
}
