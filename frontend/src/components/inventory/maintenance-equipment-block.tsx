import { useEffect, useRef, useState } from 'react'
import { ChevronRight, Download, Plus } from 'lucide-react'

import { Button, buttonVariants } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { ConfirmDelete } from '@/components/patterns'
import { MaintenanceLogEntryDialog } from '@/components/inventory/maintenance-log-dialogs'
import { MaintenanceRuleDialog } from '@/components/inventory/maintenance-rule-dialog'
import { OverrideMarker } from '@/components/inventory/maintenance-provenance'
import { MaintenanceStatusBadge } from '@/components/inventory/maintenance-status-badge'
import { useEquipmentProfiles } from '@/hooks/use-equipment-profiles'
import { formatAppLocation } from '@/lib/app-location'
import { todayISO } from '@/lib/local-date'
import { cn } from '@/lib/utils'
import {
  createMaintenanceLogEntry,
  createMaintenanceRule,
  deleteMaintenanceLogEntry,
  deleteMaintenanceRule,
  maintenanceLogExportURL,
  recordHourMeterReset,
  updateMaintenanceLogEntry,
  updateMaintenanceRule,
  useHourMeterResets,
  useMaintenanceLogEntries,
  useMaintenanceRules,
  isProfileJob,
  type MaintenanceLogEntry,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'

// ADR 0138 spec §9: "Item's own page (equipment editor) gets a Maintenance
// block listing its rules and recent log entries with add/edit." Day-to-day
// actions on a rule already due (Complete, Acknowledge) live on the main
// Maintenance list (maintenance-section.tsx), which is where an operator
// actually works through what's due; this block is the item's own record of
// what rules exist and what has been done, plus recording a meter
// replacement.
//
// ADR 0148: the schedule is grouped by where each rule comes from. The
// item's profile jobs are live (the profile owns them; this item can only
// override values), hand rules are the item's own, and jobs that have left
// the profile are kept here with their history until the operator deletes
// them. An item whose profile can't be read shows an explicit error instead
// of any profile job.

interface MaintenanceEquipmentBlockProps {
  equipmentId: string
  hourMeterPath: string
  /** The item's saved profile id: when it changes the schedule is re-read,
   * since its profile jobs came from the old one. */
  scheduleKey?: string
  canWrite?: boolean
}

export function MaintenanceEquipmentBlock({ equipmentId, hourMeterPath, scheduleKey = '', canWrite = true }: MaintenanceEquipmentBlockProps) {
  const { rules, removed, scheduleErrors, loading: rulesLoading, error: rulesError, refresh: refreshRules } = useMaintenanceRules({ equipment: equipmentId, includeStored: true })
  const { profiles } = useEquipmentProfiles(true)
  const { entries, loading: entriesLoading, refresh: refreshEntries } = useMaintenanceLogEntries({ equipment: equipmentId })
  const { resets, refresh: refreshResets } = useHourMeterResets(hourMeterPath ? equipmentId : null)

  const seenScheduleKey = useRef(scheduleKey)
  useEffect(() => {
    if (seenScheduleKey.current === scheduleKey) return
    seenScheduleKey.current = scheduleKey
    void refreshRules()
  }, [scheduleKey, refreshRules])

  const [creatingRule, setCreatingRule] = useState(false)
  const [editingRule, setEditingRule] = useState<MaintenanceRule | null>(null)
  const [creatingEntry, setCreatingEntry] = useState(false)
  const [editingEntry, setEditingEntry] = useState<MaintenanceLogEntry | null>(null)
  const [showRemoved, setShowRemoved] = useState(false)
  const [deletingRemoved, setDeletingRemoved] = useState<MaintenanceRule | null>(null)
  const [removedBusy, setRemovedBusy] = useState(false)
  const [removedError, setRemovedError] = useState<string | null>(null)

  const [showMeterForm, setShowMeterForm] = useState(false)
  const [oldReading, setOldReading] = useState('')
  const [newReading, setNewReading] = useState('0')
  const [meterDate, setMeterDate] = useState(todayISO())
  const [meterSaving, setMeterSaving] = useState(false)
  const [meterError, setMeterError] = useState<string | null>(null)

  const profileJobs = rules.filter(isProfileJob)
  const handRules = rules.filter((rule) => !isProfileJob(rule))
  const scheduleError = scheduleErrors.find((e) => e.equipment_id === equipmentId) ?? null
  const profileIdInUse = profileJobs[0]?.profile_id ?? ''
  const profileName = profiles.find((p) => p.id === profileIdInUse)?.name ?? profileIdInUse

  const handleDeleteRemoved = async () => {
    if (!deletingRemoved) return
    setRemovedBusy(true)
    setRemovedError(null)
    try {
      await deleteMaintenanceRule(deletingRemoved.id)
      setDeletingRemoved(null)
      await refreshRules()
    } catch (err) {
      setRemovedError(err instanceof Error ? err.message : String(err))
    } finally {
      setRemovedBusy(false)
    }
  }

  const handleRecordMeterReset = async () => {
    setMeterSaving(true)
    setMeterError(null)
    try {
      await recordHourMeterReset(equipmentId, Number(oldReading), Number(newReading), meterDate)
      await refreshResets()
      setShowMeterForm(false)
      setOldReading('')
      setNewReading('0')
    } catch (err) {
      setMeterError(err instanceof Error ? err.message : String(err))
    } finally {
      setMeterSaving(false)
    }
  }

  const ruleRow = (rule: MaintenanceRule) => {
    const notSet = rule.status === 'interval_not_set'
    return (
      <div
        key={rule.id}
        className={cn(
          'flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2 hover:bg-muted/40',
          rule.not_applicable && 'text-muted-foreground',
        )}
      >
        <button type="button" onClick={() => setEditingRule(rule)} className="min-w-0 flex-1 text-left">
          <span className="flex min-w-0 items-center gap-2">
            <span className={cn('truncate text-sm font-medium', rule.not_applicable && 'font-normal')}>{rule.description}</span>
            <OverrideMarker rule={rule} />
          </span>
          <span className="block truncate text-xs text-muted-foreground">
            {rule.last_done_at ? `Last done ${rule.last_done_at}` : 'Never recorded'}
          </span>
        </button>
        {notSet && canWrite && (
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-label={`Set interval for ${rule.description}`}
            onClick={() => setEditingRule(rule)}
          >
            Set interval
          </Button>
        )}
        <MaintenanceStatusBadge status={rule.status} />
      </div>
    )
  }

  const groupHeading = (text: string) => (
    <h4 className="text-[10px] font-semibold tracking-wider text-muted-foreground uppercase">{text}</h4>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">
          {rulesLoading ? 'Loading...' : `${rules.length} rule${rules.length === 1 ? '' : 's'}`}
        </p>
        {canWrite && (
          <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => setCreatingRule(true)}>
            <Plus className="h-3.5 w-3.5" aria-hidden="true" />
            Add rule
          </Button>
        )}
      </div>
      {hourMeterPath.trim() === '' && rules.some((rule) => rule.interval_hours !== null && !rule.not_applicable) && (
        <p role="status" className="rounded-md border border-amber-500/40 bg-amber-500/10 px-3 py-2 text-sm text-amber-700 dark:text-amber-400">
          Rules counted in hours can't count hours until an hour meter is set above.
        </p>
      )}
      {scheduleError && (
        <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
          <p>
            This item's profile is missing or invalid, so its service jobs can't be shown
            {scheduleError.profile_id ? ` (profile: ${scheduleError.profile_id})` : ''}.
          </p>
          <p className="mt-1 text-xs">{scheduleError.error}</p>
          <p className="mt-1 text-xs">
            Fix or restore it on the{' '}
            <a
              href={formatAppLocation({ panel: 'inventory', inventorySection: 'profiles' }, { firstPageId: null })}
              className="underline"
            >
              Profiles
            </a>{' '}
            page, or pick a different profile for this item.
          </p>
        </div>
      )}
      {rulesError && <p role="alert" className="text-sm text-destructive">{rulesError}</p>}
      {removedError && <p role="alert" className="text-sm text-destructive">{removedError}</p>}

      <div className="flex flex-col gap-4">
        {rules.length === 0 && removed.length === 0 && !rulesLoading && !scheduleError && (
          <p className="text-sm text-muted-foreground">No maintenance rules for this item yet.</p>
        )}
        {profileJobs.length > 0 && (
          <div className="flex flex-col gap-2">
            {groupHeading(`From profile: ${profileName}`)}
            {profileJobs.map(ruleRow)}
          </div>
        )}
        {handRules.length > 0 && (
          <div className="flex flex-col gap-2">
            {profileJobs.length > 0 && groupHeading('Added for this item')}
            {handRules.map(ruleRow)}
          </div>
        )}
        {removed.length > 0 && (
          <div className="flex flex-col gap-2">
            <button
              type="button"
              aria-expanded={showRemoved}
              onClick={() => setShowRemoved((v) => !v)}
              className="flex items-center gap-1 self-start text-[10px] font-semibold tracking-wider text-muted-foreground uppercase hover:text-foreground"
            >
              <ChevronRight className={cn('h-3 w-3 transition-transform', showRemoved && 'rotate-90')} aria-hidden="true" />
              No longer in the profile ({removed.length})
            </button>
            {showRemoved && (
              <>
                <p className="text-xs text-muted-foreground">
                  These jobs are no longer in this item's profile and are never due. Their history is kept until you delete them.
                </p>
                {removed.map((rule) => (
                  <div key={rule.id} className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-muted-foreground">
                    <div className="min-w-0">
                      <p className="truncate text-sm">{rule.description}</p>
                      <p className="truncate text-xs">{rule.last_done_at ? `Last done ${rule.last_done_at}` : 'Never recorded'}</p>
                    </div>
                    {canWrite && (
                      <Button
                        type="button"
                        variant="ghost"
                        size="sm"
                        className="text-destructive"
                        aria-label={`Delete ${rule.description}`}
                        onClick={() => setDeletingRemoved(rule)}
                      >
                        Delete
                      </Button>
                    )}
                  </div>
                ))}
              </>
            )}
          </div>
        )}
      </div>

      {hourMeterPath && (
        <div className="rounded-md border border-border p-3">
          <div className="flex items-center justify-between gap-2">
            <FieldLabel>Hour meter</FieldLabel>
            {canWrite && (
              <Button type="button" variant="ghost" size="sm" onClick={() => setShowMeterForm((v) => !v)}>
                Record meter replacement
              </Button>
            )}
          </div>
          {resets.length > 0 && (
            <p className="mt-1 text-xs text-muted-foreground">
              Last replaced {resets[0].changed_at}: {resets[0].old_reading}h -&gt; {resets[0].new_reading}h
            </p>
          )}
          {showMeterForm && (
            <div className="mt-2 grid grid-cols-3 gap-2">
              <Field>
                <FieldLabel htmlFor="meter-old">Old reading</FieldLabel>
                <Input id="meter-old" type="number" inputMode="decimal" value={oldReading} onChange={(e) => setOldReading(e.target.value)} />
              </Field>
              <Field>
                <FieldLabel htmlFor="meter-new">New reading</FieldLabel>
                <Input id="meter-new" type="number" inputMode="decimal" value={newReading} onChange={(e) => setNewReading(e.target.value)} />
              </Field>
              <Field>
                <FieldLabel htmlFor="meter-date">Date</FieldLabel>
                <Input id="meter-date" type="date" value={meterDate} onChange={(e) => setMeterDate(e.target.value)} />
              </Field>
              <Button type="button" size="sm" className="col-span-3" disabled={meterSaving || oldReading.trim() === ''} onClick={() => { void handleRecordMeterReset() }}>
                {meterSaving ? 'Saving...' : 'Save'}
              </Button>
              {meterError && <p role="alert" className="col-span-3 text-sm text-destructive">{meterError}</p>}
            </div>
          )}
        </div>
      )}

      <div className="flex items-center justify-between gap-2">
        <FieldLabel>Service log</FieldLabel>
        <div className="flex gap-2">
          <a
            href={maintenanceLogExportURL(equipmentId)}
            download
            className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-1.5')}
          >
            <Download className="h-3.5 w-3.5" aria-hidden="true" />
            Export CSV
          </a>
          {canWrite && (
            <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => setCreatingEntry(true)}>
              <Plus className="h-3.5 w-3.5" aria-hidden="true" />
              Add log entry
            </Button>
          )}
        </div>
      </div>
      <div className="flex flex-col gap-2">
        {entries.length === 0 && !entriesLoading && (
          <p className="text-sm text-muted-foreground">No service log entries yet.</p>
        )}
        {entries.slice(0, 10).map((entry) => (
          <button
            key={entry.id}
            type="button"
            onClick={() => setEditingEntry(entry)}
            className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-left hover:bg-muted/40"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{entry.description || entry.kind}</p>
              <p className="truncate text-xs text-muted-foreground">{entry.performed_at} - {entry.kind}</p>
            </div>
            {entry.cost != null && (
              <span className="shrink-0 text-xs tabular-nums text-muted-foreground">{entry.cost} {entry.currency}</span>
            )}
          </button>
        ))}
      </div>

      <MaintenanceRuleDialog
        rule={creatingRule ? undefined : editingRule}
        presetEquipmentId={equipmentId}
        open={creatingRule || editingRule !== null}
        onCancel={() => { setCreatingRule(false); setEditingRule(null) }}
        onSave={async (input) => {
          const saved = editingRule ? await updateMaintenanceRule(editingRule.id, input) : await createMaintenanceRule(input)
          await refreshRules()
          setCreatingRule(false)
          setEditingRule(null)
          return saved
        }}
        onDelete={async (id) => {
          await deleteMaintenanceRule(id)
          await refreshRules()
          setEditingRule(null)
        }}
        onRuleChanged={() => { void refreshRules() }}
      />

      <ConfirmDelete
        open={deletingRemoved !== null}
        onOpenChange={(isOpen) => { if (!isOpen) setDeletingRemoved(null) }}
        title={`Delete "${deletingRemoved?.description ?? ''}"?`}
        description="Its saved settings and service history for this item go with it. This can't be undone."
        deleting={removedBusy}
        onConfirm={() => { void handleDeleteRemoved() }}
      />

      <MaintenanceLogEntryDialog
        entry={creatingEntry ? undefined : editingEntry}
        equipmentId={equipmentId}
        open={creatingEntry || editingEntry !== null}
        onCancel={() => { setCreatingEntry(false); setEditingEntry(null); void refreshEntries() }}
        onSave={async (input, existingId) => {
          const saved = existingId ? await updateMaintenanceLogEntry(existingId, input) : await createMaintenanceLogEntry(input)
          await refreshEntries()
          return saved
        }}
        onDelete={async (id) => {
          await deleteMaintenanceLogEntry(id)
          await refreshEntries()
        }}
      />
    </div>
  )
}
