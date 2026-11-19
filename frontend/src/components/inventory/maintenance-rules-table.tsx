import { useMemo, useState, type ReactNode } from 'react'
import { BookOpen, CheckCircle2 } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { cn } from '@/lib/utils'
import { MaintenanceAckDialog, MaintenanceLastDoneDialog } from '@/components/inventory/maintenance-quick-dialogs'
import { HandHoursDialog } from '@/components/inventory/maintenance-hand-hours-dialog'
import { MaintenanceCompleteDialog } from '@/components/inventory/maintenance-log-dialogs'
import { MaintenanceRuleDialog } from '@/components/inventory/maintenance-rule-dialog'
import { OverrideMarker } from '@/components/inventory/maintenance-provenance'
import { MaintenanceStatusBadge } from '@/components/inventory/maintenance-status-badge'
import { EQUIPMENT_SYSTEM_LABELS, type EquipmentSystem } from '@/hooks/use-inventory'
import {
  acknowledgeMaintenanceRule,
  completeMaintenanceRule,
  createMaintenanceRule,
  deleteMaintenanceRule,
  canLogHandReading,
  hoursAgeLabel,
  recordHandHourReading,
  setMaintenanceRuleLastDone,
  updateMaintenanceRule,
  MAINTENANCE_STATUS_ORDER,
  type MaintenanceRule,
  type MaintenanceStatus,
} from '@/hooks/use-maintenance'
import { useIsMobile } from '@/hooks/use-mobile'
import { useBoatDayKey } from '@/hooks/use-vessel-clock'
import { documentViewerHref } from '@/lib/document-citation'
import { equipmentLabeller, joinEquipmentLabel } from '@/lib/maintenance-equipment-labels'

// ADR 0138, ADR 0293: the rows of service rules, shared by the Service
// schedule (every status) and the Jobs page (overdue and due soon). One
// implementation of the row and of the dialogs behind its buttons, so a rule
// reads and behaves the same wherever the operator meets it.

export interface RuleGroup {
  status: MaintenanceStatus
  rules: MaintenanceRule[]
}

/**
 * Grouped by status in the given order (the spec's display order by default),
 * an acknowledged rule after the unacknowledged ones within each group,
 * otherwise by description. The API makes no promise about ordering beyond
 * that (ListMaintenanceRules' own doc comment, backend/maintenance_store.go),
 * so every ordering decision an operator sees is made here. Empty groups are
 * left out.
 */
