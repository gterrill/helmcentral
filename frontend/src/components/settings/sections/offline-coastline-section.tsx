import { useCallback, useEffect, useRef, useState } from 'react'

import { useVesselStateSelector } from '@/hooks/use-vessel-state'
import { selectTimeZone } from '@/lib/vessel-time'
import { ConfirmDelete, FormSection, SettingsLayout } from '@/components/patterns'
import { OfflineCoastlineMap } from '@/components/offline-coastline-map'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { useOfflineCoastline } from '@/hooks/use-offline-coastline'
import type { HelpTarget } from '@/lib/help-links'
import {
  clockTime,
  coastlineCheckFailures,
  coastlineRefreshFailures,
  coastlineFailures,
  coastlineStatusLine,
  downloadConfirmText,
  editCheckPauseLine,
  inlandSkippedLine,
  removeConfirmText,
  requestedLine,
  summariseSelection,
  waitingRequests,
  type OfflineCoastlineStatus,
  type RequestPreview,
} from '@/lib/offline-coastline'

interface OfflineCoastlineSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
  /** Opens the in-app help, for the link to the Overpass how-to. */
  onOpenHelp: (target: HelpTarget) => void
}

const OVERPASS_HELP_TARGET: HelpTarget = { page: 'how-to/run-your-own-overpass-server' }

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

function retryTime(iso: string | undefined, timeZone: string | undefined): string | null {
  if (!iso) return null
  const t = new Date(iso)
  if (Number.isNaN(t.getTime()) || t.getTime() <= Date.now()) return null
  return clockTime(iso, timeZone)
}

function Failures({ status }: { status: OfflineCoastlineStatus }) {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const failures = coastlineFailures(status)
  const checks = coastlineCheckFailures(status)
  const refreshes = coastlineRefreshFailures(status)
  if (failures.length === 0 && checks.length === 0 && refreshes.length === 0) return null
  const list = (items: typeof failures) => (
    <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
      {items.map((f) => {
        const retry = retryTime(f.nextAttemptAt, timeZone)
        return (
          <li key={f.label} className="min-w-0 break-words">
            <span className="font-medium text-foreground">{f.label}</span>: {f.error}
            {retry ? ` (trying again about ${retry})` : ''}
          </li>
        )
      })}
    </ul>
  )
  return (
    <div className="flex flex-col gap-3" data-testid="offline-coastline-failures">
      {failures.length > 0 && (
        <div className="flex flex-col gap-1">
          <p className="text-xs font-medium text-destructive">
            {plural(failures.length, 'square', 'squares')} did not download
          </p>
          {list(failures)}
        </div>
      )}
      {refreshes.length > 0 && (
        <div className="flex flex-col gap-1" data-testid="offline-coastline-refresh-failures">
          <p className="text-xs font-medium">
            {plural(refreshes.length, 'held square', 'held squares')} could not be downloaded again (the old copy is kept)
          </p>
          {list(refreshes)}
        </div>
      )}
      {checks.length > 0 && (
        <div className="flex flex-col gap-1" data-testid="offline-coastline-check-failures">
          <p className="text-xs font-medium">
            {plural(checks.length, 'held square', 'held squares')} could not be checked for edits (the coverage is kept)
          </p>
          {list(checks)}
        </div>
      )}
    </div>
  )
}

