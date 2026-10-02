import { useEffect, useMemo, useState, type ReactNode } from 'react'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Field, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'

// Which models the catalog lists: Mate answers through tool calls, document
// indexing reads images.
export type ModelCapability = 'tools' | 'images'

const chooseNewModelValue = '__choose_new_model__'
const modelSearchDebounceMs = 300
export const customModelIDPattern = /^[a-z0-9][a-z0-9._-]*\/[a-z0-9][a-z0-9._:-]*$/i

type ModelsSortKey = 'popular' | 'newest' | 'throughput' | 'latency' | 'price'

type ModelOption = {
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

function modelMatchesSearch(model: ModelOption, query: string): boolean {
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

function parseSortSelection(value: string): { sort: ModelsSortKey; order: 'asc' | 'desc' } {
  if (value === 'price_asc') return { sort: 'price', order: 'asc' }
  if (value === 'price_desc') return { sort: 'price', order: 'desc' }
  if (value === 'newest') return { sort: 'newest', order: 'desc' }
  if (value === 'throughput') return { sort: 'throughput', order: 'desc' }
  if (value === 'latency') return { sort: 'latency', order: 'asc' }
  return { sort: 'popular', order: 'desc' }
}

function formatSortSelection(sort: ModelsSortKey, order: 'asc' | 'desc'): string {
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

interface ModelCatalogDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  capability: ModelCapability
  title: string
  description: string
  // When set, the dialog offers a "Use a model ID" form and calls this with a
  // validated id. Omit it where a free-typed id makes no sense (Auto filters).
  onUseModelID?: (id: string) => void
  // Per-row action cell and an optional badge line under the model id.
  renderActions: (model: ModelOption, close: () => void) => ReactNode
  renderBadges?: (model: ModelOption) => ReactNode
}

// The catalog table: server-side sort and paging, client-side search over the
// returned page. Shared by the single-model pickers and the Auto filter
// manager so the table code exists once.
export function ModelCatalogDialog({
  open,
  onOpenChange,
  capability,
  title,
  description,
  onUseModelID,
  renderActions,
  renderBadges,
}: ModelCatalogDialogProps) {
  const [tableModels, setTableModels] = useState<ModelOption[]>([])
  const [tableLoading, setTableLoading] = useState(false)
  const [tableError, setTableError] = useState<string | null>(null)
  const [tablePage, setTablePage] = useState(1)
  const [tableTotalPages, setTableTotalPages] = useState(1)
  const [tableSort, setTableSort] = useState<ModelsSortKey>('popular')
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

  useEffect(() => {
    if (open) setTablePage(1)
  }, [open])

  const visibleTableModels = useMemo(
    () => tableModels.filter((model) => modelMatchesSearch(model, tableQuery)),
    [tableModels, tableQuery],
  )

  useEffect(() => {
    if (!open) return

    let cancelled = false
    const params = new URLSearchParams({
      capability,
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
          models?: ModelOption[]
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
  }, [open, capability, tableSort, tableOrder, tablePage, tableQuery])

  const close = () => onOpenChange(false)

  const submitCustomModel = () => {
    const id = customModelID.trim()
    if (!customModelIDPattern.test(id)) {
      setCustomModelError(true)
      return
    }
    onUseModelID?.(id)
    setCustomModelID('')
    setCustomModelError(false)
    close()
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="w-[calc(100vw-2rem)] max-w-5xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>

        {onUseModelID ? (
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
                        {renderBadges?.(model)}
                      </td>
                      <td className="px-3 py-2 tabular-nums">{formatModelPrice(model.price)}</td>
                      <td className="px-3 py-2">{formatCreatedDate(model.created_at)}</td>
                      <td className="px-3 py-2 text-right">{renderActions(model, close)}</td>
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
  )
}

interface ModelPickerProps {
  value: string
  onChange: (modelID: string) => void
  id: string
  ariaLabel: string
  // localStorage key for the recent-models list, so each setting keeps its own.
  recentStorageKey: string
  capability: ModelCapability
  // Shown in the trigger while value is blank (the backend fills in a default).
  emptyLabel?: string
  description: ReactNode
  dialogDescription: string
}

// A select of the current and recently used models, with "Choose a new
// model..." opening the catalog. Used for the Mate model and the document
// indexing model.
export function ModelPicker({
  value,
  onChange,
  id,
  ariaLabel,
  recentStorageKey,
  capability,
  emptyLabel,
  description,
  dialogDescription,
}: ModelPickerProps) {
  const [recentModels, setRecentModels] = useState<string[]>([])
  const [dialogOpen, setDialogOpen] = useState(false)

  useEffect(() => {
    try {
      const raw = window.localStorage.getItem(recentStorageKey)
      if (!raw) return
      const parsed = JSON.parse(raw) as string[]
      if (!Array.isArray(parsed)) return
      setRecentModels(parsed.map((item) => item.trim()).filter((item) => item.length > 0))
    } catch {
      // Ignore unreadable localStorage values.
    }
  }, [recentStorageKey])

  const modelOptions = useMemo(() => {
    const current = value.trim()
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
  }, [value, recentModels])

  const pushRecentModel = (modelID: string) => {
    const trimmed = modelID.trim()
    if (!trimmed) return
    const updated = [trimmed, ...recentModels.filter((item) => item !== trimmed)].slice(0, 6)
    setRecentModels(updated)
    try {
      window.localStorage.setItem(recentStorageKey, JSON.stringify(updated))
    } catch {
      // Ignore localStorage write failures and keep in-memory recents.
    }
  }

  const choose = (modelID: string) => {
    onChange(modelID)
    pushRecentModel(modelID)
  }

  return (
    <>
      <Field>
        <FieldLabel htmlFor={id}>Model</FieldLabel>
        <Select
          value={value}
          onValueChange={(next) => {
            if (!next) return
            if (next === chooseNewModelValue) {
              setDialogOpen(true)
              return
            }
            choose(next)
          }}
        >
          <SelectTrigger id={id} aria-label={ariaLabel}>
            <SelectValue>{(current: string | null) => (current ? current : (emptyLabel ?? ''))}</SelectValue>
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
        <FieldDescription>{description}</FieldDescription>
      </Field>

      <ModelCatalogDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        capability={capability}
        title="Choose a model"
        description={dialogDescription}
        onUseModelID={choose}
        renderActions={(model, close) => (
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              choose(model.id)
              close()
            }}
          >
            Select
          </Button>
        )}
      />
    </>
  )
}
