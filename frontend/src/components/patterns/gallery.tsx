import { useState, type ReactNode } from 'react'
import { FileText, Plus, Unlink, Wrench } from 'lucide-react'
import type { ColumnDef } from '@tanstack/react-table'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Separator } from '@/components/ui/separator'
import {
  ConfirmDelete,
  DetailsLayout,
  EmptyState,
  FormRow,
  FormSection,
  IndexFilters,
  IndexTable,
  Page,
  ResourceItem,
  ResourceList,
  SaveBar,
  SaveBarSlot,
  type IndexFilter,
  type RowAction,
} from '@/components/patterns'

// ADR 0142: a dev-only tour of the CRUD pattern library, each pattern shown
// against fixture data rather than a real hook - see docs/adr/0142-crud-
// pattern-library.md. Reachable only at /patterns, only when
// import.meta.env.DEV (App.tsx's own gate), and lazily loaded so none of
// this - or the fixtures below - reaches the production entry chunk.
// Nothing here calls the network; every control is wired to local state
// purely to make the pattern's own behaviour (sorting, dirty, delete)
// visible while poking at it.

interface GalleryItem {
  id: string
  name: string
  system: string
  status: 'deployed' | 'stored'
  hours: number
}

const FIXTURE_ITEMS: GalleryItem[] = [
  { id: 'g1', name: 'Bilge pump (port)', system: 'bilge', status: 'deployed', hours: 812 },
  { id: 'g2', name: 'Main generator', system: 'electrical', status: 'deployed', hours: 1204 },
  { id: 'g3', name: 'Spare impeller', system: 'electrical', status: 'stored', hours: 0 },
  { id: 'g4', name: 'Windlass', system: 'anchoring', status: 'deployed', hours: 96 },
]

const GALLERY_SYSTEM_ORDER = ['electrical', 'bilge', 'anchoring']
const GALLERY_SYSTEM_LABELS: Record<string, string> = {
  electrical: 'Electrical',
  bilge: 'Bilge',
  anchoring: 'Anchoring',
}

const galleryColumns: ColumnDef<GalleryItem, unknown>[] = [
  { accessorKey: 'name', header: 'Name' },
  {
    id: 'status',
    accessorFn: (row) => row.status,
    header: 'Status',
    cell: ({ row }) => (
      <Badge variant={row.original.status === 'deployed' ? 'secondary' : 'outline'}>
        {row.original.status === 'deployed' ? 'Deployed' : 'Stored'}
      </Badge>
    ),
  },
  { accessorKey: 'hours', header: 'Hours' },
]

function GallerySection({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-3">
      <div>
        <h2 className="text-sm font-semibold text-foreground">{title}</h2>
        <p className="text-xs text-muted-foreground">{description}</p>
      </div>
      {children}
    </section>
  )
}

