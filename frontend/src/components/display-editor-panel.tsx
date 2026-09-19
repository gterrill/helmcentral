import { useRef, useState } from 'react'
import { ArrowDown, ArrowUp, Copy, ExternalLink, Plus, Trash2, Unlink2 } from 'lucide-react'

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Field, FieldGroup, FieldLabel, FieldLegend, FieldSeparator, FieldSet } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { DisplayFields } from '@/hooks/use-displays'
import { DEFAULT_DWELL_SECONDS, type Display } from '@/lib/displays'
import { moveWithinDisplay } from '@/lib/page-order'
import { cn } from '@/lib/utils'

// ADR 0112: the per-display editor. Same "no state, no requests" stance as
// wall-displays-panel.tsx (its index) - every value and every mutation is a
// prop, so App.tsx's route wiring decides how `display` gets resolved (by
// id or by the `/wall-displays/<slug>` the ADR specifies) and this component
// never has to know.
//
// Field validation and copy are harvested from the dialog this replaces
// (displays-dialog.tsx) rather than rewritten - the bounds mirror
// backend/displays.go's own constants exactly, and stay client-side hints
// only: the server is still the authority, and a rejected save surfaces its
// own message through onUpdate's existing toast plumbing, never a fabricated
// string from here.
const DISPLAY_NAME_MAX_LEN = 48
const DISPLAY_SLUG_MAX_LEN = 32
const DISPLAY_MIN_PX = 200
const DISPLAY_MAX_WIDTH_PX = 7680
const DISPLAY_MAX_HEIGHT_PX = 4320
const DISPLAY_MIN_SCALE = 0.5
const DISPLAY_MAX_SCALE = 4.0
const DISPLAY_SLUG_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/

// Mirrors backend/dashboard_pages.go's dwellSecondsMin/dwellSecondsMax
// exactly, same reasoning page-display-select.tsx's own copy of these gives:
// a value this control lets through has to be one the server actually
// accepts.
const MIN_DWELL_SECONDS = 5
const MAX_DWELL_SECONDS = 3600

function validName(name: string): boolean {
  return name.trim() !== '' && name.length <= DISPLAY_NAME_MAX_LEN
}

function validSlug(slug: string): boolean {
  return slug !== '' && slug.length <= DISPLAY_SLUG_MAX_LEN && DISPLAY_SLUG_PATTERN.test(slug)
}

function validGeometry(width: number, height: number): boolean {
  if (Number.isNaN(width) || Number.isNaN(height)) return false
  if (width === 0 && height === 0) return true
  return width >= DISPLAY_MIN_PX && width <= DISPLAY_MAX_WIDTH_PX &&
    height >= DISPLAY_MIN_PX && height <= DISPLAY_MAX_HEIGHT_PX
}

function validScale(scale: number, width: number): boolean {
  if (Number.isNaN(scale)) return false
  if (scale === 0 || scale === 1) return true
  if (scale < DISPLAY_MIN_SCALE || scale > DISPLAY_MAX_SCALE) return false
  return width !== 0
}

function validDwell(seconds: number): boolean {
  return Number.isInteger(seconds) && seconds >= MIN_DWELL_SECONDS && seconds <= MAX_DWELL_SECONDS
}

function deleteConfirmationMessage(display: Display, pageCount: number): string {
  if (pageCount === 0) return `Delete ${display.name}? It has no pages on it.`
  const pages = pageCount === 1 ? '1 page' : `${pageCount} pages`
  return `Delete ${display.name}? Its ${pages} stay, and move back into the Dashboard list.`
}

export type WallDisplayCondition = 'always' | 'anchored' | 'motoring' | 'sailing' | 'moored'

/** Everything a page-assignment edit can touch in one call - the same shape
 * page-display-select.tsx's `onDisplayPatch` already takes, so whatever
 * App.tsx handler backs that control backs this table's Add/Remove/Dwell/
 * Condition actions too. */
export interface DisplayPatch {
  display_id?: string
  dwell_seconds?: number
  show_when?: WallDisplayCondition
  /** Only its length is read, to mark a page that has nothing on it yet.
   * displayFeed skips such a page deliberately (a blank slot for a whole
   * dwell is a mistake to skip, not a screen to render), so without the
   * marker a freshly created page looks like it silently failed to join
   * the rotation. */
  widgets?: unknown[]
}

