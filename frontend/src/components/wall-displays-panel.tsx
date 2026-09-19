import { ExternalLink, Monitor, Plus } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbList,
  BreadcrumbPage,
} from '@/components/ui/breadcrumb'
import { Button, buttonVariants } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { displayScale, type Display } from '@/lib/displays'
import type { HelpTarget } from '@/lib/help-links'
import { cn } from '@/lib/utils'

// ADR 0112: the wall-displays index. Follows documents-panel.tsx's shell
// (flex column, gap-4 p-4, breadcrumb-left/actions-right header, a bordered
// scroll region below) because that panel is the only other index+drilldown
// surface in the app and the only other consumer of ui/table - this reuses
// its vocabulary rather than inventing a second one. Unlike DocumentsPanel,
// this component owns no state and makes no requests: every value it shows
// and every mutation it can trigger arrives as a prop, so the page/route
// wiring (still in flight elsewhere) can drive it from whatever hook shape
// it ends up with.

const MICRO_LABEL = 'text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground'

const LINK_BUTTON_CLASSNAME =
  'flex min-h-10 items-center gap-2 rounded-sm text-left font-medium hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2'

/** The empty-state's link to the how-to doc (docs/how-to/set-up-a-wall-display.md).
 * Built inline rather than imported from lib/help-links.ts (which this
 * component doesn't touch): a HelpTarget is just `{ page, heading? }`, and
 * this is the only place that needs this one. */
const SET_UP_WALL_DISPLAY_HELP_TARGET: HelpTarget = { page: 'how-to/set-up-a-wall-display' }

/** The subset of a dashboard page this index needs - just enough to count
 * how many pages sit on each display (the Pages column). Structurally
 * satisfied by DashboardPage (use-dashboard-pages.ts), so the real page
 * list can be passed straight through with no mapping. */
export interface WallDisplayPageRef {
  display_id?: string
}

export interface WallDisplaysPanelProps {
  displays: Display[]
  /** Every dashboard page - used only to compute each display's page count. */
  pages: WallDisplayPageRef[]
  /** True while the initial displays fetch is in flight - renders skeleton
   * rows, not a centred spinner. */
  loading: boolean
  /** Set only when the initial displays fetch itself failed (mirrors
   * useDisplays' own `error`). A failed field save on the editor surfaces
   * through the server's own message via the caller's existing toast
   * plumbing instead - this component never fabricates its own copy for
   * that case, so it carries no error prop for anything but this. */
  error: string | null
  onRetry: () => void
  /** Opens the editor for one display (display-editor-panel.tsx) - fired by
   * clicking a row's name. */
  onOpenDisplay: (id: string) => void
  /**
   * Creates a new display. Mirrors App.tsx's existing "New Page" flow (ADR
   * 0107): this button does not collect a name up front and opens no
   * dialog - the caller creates the record immediately (with whatever
   * placeholder name and starting geometry it already uses for
   * createDisplay), navigates straight into the editor, and puts that
   * editor's Name field into the same focused, start-empty naming mode
   * namingPageId gives a freshly created dashboard page. This component
   * only asks for that to happen; it has no opinion on the placeholder name.
   */
  onCreateDisplay: () => void
  /** Opens the in-app help at a target (App.tsx already threads this
   * exact callback into empty-page-prompt.tsx). Only the empty state uses
   * it, to link to docs/how-to/set-up-a-wall-display.md. */
  onOpenHelp: (target: HelpTarget) => void
  canWrite?: boolean
}

function formatCanvas(display: Display): string {
  if (display.width === 0 && display.height === 0) return 'Not measured'
  return `${display.width} × ${display.height} @ ${displayScale(display)}×`
}

function formatRotation(display: Display): string {
  return display.rotate === 180 ? '180°' : 'Upright'
}

function SkeletonRow() {
  return (
    <TableRow>
      <TableCell><Skeleton className="h-4 w-32" /></TableCell>
      <TableCell><Skeleton className="h-4 w-28" /></TableCell>
      <TableCell><Skeleton className="h-4 w-36" /></TableCell>
      <TableCell><Skeleton className="h-4 w-16" /></TableCell>
      <TableCell><Skeleton className="h-4 w-8" /></TableCell>
      <TableCell><Skeleton className="h-4 w-20" /></TableCell>
      <TableCell><Skeleton className="h-8 w-8" /></TableCell>
    </TableRow>
  )
}

