import { useMemo, useState } from 'react'
import { BookOpen, CheckCircle2, Download, Plus, Wrench } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button, buttonVariants } from '@/components/ui/button'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { MaintenanceAckDialog, MaintenanceLastDoneDialog } from '@/components/inventory/maintenance-quick-dialogs'
import { MaintenanceCompleteDialog } from '@/components/inventory/maintenance-log-dialogs'
import { MaintenanceRuleDialog } from '@/components/inventory/maintenance-rule-dialog'
import { MaintenanceStatusBadge } from '@/components/inventory/maintenance-status-badge'
import { EQUIPMENT_SYSTEMS, EQUIPMENT_SYSTEM_LABELS, type EquipmentSystem } from '@/hooks/use-inventory'
import {
  acknowledgeMaintenanceRule,
  completeMaintenanceRule,
  createMaintenanceRule,
  deleteMaintenanceRule,
  hoursAsOfLabel,
  maintenanceLogExportURL,
  setMaintenanceRuleLastDone,
  updateMaintenanceRule,
  useMaintenanceRules,
  MAINTENANCE_STATUS_ORDER,
  type MaintenanceRule,
} from '@/hooks/use-maintenance'
import { documentViewerHref } from '@/lib/document-citation'

// ADR 0138: the Maintenance section - a plain sortable list grouped by
// status (spec §9), NOT kanban: recurring work has no "column" it belongs
// in permanently, it just moves between the same six states as time and
// hours pass. Follows equipment-index.tsx's own shell (a toolbar above a
// bordered ui/table) since that is this app's one other plain inventory
// index - grouping here is by status instead of by system, and sorting
// within a group puts an acknowledged rule after the unacknowledged ones
// (spec §5), both purely client-side over whatever flat list the hook
// returns, the same way equipment-index.tsx's own system grouping is.

const ALL_VALUE = '__all__'

interface MaintenanceSectionProps {
  onOpenEquipment: (id: string) => void
  canWrite?: boolean
}

function remainingLabel(rule: MaintenanceRule): string {
  const parts: string[] = []
  if (rule.remaining_hours != null) parts.push(`${Math.round(rule.remaining_hours)} h`)
  if (rule.remaining_days != null) parts.push(`${rule.remaining_days} d`)
  return parts.length > 0 ? parts.join(' / ') : '--'
}

function groupLabel(system: string): string {
  if (system === '') return 'Certificates and expiries'
  return EQUIPMENT_SYSTEM_LABELS[system as EquipmentSystem] ?? system
}