export function PatternsGallery() {
  const [query, setQuery] = useState('')
  const [system, setSystem] = useState('')
  const [status, setStatus] = useState('')
  const [dirty, setDirty] = useState(true)
  const [saving, setSaving] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [showEmpty, setShowEmpty] = useState(false)
  const [showLoading, setShowLoading] = useState(false)
  const [showError, setShowError] = useState(false)

  const filters: IndexFilter[] = [
    {
      id: 'system',
      label: 'Filter by system',
      value: system,
      options: GALLERY_SYSTEM_ORDER.map((key) => ({ value: key, label: GALLERY_SYSTEM_LABELS[key] })),
      onChange: setSystem,
    },
    {
      id: 'status',
      label: 'Filter by status',
      value: status,
      options: [
        { value: 'deployed', label: 'Deployed' },
        { value: 'stored', label: 'Stored' },
      ],
      onChange: setStatus,
    },
  ]

  const filteredRows = FIXTURE_ITEMS.filter((row) => {
    if (query && !row.name.toLowerCase().includes(query.toLowerCase())) return false
    if (system && row.system !== system) return false
    if (status && row.status !== status) return false
    return true
  })

  const rowActions = (): RowAction<GalleryItem>[] => [
    { label: 'Open', onSelect: (row) => window.alert(`Open ${row.name}`) },
    { label: 'Delete', destructive: true, onSelect: () => setConfirmOpen(true) },
  ]

  return (
    <>
    {/* Stand-in for the app header: SaveBar takes over the header while
        dirty, and the gallery renders outside the app shell, so it brings a
        header of its own with the same SaveBarSlot App.tsx mounts. Sticky so
        the takeover is visible wherever you are on the page. */}
    <div className="sticky top-0 z-20 flex h-14 items-center justify-between border-b border-border bg-background px-4 lg:h-16">
      <span className="text-sm text-muted-foreground">App header (stand-in)</span>
      <SaveBarSlot />
    </div>
    <Page
      title="Pattern gallery"
      primaryAction={{ label: 'New item', icon: <Plus className="h-4 w-4" aria-hidden="true" />, onClick: () => window.alert('New item') }}
      secondaryActions={[{ label: 'Something destructive', destructive: true, onClick: () => setConfirmOpen(true) }]}
    >
      <p className="max-w-2xl text-sm text-muted-foreground">
        Every control below is wired to local fixture state, not a live hook - this page exists to see each
        pattern's own behaviour (sort, filter, dirty, delete) in isolation before composing them into a real
        index or details page. See docs/adr/0142-crud-pattern-library.md.
      </p>

      <Separator />

      <GallerySection title="IndexFilters + IndexTable" description="Search, filter selects, sortable columns, row actions, grouping.">
        <div className="flex flex-wrap items-center gap-3">
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input type="checkbox" checked={showLoading} onChange={(e) => setShowLoading(e.target.checked)} />
            loading
          </label>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input type="checkbox" checked={showError} onChange={(e) => setShowError(e.target.checked)} />
            error
          </label>
          <label className="flex items-center gap-2 text-xs text-muted-foreground">
            <input type="checkbox" checked={showEmpty} onChange={(e) => setShowEmpty(e.target.checked)} />
            empty
          </label>
        </div>
        <IndexFilters
          searchValue={query}
          onSearchChange={setQuery}
          searchLabel="Search"
          searchPlaceholder="Name..."
          filters={filters}
          onClearAll={() => { setQuery(''); setSystem(''); setStatus('') }}
        />
        {/* IndexTable's own root uses flex-1/min-h-0 to size itself within a
            flex parent - a plain h-72 box isn't one, so the fixed height was
            ignored and the (7-row, grouped) fixture list spilled into the
            next section below it. */}
        <div className="flex h-72 flex-col">
          <IndexTable
            columns={galleryColumns}
            rows={showEmpty ? [] : filteredRows}
            getRowId={(row) => row.id}
            onOpen={(row) => window.alert(`Open ${row.name}`)}
            rowActions={rowActions}
            rowActionsLabel={(row) => `Actions for ${row.name}`}
            groupBy={(row) => row.system}
            groupOrder={GALLERY_SYSTEM_ORDER}
            groupLabel={(key) => GALLERY_SYSTEM_LABELS[key] ?? key}
            loading={showLoading}
            error={showError ? 'Could not load the fixture list.' : null}
            onRetry={() => setShowError(false)}
            empty={
              <EmptyState
                icon={<Wrench className="h-8 w-8 text-muted-foreground" aria-hidden="true" />}
                title="No fixtures match"
                description="Clear a filter or toggle the empty-state checkbox off."
              />
            }
          />
        </div>
      </GallerySection>

      <Separator />

      <GallerySection
        title="DetailsLayout + FormSection + FormRow"
        description={'The Details template: editable content in the main column, scannable status/organisation in the aside (asidePosition="start" puts it first below lg, where the grid collapses to one column - try a narrow window).'}
      >
        <DetailsLayout
          asidePosition="start"
          aside={
            <>
              <FormSection title="Status">
                <p className="text-sm text-foreground">Deployed</p>
              </FormSection>
              <FormSection title="Organisation">
                <p className="text-sm text-muted-foreground">Profile: Onan 13.5kW</p>
                <p className="text-sm text-muted-foreground">Next service: 96 hours</p>
              </FormSection>
            </>
          }
        >
          <FormSection title="Specifications" description="What you need standing in front of the thing.">
            <p className="text-sm text-foreground">Main generator - Onan 13.5 kW</p>
            {/* FormRow: two columns when THIS card is wide (the main column),
                one when it is narrow - the aside, a phone. Resize the window
                to watch it flip. */}
            <FormRow>
              <label className="flex flex-col gap-1 text-sm">Manufacturer<Input defaultValue="Cummins Onan" /></label>
              <label className="flex flex-col gap-1 text-sm">Model<Input defaultValue="13.5 kW 60 Hz" /></label>
            </FormRow>
          </FormSection>
          <FormSection title="Documents">
            <p className="text-sm text-muted-foreground">No documents linked yet.</p>
          </FormSection>
        </DetailsLayout>
      </GallerySection>

      <Separator />

      <GallerySection
        title="ResourceList + ResourceItem"
        description="A short list of media-led entries inside a Details section: media, title, metadata line, badge and a menu. Click or Enter opens the item; the menu never does. Use IndexTable instead when rows are compared column by column, or sorted and filtered."
      >
        <div className="grid gap-6 md:grid-cols-2">
          <ResourceList label="Sample documents" header="Documents" count={2}>
            <ResourceItem
              item="manual"
              media={<FileText className="h-5 w-5" aria-hidden="true" />}
              title="Generator installation manual"
              meta="12 Jan 2026 09:30"
              badge={<Badge variant="outline">PDF</Badge>}
              onOpen={() => {}}
              actionsLabel="Actions for Generator installation manual"
              actions={[{ label: 'Remove from item', icon: <Unlink className="h-4 w-4" aria-hidden="true" />, destructive: true, onSelect: () => {} }]}
            />
            <ResourceItem
              item="service"
              media={<FileText className="h-5 w-5" aria-hidden="true" />}
              title="Service notes"
              meta="3 Mar 2026 14:05"
              badge={<Badge variant="outline">NOTE</Badge>}
              onOpen={() => {}}
            />
          </ResourceList>
          <div className="flex flex-col gap-6">
            <ResourceList label="Loading example" header="Loading" loading />
            <ResourceList label="Error example" header="Error" error="Could not load documents." onRetry={() => {}} />
            <ResourceList label="Empty example" header="Empty" empty={<p className="text-sm text-muted-foreground">No documents linked yet.</p>}>{[]}</ResourceList>
          </div>
        </div>
      </GallerySection>

      <Separator />

      <GallerySection
        title="SaveBar"
        description="Appears only while dirty, and takes over the app header (Polaris' contextual save bar) - toggle dirty and look at the stand-in header at the top of this page, not here. Save/Discard disable while saving."
      >
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input type="checkbox" checked={dirty} onChange={(e) => setDirty(e.target.checked)} />
          dirty
        </label>
        <SaveBar
          dirty={dirty}
          saving={saving}
          onSave={() => { setSaving(true); setTimeout(() => { setSaving(false); setDirty(false) }, 600) }}
          onDiscard={() => setDirty(false)}
        />
      </GallerySection>

      <Separator />

      <GallerySection title="ConfirmDelete" description="An alert-dialog wrapper that leaves closing to the caller.">
        <Button type="button" variant="outline" onClick={() => setConfirmOpen(true)}>Delete something…</Button>
      </GallerySection>

      <ConfirmDelete
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title='Delete "Main generator"?'
        description="This removes the fixture record. It can't be undone."
        onConfirm={() => setConfirmOpen(false)}
      />
    </Page>
    </>
  )
}
