import { useState } from 'react'
import { Download, Plus, RefreshCw } from 'lucide-react'

import { Button, buttonVariants } from '@/components/ui/button'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { MaintenanceLogEntryDialog } from '@/components/inventory/maintenance-log-dialogs'
import { MaintenanceRuleDialog } from '@/components/inventory/maintenance-rule-dialog'
import { MaintenanceStatusBadge } from '@/components/inventory/maintenance-status-badge'
import { cn } from '@/lib/utils'
import {
  copyMaintenanceProfileSchedule,
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
  type MaintenanceLogEntry,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'

// ADR 0138 spec §9: "Item's own page (equipment editor) gets a Maintenance
// block listing its rules and recent log entries with add/edit." Day-to-day
// actions on a rule already due (Complete, Acknowledge) live on the main
// Maintenance list (maintenance-section.tsx), which is where an operator
// actually works through what's due; this block is the item's own record of
// what rules exist and what has been done, plus the two actions that are
// really about THIS item specifically: copying its profile's service
// schedule, and recording a meter replacement.

interface MaintenanceEquipmentBlockProps {
  equipmentId: string
  profileId: string
  hourMeterPath: string
  canWrite?: boolean
}

function todayISO(): string {
  return new Date().toISOString().slice(0, 10)
}

export function MaintenanceEquipmentBlock({ equipmentId, profileId, hourMeterPath, canWrite = true }: MaintenanceEquipmentBlockProps) {
  const { rules, loading: rulesLoading, error: rulesError, refresh: refreshRules } = useMaintenanceRules({ equipment: equipmentId, includeStored: true })
  const { entries, loading: entriesLoading, refresh: refreshEntries } = useMaintenanceLogEntries({ equipment: equipmentId })
  const { resets, refresh: refreshResets } = useHourMeterResets(hourMeterPath ? equipmentId : null)

  const [creatingRule, setCreatingRule] = useState(false)
  const [editingRule, setEditingRule] = useState<MaintenanceRule | null>(null)
  const [creatingEntry, setCreatingEntry] = useState(false)
  const [editingEntry, setEditingEntry] = useState<MaintenanceLogEntry | null>(null)
  const [copyingSchedule, setCopyingSchedule] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)

  const [showMeterForm, setShowMeterForm] = useState(false)
  const [oldReading, setOldReading] = useState('')
  const [newReading, setNewReading] = useState('0')
  const [meterDate, setMeterDate] = useState(todayISO())
  const [meterSaving, setMeterSaving] = useState(false)
  const [meterError, setMeterError] = useState<string | null>(null)

  const handleCopySchedule = async () => {
    setCopyingSchedule(true)
    setCopyError(null)
    try {
      await copyMaintenanceProfileSchedule(equipmentId)
      await refreshRules()
    } catch (err) {
      setCopyError(err instanceof Error ? err.message : String(err))
    } finally {
      setCopyingSchedule(false)
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

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-2">
        <p className="text-sm text-muted-foreground">
          {rulesLoading ? 'Loading...' : `${rules.length} rule${rules.length === 1 ? '' : 's'}`}
        </p>
        {canWrite && (
          <div className="flex gap-2">
            {profileId && (
              <Button type="button" variant="outline" size="sm" className="gap-1.5" disabled={copyingSchedule} onClick={() => { void handleCopySchedule() }}>
                <RefreshCw className="h-3.5 w-3.5" aria-hidden="true" />
                {copyingSchedule ? 'Copying...' : 'Use profile schedule'}
              </Button>
            )}
            <Button type="button" variant="outline" size="sm" className="gap-1.5" onClick={() => setCreatingRule(true)}>
              <Plus className="h-3.5 w-3.5" aria-hidden="true" />
              Add rule
            </Button>
          </div>
        )}
      </div>
      {copyError && <p role="alert" className="text-sm text-destructive">{copyError}</p>}
      {rulesError && <p role="alert" className="text-sm text-destructive">{rulesError}</p>}

      <div className="flex flex-col gap-2">
        {rules.length === 0 && !rulesLoading && (
          <p className="text-sm text-muted-foreground">No maintenance rules for this item yet.</p>
        )}
        {rules.map((rule) => (
          <button
            key={rule.id}
            type="button"
            onClick={() => setEditingRule(rule)}
            className="flex items-center justify-between gap-2 rounded-md border border-border px-3 py-2 text-left hover:bg-muted/40"
          >
            <div className="min-w-0">
              <p className="truncate text-sm font-medium">{rule.description}</p>
              <p className="truncate text-xs text-muted-foreground">
                {rule.last_done_at ? `Last done ${rule.last_done_at}` : 'Never recorded'}
              </p>
            </div>
            <MaintenanceStatusBadge status={rule.status} />
          </button>
        ))}
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

      <MaintenanceLogEntryDialog
        entry={creatingEntry ? undefined : editingEntry}
        equipmentId={equipmentId}
        open={creatingEntry || editingEntry !== null}
        onCancel={() => { setCreatingEntry(false); setEditingEntry(null); void refreshEntries() }}
        onSave={async (input) => {
          const saved = editingEntry ? await updateMaintenanceLogEntry(editingEntry.id, input) : await createMaintenanceLogEntry(input)
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