export function MaintenanceSection({ onOpenEquipment, canWrite = true }: MaintenanceSectionProps) {
  const [system, setSystem] = useState<EquipmentSystem | ''>('')
  const { rules, loading, error, refresh } = useMaintenanceRules({ system })

  const [creatingRule, setCreatingRule] = useState(false)
  const [editingRule, setEditingRule] = useState<MaintenanceRule | null>(null)
  const [completingRule, setCompletingRule] = useState<MaintenanceRule | null>(null)
  const [acknowledgingRule, setAcknowledgingRule] = useState<MaintenanceRule | null>(null)
  const [settingLastDoneRule, setSettingLastDoneRule] = useState<MaintenanceRule | null>(null)

  // Grouped by status in the spec's own display order, acknowledged rules
  // sorted after unacknowledged ones within each group, otherwise stable
  // (system, then description) - the API itself makes no promise about
  // ordering beyond that (ListMaintenanceRules' own doc comment,
  // backend/maintenance_store.go), so every ordering decision an operator
  // actually sees is made here.
  const groups = useMemo(() => {
    return MAINTENANCE_STATUS_ORDER
      .map((status) => ({
        status,
        rules: rules
          .filter((r) => r.status === status)
          .slice()
          .sort((a, b) => {
            if (a.acknowledged !== b.acknowledged) return a.acknowledged ? 1 : -1
            return a.description.localeCompare(b.description)
          }),
      }))
      .filter((g) => g.rules.length > 0)
  }, [rules])

  return (
    <div className="flex h-full min-h-0 flex-col gap-4">
      <div className="flex flex-wrap items-end gap-2">
        <Select value={system || ALL_VALUE} onValueChange={(value) => setSystem(value === ALL_VALUE ? '' : (value as EquipmentSystem))}>
          <SelectTrigger aria-label="Filter by system" className="h-9 w-auto min-w-32">
            <SelectValue>{(value: string) => (value === ALL_VALUE ? 'All systems' : EQUIPMENT_SYSTEM_LABELS[value as EquipmentSystem])}</SelectValue>
          </SelectTrigger>
          <SelectPopup>
            <SelectItem value={ALL_VALUE}>All systems</SelectItem>
            {EQUIPMENT_SYSTEMS.map((sys) => (
              <SelectItem key={sys} value={sys}>{EQUIPMENT_SYSTEM_LABELS[sys]}</SelectItem>
            ))}
          </SelectPopup>
        </Select>

        <div className="ml-auto flex items-center gap-2">
          <a
            href={maintenanceLogExportURL()}
            download
            className={cn(buttonVariants({ variant: 'outline', size: 'sm' }), 'gap-2')}
          >
            <Download className="h-4 w-4" aria-hidden="true" />
            Export CSV
          </a>
          {canWrite && (
            <Button type="button" size="sm" className="gap-2" onClick={() => setCreatingRule(true)}>
              <Plus className="h-4 w-4" aria-hidden="true" />
              New rule
            </Button>
          )}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto rounded-md border border-border bg-card">
        {error ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
            <p role="alert" className="text-sm text-destructive">{error}</p>
            <Button type="button" variant="outline" size="sm" onClick={() => { void refresh() }}>Retry</Button>
          </div>
        ) : !loading && groups.length === 0 ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 px-6 py-16 text-center">
            <Wrench className="h-8 w-8 text-muted-foreground" aria-hidden="true" />
            <p className="max-w-md text-sm text-muted-foreground">
              No maintenance rules yet. Add one by hand, or open an item with a profile and use
              its service schedule.
            </p>
            {canWrite && (
              <Button type="button" onClick={() => setCreatingRule(true)} className="mt-2 gap-2">
                <Plus className="h-4 w-4" aria-hidden="true" />
                New rule
              </Button>
            )}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Item / rule</TableHead>
                <TableHead>Remaining</TableHead>
                <TableHead>Last done</TableHead>
                <TableHead />
                <TableHead className="text-right">Action</TableHead>
              </TableRow>
            </TableHeader>
            {loading ? (
              <TableBody>
                <TableRow>
                  <TableCell colSpan={5} className="py-6 text-center text-sm text-muted-foreground">Loading...</TableCell>
                </TableRow>
              </TableBody>
            ) : (
              groups.map((group) => (
                <TableBody key={group.status}>
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={5} className="bg-muted/40 py-1.5">
                      <MaintenanceStatusBadge status={group.status} />
                    </TableCell>
                  </TableRow>
                  {group.rules.map((rule) => (
                    <TableRow key={rule.id}>
                      <TableCell className="max-w-0">
                        <div className="flex min-w-0 flex-col">
                          {rule.equipment_id ? (
                            <button
                              type="button"
                              onClick={() => onOpenEquipment(rule.equipment_id!)}
                              className="truncate text-left text-xs font-medium text-muted-foreground hover:underline"
                            >
                              {rule.equipment_name}
                            </button>
                          ) : (
                            <span className="truncate text-xs font-medium text-muted-foreground">{groupLabel('')}</span>
                          )}
                          <button
                            type="button"
                            onClick={() => setEditingRule(rule)}
                            className="truncate text-left font-medium hover:underline"
                          >
                            {rule.description}
                          </button>
                          {rule.hours_unknown && (
                            <span className="truncate text-[10px] text-muted-foreground">
                              {rule.has_hour_meter_path ? 'Hours unknown' : 'No hour meter bound'}
                            </span>
                          )}
                          {/* Only for a rule whose own status actually reads
                              the hours axis - an hour meter's age is not
                              interesting on a purely calendar-based rule
                              just because its item happens to have one.
                              2026-09-27 amendment: purely informational,
                              never a staleness warning - a live reading
                              never goes stale (an hour meter only has a
                              reading while its engine runs). */}
                          {!rule.hours_unknown && rule.interval_hours != null && hoursAsOfLabel(rule.hours_as_of) && (
                            <span className="truncate text-[10px] text-muted-foreground">
                              {hoursAsOfLabel(rule.hours_as_of)}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="tabular-nums text-muted-foreground">{remainingLabel(rule)}</TableCell>
                      <TableCell className="text-muted-foreground">{rule.last_done_at || '--'}</TableCell>
                      <TableCell>
                        <div className="flex items-center gap-1.5">
                          {rule.acknowledged && (
                            <Badge variant="outline" className="gap-1 text-[10px]" title={rule.ack_reason}>
                              Ack
                            </Badge>
                          )}
                          {rule.procedure_note_id && (
                            <a
                              href={documentViewerHref(rule.procedure_note_id)}
                              aria-label="Open procedure note"
                              className="text-muted-foreground hover:text-primary"
                            >
                              <BookOpen className="h-4 w-4" aria-hidden="true" />
                            </a>
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        {canWrite && (
                          <div className="flex justify-end gap-1">
                            {rule.status === 'never_recorded' && (
                              <Button type="button" variant="outline" size="sm" onClick={() => setSettingLastDoneRule(rule)}>
                                Set last done
                              </Button>
                            )}
                            {(rule.status === 'overdue' || rule.status === 'due_soon') && (
                              <Button type="button" variant="ghost" size="sm" onClick={() => setAcknowledgingRule(rule)}>
                                Ack
                              </Button>
                            )}
                            <Button type="button" size="sm" className="gap-1.5" onClick={() => setCompletingRule(rule)}>
                              <CheckCircle2 className="h-3.5 w-3.5" aria-hidden="true" />
                              Complete
                            </Button>
                          </div>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              ))
            )}
          </Table>
        )}
      </div>

      <MaintenanceRuleDialog
        rule={creatingRule ? undefined : editingRule}
        open={creatingRule || editingRule !== null}
        onCancel={() => { setCreatingRule(false); setEditingRule(null) }}
        onSave={async (input) => {
          const saved = editingRule ? await updateMaintenanceRule(editingRule.id, input) : await createMaintenanceRule(input)
          await refresh()
          setCreatingRule(false)
          setEditingRule(null)
          return saved
        }}
        onDelete={async (id) => {
          await deleteMaintenanceRule(id)
          await refresh()
          setEditingRule(null)
        }}
        onRuleChanged={() => { void refresh() }}
      />

      <MaintenanceCompleteDialog
        rule={completingRule}
        onCancel={() => { setCompletingRule(null); void refresh() }}
        onComplete={async (input) => {
          const result = await completeMaintenanceRule(completingRule!.id, input)
          await refresh()
          return result
        }}
      />

      <MaintenanceAckDialog
        rule={acknowledgingRule}
        onCancel={() => setAcknowledgingRule(null)}
        onConfirm={async (reason) => {
          await acknowledgeMaintenanceRule(acknowledgingRule!.id, reason)
          await refresh()
          setAcknowledgingRule(null)
        }}
      />

      <MaintenanceLastDoneDialog
        rule={settingLastDoneRule}
        onCancel={() => setSettingLastDoneRule(null)}
        onConfirm={async (lastDoneAt, lastDoneHours) => {
          await setMaintenanceRuleLastDone(settingLastDoneRule!.id, lastDoneAt, lastDoneHours)
          await refresh()
          setSettingLastDoneRule(null)
        }}
      />
    </div>
  )
}