export function OfflineCoastlineSection({ draft, onChange, onOpenHelp }: OfflineCoastlineSectionProps) {
  const timeZone = useVesselStateSelector(selectTimeZone)
  const { status, loading, error, update, clearCoverage, previewRequest, requestSquares, removeSquares } = useOfflineCoastline()
  const checkPauseLine = status ? editCheckPauseLine(status, timeZone) : null
  const [actionError, setActionError] = useState<string | null>(null)
  const [updating, setUpdating] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const [clearing, setClearing] = useState(false)

  const [selecting, setSelecting] = useState(false)
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set())
  // The download confirm: closed, or open with the server's count of what the
  // request would do. checkError is the count failing (the cap, the switch
  // off); requestError is the request itself failing after the operator agreed.
  const [download, setDownload] = useState<{
    open: boolean
    /** The squares the preview was counted for, and the ones a confirm sends. */
    squares: string[]
    preview: RequestPreview | null
    checkError: string | null
    requestError: string | null
    busy: boolean
  }>({ open: false, squares: [], preview: null, checkError: null, requestError: null, busy: false })
  const [confirmRemove, setConfirmRemove] = useState(false)
  const [removing, setRemoving] = useState(false)

  const switchedOn = status?.enabled === true
  const clearSelection = useCallback(() => setSelected(new Set()), [])
  const leaveSelecting = useCallback(() => {
    setSelecting(false)
    clearSelection()
  }, [clearSelection])
  // The server turning the switch off ends select mode: a request would be refused.
  useEffect(() => {
    if (status && !status.enabled) leaveSelecting()
  }, [status, leaveSelecting])
  const selection = status ? summariseSelection(selected, status) : null
  const preview = download.preview

  // Each open of the dialog takes a number, so a slow count for an earlier
  // open cannot overwrite the one on screen.
  const previewSeq = useRef(0)
  const openDownload = async () => {
    if (!selection) return
    const squares = selection.download
    const seq = ++previewSeq.current
    setActionError(null)
    setDownload({ open: true, squares, preview: null, checkError: null, requestError: null, busy: false })
    try {
      const preview = await previewRequest(squares)
      if (seq === previewSeq.current) setDownload((d) => ({ ...d, preview }))
    } catch (e) {
      const checkError = e instanceof Error ? e.message : 'Could not check the request'
      if (seq === previewSeq.current) setDownload((d) => ({ ...d, checkError }))
    }
  }

  // Sends the squares that were counted, not whatever the selection is by now.
  const handleDownload = async () => {
    setDownload((d) => ({ ...d, busy: true, requestError: null }))
    try {
      await requestSquares(download.squares)
      setDownload((d) => ({ ...d, open: false, busy: false }))
      leaveSelecting()
    } catch (e) {
      const requestError = e instanceof Error ? e.message : 'Download request failed'
      setDownload((d) => ({ ...d, busy: false, requestError }))
    }
  }

  const handleRemove = async () => {
    if (!selection) return
    setActionError(null)
    setRemoving(true)
    try {
      await removeSquares(selection.remove)
      leaveSelecting()
    } catch (e) {
      setActionError(e instanceof Error ? e.message : 'Removing squares failed')
    } finally {
      setConfirmRemove(false)
      setRemoving(false)
    }
  }

  const unsaved = status !== undefined && status.enabled !== draft.offlineCoastlineEnabled
  const hasEdits = (status?.squares_with_edits ?? 0) > 0
  const hasSquares = (status?.squares.some((q) => q.state !== 'pending' || q.requested)) ?? false
  const waiting = status ? waitingRequests(status) : 0
  // The radius is the server's; until the status loads the text names no figure.
  const withinRadius = status ? `within ${status.radius_nm} nm of the boat` : 'within range of the boat'

  const handleUpdate = async () => {
    setActionError(null)
    setUpdating(true)
    try {
      await update()
    } catch (e) {
      setActionError(e instanceof Error ? e.message : 'Update failed')
    } finally {
      setUpdating(false)
    }
  }

  const handleClear = async () => {
    setActionError(null)
    setClearing(true)
    try {
      await clearCoverage()
      setConfirmClear(false)
    } catch (e) {
      setActionError(e instanceof Error ? e.message : 'Clearing coverage failed')
    } finally {
      setClearing(false)
    }
  }

  const tools = (hasEdits || hasSquares) ? (
    <>
      {hasEdits && status && (
        <div className="flex flex-col gap-1">
          <Button type="button" variant="outline" size="sm" onClick={() => void handleUpdate()} disabled={updating || status.state === 'updating'}>
            Update
          </Button>
          <p className="text-xs text-muted-foreground">
            {plural(status.edits_since, 'edit', 'edits')} since download in {plural(status.squares_with_edits, 'square', 'squares')}.
            Update downloads those squares again.
          </p>
        </div>
      )}
      {hasSquares && (
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="border-destructive/40 text-destructive"
          onClick={() => setConfirmClear(true)}
        >
          Clear coverage
        </Button>
      )}
    </>
  ) : undefined

  return (
    <SettingsLayout title="Offline coastline" tools={tools}>
      <FormSection
        title="Offline coastline"
        description="The charted coastline and reefs, kept on board for the water around the boat, so Mate can judge the shelter of an anchorage without internet."
      >
        <FieldGroup>
          <Field orientation="horizontal">
            <Switch
              checked={draft.offlineCoastlineEnabled}
              onCheckedChange={(checked) => onChange({ offlineCoastlineEnabled: checked })}
              aria-label="Keep an offline coastline"
            />
            <FieldContent>
              <FieldLabel>Keep an offline coastline</FieldLabel>
              <FieldDescription>
                Downloads OpenStreetMap coastline and reefs for every 1° square {withinRadius}, one square at a
                time in the background, and checks each week whether the public map has changed. It uses the internet while
                it fills. Off, nothing is downloaded or checked.
              </FieldDescription>
              <FieldDescription>
                The squares come from the Overpass server set in Settings → Plugins → OpenStreetMap (Overpass). A server
                aboard fills them much faster than a public one.{' '}
                <button
                  type="button"
                  onClick={() => onOpenHelp(OVERPASS_HELP_TARGET)}
                  className="rounded-sm font-medium text-primary hover:underline focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                >
                  Run your own Overpass server
                </button>
              </FieldDescription>
            </FieldContent>
          </Field>
        </FieldGroup>

        <div className="flex flex-col gap-1" role="status" aria-live="polite" data-testid="offline-coastline-status">
          {loading && !status ? (
            <p className="text-sm text-muted-foreground">Reading status…</p>
          ) : error && !status ? (
            <p className="text-sm text-destructive">Could not read the offline coastline status: {error}</p>
          ) : status ? (
            <p className="text-sm font-medium">{coastlineStatusLine(status, new Date(), timeZone)}</p>
          ) : null}
          {status && inlandSkippedLine(status) && (
            <p className="text-xs text-muted-foreground" data-testid="offline-coastline-inland">
              {inlandSkippedLine(status)}
            </p>
          )}
          {status && requestedLine(status) && (
            <p className="text-xs text-muted-foreground" data-testid="offline-coastline-requested">
              {requestedLine(status)}
            </p>
          )}
          {checkPauseLine && (
            <p className="text-xs text-muted-foreground" data-testid="offline-coastline-check-pause">
              {checkPauseLine}
            </p>
          )}
          {unsaved && <p className="text-xs text-muted-foreground">Save settings to apply the switch.</p>}
        </div>
        {actionError && <p className="text-sm text-destructive" role="alert">{actionError}</p>}
      </FormSection>

      {status && (status.squares.length > 0 || (status.enabled && status.position)) && (
        <FormSection
          title="Coverage"
          description={`Each square is 1° of latitude by 1° of longitude, named by its south-west corner. Shaded squares are held, and stronger shading means a more recent download. Squares drawn as an outline only are within the ring but not downloaded yet. Red squares failed to download. A heavy outline means edits since the download. Amber squares are ones you asked for that are still waiting. The dashed ring is ${status.radius_nm} nm around the boat.`}
        >
          {/* Escape ends select mode only from inside this block. Sheets and popups
              render elsewhere and keep their own Escape. */}
          <div
            className="flex flex-col gap-4"
            onKeyDown={(e) => {
              if (e.key === 'Escape' && selecting && !e.defaultPrevented) leaveSelecting()
            }}
          >
            <div className="flex flex-wrap items-center gap-2" data-testid="offline-coastline-select-bar">
              <Button
                type="button"
                variant="outline"
                size="sm"
                disabled={!switchedOn}
                onClick={() => (selecting ? leaveSelecting() : setSelecting(true))}
              >
                {selecting ? 'Cancel' : 'Select squares'}
              </Button>
              {!switchedOn && (
                <p className="text-xs text-muted-foreground">Turn on the offline coastline to download squares</p>
              )}
              {selecting && selection && (
                <>
                  {selection.download.length > 0 && (
                    <Button type="button" size="sm" onClick={() => void openDownload()}>
                      Download {selection.download.length}
                    </Button>
                  )}
                  {selection.remove.length > 0 && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="border-destructive/40 text-destructive"
                      onClick={() => setConfirmRemove(true)}
                    >
                      Remove {selection.remove.length}
                    </Button>
                  )}
                </>
              )}
              {selecting && (
                <p className="basis-full text-xs text-muted-foreground">
                  Tap or click a square to select it. To select a block, Shift-drag with a mouse, or press and hold then drag on a touch screen.
                </p>
              )}
            </div>
            <div className="h-72 overflow-hidden rounded-md border border-border" data-testid="offline-coastline-map">
              <OfflineCoastlineMap
                status={status}
                selecting={selecting}
                selected={selected}
                onSelectedChange={setSelected}
              />
            </div>
          </div>
          <Failures status={status} />
        </FormSection>
      )}

      <AlertDialog
        open={download.open}
        onOpenChange={(open) => {
          if (!open && !download.busy) setDownload((d) => ({ ...d, open: false }))
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {download.preview ? `Download ${plural(download.preview.requested, 'square', 'squares')}?` : 'Download squares?'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {preview
                ? preview.requested > 0
                  ? downloadConfirmText(preview)
                  : `Nothing to download: ${plural(preview.inland_skipped, 'all-land square', 'all-land squares')} skipped.`
                : download.checkError ?? 'Checking the squares…'}
            </AlertDialogDescription>
          </AlertDialogHeader>
          {download.requestError && <p className="text-sm text-destructive" role="alert">{download.requestError}</p>}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={download.busy}>Cancel</AlertDialogCancel>
            <Button
              type="button"
              disabled={download.busy || !download.preview || download.preview.requested === 0}
              onClick={() => void handleDownload()}
            >
              {download.busy ? 'Requesting…' : 'Download'}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      <ConfirmDelete
        open={confirmRemove}
        onOpenChange={setConfirmRemove}
        title={selection ? `Remove ${plural(selection.remove.length, 'square', 'squares')}?` : 'Remove squares?'}
        description={selection && status ? removeConfirmText(selection, status.radius_nm) : undefined}
        confirmLabel="Remove"
        onConfirm={() => void handleRemove()}
        deleting={removing}
      />

      <ConfirmDelete
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title="Clear coverage?"
        description={`This deletes every downloaded square${waiting > 0 ? ` and cancels the ${plural(waiting, 'square', 'squares')} you asked to download` : ''}. Mate cannot judge shelter until squares are downloaded again. With the offline coastline on, the squares ${withinRadius} start downloading again straight away; turn it off first to keep them gone.`}
        confirmLabel="Clear coverage"
        onConfirm={() => void handleClear()}
        deleting={clearing}
      />
    </SettingsLayout>
  )
}
