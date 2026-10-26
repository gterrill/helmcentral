import { useRef, useState } from 'react'
import { toast } from 'sonner'

import type { DistanceUnits } from '@/config/app-config'
import { CruiseEditPanel } from '@/components/passages/cruise-edit-panel'
import { MergePassagesButton } from '@/components/passages/merge-passages-button'
import { NameCruiseDialog } from '@/components/passages/name-cruise-dialog'
import { passageColour } from '@/components/passages/passage-map'
import { PassageExportMenu } from '@/components/passages/passage-export-menu'
import { PassagePlans } from '@/components/passages/passage-plans'
import { PassageEditPanel } from '@/components/passages/passage-edit-panel'
import { CruiseStatsPanel, PassageStatsStrip } from '@/components/passages/passage-stats'
import { ReplayPanel, type ReplayCursor } from '@/components/passages/replay-panel'
import { DetailsLayout, FormSection, Page } from '@/components/patterns'
import { Skeleton } from '@/components/ui/skeleton'
import { usePassageLegs } from '@/hooks/use-passage-legs'
import { useCruiseDetails, usePassageDetail, usePassageDetails } from '@/hooks/use-passages'
import { combinedTitle } from '@/lib/passage-export'
import { legsOf, type PassageLeg } from '@/lib/passage-legs'
import { passagesApi } from '@/lib/passages-api'
import {
  fmtDuration,
  fmtNM,
  formatLocalDate,
  passageHeading,
  riseLabelTime,
  passageRoute,
  type CruiseRecord,
  type PassageDetail,
  type PassageSummary,
  type WaterRise,
  type WaterRiseLabel,
} from '@/lib/passages'

export type PassageDetailsMode =
  | { kind: 'passage'; id: string }
  | { kind: 'cruise'; id: string }
  | { kind: 'combined'; ids: string[] }

export interface PassageDetailsPageProps {
  units: DistanceUnits
  mode: PassageDetailsMode
  canWrite: boolean
  isDarkTheme: boolean
  /** The visible passages, newest first, for neighbours and names. */
  list: PassageSummary[]
  cruises: CruiseRecord[]
  onBack: () => void
  onOpenPassage: (id: string) => void
  onOpenCruise: (id: string) => void
}

function ErrorLine({ message }: { message: string }) {
  return <p role="alert" className="rounded-md border border-border bg-card p-3 text-sm text-destructive">{message}</p>
}

function Loading() {
  return (
    <div className="flex flex-col gap-2" data-testid="passage-details-loading">
      <Skeleton className="h-[26rem] w-full" />
      <Skeleton className="h-20 w-full" />
    </div>
  )
}

function firstLon(passages: PassageDetail[]): number {
  for (const p of passages) {
    if (p.start_lon !== null) return p.start_lon
  }
  return 0
}

function PassageList({
  passages, onOpen, legs,
}: { passages: (Parameters<typeof passageHeading>[0] & { id: string })[]; onOpen: (id: string) => void; legs?: readonly PassageLeg[] }) {
  return (
    <ul className="flex flex-col gap-1">
      {passages.map((p) => (
        <li key={p.id}>
          <button
            type="button"
            className="flex w-full min-w-0 items-center gap-2 rounded-sm px-1 py-1.5 text-left text-sm hover:bg-accent focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring"
            onClick={() => onOpen(p.id)}
          >
            {legs && (
              <span aria-hidden="true" className="flex shrink-0 gap-0.5" data-testid="passage-legend">
                {legsOf(legs, p.id).map((l) => (
                  <span key={l.colourIndex} className="h-2 w-4 rounded-sm" style={{ backgroundColor: passageColour(l.colourIndex) }} />
                ))}
              </span>
            )}
            <span className="min-w-0 flex-1 truncate">{passageHeading(p)}</span>
          </button>
        </li>
      ))}
    </ul>
  )
}

export function PassageDetailsPage(props: PassageDetailsPageProps) {
  const { mode } = props
  if (mode.kind === 'passage') return <SinglePassage key={mode.id} {...props} id={mode.id} />
  if (mode.kind === 'cruise') return <CruiseDetails key={mode.id} {...props} id={mode.id} />
  return <CombinedPassages key={mode.ids.join(',')} {...props} ids={mode.ids} />
}

// ── one passage ───────────────────────────────────────────────────────────

function SinglePassage({ id, units, canWrite, isDarkTheme, list, cruises, onBack, onOpenPassage }: PassageDetailsPageProps & { id: string }) {
  const { passage, loading, error } = usePassageDetail(id)
  const cursorRef = useRef<ReplayCursor | null>(null)
  const index = list.findIndex((p) => p.id === id)
  return (
    <Page
      title={passage ? passageHeading(passage) : 'Passage'}
      onBack={onBack}
      backLabel="Back to passages"
    >
      {error ? <ErrorLine message={error} /> : loading && !passage ? <Loading /> : passage && (
        <DetailsLayout
          aside={
            <PassageEditPanel
              key={passage.id}
              passage={passage}
              cruises={cruises}
              olderNeighbour={index >= 0 ? list[index + 1] ?? null : null}
              newerNeighbour={index > 0 ? list[index - 1] : null}
              cursorRef={cursorRef}
              canWrite={canWrite}
              onMerged={(mergedId) => { if (mergedId !== id) onOpenPassage(mergedId) }}
              onHidden={onBack}
            />
          }
        >
          <p className="text-sm text-muted-foreground">{passage.from_name || passage.to_name ? `${passageRoute(passage)} · ` : ''}{formatLocalDate(passage.start, firstLon([passage]))}</p>
          <PassagePlans passageId={passage.id} plans={passage.plannedWithMate} error={passage.plannedWithMateError} canWrite={canWrite} />
          <div className="flex justify-end">
            <PassageExportMenu kind="passage" title={passageHeading(passage)} passages={[passage]} units={units} />
          </div>
          <ReplayPanel passages={[passage]} isDarkTheme={isDarkTheme} cursorRef={cursorRef} />
          <PassageStatsStrip stats={passage.stats} units={units} />
        </DetailsLayout>
      )}
    </Page>
  )
}

