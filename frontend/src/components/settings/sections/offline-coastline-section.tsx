import { useState } from 'react'

import { ConfirmDelete, FormSection, SettingsLayout } from '@/components/patterns'
import { OfflineCoastlineMap } from '@/components/offline-coastline-map'
import type { RegularSettingsDraft } from '@/components/settings/settings-draft'
import { Button } from '@/components/ui/button'
import { Field, FieldContent, FieldDescription, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Switch } from '@/components/ui/switch'
import { useOfflineCoastline } from '@/hooks/use-offline-coastline'
import type { HelpTarget } from '@/lib/help-links'
import {
  clockTime,
  coastlineCheckFailures,
  coastlineFailures,
  coastlineStatusLine,
  inlandSkippedLine,
  type OfflineCoastlineStatus,
} from '@/lib/offline-coastline'

interface OfflineCoastlineSectionProps {
  draft: RegularSettingsDraft
  onChange: (patch: Partial<RegularSettingsDraft>) => void
  /** Opens the in-app help, for the link to the Overpass how-to. */
  onOpenHelp: (target: HelpTarget) => void
}

const OVERPASS_HELP_TARGET: HelpTarget = { page: 'how-to/run-your-own-overpass-server' }

const plural = (n: number, one: string, many: string) => `${n} ${n === 1 ? one : many}`

function retryTime(iso: string | undefined): string | null {
  if (!iso) return null
  const t = new Date(iso)
  if (Number.isNaN(t.getTime()) || t.getTime() <= Date.now()) return null
  return clockTime(iso)
}

function Failures({ status }: { status: OfflineCoastlineStatus }) {
  const failures = coastlineFailures(status)
  const checks = coastlineCheckFailures(status)
  if (failures.length === 0 && checks.length === 0) return null
  const list = (items: typeof failures) => (
    <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
      {items.map((f) => {
        const retry = retryTime(f.nextAttemptAt)
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
  const { status, loading, error, update, clearCoverage } = useOfflineCoastline()
  const [actionError, setActionError] = useState<string | null>(null)
  const [updating, setUpdating] = useState(false)
  const [confirmClear, setConfirmClear] = useState(false)
  const [clearing, setClearing] = useState(false)

  const unsaved = status !== undefined && status.enabled !== draft.offlineCoastlineEnabled
  const hasEdits = (status?.squares_with_edits ?? 0) > 0
  const hasSquares = (status?.squares.some((q) => q.state !== 'pending')) ?? false

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
                Downloads OpenStreetMap coastline and reefs for every 1° square within 200 nm of the boat, one square at a
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
            <p className="text-sm font-medium">{coastlineStatusLine(status)}</p>
          ) : null}
          {status && inlandSkippedLine(status) && (
            <p className="text-xs text-muted-foreground" data-testid="offline-coastline-inland">
              {inlandSkippedLine(status)}
            </p>
          )}
          {unsaved && <p className="text-xs text-muted-foreground">Save settings to apply the switch.</p>}
        </div>
        {actionError && <p className="text-sm text-destructive" role="alert">{actionError}</p>}
      </FormSection>

      {status && (status.squares.length > 0 || (status.enabled && status.position)) && (
        <FormSection
          title="Coverage"
          description="Each square is 1° of latitude by 1° of longitude, named by its south-west corner. Shaded squares are held, and stronger shading means a more recent download. Squares drawn as an outline only are within the ring but not downloaded yet. Red squares failed to download. A heavy outline means edits since the download. The dashed ring is 200 nm around the boat."
        >
          <div className="h-72 overflow-hidden rounded-md border border-border" data-testid="offline-coastline-map">
            <OfflineCoastlineMap status={status} />
          </div>
          <Failures status={status} />
        </FormSection>
      )}

      <ConfirmDelete
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title="Clear coverage?"
        description="This deletes every downloaded square. Mate cannot judge shelter until squares are downloaded again. With the offline coastline on, the squares within 200 nm of the boat start downloading again straight away; turn it off first to keep them gone."
        confirmLabel="Clear coverage"
        onConfirm={() => void handleClear()}
        deleting={clearing}
      />
    </SettingsLayout>
  )
}