/** The subset of a dashboard page this editor needs. Structurally satisfied
 * by DashboardPage (use-dashboard-pages.ts) - pass the full fetched list
 * straight through with no mapping. This editor filters it three ways:
 * `display_id === display.id` for its own table (already in feed/server
 * order - see lib/displays.ts's displayFeed comment, there is no separate
 * wall ordering), `!display_id` for the Add-page picker's candidates, and
 * the whole list is what lib/page-order.ts's moveWithinDisplay needs to
 * compute a reorder. */
export interface DisplayEditorPage {
  id: string
  name: string
  display_id?: string
  dwell_seconds?: number
  show_when?: WallDisplayCondition
  /** Only its length is read, to mark a page with nothing on it yet.
   * displayFeed skips such a page deliberately (a blank slot for a whole
   * dwell is a mistake to skip, not a screen to render), so without the
   * marker a page created here looks like it silently failed to join the
   * rotation it was just added to. */
  widgets?: unknown[]
}

export interface DisplayEditorPanelProps {
  /**
   * The display being edited, resolved by the caller from whatever the
   * `/wall-displays/<slug>` route names (ADR 0112) - this component reads
   * fields off it and never resolves an id or slug itself. Null while it
   * hasn't loaded yet, or names a display that no longer exists: the
   * component renders a "not found" state with a way back rather than
   * crashing on a field read.
   */
  display: Display | null
  pages: DisplayEditorPage[]
  /** Every configured display, this one included - filtered internally to
   * "every other one" for the Duplicate menu's target list. */
  displays: Display[]
  /** The breadcrumb's first segment ("Wall displays") calls this. */
  onBack: () => void
  onUpdate: (id: string, patch: DisplayFields) => Promise<Display | null>
  /**
   * Resolves to the released page ids on success (the exact shape
   * useDisplays.deleteDisplay already returns) or null on failure. This
   * component calls onBack itself once a delete actually succeeds; it does
   * nothing further on a null (the caller's own toast already reported it).
   */
  onDelete: (id: string) => Promise<string[] | null>
  /**
   * Patches one page's display assignment, dwell or condition - see
   * DisplayPatch above. Drives four actions here: assigning a page from the
   * Add-page picker (`{ display_id, dwell_seconds }`), removing one
   * (`{ display_id: '' }`), and committing a dwell or condition edit.
   */
  onDisplayPatch: (pageId: string, patch: DisplayPatch) => void
  /**
   * Duplicates `pageId` onto `targetDisplayId` - the same relation
   * layout-toolbar.tsx's `onDuplicateToDisplay` already covers from the
   * page's own toolbar (that prop also accepts `null`, for "duplicate onto
   * the plain Dashboard list"; this row action only ever offers another
   * display, so it never passes null). The same App.tsx handler can back
   * both: it owns the actual create call and putting the copy into ADR
   * 0107's naming mode: this component only offers the choice of target.
   */
  onDuplicateToDisplay: (pageId: string, targetDisplayId: string) => void
  /**
   * Opens `/dashboard/<id>` where the page's tiles are actually arranged.
   * A callback rather than a real `<a href>`, matching how the rest of the
   * app's internal navigation already works (e.g. display-sidebar-group.tsx's
   * `onSelectPage`) rather than a raw path this component would have to get
   * right on its own.
   */
  onOpenPage: (pageId: string) => void
  /**
   * Creates an empty page already assigned to this display, and resolves to
   * it. Setting a screen up is exactly when its pages do not exist yet, so
   * an adopt-only picker sent the operator away to make them and back to
   * adopt them; this is the other half of that. The caller owns the create
   * (App.tsx's createPage), including the default dwell the server
   * requires on any page that carries a display.
   */
  onCreatePage: (displayId: string) => Promise<{ id: string; name: string } | null>
  /**
   * Commits a full reorder - the exact shape useDashboardPages.reorderPages
   * already has (and which already toasts its own failures), so it can be
   * passed straight through. This component computes the new full-list
   * order itself via lib/page-order.ts's moveWithinDisplay before calling
   * this; a null from that (already at the end, or the page isn't on this
   * display after all) means the click is silently a no-op.
   */
  onReorder: (orderedPageIds: string[]) => Promise<boolean>
  /** True while a reorder is in flight - disables every Order button so a
   * second click can't race the first. */
  reordering?: boolean
  canWrite?: boolean
}

