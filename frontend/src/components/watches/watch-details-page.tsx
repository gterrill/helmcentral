import { useCallback, useEffect, useRef, useState } from 'react'
import { Play, Trash2 } from 'lucide-react'

import { WatchNotifySection } from '@/components/watches/watch-notify-section'
import { WatchRunCard, WatchRunList } from '@/components/watches/watch-run-list'
import { schedulePreviewBlocksSave, WatchSchedulePicker } from '@/components/watches/watch-schedule-picker'
import { ConfirmDelete, DetailsLayout, FormSection, Page, RefreshFailedNote, SaveBar } from '@/components/patterns'
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from '@/components/ui/breadcrumb'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldDescription, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import { useNavigationGuard } from '@/hooks/use-navigation-guard'
import { useEnabledNotifyChannels, useLinkedWatchRun, useSchedulePreview, useWatchDetail, watchesApi } from '@/hooks/use-watches'
import type { AppLocation } from '@/lib/app-location'
import {
  draftOf,
  hasDraftErrors,
  MAX_LOOK_OUT_FOR,
  MAX_WATCH_NAME,
  newWatchDraft,
  oneOffTimeLabel,
  runTimeLabel,
  sameDraft,
  validateWatchDraft,
  WATCH_SECTIONS,
  type WatchDraft,
  type WatchDraftErrors,
  type WatchRecord,
  type WatchRun,
  type WatchTemplate,
  type WatchThresholds,
} from '@/lib/watches'

// ADR 0227: one watch. The settings are one draft saved through the Save bar;
// the run history sits beneath them. A scheduled watch is edited here, a
// one-off watch (started from a Mate chat) is read-only and can be deleted.

/** How long a run a link points at stays marked. */
export const LINKED_RUN_HIGHLIGHT_MS = 2500

const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err))

export type WatchEditorMode =
  | { kind: 'existing'; id: string }
  | { kind: 'new'; template: WatchTemplate | null }

export interface WatchDetailsPageProps {
  mode: WatchEditorMode
  /** A run a link points at: shown at the top of the history, scrolled to and highlighted for a moment. */
  linkedRunId?: string | null
  canWrite: boolean
  /** The breadcrumb's Watches: up to the index. */
  onOpenIndex: () => void
  /** A new watch was saved: show it by its id. */
  onCreated: (id: string) => void
  /** A delete that already succeeded: the page no longer has a record. */
  onDeleted: () => void
  /** Opens Mate on a new conversation about one run. */
  onAskMate?: (question: string, options: { newConversation: true; watchRun: { id: string; name: string } }) => void
}

function leavesEditor(mode: WatchEditorMode, target: AppLocation): boolean {
  if (mode.kind === 'new') return true
  return !(target.panel === 'watches' && target.route.view === 'details' && target.route.id === mode.id)
}

/** A number field that lets a decimal be typed ("0.", "2.5") without the text jumping under the cursor. */
function NumberField({
  id,
  label,
  hint,
  value,
  error,
  step,
  disabled,
  onChange,
}: {
  id: string
  label: string
  hint?: string
  value: number
  error?: string
  step?: string
  disabled?: boolean
  onChange: (n: number) => void
}) {
  const [text, setText] = useState(Number.isFinite(value) ? String(value) : '')
  const parsed = text.trim() === '' ? Number.NaN : Number(text)
  if (!Object.is(parsed, value) && !(Number.isNaN(parsed) && Number.isNaN(value))) {
    setText(Number.isFinite(value) ? String(value) : '')
  }
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <Input
        id={id}
        type="number"
        inputMode="decimal"
        step={step}
        className="max-w-40 tabular-nums"
        disabled={disabled}
        aria-invalid={error ? true : undefined}
        value={text}
        onChange={(e) => {
          setText(e.target.value)
          onChange(e.target.value.trim() === '' ? Number.NaN : Number(e.target.value))
        }}
      />
      {error ? <FieldError>{error}</FieldError> : hint ? <FieldDescription>{hint}</FieldDescription> : null}
    </Field>
  )
}

interface ThresholdSpec {
  key: keyof WatchThresholds
  label: string
  hint: string
  step: string
}