function WallDisplaysEmptyState({
  onCreateDisplay,
  onOpenHelp,
  canWrite,
}: {
  onCreateDisplay: () => void
  onOpenHelp: (target: HelpTarget) => void
  canWrite: boolean
}) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      <Monitor className="h-8 w-8 text-muted-foreground" aria-hidden="true" />
      <div className="flex max-w-md flex-col gap-1.5">
        <p className="text-sm text-foreground">
          A wall display is its own screen - a saloon TV, a chart-table monitor - with a name, an
          address, and a set of dashboard pages that rotate on it in turn.
        </p>
        <p className="text-sm text-muted-foreground">
          Its canvas width, height and pixel density come from that screen itself, not from typing
          in a guess: open <span className="font-mono text-xs">/display-probe.html</span> on the
          actual screen and carry its numbers into the new display's Canvas fields.
        </p>
      </div>
      <button
        type="button"
        onClick={() => onOpenHelp(SET_UP_WALL_DISPLAY_HELP_TARGET)}
        className="text-xs font-semibold uppercase tracking-[0.1em] text-primary hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 rounded-sm"
      >
        How to set up a wall display
      </button>
      {canWrite && (
        <Button type="button" onClick={onCreateDisplay} className="mt-2">
          <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
          New display
        </Button>
      )}
    </div>
  )
}

export function WallDisplaysPanel({
  displays,
  pages,
  loading,
  error,
  onRetry,
  onOpenDisplay,
  onCreateDisplay,
  onOpenHelp,
  canWrite = true,
}: WallDisplaysPanelProps) {
  const pageCountFor = (displayId: string) => pages.filter((p) => p.display_id === displayId).length

  return (
    <div className="flex h-full min-h-0 flex-col gap-4 overflow-y-auto p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Breadcrumb>
          <BreadcrumbList>
            <BreadcrumbItem>
              <BreadcrumbPage>Wall displays</BreadcrumbPage>
            </BreadcrumbItem>
          </BreadcrumbList>
        </Breadcrumb>
        {canWrite && !loading && !error && displays.length > 0 && (
          <Button type="button" size="sm" onClick={onCreateDisplay}>
            <Plus className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
            New display
          </Button>
        )}
      </div>

      <div className="overflow-x-auto rounded-md border border-border bg-card">
        {error ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
            <p role="alert" className="text-sm text-destructive">{error}</p>
            <Button type="button" variant="outline" size="sm" onClick={onRetry}>Retry</Button>
          </div>
        ) : !loading && displays.length === 0 ? (
          <WallDisplaysEmptyState onCreateDisplay={onCreateDisplay} onOpenHelp={onOpenHelp} canWrite={canWrite} />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className={MICRO_LABEL}>Name</TableHead>
                <TableHead className={MICRO_LABEL}>Address</TableHead>
                <TableHead className={MICRO_LABEL}>Canvas</TableHead>
                <TableHead className={MICRO_LABEL}>Rotation</TableHead>
                <TableHead className={MICRO_LABEL}>Pages</TableHead>
                <TableHead className={MICRO_LABEL}>Flags</TableHead>
                <TableHead className="w-10" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading
                ? Array.from({ length: 3 }).map((_, i) => <SkeletonRow key={i} />)
                : displays.map((display) => (
                  <TableRow key={display.id}>
                    <TableCell>
                      <button
                        type="button"
                        onClick={() => onOpenDisplay(display.id)}
                        className={LINK_BUTTON_CLASSNAME}
                      >
                        <Monitor className="h-4 w-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                        {display.name}
                      </button>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs text-muted-foreground">/display/{display.slug}</span>
                    </TableCell>
                    <TableCell>
                      <span
                        className="font-mono text-xs tabular-nums text-muted-foreground"
                        title={display.width === 0 && display.height === 0
                          ? 'Run /display-probe.html on this screen to fill this in.'
                          : undefined}
                      >
                        {formatCanvas(display)}
                      </span>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">{formatRotation(display)}</span>
                    </TableCell>
                    <TableCell>
                      <span className="font-mono text-xs tabular-nums text-muted-foreground">{pageCountFor(display.id)}</span>
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap gap-1">
                        {display.pixel_shift && <Badge variant="outline">OLED panel</Badge>}
                        {display.wake_lock && <Badge variant="outline">Keep awake</Badge>}
                      </div>
                    </TableCell>
                    <TableCell>
                      <a
                        href={`/display/${display.slug}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        aria-label={`Preview ${display.name}`}
                        title="Preview"
                        className={cn(buttonVariants({ variant: 'ghost', size: 'icon' }))}
                      >
                        <ExternalLink className="h-4 w-4" aria-hidden="true" />
                      </a>
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
        )}
      </div>
    </div>
  )
}
