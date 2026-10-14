import type { ColumnDef } from '@tanstack/react-table'
import { ChevronDown, FolderPlus, Layers, Route, Upload } from 'lucide-react'
import { useMemo, useRef, useState, type ChangeEvent } from 'react'
import { toast } from 'sonner'

import { ImportSummaryCard } from '@/components/passages/import-summary'
import { NameCruiseDialog } from '@/components/passages/name-cruise-dialog'
import {
  EmptyState,
  FolderTree,
  IndexTable,
  Page,
  TreeLayout,
  type RowAction,
  type TreeNode,
} from '@/components/patterns'
import { Button } from '@/components/ui/button'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import type { PassagesFilter } from '@/lib/app-location'
import { passagesApi } from '@/lib/passages-api'
import {
  fmtDuration,
  fmtNM,
  formatLocalDate,
  passageHeading,
  passageRoute,
  type CruiseRecord,
  type ImportSummary,
  type PassageSummary,
} from '@/lib/passages'

const TREE_COLLAPSED_KEY = 'passages.tree.collapsed'
const TREE_EXPANDED_KEY = 'passages.tree.expanded'

const ALL_NODE = 'all'
const UNFILED_NODE = 'unfiled'
const cruiseNode = (id: string) => `cruise:${id}`

export function filterNodeId(filter: PassagesFilter): string {
  if (filter.kind === 'cruise') return cruiseNode(filter.id)
  return filter.kind === 'unfiled' ? UNFILED_NODE : ALL_NODE
}

export function passagesInFilter(passages: PassageSummary[], filter: PassagesFilter): PassageSummary[] {
  if (filter.kind === 'all') return passages
  if (filter.kind === 'unfiled') return passages.filter((p) => p.cruise_id === null)
  return passages.filter((p) => p.cruise_id === filter.id)
}

export interface PassagesPanelProps {
  filter: PassagesFilter
  passages: PassageSummary[]
  cruises: CruiseRecord[]
  loading: boolean
  error: string | null
  canWrite: boolean
  onRetry: () => void
  onFilter: (filter: PassagesFilter) => void
  onOpenPassage: (id: string) => void
  onOpenCruise: (id: string) => void
  onCombine: (ids: string[]) => void
  /** Something changed on the server: refresh the list. */
  onChanged: () => void
}