function CanvasMagnificationFields({ display, onUpdate, canWrite }: { display: Display; onUpdate: DisplayEditorPanelProps['onUpdate']; canWrite: boolean }) {
  const [geometryInvalid, setGeometryInvalid] = useState(false)
  const [scaleInvalid, setScaleInvalid] = useState(false)
  const widthRef = useRef<HTMLInputElement>(null)
  const heightRef = useRef<HTMLInputElement>(null)

  const commitGeometry = () => {
    const width = Number(widthRef.current?.value ?? '')
    const height = Number(heightRef.current?.value ?? '')
    if (!validGeometry(width, height)) { setGeometryInvalid(true); return }
    setGeometryInvalid(false)
    if (width !== display.width || height !== display.height) void onUpdate(display.id, { width, height })
  }
  const commitGeometryOnEnter = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return
    e.preventDefault()
    commitGeometry()
  }

  const commitScale = (raw: string) => {
    const scale = Number(raw)
    if (!validScale(scale, display.width)) { setScaleInvalid(true); return }
    setScaleInvalid(false)
    if (scale !== (display.scale || 1)) void onUpdate(display.id, { scale })
  }

  return (
    <>
      <Field className="w-auto">
        <FieldLabel>Canvas</FieldLabel>
        <div className="flex items-center gap-1.5">
          <Input
            ref={widthRef}
            key={`${display.id}-width-${display.width}`}
            type="number"
            aria-label={`Width for ${display.name}`}
            defaultValue={display.width}
            disabled={!canWrite}
            className={cn('w-24 font-mono tabular-nums', geometryInvalid && 'border-destructive text-destructive')}
            onBlur={commitGeometry}
            onKeyDown={commitGeometryOnEnter}
          />
          <span aria-hidden="true" className="text-muted-foreground">×</span>
          <Input
            ref={heightRef}
            key={`${display.id}-height-${display.height}`}
            type="number"
            aria-label={`Height for ${display.name}`}
            defaultValue={display.height}
            disabled={!canWrite}
            className={cn('w-24 font-mono tabular-nums', geometryInvalid && 'border-destructive text-destructive')}
            onBlur={commitGeometry}
            onKeyDown={commitGeometryOnEnter}
          />
        </div>
      </Field>
      <Field className="w-24">
        <FieldLabel htmlFor={`display-${display.id}-scale`}>Magnification</FieldLabel>
        <Input
          id={`display-${display.id}-scale`}
          key={`${display.id}-scale-${display.scale}`}
          type="number"
          step={0.1}
          aria-label={`Scale for ${display.name}`}
          defaultValue={display.scale || 1}
          disabled={!canWrite}
          className={cn('font-mono tabular-nums', scaleInvalid && 'border-destructive text-destructive')}
          onBlur={(e) => commitScale(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commitScale(e.currentTarget.value) } }}
        />
      </Field>
    </>
  )
}