const THRESHOLDS: Partial<Record<(typeof WATCH_SECTIONS)[number]['id'], ThresholdSpec>> = {
  alarms: { key: 'alarms_raised', label: 'Alarms raised that count', hint: 'Since the last run. 1 means any alarm.', step: '1' },
  instruments: { key: 'instrument_issues', label: 'Sensor problems that count', hint: 'Frozen, out of range or silent sensors.', step: '1' },
  tanks: { key: 'tank_change_points', label: 'Tank change that counts (points)', hint: 'Percentage points since the last run.', step: '1' },
  weather: { key: 'pressure_fall_hpa', label: 'Pressure fall that counts (hPa)', hint: 'Fall over 3 hours.', step: '0.5' },
}

const SECTION_NOTES: Partial<Record<(typeof WATCH_SECTIONS)[number]['id'], string>> = {
  power: 'A finding when the house bank drains much faster overnight than it usually does.',
  anchor: 'A finding when the boat is outside the anchor watch radius.',
}

function oneOffStatus(w: WatchRecord): string {
  if (w.running) return w.ends_at ? `Running until ${oneOffTimeLabel(w, 'ends')}` : 'Running'
  return w.ends_at ? `Ended ${oneOffTimeLabel(w, 'ends')}` : 'Finished'
}