// ── a cruise ──────────────────────────────────────────────────────────────

function CruiseDetails({ id, units, canWrite, isDarkTheme, onBack, onOpenPassage }: PassageDetailsPageProps & { id: string }) {
  const { data, loading, error } = useCruiseDetails(id)
  const visible = (data?.passages ?? []).filter((p) => !p.hidden)
  const { passages, error: tracksError } = usePassageDetails(visible.map((p) => p.id))
  const lon = firstLon(passages)
  const { legs } = usePassageLegs(visible)

  const labelRise = async (rise: WaterRise, label: WaterRiseLabel | '') => {
    try {
      await passagesApi.labelWaterRise(id, riseLabelTime(rise), label)
    } catch (err) {
      toast.error('Could not label water', { description: err instanceof Error ? err.message : String(err) })
    }
  }

  return (
    <Page title={data?.cruise.name ?? 'Cruise'} onBack={onBack} backLabel="Back to passages">
      {error ? <ErrorLine message={error} /> : loading && !data ? <Loading /> : data && (
        <DetailsLayout
          aside={
            <>
              <CruiseEditPanel
                key={`${data.cruise.id}:${data.cruise.name}:${data.cruise.notes}`}
                cruise={data.cruise}
                canWrite={canWrite}
                onDeleted={onBack}
              />
              <FormSection title="Passages">
                {visible.length === 0
                  ? <p className="text-sm text-muted-foreground">No passages in this cruise yet.</p>
                  : <PassageList passages={visible} onOpen={onOpenPassage} legs={legs} />}
              </FormSection>
            </>
          }
        >
          {tracksError ? <ErrorLine message={tracksError} /> : (
            <>
              <div className="flex justify-end">
                <PassageExportMenu kind="cruise" title={data.cruise.name} passages={passages} cruiseStats={data.stats} units={units} />
              </div>
              <ReplayPanel passages={passages} isDarkTheme={isDarkTheme} />
            </>
          )}
          <CruiseStatsPanel stats={data.stats} units={units} lon={lon} canWrite={canWrite} onLabelRise={(r, l) => void labelRise(r, l)} />
        </DetailsLayout>
      )}
    </Page>
  )
}

// ── an ad-hoc selection ───────────────────────────────────────────────────

function CombinedPassages({ ids, units, canWrite, isDarkTheme, list, onBack, onOpenPassage, onOpenCruise }: PassageDetailsPageProps & { ids: string[] }) {
  const { passages, loading, error } = usePassageDetails(ids)
  const [saveOpen, setSaveOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const ordered = [...passages].sort((a, b) => Date.parse(a.start) - Date.parse(b.start))
  const { legs } = usePassageLegs(ordered)
  const distance = ordered.reduce((n, p) => n + p.stats.distance_nm, 0)
  const underway = ordered.reduce((n, p) => n + p.stats.underway_seconds, 0)

  const saveAsCruise = async (name: string) => {
    setBusy(true)
    try {
      const cruise = await passagesApi.createCruise(name)
      for (const p of ordered) await passagesApi.patch(p.id, { cruise_id: cruise.id })
      setSaveOpen(false)
      onOpenCruise(cruise.id)
    } catch (err) {
      toast.error('Could not save cruise', { description: err instanceof Error ? err.message : String(err) })
    } finally {
      setBusy(false)
    }
  }

  return (
    <Page
      title={`${ids.length} passages together`}
      onBack={onBack}
      backLabel="Back to passages"
      primaryAction={canWrite ? { label: 'Save as cruise', onClick: () => setSaveOpen(true), disabled: ordered.length === 0 } : undefined}
    >
      {error ? <ErrorLine message={error} /> : loading && passages.length === 0 ? <Loading /> : (
        <DetailsLayout
          aside={
            <FormSection title="Passages" description={`${fmtNM(distance)} in ${fmtDuration(underway)} underway`}>
              <PassageList passages={ordered} onOpen={onOpenPassage} legs={legs} />
            </FormSection>
          }
        >
          <div className="flex flex-wrap justify-end gap-2">
            {canWrite && <MergePassagesButton selected={ordered} all={list} onMerged={onOpenPassage} />}
            <PassageExportMenu kind="combined" title={combinedTitle(ordered)} passages={ordered} units={units} />
          </div>
          <ReplayPanel passages={ordered} isDarkTheme={isDarkTheme} />
        </DetailsLayout>
      )}
      <NameCruiseDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        title="Save as cruise"
        description="The passages move into the new cruise, leaving any cruise they were in."
        confirmLabel="Save cruise"
        busy={busy}
        onConfirm={(name) => void saveAsCruise(name)}
      />
    </Page>
  )
}