export function DisplayEditorPanel({
  display,
  pages,
  displays,
  onBack,
  onUpdate,
  onDelete,
  onDisplayPatch,
  onDuplicateToDisplay,
  onOpenPage,
  onCreatePage,
  onReorder,
  reordering = false,
  canWrite = true,
}: DisplayEditorPanelProps) {
  const [nameInvalid, setNameInvalid] = useState(false)
  const [slugInvalid, setSlugInvalid] = useState(false)
  const [dwellInvalid, setDwellInvalid] = useState<Record<string, boolean>>({})
  const [deleteOpen, setDeleteOpen] = useState(false)
  const [addPageValue, setAddPageValue] = useState('')
  const [creating, setCreating] = useState(false)

  const breadcrumb = (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); onBack() }}>
            Wall displays
          </BreadcrumbLink>
        </BreadcrumbItem>
        {display && (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>{display.name}</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        )}
      </BreadcrumbList>
    </Breadcrumb>
  )

  if (!display) {
    return (
      <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
        {breadcrumb}

        <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
          This display could not be found.
        </div>
      </div>
    )
  }

  const commitName = (raw: string) => {
    const name = raw.trim()
    if (!validName(name)) { setNameInvalid(true); return }
    setNameInvalid(false)
    if (name !== display.name) void onUpdate(display.id, { name })
  }

  const commitSlug = (raw: string) => {
    const slug = raw.trim()
    if (!validSlug(slug)) { setSlugInvalid(true); return }
    setSlugInvalid(false)
    if (slug !== display.slug) void onUpdate(display.id, { slug })
  }

  const commitOnEnter = (commit: (value: string) => void) => (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Enter') return
    e.preventDefault()
    commit(e.currentTarget.value)
  }

  const displayPages = pages.filter((p) => p.display_id === display.id)
  const availablePages = pages.filter((p) => !p.display_id)
  const otherDisplays = displays.filter((d) => d.id !== display.id)

  const commitDwell = (page: DisplayEditorPage, raw: string) => {
    const seconds = Number(raw)
    if (!validDwell(seconds)) {
      setDwellInvalid((prev) => ({ ...prev, [page.id]: true }))
      return
    }
    setDwellInvalid((prev) => ({ ...prev, [page.id]: false }))
    if (seconds !== page.dwell_seconds) onDisplayPatch(page.id, { dwell_seconds: seconds })
  }

  const handleMove = (pageId: string, direction: 'up' | 'down') => {
    if (reordering) return
    const order = moveWithinDisplay(pages, display.id, pageId, direction)
    if (order) void onReorder(order)
  }

  // Setting a screen up is exactly when its pages don't exist yet, so the
  // adopt-only picker below sent the operator away to make one and back to
  // adopt it. This makes the blank page here, already on this display, and
  // leaves them on the rotation they are composing.
  const handleNewPage = async () => {
    setCreating(true)
    try {
      await onCreatePage(display.id)
    } finally {
      setCreating(false)
    }
  }

  const handleAddPage = (pageId: string) => {
    if (pageId === '') return
    onDisplayPatch(pageId, { display_id: display.id, dwell_seconds: DEFAULT_DWELL_SECONDS })
    setAddPageValue('')
  }

  const handleDeleteConfirm = async () => {
    const released = await onDelete(display.id)
    setDeleteOpen(false)
    if (released !== null) onBack()
  }

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
      {breadcrumb}

      {/* The page's own toolbar, in layout-toolbar.tsx's vocabulary: these
          act on the display as a whole, not on any one field below, so they
          sit above the settings rather than stranded inside them. */}
      <div data-testid="display-editor-toolbar" className="flex w-fit flex-wrap items-center gap-2">
        <a
          href={`/display/${display.slug}`}
          target="_blank"
          rel="noopener noreferrer"
          className="inline-flex w-fit items-center gap-1 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors hover:border-primary/40 hover:text-primary focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
        >
          <ExternalLink className="h-3.5 w-3.5" aria-hidden="true" />
          Preview
        </a>
        {canWrite && (
          <>
            <div className="mx-1 h-5 w-px bg-border" aria-hidden="true" />
            <button
              type="button"
              onClick={() => setDeleteOpen(true)}
              className="inline-flex w-fit items-center gap-1 rounded-md border border-border bg-background/70 px-3 py-1.5 text-xs font-semibold uppercase tracking-[0.1em] text-muted-foreground transition-colors hover:border-destructive/40 hover:text-destructive focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <Trash2 className="h-3.5 w-3.5" aria-hidden="true" />
              Delete display
            </button>
          </>
        )}
      </div>

      {/* One group, two rows: the fields that describe the screen's
          geometry, and beneath them the two flags that describe how the
          panel behaves. shadcn's FieldSet/FieldLegend carry the house
          micro-label treatment already, so the section names need no
          bespoke heading. */}
      <FieldSet className="rounded-md border border-border bg-card p-4">
        <FieldLegend variant="label">Display settings</FieldLegend>
        <FieldGroup>
          <div className="flex flex-wrap items-start gap-x-6 gap-y-4">
        <Field className="w-44">
          <FieldLabel htmlFor={`display-${display.id}-name`}>Name</FieldLabel>
          <Input
            id={`display-${display.id}-name`}
            key={`${display.id}-name-${display.name}`}
            defaultValue={display.name}
            maxLength={DISPLAY_NAME_MAX_LEN}
            disabled={!canWrite}
            className={cn(nameInvalid && 'border-destructive text-destructive')}
            onBlur={(e) => commitName(e.target.value)}
            onKeyDown={commitOnEnter(commitName)}
          />
        </Field>

        <Field className="w-44">
          <FieldLabel htmlFor={`display-${display.id}-slug`}>Address</FieldLabel>
          <Input
            id={`display-${display.id}-slug`}
            key={`${display.id}-slug-${display.slug}`}
            defaultValue={display.slug}
            maxLength={DISPLAY_SLUG_MAX_LEN}
            disabled={!canWrite}
            className={cn('font-mono', slugInvalid && 'border-destructive text-destructive')}
            onBlur={(e) => commitSlug(e.target.value)}
            onKeyDown={commitOnEnter(commitSlug)}
          />
        </Field>

        <CanvasMagnificationFields display={display} onUpdate={onUpdate} canWrite={canWrite} />

        <Field className="w-36">
          <FieldLabel htmlFor={`display-${display.id}-rotate`}>Rotation</FieldLabel>
          <select
            id={`display-${display.id}-rotate`}
            className="h-10 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
            value={display.rotate}
            disabled={!canWrite}
            onChange={(e) => void onUpdate(display.id, { rotate: Number(e.target.value) as 0 | 180 })}
          >
            <option value={0}>Upright</option>
            <option value={180}>180°</option>
          </select>
        </Field>

          </div>

          <FieldSeparator />

          <div className="flex flex-wrap items-center gap-x-8 gap-y-3">
        <Field className="w-auto" orientation="horizontal">
          <div
            className="flex items-center gap-2"
            title="Shifts the image a few pixels on a slow cycle so a static board doesn't burn a ghost into the panel."
          >
            <Switch
              id={`display-${display.id}-oled`}
              checked={display.pixel_shift}
              disabled={!canWrite}
              onCheckedChange={(checked) => void onUpdate(display.id, { pixel_shift: checked })}
            />
            <Label htmlFor={`display-${display.id}-oled`}>OLED panel</Label>
          </div>
        </Field>

        <Field className="w-auto" orientation="horizontal">
          <div className="flex items-center gap-2">
            <Switch
              id={`display-${display.id}-wake`}
              checked={display.wake_lock}
              disabled={!canWrite}
              onCheckedChange={(checked) => void onUpdate(display.id, { wake_lock: checked })}
            />
            <Label htmlFor={`display-${display.id}-wake`}>Keep awake</Label>
          </div>
          </Field>
          </div>
        </FieldGroup>
      </FieldSet>

      {/* One group: the add control belongs to the table it acts on, not
          floating between two others as a stray field. The table scrolls
          sideways inside it when the viewport is narrower than the columns
          need, so the Actions column is never unreachable. */}
      <FieldSet className="rounded-md border border-border bg-card">
        <FieldLegend variant="label">Pages</FieldLegend>
        {canWrite && (
          <div className="flex flex-wrap items-center gap-3 border-b border-border px-4 py-3">
            <Button type="button" size="sm" disabled={creating} onClick={() => void handleNewPage()}>
              <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
              New page
            </Button>
            <div className="mx-1 h-5 w-px bg-border" aria-hidden="true" />
            <label
              htmlFor={`display-${display.id}-add-page`}
              className="text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground"
            >
              Add page
            </label>
            <select
              id={`display-${display.id}-add-page`}
              className="h-9 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
              value={addPageValue}
              disabled={availablePages.length === 0}
              onChange={(e) => handleAddPage(e.target.value)}
            >
              <option value="">{availablePages.length === 0 ? 'No pages available' : 'Choose a page…'}</option>
              {availablePages.map((p) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
          </div>
        )}
        <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="w-24 text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">Order</TableHead>
              <TableHead className="text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">Page</TableHead>
              <TableHead className="text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">Dwell</TableHead>
              <TableHead className="text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">Condition</TableHead>
              <TableHead className="text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {displayPages.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-8 text-center text-sm text-muted-foreground">
                  Nothing on this screen yet. New page starts a blank one here; Add page moves an existing dashboard page onto it.
                </TableCell>
              </TableRow>
            )}
            {displayPages.map((page, index) => (
              <TableRow key={page.id}>
                <TableCell>
                  <div className="flex gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Move ${page.name} up`}
                      disabled={!canWrite || reordering || index === 0}
                      focusableWhenDisabled
                      onClick={() => handleMove(page.id, 'up')}
                    >
                      <ArrowUp className="h-4 w-4" aria-hidden="true" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={`Move ${page.name} down`}
                      disabled={!canWrite || reordering || index === displayPages.length - 1}
                      focusableWhenDisabled
                      onClick={() => handleMove(page.id, 'down')}
                    >
                      <ArrowDown className="h-4 w-4" aria-hidden="true" />
                    </Button>
                  </div>
                </TableCell>
                <TableCell>
                  <button
                    type="button"
                    onClick={() => onOpenPage(page.id)}
                    className="rounded-sm text-left font-medium hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                  >
                    {page.name}
                  </button>
                  {(page.widgets?.length ?? 0) === 0 && (
                    <span className="ml-2 whitespace-nowrap text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">
                      No tiles yet
                    </span>
                  )}
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-1">
                    <input
                      type="number"
                      min={MIN_DWELL_SECONDS}
                      max={MAX_DWELL_SECONDS}
                      step={5}
                      key={`${page.id}-dwell-${page.dwell_seconds}`}
                      defaultValue={page.dwell_seconds ?? DEFAULT_DWELL_SECONDS}
                      aria-label={`Dwell seconds for ${page.name}`}
                      disabled={!canWrite}
                      className={cn(
                        'h-9 w-20 rounded-md border border-input bg-background px-2 font-mono text-sm tabular-nums focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50',
                        dwellInvalid[page.id] && 'border-destructive text-destructive',
                      )}
                      onBlur={(e) => commitDwell(page, e.target.value)}
                      onKeyDown={(e) => { if (e.key === 'Enter') { e.preventDefault(); commitDwell(page, e.currentTarget.value) } }}
                    />
                    <span className="text-xs text-muted-foreground">s</span>
                  </div>
                </TableCell>
                <TableCell>
                  <select
                    aria-label={`Condition for ${page.name}`}
                    value={page.show_when ?? 'always'}
                    disabled={!canWrite}
                    onChange={(e) => onDisplayPatch(page.id, { show_when: e.target.value as WallDisplayCondition })}
                    className="h-9 rounded-md border border-input bg-background px-2 text-sm focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    <option value="always">Always</option>
                    <option value="anchored">Anchored</option>
                    <option value="motoring">Motoring</option>
                    <option value="sailing">Sailing</option>
                    <option value="moored">Moored</option>
                  </select>
                </TableCell>
                <TableCell>
                  <div className="flex items-center gap-1">
                    <DropdownMenu>
                      <DropdownMenuTrigger
                        render={
                          <Button
                            variant="ghost"
                            size="sm"
                            disabled={!canWrite || otherDisplays.length === 0}
                            title={otherDisplays.length === 0 ? 'Add another display first.' : undefined}
                          >
                            <Copy className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                            Duplicate
                          </Button>
                        }
                      />
                      <DropdownMenuContent>
                        {otherDisplays.map((d) => (
                          <DropdownMenuItem key={d.id} onClick={() => onDuplicateToDisplay(page.id, d.id)}>
                            {d.name}
                          </DropdownMenuItem>
                        ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                    {canWrite && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        onClick={() => onDisplayPatch(page.id, { display_id: '' })}
                      >
                        <Unlink2 className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                        Remove from this display
                      </Button>
                    )}
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        </div>
      </FieldSet>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {display.name}?</AlertDialogTitle>
            <AlertDialogDescription>{deleteConfirmationMessage(display, displayPages.length)}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
              onClick={() => { void handleDeleteConfirm() }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