export function WatchDetailsPage({ mode, linkedRunId = null, canWrite, onOpenIndex, onCreated, onDeleted, onAskMate }: WatchDetailsPageProps) {
  const id = mode.kind === 'existing' ? mode.id : null
  const detail = useWatchDetail(id)
  const record = detail.watch
  const oneOff = record?.kind === 'one_off'
  const channels = useEnabledNotifyChannels()

  // A new watch has no channels chosen (notify is null) until the operator picks
  // some: the picker shows the channels that are on, and the request leaves the
  // field out so the server's default applies. The channels arriving later
  // therefore never change the baseline, and never look like an edit.
  const baseline: WatchDraft | null = mode.kind === 'new' ? newWatchDraft(mode.template) : record ? draftOf(record) : null
  const baselineKey = JSON.stringify(baseline)
  const [seenKey, setSeenKey] = useState(baselineKey)
  const [seenBaseline, setSeenBaseline] = useState<WatchDraft | null>(baseline)
  const [draft, setDraft] = useState<WatchDraft>(baseline ?? newWatchDraft())
  const [changedElsewhere, setChangedElsewhere] = useState(false)
  // The record behind the draft changed: first load, our own save landing, or
  // someone else's change. During render, so no frame shows a stale draft as
  // dirty. Edits are never thrown away: only a draft with none takes the
  // server's version, and one with edits keeps them and says what it has done.
  if (baselineKey !== seenKey) {
    setSeenKey(baselineKey)
    setSeenBaseline(baseline)
    if (baseline) {
      const edited = seenBaseline !== null && !sameDraft(draft, seenBaseline) && !sameDraft(draft, baseline)
      if (edited) setChangedElsewhere(true)
      else setDraft(baseline)
    }
  }

  const linked = useLinkedWatchRun(mode.kind === 'existing' ? mode.id : null, mode.kind === 'existing' ? linkedRunId : null)
  const [highlighted, setHighlighted] = useState(false)
  const linkedRef = useRef<HTMLDivElement | null>(null)
  const linkedId = linked.run?.id ?? null
  // Once the linked run is on screen: bring it into view and mark it for a moment.
  useEffect(() => {
    if (linkedId === null) return
    linkedRef.current?.scrollIntoView?.({ block: 'center' })
    setHighlighted(true)
    const timer = window.setTimeout(() => setHighlighted(false), LINKED_RUN_HIGHLIGHT_MS)
    return () => window.clearTimeout(timer)
  }, [linkedId])

  const [nameTouched, setNameTouched] = useState(false)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [running, setRunning] = useState(false)
  const [runError, setRunError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [deleteError, setDeleteError] = useState<string | null>(null)
  const leftRef = useRef(false)

  const preview = useSchedulePreview(draft.schedule, !oneOff && baseline !== null)
  const errors: WatchDraftErrors = validateWatchDraft(draft)
  const dirty =
    canWrite && !oneOff && baseline !== null && (mode.kind === 'new' || !sameDraft(draft, baseline))
  const saveDisabled = hasDraftErrors(errors) || schedulePreviewBlocksSave(preview)

  const update = (patch: Partial<WatchDraft>) => setDraft((d) => ({ ...d, ...patch }))
  const setThreshold = (key: keyof WatchThresholds, n: number) =>
    setDraft((d) => ({ ...d, thresholds: { ...d.thresholds, [key]: n } }))
  const toggleSection = (section: string, on: boolean) =>
    setDraft((d) => ({
      ...d,
      sections: on
        ? WATCH_SECTIONS.map((s) => s.id as string).filter((s) => s === section || d.sections.includes(s))
        : d.sections.filter((s) => s !== section),
    }))

  const performSave = useCallback(async () => {
    if (saveDisabled) throw new Error('Fix the highlighted fields first.')
    setSaving(true)
    setSaveError(null)
    try {
      if (mode.kind === 'new') {
        const created = await watchesApi.create(draft)
        leftRef.current = true
        onCreated(created.id)
      } else {
        await watchesApi.change(mode.id, draft)
        setChangedElsewhere(false)
      }
    } catch (err) {
      // The server's own message, never an invented one.
      setSaveError(errorMessage(err))
      throw err
    } finally {
      setSaving(false)
    }
  }, [saveDisabled, mode, draft, onCreated])

  useNavigationGuard(dirty
    ? {
        owner: 'watches',
        label: 'Watches',
        blocks: (_from, to) => (!leftRef.current && leavesEditor(mode, to) ? { kind: 'dirty' } : null),
        save: performSave,
      }
    : null)

  const runNow = async () => {
    if (mode.kind !== 'existing') return
    setRunning(true)
    setRunError(null)
    try {
      await watchesApi.runNow(mode.id)
    } catch (err) {
      setRunError(errorMessage(err))
    } finally {
      setRunning(false)
    }
  }

  const confirmDelete = async () => {
    if (mode.kind !== 'existing') return
    setDeleting(true)
    setDeleteError(null)
    try {
      await watchesApi.remove(mode.id)
      leftRef.current = true
      setPendingDelete(false)
      onDeleted()
    } catch (err) {
      setDeleteError(errorMessage(err))
    } finally {
      setDeleting(false)
    }
  }

  const askMate = onAskMate && record
    ? (run: WatchRun) => {
        onAskMate(`What do you make of the ${record.name} report from ${runTimeLabel(run)}?`, {
          newConversation: true,
          watchRun: { id: run.id, name: record.name },
        })
      }
    : undefined

  const title = mode.kind === 'new' ? 'New watch' : record?.name ?? 'Watch'
  const breadcrumb = (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem>
          <BreadcrumbLink href="#" onClick={(e) => { e.preventDefault(); onOpenIndex() }}>Watches</BreadcrumbLink>
        </BreadcrumbItem>
        <BreadcrumbSeparator />
        <BreadcrumbItem className="min-w-0">
          <BreadcrumbPage className="truncate">{title}</BreadcrumbPage>
        </BreadcrumbItem>
      </BreadcrumbList>
    </Breadcrumb>
  )

  if (mode.kind === 'existing' && !record) {
    return (
      <div className="h-full overflow-y-auto p-4">
        <Page title="Watch" breadcrumb={breadcrumb}>
          {detail.loading ? (
            <div className="flex min-h-[240px] items-center justify-center text-sm text-muted-foreground">Loading...</div>
          ) : (
            <div className="flex min-h-[240px] flex-col items-center justify-center gap-3 text-center">
              <p role="alert" className="text-sm text-destructive">{detail.error ?? 'This watch could not be found.'}</p>
              {detail.error && (
                <Button type="button" variant="outline" size="sm" onClick={() => { void detail.refresh() }}>Retry</Button>
              )}
            </div>
          )}
        </Page>
      </div>
    )
  }

  const readOnly = !canWrite || oneOff

  return (
    <div className="h-full overflow-y-auto p-4">
      <Page
        title={title}
        breadcrumb={breadcrumb}
        secondaryActions={
          canWrite && mode.kind === 'existing'
            ? [{
                label: 'Delete watch',
                icon: <Trash2 className="h-4 w-4" aria-hidden="true" />,
                destructive: true,
                onClick: () => { setDeleteError(null); setPendingDelete(true) },
              }]
            : undefined
        }
      >
        {changedElsewhere && dirty && (
          <p role="status" className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-foreground">
            This watch was changed elsewhere. Saving will overwrite those changes.
          </p>
        )}
        {detail.error && record && <RefreshFailedNote reason={detail.error} onRetry={() => { void detail.refresh() }} />}
        <DetailsLayout>
          {oneOff && record ? (
            <FormSection
              title="One-off watch"
              description="Started from a chat with Mate to sample a few readings for a short time. Its settings can't be changed."
            >
              <dl className="grid gap-2 text-sm sm:grid-cols-[10rem_minmax(0,1fr)]">
                <dt className="text-muted-foreground">Name</dt>
                <dd className="min-w-0 truncate text-foreground">{record.name}</dd>
                <dt className="text-muted-foreground">Length</dt>
                <dd className="text-foreground">{record.duration_minutes} {record.duration_minutes === 1 ? 'minute' : 'minutes'}</dd>
                {record.started_at && (
                  <>
                    <dt className="text-muted-foreground">Started</dt>
                    <dd className="text-foreground tabular-nums">{oneOffTimeLabel(record, 'started')}</dd>
                  </>
                )}
                <dt className="text-muted-foreground">Status</dt>
                <dd className="text-foreground">{oneOffStatus(record)}</dd>
              </dl>
            </FormSection>
          ) : (
            <>
              <FormSection title="Watch">
                <Field>
                  <FieldLabel htmlFor="watch-name">Name</FieldLabel>
                  <Input
                    id="watch-name"
                    className="max-w-sm"
                    value={draft.name}
                    maxLength={MAX_WATCH_NAME + 20}
                    disabled={readOnly}
                    aria-invalid={nameTouched && errors.name ? true : undefined}
                    onChange={(e) => { setNameTouched(true); update({ name: e.target.value }) }}
                    onBlur={() => setNameTouched(true)}
                  />
                  {nameTouched && errors.name && <FieldError>{errors.name}</FieldError>}
                </Field>
                <Field orientation="horizontal" className="items-center">
                  <Switch
                    id="watch-enabled"
                    aria-label="Enabled"
                    checked={draft.enabled}
                    disabled={readOnly}
                    onCheckedChange={(on) => update({ enabled: on })}
                  />
                  <FieldLabel htmlFor="watch-enabled">Enabled</FieldLabel>
                </Field>
              </FormSection>

              <FormSection title="Schedule" description="Times are on the boat's clock, worked out from its position.">
                <WatchSchedulePicker
                  value={draft.schedule}
                  onChange={(schedule) => update({ schedule })}
                  preview={preview}
                  disabled={readOnly}
                />
              </FormSection>

              <FormSection
                title="Report sections"
                description="What each report covers, and how much of it counts as a finding."
              >
                <div className="flex flex-col gap-4" role="group" aria-label="Report sections">
                  {WATCH_SECTIONS.map((s) => {
                    const on = draft.sections.includes(s.id)
                    const spec = THRESHOLDS[s.id]
                    return (
                      <div key={s.id} className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start sm:gap-6">
                        <div className="flex min-h-9 items-center gap-2 sm:w-40">
                          <Checkbox
                            id={`watch-section-${s.id}`}
                            checked={on}
                            disabled={readOnly}
                            onCheckedChange={(checked) => toggleSection(s.id, checked)}
                          />
                          <label htmlFor={`watch-section-${s.id}`} className="text-sm font-medium text-foreground">{s.label}</label>
                        </div>
                        {spec ? (
                          <NumberField
                            id={`watch-threshold-${spec.key}`}
                            label={spec.label}
                            hint={spec.hint}
                            step={spec.step}
                            value={draft.thresholds[spec.key]}
                            error={errors[spec.key]}
                            disabled={readOnly}
                            onChange={(n) => setThreshold(spec.key, n)}
                          />
                        ) : (
                          <p className="min-w-0 text-xs text-muted-foreground sm:pt-2.5">{SECTION_NOTES[s.id]}</p>
                        )}
                      </div>
                    )
                  })}
                  {errors.sections && <FieldError>{errors.sections}</FieldError>}
                </div>
              </FormSection>

              <WatchNotifySection
                notify={draft.notify ?? channels.on}
                send={draft.send}
                channelsOn={channels.on}
                channelsLoaded={channels.loaded}
                channelsError={channels.error}
                disabled={readOnly}
                onNotifyChange={(notify) => update({ notify })}
                onSendChange={(send) => update({ send })}
              />

              <FormSection title="What to look out for">
                <Field>
                  <FieldLabel htmlFor="watch-look-out-for" className="sr-only">What to look out for</FieldLabel>
                  <Textarea
                    id="watch-look-out-for"
                    rows={3}
                    value={draft.look_out_for}
                    disabled={readOnly}
                    aria-invalid={errors.look_out_for ? true : undefined}
                    onChange={(e) => update({ look_out_for: e.target.value })}
                  />
                  {errors.look_out_for ? (
                    <FieldError>{errors.look_out_for}</FieldError>
                  ) : (
                    <FieldDescription>
                      For Mate&apos;s review, in your own words. For example: the watermaker runs overnight; tell me if the fridge draw climbs.
                    </FieldDescription>
                  )}
                  <p className="text-xs tabular-nums text-muted-foreground">{[...draft.look_out_for.trim()].length} / {MAX_LOOK_OUT_FOR}</p>
                </Field>
              </FormSection>
            </>
          )}

          {mode.kind === 'existing' && (
            <FormSection
              title="Run history"
              description="Newest first. Runs with a finding are kept for good."
              action={
                canWrite && !oneOff ? (
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={running || dirty}
                    title={dirty ? 'Save your changes first' : undefined}
                    onClick={() => { void runNow() }}
                  >
                    <Play className="h-4 w-4" data-icon="inline-start" aria-hidden="true" />
                    {running ? 'Running...' : 'Run now'}
                  </Button>
                ) : undefined
              }
            >
              {running && <p role="status" className="text-xs text-muted-foreground">Building the report. Mate&apos;s review can take a minute.</p>}
              {runError && <p role="alert" className="text-sm text-destructive">{runError}</p>}
              {linkedRunId && (
                <div ref={linkedRef} data-testid="watch-linked-run" className="flex flex-col gap-1">
                  {linked.run ? (
                    <>
                      <p className="text-[10px] font-normal uppercase tracking-[0.16em] text-muted-foreground">Linked run</p>
                      <WatchRunCard
                        run={linked.run}
                        canWrite={false}
                        highlighted={highlighted}
                        onAskMate={askMate}
                        onDelete={() => {}}
                      />
                    </>
                  ) : linked.gone ? (
                    <p className="text-sm text-muted-foreground">The linked run is no longer there. It may have been deleted.</p>
                  ) : linked.error ? (
                    <p role="alert" className="text-sm text-destructive">Could not read the linked run: {linked.error}</p>
                  ) : (
                    <p className="text-xs text-muted-foreground">Loading the linked run...</p>
                  )}
                </div>
              )}
              <WatchRunList
                runs={linkedRunId ? detail.runs.filter((r) => r.id !== linkedRunId) : detail.runs}
                hasOlder={detail.hasOlder}
                canWrite={canWrite}
                loadingOlder={detail.loadingOlder}
                olderError={detail.olderError}
                onShowOlder={() => { void detail.showOlder() }}
                onAskMate={askMate}
                onDeleteRun={async (run) => {
                  await watchesApi.removeRun(mode.id, run.id)
                  detail.dropRun(run.id)
                }}
              />
            </FormSection>
          )}
        </DetailsLayout>

        <ConfirmDelete
          open={pendingDelete}
          onOpenChange={(open) => { if (!open) { setPendingDelete(false); setDeleteError(null) } }}
          title={`Delete "${record?.name ?? 'this watch'}"?`}
          description={deleteError ?? (oneOff && record?.running
            ? 'It stops sampling now and nothing goes to the chat. Its history is removed.'
            : 'The watch and all of its runs are removed. This can’t be undone.')}
          confirmLabel="Delete watch"
          deleting={deleting}
          onConfirm={() => { void confirmDelete() }}
        />

        {canWrite && !oneOff && (
          <SaveBar
            dirty={dirty}
            saving={saving}
            error={saveError}
            saveDisabled={saveDisabled}
            saveLabel={mode.kind === 'new' ? 'Create watch' : 'Save'}
            onSave={() => { void performSave().catch(() => {}) }}
            onDiscard={() => {
              setDraft(baseline ?? newWatchDraft())
              setChangedElsewhere(false)
              setSaveError(null)
              setNameTouched(false)
              if (mode.kind === 'new') {
                leftRef.current = true
                onOpenIndex()
              }
            }}
          />
        )}
      </Page>
    </div>
  )
}
