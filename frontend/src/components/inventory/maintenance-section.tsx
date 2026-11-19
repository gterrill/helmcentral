import { useMemo, useState } from 'react'
import { Page, RefreshFailedNote } from '@/components/patterns'
import { Download, Plus, Wrench } from 'lucide-react'

import { Button, buttonVariants } from '@/components/ui/button'
import { Select, SelectItem, SelectPopup, SelectTrigger, SelectValue } from '@/components/ui/select'
import { cn } from '@/lib/utils'
import { MaintenanceRulesTable, groupRulesByStatus } from '@/components/inventory/maintenance-rules-table'
import { ScheduleErrorsAlert } from '@/components/inventory/schedule-errors-alert'
import { EQUIPMENT_SYSTEMS, EQUIPMENT_SYSTEM_LABELS, type EquipmentSystem } from '@/hooks/use-inventory'
import { maintenanceLogExportURL, useMaintenanceRules } from '@/hooks/use-maintenance'

// ADR 0138: the Service schedule section (called Maintenance until ADR 0293;
// its id and address are unchanged) - a plain sortable list grouped by
// status (spec §9), NOT kanban: recurring work has no "column" it belongs
// in permanently, it just moves between the same six states as time and
// hours pass. Follows equipment-index.tsx's own shell (a toolbar above a
// bordered ui/table) since that is this app's one other plain inventory
// index - grouping here is by status instead of by system, and sorting
// within a group puts an acknowledged rule after the unacknowledged ones
// (spec §5), both purely client-side over whatever flat list the hook
// returns, the same way equipment-index.tsx's own system grouping is. The
// rows and their dialogs are maintenance-rules-table.tsx, shared with the
// Jobs page. Open jobs are on the Jobs page, not here (ADR 0293).

const ALL_VALUE = '__all__'

interface MaintenanceSectionProps {
  onOpenEquipment: (id: string) => void
  canWrite?: boolean
}

export function MaintenanceSection({ onOpenEquipment, canWrite = true }: MaintenanceSectionProps) {
  const [system, setSystem] = useState<EquipmentSystem | ''>('')
  const { rules, scheduleErrors, loading, error, refresh } = useMaintenanceRules({ system })
  const [creatingRule, setCreatingRule] = useState(false)

  const groups = useMemo(() => groupRulesByStatus(rules), [rules])

  const fallback = error && rules.length === 0 ? (
    <div className="flex flex-col items-center justify-center gap-3 p-8 text-center">
      <p role="alert" className="text-sm text-destructive">{error}</p>
      <Button type="button" variant="outline" size="sm" onClick={() => { void refresh() }}>Retry</Button>
    </div>
  ) : !loading && groups.length === 0 ? (
    <div className="flex flex-col items-center justify-center gap-3 px-6 py-16 text-center">
      <Wrench className="h-8 w-8 text-muted-foreground" aria-hidden="true" />
      <p className="max-w-md text-sm text-muted-foreground">
        No maintenance rules yet. Add one by hand, or give an item an equipment profile and
        its service jobs appear here.
      </p>
      {canWrite && (
        <Button type="button" onClick={() => setCreatingRule(true)} className="mt-2 gap-2">
          <Plus className="h-4 w-4" aria-hidden="true" />
          New rule
        </Button>
      )}
    </div>
  ) : undefined

  return (
    <Page
      title="Service schedule"
      primaryAction={
        canWrite
          ? { label: 'New rule', icon: <Plus className="h-4 w-4" aria-hidden="true" />, onClick: () => setCreatingRule(true) }
          : undefined
      }
    >
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
        </div>
      </div>

      <ScheduleErrorsAlert errors={scheduleErrors} onOpenEquipment={onOpenEquipment} />

      {error && rules.length > 0 && <RefreshFailedNote reason={error} onRetry={() => { void refresh() }} />}
      <MaintenanceRulesTable
        groups={groups}
        labelRules={rules}
        loading={loading}
        fallback={fallback}
        canWrite={canWrite}
        onOpenEquipment={onOpenEquipment}
        creating={creatingRule}
        onCreatingChange={setCreatingRule}
      />
    </Page>
  )
}