export function PassagesPanel({
  filter, passages, cruises, loading, error, canWrite, onRetry, onFilter, onOpenPassage, onOpenCruise, onCombine, onChanged,
}: PassagesPanelProps) {
  const fileInput = useRef<HTMLInputElement>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [importing, setImporting] = useState(false)
  const [summary, setSummary] = useState<ImportSummary | null>(null)
  const [cruiseDialog, setCruiseDialog] = useState<'new' | 'save' | null>(null)
  const [busy, setBusy] = useState(false)

  const cruiseName = useMemo(() => new Map(cruises.map((c) => [c.id, c.name])), [cruises])
  const rows = passagesInFilter(passages, filter)
  const activeCruise = filter.kind === 'cruise' ? cruises.find((c) => c.id === filter.id) ?? null : null

  const treeNodes: TreeNode[] = [
    { id: ALL_NODE, label: 'All passages', count: passages.length, icon: <Route className="h-4 w-4" aria-hidden="true" /> },
    ...cruises.map((c): TreeNode => ({
      id: cruiseNode(c.id),
      label: c.name,
      count: passages.filter((p) => p.cruise_id === c.id).length,
      actions: [{ label: 'Open cruise', onSelect: () => onOpenCruise(c.id) }],
      separatorBefore: false,
    })),
    {
      id: UNFILED_NODE,
      label: 'Unfiled',
      count: passages.filter((p) => p.cruise_id === null).length,
      separatorBefore: cruises.length > 0,
    },
  ]

  const selectNode = (id: string) => {
    setSelected(new Set())
    if (id === ALL_NODE) onFilter({ kind: 'all' })
    else if (id === UNFILED_NODE) onFilter({ kind: 'unfiled' })
    else if (id.startsWith('cruise:')) onFilter({ kind: 'cruise', id: id.slice('cruise:'.length) })
  }

  const columns = useMemo<ColumnDef<PassageSummary, unknown>[]>(() => [
    {
      id: 'date',
      header: 'Date',
      accessorFn: (p) => p.start,
      cell: ({ row }) => <span className="tabular-nums text-foreground">{formatLocalDate(row.original.start, row.original.start_lon ?? 0)}</span>,
    },
    {
      id: 'route',
      header: 'From → To',
      accessorFn: (p) => passageHeading(p),
      enableSorting: false,
      cell: ({ row }) => {
        const p = row.original
        return (
          <span className="flex min-w-0 flex-col">
            <span className="truncate font-medium text-foreground">{passageHeading(p)}</span>
            {p.title.trim() !== '' && <span className="truncate text-xs text-muted-foreground">{passageRoute(p)}</span>}
          </span>
        )
      },
    },
    {
      id: 'distance',
      header: 'Distance',
      accessorFn: (p) => p.stats.distance_nm,
      cell: ({ row }) => <span className="tabular-nums">{fmtNM(row.original.stats.distance_nm)}</span>,
    },
    {
      id: 'underway',
      header: 'Underway',
      accessorFn: (p) => p.stats.underway_seconds,
      cell: ({ row }) => <span className="tabular-nums">{fmtDuration(row.original.stats.underway_seconds)}</span>,
    },
    {
      id: 'cruise',
      header: 'Cruise',
      accessorFn: (p) => (p.cruise_id ? cruiseName.get(p.cruise_id) ?? '' : ''),
      cell: ({ getValue }) => <span className="text-muted-foreground">{getValue<string>() || '--'}</span>,
    },
  ], [cruiseName])

  const rowActions = (p: PassageSummary): RowAction<PassageSummary>[] => [
    { label: 'Open', onSelect: () => onOpenPassage(p.id) },
  ]

  const handleFiles = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files ?? [])
    e.target.value = ''
    if (files.length === 0) return
    setImporting(true)
    try {
      setSummary(await passagesApi.importGPX(files))
      onChanged()
    } catch (err) {
      toast.error('Could not import tracks', { description: err instanceof Error ? err.message : String(err) })
    } finally {
      setImporting(false)
    }
  }

  const selectedIds = [...selected]
  const afterBulk = () => { setSelected(new Set()); onChanged() }

  const moveSelected = async (cruiseId: string | null) => {
    setBusy(true)
    try {
      for (const id of selectedIds) await passagesApi.patch(id, { cruise_id: cruiseId })
    } catch (err) {
      toast.error('Could not move passages', { description: err instanceof Error ? err.message : String(err) })
    } finally {
      setBusy(false)
      afterBulk()
    }
  }

  const createCruise = async (name: string) => {
    setBusy(true)
    try {
      const cruise = await passagesApi.createCruise(name)
      if (cruiseDialog === 'save') {
        for (const id of selectedIds) await passagesApi.patch(id, { cruise_id: cruise.id })
      }
      setCruiseDialog(null)
      afterBulk()
      onFilter({ kind: 'cruise', id: cruise.id })
    } catch (err) {
      toast.error('Could not create cruise', { description: err instanceof Error ? err.message : String(err) })
      onChanged()
    } finally {
      setBusy(false)
    }
  }

  const bulkActions = (
    <div className="flex flex-wrap items-center gap-2">
      <Button type="button" size="sm" variant="outline" disabled={selected.size < 2} onClick={() => onCombine(selectedIds)}>
        <Layers className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
        View together
      </Button>
      {canWrite && (
        <>
          <Button type="button" size="sm" variant="outline" disabled={busy} onClick={() => setCruiseDialog('save')}>
            <FolderPlus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            Save as cruise
          </Button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button type="button" size="sm" variant="outline" disabled={busy}>
                  Move to cruise
                  <ChevronDown className="h-4 w-4" data-icon="inline-end" aria-hidden="true" />
                </Button>
              }
            />
            <DropdownMenuContent align="start">
              {cruises.map((c) => (
                <DropdownMenuItem key={c.id} onClick={() => void moveSelected(c.id)}>{c.name}</DropdownMenuItem>
              ))}
              {cruises.length > 0 && <DropdownMenuSeparator />}
              <DropdownMenuItem onClick={() => void moveSelected(null)}>Unfiled</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </>
      )}
    </div>
  )

  const importButton = (
    <Button type="button" variant="outline" disabled={!canWrite || importing} onClick={() => fileInput.current?.click()}>
      <Upload className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
      {importing ? 'Importing…' : 'Import GPX'}
    </Button>
  )

  const nothingRecorded = !loading && !error && passages.length === 0
  const title = filter.kind === 'cruise' ? activeCruise?.name ?? 'Cruise' : filter.kind === 'unfiled' ? 'Unfiled passages' : 'Passages'

  return (
    <Page title={title} primaryAction={canWrite ? { label: importing ? 'Importing…' : 'Import GPX', icon: <Upload className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />, onClick: () => fileInput.current?.click(), disabled: importing } : undefined}>
      <input
        ref={fileInput}
        type="file"
        multiple
        accept=".gpx,application/gpx+xml"
        className="hidden"
        data-testid="passages-file-input"
        onChange={(e) => void handleFiles(e)}
      />
      <TreeLayout
        paneLabel="Cruises"
        collapsedStorageKey={TREE_COLLAPSED_KEY}
        pane={({ dismiss }) => (
          <div className="flex flex-col gap-2">
            <FolderTree
              label="Cruises"
              nodes={treeNodes}
              selectedId={filterNodeId(filter)}
              onSelect={selectNode}
              storageKey={TREE_EXPANDED_KEY}
              onDismiss={dismiss}
            />
            {canWrite && (
              <Button type="button" variant="ghost" size="sm" className="justify-start" onClick={() => setCruiseDialog('new')}>
                <FolderPlus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                New cruise
              </Button>
            )}
          </div>
        )}
      >
        {summary && <ImportSummaryCard summary={summary} onDismiss={() => setSummary(null)} />}
        {activeCruise && (
          <div>
            <Button type="button" variant="outline" size="sm" onClick={() => onOpenCruise(activeCruise.id)}>Cruise details</Button>
          </div>
        )}
        <IndexTable<PassageSummary>
          columns={columns}
          rows={rows}
          getRowId={(p) => p.id}
          onOpen={(p) => onOpenPassage(p.id)}
          rowActions={rowActions}
          rowActionsLabel={(p) => `Actions for ${passageHeading(p)}`}
          loading={loading}
          error={error}
          onRetry={onRetry}
          initialSorting={[{ id: 'date', desc: true }]}
          selectable
          selectedIds={selected}
          onSelectionChange={setSelected}
          rowSelectLabel={(p) => `Select ${passageHeading(p)}`}
          bulkActions={bulkActions}
          empty={nothingRecorded ? (
            <EmptyState
              icon={<Route className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
              title="No passages yet"
              description="Helmcentral keeps the boat's track as she goes and finds each passage by itself, so the first one appears after you next get underway. Tracks from a chartplotter can be imported as GPX files."
              action={importButton}
            />
          ) : (
            <EmptyState
              title={filter.kind === 'all' ? 'No passages' : 'No passages here'}
              description={filter.kind === 'cruise'
                ? 'Select passages in the list and use Move to cruise to put them in this one.'
                : filter.kind === 'unfiled' ? 'Every passage belongs to a cruise.' : undefined}
            />
          )}
        />
      </TreeLayout>
      <NameCruiseDialog
        open={cruiseDialog !== null}
        onOpenChange={(open) => { if (!open) setCruiseDialog(null) }}
        title={cruiseDialog === 'save' ? 'Save as cruise' : 'New cruise'}
        description={cruiseDialog === 'save' ? 'The selected passages move into the new cruise, leaving any cruise they were in.' : undefined}
        confirmLabel={cruiseDialog === 'save' ? 'Save cruise' : 'Create cruise'}
        busy={busy}
        onConfirm={(name) => void createCruise(name)}
      />
    </Page>
  )
}