export function groupRulesByStatus(
  rules: MaintenanceRule[],
  statuses: MaintenanceStatus[] = MAINTENANCE_STATUS_ORDER,
): RuleGroup[] {
  return statuses
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

// Complete is the one solid button only where the work is due now. A rule with
// no schedule, or no record yet, leads with a setup action instead (Set
// interval, Set last done), so Complete steps back to ghost there.
function completeVariant(status: MaintenanceRule['status']): 'default' | 'outline' | 'ghost' {
  if (status === 'overdue' || status === 'due_soon') return 'default'
  if (status === 'interval_not_set' || status === 'never_recorded') return 'ghost'
  return 'outline'
}

interface MaintenanceRulesTableProps {
  groups: RuleGroup[]
  /** Every rule the list was read from, so two items of the same name stay told apart even when only some are shown. */
  labelRules: MaintenanceRule[]
  loading: boolean
  /** Shown in place of the table: an error, or an empty state. The dialogs are still mounted. */
  fallback?: ReactNode
  canWrite: boolean
  onOpenEquipment: (id: string) => void
  /** The New rule dialog, which the page's own button opens. */
  creating?: boolean
  onCreatingChange?: (creating: boolean) => void
}

export function MaintenanceRulesTable({
  groups,
  labelRules,
  loading,
  fallback,
  canWrite,
  onOpenEquipment,
  creating = false,
  onCreatingChange,
}: MaintenanceRulesTableProps) {
  // Hand-read figures are aged from the boat's day, never the device's.
  const boatDay = useBoatDayKey()
  const isMobile = useIsMobile()

  const [editingRule, setEditingRule] = useState<MaintenanceRule | null>(null)
  const [completingRule, setCompletingRule] = useState<MaintenanceRule | null>(null)
  const [acknowledgingRule, setAcknowledgingRule] = useState<MaintenanceRule | null>(null)
  const [settingLastDoneRule, setSettingLastDoneRule] = useState<MaintenanceRule | null>(null)
  const [loggingReadingRule, setLoggingReadingRule] = useState<MaintenanceRule | null>(null)

  const labelFor = useMemo(() => equipmentLabeller(labelRules), [labelRules])
  // The one-line label for a dialog's wording, only where the list has another
  // item of the same name; otherwise the dialog keeps its own wording.
  const labelOf = (rule: MaintenanceRule | null): string | undefined => {
    if (!rule || !rule.equipment_id) return undefined
    const label = labelFor(rule)
    return label.detail ? joinEquipmentLabel(label) : undefined
  }

  // The equipment line, the service's description and what is worth saying
  // about its hours: the same in a table row and on a phone card.
  const titleBlock = (rule: MaintenanceRule) => (
    <div className="flex min-w-0 flex-col">
      {rule.equipment_id ? (
        <button
          type="button"
          onClick={() => onOpenEquipment(rule.equipment_id!)}
          className="flex min-w-0 items-baseline gap-1 text-left text-xs font-medium text-muted-foreground hover:underline"
        >
          {/* Detail outranks the name: the name gives way
              first (shrink 3 to 1, 3ch floor), then both truncate. */}
          <span className="min-w-[3ch] shrink-[3] truncate">{labelFor(rule).name}</span>
          {labelFor(rule).detail && <span className="min-w-0 shrink truncate">({labelFor(rule).detail})</span>}
        </button>
      ) : (
        <span className="truncate text-xs font-medium text-muted-foreground">{groupLabel('')}</span>
      )}
      <div className="flex min-w-0 items-center gap-2">
        <button
          type="button"
          onClick={() => setEditingRule(rule)}
          className={cn('truncate text-left hover:underline', rule.not_applicable ? 'font-normal' : 'font-medium')}
        >
          {rule.description}
        </button>
        <OverrideMarker rule={rule} />
      </div>
      {rule.hours_unknown && (
        <span className="truncate text-xs text-muted-foreground">
          {rule.has_hour_meter_path ? 'Hours unknown' : 'No hour meter and no reading logged'}
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
      {!rule.hours_unknown && rule.interval_hours != null && hoursAgeLabel(rule, boatDay) && (
        <span className="truncate text-xs text-muted-foreground">
          {hoursAgeLabel(rule, boatDay)}
        </span>
      )}
    </div>
  )

  const marks = (rule: MaintenanceRule) => (
    <div className="flex items-center gap-1.5">
      {rule.acknowledged && (
        <Badge variant="outline" className="gap-1 text-xs" title={rule.ack_reason}>
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
  )

  const actions = (rule: MaintenanceRule) =>
    canWrite && !rule.not_applicable && (
      <div className="flex flex-wrap justify-end gap-1">
        {rule.interval_hours !== null && canLogHandReading(rule) && (
          <Button type="button" variant="ghost" size="sm" onClick={() => setLoggingReadingRule(rule)}>
            Log a reading
          </Button>
        )}
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
        {rule.status === 'interval_not_set' && (
          <Button type="button" variant="outline" size="sm" onClick={() => setEditingRule(rule)}>
            Set interval
          </Button>
        )}
        <Button
          type="button"
          variant={completeVariant(rule.status)}
          size="sm"
          className="gap-1.5"
          onClick={() => setCompletingRule(rule)}
        >
          <CheckCircle2 className="h-3.5 w-3.5" aria-hidden="true" />
          Complete
        </Button>
      </div>
    )

  // A phone has no room for five columns, and the actions are the one column
  // that must not fall off the edge: each service is a card, as the Open jobs
  // are (below the same breakpoint, useIsMobile).
  const cards = (
    <div className="flex flex-col gap-3">
      {loading ? (
        <p className="py-6 text-center text-sm text-muted-foreground">Loading...</p>
      ) : (
        groups.map((group) => (
          <div key={group.status} className="flex flex-col gap-2">
            <MaintenanceStatusBadge status={group.status} />
            {group.rules.map((rule) => (
              <div
                key={rule.id}
                data-slot="service-card"
                className={cn('flex flex-col gap-1.5 rounded-md border border-border bg-card p-3', rule.not_applicable && 'text-muted-foreground')}
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0 flex-1">{titleBlock(rule)}</div>
                  {marks(rule)}
                </div>
                <div className="flex items-baseline justify-between gap-2 text-xs">
                  <span className="text-muted-foreground">Remaining</span>
                  <span className="tabular-nums text-foreground">{remainingLabel(rule)}</span>
                </div>
                <div className="flex items-baseline justify-between gap-2 text-xs">
                  <span className="text-muted-foreground">Last done</span>
                  <span className="text-foreground">{rule.last_done_at || '--'}</span>
                </div>
                {actions(rule)}
              </div>
            ))}
          </div>
        ))
      )}
    </div>
  )

  return (
    <>
      {isMobile && !fallback ? cards : (
      <div className="overflow-x-auto rounded-md border border-border bg-card">
        {fallback ?? (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Equipment / service</TableHead>
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
                    <TableRow key={rule.id} className={cn(rule.not_applicable && 'text-muted-foreground')}>
                      <TableCell className="max-w-0">{titleBlock(rule)}</TableCell>
                      <TableCell className="tabular-nums text-muted-foreground">{remainingLabel(rule)}</TableCell>
                      <TableCell className="text-muted-foreground">{rule.last_done_at || '--'}</TableCell>
                      <TableCell>{marks(rule)}</TableCell>
                      <TableCell className="text-right">{actions(rule)}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              ))
            )}
          </Table>
        )}
      </div>
      )}

      <MaintenanceRuleDialog
        rule={creating ? undefined : editingRule}
        equipmentLabel={labelOf(editingRule)}
        open={creating || editingRule !== null}
        onCancel={() => { onCreatingChange?.(false); setEditingRule(null) }}
        onSave={async (input) => {
          const saved = editingRule ? await updateMaintenanceRule(editingRule.id, input) : await createMaintenanceRule(input)
          onCreatingChange?.(false)
          setEditingRule(null)
          return saved
        }}
        onDelete={async (id) => {
          await deleteMaintenanceRule(id)
          setEditingRule(null)
        }}
      />

      <MaintenanceCompleteDialog
        rule={completingRule}
        equipmentLabel={labelOf(completingRule)}
        onCancel={() => setCompletingRule(null)}
        onComplete={async (input) => {
          return completeMaintenanceRule(completingRule!.id, input)
        }}
      />

      <MaintenanceAckDialog
        rule={acknowledgingRule}
        equipmentLabel={labelOf(acknowledgingRule)}
        onCancel={() => setAcknowledgingRule(null)}
        onConfirm={async (reason) => {
          await acknowledgeMaintenanceRule(acknowledgingRule!.id, reason)
          setAcknowledgingRule(null)
        }}
      />

      <HandHoursDialog
        equipmentName={labelOf(loggingReadingRule) ?? loggingReadingRule?.equipment_name ?? ''}
        open={loggingReadingRule !== null}
        onCancel={() => setLoggingReadingRule(null)}
        onSave={async (input) => {
          await recordHandHourReading(loggingReadingRule!.equipment_id!, input)
          setLoggingReadingRule(null)
        }}
      />

      <MaintenanceLastDoneDialog
        rule={settingLastDoneRule}
        equipmentLabel={labelOf(settingLastDoneRule)}
        onCancel={() => setSettingLastDoneRule(null)}
        onConfirm={async (lastDoneAt, lastDoneHours) => {
          await setMaintenanceRuleLastDone(settingLastDoneRule!.id, lastDoneAt, lastDoneHours)
          setSettingLastDoneRule(null)
        }}
      />
    </>
  )
}
